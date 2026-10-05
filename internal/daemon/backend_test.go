package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/events"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/history"
	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/runner"
)

type fakeManager struct {
	insts    []model.InstanceStatus
	cleared  bool
	killed   []string
	pruneErr error
	prunes   int
	degraded string
}

func (f *fakeManager) StartPrune() error {
	f.prunes++
	return f.pruneErr
}

func (f *fakeManager) Status() model.Status {
	return model.Status{Instances: f.insts, Degraded: f.degraded != "", DegradedReason: f.degraded}
}
func (f *fakeManager) RunnerLog(id, cursor string) (model.LogChunk, error) {
	return model.LogChunk{}, runner.ErrUnknownRunner(id)
}
func (f *fakeManager) RunnerContainers(ctx context.Context, id string) ([]model.Container, error) {
	return nil, runner.ErrUnknownRunner(id)
}
func (f *fakeManager) RunnerRepoAndRun(id string) (string, int64, string, error) {
	if id != "aaaaaa" {
		return "", 0, "", runner.ErrUnknownRunner(id)
	}
	return "darkcloud", 55, "ghr-darkcloud-aaaaaa", nil
}
func (f *fakeManager) Kill(ctx context.Context, id string) error {
	f.killed = append(f.killed, id)
	return nil
}
func (f *fakeManager) ClearDegraded() { f.cleared = true }

type fakeGH struct {
	repos  map[string]*github.Repository
	forgot bool
	meta   github.TokenMeta
}

func (f *fakeGH) TokenMeta() github.TokenMeta { return f.meta }

func (f *fakeGH) GetRepo(ctx context.Context, repo string) (*github.Repository, error) {
	r, ok := f.repos[repo]
	if !ok {
		return nil, &github.APIError{Status: 404, Kind: github.ErrNotFound}
	}
	return r, nil
}
func (f *fakeGH) ListJobs(ctx context.Context, repo string, runID int64) ([]github.Job, error) {
	return []github.Job{
		{RunnerName: "someone-else", Steps: []github.Step{{Name: "x"}}},
		{RunnerName: "ghr-darkcloud-aaaaaa", Steps: []github.Step{{Number: 1, Name: "checkout", Status: "completed", Conclusion: "success"}}},
	}, nil
}
func (f *fakeGH) ForgetCache() { f.forgot = true }

func newBackend(t *testing.T) (*Backend, *fakeManager, *fakeGH) {
	t.Helper()
	m := &fakeManager{}
	gh := &fakeGH{repos: map[string]*github.Repository{
		"newrepo": {Private: true}, "public": {Private: false}, "darkcloud": {Private: true},
	}}
	b := &Backend{
		Store: newStore(t), M: m, GH: gh, Events: events.New(),
		Hist: &history.Store{Path: filepath.Join(t.TempDir(), "h.jsonl")},
		CheckToken: func(ctx context.Context, token, repo string) error {
			if token == "bad" {
				return errors.New("401 Bad credentials")
			}
			return nil
		},
	}
	return b, m, gh
}

func apiStatus(err error) int {
	var ae *api.Error
	if errors.As(err, &ae) {
		return ae.Status
	}
	return 0
}

func TestPatchConfig(t *testing.T) {
	b, _, _ := newBackend(t)
	all, two, idle := "all", 2, "10m"
	paused := true
	if err := b.PatchConfig(model.ConfigPatch{Mode: &all, IdleTimeout: &idle, Repos: map[string]model.RepoPatch{"darkmem": {Max: &two, Paused: &paused}}}); err != nil {
		t.Fatal(err)
	}
	c := b.Store.Config()
	if c.Mode != "all" || c.IdleTimeout.String() != "10m0s" || *c.Repo("darkmem").Max != 2 || !c.Repo("darkmem").Paused {
		t.Fatalf("cfg %+v", c)
	}
	bad := "fast"
	if err := b.PatchConfig(model.ConfigPatch{Mode: &bad}); apiStatus(err) != 400 {
		t.Fatalf("bad mode err %v", err)
	}
	if err := b.PatchConfig(model.ConfigPatch{Repos: map[string]model.RepoPatch{"nope": {Max: &two}}}); apiStatus(err) != 404 {
		t.Fatalf("unknown repo err %v", err)
	}
}

// Every setting except owner can be patched live, and the change is persisted.
func TestPatchConfigSettings(t *testing.T) {
	b, _, _ := newBackend(t)
	poll, retention, keep := "30s", "14d", "10GB"
	mem, cpu, disk := "4G", "150%", 70
	labels := []string{"homelab", "docker"}
	if err := b.PatchConfig(model.ConfigPatch{PollInterval: &poll, HistoryRetention: &retention, DiskHighWater: &disk,
		BuildCacheKeep: &keep, Labels: &labels, RunnerLimits: &model.RunnerLimitsPatch{MemoryMax: &mem, CPUQuota: &cpu}}); err != nil {
		t.Fatal(err)
	}
	c := b.Store.Config()
	if c.PollInterval.String() != "30s" || c.HistoryRetention.String() != "14d" || c.DiskHighWater != 70 || c.BuildCacheKeep != "10GB" ||
		!reflect.DeepEqual(c.Labels, labels) || c.RunnerLimits != (config.RunnerLimits{MemoryMax: "4G", CPUQuota: "150%"}) {
		t.Fatalf("cfg %+v", c)
	}
	saved, _, err := config.Load(b.Store.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if saved.PollInterval != c.PollInterval || saved.HistoryRetention != c.HistoryRetention || saved.DiskHighWater != 70 ||
		saved.BuildCacheKeep != "10GB" || !reflect.DeepEqual(saved.Labels, labels) || saved.RunnerLimits != c.RunnerLimits {
		t.Fatalf("not persisted: %+v", saved)
	}
	// A partial runner_limits patch leaves the other limit alone.
	mem = "8G"
	if err := b.PatchConfig(model.ConfigPatch{RunnerLimits: &model.RunnerLimitsPatch{MemoryMax: &mem}}); err != nil {
		t.Fatal(err)
	}
	if rl := b.Store.Config().RunnerLimits; rl != (config.RunnerLimits{MemoryMax: "8G", CPUQuota: "150%"}) {
		t.Fatalf("limits %+v", rl)
	}
}

// The JSON keys clients send (PATCH /config decodes straight into ConfigPatch)
// reach the new fields; a wrong or missing struct tag fails here.
func TestPatchConfigJSONKeys(t *testing.T) {
	b, _, _ := newBackend(t)
	body := `{"poll_interval":"20s","history_retention":"7d","disk_high_water":75,"build_cache_keep":"5GB",` +
		`"labels":["homelab","x"],"runner_limits":{"memory_max":"2G","cpu_quota":"100%"}}`
	var p model.ConfigPatch
	if err := json.Unmarshal([]byte(body), &p); err != nil {
		t.Fatal(err)
	}
	if err := b.PatchConfig(p); err != nil {
		t.Fatal(err)
	}
	c := b.Store.Config()
	if c.PollInterval.String() != "20s" || c.HistoryRetention.String() != "7d" || c.DiskHighWater != 75 || c.BuildCacheKeep != "5GB" ||
		!reflect.DeepEqual(c.Labels, []string{"homelab", "x"}) || c.RunnerLimits != (config.RunnerLimits{MemoryMax: "2G", CPUQuota: "100%"}) {
		t.Fatalf("cfg %+v", c)
	}
}

// An invalid setting is a 400 that names the problem and leaves config.yaml untouched.
func TestPatchConfigRejectsInvalidSettings(t *testing.T) {
	zero, size, cpu, mem, dur := 0, "lots", "fast", "6 gigs", "soon"
	fast, brief := "1ms", "1s"
	empty := []string{}
	cases := []struct {
		name string
		p    model.ConfigPatch
		msg  string
	}{
		{"disk", model.ConfigPatch{DiskHighWater: &zero}, "disk_high_water must be 1..100"},
		{"size", model.ConfigPatch{BuildCacheKeep: &size}, "build_cache_keep must look like 20GB"},
		{"cpu", model.ConfigPatch{RunnerLimits: &model.RunnerLimitsPatch{CPUQuota: &cpu}}, "runner_limits.cpu_quota"},
		{"memory", model.ConfigPatch{RunnerLimits: &model.RunnerLimitsPatch{MemoryMax: &mem}}, "runner_limits.memory_max"},
		{"poll", model.ConfigPatch{PollInterval: &dur}, `poll_interval: invalid duration "soon"`},
		{"start", model.ConfigPatch{StartTimeout: &dur}, `start_timeout: invalid duration "soon"`},
		{"idle", model.ConfigPatch{IdleTimeout: &dur}, `idle_timeout: invalid duration "soon"`},
		{"retention", model.ConfigPatch{HistoryRetention: &dur}, `history_retention: invalid duration "soon"`},
		{"labels", model.ConfigPatch{Labels: &empty}, "needs at least one label"},
		{"poll floor", model.ConfigPatch{PollInterval: &fast}, "poll_interval must be >= 5s"},
		{"retention floor", model.ConfigPatch{HistoryRetention: &brief}, "history_retention must be >= 1d"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, _, _ := newBackend(t)
			before, err := os.ReadFile(b.Store.ConfigPath)
			if err != nil {
				t.Fatal(err)
			}
			err = b.PatchConfig(tc.p)
			if apiStatus(err) != 400 || !strings.Contains(err.Error(), tc.msg) {
				t.Fatalf("err %v, want 400 containing %q", err, tc.msg)
			}
			after, err := os.ReadFile(b.Store.ConfigPath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Fatalf("config.yaml changed:\n%s", after)
			}
		})
	}
}

// With several bad durations, the first in the order poll_interval,
// start_timeout, idle_timeout, history_retention is the one reported.
func TestPatchConfigReportsFirstBadDuration(t *testing.T) {
	bad := "x"
	cases := []struct {
		p    model.ConfigPatch
		want string
	}{
		{model.ConfigPatch{PollInterval: &bad, StartTimeout: &bad, IdleTimeout: &bad, HistoryRetention: &bad}, "poll_interval: "},
		{model.ConfigPatch{StartTimeout: &bad, IdleTimeout: &bad, HistoryRetention: &bad}, "start_timeout: "},
		{model.ConfigPatch{IdleTimeout: &bad, HistoryRetention: &bad}, "idle_timeout: "},
		{model.ConfigPatch{HistoryRetention: &bad, StartTimeout: &bad}, "start_timeout: "},
	}
	b, _, _ := newBackend(t)
	for _, tc := range cases {
		// Repeated, because a map-ordered loop would pass a single run by luck.
		for i := 0; i < 20; i++ {
			err := b.PatchConfig(tc.p)
			if apiStatus(err) != 400 || !strings.HasPrefix(err.Error(), tc.want) {
				t.Fatalf("err %v, want 400 starting %q", err, tc.want)
			}
		}
	}
}

func TestAddRepo(t *testing.T) {
	b, _, _ := newBackend(t)
	ctx := context.Background()
	if err := b.AddRepo(ctx, model.AddRepoRequest{Name: "missing"}); apiStatus(err) != 400 || !strings.Contains(err.Error(), "repository access") {
		t.Fatalf("missing err %v", err)
	}
	if err := b.AddRepo(ctx, model.AddRepoRequest{Name: "public"}); apiStatus(err) != 409 {
		t.Fatalf("public err %v", err)
	}
	if err := b.AddRepo(ctx, model.AddRepoRequest{Name: "darkcloud"}); apiStatus(err) != 409 {
		t.Fatalf("duplicate err %v", err)
	}
	if err := b.AddRepo(ctx, model.AddRepoRequest{Name: "public", AllowPublic: true}); err != nil {
		t.Fatal(err)
	}
	if err := b.AddRepo(ctx, model.AddRepoRequest{Name: "newrepo", Labels: []string{"x"}}); err != nil {
		t.Fatal(err)
	}
	if b.Store.Config().Repo("newrepo") == nil {
		t.Fatal("repo not saved")
	}
}

func TestRemoveRepoWaitsForRunners(t *testing.T) {
	b, m, _ := newBackend(t)
	m.insts = []model.InstanceStatus{{ID: "aaaaaa", Repo: "darkmem"}}
	if err := b.RemoveRepo("darkmem"); err != nil {
		t.Fatal(err)
	}
	if !b.Store.Config().Repo("darkmem").Paused {
		t.Fatal("not paused")
	}
	b.FinalizeRemovals()
	if b.Store.Config().Repo("darkmem") == nil {
		t.Fatal("removed while a runner was live")
	}
	m.insts = nil
	b.FinalizeRemovals()
	if b.Store.Config().Repo("darkmem") != nil {
		t.Fatal("not removed after runners finished")
	}
	if err := b.RemoveRepo("nope"); apiStatus(err) != 404 {
		t.Fatalf("unknown repo err %v", err)
	}
}

// The removal intent lives in config.yaml: a restarted daemon (a new Store and
// Backend over the same files) still finishes it.
func TestRemovalSurvivesRestart(t *testing.T) {
	b, m, _ := newBackend(t)
	m.insts = []model.InstanceStatus{{ID: "aaaaaa", Repo: "darkmem"}}
	if err := b.RemoveRepo("darkmem"); err != nil {
		t.Fatal(err)
	}
	store, _, err := OpenStore(b.Store.ConfigPath, b.Store.TokenPath)
	if err != nil {
		t.Fatal(err)
	}
	if r := store.Config().Repo("darkmem"); r == nil || !r.Paused || !r.Removing {
		t.Fatalf("removal intent not persisted: %+v", r)
	}
	restarted := &Backend{Store: store, M: &fakeManager{}, GH: b.GH, Events: events.New(), Hist: b.Hist}
	restarted.FinalizeRemovals()
	if store.Config().Repo("darkmem") != nil {
		t.Fatal("restarted daemon did not finish the removal")
	}
}

func TestResumeCancelsRemoval(t *testing.T) {
	b, m, _ := newBackend(t)
	m.insts = []model.InstanceStatus{{ID: "aaaaaa", Repo: "darkmem"}}
	b.RemoveRepo("darkmem")
	resume := false
	if err := b.PatchConfig(model.ConfigPatch{Repos: map[string]model.RepoPatch{"darkmem": {Paused: &resume}}}); err != nil {
		t.Fatal(err)
	}
	m.insts = nil
	b.FinalizeRemovals()
	if r := b.Store.Config().Repo("darkmem"); r == nil || r.Paused || r.Removing {
		t.Fatalf("resumed repo removed or still marked: %+v", r)
	}
}

// A rejected patch changes nothing, including a pending removal.
func TestRejectedPatchKeepsRemoval(t *testing.T) {
	b, _, _ := newBackend(t)
	b.RemoveRepo("darkmem")
	resume, zero := false, 0
	err := b.PatchConfig(model.ConfigPatch{GlobalMax: &zero, Repos: map[string]model.RepoPatch{"darkmem": {Paused: &resume}}})
	if apiStatus(err) != 400 {
		t.Fatalf("err %v", err)
	}
	if r := b.Store.Config().Repo("darkmem"); !r.Paused || !r.Removing {
		t.Fatalf("removal lost by a rejected patch: %+v", r)
	}
}

func TestResumeAllKeepsRemovingReposPaused(t *testing.T) {
	b, _, _ := newBackend(t)
	b.SetPausedAll(true)
	b.RemoveRepo("darkmem")
	if err := b.SetPausedAll(false); err != nil {
		t.Fatal(err)
	}
	c := b.Store.Config()
	if c.Repo("darkcloud").Paused || !c.Repo("darkmem").Paused || !c.Repo("darkmem").Removing {
		t.Fatalf("repos %+v", c.Repos)
	}
}

func TestConfigChangesWakeTheLoop(t *testing.T) {
	b, _, _ := newBackend(t)
	wakes := 0
	b.Wake = func() { wakes++ }
	b.RemoveRepo("darkmem")
	b.SetPausedAll(true)
	zero := 0
	b.PatchConfig(model.ConfigPatch{GlobalMax: &zero}) // rejected: no wake
	if wakes != 2 {
		t.Fatalf("wakes = %d", wakes)
	}
}

func TestPauseAllStepsKillToken(t *testing.T) {
	b, m, gh := newBackend(t)
	ctx := context.Background()
	if err := b.SetPausedAll(true); err != nil {
		t.Fatal(err)
	}
	for _, r := range b.Store.Config().Repos {
		if !r.Paused {
			t.Fatalf("%s not paused", r.Name)
		}
	}
	steps, err := b.RunnerSteps(ctx, "aaaaaa")
	if err != nil || len(steps) != 1 || steps[0].Name != "checkout" {
		t.Fatalf("steps %+v err %v", steps, err)
	}
	if _, err := b.RunnerSteps(ctx, "zzzzzz"); apiStatus(err) != 404 {
		t.Fatalf("unknown runner err %v", err)
	}
	if _, err := b.RunnerLog("zzzzzz", ""); apiStatus(err) != 404 {
		t.Fatalf("log err %v", err)
	}
	if _, err := b.RunnerContainers(ctx, "zzzzzz"); apiStatus(err) != 404 {
		t.Fatalf("containers err %v", err)
	}
	if err := b.KillRunner(ctx, "aaaaaa"); err != nil || m.killed[0] != "aaaaaa" {
		t.Fatalf("kill err %v", err)
	}
	if err := b.SetToken(ctx, "bad"); apiStatus(err) != 400 || b.Store.Token() != "tok1" {
		t.Fatalf("bad token err %v token %q", err, b.Store.Token())
	}
	if err := b.SetToken(ctx, "tok2"); err != nil {
		t.Fatal(err)
	}
	if b.Store.Token() != "tok2" || !gh.forgot || !m.cleared {
		t.Fatal("token not applied")
	}
}

func TestReloadAppliesWakesAndRejects(t *testing.T) {
	b, m, gh := newBackend(t)
	woke := 0
	b.Wake = func() { woke++ }
	ws, err := b.Reload()
	if err != nil || ws == nil || woke != 1 || !gh.forgot || !m.cleared {
		t.Fatalf("reload: %v %v woke=%d forgot=%v cleared=%v", ws, err, woke, gh.forgot, m.cleared)
	}
	if err := os.WriteFile(b.Store.TokenPath, []byte("  \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Reload(); apiStatus(err) != 400 || woke != 1 {
		t.Fatalf("empty token: %v woke=%d", err, woke)
	}
	var txt strings.Builder
	for _, e := range b.Events.After(0) {
		txt.WriteString(e.Msg + "\n")
	}
	if !strings.Contains(txt.String(), "config and token reloaded") || !strings.Contains(txt.String(), "reload rejected") {
		t.Fatalf("events:\n%s", txt.String())
	}
}

func TestPruneStartsOrConflicts(t *testing.T) {
	b, m, _ := newBackend(t)
	if err := b.Prune(); err != nil || m.prunes != 1 {
		t.Fatalf("prune: %v %d", err, m.prunes)
	}
	m.pruneErr = runner.ErrPruneRunning
	if err := b.Prune(); apiStatus(err) != 409 {
		t.Fatalf("overlap: %v", err)
	}
}

func TestTokenStates(t *testing.T) {
	b, m, gh := newBackend(t)
	if ts := b.Token(); ts.State != "unverified" || ts.CheckedAt != nil {
		t.Fatalf("fresh: %+v", ts)
	}
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	exp := now.Add(80 * 24 * time.Hour)
	rem := 4800
	gh.meta = github.TokenMeta{CheckedAt: now, OK: true, RateRemaining: &rem, ExpiresAt: &exp}
	if ts := b.Token(); ts.State != "ok" || !ts.CheckedAt.Equal(now) || *ts.RateRemaining != 4800 || !ts.ExpiresAt.Equal(exp) {
		t.Fatalf("ok: %+v", ts)
	}
	m.degraded = "GitHub rejected the token"
	if ts := b.Token(); ts.State != "rejected" || ts.Reason != "GitHub rejected the token" {
		t.Fatalf("degraded: %+v", ts)
	}
	m.degraded = ""
	gh.meta.OK = false
	if ts := b.Token(); ts.State != "rejected" {
		t.Fatalf("last call rejected: %+v", ts)
	}
}
