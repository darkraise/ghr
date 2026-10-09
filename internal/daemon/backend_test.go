package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
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
	insts     []model.InstanceStatus
	cleared   bool
	killed    []string
	pruneErr  error
	prunes    int
	degraded  string
	updateErr error // returned by QueueUpdate and CancelUpdate
	updates   []string
	paused    time.Time        // returned by PausedUntil
	scopes    []string         // scopes StartPruneScope was asked for
	lastPrune *model.LastPrune // returned by LastPrune
	diskPct   int              // Status().DiskPct
	pending   []model.HistoryEntry
	onPending func() // runs when Pending is read, before it returns
}

func (f *fakeManager) PausedUntil() time.Time { return f.paused }

func (f *fakeManager) QueueUpdate(context.Context) error {
	f.updates = append(f.updates, "queue")
	return f.updateErr
}

func (f *fakeManager) CancelUpdate() error {
	f.updates = append(f.updates, "cancel")
	return f.updateErr
}

func (f *fakeManager) StartPruneScope(scope string) error {
	f.prunes++
	f.scopes = append(f.scopes, scope)
	return f.pruneErr
}

func (f *fakeManager) LastPrune() *model.LastPrune { return f.lastPrune }

func (f *fakeManager) Pending() []model.HistoryEntry {
	if f.onPending != nil {
		f.onPending()
	}
	return f.pending
}

func (f *fakeManager) Status() model.Status {
	return model.Status{Instances: f.insts, Degraded: f.degraded != "", DegradedReason: f.degraded, DiskPct: f.diskPct}
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
	repos    map[string]*github.Repository
	forgot   bool
	meta     github.TokenMeta
	runners  map[int64]github.Runner
	getErr   error
	delErr   error
	deleted  []int64
	getCalls int // GetRepo calls

	userRepos      []github.UserRepo
	repoErr        error // returned by ListUserRepos
	recent         []github.Run
	runs           map[string][]github.Run
	jobs           map[int64][]github.Job
	jobErr         map[int64]error // returned with that run's jobs
	block          bool            // ListJobs waits for the context to end
	listErr        error           // returned by ListRecentRuns
	gate           chan struct{}   // when set, ListJobs waits for it after recording the call
	onJobs         func()          // when set, ListJobs calls it after recording the call
	lmu            sync.Mutex      // guards the fields below, written by scan goroutines
	jobCalls       []int64
	inFlight       int                     // ListJobs calls currently blocked
	cancelled      int                     // blocked ListJobs calls ended by cancellation (not the deadline)
	recentBy       map[string][]github.Run // ListRecentRuns answers by repo, when set
	recentErr      map[string]error
	recentWait     chan struct{} // when set, ListRecentRuns waits for it or the context
	recentCalls    []string      // guarded by lmu
	recentN        []int         // the n of each call, guarded by lmu
	recentInFlight int           // guarded by lmu
	recentPeak     int           // guarded by lmu
}

func (f *fakeGH) TokenMeta() github.TokenMeta { return f.meta }

func (f *fakeGH) ListUserRepos(context.Context) ([]github.UserRepo, error) {
	return f.userRepos, f.repoErr
}

func (f *fakeGH) GetRepo(ctx context.Context, repo string) (*github.Repository, error) {
	f.getCalls++
	r, ok := f.repos[repo]
	if !ok {
		return nil, &github.APIError{Status: 404, Kind: github.ErrNotFound}
	}
	return r, nil
}
func (f *fakeGH) ListJobs(ctx context.Context, repo string, runID int64) ([]github.Job, error) {
	if f.block {
		f.lmu.Lock()
		f.inFlight++
		f.lmu.Unlock()
		<-ctx.Done()
		f.lmu.Lock()
		f.inFlight--
		if errors.Is(ctx.Err(), context.Canceled) {
			f.cancelled++
		}
		f.lmu.Unlock()
		return nil, ctx.Err()
	}
	f.lmu.Lock()
	f.jobCalls = append(f.jobCalls, runID)
	f.lmu.Unlock()
	if f.onJobs != nil {
		f.onJobs()
	}
	if f.gate != nil {
		<-f.gate
	}
	if f.jobs != nil {
		return f.jobs[runID], f.jobErr[runID]
	}
	return []github.Job{
		{RunnerName: "someone-else", Steps: []github.Step{{Name: "x"}}},
		{RunnerName: "ghr-darkcloud-aaaaaa", Steps: []github.Step{{Number: 1, Name: "checkout", Status: "completed", Conclusion: "success"}}},
	}, nil
}
func (f *fakeGH) ListRecentRuns(ctx context.Context, repo string, n int) ([]github.Run, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	if f.recentBy == nil && f.recentErr == nil && f.recentWait == nil {
		return f.recent, nil
	}
	f.lmu.Lock()
	f.recentCalls = append(f.recentCalls, repo)
	f.recentN = append(f.recentN, n)
	f.recentInFlight++
	f.recentPeak = max(f.recentPeak, f.recentInFlight)
	f.lmu.Unlock()
	defer func() {
		f.lmu.Lock()
		f.recentInFlight--
		f.lmu.Unlock()
	}()
	if f.recentWait != nil {
		select {
		case <-f.recentWait:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if err := f.recentErr[repo]; err != nil {
		return nil, err
	}
	return f.recentBy[repo], nil
}
func (f *fakeGH) ListRuns(ctx context.Context, repo, status string) ([]github.Run, error) {
	return f.runs[status], nil
}

// scanCounts waits up to 3 s for cond over (in flight, cancelled), failing the test otherwise.
func (f *fakeGH) scanCounts(t *testing.T, cond func(inFlight, cancelled int) bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		f.lmu.Lock()
		in, c := f.inFlight, f.cancelled
		f.lmu.Unlock()
		if cond(in, c) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("scan counts never matched: in flight %d, cancelled %d", in, c)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
func (f *fakeGH) ForgetCache() {
	f.forgot = true
	f.meta.Generation++
}

func (f *fakeGH) ListRunners(ctx context.Context, repo string) ([]github.Runner, error) {
	var out []github.Runner
	for _, id := range []int64{1, 2, 3, 4} {
		if r, ok := f.runners[id]; ok {
			out = append(out, r)
		}
	}
	return out, nil
}
func (f *fakeGH) GetRunner(ctx context.Context, repo string, id int64) (*github.Runner, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	r, ok := f.runners[id]
	if !ok {
		return nil, &github.APIError{Status: 404, Kind: github.ErrNotFound}
	}
	return &r, nil
}
func (f *fakeGH) DeleteRunner(ctx context.Context, repo string, id int64) error {
	if f.delErr != nil {
		return f.delErr
	}
	f.deleted = append(f.deleted, id)
	return nil
}

func newBackend(t *testing.T) (*Backend, *fakeManager, *fakeGH) {
	t.Helper()
	m := &fakeManager{}
	gh := &fakeGH{repos: map[string]*github.Repository{
		"newrepo": {Private: true}, "public": {Private: false}, "darkcloud": {Private: true},
	}}
	b := &Backend{
		Store: newStore(t), M: m, GH: gh, Events: events.New(),
		Hist:  &history.Store{Path: filepath.Join(t.TempDir(), "h.jsonl")},
		Space: &fakeStorage{},
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

// Without a sampler the series is empty, never null on the wire.
func TestMetricsWithoutSampler(t *testing.T) {
	b, _, _ := newBackend(t)
	if m := b.Metrics(); m.Samples == nil || len(m.Samples) != 0 || m.CPU != nil || m.MemUsed != nil {
		t.Fatalf("%+v", m)
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
	m.pruneErr = runner.ErrClosed
	if err := b.Prune(); apiStatus(err) != 503 {
		t.Fatalf("after Close: %v", err)
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

func registrationGH(gh *fakeGH) {
	gh.runners = map[int64]github.Runner{
		1: {ID: 1, Name: "linux-1", Status: "offline", Labels: []github.Label{{Name: "self-hosted"}, {Name: "X64"}}},
		2: {ID: 2, Name: "ghr-darkcloud-aaaaaa", Status: "offline"},
		3: {ID: 3, Name: "laptop", Status: "online"},
		4: {ID: 4, Name: "build-box", Status: "offline", Busy: true},
	}
}

func TestRegistrationsListed(t *testing.T) {
	b, _, gh := newBackend(t)
	registrationGH(gh)
	rs, err := b.Registrations(context.Background(), "DarkCloud")
	if err != nil || len(rs) != 4 {
		t.Fatalf("%+v %v", rs, err)
	}
	if rs[0].GHR || !reflect.DeepEqual(rs[0].Labels, []string{"self-hosted", "X64"}) || !rs[1].GHR || rs[2].GHR {
		t.Fatalf("%+v", rs)
	}
	if _, err := b.Registrations(context.Background(), "nope"); apiStatus(err) != 404 {
		t.Fatalf("unknown repo: %v", err)
	}
}

// A successful delete posts an event; a failed one returns GitHub's error and
// posts nothing.
func TestDeleteRegistrationEventAndFailure(t *testing.T) {
	b, _, gh := newBackend(t)
	registrationGH(gh)
	ctx := context.Background()
	gh.delErr = &github.APIError{Status: 403, Kind: github.ErrAuth, Message: "Resource not accessible"}
	if err := b.DeleteRegistration(ctx, "darkcloud", 1); !errors.Is(err, gh.delErr) {
		t.Fatalf("delete failure: %v", err)
	}
	if evs := b.Events.After(0); len(evs) != 0 {
		t.Fatalf("a failed delete posted %+v", evs)
	}
	gh.delErr = nil
	if err := b.DeleteRegistration(ctx, "DarkCloud", 1); err != nil {
		t.Fatal(err)
	}
	evs := b.Events.After(0)
	if len(evs) != 1 || evs[0].Repo != "darkcloud" || evs[0].Msg != "deleted runner registration linux-1" {
		t.Fatalf("events %+v", evs)
	}
}

// ghr's namespace is matched case-insensitively, and only a name that is
// exactly ghr-<repo>-<6 hex> belongs to ghr.
func TestGHROwned(t *testing.T) {
	for _, tc := range []struct {
		repo, name string
		want       bool
	}{
		{"darkcloud", "ghr-darkcloud-a3f9c1", true},
		{"DarkCloud", "GHR-darkcloud-A3F9C1", true},
		{"darkcloud", "ghr-darkcloud-a3f9c", false},
		{"darkcloud", "ghr-darkcloud-a3f9c1x", false},
		{"darkcloud", "ghr-darkcloud-a3f9g1", false},
		{"darkcloud", "ghr-darkcloud-x-a3f9c1", false},
		{"darkcloud", "ghr-darkcloudx-a3f9c1", false},
		{"dark", "ghr-darkcloud-a3f9c1", false},
		{"darkcloud", "my-ghr-darkcloud-a3f9c1", false},
	} {
		if got := ghrOwned(tc.repo, tc.name); got != tc.want {
			t.Errorf("ghrOwned(%q, %q) = %v", tc.repo, tc.name, got)
		}
	}
}

// A mixed-case ghr name is still refused, so a runner that is starting cannot
// be deleted under it.
func TestDeleteRegistrationRefusesMixedCaseGHRName(t *testing.T) {
	b, _, gh := newBackend(t)
	gh.runners = map[int64]github.Runner{7: {ID: 7, Name: "GHR-DarkCloud-A3F9C1", Status: "offline"}}
	if err := b.DeleteRegistration(context.Background(), "DARKCLOUD", 7); apiStatus(err) != 409 || len(gh.deleted) != 0 {
		t.Fatalf("%v %v", err, gh.deleted)
	}
}

func TestDeleteRegistrationRefusals(t *testing.T) {
	b, m, gh := newBackend(t)
	registrationGH(gh)
	gh.runners[5] = github.Runner{ID: 5, Name: "odd-box", Status: "weird"}
	ctx := context.Background()
	for id, want := range map[int64]int{2: 409, 3: 409, 4: 409, 5: 409} {
		if err := b.DeleteRegistration(ctx, "darkcloud", id); apiStatus(err) != want {
			t.Errorf("id %d: %v", id, err)
		}
	}
	if err := b.DeleteRegistration(ctx, "darkcloud", 99); err != nil {
		t.Fatalf("a runner already gone is deleted: %v", err)
	}
	if err := b.DeleteRegistration(ctx, "darkcloud", 1); err != nil || !reflect.DeepEqual(gh.deleted, []int64{1}) {
		t.Fatalf("offline foreign runner: %v %v", err, gh.deleted)
	}
	gh.getErr = errors.New("connection reset")
	if err := b.DeleteRegistration(ctx, "darkcloud", 1); apiStatus(err) != 502 || len(gh.deleted) != 1 {
		t.Fatalf("failed lookup must not delete: %v %v", err, gh.deleted)
	}
	gh.getErr = nil
	m.degraded = "GitHub rejected the token"
	if err := b.DeleteRegistration(ctx, "darkcloud", 1); apiStatus(err) != 503 {
		t.Fatalf("degraded: %v", err)
	}
}

func jobCallCount(gh *fakeGH) int {
	gh.lmu.Lock()
	defer gh.lmu.Unlock()
	return len(gh.jobCalls)
}

func TestRunnerStepsAreCached(t *testing.T) {
	b, m, gh := newBackend(t)
	m.insts = []model.InstanceStatus{{ID: "aaaaaa"}}
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	b.Now = func() time.Time { return now }
	ctx := context.Background()
	for range 3 {
		if s, err := b.RunnerSteps(ctx, "aaaaaa"); err != nil || len(s) != 1 || s[0].Name != "checkout" {
			t.Fatalf("steps %+v err %v", s, err)
		}
	}
	if n := jobCallCount(gh); n != 1 {
		t.Fatalf("%d GitHub calls inside the TTL, want 1", n)
	}
	now = now.Add(stepsTTL)
	if _, err := b.RunnerSteps(ctx, "aaaaaa"); err != nil {
		t.Fatal(err)
	}
	if n := jobCallCount(gh); n != 2 {
		t.Fatalf("%d GitHub calls after the TTL, want 2", n)
	}
}

func TestRunnerStepsAgeFromStorage(t *testing.T) {
	b, m, gh := newBackend(t)
	m.insts = []model.InstanceStatus{{ID: "aaaaaa"}}
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	b.Now = func() time.Time { return now }
	gh.onJobs = func() { now = now.Add(stepsTTL) }
	ctx := context.Background()
	for range 2 {
		if _, err := b.RunnerSteps(ctx, "aaaaaa"); err != nil {
			t.Fatal(err)
		}
	}
	if n := jobCallCount(gh); n != 1 {
		t.Fatalf("%d GitHub calls: a slow fetch stored an already expired entry", n)
	}
}

func TestRunnerStepsDropAGoneRunner(t *testing.T) {
	b, m, gh := newBackend(t)
	m.insts = []model.InstanceStatus{{ID: "aaaaaa"}}
	ctx := context.Background()
	if _, err := b.RunnerSteps(ctx, "aaaaaa"); err != nil {
		t.Fatal(err)
	}
	m.insts = nil
	b.RunnerSteps(ctx, "aaaaaa")
	if n := jobCallCount(gh); n != 2 {
		t.Fatalf("%d GitHub calls: a runner that left Status kept its cached steps", n)
	}
}

func TestRunnerStepsErrorsAreNotCached(t *testing.T) {
	b, m, gh := newBackend(t)
	m.insts = []model.InstanceStatus{{ID: "aaaaaa"}}
	gh.jobs = map[int64][]github.Job{}
	gh.jobErr = map[int64]error{55: errors.New("boom")}
	for range 2 {
		if _, err := b.RunnerSteps(context.Background(), "aaaaaa"); err == nil {
			t.Fatal("error swallowed")
		}
	}
	if n := jobCallCount(gh); n != 2 {
		t.Fatalf("%d GitHub calls, want 2: an error was cached", n)
	}
}

func TestRunnerStepsShareOneFetch(t *testing.T) {
	b, m, gh := newBackend(t)
	m.insts = []model.InstanceStatus{{ID: "aaaaaa"}}
	gh.gate = make(chan struct{})
	type result struct {
		steps []model.Step
		err   error
	}
	results := make(chan result, 8)
	for range 8 {
		go func() {
			s, err := b.RunnerSteps(context.Background(), "aaaaaa")
			results <- result{s, err}
		}()
	}
	waitFor(t, "the first fetch", func() bool { return jobCallCount(gh) == 1 })
	time.Sleep(50 * time.Millisecond)
	close(gh.gate)
	for range 8 {
		r := <-results
		if r.err != nil || len(r.steps) != 1 || r.steps[0].Name != "checkout" {
			t.Fatalf("steps %+v err %v", r.steps, r.err)
		}
	}
	if n := jobCallCount(gh); n != 1 {
		t.Fatalf("%d GitHub calls for 8 concurrent callers, want 1", n)
	}
}

func TestReloadWarnsOnWebChange(t *testing.T) {
	b, _, _ := newBackend(t)
	write := func(extra string) {
		if err := os.WriteFile(b.Store.ConfigPath, []byte(cfgYAML+extra), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	const warning = "web settings changed; restart ghr to apply"
	write("web:\n  listen: 0.0.0.0:8080\n")
	ws, err := b.Reload()
	if err != nil || !slices.Contains(ws, warning) {
		t.Fatalf("changed listen: %v %v", ws, err)
	}
	found := false
	for _, e := range b.Events.After(0) {
		if e.Level == "warn" && strings.Contains(e.Msg, warning) {
			found = true
		}
	}
	if !found {
		t.Fatal("no warn event for the web change")
	}
	write("web:\n  hosts: [ghr.lan]\n")
	if ws, err := b.Reload(); err != nil || !slices.Contains(ws, warning) {
		t.Fatalf("changed hosts: %v %v", ws, err)
	}
	write("")
	if ws, err := b.Reload(); err != nil || len(ws) != 0 {
		t.Fatalf("reverted web still warns: %v %v", ws, err)
	}
}

func TestPatchCannotChangeWeb(t *testing.T) {
	b, _, _ := newBackend(t)
	srv := httptest.NewServer(api.NewServer(b))
	defer srv.Close()
	req, _ := http.NewRequest(http.MethodPatch, srv.URL+"/config",
		strings.NewReader(`{"global_max":3,"web":{"listen":"0.0.0.0:9000"}}`))
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("patch: %d", resp.StatusCode)
	}
	if c := b.Store.Config(); c.GlobalMax != 3 || c.Web.Listen != "" {
		t.Fatalf("global_max %d web %+v", c.GlobalMax, c.Web)
	}
}

func TestStatusReportsWebSetupRequired(t *testing.T) {
	b, _, _ := newBackend(t)
	if b.Status().WebSetupRequired {
		t.Fatal("without a web listener: want false")
	}
	required := true
	b.WebSetupRequired = func() bool { return required }
	if !b.Status().WebSetupRequired {
		t.Fatal("a listener with no password: want true")
	}
	required = false
	if b.Status().WebSetupRequired {
		t.Fatal("after a password is set: want false")
	}
}

func TestStatusCarriesTheRateLimit(t *testing.T) {
	b, _, gh := newBackend(t)
	if st := b.Status(); st.RateLimit != 0 {
		t.Fatalf("before any response: %d", st.RateLimit)
	}
	n := 15000
	gh.meta = github.TokenMeta{RateLimit: &n}
	if st := b.Status(); st.RateLimit != 15000 {
		t.Fatalf("rate limit = %d", st.RateLimit)
	}
}

func TestActivityBuildsFromHistoryAndInstances(t *testing.T) {
	b, m, _ := newBackend(t)
	now := time.Date(2026, 10, 3, 14, 5, 0, 0, time.UTC)
	b.Now = func() time.Time { return now }
	if err := b.Hist.Append(model.HistoryEntry{ID: "a", Repo: "darkmem", RunID: 1, Conclusion: "success",
		StartedAt: now.Add(-50 * time.Minute), FinishedAt: now.Add(-40 * time.Minute)}); err != nil {
		t.Fatal(err)
	}
	m.insts = []model.InstanceStatus{{ID: "r", Repo: "darkmem", State: "busy", Since: now.Add(-time.Minute)}}
	m.pending = []model.HistoryEntry{{ID: "p", Repo: "darkmem", Conclusion: "unknown",
		StartedAt: now.Add(-45 * time.Minute), FinishedAt: now.Add(-2 * time.Minute)}}
	a, err := b.Activity(context.Background(), "1h", "", time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if a.Capacity == nil || *a.Capacity != 2 || len(a.Lanes) != 2 || !a.HistoryFrom.Equal(now.Add(-30*24*time.Hour)) {
		t.Fatalf("activity %+v", a)
	}
	states := map[string]string{}
	for _, l := range a.Lanes {
		for _, r := range l.Runs {
			states[r.InstanceID] = r.Segments[0].State
		}
	}
	if states["a"] != "succeeded" || states["r"] != "running" || states["p"] != "finishing" {
		t.Fatalf("states %v", states)
	}
	if len(a.Repos) != 2 || a.Repos[0].Repo != "darkcloud" || a.Repos[1].Repo != "darkmem" {
		t.Fatalf("repos %+v", a.Repos)
	}
}

// A job finalized while the view is built has left pending for history.
func TestActivityKeepsAJobFinalizedMidBuild(t *testing.T) {
	b, m, _ := newBackend(t)
	now := time.Date(2026, 10, 3, 14, 5, 0, 0, time.UTC)
	b.Now = func() time.Time { return now }
	m.onPending = func() {
		if err := b.Hist.Append(model.HistoryEntry{ID: "p", Repo: "darkmem", RunID: 1, Conclusion: "success",
			StartedAt: now.Add(-20 * time.Minute), FinishedAt: now.Add(-10 * time.Minute)}); err != nil {
			t.Fatal(err)
		}
	}
	a, err := b.Activity(context.Background(), "1h", "", time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Lanes) == 0 || len(a.Lanes[0].Runs) != 1 || a.Lanes[0].Runs[0].InstanceID != "p" {
		t.Fatalf("lanes %+v", a.Lanes)
	}
}

func TestActivityCachesPerWindowAndZone(t *testing.T) {
	b, _, _ := newBackend(t)
	now := time.Date(2026, 10, 3, 14, 5, 0, 0, time.UTC)
	b.Now = func() time.Time { return now }
	ctx := context.Background()
	runs := func(a model.Activity) int {
		n := 0
		for _, l := range a.Lanes {
			n += len(l.Runs)
		}
		return n
	}
	first, _ := b.Activity(ctx, "1h", "", time.UTC)
	b.Hist.Append(model.HistoryEntry{ID: "a", Repo: "darkmem", RunID: 1, Conclusion: "success",
		StartedAt: now.Add(-20 * time.Minute), FinishedAt: now.Add(-10 * time.Minute)})
	if again, _ := b.Activity(ctx, "1h", "", time.UTC); runs(again) != runs(first) {
		t.Fatal("a second call within 5 s rebuilt the response")
	}
	if other, _ := b.Activity(ctx, "3h", "", time.UTC); runs(other) != 1 {
		t.Fatal("another window shared the cache entry")
	}
	if zoned, _ := b.Activity(ctx, "1h", "", time.FixedZone("ICT", 7*60*60)); zoned.TZ != "ICT" || runs(zoned) != 1 {
		t.Fatal("another zone shared the cache entry")
	}
	now = now.Add(6 * time.Second)
	if later, _ := b.Activity(ctx, "1h", "", time.UTC); runs(later) != 1 {
		t.Fatal("the cache outlived 5 s")
	}
}

func TestActivitySlowWindowDoesNotBlockAnother(t *testing.T) {
	b, _, _ := newBackend(t)
	held := b.activityEntry("30d|UTC")
	held.mu.Lock()
	defer held.mu.Unlock()
	done := make(chan struct{})
	go func() {
		b.Activity(context.Background(), "1h", "", time.UTC)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("a 1h build waited for a 30d build")
	}
}

func TestActivityCacheDropsExpiredEntries(t *testing.T) {
	b, _, _ := newBackend(t)
	now := time.Date(2026, 10, 3, 14, 5, 0, 0, time.UTC)
	b.Now = func() time.Time { return now }
	ctx := context.Background()
	b.Activity(ctx, "1h", "", time.UTC)
	now = now.Add(6 * time.Second)
	b.Activity(ctx, "3h", "", time.UTC)
	b.activityMu.Lock()
	_, kept := b.activity["1h|UTC"]
	n := len(b.activity)
	b.activityMu.Unlock()
	if kept || n != 1 {
		t.Fatalf("expired entry kept: %v, %d entries", kept, n)
	}
}

func TestActivityAllModeHasNoCapacity(t *testing.T) {
	b, _, _ := newBackend(t)
	if _, err := b.Store.Update(func(c *config.Config) error { c.Mode = config.ModeAll; return nil }); err != nil {
		t.Fatal(err)
	}
	a, err := b.Activity(context.Background(), "24h", "", time.UTC)
	if err != nil || a.Window != "24h" || a.Capacity != nil {
		t.Fatalf("window %q capacity %v err %v", a.Window, a.Capacity, err)
	}
}

func TestActivityForOneRepo(t *testing.T) {
	b, _, _ := newBackend(t)
	now := time.Date(2026, 10, 3, 14, 5, 0, 0, time.UTC)
	b.Now = func() time.Time { return now }
	ctx := context.Background()
	b.Hist.Append(model.HistoryEntry{ID: "a", Repo: "darkmem", RunID: 1, Conclusion: "success",
		StartedAt: now.Add(-20 * time.Minute), FinishedAt: now.Add(-10 * time.Minute)})
	all, _ := b.Activity(ctx, "24h", "", time.UTC)
	one, err := b.Activity(ctx, "24h", "DarkMem", time.UTC)
	if err != nil || all.Capacity == nil || one.Capacity != nil || len(one.Repos) != 1 || one.Repos[0].Repo != "darkmem" {
		t.Fatalf("one repo %+v capacity %v err %v", one.Repos, one.Capacity, err)
	}
	b.Hist.Append(model.HistoryEntry{ID: "b", Repo: "darkmem", RunID: 2, Conclusion: "failure",
		StartedAt: now.Add(-9 * time.Minute), FinishedAt: now.Add(-5 * time.Minute)})
	if again, _ := b.Activity(ctx, "24h", "darkmem", time.UTC); again.Repos[0].Week.Failed != 0 {
		t.Fatal("a differently cased name missed the cache entry")
	}
	if other, _ := b.Activity(ctx, "24h", "darkcloud", time.UTC); len(other.Repos) != 1 || other.Repos[0].Repo != "darkcloud" {
		t.Fatalf("another repo shared the cache entry: %+v", other.Repos)
	}
}

func TestRunnerStepsCarryTimes(t *testing.T) {
	b, m, gh := newBackend(t)
	m.insts = []model.InstanceStatus{{ID: "aaaaaa"}}
	start := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	end := start.Add(4 * time.Second)
	gh.jobs = map[int64][]github.Job{55: {{RunnerName: "ghr-darkcloud-aaaaaa", Steps: []github.Step{
		{Number: 1, Name: "checkout", Status: "completed", Conclusion: "success", StartedAt: &start, CompletedAt: &end},
		{Number: 2, Name: "test", Status: "queued"},
	}}}}
	s, err := b.RunnerSteps(context.Background(), "aaaaaa")
	if err != nil || len(s) != 2 || s[0].StartedAt == nil || !s[0].StartedAt.Equal(start) || s[0].CompletedAt == nil ||
		!s[0].CompletedAt.Equal(end) || s[1].StartedAt != nil || s[1].CompletedAt != nil {
		t.Fatalf("steps %+v err %v", s, err)
	}
}

func TestWatchRepo(t *testing.T) {
	b, _, _ := newBackend(t)
	ctx := context.Background()
	for _, c := range []struct {
		name   string
		status int
		text   string
	}{
		{" ", 400, "required"},
		{"other/docs", 400, "without a slash"},
		{"missing", 400, "repository access"},
		{"DarkCloud", 409, "already configured"},
	} {
		if err := b.WatchRepo(ctx, c.name); apiStatus(err) != c.status || !strings.Contains(err.Error(), c.text) {
			t.Errorf("WatchRepo(%q) = %v", c.name, err)
		}
	}
	if err := b.WatchRepo(ctx, "public"); err != nil {
		t.Fatal(err)
	}
	if err := b.WatchRepo(ctx, "Public"); apiStatus(err) != 409 || !strings.Contains(err.Error(), "already watched") {
		t.Fatalf("second watch: %v", err)
	}
	if got := b.Store.Config().WatchRepos; len(got) != 1 || got[0] != "public" {
		t.Fatalf("watch_repos %v", got)
	}
	if _, err := b.Store.Update(func(c *config.Config) error {
		r := c.Repo("darkmem")
		r.Paused, r.Removing = true, true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := b.WatchRepo(ctx, "darkmem"); apiStatus(err) != 409 {
		t.Fatalf("repo being removed: %v", err)
	}
}

func TestUnwatchRepoNeverAsksGitHub(t *testing.T) {
	b, _, gh := newBackend(t)
	if err := b.WatchRepo(context.Background(), "public"); err != nil {
		t.Fatal(err)
	}
	delete(gh.repos, "public")
	before := gh.getCalls
	if err := b.UnwatchRepo("nope"); apiStatus(err) != 404 {
		t.Fatalf("unknown: %v", err)
	}
	if err := b.UnwatchRepo("PUBLIC"); err != nil {
		t.Fatal(err)
	}
	if got := b.Store.Config().WatchRepos; len(got) != 0 {
		t.Fatalf("watch_repos %v", got)
	}
	if gh.getCalls != before {
		t.Fatalf("unwatch called GetRepo %d times", gh.getCalls-before)
	}
}

func TestConcurrentUnwatchSucceedsOnce(t *testing.T) {
	b, _, _ := newBackend(t)
	if err := b.WatchRepo(context.Background(), "public"); err != nil {
		t.Fatal(err)
	}
	errs := make(chan error, 2)
	for range 2 {
		go func() { errs <- b.UnwatchRepo("public") }()
	}
	first, second := <-errs, <-errs
	if (first == nil) == (second == nil) || (apiStatus(first) != 404 && apiStatus(second) != 404) {
		t.Fatalf("errors %v and %v, want one success and one 404", first, second)
	}
}

func TestAddRepoTakesOverAWatchedRepo(t *testing.T) {
	b, _, _ := newBackend(t)
	ctx := context.Background()
	if err := b.WatchRepo(ctx, "newrepo"); err != nil {
		t.Fatal(err)
	}
	if err := b.AddRepo(ctx, model.AddRepoRequest{Name: "newrepo", Labels: []string{"x"}}); err != nil {
		t.Fatal(err)
	}
	c := b.Store.Config()
	if c.Repo("newrepo") == nil || len(c.WatchRepos) != 0 {
		t.Fatalf("repos %+v watch_repos %v", c.Repos, c.WatchRepos)
	}
}

func TestPatchConfigKeepsWatchRepos(t *testing.T) {
	b, _, _ := newBackend(t)
	if err := b.WatchRepo(context.Background(), "public"); err != nil {
		t.Fatal(err)
	}
	three := 3
	if err := b.PatchConfig(model.ConfigPatch{GlobalMax: &three}); err != nil {
		t.Fatal(err)
	}
	if got := b.Store.Config().WatchRepos; len(got) != 1 || got[0] != "public" {
		t.Fatalf("watch_repos %v", got)
	}
}

func TestAvailableReposMarksWatched(t *testing.T) {
	b, _, gh := newBackend(t)
	gh.userRepos = []github.UserRepo{
		{Name: "public", Owner: github.Account{Login: "darkraise"}},
		{Name: "darkmem", Private: true, Owner: github.Account{Login: "darkraise"}},
	}
	if err := b.WatchRepo(context.Background(), "public"); err != nil {
		t.Fatal(err)
	}
	rs, err := b.AvailableRepos(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []model.AvailableRepo{{Name: "darkmem", Private: true, Configured: true}, {Name: "public", Watched: true}}
	if len(rs) != 2 || rs[0] != want[0] || rs[1] != want[1] {
		t.Fatalf("available %+v", rs)
	}
}
