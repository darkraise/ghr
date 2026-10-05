package runner

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/sched"
)

// Tick runs one poll → schedule → spawn → reap cycle. Every phase that calls
// the GitHub API re-checks apiAllowed, because an earlier phase may have hit
// a rate limit or an auth failure.
func (m *Manager) Tick(ctx context.Context) {
	cfg := m.Config()
	now := m.Now()
	m.ticks++
	m.refreshUnits(ctx)
	m.readJobFiles()
	if m.apiAllowed(now) {
		m.refreshRunners(ctx)
	}
	demandOK := false
	if m.apiAllowed(now) {
		demandOK = m.gatherDemand(ctx, cfg, now)
	}
	if m.apiAllowed(now) && !m.isDegraded() && m.releaseCheckDue(now) {
		m.checkRelease(ctx)
	}
	if m.apiAllowed(now) {
		m.stopIdle(ctx, cfg, now)
	}
	m.stopStartTimedOut(ctx, cfg, now)
	m.startUpdateIfFree(cfg, now, demandOK)
	if m.apiAllowed(now) && !m.isDegraded() {
		m.spawnPlanned(ctx, cfg, now)
	}
	if m.apiAllowed(now) && !m.isDegraded() && m.ticks%30 == 0 {
		m.Reconcile(ctx, cfg)
	}
	if m.apiAllowed(now) {
		m.finalizePending(ctx)
	}
	diskDue := m.ticks%10 == 1
	m.mu.Lock()
	retentionDue := now.Sub(m.lastPrune) >= 24*time.Hour
	m.mu.Unlock()
	// Manual and automatic pruning share one reservation, so neither runs
	// Docker's prune commands or rewrites the history file under the other;
	// a skipped automatic prune runs on a later tick.
	if (diskDue || retentionDue) && m.reservePrune() {
		func() {
			defer m.releasePrune()
			if diskDue {
				m.checkDisk(ctx, cfg)
			}
			if retentionDue {
				m.prune(cfg, now)
			}
		}()
	}
}

// gatherDemand lists matching queued jobs per repo and confirms our in-progress
// jobs. Degraded clears only after a tick in which some repo answered and none
// failed authentication. It reports whether every unpaused repo answered.
func (m *Manager) gatherDemand(ctx context.Context, cfg *config.Config, now time.Time) bool {
	ours := map[string]string{}
	for _, i := range m.snapshot() {
		ours[i.RunnerName] = i.ID
	}
	demand := sched.Demand{}
	anyOK, authFailed, complete := false, false, true
	for _, r := range cfg.Repos {
		if r.Paused {
			continue
		}
		m.mu.Lock()
		wait := now.Before(m.retryAt[r.Name])
		m.mu.Unlock()
		if wait {
			complete = false
			continue
		}
		jobs, err := m.repoDemand(ctx, cfg, r, ours, now)
		if err != nil {
			complete = false
			if github.IsKind(err, github.ErrAuth) {
				authFailed = true
			}
			m.apiErr(r.Name, err, now)
			if authFailed || !m.apiAllowed(now) {
				break
			}
			continue
		}
		anyOK = true
		demand[r.Name] = jobs
		m.mu.Lock()
		delete(m.fails, r.Name)
		delete(m.retryAt, r.Name)
		delete(m.repoErr, r.Name)
		m.mu.Unlock()
	}
	m.mu.Lock()
	m.demand = demand
	recovered := anyOK && !authFailed && m.degraded
	if recovered {
		m.degraded = false
		m.degradedReason = ""
	}
	m.mu.Unlock()
	if recovered {
		m.Events.Add("ok", "", "GitHub token accepted again")
	}
	return complete
}

func (m *Manager) repoDemand(ctx context.Context, cfg *config.Config, r config.Repo, ours map[string]string, now time.Time) ([]sched.QueuedJob, error) {
	eff := cfg.EffectiveLabels(r)
	seen := map[int64]bool{}
	var out []sched.QueuedJob
	for _, status := range []string{"queued", "in_progress", "waiting"} {
		runs, err := m.GH.ListRuns(ctx, r.Name, status)
		if err != nil {
			return nil, err
		}
		for _, run := range runs {
			if seen[run.ID] {
				continue
			}
			seen[run.ID] = true
			jobs, err := m.GH.ListJobs(ctx, r.Name, run.ID)
			if err != nil {
				return nil, err
			}
			for _, j := range jobs {
				switch j.Status {
				case "queued":
					if sched.MatchLabels(j.Labels, eff) {
						out = append(out, sched.QueuedJob{Repo: r.Name, ID: j.ID, CreatedAt: j.CreatedAt})
					}
				case "in_progress":
					if id, ok := ours[j.RunnerName]; ok {
						m.confirm(id, j, now)
					}
				}
			}
		}
	}
	return out, nil
}

func (m *Manager) confirm(id string, j github.Job, now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	i, ok := m.insts[id]
	if !ok || i.State == sched.Cleaning {
		return
	}
	if i.State != sched.Busy {
		i.State = sched.Busy
		i.StateSince = now
	}
	i.JobConfirmed = true
	started := now
	if i.Job != nil && !i.Job.StartedAt.IsZero() {
		started = i.Job.StartedAt
	}
	if j.StartedAt != nil {
		started = *j.StartedAt
	}
	runNumber := ""
	if i.Job != nil {
		runNumber = i.Job.RunNumber
	}
	i.Job = &model.JobInfo{RunID: j.RunID, RunNumber: runNumber, Workflow: j.WorkflowName, Name: j.Name, HTMLURL: j.HTMLURL, StartedAt: started}
}

// spawnPlanned spawns what sched.Plan decides, nothing while a runner update
// runs. Repos in an error state are left out of planning (no warm spawns into
// a failing repo) until a demand poll succeeds. Spawning stops as soon as the
// live config differs from the one the plan used, so a pause or cap change
// made during polling is never overridden.
func (m *Manager) spawnPlanned(ctx context.Context, cfg *config.Config, now time.Time) {
	m.mu.Lock()
	if m.upd.running {
		m.mu.Unlock()
		return
	}
	demand := m.demand
	planCfg := *cfg
	planCfg.Repos = nil
	for _, r := range cfg.Repos {
		if m.repoErr[r.Name] == "" && !now.Before(m.retryAt[r.Name]) {
			planCfg.Repos = append(planCfg.Repos, r)
		}
	}
	m.mu.Unlock()
	failed := map[string]bool{}
	for _, s := range sched.Plan(&planCfg, m.schedInstances(cfg), demand, now) {
		if m.Config() != cfg {
			return
		}
		if failed[strings.ToLower(s.Repo)] {
			continue
		}
		if err := m.spawn(ctx, cfg, s.Repo); err != nil {
			m.Events.Add("error", s.Repo, "spawn failed: %v", err)
			failed[strings.ToLower(s.Repo)] = true
			var ae *github.APIError
			if errors.As(err, &ae) && (ae.Kind == github.ErrAuth || ae.Kind == github.ErrRateLimit) {
				m.apiErr(s.Repo, err, now)
				return
			}
		}
	}
}
