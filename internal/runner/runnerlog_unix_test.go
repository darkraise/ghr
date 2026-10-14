//go:build unix

package runner

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/model"
)

// readLog runs RunnerLog with a deadline: reading a FIFO nobody writes to
// would block for ever.
func readLog(t *testing.T, h *harness, id string) (model.LogChunk, error) {
	t.Helper()
	type result struct {
		c   model.LogChunk
		err error
	}
	done := make(chan result, 1)
	go func() {
		c, err := h.m.RunnerLog(id, "")
		done <- result{c, err}
	}()
	select {
	case r := <-done:
		return r.c, r.err
	case <-time.After(5 * time.Second):
		t.Fatal("RunnerLog blocked")
		return model.LogChunk{}, nil
	}
}

// The job wrote these files and the daemon reads them as root. Only regular
// files inside the log directory are read.
func TestRunnerLogReadsOnlyRealFilesInsideTheLogDirectory(t *testing.T) {
	h := newHarness(t)
	diag := filepath.Join(h.m.instanceDir("aaaaaa"), "_diag")
	if err := os.MkdirAll(diag, 0o755); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(h.root, "token")
	if err := os.WriteFile(secret, []byte("SECRET"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(diag, "Runner_1.log"), []byte("real\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(diag, "Runner_2.log")); err != nil {
		t.Fatal(err)
	}
	// A link to a file the job owns, but outside the log directory.
	if err := os.WriteFile(filepath.Join(h.m.instanceDir("aaaaaa"), "notes.txt"), []byte("NOTES"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../notes.txt", filepath.Join(diag, "Runner_3.log")); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(diag, "Worker_1.log"), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := readLog(t, h, "aaaaaa")
	if err != nil || c.Data != "==> Runner_1.log <==\nreal\n" {
		t.Fatalf("data %q err %v", c.Data, err)
	}
}

// The archive directory is root's, but its files came from the job.
func TestRunnerLogSkipsASymlinkInTheArchive(t *testing.T) {
	h := newHarness(t)
	archive := filepath.Join(h.m.Paths.Logs, "aaaaaa")
	if err := os.MkdirAll(archive, 0o755); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(h.root, "token")
	if err := os.WriteFile(secret, []byte("SECRET"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(archive, "Runner_1.log"), []byte("real\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(archive, "Worker_1.log")); err != nil {
		t.Fatal(err)
	}
	c, err := readLog(t, h, "aaaaaa")
	if err != nil || c.Data != "==> Runner_1.log <==\nreal\n" {
		t.Fatalf("data %q err %v", c.Data, err)
	}
}

func TestRunnerLogRefusesASymlinkedLogDirectory(t *testing.T) {
	h := newHarness(t)
	elsewhere := filepath.Join(h.root, "elsewhere")
	if err := os.MkdirAll(elsewhere, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(elsewhere, "Runner_1.log"), []byte("SECRET"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(h.m.instanceDir("aaaaaa"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, filepath.Join(h.m.instanceDir("aaaaaa"), "_diag")); err != nil {
		t.Fatal(err)
	}
	if c, err := readLog(t, h, "aaaaaa"); err == nil {
		t.Fatalf("read through a symlinked log directory: %q", c.Data)
	}
}

// The job owns _diag and can fill it. Its archive keeps only the logs ghr
// shows, each cut to its head; the step output the runner pages under _diag
// is on GitHub.
func TestArchiveKeepsOnlyTheRunnerLogs(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkmem")
	diag := filepath.Join(h.m.instanceDir("aaaaaa"), "_diag")
	for _, d := range []string{"pages", "blocks", "Worker_2.log"} {
		if err := os.MkdirAll(filepath.Join(diag, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for name, data := range map[string]string{"pages/x_y_1.log": "step output", "blocks/x_y.1": "block",
		"notes.txt": "notes", "Runner_1.log": "real\n", "Worker_1.log": "head\n"} {
		if err := os.WriteFile(filepath.Join(diag, name), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Truncate(filepath.Join(diag, "Worker_1.log"), maxArchivedLog+100); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(h.root, "token")
	if err := os.WriteFile(secret, []byte("SECRET"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(diag, "Runner_2.log")); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(diag, "Worker_3.log"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.sd.active["ghr-runner-aaaaaa"] = false
	mustReturn(t, "finish", func() {
		h.m.refreshUnits(context.Background())
		h.m.Wait()
	})
	archive := filepath.Join(h.m.Paths.Logs, "aaaaaa")
	entries, err := os.ReadDir(archive)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if !reflect.DeepEqual(names, []string{"Runner_1.log", "Worker_1.log"}) {
		t.Fatalf("archive holds %v\n%s", names, h.eventText())
	}
	if fi, err := os.Stat(filepath.Join(archive, "Worker_1.log")); err != nil || fi.Size() != maxArchivedLog {
		t.Fatalf("worker log %v, %v", fi, err)
	}
	if _, err := os.Lstat(secret); err != nil {
		t.Fatalf("the symlink's target went: %v", err)
	}
	c, err := readLog(t, h, "aaaaaa")
	if err != nil || !strings.HasPrefix(c.Data, "==> Runner_1.log <==\nreal\n\n==> Worker_1.log <==\nhead\n") {
		t.Fatalf("data %.80q err %v", c.Data, err)
	}
}
