package sched

import (
	"reflect"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/config"
)

var t0 = time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)

func ptr(n int) *int { return &n }

func cfg(mode string, globalMax int, repos ...config.Repo) *config.Config {
	return &config.Config{
		Owner: "o", Mode: mode, GlobalMax: globalMax, Labels: []string{"homelab"},
		IdleTimeout: config.Duration(5 * time.Minute), StartTimeout: config.Duration(2 * time.Minute),
		Repos: repos,
	}
}

func jobs(repo string, minutesAgo ...int) []QueuedJob {
	var out []QueuedJob
	for i, m := range minutesAgo {
		out = append(out, QueuedJob{Repo: repo, ID: int64(i + 1), CreatedAt: t0.Add(-time.Duration(m) * time.Minute)})
	}
	return out
}

func repos(spawns []Spawn) []string {
	out := []string{}
	for _, s := range spawns {
		out = append(out, s.Repo)
	}
	return out
}

func TestPlan(t *testing.T) {
	a := config.Repo{Name: "a"}
	b := config.Repo{Name: "b"}
	tests := []struct {
		name   string
		cfg    *config.Config
		insts  []Instance
		demand Demand
		want   []string
	}{
		{"queue: global cap stops spawning", cfg("queue", 2, config.Repo{Name: "a", Max: ptr(0)}), nil,
			Demand{"a": jobs("a", 3, 2, 1)}, []string{"a", "a"}},
		{"queue: repo default max 1", cfg("queue", 5, a), nil,
			Demand{"a": jobs("a", 3, 2)}, []string{"a"}},
		{"queue: cross-repo FIFO", cfg("queue", 1, a, b), nil,
			Demand{"a": jobs("a", 1), "b": jobs("b", 9)}, []string{"b"}},
		{"queue: capped head is skipped, next repo served", cfg("queue", 3, a, b),
			[]Instance{{ID: "1", Repo: "a", State: Busy, StateSince: t0.Add(-time.Hour), JobConfirmed: true}},
			Demand{"a": jobs("a", 9), "b": jobs("b", 1)}, []string{"b"}},
		{"queue: idle instance covers a job", cfg("queue", 3, config.Repo{Name: "a", Max: ptr(3)}),
			[]Instance{{ID: "1", Repo: "a", State: Idle, StateSince: t0}},
			Demand{"a": jobs("a", 2, 1)}, []string{"a"}},
		{"queue: unconfirmed busy covers its job (no extra spawn)", cfg("queue", 3, config.Repo{Name: "a", Max: ptr(3)}),
			[]Instance{{ID: "1", Repo: "a", State: Busy, StateSince: t0.Add(-10 * time.Second)}},
			Demand{"a": jobs("a", 1)}, []string{}},
		{"queue: unconfirmed busy stops covering after grace", cfg("queue", 3, config.Repo{Name: "a", Max: ptr(3)}),
			[]Instance{{ID: "1", Repo: "a", State: Busy, StateSince: t0.Add(-2 * time.Minute)}},
			Demand{"a": jobs("a", 1)}, []string{"a"}},
		{"queue: cleaning counts toward repo cap", cfg("queue", 3, a),
			[]Instance{{ID: "1", Repo: "a", State: Cleaning, StateSince: t0}},
			Demand{"a": jobs("a", 1)}, []string{}},
		{"queue: cleaning does not count toward global cap", cfg("queue", 1, a, b),
			[]Instance{{ID: "1", Repo: "a", State: Cleaning, StateSince: t0}},
			Demand{"b": jobs("b", 1)}, []string{"b"}},
		{"queue: paused repo ignored", cfg("queue", 3, config.Repo{Name: "a", Paused: true}), nil,
			Demand{"a": jobs("a", 1)}, []string{}},
		{"all: warm runner kept without demand", cfg("all", 1, a, b), nil,
			Demand{}, []string{"a", "b"}},
		{"all: no global cap, unlimited by default", cfg("all", 1, config.Repo{Name: "a", Warm: ptr(0)}), nil,
			Demand{"a": jobs("a", 3, 2, 1)}, []string{"a", "a", "a"}},
		{"all: explicit max limits", cfg("all", 1, config.Repo{Name: "a", Max: ptr(2), Warm: ptr(1)}),
			[]Instance{{ID: "1", Repo: "a", State: Busy, StateSince: t0.Add(-time.Hour), JobConfirmed: true}},
			Demand{"a": jobs("a", 3, 2)}, []string{"a"}},
		{"all: warm satisfied by idle", cfg("all", 1, a),
			[]Instance{{ID: "1", Repo: "a", State: Idle, StateSince: t0}},
			Demand{}, []string{}},
		{"all: stale idle neither covers nor counts as warm", cfg("all", 1, a),
			[]Instance{{ID: "1", Repo: "a", State: Idle, StateSince: t0, Stale: true}},
			Demand{"a": jobs("a", 1)}, []string{"a"}},
		{"queue: stale idle does not cover but holds the repo cap", cfg("queue", 3, config.Repo{Name: "a", Max: ptr(2)}),
			[]Instance{{ID: "1", Repo: "a", State: Idle, StateSince: t0, Stale: true}},
			Demand{"a": jobs("a", 2, 1)}, []string{"a"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := repos(Plan(tc.cfg, tc.insts, tc.demand, t0))
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestMatchLabels(t *testing.T) {
	eff := []string{"self-hosted", "linux", "x64", "homelab"}
	if !MatchLabels([]string{"Self-Hosted", "Linux", "X64"}, eff) {
		t.Fatal("system labels should match case-insensitively")
	}
	if MatchLabels([]string{"ubuntu-latest"}, eff) {
		t.Fatal("hosted label must not match")
	}
}

func TestIdleToStop(t *testing.T) {
	idle := func(id, repo string, ago time.Duration) Instance {
		return Instance{ID: id, Repo: repo, State: Idle, StateSince: t0.Add(-ago)}
	}
	q := cfg("queue", 2, config.Repo{Name: "a"}, config.Repo{Name: "p", Paused: true})
	got := IdleToStop(q, []Instance{idle("old", "a", 6*time.Minute), idle("new", "a", time.Minute), idle("pz", "p", 0)}, t0)
	if !reflect.DeepEqual(got, []string{"old", "pz"}) {
		t.Fatalf("queue: got %v", got)
	}
	al := cfg("all", 2, config.Repo{Name: "a", Warm: ptr(1)})
	got = IdleToStop(al, []Instance{idle("warm", "a", time.Hour), idle("extra", "a", 10*time.Minute), idle("fresh", "a", time.Minute)}, t0)
	if !reflect.DeepEqual(got, []string{"extra"}) {
		t.Fatalf("all: got %v", got)
	}
	got = IdleToStop(al, []Instance{idle("warm", "a", time.Hour), idle("older", "a", 20*time.Minute), idle("newer", "a", 10*time.Minute)}, t0)
	if !reflect.DeepEqual(got, []string{"newer", "older"}) {
		t.Fatalf("all: surplus must stop newest first, got %v", got)
	}
	stale := idle("stale", "a", 0)
	stale.Stale = true
	got = IdleToStop(al, []Instance{idle("warm", "a", time.Hour), stale}, t0)
	if !reflect.DeepEqual(got, []string{"stale"}) {
		t.Fatalf("stale idle must stop at once, got %v", got)
	}
	got = IdleToStop(q, []Instance{idle("gone", "removed-repo", 0)}, t0)
	if !reflect.DeepEqual(got, []string{"gone"}) {
		t.Fatalf("unconfigured repo: got %v", got)
	}
}

func TestStartTimedOut(t *testing.T) {
	c := cfg("queue", 2, config.Repo{Name: "a"})
	got := StartTimedOut(c, []Instance{
		{ID: "late", Repo: "a", State: Starting, StateSince: t0.Add(-3 * time.Minute)},
		{ID: "ok", Repo: "a", State: Starting, StateSince: t0.Add(-time.Minute)},
		{ID: "idle", Repo: "a", State: Idle, StateSince: t0.Add(-time.Hour)},
	}, t0)
	if !reflect.DeepEqual(got, []string{"late"}) {
		t.Fatalf("got %v", got)
	}
}

func TestRepoNamesIgnoreCase(t *testing.T) {
	busy := Instance{ID: "1", Repo: "a", State: Busy, StateSince: t0.Add(-time.Hour), JobConfirmed: true}
	if got := repos(Plan(cfg("queue", 2, config.Repo{Name: "A"}), []Instance{busy}, Demand{"A": jobs("A", 1)}, t0)); len(got) != 0 {
		t.Fatalf("queue: repo cap must count a differently-cased instance, got %v", got)
	}
	idle := Instance{ID: "1", Repo: "a", State: Idle, StateSince: t0}
	if got := repos(Plan(cfg("all", 1, config.Repo{Name: "A"}), []Instance{idle}, Demand{}, t0)); len(got) != 0 {
		t.Fatalf("all: a differently-cased idle instance must satisfy warm, got %v", got)
	}
	al := cfg("all", 2, config.Repo{Name: "A", Warm: ptr(1)})
	got := IdleToStop(al, []Instance{
		{ID: "warm", Repo: "a", State: Idle, StateSince: t0.Add(-time.Hour)},
		{ID: "extra", Repo: "A", State: Idle, StateSince: t0.Add(-10 * time.Minute)},
	}, t0)
	if !reflect.DeepEqual(got, []string{"extra"}) {
		t.Fatalf("idle grouping must ignore case, got %v", got)
	}
}
