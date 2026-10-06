package storage

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/toolchain"
)

func queueService(t *testing.T, tools *fakeTools) (*Service, *atomic.Int32) {
	t.Helper()
	var busy atomic.Int32
	s := newService(t, tools, &fakeDisk{})
	s.Busy = func() int { return int(busy.Load()) }
	startService(t, s)
	return s, &busy
}

func nodeTools() (*fakeTools, *fakeInstaller) {
	node := newInstaller(map[string]string{"22": "22.11.0", "24": "24.9.0"})
	return &fakeTools{tools: map[string]*fakeInstaller{"node": node}}, node
}

func recvVersion(t *testing.T, ch <-chan string) string {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for an install to start")
		return ""
	}
}

func recentOutcomes(s *Service) []string {
	var out []string
	for _, o := range s.Snapshot().Operations.Recent {
		out = append(out, o.Kind+" "+o.Target+" "+o.Outcome)
	}
	return out
}

func eventCount(s *Service, sub string) int {
	n := 0
	for _, e := range s.Events.After(0) {
		if strings.Contains(e.Msg, sub) {
			n++
		}
	}
	return n
}

func TestQueueRunsOperationsInOrderOneAtATime(t *testing.T) {
	tools, node := nodeTools()
	node.gate = make(chan struct{})
	node.started = make(chan string, 4)
	s, _ := queueService(t, tools)
	if err := s.Install("node", "22"); err != nil {
		t.Fatal(err)
	}
	if err := s.Install("node", "24"); err != nil {
		t.Fatal(err)
	}
	if v := recvVersion(t, node.started); v != "22.11.0" {
		t.Fatalf("first install %s", v)
	}
	waitFor(t, "the install's progress", func() bool {
		c := s.Snapshot().Operations.Current
		return c != nil && c.Progress == "downloading"
	})
	st := s.Snapshot()
	if st.Operations.Current.Target != "node 22" || st.Operations.Current.Kind != "install" || st.Operations.Queued != 1 {
		t.Fatalf("operations %+v", st.Operations)
	}
	node.gate <- struct{}{}
	if v := recvVersion(t, node.started); v != "24.9.0" {
		t.Fatalf("second install %s", v)
	}
	node.gate <- struct{}{}
	waitFor(t, "both installs", func() bool { return len(s.Snapshot().Operations.Recent) == 2 })
	if got := recentOutcomes(s); !slices.Equal(got, []string{"install node 24 ok", "install node 22 ok"}) {
		t.Fatalf("recent %q", got)
	}
	if got := node.installList(); !slices.Equal(got, []string{"22.11.0", "24.9.0"}) {
		t.Fatalf("installs %q", got)
	}
	r := s.Snapshot().Operations.Recent[0]
	if r.Message != "installed node 24.9.0" || r.FinishedAt == nil || r.Progress != "" || r.ID == "" {
		t.Fatalf("recent[0] %+v", r)
	}
}

func TestInstallOutcomes(t *testing.T) {
	tools, node := nodeTools()
	node.have["22.11.0"] = true
	s, _ := queueService(t, tools)
	if err := s.Install("node", "22"); err != nil {
		t.Fatal(err)
	}
	if err := s.Install("node", "99"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "two operations", func() bool { return len(s.Snapshot().Operations.Recent) == 2 })
	recent := s.Snapshot().Operations.Recent
	if recent[1].Outcome != "skipped" || recent[1].Message != "node 22.11.0 is already installed" {
		t.Fatalf("installed version %+v", recent[1])
	}
	if recent[0].Outcome != "failed" || !strings.Contains(recent[0].Message, `no release matches "99"`) {
		t.Fatalf("unresolvable version %+v", recent[0])
	}
}

func TestRequestsThatAreRefusedAtOnce(t *testing.T) {
	tools, _ := nodeTools()
	s, _ := queueService(t, tools)
	if err := s.Install("ruby", "3.3"); !errors.Is(err, toolchain.ErrUnknownTool) {
		t.Errorf("unknown tool: %v", err)
	}
	if err := s.Install("node", " "); !errors.Is(err, ErrMissingVersion) {
		t.Errorf("missing version: %v", err)
	}
	if err := s.InstallPreset("everything"); !errors.Is(err, ErrUnknownPreset) {
		t.Errorf("unknown preset: %v", err)
	}
	if err := s.Remove("ruby", "3.3.0"); !errors.Is(err, toolchain.ErrUnknownTool) {
		t.Errorf("remove unknown tool: %v", err)
	}
	if err := s.Remove("node", "20.0.0"); !errors.Is(err, toolchain.ErrNotInstalled) {
		t.Errorf("remove a version not installed: %v", err)
	}
	if err := s.Clear("bogus"); !errors.Is(err, ErrUnknownCache) {
		t.Errorf("unknown cache: %v", err)
	}
	if err := s.Clear("cargo"); !errors.Is(err, ErrNotPresent) {
		t.Errorf("absent cache: %v", err)
	}
	if st := s.Snapshot(); st.Operations.Queued != 0 || st.Operations.Current != nil {
		t.Fatalf("a refused request was queued: %+v", st.Operations)
	}
}

func TestPopularPresetQueuesNineInstallsInOrder(t *testing.T) {
	tools := &fakeTools{tools: map[string]*fakeInstaller{}}
	for _, e := range toolchain.Popular {
		i := tools.tools[e.Tool]
		if i == nil {
			i = newInstaller(map[string]string{})
			tools.tools[e.Tool] = i
		}
		i.resolve[e.Spec] = e.Spec + ".0"
	}
	s, _ := queueService(t, tools)
	if err := s.InstallPreset("popular"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "nine installs", func() bool { return len(s.Snapshot().Operations.Recent) == len(toolchain.Popular) })
	recent := s.Snapshot().Operations.Recent
	for i, e := range toolchain.Popular {
		r := recent[len(recent)-1-i]
		if r.Target != e.Tool+" "+e.Spec || r.Outcome != "ok" {
			t.Errorf("install %d: %+v", i, r)
		}
	}
}

func TestRemoveAndClearCheckBusyWhenTheyRun(t *testing.T) {
	tools, node := nodeTools()
	tools.installed = []toolchain.Installed{{Tool: "node", Version: "20.18.0", Arch: "x64"}}
	node.gate = make(chan struct{})
	node.started = make(chan string, 1)
	// Windows refuses to rename a directory another handle has open, so park
	// the measurer, which walks the caches, in Docker's df throughout.
	disk := &fakeDisk{entered: make(chan struct{}, 10), gate: make(chan struct{})}
	var busy atomic.Int32
	s := newService(t, tools, disk)
	s.Busy = func() int { return int(busy.Load()) }
	startService(t, s)
	recvOrFail(t, disk.entered, "the first measurement")
	pkg := filepath.Join(s.Home, ".nuget", "packages", "pkg", "a.nupkg")
	writeFile(t, pkg, "x")
	if err := s.Install("node", "22"); err != nil {
		t.Fatal(err)
	}
	recvVersion(t, node.started)
	if err := s.Remove("node", "20.18.0"); err != nil {
		t.Fatal(err)
	}
	if err := s.Clear("nuget"); err != nil {
		t.Fatal(err)
	}
	busy.Store(2)
	node.gate <- struct{}{}
	waitFor(t, "three operations", func() bool { return len(s.Snapshot().Operations.Recent) == 3 })
	for _, r := range s.Snapshot().Operations.Recent[:2] {
		if r.Outcome != "refused" || r.Message != "refused: 2 jobs running" {
			t.Fatalf("while busy: %+v", r)
		}
	}
	if got := node.removedList(); len(got) != 0 {
		t.Fatalf("a refused removal ran: %q", got)
	}
	if _, err := os.Stat(pkg); err != nil {
		t.Fatalf("a refused clear deleted files: %v", err)
	}
	busy.Store(0)
	if err := s.Remove("node", "20.18.0"); err != nil {
		t.Fatal(err)
	}
	if err := s.Clear("nuget"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "five operations", func() bool { return len(s.Snapshot().Operations.Recent) == 5 })
	if got := recentOutcomes(s)[:2]; !slices.Equal(got, []string{"clear nuget ok", "remove node 20.18.0 ok"}) {
		t.Fatalf("once idle: %q", got)
	}
	if got := node.removedList(); !slices.Equal(got, []string{"20.18.0"}) {
		t.Fatalf("removed %q", got)
	}
	if got := entries(t, filepath.Join(s.Home, ".nuget", "packages")); len(got) != 0 {
		t.Fatalf("not cleared: %v", got)
	}
}

func TestCloseInterruptsTheRunningOperationAndDropsTheRest(t *testing.T) {
	tools, node := nodeTools()
	node.gate = make(chan struct{})
	node.started = make(chan string, 1)
	s := newService(t, tools, &fakeDisk{})
	s.Start()
	for _, spec := range []string{"22", "24", "24"} {
		if err := s.Install("node", spec); err != nil {
			t.Fatal(err)
		}
	}
	recvVersion(t, node.started)
	s.Close()
	waited := make(chan struct{})
	go func() {
		s.Wait()
		close(waited)
	}()
	recvOrFail(t, waited, "Wait after Close")
	st := s.Snapshot()
	if st.Operations.Queued != 0 || st.Operations.Current != nil || len(st.Operations.Recent) != 1 || st.Operations.Recent[0].Outcome != "interrupted" {
		t.Fatalf("operations %+v", st.Operations)
	}
	if n := eventCount(s, "dropped: ghr is shutting down"); n != 2 {
		t.Fatalf("%d dropped events", n)
	}
	if err := s.Install("node", "22"); !errors.Is(err, ErrClosed) {
		t.Fatalf("install after Close: %v", err)
	}
}

func TestRecentKeepsTheLastTen(t *testing.T) {
	tools, node := nodeTools()
	node.have["22.11.0"] = true
	s, _ := queueService(t, tools)
	for i := 0; i < 12; i++ {
		if err := s.Install("node", "22"); err != nil {
			t.Fatal(err)
		}
	}
	waitFor(t, "twelve operations", func() bool { return eventCount(s, "install node 22 skipped") == 12 })
	if n := len(s.Snapshot().Operations.Recent); n != 10 {
		t.Fatalf("%d recent operations", n)
	}
}

func TestOperationsReportEventsAndTriggerAMeasurement(t *testing.T) {
	tools, _ := nodeTools()
	disk := &fakeDisk{}
	s := newService(t, tools, disk)
	startService(t, s)
	waitFor(t, "the first measurement", func() bool { return s.Snapshot().MeasuredAt != nil && disk.callCount() == 1 })
	if err := s.Install("node", "22"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the measurement after the install", func() bool { return disk.callCount() >= 2 })
	if eventCount(s, "install node 22 started") != 1 || eventCount(s, "install node 22 ok: installed node 22.11.0") != 1 {
		t.Fatalf("events %+v", s.Events.After(0))
	}
}

func TestAvailableConvertsChoices(t *testing.T) {
	tools, _ := nodeTools()
	s, _ := queueService(t, tools)
	cs, err := s.Available(t.Context(), "node")
	if err != nil || len(cs) != 2 || cs[0].Spec != "22" || cs[0].Version != "22.11.0" {
		t.Fatalf("choices %+v, %v", cs, err)
	}
	if _, err := s.Available(t.Context(), "ruby"); !errors.Is(err, toolchain.ErrUnknownTool) {
		t.Fatalf("unknown tool: %v", err)
	}
}
