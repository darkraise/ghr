// Package runner owns the runner instances: spawning, state tracking, cleanup and reconciliation.
package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
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
	authRecheck = 60 * time.Second
	maxBackoff  = 2 * time.Minute
)

var idRe = regexp.MustCompile(`^[0-9a-f]{6}$`)

type GitHub interface {
	ListRuns(ctx context.Context, repo, status string) ([]github.Run, error)
	ListJobs(ctx context.Context, repo string, runID int64) ([]github.Job, error)
	ListRunners(ctx context.Context, repo string) ([]github.Runner, error)
	GetRunner(ctx context.Context, repo string, id int64) (*github.Runner, error)
	DeleteRunner(ctx context.Context, repo string, id int64) error
	GenerateJITConfig(ctx context.Context, repo, name string, labels []string) (*github.JITConfig, error)
	ListRunnerReleases(ctx context.Context) ([]github.Release, error)
	RateRemaining() int
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
	ToolCache string
	Hooks     string
	Home      string
	// UpdateState is runner-update.json; empty keeps the update queue in memory only.
	UpdateState string
}

// Meta is written to <instance>/ghr.json at spawn and read back on re-adoption.
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
	deregistered bool      // stopIdleRunner deleted its registration
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
	repoErr        map[string]string
	fails          map[string]int
	retryAt        map[string]time.Time
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
	epoch          string
	wg             sync.WaitGroup
	upd            updateState // guarded by mu
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
	m.lastJob = map[string]model.HistoryEntry{}
	m.lastPrune = m.Now()
	m.maintCtx, m.maintCancel = context.WithCancel(context.Background())
	m.epoch = m.Epoch
	if m.epoch == "" {
		m.epoch = strconv.FormatInt(m.Now().UnixNano(), 36)
	}
	m.loadUpdate()
	entries, err := m.History.Query("", "", 0)
	if err != nil {
		return err
	}
	for _, e := range entries {
		m.recordLastJob(e)
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
// it. unused-volumes is refused while a runner is busy: a job's unnamed
// volume is unreferenced between the steps that mount it.
func (m *Manager) StartPruneScope(scope string) error {
	if !slices.Contains(Scopes, scope) {
		return fmt.Errorf("%w %q", ErrUnknownScope, scope)
	}
	m.mu.Lock()
	closed := m.closed
	m.mu.Unlock()
	if closed {
		return ErrClosed
	}
	if scope == ScopeUnusedVolumes {
		if n := m.BusyCount(); n > 0 {
			return model.BusyError{N: n}
		}
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return ErrClosed
	}
	if m.upd.running {
		m.mu.Unlock()
		return ErrUpdateRunning
	}
	if m.pruning {
		m.mu.Unlock()
		return ErrPruneRunning
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
		m.forcedPrune(ctx, scope)
	}()
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

// BusyCount is the number of runners with a job. It reads job.json first, so
// a job the hook has just recorded counts before GitHub confirms it.
// readJobFiles takes mu itself, so the count takes it afterwards.
func (m *Manager) BusyCount() int {
	m.readJobFiles()
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, i := range m.insts {
		if i.State == sched.Busy {
			n++
		}
	}
	return n
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

func (m *Manager) instanceDir(id string) string { return filepath.Join(m.Paths.Instances, id) }

func (m *Manager) setState(id string, s sched.State) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if i, ok := m.insts[id]; ok && i.State != s && i.State != sched.Cleaning {
		i.State = s
		i.StateSince = m.Now()
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
	var out []sched.Instance
	for _, i := range m.snapshot() {
		out = append(out, sched.Instance{ID: i.ID, Repo: i.Repo, State: i.State, StateSince: i.StateSince,
			JobConfirmed: i.JobConfirmed, Stale: stale(cfg, i)})
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
	tmp := path + ".tmp"
	if err := writeJSON(tmp, v); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
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
		DiskPct:       m.disk.Pct, DiskUsedBytes: m.disk.Used, DiskTotalBytes: m.disk.Total,
		Repos: []model.RepoStatus{}, Instances: []model.InstanceStatus{},
		Maintenance: m.maint, RunnerUpdate: m.runnerUpdate(),
	}
	for _, r := range cfg.Repos {
		rs := model.RepoStatus{Name: r.Name, Paused: r.Paused, Removing: r.Removing, Max: cfg.EffectiveMax(r),
			Queued: len(m.demand[r.Name]), Error: m.repoErr[r.Name]}
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

// Kill stops a runner's unit; its exit is handled by the next tick.
func (m *Manager) Kill(ctx context.Context, id string) error {
	m.mu.Lock()
	i, ok := m.insts[id]
	m.mu.Unlock()
	if !ok {
		return ErrUnknownRunner(id)
	}
	if err := m.SD.Stop(ctx, UnitPrefix+id); err != nil {
		return err
	}
	m.Events.Add("warn", i.Repo, "runner %s stopped by user", id)
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

// ClearDegraded is called after the token is replaced or reloaded.
func (m *Manager) ClearDegraded() {
	m.mu.Lock()
	m.degraded = false
	m.degradedReason = ""
	m.mu.Unlock()
}

// apiErr applies the error-handling table: auth → degraded, rate limit → pause,
// 404 → per-repo access error, anything else → per-repo backoff.
func (m *Manager) apiErr(repo string, err error, now time.Time) {
	var ae *github.APIError
	if errors.As(err, &ae) {
		switch ae.Kind {
		case github.ErrAuth:
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
		case github.ErrRateLimit:
			m.mu.Lock()
			was := now.Before(m.pauseUntil)
			if ae.RetryAt.After(m.pauseUntil) {
				m.pauseUntil = ae.RetryAt
			}
			m.mu.Unlock()
			if !was {
				m.Events.Add("warn", repo, "GitHub rate limit; pausing API calls until %s", ae.RetryAt.Format(time.TimeOnly))
			}
			return
		}
	}
	msg := err.Error()
	if github.IsKind(err, github.ErrNotFound) {
		msg = "token lacks access to repo (add it to the PAT's repository access)"
	}
	m.mu.Lock()
	m.fails[repo]++
	d := 10 * time.Second << (m.fails[repo] - 1)
	if d > maxBackoff || d <= 0 {
		d = maxBackoff
	}
	m.retryAt[repo] = now.Add(d)
	m.repoErr[repo] = msg
	m.mu.Unlock()
	m.Events.Add("warn", repo, "%s; retrying in %s", msg, d)
}
