package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
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
		case r.URL.Path == "/web/setup-code":
			json.NewEncoder(w).Encode(map[string]string{"code": "ABCDE-FGHJK-MNPQR-STVWX"})
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

func TestStatusPrintsStoppingForAStoppingRunner(t *testing.T) {
	now := time.Date(2026, 10, 3, 14, 0, 0, 0, time.UTC)
	var out bytes.Buffer
	printStatus(&out, model.Status{Now: now, Mode: "queue", GlobalMax: 3, Instances: []model.InstanceStatus{
		{ID: "a3f9c1", Repo: "darkcloud", State: "busy", Stopping: true, Since: now.Add(-time.Minute),
			Job: &model.JobInfo{Name: "e2e", RunNumber: "412"}},
		{ID: "b7d2e0", Repo: "darkcloud", State: "idle", Since: now.Add(-time.Minute)},
	}})
	rows := map[string][]string{}
	for _, line := range strings.Split(out.String(), "\n") {
		if f := strings.Fields(line); len(f) > 0 {
			rows[f[0]] = f
		}
	}
	if got := strings.Join(rows["a3f9c1"], " "); got != "a3f9c1 darkcloud stopping e2e #412 1m0s" {
		t.Errorf("stopping row %q in:\n%s", got, out.String())
	}
	if got := strings.Join(rows["b7d2e0"], " "); got != "b7d2e0 darkcloud idle - 1m0s" {
		t.Errorf("idle row %q in:\n%s", got, out.String())
	}
	if !strings.Contains(out.String(), "global 2/3") {
		t.Errorf("a stopping runner must still count:\n%s", out.String())
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
		`{"name":"r4","max":4,"labels":["a"],"allow_public":false}`,
		`{"name":"r5","max":5,"allow_public":true}`,
	}
	for _, args := range [][]string{
		{"repo", "add", "--max", "4", "--label", "a", "r4"},
		{"repo", "add", "--allow-public", "r5", "--max", "5"},
	} {
		if code, _, errOut := runCLI(t, "", args...); code != 0 {
			t.Fatalf("%v: exit %d: %s", args, code, errOut)
		}
	}
	if len(*reqs) != len(want) {
		t.Fatalf("%d requests, want %d: %v", len(*reqs), len(want), *reqs)
	}
	for i, w := range want {
		if got := strings.TrimSpace((*reqs)[i].body); got != w {
			t.Errorf("request %d: got %s want %s", i, got, w)
		}
	}
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"repo", "add", "r6", "--max", "-2"}, "--max must be >= 0"},
		{[]string{"repo", "add", "r7", "--max", "-1"}, "--max must be >= 0"},
		{[]string{"repo", "add", "r8", "extra"}, "unexpected argument extra"},
		{[]string{"repo", "add", "--max", "2"}, "usage: ghr repo add <name>"},
		{[]string{"repo", "add", "--", "-r9"}, "repo name must not begin with -: -r9"},
		{[]string{"repo", "add", "-"}, "repo name must not begin with -: -"},
		{[]string{"repo", "add", "--bogus"}, "flag provided but not defined: -bogus"},
	} {
		if code, _, errOut := runCLI(t, "", tc.args...); code != 2 || !strings.Contains(errOut, "ghr: "+tc.want) {
			first, _, _ := strings.Cut(errOut, "\n")
			t.Errorf("%v: exit %d stderr %q, want exit 2 and %q", tc.args, code, first, tc.want)
		}
	}
	if len(*reqs) != len(want) {
		t.Fatalf("rejected adds reached the daemon: %v", *reqs)
	}
	code, out, errOut := runCLI(t, "", "repo", "add", "-h")
	if code != 0 || !strings.Contains(out, "usage: ghr") || errOut != "" || len(*reqs) != len(want) {
		t.Fatalf("repo add -h: exit %d stdout %d bytes stderr %q requests %v", code, len(out), errOut, *reqs)
	}
}

func TestTokenSetReadsStdin(t *testing.T) {
	reqs := fakeDaemon(t)
	stubPasswordInput(t, false)
	if code, _, _ := runCLI(t, "github_pat_new\n", "token", "set"); code != 0 {
		t.Fatal("token set failed")
	}
	if r := (*reqs)[0]; r.method != "PUT" || r.path != "/token" || r.body != "github_pat_new" {
		t.Fatalf("request %+v", r)
	}
}

// At a terminal the token is asked for without echo, so it stays out of the
// screen and the scrollback.
func TestTokenSetPromptsAtATerminal(t *testing.T) {
	reqs := fakeDaemon(t)
	stubPasswordInput(t, true, " github_pat_typed ")
	code, out, errOut := runCLI(t, "not read\n", "token", "set")
	if code != 0 || out != "" || errOut != "GitHub token: \n" {
		t.Fatalf("exit %d out %q err %q", code, out, errOut)
	}
	if r := (*reqs)[0]; r.method != "PUT" || r.path != "/token" || r.body != "github_pat_typed" {
		t.Fatalf("request %+v", r)
	}
}

func TestDrainAndResumeAllTakeNoArguments(t *testing.T) {
	reqs := fakeDaemon(t)
	for _, args := range [][]string{{"drain", "darkcloud"}, {"resume-all", "darkcloud"}, {"status", "darkcloud"}} {
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
	reqs := fakeDaemon(t)
	if code, out, _ := runCLI(t, "", "logs", "a3f9c1"); code != 0 || out != "hello log\nsecond chunk\n" {
		t.Fatalf("logs %d %q", code, out)
	}
	code, out, _ := runCLI(t, "", "history", "--repo", "darkmem")
	if code != 0 || !strings.Contains(out, "#88") || !strings.Contains(out, "failure") || !strings.Contains(out, "1m0s") {
		t.Fatalf("history %d:\n%s", code, out)
	}
	for _, r := range *reqs {
		if strings.HasPrefix(r.path, "/history") && strings.Contains(r.path, "since=") {
			t.Fatalf("ghr history sent a since bound: %s", r.path)
		}
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

func TestCode(t *testing.T) {
	reqs := fakeDaemon(t)
	code, out, errOut := runCLI(t, "", "code")
	if code != 0 || out != "ABCDE-FGHJK-MNPQR-STVWX\n" {
		t.Fatalf("exit %d out %q err %q", code, out, errOut)
	}
	if last := (*reqs)[len(*reqs)-1]; last.method != "GET" || last.path != "/web/setup-code" {
		t.Fatalf("request %+v", last)
	}
	if code, _, errOut := runCLI(t, "", "code", "now"); code != 2 || !strings.Contains(errOut, "usage: ghr code") {
		t.Errorf("extra argument: exit %d err %q", code, errOut)
	}
	if _, out, _ := runCLI(t, "", "help"); !strings.Contains(out, "\n  code ") {
		t.Fatalf("usage lacks code:\n%s", out)
	}
}

func TestCodeWithAPasswordSet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]string{"error": "a web password is already set"})
	}))
	t.Cleanup(srv.Close)
	newClient = func() *api.Client { return &api.Client{Base: srv.URL, HTTP: srv.Client()} }
	code, out, errOut := runCLI(t, "", "code")
	if code != 1 || out != "" || !strings.Contains(errOut, "ghr: a web password is already set") {
		t.Fatalf("exit %d out %q err %q", code, out, errOut)
	}
}

func TestWebResetPassword(t *testing.T) {
	reqs := fakeDaemon(t)
	code, out, errOut := runCLI(t, "", "web", "reset-password")
	if code != 0 || out != "web password removed; set a new one with: ghr web set-password, or in the browser with setup code ABCDE-FGHJK-MNPQR-STVWX\n" {
		t.Fatalf("exit %d out %q err %q", code, out, errOut)
	}
	if n := len(*reqs); n < 2 || (*reqs)[n-2].method != "POST" || (*reqs)[n-2].path != "/web/reset-password" ||
		(*reqs)[n-1].method != "GET" || (*reqs)[n-1].path != "/web/setup-code" {
		t.Fatalf("requests %+v", *reqs)
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
// no-echo reads from answers. There is no terminal whose input could be
// hidden.
func stubPasswordInput(t *testing.T, terminal bool, answers ...string) {
	t.Helper()
	oldTerm, oldRead, oldHide := stdinIsTerminal, readPassword, hideInput
	stdinIsTerminal = func() bool { return terminal }
	hideInput = func() (func(), error) { return func() {}, nil }
	readPassword = func() ([]byte, error) {
		if len(answers) == 0 {
			return nil, io.EOF
		}
		a := answers[0]
		answers = answers[1:]
		return []byte(a), nil
	}
	t.Cleanup(func() { stdinIsTerminal, readPassword, hideInput = oldTerm, oldRead, oldHide })
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

// askPassword is the only code that changes the terminal's mode: it hides
// the input before the read starts and puts the terminal back when the read
// or the context has ended, whichever is first. Whenever the context ends,
// also before the read has started, the change back must be the last one,
// and the read left behind must change nothing after it.
func TestPasswordPromptAlwaysPutsTheTerminalBack(t *testing.T) {
	const beforePrompt, whileHidden, whileReading = "before the prompt", "while the input is hidden", "while the read waits"
	failed := errors.New("stdin closed")
	for _, tc := range []struct {
		name   string
		cancel string
		read   func() ([]byte, error) // nil waits for an Enter that comes too late
		want   string
		err    error
	}{
		{"the read returns", "", func() ([]byte, error) { return []byte("a long secret"), nil }, "a long secret", nil},
		{"the read fails", "", func() ([]byte, error) { return nil, failed }, "", failed},
		{"cancelled before the prompt", beforePrompt, nil, "", context.Canceled},
		{"cancelled before the read starts", whileHidden, nil, "", context.Canceled},
		{"cancelled while the read waits", whileReading, nil, "", context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				stubPasswordInput(t, true)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				terminal, changes := "saved", []string(nil)
				hideInput = func() (func(), error) {
					terminal, changes = "raw", append(changes, "raw")
					if tc.cancel == whileHidden {
						cancel()
					}
					return func() { terminal, changes = "saved", append(changes, "saved") }, nil
				}
				enter := make(chan struct{})
				pressEnter := sync.OnceFunc(func() { close(enter) })
				defer pressEnter()
				if readPassword = tc.read; tc.read == nil {
					readPassword = func() ([]byte, error) {
						<-enter
						return []byte("typed too late"), nil
					}
				}
				if tc.cancel == beforePrompt {
					cancel()
				}
				var prompts bytes.Buffer
				var got string
				var err error
				returned := make(chan struct{})
				go func() {
					got, err = askPassword(ctx, &prompts, "New web password: ")
					close(returned)
				}()
				synctest.Wait()
				if tc.cancel == whileReading {
					if terminal != "raw" || prompts.String() != "New web password: " {
						t.Errorf("while the read waits: terminal %s, prompts %q", terminal, prompts.String())
					}
					cancel()
					synctest.Wait()
				}
				select {
				case <-returned:
				default:
					pressEnter()
					<-returned
					t.Fatal("the prompt still waits for the read after its context ended")
				}
				if got != tc.want || !errors.Is(err, tc.err) {
					t.Errorf("password %q err %v, want %q and %v", got, err, tc.want, tc.err)
				}
				pressEnter()
				synctest.Wait()
				if terminal != "saved" || !slices.Equal(changes, []string{"raw", "saved"}) || prompts.String() != "New web password: \n" {
					t.Fatalf("terminal %s after the changes %v, prompts %q", terminal, changes, prompts.String())
				}
			})
		})
	}
}

// readHidden reads what a terminal in raw mode delivers: Enter arrives as a
// carriage return, and Ctrl-C and Ctrl-D arrive as bytes, not as a signal.
func TestReadHidden(t *testing.T) {
	for _, tc := range []struct {
		name, typed, want string
		err               error
	}{
		{"a line ended by Enter", "a long secret\r", "a long secret", nil},
		{"a line ended by a line feed", "a long secret\n", "a long secret", nil},
		{"spaces are kept", " a long secret \r", " a long secret ", nil},
		{"backspace removes a character", "a lomg\x7f\x7fng secret\r", "a long secret", nil},
		{"Ctrl-C", "\x03", "", errInterrupted},
		{"Ctrl-C after some typing", "a long\x03", "", errInterrupted},
		{"Ctrl-D on an empty line", "\x04", "", errInterrupted},
		{"the terminal goes away", "", "", errInterrupted},
	} {
		got, err := readHidden(strings.NewReader(tc.typed))
		if string(got) != tc.want || !errors.Is(err, tc.err) {
			t.Errorf("%s: read %q err %v, want %q and %v", tc.name, got, err, tc.want, tc.err)
		}
	}
}

func TestCtrlCAtAPromptPrintsInterrupted(t *testing.T) {
	reqs := fakeDaemon(t)
	stubPasswordInput(t, true)
	readPassword = func() ([]byte, error) { return readHidden(strings.NewReader("a long\x03")) }
	code, out, errOut := runCLI(t, "", "web", "set-password")
	if code != 1 || out != "" || errOut != "New web password: \nghr: interrupted\n" || len(*reqs) != 0 {
		t.Fatalf("exit %d out %q err %q requests %v", code, out, errOut, *reqs)
	}
}

func TestStatusWarnsWhileTheWebUIHasNoPassword(t *testing.T) {
	var out bytes.Buffer
	printStatus(&out, model.Status{Mode: "queue", GlobalMax: 1, WebSetupRequired: true})
	const warning = "warning: the web UI has no password. Run: ghr web set-password, or open the web UI with the code from: ghr code\n"
	if !strings.HasPrefix(out.String(), warning) {
		t.Fatalf("output:\n%s", out.String())
	}
	out.Reset()
	printStatus(&out, model.Status{Mode: "queue", GlobalMax: 1})
	if strings.Contains(out.String(), "warning:") {
		t.Fatalf("warned with a password set:\n%s", out.String())
	}
}

func TestStatusWhileUnconfigured(t *testing.T) {
	var out bytes.Buffer
	printStatus(&out, model.Status{Mode: "queue", GlobalMax: 2, Unconfigured: true, WebSetupRequired: true})
	want := "warning: ghr is not configured. Run: ghr setup github --owner <owner>, or open the web UI\n" +
		"warning: the web UI has no password. Run: ghr web set-password, or open the web UI with the code from: ghr code\n"
	if out.String() != want {
		t.Fatalf("output:\n%s", out.String())
	}
	out.Reset()
	printStatus(&out, model.Status{Mode: "queue", GlobalMax: 2})
	if !strings.HasPrefix(out.String(), "mode queue") {
		t.Fatalf("a status without the field must print as configured:\n%s", out.String())
	}
}

func TestStatusPrintsTheArtifactQuota(t *testing.T) {
	resets := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	runsOut := time.Date(2026, 10, 24, 6, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		alert *model.StorageAlert
		want  string
	}{
		{&model.StorageAlert{Level: "exhausted", UsedGBHours: 373.3, IncludedGBHours: 363.28, UsedPct: 103, ResetsAt: resets},
			"artifacts  373 / 363 GB-h this month (103%), exhausted until Nov 1"},
		{&model.StorageAlert{Level: "critical", UsedGBHours: 330.2, IncludedGBHours: 363.28, UsedPct: 91, RunsOutAt: &runsOut, ResetsAt: resets},
			"artifacts  330 / 363 GB-h this month (91%), critical, runs out around Oct 24"},
		{&model.StorageAlert{Level: "warn", UsedGBHours: 260, IncludedGBHours: 363.28, UsedPct: 72, RunsOutAt: &runsOut, ResetsAt: resets},
			"artifacts  260 / 363 GB-h this month (72%), warning, runs out around Oct 24"},
		{&model.StorageAlert{Level: "projected", UsedGBHours: 120, IncludedGBHours: 363.28, UsedPct: 33, RunsOutAt: &runsOut, ResetsAt: resets},
			"artifacts  120 / 363 GB-h this month (33%), runs out around Oct 24"},
		{&model.StorageAlert{UsedGBHours: 20.4, IncludedGBHours: 363.28, UsedPct: 6, ResetsAt: resets},
			"artifacts  20 / 363 GB-h this month (6%)"},
		{nil, ""},
	} {
		var out bytes.Buffer
		printStatus(&out, model.Status{Mode: "queue", GlobalMax: 1, GitHubStorage: tc.alert})
		got := ""
		for _, line := range strings.Split(out.String(), "\n") {
			if strings.HasPrefix(line, "artifacts") {
				got = line
			}
		}
		if got != tc.want {
			t.Errorf("artifacts line %q, want %q in:\n%s", got, tc.want, out.String())
		}
	}
}
