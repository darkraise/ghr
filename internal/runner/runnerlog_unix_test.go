//go:build unix

package runner

import (
	"os"
	"path/filepath"
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
