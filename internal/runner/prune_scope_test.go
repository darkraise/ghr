package runner

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/sched"
)

func countPruneDone(h *harness) *atomic.Int32 {
	var n atomic.Int32
	h.m.PruneDone = func() { n.Add(1) }
	return &n
}

func stepNames(lp *model.LastPrune) string {
	var out []string
	for _, s := range lp.Steps {
		out = append(out, s.Name)
	}
	return strings.Join(out, ",")
}

func TestPruneScopesRunOnlyTheirSteps(t *testing.T) {
	for _, tc := range []struct{ scope, prunes, steps string }{
		{ScopeStandard, "keep=K,images", "build cache to K,dangling images,history and logs past retention"},
		{ScopeBuildCacheKeep, "keep=K", "build cache to K"},
		{ScopeBuildCacheAll, "all", "all build cache"},
		{ScopeDanglingImages, "images", "dangling images"},
		{ScopeUnusedVolumes, "volumes", "unused volumes"},
	} {
		h := newHarness(t)
		keep := h.cfg.BuildCacheKeep
		done := countPruneDone(h)
		if err := h.m.StartPruneScope(tc.scope); err != nil {
			t.Fatalf("%s: %v", tc.scope, err)
		}
		h.m.Wait()
		if got, want := h.docker.pruneList(), strings.ReplaceAll(tc.prunes, "K", keep); got != want {
			t.Errorf("%s: prunes %q, want %q", tc.scope, got, want)
		}
		lp := h.m.LastPrune()
		if lp == nil {
			t.Fatalf("%s: no last prune", tc.scope)
		}
		if lp.Trigger != "manual" || lp.Scope != tc.scope || lp.Outcome != "ok" || lp.FinishedAt == nil || !lp.StartedAt.Equal(h.now) {
			t.Errorf("%s: last prune %+v", tc.scope, lp)
		}
		if got, want := stepNames(lp), strings.ReplaceAll(tc.steps, "K", keep); got != want {
			t.Errorf("%s: steps %q, want %q", tc.scope, got, want)
		}
		if n := done.Load(); n != 1 {
			t.Errorf("%s: PruneDone ran %d times", tc.scope, n)
		}
	}
}

func TestPruneStepsRecordBytesFreed(t *testing.T) {
	h := newHarness(t)
	if err := h.m.StartPruneScope(ScopeStandard); err != nil {
		t.Fatal(err)
	}
	h.m.Wait()
	want := []model.PruneStep{
		{Name: "build cache to " + h.cfg.BuildCacheKeep, Freed: 5000000000},
		{Name: "dangling images", Freed: 200000000},
		{Name: "history and logs past retention"},
	}
	if got := h.m.LastPrune().Steps; !reflect.DeepEqual(got, want) {
		t.Fatalf("steps %+v", got)
	}
}

func TestAFailedPruneStepKeepsItsError(t *testing.T) {
	h := newHarness(t)
	h.docker.setErr("PruneUnusedVolumes", errors.New("boom"))
	if err := h.m.StartPruneScope(ScopeUnusedVolumes); err != nil {
		t.Fatal(err)
	}
	h.m.Wait()
	lp := h.m.LastPrune()
	if lp.Outcome != "errors" || !reflect.DeepEqual(lp.Steps, []model.PruneStep{{Name: "unused volumes", Error: "boom"}}) {
		t.Fatalf("last prune %+v", lp)
	}
}

func TestUnknownPruneScope(t *testing.T) {
	h := newHarness(t)
	if err := h.m.StartPruneScope("everything"); !errors.Is(err, ErrUnknownScope) {
		t.Fatalf("err %v", err)
	}
	if h.m.LastPrune() != nil || h.docker.pruneList() != "" {
		t.Fatal("an unknown scope ran or recorded a prune")
	}
}

func TestUnusedVolumesRefusedWhileARunnerIsBusy(t *testing.T) {
	h := newHarness(t)
	addInstance(t, h, "aaaaaa", sched.Idle)
	h.writeJob(t, "aaaaaa", 55)
	err := h.m.StartPruneScope(ScopeUnusedVolumes)
	var busy BusyError
	if !errors.As(err, &busy) || busy.N != 1 || err.Error() != "refused: 1 jobs running" {
		t.Fatalf("err %v", err)
	}
	h.m.Wait()
	if h.docker.pruneList() != "" || h.m.LastPrune() != nil {
		t.Fatal("a refused prune ran or recorded")
	}
	if err := h.m.StartPruneScope(ScopeDanglingImages); err != nil {
		t.Fatalf("other scopes run while busy: %v", err)
	}
	h.m.Wait()
	h.m.Close()
	if err := h.m.StartPruneScope(ScopeUnusedVolumes); !errors.Is(err, ErrClosed) {
		t.Fatalf("after Close, with a busy runner: %v", err)
	}
}

func TestCheckDiskCutShortByShutdownIsInterrupted(t *testing.T) {
	h := newHarness(t)
	h.docker.usage = []int{99}
	entered, _ := holdOn(t, h, "PruneBuildCacheOlderThan")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		h.m.checkDisk(ctx, h.cfg)
		close(done)
	}()
	waitOrFail(t, entered, "the automatic prune")
	cancel()
	waitOrFail(t, done, "checkDisk to return")
	if lp := h.m.LastPrune(); lp == nil || lp.Outcome != "interrupted" {
		t.Fatalf("last prune %+v", lp)
	}
}

func TestPruneDoneFiresWhenAPruneIsInterrupted(t *testing.T) {
	h := newHarness(t)
	done := countPruneDone(h)
	entered, _ := holdOn(t, h, "PruneAllBuildCache")
	if err := h.m.StartPruneScope(ScopeBuildCacheAll); err != nil {
		t.Fatal(err)
	}
	waitOrFail(t, entered, "the build cache prune")
	h.m.Close()
	h.m.Wait()
	if lp := h.m.LastPrune(); lp.Outcome != "interrupted" || done.Load() != 1 {
		t.Fatalf("last prune %+v, PruneDone %d", lp, done.Load())
	}
}

func TestCheckDiskRecordsAnAutomaticPrune(t *testing.T) {
	h := newHarness(t)
	done := countPruneDone(h)
	h.docker.usage = []int{99, 99, 50}
	h.m.checkDisk(context.Background(), h.cfg)
	lp := h.m.LastPrune()
	if lp == nil || lp.Trigger != "auto" || lp.Scope != "auto" || lp.Outcome != "ok" || lp.FinishedAt == nil {
		t.Fatalf("last prune %+v", lp)
	}
	want := []model.PruneStep{
		{Name: "build cache older than 72h", Freed: 1000000000},
		{Name: "build cache to " + h.cfg.BuildCacheKeep, Freed: 5000000000},
		{Name: "dangling images", Freed: 200000000},
	}
	if !reflect.DeepEqual(lp.Steps, want) {
		t.Fatalf("steps %+v", lp.Steps)
	}
	if n := done.Load(); n != 1 {
		t.Fatalf("PruneDone ran %d times", n)
	}
}

func TestCheckDiskUnderTheHighWaterMarkRecordsNothing(t *testing.T) {
	h := newHarness(t)
	done := countPruneDone(h)
	h.docker.usage = []int{40}
	h.m.checkDisk(context.Background(), h.cfg)
	if h.m.LastPrune() != nil || done.Load() != 0 {
		t.Fatalf("last prune %+v, PruneDone %d", h.m.LastPrune(), done.Load())
	}
}

func TestCheckDiskRecordsFailedReadings(t *testing.T) {
	h := newHarness(t)
	h.docker.usage = []int{99, -1}
	h.m.checkDisk(context.Background(), h.cfg)
	lp := h.m.LastPrune()
	if lp.Outcome != "errors" || stepNames(lp) != "build cache older than 72h,disk usage,dangling images,disk usage" {
		t.Fatalf("last prune %+v", lp)
	}
	if lp.Steps[1].Error != "df failed" {
		t.Fatalf("step %+v", lp.Steps[1])
	}
}
