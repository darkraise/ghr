package runner

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/github"
)

// visibility is what the manager last read for repo.
func (h *harness) visibility(repo string) (public, known bool) {
	h.m.mu.Lock()
	defer h.m.mu.Unlock()
	return h.m.visibilityLocked(repo)
}

func (h *harness) repoReads(repo string) int {
	return strings.Count(h.gh.callsText(), "GetRepo "+repo)
}

// A repository's visibility is read right after its poll succeeded, before
// the tick goes on to the next repository, and then not again until the
// answer is six hours old.
func TestVisibilityIsReadAfterThePollAndEverySixHours(t *testing.T) {
	h := newHarness(t)
	h.gh.setPublic("darkmem", true)
	if _, known := h.visibility("darkmem"); known {
		t.Fatal("an answer before any read")
	}
	h.m.Tick(context.Background())
	want := "ListRuns darkcloud,ListRuns darkcloud,ListRuns darkcloud,GetRepo darkcloud," +
		"ListRuns darkmem,ListRuns darkmem,ListRuns darkmem,GetRepo darkmem"
	if got := h.gh.callsText(); !strings.HasPrefix(got, want) {
		t.Fatalf("calls %s\nwant them to start with %s", got, want)
	}
	if public, known := h.visibility("darkcloud"); public || !known {
		t.Fatalf("darkcloud: public %v known %v", public, known)
	}
	// Repository names are case-insensitive.
	if public, known := h.visibility("DarkMem"); !public || !known {
		t.Fatalf("darkmem: public %v known %v", public, known)
	}
	h.gh.setPublic("darkmem", false)
	h.now = h.now.Add(visibilityTTL - time.Second)
	h.m.Tick(context.Background())
	if public, _ := h.visibility("darkmem"); !public || h.repoReads("darkmem") != 1 {
		t.Fatalf("read again inside six hours: public %v, %d reads", public, h.repoReads("darkmem"))
	}
	h.now = h.now.Add(time.Second)
	h.m.Tick(context.Background())
	if public, _ := h.visibility("darkmem"); public || h.repoReads("darkmem") != 2 {
		t.Fatalf("not read again after six hours: public %v, %d reads", public, h.repoReads("darkmem"))
	}
}

// A replaced or reloaded token is the owner saying "look again": every answer
// is read again at the next poll, and stands until that read.
func TestClearDegradedMakesEveryVisibilityDue(t *testing.T) {
	h := newHarness(t)
	h.gh.setPublic("darkmem", true)
	h.m.Tick(context.Background())
	h.gh.setPublic("darkmem", false)
	h.m.ClearDegraded()
	if public, known := h.visibility("darkmem"); !public || !known {
		t.Fatalf("the last answer must stand until the next read: public %v known %v", public, known)
	}
	h.now = h.now.Add(10 * time.Second)
	h.m.Tick(context.Background())
	if public, _ := h.visibility("darkmem"); public || h.repoReads("darkmem") != 2 || h.repoReads("darkcloud") != 2 {
		t.Fatalf("public %v, reads darkmem %d darkcloud %d", public, h.repoReads("darkmem"), h.repoReads("darkcloud"))
	}
	h.now = h.now.Add(10 * time.Second)
	h.m.Tick(context.Background())
	if h.repoReads("darkmem") != 2 {
		t.Fatalf("read on every tick after ClearDegraded: %d reads", h.repoReads("darkmem"))
	}
}

// A read that fails is handled as that repository's failed poll: its error
// shows, its wait doubles while it keeps failing, and what GitHub said before
// still stands.
func TestFailedVisibilityReadKeepsTheLastAnswer(t *testing.T) {
	h := newHarness(t)
	h.m.Tick(context.Background()) // darkmem is read: private
	h.gh.setPublic("darkmem", true)
	h.gh.setErr("GetRepo darkmem", &github.APIError{Status: 502, Kind: github.ErrServer, Message: "Bad Gateway"})
	h.m.ClearDegraded()
	retryIn := func() time.Duration {
		h.m.mu.Lock()
		defer h.m.mu.Unlock()
		return h.m.retryAt["darkmem"].Sub(h.now)
	}
	for _, wait := range []time.Duration{10 * time.Second, 20 * time.Second} {
		h.now = h.now.Add(wait)
		ok, unanswered := h.m.gatherDemand(context.Background(), h.cfg, h.now)
		if ok || unanswered != "darkmem" {
			t.Fatalf("answered %v, unanswered %q", ok, unanswered)
		}
		st := h.m.Status()
		if st.Degraded || st.Repos[0].Error != "" || st.Repos[1].Error != "github: 502 Bad Gateway" {
			t.Fatalf("degraded %v errors %q and %q", st.Degraded, st.Repos[0].Error, st.Repos[1].Error)
		}
		if got := retryIn(); got != wait {
			t.Fatalf("next poll in %s, want %s", got, wait)
		}
		if public, known := h.visibility("darkmem"); public || !known {
			t.Fatalf("the last answer did not stand: public %v known %v", public, known)
		}
	}
	h.gh.setErr("GetRepo darkmem", nil)
	h.now = h.now.Add(40 * time.Second)
	if ok, unanswered := h.m.gatherDemand(context.Background(), h.cfg, h.now); !ok {
		t.Fatalf("%s did not answer after the read works again", unanswered)
	}
	if public, _ := h.visibility("darkmem"); !public {
		t.Fatal("the new answer was not stored")
	}
}

// A first read that fails leaves the repository without an answer. Only a
// rejected token or a rate limit stops the other repositories being read.
func TestFailedFirstVisibilityReadLeavesNoAnswer(t *testing.T) {
	for name, tc := range map[string]struct {
		err                    github.APIError
		degraded, paused, next bool // next: darkmem, the repository after it, is still read
	}{
		"server error":   {github.APIError{Status: 502, Kind: github.ErrServer}, false, false, true},
		"not visible":    {github.APIError{Status: 404, Kind: github.ErrNotFound}, false, false, true},
		"permission":     {github.APIError{Status: 403, Kind: github.ErrAuth}, false, false, true},
		"token rejected": {github.APIError{Status: 401, Kind: github.ErrAuth}, true, false, false},
		"rate limit":     {github.APIError{Status: 403, Kind: github.ErrRateLimit}, false, true, false},
	} {
		h := newHarness(t)
		tc.err.RetryAt = h.now.Add(time.Hour)
		h.gh.setErr("GetRepo darkcloud", &tc.err)
		ok, unanswered := h.m.gatherDemand(context.Background(), h.cfg, h.now)
		if ok || unanswered != "darkcloud" {
			t.Fatalf("%s: answered %v, unanswered %q", name, ok, unanswered)
		}
		if _, known := h.visibility("darkcloud"); known {
			t.Fatalf("%s: an answer from a failed read", name)
		}
		if _, known := h.visibility("darkmem"); known != tc.next {
			t.Fatalf("%s: darkmem read %v, want %v", name, known, tc.next)
		}
		paused := h.m.PausedUntil().After(h.now)
		if st := h.m.Status(); st.Degraded != tc.degraded || paused != tc.paused || (st.Repos[0].Error != "") != tc.next {
			t.Fatalf("%s: degraded %v paused %v error %q", name, st.Degraded, paused, st.Repos[0].Error)
		}
	}
}

// Only a repository whose poll just succeeded is read.
func TestVisibilityIsNotReadWithoutASuccessfulPoll(t *testing.T) {
	h := newHarness(t)
	h.gh.setErr("ListRuns darkcloud", &github.APIError{Status: 502, Kind: github.ErrServer})
	pause(h, "darkmem")
	h.m.Tick(context.Background())
	if calls := h.gh.callsText(); strings.Contains(calls, "GetRepo") {
		t.Fatalf("read without a successful poll: %s", calls)
	}
}

// What is kept about a repository goes when it leaves the config, on every
// tick and without an API call, so one that is added again starts unread.
func TestRemovedRepositoryIsForgotten(t *testing.T) {
	h := newHarness(t)
	h.m.Tick(context.Background())
	if _, known := h.visibility("darkmem"); !known {
		t.Fatal("setup: darkmem was not read")
	}
	h.m.apiErr("darkcloud", &github.APIError{Status: 403, Kind: github.ErrRateLimit, RetryAt: h.now.Add(time.Hour)}, h.now)
	without := h.cfg.Clone()
	without.Repos = without.Repos[:1]
	h.cfg = without
	calls := h.gh.callCount()
	h.m.Tick(context.Background())
	if _, known := h.visibility("darkmem"); known {
		t.Fatal("a removed repository kept its answer")
	}
	if _, known := h.visibility("darkcloud"); !known || h.gh.callCount() != calls {
		t.Fatalf("darkcloud known %v, API calls during the pause %d", known, h.gh.callCount()-calls)
	}
}

// A reload that lands while a read is in flight must not be undone by that
// read's answer: the repository is read again at its next poll.
func TestClearDegradedDuringAReadLeavesItDue(t *testing.T) {
	h := newHarness(t)
	h.gh.hook = func(method string) {
		if method == "GetRepo" {
			h.m.ClearDegraded()
		}
	}
	h.m.Tick(context.Background())
	h.gh.hook = nil
	h.now = h.now.Add(10 * time.Second)
	h.m.Tick(context.Background())
	if h.repoReads("darkmem") != 2 || h.repoReads("darkcloud") != 2 {
		t.Fatalf("reads darkmem %d darkcloud %d, want 2 each", h.repoReads("darkmem"), h.repoReads("darkcloud"))
	}
}

// AddRepo reads a repository before it saves it and hands the answer over,
// as a read made now. Without it, a repository removed and added again before
// a tick would keep the answer from before its removal.
func TestRecordVisibilityReplacesAnOlderAnswer(t *testing.T) {
	h := newHarness(t)
	blockDarkmem(t, h)
	reads := h.repoReads("darkmem")
	h.m.RecordVisibility("DarkMem", false)
	if mem := h.cfg.Repos[1]; h.m.unread(mem) || h.m.blocked(h.cfg, mem) {
		t.Fatalf("after a private answer: unread %v blocked %v", h.m.unread(mem), h.m.blocked(h.cfg, mem))
	}
	queuedRun(h, "darkmem", 7, homelabJob(h, 71))
	h.now = h.now.Add(10 * time.Second)
	h.m.Tick(context.Background())
	h.m.Wait()
	// GitHub still says public to a read, so a read would have blocked it again.
	if spawnsFor(h, "darkmem") != 1 || h.repoReads("darkmem") != reads {
		t.Fatalf("%d spawns, %d reads since the answer\n%s", spawnsFor(h, "darkmem"), h.repoReads("darkmem")-reads, h.eventText())
	}
	h.m.RecordVisibility("darkmem", true)
	if !h.m.blocked(h.cfg, h.cfg.Repos[1]) {
		t.Fatal("not blocked after a public answer")
	}
}

// A read of the repository that began before an answer was handed over ends
// after it. The handed-over answer is the newer one and stays.
func TestOlderRepositoryReadDoesNotReplaceAHandedOverAnswer(t *testing.T) {
	h := newHarness(t)
	h.gh.hook = func(method string) {
		if method == "GetRepo" && strings.HasSuffix(h.gh.callsText(), "GetRepo darkmem") {
			h.m.RecordVisibility("darkmem", true)
		}
	}
	h.m.Tick(context.Background()) // the read in flight will answer private
	h.gh.hook = nil
	if public, known := h.visibility("darkmem"); !public || !known || h.repoReads("darkmem") != 1 {
		t.Fatalf("public %v known %v after %d reads; the read that began first replaced the answer", public, known, h.repoReads("darkmem"))
	}
}

// An answer is handed over just before its repository shows up in the
// config. A tick that still sees the config without the repository keeps the
// answer, the tick that first sees the repository plans with it without a
// read of its own, and removing the repository later drops it.
func TestHandedOverAnswerWaitsForItsRepository(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	with := h.cfg
	without := h.cfg.Clone()
	without.Repos = without.Repos[:1]
	tick := func(cfg *config.Config) {
		h.cfg = cfg
		h.now = h.now.Add(10 * time.Second)
		h.m.Tick(ctx)
		h.m.Wait()
	}
	tick(without)
	h.m.RecordVisibility("darkmem", false)
	tick(without)
	if _, known := h.visibility("darkmem"); !known {
		t.Fatal("a tick before the repository was in the config dropped the answer")
	}
	queuedRun(h, "darkmem", 7, homelabJob(h, 71))
	tick(with)
	if spawnsFor(h, "darkmem") != 1 || h.repoReads("darkmem") != 0 {
		t.Fatalf("%d spawns, %d reads; want a runner planned on the handed-over answer\n%s", spawnsFor(h, "darkmem"), h.repoReads("darkmem"), h.eventText())
	}
	tick(without)
	if _, known := h.visibility("darkmem"); known {
		t.Fatal("the answer outlived its repository")
	}
}
