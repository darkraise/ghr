//go:build unix

package runner

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// mustReturn fails the test when f has not returned after 5 s: opening a FIFO
// nobody writes to blocks for ever.
func mustReturn(t *testing.T, what string, f func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		f()
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("%s blocked", what)
	}
}

func mkfifo(t *testing.T, path string) {
	t.Helper()
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(path, 0o644); err != nil {
		t.Fatal(err)
	}
}

// A job can write its instance directory, and root reads job.json there:
// only a regular file of at most 1 MiB, and never through a link.
func TestReadJobJSONReadsOnlyASmallRegularFile(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	const open, end = `{"job":"`, `"}`
	write("real.json", open+"e2e"+end)
	write("full.json", open+strings.Repeat("x", maxJobFile-len(open)-len(end))+end)
	write("big.json", open+strings.Repeat("x", maxJobFile-len(open)-len(end)+1)+end)
	if err := os.Symlink(filepath.Join(dir, "real.json"), filepath.Join(dir, "link.json")); err != nil {
		t.Fatal(err)
	}
	mkfifo(t, filepath.Join(dir, "fifo.json"))
	for _, tc := range []struct {
		name string
		ok   bool
	}{
		{"real.json", true},
		{"full.json", true},
		{"big.json", false},
		{"link.json", false},
		{"fifo.json", false},
		{"none.json", false},
	} {
		var rec JobRecord
		var err error
		mustReturn(t, "reading "+tc.name, func() { err = readJobJSON(filepath.Join(dir, tc.name), &rec) })
		if (err == nil) != tc.ok || (tc.ok && rec.Job == "") {
			t.Errorf("%s: job of %d bytes, err %v", tc.name, len(rec.Job), err)
		}
		if tc.name == "none.json" && !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("a missing file: %v", err)
		}
	}
}

// A FIFO named job.json blocks neither the tick that looks for a started job
// nor the cleanup that reads the job's record, and is not a job record.
func TestAFIFONamedJobFileBlocksNeitherTickNorCleanup(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if err := h.m.spawn(ctx, h.cfg, "darkmem"); err != nil {
		t.Fatal(err)
	}
	h.gh.setRunner(101, "online", false)
	mkfifo(t, filepath.Join(h.m.instanceDir("aaaaaa"), JobFile))
	mustReturn(t, "the tick", func() { h.m.Tick(ctx) })
	if h.state("aaaaaa") != "idle" {
		t.Fatalf("state %s", h.state("aaaaaa"))
	}
	h.sd.active["ghr-runner-aaaaaa"] = false
	mustReturn(t, "the cleanup", func() {
		h.m.Tick(ctx)
		h.m.Wait()
	})
	if h.state("aaaaaa") != "gone" {
		t.Fatalf("state after the unit stopped: %s\n%s", h.state("aaaaaa"), h.eventText())
	}
}

// Adoption reads job.json in every instance directory, and a FIFO there
// leaves a runner adopted without a job. One in place of ghr.json, beside no
// metadata record, is not read at all.
func TestAdoptIsNotBlockedByFIFOsInInstanceDirs(t *testing.T) {
	h := newHarness(t)
	makeInstance(t, h, "aaaaaa", "darkcloud", 1, false)
	mkfifo(t, filepath.Join(h.m.instanceDir("aaaaaa"), JobFile))
	makeInstance(t, h, "bbbbbb", "darkmem", 2, false)
	os.Remove(filepath.Join(h.m.Paths.Private, "bbbbbb.json"))
	os.Remove(filepath.Join(h.m.instanceDir("bbbbbb"), MetaFile))
	mkfifo(t, filepath.Join(h.m.instanceDir("bbbbbb"), MetaFile))
	h.sd.active["ghr-runner-aaaaaa"] = true
	h.sd.active["ghr-runner-bbbbbb"] = true
	mustReturn(t, "adoption", func() {
		if err := h.m.Adopt(context.Background()); err != nil {
			t.Error(err)
		}
		h.m.Wait()
	})
	if h.state("aaaaaa") != "starting" {
		t.Fatalf("aaaaaa %s", h.state("aaaaaa"))
	}
	if _, err := os.Stat(h.m.instanceDir("bbbbbb")); !os.IsNotExist(err) || !strings.Contains(h.eventText(), "instance bbbbbb had no metadata record; stopped and removed") {
		t.Fatalf("bbbbbb: stat %v\n%s", err, h.eventText())
	}
}
