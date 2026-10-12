package runner

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/model"
)

// holdOn blocks the named fake Docker method until release is called or the
// call's context ends, signalling entered when it starts. Cleanup releases
// it too, so a test that fails while holding cannot strand the goroutine.
func holdOn(t *testing.T, h *harness, method string) (entered chan struct{}, release func()) {
	entered = make(chan struct{})
	released := make(chan struct{})
	var once sync.Once
	release = func() { once.Do(func() { close(released) }) }
	t.Cleanup(release)
	h.docker.hold = func(ctx context.Context, m string) error {
		if m != method {
			return nil
		}
		close(entered)
		select {
		case <-released:
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

func TestStartPruneRefusedAfterClose(t *testing.T) {
	h := newHarness(t)
	h.m.Close()
	if err := h.m.StartPrune(); !errors.Is(err, ErrClosed) {
		t.Fatalf("StartPrune after Close: %v", err)
	}
	h.m.Wait()
	if got := h.docker.pruneList(); got != "" {
		t.Fatalf("a refused prune still ran: %s", got)
	}
}

func TestStartPruneRunsForcedSequence(t *testing.T) {
	h := newHarness(t)
	h.docker.usage = []int{40} // below the high-water mark: a forced prune runs anyway
	// One entry past retention, so the retention step has something to report.
	h.m.History.Append(model.HistoryEntry{ID: "old", Repo: "darkmem", FinishedAt: h.now.Add(-40 * 24 * time.Hour)})
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
	entered, release := holdOn(t, h, "PruneBuildCacheTo")
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
	release()
	h.m.Wait()
	if ms := h.m.Maintenance(); ms.Running || ms.LastOutcome != "ok" {
		t.Fatalf("maintenance %+v", ms)
	}
}

// tickInBackground runs one tick and fails the test if it does not return:
// a tick must never wait for a Docker prune.
func tickInBackground(t *testing.T, h *harness) {
	t.Helper()
	ticked := make(chan struct{})
	go func() {
		h.m.Tick(context.Background())
		close(ticked)
	}()
	waitOrFail(t, ticked, "the tick to return while the prune runs")
}

// An automatic prune runs in the background and holds the reservation: the
// tick returns while it runs, StartPrune is refused until it ends, and a later
// disk check does not start a second one.
func TestAutomaticPruneRunsOffTheTick(t *testing.T) {
	h := newHarness(t)
	h.docker.usage = []int{99, 99, 50}
	entered, release := holdOn(t, h, "PruneBuildCacheOlderThan")
	h.m.ticks = 0 // the next tick is tick 1, a disk check
	tickInBackground(t, h)
	waitOrFail(t, entered, "the automatic prune")
	if err := h.m.StartPrune(); !errors.Is(err, ErrPruneRunning) {
		t.Fatalf("StartPrune during an automatic prune: %v", err)
	}
	if ms := h.m.Maintenance(); ms.Running || ms.LastStarted != nil {
		t.Fatalf("a refused StartPrune changed the state: %+v", ms)
	}
	h.m.ticks = 10 // the next tick is another disk check
	tickInBackground(t, h)
	if got := h.docker.pruneList(); got != "until=72h" {
		t.Fatalf("a second automatic prune started: %s", got)
	}
	release()
	h.m.Wait()
	if err := h.m.StartPrune(); err != nil {
		t.Fatalf("StartPrune after the automatic prune: %v", err)
	}
	h.m.Wait()
}

// A Docker daemon that does not answer must not hold the tick for the command
// timeout: the tick's own disk reading has a short bound. The bound is a real
// timer, shortened here; nothing sleeps.
func TestAHungDiskMeasurementDoesNotHoldTheTick(t *testing.T) {
	h := newHarness(t)
	orig := diskMeasureTimeout
	diskMeasureTimeout = 20 * time.Millisecond
	t.Cleanup(func() { diskMeasureTimeout = orig })
	holdOn(t, h, "DataRootUsage")
	h.m.ticks = 0
	tickInBackground(t, h)
	if !strings.Contains(h.eventText(), "warn  disk usage: context deadline exceeded") {
		t.Fatalf("events:\n%s", h.eventText())
	}
	if !h.m.reservePrune() {
		t.Fatal("the tick kept the prune reservation")
	}
	h.m.releasePrune()
}

func TestCloseInterruptsABackgroundAutomaticPrune(t *testing.T) {
	h := newHarness(t)
	h.docker.usage = []int{99}
	entered, _ := holdOn(t, h, "PruneBuildCacheOlderThan")
	h.m.ticks = 0
	tickInBackground(t, h)
	waitOrFail(t, entered, "the automatic prune")
	h.m.Close()
	h.m.Wait()
	if lp := h.m.LastPrune(); lp == nil || lp.Outcome != "interrupted" {
		t.Fatalf("last prune %+v", lp)
	}
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
	entered, _ := holdOn(t, h, "PruneDanglingImages")
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
