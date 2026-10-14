package runner

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
)

func queuedRun(h *harness, repo string, runID int64, jobs ...github.Job) {
	h.gh.runs[repo+"/queued"] = append(h.gh.runs[repo+"/queued"], github.Run{ID: runID, Status: "queued"})
	h.gh.jobs[runID] = jobs
}

func TestGatherDemandFiltersLabelsAndConfirms(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkmem")
	queuedRun(h, "darkmem", 1,
		github.Job{ID: 11, Status: "queued", Labels: []string{"self-hosted", "Linux", "homelab"}, CreatedAt: h.now},
		github.Job{ID: 12, Status: "queued", Labels: []string{"ubuntu-latest"}, CreatedAt: h.now},
		github.Job{ID: 13, Status: "in_progress", RunID: 1, Name: "build", WorkflowName: "CI", RunnerName: "ghr-darkmem-aaaaaa", HTMLURL: "u"},
	)
	h.gh.runs["darkmem/in_progress"] = []github.Run{{ID: 1}} // same run listed twice must be read once
	h.m.gatherDemand(context.Background(), h.cfg, h.now)
	st := h.m.Status()
	var mem int
	for _, r := range st.Repos {
		if r.Name == "darkmem" {
			mem = r.Queued
		}
	}
	if mem != 1 {
		t.Fatalf("darkmem queued = %d", mem)
	}
	if h.state("aaaaaa") != "busy" || st.Instances[0].Job.Name != "build" || st.Instances[0].Job.Workflow != "CI" {
		t.Fatalf("state %s job %+v", h.state("aaaaaa"), st.Instances[0].Job)
	}
}

// A run waiting on an environment protection rule can hold an independent
// queued job; listing status=waiting runs finds it and a runner is spawned.
func TestQueuedJobInWaitingRunIsServed(t *testing.T) {
	h := newHarness(t)
	h.gh.runs["darkmem/waiting"] = []github.Run{{ID: 9, Status: "waiting"}}
	h.gh.jobs[9] = []github.Job{
		{ID: 91, Status: "waiting", Labels: []string{"homelab"}, CreatedAt: h.now},
		{ID: 92, Status: "queued", Labels: []string{"homelab"}, CreatedAt: h.now},
	}
	h.m.Tick(context.Background())
	if len(h.gh.jitCalls) != 1 || !strings.HasPrefix(h.gh.jitCalls[0], "ghr-darkmem-") {
		t.Fatalf("jit calls %v", h.gh.jitCalls)
	}
}

func TestTickSpawnsWithinCaps(t *testing.T) {
	h := newHarness(t)
	labels := []string{"self-hosted", "homelab"}
	queuedRun(h, "darkcloud", 1, github.Job{ID: 1, Status: "queued", Labels: labels, CreatedAt: h.now.Add(-3 * time.Minute)},
		github.Job{ID: 2, Status: "queued", Labels: labels, CreatedAt: h.now.Add(-2 * time.Minute)})
	queuedRun(h, "darkmem", 2, github.Job{ID: 3, Status: "queued", Labels: labels, CreatedAt: h.now.Add(-time.Minute)})
	h.m.Tick(context.Background())
	if len(h.sd.started) != 2 {
		t.Fatalf("started %d units", len(h.sd.started))
	}
	if !strings.Contains(h.gh.jitCalls[0], "darkcloud") || !strings.Contains(h.gh.jitCalls[1], "darkmem") {
		t.Fatalf("jit calls %v", h.gh.jitCalls)
	}
	h.now = h.now.Add(10 * time.Second)
	h.m.Tick(context.Background())
	if len(h.sd.started) != 2 {
		t.Fatalf("second tick over-spawned: %d", len(h.sd.started))
	}
}

func TestAuthErrorDegradesAndRecovers(t *testing.T) {
	h := newHarness(t)
	h.gh.setErr("ListRuns darkcloud", &github.APIError{Status: 401, Kind: github.ErrAuth, Message: "Bad credentials"})
	queuedRun(h, "darkmem", 2, github.Job{ID: 3, Status: "queued", Labels: []string{"homelab"}, CreatedAt: h.now})
	h.m.Tick(context.Background())
	st := h.m.Status()
	if !st.Degraded || len(h.sd.started) != 0 {
		t.Fatalf("degraded %v started %d", st.Degraded, len(h.sd.started))
	}
	h.now = h.now.Add(30 * time.Second)
	if h.m.apiAllowed(h.now) {
		t.Fatal("API should wait for the 60s recheck")
	}
	h.gh.setErr("ListRuns darkcloud", nil)
	h.now = h.now.Add(31 * time.Second)
	h.m.Tick(context.Background())
	if h.m.Status().Degraded || len(h.sd.started) != 1 {
		t.Fatalf("not recovered: degraded %v started %d", h.m.Status().Degraded, len(h.sd.started))
	}
}

func TestAuthErrorNamesCallAndPermission(t *testing.T) {
	h := newHarness(t)
	h.gh.setErr("ListRuns darkcloud", &github.APIError{Status: 401, Kind: github.ErrAuth,
		Message: "Bad credentials",
		Method:  "GET", Path: "/repos/darkraise/darkcloud/actions/runs", Permissions: "actions=read"})
	h.m.Tick(context.Background())
	want := "GitHub rejected the token on GET /repos/darkraise/darkcloud/actions/runs (needs actions=read): " +
		"github: 401 Bad credentials"
	if got := h.m.Status().DegradedReason; got != want {
		t.Fatalf("reason %q\nwant   %q", got, want)
	}
	if !strings.Contains(h.eventText(), "error darkcloud "+want+"; spawning stopped") {
		t.Fatalf("events %s", h.eventText())
	}
}

// The first repo answers, the second fails authentication: still degraded, no spawn.
func TestAuthFailureOnSecondRepoKeepsDegraded(t *testing.T) {
	h := newHarness(t)
	h.gh.setErr("ListRuns darkcloud", &github.APIError{Status: 401, Kind: github.ErrAuth})
	h.m.Tick(context.Background())
	h.gh.setErr("ListRuns darkcloud", nil)
	h.gh.setErr("ListRuns darkmem", &github.APIError{Status: 401, Kind: github.ErrAuth})
	queuedRun(h, "darkcloud", 1, github.Job{ID: 1, Status: "queued", Labels: []string{"homelab"}, CreatedAt: h.now})
	h.now = h.now.Add(authRecheck)
	h.m.Tick(context.Background())
	if !h.m.Status().Degraded || len(h.sd.started) != 0 {
		t.Fatalf("degraded %v started %d", h.m.Status().Degraded, len(h.sd.started))
	}
}

func TestRateLimitPausesWithoutDegrading(t *testing.T) {
	h := newHarness(t)
	retry := h.now.Add(5 * time.Minute)
	h.gh.setErr("ListRuns darkcloud", &github.APIError{Status: 403, Kind: github.ErrRateLimit, RetryAt: retry})
	h.m.Tick(context.Background())
	if h.m.Status().Degraded || h.m.apiAllowed(h.now.Add(time.Minute)) || !h.m.apiAllowed(retry) {
		t.Fatal("rate limit should pause until reset without degrading")
	}
}

// A rate limit found while polling demand stops every later API phase of the tick.
func TestRateLimitStopsTheRestOfTheTick(t *testing.T) {
	h := newHarness(t)
	h.cfg.Mode = config.ModeAll // warm spawns would otherwise follow
	h.gh.setErr("ListRuns darkcloud", &github.APIError{Status: 403, Kind: github.ErrRateLimit, RetryAt: h.now.Add(time.Minute)})
	h.m.Tick(context.Background())
	if n := h.gh.callCount(); n != 1 {
		t.Fatalf("API calls after the rate limit: %v", h.gh.calls)
	}
	h.now = h.now.Add(30 * time.Second)
	h.m.Tick(context.Background())
	if n := h.gh.callCount(); n != 1 {
		t.Fatalf("API calls before RetryAt: %v", h.gh.calls)
	}
}

func TestNotFoundIsPerRepoWithBackoff(t *testing.T) {
	h := newHarness(t)
	h.gh.setErr("ListRuns darkcloud", &github.APIError{Status: 404, Kind: github.ErrNotFound})
	queuedRun(h, "darkmem", 2, github.Job{ID: 3, Status: "queued", Labels: []string{"homelab"}, CreatedAt: h.now})
	h.m.Tick(context.Background())
	st := h.m.Status()
	if !strings.Contains(st.Repos[0].Error, "token lacks access") || len(h.sd.started) != 1 {
		t.Fatalf("repo error %q started %d", st.Repos[0].Error, len(h.sd.started))
	}
	h.m.mu.Lock()
	retry := h.m.retryAt["darkcloud"]
	h.m.mu.Unlock()
	if retry != h.now.Add(10*time.Second) {
		t.Fatalf("retryAt %v", retry)
	}
	h.m.apiErr("darkcloud", &github.APIError{Status: 502, Kind: github.ErrServer}, h.now)
	h.m.mu.Lock()
	retry = h.m.retryAt["darkcloud"]
	h.m.mu.Unlock()
	if retry != h.now.Add(20*time.Second) {
		t.Fatalf("second backoff %v", retry)
	}
}

func TestSpawnsPerTickAreCapped(t *testing.T) {
	h := newHarness(t)
	h.cfg.Mode = config.ModeAll
	warm := 30
	h.cfg.Repos[1].Warm = &warm
	h.ids = nil
	for i := 0; i < 40; i++ {
		h.ids = append(h.ids, fmt.Sprintf("%06x", i+1))
	}
	for tick, want := range []int{maxSpawnsPerTick, 2 * maxSpawnsPerTick, 3 * maxSpawnsPerTick, 3*maxSpawnsPerTick + 1} {
		h.m.Tick(context.Background())
		if got := len(h.sd.started); got != want {
			t.Fatalf("tick %d: started %d units, want %d", tick+1, got, want)
		}
		h.now = h.now.Add(10 * time.Second)
	}
}

func TestCappedSpawnsAreSharedAcrossRepos(t *testing.T) {
	h := newHarness(t)
	h.cfg.Mode = config.ModeAll
	warm := 15
	uncapped := 0
	h.cfg.Repos[0].Max = &uncapped
	h.cfg.Repos[0].Warm = &warm
	h.cfg.Repos[1].Warm = &warm
	h.ids = nil
	for i := 0; i < 40; i++ {
		h.ids = append(h.ids, fmt.Sprintf("%06x", i+1))
	}
	h.m.Tick(context.Background())
	var darkcloud, darkmem int
	for _, c := range h.gh.jitCalls {
		switch {
		case strings.HasPrefix(c, "ghr-darkcloud-"):
			darkcloud++
		case strings.HasPrefix(c, "ghr-darkmem-"):
			darkmem++
		}
	}
	if darkcloud != maxSpawnsPerTick/2 || darkmem != maxSpawnsPerTick/2 {
		t.Fatalf("first tick spawned %d darkcloud and %d darkmem runners: %v", darkcloud, darkmem, h.gh.jitCalls)
	}
}

// In all mode a repo in an error state gets no warm runner until a poll succeeds.
func TestAllModeSkipsWarmSpawnsForFailingRepo(t *testing.T) {
	for _, err := range []error{
		&github.APIError{Status: 404, Kind: github.ErrNotFound},
		&github.APIError{Status: 502, Kind: github.ErrServer},
		context.DeadlineExceeded,
	} {
		h := newHarness(t)
		h.cfg.Mode = config.ModeAll
		h.gh.setErr("ListRuns darkcloud", err)
		for tick := 0; tick < 3; tick++ {
			h.m.Tick(context.Background())
			h.now = h.now.Add(10 * time.Second)
		}
		for _, c := range h.gh.jitCalls {
			if strings.Contains(c, "darkcloud") {
				t.Fatalf("%v: warm spawn into a failing repo: %v", err, h.gh.jitCalls)
			}
		}
		if len(h.gh.jitCalls) != 1 {
			t.Fatalf("%v: darkmem's warm runner missing: %v", err, h.gh.jitCalls)
		}
		h.gh.setErr("ListRuns darkcloud", nil)
		h.now = h.now.Add(maxBackoff)
		h.m.Tick(context.Background())
		// darkmem's runner never came online, so the recovery tick replaces it
		// next to darkcloud's first warm runner.
		want := []string{
			"ghr-darkmem-aaaaaa self-hosted,linux,x64,homelab",
			"ghr-darkcloud-bbbbbb self-hosted,linux,x64,homelab",
			"ghr-darkmem-cccccc self-hosted,linux,x64,homelab",
		}
		if !slices.Equal(h.gh.jitCalls, want) {
			t.Fatalf("%v: spawns %v, want %v", err, h.gh.jitCalls, want)
		}
	}
}

// A pause applied while the tick was polling must not be overridden by its spawns.
func TestConfigChangeDuringTickStopsSpawning(t *testing.T) {
	h := newHarness(t)
	queuedRun(h, "darkmem", 2, github.Job{ID: 3, Status: "queued", Labels: []string{"homelab"}, CreatedAt: h.now})
	paused := h.cfg.Clone()
	paused.Repo("darkmem").Paused = true
	polls := 0
	h.m.Config = func() *config.Config {
		polls++
		if polls == 1 {
			return h.cfg // the snapshot Tick plans with
		}
		return paused // the operator paused darkmem meanwhile
	}
	h.m.Tick(context.Background())
	if len(h.gh.jitCalls) != 0 {
		t.Fatalf("spawned with a stale config: %v", h.gh.jitCalls)
	}
}

// After a repo's labels change, its idle runner (registered with the old labels)
// no longer covers jobs: it is stopped and a runner with the new labels spawns.
func TestLabelChangeReplacesStaleIdleRunner(t *testing.T) {
	h := newHarness(t)
	h.cfg.Repos[1].Max = new(int)
	*h.cfg.Repos[1].Max = 2
	h.m.spawn(context.Background(), h.cfg, "darkmem")
	h.gh.setRunner(101, "online", false)
	h.m.refreshRunners(context.Background())
	changed := h.cfg.Clone()
	changed.Repo("darkmem").Labels = []string{"gpu"}
	h.cfg = changed
	queuedRun(h, "darkmem", 2, github.Job{ID: 3, Status: "queued", Labels: []string{"homelab", "gpu"}, CreatedAt: h.now})
	h.m.Tick(context.Background())
	if len(h.sd.stopped) != 1 || h.sd.stopped[0] != "ghr-runner-aaaaaa" {
		t.Fatalf("stale idle runner not stopped: %v", h.sd.stopped)
	}
	if len(h.gh.jitCalls) != 2 || h.gh.jitCalls[1] != "ghr-darkmem-bbbbbb self-hosted,linux,x64,homelab,gpu" {
		t.Fatalf("jit calls %v", h.gh.jitCalls)
	}
}

func TestPausedRepoGetsNoRunners(t *testing.T) {
	h := newHarness(t)
	h.cfg.Repos[1].Paused = true
	queuedRun(h, "darkmem", 2, github.Job{ID: 3, Status: "queued", Labels: []string{"homelab"}, CreatedAt: h.now})
	h.m.Tick(context.Background())
	if len(h.sd.started) != 0 {
		t.Fatalf("started %d", len(h.sd.started))
	}
}

// A failed spawn is retried on the next tick, not again for the same repo
// within this one.
func TestSpawnFailureStopsRepoForTick(t *testing.T) {
	h := newHarness(t)
	h.cfg.Repos[1].Max = new(int)
	*h.cfg.Repos[1].Max = 2
	labels := []string{"homelab"}
	queuedRun(h, "darkmem", 2, github.Job{ID: 3, Status: "queued", Labels: labels, CreatedAt: h.now.Add(-2 * time.Minute)},
		github.Job{ID: 4, Status: "queued", Labels: labels, CreatedAt: h.now.Add(-time.Minute)})
	h.gh.setErr("GenerateJITConfig darkmem", &github.APIError{Status: 502, Kind: github.ErrServer})
	jitAttempts := func() int {
		n := 0
		for _, c := range h.gh.calls {
			if c == "GenerateJITConfig darkmem" {
				n++
			}
		}
		return n
	}
	h.m.Tick(context.Background())
	if n := jitAttempts(); n != 1 {
		t.Fatalf("JIT attempts in one tick = %d: %v", n, h.gh.calls)
	}
	if n := strings.Count(h.eventText(), "error darkmem spawn failed"); n != 1 {
		t.Fatalf("spawn failed events = %d:\n%s", n, h.eventText())
	}
	h.now = h.now.Add(10 * time.Second)
	h.m.Tick(context.Background())
	if n := jitAttempts(); n != 2 {
		t.Fatalf("JIT attempts after the second tick = %d: %v", n, h.gh.calls)
	}
}

// An in-progress API job without started_at keeps the start time already known.
func TestConfirmKeepsKnownStartTime(t *testing.T) {
	h := newHarness(t)
	if err := h.m.spawn(context.Background(), h.cfg, "darkmem"); err != nil {
		t.Fatal(err)
	}
	past := h.now.Add(-7 * time.Minute)
	h.m.mu.Lock()
	h.m.insts["aaaaaa"].Job = &model.JobInfo{RunID: 1, RunNumber: "412", Workflow: "CI", Name: "build", StartedAt: past}
	h.m.mu.Unlock()
	h.m.confirm("aaaaaa", github.Job{ID: 13, Status: "in_progress", RunID: 1, Name: "build", WorkflowName: "CI", RunnerName: "ghr-darkmem-aaaaaa"}, h.now)
	job := h.m.Status().Instances[0].Job
	if job == nil || !job.StartedAt.Equal(past) || job.RunNumber != "412" {
		t.Fatalf("job %+v, want StartedAt %v", job, past)
	}
}

// One repository refusing a write must not stop the others: only a 401 says
// the token itself is bad.
func TestForbiddenSpawnStopsOnlyItsRepository(t *testing.T) {
	h := newHarness(t)
	labels := []string{"homelab"}
	queuedRun(h, "darkcloud", 1, github.Job{ID: 1, Status: "queued", Labels: labels, CreatedAt: h.now.Add(-time.Minute)})
	queuedRun(h, "darkmem", 2, github.Job{ID: 2, Status: "queued", Labels: labels, CreatedAt: h.now})
	h.gh.setErr("GenerateJITConfig darkcloud", &github.APIError{Status: 403, Kind: github.ErrAuth,
		Message: "Resource not accessible by personal access token", Permissions: "administration=write"})
	h.m.Tick(context.Background())
	if h.m.Status().Degraded {
		t.Fatalf("degraded: %s", h.m.Status().DegradedReason)
	}
	if len(h.sd.started) != 1 || !strings.Contains(h.gh.jitCalls[0], "darkmem") {
		t.Fatalf("started %d, jit calls %v", len(h.sd.started), h.gh.jitCalls)
	}
}

// A 403 on the per-runner check is filed under its repository once per
// backoff window, and the other repositories' runners are still checked.
func TestForbiddenRunnerCheckIsThatRepositorysError(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkcloud") // aaaaaa, runner 101
	h.m.spawn(context.Background(), h.cfg, "darkmem")   // bbbbbb, runner 102
	h.gh.setRunner(102, "online", false)
	h.gh.setErr("GetRunner darkcloud", &github.APIError{Status: 403, Kind: github.ErrAuth,
		Message: "Resource not accessible by personal access token", Permissions: "administration=read"})
	h.m.refreshRunners(context.Background())
	st := h.m.Status()
	want := "token lacks permission for this repository (needs administration=read): github: 403 Resource not accessible by personal access token"
	if st.Degraded || st.Repos[0].Error != want || h.state("bbbbbb") != "idle" {
		t.Fatalf("degraded %v error %q bbbbbb %s", st.Degraded, st.Repos[0].Error, h.state("bbbbbb"))
	}
	h.m.refreshRunners(context.Background()) // the same instant: inside the backoff
	if n := strings.Count(h.eventText(), "token lacks permission"); n != 1 {
		t.Fatalf("%d events for one refusal:\n%s", n, h.eventText())
	}
}

// A 403 that arrives inside another error's backoff window is not filed as an
// event, but the repository's status line shows it at once.
func TestForbiddenInsideAnotherBackoffShowsAtOnce(t *testing.T) {
	h := newHarness(t)
	h.m.apiErr("darkcloud", &github.APIError{Status: 502, Kind: github.ErrServer}, h.now)
	h.m.apiErr("darkcloud", &github.APIError{Status: 403, Kind: github.ErrAuth,
		Message: "Resource not accessible by personal access token", Permissions: "administration=read"}, h.now)
	want := "token lacks permission for this repository (needs administration=read): github: 403 Resource not accessible by personal access token"
	if got := h.m.Status().Repos[0].Error; got != want {
		t.Fatalf("status error %q, want %q", got, want)
	}
	if strings.Contains(h.eventText(), "token lacks permission") {
		t.Fatalf("an event was filed inside the window:\n%s", h.eventText())
	}
}

func TestForbiddenPollIsThatRepositorysError(t *testing.T) {
	h := newHarness(t)
	h.gh.setErr("ListRuns darkcloud", &github.APIError{Status: 403, Kind: github.ErrAuth,
		Message: "Resource not accessible by personal access token", Permissions: "actions=read"})
	queuedRun(h, "darkmem", 2, github.Job{ID: 3, Status: "queued", Labels: []string{"homelab"}, CreatedAt: h.now})
	h.m.Tick(context.Background())
	st := h.m.Status()
	want := "token lacks permission for this repository (needs actions=read): github: 403 Resource not accessible by personal access token"
	if st.Degraded || st.Repos[0].Error != want || len(h.sd.started) != 1 {
		t.Fatalf("degraded %v started %d error %q", st.Degraded, len(h.sd.started), st.Repos[0].Error)
	}
}

// A repository whose poll fails keeps its last known queued count.
func TestQueuedCountSurvivesAFailedPoll(t *testing.T) {
	h := newHarness(t)
	h.cfg.GlobalMax = 0 // nothing spawns, so the job stays queued
	queuedRun(h, "darkmem", 2, github.Job{ID: 3, Status: "queued", Labels: []string{"homelab"}, CreatedAt: h.now})
	h.m.Tick(context.Background())
	h.gh.setErr("ListRuns darkmem", &github.APIError{Status: 502, Kind: github.ErrServer})
	h.now = h.now.Add(10 * time.Second)
	h.m.Tick(context.Background())
	if r := h.m.Status().Repos[1]; r.Queued != 1 || r.Error == "" {
		t.Fatalf("queued %d error %q", r.Queued, r.Error)
	}
}

func pause(h *harness, repo string) {
	p := h.cfg.Clone()
	p.Repo(repo).Paused = true
	h.cfg = p
}

// A paused repository is not polled, so an error from before the pause would
// otherwise stay on screen for as long as the pause lasts.
func TestPausedRepositoryDropsItsError(t *testing.T) {
	h := newHarness(t)
	h.gh.setErr("ListRuns darkmem", &github.APIError{Status: 404, Kind: github.ErrNotFound})
	h.m.Tick(context.Background())
	if h.m.Status().Repos[1].Error == "" {
		t.Fatal("no error before the pause")
	}
	pause(h, "darkmem")
	h.m.Tick(context.Background())
	if e := h.m.Status().Repos[1].Error; e != "" {
		t.Fatalf("error kept while paused: %q", e)
	}
}

// A paused repository's busy runner still gets its job confirmed, so its job
// name and link show. Its queue is not counted: nothing will serve it, and it
// must not hold a runner update back.
func TestPausedRepositoryStillConfirmsItsBusyRunner(t *testing.T) {
	h := newHarness(t)
	if err := h.m.spawn(context.Background(), h.cfg, "darkmem"); err != nil {
		t.Fatal(err)
	}
	h.writeJob(t, "aaaaaa", 55)
	h.gh.runs["darkmem/in_progress"] = []github.Run{{ID: 55}}
	h.gh.jobs[55] = []github.Job{
		{ID: 13, Status: "in_progress", RunID: 55, Name: "build", WorkflowName: "CI", RunnerName: "ghr-darkmem-aaaaaa", HTMLURL: "https://x/13"},
		{ID: 14, Status: "queued", Labels: []string{"homelab"}, CreatedAt: h.now},
	}
	pause(h, "darkmem")
	h.m.Tick(context.Background())
	st := h.m.Status()
	if j := st.Instances[0].Job; j == nil || j.Name != "build" || j.HTMLURL != "https://x/13" {
		t.Fatalf("job %+v", st.Instances[0].Job)
	}
	if st.Repos[1].Queued != 0 {
		t.Fatalf("a paused repository's queue is counted: %d", st.Repos[1].Queued)
	}
	polls := strings.Count(h.gh.callsText(), "ListRuns darkmem")
	h.m.Tick(context.Background())
	if n := strings.Count(h.gh.callsText(), "ListRuns darkmem"); n != polls {
		t.Fatalf("polled again after the job was confirmed: %d then %d", polls, n)
	}
}

// Pausing a repository while API calls are paused must still clear what its
// last polls left behind; that needs no API call.
func TestPausingDuringAnAPIPauseStillClearsTheRepository(t *testing.T) {
	h := newHarness(t)
	h.cfg.GlobalMax = 0 // nothing spawns, so the job stays queued
	queuedRun(h, "darkmem", 2, github.Job{ID: 3, Status: "queued", Labels: []string{"homelab"}, CreatedAt: h.now})
	h.m.Tick(context.Background())
	h.m.apiErr("darkmem", &github.APIError{Status: 502, Kind: github.ErrServer}, h.now)
	h.m.apiErr("darkcloud", &github.APIError{Status: 403, Kind: github.ErrRateLimit, RetryAt: h.now.Add(time.Hour)}, h.now)
	if r := h.m.Status().Repos[1]; r.Queued != 1 || r.Error == "" {
		t.Fatalf("setup: queued %d error %q", r.Queued, r.Error)
	}
	pause(h, "darkmem")
	h.m.Tick(context.Background())
	if r := h.m.Status().Repos[1]; r.Queued != 0 || r.Error != "" {
		t.Fatalf("kept while paused: queued %d error %q", r.Queued, r.Error)
	}
}

func TestPausedRepositoryHidesItsSpawnError(t *testing.T) {
	h := newHarness(t)
	h.m.spawnFailed("darkmem", "boom", h.now)
	pause(h, "darkmem")
	if e := h.m.Status().Repos[1].Error; e != "" {
		t.Fatalf("spawn error shown for a paused repository: %q", e)
	}
}
