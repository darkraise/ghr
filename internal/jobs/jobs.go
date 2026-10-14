// Package jobs keeps the schedule and state of ghr's background tasks. A
// task runs where it always ran; its owner asks Due, and reports Started and
// Finished.
package jobs

import (
	"errors"
	"sync"
	"time"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/model"
)

var (
	ErrUnknown = errors.New("no such background task")
	ErrOff     = errors.New("the task is off")
	ErrBusy    = errors.New("the task is already running or about to run")
)

type def struct {
	title, description string
	// dueAtStart runs the task at daemon start; the others first run one
	// interval after it.
	dueAtStart bool
}

var defs = map[string]def{
	config.JobGhrUpdateCheck:    {"ghr update check", "Asks GitHub for the newest ghr release.", true},
	config.JobRunnerUpdateCheck: {"Runner update check", "Asks GitHub for the newest runner release and its forced-update deadline.", true},
	config.JobPrune:             {"Prune", "Removes history and logs older than history_retention. While on, a disk above disk_high_water also prunes Docker.", false},
	config.JobStorageMeasure:    {"Storage measurement", "Measures the disk use of Docker, toolchains and package caches for the Storage page.", false},
}

type state struct {
	running, pending               bool
	lastStart, lastFinish, retryAt time.Time
	lastErr                        string
	wake                           func()
}

// Registry holds every task's state. Its schedule is read from cfg on every
// call, so a config change applies without a call here; Changed only wakes
// the owners so their timers pick it up.
type Registry struct {
	cfg      func() config.Jobs
	now      func() time.Time
	onChange func()
	start    time.Time

	mu    sync.Mutex
	tasks map[string]*state
}

// New makes a registry; onChange, when set, runs after every state change.
func New(cfg func() config.Jobs, now func() time.Time, onChange func()) *Registry {
	r := &Registry{cfg: cfg, now: now, onChange: onChange, start: now(), tasks: map[string]*state{}}
	for _, name := range config.JobNames {
		r.tasks[name] = &state{}
	}
	return r
}

// Register sets the function RunNow and Changed call to wake the task's owner.
func (r *Registry) Register(name string, wake func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s := r.tasks[name]; s != nil {
		s.wake = wake
	}
}

// next is when the task runs next by its schedule; ok is false while it is
// off. r.mu must be held.
func (r *Registry) next(name string, s *state, j config.Job) (time.Time, bool) {
	switch {
	case !j.Enabled:
		return time.Time{}, false
	case !s.retryAt.IsZero():
		return s.retryAt, true
	case !s.lastFinish.IsZero():
		return s.lastFinish.Add(j.Every.D()), true
	case defs[name].dueAtStart:
		return r.start, true
	}
	return r.start.Add(j.Every.D()), true
}

// Due reports whether the task should run now: on, not running, and asked
// for by Run now or past its next run.
func (r *Registry) Due(name string, now time.Time) bool {
	j, known := r.cfg().Get(name)
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.tasks[name]
	if !known || s == nil || !j.Enabled || s.running {
		return false
	}
	if s.pending {
		return true
	}
	next, _ := r.next(name, s, j)
	return !now.Before(next)
}

// Next is when the task runs next by its schedule; ok is false while it is off.
func (r *Registry) Next(name string) (time.Time, bool) {
	j, known := r.cfg().Get(name)
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.tasks[name]
	if !known || s == nil {
		return time.Time{}, false
	}
	return r.next(name, s, j)
}

// Until is how long an owner's timer waits for the task's next run: zero
// when it is due, an hour while it is off, since Changed wakes the owner
// when it is turned on.
func (r *Registry) Until(name string, now time.Time) time.Duration {
	next, ok := r.Next(name)
	if !ok {
		return time.Hour
	}
	return max(next.Sub(now), 0)
}

// Started records a run's start, whoever started it.
func (r *Registry) Started(name string, now time.Time) {
	r.update(name, func(s *state) {
		s.running, s.pending, s.lastStart = true, false, now
	})
}

// Finished records a run's end. The next run is retryAt when given, else one
// interval after now.
func (r *Registry) Finished(name string, now time.Time, err error, retryAt *time.Time) {
	r.update(name, func(s *state) {
		s.running, s.lastFinish, s.lastErr, s.retryAt = false, now, "", time.Time{}
		if err != nil {
			s.lastErr = err.Error()
		}
		if retryAt != nil {
			s.retryAt = *retryAt
		}
	})
}

func (r *Registry) update(name string, fn func(s *state)) {
	r.mu.Lock()
	s := r.tasks[name]
	if s != nil {
		fn(s)
	}
	r.mu.Unlock()
	if s != nil && r.onChange != nil {
		r.onChange()
	}
}

// RunNow asks for a run at the owner's next pass and wakes the owner.
func (r *Registry) RunNow(name string) error {
	j, known := r.cfg().Get(name)
	r.mu.Lock()
	s := r.tasks[name]
	var err error
	switch {
	case !known || s == nil:
		err = ErrUnknown
	case !j.Enabled:
		err = ErrOff
	case s.running || s.pending:
		err = ErrBusy
	}
	if err != nil {
		r.mu.Unlock()
		return err
	}
	s.pending = true
	wake := s.wake
	r.mu.Unlock()
	if wake != nil {
		wake()
	}
	if r.onChange != nil {
		r.onChange()
	}
	return nil
}

// Changed wakes every owner after a config change, so a new interval or an
// on/off switch applies without waiting for the old timer. A task turned off
// drops a Run now still pending.
func (r *Registry) Changed() {
	cfg := r.cfg()
	r.mu.Lock()
	var wakes []func()
	for _, name := range config.JobNames {
		s := r.tasks[name]
		if j, _ := cfg.Get(name); !j.Enabled {
			s.pending = false
		}
		if s.wake != nil {
			wakes = append(wakes, s.wake)
		}
	}
	r.mu.Unlock()
	for _, w := range wakes {
		w()
	}
	if r.onChange != nil {
		r.onChange()
	}
}

// Snapshot lists every task for the status, in JobNames order.
func (r *Registry) Snapshot() []model.JobStatus {
	if r == nil {
		return nil
	}
	cfg := r.cfg()
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]model.JobStatus, 0, len(config.JobNames))
	for _, name := range config.JobNames {
		s := r.tasks[name]
		j, _ := cfg.Get(name)
		js := model.JobStatus{
			Name: name, Title: defs[name].title, Description: defs[name].description,
			Enabled: j.Enabled, Every: j.Every.String(), Running: s.running, Pending: s.pending, LastError: s.lastErr,
		}
		if !s.lastStart.IsZero() {
			at := s.lastStart
			js.LastStart = &at
		}
		if !s.lastFinish.IsZero() {
			at := s.lastFinish
			js.LastFinish = &at
		}
		if next, ok := r.next(name, s, j); ok && !s.running {
			js.Next = &next
		}
		out = append(out, js)
	}
	return out
}
