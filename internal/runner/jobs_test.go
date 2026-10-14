package runner

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/jobs"
	"github.com/darkraise/ghr/internal/model"
)

// jobStatus is the named task as the manager's registry reports it.
func jobStatus(t *testing.T, r *jobs.Registry, name string) model.JobStatus {
	t.Helper()
	for _, j := range r.Snapshot() {
		if j.Name == name {
			return j
		}
	}
	t.Fatalf("no task %s", name)
	return model.JobStatus{}
}

func TestRunnerCheckFollowsTheJobsBlock(t *testing.T) {
	h := newHarness(t)
	h.linkDist(t, "2.337.0")
	h.gh.setReleases(release("2.337.0", 0, ""))
	ctx := context.Background()
	checks := func() int { return strings.Count(h.gh.callsText(), "ListRunnerReleases") }
	h.cfg.Jobs.RunnerUpdateCheck.Every = config.Duration(2 * time.Hour)

	h.m.Tick(ctx)
	if checks() != 1 {
		t.Fatalf("first tick: %d checks", checks())
	}
	if j := jobStatus(t, h.m.Jobs, config.JobRunnerUpdateCheck); j.LastFinish == nil || !j.LastFinish.Equal(h.now) || j.LastError != "" {
		t.Fatalf("task %+v", j)
	}
	if err := h.m.Jobs.RunNow(config.JobRunnerUpdateCheck); err != nil {
		t.Fatal(err)
	}
	h.m.Tick(ctx)
	if checks() != 2 {
		t.Fatalf("after Run now: %d checks", checks())
	}
	h.now = h.now.Add(2 * time.Hour)
	h.m.Tick(ctx)
	if checks() != 3 {
		t.Fatalf("after the 2h interval: %d checks", checks())
	}
	h.cfg.Jobs.RunnerUpdateCheck.Enabled = false
	h.now = h.now.Add(48 * time.Hour)
	h.m.Tick(ctx)
	if checks() != 3 {
		t.Fatalf("checked while off: %d checks", checks())
	}
	if err := h.m.Jobs.RunNow(config.JobRunnerUpdateCheck); !errors.Is(err, jobs.ErrOff) {
		t.Fatalf("Run now while off: %v", err)
	}
}

func TestPruneOffStillMeasuresTheDisk(t *testing.T) {
	h := newHarness(t)
	h.cfg.Jobs.Prune.Enabled = false
	h.docker.mu.Lock()
	h.docker.usage = []int{99}
	h.docker.mu.Unlock()
	h.now = h.now.Add(48 * time.Hour)
	h.m.ticks = 0 // the next tick is tick 1, when the disk is measured
	h.m.Tick(context.Background())
	h.m.Wait() // a Docker prune would run in the background
	if pct := h.m.Status().DiskPct; pct != 99 {
		t.Fatalf("disk %d%%", pct)
	}
	if got := h.docker.pruneList(); got != "" {
		t.Fatalf("pruned with the task off: %s", got)
	}
	if j := jobStatus(t, h.m.Jobs, config.JobPrune); j.LastStart != nil {
		t.Fatalf("the retention prune ran with the task off: %+v", j)
	}
}

func TestRetentionPruneFollowsItsInterval(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.cfg.Jobs.Prune.Every = config.Duration(12 * time.Hour)
	h.m.ticks = 4 // keeps the disk check, on every tenth tick, out of the way
	h.m.Tick(ctx)
	if j := jobStatus(t, h.m.Jobs, config.JobPrune); j.LastStart != nil {
		t.Fatal("pruned before the interval")
	}
	h.now = h.now.Add(12 * time.Hour)
	h.m.Tick(ctx)
	if j := jobStatus(t, h.m.Jobs, config.JobPrune); j.LastFinish == nil || !j.LastFinish.Equal(h.now) || j.LastError != "" {
		t.Fatalf("after the interval %+v", j)
	}
	if err := h.m.Jobs.RunNow(config.JobPrune); err != nil {
		t.Fatal(err)
	}
	h.docker.mu.Lock()
	h.docker.usage = []int{42}
	h.docker.mu.Unlock()
	h.now = h.now.Add(time.Minute)
	h.m.Tick(ctx) // tick 7: not a disk-check tick
	if j := jobStatus(t, h.m.Jobs, config.JobPrune); j.Pending || j.LastFinish == nil || !j.LastFinish.Equal(h.now) {
		t.Fatalf("after Run now %+v", j)
	}
	if pct := h.m.Status().DiskPct; pct != 42 {
		t.Fatalf("a retention pass did not measure the disk: %d%%", pct)
	}
}
