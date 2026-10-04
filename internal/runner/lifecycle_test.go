package runner

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/sched"
)

func TestSpawnWritesMetaAndStartsUnit(t *testing.T) {
	h := newHarness(t)
	if err := h.m.spawn(context.Background(), h.cfg, "darkcloud"); err != nil {
		t.Fatal(err)
	}
	if h.gh.jitCalls[0] != "ghr-darkcloud-aaaaaa self-hosted,linux,x64,homelab" {
		t.Fatalf("jit call %q", h.gh.jitCalls[0])
	}
	var meta Meta
	if err := readJSON(filepath.Join(h.m.instanceDir("aaaaaa"), MetaFile), &meta); err != nil {
		t.Fatal(err)
	}
	if meta.RunnerID != 101 || meta.DistVersion != "2.330.0" || meta.RunnerName != "ghr-darkcloud-aaaaaa" ||
		!reflect.DeepEqual(meta.Labels, []string{"homelab"}) || !meta.SpawnedAt.Equal(h.now) {
		t.Fatalf("meta %+v", meta)
	}
	u := h.sd.started[0]
	if u.Unit != "ghr-runner-aaaaaa" || u.User != "ghrunner" || u.Command[1] != "--jitconfig" || u.Command[2] != "ENC-ghr-darkcloud-aaaaaa" {
		t.Fatalf("unit %+v", u)
	}
	if u.Env["COMPOSE_PROJECT_NAME"] != "ghr-aaaaaa" || u.Env["GHR_INSTANCE_DIR"] != h.m.instanceDir("aaaaaa") ||
		!strings.HasSuffix(u.Env["ACTIONS_RUNNER_HOOK_JOB_STARTED"], "job-started.sh") || u.Env["DOTNET_INSTALL_DIR"] == "" {
		t.Fatalf("env %+v", u.Env)
	}
	if strings.Join(u.Props, " ") != "MemoryMax=6G CPUQuota=200% KillMode=control-group" {
		t.Fatalf("props %v", u.Props)
	}
	if len(h.host.chowned) != 1 || !strings.HasSuffix(h.host.chowned[0], ":ghrunner") {
		t.Fatalf("chown %v", h.host.chowned)
	}
	if h.state("aaaaaa") != "starting" {
		t.Fatalf("state %s", h.state("aaaaaa"))
	}
}

func TestSpawnFailureCleansUp(t *testing.T) {
	h := newHarness(t)
	h.m.Paths.Dist = filepath.Join(h.root, "missing")
	if err := h.m.spawn(context.Background(), h.cfg, "darkcloud"); err == nil {
		t.Fatal("expected error")
	}
	if len(h.gh.deleted) != 1 || h.gh.deleted[0] != 101 {
		t.Fatalf("registration not deleted: %v", h.gh.deleted)
	}
	if _, err := os.Stat(h.m.instanceDir("aaaaaa")); !os.IsNotExist(err) {
		t.Fatal("instance dir left behind")
	}
}

func TestUniqueIDSkipsArchivedLogsAndPendingHistory(t *testing.T) {
	h := newHarness(t)
	os.MkdirAll(filepath.Join(h.m.Paths.Logs, "aaaaaa"), 0o755)
	os.MkdirAll(h.m.Paths.Pending, 0o755)
	os.WriteFile(filepath.Join(h.m.Paths.Pending, "bbbbbb.json"), []byte("{}"), 0o644)
	if err := h.m.spawn(context.Background(), h.cfg, "darkmem"); err != nil {
		t.Fatal(err)
	}
	if h.state("cccccc") != "starting" {
		t.Fatalf("expected cccccc, events:\n%s", h.eventText())
	}
}

func TestStartingToIdleToBusy(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkmem")
	h.m.refreshRunners(context.Background())
	if h.state("aaaaaa") != "starting" {
		t.Fatal("offline runner should stay starting")
	}
	h.gh.setRunner(101, "online", false)
	h.m.refreshRunners(context.Background())
	if h.state("aaaaaa") != "idle" {
		t.Fatalf("state %s", h.state("aaaaaa"))
	}
	h.writeJob(t, "aaaaaa", 55)
	h.m.readJobFiles()
	if h.state("aaaaaa") != "busy" {
		t.Fatalf("state %s", h.state("aaaaaa"))
	}
	st := h.m.Status()
	if st.Instances[0].Job == nil || st.Instances[0].Job.RunID != 55 || st.Instances[0].Job.RunNumber != "412" {
		t.Fatalf("job %+v", st.Instances[0].Job)
	}
}

// No hook record and no jobs API entry yet: the runners API alone moves idle to busy.
func TestIdleRunnerReportedBusyBecomesBusy(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkmem")
	h.gh.setRunner(101, "online", false)
	h.m.refreshRunners(context.Background())
	h.gh.setRunner(101, "online", true)
	h.m.refreshRunners(context.Background())
	if h.state("aaaaaa") != "busy" {
		t.Fatalf("state %s", h.state("aaaaaa"))
	}
}

func completedJob(h *harness, runID int64, runner, conclusion string) github.Job {
	started := h.now.Add(-2 * time.Minute)
	done := h.now
	return github.Job{ID: 9, RunID: runID, Name: "CI / e2e", Status: "completed", Conclusion: conclusion,
		RunnerName: runner, StartedAt: &started, CompletedAt: &done, HTMLURL: "https://x/9"}
}

func TestExitRecordsHistoryArchivesLogsAndCleansUp(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkcloud")
	h.writeJob(t, "aaaaaa", 55)
	os.WriteFile(filepath.Join(h.m.instanceDir("aaaaaa"), "_diag", "Worker_1.log"), []byte("log"), 0o644)
	h.gh.setJobs(55, completedJob(h, 55, "ghr-darkcloud-aaaaaa", "success"))
	h.docker.byLabel["com.docker.compose.project=ghr-aaaaaa"] = []string{"c1"}
	h.sd.active["ghr-runner-aaaaaa"] = false

	h.m.refreshUnits(context.Background())
	h.m.Wait()

	if h.state("aaaaaa") != "gone" {
		t.Fatalf("state %s", h.state("aaaaaa"))
	}
	got, _ := h.m.History.Query("", "", 0)
	if len(got) != 1 || got[0].Conclusion != "success" || got[0].JobName != "CI / e2e" || got[0].HTMLURL != "https://x/9" {
		t.Fatalf("history %+v", got)
	}
	archive := filepath.Join(h.m.Paths.Logs, "aaaaaa")
	if _, err := os.Stat(filepath.Join(archive, "Worker_1.log")); err != nil {
		t.Fatal("log not archived")
	}
	if h.host.chowned[len(h.host.chowned)-1] != archive+":root" {
		t.Fatalf("archive not chowned to root: %v", h.host.chowned)
	}
	if _, err := os.Stat(h.m.instanceDir("aaaaaa")); !os.IsNotExist(err) {
		t.Fatal("instance dir kept")
	}
	if _, err := os.Stat(h.m.pendingPath("aaaaaa")); !os.IsNotExist(err) {
		t.Fatal("pending record kept")
	}
	if len(h.gh.deleted) != 1 || h.gh.deleted[0] != 101 {
		t.Fatalf("deleted %v", h.gh.deleted)
	}
	if !strings.Contains(h.eventText(), "ok darkcloud #412 CI / e2e success 2m0s  cleanup: 1 ctrs") {
		t.Fatalf("events %s", h.eventText())
	}
	if h.m.Status().Repos[0].LastJob == nil {
		t.Fatal("last job not recorded")
	}
}

func TestExitWithoutJobRecordsNoHistory(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkmem")
	h.sd.active["ghr-runner-aaaaaa"] = false
	h.m.refreshUnits(context.Background())
	h.m.Wait()
	if got := h.history(t); len(got) != 0 || !strings.Contains(h.eventText(), "exited without a job") {
		t.Fatalf("history %v events %s", got, h.eventText())
	}
}

// A killed job's conclusion arrives after the runner exits: history waits for it.
func TestDelayedConclusionIsRetried(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkmem")
	h.writeJob(t, "aaaaaa", 55)
	running := completedJob(h, 55, "ghr-darkmem-aaaaaa", "")
	running.Status, running.CompletedAt = "in_progress", nil
	h.gh.setJobs(55, running)
	h.sd.active["ghr-runner-aaaaaa"] = false
	h.m.refreshUnits(context.Background())
	h.m.Wait()
	if h.state("aaaaaa") != "gone" || len(h.history(t)) != 0 {
		t.Fatalf("state %s history %v: must wait for the conclusion", h.state("aaaaaa"), h.history(t))
	}
	h.m.finalizePending(context.Background())
	if len(h.history(t)) != 0 {
		t.Fatal("still in progress: no history yet")
	}
	h.gh.setJobs(55, completedJob(h, 55, "ghr-darkmem-aaaaaa", "cancelled"))
	h.m.finalizePending(context.Background())
	if got := h.history(t); !reflect.DeepEqual(got, []string{"aaaaaa cancelled"}) {
		t.Fatalf("history %v", got)
	}
}

func TestConclusionFallsBackToUnknownAfterDeadline(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkmem")
	h.writeJob(t, "aaaaaa", 55)
	h.gh.setErr("ListJobs darkmem", &github.APIError{Status: 502, Kind: github.ErrServer})
	h.sd.active["ghr-runner-aaaaaa"] = false
	h.m.refreshUnits(context.Background())
	h.m.Wait()
	h.m.finalizePending(context.Background())
	if len(h.history(t)) != 0 {
		t.Fatal("API failure before the deadline must retry, not record")
	}
	h.now = h.now.Add(conclusionWait)
	h.m.finalizePending(context.Background())
	if got := h.history(t); !reflect.DeepEqual(got, []string{"aaaaaa unknown"}) {
		t.Fatalf("history %v", got)
	}
}

// The hook record is missing, but the jobs API confirmed the job: history still records it.
func TestFinishFallsBackToConfirmedJob(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkmem")
	job := completedJob(h, 77, "ghr-darkmem-aaaaaa", "")
	job.Status, job.CompletedAt = "in_progress", nil
	h.m.mu.Lock() // what Tick's confirm records from the jobs API
	h.m.insts["aaaaaa"].Job = &model.JobInfo{RunID: job.RunID, Name: job.Name, StartedAt: *job.StartedAt}
	h.m.mu.Unlock()
	os.WriteFile(filepath.Join(h.m.instanceDir("aaaaaa"), JobFile), []byte("{not json"), 0o644)
	h.gh.setJobs(77, completedJob(h, 77, "ghr-darkmem-aaaaaa", "failure"))
	h.sd.active["ghr-runner-aaaaaa"] = false
	h.m.refreshUnits(context.Background())
	h.m.Wait()
	got, _ := h.m.History.Query("", "", 0)
	if len(got) != 1 || got[0].RunID != 77 || got[0].Conclusion != "failure" {
		t.Fatalf("history %+v", got)
	}
}

// Without a CompletedAt from the API, the hook's finished_at is the finish time.
func TestFinishTimeFromCompletionHook(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkmem")
	h.writeJob(t, "aaaaaa", 55, `"finished_at":"2026-10-03T12:05:00Z"`)
	h.gh.setJobs(55, github.Job{RunID: 55, Name: "e2e", Status: "completed", Conclusion: "success", RunnerName: "ghr-darkmem-aaaaaa"})
	h.now = h.now.Add(time.Hour) // cleanup runs much later
	h.sd.active["ghr-runner-aaaaaa"] = false
	h.m.refreshUnits(context.Background())
	h.m.Wait()
	got, _ := h.m.History.Query("", "", 0)
	if len(got) != 1 || !got[0].FinishedAt.Equal(time.Date(2026, 10, 3, 12, 5, 0, 0, time.UTC)) {
		t.Fatalf("history %+v", got)
	}
}

// A Docker failure keeps the instance cleaning (holding the repo slot) and is retried.
func TestDockerFailureKeepsSlotAndRetries(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkcloud")
	h.writeJob(t, "aaaaaa", 55)
	h.gh.setJobs(55, completedJob(h, 55, "ghr-darkcloud-aaaaaa", "success"))
	h.docker.setErr("RemoveVolumesByLabel", errors.New("volume in use"))
	h.sd.active["ghr-runner-aaaaaa"] = false
	h.m.refreshUnits(context.Background())
	h.m.Wait()
	if h.state("aaaaaa") != "cleaning" || !strings.Contains(h.eventText(), "volume in use") {
		t.Fatalf("state %s events %s", h.state("aaaaaa"), h.eventText())
	}
	if _, err := os.Stat(h.m.instanceDir("aaaaaa")); err != nil {
		t.Fatal("instance dir removed before cleanup succeeded")
	}
	if spawns := sched.Plan(h.cfg, h.m.schedInstances(h.cfg), sched.Demand{"darkcloud": {{Repo: "darkcloud", ID: 1, CreatedAt: h.now}}}, h.now); len(spawns) != 0 {
		t.Fatalf("repo cap 1 must stay held while cleanup fails, got %v", spawns)
	}
	h.docker.setErr("RemoveVolumesByLabel", nil)
	h.m.refreshUnits(context.Background()) // before the retry delay: nothing
	h.m.Wait()
	if h.state("aaaaaa") != "cleaning" {
		t.Fatal("retried before finishRetry")
	}
	h.now = h.now.Add(finishRetry)
	h.m.refreshUnits(context.Background())
	h.m.Wait()
	if h.state("aaaaaa") != "gone" || !reflect.DeepEqual(h.history(t), []string{"aaaaaa success"}) {
		t.Fatalf("state %s history %v", h.state("aaaaaa"), h.history(t))
	}
}

// A network another container keeps attached never goes away: after
// maxCleanupFailures attempts the slot and registration are released anyway.
func TestPersistentDockerFailureReleasesSlot(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkcloud")
	h.writeJob(t, "aaaaaa", 55)
	h.gh.setJobs(55, completedJob(h, 55, "ghr-darkcloud-aaaaaa", "success"))
	h.docker.setErr("RemoveNetworksByLabel", errors.New("network ghr-aaaaaa_default has active endpoints"))
	h.sd.active["ghr-runner-aaaaaa"] = false
	for n := 1; n < maxCleanupFailures; n++ {
		h.m.refreshUnits(context.Background())
		h.m.Wait()
		if h.state("aaaaaa") != "cleaning" {
			t.Fatalf("attempt %d: state %s, must keep retrying", n, h.state("aaaaaa"))
		}
		h.now = h.now.Add(finishRetry)
	}
	if strings.Contains("\n"+h.eventText(), "\nerror ") || len(h.gh.deleted) != 0 {
		t.Fatalf("gave up early: deleted %v events %s", h.gh.deleted, h.eventText())
	}
	h.m.refreshUnits(context.Background())
	h.m.Wait()
	if h.state("aaaaaa") != "gone" || len(h.gh.deleted) != 1 || h.gh.deleted[0] != 101 {
		t.Fatalf("state %s deleted %v", h.state("aaaaaa"), h.gh.deleted)
	}
	if ev := h.eventText(); !strings.Contains(ev, "error darkcloud") || !strings.Contains(ev, "aaaaaa") ||
		!strings.Contains(ev, "network ghr-aaaaaa_default has active endpoints") {
		t.Fatalf("events %s", ev)
	}
	if spawns := sched.Plan(h.cfg, h.m.schedInstances(h.cfg), sched.Demand{"darkcloud": {{Repo: "darkcloud", ID: 1, CreatedAt: h.now}}}, h.now); len(spawns) != 1 {
		t.Fatalf("repo slot not released, spawns %v", spawns)
	}
	if !reflect.DeepEqual(h.history(t), []string{"aaaaaa success"}) {
		t.Fatalf("history %v", h.history(t))
	}
}

// diskFullUntilCleanup fails the pending-history and ghr-projects writes with
// fail until Docker cleanup has removed a volume, like a disk only that frees.
func diskFullUntilCleanup(t *testing.T, h *harness, fail error) {
	t.Helper()
	orig := writeFile
	t.Cleanup(func() { writeFile = orig })
	writeFile = func(name string, data []byte, perm os.FileMode) error {
		h.docker.mu.Lock()
		full := len(h.docker.volLabels) == 0
		h.docker.mu.Unlock()
		if full && (strings.HasPrefix(name, h.m.Paths.Pending) || filepath.Base(name) == projectsFile) {
			return &fs.PathError{Op: "write", Path: name, Err: fail}
		}
		return orig(name, data, perm)
	}
}

// On a full disk the metadata writes must not block the Docker cleanup that
// frees space; they are retried once it has run.
func TestFullDiskStillRunsDockerCleanup(t *testing.T) {
	for _, withJob := range []bool{true, false} {
		h := newHarness(t)
		diskFullUntilCleanup(t, h, syscall.ENOSPC)
		h.m.spawn(context.Background(), h.cfg, "darkcloud")
		if withJob {
			h.writeJob(t, "aaaaaa", 55)
			h.gh.setJobs(55, completedJob(h, 55, "ghr-darkcloud-aaaaaa", "success"))
		}
		h.docker.byLabel["com.docker.compose.project=ghr-aaaaaa"] = []string{"c1"}
		h.sd.active["ghr-runner-aaaaaa"] = false
		h.m.refreshUnits(context.Background())
		h.m.Wait()
		if h.state("aaaaaa") != "gone" || !reflect.DeepEqual(h.docker.removed, []string{"c1"}) {
			t.Fatalf("job %v: state %s removed %v events %s", withJob, h.state("aaaaaa"), h.docker.removed, h.eventText())
		}
		if !strings.Contains(h.eventText(), syscall.ENOSPC.Error()) {
			t.Fatalf("job %v: the full disk was not reported: %s", withJob, h.eventText())
		}
		if withJob && !reflect.DeepEqual(h.history(t), []string{"aaaaaa success"}) {
			t.Fatalf("history %v", h.history(t))
		}
	}
}

// Any other write error keeps the crash-safe order: nothing is freed before the record exists.
func TestFailedPendingWriteKeepsDockerResources(t *testing.T) {
	h := newHarness(t)
	orig := writeFile
	t.Cleanup(func() { writeFile = orig })
	writeFile = func(name string, data []byte, perm os.FileMode) error {
		if strings.HasPrefix(name, h.m.Paths.Pending) {
			return &fs.PathError{Op: "write", Path: name, Err: syscall.EIO}
		}
		return orig(name, data, perm)
	}
	h.m.spawn(context.Background(), h.cfg, "darkcloud")
	h.writeJob(t, "aaaaaa", 55)
	h.docker.byLabel["com.docker.compose.project=ghr-aaaaaa"] = []string{"c1"}
	h.sd.active["ghr-runner-aaaaaa"] = false
	h.m.refreshUnits(context.Background())
	h.m.Wait()
	if h.state("aaaaaa") != "cleaning" || len(h.docker.removed) != 0 || !strings.Contains(h.eventText(), "write pending history") {
		t.Fatalf("state %s removed %v events %s", h.state("aaaaaa"), h.docker.removed, h.eventText())
	}
}

func TestArchiveFailureKeepsInstanceDir(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkmem")
	os.WriteFile(h.m.Paths.Logs, []byte("a file where the log dir belongs"), 0o644)
	h.sd.active["ghr-runner-aaaaaa"] = false
	h.m.refreshUnits(context.Background())
	h.m.Wait()
	if h.state("aaaaaa") != "cleaning" || !strings.Contains(h.eventText(), "archive logs") {
		t.Fatalf("state %s events %s", h.state("aaaaaa"), h.eventText())
	}
	if _, err := os.Stat(filepath.Join(h.m.instanceDir("aaaaaa"), "_diag")); err != nil {
		t.Fatal("logs deleted although archiving failed")
	}
}

func TestStopIdleRechecksBusy(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkmem")
	h.m.spawn(context.Background(), h.cfg, "darkcloud")
	h.gh.setRunner(101, "online", false)
	h.gh.setRunner(102, "online", false)
	h.m.refreshRunners(context.Background())
	h.now = h.now.Add(6 * time.Minute)
	h.gh.setRunner(102, "online", true) // darkcloud picked a job at the last moment
	h.m.stopIdle(context.Background(), h.cfg, h.now)
	if len(h.sd.stopped) != 1 || h.sd.stopped[0] != "ghr-runner-aaaaaa" {
		t.Fatalf("stopped %v", h.sd.stopped)
	}
	if h.state("bbbbbb") != "busy" {
		t.Fatalf("bbbbbb state %s", h.state("bbbbbb"))
	}
}

// Only an affirmative busy=false stops a runner; a failed check leaves it running.
func TestStopIdleLeavesRunnerOnCheckFailure(t *testing.T) {
	for _, err := range []error{
		&github.APIError{Status: 502, Kind: github.ErrServer},
		&github.APIError{Status: 404, Kind: github.ErrNotFound},
		errors.New("connection reset"),
	} {
		h := newHarness(t)
		h.m.spawn(context.Background(), h.cfg, "darkmem")
		h.gh.setRunner(101, "online", false)
		h.m.refreshRunners(context.Background())
		h.now = h.now.Add(6 * time.Minute)
		h.gh.setErr("GetRunner darkmem", err)
		h.m.stopIdle(context.Background(), h.cfg, h.now)
		if len(h.sd.stopped) != 0 || h.state("aaaaaa") != "idle" {
			t.Fatalf("%v: stopped %v state %s", err, h.sd.stopped, h.state("aaaaaa"))
		}
	}
}

// A failed or cancelled systemd query is not an exit: the busy runner's dir stays.
func TestUnitQueryErrorIsNotAnExit(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkmem")
	h.writeJob(t, "aaaaaa", 55)
	h.m.readJobFiles()
	h.sd.activeErr["ghr-runner-aaaaaa"] = errors.New("Failed to connect to bus")
	h.m.refreshUnits(context.Background())
	h.m.Wait()
	if h.state("aaaaaa") != "busy" {
		t.Fatalf("state %s", h.state("aaaaaa"))
	}
	if _, err := os.Stat(h.m.instanceDir("aaaaaa")); err != nil {
		t.Fatal("instance dir removed")
	}
}

func TestStartTimeoutStopsUnit(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkmem")
	h.now = h.now.Add(3 * time.Minute)
	h.m.stopStartTimedOut(context.Background(), h.cfg, h.now)
	if len(h.sd.stopped) != 1 || !strings.Contains(h.eventText(), "did not come online within 2m0s") {
		t.Fatalf("stopped %v events %s", h.sd.stopped, h.eventText())
	}
}

func TestKill(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkmem")
	if err := h.m.Kill(context.Background(), "aaaaaa"); err != nil {
		t.Fatal(err)
	}
	if err := h.m.Kill(context.Background(), "zzzzzz"); err == nil {
		t.Fatal("unknown runner accepted")
	}
	if h.sd.stopped[0] != "ghr-runner-aaaaaa" {
		t.Fatalf("stopped %v", h.sd.stopped)
	}
}

// A read error that is not a decode error is retried, not deleted.
func TestFinalizeKeepsUnreadablePending(t *testing.T) {
	h := newHarness(t)
	path := h.m.pendingPath("aaaaaa")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	h.m.finalizePending(context.Background())
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("pending path removed: %v; events %s", err, h.eventText())
	}
	if strings.Contains(h.eventText(), "removed") {
		t.Fatalf("events %s", h.eventText())
	}
}

func TestHookRecordFillsRunNumberOfConfirmedJob(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkmem")
	h.m.mu.Lock()
	h.m.insts["aaaaaa"].State = sched.Busy
	h.m.insts["aaaaaa"].Job = &model.JobInfo{RunID: 55, Name: "build"}
	h.m.mu.Unlock()
	h.writeJob(t, "aaaaaa", 55)
	h.m.readJobFiles()
	h.m.mu.Lock()
	j := *h.m.insts["aaaaaa"].Job
	h.m.mu.Unlock()
	if j.RunNumber != "412" || j.Workflow != "CI" || j.Name != "build" {
		t.Fatalf("job %+v", j)
	}
}

func TestCompletionOnlyRecordHasNoNegativeDuration(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkmem")
	finished := h.now.Add(time.Minute).UTC().Format(time.RFC3339)
	if err := os.WriteFile(filepath.Join(h.m.instanceDir("aaaaaa"), JobFile), []byte(`{"finished_at":"`+finished+`"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	h.now = h.now.Add(5 * time.Minute)
	h.sd.active["ghr-runner-aaaaaa"] = false
	h.m.refreshUnits(context.Background())
	h.m.Wait()
	got, _ := h.m.History.Query("", "", 0)
	if len(got) != 1 || got[0].StartedAt.After(got[0].FinishedAt) {
		t.Fatalf("history %+v", got)
	}
}
