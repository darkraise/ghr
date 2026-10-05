package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/github"
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
