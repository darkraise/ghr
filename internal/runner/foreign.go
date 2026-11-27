package runner

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/sched"
)

// foreignCover counts repo's registrations that another ghr host made and
// that GitHub can hand any job this host would start a runner for: named
// ghr-<repo>-<id>, not this host's, not busy, and carrying every label this
// host registers. GitHub shows no creation time, so an offline one counts as
// starting only for start_timeout from when this host first listed it; a gone
// host's leftovers then stop holding jobs back. ok is false when the list
// could not be read.
func (m *Manager) foreignCover(ctx context.Context, cfg *config.Config, r config.Repo, now time.Time) (n int, ok bool) {
	l := m.runnerList(ctx, r.Name, now)
	if !l.ok {
		return 0, false
	}
	eff := cfg.EffectiveLabels(r)
	key := strings.ToLower(r.Name)
	prefix := "ghr-" + key + "-"
	m.mu.Lock()
	defer m.mu.Unlock()
	ours := map[int64]bool{}
	for _, i := range m.insts {
		ours[i.RunnerID] = true
	}
	for id := range m.undeleted {
		ours[id] = true
	}
	seen := m.foreignSeen[key]
	next := map[int64]time.Time{}
	for _, rn := range l.runners {
		name := strings.ToLower(rn.Name)
		if !strings.HasPrefix(name, prefix) || !idRe.MatchString(name[len(prefix):]) || ours[rn.ID] {
			continue
		}
		first, known := seen[rn.ID]
		if !known {
			first = now
		}
		next[rn.ID] = first
		if rn.Busy || !sched.MatchLabels(eff, labelNames(rn.Labels)) {
			continue
		}
		if rn.Status == "online" || now.Sub(first) < cfg.StartTimeout.D() {
			n++
		}
	}
	m.foreignSeen[key] = next
	return n, true
}

func labelNames(ls []github.Label) []string {
	out := make([]string, len(ls))
	for i, l := range ls {
		out[i] = l.Name
	}
	return out
}

// withoutForeign takes from demand, for each repository the plan spawns for
// that has queued jobs, as many of its oldest jobs as other hosts' runners
// cover, the ones a covering runner is counted against in sched.Uncovered.
// A repository whose runner list cannot be read leaves plan this tick: it
// cannot tell whether another host is already starting a runner for its jobs.
func (m *Manager) withoutForeign(ctx context.Context, cfg, plan *config.Config, spawns []sched.Spawn, demand sched.Demand, now time.Time) sched.Demand {
	out := sched.Demand{}
	for repo, jobs := range demand {
		out[repo] = jobs
	}
	read := map[string]bool{}
	for _, s := range spawns {
		key := strings.ToLower(s.Repo)
		if read[key] || len(demand[s.Repo]) == 0 || !m.apiAllowed(now) {
			continue
		}
		read[key] = true
		r := plan.Repo(s.Repo)
		n, ok := m.foreignCover(ctx, cfg, *r, now)
		if !ok {
			plan.Repos = removeRepo(plan.Repos, s.Repo)
			continue
		}
		jobs := append([]sched.QueuedJob{}, demand[s.Repo]...)
		sort.SliceStable(jobs, func(a, b int) bool { return jobs[a].CreatedAt.Before(jobs[b].CreatedAt) })
		out[s.Repo] = jobs[min(n, len(jobs)):]
	}
	return out
}

func removeRepo(repos []config.Repo, name string) []config.Repo {
	var out []config.Repo
	for _, r := range repos {
		if !strings.EqualFold(r.Name, name) {
			out = append(out, r)
		}
	}
	return out
}
