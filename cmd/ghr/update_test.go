package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/api"
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
