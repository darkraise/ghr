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
