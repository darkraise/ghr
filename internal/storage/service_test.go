package storage

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/events"
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
	if n := tools.cleanCount(); n != 1 {
		t.Fatalf("CleanTmp ran %d times", n)
	}
	if _, err := os.Lstat(left); !os.IsNotExist(err) {
		t.Fatalf("an interrupted clear survived start: %v", err)
	}
	st := s.Snapshot()
	if len(st.PackageCaches) != len(Caches) || len(st.Docker.Rows) != 1 || st.Toolchains == nil || st.OtherToolCache == nil ||
		st.Docker.BuildCacheTypes == nil || st.Operations.Recent == nil || st.Operations.Current != nil || st.Measuring {
		t.Fatalf("snapshot %+v", st)
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
