package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/model"
)

// asFake is fakeBackend's self-hosted artifact storage state.
type asFake struct {
	snap  model.ArtifactStore
	test  model.ArtifactStoreTest
	row   model.ArtifactStoreRepo
	rows  []model.ArtifactStoreRepo
	items []model.ArtifactStoreItem
	link  model.ArtifactLink
	err   error // returned by every method that can fail
	calls []string
}

func (f *fakeBackend) ArtifactStore() model.ArtifactStore { return f.as.snap }
func (f *fakeBackend) RefreshArtifactStore() error {
	f.as.calls = append(f.as.calls, "refresh")
	return f.as.err
}
func (f *fakeBackend) TestArtifactStore(context.Context) (model.ArtifactStoreTest, error) {
	f.as.calls = append(f.as.calls, "test")
	return f.as.test, f.as.err
}
func (f *fakeBackend) SetArtifactKey(req model.ArtifactKeyRequest) error {
	f.as.calls = append(f.as.calls, "set-key "+req.Which+" "+req.ID+" "+req.Secret)
	return f.as.err
}
func (f *fakeBackend) SetArtifactRepo(_ context.Context, repo string, req model.ArtifactRepoRequest) (model.ArtifactStoreRepo, error) {
	body, _ := json.Marshal(req)
	f.as.calls = append(f.as.calls, "set-repo "+repo+" "+string(body))
	return f.as.row, f.as.err
}
func (f *fakeBackend) EnablePrivateArtifactRepos(context.Context) ([]model.ArtifactStoreRepo, error) {
	f.as.calls = append(f.as.calls, "enable-private")
	return f.as.rows, f.as.err
}
func (f *fakeBackend) ArtifactRunObjects(_ context.Context, repo string, runID int64) ([]model.ArtifactStoreItem, error) {
	f.as.calls = append(f.as.calls, fmt.Sprintf("run-objects %s %d", repo, runID))
	return f.as.items, f.as.err
}
func (f *fakeBackend) DeleteArtifactObjects(repo string, req model.ArtifactDeleteRequest) (model.OperationStarted, error) {
	f.as.calls = append(f.as.calls, fmt.Sprintf("delete %s %v", repo, req.Keys))
	return model.OperationStarted{ID: "op-7"}, f.as.err
}
func (f *fakeBackend) ArtifactLink(req model.ArtifactLinkRequest) (model.ArtifactLink, error) {
	f.as.calls = append(f.as.calls, "link "+req.Key+" "+req.TTL)
	return f.as.link, f.as.err
}

const (
	asSecret  = "job-secret-value"
	asKeyBody = `{"which":"job","id":"JOBKEY12345678","secret":"` + asSecret + `"}`
)

func TestArtifactStoreRoutes(t *testing.T) {
	at := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	b := &fakeBackend{}
	b.as.snap = model.ArtifactStore{
		Configured: true, Endpoint: "https://s3.example.test", Bucket: "gha-artifacts", Region: "us-east-1",
		AdminKey: &model.ArtifactKeyInfo{ID: "ADMI…1234", SetAt: at}, UsedBytes: 10, Count: 1,
		Repos: []model.ArtifactStoreRepo{}, Deletions: []model.Deletion{},
	}
	b.as.test = model.ArtifactStoreTest{Checks: []model.ArtifactStoreCheck{{Name: "list", OK: true, Detail: "3 objects"}}}
	b.as.row = model.ArtifactStoreRepo{Name: "darkmem", Private: true, Enabled: true, State: "on", Cleanup: "7d", CleanupSource: "default",
		Items: []model.ArtifactStoreItem{}}
	b.as.link = model.ArtifactLink{URL: "https://s3.example.test/gha-artifacts/darkraise/darkmem/42/dist.zip", ExpiresAt: at.Add(time.Hour)}
	item := model.ArtifactStoreItem{Key: "darkraise/darkmem/42/dist.zip", Name: "dist", RunID: 42, Attempt: 1, Bytes: 5, Files: 2, UploadedAt: at}
	srv := httptest.NewServer(NewServer(b))
	defer srv.Close()
	const row = `{"name":"darkmem","private":true,"enabled":true,"state":"on","cleanup":"7d","cleanup_source":"default","bytes":0,"count":0,"items":[]}`
	for _, tc := range []struct {
		method, path, body string
		status             int
		answer             string
		rows               []model.ArtifactStoreRepo
		items              []model.ArtifactStoreItem
	}{
		{"GET", "/artifact-store", "", 200, `{"configured":true,"endpoint":"https://s3.example.test","bucket":"gha-artifacts",` +
			`"region":"us-east-1","admin_key":{"id":"ADMI…1234","set_at":"2026-10-15T12:00:00Z"},"used_bytes":10,"count":1,` +
			`"repos":[],"deletions":[]}`, nil, nil},
		{"POST", "/artifact-store/refresh", "", 202, "", nil, nil},
		{"POST", "/artifact-store/test", "", 200, `{"checks":[{"name":"list","ok":true,"detail":"3 objects"}]}`, nil, nil},
		{"PUT", "/artifact-store/keys", asKeyBody, 204, "", nil, nil},
		{"PUT", "/artifact-store/repos/darkmem", `{"enabled":true}`, 200, row, nil, nil},
		{"PUT", "/artifact-store/repos/darkmem", `{"cleanup":"3d"}`, 200, row, nil, nil},
		{"POST", "/artifact-store/repos/enable-private", "", 200, "[" + row + "]", []model.ArtifactStoreRepo{b.as.row}, nil},
		{"POST", "/artifact-store/repos/enable-private", "", 200, "[]", nil, nil},
		{"GET", "/artifact-store/darkmem/runs/42", "", 200, `[{"key":"darkraise/darkmem/42/dist.zip","name":"dist","run_id":42,` +
			`"attempt":1,"bytes":5,"files":2,"uploaded_at":"2026-10-15T12:00:00Z"}]`, nil, []model.ArtifactStoreItem{item}},
		{"GET", "/artifact-store/darkmem/runs/43", "", 200, "[]", nil, nil},
		{"POST", "/artifact-store/darkmem/artifacts/delete", `{"keys":["darkraise/darkmem/42/dist.zip"]}`, 202, `{"id":"op-7"}`, nil, nil},
		{"POST", "/artifact-store/link", `{"key":"darkraise/darkmem/42/dist.zip","ttl":"1h"}`, 200,
			`{"url":"https://s3.example.test/gha-artifacts/darkraise/darkmem/42/dist.zip","expires_at":"2026-10-15T13:00:00Z"}`, nil, nil},
	} {
		b.as.rows, b.as.items = tc.rows, tc.items
		status, answer := ghsCall(t, srv, tc.method, tc.path, tc.body)
		if status != tc.status || answer != tc.answer {
			t.Errorf("%s %s: %d %s, want %d %s", tc.method, tc.path, status, answer, tc.status, tc.answer)
		}
	}
	want := []string{
		"refresh", "test", "set-key job JOBKEY12345678 " + asSecret,
		`set-repo darkmem {"enabled":true}`, `set-repo darkmem {"cleanup":"3d"}`, "enable-private", "enable-private",
		"run-objects darkmem 42", "run-objects darkmem 43", "delete darkmem [darkraise/darkmem/42/dist.zip]",
		"link darkraise/darkmem/42/dist.zip 1h",
	}
	if !reflect.DeepEqual(b.as.calls, want) {
		t.Fatalf("calls %q", b.as.calls)
	}
}

// A refused request never reaches the backend, and no answer, refusals
// included, repeats the secret the key route was sent.
func TestArtifactStoreRouteErrors(t *testing.T) {
	b := &fakeBackend{}
	srv := httptest.NewServer(NewServer(b))
	defer srv.Close()
	for _, tc := range []struct{ method, path, body, answer string }{
		{"PUT", "/artifact-store/keys", asKeyBody[:len(asKeyBody)-3], `invalid JSON`},
		{"PUT", "/artifact-store/keys", `{"which":"job","id":"JOBKEY12345678","secret":["` + asSecret + `"]}`, `invalid JSON`},
		{"PUT", "/artifact-store/repos/darkmem", `{"enabled":"yes"}`, `invalid JSON`},
		{"POST", "/artifact-store/darkmem/artifacts/delete", `{"keys":"darkraise/darkmem/42/dist.zip"}`, `invalid JSON`},
		{"POST", "/artifact-store/link", ``, `invalid JSON`},
		{"GET", "/artifact-store/darkmem/runs/0", ``, `run id must be a positive integer`},
		{"GET", "/artifact-store/darkmem/runs/abc", ``, `run id must be a positive integer`},
	} {
		status, answer := ghsCall(t, srv, tc.method, tc.path, tc.body)
		if status != 400 || !strings.Contains(answer, tc.answer) || strings.Contains(answer, asSecret) {
			t.Errorf("%s %s %s: %d %s", tc.method, tc.path, tc.body, status, answer)
		}
	}
	if len(b.as.calls) != 0 {
		t.Fatalf("a refused request reached the backend: %q", b.as.calls)
	}
	retry := time.Date(2026, 10, 15, 13, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		err    error
		status int
		answer string
	}{
		{BadRequest("ttl must be 1h, 24h or 7d"), 400, `{"error":"ttl must be 1h, 24h or 7d"}`},
		{NotFound("nope is not a covered repository"), 404, `{"error":"nope is not a covered repository"}`},
		{Conflict("set the job key first"), 409, `{"error":"set the job key first"}`},
		{&Error{Status: 429, Msg: "GitHub storage writes are used up", RetryAt: retry}, 429,
			`{"error":"GitHub storage writes are used up","retry_at":"2026-10-15T13:00:00Z"}`},
		{&Error{Status: 502, Msg: "AccessDenied: Access Denied."}, 502, `{"error":"AccessDenied: Access Denied."}`},
		{&Error{Status: 503, Msg: "ghr is shutting down"}, 503, `{"error":"ghr is shutting down"}`},
	} {
		b.as.err = tc.err
		for _, r := range []struct{ method, path, body string }{
			{"POST", "/artifact-store/refresh", ""},
			{"POST", "/artifact-store/test", ""},
			{"PUT", "/artifact-store/keys", asKeyBody},
			{"PUT", "/artifact-store/repos/darkmem", `{"enabled":true}`},
			{"POST", "/artifact-store/repos/enable-private", ""},
			{"GET", "/artifact-store/darkmem/runs/42", ""},
			{"POST", "/artifact-store/darkmem/artifacts/delete", `{"keys":["darkraise/darkmem/42/dist.zip"]}`},
			{"POST", "/artifact-store/link", `{"key":"darkraise/darkmem/42/dist.zip","ttl":"1h"}`},
		} {
			status, answer := ghsCall(t, srv, r.method, r.path, r.body)
			if status != tc.status || answer != tc.answer {
				t.Errorf("%s %s with %v: %d %s", r.method, r.path, tc.err, status, answer)
			}
		}
	}
}
