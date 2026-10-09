package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
)

type fakeBackend struct {
	patches       []model.ConfigPatch
	added         []model.AddRepoRequest
	removed       []string
	pauseAll      []bool
	token         string
	killed        []string
	killErr       error
	pruneErr      error
	reloadErr     error
	tokenStatus   model.TokenStatus
	tokenCalls    int
	regs          []model.Registration
	regErr        error
	regRepos      []string
	deletedReg    []string
	checked       []string
	startErr      error
	lc            model.LabelCheck
	lcErr         error
	lcRepos       []string
	metrics       model.Metrics
	metricsCalls  int
	activityCalls []string
	historySince  []time.Time
	activity      model.Activity
	updates       []string
	updateErr     error
	avail         []model.AvailableRepo
	availErr      error
	storage       model.Storage
	storageErr    error // returned by every storage method
	choices       []model.ToolchainChoice
	storageCalls  []string
	watched       []string
	unwatched     []string
}

func (f *fakeBackend) AvailableRepos(context.Context) ([]model.AvailableRepo, error) {
	return f.avail, f.availErr
}

func (f *fakeBackend) WatchRepo(ctx context.Context, name string) error {
	f.watched = append(f.watched, name)
	return nil
}

func (f *fakeBackend) UnwatchRepo(name string) error {
	f.unwatched = append(f.unwatched, name)
	return nil
}

func (f *fakeBackend) QueueRunnerUpdate(context.Context) error {
	f.updates = append(f.updates, "queue")
	return f.updateErr
}

func (f *fakeBackend) CancelRunnerUpdate() error {
	f.updates = append(f.updates, "cancel")
	return f.updateErr
}

func (f *fakeBackend) Activity(_ context.Context, window, repo string, loc *time.Location) (model.Activity, error) {
	call := window + "|" + loc.String()
	if repo != "" {
		call += "|" + repo
	}
	f.activityCalls = append(f.activityCalls, call)
	return f.activity, nil
}

func (f *fakeBackend) Metrics() model.Metrics {
	f.metricsCalls++
	return f.metrics
}

func (f *fakeBackend) Reload() ([]string, error) {
	return []string{"labels: duplicate"}, f.reloadErr
}
func (f *fakeBackend) Prune() error { return f.pruneErr }

func (f *fakeBackend) Registrations(ctx context.Context, repo string) ([]model.Registration, error) {
	f.regRepos = append(f.regRepos, repo)
	return f.regs, f.regErr
}
func (f *fakeBackend) DeleteRegistration(ctx context.Context, repo string, id int64) error {
	f.deletedReg = append(f.deletedReg, fmt.Sprintf("%s/%d", repo, id))
	return f.regErr
}

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
func (f *fakeBackend) History(repo, conclusion string, since time.Time, limit int) ([]model.HistoryEntry, error) {
	f.historySince = append(f.historySince, since)
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
	h, err := c.History(ctx, "darkmem", "failure", time.Time{}, 5)
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
	b.reloadErr = BadRequest("config.yaml: bad max")
	var re *Error
	if _, err := c.Reload(context.Background()); !errors.As(err, &re) || re.Status != 400 {
		t.Fatalf("reload error: %v", err)
	}
	if err := c.Prune(context.Background()); err != nil {
		t.Fatal(err)
	}
	b.pruneErr = Conflict("a prune is already running")
	var ae *Error
	if err := c.Prune(context.Background()); !errors.As(err, &ae) || ae.Status != 409 {
		t.Fatalf("overlap: %v", err)
	}
	b.pruneErr = &Error{Status: http.StatusServiceUnavailable, Msg: "runner manager closed"}
	if err := c.Prune(context.Background()); !errors.As(err, &ae) || ae.Status != 503 || ae.Msg != "runner manager closed" {
		t.Fatalf("shutting down: %v", err)
	}
	b.pruneErr = errors.New("disk on fire")
	if err := c.Prune(context.Background()); !errors.As(err, &ae) || ae.Status != 500 || ae.Msg != "disk on fire" {
		t.Fatalf("unmapped error: %v", err)
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

func TestRegistrationRoutes(t *testing.T) {
	c, b := setup(t)
	ctx := context.Background()
	b.regs = []model.Registration{{ID: 1, Name: "linux-1", Status: "offline", Labels: []string{"self-hosted"}}}
	rs, err := c.Registrations(ctx, "darkcloud")
	if err != nil || len(rs) != 1 || rs[0].Name != "linux-1" || !reflect.DeepEqual(rs[0].Labels, []string{"self-hosted"}) {
		t.Fatalf("%+v %v", rs, err)
	}
	b.regs = []model.Registration{}
	rs, err = c.Registrations(ctx, "darkmem")
	if err != nil || rs == nil || len(rs) != 0 {
		t.Fatalf("empty list: %#v %v", rs, err)
	}
	b.regErr = NotFound("unknown repo nope")
	var ae *Error
	if _, err := c.Registrations(ctx, "nope"); !errors.As(err, &ae) || ae.Status != 404 || ae.Msg != "unknown repo nope" {
		t.Fatalf("error: %v", err)
	}
	if !reflect.DeepEqual(b.regRepos, []string{"darkcloud", "darkmem", "nope"}) {
		t.Fatalf("backend saw %v", b.regRepos)
	}
	if err := c.DeleteRegistration(ctx, "darkcloud", 7); !errors.As(err, &ae) || ae.Status != 404 {
		t.Fatalf("delete error: %v", err)
	}
	b.regErr = nil
	if err := c.DeleteRegistration(ctx, "darkcloud", 1); err != nil || !reflect.DeepEqual(b.deletedReg, []string{"darkcloud/7", "darkcloud/1"}) {
		t.Fatalf("%v %v", err, b.deletedReg)
	}
	for _, bad := range []string{"abc", "0", "-3", "99999999999999999999"} {
		req, _ := http.NewRequest(http.MethodDelete, c.Base+"/repos/darkcloud/registrations/"+bad, nil)
		resp, err := c.HTTP.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 400 {
			t.Errorf("id %s: status %d", bad, resp.StatusCode)
		}
	}
	if len(b.deletedReg) != 2 {
		t.Fatalf("a bad id reached the backend: %v", b.deletedReg)
	}
}

func (f *fakeBackend) StartLabelCheck(repo string) error {
	f.checked = append(f.checked, repo)
	return f.startErr
}
func (f *fakeBackend) LabelCheck(repo string) (model.LabelCheck, error) {
	f.lcRepos = append(f.lcRepos, repo)
	return f.lc, f.lcErr
}

func TestLabelCheckRoutes(t *testing.T) {
	c, b := setup(t)
	ctx := context.Background()
	if err := c.StartLabelCheck(ctx, "dark cloud"); err != nil || !reflect.DeepEqual(b.checked, []string{"dark cloud"}) {
		t.Fatalf("%v %v", err, b.checked)
	}
	retry := time.Date(2026, 10, 5, 12, 1, 0, 0, time.UTC)
	b.startErr = &Error{Status: http.StatusTooManyRequests, Msg: "this repo was checked less than a minute ago", RetryAt: retry}
	var ae *Error
	if err := c.StartLabelCheck(ctx, "darkcloud"); !errors.As(err, &ae) || ae.Status != 429 || !ae.RetryAt.Equal(retry) || len(b.checked) != 2 {
		t.Fatalf("throttled start: %v %v", err, b.checked)
	}
	b.lc = model.LabelCheck{State: "done", Groups: []model.LabelGroup{{Labels: []string{"self-hosted"}, Count: 2}}}
	lc, err := c.LabelCheck(ctx, "darkcloud")
	if err != nil || lc.State != "done" || len(lc.Groups) != 1 || lc.Groups[0].Count != 2 {
		t.Fatalf("%+v %v", lc, err)
	}
	b.lc = model.LabelCheck{State: "not_checked", Groups: []model.LabelGroup{}}
	lc, err = c.LabelCheck(ctx, "darkmem")
	if err != nil || lc.State != "not_checked" || lc.Groups == nil || len(lc.Groups) != 0 {
		t.Fatalf("empty: %#v %v", lc, err)
	}
	b.lcErr = NotFound("unknown repo nope")
	if _, err := c.LabelCheck(ctx, "nope"); !errors.As(err, &ae) || ae.Status != 404 {
		t.Fatalf("error: %v", err)
	}
	if !reflect.DeepEqual(b.lcRepos, []string{"darkcloud", "darkmem", "nope"}) {
		t.Fatalf("backend saw %v", b.lcRepos)
	}
}

func TestMetricsRoute(t *testing.T) {
	c, b := setup(t)
	cpu := 12.5
	b.metrics = model.Metrics{DiskPct: 11, CPU: &cpu, Samples: []model.MetricSample{{Live: 1, Queued: 2}}}
	m, err := c.Metrics(context.Background())
	if err != nil || b.metricsCalls != 1 || m.DiskPct != 11 || m.CPU == nil || *m.CPU != 12.5 || len(m.Samples) != 1 || m.Samples[0].Queued != 2 {
		t.Fatalf("%+v %v", m, err)
	}
	b.metrics = model.Metrics{DiskPct: 40, Samples: []model.MetricSample{}}
	m, err = c.Metrics(context.Background())
	if err != nil || b.metricsCalls != 2 || m.DiskPct != 40 || m.CPU != nil || m.Samples == nil || len(m.Samples) != 0 {
		t.Fatalf("second call %#v %v", m, err)
	}
}

// A daemon older than the client answers a route it lacks with the router's
// plain-text 404 or 405; the client names the cause instead.
func TestOlderDaemonAsksForARestart(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /repos/{name}", func(w http.ResponseWriter, r *http.Request) {})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := &Client{Base: srv.URL, HTTP: &http.Client{Timeout: 5 * time.Second}}
	_, err := c.AvailableRepos(context.Background()) // 405: the path matches another method
	if err == nil || !strings.Contains(err.Error(), "systemctl restart ghr") {
		t.Fatalf("405: %v", err)
	}
	if err := c.QueueRunnerUpdate(context.Background()); err == nil || !strings.Contains(err.Error(), "systemctl restart ghr") {
		t.Fatalf("404: %v", err)
	}
}

func TestResetWebPasswordPostsToTheSocketRoute(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Method + " " + r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	c := &Client{Base: srv.URL, HTTP: &http.Client{Timeout: 5 * time.Second}}
	if err := c.ResetWebPassword(context.Background()); err != nil || got != "POST /web/reset-password" {
		t.Fatalf("err %v request %q", err, got)
	}
}

func TestSetWebPasswordPostsTheBody(t *testing.T) {
	var got, body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Method + " " + r.URL.Path
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	c := &Client{Base: srv.URL, HTTP: &http.Client{Timeout: 5 * time.Second}}
	if err := c.SetWebPassword(context.Background(), "a <long> secret"); err != nil || got != "POST /web/set-password" {
		t.Fatalf("err %v request %q", err, got)
	}
	var sent map[string]string
	if err := json.Unmarshal([]byte(body), &sent); err != nil || sent["password"] != "a <long> secret" {
		t.Fatalf("body %q: %v", body, err)
	}
}

func TestActivityRoute(t *testing.T) {
	c, b := setup(t)
	ctx := context.Background()
	b.activity = model.Activity{Window: "1h", TZ: "Asia/Ho_Chi_Minh"}
	var got model.Activity
	if err := c.call(ctx, http.MethodGet, "/activity?window=1h&tz=Asia/Ho_Chi_Minh", nil, &got); err != nil || got.Window != "1h" {
		t.Fatalf("%+v %v", got, err)
	}
	if err := c.call(ctx, http.MethodGet, "/activity?window=24h", nil, &got); err != nil {
		t.Fatal(err)
	}
	if err := c.call(ctx, http.MethodGet, "/activity?window=24h&repo=ghr", nil, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(b.activityCalls, []string{"1h|Asia/Ho_Chi_Minh", "24h|Local", "24h|Local|ghr"}) {
		t.Fatalf("backend saw %v", b.activityCalls)
	}
	for _, q := range []string{"window=2h", "window=1h&tz=Mars/Olympus_Mons", ""} {
		err := c.call(ctx, http.MethodGet, "/activity?"+q, nil, &got)
		var ae *Error
		if !errors.As(err, &ae) || ae.Status != http.StatusBadRequest {
			t.Fatalf("%q: %v", q, err)
		}
	}
	if len(b.activityCalls) != 3 {
		t.Fatalf("a bad request reached the backend: %v", b.activityCalls)
	}
}

func TestHistorySinceParameter(t *testing.T) {
	c, b := setup(t)
	ctx := context.Background()
	var got []model.HistoryEntry
	for _, q := range []string{"since=2026-10-01T18:00:00%2B07:00", "since=", ""} {
		if err := c.call(ctx, http.MethodGet, "/history?"+q, nil, &got); err != nil {
			t.Fatalf("%q: %v", q, err)
		}
	}
	want := time.Date(2026, 10, 1, 11, 0, 0, 0, time.UTC)
	if len(b.historySince) != 3 || !b.historySince[0].Equal(want) || !b.historySince[1].IsZero() || !b.historySince[2].IsZero() {
		t.Fatalf("backend saw %v", b.historySince)
	}
	err := c.call(ctx, http.MethodGet, "/history?since=yesterday", nil, &got)
	var ae *Error
	if !errors.As(err, &ae) || ae.Status != http.StatusBadRequest || len(b.historySince) != 3 {
		t.Fatalf("unparsable since: %v, backend saw %d calls", err, len(b.historySince))
	}
}

func TestClientHistorySendsSince(t *testing.T) {
	c, b := setup(t)
	ctx := context.Background()
	since := time.Date(2026, 10, 1, 18, 0, 0, 500, time.FixedZone("ICT", 7*60*60))
	if _, err := c.History(ctx, "", "", since, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := c.History(ctx, "", "", time.Time{}, 0); err != nil {
		t.Fatal(err)
	}
	if len(b.historySince) != 2 || !b.historySince[0].Equal(since) || !b.historySince[1].IsZero() {
		t.Fatalf("backend saw %v", b.historySince)
	}

	// The zero time round-trips as zero, so only the raw query shows that it
	// was left out rather than sent.
	var raw []url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw = append(raw, r.URL.Query())
		fmt.Fprint(w, "[]")
	}))
	defer srv.Close()
	rc := &Client{Base: srv.URL, HTTP: srv.Client()}
	if _, err := rc.History(ctx, "", "", time.Time{}, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := rc.History(ctx, "", "", since, 0); err != nil {
		t.Fatal(err)
	}
	if len(raw) != 2 || raw[0].Has("since") || raw[1].Get("since") != "2026-10-01T18:00:00.0000005+07:00" {
		t.Fatalf("raw queries %v", raw)
	}
}

func TestWatchRoutes(t *testing.T) {
	c, b := setup(t)
	ctx := context.Background()
	if err := c.call(ctx, http.MethodPost, "/watch", strings.NewReader(`{"name":"docs"}`), nil); err != nil {
		t.Fatal(err)
	}
	if err := c.call(ctx, http.MethodDelete, "/watch/old-docs", nil, nil); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(b.watched, []string{"docs"}) || !reflect.DeepEqual(b.unwatched, []string{"old-docs"}) {
		t.Fatalf("watched %v unwatched %v", b.watched, b.unwatched)
	}
}
