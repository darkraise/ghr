package storage

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/events"
	"github.com/darkraise/ghr/internal/jobs"
	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/system"
)

// newService builds an unstarted service over the fakes, with a fresh home.
func newService(t *testing.T, tools *fakeTools, disk *fakeDisk) *Service {
	t.Helper()
	if tools.root == "" {
		tools.root = t.TempDir()
	}
	return &Service{Tools: tools, Docker: disk, Home: t.TempDir(), Events: events.New()}
}

func startService(t *testing.T, s *Service) {
	t.Helper()
	s.Start()
	t.Cleanup(func() {
		s.Close()
		s.Wait()
	})
	// Until the start-up reconcile returns it may clear an error a test sets
	// or queue an install of a name a test lists.
	recvOrFail(t, s.Reconciled(), "the start-up reconcile")
}

func recvOrFail(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func TestStartCleansUpAndMeasuresAtOnce(t *testing.T) {
	tools := &fakeTools{}
	s := newService(t, tools, &fakeDisk{rows: []system.DiskRow{{Type: "Images", Count: 1}}})
	left := filepath.Join(s.Home, ".nuget", ".packages"+clearingTag+"op1")
	writeFile(t, filepath.Join(left, "x"), "x")
	startService(t, s)
	waitFor(t, "the first measurement", func() bool { return s.Snapshot().MeasuredAt != nil })
	waitFor(t, "the clean-up", func() bool {
		_, err := os.Lstat(left)
		return tools.cleanCount() == 1 && os.IsNotExist(err)
	})
	st := s.Snapshot()
	if len(st.PackageCaches) != len(Caches) || len(st.Docker.Rows) != 1 || st.Toolchains == nil || st.OtherToolCache == nil ||
		st.Docker.BuildCacheTypes == nil || st.Operations.Recent == nil || st.Operations.Current != nil || st.Measuring {
		t.Fatalf("snapshot %+v", st)
	}
}

func TestSnapshotReportsWhetherTheCachesShareTheDataRootFilesystem(t *testing.T) {
	s := newService(t, &fakeTools{}, &fakeDisk{})
	s.Docker = rootedDisk{fakeDisk: &fakeDisk{}, root: otherFilesystem(t)}
	startService(t, s)
	waitFor(t, "the first measurement", func() bool { return s.Snapshot().MeasuredAt != nil })
	if s.Snapshot().CachesOnDataRoot {
		t.Fatal("CachesOnDataRoot is true with the data root on another filesystem")
	}
}

func TestStartDoesNotWaitForTheCleanUpButTheQueueDoes(t *testing.T) {
	tools, node := nodeTools()
	tools.cleanGate = make(chan struct{})
	node.started = make(chan string, 1)
	s := newService(t, tools, &fakeDisk{})
	started := make(chan struct{})
	go func() {
		startService(t, s)
		close(started)
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		close(tools.cleanGate)
		t.Fatal("Start waited for the clean-up")
	}
	if err := s.Install("node", "22"); err != nil {
		t.Fatal(err)
	}
	select {
	case v := <-node.started:
		t.Fatalf("install %s ran before the clean-up", v)
	case <-time.After(100 * time.Millisecond):
	}
	close(tools.cleanGate)
	if v := recvVersion(t, node.started); v != "22.11.0" {
		t.Fatalf("install %s", v)
	}
}

func TestSnapshotBeforeTheFirstMeasurementHasEmptyLists(t *testing.T) {
	disk := &fakeDisk{entered: make(chan struct{}, 10), gate: make(chan struct{})}
	s := newService(t, &fakeTools{}, disk)
	startService(t, s)
	recvOrFail(t, disk.entered, "the first measurement")
	st := s.Snapshot()
	if st.MeasuredAt != nil || !st.Measuring || st.Toolchains == nil || st.PackageCaches == nil || st.Docker.Rows == nil {
		t.Fatalf("snapshot %+v", st)
	}
}

func TestRefreshIsRefusedDuringAMeasurement(t *testing.T) {
	disk := &fakeDisk{entered: make(chan struct{}, 10), gate: make(chan struct{})}
	s := newService(t, &fakeTools{}, disk)
	startService(t, s)
	recvOrFail(t, disk.entered, "the first measurement")
	if err := s.Refresh(); !errors.Is(err, ErrMeasuring) {
		t.Fatalf("refresh during a measurement: %v", err)
	}
	disk.gate <- struct{}{}
	waitFor(t, "the measurement to end", func() bool {
		st := s.Snapshot()
		return !st.Measuring && st.MeasuredAt != nil
	})
	if err := s.Refresh(); err != nil {
		t.Fatalf("refresh after it: %v", err)
	}
	recvOrFail(t, disk.entered, "the refreshed measurement")
}

func TestTriggersDuringAMeasurementCoalesce(t *testing.T) {
	disk := &fakeDisk{entered: make(chan struct{}, 10), gate: make(chan struct{})}
	s := newService(t, &fakeTools{}, disk)
	startService(t, s)
	recvOrFail(t, disk.entered, "the first measurement")
	s.Trigger()
	s.Trigger()
	s.Trigger()
	disk.gate <- struct{}{}
	recvOrFail(t, disk.entered, "the follow-up measurement")
	if n := len(s.trigger); n != 0 {
		t.Fatalf("%d more measurements queued", n)
	}
	if n := disk.callCount(); n != 2 {
		t.Fatalf("%d measurements", n)
	}
}

func TestReadersNeverSeeAPartialMeasurement(t *testing.T) {
	s := newService(t, &fakeTools{}, &fakeDisk{rows: []system.DiskRow{{Type: "Images"}}})
	startService(t, s)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 50; i++ {
			s.Trigger()
			time.Sleep(time.Millisecond)
		}
	}()
	for {
		select {
		case <-done:
			return
		default:
		}
		st := s.Snapshot()
		if st.MeasuredAt != nil && (len(st.PackageCaches) != len(Caches) || len(st.Docker.Rows) != 1) {
			t.Fatalf("partial snapshot %+v", st)
		}
	}
}

func TestCloseStopsTheService(t *testing.T) {
	s := newService(t, &fakeTools{}, &fakeDisk{})
	s.Start()
	s.Close()
	waited := make(chan struct{})
	go func() {
		s.Wait()
		close(waited)
	}()
	recvOrFail(t, waited, "Wait after Close")
	if err := s.Refresh(); !errors.Is(err, ErrClosed) {
		t.Fatalf("refresh after Close: %v", err)
	}
}

// jobsRig is a registry whose config the test changes under a lock, since
// the measure loop reads it from its own goroutine.
func jobsRig() (*jobs.Registry, func(fn func(c *config.Jobs))) {
	var mu sync.Mutex
	cfg := config.DefaultJobs()
	r := jobs.New(func() config.Jobs {
		mu.Lock()
		defer mu.Unlock()
		return cfg
	}, time.Now, nil)
	return r, func(fn func(c *config.Jobs)) {
		mu.Lock()
		fn(&cfg)
		mu.Unlock()
	}
}

func storageJob(t *testing.T, r *jobs.Registry) model.JobStatus {
	t.Helper()
	for _, j := range r.Snapshot() {
		if j.Name == config.JobStorageMeasure {
			return j
		}
	}
	t.Fatal("no storage_measure task")
	return model.JobStatus{}
}

func TestRunNowMeasuresAndAConfigChangeOnlyRearms(t *testing.T) {
	disk := &fakeDisk{}
	s := newService(t, &fakeTools{}, disk)
	reg, set := jobsRig()
	s.Jobs = reg
	startService(t, s)
	waitFor(t, "the first measurement", func() bool {
		j := storageJob(t, reg)
		return j.LastFinish != nil && !j.Running
	})
	first := *storageJob(t, reg).LastFinish
	if err := reg.RunNow(config.JobStorageMeasure); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the Run now measurement", func() bool {
		j := storageJob(t, reg)
		return j.LastFinish != nil && j.LastFinish.After(first) && !j.Running && !j.Pending
	})
	calls := disk.callCount()
	set(func(c *config.Jobs) { c.StorageMeasure.Every = config.Duration(time.Hour) })
	reg.Changed()
	time.Sleep(50 * time.Millisecond)
	if n := disk.callCount(); n != calls {
		t.Fatalf("a config change measured: %d calls, want %d", n, calls)
	}
	if j := storageJob(t, reg); j.Every != "1h0m0s" {
		t.Fatalf("task %+v", j)
	}
}

func TestATriggerMeasuresWhileTheTaskIsOff(t *testing.T) {
	disk := &fakeDisk{}
	s := newService(t, &fakeTools{}, disk)
	reg, set := jobsRig()
	set(func(c *config.Jobs) { c.StorageMeasure.Enabled = false })
	s.Jobs = reg
	startService(t, s)
	waitFor(t, "the measurement at start", func() bool { return s.Snapshot().MeasuredAt != nil && !s.Snapshot().Measuring })
	if err := reg.RunNow(config.JobStorageMeasure); !errors.Is(err, jobs.ErrOff) {
		t.Fatalf("Run now while off: %v", err)
	}
	calls := disk.callCount()
	if err := s.Refresh(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the refresh", func() bool { return disk.callCount() > calls })
}

func TestMeasureIntervalIsTheJobsDefault(t *testing.T) {
	if got := config.DefaultJobs().StorageMeasure.Every.D(); got != MeasureInterval {
		t.Fatalf("storage_measure defaults to %s, MeasureInterval is %s", got, MeasureInterval)
	}
}
