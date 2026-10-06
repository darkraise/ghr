package runner

import (
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

func TestBusyCountCountsAHookRecordedJob(t *testing.T) {
	h := newHarness(t)
	addInstance(t, h, "aaaaaa", sched.Idle)
	addInstance(t, h, "bbbbbb", sched.Idle)
	addInstance(t, h, "cccccc", sched.Busy)
	if n := h.m.BusyCount(); n != 1 {
		t.Fatalf("before the hook: %d busy", n)
	}
	h.writeJob(t, "aaaaaa", 55)
	if n := h.m.BusyCount(); n != 2 {
		t.Fatalf("after the hook: %d busy", n)
	}
	h.m.mu.Lock()
	confirmed := h.m.insts["aaaaaa"].JobConfirmed
	h.m.mu.Unlock()
	if confirmed {
		t.Fatal("BusyCount must not wait for GitHub to confirm the job")
	}
}

func TestBusyCountIgnoresCleaningInstances(t *testing.T) {
	h := newHarness(t)
	addInstance(t, h, "aaaaaa", sched.Cleaning)
	h.writeJob(t, "aaaaaa", 55)
	if n := h.m.BusyCount(); n != 0 {
		t.Fatalf("%d busy", n)
	}
}
