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

// finish moves _diag into the logs directory and then trims, chowns and
// touches the archive as root. A _diag the job swapped for a link or a file
// is not archived, so none of that reaches what it points at.
func TestFinishArchivesOnlyARealDiagDirectory(t *testing.T) {
	for _, tc := range []struct {
		name  string
		plant func(t *testing.T, diag, elsewhere string)
	}{
		{"symlink", func(t *testing.T, diag, elsewhere string) {
			if err := os.Symlink(elsewhere, diag); err != nil {
				t.Fatal(err)
			}
		}},
		{"regular file", func(t *testing.T, diag, _ string) {
			if err := os.WriteFile(diag, []byte("not a directory"), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.m.spawn(context.Background(), h.cfg, "darkmem")
			elsewhere := filepath.Join(h.root, "elsewhere")
			if err := os.MkdirAll(filepath.Join(elsewhere, "keep"), 0o755); err != nil {
				t.Fatal(err)
			}
			for name, data := range map[string]string{"notes.txt": "notes", "Runner_1.log": "log", "keep/x": "x"} {
				if err := os.WriteFile(filepath.Join(elsewhere, name), []byte(data), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Truncate(filepath.Join(elsewhere, "Runner_1.log"), maxArchivedLog+100); err != nil {
				t.Fatal(err)
			}
			old := h.now.Add(-72 * time.Hour)
			if err := os.Chtimes(elsewhere, old, old); err != nil {
				t.Fatal(err)
			}
			diag := filepath.Join(h.m.instanceDir("aaaaaa"), "_diag")
			if err := os.RemoveAll(diag); err != nil {
				t.Fatal(err)
			}
			tc.plant(t, diag, elsewhere)
			h.sd.active["ghr-runner-aaaaaa"] = false
			mustReturn(t, "finish", func() {
				h.m.refreshUnits(context.Background())
				h.m.Wait()
			})
			archive := filepath.Join(h.m.Paths.Logs, "aaaaaa")
			if _, err := os.Lstat(archive); !os.IsNotExist(err) {
				t.Fatalf("archive created: %v\n%s", err, h.eventText())
			}
			if strings.Contains(strings.Join(h.host.chowned, ","), archive) {
				t.Fatalf("chowned %v", h.host.chowned)
			}
			for name, size := range map[string]int64{"notes.txt": 5, "Runner_1.log": maxArchivedLog + 100, "keep/x": 1} {
				if fi, err := os.Stat(filepath.Join(elsewhere, name)); err != nil || fi.Size() != size {
					t.Fatalf("%s: %v, %v", name, fi, err)
				}
			}
			if fi, err := os.Stat(elsewhere); err != nil || !fi.ModTime().Equal(old) {
				t.Fatalf("target touched: %v, %v", fi, err)
			}
			if _, err := h.m.RunnerLog("aaaaaa", ""); err == nil {
				t.Fatal("a log was read")
			}
		})
	}
}

// The archive path is the job's id under the logs directory, which the job
// cannot write to; if a link is there all the same, finish must not trim,
// chown or touch what it points at.
func TestFinishLeavesASymlinkAtTheArchivePathAlone(t *testing.T) {
	for _, tc := range []struct {
		name     string
		withDiag bool
	}{
		{"no _diag", false},
		{"a real _diag", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.m.spawn(context.Background(), h.cfg, "darkmem")
			elsewhere := filepath.Join(h.root, "elsewhere")
			if err := os.MkdirAll(elsewhere, 0o755); err != nil {
				t.Fatal(err)
			}
			for name, data := range map[string]string{"notes.txt": "notes", "Runner_1.log": "log"} {
				if err := os.WriteFile(filepath.Join(elsewhere, name), []byte(data), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Truncate(filepath.Join(elsewhere, "Runner_1.log"), maxArchivedLog+100); err != nil {
				t.Fatal(err)
			}
			old := h.now.Add(-72 * time.Hour)
			if err := os.Chtimes(elsewhere, old, old); err != nil {
				t.Fatal(err)
			}
			archive := filepath.Join(h.m.Paths.Logs, "aaaaaa")
			if err := os.MkdirAll(h.m.Paths.Logs, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(elsewhere, archive); err != nil {
				t.Fatal(err)
			}
			diag := filepath.Join(h.m.instanceDir("aaaaaa"), "_diag")
			if err := os.RemoveAll(diag); err != nil {
				t.Fatal(err)
			}
			if tc.withDiag {
				if err := os.MkdirAll(diag, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			h.sd.active["ghr-runner-aaaaaa"] = false
			mustReturn(t, "finish", func() {
				h.m.refreshUnits(context.Background())
				h.m.Wait()
			})
			if strings.Contains(strings.Join(h.host.chowned, ","), archive) {
				t.Fatalf("chowned %v", h.host.chowned)
			}
			for name, size := range map[string]int64{"notes.txt": 5, "Runner_1.log": maxArchivedLog + 100} {
				if fi, err := os.Stat(filepath.Join(elsewhere, name)); err != nil || fi.Size() != size {
					t.Fatalf("%s: %v, %v", name, fi, err)
				}
			}
			if fi, err := os.Stat(elsewhere); err != nil || !fi.ModTime().Equal(old) {
				t.Fatalf("target touched: %v, %v", fi, err)
			}
			if c, err := h.m.RunnerLog("aaaaaa", ""); err == nil && c.Data != "" {
				t.Fatalf("read %.40q through the link", c.Data)
			}
		})
	}
}

// A name in _diag can be a second name for a file elsewhere on the host. The
// archive is trimmed as root, so cutting such a log to size would cut the
// original too: a log with more than one name is dropped from the archive and
// the original is left as it was.
func TestArchiveDropsAHardLinkedLog(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkmem")
	diag := filepath.Join(h.m.instanceDir("aaaaaa"), "_diag")
	outside := filepath.Join(h.root, "outside.log")
	if err := os.WriteFile(outside, []byte("not a job log\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(outside, maxArchivedLog+100); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(outside, filepath.Join(diag, "Runner_1.log")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(diag, "Worker_1.log"), []byte("head\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.sd.active["ghr-runner-aaaaaa"] = false
	mustReturn(t, "finish", func() {
		h.m.refreshUnits(context.Background())
		h.m.Wait()
	})
	entries, err := os.ReadDir(filepath.Join(h.m.Paths.Logs, "aaaaaa"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if !reflect.DeepEqual(names, []string{"Worker_1.log"}) {
		t.Fatalf("archive holds %v\n%s", names, h.eventText())
	}
	if fi, err := os.Stat(outside); err != nil || fi.Size() != maxArchivedLog+100 {
		t.Fatalf("the original: %v, %v", fi, err)
	}
}
