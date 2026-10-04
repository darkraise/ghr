package runner

import (
	"context"
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
		if len(h.gh.jitCalls) != 2 {
			t.Fatalf("%v: no warm runner after recovery: %v", err, h.gh.jitCalls)
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
	if len(h.gh.jitCalls) != 2 || h.gh.jitCalls[1] != "ghr-darkmem-bbbbbb homelab,gpu" {
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
