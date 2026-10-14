package runner

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/github"
)

func jitAttempts(h *harness, repo string) int {
	return strings.Count(h.gh.callsText(), "GenerateJITConfig "+repo)
}

func spawnFails(h *harness, repo string) int {
	h.m.mu.Lock()
	defer h.m.mu.Unlock()
	return h.m.spawnFails[repo]
}

func spawnFailures(h *harness) int {
	h.m.mu.Lock()
	defer h.m.mu.Unlock()
	return len(h.m.spawnFails)
}

// A failing spawn is retried after 10 s, 20 s, 40 s and so on. The polls
// between the attempts succeed and must not shorten the wait.
func TestSpawnBackoffDoublesAndSurvivesPolls(t *testing.T) {
	h := newHarness(t)
	queuedRun(h, "darkmem", 2, github.Job{ID: 3, Status: "queued", Labels: []string{"homelab"}, CreatedAt: h.now})
	h.gh.setErr("GenerateJITConfig darkmem", &github.APIError{Status: 502, Kind: github.ErrServer})
	for tick, want := range []int{1, 2, 2, 3, 3, 3, 3, 4} { // ticks 10 s apart
		h.m.Tick(context.Background())
		if got := jitAttempts(h, "darkmem"); got != want {
			t.Fatalf("tick %d: %d attempts, want %d\n%s", tick, got, want, h.eventText())
		}
		h.now = h.now.Add(10 * time.Second)
	}
	if !strings.Contains(h.eventText(), "error darkmem spawn failed: github: 502 ; next attempt in 10s") {
		t.Fatalf("events:\n%s", h.eventText())
	}
	var d time.Duration
	for range 20 {
		d = h.m.spawnFailed("darkmem", "boom", h.now)
	}
	if d != maxSpawnBackoff {
		t.Fatalf("delay %s, want the cap %s", d, maxSpawnBackoff)
	}
}

// A 403 on a spawn carries the permission wording in its event and in the
// repository's status line.
func TestForbiddenSpawnSaysWhichPermissionIsMissing(t *testing.T) {
	h := newHarness(t)
	queuedRun(h, "darkcloud", 1, github.Job{ID: 1, Status: "queued", Labels: []string{"homelab"}, CreatedAt: h.now})
	h.gh.setErr("GenerateJITConfig darkcloud", &github.APIError{Status: 403, Kind: github.ErrAuth,
		Message: "Resource not accessible by personal access token", Permissions: "administration=write"})
	h.m.Tick(context.Background())
	want := "token lacks permission for this repository (needs administration=write): github: 403 Resource not accessible by personal access token"
	if !strings.Contains(h.eventText(), "error darkcloud spawn failed: "+want+"; next attempt in 10s") || h.m.Status().Repos[0].Error != want {
		t.Fatalf("status %q\n%s", h.m.Status().Repos[0].Error, h.eventText())
	}
}

func TestSpawnBackoffClearsWhenARunnerComesOnline(t *testing.T) {
	h := newHarness(t)
	h.m.spawnFailed("darkmem", "boom", h.now)
	if e := h.m.Status().Repos[1].Error; e != "boom" {
		t.Fatalf("status error %q, want the spawn error", e)
	}
	if err := h.m.spawn(context.Background(), h.cfg, "darkmem"); err != nil {
		t.Fatal(err)
	}
	h.gh.setRunner(101, "online", false)
	h.m.refreshRunners(context.Background())
	if spawnFailures(h) != 0 || h.m.Status().Repos[1].Error != "" {
		t.Fatalf("failures %d error %q", spawnFailures(h), h.m.Status().Repos[1].Error)
	}
	h.m.spawnFailed("darkmem", "boom", h.now)
	h.m.ClearDegraded()
	if spawnFailures(h) != 0 {
		t.Fatal("a replaced token must clear the spawn backoff")
	}
}

// A runner that exits before it ever came online is a failed start: the tick
// that sees the exit must not respawn it at once.
func TestRunnerThatNeverCameOnlineDelaysTheNextSpawn(t *testing.T) {
	h := newHarness(t)
	queuedRun(h, "darkmem", 2, github.Job{ID: 3, Status: "queued", Labels: []string{"homelab"}, CreatedAt: h.now})
	h.m.Tick(context.Background())
	if jitAttempts(h, "darkmem") != 1 {
		t.Fatalf("attempts %d", jitAttempts(h, "darkmem"))
	}
	h.sd.active["ghr-runner-aaaaaa"] = false
	h.m.Tick(context.Background())
	h.m.Wait()
	if !strings.Contains(h.eventText(), "warn darkmem runner aaaaaa exited before coming online; next spawn in 10s") {
		t.Fatalf("events:\n%s", h.eventText())
	}
	h.m.Tick(context.Background())
	if n := jitAttempts(h, "darkmem"); n != 1 {
		t.Fatalf("respawned inside the delay: %d attempts", n)
	}
	h.now = h.now.Add(10 * time.Second)
	h.m.Tick(context.Background())
	if n := jitAttempts(h, "darkmem"); n != 2 {
		t.Fatalf("attempts after the delay: %d", n)
	}
}

// A runner stopped for not coming online is a failed start like one that
// exits: its replacement waits out the delay, which doubles with each repeat.
func TestStartTimedOutRunnerDelaysItsReplacement(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.cfg.Mode = config.ModeAll
	h.cfg.Repo("darkcloud").Paused = true
	step := func(d time.Duration, want int) {
		t.Helper()
		h.now = h.now.Add(d)
		h.m.Tick(ctx)
		h.m.Wait()
		if got := jitAttempts(h, "darkmem"); got != want {
			t.Fatalf("at +%s: %d attempts, want %d\n%s", d, got, want, h.eventText())
		}
	}
	step(0, 1)
	step(3*time.Minute, 1) // aaaaaa times out and is stopped; its replacement waits
	if !strings.Contains(h.eventText(), "next spawn in 10s") {
		t.Fatalf("events:\n%s", h.eventText())
	}
	step(0, 1) // the exit the next tick sees is the same failed start
	if n := spawnFails(h, "darkmem"); n != 1 {
		t.Fatalf("%d failed starts, want 1", n)
	}
	step(10*time.Second, 2) // bbbbbb
	step(3*time.Minute, 2)  // bbbbbb times out: the second failure in a row
	step(10*time.Second, 2)
	step(10*time.Second, 3)
}

func TestAStoppedOrAdoptedRunnerIsNotASpawnFailure(t *testing.T) {
	t.Run("stopped by a person", func(t *testing.T) {
		h := newHarness(t)
		if err := h.m.spawn(context.Background(), h.cfg, "darkmem"); err != nil {
			t.Fatal(err)
		}
		h.sd.keepActive = true // the unit is still stopping when Kill returns
		if err := h.m.Kill(context.Background(), "aaaaaa"); err != nil {
			t.Fatal(err)
		}
		h.sd.keepActive = false
		h.sd.active["ghr-runner-aaaaaa"] = false
		h.m.Tick(context.Background())
		h.m.Wait()
		if spawnFailures(h) != 0 {
			t.Fatalf("events:\n%s", h.eventText())
		}
	})
	t.Run("adopted", func(t *testing.T) {
		h := newHarness(t)
		makeInstance(t, h, "bbbbbb", "darkmem", 7, false)
		h.sd.active["ghr-runner-bbbbbb"] = true
		if err := h.m.Adopt(context.Background()); err != nil {
			t.Fatal(err)
		}
		h.sd.active["ghr-runner-bbbbbb"] = false
		h.m.Tick(context.Background())
		h.m.Wait()
		if spawnFailures(h) != 0 {
			t.Fatalf("events:\n%s", h.eventText())
		}
	})
}

// A runner that ran a whole job between two ticks did come online, even
// though no tick saw it idle.
func TestRunnerThatRanAJobBetweenTicksClearsTheSpawnBackoff(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.m.spawnFailed("darkmem", "boom", h.now)
	if err := h.m.spawn(ctx, h.cfg, "darkmem"); err != nil {
		t.Fatal(err)
	}
	h.writeJob(t, "aaaaaa", 55)
	h.sd.active["ghr-runner-aaaaaa"] = false
	h.m.refreshUnits(ctx)
	h.m.Wait()
	if spawnFailures(h) != 0 || h.m.Status().Repos[1].Error != "" {
		t.Fatalf("failures %d error %q", spawnFailures(h), h.m.Status().Repos[1].Error)
	}
}

// An instance carries its repository in the spelling it was spawned under. A
// refusal filed under that spelling holds back the config's repository all
// the same, and its status shows it.
func TestRepoBackoffIgnoresTheSpelling(t *testing.T) {
	h := newHarness(t)
	h.cfg.Repos[1].Name = "myrepo"
	h.m.apiErr("MyRepo", &github.APIError{Status: 403, Kind: github.ErrAuth, Message: "Resource not accessible by personal access token"}, h.now)
	h.m.Tick(context.Background())
	if strings.Contains(h.gh.callsText(), "ListRuns myrepo") {
		t.Fatalf("polled during its backoff: %s", h.gh.callsText())
	}
	if got := h.m.Status().Repos[1].Error; !strings.Contains(got, "token lacks permission") {
		t.Fatalf("status error %q", got)
	}
}
