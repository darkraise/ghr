package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/model"
)

// ghsFake is fakeBackend's GitHub storage state.
type ghsFake struct {
	snap    model.GitHubStorage
	preview []model.StoragePreview
	err     error // returned by every method that can fail
	calls   []string
}

func (f *fakeBackend) GitHubStorage() model.GitHubStorage { return f.ghs.snap }
func (f *fakeBackend) RefreshGitHubStorage() error {
	f.ghs.calls = append(f.ghs.calls, "refresh")
	return f.ghs.err
}
func (f *fakeBackend) GitHubStoragePreview(cleanup, repo string) ([]model.StoragePreview, error) {
	f.ghs.calls = append(f.ghs.calls, "preview "+cleanup+" "+repo)
	return f.ghs.preview, f.ghs.err
}
func (f *fakeBackend) DeleteGitHubArtifact(_ context.Context, repo string, id int64) error {
	f.ghs.calls = append(f.ghs.calls, fmt.Sprintf("delete-artifact %s %d", repo, id))
	return f.ghs.err
}
func (f *fakeBackend) DeleteGitHubCache(_ context.Context, repo string, id int64) error {
	f.ghs.calls = append(f.ghs.calls, fmt.Sprintf("delete-cache %s %d", repo, id))
	return f.ghs.err
}
func (f *fakeBackend) DeleteGitHubArtifacts(repo string, req model.DeleteArtifactsRequest) (model.OperationStarted, error) {
	f.ghs.calls = append(f.ghs.calls, fmt.Sprintf("delete-artifacts %s %v %s", repo, req.IDs, req.OlderThan))
	return model.OperationStarted{ID: "op-1"}, f.ghs.err
}
func (f *fakeBackend) DeleteGitHubCaches(repo string, req model.DeleteCachesRequest) (model.OperationStarted, error) {
	f.ghs.calls = append(f.ghs.calls, fmt.Sprintf("delete-caches %s %v %t", repo, req.IDs, req.All))
	return model.OperationStarted{ID: "op-2"}, f.ghs.err
}
func (f *fakeBackend) SetGitHubRetention(_ context.Context, repo string, days int) error {
	f.ghs.calls = append(f.ghs.calls, fmt.Sprintf("retention %s %d", repo, days))
	return f.ghs.err
}
func (f *fakeBackend) SetGitHubRetentionAll(days int) (model.OperationStarted, error) {
	f.ghs.calls = append(f.ghs.calls, fmt.Sprintf("retention-all %d", days))
	return model.OperationStarted{ID: "op-3"}, f.ghs.err
}

// ghsCall sends one request to srv and returns its status and body.
func ghsCall(t *testing.T, srv *httptest.Server, method, path, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, strings.TrimSpace(string(b))
}

func TestGitHubStorageRoutes(t *testing.T) {
	b := &fakeBackend{}
	b.ghs.snap = model.GitHubStorage{QuotaError: "x", Repos: []model.RepoStorage{}, Deletions: []model.Deletion{}}
	srv := httptest.NewServer(NewServer(b))
	defer srv.Close()
	for _, tc := range []struct {
		method, path, body string
		status             int
		answer             string
		preview            []model.StoragePreview
	}{
		{"GET", "/github-storage", "", 200, `{"quota_error":"x","repos":[],"deletions":[]}`, nil},
		{"POST", "/github-storage/refresh", "", 202, "", nil},
		{"GET", "/github-storage/preview?cleanup=2d&repo=darkmem", "", 200, `[{"repo":"darkmem","count":2,"bytes":10}]`,
			[]model.StoragePreview{{Repo: "darkmem", Count: 2, Bytes: 10}}},
		{"GET", "/github-storage/preview?cleanup=off", "", 200, `[]`, nil},
		{"POST", "/github-storage/retention", `{"days":7}`, 202, `{"id":"op-3"}`, nil},
		{"POST", "/github-storage/darkmem/artifacts/delete", `{"ids":[1,2]}`, 202, `{"id":"op-1"}`, nil},
		{"POST", "/github-storage/darkmem/artifacts/delete", `{"older_than":"24h"}`, 202, `{"id":"op-1"}`, nil},
		{"DELETE", "/github-storage/darkmem/artifacts/9", "", 204, "", nil},
		{"POST", "/github-storage/darkmem/caches/delete", `{"all":true}`, 202, `{"id":"op-2"}`, nil},
		{"DELETE", "/github-storage/darkmem/caches/8", "", 204, "", nil},
		{"PUT", "/github-storage/darkmem/retention", `{"days":5}`, 204, "", nil},
	} {
		b.ghs.preview = tc.preview
		status, answer := ghsCall(t, srv, tc.method, tc.path, tc.body)
		if status != tc.status || answer != tc.answer {
			t.Errorf("%s %s: %d %s, want %d %s", tc.method, tc.path, status, answer, tc.status, tc.answer)
		}
	}
	want := []string{
		"refresh", "preview 2d darkmem", "preview off ", "retention-all 7",
		"delete-artifacts darkmem [1 2] ", "delete-artifacts darkmem [] 24h", "delete-artifact darkmem 9",
		"delete-caches darkmem [] true", "delete-cache darkmem 8", "retention darkmem 5",
	}
	if !reflect.DeepEqual(b.ghs.calls, want) {
		t.Fatalf("calls %q", b.ghs.calls)
	}
}

func TestGitHubStorageRouteErrors(t *testing.T) {
	b := &fakeBackend{}
	srv := httptest.NewServer(NewServer(b))
	defer srv.Close()
	for _, tc := range []struct{ method, path, body, answer string }{
		{"POST", "/github-storage/retention", `{"days":`, `invalid JSON`},
		{"POST", "/github-storage/darkmem/artifacts/delete", ``, `invalid JSON`},
		{"POST", "/github-storage/darkmem/caches/delete", `[1]`, `invalid JSON`},
		{"PUT", "/github-storage/darkmem/retention", `{"days":"7"}`, `invalid JSON`},
		{"DELETE", "/github-storage/darkmem/artifacts/abc", ``, `artifact id must be a positive integer`},
		{"DELETE", "/github-storage/darkmem/caches/0", ``, `cache id must be a positive integer`},
	} {
		status, answer := ghsCall(t, srv, tc.method, tc.path, tc.body)
		if status != 400 || !strings.Contains(answer, tc.answer) {
			t.Errorf("%s %s %s: %d %s", tc.method, tc.path, tc.body, status, answer)
		}
	}
	if len(b.ghs.calls) != 0 {
		t.Fatalf("a refused request reached the backend: %q", b.ghs.calls)
	}
	retry := time.Date(2026, 10, 13, 13, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		err    error
		status int
		answer string
	}{
		{NotFound("not in the last GitHub storage inventory"), 404, `{"error":"not in the last GitHub storage inventory"}`},
		{Conflict("another GitHub storage operation is running"), 409, `{"error":"another GitHub storage operation is running"}`},
		{BadRequest("give either ids or older_than"), 400, `{"error":"give either ids or older_than"}`},
		{&Error{Status: 429, Msg: "GitHub rate limit; API calls are paused", RetryAt: retry}, 429,
			`{"error":"GitHub rate limit; API calls are paused","retry_at":"2026-10-13T13:00:00Z"}`},
		{&Error{Status: 503, Msg: "GitHub is rejecting the token: Bad credentials"}, 503,
			`{"error":"GitHub is rejecting the token: Bad credentials"}`},
	} {
		b.ghs.err = tc.err
		for _, r := range []struct{ method, path, body string }{
			{"POST", "/github-storage/refresh", ""},
			{"GET", "/github-storage/preview?cleanup=2d", ""},
			{"POST", "/github-storage/retention", `{"days":7}`},
			{"POST", "/github-storage/darkmem/artifacts/delete", `{"ids":[1]}`},
			{"DELETE", "/github-storage/darkmem/artifacts/1", ""},
			{"POST", "/github-storage/darkmem/caches/delete", `{"ids":[1]}`},
			{"DELETE", "/github-storage/darkmem/caches/1", ""},
			{"PUT", "/github-storage/darkmem/retention", `{"days":7}`},
		} {
			status, answer := ghsCall(t, srv, r.method, r.path, r.body)
			if status != tc.status || answer != tc.answer {
				t.Errorf("%s %s with %v: %d %s", r.method, r.path, tc.err, status, answer)
			}
		}
	}
}
