package runner

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/github"
)

func labelsOf(names ...string) []github.Label {
	out := make([]github.Label, len(names))
	for i, n := range names {
		out[i] = github.Label{Name: n}
	}
	return out
}

func (h *harness) effective(repo string) []string {
	return h.cfg.EffectiveLabels(*h.cfg.Repo(repo))
}

func (h *harness) queueJobs(repo string, runID int64, n int) {
	var jobs []github.Job
	for i := range n {
		jobs = append(jobs, github.Job{ID: runID*10 + int64(i), Status: "queued", Labels: []string{"homelab"}, CreatedAt: h.now})
	}
	queuedRun(h, repo, runID, jobs...)
}

func (h *harness) spawnedFor(repo string) int {
	n := 0
	for _, c := range h.gh.jitCalls {
		if strings.HasPrefix(c, "ghr-"+repo+"-") {
			n++
		}
	}
	return n
}

// A runner another ghr host registered for the repository takes the queued
// job GitHub hands it, so this host does not start a second runner for it.
func TestForeignRegistrationCoversQueuedJob(t *testing.T) {
	upper := func(ls []string) []string {
		out := make([]string, len(ls))
		for i, l := range ls {
			out[i] = strings.ToUpper(l)
		}
		return out
	}
	tests := []struct {
		name   string
		runner func(h *harness) github.Runner
		spawns int
	}{
		{"idle online runner covers", func(h *harness) github.Runner {
			return github.Runner{ID: 9001, Name: "ghr-darkmem-123abc", Status: "online", Labels: labelsOf(h.effective("darkmem")...)}
		}, 0},
		{"offline runner that may still be starting covers", func(h *harness) github.Runner {
			return github.Runner{ID: 9001, Name: "ghr-darkmem-123abc", Status: "offline", Labels: labelsOf(h.effective("darkmem")...)}
		}, 0},
		{"labels match case-insensitively", func(h *harness) github.Runner {
			return github.Runner{ID: 9001, Name: "GHR-DarkMem-123abc", Status: "online", Labels: labelsOf(upper(h.effective("darkmem"))...)}
		}, 0},
		{"extra labels still cover", func(h *harness) github.Runner {
			return github.Runner{ID: 9001, Name: "ghr-darkmem-123abc", Status: "online", Labels: labelsOf(append(h.effective("darkmem"), "gpu")...)}
		}, 0},
		{"busy runner does not cover", func(h *harness) github.Runner {
			return github.Runner{ID: 9001, Name: "ghr-darkmem-123abc", Status: "online", Busy: true, Labels: labelsOf(h.effective("darkmem")...)}
		}, 1},
		{"runner missing one of this host's labels does not cover", func(h *harness) github.Runner {
			return github.Runner{ID: 9001, Name: "ghr-darkmem-123abc", Status: "online", Labels: labelsOf(config.SystemLabels...)}
		}, 1},
		{"runner not named by ghr does not cover", func(h *harness) github.Runner {
			return github.Runner{ID: 9001, Name: "builder-1", Status: "online", Labels: labelsOf(h.effective("darkmem")...)}
		}, 1},
		{"runner of a repository whose name extends this one does not cover", func(h *harness) github.Runner {
			return github.Runner{ID: 9001, Name: "ghr-darkmem-web-123abc", Status: "online", Labels: labelsOf(h.effective("darkmem")...)}
		}, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.queueJobs("darkmem", 1, 1)
			h.gh.listed["darkmem"] = []github.Runner{tc.runner(h)}
			h.m.Tick(context.Background())
			if got := h.spawnedFor("darkmem"); got != tc.spawns {
				t.Fatalf("spawned %d runners, want %d: %v", got, tc.spawns, h.gh.jitCalls)
			}
		})
	}
}

func TestForeignRegistrationCoversOneJobEach(t *testing.T) {
	h := newHarness(t)
	three := 3
	h.cfg.Repos[1].Max = &three
	h.queueJobs("darkmem", 1, 2)
	h.gh.listed["darkmem"] = []github.Runner{{ID: 9001, Name: "ghr-darkmem-123abc", Status: "online", Labels: labelsOf(h.effective("darkmem")...)}}
	h.m.Tick(context.Background())
	if got := h.spawnedFor("darkmem"); got != 1 {
		t.Fatalf("spawned %d runners, want 1: %v", got, h.gh.jitCalls)
	}
}

// A registration left behind by a host that is gone stays offline; it covers
// only as long as a runner gets to come online, then this host spawns.
func TestOfflineForeignRegistrationStopsCoveringAfterStartTimeout(t *testing.T) {
	h := newHarness(t)
	h.queueJobs("darkmem", 1, 1)
	h.gh.listed["darkmem"] = []github.Runner{{ID: 9001, Name: "ghr-darkmem-123abc", Status: "offline", Labels: labelsOf(h.effective("darkmem")...)}}
	h.m.Tick(context.Background())
	h.now = h.now.Add(h.cfg.StartTimeout.D() - h.cfg.PollInterval.D())
	h.m.Tick(context.Background())
	if got := h.spawnedFor("darkmem"); got != 0 {
		t.Fatalf("spawned %d runners within the start timeout", got)
	}
	h.now = h.now.Add(h.cfg.PollInterval.D())
	h.m.Tick(context.Background())
	if got := h.spawnedFor("darkmem"); got != 1 {
		t.Fatalf("spawned %d runners after the start timeout, want 1", got)
	}
}

// This host's own registrations are in the list too; they cover through the
// instances that track them and must not count a second time.
func TestOwnRegistrationsAreNotForeign(t *testing.T) {
	h := newHarness(t)
	three := 3
	h.cfg.GlobalMax = 3
	h.cfg.Repos[1].Max = &three
	h.queueJobs("darkmem", 1, 2)
	h.m.Tick(context.Background())
	if got := h.spawnedFor("darkmem"); got != 2 {
		t.Fatalf("first tick spawned %d runners, want 2", got)
	}
	for _, r := range h.gh.runners {
		h.gh.listed["darkmem"] = append(h.gh.listed["darkmem"], github.Runner{ID: r.ID, Name: r.Name, Status: "offline", Labels: labelsOf(h.effective("darkmem")...)})
	}
	h.queueJobs("darkmem", 2, 1)
	h.now = h.now.Add(h.cfg.PollInterval.D())
	h.m.Tick(context.Background())
	if got := h.spawnedFor("darkmem"); got != 3 {
		t.Fatalf("second tick left %d runners, want 3", got)
	}
}

// A registration whose delete failed is this host's, from a runner that is
// gone; it can take no job.
func TestUndeletedRegistrationIsNotForeign(t *testing.T) {
	h := newHarness(t)
	h.queueJobs("darkmem", 1, 1)
	h.m.undeleted[9001] = registration{repo: "darkmem", name: "ghr-darkmem-123abc"}
	h.gh.setErr("DeleteRunner darkmem", &github.APIError{Status: 502, Kind: github.ErrServer})
	h.gh.listed["darkmem"] = []github.Runner{{ID: 9001, Name: "ghr-darkmem-123abc", Status: "offline", Labels: labelsOf(h.effective("darkmem")...)}}
	h.m.Tick(context.Background())
	if got := h.spawnedFor("darkmem"); got != 1 {
		t.Fatalf("spawned %d runners, want 1", got)
	}
}

// Without the runner list this host cannot tell whether another host is
// already starting a runner, so the repository waits like one whose poll failed.
func TestUnreadRunnerListHoldsTheRepositorysSpawns(t *testing.T) {
	h := newHarness(t)
	h.queueJobs("darkmem", 1, 1)
	h.gh.setErr("ListRunners darkmem", &github.APIError{Status: 502, Kind: github.ErrServer, Message: "bad gateway"})
	h.m.Tick(context.Background())
	if got := h.spawnedFor("darkmem"); got != 0 {
		t.Fatalf("spawned %d runners without a runner list", got)
	}
	for _, r := range h.m.Status().Repos {
		if r.Name == "darkmem" && r.Error == "" {
			t.Fatal("the failed runner list is not shown on the repository")
		}
	}
}

// Warm runners are this host's own, so a spawn for warmth reads no list.
func TestWarmSpawnReadsNoRunnerList(t *testing.T) {
	h := newHarness(t)
	h.cfg.Mode = config.ModeAll
	h.m.Tick(context.Background())
	if h.spawnedFor("darkmem") == 0 {
		t.Fatal("no warm runner spawned")
	}
	for _, c := range h.gh.calls {
		if strings.HasPrefix(c, "ListRunners ") {
			t.Fatalf("read the runner list for a warm spawn: %v", h.gh.calls)
		}
	}
}

// Covered jobs are a repository's oldest, as with this host's own runners,
// so the global first-come order goes by the jobs still waiting.
func TestForeignRegistrationCoversTheOldestJobs(t *testing.T) {
	h := newHarness(t)
	h.cfg.GlobalMax = 1
	queuedRun(h, "darkmem", 1,
		github.Job{ID: 11, Status: "queued", Labels: []string{"homelab"}, CreatedAt: h.now.Add(-10 * time.Minute)},
		github.Job{ID: 12, Status: "queued", Labels: []string{"homelab"}, CreatedAt: h.now.Add(-time.Minute)})
	queuedRun(h, "darkcloud", 2, github.Job{ID: 21, Status: "queued", Labels: []string{"homelab"}, CreatedAt: h.now.Add(-5 * time.Minute)})
	h.gh.listed["darkmem"] = []github.Runner{{ID: 9001, Name: "ghr-darkmem-123abc", Status: "online", Labels: labelsOf(h.effective("darkmem")...)}}
	h.m.Tick(context.Background())
	if len(h.gh.jitCalls) != 1 || h.spawnedFor("darkcloud") != 1 {
		t.Fatalf("jit calls %v, want one for darkcloud", h.gh.jitCalls)
	}
}
