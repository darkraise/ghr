package runner

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// holdOn blocks the named fake Docker method until release is closed or the
// call's context ends, signalling entered when it starts.
func holdOn(h *harness, method string) (entered, release chan struct{}) {
	entered, release = make(chan struct{}), make(chan struct{})
	h.docker.hold = func(ctx context.Context, m string) error {
		if m != method {
			return nil
		}
		close(entered)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return entered, release
}

func waitOrFail(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func TestStartPruneRunsForcedSequence(t *testing.T) {
	h := newHarness(t)
	h.docker.usage = []int{40} // below the high-water mark: a forced prune runs anyway
	if err := h.m.StartPrune(); err != nil {
		t.Fatal(err)
	}
	h.m.Wait()
	if got := h.docker.pruneList(); got != "keep="+h.cfg.BuildCacheKeep+",images" {
		t.Fatalf("prunes %s", got)
	}
	ms := h.m.Maintenance()
	if ms.Running || ms.LastOutcome != "ok" || ms.LastStarted == nil || ms.LastFinished == nil {
		t.Fatalf("maintenance %+v", ms)
	}
	if st := h.m.Status(); st.Maintenance.LastOutcome != "ok" {
		t.Fatalf("status maintenance %+v", st.Maintenance)
	}
	txt := h.eventText()
	at := -1
	for _, want := range []string{
		"prune: build cache to " + h.cfg.BuildCacheKeep + " freed 5GB",
		"prune: dangling images freed 200MB",
		"prune: history and logs past retention removed",
		"prune: disk 40% used",
		"prune finished",
	} {
		i := strings.Index(txt, want)
		if i <= at {
			t.Fatalf("%q missing or out of order in events:\n%s", want, txt)
		}
		at = i
	}
}

// A manual prune holds the reservation: a tick that would prune for disk
// space and for retention does neither until it ends.
func TestTickSkipsPruningDuringManualPrune(t *testing.T) {
	h := newHarness(t)
	h.m.mu.Lock()
	h.m.lastPrune = h.now.Add(-48 * time.Hour)
	h.m.mu.Unlock()
	entered, release := holdOn(h, "PruneBuildCacheTo")
	if err := h.m.StartPrune(); err != nil {
		t.Fatal(err)
	}
	waitOrFail(t, entered, "the manual prune")
	if err := h.m.StartPrune(); !errors.Is(err, ErrPruneRunning) {
		t.Fatalf("second StartPrune: %v", err)
	}
	h.docker.mu.Lock()
	h.docker.usage = []int{99} // above high-water: an unreserved disk check would prune
	h.docker.mu.Unlock()
	h.m.ticks = 0 // the next tick is tick 1, when checkDisk runs
	h.m.Tick(context.Background())
	if got := h.docker.pruneList(); got != "keep="+h.cfg.BuildCacheKeep {
		t.Fatalf("automatic pruning ran during a manual prune: %s", got)
	}
	h.m.mu.Lock()
	last := h.m.lastPrune
	h.m.mu.Unlock()
	if !last.Equal(h.now.Add(-48 * time.Hour)) {
		t.Fatal("the retention prune ran during a manual prune")
	}
	close(release)
	h.m.Wait()
	if ms := h.m.Maintenance(); ms.Running || ms.LastOutcome != "ok" {
		t.Fatalf("maintenance %+v", ms)
	}
}

// An automatic prune holds the reservation: StartPrune is refused until the
// tick ends, then accepted.
func TestStartPruneRefusedDuringAutomaticPrune(t *testing.T) {
	h := newHarness(t)
	h.docker.usage = []int{99, 99, 50}
	entered, release := holdOn(h, "PruneBuildCacheOlderThan")
	h.m.ticks = 0
	ticked := make(chan struct{})
	go func() {
		h.m.Tick(context.Background())
		close(ticked)
	}()
	waitOrFail(t, entered, "the automatic prune")
	if err := h.m.StartPrune(); !errors.Is(err, ErrPruneRunning) {
		t.Fatalf("StartPrune during an automatic prune: %v", err)
	}
	if ms := h.m.Maintenance(); ms.Running || ms.LastStarted != nil {
		t.Fatalf("a refused StartPrune changed the state: %+v", ms)
	}
	close(release)
	waitOrFail(t, ticked, "the tick")
	if err := h.m.StartPrune(); err != nil {
		t.Fatalf("StartPrune after the tick: %v", err)
	}
	h.m.Wait()
}

func TestPruneErrors(t *testing.T) {
	h := newHarness(t)
	h.docker.errs["PruneDanglingImages"] = errors.New("boom")
	if err := h.m.StartPrune(); err != nil {
		t.Fatal(err)
	}
	h.m.Wait()
	ms := h.m.Maintenance()
	txt := h.eventText()
	if ms.LastOutcome != "errors" || !strings.Contains(txt, "prune: dangling images failed: boom") || !strings.Contains(txt, "prune finished with errors") {
		t.Fatalf("maintenance %+v events:\n%s", ms, txt)
	}
}

// Close cancels a Docker call already in flight; the prune then ends as
// interrupted, runs no further step, and Wait returns.
func TestCloseInterruptsRunningPrune(t *testing.T) {
	h := newHarness(t)
	entered, _ := holdOn(h, "PruneDanglingImages")
	if err := h.m.StartPrune(); err != nil {
		t.Fatal(err)
	}
	waitOrFail(t, entered, "the dangling-image prune")
	h.m.Close()
	waited := make(chan struct{})
	go func() {
		h.m.Wait()
		close(waited)
	}()
	waitOrFail(t, waited, "Wait after Close")
	ms := h.m.Maintenance()
	txt := h.eventText()
	if ms.Running || ms.LastOutcome != "interrupted" || !strings.Contains(txt, "prune interrupted by shutdown") {
		t.Fatalf("maintenance %+v events:\n%s", ms, txt)
	}
	if strings.Contains(txt, "history and logs past retention removed") || strings.Contains(txt, "prune: disk") {
		t.Fatalf("steps ran after the interruption:\n%s", txt)
	}
}
