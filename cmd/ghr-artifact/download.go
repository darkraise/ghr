package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/darkraise/ghr/internal/artifact"
	"github.com/darkraise/ghr/internal/s3"
)

// errMissing exits 3, so the download action can fall back to GitHub.
var errMissing = errors.New("no artifact matched")

// objectMissing is s3.IsNotFound without a missing bucket: a wrong bucket must
// fail the step, not fall back to GitHub.
func objectMissing(err error) bool {
	var se *s3.Error
	return s3.IsNotFound(err) && !(errors.As(err, &se) && se.Code == "NoSuchBucket")
}

type match struct{ name, key string }

func download(ctx context.Context, args []string, env func(string) string, stdout, stderr io.Writer) error {
	fs, storeFlag, keyFlag := newFlags("download", stderr)
	name := fs.String("name", "", "the artifact to download; empty downloads every artifact of the run")
	pattern := fs.String("pattern", "", "a glob matching the artifact names to download; ignored with --name")
	dest := fs.String("path", "", "where to put the files (default $GITHUB_WORKSPACE)")
	merge := fs.Bool("merge-multiple", false, "put every matched artifact straight into --path instead of one directory each")
	runFlag := fs.String("run-id", "", "the run that uploaded the artifacts (default this run)")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if *name != "" {
		if err := artifact.ValidName(*name); err != nil {
			return err
		}
	} else if _, err := path.Match(*pattern, ""); err != nil {
		return usageError(fmt.Sprintf("--pattern %q is not a valid glob", *pattern))
	}
	var runID int64
	if *runFlag != "" {
		id, err := strconv.ParseInt(*runFlag, 10, 64)
		if err != nil || id <= 0 {
			return usageError(fmt.Sprintf("--run-id %q is not a run ID", *runFlag))
		}
		runID = id
	}
	j, err := loadJob(env, *storeFlag, *keyFlag)
	if err != nil {
		return err
	}
	if runID == 0 {
		runID = j.runID
	}
	target, err := resolvePath(*dest, j.workspace)
	if err != nil {
		return err
	}
	client, err := newClient(j.store, j.cred)
	if err != nil {
		return err
	}

	var found []match
	if *name != "" {
		found = []match{{*name, artifact.ObjectKey(j.owner, j.repo, runID, *name)}}
	} else {
		objs, err := client.List(ctx, artifact.RunPrefix(j.owner, j.repo, runID))
		if err != nil {
			return describe(err, j.store)
		}
		for _, o := range objs {
			_, _, id, n, ok := artifact.ParseObjectKey(o.Key)
			if !ok || id != runID || artifact.ValidName(n) != nil || !filepath.IsLocal(n) {
				continue
			}
			if ok, _ := path.Match(*pattern, n); *pattern == "" || ok {
				found = append(found, match{n, o.Key})
			}
		}
		sort.Slice(found, func(a, b int) bool { return found[a].name < found[b].name })
	}

	for _, m := range found {
		dir := target
		if *name == "" && !*merge {
			dir = filepath.Join(target, m.name)
		}
		files, err := fetch(ctx, client, j, m, dir)
		if objectMissing(err) {
			if *name != "" {
				found = nil
				break
			}
			// Listed a moment ago, so something deleted it during the download.
			return describe(err, j.store)
		}
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "Downloaded %s: %d files to %s\n", m.name, files, dir)
	}
	if len(found) == 0 {
		what := "run " + strconv.FormatInt(runID, 10)
		switch {
		case *name != "":
			what = "the name " + *name + " in " + what
		case *pattern != "":
			what = "the pattern " + *pattern + " in " + what
		}
		fmt.Fprintf(stdout, "No artifact matched %s in the artifact store.\n", what)
		if err := writeOutputs(env("GITHUB_OUTPUT"), "missing", "true"); err != nil {
			return err
		}
		return errMissing
	}
	return writeOutputs(env("GITHUB_OUTPUT"), "download-path", target)
}

func resolvePath(p, workspace string) (string, error) {
	if p == "" {
		p = workspace
	}
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("cannot expand ~ in %q: %w", p, err)
		}
		p = home + p[1:]
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(workspace, p)
	}
	return filepath.Abs(p)
}

// fetch saves the object to RUNNER_TEMP while hashing it, and extracts it only
// when the hash matches the digest recorded at upload.
func fetch(ctx context.Context, client *s3.Client, j job, m match, dir string) (int, error) {
	body, head, err := client.Get(ctx, m.key)
	if err != nil {
		if objectMissing(err) {
			return 0, err
		}
		return 0, describe(err, j.store)
	}
	defer body.Close()
	zf, err := os.CreateTemp(j.temp, "ghr-artifact-*.zip")
	if err != nil {
		return 0, err
	}
	defer os.Remove(zf.Name())
	defer zf.Close()
	sum := sha256.New()
	size, err := io.Copy(io.MultiWriter(zf, sum), body)
	if err != nil {
		return 0, describe(err, j.store)
	}
	if "sha256:"+hex.EncodeToString(sum.Sum(nil)) != artifact.ParseMeta(head.Meta).Digest {
		return 0, fmt.Errorf("%s.zip is damaged: its digest does not match; upload it again", m.name)
	}
	files, err := artifact.Extract(zf, size, dir)
	if err != nil {
		return 0, fmt.Errorf("extracting %s.zip: %w", m.name, err)
	}
	return files, nil
}
