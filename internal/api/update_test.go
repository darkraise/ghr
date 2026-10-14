package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
)

func TestRunnerUpdateRoutes(t *testing.T) {
	c, b := setup(t)
	if err := c.QueueRunnerUpdate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := c.CancelRunnerUpdate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(b.updates, ","); got != "queue,cancel" {
		t.Fatalf("calls %s", got)
	}
	var ae *Error
	b.updateErr = Conflict("runner 2.337.0 is already up to date")
	if err := c.QueueRunnerUpdate(context.Background()); !errors.As(err, &ae) || ae.Status != 409 || ae.Msg != "runner 2.337.0 is already up to date" {
		t.Fatalf("up to date: %v", err)
	}
	if err := c.CancelRunnerUpdate(context.Background()); !errors.As(err, &ae) || ae.Status != 409 {
		t.Fatalf("cancel refused: %v", err)
	}
	retry := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	b.updateErr = &github.APIError{Status: 403, Kind: github.ErrRateLimit, Message: "API rate limit exceeded", RetryAt: retry}
	if err := c.QueueRunnerUpdate(context.Background()); !errors.As(err, &ae) || ae.Status != 429 || !ae.RetryAt.Equal(retry) {
		t.Fatalf("rate limit: %v", err)
	}
}

// The routes answer with the statuses the spec names, which the client alone
// cannot tell apart.
func TestRunnerUpdateStatusCodes(t *testing.T) {
	c, b := setup(t)
	do := func(method string) int {
		t.Helper()
		req, err := http.NewRequest(method, c.Base+"/runner-update", nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := c.HTTP.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	for _, s := range []struct {
		method string
		err    error
		want   int
	}{
		{http.MethodPost, nil, 202},
		{http.MethodPost, nil, 202}, // already queued
		{http.MethodDelete, nil, 204},
		{http.MethodDelete, nil, 204}, // nothing queued
		{http.MethodPost, Conflict("runner 2.337.0 is already up to date"), 409},
		{http.MethodPost, Conflict("a runner update is running"), 409},
		{http.MethodDelete, Conflict("a runner update is running"), 409},
		{http.MethodPost, &Error{Status: http.StatusServiceUnavailable, Msg: "GitHub is rejecting the token"}, 503},
	} {
		b.updateErr = s.err
		if got := do(s.method); got != s.want {
			t.Errorf("%s with %v: %d, want %d", s.method, s.err, got, s.want)
		}
	}
}

func TestAvailableReposRoute(t *testing.T) {
	c, b := setup(t)
	b.avail = []model.AvailableRepo{{Name: "booklore"}, {Name: "darkcloud", Private: true, Configured: true}}
	got, err := c.AvailableRepos(context.Background())
	if err != nil || !reflect.DeepEqual(got, b.avail) {
		t.Fatalf("got %+v %v", got, err)
	}
	b.avail = nil
	if got, err := c.AvailableRepos(context.Background()); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("empty list must decode as []: %#v %v", got, err)
	}
	b.availErr = &Error{Status: 503, Msg: "GitHub is rejecting the token"}
	var ae *Error
	if _, err := c.AvailableRepos(context.Background()); !errors.As(err, &ae) || ae.Status != 503 {
		t.Fatalf("degraded: %v", err)
	}
}

func TestJobAndGhrUpdateRoutes(t *testing.T) {
	c, b := setup(t)
	ctx := context.Background()
	for _, name := range []string{"prune", "a b/c"} {
		if err := c.RunJob(ctx, name); err != nil {
			t.Fatal(err)
		}
	}
	if !slices.Equal(b.jobRuns, []string{"prune", "a b/c"}) {
		t.Fatalf("runs %q", b.jobRuns)
	}
	if err := c.StartGhrUpdate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.CancelGhrUpdate(ctx); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(b.ghrCalls, ","); got != "start,cancel" {
		t.Fatalf("calls %s", got)
	}
	var ae *Error
	b.jobErr = NotFound(`no background task "nightly"`)
	if err := c.RunJob(ctx, "nightly"); !errors.As(err, &ae) || ae.Status != 404 || ae.Msg != `no background task "nightly"` {
		t.Fatalf("unknown task: %v", err)
	}
	b.ghrErr = Conflict("ghr v0.1.26 is the newest release")
	if err := c.StartGhrUpdate(ctx); !errors.As(err, &ae) || ae.Status != 409 || ae.Msg != "ghr v0.1.26 is the newest release" {
		t.Fatalf("refused install: %v", err)
	}
}

func TestJobAndGhrUpdateStatusCodes(t *testing.T) {
	c, b := setup(t)
	do := func(method, path string) int {
		t.Helper()
		req, err := http.NewRequest(method, c.Base+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := c.HTTP.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	for _, s := range []struct {
		method, path   string
		jobErr, ghrErr error
		want           int
	}{
		{http.MethodPost, "/jobs/prune/run", nil, nil, 202},
		{http.MethodPost, "/jobs/nightly/run", NotFound(`no background task "nightly"`), nil, 404},
		{http.MethodPost, "/jobs/prune/run", Conflict("prune is off; turn it on first"), nil, 409},
		{http.MethodPost, "/ghr-update", nil, nil, 202},
		{http.MethodPost, "/ghr-update", nil, Conflict("a ghr update is already running"), 409},
		{http.MethodDelete, "/ghr-update", nil, nil, 204},
		{http.MethodDelete, "/ghr-update", nil, Conflict("no ghr update is running"), 409},
		{http.MethodGet, "/jobs/prune/run", nil, nil, 405},
	} {
		b.jobErr, b.ghrErr = s.jobErr, s.ghrErr
		if got := do(s.method, s.path); got != s.want {
			t.Errorf("%s %s with %v %v: %d, want %d", s.method, s.path, s.jobErr, s.ghrErr, got, s.want)
		}
	}
}

func TestStatusJSONNamesTheNewFields(t *testing.T) {
	data, err := json.Marshal(model.Status{
		GhrUpdate: model.GhrUpdate{Installed: "v0.1.25", Newer: true, Latest: "v0.1.26"},
		Jobs:      []model.JobStatus{{Name: "prune", Title: "Prune", Description: "d", Enabled: true, Every: "1d"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"ghr_update":{"installed":"v0.1.25","newer":true,"latest":"v0.1.26"}`,
		`"jobs":[{"name":"prune","title":"Prune","description":"d","enabled":true,"every":"1d"}]`,
	} {
		if !strings.Contains(string(data), want) {
			t.Errorf("status JSON lacks %s:\n%s", want, data)
		}
	}
}
