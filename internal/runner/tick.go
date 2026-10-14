package runner

import (
	"context"
	"slices"
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
	m.beginTick()
	m.refreshUnits(ctx)
	m.readJobFiles()
	m.forgetPaused(cfg)
	m.forgetRemoved(cfg, now)
	if m.apiAllowed(now) {
		m.refreshRunners(ctx)
	}
	demandOK, unanswered := false, ""
	if m.apiAllowed(now) {
		demandOK, unanswered = m.gatherDemand(ctx, cfg, now)
	}
	m.noteBlocked(cfg)
	if m.apiAllowed(now) && !m.isDegraded() && m.releaseCheckDue(now) {
		m.checkRelease(ctx)
	}
	if m.apiAllowed(now) {
		m.stopIdle(ctx, cfg, now)
	} else {
		m.stopDeregistered(ctx, now)
	}
	m.stopStartTimedOut(ctx, cfg, now)
	m.startUpdateIfFree(ctx, cfg, now, demandOK, unanswered)
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
	// a skipped automatic prune runs on a later tick. The tick measures the
	// disk and prunes history itself; Docker prunes run in the background,
	// which then holds the reservation until they end.
	if (diskDue || retentionDue) && m.reservePrune() {
		handedOver := false
		if retentionDue {
			m.prune(cfg, now)
		}
		if diskDue {
			if pct, over := m.measureDisk(ctx, cfg); over {
				handedOver = m.startDiskPrune(cfg, pct)
			}
		}
		if !handedOver {
			m.releasePrune()
		}
	}
}

// gatherDemand lists matching queued jobs per repo and confirms our in-progress
// jobs. Degraded clears only after a tick in which some repo answered and none
// rejected the token. It reports whether every served repo answered, and the
// first one, in config order, that did not. A served repo is one that is
// neither paused nor blocked.
//
// A repo that does not answer keeps its last known queued jobs for status;
// spawnPlanned already leaves such a repo out. A paused repo is polled only
// while one of its busy runners still lacks its job's details, and a blocked
// repo is polled like any other. The queue of either is never kept: nothing
// will serve it, and it must not hold an update back.
func (m *Manager) gatherDemand(ctx context.Context, cfg *config.Config, now time.Time) (bool, string) {
	ours := map[string]string{}
	for _, i := range m.snapshot() {
		ours[i.RunnerName] = i.ID
	}
	m.mu.Lock()
	last := m.demand
	m.mu.Unlock()
	// Asked per use, not once: a repo's own read, below, can change the answer.
	served := func(r config.Repo) bool { return !r.Paused && !m.blocked(cfg, r) }
	demand := sched.Demand{}
	for _, r := range cfg.Repos {
		if jobs, ok := last[r.Name]; ok && served(r) {
			demand[r.Name] = jobs
		}
	}
	anyOK, authFailed, unanswered := false, false, ""
	missed := func(r config.Repo) {
		if served(r) && unanswered == "" {
			unanswered = r.Name
		}
	}
	forget := func(name string) {
		m.mu.Lock()
		delete(m.fails, name)
		delete(m.retryAt, name)
		delete(m.repoErr, name)
		m.mu.Unlock()
	}
	for _, r := range cfg.Repos {
		if r.Paused && !m.unconfirmedBusy(r.Name) {
			continue // forgetPaused has dropped what its last polls left behind
		}
		m.mu.Lock()
		wait := now.Before(m.retryAt[r.Name])
		m.mu.Unlock()
		if wait {
			missed(r)
			continue
		}
		jobs, err := m.repoDemand(ctx, cfg, r, ours, now)
		if err == nil {
			// Read here, before the tick plans anything, so nothing is planned
			// on an answer that is due. A failed read is this poll's failure.
			err = m.readVisibility(ctx, r.Name, now)
		}
		if err != nil {
			missed(r)
			if !served(r) {
				// The listing blocked the repo before a later call of its poll
				// failed; what an earlier poll left for it goes all the same.
				delete(demand, r.Name)
			}
			if tokenRejected(err) {
				authFailed = true
			}
			m.apiErr(r.Name, err, now)
			if authFailed || !m.apiAllowed(now) {
				break
			}
			continue
		}
		anyOK = true
		if served(r) {
			demand[r.Name] = jobs
		} else {
			// The read above may have just found the repo public; what an
			// earlier poll left for it goes too.
			delete(demand, r.Name)
		}
		forget(r.Name)
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
	return unanswered == "", unanswered
}

// forgetPaused drops what the last polls left behind for paused repos that
// are not being polled: their error, their backoff and their queued jobs. It
// needs no API call, so it runs on every tick, API pause or not.
func (m *Manager) forgetPaused(cfg *config.Config) {
	for _, r := range cfg.Repos {
		if !r.Paused || m.unconfirmedBusy(r.Name) {
			continue
		}
		m.mu.Lock()
		delete(m.fails, r.Name)
		delete(m.retryAt, r.Name)
		delete(m.repoErr, r.Name)
		delete(m.demand, r.Name)
		m.mu.Unlock()
	}
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
		// Recorded before the runs' jobs are read, so a failure there does
		// not lose the answer.
		if slices.ContainsFunc(runs, github.Run.RepoPublic) {
			m.sawPublic(r.Name, now)
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
		m.enterLocked(i, sched.Busy, now)
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
// runs or maintenance holds the runners. It plans from planConfig, so a
// blocked repo gets nothing. Repos that are unread, in an error state, or
// waiting after a failed start, are left out of planning. Spawning stops as
// soon as the live config differs from the one the plan used, so a pause or
// cap change made during polling is never overridden.
func (m *Manager) spawnPlanned(ctx context.Context, cfg *config.Config, now time.Time) {
	plan := m.planConfig(cfg)
	var read []config.Repo
	for _, r := range plan.Repos {
		if !m.unread(r) {
			read = append(read, r)
		}
	}
	m.mu.Lock()
	if m.upd.running || m.held > 0 {
		m.mu.Unlock()
		return
	}
	demand := m.demand
	planCfg := *plan
	planCfg.Repos = nil
	for _, r := range read {
		if m.repoErr[r.Name] == "" && !now.Before(m.retryAt[r.Name]) && !now.Before(m.spawnRetryAt[strings.ToLower(r.Name)]) {
			planCfg.Repos = append(planCfg.Repos, r)
		}
	}
	m.mu.Unlock()
	spawns := sched.Plan(&planCfg, m.schedInstances(cfg), demand, now)
	if len(spawns) == 0 {
		return
	}
	// A runner is in insts only once its unit has started, so a hold taken
	// meanwhile would not see it; spawning refuses holds until these end.
	m.mu.Lock()
	if m.held > 0 {
		m.mu.Unlock()
		return
	}
	m.spawning = true
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		m.spawning = false
		m.mu.Unlock()
	}()
	failed := map[string]bool{}
	for _, s := range spawns {
		if m.Config() != cfg {
			return
		}
		if failed[strings.ToLower(s.Repo)] {
			continue
		}
		err := m.spawn(ctx, cfg, s.Repo)
		if err == nil {
			continue
		}
		failed[strings.ToLower(s.Repo)] = true
		if tokenRejected(err) || github.IsKind(err, github.ErrRateLimit) {
			m.Events.Add("error", s.Repo, "spawn failed: %v", err)
			m.apiErr(s.Repo, err, now)
			return
		}
		msg := spawnText(err)
		d := m.spawnFailed(s.Repo, msg, m.Now())
		m.Events.Add("error", s.Repo, "spawn failed: %s; next attempt in %s", msg, d)
	}
}
