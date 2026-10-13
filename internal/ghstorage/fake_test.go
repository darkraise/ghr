package ghstorage

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/events"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
)

// fakeGH is a GitHub whose answers the test sets. Calls records every call
// as "Method repo [id]". Hooks run outside the lock, before the answer.
type fakeGH struct {
	mu        sync.Mutex
	isOrg     bool
	login     string
	usage     []github.UsageItem
	usageErr  error
	repos     []github.UserRepo
	reposErr  error
	artifacts map[string][]github.Artifact
	caches    map[string][]github.Cache
	listErr   map[string]error
	retention map[string]github.Retention
	retErr    map[string]error
	setRetErr map[string]error
	runs      map[string]map[string][]github.Run // repo → status → runs
	runsErr   map[string]error
	gone      map[int64]bool  // a delete of this id answers 404
	delErr    map[int64]error // a delete of this id fails
	gen       uint64
	suspended time.Time
	remaining int
	calls     []string

	// onList runs at the start of ListRepoArtifacts; an error it returns is
	// the call's answer.
	onList func(ctx context.Context, repo string) error
	// onDelete runs at the start of DeleteArtifact and DeleteCache.
	onDelete func(ctx context.Context, repo string, id int64) error
	// onUsage runs at the start of StorageUsage; an error it returns is the
	// call's answer.
	onUsage func(ctx context.Context) error
}

func newFakeGH() *fakeGH {
	return &fakeGH{
		login: "darkraise", remaining: -1,
		artifacts: map[string][]github.Artifact{}, caches: map[string][]github.Cache{},
		listErr: map[string]error{}, retention: map[string]github.Retention{}, retErr: map[string]error{},
		setRetErr: map[string]error{}, runs: map[string]map[string][]github.Run{}, runsErr: map[string]error{},
		gone: map[int64]bool{}, delErr: map[int64]error{},
	}
}

func (f *fakeGH) call(format string, args ...any) {
	f.mu.Lock()
	f.calls = append(f.calls, fmt.Sprintf(format, args...))
	f.mu.Unlock()
}

// callsLike returns the recorded calls that start with prefix.
func (f *fakeGH) callsLike(prefix string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []string{}
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			out = append(out, c)
		}
	}
	return out
}

func (f *fakeGH) OwnerType(context.Context) (bool, error) {
	f.call("OwnerType")
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.isOrg, nil
}

func (f *fakeGH) TokenLogin(context.Context) (string, error) {
	f.call("TokenLogin")
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.login, nil
}

func (f *fakeGH) StorageUsage(ctx context.Context, isOrg bool, year int, month time.Month) ([]github.UsageItem, error) {
	f.call("StorageUsage %v %d-%02d", isOrg, year, int(month))
	if f.onUsage != nil {
		if err := f.onUsage(ctx); err != nil {
			return nil, err
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.usage), f.usageErr
}

func (f *fakeGH) ListUserRepos(context.Context) ([]github.UserRepo, error) {
	f.call("ListUserRepos")
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.repos), f.reposErr
}

func (f *fakeGH) ListRepoArtifacts(ctx context.Context, repo string) ([]github.Artifact, error) {
	f.call("ListRepoArtifacts %s", repo)
	if f.onList != nil {
		if err := f.onList(ctx, repo); err != nil {
			return nil, err
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.listErr[repo]; err != nil {
		return nil, err
	}
	return slices.Clone(f.artifacts[repo]), nil
}

func (f *fakeGH) ListCaches(_ context.Context, repo string) ([]github.Cache, error) {
	f.call("ListCaches %s", repo)
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.caches[repo]), nil
}

func (f *fakeGH) Retention(_ context.Context, repo string) (github.Retention, error) {
	f.call("Retention %s", repo)
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.retention[repo], f.retErr[repo]
}

func (f *fakeGH) SetRetention(_ context.Context, repo string, days int) error {
	f.call("SetRetention %s %d", repo, days)
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.setRetErr[repo]; err != nil {
		return err
	}
	r := f.retention[repo]
	r.Days = days
	f.retention[repo] = r
	return nil
}

func (f *fakeGH) ListRuns(_ context.Context, repo, status string) ([]github.Run, error) {
	f.call("ListRuns %s %s", repo, status)
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.runsErr[repo]; err != nil {
		return nil, err
	}
	return slices.Clone(f.runs[repo][status]), nil
}

func (f *fakeGH) del(ctx context.Context, kind, repo string, id int64) (bool, error) {
	f.call("Delete%s %s %d", kind, repo, id)
	if f.onDelete != nil {
		if err := f.onDelete(ctx, repo, id); err != nil {
			return false, err
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.delErr[id]; err != nil {
		return false, err
	}
	if f.gone[id] {
		return false, nil
	}
	f.gone[id] = true
	return true, nil
}

func (f *fakeGH) DeleteArtifact(ctx context.Context, repo string, id int64) (bool, error) {
	return f.del(ctx, "Artifact", repo, id)
}

func (f *fakeGH) DeleteCache(ctx context.Context, repo string, id int64) (bool, error) {
	return f.del(ctx, "Cache", repo, id)
}

func (f *fakeGH) TokenMeta() github.TokenMeta {
	f.mu.Lock()
	defer f.mu.Unlock()
	return github.TokenMeta{Generation: f.gen}
}

func (f *fakeGH) SuspendedUntil() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.suspended
}

func (f *fakeGH) RateRemaining() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.remaining
}

// addRepo makes repo reachable with the given artifacts, all made by
// different runs. Bytes are 1 GiB each unless set.
func (f *fakeGH) addRepo(repo string, private bool, arts ...github.Artifact) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.repos = append(f.repos, github.UserRepo{Name: repo, Private: private, Owner: github.Account{Login: "darkraise"}})
	f.artifacts[repo] = arts
	f.retention[repo] = github.Retention{Days: 90, MaxDays: 400}
}

// ghArt is an artifact of run run, created age before at.
func ghArt(id, run int64, at time.Time, age time.Duration, bytes int64) github.Artifact {
	return github.Artifact{ID: id, Name: fmt.Sprintf("art-%d", id), SizeInBytes: bytes, CreatedAt: at.Add(-age),
		WorkflowRun: github.ArtifactRun{ID: run, HeadBranch: "main"}}
}

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// newTestService builds a service on gh and cfg that runs no loop: the test
// runs passes with s.pass. Its clock starts at 2026-10-13 12:00 UTC and its
// operation ids are op-1, op-2, and so on.
func newTestService(t *testing.T, gh *fakeGH, cfg *config.Config) (*Service, *fakeClock) {
	t.Helper()
	clock := &fakeClock{t: time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)}
	ev := events.New()
	ev.Now = clock.Now
	n := 0
	s := &Service{GH: gh, Config: func() *config.Config { return cfg }, Events: ev, Now: clock.Now,
		NewID: func() string { n++; return fmt.Sprintf("op-%d", n) }}
	s.init()
	t.Cleanup(func() { s.Close(); s.Wait() })
	return s, clock
}

func testConfig() *config.Config { return &config.Config{Owner: "darkraise"} }

// row returns the snapshot's row for repo, failing the test without one.
func row(t *testing.T, st model.GitHubStorage, repo string) model.RepoStorage {
	t.Helper()
	for _, r := range st.Repos {
		if r.Name == repo {
			return r
		}
	}
	t.Fatalf("no row for %s in %+v", repo, st.Repos)
	return model.RepoStorage{}
}

// eventsLike returns the messages of the events whose text contains sub.
func eventsLike(ev *events.Ring, sub string) []string {
	out := []string{}
	for _, e := range ev.After(0) {
		if strings.Contains(e.Msg, sub) {
			out = append(out, e.Msg)
		}
	}
	return out
}
