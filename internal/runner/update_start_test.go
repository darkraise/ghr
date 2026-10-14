package runner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/config"
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

// holdReleaseCheck makes the first release listing block until release is
// called, signalling entered when it starts. With a queued update that is
// the update's own check.
func holdReleaseCheck(t *testing.T, h *harness) (entered chan struct{}, release func()) {
	entered = make(chan struct{})
	released := make(chan struct{})
	var enter, once sync.Once
	release = func() { once.Do(func() { close(released) }) }
	t.Cleanup(release)
	h.gh.hook = func(method string) {
		if method == "ListRunnerReleases" {
			enter.Do(func() { close(entered) })
			<-released
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
	h.m.move(id, sched.Idle, sched.Starting)
	return id
}

func jitCalls(h *harness) int {
	return strings.Count(h.gh.callsText(), "GenerateJITConfig")
}

func TestQueuedUpdateRunsWhenFree(t *testing.T) {
	h := newHarness(t)
	preparedUpdate(t, h)
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
			h.m.move(id, sched.Busy, sched.Idle)
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
			if err := h.m.Jobs.RunNow(config.JobRunnerUpdateCheck); err != nil {
				t.Fatal(err)
			}
			h.gh.setErr("ListRunnerReleases ", &github.APIError{Status: 403, Kind: github.ErrRateLimit, RetryAt: h.now.Add(time.Minute)})
		}},
		{"degraded after the poll", func(t *testing.T, h *harness) {
			if err := h.m.Jobs.RunNow(config.JobRunnerUpdateCheck); err != nil {
				t.Fatal(err)
			}
			h.gh.setErr("ListRunnerReleases ", &github.APIError{Status: 401, Kind: github.ErrAuth})
		}},
		{"prune running", func(t *testing.T, h *harness) {
			h.m.mu.Lock()
			h.m.pruning = true
			h.m.mu.Unlock()
		}},
		{"maintenance hold", func(t *testing.T, h *harness) {
			h.m.mu.Lock()
			h.m.held = 1
			h.m.mu.Unlock()
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			fetched := preparedUpdate(t, h)
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
	preparedUpdate(t, h)
	entered, release := holdReleaseCheck(t, h)
	h.m.Tick(context.Background())
	waitOrFail(t, entered, "the update's release check")
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
	preparedUpdate(t, h)
	entered, release := holdReleaseCheck(t, h)
	h.m.Tick(context.Background())
	waitOrFail(t, entered, "the update's release check")
	h.m.Close()
	release()
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
			fetched := preparedUpdate(t, h)
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

// An idle runner that cannot be checked holds the update off for a while
// instead of every tick stopping the other idle runners again.
func TestUpdateBacksOffWhenAnIdleRunnerCannotStop(t *testing.T) {
	h := newHarness(t)
	preparedUpdate(t, h)
	idleRunner(t, h)
	if err := h.m.spawn(context.Background(), h.cfg, "darkcloud"); err != nil {
		t.Fatal(err)
	}
	h.gh.setRunner(h.m.insts["bbbbbb"].RunnerID, "online", false)
	h.m.move("bbbbbb", sched.Idle, sched.Starting)
	h.gh.setErr("GetRunner darkcloud", &github.APIError{Status: 502, Kind: github.ErrServer})
	postponed := func() int { return strings.Count(h.eventText(), "runner update postponed") }
	h.m.Tick(context.Background())
	h.m.Wait()
	if postponed() != 1 || strings.Contains(h.eventText(), "runner update started") {
		t.Fatalf("events:\n%s", h.eventText())
	}
	h.now = h.now.Add(time.Minute)
	h.m.Tick(context.Background())
	h.m.Wait()
	if postponed() != 1 || strings.Contains(h.eventText(), "runner update started") {
		t.Fatalf("retried within the backoff:\n%s", h.eventText())
	}
	h.gh.setErr("GetRunner darkcloud", nil)
	h.now = h.now.Add(15 * time.Minute)
	h.m.Tick(context.Background())
	h.m.Wait()
	if current(t, h) != "2.338.0" {
		t.Fatalf("no update after the backoff:\n%s", h.eventText())
	}
}

// A config change during the demand poll defers the update to the next tick,
// whose poll covers the new config.
func TestConfigChangeDuringPollDefersTheUpdate(t *testing.T) {
	h := newHarness(t)
	fetched := preparedUpdate(t, h)
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

// A shutdown that arrives while the idle runners stop leaves the update
// queued and not started: the stops run under the tick's context too.
func TestShutdownDuringIdleStopsKeepsTheQueue(t *testing.T) {
	h := newHarness(t)
	fetched := preparedUpdate(t, h)
	idleRunner(t, h)
	tickCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.gh.hook = func(m string) {
		if strings.HasPrefix(m, "GetRunner") {
			cancel()
		}
	}
	h.m.Tick(tickCtx)
	h.m.Wait()
	if strings.Contains(h.eventText(), "runner update started") {
		t.Fatalf("the update started after the shutdown:\n%s", h.eventText())
	}
	h.m.mu.Lock()
	queued, running, pruning := h.m.upd.file.QueuedAt != nil, h.m.upd.running, h.m.pruning
	h.m.mu.Unlock()
	if !queued || running || pruning {
		t.Fatalf("queued %v running %v pruning %v", queued, running, pruning)
	}
	if len(*fetched) != 0 || current(t, h) != "2.337.0" {
		t.Fatalf("an install ran: fetched %v current %s", *fetched, current(t, h))
	}
}

// The tick's idle stop and the update start of the same tick both stop the
// same idle runner. The second stop finds the unit gone and must not postpone
// the update.
func TestUpdateStartsInTheTickThatStoppedItsIdleRunner(t *testing.T) {
	h := newHarness(t)
	preparedUpdate(t, h)
	id := idleRunner(t, h)
	h.now = h.now.Add(6 * time.Minute) // past idle_timeout
	h.m.Tick(context.Background())
	h.m.Wait()
	if len(h.sd.stopped) != 2 || h.sd.stopped[0] != UnitPrefix+id || h.sd.stopped[1] != UnitPrefix+id {
		t.Fatalf("stopped %v, want the idle stop and then the update's own stop", h.sd.stopped)
	}
	if strings.Contains(h.eventText(), "runner update postponed") || current(t, h) != "2.338.0" {
		t.Fatalf("current %s\n%s", current(t, h), h.eventText())
	}
}

// A queued update that cannot start for a reason other than runners working
// says why, once per reason.
func TestQueuedUpdateSaysWhyItWaits(t *testing.T) {
	h := newHarness(t)
	preparedUpdate(t, h)
	waiting := func() []string {
		var out []string
		for _, line := range strings.Split(h.eventText(), "\n") {
			if strings.Contains(line, "runner update waiting") {
				out = append(out, line)
			}
		}
		return out
	}
	h.gh.setErr("ListRuns darkcloud", &github.APIError{Status: 502, Kind: github.ErrServer})
	h.m.Tick(context.Background())
	h.now = h.now.Add(5 * time.Second) // darkcloud is still in its 10 s backoff
	h.m.Tick(context.Background())
	want := []string{"warn  runner update waiting: darkcloud did not answer its poll"}
	if got := waiting(); !reflect.DeepEqual(got, want) {
		t.Fatalf("events %q", got)
	}
	// A queue made anew is a new wait: its reason is said again.
	if err := h.m.CancelUpdate(); err != nil {
		t.Fatal(err)
	}
	if err := h.m.QueueUpdate(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.m.Tick(context.Background())
	want = append(want, want[0])
	if got := waiting(); !reflect.DeepEqual(got, want) {
		t.Fatalf("events after a requeue %q", got)
	}
	h.gh.setErr("ListRuns darkcloud", &github.APIError{Status: 403, Kind: github.ErrRateLimit, RetryAt: h.now.Add(time.Hour)})
	h.now = h.now.Add(10 * time.Second)
	h.m.Tick(context.Background())
	want = append(want, "warn  runner update waiting: GitHub API calls are paused")
	if got := waiting(); !reflect.DeepEqual(got, want) {
		t.Fatalf("events %q", got)
	}
	h.gh.setErr("ListRuns darkcloud", nil)
	h.now = h.now.Add(time.Hour)
	h.m.Tick(context.Background())
	h.m.Wait()
	if got := waiting(); !reflect.DeepEqual(got, want) || current(t, h) != "2.338.0" {
		t.Fatalf("current %s events %q", current(t, h), got)
	}
}

// A queued update downloads and prepares its version in the background. It
// starts only once that version is ready, and then only switches to it.
func TestQueuedUpdateStartsOnlyOncePrepared(t *testing.T) {
	h := newHarness(t)
	dist, fetched := queuedUpdate(t, h, tarballSum())
	id := idleRunner(t, h)
	h.m.Tick(context.Background())
	h.m.Wait()
	if strings.Contains(h.eventText(), "runner update started") || len(h.sd.stopped) != 0 || current(t, h) != "2.337.0" {
		t.Fatalf("started before the version was ready: stopped %v\n%s", h.sd.stopped, h.eventText())
	}
	if len(*fetched) != 1 || !complete(filepath.Join(dist, "2.338.0")) || h.host.hostCalls() != "Extract 2.338.0.tmp,RunScript 2.338.0.tmp" {
		t.Fatalf("not prepared: fetched %v host %q\n%s", *fetched, h.host.hostCalls(), h.eventText())
	}
	if u := h.m.Status().RunnerUpdate; !u.Queued || u.Running {
		t.Fatalf("status %+v", u)
	}
	h.m.Tick(context.Background())
	h.m.Wait()
	if current(t, h) != "2.338.0" || len(h.sd.stopped) != 1 || h.sd.stopped[0] != UnitPrefix+id {
		t.Fatalf("current %s stopped %v\n%s", current(t, h), h.sd.stopped, h.eventText())
	}
	if len(*fetched) != 1 || h.host.hostCalls() != "Extract 2.338.0.tmp,RunScript 2.338.0.tmp" {
		t.Fatalf("the update downloaded again: fetched %v host %q", *fetched, h.host.hostCalls())
	}
}

// Runners keep spawning while the new version downloads.
func TestSpawnsWhileTheUpdatePrepares(t *testing.T) {
	h := newHarness(t)
	queuedUpdate(t, h, tarballSum())
	entered, release := holdFetch(t, h)
	h.m.Tick(context.Background())
	waitOrFail(t, entered, "the version to download")
	if u := h.m.Status().RunnerUpdate; !u.Preparing || u.Running {
		t.Fatalf("status while preparing %+v", u)
	}
	queuedRun(h, "darkmem", 7, github.Job{ID: 70, Status: "queued", Labels: []string{"homelab"}, CreatedAt: h.now})
	h.m.Tick(context.Background())
	if n := jitCalls(h); n != 1 {
		t.Fatalf("%d spawns while the update prepared", n)
	}
	if err := h.m.StartPrune(); err != nil {
		t.Fatalf("the prepare held the maintenance reservation: %v", err)
	}
	release()
	h.m.Wait()
	if u := h.m.Status().RunnerUpdate; u.Preparing {
		t.Fatalf("status after preparing %+v", u)
	}
}

// A failed prepare keeps the queue and shows in the status as a failed
// update. It is retried an hour later, then at doubling intervals; a
// prepare that succeeds clears the failure.
func TestFailedPrepareKeepsTheQueue(t *testing.T) {
	h := newHarness(t)
	dist, _ := queuedUpdate(t, h, tarballSum())
	attempts := 0
	fail := true
	h.m.Fetch = func(_ context.Context, _, dst string) error {
		attempts++
		if fail {
			return errors.New("connection reset")
		}
		return os.WriteFile(dst, tarball, 0o644)
	}
	h.m.Tick(context.Background())
	h.m.Wait()
	if u := h.m.Status().RunnerUpdate; !u.Queued || u.Running || u.LastOutcome != "failed" || u.LastError != "download: connection reset" ||
		u.LastFinished == nil || !u.LastFinished.Equal(h.now) {
		t.Fatalf("status %+v", u)
	}
	if !strings.Contains(h.eventText(), "warn  runner update: preparing 2.338.0 failed: download: connection reset; retrying in 1h0m0s") {
		t.Fatalf("events:\n%s", h.eventText())
	}
	if _, err := os.Stat(filepath.Join(dist, "2.338.0.tmp")); !os.IsNotExist(err) {
		t.Fatalf("staging kept: %v", err)
	}
	h.now = h.now.Add(time.Minute)
	h.m.Tick(context.Background())
	h.m.Wait()
	if attempts != 1 || strings.Contains(h.eventText(), "runner update started") {
		t.Fatalf("%d attempts within the hour\n%s", attempts, h.eventText())
	}
	h.now = h.now.Add(releaseCheckRetry)
	h.m.Tick(context.Background())
	h.m.Wait()
	if attempts != 2 || !strings.Contains(h.eventText(), "retrying in 2h0m0s") {
		t.Fatalf("%d attempts after the hour\n%s", attempts, h.eventText())
	}
	h.now = h.now.Add(releaseCheckRetry)
	h.m.Tick(context.Background())
	h.m.Wait()
	if attempts != 2 {
		t.Fatalf("%d attempts one hour into a two-hour wait", attempts)
	}
	fail = false
	h.now = h.now.Add(releaseCheckRetry)
	h.m.Tick(context.Background())
	h.m.Wait()
	if u := h.m.Status().RunnerUpdate; attempts != 3 || !u.Queued || u.LastOutcome != "" || u.LastError != "" || u.LastFinished != nil {
		t.Fatalf("%d attempts, status %+v", attempts, u)
	}
}

// Cancelling the update forgets its failed prepare, and queueing it again
// prepares at once.
func TestCancelForgetsAFailedPrepare(t *testing.T) {
	h := newHarness(t)
	queuedUpdate(t, h, tarballSum())
	attempts := 0
	h.m.Fetch = func(context.Context, string, string) error {
		attempts++
		return errors.New("connection reset")
	}
	h.m.Tick(context.Background())
	h.m.Wait()
	if err := h.m.CancelUpdate(); err != nil {
		t.Fatal(err)
	}
	if u := h.m.Status().RunnerUpdate; u.Queued || u.LastOutcome != "" || u.LastError != "" {
		t.Fatalf("status after the cancel %+v", u)
	}
	if err := h.m.QueueUpdate(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.m.Tick(context.Background())
	h.m.Wait()
	if attempts != 2 {
		t.Fatalf("%d attempts; queueing again must not wait out the retry", attempts)
	}
}

// A shutdown or a cancel during the prepare stops it; only a cancel drops
// the queue, and neither is a failure.
func TestPrepareEndsWithShutdownOrCancel(t *testing.T) {
	for _, c := range []struct {
		name   string
		end    func(t *testing.T, h *harness)
		queued bool
	}{
		{"shutdown", func(_ *testing.T, h *harness) { h.m.Close() }, true},
		{"cancel", func(t *testing.T, h *harness) {
			if err := h.m.CancelUpdate(); err != nil {
				t.Fatal(err)
			}
		}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			dist, _ := queuedUpdate(t, h, tarballSum())
			entered, _ := holdFetch(t, h)
			h.m.Tick(context.Background())
			waitOrFail(t, entered, "the version to download")
			c.end(t, h)
			h.m.Wait()
			if u := h.m.Status().RunnerUpdate; u.Queued != c.queued || u.Running || current(t, h) != "2.337.0" {
				t.Fatalf("status %+v current %s", u, current(t, h))
			}
			if strings.Contains(h.eventText(), "failed") {
				t.Fatalf("events:\n%s", h.eventText())
			}
			if got := distEntries(t, dist); got != "2.330.0,2.337.0,current" {
				t.Fatalf("dist holds %s", got)
			}
		})
	}
}

// A cancel during the dependency script lets the script finish, since
// killing apt midway can leave dpkg half-configured; the prepare ends right
// after it.
func TestCancelLetsTheDependencyScriptFinish(t *testing.T) {
	h := newHarness(t)
	dist, _ := queuedUpdate(t, h, tarballSum())
	var scriptErr error
	h.host.onScript = func(ctx context.Context) {
		if err := h.m.CancelUpdate(); err != nil {
			t.Error(err)
		}
		scriptErr = ctx.Err()
	}
	h.m.Tick(context.Background())
	h.m.Wait()
	if scriptErr != nil {
		t.Fatalf("the cancel ended the dependency script: %v", scriptErr)
	}
	if u := h.m.Status().RunnerUpdate; u.Queued || u.LastOutcome != "" {
		t.Fatalf("status %+v", u)
	}
	if got := distEntries(t, dist); got != "2.330.0,2.337.0,current" || strings.Contains(h.eventText(), "is ready") || strings.Contains(h.eventText(), "failed") {
		t.Fatalf("dist holds %s\n%s", got, h.eventText())
	}
}

// After a restart nothing is known about the releases until a check
// succeeds. A failing first check keeps the update queued rather than
// starting it with its version not prepared.
func TestUpdateWaitsForAReleaseCheckAfterARestart(t *testing.T) {
	h := newHarness(t)
	_, fetched := queuedUpdate(t, h, tarballSum())
	h.restart(t)
	h.gh.setErr("ListRunnerReleases ", &github.APIError{Status: 502, Kind: github.ErrServer})
	h.m.Tick(context.Background())
	h.m.Wait()
	if u := h.m.Status().RunnerUpdate; !u.Queued || u.LastOutcome != "" || len(*fetched) != 0 || strings.Contains(h.eventText(), "runner update started") {
		t.Fatalf("status %+v fetched %v\n%s", u, *fetched, h.eventText())
	}
	if !strings.Contains(h.eventText(), "warn  runner update waiting: no runner release check has succeeded") {
		t.Fatalf("events:\n%s", h.eventText())
	}
	h.gh.setErr("ListRunnerReleases ", nil)
	h.now = h.now.Add(releaseCheckRetry)
	h.m.Tick(context.Background())
	h.m.Wait()
	h.m.Tick(context.Background())
	h.m.Wait()
	if current(t, h) != "2.338.0" || len(*fetched) != 1 {
		t.Fatalf("current %s fetched %v\n%s", current(t, h), *fetched, h.eventText())
	}
}

// A release whose notes carry no checksum has nothing to prepare: the update
// starts and fails at once, ending the queue, without downloading.
func TestUpdateWithoutAChecksumFailsAtOnce(t *testing.T) {
	h := newHarness(t)
	_, fetched := queuedUpdate(t, h, "")
	h.m.Tick(context.Background())
	h.m.Wait()
	u := h.m.Status().RunnerUpdate
	if u.Queued || u.LastOutcome != "failed" || !strings.HasPrefix(u.LastError, "checksum: no linux-x64 checksum") || len(*fetched) != 0 {
		t.Fatalf("status %+v fetched %v\n%s", u, *fetched, h.eventText())
	}
}
