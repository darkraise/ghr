package runner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/github"
)

var published = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

// release is an actions/runner release published days after published.
func release(ver string, days int, sum string) github.Release {
	body := "release notes"
	if sum != "" {
		body += "\n<!-- BEGIN SHA linux-x64 -->" + sum + "<!-- END SHA linux-x64 -->"
	}
	return github.Release{TagName: "v" + ver, PublishedAt: published.AddDate(0, 0, days), Body: body}
}

func TestFactsFor(t *testing.T) {
	rels := []github.Release{release("2.339.0", 9, ""), release("2.338.0", 3, ""), release("2.337.0", 0, ""), {TagName: "nightly"}}
	for _, c := range []struct {
		installed, latest string
		deadline          time.Time
	}{
		{"2.339.0", "2.339.0", time.Time{}},                 // current
		{"2.338.0", "2.339.0", published.AddDate(0, 0, 39)}, // one newer
		{"2.336.0", "2.339.0", published.AddDate(0, 0, 30)}, // several newer: the earliest counts
		{"2.340.0", "2.339.0", time.Time{}},                 // installed newer than latest
		{"", "2.339.0", time.Time{}},                        // installed unknown
	} {
		f, ok := factsFor(c.installed, rels)
		if !ok || f.version.String() != c.latest || !f.deadline.Equal(c.deadline) {
			t.Errorf("installed %q: latest %s deadline %v ok %v; want %s %v", c.installed, f.version, f.deadline, ok, c.latest, c.deadline)
		}
	}
	if _, ok := factsFor("2.337.0", []github.Release{{TagName: "nightly"}}); ok {
		t.Fatal("a listing without version tags gave facts")
	}
}

func TestReleaseCheckWarnsOncePerVersion(t *testing.T) {
	h := newHarness(t)
	h.linkDist(t, "2.337.0")
	h.gh.setReleases(release("2.338.0", 0, ""), release("2.337.0", -20, ""))
	h.m.Tick(context.Background())
	const warning = "warn  runner 2.338.0 is available (installed 2.337.0); update by 2026-10-31"
	if n := strings.Count(h.eventText(), warning); n != 1 {
		t.Fatalf("%d warnings:\n%s", n, h.eventText())
	}
	u := h.m.Status().RunnerUpdate
	if u.Installed != "2.337.0" || u.Latest != "2.338.0" || u.Deadline == nil || !u.Deadline.Equal(published.AddDate(0, 0, 30)) ||
		u.CheckedAt == nil || !u.CheckedAt.Equal(h.now) || u.LatestPublished == nil || u.CheckError != "" || u.Queued {
		t.Fatalf("status %+v", u)
	}

	h.now = h.now.Add(23 * time.Hour)
	h.m.Tick(context.Background())
	if n := strings.Count(h.gh.callsText(), "ListRunnerReleases"); n != 1 {
		t.Fatalf("checked again within a day: %s", h.gh.callsText())
	}
	h.now = h.now.Add(time.Hour)
	h.m.Tick(context.Background())
	if n := strings.Count(h.gh.callsText(), "ListRunnerReleases"); n != 2 || strings.Count(h.eventText(), warning) != 1 {
		t.Fatalf("daily check: %s\n%s", h.gh.callsText(), h.eventText())
	}

	// A restarted daemon checks at once and remembers the warned version.
	h.restart(t)
	h.m.Tick(context.Background())
	if n := strings.Count(h.gh.callsText(), "ListRunnerReleases"); n != 3 || strings.Contains(h.eventText(), "is available") {
		t.Fatalf("after a restart: %s\n%s", h.gh.callsText(), h.eventText())
	}
	h.gh.setReleases(release("2.339.0", 5, ""), release("2.338.0", 0, ""))
	h.now = h.now.Add(24 * time.Hour)
	h.m.Tick(context.Background())
	if !strings.Contains(h.eventText(), "runner 2.339.0 is available (installed 2.337.0); update by 2026-10-31") {
		t.Fatalf("no warning for the next version:\n%s", h.eventText())
	}
}

func TestReleaseCheckFailureRetriesAfterAnHour(t *testing.T) {
	h := newHarness(t)
	h.linkDist(t, "2.337.0")
	h.gh.setErr("ListRunnerReleases ", &github.APIError{Status: 502, Kind: github.ErrServer, Message: "bad gateway"})
	h.m.Tick(context.Background())
	u := h.m.Status().RunnerUpdate
	if u.CheckError != "github: 502 bad gateway" || u.CheckedAt != nil || u.Installed != "2.337.0" {
		t.Fatalf("status %+v", u)
	}
	if strings.Contains(h.eventText(), "runner") {
		t.Fatalf("a failed check posted:\n%s", h.eventText())
	}
	h.gh.setErr("ListRunnerReleases ", nil)
	h.gh.setReleases(release("2.337.0", 0, ""))
	h.now = h.now.Add(59 * time.Minute)
	h.m.Tick(context.Background())
	if u := h.m.Status().RunnerUpdate; u.CheckedAt != nil {
		t.Fatalf("retried before an hour: %+v", u)
	}
	h.now = h.now.Add(time.Minute)
	h.m.Tick(context.Background())
	if u := h.m.Status().RunnerUpdate; u.CheckedAt == nil || u.CheckError != "" || u.Latest != "2.337.0" || u.Deadline != nil {
		t.Fatalf("retry: %+v", u)
	}
}

func TestReleaseCheckWaitsWhileDegraded(t *testing.T) {
	h := newHarness(t)
	h.gh.setErr("ListRuns darkcloud", &github.APIError{Status: 401, Kind: github.ErrAuth})
	h.m.Tick(context.Background())
	if strings.Contains(h.gh.callsText(), "ListRunnerReleases") {
		t.Fatalf("checked while degraded: %s", h.gh.callsText())
	}
}

func TestReleaseCheckWithoutDistIsUnknown(t *testing.T) {
	h := newHarness(t)
	h.m.Paths.Dist = h.root + "/dist/current"
	h.gh.setReleases(release("2.338.0", 0, ""))
	h.m.Tick(context.Background())
	if u := h.m.Status().RunnerUpdate; u.Installed != "" || u.Latest != "2.338.0" || u.Deadline != nil {
		t.Fatalf("status %+v", u)
	}
	if strings.Contains(h.eventText(), "is available") {
		t.Fatalf("warned without an installed version:\n%s", h.eventText())
	}
	if err := h.m.QueueUpdate(context.Background()); !errors.Is(err, ErrNoDist) {
		t.Fatalf("queue without dist: %v", err)
	}
}

func TestQueueAndCancelUpdate(t *testing.T) {
	h := newHarness(t)
	h.linkDist(t, "2.337.0")
	h.gh.setReleases(release("2.337.0", 0, ""))
	err := h.m.QueueUpdate(context.Background())
	var utd UpToDateError
	if !errors.As(err, &utd) || err.Error() != "runner 2.337.0 is already up to date" {
		t.Fatalf("current: %v", err)
	}

	h.gh.setReleases(release("2.338.0", 0, ""))
	if err := h.m.QueueUpdate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := h.m.QueueUpdate(context.Background()); err != nil {
		t.Fatalf("queueing twice: %v", err)
	}
	if n := strings.Count(h.eventText(), "info  runner update queued"); n != 1 {
		t.Fatalf("%d queued events:\n%s", n, h.eventText())
	}
	if u := h.m.Status().RunnerUpdate; !u.Queued || u.QueuedAt == nil || !u.QueuedAt.Equal(h.now) {
		t.Fatalf("status %+v", u)
	}

	// The queue survives a restart.
	h.restart(t)
	if u := h.m.Status().RunnerUpdate; !u.Queued || u.QueuedAt == nil || !u.QueuedAt.Equal(h.now) {
		t.Fatalf("queue lost on restart: %+v", u)
	}

	if err := h.m.CancelUpdate(); err != nil {
		t.Fatal(err)
	}
	if err := h.m.CancelUpdate(); err != nil {
		t.Fatalf("cancelling nothing: %v", err)
	}
	if n := strings.Count(h.eventText(), "info  runner update cancelled"); n != 1 {
		t.Fatalf("%d cancelled events:\n%s", n, h.eventText())
	}
	data, err := os.ReadFile(h.m.Paths.UpdateState)
	if err != nil || strings.Contains(string(data), "queued_at") || !strings.Contains(string(data), `"warned_version":"2.338.0"`) {
		t.Fatalf("state file %s %v", data, err)
	}

	h.m.mu.Lock()
	h.m.upd.running = true
	h.m.mu.Unlock()
	if err := h.m.QueueUpdate(context.Background()); !errors.Is(err, ErrUpdateRunning) {
		t.Fatalf("queue while running: %v", err)
	}
	if err := h.m.CancelUpdate(); !errors.Is(err, ErrUpdateRunning) {
		t.Fatalf("cancel while running: %v", err)
	}
}

func TestQueueUpdatePassesGitHubErrors(t *testing.T) {
	h := newHarness(t)
	h.linkDist(t, "2.337.0")
	limit := &github.APIError{Status: 403, Kind: github.ErrRateLimit, RetryAt: h.now.Add(time.Minute)}
	h.gh.setErr("ListRunnerReleases ", limit)
	if err := h.m.QueueUpdate(context.Background()); !errors.Is(err, limit) {
		t.Fatalf("rate limit: %v", err)
	}
	if h.m.apiAllowed(h.now) {
		t.Fatal("a rate-limited check did not pause the API")
	}
	h.m.Close()
	if err := h.m.QueueUpdate(context.Background()); !errors.Is(err, ErrClosed) {
		t.Fatalf("after Close: %v", err)
	}
}

func TestUnreadableUpdateStateStartsEmpty(t *testing.T) {
	h := newHarness(t)
	if err := os.WriteFile(h.m.Paths.UpdateState, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.restart(t)
	if !strings.Contains(h.eventText(), "runner update state unreadable, starting empty") || h.m.Status().RunnerUpdate.Queued {
		t.Fatalf("events:\n%s", h.eventText())
	}
}

// A rate limit on the release check pauses the API as on every GitHub call;
// the check itself posts nothing.
func TestReleaseCheckRateLimitPostsNoRunnerEvent(t *testing.T) {
	h := newHarness(t)
	h.linkDist(t, "2.337.0")
	h.gh.setErr("ListRunnerReleases ", &github.APIError{Status: 403, Kind: github.ErrRateLimit, RetryAt: h.now.Add(time.Hour)})
	h.m.Tick(context.Background())
	if h.m.apiAllowed(h.now) {
		t.Fatal("the rate limit did not pause the API")
	}
	if txt := h.eventText(); !strings.Contains(txt, "GitHub rate limit") || strings.Contains(txt, "runner") {
		t.Fatalf("events:\n%s", txt)
	}
}

// A check that was in flight while an install switched dist/current does
// not overwrite the install's facts.
func TestStaleReleaseCheckIsDropped(t *testing.T) {
	h := newHarness(t)
	h.linkDist(t, "2.337.0")
	h.gh.setReleases(release("2.338.0", 0, ""), release("2.337.0", -20, ""))
	h.gh.hook = func(method string) {
		if method != "ListRunnerReleases" {
			return
		}
		h.m.mu.Lock()
		h.m.upd.installed, h.m.upd.deadline, h.m.upd.gen = "2.338.0", time.Time{}, h.m.upd.gen+1
		h.m.mu.Unlock()
	}
	if _, err := h.m.checkRelease(context.Background()); err != nil {
		t.Fatal(err)
	}
	if u := h.m.Status().RunnerUpdate; u.Installed != "2.338.0" || u.Deadline != nil || u.CheckedAt != nil {
		t.Fatalf("a stale check overwrote the install: %+v", u)
	}
	if strings.Contains(h.eventText(), "is available") {
		t.Fatalf("a stale check warned:\n%s", h.eventText())
	}
}

// A failed check after dist/current changed forgets what the last check said
// about the version installed before.
func TestFailedCheckForgetsFactsOfAnotherVersion(t *testing.T) {
	for _, c := range []struct {
		name, installed string
		change          func(t *testing.T, h *harness, dist string)
	}{
		{"switched to the latest", "2.338.0", func(t *testing.T, h *harness, _ string) { h.linkDist(t, "2.338.0") }},
		{"current removed", "", func(t *testing.T, h *harness, dist string) {
			if err := os.Remove(filepath.Join(dist, "current")); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			dist := h.linkDist(t, "2.337.0")
			h.gh.setReleases(release("2.338.0", 0, ""), release("2.337.0", -20, ""))
			h.m.Tick(context.Background())
			if u := h.m.Status().RunnerUpdate; u.Deadline == nil {
				t.Fatalf("no update found: %+v", u)
			}
			c.change(t, h, dist)
			h.gh.setErr("ListRunnerReleases ", &github.APIError{Status: 502, Kind: github.ErrServer, Message: "bad gateway"})
			if _, err := h.m.checkRelease(context.Background()); err == nil {
				t.Fatal("the check should fail")
			}
			u := h.m.Status().RunnerUpdate
			if u.Installed != c.installed || u.Deadline != nil || u.Latest != "" || u.CheckedAt != nil || u.CheckError != "github: 502 bad gateway" {
				t.Fatalf("status %+v", u)
			}
		})
	}
}
