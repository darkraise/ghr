package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/darkraise/ghr/internal/artifact"
	"github.com/darkraise/ghr/internal/s3"
)

func upload(ctx context.Context, args []string, env func(string) string, stdout, stderr io.Writer) error {
	fs, storeFlag, keyFlag := newFlags("upload", stderr)
	name := fs.String("name", "artifact", "the artifact name")
	paths := fs.String("path", "", "files, directories or wildcard patterns, one per line; ! excludes")
	retention := fs.Int("retention-days", 0, "days until ghr deletes the artifact, 1-90; 0 leaves it to ghr's cleanup age")
	ifNone := fs.String("if-no-files-found", "warn", "warn, error or ignore")
	overwrite := fs.Bool("overwrite", false, "replace an artifact of the same name uploaded by this run attempt")
	level := fs.Int("compression-level", 6, "zip compression level, 0 (none) to 9 (best)")
	hidden := fs.Bool("include-hidden-files", false, "include files and directories whose names start with a dot")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	switch {
	case strings.TrimSpace(*paths) == "":
		return usageError("--path is required")
	case *retention < 0 || *retention > artifact.MaxRetentionDays:
		return usageError(fmt.Sprintf("--retention-days must be between 1 and %d, or 0", artifact.MaxRetentionDays))
	case *ifNone != "warn" && *ifNone != "error" && *ifNone != "ignore":
		return usageError("--if-no-files-found must be warn, error or ignore")
	case *level < 0 || *level > 9:
		return usageError("--compression-level must be between 0 and 9")
	}
	if err := artifact.ValidName(*name); err != nil {
		return err
	}
	j, err := loadJob(env, *storeFlag, *keyFlag)
	if err != nil {
		return err
	}

	root, files, err := artifact.Collect(strings.Split(*paths, "\n"), *hidden, j.workspace)
	if errors.Is(err, artifact.ErrNoFiles) {
		msg := "No files were found with the provided path: " + *paths + ". No artifacts will be uploaded."
		switch *ifNone {
		case "error":
			return errors.New(msg)
		case "warn":
			fmt.Fprintln(stdout, "::warning::"+command(msg))
		default:
			fmt.Fprintln(stdout, msg)
		}
		return nil
	}
	if err != nil {
		return err
	}

	client, err := newClient(j.store, j.cred)
	if err != nil {
		return err
	}
	key := artifact.ObjectKey(j.owner, j.repo, j.runID, *name)
	head, err := client.Head(ctx, key)
	switch {
	case err == nil:
		if artifact.ParseMeta(head.Meta).Attempt == j.attempt && !*overwrite {
			return fmt.Errorf("an artifact named %s was already uploaded by this run attempt; set overwrite: true to replace it", *name)
		}
	case !s3.IsNotFound(err):
		return describe(err, j.store)
	}

	zf, err := os.CreateTemp(j.temp, "ghr-artifact-*.zip")
	if err != nil {
		return err
	}
	defer os.Remove(zf.Name())
	defer zf.Close()
	sum := sha256.New()
	if err := artifact.WriteZip(io.MultiWriter(zf, sum), root, files, *level); err != nil {
		return fmt.Errorf("zipping %s: %w", root, err)
	}
	size, err := zf.Seek(0, io.SeekCurrent)
	if err != nil {
		return err
	}
	digest := "sha256:" + hex.EncodeToString(sum.Sum(nil))

	meta := artifact.Meta{
		RunID:    j.runID,
		Attempt:  j.attempt,
		Workflow: env("GITHUB_WORKFLOW"),
		Job:      env("GITHUB_JOB"),
		SHA:      env("GITHUB_SHA"),
		Ref:      env("GITHUB_REF_NAME"),
		Files:    len(files),
		Digest:   digest,
		Uploader: "ghr-artifact/" + version,
	}
	if *retention > 0 {
		exp := now().Add(time.Duration(*retention) * 24 * time.Hour).UTC().Truncate(time.Second)
		meta.Expires = &exp
	}
	err = client.Upload(ctx, key, zf, size, s3.PutOptions{
		ContentType:        "application/zip",
		ContentDisposition: artifact.ContentDisposition(*name),
		Meta:               meta.Map(),
	})
	if err != nil {
		return describe(err, j.store)
	}
	fmt.Fprintf(stdout, "Uploaded %s: %d files, %d bytes, %s\n", key, len(files), size, digest)
	if err := writeOutputs(env("GITHUB_OUTPUT"), "object-key", key, "artifact-digest", digest); err != nil {
		return fmt.Errorf("uploaded %s, but cannot write the step outputs: %w", key, err)
	}
	return nil
}
