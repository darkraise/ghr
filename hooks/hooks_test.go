package hooks

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func runHook(t *testing.T, script string, env ...string) {
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
	cmd := exec.Command(bash, script)
	cmd.Env = append(os.Environ(), env...)
	if out, err := cmd.CombinedOutput(); err != nil {
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
