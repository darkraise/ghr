package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/model"
)

type recorded struct {
	method, path, body string
}

func fakeDaemon(t *testing.T) *[]recorded {
	t.Helper()
	var reqs []recorded
	now := time.Date(2026, 10, 3, 14, 0, 0, 0, time.UTC)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		reqs = append(reqs, recorded{r.Method, r.URL.RequestURI(), string(b)})
		switch {
		case r.URL.Path == "/status":
			json.NewEncoder(w).Encode(model.Status{
				Now: now, Mode: "queue", GlobalMax: 3, RateRemaining: 4812, DiskPct: 61,
				Repos: []model.RepoStatus{
					{Name: "darkcloud", Max: 1, Active: 1, Queued: 2,
						LastJob: &model.HistoryEntry{Conclusion: "success", RunNumber: "411", JobName: "lint", FinishedAt: now.Add(-2 * time.Minute)}},
					{Name: "darkagents", Paused: true, Max: 0},
				},
				Instances: []model.InstanceStatus{{ID: "a3f9c1", Repo: "darkcloud", State: "busy", Since: now.Add(-12*time.Minute - 4*time.Second),
					Job: &model.JobInfo{Name: "CI / e2e-journeys", RunNumber: "412"}}},
			})
		case r.URL.Path == "/history":
			json.NewEncoder(w).Encode([]model.HistoryEntry{{Repo: "darkmem", RunNumber: "88", JobName: "build", Conclusion: "failure",
				StartedAt: now.Add(-time.Minute), FinishedAt: now}})
		case strings.HasSuffix(r.URL.Path, "/log"):
			// Two chunks, then nothing new: an unknown cursor gets an empty chunk.
			chunks := map[string]model.LogChunk{
				"":                {Data: "hello log\n", Next: "Runner_1.log=10"},
				"Runner_1.log=10": {Data: "second chunk\n", Next: "Runner_1.log=23"},
			}
			json.NewEncoder(w).Encode(chunks[r.URL.Query().Get("cursor")])
		case r.URL.Path == "/repos" && strings.Contains(string(b), `"public"`) && !strings.Contains(string(b), `"allow_public":true`):
			w.WriteHeader(409)
			json.NewEncoder(w).Encode(map[string]string{"error": "public is public"})
		case r.URL.Path == "/storage":
			json.NewEncoder(w).Encode(sampleStorage(now))
		case r.URL.Path == "/toolchains/available":
			json.NewEncoder(w).Encode([]model.ToolchainChoice{{Spec: "21", Version: "21.0.8+9", LTS: true}, {Spec: "24", Version: "24.0.2+12"}})
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	t.Cleanup(srv.Close)
	newClient = func() *api.Client { return &api.Client{Base: srv.URL, HTTP: srv.Client()} }
	return &reqs
}

func runCLI(t *testing.T, stdin string, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := run(args, strings.NewReader(stdin), &out, &errb)
	return code, out.String(), errb.String()
}

func TestStatusOutput(t *testing.T) {
	fakeDaemon(t)
	code, out, _ := runCLI(t, "", "status")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	for _, want := range []string{"mode queue  global 1/3  api 4812  disk 61%", "darkcloud", "1/1", "success #411 lint (2m0s ago)",
		"darkagents", "paused", "0/∞", "a3f9c1", "CI / e2e-journeys #412", "12m4s"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestMutatingCommands(t *testing.T) {
	reqs := fakeDaemon(t)
	cases := [][]string{
		{"pause", "darkcloud"},
		{"resume", "darkcloud"},
		{"drain"},
		{"resume-all"},
		{"set", "mode", "all"},
		{"set", "global-max", "3"},
		{"set", "max", "darkmem", "0"},
		{"set", "warm", "darkmem", "2"},
		{"repo", "add", "newrepo", "--max", "2", "--label", "a", "--label", "b"},
		{"repo", "rm", "old"},
		{"kill", "a3f9c1"},
	}
	for _, args := range cases {
		if code, _, errOut := runCLI(t, "", args...); code != 0 {
			t.Fatalf("%v: exit %d: %s", args, code, errOut)
		}
	}
	got := []string{}
	for _, r := range *reqs {
		got = append(got, r.method+" "+r.path+" "+r.body)
	}
	want := []string{
		"POST /repos/darkcloud/pause ",
		"POST /repos/darkcloud/resume ",
		"POST /pause-all ",
		"POST /resume-all ",
		`PATCH /config {"mode":"all"}`,
		`PATCH /config {"global_max":3}`,
		`PATCH /config {"repos":{"darkmem":{"max":0}}}`,
		`PATCH /config {"repos":{"darkmem":{"warm":2}}}`,
		`POST /repos {"name":"newrepo","max":2,"labels":["a","b"],"allow_public":false}`,
		"DELETE /repos/old ",
		"DELETE /runners/a3f9c1 ",
	}
	for i := range want {
		if strings.TrimSpace(got[i]) != strings.TrimSpace(want[i]) {
			t.Errorf("request %d:\n got %s\nwant %s", i, got[i], want[i])
		}
	}
}

func TestRepoAddMaxFlag(t *testing.T) {
	reqs := fakeDaemon(t)
	for _, args := range [][]string{
		{"repo", "add", "r1"},
		{"repo", "add", "r2", "--max", "0"},
		{"repo", "add", "r3", "--max", "3"},
	} {
		if code, _, errOut := runCLI(t, "", args...); code != 0 {
			t.Fatalf("%v: exit %d: %s", args, code, errOut)
		}
	}
	want := []string{
		`{"name":"r1","allow_public":false}`,
		`{"name":"r2","max":0,"allow_public":false}`,
		`{"name":"r3","max":3,"allow_public":false}`,
	}
	for i, w := range want {
		if got := strings.TrimSpace((*reqs)[i].body); got != w {
			t.Errorf("request %d: got %s want %s", i, got, w)
		}
	}
	for _, args := range [][]string{
		{"repo", "add", "r4", "--max", "-2"},
		{"repo", "add", "r5", "--max", "-1"},
		{"repo", "add", "r6", "extra"},
	} {
		if code, _, _ := runCLI(t, "", args...); code != 2 {
			t.Errorf("%v: exit %d, want 2", args, code)
		}
	}
	if len(*reqs) != 3 {
		t.Fatalf("rejected adds reached the daemon: %v", *reqs)
	}
}

func TestTokenSetReadsStdin(t *testing.T) {
	reqs := fakeDaemon(t)
	if code, _, _ := runCLI(t, "github_pat_new\n", "token", "set"); code != 0 {
		t.Fatal("token set failed")
	}
	if r := (*reqs)[0]; r.method != "PUT" || r.path != "/token" || r.body != "github_pat_new" {
		t.Fatalf("request %+v", r)
	}
}

func TestDrainAndResumeAllTakeNoArguments(t *testing.T) {
	reqs := fakeDaemon(t)
	for _, args := range [][]string{{"drain", "darkcloud"}, {"resume-all", "darkcloud"}} {
		if code, _, _ := runCLI(t, "", args...); code != 2 {
			t.Errorf("%v: exit %d, want 2", args, code)
		}
	}
	if len(*reqs) != 0 {
		t.Fatalf("rejected commands reached the daemon: %v", *reqs)
	}
}

func TestErrorsAndUsage(t *testing.T) {
	fakeDaemon(t)
	code, _, errOut := runCLI(t, "", "repo", "add", "public")
	if code != 1 || !strings.Contains(errOut, "public is public") {
		t.Fatalf("exit %d err %s", code, errOut)
	}
	if code, _, _ := runCLI(t, "", "set", "global-max", "lots"); code != 2 {
		t.Fatalf("bad number exit %d", code)
	}
	if code, _, _ := runCLI(t, "", "frobnicate"); code != 2 {
		t.Fatalf("unknown command exit %d", code)
	}
	if code, out, _ := runCLI(t, "", "version"); code != 0 || strings.TrimSpace(out) != "dev" {
		t.Fatalf("version %d %q", code, out)
	}
}

func TestLogsAndHistory(t *testing.T) {
	fakeDaemon(t)
	if code, out, _ := runCLI(t, "", "logs", "a3f9c1"); code != 0 || out != "hello log\nsecond chunk\n" {
		t.Fatalf("logs %d %q", code, out)
	}
	code, out, _ := runCLI(t, "", "history", "--repo", "darkmem")
	if code != 0 || !strings.Contains(out, "#88") || !strings.Contains(out, "failure") || !strings.Contains(out, "1m0s") {
		t.Fatalf("history %d:\n%s", code, out)
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }

func TestLogsReportsWriteError(t *testing.T) {
	fakeDaemon(t)
	if err := logs(context.Background(), newClient(), []string{"a3f9c1", "-f"}, failWriter{}); err == nil {
		t.Fatal("a failed write must be an error")
	}
}

func TestWebResetPassword(t *testing.T) {
	reqs := fakeDaemon(t)
	code, out, errOut := runCLI(t, "", "web", "reset-password")
	if code != 0 || !strings.Contains(out, "web password removed; the next visitor to the web UI sets a new one") {
		t.Fatalf("exit %d out %q err %q", code, out, errOut)
	}
	last := (*reqs)[len(*reqs)-1]
	if last.method != "POST" || last.path != "/web/reset-password" {
		t.Fatalf("request %+v", last)
	}
	for _, args := range [][]string{{"web"}, {"web", "reset"}, {"web", "reset-password", "now"}} {
		code, _, errOut := runCLI(t, "", args...)
		if code != 2 || !strings.Contains(errOut, "usage: ghr web <reset-password|set-password>") {
			t.Errorf("%v: exit %d err %q", args, code, errOut)
		}
	}
	_, out, _ = runCLI(t, "", "help")
	for _, want := range []string{"web reset-password", "web set-password"} {
		if !strings.Contains(out, want) {
			t.Fatalf("usage lacks %q:\n%s", want, out)
		}
	}
}

// stubPasswordInput makes stdin look like a terminal, or not, and feeds the
// no-echo reads from answers.
func stubPasswordInput(t *testing.T, terminal bool, answers ...string) {
	t.Helper()
	oldTerm, oldRead := stdinIsTerminal, readPassword
	stdinIsTerminal = func() bool { return terminal }
	readPassword = func() ([]byte, error) {
		if len(answers) == 0 {
			return nil, io.EOF
		}
		a := answers[0]
		answers = answers[1:]
		return []byte(a), nil
	}
	t.Cleanup(func() { stdinIsTerminal, readPassword = oldTerm, oldRead })
}

func TestWebSetPasswordFromAPipe(t *testing.T) {
	reqs := fakeDaemon(t)
	stubPasswordInput(t, false)
	code, out, errOut := runCLI(t, " a long secret \r\n", "web", "set-password")
	if code != 0 || !strings.Contains(out, "web password set; every browser was logged out") {
		t.Fatalf("exit %d out %q err %q", code, out, errOut)
	}
	last := (*reqs)[len(*reqs)-1]
	if last.method != "POST" || last.path != "/web/set-password" || last.body != `{"password":" a long secret "}` {
		t.Fatalf("request %+v", last)
	}
	runCLI(t, "secret one\n\n", "web", "set-password")
	if last := (*reqs)[len(*reqs)-1]; last.body != `{"password":"secret one\n"}` {
		t.Fatalf("only one line ending may be stripped: %+v", last)
	}
}

func TestWebSetPasswordPromptsTwice(t *testing.T) {
	reqs := fakeDaemon(t)
	stubPasswordInput(t, true, "a long secret", "a long secret")
	code, out, errOut := runCLI(t, "", "web", "set-password")
	if code != 0 || !strings.Contains(out, "web password set; every browser was logged out") {
		t.Fatalf("exit %d out %q err %q", code, out, errOut)
	}
	if errOut != "New web password: \nRepeat: \n" {
		t.Fatalf("prompts %q", errOut)
	}
	if last := (*reqs)[len(*reqs)-1]; last.path != "/web/set-password" || last.body != `{"password":"a long secret"}` {
		t.Fatalf("request %+v", last)
	}

	n := len(*reqs)
	stubPasswordInput(t, true, "a long secret", "a different one")
	code, _, errOut = runCLI(t, "", "web", "set-password")
	if code != 1 || !strings.Contains(errOut, "ghr: passwords do not match") || len(*reqs) != n {
		t.Fatalf("mismatch: exit %d err %q new requests %d", code, errOut, len(*reqs)-n)
	}
}

func TestSetPasswordPromptStopsWhenCancelled(t *testing.T) {
	stubPasswordInput(t, true, "a long secret", "a long secret")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var prompts bytes.Buffer
	if _, err := readNewPassword(ctx, strings.NewReader(""), &prompts); !errors.Is(err, context.Canceled) {
		t.Fatalf("err %v", err)
	}
	if prompts.String() != "New web password: \n" {
		t.Fatalf("asked again after the cancel: %q", prompts.String())
	}
}

func TestStatusWarnsWhileTheWebUIHasNoPassword(t *testing.T) {
	var out bytes.Buffer
	printStatus(&out, model.Status{Mode: "queue", GlobalMax: 1, WebSetupRequired: true})
	const warning = "warning: the web UI has no password; the first visitor sets it. Run: ghr web set-password\n"
	if !strings.HasPrefix(out.String(), warning) {
		t.Fatalf("output:\n%s", out.String())
	}
	out.Reset()
	printStatus(&out, model.Status{Mode: "queue", GlobalMax: 1})
	if strings.Contains(out.String(), "warning:") {
		t.Fatalf("warned with a password set:\n%s", out.String())
	}
}
