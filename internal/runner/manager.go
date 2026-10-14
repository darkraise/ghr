// Package runner owns the runner instances: spawning, state tracking, cleanup and reconciliation.
package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/events"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/history"
	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/sched"
	"github.com/darkraise/ghr/internal/system"
)

const (
	RunnerUser = "ghrunner"
	UnitPrefix = "ghr-runner-"
	MetaFile   = "ghr.json"
	JobFile    = "job.json"
)

const (
	authRecheck     = 60 * time.Second
	maxBackoff      = 2 * time.Minute
	maxSpawnBackoff = 10 * time.Minute
)

var idRe = regexp.MustCompile(`^[0-9a-f]{6}$`)

type GitHub interface {
	ListRuns(ctx context.Context, repo, status string) ([]github.Run, error)
	ListJobs(ctx context.Context, repo string, runID int64) ([]github.Job, error)
	ListAttemptJobs(ctx context.Context, repo string, runID int64, attempt int) ([]github.Job, error)
	ListRunners(ctx context.Context, repo string) ([]github.Runner, error)
	GetRunner(ctx context.Context, repo string, id int64) (*github.Runner, error)
	DeleteRunner(ctx context.Context, repo string, id int64) error
	GenerateJITConfig(ctx context.Context, repo, name string, labels []string) (*github.JITConfig, error)
	GetRepo(ctx context.Context, repo string) (*github.Repository, error)
	ListRunnerReleases(ctx context.Context) ([]github.Release, error)
	RateRemaining() int
	SuspendedUntil() time.Time
}

type Systemd interface {
	Start(ctx context.Context, u system.UnitSpec) error
	Stop(ctx context.Context, unit string) error
	Active(ctx context.Context, unit string) (bool, error)
	List(ctx context.Context, prefix string) ([]string, error)
}

type Docker interface {
	ComposeContainers(ctx context.Context) ([]system.ComposeContainer, error)
	Containers(ctx context.Context) ([]system.NamedContainer, error)
	ProjectContainers(ctx context.Context, project string) ([]system.ProjectContainer, error)
	ContainerIDsByLabel(ctx context.Context, label string) ([]string, error)
	RemoveContainers(ctx context.Context, ids []string) error
	RemoveNetworksByLabel(ctx context.Context, label string) error
	RemoveVolumesByLabel(ctx context.Context, label string) error
	DataRootUsage(ctx context.Context) (int, error)
	PruneBuildCacheOlderThan(ctx context.Context, hours int) (string, error)
	PruneBuildCacheTo(ctx context.Context, keep string) (string, error)
	PruneDanglingImages(ctx context.Context) (string, error)
}

// Disk is Docker's disk reporting and the prunes only the prune scopes run.
type Disk interface {
	DiskUsage(ctx context.Context) ([]system.DiskRow, error)
	BuildCacheUsage(ctx context.Context) ([]system.CacheTypeUsage, error)
	PruneAllBuildCache(ctx context.Context) (string, error)
	PruneUnusedVolumes(ctx context.Context) (string, error)
}

var _ Disk = system.Docker{}

type Host interface {
	CopyTree(ctx context.Context, src, dst string) error
	ChownR(ctx context.Context, path, user string) error
	Extract(ctx context.Context, tarball, dir string) error
	RunScript(ctx context.Context, dir, path string) error
	ReadLink(path string) (string, error)
	SwitchLink(target, path string) error
}

// Paths are the on-disk locations ghr uses. Dist may be a symlink to the current runner version.
type Paths struct {
	Dist      string
	Instances string
	Logs      string
	Pending   string // history records waiting for the job's final conclusion
	// Private holds what root must trust about an instance, mode 0700. The
	// instance directory itself belongs to the runner user.
	Private   string
	ToolCache string
	Hooks     string
	Home      string
	// UpdateState is runner-update.json; empty keeps the update queue in memory only.
	UpdateState string
}

// Meta is recorded in Paths.Private at spawn and read back on re-adoption;
// <instance>/ghr.json holds a copy.
type Meta struct {
	ID          string    `json:"id"`
	Repo        string    `json:"repo"`
	RunnerID    int64     `json:"runner_id"`
	RunnerName  string    `json:"runner_name"`
	Labels      []string  `json:"labels"` // the custom labels the runner was registered with
	DistVersion string    `json:"dist_version"`
	SpawnedAt   time.Time `json:"spawned_at"`
}

// JobRecord is <instance>/job.json: written by hooks/job-started.sh, and
// job-completed.sh adds FinishedAt. Times are RFC3339 UTC strings.
type JobRecord struct {
	RunID      string `json:"run_id"`
	RunAttempt string `json:"run_attempt"`
	RunNumber  string `json:"run_number"`
	Workflow   string `json:"workflow"`
	Job        string `json:"job"`
	RunnerName string `json:"runner_name"`
	StartedAt  string `json:"started_at"`
	FinishedAt string `json:"finished_at"`
}

type instance struct {
	Meta
	State        sched.State
	StateSince   time.Time
	JobConfirmed bool
	Job          *model.JobInfo
	finishing    bool      // a finish goroutine is running
	retryFinish  time.Time // when a failed finish may run again
	cleanupFails int       // consecutive failed Docker cleanups
	gaveUp       bool      // the cleanup give-up has been reported
	deregistered bool      // its registration was deleted by ghr or confirmed missing
	lost         bool      // the registration was confirmed missing, not deleted by ghr
	offlineSince time.Time // when an idle runner was first seen offline; zero while online
	adopted      bool      // rebuilt by Adopt, not spawned by this process
	stopping     bool      // Kill asked systemd to stop the unit
	cameOnline   bool      // it has been idle or busy at least once
	startFailed  bool      // its failed start is already recorded in the repo's spawn backoff
	cleaned      int       // containers removed by earlier finish attempts of a runner that ran no job
	answered     bool      // the runners API answered for it in the current tick
}

// registration names a runner registration for a delete retried later.
type registration struct {
	repo, name string
}

// runnerList is one repository's registered runner IDs as read in this tick;
// ok is false when the read failed.
type runnerList struct {
	ids map[int64]bool
	ok  bool
}

type Manager struct {
	Config  func() *config.Config
	GH      GitHub
	SD      Systemd
	Docker  Docker
	Disk    Disk // the daemon sets system.Docker
	Host    Host
	Paths   Paths
	Events  *events.Ring
	History *history.Store
	Now     func() time.Time
	NewID   func() string
	// Fetch downloads url to dst; the daemon sets system.Download.
	Fetch func(ctx context.Context, url, dst string) error
	// PruneDone, when set, is called after every manual prune and after an
	// automatic prune that pruned.
	PruneDone func()
	// Epoch, when set, is the status epoch Init uses instead of minting one,
	// so a daemon that served before the manager started keeps one epoch.
	Epoch string

	mu             sync.Mutex
	insts          map[string]*instance
	demand         sched.Demand
	repoErr        map[string]string // by lower-cased repo, like fails and retryAt
	fails          map[string]int
	retryAt        map[string]time.Time
	lists          map[string]runnerList // by lower-cased repo; reset every tick
	spawnFails     map[string]int        // failed starts in a row, by lower-cased repo
	spawnRetryAt   map[string]time.Time  // no spawn for the repo before this
	spawnErr       map[string]string     // the last failed start's message
	vis            map[string]visibility // by lower-cased repo; a repo without an entry is unread
	wasBlocked     map[string]bool       // lower-cased repos whose block has been reported
	visGen         uint64                // bumped by ClearDegraded; guarded by mu
	degraded       bool
	degradedReason string
	authCheckAt    time.Time
	pauseUntil     time.Time
	disk           system.DiskUsage
	ticks          int
	lastPrune      time.Time
	pruning        bool                    // a manual or automatic prune holds the reservation; guarded by mu
	closed         bool                    // set by Close; StartPrune refuses afterwards so it cannot race Wait; guarded by mu
	maint          model.MaintenanceStatus // manual prunes; guarded by mu
	lastPruneRec   *model.LastPrune        // the newest manual or automatic prune; guarded by mu
	maintCtx       context.Context         // cancelled by Close
	maintCancel    context.CancelFunc
	lastJob        map[string]model.HistoryEntry
	histIDs        map[string]bool        // instance IDs history holds; guarded by mu
	orphans        map[int64]bool         // untracked registrations seen offline at the last reconcile; guarded by mu
	undeleted      map[int64]registration // by runner ID: registrations finish could not delete; guarded by mu
	epoch          string
	wg             sync.WaitGroup
	upd            updateState // guarded by mu
	held           int         // maintenance holds that keep runners from spawning; guarded by mu
	spawning       bool        // a tick is spawning runners not yet in insts; guarded by mu
	// fileMu serialises changes to the persisted update state, so a queue,
	// a cancel, a warning and an update start never overwrite one another;
	// take it before mu.
	fileMu sync.Mutex
}

// Init prepares internal state and loads the last finished job per repo from history.
func (m *Manager) Init() error {
	m.insts = map[string]*instance{}
	m.demand = sched.Demand{}
	m.repoErr = map[string]string{}
	m.fails = map[string]int{}
	m.retryAt = map[string]time.Time{}
	m.lists = map[string]runnerList{}
	m.spawnFails = map[string]int{}
	m.spawnRetryAt = map[string]time.Time{}
	m.spawnErr = map[string]string{}
	m.vis = map[string]visibility{}
	m.wasBlocked = map[string]bool{}
	m.lastJob = map[string]model.HistoryEntry{}
	m.undeleted = map[int64]registration{}
	m.histIDs = map[string]bool{}
	m.lastPrune = m.Now()
	m.maintCtx, m.maintCancel = context.WithCancel(context.Background())
	m.epoch = m.Epoch
	if m.epoch == "" {
		m.epoch = strconv.FormatInt(m.Now().UnixNano(), 36)
	}
	m.loadUpdate()
	entries, err := m.History.Query("", "", time.Time{}, 0)
	if err != nil {
		return err
	}
	for _, e := range entries {
		m.recordLastJob(e)
		m.histIDs[e.ID] = true
	}
	return nil
}

// recordLastJob keeps the newest finished job per repo, keyed by the lower-cased
// name because repo names are case-insensitive; m.mu must be held or unshared.
func (m *Manager) recordLastJob(e model.HistoryEntry) {
	key := strings.ToLower(e.Repo)
	if cur, ok := m.lastJob[key]; !ok || e.FinishedAt.After(cur.FinishedAt) {
		m.lastJob[key] = e
	}
}

// Wait blocks until background finishes and prunes return; a finish is
// bounded by finishTimeout, a prune by Close.
func (m *Manager) Wait() { m.wg.Wait() }

// ErrPruneRunning is returned by StartPrune while a prune runs; a runner
// update holds the same reservation and gets ErrUpdateRunning instead.
var ErrPruneRunning = errors.New("a prune is already running")

// ErrClosed is returned by StartPrune once Close has been called.
var ErrClosed = errors.New("ghr is shutting down")

// Prune scopes, as POST /prune/{scope} names them.
const (
	ScopeStandard       = "standard"
	ScopeBuildCacheKeep = "build-cache-keep"
	ScopeBuildCacheAll  = "build-cache-all"
	ScopeDanglingImages = "dangling-images"
	ScopeUnusedVolumes  = "unused-volumes"
)

var Scopes = []string{ScopeStandard, ScopeBuildCacheKeep, ScopeBuildCacheAll, ScopeDanglingImages, ScopeUnusedVolumes}

var ErrUnknownScope = errors.New("unknown prune scope")

// StartPrune starts a standard manual prune in the background.
func (m *Manager) StartPrune() error { return m.StartPruneScope(ScopeStandard) }

// StartPruneScope starts a manual prune of one scope in the background. The
// caller returns before it finishes; Wait waits for it and Close interrupts
// it. unused-volumes holds the runners for maintenance until it ends, so it
// is refused while a runner is busy: a job's unnamed volume is unreferenced
// between the steps that mount it.
func (m *Manager) StartPruneScope(scope string) error {
	if !slices.Contains(Scopes, scope) {
		return fmt.Errorf("%w %q", ErrUnknownScope, scope)
	}
	m.mu.Lock()
	err := m.pruneRefusedLocked()
	m.mu.Unlock()
	if err != nil {
		return err
	}
	release := func() {}
	if scope == ScopeUnusedVolumes {
		if release, err = m.HoldForMaintenance(m.maintCtx); err != nil {
			return err
		}
	}
	m.mu.Lock()
	if err := m.pruneRefusedLocked(); err != nil {
		m.mu.Unlock()
		release()
		return err
	}
	now := m.Now()
	m.pruning = true
	m.maint.Running = true
	m.maint.LastStarted = &now
	m.lastPruneRec = &model.LastPrune{Trigger: "manual", Scope: scope, StartedAt: now, Steps: []model.PruneStep{}}
	ctx := m.maintCtx
	m.wg.Add(1)
	m.mu.Unlock()
	go func() {
		defer m.wg.Done()
		defer release()
		m.forcedPrune(ctx, scope)
	}()
	return nil
}

// pruneRefusedLocked is why a manual prune cannot start now, nil when it
// can; m.mu must be held.
func (m *Manager) pruneRefusedLocked() error {
	switch {
	case m.closed:
		return ErrClosed
	case m.upd.running:
		return ErrUpdateRunning
	case m.pruning:
		return ErrPruneRunning
	}
	return nil
}

// Maintenance reports the manual prune state.
func (m *Manager) Maintenance() model.MaintenanceStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.maint
}

// LastPrune is the newest manual or automatic prune, nil before the first.
func (m *Manager) LastPrune() *model.LastPrune {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.lastPruneRec == nil {
		return nil
	}
	lp := *m.lastPruneRec
	lp.Steps = slices.Clone(lp.Steps)
	return &lp
}

// busyLocked counts the busy runners; m.mu must be held.
func (m *Manager) busyLocked() int {
	n := 0
	for _, i := range m.insts {
		if i.State == sched.Busy {
			n++
		}
	}
	return n
}

// ErrHoldRefused wraps why HoldForMaintenance could not keep the runners off
// when no job is running.
var ErrHoldRefused = errors.New("refused")

// HoldForMaintenance keeps every runner off for maintenance that must not run
// beside a job. It is refused while a runner is starting or busy or a runner
// update runs; otherwise it stops every idle runner, and no runner spawns
// until release is called. An idle runner that took a job before it
// could be stopped, or that could not be stopped, refuses the hold. release
// may be called more than once.
//
// job.json is read first, so a job the hook has just recorded counts before
// GitHub confirms it; readJobFiles takes mu itself.
func (m *Manager) HoldForMaintenance(ctx context.Context) (release func(), err error) {
	m.readJobFiles()
	m.mu.Lock()
	starting := m.spawning
	for _, i := range m.insts {
		starting = starting || i.State == sched.Starting
	}
	switch {
	case m.closed:
		err = ErrClosed
	case m.upd.running:
		err = ErrUpdateRunning
	case m.busyLocked() > 0:
		err = model.BusyError{N: m.busyLocked()}
	case starting:
		err = fmt.Errorf("%w: a runner is starting", ErrHoldRefused)
	}
	if err != nil {
		m.mu.Unlock()
		return nil, err
	}
	m.held++
	m.mu.Unlock()
	var once sync.Once
	release = func() {
		once.Do(func() {
			m.mu.Lock()
			m.held--
			m.mu.Unlock()
		})
	}
	for _, i := range m.snapshot() {
		if i.State != sched.Idle {
			continue
		}
		switch m.stopIdleRunner(ctx, i, m.Now()) {
		case idleStopped:
			continue
		case idleBusy:
			m.mu.Lock()
			err = model.BusyError{N: m.busyLocked()}
			m.mu.Unlock()
		default:
			err = fmt.Errorf("%w: idle runner %s could not be stopped", ErrHoldRefused, i.ID)
		}
		release()
		return nil, err
	}
	return release, nil
}

// Close interrupts a running prune and refuses new ones; the daemon calls it
// on shutdown before Wait.
func (m *Manager) Close() {
	m.mu.Lock()
	m.closed = true
	m.mu.Unlock()
	if m.maintCancel != nil {
		m.maintCancel()
	}
}

// reservePrune takes the reservation shared by manual and automatic pruning,
// reporting false when a prune already holds it.
func (m *Manager) reservePrune() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pruning {
		return false
	}
	m.pruning = true
	return true
}

func (m *Manager) releasePrune() {
	m.mu.Lock()
	m.pruning = false
	m.mu.Unlock()
}

// Pruning reports whether a manual or automatic prune is running. A runner
// update holds the same reservation and is not a prune.
func (m *Manager) Pruning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.pruning && !m.upd.running
}

// startDiskPrune runs pruneDisk in the background and releases the prune
// reservation when it ends; the caller must hold the reservation. Docker's
// prune commands can take minutes, and a tick that waited for them would
// spawn and reap nothing meanwhile. It reports false, starting nothing, once
// Close has been called.
func (m *Manager) startDiskPrune(cfg *config.Config, pct int) bool {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return false
	}
	ctx := m.maintCtx
	// Counted under the lock that guards closed, so Wait cannot miss it.
	m.wg.Add(1)
	m.mu.Unlock()
	go func() {
		defer m.wg.Done()
		defer m.releasePrune()
		m.pruneDisk(ctx, cfg, pct)
	}()
	return true
}

func (m *Manager) instanceDir(id string) string { return filepath.Join(m.Paths.Instances, id) }

// enterLocked puts i in state s; m.mu must be held. The first time a runner
// comes online its repository's failed starts are forgotten.
func (m *Manager) enterLocked(i *instance, s sched.State, now time.Time) {
	i.State = s
	i.StateSince = now
	if s == sched.Idle || s == sched.Busy {
		if !i.cameOnline {
			m.clearSpawnBackoffLocked(i.Repo)
		}
		i.cameOnline = true
	}
}

// move puts the instance in state to when its live state is one of from, and
// reports whether it did. Callers decide from a snapshot that a network call
// has made old, so only the live state counts.
func (m *Manager) move(id string, to sched.State, from ...sched.State) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	i, ok := m.insts[id]
	if !ok || !slices.Contains(from, i.State) {
		return false
	}
	m.enterLocked(i, to, m.Now())
	return true
}

// backoff is the wait after n failures in a row: 10 s, doubling up to limit.
func backoff(n int, limit time.Duration) time.Duration {
	d := 10 * time.Second << (n - 1)
	if d > limit || d <= 0 {
		return limit
	}
	return d
}

// spawnFailed records a failed start for repo and returns the wait before the
// next attempt: 10 s, doubling to maxSpawnBackoff. A successful poll does not
// reset it, because a poll that works says nothing about a spawn that does not.
func (m *Manager) spawnFailed(repo, msg string, now time.Time) time.Duration {
	key := strings.ToLower(repo)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.spawnFails[key]++
	d := backoff(m.spawnFails[key], maxSpawnBackoff)
	m.spawnRetryAt[key] = now.Add(d)
	m.spawnErr[key] = msg
	return d
}

// clearSpawnBackoffLocked forgets repo's failed starts; m.mu must be held.
func (m *Manager) clearSpawnBackoffLocked(repo string) {
	key := strings.ToLower(repo)
	delete(m.spawnFails, key)
	delete(m.spawnRetryAt, key)
	delete(m.spawnErr, key)
}

// spawnText words a failed start for the repository's status line.
func spawnText(err error) string {
	var ae *github.APIError
	if errors.As(err, &ae) && ae.Kind == github.ErrAuth {
		return permissionText(ae)
	}
	return err.Error()
}

// beginTick forgets what the last tick learned from the runners API.
func (m *Manager) beginTick() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lists = map[string]runnerList{}
	for _, i := range m.insts {
		i.answered = false
	}
}

func (m *Manager) markAnswered(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if i, ok := m.insts[id]; ok {
		i.answered = true
	}
}

// noteOnline records what the runners API said about a runner's connection:
// the first time an idle runner is seen offline, or that it is connected.
func (m *Manager) noteOnline(id string, online bool, now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	i, ok := m.insts[id]
	switch {
	case !ok:
	case online:
		i.offlineSince = time.Time{}
	case i.State == sched.Idle && i.offlineSince.IsZero():
		i.offlineSince = now
	}
}

func (m *Manager) snapshot() []instance {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]instance, 0, len(m.insts))
	for _, i := range m.insts {
		out = append(out, *i)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].ID < out[b].ID })
	return out
}

// unconfirmedBusy reports whether repo has a busy runner whose job the jobs
// API has not confirmed yet.
func (m *Manager) unconfirmedBusy(repo string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, i := range m.insts {
		if strings.EqualFold(i.Repo, repo) && i.State == sched.Busy && !i.JobConfirmed {
			return true
		}
	}
	return false
}

// stale reports whether an instance was registered with labels other than its
// repo's current custom labels; an unconfigured repo's instance is not stale.
func stale(cfg *config.Config, i instance) bool {
	r := cfg.Repo(i.Repo)
	if r == nil {
		return false
	}
	want := cfg.CustomLabels(*r)
	got := append([]string{}, i.Labels...)
	sort.Strings(want)
	sort.Strings(got)
	return !slices.Equal(want, got)
}

func (m *Manager) schedInstances(cfg *config.Config) []sched.Instance {
	now, grace := m.Now(), cfg.StartTimeout.D()
	var out []sched.Instance
	for _, i := range m.snapshot() {
		// An idle runner may be reconnecting; start_timeout is how long a
		// runner gets to connect in the first place.
		offline := i.State == sched.Idle && !i.offlineSince.IsZero() && now.Sub(i.offlineSince) >= grace
		out = append(out, sched.Instance{ID: i.ID, Repo: i.Repo, State: i.State, StateSince: i.StateSince,
			JobConfirmed: i.JobConfirmed, Stale: stale(cfg, i), Unusable: i.deregistered || offline})
	}
	return out
}

// writeFile is os.WriteFile; tests replace it to simulate a full disk.
var writeFile = os.WriteFile

func writeJSON(path string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return writeFile(path, data, 0o644)
}

// writeJSONAtomic writes via a temp file and rename so a crash never leaves a partial file.
func writeJSONAtomic(path string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return writeFileAtomic(path, data, 0o644)
}

// writeFileAtomic replaces path through a temp file that is synced before the
// rename, so a crash or power loss leaves the old content or the new, never
// a truncated file.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	defer os.Remove(tmp)
	if err := writeFile(tmp, data, perm); err != nil {
		return err
	}
	// Opened for writing: Windows cannot flush a read-only handle.
	f, err := os.OpenFile(tmp, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	err = f.Sync()
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	// Syncing the directory makes the rename itself durable. Some platforms,
	// Windows among them, cannot sync a directory, so it is best effort.
	if d, err := os.Open(filepath.Dir(path)); err == nil {
		d.Sync()
		d.Close()
	}
	return nil
}

func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// maxJobFile bounds a file read from an instance directory.
const maxJobFile = 1 << 20

// readJobFile reads a file from an instance directory. The job can
// write that directory and root reads the file, so it gets the checks
// readFrom makes for logs: a link is refused, only a regular file is
// read, and at most maxJobFile of it. A FIFO would block the caller, which is
// the tick, and a device would feed it for ever. The check is repeated on the
// open file, because the job can swap the name between the two; whatever it
// swaps in, the root lets the open reach only a file inside that directory.
func readJobFile(path string) ([]byte, error) {
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	defer root.Close()
	name := filepath.Base(path)
	fi, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, fs.ErrInvalid
	}
	f, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if fi, err := f.Stat(); err != nil || !fi.Mode().IsRegular() {
		return nil, fs.ErrInvalid
	}
	data, err := io.ReadAll(io.LimitReader(f, maxJobFile+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxJobFile {
		return nil, fmt.Errorf("%s is larger than %d bytes", path, maxJobFile)
	}
	return data, nil
}

// readJobJSON reads a JSON file from an instance directory the way readJobFile does.
func readJobJSON(path string, v any) error {
	data, err := readJobFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// Status renders the state for the control API.
func (m *Manager) Status() model.Status {
	cfg := m.Config()
	m.mu.Lock()
	defer m.mu.Unlock()
	st := model.Status{
		Now: m.Now(), Epoch: m.epoch, Mode: cfg.Mode, GlobalMax: cfg.GlobalMax,
		Degraded: m.degraded, DegradedReason: m.degradedReason,
		RateRemaining: m.GH.RateRemaining(),
		DiskPct:       m.disk.Pct, DiskUsedBytes: m.disk.Used, DiskTotalBytes: m.disk.Total, DiskRoot: m.disk.Root,
		Repos: []model.RepoStatus{}, Instances: []model.InstanceStatus{},
		Maintenance: m.maint, RunnerUpdate: m.runnerUpdate(),
	}
	for _, r := range cfg.Repos {
		rs := model.RepoStatus{Name: r.Name, Paused: r.Paused, Removing: r.Removing, Max: cfg.EffectiveMax(r),
			Queued: len(m.demand[r.Name]), Error: m.repoErr[strings.ToLower(r.Name)]}
		// A paused repo gets no spawns, so a failed start from before the
		// pause is not its state any more.
		if rs.Error == "" && !r.Paused {
			rs.Error = m.spawnErr[strings.ToLower(r.Name)]
		}
		// Why a blocked repo gets nothing comes before what its poll or its
		// last start said. It is worked out here and not stored with them,
		// so a poll that succeeds does not clear it.
		if m.blockedLocked(cfg, r) {
			rs.Error = blockedText(r.Name)
		}
		if public, known := m.visibilityLocked(r.Name); known {
			rs.Public = &public
		}
		for _, q := range m.demand[r.Name] {
			if rs.OldestQueuedAt == nil || q.CreatedAt.Before(*rs.OldestQueuedAt) {
				at := q.CreatedAt
				rs.OldestQueuedAt = &at
			}
		}
		for _, i := range m.insts {
			if strings.EqualFold(i.Repo, r.Name) {
				rs.Active++
			}
		}
		if e, ok := m.lastJob[strings.ToLower(r.Name)]; ok {
			e := e
			rs.LastJob = &e
		}
		st.Repos = append(st.Repos, rs)
	}
	for _, i := range m.insts {
		st.Instances = append(st.Instances, model.InstanceStatus{
			ID: i.ID, Repo: i.Repo, RunnerName: i.RunnerName, State: string(i.State), Since: i.StateSince, Job: i.Job,
			Stopping: i.stopping && i.State != sched.Cleaning,
		})
	}
	sort.Slice(st.Instances, func(a, b int) bool {
		if st.Instances[a].Repo != st.Instances[b].Repo {
			return st.Instances[a].Repo < st.Instances[b].Repo
		}
		return st.Instances[a].ID < st.Instances[b].ID
	})
	return st
}

// ErrUnknownRunner is returned for an ID with no live instance.
type ErrUnknownRunner string

func (e ErrUnknownRunner) Error() string { return "unknown runner " + string(e) }

// Kill stops a runner's unit, marks the runner stopping while the unit stops,
// and starts its cleanup once systemd confirms the unit is down. It does not
// leave the exit to a tick: one in flight can take seconds. Neither the stop
// nor the check ends with ctx: a caller that gives up would kill systemctl
// while the unit may still be stopping, and the runner would read as running
// again.
func (m *Manager) Kill(ctx context.Context, id string) error {
	m.mu.Lock()
	i, ok := m.insts[id]
	if ok {
		i.stopping = true
	}
	m.mu.Unlock()
	if !ok {
		return ErrUnknownRunner(id)
	}
	ctx = context.WithoutCancel(ctx)
	if err := m.SD.Stop(ctx, UnitPrefix+id); err != nil {
		m.mu.Lock()
		if cur, ok := m.insts[id]; ok {
			cur.stopping = false
		}
		m.mu.Unlock()
		return err
	}
	m.Events.Add("warn", i.Repo, "runner %s stopped by user", id)
	if active, err := m.SD.Active(ctx, UnitPrefix+id); err == nil && !active {
		m.startFinish(id, true)
	}
	return nil
}

// RunnerRepoAndRun returns the repo, run ID and runner name of an instance's current job, for step lookups.
func (m *Manager) RunnerRepoAndRun(id string) (repo string, runID int64, runnerName string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	i, ok := m.insts[id]
	if !ok {
		return "", 0, "", ErrUnknownRunner(id)
	}
	if i.Job == nil {
		return i.Repo, 0, i.RunnerName, nil
	}
	return i.Repo, i.Job.RunID, i.RunnerName, nil
}

func (m *Manager) isDegraded() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.degraded
}

func (m *Manager) apiAllowed(now time.Time) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if now.Before(m.pauseUntil) {
		return false
	}
	return !m.degraded || !now.Before(m.authCheckAt)
}

// PausedUntil is when a rate limit's pause on GitHub API calls ends; zero or
// past when none is in force.
func (m *Manager) PausedUntil() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.pauseUntil
}

// ClearDegraded is called after the token is replaced or reloaded. A new
// token may also be what a failing spawn was waiting for, and the rate limit
// and repository backoffs were met with the old one. Every repository's
// visibility is read again at its next poll; the last answer stands until
// then.
func (m *Manager) ClearDegraded() {
	m.mu.Lock()
	m.degraded = false
	m.degradedReason = ""
	m.pauseUntil = time.Time{}
	m.fails, m.retryAt, m.repoErr = map[string]int{}, map[string]time.Time{}, map[string]string{}
	m.spawnFails, m.spawnRetryAt, m.spawnErr = map[string]int{}, map[string]time.Time{}, map[string]string{}
	m.visGen++
	for key, v := range m.vis {
		v.readAt = time.Time{}
		m.vis[key] = v
	}
	m.mu.Unlock()
}

// tokenRejected reports whether err means GitHub rejected the token itself,
// which only a 401 says. A 403 refuses one repository or one call.
func tokenRejected(err error) bool {
	var ae *github.APIError
	return errors.As(err, &ae) && ae.Kind == github.ErrAuth && ae.Status == 401
}

// permissionText words a 403 that is not a rate limit.
func permissionText(ae *github.APIError) string {
	msg := "token lacks permission for this repository"
	if ae.Permissions != "" {
		msg += " (needs " + ae.Permissions + ")"
	}
	return msg + ": " + ae.Error()
}

// apiErr applies the error-handling table: 401 → degraded, rate limit → pause,
// 403 → per-repo permission error, 404 → per-repo access error, anything else
// → per-repo backoff.
func (m *Manager) apiErr(repo string, err error, now time.Time) {
	var ae *github.APIError
	if errors.As(err, &ae) {
		switch {
		case tokenRejected(err):
			reason := "GitHub rejected the token"
			if ae.Path != "" {
				reason += " on " + ae.Method + " " + ae.Path
			}
			if ae.Permissions != "" {
				reason += " (needs " + ae.Permissions + ")"
			}
			reason += ": " + ae.Error()
			m.mu.Lock()
			was := m.degraded
			m.degraded = true
			m.degradedReason = reason
			m.authCheckAt = now.Add(authRecheck)
			m.mu.Unlock()
			if !was {
				m.Events.Add("error", repo, "%s; spawning stopped", reason)
			}
			return
		case ae.Kind == github.ErrRateLimit:
			// The client's suspension, not the error's retry time: the client
			// drops a rate limit that answered a request made with a token it
			// has since replaced.
			until := m.GH.SuspendedUntil()
			m.mu.Lock()
			was := now.Before(m.pauseUntil)
			if until.After(m.pauseUntil) {
				m.pauseUntil = until
			}
			paused := now.Before(m.pauseUntil)
			m.mu.Unlock()
			if !was && paused {
				m.Events.Add("warn", repo, "GitHub rate limit; pausing API calls until %s", until.Format(time.TimeOnly))
			}
			return
		}
	}
	msg := err.Error()
	switch {
	case github.IsKind(err, github.ErrNotFound):
		msg = "token lacks access to repo (add it to the PAT's repository access)"
	case github.IsKind(err, github.ErrAuth):
		msg = permissionText(ae)
	}
	key := strings.ToLower(repo)
	m.mu.Lock()
	// A 403 from a call that does not wait for the repo's backoff repeats on
	// every tick; it is filed once per backoff window.
	if github.IsKind(err, github.ErrAuth) && now.Before(m.retryAt[key]) {
		m.repoErr[key] = msg
		m.mu.Unlock()
		return
	}
	m.fails[key]++
	d := backoff(m.fails[key], maxBackoff)
	m.retryAt[key] = now.Add(d)
	m.repoErr[key] = msg
	m.mu.Unlock()
	m.Events.Add("warn", repo, "%s; retrying in %s", msg, d)
}

// repoRefused files an error from a call that is made on every tick under
// its repository, once per backoff window: the status shows it, and the
// event list does not get a line per tick.
func (m *Manager) repoRefused(repo string, err error, now time.Time) {
	m.mu.Lock()
	waiting := now.Before(m.retryAt[strings.ToLower(repo)])
	m.mu.Unlock()
	if !waiting {
		m.apiErr(repo, err, now)
	}
}
