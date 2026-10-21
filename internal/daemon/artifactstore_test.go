package daemon

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/artifactstore"
	"github.com/darkraise/ghr/internal/ghstorage"
	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/s3"
)

// fakeAS records every call as "<method> <args>". SyncRepo answers with
// syncErr, SwitchOff with offErr, Retire with retireErr and every other
// method that can fail with err. Row answers a row named after the
// repository, or none when noRow is set.
type fakeAS struct {
	mu        sync.Mutex
	calls     []string
	snap      model.ArtifactStore
	summary   *model.ArtifactStoreSummary
	test      model.ArtifactStoreTest
	items     []model.ArtifactStoreItem
	link      model.ArtifactLink
	err       error
	syncErr   error
	offErr    error
	retireErr error
	noRow     bool
	version   atomic.Int64
}

func (f *fakeAS) record(format string, args ...any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fmt.Sprintf(format, args...))
}

func (f *fakeAS) recorded() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func (f *fakeAS) Kick()                                { f.record("kick") }
func (f *fakeAS) Refresh() error                       { f.record("refresh"); return f.err }
func (f *fakeAS) Version() int64                       { return f.version.Load() }
func (f *fakeAS) Snapshot() model.ArtifactStore        { return f.snap }
func (f *fakeAS) Summary() *model.ArtifactStoreSummary { return f.summary }
func (f *fakeAS) KeysChanged()                         { f.record("keys-changed") }
func (f *fakeAS) PolicyChanged()                       { f.record("policy") }
func (f *fakeAS) SetKey(which, id, secret string) error {
	f.record("set-key %s %s %s", which, id, secret)
	return f.err
}
func (f *fakeAS) SyncRepo(_ context.Context, repo string) (model.ArtifactStoreRepo, error) {
	f.record("sync %s", repo)
	return model.ArtifactStoreRepo{Name: repo, State: "set"}, f.syncErr
}
func (f *fakeAS) SwitchOff(_ context.Context, repo string) error {
	f.record("switch-off %s", repo)
	return f.offErr
}
func (f *fakeAS) Row(repo string) (model.ArtifactStoreRepo, bool) {
	f.record("row %s", repo)
	return model.ArtifactStoreRepo{Name: repo, State: "row"}, !f.noRow
}
func (f *fakeAS) Retire(_ context.Context, repo string) error {
	f.record("retire %s", repo)
	return f.retireErr
}
func (f *fakeAS) Test(context.Context) (model.ArtifactStoreTest, error) {
	f.record("test")
	return f.test, f.err
}
func (f *fakeAS) RunObjects(_ context.Context, repo string, runID int64) ([]model.ArtifactStoreItem, error) {
	f.record("run-objects %s %d", repo, runID)
	return f.items, f.err
}
func (f *fakeAS) Delete(repo string, keys []string) (string, error) {
	f.record("delete %s %v", repo, keys)
	return "op-7", f.err
}
func (f *fakeAS) Link(key, ttl string) (model.ArtifactLink, error) {
	f.record("link %s %s", key, ttl)
	return f.link, f.err
}

func newASBackend(t *testing.T) (*Backend, *fakeManager, *fakeGH, *fakeAS) {
	t.Helper()
	b, m, gh := newBackend(t)
	f := &fakeAS{}
	b.AS = f
	return b, m, gh, f
}

const asKey = "darkraise/darkmem/42/dist.zip"

// asCalls are the backend's artifact store methods that can fail, each named
// by what the fake records for it.
func asCalls(b *Backend) map[string]func() error {
	ctx := context.Background()
	return map[string]func() error{
		"refresh": b.RefreshArtifactStore,
		"test":    func() error { _, err := b.TestArtifactStore(ctx); return err },
		"set-key": func() error {
			return b.SetArtifactKey(model.ArtifactKeyRequest{Which: "job", ID: "JOBKEY12345678", Secret: "job-secret-value"})
		},
		"run-objects": func() error { _, err := b.ArtifactRunObjects(ctx, "darkmem", 42); return err },
		"delete": func() error {
			_, err := b.DeleteArtifactObjects("darkmem", model.ArtifactDeleteRequest{Keys: []string{asKey}})
			return err
		},
		"link": func() error {
			_, err := b.ArtifactLink(model.ArtifactLinkRequest{Key: asKey, TTL: "1h"})
			return err
		},
	}
}

func TestArtifactStoreCallsReachTheService(t *testing.T) {
	b, _, _, f := newASBackend(t)
	ctx := context.Background()
	at := time.Date(2026, 10, 15, 13, 0, 0, 0, time.UTC)
	f.snap = model.ArtifactStore{Configured: true, Bucket: "gha-artifacts"}
	f.test = model.ArtifactStoreTest{Checks: []model.ArtifactStoreCheck{{Name: "list", OK: true, Detail: "3 objects"}}}
	f.items = []model.ArtifactStoreItem{{Key: asKey, Name: "dist", RunID: 42}}
	f.link = model.ArtifactLink{URL: "https://s3.example.test/gha-artifacts/" + asKey, ExpiresAt: at}
	if st := b.ArtifactStore(); !st.Configured || st.Bucket != "gha-artifacts" {
		t.Fatalf("snapshot %+v", st)
	}
	if err := b.RefreshArtifactStore(); err != nil {
		t.Fatal(err)
	}
	if tr, err := b.TestArtifactStore(ctx); err != nil || len(tr.Checks) != 1 || tr.Checks[0].Name != "list" {
		t.Fatalf("test %+v, %v", tr, err)
	}
	if err := b.SetArtifactKey(model.ArtifactKeyRequest{Which: "admin", ID: "ADMINKEY1234", Secret: "admin-secret"}); err != nil {
		t.Fatal(err)
	}
	if items, err := b.ArtifactRunObjects(ctx, "darkmem", 42); err != nil || len(items) != 1 || items[0].Key != asKey {
		t.Fatalf("run objects %+v, %v", items, err)
	}
	if op, err := b.DeleteArtifactObjects("DarkMem", model.ArtifactDeleteRequest{Keys: []string{asKey}}); err != nil || op.ID != "op-7" {
		t.Fatalf("delete %+v, %v", op, err)
	}
	if l, err := b.ArtifactLink(model.ArtifactLinkRequest{Key: asKey, TTL: "24h"}); err != nil || l != f.link {
		t.Fatalf("link %+v, %v", l, err)
	}
	want := []string{
		"refresh", "test", "set-key admin ADMINKEY1234 admin-secret", "run-objects darkmem 42",
		"delete DarkMem [" + asKey + "]", "link " + asKey + " 24h",
	}
	if got := f.recorded(); !slices.Equal(got, want) {
		t.Fatalf("calls %q", got)
	}
}

func TestArtifactStoreErrorsMapToStatuses(t *testing.T) {
	b, _, _, f := newASBackend(t)
	retry := time.Date(2026, 10, 15, 13, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		err  error
		want int
	}{
		{artifactstore.BadInput("ttl must be 1h, 24h or 7d"), 400},
		{artifactstore.ErrUnknownRepo, 404},
		{artifactstore.ErrBusy, 409},
		{artifactstore.ErrOperationRunning, 409},
		{artifactstore.ErrNotConfigured, 409},
		{artifactstore.ErrNoAdminKey, 409},
		{artifactstore.ErrNoJobKey, 409},
		{&ghstorage.BudgetError{RetryAt: retry}, 429},
		{artifactstore.ErrClosed, 503},
		{&s3.Error{Status: 403, Code: "AccessDenied", Message: "Access Denied."}, 502},
	} {
		f.err = tc.err
		for name, call := range asCalls(b) {
			err := call()
			if apiStatus(err) != tc.want || apiMsg(err) != tc.err.Error() {
				t.Errorf("%s with %v: status %d message %q, want %d", name, tc.err, apiStatus(err), apiMsg(err), tc.want)
			}
		}
	}
	f.err = &ghstorage.BudgetError{RetryAt: retry}
	var ae *api.Error
	if err := b.RefreshArtifactStore(); !errors.As(err, &ae) || !ae.RetryAt.Equal(retry) {
		t.Fatalf("budget retry time: %v", err)
	}
	f.err = errors.New("unexpected")
	if err := b.RefreshArtifactStore(); err != f.err {
		t.Fatalf("an unexpected error must pass through unchanged: %v", err)
	}
}

// A key outside the repository's prefix, or the owner's for a link, or with
// an empty, "." or ".." segment, is refused before the service sees it.
func TestArtifactKeysMustStayInScope(t *testing.T) {
	b, _, _, f := newASBackend(t)
	del := func(repo string, keys ...string) func() error {
		return func() error {
			_, err := b.DeleteArtifactObjects(repo, model.ArtifactDeleteRequest{Keys: keys})
			return err
		}
	}
	link := func(key string) func() error {
		return func() error {
			_, err := b.ArtifactLink(model.ArtifactLinkRequest{Key: key, TTL: "1h"})
			return err
		}
	}
	for _, tc := range []struct {
		name string
		call func() error
	}{
		{"another repository's key", del("darkmem", asKey, "darkraise/darkcloud/42/dist.zip")},
		{"a parent segment", del("darkmem", "darkraise/darkmem/../darkcloud/42/dist.zip")},
		{"an empty segment", del("darkmem", "darkraise/darkmem//42/dist.zip")},
		{"a repository name with a slash", del("darkmem/42", asKey)},
		{"another owner's key", link("someone/darkmem/42/dist.zip")},
		{"a dot segment", link("darkraise/./darkmem/42/dist.zip")},
	} {
		if err := tc.call(); apiStatus(err) != 400 {
			t.Errorf("%s: %v", tc.name, err)
		}
	}
	if got := f.recorded(); len(got) != 0 {
		t.Fatalf("a refused key reached the service: %q", got)
	}
}

func TestSetArtifactKeyKeepsTheKeyOutOfEvents(t *testing.T) {
	b, _, _, f := newASBackend(t)
	f.err = artifactstore.BadInput("the secret must be one line")
	if err := b.SetArtifactKey(model.ArtifactKeyRequest{Which: "job", ID: "JOBKEY12345678", Secret: "job-secret\nvalue"}); apiStatus(err) != 400 {
		t.Fatalf("a refused key: %v", err)
	}
	if evs := b.Events.After(0); len(evs) != 0 {
		t.Fatalf("a refused key wrote events: %+v", evs)
	}
	f.err = nil
	if err := b.SetArtifactKey(model.ArtifactKeyRequest{Which: "job", ID: "JOBKEY12345678", Secret: "job-secret-value"}); err != nil {
		t.Fatal(err)
	}
	// The service writes the one event, with the shortened ID only.
	if evs := b.Events.After(0); len(evs) != 0 {
		t.Fatalf("the backend wrote events of its own: %+v", evs)
	}
}

func TestStatusCarriesTheArtifactStoreSummary(t *testing.T) {
	b, _, _ := newBackend(t)
	if st := b.Status(); st.ArtifactStore != nil {
		t.Fatalf("summary without a service: %+v", st.ArtifactStore)
	}
	f := &fakeAS{}
	b.AS = f
	if st := b.Status(); st.ArtifactStore != nil {
		t.Fatalf("summary while not set up: %+v", st.ArtifactStore)
	}
	f.summary = &model.ArtifactStoreSummary{UsedBytes: 12_400_000_000, CapacityBytes: 50_000_000_000, ReposOn: 2}
	if st := b.Status(); st.ArtifactStore == nil || *st.ArtifactStore != *f.summary {
		t.Fatalf("summary %+v", st.ArtifactStore)
	}
}

func TestFingerprintsCountTheArtifactStore(t *testing.T) {
	b, _, _, f := newASBackend(t)
	prev := b.fingerprints()
	if _, ok := prev["artifact-store"]; !ok {
		t.Fatalf("no artifact-store topic in %v", prev)
	}
	f.version.Add(1)
	cur := b.fingerprints()
	if got := changed(prev, cur); !slices.Equal(got, []string{"artifact-store"}) {
		t.Fatalf("a snapshot change changed %v", got)
	}
	f.summary = &model.ArtifactStoreSummary{UsedBytes: 5, ReposOn: 1}
	if got := changed(cur, b.fingerprints()); !slices.Equal(got, []string{"status"}) {
		t.Fatalf("a summary change changed %v", got)
	}
}

// The daemon starts the artifact store with its config: a block without
// keys answers with the missing admin key in the status, and nothing calls
// RustFS.
func TestRunStartsTheArtifactStore(t *testing.T) {
	o := testOptions(t, cfgYAML+"artifact_store:\n  endpoint: https://s3.example.test\n  bucket: gha-artifacts\n")
	startRun(t, o)
	c := api.NewUnixClient(o.Socket)
	waitFor(t, "the status to name the missing admin key", func() bool {
		st, err := c.Status(context.Background())
		return err == nil && st.ArtifactStore != nil && strings.Contains(st.ArtifactStore.Error, "no admin key set")
	})
}
