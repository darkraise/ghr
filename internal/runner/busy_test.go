package runner

import (
	"context"
	"os"
	"testing"

	"github.com/darkraise/ghr/internal/sched"
)

var _ Disk = (*fakeDocker)(nil)

// addInstance records an instance in state, with its directory, as a spawn would.
func addInstance(t *testing.T, h *harness, id string, state sched.State) {
	t.Helper()
	h.m.mu.Lock()
	h.m.insts[id] = &instance{Meta: Meta{ID: id, Repo: "darkcloud"}, State: state, StateSince: h.now}
	h.m.mu.Unlock()
	if err := os.MkdirAll(h.m.instanceDir(id), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestHoldCountsAHookRecordedJob(t *testing.T) {
	h := newHarness(t)
	addInstance(t, h, "aaaaaa", sched.Idle)
	addInstance(t, h, "cccccc", sched.Busy)
	if _, err := h.m.HoldForMaintenance(context.Background()); err == nil || err.Error() != "refused: 1 jobs running" {
		t.Fatalf("before the hook: %v", err)
	}
	h.writeJob(t, "aaaaaa", 55)
	if _, err := h.m.HoldForMaintenance(context.Background()); err == nil || err.Error() != "refused: 2 jobs running" {
		t.Fatalf("after the hook: %v", err)
	}
	h.m.mu.Lock()
	confirmed := h.m.insts["aaaaaa"].JobConfirmed
	h.m.mu.Unlock()
	if confirmed {
		t.Fatal("the hold must not wait for GitHub to confirm the job")
	}
}

func TestHoldIgnoresCleaningInstances(t *testing.T) {
	h := newHarness(t)
	addInstance(t, h, "aaaaaa", sched.Cleaning)
	h.writeJob(t, "aaaaaa", 55)
	release, err := h.m.HoldForMaintenance(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	release()
}
