package daemon

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/runner"
)

func TestQueueRunnerUpdate(t *testing.T) {
	b, m, _ := newBackend(t)
	woke := 0
	b.Wake = func() { woke++ }
	if err := b.QueueRunnerUpdate(context.Background()); err != nil || woke != 1 {
		t.Fatalf("queue: %v, woke %d", err, woke)
	}
	for _, c := range []struct {
		err  error
		want int
	}{
		{runner.UpToDateError("2.337.0"), 409},
		{runner.ErrUpdateRunning, 409},
		{runner.ErrNoDist, 409},
		{runner.ErrClosed, 503},
	} {
		m.updateErr = c.err
		err := b.QueueRunnerUpdate(context.Background())
		if apiStatus(err) != c.want || err.Error() != c.err.Error() {
			t.Errorf("%v: got %v (%d), want %d", c.err, err, apiStatus(err), c.want)
		}
	}
	limit := &github.APIError{Status: 403, Kind: github.ErrRateLimit, RetryAt: time.Now().Add(time.Minute)}
	m.updateErr = limit
	if err := b.QueueRunnerUpdate(context.Background()); !errors.Is(err, limit) {
		t.Fatalf("a GitHub error must reach the API unchanged: %v", err)
	}
	if woke != 1 {
		t.Fatalf("a refused queue woke the loop")
	}

	calls := len(m.updates)
	m.degraded = "GitHub rejected the token"
	if err := b.QueueRunnerUpdate(context.Background()); apiStatus(err) != 503 || len(m.updates) != calls {
		t.Fatalf("degraded: %v, manager called %v", err, m.updates)
	}
}

func TestCancelRunnerUpdate(t *testing.T) {
	b, m, _ := newBackend(t)
	if err := b.CancelRunnerUpdate(); err != nil {
		t.Fatal(err)
	}
	m.updateErr = runner.ErrUpdateRunning
	if err := b.CancelRunnerUpdate(); apiStatus(err) != 409 {
		t.Fatalf("running: %v", err)
	}
	if got := strings.Join(m.updates, ","); got != "cancel,cancel" {
		t.Fatalf("calls %s", got)
	}
}

func TestPruneRefusedWhileUpdating(t *testing.T) {
	b, m, _ := newBackend(t)
	m.pruneErr = runner.ErrUpdateRunning
	err := b.Prune()
	var ae *api.Error
	if !errors.As(err, &ae) || ae.Status != 409 || ae.Msg != "a runner update is running" {
		t.Fatalf("prune: %v", err)
	}
}
