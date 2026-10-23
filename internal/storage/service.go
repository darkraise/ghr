package storage

import (
	"context"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/events"
	"github.com/darkraise/ghr/internal/jobs"
	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/packages"
)

var (
	ErrMeasuring = errors.New("a measurement is already running")
	ErrClosed    = errors.New("ghr is shutting down")
)

// MeasureInterval is the storage_measure task's default; config.DefaultJobs repeats it.
const MeasureInterval = 30 * time.Minute

// Service holds the snapshot GET /storage serves, runs the measurer, and
// runs toolchain installs, removals and cache clears one at a time.
type Service struct {
	Tools  Toolchains
	Docker Docker
	Home   string // the runner user's home, where the package caches live
	User   string // owns the directories a clear recreates; "" leaves them to the daemon
	// Hold keeps the runners off while a removal or clear runs and is
	// refused while a job runs; nil holds nothing.
	Hold   func(ctx context.Context) (release func(), err error)
	Events *events.Ring
	Now    func() time.Time // nil means time.Now
	NewID  func() string    // operation ids; nil makes random ones
	// Jobs schedules the periodic measurement; Start makes one with the
	// default schedule when the daemon sets none.
	Jobs *jobs.Registry
	// Apt runs the apt package operations; nil, or a host without apt,
	// makes every package request fail with packages.ErrUnavailable.
	Apt     packages.Manager
	Managed Managed
	// Base is what setup.sh installs on this host; ghr never changes it.
	Base       []string
	AptLists   string // apt's index directory, whose *_InRelease files date an index ghr did not refresh
	IndexStamp string // touched after each refresh ghr runs; "" keeps no stamp

	mu        sync.Mutex
	snap      measured
	measuring bool
	closed    bool
	queue     []*op
	current   *model.Operation
	recent    []model.Operation // newest first
	pkgErrs   map[string]string // the last install error of each package name

	pkgVersion atomic.Int64

	ctx     context.Context
	cancel  context.CancelFunc
	trigger chan struct{}
	wake    chan struct{}
	jobWake chan struct{}
	pkgWake chan struct{}
	// reconciled is closed once the start-up reconcile has returned.
	reconciled chan struct{}
	wg         sync.WaitGroup
}

// op is one queued operation; run returns its outcome and message.
type op struct {
	model.Operation
	run func(ctx context.Context, progress func(step string)) (outcome, message string)
	// quiet leaves a successful run out of recent, so a periodic task does
	// not push the other operations out.
	quiet bool
	// key, when set, names the work in place of Target, which may be cut
	// short, when enqueue looks for the same operation already waiting.
	key string
	// finished, when set, is closed once result holds the ended operation,
	// including one dropped at shutdown.
	finished chan struct{}
	result   model.Operation
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Start starts the measurer, which measures at once, the queue worker,
// which first deletes what an interrupted install or clear left behind, and
// the package_index loop, which first reconciles the managed packages. Call
// it once, before any other method.
func (s *Service) Start() {
	s.snap = emptyMeasured()
	s.recent = []model.Operation{}
	s.pkgErrs = map[string]string{}
	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.trigger = make(chan struct{}, 1)
	s.wake = make(chan struct{}, 1)
	if s.Jobs == nil {
		s.Jobs = jobs.New(config.DefaultJobs, s.now, nil)
	}
	s.jobWake = make(chan struct{}, 1)
	s.Jobs.Register(config.JobStorageMeasure, func() {
		select {
		case s.jobWake <- struct{}{}:
		default:
		}
	})
	s.pkgWake = make(chan struct{}, 1)
	s.reconciled = make(chan struct{})
	s.Jobs.Register(config.JobPackageIndex, func() {
		select {
		case s.pkgWake <- struct{}{}:
		default:
		}
	})
	s.trigger <- struct{}{}
	s.wg.Add(3)
	go func() {
		defer s.wg.Done()
		s.measureLoop()
	}()
	go func() {
		defer s.wg.Done()
		s.indexLoop()
	}()
	go func() {
		defer s.wg.Done()
		s.work()
	}()
}

// cleanUp runs on the worker before its first operation, so an install never
// races the .tmp clean and the API is not held up by a large interrupted clear.
func (s *Service) cleanUp() {
	if err := s.Tools.CleanTmp(); err != nil {
		s.Events.Add("warn", "", "storage: clearing the tool cache's .tmp: %v", err)
	}
	if err := sweepClearing(s.Home); err != nil {
		s.Events.Add("warn", "", "storage: removing interrupted cache clears: %v", err)
	}
}

// measureLoop measures on a trigger, which always measures, and when the
// storage_measure task is due: its timer fired, or Run now woke it. A wake
// from a config change finds the task not due and only resets the timer.
func (s *Service) measureLoop() {
	until := func() time.Duration { return s.Jobs.Until(config.JobStorageMeasure, s.now()) }
	timer := time.NewTimer(until())
	defer timer.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.trigger:
		case <-s.jobWake:
			if !s.Jobs.Due(config.JobStorageMeasure, s.now()) {
				timer.Reset(until())
				continue
			}
		case <-timer.C:
			if !s.Jobs.Due(config.JobStorageMeasure, s.now()) {
				timer.Reset(until())
				continue
			}
		}
		s.Jobs.Started(config.JobStorageMeasure, s.now())
		s.mu.Lock()
		s.measuring = true
		s.mu.Unlock()
		m := measure(s.ctx, s.Tools, s.Docker, s.Home, s.now())
		s.mu.Lock()
		if s.ctx.Err() == nil {
			s.snap = m
		}
		s.measuring = false
		s.mu.Unlock()
		s.Jobs.Finished(config.JobStorageMeasure, s.now(), nil, nil)
		timer.Reset(until())
	}
}

// Trigger asks for a measurement. Triggers that arrive during one coalesce
// into a single measurement after it.
func (s *Service) Trigger() {
	select {
	case s.trigger <- struct{}{}:
	default:
	}
}

// Refresh asks for a measurement now; ErrMeasuring while one runs.
func (s *Service) Refresh() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case s.closed:
		return ErrClosed
	case s.measuring:
		return ErrMeasuring
	}
	s.Trigger()
	return nil
}

// Snapshot is the last complete measurement and the operation queue's
// state; it never walks a directory.
func (s *Service) Snapshot() model.Storage {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := model.Storage{
		Toolchains:     slices.Clone(s.snap.toolchains),
		OtherToolCache: slices.Clone(s.snap.other),
		PackageCaches:  slices.Clone(s.snap.caches),
		Docker: model.DockerDisk{
			Rows:            slices.Clone(s.snap.docker),
			BuildCacheTypes: slices.Clone(s.snap.cacheTypes),
		},
		CachesOnDataRoot: s.snap.cachesOnDataRoot,
		Measuring:        s.measuring,
		MeasureError:     s.snap.err,
		Operations:       model.Operations{Queued: len(s.queue), Recent: slices.Clone(s.recent)},
	}
	if !s.snap.at.IsZero() {
		at := s.snap.at
		st.MeasuredAt = &at
	}
	if s.current != nil {
		cur := *s.current
		st.Operations.Current = &cur
	}
	return st
}

// Close stops the measurer and the queue: the running operation is
// cancelled and queued ones are dropped. Wait returns once both stopped.
func (s *Service) Close() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *Service) Wait() { s.wg.Wait() }
