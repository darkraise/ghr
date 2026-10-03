package runner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/events"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/history"
	"github.com/darkraise/ghr/internal/system"
)

type fakeGH struct {
	mu        sync.Mutex
	runs      map[string][]github.Run // key repo/status
	jobs      map[int64][]github.Job
	runners   map[int64]*github.Runner
	listed    map[string][]github.Runner
	deleted   []int64
	jitCalls  []string
	calls     []string // "<method> <repo>" of every call
	nextID    int64
	errs      map[string]error // key "<method> <repo>"
	remaining int
}

func newFakeGH() *fakeGH {
	return &fakeGH{runs: map[string][]github.Run{}, jobs: map[int64][]github.Job{}, runners: map[int64]*github.Runner{},
		listed: map[string][]github.Runner{}, errs: map[string]error{}, nextID: 100, remaining: 4999}
}

// call records the call and returns its injected error, if any.
func (f *fakeGH) call(method, repo string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, method+" "+repo)
	return f.errs[method+" "+repo]
}

func (f *fakeGH) setErr(key string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err == nil {
		delete(f.errs, key)
		return
	}
	f.errs[key] = err
}

func (f *fakeGH) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeGH) ListRuns(_ context.Context, repo, status string) ([]github.Run, error) {
	if err := f.call("ListRuns", repo); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.runs[repo+"/"+status], nil
}

func (f *fakeGH) ListJobs(_ context.Context, repo string, runID int64) ([]github.Job, error) {
	if err := f.call("ListJobs", repo); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.jobs[runID], nil
}

func (f *fakeGH) ListRunners(_ context.Context, repo string) ([]github.Runner, error) {
	if err := f.call("ListRunners", repo); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.listed[repo], nil
}

func (f *fakeGH) GetRunner(_ context.Context, repo string, id int64) (*github.Runner, error) {
	if err := f.call("GetRunner", repo); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.runners[id]
	if !ok {
		return nil, &github.APIError{Status: 404, Kind: github.ErrNotFound}
	}
	c := *r
	return &c, nil
}

func (f *fakeGH) DeleteRunner(_ context.Context, repo string, id int64) error {
	if err := f.call("DeleteRunner", repo); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, id)
	delete(f.runners, id)
	return nil
}

func (f *fakeGH) GenerateJITConfig(_ context.Context, repo, name string, labels []string) (*github.JITConfig, error) {
	if err := f.call("GenerateJITConfig", repo); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	f.jitCalls = append(f.jitCalls, name+" "+strings.Join(labels, ","))
	f.runners[f.nextID] = &github.Runner{ID: f.nextID, Name: name, Status: "offline"}
	return &github.JITConfig{Runner: github.Runner{ID: f.nextID, Name: name}, EncodedJITConfig: "ENC-" + name}, nil
}

func (f *fakeGH) RateRemaining() int { return f.remaining }

func (f *fakeGH) setRunner(id int64, status string, busy bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.runners[id] = &github.Runner{ID: id, Status: status, Busy: busy}
}

func (f *fakeGH) setJobs(runID int64, jobs ...github.Job) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.jobs[runID] = jobs
}

type fakeSD struct {
	mu        sync.Mutex
	started   []system.UnitSpec
	stopped   []string
	active    map[string]bool
	stopErr   map[string]error // by unit
	activeErr map[string]error // by unit
}

func newFakeSD() *fakeSD {
	return &fakeSD{active: map[string]bool{}, stopErr: map[string]error{}, activeErr: map[string]error{}}
}

func (f *fakeSD) Start(_ context.Context, u system.UnitSpec) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.started = append(f.started, u)
	f.active[u.Unit] = true
	return nil
}

func (f *fakeSD) Stop(_ context.Context, unit string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.stopErr[unit]; err != nil {
		return err
	}
	f.stopped = append(f.stopped, unit)
	f.active[unit] = false
	return nil
}

func (f *fakeSD) Active(_ context.Context, unit string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.activeErr[unit]; err != nil {
		return false, err
	}
	return f.active[unit], nil
}

func (f *fakeSD) List(_ context.Context, prefix string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for u, a := range f.active {
		if a && strings.HasPrefix(u, prefix) {
			out = append(out, u)
		}
	}
	sort.Strings(out)
	return out, nil
}

type fakeDocker struct {
	mu        sync.Mutex
	compose   []system.ComposeContainer
	named     []system.NamedContainer
	byProject map[string][]system.ProjectContainer
	byLabel   map[string][]string
	removed   []string
	netLabels []string
	volLabels []string
	usage     []int // successive DataRootUsage results; last value repeats
	prunes    []string
	errs      map[string]error // by method name
}

func newFakeDocker() *fakeDocker {
	return &fakeDocker{byLabel: map[string][]string{}, byProject: map[string][]system.ProjectContainer{}, errs: map[string]error{}}
}

func (f *fakeDocker) err(method string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.errs[method]
}

func (f *fakeDocker) setErr(method string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err == nil {
		delete(f.errs, method)
		return
	}
	f.errs[method] = err
}

func (f *fakeDocker) ComposeContainers(context.Context) ([]system.ComposeContainer, error) {
	if err := f.err("ComposeContainers"); err != nil {
		return nil, err
	}
	return f.compose, nil
}
func (f *fakeDocker) Containers(context.Context) ([]system.NamedContainer, error) {
	if err := f.err("Containers"); err != nil {
		return nil, err
	}
	return f.named, nil
}
func (f *fakeDocker) ProjectContainers(_ context.Context, project string) ([]system.ProjectContainer, error) {
	if err := f.err("ProjectContainers"); err != nil {
		return nil, err
	}
	return f.byProject[project], nil
}
func (f *fakeDocker) ContainerIDsByLabel(_ context.Context, label string) ([]string, error) {
	if err := f.err("ContainerIDsByLabel"); err != nil {
		return nil, err
	}
	return f.byLabel[label], nil
}
func (f *fakeDocker) RemoveContainers(_ context.Context, ids []string) error {
	if err := f.err("RemoveContainers"); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removed = append(f.removed, ids...)
	return nil
}
func (f *fakeDocker) RemoveNetworksByLabel(_ context.Context, label string) error {
	if err := f.err("RemoveNetworksByLabel"); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.netLabels = append(f.netLabels, label)
	return nil
}
func (f *fakeDocker) RemoveVolumesByLabel(_ context.Context, label string) error {
	if err := f.err("RemoveVolumesByLabel"); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.volLabels = append(f.volLabels, label)
	return nil
}

// DataRootUsage returns successive f.usage values; a negative value is a failed read.
func (f *fakeDocker) DataRootUsage(context.Context) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.usage) == 0 {
		return 10, nil
	}
	v := f.usage[0]
	if len(f.usage) > 1 {
		f.usage = f.usage[1:]
	}
	if v < 0 {
		return 0, fmt.Errorf("df failed")
	}
	return v, nil
}
func (f *fakeDocker) PruneBuildCacheOlderThan(_ context.Context, h int) (string, error) {
	f.prunes = append(f.prunes, fmt.Sprintf("until=%dh", h))
	if err := f.err("PruneBuildCacheOlderThan"); err != nil {
		return "", err
	}
	return "1GB", nil
}
func (f *fakeDocker) PruneBuildCacheTo(_ context.Context, keep string) (string, error) {
	f.prunes = append(f.prunes, "keep="+keep)
	if err := f.err("PruneBuildCacheTo"); err != nil {
		return "", err
	}
	return "5GB", nil
}
func (f *fakeDocker) PruneDanglingImages(context.Context) (string, error) {
	f.prunes = append(f.prunes, "images")
	if err := f.err("PruneDanglingImages"); err != nil {
		return "", err
	}
	return "200MB", nil
}

// fakeHost copies by creating the destination with a run.sh, like a real dist.
type fakeHost struct {
	mu      sync.Mutex
	chowned []string
}

func (f *fakeHost) CopyTree(_ context.Context, src, dst string) error {
	if err := os.MkdirAll(filepath.Join(dst, "_diag"), 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dst, "run.sh"), []byte("#!/bin/sh\n"), 0o755)
}
func (f *fakeHost) ChownR(_ context.Context, path, user string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.chowned = append(f.chowned, path+":"+user)
	return nil
}

type harness struct {
	m      *Manager
	cfg    *config.Config
	gh     *fakeGH
	sd     *fakeSD
	docker *fakeDocker
	host   *fakeHost
	now    time.Time
	ids    []string
	root   string
}

const testConfig = `
owner: darkraise
mode: queue
global_max: 2
labels: [homelab]
repos:
  - name: darkcloud
    max: 1
    cleanup_name_prefixes: [dc-e2e-]
  - name: darkmem
`

func newHarness(t *testing.T) *harness {
	t.Helper()
	cfg, _, err := config.Parse([]byte(testConfig))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	dist := filepath.Join(root, "dist", "2.330.0")
	if err := os.MkdirAll(dist, 0o755); err != nil {
		t.Fatal(err)
	}
	h := &harness{cfg: cfg, gh: newFakeGH(), sd: newFakeSD(), docker: newFakeDocker(), host: &fakeHost{},
		now: time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC), root: root,
		ids: []string{"aaaaaa", "bbbbbb", "cccccc", "dddddd", "eeeeee", "ffffff"}}
	ev := events.New()
	ev.Now = func() time.Time { return h.now }
	h.m = &Manager{
		Config: func() *config.Config { return h.cfg },
		GH:     h.gh, SD: h.sd, Docker: h.docker, Host: h.host,
		Paths: Paths{Dist: dist, Instances: filepath.Join(root, "instances"), Logs: filepath.Join(root, "logs"),
			Pending: filepath.Join(root, "pending"), ToolCache: filepath.Join(root, "toolcache"),
			Hooks: "/opt/ghr/hooks", Home: "/home/ghrunner"},
		Events:  ev,
		History: &history.Store{Path: filepath.Join(root, "history.jsonl")},
		Now:     func() time.Time { return h.now },
		NewID: func() string {
			id := h.ids[0]
			h.ids = h.ids[1:]
			return id
		},
	}
	if err := h.m.Init(); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(h.m.Paths.Instances, 0o755); err != nil {
		t.Fatal(err)
	}
	return h
}

func (h *harness) state(id string) string {
	h.m.mu.Lock()
	defer h.m.mu.Unlock()
	if i, ok := h.m.insts[id]; ok {
		return string(i.State)
	}
	return "gone"
}

// writeJob writes a job-started record; extra fields (such as finished_at) are appended.
func (h *harness) writeJob(t *testing.T, id string, runID int64, extra ...string) {
	t.Helper()
	rec := fmt.Sprintf(`{"run_id":"%d","run_attempt":"1","run_number":"412","workflow":"CI","job":"e2e","runner_name":"x","started_at":"2026-10-03T12:00:00Z"`, runID)
	for _, e := range extra {
		rec += "," + e
	}
	if err := os.WriteFile(filepath.Join(h.m.instanceDir(id), JobFile), []byte(rec+"}"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (h *harness) eventText() string {
	var b strings.Builder
	for _, e := range h.m.Events.After(0) {
		b.WriteString(e.Level + " " + e.Repo + " " + e.Msg + "\n")
	}
	return b.String()
}

func (h *harness) history(t *testing.T) []string {
	t.Helper()
	got, err := h.m.History.Query("", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range got {
		out = append(out, e.ID+" "+e.Conclusion)
	}
	return out
}
