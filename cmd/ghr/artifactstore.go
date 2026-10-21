package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/model"
)

const artifactStoreUsage = "usage: ghr artifact-store [set-key admin|job]"

func artifactStoreCmd(ctx context.Context, c *api.Client, args []string, stdin io.Reader, out, prompts io.Writer) error {
	switch {
	case len(args) == 0:
		s, err := c.ArtifactStore(ctx)
		if err != nil {
			return err
		}
		printArtifactStore(out, s)
		return nil
	case len(args) == 2 && args[0] == "set-key" && (args[1] == "admin" || args[1] == "job"):
		id, secret, err := readArtifactKey(ctx, stdin, prompts)
		if err != nil {
			return err
		}
		if err := c.SetArtifactKey(ctx, model.ArtifactKeyRequest{Which: args[1], ID: id, Secret: secret}); err != nil {
			return err
		}
		if args[1] == "job" {
			fmt.Fprintln(out, "job key set; switched-on repositories get it on the next pass")
		} else {
			fmt.Fprintln(out, "admin key set")
		}
		return nil
	}
	return usageError(artifactStoreUsage)
}

// maxPipedKey bounds what is read from a pipe: the daemon refuses an ID or a
// secret over 64 KB, and both line endings must fit after them.
const maxPipedKey = 2*(64<<10) + 4

// readArtifactKey asks for the access key ID and the secret without echo at
// a terminal, or reads them as the two lines of a pipe. The daemon trims
// both and refuses an empty one.
func readArtifactKey(ctx context.Context, stdin io.Reader, prompts io.Writer) (id, secret string, err error) {
	if stdinIsTerminal() {
		if id, err = askPassword(ctx, prompts, "Access key ID: "); err != nil {
			return "", "", err
		}
		secret, err = askPassword(ctx, prompts, "Secret access key: ")
		return id, secret, err
	}
	data, err := io.ReadAll(io.LimitReader(stdin, maxPipedKey))
	if err != nil {
		return "", "", err
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) != 2 {
		return "", "", errors.New("give the access key ID and the secret access key on two lines of standard input")
	}
	return lines[0], lines[1], nil
}

func printArtifactStore(out io.Writer, s model.ArtifactStore) {
	safe := stdoutLine()
	if !s.Configured {
		fmt.Fprintln(out, "self-hosted storage is not set up; set it up in the web UI under Settings › Self-hosted storage")
		return
	}
	fmt.Fprintf(out, "endpoint %s  bucket %s  region %s\n", safe(s.Endpoint), safe(s.Bucket), safe(s.Region))
	fmt.Fprintf(out, "admin key %s  job key %s\n", artifactKeyText(s.AdminKey), artifactKeyText(s.JobKey))
	used := fmt.Sprintf("used %s, %d artifacts", artifactUsed(s.UsedBytes, s.CapacityBytes), s.Count)
	if s.OtherBytes > 0 {
		used += ", " + model.HumanBytes(s.OtherBytes) + " outside ghr's prefixes"
	}
	fmt.Fprintln(out, used)
	listed := "not listed yet"
	if s.CheckedAt != nil {
		listed = "listed " + stamp(*s.CheckedAt)
	}
	if s.Error != "" {
		listed += "  error: " + safe(s.Error)
	}
	fmt.Fprintln(out, listed)
	if op := s.Operation; op != nil && op.FinishedAt == nil {
		fmt.Fprintf(out, "deleting %d of %d objects in %s\n", op.Done, op.Total, safe(op.Repo))
	}
	fmt.Fprintln(out)
	w := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "REPO\tSTATE\tON\tSIZE\tARTIFACTS\tCLEANUP")
	for _, r := range s.Repos {
		on := "off"
		if r.Enabled {
			on = "on"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%s (%s)\n", safe(r.Name), safe(r.State), on, model.HumanBytes(r.Bytes), r.Count, r.Cleanup, r.CleanupSource)
	}
	w.Flush()
}

// artifactKeyText shows a key by the shortened ID the daemon sends; the
// daemon never sends the secret.
func artifactKeyText(k *model.ArtifactKeyInfo) string {
	if k == nil {
		return "not set"
	}
	return stdoutLine()(k.ID) + " (set " + stamp(k.SetAt) + ")"
}

// artifactUsed is the bytes stored, of the capacity when one is set, in
// decimal units as the web UI shows them.
func artifactUsed(used, capacity int64) string {
	if capacity > 0 {
		return model.HumanBytes(used) + " of " + model.HumanBytes(capacity)
	}
	return model.HumanBytes(used)
}

// artifactStoreLine is the status line about self-hosted artifact storage.
func artifactStoreLine(s model.ArtifactStoreSummary) string {
	if s.Error != "" {
		return "artifact store: " + stdoutLine()(s.Error)
	}
	repos := fmt.Sprintf("%d repositories", s.ReposOn)
	if s.ReposOn == 1 {
		repos = "1 repository"
	}
	return "artifact store: " + artifactUsed(s.UsedBytes, s.CapacityBytes) + ", " + repos + " on"
}
