package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/model"
)

// fakeSetupDaemon answers GET /setup with states in turn (the last repeats),
// refuses POST /setup/github with the token "bad", and accepts the rest.
func fakeSetupDaemon(t *testing.T, states ...model.SetupState) *[]recorded {
	t.Helper()
	var (
		mu   sync.Mutex
		reqs []recorded
		next int
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		defer mu.Unlock()
		reqs = append(reqs, recorded{r.Method, r.URL.RequestURI(), string(b)})
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/setup":
			json.NewEncoder(w).Encode(states[min(next, len(states)-1)])
			next++
		case r.URL.Path == "/setup/github" && strings.Contains(string(b), `"token":"bad"`):
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "token rejected by GitHub: github: 401 Bad credentials"})
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	t.Cleanup(srv.Close)
	newClient = func() *api.Client { return &api.Client{Base: srv.URL, HTTP: srv.Client()} }
	return &reqs
}

func fastSetupPoll(t *testing.T, wait time.Duration) {
	t.Helper()
	oldPoll, oldWait := setupPoll, setupWait
	setupPoll, setupWait = time.Millisecond, wait
	t.Cleanup(func() { setupPoll, setupWait = oldPoll, oldWait })
}

func TestSetupPrintsTheState(t *testing.T) {
	for _, c := range []struct {
		st   model.SetupState
		want string
	}{
		{model.SetupState{Configured: true, Owner: "DarkRaise", WebListen: "0.0.0.0:8080"}, "configured: yes\nwizard: done\nowner: DarkRaise\nweb: 0.0.0.0:8080\n"},
		{model.SetupState{Starting: true, SetupPending: true, Owner: "DarkRaise"}, "configured: starting\nwizard: pending\nowner: DarkRaise\nweb: -\n"},
		{model.SetupState{SetupPending: true}, "configured: no\nwizard: pending\nowner: -\nweb: -\n"},
	} {
		fakeSetupDaemon(t, c.st)
		if code, out, errOut := runCLI(t, "", "setup"); code != 0 || out != c.want {
			t.Errorf("%+v: exit %d out %q err %q", c.st, code, out, errOut)
		}
	}
}

func TestSetupGitHubFromAPipe(t *testing.T) {
	fastSetupPoll(t, time.Second)
	reqs := fakeSetupDaemon(t, model.SetupState{Starting: true}, model.SetupState{Configured: true})
	stubPasswordInput(t, false)
	code, out, errOut := runCLI(t, " tok \n", "setup", "github", "--owner", "DarkRaise")
	if code != 0 || out != "GitHub owner and token set; ghr is running\n" {
		t.Fatalf("exit %d out %q err %q", code, out, errOut)
	}
	first := (*reqs)[0]
	if first.method != "POST" || first.path != "/setup/github" || first.body != `{"owner":"DarkRaise","token":"tok"}` {
		t.Fatalf("request %+v", first)
	}
	if len(*reqs) != 3 {
		t.Fatalf("want one POST and two polls, got %+v", *reqs)
	}
}

func TestSetupGitHubPrompts(t *testing.T) {
	fastSetupPoll(t, time.Second)
	reqs := fakeSetupDaemon(t, model.SetupState{Configured: true})
	stubPasswordInput(t, true, " tok ")
	code, _, errOut := runCLI(t, "", "setup", "github", "--owner", "DarkRaise")
	if code != 0 || errOut != "GitHub token: \n" || (*reqs)[0].body != `{"owner":"DarkRaise","token":"tok"}` {
		t.Fatalf("exit %d err %q requests %+v", code, errOut, *reqs)
	}
}

func TestSetupGitHubUsesTheConfigOwner(t *testing.T) {
	fastSetupPoll(t, time.Second)
	reqs := fakeSetupDaemon(t, model.SetupState{Owner: "DarkRaise"}, model.SetupState{Configured: true})
	stubPasswordInput(t, false)
	if code, _, errOut := runCLI(t, "tok\n", "setup", "github"); code != 0 || (*reqs)[1].body != `{"owner":"","token":"tok"}` {
		t.Fatalf("exit %d err %q requests %+v", code, errOut, *reqs)
	}
	fakeSetupDaemon(t, model.SetupState{})
	code, _, errOut := runCLI(t, "tok\n", "setup", "github")
	if code != 2 || !strings.Contains(errOut, "ghr setup github needs --owner <owner>: config.yaml names none") {
		t.Fatalf("no owner anywhere: exit %d err %q", code, errOut)
	}
}

func TestSetupGitHubReportsTheDaemon(t *testing.T) {
	reqs := fakeSetupDaemon(t, model.SetupState{Configured: true})
	stubPasswordInput(t, false)
	code, _, errOut := runCLI(t, "bad\n", "setup", "github", "--owner", "DarkRaise")
	if code != 1 || !strings.Contains(errOut, "ghr: token rejected by GitHub: github: 401 Bad credentials") || len(*reqs) != 1 {
		t.Fatalf("exit %d err %q requests %d", code, errOut, len(*reqs))
	}
}

func TestSetupGitHubGivesUpWaiting(t *testing.T) {
	fastSetupPoll(t, 20*time.Millisecond)
	fakeSetupDaemon(t, model.SetupState{Starting: true})
	stubPasswordInput(t, false)
	code, _, errOut := runCLI(t, "tok\n", "setup", "github", "--owner", "DarkRaise")
	if code != 1 || !strings.Contains(errOut, "ghr: owner and token saved, but ghr is still starting; check: journalctl -u ghr -n 50") {
		t.Fatalf("exit %d err %q", code, errOut)
	}
}

func TestSetupFinishAndUsage(t *testing.T) {
	reqs := fakeSetupDaemon(t, model.SetupState{})
	code, out, _ := runCLI(t, "", "setup", "finish")
	if last := (*reqs)[len(*reqs)-1]; code != 0 || out != "first-run setup finished\n" || last.path != "/setup/finish" || last.body != `{"toolchains":"none"}` {
		t.Fatalf("exit %d out %q request %+v", code, out, last)
	}
	runCLI(t, "", "setup", "finish", "--toolchains", "popular")
	if last := (*reqs)[len(*reqs)-1]; last.body != `{"toolchains":"popular"}` {
		t.Fatalf("popular: %+v", last)
	}
	for _, args := range [][]string{
		{"setup", "finish", "--toolchains", "all"}, {"setup", "finish", "now"}, {"setup", "bogus"},
		{"setup", "github", "--owner", "x", "extra"},
	} {
		code, _, errOut := runCLI(t, "", args...)
		if code != 2 || !strings.Contains(errOut, "usage: ghr setup [github [--owner <owner>] | finish [--toolchains popular|none]]") {
			t.Errorf("%v: exit %d err %q", args, code, errOut)
		}
	}
	_, out, _ = runCLI(t, "", "help")
	for _, want := range []string{"setup github [--owner <owner>]", "setup finish [--toolchains popular|none]"} {
		if !strings.Contains(out, want) {
			t.Fatalf("usage lacks %q:\n%s", want, out)
		}
	}
}
