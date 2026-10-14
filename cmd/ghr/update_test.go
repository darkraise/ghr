package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/model"
)

func TestRunnerUpdateCommands(t *testing.T) {
	reqs := fakeDaemon(t)
	code, out, errOut := runCLI(t, "", "runner-update")
	if code != 0 || out != "runner update queued; it runs when no job is running or queued\n" {
		t.Fatalf("queue: exit %d out %q err %q", code, out, errOut)
	}
	if code, out, errOut := runCLI(t, "", "runner-update", "--cancel"); code != 0 || out != "" {
		t.Fatalf("cancel: exit %d out %q err %q", code, out, errOut)
	}
	if code, _, errOut := runCLI(t, "", "runner-update", "--now"); code != 2 || !strings.Contains(errOut, "usage: ghr runner-update [--cancel]") {
		t.Fatalf("bad flag: exit %d err %q", code, errOut)
	}
	var got []string
	for _, r := range *reqs {
		got = append(got, r.method+" "+r.path)
	}
	if strings.Join(got, "|") != "POST /runner-update|DELETE /runner-update" {
		t.Fatalf("requests %v", got)
	}
}

func TestRunnerUpdateReportsRefusal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]string{"error": "runner 2.337.0 is already up to date"})
	}))
	t.Cleanup(srv.Close)
	newClient = func() *api.Client { return &api.Client{Base: srv.URL, HTTP: srv.Client()} }
	if code, _, errOut := runCLI(t, "", "runner-update"); code != 1 || errOut != "ghr: runner 2.337.0 is already up to date\n" {
		t.Fatalf("exit %d err %q", code, errOut)
	}
}

func TestRunnerLine(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	at := func(d time.Duration) *time.Time { t := now.Add(d); return &t }
	for _, c := range []struct {
		u    model.RunnerUpdate
		want string
	}{
		{model.RunnerUpdate{}, "runner version unknown (no dist/current)"},
		{model.RunnerUpdate{CheckError: "github: 502"}, "runner version unknown (no dist/current)  last check failed: github: 502"},
		{model.RunnerUpdate{Installed: "2.337.0"}, "runner 2.337.0  checking…"},
		{model.RunnerUpdate{Installed: "2.337.0", Latest: "2.337.0", CheckedAt: at(-2 * time.Hour)}, "runner 2.337.0  up to date (checked 2h0m0s ago)"},
		{model.RunnerUpdate{Installed: "2.337.0", Latest: "2.338.0", CheckedAt: at(0), Deadline: at(30 * 24 * time.Hour)},
			"runner 2.337.0 → 2.338.0  update available, update by " + now.Add(30*24*time.Hour).Local().Format(time.DateOnly) + " (30 days)"},
		{model.RunnerUpdate{Installed: "2.337.0", Latest: "2.338.0", CheckedAt: at(0), Deadline: at(-time.Hour), Queued: true},
			"runner 2.337.0 → 2.338.0  update available, update by " + now.Add(-time.Hour).Local().Format(time.DateOnly) + " (overdue)  queued: runs when no job is running or queued"},
		{model.RunnerUpdate{Installed: "2.337.0", Latest: "2.338.0", CheckedAt: at(0), Deadline: at(time.Hour), Queued: true, Running: true},
			"runner 2.337.0 → 2.338.0  update available, update by " + now.Add(time.Hour).Local().Format(time.DateOnly) + " (0 days)  updating"},
		{model.RunnerUpdate{Installed: "2.337.0", CheckError: "github: 502 bad gateway"}, "runner 2.337.0  last check failed: github: 502 bad gateway"},
	} {
		if got := runnerLine(model.Status{Now: now, RunnerUpdate: c.u}); got != c.want {
			t.Errorf("%+v:\n got %q\nwant %q", c.u, got, c.want)
		}
	}
}

// scriptedDaemon serves GET /status from statuses in turn, repeating the
// last; a nil entry answers 502, as a daemon that is restarting cannot
// answer. Every other request is recorded and answered 204.
type scriptedDaemon struct {
	mu       sync.Mutex
	statuses []*model.Status
	reqs     []string
	conflict []string // request paths answered 409, as a task already running is
}

func newScriptedDaemon(t *testing.T, statuses ...*model.Status) *scriptedDaemon {
	t.Helper()
	d := &scriptedDaemon{statuses: statuses}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d.mu.Lock()
		defer d.mu.Unlock()
		if r.Method == http.MethodGet && r.URL.Path == "/status" {
			st := d.statuses[0]
			if len(d.statuses) > 1 {
				d.statuses = d.statuses[1:]
			}
			if st == nil {
				w.WriteHeader(http.StatusBadGateway)
				return
			}
			json.NewEncoder(w).Encode(st)
			return
		}
		body, _ := io.ReadAll(r.Body)
		d.reqs = append(d.reqs, strings.TrimSpace(r.Method+" "+r.URL.EscapedPath()+" "+string(body)))
		if slices.Contains(d.conflict, r.URL.EscapedPath()) {
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(map[string]string{"error": "already running or about to run"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	newClient = func() *api.Client { return &api.Client{Base: srv.URL, HTTP: srv.Client()} }
	savedCheck, savedInstall, savedPoll := checkWait, installWait, updatePoll
	updatePoll = time.Millisecond
	t.Cleanup(func() { checkWait, installWait, updatePoll = savedCheck, savedInstall, savedPoll })
	return d
}

func (d *scriptedDaemon) requests() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Clone(d.reqs)
}

var updNow = time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)

func updAt(d time.Duration) *time.Time { t := updNow.Add(d); return &t }

// updJobs is the two release checks, on unless named in off.
func updJobs(off ...string) []model.JobStatus {
	var out []model.JobStatus
	for _, name := range []string{config.JobGhrUpdateCheck, config.JobRunnerUpdateCheck} {
		out = append(out, model.JobStatus{Name: name, Enabled: !slices.Contains(off, name), Every: "1d"})
	}
	return out
}

func TestUpdateShowsBothVersions(t *testing.T) {
	newScriptedDaemon(t, &model.Status{
		Now:          updNow,
		GhrUpdate:    model.GhrUpdate{Installed: "v0.1.25", Latest: "v0.1.26", Newer: true, CheckedAt: updAt(-2 * time.Hour)},
		RunnerUpdate: model.RunnerUpdate{Installed: "2.337.0", Latest: "2.337.0", CheckedAt: updAt(-time.Hour)},
		Jobs:         updJobs(config.JobRunnerUpdateCheck),
	})
	code, out, errOut := runCLI(t, "", "update")
	want := "ghr v0.1.25 → v0.1.26  update available (checked 2h0m0s ago)\n" +
		"runner 2.337.0  up to date (checked 1h0m0s ago)\n" +
		"warning: the runner update check is off: " + config.RunnerCheckOffReason + "\n"
	if code != 0 || out != want {
		t.Fatalf("exit %d err %q\n got %q\nwant %q", code, errOut, out, want)
	}
}

func TestGhrLine(t *testing.T) {
	for _, c := range []struct {
		g       model.GhrUpdate
		blocked []string
		want    string
	}{
		{model.GhrUpdate{Installed: "v0.1.25"}, nil, "ghr v0.1.25  checking…"},
		{model.GhrUpdate{Installed: "v0.1.26", Latest: "v0.1.26", CheckedAt: updAt(-time.Hour)}, nil, "ghr v0.1.26  up to date (checked 1h0m0s ago)"},
		{model.GhrUpdate{Installed: "dev", Development: true, Latest: "v0.1.26", CheckedAt: updAt(0)}, nil, "ghr dev  development build, newest release v0.1.26 (checked 0s ago)"},
		{model.GhrUpdate{Installed: "v0.1.25", Latest: "v0.1.26", CheckError: "GET: 502 Bad Gateway"}, nil, "ghr v0.1.25  last check failed: GET: 502 Bad Gateway"},
		{model.GhrUpdate{Installed: "v0.1.25", Latest: "v0.1.26", Newer: true, Installing: true, Step: "verify"}, nil, "ghr v0.1.25 → v0.1.26  installing: verify"},
		{model.GhrUpdate{Installed: "v0.1.25", Latest: "v0.1.26", Newer: true, Installing: true, Step: "waiting"}, []string{"pruning"}, "ghr v0.1.25 → v0.1.26  installing: waiting for: pruning"},
		{model.GhrUpdate{Installed: "v0.1.25", Latest: "v0.1.26", Newer: true, RestartPending: "v0.1.26"}, nil, "ghr v0.1.25 → v0.1.26  installed, restart pending"},
		{model.GhrUpdate{Installed: "v0.1.25", Latest: "v0.1.26", Newer: true, CheckedAt: updAt(0), LastOutcome: "failed", LastError: "smoke: bad"}, nil,
			"ghr v0.1.25 → v0.1.26  update available (checked 0s ago)  last install failed: smoke: bad"},
		{model.GhrUpdate{Installed: "v0.1.25", Latest: "v0.1.26", Newer: true, CheckedAt: updAt(0), LastOutcome: "cancelled"}, nil,
			"ghr v0.1.25 → v0.1.26  update available (checked 0s ago)  last install cancelled"},
		{model.GhrUpdate{Installed: "v0.1.26", Latest: "v0.1.26", CheckedAt: updAt(0), LastOutcome: "ok"}, nil, "ghr v0.1.26  up to date (checked 0s ago)  updated"},
	} {
		st := model.Status{Now: updNow, GhrUpdate: c.g, Restart: model.RestartStatus{BlockedBy: c.blocked}}
		if got := ghrLine(st); got != c.want {
			t.Errorf("%+v:\n got %q\nwant %q", c.g, got, c.want)
		}
	}
}

func TestUpdateCheck(t *testing.T) {
	before := &model.Status{Now: updNow, Jobs: updJobs()}
	running := &model.Status{Now: updNow, Jobs: updJobs()}
	running.Jobs[0].Running, running.Jobs[0].LastStart = true, updAt(time.Second)
	done := &model.Status{Now: updNow.Add(2 * time.Second), Jobs: updJobs(),
		GhrUpdate: model.GhrUpdate{Installed: "v0.1.26", Latest: "v0.1.26", CheckedAt: updAt(time.Second)}}
	for i := range done.Jobs {
		done.Jobs[i].LastStart, done.Jobs[i].LastFinish = updAt(time.Second), updAt(time.Second)
	}
	d := newScriptedDaemon(t, before, running, done)
	code, out, errOut := runCLI(t, "", "update", "--check")
	if code != 0 || !strings.HasPrefix(out, "ghr v0.1.26  up to date (checked 0s ago)\n") {
		t.Fatalf("exit %d out %q err %q", code, out, errOut)
	}
	if got := d.requests(); !slices.Equal(got, []string{"POST /jobs/ghr_update_check/run", "POST /jobs/runner_update_check/run"}) {
		t.Fatalf("requests %q", got)
	}
}

func TestUpdateCheckSkipsACheckThatIsOff(t *testing.T) {
	before := &model.Status{Now: updNow, Jobs: updJobs(config.JobRunnerUpdateCheck)}
	done := &model.Status{Now: updNow, Jobs: updJobs(config.JobRunnerUpdateCheck)}
	done.Jobs[0].LastStart, done.Jobs[0].LastFinish = updAt(time.Second), updAt(time.Second)
	d := newScriptedDaemon(t, before, done)
	code, out, errOut := runCLI(t, "", "update", "--check")
	if code != 0 || !strings.HasPrefix(out, "runner_update_check is off; skipped\n") {
		t.Fatalf("exit %d out %q err %q", code, out, errOut)
	}
	if got := d.requests(); !slices.Equal(got, []string{"POST /jobs/ghr_update_check/run"}) {
		t.Fatalf("requests %q", got)
	}
}

// Both checks are due when the daemon starts, so one may already be about to
// run, or running since before the request, when --check asks for it.
func TestUpdateCheckJoinsACheckInFlight(t *testing.T) {
	started := updNow.Add(-time.Minute)
	before := &model.Status{Now: updNow, Jobs: updJobs()}
	before.Jobs[0].Pending = true
	running := &model.Status{Now: updNow, Jobs: updJobs()}
	running.Jobs[0].Running = true
	running.Jobs[0].LastStart = &started
	done := &model.Status{Now: updNow, Jobs: updJobs()}
	done.Jobs[0].LastStart = &started
	for i := range done.Jobs {
		if done.Jobs[i].Name == "runner_update_check" {
			s := updNow.Add(time.Second)
			done.Jobs[i].LastStart = &s
		}
	}
	d := newScriptedDaemon(t, before, running, done)
	d.conflict = []string{"/jobs/ghr_update_check/run"}
	code, _, errOut := runCLI(t, "", "update", "--check")
	if code != 0 {
		t.Fatalf("exit %d err %q", code, errOut)
	}
	if got := d.requests(); !slices.Equal(got, []string{"POST /jobs/ghr_update_check/run", "POST /jobs/runner_update_check/run"}) {
		t.Fatalf("requests %q", got)
	}
}

func TestUpdateCheckGivesUp(t *testing.T) {
	before := &model.Status{Now: updNow, Jobs: updJobs()}
	running := &model.Status{Now: updNow, Jobs: updJobs()}
	running.Jobs[0].Running = true
	newScriptedDaemon(t, before, running)
	checkWait = 20 * time.Millisecond
	code, _, errOut := runCLI(t, "", "update", "--check")
	if code != 1 || errOut != "ghr: ghr_update_check, runner_update_check still running after 20ms\n" {
		t.Fatalf("exit %d err %q", code, errOut)
	}
}

func TestUpdateGhrFollowsTheInstall(t *testing.T) {
	ghr := func(g model.GhrUpdate) *model.Status {
		g.Installed, g.Latest = "v0.1.25", "v0.1.26"
		return &model.Status{Now: updNow, GhrUpdate: g}
	}
	steps := []*model.Status{ghr(model.GhrUpdate{Newer: true})}
	for _, s := range []string{"download", "verify", "smoke", "waiting", "swap"} {
		steps = append(steps, ghr(model.GhrUpdate{Installing: true, Step: s}))
	}
	steps = append(steps, ghr(model.GhrUpdate{Installing: true, Step: "restart", RestartPending: "v0.1.26"}), nil, nil,
		&model.Status{Now: updNow, GhrUpdate: model.GhrUpdate{Installed: "v0.1.26", Latest: "v0.1.26", LastOutcome: "ok"}})
	d := newScriptedDaemon(t, steps...)
	code, out, errOut := runCLI(t, "", "update", "ghr")
	want := "installing ghr v0.1.26\n" +
		"downloading\n" +
		"verifying the checksum and the archive\n" +
		"testing the new binary\n" +
		"waiting for running work to finish\n" +
		"installing the files\n" +
		"restarting ghr\n" +
		"ghr v0.1.26 is running\n"
	if code != 0 || out != want {
		t.Fatalf("exit %d err %q\n got %q\nwant %q", code, errOut, out, want)
	}
	if got := d.requests(); !slices.Equal(got, []string{"POST /ghr-update"}) {
		t.Fatalf("requests %q", got)
	}
}

// With no work to wait for, the daemon can swap and drop its socket between
// two polls, so the last status the CLI saw is still an early step.
func TestUpdateGhrSurvivesAFastRestart(t *testing.T) {
	d := newScriptedDaemon(t,
		&model.Status{GhrUpdate: model.GhrUpdate{Installed: "v0.1.25", Latest: "v0.1.26", Newer: true}},
		&model.Status{GhrUpdate: model.GhrUpdate{Installed: "v0.1.25", Latest: "v0.1.26", Installing: true, Step: "download"}},
		nil,
		&model.Status{GhrUpdate: model.GhrUpdate{Installed: "v0.1.26", Latest: "v0.1.26", LastOutcome: "ok"}},
	)
	code, out, errOut := runCLI(t, "", "update", "ghr")
	if want := "installing ghr v0.1.26\ndownloading\nghr v0.1.26 is running\n"; code != 0 || out != want {
		t.Fatalf("exit %d err %q\n got %q\nwant %q", code, errOut, out, want)
	}
	if got := d.requests(); !slices.Equal(got, []string{"POST /ghr-update"}) {
		t.Fatalf("requests %q", got)
	}
}

func TestUpdateGhrReportsAFailure(t *testing.T) {
	newScriptedDaemon(t,
		&model.Status{GhrUpdate: model.GhrUpdate{Installed: "v0.1.25", Latest: "v0.1.26", Newer: true}},
		&model.Status{GhrUpdate: model.GhrUpdate{Installed: "v0.1.25", Latest: "v0.1.26", Installing: true, Step: "download"}},
		&model.Status{GhrUpdate: model.GhrUpdate{Installed: "v0.1.25", Latest: "v0.1.26", LastOutcome: "failed", LastError: "verify: SHA-256 mismatch"}},
	)
	if code, _, errOut := runCLI(t, "", "update", "ghr"); code != 1 || errOut != "ghr: ghr update failed: verify: SHA-256 mismatch\n" {
		t.Fatalf("exit %d err %q", code, errOut)
	}
}

func TestUpdateGhrGivesUpOnTheRestart(t *testing.T) {
	newScriptedDaemon(t,
		&model.Status{GhrUpdate: model.GhrUpdate{Installed: "v0.1.25", Latest: "v0.1.26", Newer: true}},
		&model.Status{GhrUpdate: model.GhrUpdate{Installed: "v0.1.25", Latest: "v0.1.26", RestartPending: "v0.1.26"}},
		nil,
	)
	installWait = 20 * time.Millisecond
	if code, _, errOut := runCLI(t, "", "update", "ghr"); code != 1 || errOut != "ghr: restart requested; the daemon is not back\n" {
		t.Fatalf("exit %d err %q", code, errOut)
	}
}

// Work that starts during the swap sends the install back to waiting; the
// two minutes count from the restart, not from the swap.
func TestUpdateGhrWaitsAfterTheSwap(t *testing.T) {
	ghr := func(g model.GhrUpdate) *model.Status {
		g.Installed, g.Latest = "v0.1.25", "v0.1.26"
		return &model.Status{Now: updNow, GhrUpdate: g}
	}
	steps := []*model.Status{ghr(model.GhrUpdate{Newer: true}), ghr(model.GhrUpdate{Installing: true, Step: "swap"})}
	for range 100 {
		steps = append(steps, ghr(model.GhrUpdate{Installing: true, Step: "waiting", RestartPending: "v0.1.26"}))
	}
	steps = append(steps, ghr(model.GhrUpdate{Installing: true, Step: "restart", RestartPending: "v0.1.26"}), nil,
		&model.Status{Now: updNow, GhrUpdate: model.GhrUpdate{Installed: "v0.1.26", Latest: "v0.1.26", LastOutcome: "ok"}})
	newScriptedDaemon(t, steps...)
	installWait = 50 * time.Millisecond
	want := "installing ghr v0.1.26\n" +
		"installing the files\n" +
		"waiting for running work to finish\n" +
		"restarting ghr\n" +
		"ghr v0.1.26 is running\n"
	if code, out, errOut := runCLI(t, "", "update", "ghr"); code != 0 || out != want {
		t.Fatalf("exit %d err %q\n got %q\nwant %q", code, errOut, out, want)
	}
}

func TestUpdateSubcommands(t *testing.T) {
	d := newScriptedDaemon(t, &model.Status{})
	for _, c := range []struct {
		args []string
		out  string
	}{
		{[]string{"update", "ghr", "--cancel"}, ""},
		{[]string{"update", "runner"}, "runner update queued; it runs when no job is running or queued\n"},
		{[]string{"update", "runner", "--cancel"}, ""},
		{[]string{"runner-update"}, "runner update queued; it runs when no job is running or queued\n"},
	} {
		if code, out, errOut := runCLI(t, "", c.args...); code != 0 || out != c.out {
			t.Errorf("%v: exit %d out %q err %q", c.args, code, out, errOut)
		}
	}
	want := []string{"DELETE /ghr-update", "POST /runner-update", "DELETE /runner-update", "POST /runner-update"}
	if got := d.requests(); !slices.Equal(got, want) {
		t.Fatalf("requests %q", got)
	}
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"update", "runner", "--now"}, "usage: ghr update runner [--cancel]"},
		{[]string{"update", "ghr", "--now"}, "usage: ghr update"},
		{[]string{"update", "bogus"}, "usage: ghr update"},
	} {
		if code, _, errOut := runCLI(t, "", c.args...); code != 2 || !strings.Contains(errOut, c.want) {
			t.Errorf("%v: exit %d err %q", c.args, code, errOut)
		}
	}
}

func TestJobsCommands(t *testing.T) {
	jobs := []model.JobStatus{
		{Name: "ghr_update_check", Enabled: true, Every: "1d", LastStart: updAt(-time.Hour), LastFinish: updAt(-time.Hour), Next: updAt(23 * time.Hour)},
		{Name: "runner_update_check", Enabled: true, Every: "1d", LastStart: updAt(-time.Hour), LastFinish: updAt(-time.Hour), LastError: "github: 502", Next: updAt(0)},
		{Name: "prune", Enabled: false, Every: "12h"},
		{Name: "storage_measure", Enabled: true, Every: "30m0s", Running: true, LastStart: updAt(0)},
	}
	d := newScriptedDaemon(t, &model.Status{Now: updNow, Jobs: jobs})
	code, out, errOut := runCLI(t, "", "jobs")
	if code != 0 {
		t.Fatalf("exit %d err %q", code, errOut)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	want := [][]string{
		{"NAME", "ON", "EVERY", "LAST", "RUN", "NEXT"},
		{"ghr_update_check", "on", "1d", "ok", "1h0m0s", "ago", "in", "23h0m0s"},
		{"runner_update_check", "on", "1d", "failed", "1h0m0s", "ago:", "github:", "502", "due"},
		{"prune", "off", "12h", "never", "off"},
		{"storage_measure", "on", "30m0s", "running", "-"},
	}
	if len(lines) != len(want) {
		t.Fatalf("output\n%s", out)
	}
	for i, l := range lines {
		if got := strings.Fields(l); !slices.Equal(got, want[i]) {
			t.Errorf("line %d: %q, want %q", i, got, want[i])
		}
	}

	for _, args := range [][]string{
		{"jobs", "run", "prune"},
		{"jobs", "set", "prune", "--every", "12h", "--off"},
		{"jobs", "set", "storage_measure", "--on"},
	} {
		if code, _, errOut := runCLI(t, "", args...); code != 0 {
			t.Fatalf("%v: exit %d err %q", args, code, errOut)
		}
	}
	reqs := d.requests()
	if len(reqs) != 3 || reqs[0] != "POST /jobs/prune/run" ||
		!strings.Contains(reqs[1], `"jobs":{"prune":{"enabled":false,"every":"12h"}}`) ||
		!strings.Contains(reqs[2], `"jobs":{"storage_measure":{"enabled":true}}`) {
		t.Fatalf("requests %q", reqs)
	}
	for _, args := range [][]string{
		{"jobs", "run"},
		{"jobs", "set", "prune"},
		{"jobs", "set", "prune", "--on", "--off"},
		{"jobs", "set", "prune", "--every", "1h", "extra"},
		{"jobs", "bogus"},
	} {
		if code, _, errOut := runCLI(t, "", args...); code != 2 || !strings.Contains(errOut, "usage: ghr jobs") {
			t.Errorf("%v: exit %d err %q", args, code, errOut)
		}
	}
}

func TestTheDaemonKnowsItsVersion(t *testing.T) {
	if got := daemonOptions().Version; got != version {
		t.Fatalf("daemon version %q, want %q", got, version)
	}
}
