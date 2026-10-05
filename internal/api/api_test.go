package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
)

type fakeBackend struct {
	patches     []model.ConfigPatch
	added       []model.AddRepoRequest
	removed     []string
	pauseAll    []bool
	token       string
	killed      []string
	killErr     error
	pruneErr    error
	tokenStatus model.TokenStatus
	tokenCalls  int
}

func (f *fakeBackend) Reload() ([]string, error) { return []string{"labels: duplicate"}, nil }
func (f *fakeBackend) Prune() error              { return f.pruneErr }

func (f *fakeBackend) Token() model.TokenStatus {
	f.tokenCalls++
	return f.tokenStatus
}

func (f *fakeBackend) Status() model.Status {
	return model.Status{Mode: "queue", GlobalMax: 2, Repos: []model.RepoStatus{{Name: "darkcloud", Max: 1, Queued: 2}}}
}
func (f *fakeBackend) EventsAfter(seq int64) []model.Event {
	if seq >= 1 {
		return nil
	}
	return []model.Event{{Seq: 1, Level: "info", Msg: "hello"}}
}
func (f *fakeBackend) History(repo, conclusion string, limit int) ([]model.HistoryEntry, error) {
	return []model.HistoryEntry{{ID: "a", Repo: repo, Conclusion: conclusion, RunNumber: "7"}}, nil
}
func (f *fakeBackend) RunnerLog(id, cursor string) (model.LogChunk, error) {
	if id != "aaaaaa" {
		return model.LogChunk{}, NotFound("unknown runner " + id)
	}
	return model.LogChunk{Data: "log after " + cursor, Next: cursor + "+"}, nil
}
func (f *fakeBackend) RunnerContainers(ctx context.Context, id string) ([]model.Container, error) {
	if id != "aaaaaa" {
		return nil, NotFound("unknown runner " + id)
	}
	return []model.Container{{ID: "c1", Name: "ghr-aaaaaa-db-1", Image: "postgres", State: "running", Project: "ghr-aaaaaa"}}, nil
}
func (f *fakeBackend) RunnerSteps(ctx context.Context, id string) ([]model.Step, error) {
	return []model.Step{{Number: 1, Name: "checkout", Status: "completed", Conclusion: "success"}}, nil
}
func (f *fakeBackend) Config() any { return map[string]any{"owner": "darkraise"} }
func (f *fakeBackend) PatchConfig(p model.ConfigPatch) error {
	if p.GlobalMax != nil && *p.GlobalMax < 1 {
		return BadRequest("global_max must be >= 1")
	}
	f.patches = append(f.patches, p)
	return nil
}
func (f *fakeBackend) AddRepo(ctx context.Context, req model.AddRepoRequest) error {
	if !req.AllowPublic && req.Name == "public" {
		return Conflict("public repo")
	}
	f.added = append(f.added, req)
	return nil
}
func (f *fakeBackend) RemoveRepo(name string) error { f.removed = append(f.removed, name); return nil }
func (f *fakeBackend) SetPausedAll(p bool) error    { f.pauseAll = append(f.pauseAll, p); return nil }
func (f *fakeBackend) SetToken(ctx context.Context, t string) error {
	f.token = t
	return nil
}
func (f *fakeBackend) KillRunner(ctx context.Context, id string) error {
	f.killed = append(f.killed, id)
	return f.killErr
}

func setup(t *testing.T) (*Client, *fakeBackend) {
	t.Helper()
	b := &fakeBackend{}
	srv := httptest.NewServer(NewServer(b))
	t.Cleanup(srv.Close)
	return &Client{Base: srv.URL, HTTP: &http.Client{Timeout: 5 * time.Second}}, b
}

func TestStatusEventsHistory(t *testing.T) {
	c, _ := setup(t)
	ctx := context.Background()
	st, err := c.Status(ctx)
	if err != nil || st.Repos[0].Queued != 2 {
		t.Fatalf("status %+v err %v", st, err)
	}
	ev, err := c.Events(ctx, 0)
	if err != nil || len(ev) != 1 {
		t.Fatalf("events %+v err %v", ev, err)
	}
	ev, err = c.Events(ctx, 1)
	if err != nil || ev == nil || len(ev) != 0 {
		t.Fatalf("events after 1: %+v err %v", ev, err)
	}
	h, err := c.History(ctx, "darkmem", "failure", 5)
	if err != nil || h[0].Repo != "darkmem" || h[0].Conclusion != "failure" {
		t.Fatalf("history %+v err %v", h, err)
	}
}

func TestLogStepsConfig(t *testing.T) {
	c, _ := setup(t)
	ctx := context.Background()
	l, err := c.Log(ctx, "aaaaaa", "Runner_1.log=10&Worker_2.log=3")
	if err != nil || l.Data != "log after Runner_1.log=10&Worker_2.log=3" || l.Next != "Runner_1.log=10&Worker_2.log=3+" {
		t.Fatalf("log %+v err %v", l, err)
	}
	_, err = c.Log(ctx, "zzzzzz", "")
	var ae *Error
	if !errors.As(err, &ae) || ae.Status != 404 {
		t.Fatalf("unknown runner err %v", err)
	}
	cs, err := c.Containers(ctx, "aaaaaa")
	if err != nil || len(cs) != 1 || cs[0].Project != "ghr-aaaaaa" {
		t.Fatalf("containers %+v err %v", cs, err)
	}
	if _, err := c.Containers(ctx, "zzzzzz"); !errors.As(err, &ae) || ae.Status != 404 {
		t.Fatalf("unknown runner containers err %v", err)
	}
	s, err := c.Steps(ctx, "aaaaaa")
	if err != nil || s[0].Name != "checkout" {
		t.Fatalf("steps %+v err %v", s, err)
	}
	var cfg map[string]any
	if err := c.Config(ctx, &cfg); err != nil || cfg["owner"] != "darkraise" {
		t.Fatalf("config %v err %v", cfg, err)
	}
}

func TestMutations(t *testing.T) {
	c, b := setup(t)
	ctx := context.Background()
	three := 3
	if err := c.PatchConfig(ctx, model.ConfigPatch{GlobalMax: &three}); err != nil {
		t.Fatal(err)
	}
	zero := 0
	err := c.PatchConfig(ctx, model.ConfigPatch{GlobalMax: &zero})
	var ae *Error
	if !errors.As(err, &ae) || ae.Status != 400 || ae.Msg != "global_max must be >= 1" {
		t.Fatalf("validation err %v", err)
	}
	if err := c.Pause(ctx, "darkcloud"); err != nil {
		t.Fatal(err)
	}
	if err := c.Resume(ctx, "darkcloud"); err != nil {
		t.Fatal(err)
	}
	if !*b.patches[1].Repos["darkcloud"].Paused || *b.patches[2].Repos["darkcloud"].Paused {
		t.Fatalf("pause patches %+v", b.patches)
	}
	if err := c.AddRepo(ctx, model.AddRepoRequest{Name: "public"}); !errors.As(err, &ae) || ae.Status != 409 {
		t.Fatalf("public repo err %v", err)
	}
	if err := c.AddRepo(ctx, model.AddRepoRequest{Name: "newrepo", Labels: []string{"x"}}); err != nil {
		t.Fatal(err)
	}
	if err := c.RemoveRepo(ctx, "old"); err != nil {
		t.Fatal(err)
	}
	if err := c.PauseAll(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.ResumeAll(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.SetToken(ctx, "  github_pat_x\n"); err != nil {
		t.Fatal(err)
	}
	if err := c.SetToken(ctx, "   "); !errors.As(err, &ae) || ae.Status != 400 {
		t.Fatalf("empty token err %v", err)
	}
	if err := c.Kill(ctx, "aaaaaa"); err != nil {
		t.Fatal(err)
	}
	if b.added[0].Name != "newrepo" || b.removed[0] != "old" || len(b.pauseAll) != 2 || b.token != "github_pat_x" || b.killed[0] != "aaaaaa" {
		t.Fatalf("backend %+v", b)
	}
}

func TestUnreachableDaemon(t *testing.T) {
	c := NewUnixClient("/nonexistent/ghr.sock")
	if _, err := c.Status(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

func TestGitHubErrorsMapToStatuses(t *testing.T) {
	c, b := setup(t)
	retry := time.Date(2026, 10, 5, 12, 1, 0, 0, time.UTC)
	for _, tc := range []struct {
		err    error
		status int
		retry  bool
	}{
		{&github.APIError{Status: 403, Kind: github.ErrRateLimit, Message: "rate", RetryAt: retry}, 429, true},
		{&github.APIError{Status: 401, Kind: github.ErrAuth, Message: "Bad credentials"}, 403, false},
		{&github.APIError{Status: 404, Kind: github.ErrNotFound, Message: "Not Found"}, 404, false},
		{&github.APIError{Status: 422, Kind: github.ErrUnprocessable, Message: "busy"}, 409, false},
		{&github.APIError{Status: 503, Kind: github.ErrServer, Message: "down"}, 502, false},
		{errors.New("plain"), 500, false},
		{Conflict("already"), 409, false},
	} {
		b.killErr = tc.err
		err := c.Kill(context.Background(), "aaaaaa")
		var ae *Error
		if !errors.As(err, &ae) || ae.Status != tc.status || ae.RetryAt.Equal(retry) != tc.retry {
			t.Errorf("%v: got %#v", tc.err, err)
		}
	}
}

func TestReloadAndPrune(t *testing.T) {
	c, b := setup(t)
	ws, err := c.Reload(context.Background())
	if err != nil || len(ws) != 1 || ws[0] != "labels: duplicate" {
		t.Fatalf("reload: %v %v", ws, err)
	}
	if err := c.Prune(context.Background()); err != nil {
		t.Fatal(err)
	}
	b.pruneErr = Conflict("a prune is already running")
	var ae *Error
	if err := c.Prune(context.Background()); !errors.As(err, &ae) || ae.Status != 409 {
		t.Fatalf("overlap: %v", err)
	}
}

func TestTokenStatus(t *testing.T) {
	c, b := setup(t)
	exp := time.Date(2026, 12, 31, 23, 59, 59, 0, time.UTC)
	rem := 4800
	b.tokenStatus = model.TokenStatus{State: "ok", ExpiresAt: &exp, RateRemaining: &rem}
	ts, err := c.Token(context.Background())
	if err != nil || b.tokenCalls != 1 || ts.State != "ok" || ts.ExpiresAt == nil || !ts.ExpiresAt.Equal(exp) || *ts.RateRemaining != 4800 {
		t.Fatalf("%+v %v calls %d", ts, err, b.tokenCalls)
	}
	b.tokenStatus = model.TokenStatus{State: "rejected", Reason: "GitHub rejected the token"}
	ts, err = c.Token(context.Background())
	if err != nil || b.tokenCalls != 2 || ts.State != "rejected" || ts.Reason != "GitHub rejected the token" || ts.ExpiresAt != nil || ts.RateRemaining != nil || ts.CheckedAt != nil {
		t.Fatalf("second call %+v %v calls %d", ts, err, b.tokenCalls)
	}
}
