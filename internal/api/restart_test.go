package api

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func (f *fakeBackend) RequestRestart(context.Context) (func(), error) {
	f.restarts++
	if f.restartErr != nil {
		return nil, f.restartErr
	}
	if f.restartRun == nil {
		return func() {}, nil
	}
	return f.restartRun, nil
}

// The 202 reaches the client before the restart runs: run waits for the
// client to have it, which it never would if the answer went out only when
// the handler returned.
func TestRestartAnswersBeforeItRuns(t *testing.T) {
	c, b := setup(t)
	answered, ran := make(chan struct{}), make(chan struct{})
	b.restartRun = func() {
		select {
		case <-answered:
		case <-time.After(3 * time.Second):
			t.Error("the restart ran before the 202 reached the client")
		}
		close(ran)
	}
	resp, err := c.HTTP.Post(c.Base+"/restart", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	close(answered)
	select {
	case <-ran:
	case <-time.After(5 * time.Second):
		t.Fatal("the restart never ran")
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted || b.restarts != 1 {
		t.Fatalf("status %d, %d requests", resp.StatusCode, b.restarts)
	}
}

func TestRestartRefused(t *testing.T) {
	c, b := setup(t)
	ran := false
	b.restartRun = func() { ran = true }
	for _, msg := range []string{
		"ghr is not running as ghr.service; restart it by hand",
		"waits for: installing node 24, 1 queued",
	} {
		b.restartErr = Conflict(msg)
		resp, err := c.HTTP.Post(c.Base+"/restart", "", nil)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusConflict || !strings.Contains(string(body), msg) {
			t.Errorf("%s: %d %s", msg, resp.StatusCode, body)
		}
	}
	if ran {
		t.Fatal("a refused restart ran")
	}
}
