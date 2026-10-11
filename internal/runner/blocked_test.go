package runner

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/github"
)

// allowPublic sets allow_public for repo and applies it as a reload of
// config.yaml does.
func allowPublic(h *harness, repo string) {
	c := h.cfg.Clone()
	c.Repo(repo).AllowPublic = true
	h.cfg = c
	h.m.ClearDegraded()
}

func homelabJob(h *harness, id int64) github.Job {
	return github.Job{ID: id, Status: "queued", Labels: []string{"homelab"}, CreatedAt: h.now}
}

func spawnsFor(h *harness, repo string) int {
	return strings.Count(h.gh.callsText(), "GenerateJITConfig "+repo)
}

// Blocked is public on GitHub without allow_public in the config; unread is
// no answer yet. The plan copy pauses the blocked repositories and nothing
// else, and leaves the config it was made from alone.
func TestBlockedUnreadAndThePlanCopy(t *testing.T) {
	h := newHarness(t)
	h.gh.setPublic("darkmem", true)
	cloud, mem := h.cfg.Repos[0], h.cfg.Repos[1]
	if !h.m.unread(cloud) || !h.m.unread(mem) || h.m.blocked(h.cfg, mem) {
		t.Fatal("before any read every repository is unread and none is blocked")
	}
	if plan := h.m.planConfig(h.cfg); plan.Repos[0].Paused || plan.Repos[1].Paused {
		t.Fatalf("an unread repository is paused in the plan copy: %+v", plan.Repos)
	}
	h.m.gatherDemand(context.Background(), h.cfg, h.now)
	if h.m.unread(cloud) || h.m.unread(mem) {
		t.Fatal("unread after a read")
	}
	if h.m.blocked(h.cfg, cloud) || !h.m.blocked(h.cfg, mem) {
		t.Fatalf("blocked: darkcloud %v darkmem %v", h.m.blocked(h.cfg, cloud), h.m.blocked(h.cfg, mem))
	}
	plan := h.m.planConfig(h.cfg)
	if plan == h.cfg || plan.Repos[0].Paused || !plan.Repos[1].Paused {
		t.Fatalf("plan copy %+v", plan.Repos)
	}
	if h.cfg.Repos[1].Paused {
		t.Fatal("the config itself was changed")
	}
	allowPublic(h, "darkmem")
	if h.m.blocked(h.cfg, h.cfg.Repos[1]) || h.m.planConfig(h.cfg).Repos[1].Paused {
		t.Fatal("blocked with allow_public set")
	}
}

// A repository that turns out to be public gets no new runner and loses its
// idle one. Its running job goes on, the other repositories are served, and
// allow_public brings it back.
func TestBlockedRepositoryGetsNoRunnerAndLosesItsIdleOne(t *testing.T) {
	h := newHarness(t)
	h.cfg.GlobalMax = 6
	three := 3
	h.cfg.Repos[1].Max = &three
	for range 2 {
		if err := h.m.spawn(context.Background(), h.cfg, "darkmem"); err != nil {
			t.Fatal(err)
		}
	}
	h.gh.setRunner(101, "online", true) // aaaaaa has a job
	h.gh.setRunner(102, "online", false)
	h.m.Tick(context.Background()) // darkmem is read: private
	if h.state("aaaaaa") != "busy" || h.state("bbbbbb") != "idle" {
		t.Fatalf("setup: aaaaaa %s bbbbbb %s", h.state("aaaaaa"), h.state("bbbbbb"))
	}

	h.gh.setPublic("darkmem", true)
	h.m.ClearDegraded()
	queuedRun(h, "darkmem", 7, homelabJob(h, 71), homelabJob(h, 72))
	queuedRun(h, "darkcloud", 8, homelabJob(h, 81))
	h.now = h.now.Add(10 * time.Second)
	h.m.Tick(context.Background())
	h.m.Wait()
	if len(h.sd.stopped) != 1 || h.sd.stopped[0] != "ghr-runner-bbbbbb" || h.state("aaaaaa") != "busy" {
		t.Fatalf("stopped %v, aaaaaa %s; want only the idle runner stopped", h.sd.stopped, h.state("aaaaaa"))
	}
	if spawnsFor(h, "darkmem") != 2 || spawnsFor(h, "darkcloud") != 1 {
		t.Fatalf("spawns: darkmem %d (2 before it was public), darkcloud %d", spawnsFor(h, "darkmem"), spawnsFor(h, "darkcloud"))
	}

	allowPublic(h, "darkmem")
	h.now = h.now.Add(10 * time.Second)
	h.m.Tick(context.Background())
	h.m.Wait()
	// One of the two queued jobs may be the one aaaaaa is running, which
	// GitHub has not confirmed yet, so one runner is enough.
	if spawnsFor(h, "darkmem") != 3 {
		t.Fatalf("not served with allow_public: %d spawns for darkmem\n%s", spawnsFor(h, "darkmem"), h.eventText())
	}
}

// spawnPlanned plans nothing for a repository GitHub has not answered for,
// whatever else is known about it.
func TestSpawnPlannedLeavesOutAnUnreadRepository(t *testing.T) {
	h := newHarness(t)
	h.cfg.Mode = config.ModeAll // each repository wants one warm runner
	if err := h.m.readVisibility(context.Background(), "darkcloud", h.now); err != nil {
		t.Fatal(err)
	}
	h.m.spawnPlanned(context.Background(), h.cfg, h.now)
	if len(h.gh.jitCalls) != 1 || !strings.HasPrefix(h.gh.jitCalls[0], "ghr-darkcloud-") {
		t.Fatalf("jit calls %v, want a runner for darkcloud only", h.gh.jitCalls)
	}
}

// After a restart nothing has been read. A repository whose first read fails
// keeps the runner it had and gets no new one until GitHub answers.
func TestUnreadRepositoryKeepsItsAdoptedRunner(t *testing.T) {
	h := newHarness(t)
	two := 2
	h.cfg.Repos[1].Max = &two
	makeInstance(t, h, "aaaaaa", "darkmem", 1, false)
	h.sd.active["ghr-runner-aaaaaa"] = true
	h.gh.setRunner(1, "online", false)
	if err := h.m.Adopt(context.Background()); err != nil {
		t.Fatal(err)
	}
	queuedRun(h, "darkmem", 7, homelabJob(h, 71), homelabJob(h, 72)) // one more than the adopted runner covers
	h.gh.setErr("GetRepo darkmem", &github.APIError{Status: 502, Kind: github.ErrServer})
	h.m.Tick(context.Background())
	if !h.m.unread(h.cfg.Repos[1]) || h.state("aaaaaa") != "idle" || len(h.sd.stopped) != 0 || len(h.gh.jitCalls) != 0 {
		t.Fatalf("unread %v aaaaaa %s stopped %v jit calls %v", h.m.unread(h.cfg.Repos[1]), h.state("aaaaaa"), h.sd.stopped, h.gh.jitCalls)
	}
	h.gh.setErr("GetRepo darkmem", nil)
	h.now = h.now.Add(10 * time.Second)
	h.m.Tick(context.Background())
	if len(h.sd.stopped) != 0 || len(h.gh.jitCalls) != 1 {
		t.Fatalf("stopped %v jit calls %v; want the second job's runner once GitHub answered", h.sd.stopped, h.gh.jitCalls)
	}
}

// A public repository that was paused when ghr started has never been read.
// Resuming it must not give it a runner: the read comes before the plan.
func TestResumedPublicRepositoryIsBlockedBeforeItIsPlanned(t *testing.T) {
	h := newHarness(t)
	h.gh.setPublic("darkmem", true)
	pause(h, "darkmem")
	queuedRun(h, "darkmem", 7, homelabJob(h, 71))
	h.m.Tick(context.Background())
	if !h.m.unread(h.cfg.Repos[1]) {
		t.Fatal("a paused repository was read")
	}
	resumed := h.cfg.Clone()
	resumed.Repo("darkmem").Paused = false
	h.cfg = resumed
	h.m.Tick(context.Background())
	if !h.m.blocked(h.cfg, h.cfg.Repos[1]) || len(h.gh.jitCalls) != 0 {
		t.Fatalf("blocked %v jit calls %v", h.m.blocked(h.cfg, h.cfg.Repos[1]), h.gh.jitCalls)
	}
}
