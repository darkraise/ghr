package runner

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/sched"
	"github.com/darkraise/ghr/internal/system"
)

const (
	finishTimeout  = 15 * time.Minute // bounds one finish attempt, so Wait returns on shutdown
	finishRetry    = 30 * time.Second // delay before a failed finish runs again
	conclusionWait = 30 * time.Minute // how long a finished job's conclusion is polled before "unknown"
)

// RandomID returns 6 lowercase hex characters.
func RandomID() string {
	b := make([]byte, 3)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return !os.IsNotExist(err)
}

// uniqueID returns an ID with no live instance, instance dir, log archive or
// pending history record, so retained logs and history never collide. An
// unreadable path counts as taken, so the attempts are bounded.
func (m *Manager) uniqueID() (string, error) {
	for range 1000 {
		id := m.NewID()
		m.mu.Lock()
		_, taken := m.insts[id]
		m.mu.Unlock()
		if !taken && !exists(m.instanceDir(id)) && !exists(filepath.Join(m.Paths.Logs, id)) &&
			!exists(m.pendingPath(id)) {
			return id, nil
		}
	}
	return "", errors.New("no free runner ID: check that the instance, log and pending dirs are readable")
}

func (m *Manager) pendingPath(id string) string { return filepath.Join(m.Paths.Pending, id+".json") }

// spawn registers a JIT runner for repo and starts it in a transient unit.
func (m *Manager) spawn(ctx context.Context, cfg *config.Config, repo string) error {
	r := cfg.Repo(repo)
	if r == nil {
		return fmt.Errorf("repo %s not configured", repo)
	}
	id, err := m.uniqueID()
	if err != nil {
		return err
	}
	name := "ghr-" + r.Name + "-" + id
	labels := cfg.CustomLabels(*r)
	jit, err := m.GH.GenerateJITConfig(ctx, r.Name, name, labels)
	if err != nil {
		return err
	}
	dir := m.instanceDir(id)
	fail := func(err error) error {
		os.RemoveAll(dir)
		if derr := m.GH.DeleteRunner(ctx, r.Name, jit.Runner.ID); derr != nil {
			m.Events.Add("warn", r.Name, "spawn %s: delete registration: %v", id, derr)
		}
		return err
	}
	dist, err := filepath.EvalSymlinks(m.Paths.Dist)
	if err != nil {
		return fail(err)
	}
	if err := m.Host.CopyTree(ctx, dist, dir); err != nil {
		return fail(err)
	}
	meta := Meta{ID: id, Repo: r.Name, RunnerID: jit.Runner.ID, RunnerName: name, Labels: labels,
		DistVersion: filepath.Base(dist), SpawnedAt: m.Now()}
	if err := writeJSON(filepath.Join(dir, MetaFile), meta); err != nil {
		return fail(err)
	}
	if err := m.Host.ChownR(ctx, dir, RunnerUser); err != nil {
		return fail(err)
	}
	spec := system.UnitSpec{
		Unit:    UnitPrefix + id,
		User:    RunnerUser,
		WorkDir: dir,
		Props: []string{
			"MemoryMax=" + cfg.RunnerLimits.MemoryMax,
			"CPUQuota=" + cfg.RunnerLimits.CPUQuota,
			"KillMode=control-group",
		},
		Env: map[string]string{
			"HOME":                              m.Paths.Home,
			"GHR_INSTANCE_DIR":                  dir,
			"RUNNER_TOOL_CACHE":                 m.Paths.ToolCache,
			"DOTNET_INSTALL_DIR":                filepath.Join(m.Paths.ToolCache, "dotnet"),
			"COMPOSE_PROJECT_NAME":              "ghr-" + id,
			"ACTIONS_RUNNER_HOOK_JOB_STARTED":   filepath.Join(m.Paths.Hooks, "job-started.sh"),
			"ACTIONS_RUNNER_HOOK_JOB_COMPLETED": filepath.Join(m.Paths.Hooks, "job-completed.sh"),
		},
		Command: []string{filepath.Join(dir, "run.sh"), "--jitconfig", jit.EncodedJITConfig},
	}
	if err := m.SD.Start(ctx, spec); err != nil {
		return fail(err)
	}
	m.mu.Lock()
	m.insts[id] = &instance{Meta: meta, State: sched.Starting, StateSince: m.Now()}
	m.mu.Unlock()
	m.Events.Add("info", r.Name, "spawned %s (%s)", id, meta.DistVersion)
	return nil
}

func parseTime(s string) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339, s)
	return t, err == nil
}

// readJobFiles marks instances busy once their job-started hook has written job.json.
func (m *Manager) readJobFiles() {
	for _, i := range m.snapshot() {
		if i.State != sched.Starting && i.State != sched.Idle && i.State != sched.Busy {
			continue
		}
		var rec JobRecord
		if err := readJSON(filepath.Join(m.instanceDir(i.ID), JobFile), &rec); err != nil {
			continue
		}
		m.mu.Lock()
		inst, ok := m.insts[i.ID]
		if ok && inst.State != sched.Cleaning {
			if inst.State == sched.Starting || inst.State == sched.Idle {
				inst.State = sched.Busy
				inst.StateSince = m.Now()
			}
			if inst.Job == nil {
				runID, _ := strconv.ParseInt(rec.RunID, 10, 64)
				started, ok := parseTime(rec.StartedAt)
				if !ok {
					started = m.Now()
				}
				inst.Job = &model.JobInfo{RunID: runID, RunNumber: rec.RunNumber, Workflow: rec.Workflow, Name: rec.Job, StartedAt: started}
			}
		}
		m.mu.Unlock()
	}
}

// refreshRunners moves starting and idle instances to idle or busy from the
// runners API, so an idle runner GitHub reports busy is busy even before its
// hook record or the jobs API shows the job.
func (m *Manager) refreshRunners(ctx context.Context) {
	now := m.Now()
	for _, i := range m.snapshot() {
		if i.State != sched.Starting && i.State != sched.Idle {
			continue
		}
		r, err := m.GH.GetRunner(ctx, i.Repo, i.RunnerID)
		if err != nil {
			if github.IsKind(err, github.ErrAuth) || github.IsKind(err, github.ErrRateLimit) {
				m.apiErr(i.Repo, err, now)
				return
			}
			continue // start_timeout handles runners that never appear
		}
		if r.Busy {
			m.setState(i.ID, sched.Busy)
		} else if r.Status == "online" && i.State == sched.Starting {
			m.setState(i.ID, sched.Idle)
		}
	}
}

// refreshUnits starts cleanup for every instance whose unit has confirmed
// exited, and retries failed cleanups. An unknown unit state is never an exit.
func (m *Manager) refreshUnits(ctx context.Context) {
	now := m.Now()
	for _, i := range m.snapshot() {
		if i.State == sched.Cleaning {
			if !i.finishing && !now.Before(i.retryFinish) {
				m.beginFinish(i.ID)
			}
			continue
		}
		active, err := m.SD.Active(ctx, UnitPrefix+i.ID)
		if err == nil && !active {
			m.beginFinish(i.ID)
		}
	}
}

// beginFinish moves an instance to cleaning and runs finish in the background.
// A failed finish leaves it cleaning (holding its repo slot) for refreshUnits to retry.
func (m *Manager) beginFinish(id string) {
	m.mu.Lock()
	i, ok := m.insts[id]
	if !ok || i.finishing {
		m.mu.Unlock()
		return
	}
	if i.State != sched.Cleaning {
		i.State = sched.Cleaning
		i.StateSince = m.Now()
	}
	i.finishing = true
	repo := i.Repo
	m.mu.Unlock()
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), finishTimeout)
		defer cancel()
		err := m.finish(ctx, id)
		m.mu.Lock()
		if i, ok := m.insts[id]; ok {
			i.finishing = false
			if err != nil {
				i.retryFinish = m.Now().Add(finishRetry)
			}
		}
		m.mu.Unlock()
		if err != nil {
			m.Events.Add("warn", repo, "cleanup of %s failed, retrying in %s: %v", id, finishRetry, err)
		}
	}()
}

// pending is a finished job's history record waiting for its final conclusion.
// It is written before any cleanup step, so a crash at any point loses nothing.
type pending struct {
	Entry      model.HistoryEntry `json:"entry"`
	RunnerName string             `json:"runner_name"`
	Cleanup    int                `json:"cleanup"`
	Deadline   time.Time          `json:"deadline"`
}

// pendingFor builds the record from job.json, falling back to the job the
// manager confirmed through the jobs API. ok is false when no job ran.
func (m *Manager) pendingFor(i instance, rec JobRecord, recOK bool) (pending, bool) {
	if !recOK && i.Job == nil {
		return pending{}, false
	}
	now := m.Now()
	e := model.HistoryEntry{ID: i.ID, Repo: i.Repo, Conclusion: "unknown", StartedAt: i.StateSince, FinishedAt: now}
	if i.Job != nil {
		e.RunID, e.RunNumber, e.Workflow, e.JobName, e.HTMLURL = i.Job.RunID, i.Job.RunNumber, i.Job.Workflow, i.Job.Name, i.Job.HTMLURL
		e.StartedAt = i.Job.StartedAt
	}
	if recOK {
		if id, err := strconv.ParseInt(rec.RunID, 10, 64); err == nil && id != 0 {
			e.RunID = id
		}
		if rec.RunNumber != "" {
			e.RunNumber = rec.RunNumber
		}
		if rec.Workflow != "" {
			e.Workflow = rec.Workflow
		}
		if e.JobName == "" {
			e.JobName = rec.Job
		}
		if t, ok := parseTime(rec.StartedAt); ok {
			e.StartedAt = t
		}
		if t, ok := parseTime(rec.FinishedAt); ok {
			e.FinishedAt = t
		}
	}
	return pending{Entry: e, RunnerName: i.RunnerName, Deadline: now.Add(conclusionWait)}, true
}

// finish runs the exit steps in an order that is safe to repeat after a crash
// or failure: pending history record, log archive, Docker cleanup, instance
// dir, registration. History itself is appended by finalize.
func (m *Manager) finish(ctx context.Context, id string) error {
	cfg := m.Config()
	m.mu.Lock()
	ip, ok := m.insts[id]
	if !ok {
		m.mu.Unlock()
		return nil
	}
	i := *ip
	m.mu.Unlock()
	dir := m.instanceDir(id)

	var rec JobRecord
	recOK := readJSON(filepath.Join(dir, JobFile), &rec) == nil
	p, hadJob := m.pendingFor(i, rec, recOK)
	pendingPath := m.pendingPath(id)
	if hadJob {
		if err := readJSON(pendingPath, &p); err != nil {
			if err := os.MkdirAll(m.Paths.Pending, 0o755); err != nil {
				return err
			}
			if err := writeJSONAtomic(pendingPath, p); err != nil {
				return fmt.Errorf("write pending history: %w", err)
			}
		}
	}

	archive := filepath.Join(m.Paths.Logs, id)
	if diag := filepath.Join(dir, "_diag"); exists(diag) {
		if err := os.MkdirAll(m.Paths.Logs, 0o755); err != nil {
			return fmt.Errorf("archive logs: %w", err)
		}
		if err := os.Rename(diag, archive); err != nil {
			return fmt.Errorf("archive logs: %w", err)
		}
	}
	if exists(archive) {
		if err := m.Host.ChownR(ctx, archive, "root"); err != nil {
			return fmt.Errorf("chown archived logs: %w", err)
		}
	}

	removed, err := m.cleanupDocker(ctx, cfg, id, i.Repo)
	if err != nil {
		return fmt.Errorf("docker cleanup: %w", err)
	}
	if hadJob {
		p.Cleanup = removed
		if err := writeJSONAtomic(pendingPath, p); err != nil {
			return fmt.Errorf("write pending history: %w", err)
		}
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("remove %s: %w", dir, err)
	}
	if err := m.GH.DeleteRunner(ctx, i.Repo, i.RunnerID); err != nil && !github.IsKind(err, github.ErrUnprocessable) {
		m.Events.Add("warn", i.Repo, "delete registration %s: %v (reconciliation retries)", i.RunnerName, err)
	}

	m.mu.Lock()
	delete(m.insts, id)
	m.mu.Unlock()
	if !hadJob {
		m.Events.Add("info", i.Repo, "runner %s exited without a job; cleanup: %d ctrs", id, removed)
		return nil
	}
	m.finalize(ctx, pendingPath)
	return nil
}

// finalize appends a pending record to history once the jobs API reports the
// job completed, or with conclusion "unknown" after its deadline, then deletes it.
func (m *Manager) finalize(ctx context.Context, path string) {
	var p pending
	if err := readJSON(path, &p); err != nil {
		m.Events.Add("warn", "", "unreadable pending history %s removed: %v", filepath.Base(path), err)
		os.Remove(path)
		return
	}
	e := p.Entry
	final := false
	if e.RunID != 0 {
		jobs, err := m.GH.ListJobs(ctx, e.Repo, e.RunID)
		if err != nil && !github.IsKind(err, github.ErrNotFound) {
			if github.IsKind(err, github.ErrAuth) || github.IsKind(err, github.ErrRateLimit) {
				m.apiErr(e.Repo, err, m.Now())
			}
			if m.Now().Before(p.Deadline) {
				return
			}
		}
		for _, j := range jobs {
			if j.RunnerName != p.RunnerName {
				continue
			}
			e.JobName, e.HTMLURL = j.Name, j.HTMLURL
			if j.StartedAt != nil {
				e.StartedAt = *j.StartedAt
			}
			if j.CompletedAt != nil {
				e.FinishedAt = *j.CompletedAt
			}
			if j.Status == "completed" && j.Conclusion != "" {
				e.Conclusion = j.Conclusion
				final = true
			}
		}
	}
	if !final && e.RunID != 0 && m.Now().Before(p.Deadline) {
		return
	}
	if err := m.History.Append(e); err != nil {
		m.Events.Add("warn", e.Repo, "history append: %v", err)
		return
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		m.Events.Add("warn", e.Repo, "remove pending history: %v", err)
	}
	m.mu.Lock()
	m.recordLastJob(e)
	m.mu.Unlock()
	level := "warn"
	switch e.Conclusion {
	case "success":
		level = "ok"
	case "failure":
		level = "error"
	}
	m.Events.Add(level, e.Repo, "#%s %s %s %s  cleanup: %d ctrs", e.RunNumber, e.JobName, e.Conclusion,
		e.FinishedAt.Sub(e.StartedAt).Round(time.Second), p.Cleanup)
}

// finalizePending retries every pending history record whose instance is gone.
func (m *Manager) finalizePending(ctx context.Context) {
	entries, err := os.ReadDir(m.Paths.Pending)
	if err != nil {
		return
	}
	for _, e := range entries {
		id, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok || !idRe.MatchString(id) {
			continue
		}
		m.mu.Lock()
		_, live := m.insts[id]
		m.mu.Unlock()
		if !live {
			m.finalize(ctx, filepath.Join(m.Paths.Pending, e.Name()))
		}
	}
}

// stopIdle stops idle runners chosen by sched.IdleToStop. Each is stopped only
// after the runners API affirms it is not busy; any error leaves it running.
func (m *Manager) stopIdle(ctx context.Context, cfg *config.Config, now time.Time) {
	byID := map[string]instance{}
	for _, i := range m.snapshot() {
		byID[i.ID] = i
	}
	for _, id := range sched.IdleToStop(cfg, m.schedInstances(cfg), now) {
		i := byID[id]
		r, err := m.GH.GetRunner(ctx, i.Repo, i.RunnerID)
		if err != nil {
			if !github.IsKind(err, github.ErrNotFound) {
				m.apiErr(i.Repo, err, now)
			}
			if !m.apiAllowed(now) {
				return
			}
			continue
		}
		if r.Busy {
			m.setState(id, sched.Busy)
			continue
		}
		if err := m.SD.Stop(ctx, UnitPrefix+id); err != nil {
			m.Events.Add("warn", i.Repo, "stop idle %s: %v", id, err)
			continue
		}
		m.Events.Add("info", i.Repo, "stopped idle runner %s", id)
	}
}

// stopStartTimedOut stops runners that never came online; finish deletes their registration.
func (m *Manager) stopStartTimedOut(ctx context.Context, cfg *config.Config, now time.Time) {
	for _, id := range sched.StartTimedOut(cfg, m.schedInstances(cfg), now) {
		m.mu.Lock()
		repo := m.insts[id].Repo
		m.mu.Unlock()
		if err := m.SD.Stop(ctx, UnitPrefix+id); err != nil {
			m.Events.Add("warn", repo, "stop %s: %v", id, err)
			continue
		}
		m.Events.Add("warn", repo, "runner %s did not come online within %s; replaced", id, cfg.StartTimeout)
	}
}
