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
	latestSum    string // the latest release's linux-x64 SHA-256; "" when its notes carry none
	latestPub    time.Time
	deadline     time.Time // zero unless a release newer than installed exists
	checkedAt    time.Time
	checkErr     string
	nextCheck    time.Time
	running      bool
	lastOutcome  string
	lastErr      string
	lastFinished time.Time
	gen          int       // bumped when an install switches dist/current
	stopRetryAt  time.Time // no start before this, after an idle runner did not stop
	waitReason   string    // why a queued update last waited; "" when nothing held it
	preparing    bool      // the queued version is downloading in the background
	prepareStop  context.CancelFunc
	prepareAt    time.Time // no prepare before this, after one failed
	prepareFails int       // failed prepares in a row
	prepareErr   string    // the last failed prepare's error; "" once one succeeds
	prepareErrAt time.Time
}

// forgetPrepareLocked drops what failed prepares left behind; m.mu must be
// held.
func (m *Manager) forgetPrepareLocked() {
	m.upd.prepareAt, m.upd.prepareFails, m.upd.prepareErr, m.upd.prepareErrAt = time.Time{}, 0, "", time.Time{}
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
	if !sameTime(f.QueuedAt, m.upd.file.QueuedAt) {
		// A queue that was cancelled or made anew waits for its own reasons.
		m.upd.waitReason = ""
	}
	m.upd.file = f
	m.mu.Unlock()
	return nil
}

func sameTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(*b)
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
	sum, _ := f.latest.LinuxX64SHA256()
	m.mu.Lock()
	// An install that switched dist/current during the request has already
	// published newer facts than this check read.
	stale := m.upd.gen != gen
	switch {
	case stale:
	case err != nil:
		if installed != m.upd.installed {
			m.upd.latest, m.upd.latestSum, m.upd.latestPub, m.upd.deadline, m.upd.checkedAt = "", "", time.Time{}, time.Time{}, time.Time{}
		}
		m.upd.installed, m.upd.checkErr, m.upd.nextCheck = installed, err.Error(), now.Add(releaseCheckRetry)
	default:
		m.upd.installed, m.upd.latest, m.upd.latestSum, m.upd.latestPub, m.upd.deadline = installed, latest, sum, f.latest.PublishedAt, f.deadline
		m.upd.checkedAt, m.upd.checkErr, m.upd.nextCheck = now, "", now.Add(releaseCheckEvery)
	}
	m.mu.Unlock()
	if err != nil {
		// A 403 here refuses the release listing only; it is already recorded
		// as the check's error and belongs to no repository.
		if tokenRejected(err) || github.IsKind(err, github.ErrRateLimit) {
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
	// Queueing again is the owner's retry: a failed prepare does not wait.
	m.mu.Lock()
	m.forgetPrepareLocked()
	m.mu.Unlock()
	m.Events.Add("info", "", "runner update queued")
	return nil
}

// CancelUpdate drops a queued update and stops its prepare; with none queued
// it does nothing.
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
	m.mu.Lock()
	stop := m.upd.prepareStop
	m.forgetPrepareLocked()
	m.mu.Unlock()
	if stop != nil {
		stop()
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

// runnerUpdate renders the update state for /status; m.mu must be held. A
// failing prepare is shown as the last outcome: before the prepare ran in
// the background, the same failure ended the update.
func (m *Manager) runnerUpdate() model.RunnerUpdate {
	u := m.upd
	r := model.RunnerUpdate{
		Installed: u.installed, Latest: u.latest, LatestPublished: timePtr(u.latestPub), Deadline: timePtr(u.deadline),
		CheckedAt: timePtr(u.checkedAt), CheckError: u.checkErr,
		Queued: u.file.QueuedAt != nil, QueuedAt: u.file.QueuedAt, Running: u.running,
		LastOutcome: u.lastOutcome, LastError: u.lastErr, LastFinished: timePtr(u.lastFinished),
	}
	if u.prepareErr != "" {
		r.LastOutcome, r.LastError, r.LastFinished = "failed", u.prepareErr, timePtr(u.prepareErrAt)
	}
	return r
}

const (
	// updateTimeout bounds one runner update. Its version is normally
	// prepared already, so the update only switches to it; it downloads only
	// when a release newer than the prepared one appeared in between.
	updateTimeout = 10 * time.Minute
	// prepareTimeout bounds downloading and preparing a queued version, which
	// runs beside the runners.
	prepareTimeout = 30 * time.Minute
	// updateStopRetry spaces start attempts after an idle runner could not be
	// stopped, so a persistent failure does not stop the other idle runners
	// (and respawn them) on every tick.
	updateStopRetry = 15 * time.Minute
)

// startUpdateIfFree starts a queued runner update when its version is
// prepared and ghr is free: no instance starting or busy, this tick's demand
// poll (made with cfg, still the live config) answered for every repo that is
// neither paused nor blocked with no queued job, the API allowed and not
// degraded, the maintenance reservation free and no maintenance hold.
// gatherDemand decides which repos count. Every idle runner is stopped first
// and comes back on the new version; one that took a job, or could not be
// checked or stopped, means ghr is not free after all. Spawning waits until
// the update ends. A shutdown during the stops leaves the update queued and
// not started.
func (m *Manager) startUpdateIfFree(tickCtx context.Context, cfg *config.Config, now time.Time, demandOK bool, unanswered string) {
	reason := ""
	switch {
	case m.isDegraded():
		reason = "GitHub rejected the token"
	case !m.apiAllowed(now):
		reason = "GitHub API calls are paused"
	case !demandOK:
		reason = unanswered + " did not answer its poll"
	}
	m.noteUpdateWait(reason)
	if reason != "" || m.Config() != cfg {
		return
	}
	m.fileMu.Lock()
	m.mu.Lock()
	free := m.upd.file.QueuedAt != nil && !m.upd.running && !m.upd.preparing && m.preparedLocked() &&
		!m.closed && !m.pruning && m.held == 0 && !now.Before(m.upd.stopRetryAt)
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
		if i.State != sched.Idle {
			continue
		}
		res := m.stopIdleRunner(stopCtx, i, now)
		if res == idleKept && tickCtx.Err() == nil {
			m.mu.Lock()
			m.upd.stopRetryAt = now.Add(updateStopRetry)
			m.mu.Unlock()
			m.Events.Add("warn", "", "runner update postponed %s: idle runner %s could not be stopped", updateStopRetry, i.ID)
		}
		if res != idleStopped {
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

// preparedLocked reports whether a queued update has nothing left to
// download: its version is complete in dist, or the update would not stage
// one, because no newer release is known or no checksum can verify it.
// m.mu must be held.
func (m *Manager) preparedLocked() bool {
	u := m.upd
	return u.deadline.IsZero() || u.latestSum == "" || complete(filepath.Join(filepath.Dir(m.Paths.Dist), u.latest))
}

// prepareUpdate downloads, verifies and prepares a queued update's version in
// the background, so the update itself only switches to it and runners keep
// working meanwhile. A failure keeps the queue and is retried after
// releaseCheckRetry, doubling with each failure in a row up to
// releaseCheckEvery, since a bad checksum or a failing dependency script
// fails the same way each time; a shutdown or a cancel just ends it.
func (m *Manager) prepareUpdate(now time.Time) {
	m.mu.Lock()
	ver, sum := m.upd.latest, m.upd.latestSum
	if m.upd.file.QueuedAt == nil || m.upd.running || m.upd.preparing || m.closed || now.Before(m.upd.prepareAt) || m.preparedLocked() {
		m.mu.Unlock()
		return
	}
	// A cancel ends ctx only; the dependency script runs under base, which
	// still ends with the timeout and a shutdown.
	base, cancelBase := context.WithTimeout(m.maintCtx, prepareTimeout)
	ctx, cancel := context.WithCancel(base)
	m.upd.preparing, m.upd.prepareStop = true, cancel
	m.wg.Add(1)
	m.mu.Unlock()
	m.Events.Add("info", "", "runner update: downloading %s", ver)
	go func() {
		defer m.wg.Done()
		err := m.stage(ctx, base, filepath.Dir(m.Paths.Dist), ver, sum)
		stopped := errors.Is(ctx.Err(), context.Canceled)
		cancel()
		cancelBase()
		now := m.Now()
		retry := releaseCheckRetry
		m.mu.Lock()
		m.upd.preparing, m.upd.prepareStop = false, nil
		switch {
		case err == nil:
			m.forgetPrepareLocked()
		case !stopped:
			m.upd.prepareFails++
			for n := 1; n < m.upd.prepareFails && retry < releaseCheckEvery; n++ {
				retry *= 2
			}
			retry = min(retry, releaseCheckEvery)
			m.upd.prepareAt, m.upd.prepareErr, m.upd.prepareErrAt = now.Add(retry), err.Error(), now
		}
		m.mu.Unlock()
		switch {
		case err == nil:
			m.Events.Add("info", "", "runner update: %s is ready", ver)
		case !stopped:
			m.Events.Add("warn", "", "runner update: preparing %s failed: %v; retrying in %s", ver, err, retry)
		}
	}()
}

// noteUpdateWait writes one event when the reason a queued update is held
// back changes. Runners working or jobs queued are the ordinary wait and are
// not reasons here.
func (m *Manager) noteUpdateWait(reason string) {
	m.mu.Lock()
	if m.upd.file.QueuedAt == nil || m.upd.running {
		reason = ""
	}
	changed := reason != m.upd.waitReason
	m.upd.waitReason = reason
	m.mu.Unlock()
	if changed && reason != "" {
		m.Events.Add("warn", "", "runner update waiting: %s", reason)
	}
}
