package daemon

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/changes"
	"github.com/darkraise/ghr/internal/ghstorage"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
)

// fakeGHS records every call as "<method> <args>" and answers with err.
type fakeGHS struct {
	mu      sync.Mutex
	calls   []string
	snap    model.GitHubStorage
	alert   *model.StorageAlert
	preview []model.StoragePreview
	err     error
	version atomic.Int64
}

func (f *fakeGHS) record(format string, args ...any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fmt.Sprintf(format, args...))
}

func (f *fakeGHS) recorded() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func (f *fakeGHS) Snapshot() model.GitHubStorage { return f.snap }
func (f *fakeGHS) Alert() *model.StorageAlert    { return f.alert }
func (f *fakeGHS) Version() int64                { return f.version.Load() }
func (f *fakeGHS) Refresh() error                { f.record("refresh"); return f.err }
func (f *fakeGHS) Kick()                         { f.record("kick") }
func (f *fakeGHS) TokenChanged()                 { f.record("token-changed") }
func (f *fakeGHS) PolicyChanged()                { f.record("policy") }
func (f *fakeGHS) Preview(cleanup, repo string) ([]model.StoragePreview, error) {
	f.record("preview %s %s", cleanup, repo)
	return f.preview, f.err
}
func (f *fakeGHS) DeleteArtifact(_ context.Context, repo string, id int64) error {
	f.record("delete-artifact %s %d", repo, id)
	return f.err
}
func (f *fakeGHS) DeleteCache(_ context.Context, repo string, id int64) error {
	f.record("delete-cache %s %d", repo, id)
	return f.err
}
func (f *fakeGHS) DeleteArtifacts(repo string, req model.DeleteArtifactsRequest) (string, error) {
	f.record("delete-artifacts %s %v %s", repo, req.IDs, req.OlderThan)
	return "op-1", f.err
}
func (f *fakeGHS) DeleteCaches(repo string, req model.DeleteCachesRequest) (string, error) {
	f.record("delete-caches %s %v %t", repo, req.IDs, req.All)
	return "op-2", f.err
}
func (f *fakeGHS) SetRetention(_ context.Context, repo string, days int) error {
	f.record("retention %s %d", repo, days)
	return f.err
}
func (f *fakeGHS) SetRetentionAll(days int) (string, error) {
	f.record("retention-all %d", days)
	return "op-3", f.err
}

func newGHSBackend(t *testing.T) (*Backend, *fakeManager, *fakeGHS) {
	t.Helper()
	b, m, _ := newBackend(t)
	f := &fakeGHS{}
	b.GHS = f
	return b, m, f
}

// ghsCalls are the backend's GitHub storage methods, each named by what the
// fake records for it.
func ghsCalls(b *Backend) map[string]func() error {
	ctx := context.Background()
	return map[string]func() error{
		"refresh":         b.RefreshGitHubStorage,
		"delete-artifact": func() error { return b.DeleteGitHubArtifact(ctx, "darkmem", 7) },
		"delete-cache":    func() error { return b.DeleteGitHubCache(ctx, "darkmem", 8) },
		"delete-artifacts": func() error {
			_, err := b.DeleteGitHubArtifacts("darkmem", model.DeleteArtifactsRequest{IDs: []int64{1}})
			return err
		},
		"delete-caches": func() error {
			_, err := b.DeleteGitHubCaches("darkmem", model.DeleteCachesRequest{All: true})
			return err
		},
		"retention":     func() error { return b.SetGitHubRetention(ctx, "darkmem", 7) },
		"retention-all": func() error { _, err := b.SetGitHubRetentionAll(7); return err },
		"preview":       func() error { _, err := b.GitHubStoragePreview("2d", ""); return err },
	}
}

func TestGitHubStorageCallsReachTheService(t *testing.T) {
	b, _, f := newGHSBackend(t)
	ctx := context.Background()
	f.snap = model.GitHubStorage{QuotaError: "x"}
	f.preview = []model.StoragePreview{{Repo: "darkmem", Count: 2, Bytes: 10}}
	if st := b.GitHubStorage(); st.QuotaError != "x" {
		t.Fatalf("snapshot %+v", st)
	}
	ps, err := b.GitHubStoragePreview("2d", "darkmem")
	if err != nil || len(ps) != 1 || ps[0].Count != 2 {
		t.Fatalf("preview %+v, %v", ps, err)
	}
	if err := b.RefreshGitHubStorage(); err != nil {
		t.Fatal(err)
	}
	if err := b.DeleteGitHubArtifact(ctx, "DarkMem", 7); err != nil {
		t.Fatal(err)
	}
	if err := b.DeleteGitHubCache(ctx, "darkmem", 8); err != nil {
		t.Fatal(err)
	}
	if err := b.SetGitHubRetention(ctx, "darkmem", 5); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		call func() (model.OperationStarted, error)
		id   string
	}{
		{func() (model.OperationStarted, error) {
			return b.DeleteGitHubArtifacts("darkmem", model.DeleteArtifactsRequest{OlderThan: "24h"})
		}, "op-1"},
		{func() (model.OperationStarted, error) {
			return b.DeleteGitHubCaches("darkmem", model.DeleteCachesRequest{IDs: []int64{3, 4}})
		}, "op-2"},
		{func() (model.OperationStarted, error) { return b.SetGitHubRetentionAll(3) }, "op-3"},
	} {
		op, err := tc.call()
		if err != nil || op.ID != tc.id {
			t.Fatalf("operation %+v, %v; want %s", op, err, tc.id)
		}
	}
	want := []string{
		"preview 2d darkmem", "refresh", "delete-artifact DarkMem 7", "delete-cache darkmem 8", "retention darkmem 5",
		"delete-artifacts darkmem [] 24h", "delete-caches darkmem [3 4] false", "retention-all 3",
	}
	if got := f.recorded(); !slices.Equal(got, want) {
		t.Fatalf("calls %q", got)
	}
}

func TestGitHubStorageErrorsMapToStatuses(t *testing.T) {
	b, _, f := newGHSBackend(t)
	retry := time.Date(2026, 10, 13, 13, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		err  error
		want int
	}{
		{ghstorage.BadInput("give either ids or older_than"), 400},
		{ghstorage.ErrUnknownRepo, 404},
		{ghstorage.ErrBusy, 409},
		{ghstorage.ErrOperationRunning, 409},
		{&ghstorage.BudgetError{RetryAt: retry}, 429},
		{ghstorage.ErrClosed, 503},
	} {
		f.err = tc.err
		for name, call := range ghsCalls(b) {
			err := call()
			if apiStatus(err) != tc.want || apiMsg(err) != tc.err.Error() {
				t.Errorf("%s with %v: status %d message %q, want %d", name, tc.err, apiStatus(err), apiMsg(err), tc.want)
			}
		}
	}
	f.err = &ghstorage.BudgetError{RetryAt: retry}
	var ae *api.Error
	if err := b.RefreshGitHubStorage(); !errors.As(err, &ae) || !ae.RetryAt.Equal(retry) {
		t.Fatalf("budget retry time: %v", err)
	}
	f.err = errors.New("github: 403 Resource not accessible")
	if err := b.DeleteGitHubArtifact(context.Background(), "darkmem", 1); err != f.err {
		t.Fatalf("an unexpected error must pass through unchanged: %v", err)
	}
}

// A single write GitHub refuses for a missing PAT permission names the
// permission, as the repository rows do, instead of GitHub's raw text.
func TestGitHubStorageWritesNameTheMissingPermission(t *testing.T) {
	b, _, f := newGHSBackend(t)
	ctx := context.Background()
	f.err = &github.APIError{Status: 403, Kind: github.ErrAuth, Message: "Resource not accessible by personal access token", Permissions: "actions=write"}
	const want = "token lacks actions=write on darkraise/darkmem: grant it in the PAT settings"
	for name, call := range map[string]func() error{
		"delete-artifact": func() error { return b.DeleteGitHubArtifact(ctx, "darkmem", 7) },
		"delete-cache":    func() error { return b.DeleteGitHubCache(ctx, "darkmem", 8) },
		"retention":       func() error { return b.SetGitHubRetention(ctx, "darkmem", 7) },
	} {
		if err := call(); apiStatus(err) != 403 || apiMsg(err) != want {
			t.Errorf("%s: status %d message %q", name, apiStatus(err), apiMsg(err))
		}
	}
}

// Every call that reaches GitHub waits while the token is rejected or a
// rate limit pauses the API; reading the snapshot or a preview does not.
func TestGitHubStorageWaitsForGitHub(t *testing.T) {
	b, m, f := newGHSBackend(t)
	m.degraded = "Bad credentials"
	for name, call := range ghsCalls(b) {
		want := 503
		if name == "preview" {
			want = 0
		}
		if err := call(); apiStatus(err) != want {
			t.Errorf("%s while degraded: %v", name, err)
		}
	}
	m.degraded = ""
	m.paused = time.Now().Add(time.Hour)
	for name, call := range ghsCalls(b) {
		want := 429
		if name == "preview" {
			want = 0
		}
		if err := call(); apiStatus(err) != want {
			t.Errorf("%s while paused: %v", name, err)
		}
	}
	if got := f.recorded(); !slices.Equal(got, []string{"preview 2d ", "preview 2d "}) {
		t.Fatalf("calls %q", got)
	}
}

func TestStatusCarriesTheStorageAlert(t *testing.T) {
	b, _, _ := newBackend(t)
	if st := b.Status(); st.GitHubStorage != nil {
		t.Fatalf("alert without a service: %+v", st.GitHubStorage)
	}
	f := &fakeGHS{}
	b.GHS = f
	if st := b.Status(); st.GitHubStorage != nil {
		t.Fatalf("alert without a quota: %+v", st.GitHubStorage)
	}
	f.alert = &model.StorageAlert{Level: "exhausted", Month: "2026-10", UsedPct: 103}
	if st := b.Status(); st.GitHubStorage == nil || st.GitHubStorage.Level != "exhausted" || st.GitHubStorage.UsedPct != 103 {
		t.Fatalf("alert %+v", st.GitHubStorage)
	}
}

func TestTokenReloadAndPolicyReachTheStorageService(t *testing.T) {
	b, _, f := newGHSBackend(t)
	ctx := context.Background()
	if err := b.SetToken(ctx, "tok2"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Reload(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b.Store.TokenPath, []byte("tok3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Reload(); err != nil {
		t.Fatal(err)
	}
	all := "all"
	if err := b.PatchConfig(ctx, model.ConfigPatch{Mode: &all}); err != nil {
		t.Fatal(err)
	}
	if err := patchJSON(t, b, `{"github_storage":{"cleanup_max_age":"2d"}}`); err != nil {
		t.Fatal(err)
	}
	if err := patchJSON(t, b, `{"github_storage":{"cleanup_max_age":"1h"}}`); apiStatus(err) != 400 {
		t.Fatalf("a refused patch: %v", err)
	}
	want := []string{"token-changed", "kick", "token-changed", "policy"}
	if got := f.recorded(); !slices.Equal(got, want) {
		t.Fatalf("calls %q", got)
	}
}

func TestFingerprintsCountGitHubStorage(t *testing.T) {
	b, _, f := newGHSBackend(t)
	prev := b.fingerprints()
	if _, ok := prev["github-storage"]; !ok {
		t.Fatalf("no github-storage topic in %v", prev)
	}
	f.version.Add(1)
	cur := b.fingerprints()
	if got := changed(prev, cur); !slices.Equal(got, []string{"github-storage"}) {
		t.Fatalf("a snapshot change changed %v", got)
	}
	f.alert = &model.StorageAlert{Level: "warn", Month: "2026-10", UsedPct: 72}
	if got := changed(cur, b.fingerprints()); !slices.Equal(got, []string{"status"}) {
		t.Fatalf("an alert change changed %v", got)
	}
}

func TestStorageErrText(t *testing.T) {
	text := storageErrText("darkraise")
	lacks := &github.APIError{Status: 403, Kind: github.ErrAuth, Message: "Resource not accessible", Permissions: "actions=read"}
	if got := text(lacks, "darkmem"); got != "token lacks actions=read on darkraise/darkmem: grant it in the PAT settings" {
		t.Fatalf("repository error %q", got)
	}
	missing := &github.APIError{Status: 404, Kind: github.ErrNotFound, Message: "Not Found"}
	if got := text(missing, ""); got != "github: 404 Not Found" {
		t.Fatalf("listing error %q", got)
	}
}

// The daemon starts the storage service and counts its topic: with an
// hour-long tick only wakes publish, and the pass a reload asks for changes
// the snapshot after the stream's baseline.
func TestRunStreamsGHStorage(t *testing.T) {
	old := hubOptions
	hubOptions = changes.Options{Tick: time.Hour}
	t.Cleanup(func() { hubOptions = old })
	o := testOptions(t, cfgYAML)
	reload := make(chan os.Signal, 1)
	o.Reload = reload
	startRun(t, o)
	c := api.NewUnixClient(o.Socket)
	waitFor(t, "the socket to serve status", func() bool { _, err := c.Status(context.Background()); return err == nil })
	req, err := http.NewRequest(http.MethodGet, "http://ghr/changes", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := (&http.Client{Transport: c.HTTP.Transport}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	frames := readFrames(resp.Body)
	if f := nextFrame(t, frames, "hello"); f != "event: hello\ndata: {}\n" {
		t.Fatalf("first frame %q", f)
	}
	reload <- os.Interrupt
	for !strings.Contains(nextFrame(t, frames, `a change naming "github-storage"`), `"github-storage"`) {
	}
}
