// Package sched holds the pure scheduling decisions: what to spawn and what to stop.
package sched

import (
	"sort"
	"strings"
	"time"

	"github.com/darkraise/ghr/internal/config"
)

type State string

const (
	Starting State = "starting"
	Idle     State = "idle"
	Busy     State = "busy"
	Cleaning State = "cleaning"
)

// BusyCoverGrace bounds how long a busy instance whose job the API has not yet
// reported in_progress keeps covering a queued job.
const BusyCoverGrace = 60 * time.Second

type Instance struct {
	ID           string
	Repo         string
	State        State
	StateSince   time.Time
	JobConfirmed bool
	// Stale marks a runner registered with labels that differ from its repo's
	// current labels: it may be unable to take the jobs demand now counts.
	Stale bool
}

type QueuedJob struct {
	Repo      string
	ID        int64
	CreatedAt time.Time
}

// Demand maps repo name to its matching queued jobs.
type Demand map[string][]QueuedJob

type Spawn struct {
	Repo string
}

// MatchLabels reports whether every job label is in the runner's effective set (case-insensitive).
func MatchLabels(jobLabels, effective []string) bool {
	set := map[string]bool{}
	for _, l := range effective {
		set[strings.ToLower(l)] = true
	}
	for _, l := range jobLabels {
		if !set[strings.ToLower(l)] {
			return false
		}
	}
	return true
}

func covers(i Instance, now time.Time) bool {
	if i.Stale {
		return false
	}
	switch i.State {
	case Starting, Idle:
		return true
	case Busy:
		return !i.JobConfirmed && now.Sub(i.StateSince) < BusyCoverGrace
	}
	return false
}

// Covering counts instances of repo expected to take a queued job.
func Covering(insts []Instance, repo string, now time.Time) int {
	n := 0
	for _, i := range insts {
		if i.Repo == repo && covers(i, now) {
			n++
		}
	}
	return n
}

// Uncovered returns the repo's queued jobs (oldest first) not covered by an instance.
func Uncovered(insts []Instance, repo string, jobs []QueuedJob, now time.Time) []QueuedJob {
	sorted := append([]QueuedJob{}, jobs...)
	sort.SliceStable(sorted, func(a, b int) bool { return sorted[a].CreatedAt.Before(sorted[b].CreatedAt) })
	c := Covering(insts, repo, now)
	if c >= len(sorted) {
		return nil
	}
	return sorted[c:]
}

func repoActive(insts []Instance, repo string) int {
	n := 0
	for _, i := range insts {
		if i.Repo == repo {
			n++ // starting, idle, busy and cleaning all count toward the repo cap
		}
	}
	return n
}

func totalActive(insts []Instance) int {
	n := 0
	for _, i := range insts {
		if i.State != Cleaning {
			n++
		}
	}
	return n
}

// countStates counts the repo's non-stale instances in the given states.
func countStates(insts []Instance, repo string, states ...State) int {
	n := 0
	for _, i := range insts {
		if i.Repo != repo || i.Stale {
			continue
		}
		for _, s := range states {
			if i.State == s {
				n++
			}
		}
	}
	return n
}

// Plan decides which runners to spawn this tick.
func Plan(cfg *config.Config, insts []Instance, demand Demand, now time.Time) []Spawn {
	if cfg.Mode == config.ModeAll {
		return planAll(cfg, insts, demand, now)
	}
	return planQueue(cfg, insts, demand, now)
}

func planQueue(cfg *config.Config, insts []Instance, demand Demand, now time.Time) []Spawn {
	var cands []QueuedJob
	for _, r := range cfg.Repos {
		if r.Paused {
			continue
		}
		for _, j := range Uncovered(insts, r.Name, demand[r.Name], now) {
			j.Repo = r.Name
			cands = append(cands, j)
		}
	}
	sort.SliceStable(cands, func(a, b int) bool { return cands[a].CreatedAt.Before(cands[b].CreatedAt) })
	active := map[string]int{}
	for _, r := range cfg.Repos {
		active[r.Name] = repoActive(insts, r.Name)
	}
	total := totalActive(insts)
	var out []Spawn
	for _, j := range cands {
		if total >= cfg.GlobalMax {
			break
		}
		max := cfg.EffectiveMax(*cfg.Repo(j.Repo))
		if max > 0 && active[j.Repo] >= max {
			continue
		}
		out = append(out, Spawn{Repo: j.Repo})
		active[j.Repo]++
		total++
	}
	return out
}

func planAll(cfg *config.Config, insts []Instance, demand Demand, now time.Time) []Spawn {
	var out []Spawn
	for _, r := range cfg.Repos {
		if r.Paused {
			continue
		}
		uncovered := len(Uncovered(insts, r.Name, demand[r.Name], now))
		warmNeed := cfg.EffectiveWarm(r) - countStates(insts, r.Name, Starting, Idle)
		n := uncovered
		if warmNeed > n {
			n = warmNeed
		}
		if max := cfg.EffectiveMax(r); max > 0 && n > max-repoActive(insts, r.Name) {
			n = max - repoActive(insts, r.Name)
		}
		for ; n > 0; n-- {
			out = append(out, Spawn{Repo: r.Name})
		}
	}
	return out
}

// IdleToStop returns the IDs of idle instances to stop: past idle_timeout in queue mode,
// past idle_timeout and beyond the warm count in all mode (newest first), and every
// stale idle instance or idle instance of a paused (or unconfigured) repo.
func IdleToStop(cfg *config.Config, insts []Instance, now time.Time) []string {
	byRepo := map[string][]Instance{}
	var out []string
	for _, i := range insts {
		if i.State != Idle {
			continue
		}
		if i.Stale {
			out = append(out, i.ID)
			continue
		}
		byRepo[i.Repo] = append(byRepo[i.Repo], i)
	}
	timeout := cfg.IdleTimeout.D()
	repos := make([]string, 0, len(byRepo))
	for name := range byRepo {
		repos = append(repos, name)
	}
	sort.Strings(repos)
	for _, name := range repos {
		idle := byRepo[name]
		sort.SliceStable(idle, func(a, b int) bool { return idle[a].StateSince.Before(idle[b].StateSince) })
		r := cfg.Repo(name)
		if r == nil || r.Paused {
			for _, i := range idle {
				out = append(out, i.ID)
			}
			continue
		}
		keep := 0
		if cfg.Mode == config.ModeAll {
			keep = cfg.EffectiveWarm(*r)
		}
		// The oldest idle instances are the warm ones; surplus stops newest first.
		for idx := len(idle) - 1; idx >= keep; idx-- {
			if now.Sub(idle[idx].StateSince) >= timeout {
				out = append(out, idle[idx].ID)
			}
		}
	}
	return out
}

// StartTimedOut returns the IDs of instances stuck in starting past start_timeout.
func StartTimedOut(cfg *config.Config, insts []Instance, now time.Time) []string {
	var out []string
	for _, i := range insts {
		if i.State == Starting && now.Sub(i.StateSince) >= cfg.StartTimeout.D() {
			out = append(out, i.ID)
		}
	}
	return out
}
