package runner

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/sched"
)

// holdFetch makes Fetch block until release is called or its context ends,
// signalling entered when it starts; it then writes the tarball.
func holdFetch(t *testing.T, h *harness) (entered chan struct{}, release func()) {
	entered = make(chan struct{})
	released := make(chan struct{})
	var once sync.Once
	release = func() { once.Do(func() { close(released) }) }
	t.Cleanup(release)
	h.m.Fetch = func(ctx context.Context, _, dst string) error {
		close(entered)
		select {
		case <-released:
			return os.WriteFile(dst, tarball, 0o644)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return entered, release
}

// idleRunner spawns a darkmem runner and makes it idle and online.
func idleRunner(t *testing.T, h *harness) string {
	t.Helper()
	if err := h.m.spawn(context.Background(), h.cfg, "darkmem"); err != nil {
		t.Fatal(err)
	}
	id := "aaaaaa"
	h.gh.setRunner(h.m.insts[id].RunnerID, "online", false)
	h.m.setState(id, sched.Idle)
	return id
}

func jitCalls(h *harness) int {
	return strings.Count(h.gh.callsText(), "GenerateJITConfig")
}

func TestQueuedUpdateRunsWhenFree(t *testing.T) {
	h := newHarness(t)
	queuedUpdate(t, h, tarballSum())
	id := idleRunner(t, h)
	h.m.Tick(context.Background())
	h.m.Wait()
	if current(t, h) != "2.338.0" {
		t.Fatalf("current %s\n%s", current(t, h), h.eventText())
	}
	if len(h.sd.stopped) != 1 || h.sd.stopped[0] != UnitPrefix+id {
		t.Fatalf("stopped %v; the idle runner must stop for the update", h.sd.stopped)
	}
	txt := h.eventText()
	stopped, started := strings.Index(txt, "stopped idle runner "+id), strings.Index(txt, "info  runner update started")
	if stopped < 0 || started < stopped || !strings.Contains(txt, "ok  runner updated to 2.338.0") {
		t.Fatalf("events:\n%s", txt)
	}
	if u := h.m.Status().RunnerUpdate; u.Running || u.Queued || u.LastOutcome != "ok" {
		t.Fatalf("status %+v", u)
	}
	if err := h.m.StartPrune(); err != nil {
		t.Fatalf("the update kept the maintenance reservation: %v", err)
	}
	h.m.Wait()
}

func TestQueuedUpdateWaitsUntilFree(t *testing.T) {
	for _, c := range []struct {
		name  string
		block func(t *testing.T, h *harness)
	}{
		{"starting runner", func(t *testing.T, h *harness) {
			if err := h.m.spawn(context.Background(), h.cfg, "darkmem"); err != nil {
				t.Fatal(err)
			}
		}},
		{"busy runner", func(t *testing.T, h *harness) {
			id := idleRunner(t, h)
			h.m.setState(id, sched.Busy)
		}},
		{"queued job", func(t *testing.T, h *harness) {
			queuedRun(h, "darkmem", 7, github.Job{ID: 70, Status: "queued", Labels: []string{"homelab"}, CreatedAt: h.now})
			h.cfg.GlobalMax = 0 // keep the job queued: nothing spawns for it
		}},
		{"demand poll failed", func(t *testing.T, h *harness) {
			h.gh.setErr("ListRuns darkcloud", &github.APIError{Status: 502, Kind: github.ErrServer})
		}},
		{"rate limited", func(t *testing.T, h *harness) {
			h.gh.setErr("ListRuns darkcloud", &github.APIError{Status: 403, Kind: github.ErrRateLimit, RetryAt: h.now.Add(time.Minute)})
		}},
		{"degraded", func(t *testing.T, h *harness) {
			h.gh.setErr("ListRuns darkcloud", &github.APIError{Status: 401, Kind: github.ErrAuth})
		}},
		// The release check runs after the demand poll, so every repo answered
		// and only the API condition holds the update.
		{"rate limited after the poll", func(t *testing.T, h *harness) {
			h.m.mu.Lock()
			h.m.upd.nextCheck = time.Time{}
			h.m.mu.Unlock()
			h.gh.setErr("ListRunnerReleases ", &github.APIError{Status: 403, Kind: github.ErrRateLimit, RetryAt: h.now.Add(time.Minute)})
		}},
		{"degraded after the poll", func(t *testing.T, h *harness) {
			h.m.mu.Lock()
			h.m.upd.nextCheck = time.Time{}
			h.m.mu.Unlock()
			h.gh.setErr("ListRunnerReleases ", &github.APIError{Status: 401, Kind: github.ErrAuth})
		}},
		{"prune running", func(t *testing.T, h *harness) {
			h.m.mu.Lock()
			h.m.pruning = true
			h.m.mu.Unlock()
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			_, fetched := queuedUpdate(t, h, tarballSum())
			c.block(t, h)
			h.m.Tick(context.Background())
			h.m.Wait()
			if strings.Contains(h.eventText(), "runner update started") || len(*fetched) != 0 {
				t.Fatalf("the update started:\n%s", h.eventText())
			}
			if u := h.m.Status().RunnerUpdate; !u.Queued || u.Running {
				t.Fatalf("status %+v", u)
			}
		})
	}
}

func TestNoSpawnWhileUpdating(t *testing.T) {
	h := newHarness(t)
	queuedUpdate(t, h, tarballSum())
	entered, release := holdFetch(t, h)
	h.m.Tick(context.Background())
	waitOrFail(t, entered, "the update to download")
	if u := h.m.Status().RunnerUpdate; !u.Running {
		t.Fatalf("status %+v", u)
	}
	if err := h.m.StartPrune(); !errors.Is(err, ErrUpdateRunning) {
		t.Fatalf("prune during the update: %v", err)
	}
	if err := h.m.CancelUpdate(); !errors.Is(err, ErrUpdateRunning) {
		t.Fatalf("cancel during the update: %v", err)
	}
	queuedRun(h, "darkmem", 7, github.Job{ID: 70, Status: "queued", Labels: []string{"homelab"}, CreatedAt: h.now})
	h.m.Tick(context.Background())
	if n := jitCalls(h); n != 0 {
		t.Fatalf("%d spawns while updating", n)
	}
	release()
	h.m.Wait()
	h.m.Tick(context.Background())
	if n := jitCalls(h); n != 1 {
		t.Fatalf("%d spawns after the update", n)
	}
	if !strings.Contains(h.eventText(), "spawned aaaaaa (2.338.0)") {
		t.Fatalf("the runner did not start on the new version:\n%s", h.eventText())
	}
}

func TestShutdownInterruptsTheUpdate(t *testing.T) {
	h := newHarness(t)
	queuedUpdate(t, h, tarballSum())
	entered, _ := holdFetch(t, h)
	h.m.Tick(context.Background())
	waitOrFail(t, entered, "the update to download")
	h.m.Close()
	h.m.Wait()
	if !strings.Contains(h.eventText(), "runner update interrupted by shutdown") {
		t.Fatalf("events:\n%s", h.eventText())
	}
	if u := h.m.Status().RunnerUpdate; !u.Queued || u.Running || current(t, h) != "2.337.0" {
		t.Fatalf("status %+v current %s", u, current(t, h))
	}
}

// An idle runner that takes a job, or cannot be checked or stopped, after the
// tick found ghr free means it was not free: the update waits for a later
// tick, keeping its queue and releasing the reservation.
func TestUpdateWaitsWhenAnIdleRunnerDoesNotStop(t *testing.T) {
	for _, c := range []struct {
		name  string
		setup func(h *harness, id string)
	}{
		{"took a job", func(h *harness, id string) {
			rid := h.m.insts[id].RunnerID
			h.gh.hook = func(m string) {
				if m == "ListRuns" {
					h.gh.setRunner(rid, "online", true)
				}
			}
		}},
		{"check failed", func(h *harness, _ string) {
			h.gh.hook = func(m string) {
				if m == "ListRuns" {
					h.gh.setErr("GetRunner darkmem", &github.APIError{Status: 502, Kind: github.ErrServer})
				}
			}
		}},
		{"stop failed", func(h *harness, id string) { h.sd.stopErr[UnitPrefix+id] = errors.New("unit busy") }},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			_, fetched := queuedUpdate(t, h, tarballSum())
			id := idleRunner(t, h)
			c.setup(h, id)
			h.m.Tick(context.Background())
			h.m.Wait()
			if strings.Contains(h.eventText(), "runner update started") || len(*fetched) != 0 {
				t.Fatalf("the update started:\n%s", h.eventText())
			}
			if len(h.sd.stopped) != 0 {
				t.Fatalf("stopped %v", h.sd.stopped)
			}
			if u := h.m.Status().RunnerUpdate; !u.Queued || u.Running {
				t.Fatalf("status %+v", u)
			}
			if err := h.m.StartPrune(); err != nil {
				t.Fatalf("the reservation was kept: %v", err)
			}
			h.m.Wait()
		})
	}
}

// A config change during the demand poll defers the update to the next tick,
// whose poll covers the new config.
func TestConfigChangeDuringPollDefersTheUpdate(t *testing.T) {
	h := newHarness(t)
	_, fetched := queuedUpdate(t, h, tarballSum())
	h.gh.hook = func(m string) {
		if m != "ListRuns" {
			return
		}
		h.gh.mu.Lock()
		h.gh.hook = nil
		h.gh.mu.Unlock()
		c := *h.cfg
		h.cfg = &c
	}
	h.m.Tick(context.Background())
	h.m.Wait()
	if strings.Contains(h.eventText(), "runner update started") || len(*fetched) != 0 {
		t.Fatalf("the update started on a stale poll:\n%s", h.eventText())
	}
	h.m.Tick(context.Background())
	h.m.Wait()
	if current(t, h) != "2.338.0" {
		t.Fatalf("the next tick did not update:\n%s", h.eventText())
	}
}
