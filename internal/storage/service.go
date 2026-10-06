package storage

import (
	"context"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/darkraise/ghr/internal/events"
	"github.com/darkraise/ghr/internal/model"
)

var (
	ErrMeasuring = errors.New("a measurement is already running")
	ErrClosed    = errors.New("ghr is shutting down")
)

// MeasureInterval is how often the measurer runs without being asked.
const MeasureInterval = 30 * time.Minute

// Service holds the snapshot GET /storage serves, runs the measurer, and
// runs toolchain installs, removals and cache clears one at a time.
type Service struct {
	Tools  Toolchains
	Docker Docker
	Home   string     // the runner user's home, where the package caches live
	User   string     // owns the directories a clear recreates; "" leaves them to the daemon
	Busy   func() int // runners with a job; nil counts none
	Events *events.Ring
	Now    func() time.Time // nil means time.Now
	NewID  func() string    // operation ids; nil makes random ones

	mu        sync.Mutex
	snap      measured
	measuring bool
	closed    bool
	queue     []*op
	current   *model.Operation
	recent    []model.Operation // newest first

	ctx     context.Context
	cancel  context.CancelFunc
	trigger chan struct{}
	wake    chan struct{}
	wg      sync.WaitGroup
}

// op is one queued operation; run returns its outcome and message.
type op struct {
	model.Operation
	run func(ctx context.Context, progress func(step string)) (outcome, message string)
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Start deletes what an interrupted install or clear left behind, then
// starts the measurer, which measures at once, and the queue worker. Call
// it once, before any other method.
func (s *Service) Start() {
	if err := s.Tools.CleanTmp(); err != nil {
		s.Events.Add("warn", "", "storage: clearing the tool cache's .tmp: %v", err)
	}
	if err := sweepClearing(s.Home); err != nil {
		s.Events.Add("warn", "", "storage: removing interrupted cache clears: %v", err)
	}
	s.snap = emptyMeasured()
	s.recent = []model.Operation{}
	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.trigger = make(chan struct{}, 1)
	s.wake = make(chan struct{}, 1)
	s.trigger <- struct{}{}
	s.wg.Add(2)
	go func() {
		defer s.wg.Done()
		s.measureLoop()
	}()
	go func() {
		defer s.wg.Done()
		s.work()
	}()
}

func (s *Service) measureLoop() {
	tick := time.NewTicker(MeasureInterval)
	defer tick.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-tick.C:
		case <-s.trigger:
		}
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
		Measuring:    s.measuring,
		MeasureError: s.snap.err,
		Operations:   model.Operations{Queued: len(s.queue), Recent: slices.Clone(s.recent)},
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
