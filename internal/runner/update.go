package runner

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"time"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/sched"
)

const (
	releaseCheckEvery = 24 * time.Hour
	releaseCheckRetry = time.Hour
	// GitHub stops queueing jobs to a runner 30 days after a newer version
	// was made available.
	updateGrace = 30 * 24 * time.Hour
)

var (
	ErrUpdateRunning = errors.New("a runner update is running")
	ErrNoDist        = errors.New("runner version unknown (no dist/current): run setup.sh first")
	errNoRelease     = errors.New("no actions/runner release with a version tag")
)

// UpToDateError is QueueUpdate's answer when no newer runner exists; it holds
// the installed version.
type UpToDateError string

func (e UpToDateError) Error() string { return "runner " + string(e) + " is already up to date" }

// updateFile is the persisted part of the runner update state.
type updateFile struct {
	QueuedAt      *time.Time `json:"queued_at,omitempty"`
	WarnedVersion string     `json:"warned_version,omitempty"`
}

// updateState is the runner update's state; guarded by Manager.mu. Only
// storeUpdateFile changes file, besides clearQueue's reset when the file can
// be neither rewritten nor removed.
type updateState struct {
	file         updateFile
	installed    string // "" when dist/current does not resolve
	latest       string
	latestPub    time.Time
	deadline     time.Time // zero unless a release newer than installed exists
	checkedAt    time.Time
	checkErr     string
	nextCheck    time.Time
	running      bool
	lastOutcome  string
	lastErr      string
	lastFinished time.Time
	gen          int // bumped when an install switches dist/current
}

// releaseFacts is what a release listing says about an installed version.
type releaseFacts struct {
	latest   github.Release
	version  github.Version
	deadline time.Time
}

// factsFor picks the highest release and the update deadline: 30 days after
// the earliest-published release newer than installed. ok is false when no
// release has a version tag.
func factsFor(installed string, rels []github.Release) (releaseFacts, bool) {
	inst, instOK := github.ParseVersion(installed)
	var f releaseFacts
	found := false
	var firstNewer time.Time
	for _, r := range rels {
		v, ok := github.ParseVersion(r.TagName)
		if !ok {
			continue
		}
		if !found || v.Compare(f.version) > 0 {
			f.latest, f.version, found = r, v, true
		}
		if instOK && v.Compare(inst) > 0 && (firstNewer.IsZero() || r.PublishedAt.Before(firstNewer)) {
			firstNewer = r.PublishedAt
		}
	}
	if !firstNewer.IsZero() {
		f.deadline = firstNewer.Add(updateGrace)
	}
	return f, found
}

// installedVersion is the base name of the directory dist/current resolves
// to, or "" when it does not resolve.
func (m *Manager) installedVersion() string {
	p, err := m.Host.ReadLink(m.Paths.Dist)
	if err != nil {
		return ""
	}
	return filepath.Base(p)
}

// loadUpdate starts the update state afresh from dist/current and
// runner-update.json; a missing file is an empty state.
func (m *Manager) loadUpdate() {
	m.upd = updateState{installed: m.installedVersion()}
	if m.Paths.UpdateState == "" {
		return
	}
	var f updateFile
	if err := readJSON(m.Paths.UpdateState, &f); err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			m.Events.Add("warn", "", "runner update state unreadable, starting empty: %v", err)
		}
		return
	}
	m.upd.file = f
}

// storeUpdateFile writes f and only then keeps it, so memory never claims
// what the file does not hold. m.fileMu must be held.
func (m *Manager) storeUpdateFile(f updateFile) error {
	if m.Paths.UpdateState != "" {
		if err := writeJSONAtomic(m.Paths.UpdateState, f); err != nil {
			return fmt.Errorf("save runner update state: %w", err)
		}
	}
	m.mu.Lock()
	m.upd.file = f
	m.mu.Unlock()
	return nil
}

func (m *Manager) releaseCheckDue(now time.Time) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return !now.Before(m.upd.nextCheck)
}

// checkRelease lists the runner releases and records what they say about the
// installed version, warning once per newer version. A failure keeps the last
// good facts and retries after an hour.
func (m *Manager) checkRelease(ctx context.Context) ([]github.Release, error) {
	m.mu.Lock()
	gen := m.upd.gen
	m.mu.Unlock()
	installed := m.installedVersion()
	rels, err := m.GH.ListRunnerReleases(ctx)
	now := m.Now()
	f, found := factsFor(installed, rels)
	if err == nil && !found {
		err = errNoRelease
	}
	latest := f.version.String()
	m.mu.Lock()
	// An install that switched dist/current during the request has already
	// published newer facts than this check read.
	stale := m.upd.gen != gen
	switch {
	case stale:
	case err != nil:
		if installed != m.upd.installed {
			m.upd.latest, m.upd.latestPub, m.upd.deadline, m.upd.checkedAt = "", time.Time{}, time.Time{}, time.Time{}
		}
		m.upd.installed, m.upd.checkErr, m.upd.nextCheck = installed, err.Error(), now.Add(releaseCheckRetry)
	default:
		m.upd.installed, m.upd.latest, m.upd.latestPub, m.upd.deadline = installed, latest, f.latest.PublishedAt, f.deadline
		m.upd.checkedAt, m.upd.checkErr, m.upd.nextCheck = now, "", now.Add(releaseCheckEvery)
	}
	m.mu.Unlock()
	if err != nil {
		if github.IsKind(err, github.ErrAuth) || github.IsKind(err, github.ErrRateLimit) {
			m.apiErr("", err, now)
		}
		return nil, err
	}
	if !stale && !f.deadline.IsZero() {
		m.warnUpdate(installed, latest, f.deadline)
	}
	return rels, nil
}

// warnUpdate posts the update warning unless this version was already warned
// about, before or since the last restart.
func (m *Manager) warnUpdate(installed, latest string, deadline time.Time) {
	m.fileMu.Lock()
	defer m.fileMu.Unlock()
	m.mu.Lock()
	f := m.upd.file
	m.mu.Unlock()
	if f.WarnedVersion == latest {
		return
	}
	f.WarnedVersion = latest
	if err := m.storeUpdateFile(f); err != nil {
		m.Events.Add("warn", "", "%v", err)
	}
	m.Events.Add("warn", "", "runner %s is available (installed %s); update by %s", latest, installed, deadline.Local().Format(time.DateOnly))
}

// QueueUpdate checks GitHub for a newer runner and queues its install for the
// next time ghr is free. Queueing twice is not an error.
func (m *Manager) QueueUpdate(ctx context.Context) error {
	m.mu.Lock()
	closed, running := m.closed, m.upd.running
	m.mu.Unlock()
	switch {
	case closed:
		return ErrClosed
	case running:
		return ErrUpdateRunning
	}
	if _, err := m.checkRelease(ctx); err != nil {
		return err
	}
	m.fileMu.Lock()
	defer m.fileMu.Unlock()
	m.mu.Lock()
	installed, newer, running, f := m.upd.installed, !m.upd.deadline.IsZero(), m.upd.running, m.upd.file
	m.mu.Unlock()
	switch {
	case running:
		return ErrUpdateRunning
	case installed == "":
		return ErrNoDist
	case !newer:
		return UpToDateError(installed)
	case f.QueuedAt != nil:
		return nil
	}
	now := m.Now()
	f.QueuedAt = &now
	if err := m.storeUpdateFile(f); err != nil {
		return err
	}
	m.Events.Add("info", "", "runner update queued")
	return nil
}

// CancelUpdate drops a queued update; with none queued it does nothing.
func (m *Manager) CancelUpdate() error {
	m.fileMu.Lock()
	defer m.fileMu.Unlock()
	m.mu.Lock()
	running, f := m.upd.running, m.upd.file
	m.mu.Unlock()
	if running {
		return ErrUpdateRunning
	}
	if f.QueuedAt == nil {
		return nil
	}
	f.QueuedAt = nil
	if err := m.storeUpdateFile(f); err != nil {
		return err
	}
	m.Events.Add("info", "", "runner update cancelled")
	return nil
}

func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// runnerUpdate renders the update state for /status; m.mu must be held.
func (m *Manager) runnerUpdate() model.RunnerUpdate {
	u := m.upd
	return model.RunnerUpdate{
		Installed: u.installed, Latest: u.latest, LatestPublished: timePtr(u.latestPub), Deadline: timePtr(u.deadline),
		CheckedAt: timePtr(u.checkedAt), CheckError: u.checkErr,
		Queued: u.file.QueuedAt != nil, QueuedAt: u.file.QueuedAt, Running: u.running,
		LastOutcome: u.lastOutcome, LastError: u.lastErr, LastFinished: timePtr(u.lastFinished),
	}
}

// updateTimeout bounds one runner update.
const updateTimeout = 10 * time.Minute

// startUpdateIfFree starts a queued runner update when ghr is free: no
// instance starting or busy, this tick's demand poll (made with cfg, still the
// live config) answered for every unpaused repo with no queued job, the API
// allowed and not degraded, and the maintenance reservation free. Every idle
// runner is stopped first and comes back on the new version; one that took a
// job, or could not be checked or stopped, means ghr is not free after all.
// Spawning waits until the update ends. A shutdown during the stops leaves the
// update queued and not started.
func (m *Manager) startUpdateIfFree(tickCtx context.Context, cfg *config.Config, now time.Time, demandOK bool) {
	if !demandOK || m.Config() != cfg || !m.apiAllowed(now) || m.isDegraded() {
		return
	}
	m.fileMu.Lock()
	m.mu.Lock()
	free := m.upd.file.QueuedAt != nil && !m.upd.running && !m.closed && !m.pruning
	for _, i := range m.insts {
		if i.State == sched.Starting || i.State == sched.Busy {
			free = false
		}
	}
	for _, jobs := range m.demand {
		if len(jobs) > 0 {
			free = false
		}
	}
	if !free {
		m.mu.Unlock()
		m.fileMu.Unlock()
		return
	}
	m.pruning, m.upd.running = true, true
	ctx, cancel := context.WithTimeout(m.maintCtx, updateTimeout)
	m.wg.Add(1)
	m.mu.Unlock()
	m.fileMu.Unlock()
	finish := func() {
		m.mu.Lock()
		m.pruning, m.upd.running = false, false
		m.mu.Unlock()
		cancel()
		m.wg.Done()
	}
	stopCtx, stopCancel := context.WithCancel(ctx)
	stopAfter := context.AfterFunc(tickCtx, stopCancel)
	stopped := true
	for _, i := range m.snapshot() {
		if i.State == sched.Idle && m.stopIdleRunner(stopCtx, i, now) != idleStopped {
			stopped = false
			break
		}
	}
	stopAfter()
	stopCancel()
	if !stopped || tickCtx.Err() != nil {
		finish()
		return
	}
	m.Events.Add("info", "", "runner update started")
	go func() {
		defer finish()
		m.runUpdate(ctx)
	}()
}
