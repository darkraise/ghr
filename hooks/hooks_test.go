package hooks

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// wrapper stands in for the runner's worker as the hook's parent process, so a
// hook that kills its parent kills the wrapper and never the test binary.
const wrapper = `"$1" --noprofile --norc -e -o pipefail "$2"; s=$?; echo "wrapper: hook exited $s"; exit $s`

// hookCmd runs script the way the runner runs .sh hooks, under wrapper. The
// GITHUB_* event variables are cleared so a CI run of these tests does not
// leak its own event.
func hookCmd(t *testing.T, script string, env ...string) *exec.Cmd {
	t.Helper()
	// Skip on a developer machine without the tools; CI (GitHub sets CI=true) must run these.
	skip := t.Skip
	if os.Getenv("CI") != "" {
		skip = t.Fatal
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		skip("bash not available")
	}
	if _, err := exec.LookPath("jq"); err != nil {
		skip("jq not available")
	}
	cmd := exec.Command(bash, "-c", wrapper, "wrapper", bash, script)
	cmd.Env = append(os.Environ(), "GITHUB_EVENT_NAME=", "GITHUB_EVENT_PATH=", "GITHUB_REPOSITORY=")
	cmd.Env = append(cmd.Env, env...)
	return cmd
}

func runHook(t *testing.T, script string, env ...string) {
	t.Helper()
	if out, err := hookCmd(t, script, env...).CombinedOutput(); err != nil {
		t.Fatalf("%s exited non-zero: %v\n%s", script, err, out)
	}
}

func readRecord(t *testing.T, dir string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "job.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rec map[string]string
	if err := json.Unmarshal(data, &rec); err != nil {
		t.Fatalf("invalid JSON %q: %v", data, err)
	}
	return rec
}

// checkTime asserts v is an RFC3339 UTC second-precision time within [from, to].
func checkTime(t *testing.T, name, v string, from, to time.Time) {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, v)
	if err != nil {
		t.Fatalf("%s %q is not RFC3339: %v", name, v, err)
	}
	if ts.Before(from.Truncate(time.Second)) || ts.After(to) {
		t.Fatalf("%s %v outside [%v, %v]", name, ts, from, to)
	}
}

func TestJobStartedWritesJobJSON(t *testing.T) {
	dir := t.TempDir()
	before := time.Now()
	runHook(t, "job-started.sh", "GHR_INSTANCE_DIR="+dir, "GITHUB_RUN_ID=123", "GITHUB_RUN_ATTEMPT=1",
		"GITHUB_RUN_NUMBER=412", "GITHUB_WORKFLOW=CI \"main\"\n\tnightly", `GITHUB_JOB=e2e\win`, "RUNNER_NAME=ghr-darkcloud-abc123")
	rec := readRecord(t, dir)
	if rec["run_id"] != "123" || rec["run_attempt"] != "1" || rec["run_number"] != "412" ||
		rec["workflow"] != "CI \"main\"\n\tnightly" || rec["job"] != `e2e\win` || rec["runner_name"] != "ghr-darkcloud-abc123" {
		t.Fatalf("record %v", rec)
	}
	checkTime(t, "started_at", rec["started_at"], before, time.Now())
	if _, err := os.Stat(filepath.Join(dir, "job.json.tmp")); !os.IsNotExist(err) {
		t.Fatal("temp file left behind")
	}
}

func TestJobStartedRefusesForkEvents(t *testing.T) {
	const repo = "darkraise/ghr"
	pr := func(head string) string { return `{"pull_request":{"head":{"repo":{"full_name":"` + head + `"}}}}` }
	missing := "<missing>"
	for _, tc := range []struct {
		name, event, payload string
		refuse               bool
	}{
		{"fork pull_request", "pull_request", pr("evil/ghr"), true},
		{"fork pull_request_target", "pull_request_target", pr("evil/ghr"), true},
		{"fork pull_request_review", "pull_request_review", pr("evil/ghr"), true},
		{"fork pull_request_review_comment", "pull_request_review_comment", pr("evil/ghr"), true},
		{"fork workflow_run", "workflow_run", `{"workflow_run":{"head_repository":{"full_name":"evil/ghr"}}}`, true},
		{"pull_request without head repo", "pull_request", `{"pull_request":{"head":{"repo":null}}}`, true},
		{"pull_request with invalid payload", "pull_request", `{`, true},
		{"pull_request without event path", "pull_request", missing, true},
		{"same-repo pull_request", "pull_request", pr(repo), false},
		{"same-repo pull_request in another case", "pull_request", pr("Darkraise/GHR"), false},
		{"same-repo workflow_run", "workflow_run", `{"workflow_run":{"head_repository":{"full_name":"darkraise/ghr"}}}`, false},
		{"push", "push", `{}`, false},
		{"issue_comment runs base-branch code", "issue_comment", `{}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			env := []string{"GHR_INSTANCE_DIR=" + dir, "GITHUB_RUN_ID=123", "GITHUB_EVENT_NAME=" + tc.event, "GITHUB_REPOSITORY=" + repo}
			if tc.payload != missing {
				path := filepath.Join(t.TempDir(), "event.json")
				if err := os.WriteFile(path, []byte(tc.payload), 0o644); err != nil {
					t.Fatal(err)
				}
				env = append(env, "GITHUB_EVENT_PATH="+path)
			}
			if !tc.refuse {
				out, err := hookCmd(t, "job-started.sh", env...).CombinedOutput()
				if err != nil || !strings.Contains(string(out), "wrapper: hook exited 0") {
					t.Fatalf("want the hook to exit 0 under a live parent, got %v\n%s", err, out)
				}
				if rec := readRecord(t, dir); rec["run_id"] != "123" {
					t.Fatalf("record %v", rec)
				}
				return
			}
			// A plain non-zero exit would leave the job's always() steps to run,
			// so the hook must kill its parent, the worker.
			out, err := hookCmd(t, "job-started.sh", env...).CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) {
				t.Fatalf("want the parent killed, got %v\n%s", err, out)
			}
			if ws, ok := exit.Sys().(syscall.WaitStatus); !ok || !ws.Signaled() || ws.Signal() != syscall.SIGKILL {
				t.Fatalf("want the parent killed by SIGKILL, got %v\n%s", err, out)
			}
			if strings.Contains(string(out), "wrapper:") {
				t.Fatalf("the parent outlived the hook: %s", out)
			}
			if !strings.Contains(string(out), "refusing") {
				t.Fatalf("no refusal message: %s", out)
			}
			if _, err := os.Stat(filepath.Join(dir, "job.json")); !os.IsNotExist(err) {
				t.Fatal("a refused job was recorded")
			}
		})
	}
}

func TestHooksExitZeroWithoutInstanceDir(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	runHook(t, "job-started.sh", "GHR_INSTANCE_DIR=")
	runHook(t, "job-started.sh", "GHR_INSTANCE_DIR="+missing)
	runHook(t, "job-completed.sh", "GHR_INSTANCE_DIR=")
	runHook(t, "job-completed.sh", "GHR_INSTANCE_DIR="+missing)
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatal("a hook created the missing instance dir")
	}
}

func TestJobCompletedAddsFinishedAt(t *testing.T) {
	dir := t.TempDir()
	runHook(t, "job-started.sh", "GHR_INSTANCE_DIR="+dir, "GITHUB_RUN_ID=123", "RUNNER_NAME=ghr-r-abc123")
	before := time.Now()
	runHook(t, "job-completed.sh", "GHR_INSTANCE_DIR="+dir)
	rec := readRecord(t, dir)
	if rec["run_id"] != "123" || rec["runner_name"] != "ghr-r-abc123" {
		t.Fatalf("start fields lost: %v", rec)
	}
	checkTime(t, "finished_at", rec["finished_at"], before, time.Now())
}

func TestJobCompletedWithoutJobJSON(t *testing.T) {
	dir := t.TempDir()
	before := time.Now()
	runHook(t, "job-completed.sh", "GHR_INSTANCE_DIR="+dir)
	rec := readRecord(t, dir)
	if len(rec) != 1 {
		t.Fatalf("want finished_at only, got %v", rec)
	}
	checkTime(t, "finished_at", rec["finished_at"], before, time.Now())
}

func TestJobCompletedWithEmptyJobJSON(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "job.json"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	before := time.Now()
	runHook(t, "job-completed.sh", "GHR_INSTANCE_DIR="+dir)
	rec := readRecord(t, dir)
	if len(rec) != 1 {
		t.Fatalf("want finished_at only, got %v", rec)
	}
	checkTime(t, "finished_at", rec["finished_at"], before, time.Now())
}

// A job can leave a FIFO at job.json; reading it would block the hook forever.
func TestJobCompletedSkipsFIFOJobJSON(t *testing.T) {
	runHook(t, "job-completed.sh", "GHR_INSTANCE_DIR=") // applies runHook's bash/jq skip rules
	if _, err := exec.LookPath("mkfifo"); err != nil {
		t.Skip("mkfifo not available")
	}
	dir := t.TempDir()
	fifo := filepath.Join(dir, "job.json")
	if out, err := exec.Command("mkfifo", fifo).CombinedOutput(); err != nil {
		t.Fatalf("mkfifo: %v\n%s", err, out)
	}
	cmd := hookCmd(t, "job-completed.sh", "GHR_INSTANCE_DIR="+dir)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	cmd.WaitDelay = time.Second
	before := time.Now()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	kill := time.AfterFunc(15*time.Second, func() { cmd.Process.Kill() })
	err := cmd.Wait()
	kill.Stop()
	// Release any reader still blocked on the FIFO; O_RDWR never blocks on Linux.
	if f, ferr := os.OpenFile(fifo, os.O_RDWR, 0); ferr == nil {
		f.Close()
	}
	if err != nil {
		t.Fatalf("job-completed.sh on a FIFO job.json: %v\n%s", err, out.String())
	}
	rec := readRecord(t, dir)
	if len(rec) != 1 {
		t.Fatalf("want finished_at only, got %v", rec)
	}
	checkTime(t, "finished_at", rec["finished_at"], before, time.Now())
}

func TestJobCompletedExitsZeroWhenDateFails(t *testing.T) {
	stubs := t.TempDir()
	if err := os.WriteFile(filepath.Join(stubs, "date"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	runHook(t, "job-completed.sh", "GHR_INSTANCE_DIR="+t.TempDir(), "PATH="+stubs+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// The runner invokes .sh hooks as `bash -e <file>`. A job.json directory holding a
// job.json.tmp directory makes the final mv fail; the hooks must still exit 0.
func TestHooksExitZeroUnderErrexitWhenRenameFails(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "job.json", "job.json.tmp"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, script := range []string{"job-started.sh", "job-completed.sh"} {
		runHook(t, script, "GHR_INSTANCE_DIR="+dir)
	}
}
