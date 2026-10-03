// Package runner owns the runner instances: spawning, state tracking, cleanup and reconciliation.
package runner

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
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

type Host interface {
	CopyTree(ctx context.Context, src, dst string) error
	ChownR(ctx context.Context, path, user string) error
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
}

type Manager struct {
	Config  func() *config.Config
	GH      GitHub
	SD      Systemd
	Docker  Docker
	Host    Host
	Paths   Paths
	Events  *events.Ring
	History *history.Store
	Now     func() time.Time
	NewID   func() string

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
	diskPct        int
	ticks          int
	lastPrune      time.Time
	lastJob        map[string]model.HistoryEntry
	epoch          string
	wg             sync.WaitGroup
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
	m.epoch = strconv.FormatInt(m.Now().UnixNano(), 36)
	entries, err := m.History.Query("", "", 0)
	if err != nil {
		return err
	}
	for _, e := range entries {
		m.recordLastJob(e)
	}
	return nil
}

// recordLastJob keeps the newest finished job per repo; m.mu must be held or unshared.
func (m *Manager) recordLastJob(e model.HistoryEntry) {
	if cur, ok := m.lastJob[e.Repo]; !ok || e.FinishedAt.After(cur.FinishedAt) {
		m.lastJob[e.Repo] = e
	}
}

// Wait blocks until background finishes return; each is bounded by finishTimeout.
func (m *Manager) Wait() { m.wg.Wait() }

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

func writeJSON(path string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
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
		RateRemaining: m.GH.RateRemaining(), DiskPct: m.diskPct,
		Repos: []model.RepoStatus{}, Instances: []model.InstanceStatus{},
	}
	for _, r := range cfg.Repos {
		rs := model.RepoStatus{Name: r.Name, Paused: r.Paused, Removing: r.Removing, Max: cfg.EffectiveMax(r),
			Queued: len(m.demand[r.Name]), Error: m.repoErr[r.Name]}
		for _, i := range m.insts {
			if i.Repo == r.Name {
				rs.Active++
			}
		}
		if e, ok := m.lastJob[r.Name]; ok {
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
			m.mu.Lock()
			was := m.degraded
			m.degraded = true
			m.degradedReason = ae.Error()
			m.authCheckAt = now.Add(authRecheck)
			m.mu.Unlock()
			if !was {
				m.Events.Add("error", repo, "GitHub rejected the token (%v); spawning stopped", ae)
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
