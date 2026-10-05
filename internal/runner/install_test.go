package runner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var tarball = []byte("actions-runner tarball")

func tarballSum() string {
	s := sha256.Sum256(tarball)
	return hex.EncodeToString(s[:])
}

// queuedUpdate installs 2.337.0, releases 2.338.0 whose notes carry sum, and
// queues the update. The returned list collects the URLs Fetch was asked for.
func queuedUpdate(t *testing.T, h *harness, sum string) (dist string, fetched *[]string) {
	t.Helper()
	dist = h.linkDist(t, "2.337.0")
	h.gh.setReleases(release("2.338.0", 0, sum), release("2.337.0", -20, ""))
	urls := []string{}
	h.m.Fetch = func(_ context.Context, url, dst string) error {
		urls = append(urls, url)
		return os.WriteFile(dst, tarball, 0o644)
	}
	if err := h.m.QueueUpdate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return dist, &urls
}

func current(t *testing.T, h *harness) string {
	t.Helper()
	p, err := h.host.ReadLink(h.m.Paths.Dist)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Base(p)
}

func distEntries(t *testing.T, dist string) string {
	t.Helper()
	es, err := os.ReadDir(dist)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range es {
		names = append(names, e.Name())
	}
	return strings.Join(names, ",")
}

func TestUpdateInstallsAndSwitchesCurrent(t *testing.T) {
	h := newHarness(t)
	dist, fetched := queuedUpdate(t, h, tarballSum())
	h.m.runUpdate(h.m.maintCtx)
	if got := current(t, h); got != "2.338.0" {
		t.Fatalf("current is %s", got)
	}
	if got := distEntries(t, dist); got != "2.338.0,current" {
		t.Fatalf("dist holds %s; old versions and staging must go", got)
	}
	if !complete(filepath.Join(dist, "2.338.0")) {
		t.Fatal("2.338.0 is not a complete install")
	}
	if _, err := os.Stat(filepath.Join(dist, "2.338.0", "runner.tar.gz")); !os.IsNotExist(err) {
		t.Fatalf("tarball kept: %v", err)
	}
	want := "https://github.com/actions/runner/releases/download/v2.338.0/actions-runner-linux-x64-2.338.0.tar.gz"
	if len(*fetched) != 1 || (*fetched)[0] != want {
		t.Fatalf("fetched %v", *fetched)
	}
	if got := h.host.hostCalls(); got != "Extract 2.338.0.tmp,RunScript 2.338.0.tmp" {
		t.Fatalf("host calls %s", got)
	}
	if !strings.Contains(h.eventText(), "ok  runner updated to 2.338.0") {
		t.Fatalf("events:\n%s", h.eventText())
	}
	u := h.m.Status().RunnerUpdate
	if u.Installed != "2.338.0" || u.Deadline != nil || u.Queued || u.LastOutcome != "ok" || u.LastError != "" ||
		u.LastFinished == nil || !u.LastFinished.Equal(h.now) {
		t.Fatalf("status %+v", u)
	}
	if data, _ := os.ReadFile(h.m.Paths.UpdateState); strings.Contains(string(data), "queued_at") {
		t.Fatalf("queue still on disk: %s", data)
	}
}

func TestUpdateFailuresLeaveCurrent(t *testing.T) {
	for _, c := range []struct {
		name, sum, want string
		setup           func(h *harness)
	}{
		{"checksum mismatch", strings.Repeat("0", 64), "checksum: SHA-256 mismatch", nil},
		{"no checksum", "", "checksum: no linux-x64 checksum in the v2.338.0 release notes", nil},
		{"download", tarballSum(), "download: connection reset", func(h *harness) {
			h.m.Fetch = func(context.Context, string, string) error { return errors.New("connection reset") }
		}},
		{"extract", tarballSum(), "extract: tar: corrupt", func(h *harness) { h.host.setErr("Extract", errors.New("tar: corrupt")) }},
		{"dependencies", tarballSum(), "dependencies: exit status 1", func(h *harness) { h.host.setErr("RunScript", errors.New("exit status 1")) }},
		{"switch", tarballSum(), "switch: read-only file system", func(h *harness) {
			h.host.setErr("SwitchLink", errors.New("read-only file system"))
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			dist, _ := queuedUpdate(t, h, c.sum)
			if c.setup != nil {
				c.setup(h)
			}
			h.m.runUpdate(h.m.maintCtx)
			if got := current(t, h); got != "2.337.0" {
				t.Fatalf("current moved to %s", got)
			}
			if _, err := os.Stat(filepath.Join(dist, "2.338.0.tmp")); !os.IsNotExist(err) {
				t.Fatalf("staging kept: %v", err)
			}
			if !strings.Contains(h.eventText(), "error  runner update failed: "+c.want) {
				t.Fatalf("events:\n%s", h.eventText())
			}
			u := h.m.Status().RunnerUpdate
			if u.Queued || u.LastOutcome != "failed" || !strings.HasPrefix(u.LastError, c.want) || u.Installed != "2.337.0" {
				t.Fatalf("status %+v", u)
			}
		})
	}
}

func TestUpdateSkipsDownloadForCompleteVersion(t *testing.T) {
	h := newHarness(t)
	dist, fetched := queuedUpdate(t, h, tarballSum())
	ready := filepath.Join(dist, "2.338.0")
	if err := os.MkdirAll(ready, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ready, "run.sh"), []byte("#!/bin/sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	h.m.runUpdate(h.m.maintCtx)
	if current(t, h) != "2.338.0" || len(*fetched) != 0 || h.host.hostCalls() != "" {
		t.Fatalf("current %s fetched %v host %q", current(t, h), *fetched, h.host.hostCalls())
	}
}

func TestUpdateFinishesWhenAlreadyCurrent(t *testing.T) {
	h := newHarness(t)
	_, fetched := queuedUpdate(t, h, tarballSum())
	h.gh.setReleases(release("2.337.0", -20, ""))
	h.m.runUpdate(h.m.maintCtx)
	if len(*fetched) != 0 || current(t, h) != "2.337.0" || !strings.Contains(h.eventText(), "info  runner already up to date (2.337.0)") {
		t.Fatalf("fetched %v\n%s", *fetched, h.eventText())
	}
	if u := h.m.Status().RunnerUpdate; u.Queued || u.LastOutcome != "current" {
		t.Fatalf("status %+v", u)
	}
}

func TestUpdateRemovesLeftoverStaging(t *testing.T) {
	h := newHarness(t)
	dist, _ := queuedUpdate(t, h, tarballSum())
	if err := os.MkdirAll(filepath.Join(dist, "2.336.0.tmp", "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	h.m.runUpdate(h.m.maintCtx)
	if got := distEntries(t, dist); got != "2.338.0,current" {
		t.Fatalf("dist holds %s", got)
	}
}

func TestUpdateShutdownKeepsQueue(t *testing.T) {
	h := newHarness(t)
	dist, _ := queuedUpdate(t, h, tarballSum())
	h.m.Fetch = func(ctx context.Context, _, _ string) error {
		h.m.Close()
		<-ctx.Done()
		return ctx.Err()
	}
	h.m.runUpdate(h.m.maintCtx)
	if !strings.Contains(h.eventText(), "warn  runner update interrupted by shutdown") || strings.Contains(h.eventText(), "failed") {
		t.Fatalf("events:\n%s", h.eventText())
	}
	if u := h.m.Status().RunnerUpdate; !u.Queued || u.LastOutcome != "" {
		t.Fatalf("status %+v", u)
	}
	if current(t, h) != "2.337.0" {
		t.Fatal("current moved")
	}
	if _, err := os.Stat(filepath.Join(dist, "2.338.0.tmp")); !os.IsNotExist(err) {
		t.Fatalf("staging kept: %v", err)
	}
}

// A cancel that lands before current switches leaves the installed runner as
// it was and keeps the queue, whether a version was just staged or reused.
func TestUpdateCancelledBeforeTheSwitchKeepsCurrent(t *testing.T) {
	for _, c := range []struct {
		name  string
		setup func(t *testing.T, h *harness, dist string)
	}{
		{"after the dependency script", func(t *testing.T, h *harness, _ string) { h.host.onScript = h.m.Close }},
		{"reusing a complete version", func(t *testing.T, h *harness, dist string) {
			ready := filepath.Join(dist, "2.338.0")
			if err := os.MkdirAll(ready, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(ready, "run.sh"), []byte("#!/bin/sh"), 0o755); err != nil {
				t.Fatal(err)
			}
			h.m.Close()
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			dist, _ := queuedUpdate(t, h, tarballSum())
			c.setup(t, h, dist)
			h.m.runUpdate(h.m.maintCtx)
			if got := current(t, h); got != "2.337.0" {
				t.Fatalf("current moved to %s", got)
			}
			if !strings.Contains(h.eventText(), "warn  runner update interrupted by shutdown") {
				t.Fatalf("events:\n%s", h.eventText())
			}
			if u := h.m.Status().RunnerUpdate; !u.Queued || u.LastOutcome != "" {
				t.Fatalf("status %+v", u)
			}
			if _, err := os.Stat(filepath.Join(dist, "2.338.0.tmp")); !os.IsNotExist(err) {
				t.Fatalf("staging kept: %v", err)
			}
		})
	}
}

// When the state file cannot be rewritten, the update still ends the queue
// for good: the file goes, so a restart does not update again.
func TestFailedQueueClearSurvivesRestart(t *testing.T) {
	h := newHarness(t)
	queuedUpdate(t, h, "")
	if err := os.MkdirAll(h.m.Paths.UpdateState+".tmp", 0o755); err != nil {
		t.Fatal(err)
	}
	h.m.runUpdate(h.m.maintCtx)
	if !strings.Contains(h.eventText(), "save runner update state") || !strings.Contains(h.eventText(), "runner update failed: checksum") {
		t.Fatalf("events:\n%s", h.eventText())
	}
	if _, err := os.Stat(h.m.Paths.UpdateState); !os.IsNotExist(err) {
		t.Fatalf("state file kept: %v", err)
	}
	h.restart(t)
	if u := h.m.Status().RunnerUpdate; u.Queued {
		t.Fatalf("the update is queued again after a restart: %+v", u)
	}
}

// Old versions that cannot be removed after the switch leave a warning; the
// update itself succeeded.
func TestUpdateWarnsWhenOldVersionsStay(t *testing.T) {
	h := newHarness(t)
	dist, _ := queuedUpdate(t, h, tarballSum())
	removeAll = func(path string) error {
		if filepath.Base(path) == "2.330.0" {
			return errors.New("device busy")
		}
		return os.RemoveAll(path)
	}
	t.Cleanup(func() { removeAll = os.RemoveAll })
	h.m.runUpdate(h.m.maintCtx)
	txt := h.eventText()
	if current(t, h) != "2.338.0" || !strings.Contains(txt, "warn  runner update: remove old runner 2.330.0: device busy") ||
		!strings.Contains(txt, "ok  runner updated to 2.338.0") {
		t.Fatalf("current %s\n%s", current(t, h), txt)
	}
	if got := distEntries(t, dist); got != "2.330.0,2.338.0,current" {
		t.Fatalf("dist holds %s", got)
	}
	if u := h.m.Status().RunnerUpdate; u.LastOutcome != "ok" || u.Queued {
		t.Fatalf("status %+v", u)
	}
}
