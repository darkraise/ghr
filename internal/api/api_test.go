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
	"sync"
	"sync/atomic"
	"testing"
	"testing/iotest"
	"testing/synctest"
	"time"

	"github.com/darkraise/ghr/internal/changes"
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
	watchErr      error // returned by WatchRepo and UnwatchRepo
	actions       model.Actions
	actionsCalls  int
	runCalls      []string       // "<method> <repo> <id>", plus the attempt for run calls
	runErr        error          // returned by the five run details methods
	rawBody       io.Reader      // what RawJobLog streams; "raw log" when nil
	rawSize       int64          // rawBody's length
	rawDelay      time.Duration  // RawJobLog sleeps this long before answering
	rawClosed     atomic.Int32   // RawJobLog readers closed
	hub           *changes.Hub   // Subscribe's hub; nil answers changes.ErrNotReady
	subMu         sync.Mutex     // guards subs
	subs          []*changes.Sub // subscriptions Subscribe opened
}

func (f *fakeBackend) AvailableRepos(context.Context) ([]model.AvailableRepo, error) {
	return f.avail, f.availErr
}

func (f *fakeBackend) Subscribe() (*changes.Sub, error) {
	if f.hub == nil {
		return nil, changes.ErrNotReady
	}
	s, err := f.hub.Subscribe()
	if err == nil {
		f.subMu.Lock()
		f.subs = append(f.subs, s)
		f.subMu.Unlock()
	}
	return s, err
}

func (f *fakeBackend) WatchRepo(ctx context.Context, name string) error {
	f.watched = append(f.watched, name)
	return f.watchErr
}

func (f *fakeBackend) UnwatchRepo(name string) error {
	f.unwatched = append(f.unwatched, name)
	return f.watchErr
}

func (f *fakeBackend) RunDetail(ctx context.Context, repo string, id int64, attempt int) (model.RunDetail, error) {
	f.runCalls = append(f.runCalls, fmt.Sprintf("RunDetail %s %d %d", repo, id, attempt))
	return model.RunDetail{Repo: repo, ID: id, Attempt: attempt}, f.runErr
}

func (f *fakeBackend) RunAnnotations(ctx context.Context, repo string, id int64, attempt int) (model.RunAnnotations, error) {
	f.runCalls = append(f.runCalls, fmt.Sprintf("RunAnnotations %s %d %d", repo, id, attempt))
	return model.RunAnnotations{Items: []model.Annotation{}, Omitted: map[int64]int{id: 1}}, f.runErr
}

func (f *fakeBackend) JobLog(ctx context.Context, repo string, jobID int64) (model.JobLog, error) {
	f.runCalls = append(f.runCalls, fmt.Sprintf("JobLog %s %d", repo, jobID))
	return model.JobLog{JobID: jobID, Steps: []model.LogStep{}}, f.runErr
}

func (f *fakeBackend) RawJobLog(ctx context.Context, repo string, jobID int64) (io.ReadCloser, int64, error) {
	f.runCalls = append(f.runCalls, fmt.Sprintf("RawJobLog %s %d", repo, jobID))
	time.Sleep(f.rawDelay)
	if f.runErr != nil {
		return nil, 0, f.runErr
	}
	body, size := f.rawBody, f.rawSize
	if body == nil {
		body, size = strings.NewReader("raw log"), 7
	}
	return &countingBody{Reader: body, closed: &f.rawClosed}, size, nil
}

type countingBody struct {
	io.Reader
	closed *atomic.Int32
}

func (c *countingBody) Close() error {
	c.closed.Add(1)
	return nil
}

func (f *fakeBackend) ArtifactDownload(ctx context.Context, repo string, id int64) (model.DownloadURL, error) {
	f.runCalls = append(f.runCalls, fmt.Sprintf("ArtifactDownload %s %d", repo, id))
	return model.DownloadURL{URL: "https://example.blob.core.windows.net/a.zip"}, f.runErr
}

func (f *fakeBackend) Actions(context.Context) (model.Actions, error) {
	f.actionsCalls++
	return f.actions, nil
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
	if _, err := c.Status(context.Background()); err == nil || !strings.HasPrefix(err.Error(), "ghr daemon unreachable: ") {
		t.Fatalf("err %v", err)
	}
	if c.HTTP.Timeout != 0 {
		t.Fatalf("the client cuts every call after %v, also the ones with a longer limit of their own", c.HTTP.Timeout)
	}
}

// slowDaemon answers every request with 204 after its delay, or gives up when
// the request's context ends first, as a transport does.
type slowDaemon time.Duration

func (d slowDaemon) RoundTrip(r *http.Request) (*http.Response, error) {
	select {
	case <-time.After(time.Duration(d)):
		return &http.Response{StatusCode: http.StatusNoContent, Header: http.Header{}, Body: http.NoBody, Request: r}, nil
	case <-r.Context().Done():
		return nil, r.Context().Err()
	}
}

// The bubble's clock is fake, so the limits under test are the real ones.
func TestCallTimeLimits(t *testing.T) {
	status := func(ctx context.Context, c *Client) error {
		_, err := c.Status(ctx)
		return err
	}
	kill := func(ctx context.Context, c *Client) error { return c.Kill(ctx, "a3f9c1") }
	for _, tc := range []struct {
		name    string
		answer  time.Duration
		call    func(context.Context, *Client) error
		want    string
		elapsed time.Duration
	}{
		{"a call answered in time", 29 * time.Second, status, "", 29 * time.Second},
		{"a call not answered", time.Hour, status, "ghr daemon did not answer within 30s; the request may still be running", 30 * time.Second},
		{"a stop that takes systemd's 90 seconds", 95 * time.Second, kill, "", 95 * time.Second},
		{"a stop not answered", time.Hour, kill, "ghr daemon did not answer within 100s; the request may still be running", 100 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				c := &Client{Base: "http://ghr", HTTP: &http.Client{Transport: slowDaemon(tc.answer)}}
				start := time.Now()
				err := tc.call(context.Background(), c)
				if got := time.Since(start); got != tc.elapsed {
					t.Errorf("returned after %v, want %v", got, tc.elapsed)
				}
				switch {
				case tc.want == "" && err != nil:
					t.Fatalf("err %v", err)
				case tc.want != "" && (err == nil || err.Error() != tc.want):
					t.Fatalf("err %v, want %s", err, tc.want)
				}
			})
		})
	}
}

// stalledBody is a response body that never arrives; the read gives up when
// the request's context ends.
type stalledBody struct{ ctx context.Context }

func (b stalledBody) Read([]byte) (int, error) {
	select {
	case <-time.After(time.Hour):
		return 0, io.EOF
	case <-b.ctx.Done():
		return 0, b.ctx.Err()
	}
}

func (stalledBody) Close() error { return nil }

// stalledDaemon answers at once with its status and then never sends the body.
type stalledDaemon int

func (d stalledDaemon) RoundTrip(r *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: int(d), Status: fmt.Sprintf("%d %s", int(d), http.StatusText(int(d))),
		Header: http.Header{}, Body: stalledBody{r.Context()}, Request: r,
	}, nil
}

// A call cut short while it reads the response is reported like one cut
// short before it: by its limit, or by its caller. That holds for an error
// response too. Its unread body must not become an API error, nor the advice
// to restart an older daemon that a 404 or 405 without JSON gets.
func TestCallCutShortWhileReadingTheResponse(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusInternalServerError} {
		for _, cancelled := range []bool{false, true} {
			t.Run(fmt.Sprintf("status %d, cancelled %v", status, cancelled), func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					c := &Client{Base: "http://ghr", HTTP: &http.Client{Transport: stalledDaemon(status)}}
					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					if cancelled {
						time.AfterFunc(10*time.Second, cancel)
					}
					_, err := c.Status(ctx)
					const limit = "ghr daemon did not answer within 30s; the request may still be running"
					switch {
					case cancelled && err != context.Canceled:
						t.Fatalf("err %v, want the caller's own context error", err)
					case !cancelled && (err == nil || err.Error() != limit):
						t.Fatalf("err %v, want %s", err, limit)
					}
				})
			})
		}
	}
}

// A caller that gives up (Ctrl-C in the CLI) gets its own context's error
// back, not a report that the daemon cannot be reached.
func TestCallEndedByItsCaller(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := &Client{Base: "http://ghr", HTTP: &http.Client{Transport: slowDaemon(time.Hour)}}
		ctx, cancel := context.WithCancel(context.Background())
		time.AfterFunc(10*time.Second, cancel)
		start := time.Now()
		err := c.Kill(ctx, "a3f9c1")
		if err != context.Canceled || time.Since(start) != 10*time.Second {
			t.Fatalf("err %v after %v", err, time.Since(start))
		}
	})
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
		{&github.APIError{Status: 410, Kind: github.ErrGone, Message: "Gone"}, 410, false},
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

	b.watchErr = Conflict("repo docs is already watched")
	var ae *Error
	if err := c.call(ctx, http.MethodPost, "/watch", strings.NewReader(`{"name":"docs"}`), nil); !errors.As(err, &ae) || ae.Status != 409 || ae.Msg != "repo docs is already watched" {
		t.Fatalf("watch error: %v", err)
	}
	b.watchErr = NotFound("repo old-docs is not watched")
	if err := c.call(ctx, http.MethodDelete, "/watch/old-docs", nil, nil); !errors.As(err, &ae) || ae.Status != 404 {
		t.Fatalf("unwatch error: %v", err)
	}
}

func TestActionsRoute(t *testing.T) {
	c, b := setup(t)
	b.actions = model.Actions{Runs: []model.ActionsRun{{Repo: "darkmem", ID: 7}}, Repos: []model.ActionsRepo{{Repo: "darkmem"}}}
	var got model.Actions
	if err := c.call(context.Background(), http.MethodGet, "/actions", nil, &got); err != nil {
		t.Fatal(err)
	}
	if b.actionsCalls != 1 || len(got.Runs) != 1 || got.Runs[0].ID != 7 || len(got.Repos) != 1 {
		t.Fatalf("calls %d actions %+v", b.actionsCalls, got)
	}
}

func TestRunDetailRoutes(t *testing.T) {
	c, b := setup(t)
	ctx := context.Background()
	var d model.RunDetail
	if err := c.call(ctx, http.MethodGet, "/actions/darkmem/runs/102?attempt=2", nil, &d); err != nil || d.ID != 102 || d.Attempt != 2 {
		t.Fatalf("run %+v err %v", d, err)
	}
	var a model.RunAnnotations
	if err := c.call(ctx, http.MethodGet, "/actions/darkmem/runs/102/annotations", nil, &a); err != nil || a.Omitted[102] != 1 {
		t.Fatalf("annotations %+v err %v", a, err)
	}
	var l model.JobLog
	if err := c.call(ctx, http.MethodGet, "/actions/darkmem/jobs/1002/log", nil, &l); err != nil || l.JobID != 1002 {
		t.Fatalf("log %+v err %v", l, err)
	}
	if resp, body, err := getRaw(c, "/actions/darkmem/jobs/1002/log/raw"); err != nil || resp.StatusCode != 200 || string(body) != "raw log" {
		t.Fatalf("raw %q err %v", body, err)
	}
	var u model.DownloadURL
	if err := c.call(ctx, http.MethodGet, "/actions/darkmem/artifacts/11/download", nil, &u); err != nil || u.URL == "" {
		t.Fatalf("download %+v err %v", u, err)
	}
	want := []string{"RunDetail darkmem 102 2", "RunAnnotations darkmem 102 0", "JobLog darkmem 1002", "RawJobLog darkmem 1002",
		"ArtifactDownload darkmem 11"}
	if !reflect.DeepEqual(b.runCalls, want) {
		t.Fatalf("calls %v", b.runCalls)
	}
}

func TestRunDetailRoutesRefuseBadNumbers(t *testing.T) {
	c, b := setup(t)
	for path, msg := range map[string]string{
		"/actions/darkmem/runs/abc":                       "run id must be a positive integer",
		"/actions/darkmem/runs/0/annotations":             "run id must be a positive integer",
		"/actions/darkmem/runs/102?attempt=0":             "attempt must be a positive integer",
		"/actions/darkmem/runs/102/annotations?attempt=x": "attempt must be a positive integer",
		"/actions/darkmem/runs/102?attempt=99999999999":   "attempt must be a positive integer",
		"/actions/darkmem/jobs/-4/log":                    "job id must be a positive integer",
		"/actions/darkmem/jobs/1.5/log/raw":               "job id must be a positive integer",
		"/actions/darkmem/artifacts/x/download":           "artifact id must be a positive integer",
	} {
		var ae *Error
		if err := c.call(context.Background(), http.MethodGet, path, nil, nil); !errors.As(err, &ae) || ae.Status != 400 || ae.Msg != msg {
			t.Errorf("%s: %v", path, err)
		}
	}
	if len(b.runCalls) != 0 {
		t.Fatalf("a bad number reached the backend: %v", b.runCalls)
	}
}

func TestRunDetailRoutesPassBackendStatuses(t *testing.T) {
	c, b := setup(t)
	for _, tc := range []struct {
		path string
		err  error
	}{
		{"/actions/nope/runs/1", NotFound("repo nope is not configured or watched")},
		{"/actions/darkmem/runs/1?attempt=3", BadRequest("run 1 has no attempt 3")},
		{"/actions/darkmem/jobs/1/log", Conflict("job has not finished")},
		{"/actions/darkmem/jobs/1/log/raw", &Error{Status: http.StatusRequestEntityTooLarge, Msg: "log is larger than 32 MiB; open it on GitHub"}},
		{"/actions/darkmem/artifacts/1/download", &Error{Status: http.StatusGone, Msg: "artifact has expired"}},
		{"/actions/darkmem/artifacts/1/download", &Error{Status: http.StatusBadGateway, Msg: "GitHub returned an unexpected download address"}},
	} {
		b.runErr = tc.err
		var want, ae *Error
		errors.As(tc.err, &want)
		if err := c.call(context.Background(), http.MethodGet, tc.path, nil, nil); !errors.As(err, &ae) || ae.Status != want.Status || ae.Msg != want.Msg {
			t.Errorf("%s: %v", tc.path, err)
		}
	}
}

// getRaw fetches path from the API without decoding the body; resp is nil
// when no response arrived.
func getRaw(c *Client, path string) (*http.Response, []byte, error) {
	resp, err := c.HTTP.Get(c.Base + path)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	return resp, body, err
}

// waitClosed waits up to 3 s for n RawJobLog readers to be closed: the
// handler closes its reader as it returns, which can be after the client
// has read the response.
func waitClosed(t *testing.T, b *fakeBackend, n int32) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for b.rawClosed.Load() != n {
		if time.Now().After(deadline) {
			t.Fatalf("%d readers closed, want %d", b.rawClosed.Load(), n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestRawLogRoute(t *testing.T) {
	for _, tc := range []struct {
		name   string
		body   io.Reader
		size   int64
		status int
		want   string // the body, or the JSON error's message
		broken bool   // the body fails after the status line
	}{
		{name: "streams", body: strings.NewReader("line one\nline two\n"), size: 18, status: 200, want: "line one\nline two\n"},
		{name: "empty", body: strings.NewReader(""), size: 0, status: 200, want: ""},
		{name: "fails before the first byte", body: iotest.ErrReader(errors.New("boom")), size: 10, status: 502, want: "GitHub stopped sending the log"},
		{name: "fails after the first byte", body: io.MultiReader(strings.NewReader("line one\n"), iotest.ErrReader(errors.New("boom"))), size: 100, status: 200, broken: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, b := setup(t)
			b.rawBody, b.rawSize = tc.body, tc.size
			resp, body, err := getRaw(c, "/actions/darkmem/jobs/1002/log/raw")
			if resp == nil || resp.StatusCode != tc.status {
				t.Fatalf("response %v err %v, want status %d", resp, err, tc.status)
			}
			switch {
			case tc.broken:
				if err == nil {
					t.Fatalf("a broken copy read as complete: %q", body)
				}
			case tc.status == 200:
				if err != nil || string(body) != tc.want || resp.ContentLength != tc.size || resp.Header.Get("Content-Type") != "text/plain; charset=utf-8" {
					t.Fatalf("body %q length %d type %q err %v", body, resp.ContentLength, resp.Header.Get("Content-Type"), err)
				}
			default:
				var e struct {
					Error string `json:"error"`
				}
				if err != nil || json.Unmarshal(body, &e) != nil || e.Error != tc.want {
					t.Fatalf("error body %q err %v", body, err)
				}
			}
			waitClosed(t, b, 1)
		})
	}
}

func TestRawLogRouteDeadlineCountsFromTheRouteStart(t *testing.T) {
	old := rawWriteWindow
	rawWriteWindow = 50 * time.Millisecond
	t.Cleanup(func() { rawWriteWindow = old })
	c, b := setup(t)
	b.rawDelay = 100 * time.Millisecond
	if resp, body, err := getRaw(c, "/actions/darkmem/jobs/1002/log/raw"); err == nil {
		t.Fatalf("a write after the window completed: status %d body %q", resp.StatusCode, body)
	}
	waitClosed(t, b, 1)
}

// The client leaves an unset max or warm out of the body, and the server
// tells null (clear the value) from an absent key (leave it alone).
func TestPatchConfigMaxAndWarmOverTheWire(t *testing.T) {
	c, b := setup(t)
	err := c.PatchConfig(context.Background(), model.ConfigPatch{Repos: map[string]model.RepoPatch{"darkmem": {Max: model.Int(3)}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{`{"repos":{"darkmem":{"max":null}}}`, `{"repos":{"darkmem":{}}}`} {
		req, _ := http.NewRequest(http.MethodPatch, c.Base+"/config", strings.NewReader(body))
		resp, err := c.HTTP.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("%s: status %d", body, resp.StatusCode)
		}
	}
	sent, null, absent := b.patches[0].Repos["darkmem"], b.patches[1].Repos["darkmem"], b.patches[2].Repos["darkmem"]
	if !reflect.DeepEqual(sent, model.RepoPatch{Max: model.Int(3)}) {
		t.Errorf("the client's patch arrived as %+v", sent)
	}
	if !reflect.DeepEqual(null, model.RepoPatch{Max: model.ClearInt()}) {
		t.Errorf("max null arrived as %+v", null)
	}
	if !reflect.DeepEqual(absent, model.RepoPatch{}) {
		t.Errorf("an empty patch arrived as %+v", absent)
	}
}
