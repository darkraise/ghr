package daemon

import (
	"context"
	"errors"
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/changes"
	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/metrics"
	"github.com/darkraise/ghr/internal/model"
)

// changed lists the topics whose fingerprints differ between two checks.
func changed(a, b map[string]uint64) []string {
	var out []string
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			out = append(out, k)
		}
	}
	for k := range b {
		if _, ok := a[k]; !ok {
			out = append(out, k)
		}
	}
	slices.Sort(out)
	return out
}

func TestFingerprintsCoverEveryTopic(t *testing.T) {
	b, _, _ := newBackend(t)
	got := slices.Sorted(maps.Keys(b.fingerprints()))
	want := []string{"config", "events", "gauges", "history", "metrics", "status", "storage", "token"}
	if !slices.Equal(got, want) {
		t.Fatalf("topics %v", got)
	}
}

func TestIdleCountersChangeOnlyGauges(t *testing.T) {
	b, m, gh := newBackend(t)
	now := time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC)
	limit := 5000
	gh.meta = github.TokenMeta{CheckedAt: now, OK: true, RateLimit: &limit}
	prev := b.fingerprints()
	for i := 1; i <= 3; i++ {
		rem := limit - i
		reset := now.Add(time.Duration(i) * time.Minute)
		m.rate, m.diskUsed, m.now = 4000-i, int64(1<<30+i), now.Add(time.Duration(i)*time.Second)
		gh.meta = github.TokenMeta{CheckedAt: now.Add(time.Duration(i) * time.Second), OK: true,
			RateLimit: &limit, RateRemaining: &rem, RateReset: &reset}
		cur := b.fingerprints()
		if got := changed(prev, cur); !slices.Equal(got, []string{"gauges"}) {
			t.Fatalf("check %d changed %v", i, got)
		}
		prev = cur
	}
}

// Every GitHub response moves the token's check time, several times a second
// while the daemon polls; on its own that must not publish anything.
func TestATokenCheckAloneChangesNoTopic(t *testing.T) {
	b, _, gh := newBackend(t)
	now := time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC)
	limit, rem := 5000, 4990
	reset := now.Add(time.Hour)
	gh.meta = github.TokenMeta{CheckedAt: now, OK: true, RateLimit: &limit, RateRemaining: &rem, RateReset: &reset}
	prev := b.fingerprints()
	gh.meta.CheckedAt = now.Add(400 * time.Millisecond)
	if got := changed(prev, b.fingerprints()); len(got) != 0 {
		t.Fatalf("a new check time changed %v", got)
	}
}

func TestStatusFingerprintIgnoresNow(t *testing.T) {
	b, m, _ := newBackend(t)
	prev := b.fingerprints()
	m.now = time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC)
	cur := b.fingerprints()
	if got := changed(prev, cur); len(got) != 0 {
		t.Fatalf("a new Now changed %v", got)
	}
	m.insts = []model.InstanceStatus{{ID: "r", Repo: "darkmem", State: "idle"}}
	if got := changed(cur, b.fingerprints()); !slices.Equal(got, []string{"status"}) {
		t.Fatalf("a new instance changed %v", got)
	}
}

func TestFingerprintsReadTheManagerStatusOnce(t *testing.T) {
	b, m, _ := newBackend(t)
	before := m.statusCalls.Load()
	b.fingerprints()
	if n := m.statusCalls.Load() - before; n != 1 {
		t.Fatalf("one check read the manager's status %d times", n)
	}
}

func TestStateChangesReachTheirTopics(t *testing.T) {
	b, _, _ := newBackend(t)
	prev := b.fingerprints()
	b.Events.Add("info", "", "something happened")
	cur := b.fingerprints()
	if got := changed(prev, cur); !slices.Equal(got, []string{"events"}) {
		t.Fatalf("an event changed %v", got)
	}
	if err := b.Hist.Append(model.HistoryEntry{ID: "a", Repo: "darkmem", RunID: 1}); err != nil {
		t.Fatal(err)
	}
	prev, cur = cur, b.fingerprints()
	if got := changed(prev, cur); !slices.Equal(got, []string{"history"}) {
		t.Fatalf("a history row changed %v", got)
	}
	three := 3
	if err := b.PatchConfig(context.Background(), model.ConfigPatch{GlobalMax: &three}); err != nil {
		t.Fatal(err)
	}
	if got := changed(cur, b.fingerprints()); !slices.Contains(got, "config") {
		t.Fatalf("a config change changed %v", got)
	}
}

func TestASampleMovesTheMetricsTopicAndAnIdleCheckDoesNot(t *testing.T) {
	b, _, _ := newBackend(t)
	b.Sampler = metrics.NewSampler(func() metrics.Snapshot { return metrics.Snapshot{} }, func() int { return 0 })
	prev := b.fingerprints()
	if got := changed(prev, b.fingerprints()); len(got) != 0 {
		t.Fatalf("an idle check changed %v", got)
	}
	b.Sampler.Sample()
	if got := changed(prev, b.fingerprints()); !slices.Equal(got, []string{"metrics"}) {
		t.Fatalf("a sample changed %v", got)
	}
}

func TestAStoppingFlagChangesOnlyStatus(t *testing.T) {
	b, m, _ := newBackend(t)
	m.insts = []model.InstanceStatus{{ID: "aaaaaa", Repo: "darkmem", State: "busy"}}
	prev := b.fingerprints()
	m.insts = []model.InstanceStatus{{ID: "aaaaaa", Repo: "darkmem", State: "busy", Stopping: true}}
	cur := b.fingerprints()
	if got := changed(prev, cur); !slices.Equal(got, []string{"status"}) {
		t.Fatalf("setting the flag changed %v", got)
	}
	m.insts = []model.InstanceStatus{{ID: "aaaaaa", Repo: "darkmem", State: "busy"}}
	if got := changed(cur, b.fingerprints()); !slices.Equal(got, []string{"status"}) {
		t.Fatalf("clearing the flag changed %v", got)
	}
}

type panickyStorage struct{ fakeStorage }

func (p *panickyStorage) Snapshot() model.Storage { panic("snapshot failed") }

type panickyManager struct{ *fakeManager }

func (panickyManager) Status() model.Status { panic("status failed") }

func TestAPanickingTopicIsLeftOut(t *testing.T) {
	b, m, _ := newBackend(t)
	b.Space = &panickyStorage{}
	fp := b.fingerprints()
	if _, ok := fp["storage"]; ok {
		t.Fatal("a panicking storage snapshot still has a fingerprint")
	}
	if _, ok := fp["status"]; !ok {
		t.Fatal("one topic's panic dropped another")
	}
	b.M = panickyManager{m}
	got := slices.Sorted(maps.Keys(b.fingerprints()))
	if !slices.Equal(got, []string{"config", "events", "history", "metrics"}) {
		t.Fatalf("with the manager status panicking: %v", got)
	}
}

func TestSetupFlagsAreReadOncePerSecond(t *testing.T) {
	b, _, _ := newBackend(t)
	now := time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC)
	b.Now = func() time.Time { return now }
	reads := 0
	b.WebSetupRequired = func() bool { reads++; return true }
	b.SetupPending = func() bool { return false }
	b.fingerprints()
	b.fingerprints()
	now = now.Add(999 * time.Millisecond)
	b.fingerprints()
	if reads != 1 {
		t.Fatalf("%d reads within a second, want 1", reads)
	}
	now = now.Add(time.Millisecond)
	b.fingerprints()
	if reads != 2 {
		t.Fatalf("%d reads after a second, want 2", reads)
	}
}

func TestSubscribeNeedsAHub(t *testing.T) {
	b, _, _ := newBackend(t)
	if _, err := b.Subscribe(); !errors.Is(err, changes.ErrNotReady) {
		t.Fatalf("without a hub: %v", err)
	}
	hub := changes.New(changes.Options{})
	t.Cleanup(hub.Close)
	hub.SetSources(b.fingerprints, b.staleActivity)
	b.Changes = hub
	s, err := b.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
}

func TestStatusFingerprintFollowsTheJobs(t *testing.T) {
	b, _, _ := newBackend(t)
	before := b.fingerprints()["status"]
	if err := b.Jobs.RunNow(config.JobPrune); err != nil {
		t.Fatal(err)
	}
	if b.fingerprints()["status"] == before {
		t.Fatal("a Run now left the status fingerprint unchanged")
	}
}
