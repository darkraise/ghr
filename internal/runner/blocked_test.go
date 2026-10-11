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

const darkmemBlocked = "darkmem is public; ghr serves only private repositories. Set allow_public: true for it in config.yaml and reload to serve it"

// blockDarkmem makes darkmem public on GitHub and runs the tick that finds out.
func blockDarkmem(t *testing.T, h *harness) {
	t.Helper()
	h.gh.setPublic("darkmem", true)
	h.m.ClearDegraded()
	h.m.Tick(context.Background())
	if !h.m.blocked(h.cfg, h.cfg.Repos[1]) {
		t.Fatal("setup: darkmem is not blocked")
	}
}

func eventCount(h *harness, line string) int {
	return strings.Count(h.eventText(), line+"\n")
}

// The owner sees why a blocked repository gets nothing, on the tick that
// found it public and after every later poll, whatever its poll or its last
// start did. Its queue is not counted. What the poll said is kept underneath.
func TestBlockedRepositoryShowsWhyAndNoQueue(t *testing.T) {
	h := newHarness(t)
	h.cfg.GlobalMax = 0 // nothing spawns, so the job stays queued
	queuedRun(h, "darkmem", 7, homelabJob(h, 71))
	h.m.Tick(context.Background())
	if r := h.m.Status().Repos[1]; r.Queued != 1 || r.Error != "" {
		t.Fatalf("setup: queued %d error %q", r.Queued, r.Error)
	}
	check := func(when string) {
		t.Helper()
		st := h.m.Status()
		if r := st.Repos[1]; r.Error != darkmemBlocked || r.Queued != 0 || r.OldestQueuedAt != nil || r.Paused {
			t.Fatalf("%s: error %q queued %d oldest %v paused %v", when, r.Error, r.Queued, r.OldestQueuedAt, r.Paused)
		}
		if e := st.Repos[0].Error; e != "" {
			t.Fatalf("%s: darkcloud shows %q", when, e)
		}
	}
	h.now = h.now.Add(10 * time.Second)
	blockDarkmem(t, h)
	check("on the tick that found it public")
	h.now = h.now.Add(10 * time.Second)
	h.m.Tick(context.Background())
	check("after a later successful poll")
	h.gh.setErr("ListRuns darkmem", &github.APIError{Status: 502, Kind: github.ErrServer, Message: "Bad Gateway"})
	h.m.spawnFailed("darkmem", "boom", h.now)
	h.now = h.now.Add(10 * time.Second)
	h.m.Tick(context.Background())
	check("with a failed poll and a failed start")
	allowed := h.cfg.Clone()
	allowed.Repo("darkmem").AllowPublic = true
	h.cfg = allowed
	if e := h.m.Status().Repos[1].Error; e != "github: 502 Bad Gateway" {
		t.Fatalf("with allow_public the poll error must show again, got %q", e)
	}
}

// A repository also becomes blocked without a poll, when the owner takes
// allow_public away. Its last known queue must go even if its next poll fails.
func TestBlockedByConfigDropsTheQueueWithoutAPoll(t *testing.T) {
	h := newHarness(t)
	h.cfg.GlobalMax = 0 // nothing spawns, so the job stays queued
	h.gh.setPublic("darkmem", true)
	allowPublic(h, "darkmem")
	queuedRun(h, "darkmem", 7, homelabJob(h, 71))
	h.m.Tick(context.Background())
	if r := h.m.Status().Repos[1]; r.Queued != 1 || r.Error != "" {
		t.Fatalf("setup: queued %d error %q", r.Queued, r.Error)
	}
	denied := h.cfg.Clone()
	denied.Repo("darkmem").AllowPublic = false
	h.cfg = denied
	h.gh.setErr("ListRuns darkmem", &github.APIError{Status: 502, Kind: github.ErrServer})
	h.now = h.now.Add(10 * time.Second)
	ok, unanswered := h.m.gatherDemand(context.Background(), h.cfg, h.now)
	if r := h.m.Status().Repos[1]; r.Queued != 0 || r.Error != darkmemBlocked || !ok {
		t.Fatalf("queued %d error %q, %q did not answer", r.Queued, r.Error, unanswered)
	}
}

// One event when a repository becomes blocked and one when it stops being
// blocked, whichever side changed: the flag in the config or GitHub.
func TestBlockedEventsAreWrittenOncePerChange(t *testing.T) {
	h := newHarness(t)
	blocked, lifted := "warn darkmem "+darkmemBlocked, "ok darkmem no longer blocked"
	tick := func() {
		h.now = h.now.Add(10 * time.Second)
		h.m.Tick(context.Background())
	}
	want := func(when string, nBlocked, nLifted int) {
		t.Helper()
		if eventCount(h, blocked) != nBlocked || eventCount(h, lifted) != nLifted {
			t.Fatalf("%s: want %d blocked and %d lifted events:\n%s", when, nBlocked, nLifted, h.eventText())
		}
	}
	h.m.Tick(context.Background())
	want("private", 0, 0)
	blockDarkmem(t, h)
	tick()
	want("found public", 1, 0)
	allowPublic(h, "darkmem")
	tick()
	tick()
	want("allow_public set", 1, 1)
	denied := h.cfg.Clone()
	denied.Repo("darkmem").AllowPublic = false
	h.cfg = denied
	tick()
	want("allow_public removed", 2, 1)
	h.gh.setPublic("darkmem", false)
	h.m.ClearDegraded()
	tick()
	tick()
	want("private again on GitHub", 2, 2)
}

// Removing a repository is not "no longer blocked". Added again it starts
// unread, and is reported again when it is found public again.
func TestRemovedBlockedRepositoryWritesNoEventAndStartsUnread(t *testing.T) {
	h := newHarness(t)
	blockDarkmem(t, h)
	with := h.cfg
	without := h.cfg.Clone()
	without.Repos = without.Repos[:1]
	h.cfg = without
	h.now = h.now.Add(10 * time.Second)
	h.m.Tick(context.Background())
	h.cfg = with
	if !h.m.unread(with.Repos[1]) || h.m.blocked(with, with.Repos[1]) {
		t.Fatalf("added again: unread %v blocked %v, want unread and not blocked", h.m.unread(with.Repos[1]), h.m.blocked(with, with.Repos[1]))
	}
	if e := h.m.Status().Repos[1].Error; e != "" {
		t.Fatalf("an unread repository shows %q", e)
	}
	h.now = h.now.Add(10 * time.Second)
	h.m.Tick(context.Background())
	if eventCount(h, "warn darkmem "+darkmemBlocked) != 2 || strings.Contains(h.eventText(), "no longer blocked") {
		t.Fatalf("want two blocked events and no lifted one:\n%s", h.eventText())
	}
}

// Pausing a blocked repository changes neither why it is blocked nor what the
// config says about the pause, and a paused repository is still not polled.
func TestBlockedRepositoryThatIsAlsoPaused(t *testing.T) {
	h := newHarness(t)
	blockDarkmem(t, h)
	pause(h, "darkmem")
	polls := strings.Count(h.gh.callsText(), "ListRuns darkmem")
	h.now = h.now.Add(10 * time.Second)
	h.m.Tick(context.Background())
	if r := h.m.Status().Repos[1]; !r.Paused || r.Error != darkmemBlocked {
		t.Fatalf("paused %v error %q", r.Paused, r.Error)
	}
	if n := strings.Count(h.gh.callsText(), "ListRuns darkmem"); n != polls {
		t.Fatalf("a paused repository was polled: %d then %d", polls, n)
	}
	resumed := h.cfg.Clone()
	resumed.Repo("darkmem").Paused = false
	h.cfg = resumed
	h.now = h.now.Add(10 * time.Second)
	h.m.Tick(context.Background())
	if r := h.m.Status().Repos[1]; r.Paused || r.Error != darkmemBlocked {
		t.Fatalf("resumed: paused %v error %q", r.Paused, r.Error)
	}
	if eventCount(h, "warn darkmem "+darkmemBlocked) != 1 || strings.Contains(h.eventText(), "no longer blocked") {
		t.Fatalf("a pause and a resume are not changes of the block:\n%s", h.eventText())
	}
}

// Nothing will serve a blocked repository, so neither its queue nor its
// failing poll may hold a runner update back.
func TestBlockedRepositoryDoesNotHoldAnUpdateBack(t *testing.T) {
	for name, setup := range map[string]func(h *harness){
		"its queued job": func(h *harness) { queuedRun(h, "darkmem", 7, homelabJob(h, 71)) },
		"its failed poll": func(h *harness) {
			h.gh.setErr("ListRuns darkmem", &github.APIError{Status: 502, Kind: github.ErrServer})
		},
	} {
		h := newHarness(t)
		blockDarkmem(t, h)
		setup(h)
		queuedUpdate(t, h, tarballSum())
		h.now = h.now.Add(10 * time.Second)
		h.m.Tick(context.Background())
		h.m.Wait()
		if current(t, h) != "2.338.0" || strings.Contains(h.eventText(), "runner update waiting") {
			t.Fatalf("%s: runner %s\n%s", name, current(t, h), h.eventText())
		}
	}
}
