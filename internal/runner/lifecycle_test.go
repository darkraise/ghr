package runner

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
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
	got, _ := h.m.History.Query("", "", time.Time{}, 0)
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
	got, _ := h.m.History.Query("", "", time.Time{}, 0)
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
	got, _ := h.m.History.Query("", "", time.Time{}, 0)
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

// An unreachable Docker daemon never counts toward the give-up: releasing the
// slot would delete the instance dir and leak the job's containers and volumes.
func TestDockerUnreachableKeepsCleaning(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkcloud")
	h.writeJob(t, "aaaaaa", 55)
	h.gh.setJobs(55, completedJob(h, 55, "ghr-darkcloud-aaaaaa", "success"))
	h.docker.setErr("ComposeContainers", errors.New("Cannot connect to the Docker daemon"))
	h.sd.active["ghr-runner-aaaaaa"] = false
	for n := 1; n <= maxCleanupFailures+2; n++ {
		h.m.refreshUnits(context.Background())
		h.m.Wait()
		if h.state("aaaaaa") != "cleaning" {
			t.Fatalf("attempt %d: state %s, must keep retrying", n, h.state("aaaaaa"))
		}
		h.now = h.now.Add(finishRetry)
	}
	if _, err := os.Stat(h.m.instanceDir("aaaaaa")); err != nil {
		t.Fatalf("instance dir: %v", err)
	}
	if strings.Contains("\n"+h.eventText(), "\nerror ") || len(h.gh.deleted) != 0 {
		t.Fatalf("gave up: deleted %v events %s", h.gh.deleted, h.eventText())
	}
	h.docker.setErr("ComposeContainers", nil)
	h.m.refreshUnits(context.Background())
	h.m.Wait()
	if h.state("aaaaaa") != "gone" || len(h.gh.deleted) != 1 || h.gh.deleted[0] != 101 {
		t.Fatalf("after recovery: state %s deleted %v", h.state("aaaaaa"), h.gh.deleted)
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

// The registration goes before the unit stops, so GitHub cannot hand the
// runner a job between the busy check and the stop.
func TestStopIdleDeregistersBeforeStopping(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkmem")
	h.gh.setRunner(101, "online", false)
	h.m.refreshRunners(context.Background())
	h.now = h.now.Add(6 * time.Minute)
	stoppedAtDelete := -1
	h.gh.hook = func(m string) {
		if m == "DeleteRunner" {
			h.sd.mu.Lock()
			stoppedAtDelete = len(h.sd.stopped)
			h.sd.mu.Unlock()
		}
	}
	h.m.stopIdle(context.Background(), h.cfg, h.now)
	if stoppedAtDelete != 0 || len(h.sd.stopped) != 1 || !reflect.DeepEqual(h.gh.deleted, []int64{101}) {
		t.Fatalf("stopped before delete %d, stopped %v deleted %v", stoppedAtDelete, h.sd.stopped, h.gh.deleted)
	}
}

// A runner deregistered by a stop that then failed is not checked again (its
// check would answer 404); the next stop only retries the unit.
func TestStopIdleRetriesOnlyTheUnitAfterDeregistering(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkmem")
	h.gh.setRunner(101, "online", false)
	h.m.refreshRunners(context.Background())
	h.now = h.now.Add(6 * time.Minute)
	h.sd.stopErr["ghr-runner-aaaaaa"] = errors.New("unit busy")
	h.m.stopIdle(context.Background(), h.cfg, h.now)
	if len(h.sd.stopped) != 0 || !reflect.DeepEqual(h.gh.deleted, []int64{101}) {
		t.Fatalf("stopped %v deleted %v", h.sd.stopped, h.gh.deleted)
	}
	delete(h.sd.stopErr, "ghr-runner-aaaaaa")
	calls := h.gh.callCount()
	if res := h.m.stopIdleRunner(context.Background(), h.m.snapshot()[0], h.now); res != idleStopped {
		t.Fatalf("result %v", res)
	}
	if h.gh.callCount() != calls || len(h.sd.stopped) != 1 {
		t.Fatalf("GitHub calls %d→%d stopped %v", calls, h.gh.callCount(), h.sd.stopped)
	}
}

// GitHub refuses to delete a busy runner with 422: it took a job after the
// check, so it keeps running and counts as busy. Any other failure keeps it idle.
func TestStopIdleKeepsRunnerGitHubWillNotDeregister(t *testing.T) {
	for _, c := range []struct {
		err   error
		state string
	}{
		{&github.APIError{Status: 422, Kind: github.ErrUnprocessable}, "busy"},
		{&github.APIError{Status: 502, Kind: github.ErrServer}, "idle"},
	} {
		h := newHarness(t)
		h.m.spawn(context.Background(), h.cfg, "darkmem")
		h.gh.setRunner(101, "online", false)
		h.m.refreshRunners(context.Background())
		h.now = h.now.Add(6 * time.Minute)
		h.gh.setErr("DeleteRunner darkmem", c.err)
		h.m.stopIdle(context.Background(), h.cfg, h.now)
		if len(h.sd.stopped) != 0 || h.state("aaaaaa") != c.state {
			t.Fatalf("%v: stopped %v state %s", c.err, h.sd.stopped, h.state("aaaaaa"))
		}
	}
}

// A tick stops its idle runners from a snapshot, and a stop a person asked for
// can start a runner's cleanup after that snapshot was taken.
func TestStopIdleRunnerLeavesARunnerInCleanupAlone(t *testing.T) {
	for _, tc := range []struct {
		name        string
		failCleanup bool
		state       string
	}{
		{"the runner is gone", false, "gone"},
		{"the runner is cleaning", true, "cleaning"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.m.spawn(context.Background(), h.cfg, "darkcloud")
			if tc.failCleanup {
				h.writeJob(t, "aaaaaa", 55)
				h.gh.setJobs(55, completedJob(h, 55, "ghr-darkcloud-aaaaaa", "success"))
				h.docker.setErr("RemoveVolumesByLabel", errors.New("volume in use"))
			}
			stale := h.m.snapshot()[0]
			if err := h.m.Kill(context.Background(), "aaaaaa"); err != nil {
				t.Fatal(err)
			}
			h.m.Wait()
			if h.state("aaaaaa") != tc.state {
				t.Fatalf("state %s after the kill, want %s", h.state("aaaaaa"), tc.state)
			}
			calls := h.gh.callsText()
			res := h.m.stopIdleRunner(context.Background(), stale, h.now)
			if res != idleStopped {
				t.Fatalf("result %d, want %d (idleStopped)", res, idleStopped)
			}
			if got := strings.TrimPrefix(h.gh.callsText(), calls); strings.Contains(got, "GetRunner") || strings.Contains(got, "DeleteRunner") {
				t.Fatalf("GitHub calls after the kill's own:%s", strings.ReplaceAll(got, ",", " "))
			}
			if len(h.sd.stopped) != 1 {
				t.Fatalf("unit stops %v, want only the kill's", h.sd.stopped)
			}
			if ev := h.eventText(); strings.Contains(ev, "stopped idle runner") || strings.Contains(ev, "token lacks access") {
				t.Fatalf("events %s", ev)
			}
		})
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
	h.m.refreshRunners(context.Background()) // the runners API answers: still offline
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

// stoppingOf is the Stopping flag Status reports for id.
func stoppingOf(h *harness, id string) bool {
	for _, i := range h.m.Status().Instances {
		if i.ID == id {
			return i.Stopping
		}
	}
	return false
}

func TestKillMarksTheRunnerStoppingWhileItsUnitStops(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkmem")
	h.sd.stopGate, h.sd.stopEntered = make(chan struct{}), make(chan struct{}, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- h.m.Kill(ctx, "aaaaaa") }()
	<-h.sd.stopEntered
	if !stoppingOf(h, "aaaaaa") || h.state("aaaaaa") != "starting" {
		t.Fatalf("while the unit stops: stopping %v state %s", stoppingOf(h, "aaaaaa"), h.state("aaaaaa"))
	}
	// The caller gives up: the stop goes on and the flag stays.
	cancel()
	select {
	case err := <-done:
		t.Fatalf("Kill returned %v when its caller's context ended", err)
	case <-time.After(50 * time.Millisecond):
	}
	if !stoppingOf(h, "aaaaaa") {
		t.Fatal("flag cleared when the caller's context ended")
	}
	close(h.sd.stopGate)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	h.m.Wait()
	if s := h.state("aaaaaa"); s == "starting" || stoppingOf(h, "aaaaaa") || !strings.Contains(h.eventText(), "runner aaaaaa stopped by user") {
		t.Fatalf("after the stop: state %s stopping %v events %s", s, stoppingOf(h, "aaaaaa"), h.eventText())
	}
}

func TestKillClearsStoppingWhenTheStopFails(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkmem")
	h.sd.stopErr["ghr-runner-aaaaaa"] = errors.New("bus error")
	if err := h.m.Kill(context.Background(), "aaaaaa"); err == nil || err.Error() != "bus error" {
		t.Fatalf("kill err %v", err)
	}
	if stoppingOf(h, "aaaaaa") || strings.Contains(h.eventText(), "stopped by user") {
		t.Fatalf("stopping %v events %s", stoppingOf(h, "aaaaaa"), h.eventText())
	}
}

// A successful stop reaches cleaning without a tick, and Status hides the
// flag from then on.
func TestACleaningRunnerNeverReportsStopping(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkcloud")
	h.writeJob(t, "aaaaaa", 55)
	h.gh.setJobs(55, completedJob(h, 55, "ghr-darkcloud-aaaaaa", "success"))
	h.docker.setErr("RemoveVolumesByLabel", errors.New("volume in use"))
	if err := h.m.Kill(context.Background(), "aaaaaa"); err != nil {
		t.Fatal(err)
	}
	h.m.Wait()
	if h.state("aaaaaa") != "cleaning" || stoppingOf(h, "aaaaaa") {
		t.Fatalf("state %s stopping %v", h.state("aaaaaa"), stoppingOf(h, "aaaaaa"))
	}
}

func TestKillLeavesAFailedCleanupToItsRetry(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkcloud")
	h.writeJob(t, "aaaaaa", 55)
	h.gh.setJobs(55, completedJob(h, 55, "ghr-darkcloud-aaaaaa", "success"))
	h.docker.setErr("RemoveVolumesByLabel", errors.New("volume in use"))
	if err := h.m.Kill(context.Background(), "aaaaaa"); err != nil {
		t.Fatal(err)
	}
	h.m.Wait()
	h.docker.setErr("RemoveVolumesByLabel", nil)
	if err := h.m.Kill(context.Background(), "aaaaaa"); err != nil {
		t.Fatal(err)
	}
	h.m.Wait()
	if h.state("aaaaaa") != "cleaning" {
		t.Fatalf("state %s: a kill retried a failed cleanup before finishRetry", h.state("aaaaaa"))
	}
	h.now = h.now.Add(finishRetry)
	h.m.refreshUnits(context.Background())
	h.m.Wait()
	if h.state("aaaaaa") != "gone" || !reflect.DeepEqual(h.history(t), []string{"aaaaaa success"}) {
		t.Fatalf("state %s history %v", h.state("aaaaaa"), h.history(t))
	}
}

// A tick can read a runner as running, wait on systemd, and resume after a
// stop has started the cleanup and the cleanup has failed.
func TestKillLeavesNoEarlyRetryToATickThatSawTheRunnerRunning(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkcloud")
	h.writeJob(t, "aaaaaa", 55)
	h.gh.setJobs(55, completedJob(h, 55, "ghr-darkcloud-aaaaaa", "success"))
	h.docker.setErr("RemoveVolumesByLabel", errors.New("volume in use"))
	entered, release := make(chan struct{}), make(chan struct{})
	var held atomic.Bool
	h.sd.onActive = func(string) {
		if held.CompareAndSwap(false, true) {
			close(entered)
			<-release
		}
	}
	refreshed := make(chan struct{})
	go func() {
		h.m.refreshUnits(context.Background())
		close(refreshed)
	}()
	<-entered
	if err := h.m.Kill(context.Background(), "aaaaaa"); err != nil {
		t.Fatal(err)
	}
	h.m.Wait()
	close(release)
	<-refreshed
	h.m.Wait()
	if n := strings.Count(h.eventText(), "cleanup of aaaaaa failed"); n != 1 || h.state("aaaaaa") != "cleaning" {
		t.Fatalf("%d cleanup attempts, state %s; want one attempt and cleaning", n, h.state("aaaaaa"))
	}
}

// The job's conclusion is not known yet: the record waits in pending, where
// the UI reads it as finishing.
func TestKillBeforeAnyTickLeavesAPendingRecord(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkcloud")
	h.writeJob(t, "aaaaaa", 55)
	if err := h.m.Kill(context.Background(), "aaaaaa"); err != nil {
		t.Fatal(err)
	}
	h.m.Wait()
	got := h.m.Pending()
	if len(got) != 1 || got[0].ID != "aaaaaa" || got[0].RunID != 55 || got[0].RunNumber != "412" || got[0].Workflow != "CI" {
		t.Fatalf("pending %+v", got)
	}
	if hist := h.history(t); len(hist) != 0 {
		t.Fatalf("history %v before the job's conclusion is known", hist)
	}
}

func TestKillFinishesWhileATickIsBlockedInTheAPI(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkcloud")
	h.writeJob(t, "aaaaaa", 55)
	h.gh.setJobs(55, completedJob(h, 55, "ghr-darkcloud-aaaaaa", "success"))
	h.docker.setErr("RemoveVolumesByLabel", errors.New("volume in use"))
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	h.gh.hook = func(method string) {
		if method == "ListRuns" {
			once.Do(func() {
				close(entered)
				<-release
			})
		}
	}
	ticked := make(chan struct{})
	go func() {
		h.m.Tick(context.Background())
		close(ticked)
	}()
	<-entered
	if err := h.m.Kill(context.Background(), "aaaaaa"); err != nil {
		t.Fatal(err)
	}
	if h.state("aaaaaa") != "cleaning" {
		t.Fatalf("state %s while the tick is still in its first ListRuns", h.state("aaaaaa"))
	}
	close(release)
	<-ticked
}

func TestKillLeavesAnUnconfirmedExitToTheTick(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  func(h *harness)
	}{
		{"the check fails", func(h *harness) { h.sd.activeErr["ghr-runner-aaaaaa"] = errors.New("bus error") }},
		{"the unit still reads active", func(h *harness) { h.sd.keepActive = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.m.spawn(context.Background(), h.cfg, "darkmem")
			tc.set(h)
			if err := h.m.Kill(context.Background(), "aaaaaa"); err != nil {
				t.Fatal(err)
			}
			h.m.Wait()
			if h.state("aaaaaa") != "starting" || !stoppingOf(h, "aaaaaa") {
				t.Fatalf("state %s stopping %v, want starting and stopping", h.state("aaaaaa"), stoppingOf(h, "aaaaaa"))
			}
			delete(h.sd.activeErr, "ghr-runner-aaaaaa")
			h.sd.active["ghr-runner-aaaaaa"] = false
			h.m.refreshUnits(context.Background())
			h.m.Wait()
			if h.state("aaaaaa") != "gone" {
				t.Fatalf("state %s after the tick saw the exit", h.state("aaaaaa"))
			}
		})
	}
}

// The first cleanup is held at its last step until both kills have returned,
// so a second one would have been started while it ran, and would by now have
// failed on the first one's work or reached the same step.
func TestTwoKillsStartOneCleanup(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkmem")
	entered, release := make(chan struct{}, 2), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	h.gh.hook = func(method string) {
		if method == "DeleteRunner" {
			entered <- struct{}{}
			<-release
		}
	}
	h.sd.stopGate, h.sd.stopEntered = make(chan struct{}), make(chan struct{}, 2)
	done := make(chan error, 2)
	for range 2 {
		go func() { done <- h.m.Kill(context.Background(), "aaaaaa") }()
	}
	<-h.sd.stopEntered
	<-h.sd.stopEntered
	close(h.sd.stopGate)
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if h.state("aaaaaa") != "cleaning" {
		t.Fatalf("state %s after both kills, want cleaning", h.state("aaaaaa"))
	}
	<-entered
	time.Sleep(50 * time.Millisecond)
	unblock()
	h.m.Wait()
	if n := len(h.gh.deleted); n != 1 || strings.Contains(h.eventText(), "cleanup of aaaaaa failed") || h.state("aaaaaa") != "gone" {
		t.Fatalf("%d cleanups completed, state %s, events %s; want one cleanup, none failed, and gone", n, h.state("aaaaaa"), h.eventText())
	}
}

func TestKillCleanupHoldsShutdownUntilItEnds(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkmem")
	entered, release := make(chan struct{}), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	h.gh.hook = func(method string) {
		if method == "DeleteRunner" {
			close(entered)
			<-release
		}
	}
	if err := h.m.Kill(context.Background(), "aaaaaa"); err != nil {
		t.Fatal(err)
	}
	if h.state("aaaaaa") != "cleaning" {
		t.Fatalf("state %s after the kill, want cleaning", h.state("aaaaaa"))
	}
	<-entered
	h.m.Close()
	waited := make(chan struct{})
	go func() {
		h.m.Wait()
		close(waited)
	}()
	select {
	case <-waited:
		t.Fatal("Wait returned while the cleanup a kill started was still running")
	case <-time.After(50 * time.Millisecond):
	}
	unblock()
	<-waited
	if h.state("aaaaaa") != "gone" {
		t.Fatalf("state %s after Wait, want gone", h.state("aaaaaa"))
	}
}

// A kill can outlive the daemon's shutdown: its stop completes after Close
// and Wait have returned.
func TestKillAfterCloseStartsNoCleanup(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkmem")
	h.sd.stopGate, h.sd.stopEntered = make(chan struct{}), make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() { done <- h.m.Kill(context.Background(), "aaaaaa") }()
	<-h.sd.stopEntered
	h.m.Close()
	h.m.Wait()
	close(h.sd.stopGate)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	h.m.Wait()
	if h.state("aaaaaa") != "starting" || !stoppingOf(h, "aaaaaa") {
		t.Fatalf("state %s stopping %v, want starting and stopping", h.state("aaaaaa"), stoppingOf(h, "aaaaaa"))
	}
	if _, err := os.Stat(h.m.instanceDir("aaaaaa")); err != nil {
		t.Fatalf("instance dir after Close: %v", err)
	}
}

// Under the race detector this is what catches a cleanup counted outside the
// manager lock. Whichever of the kill and the shutdown wins, the runner is in
// the same state when Wait returns as it is afterwards.
func TestKillRacingShutdownLeavesNoCleanupRunning(t *testing.T) {
	for range 50 {
		h := newHarness(t)
		h.m.spawn(context.Background(), h.cfg, "darkmem")
		killed := make(chan error, 1)
		go func() { killed <- h.m.Kill(context.Background(), "aaaaaa") }()
		h.m.Close()
		h.m.Wait()
		atWait := h.state("aaaaaa")
		if err := <-killed; err != nil {
			t.Fatal(err)
		}
		h.m.Wait()
		if s := h.state("aaaaaa"); s != atWait {
			t.Fatalf("state %s when Wait returned and %s afterwards: a cleanup started after Wait", atWait, s)
		}
	}
}

func TestPendingListsJobsAwaitingTheirConclusion(t *testing.T) {
	h := newHarness(t)
	e := model.HistoryEntry{ID: "aaaaaa", Repo: "darkmem", RunID: 7, JobName: "build", Conclusion: "unknown",
		StartedAt: time.Date(2026, 10, 3, 13, 0, 0, 0, time.UTC), FinishedAt: time.Date(2026, 10, 3, 13, 5, 0, 0, time.UTC)}
	if err := os.MkdirAll(h.m.Paths.Pending, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeJSONAtomic(h.m.pendingPath("aaaaaa"), pending{Entry: e, RunnerName: "ghr-aaaaaa"}); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(h.m.pendingPath("bbbbbb"), []byte("{not json"), 0o644)
	os.WriteFile(filepath.Join(h.m.Paths.Pending, "notes.txt"), []byte("x"), 0o644)
	os.MkdirAll(h.m.pendingPath("cccccc"), 0o755)
	got := h.m.Pending()
	if len(got) != 1 || got[0].ID != "aaaaaa" || got[0].JobName != "build" || !got[0].FinishedAt.Equal(e.FinishedAt) {
		t.Fatalf("pending %+v", got)
	}
	if _, err := os.Stat(h.m.pendingPath("bbbbbb")); err != nil {
		t.Fatalf("Pending removed an unreadable record: %v", err)
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
	got, _ := h.m.History.Query("", "", time.Time{}, 0)
	if len(got) != 1 || got[0].StartedAt.After(got[0].FinishedAt) {
		t.Fatalf("history %+v", got)
	}
}

// spawn resolves dist/current through the host, so the version it records is
// the one current points at.
func TestSpawnResolvesDistThroughHost(t *testing.T) {
	h := newHarness(t)
	h.linkDist(t, "2.337.0")
	if err := h.m.spawn(context.Background(), h.cfg, "darkcloud"); err != nil {
		t.Fatal(err)
	}
	var meta Meta
	if err := readJSON(filepath.Join(h.m.instanceDir("aaaaaa"), MetaFile), &meta); err != nil {
		t.Fatal(err)
	}
	if meta.DistVersion != "2.337.0" {
		t.Fatalf("dist version %q", meta.DistVersion)
	}
	if !strings.Contains(h.eventText(), "spawned aaaaaa (2.337.0)") {
		t.Fatalf("events:\n%s", h.eventText())
	}
}

func TestBeginFinishStartsNothingAfterClose(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkmem")
	h.m.spawn(context.Background(), h.cfg, "darkmem")
	h.sd.active["ghr-runner-aaaaaa"] = false
	h.sd.active["ghr-runner-bbbbbb"] = false
	h.m.beginFinish("aaaaaa")
	h.m.Wait()
	if h.state("aaaaaa") != "gone" {
		t.Fatalf("state %s before Close, want gone", h.state("aaaaaa"))
	}
	h.m.Close()
	h.m.beginFinish("bbbbbb")
	h.m.Wait()
	if h.state("bbbbbb") != "starting" {
		t.Fatalf("state %s after Close, want starting", h.state("bbbbbb"))
	}
	if _, err := os.Stat(h.m.instanceDir("bbbbbb")); err != nil {
		t.Fatalf("instance dir after Close: %v", err)
	}
}

// A runner can be removed while a tick is stopping another one.
func TestStartTimeoutSkipsARunnerThatIsGone(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkmem")
	h.m.spawn(context.Background(), h.cfg, "darkmem")
	h.m.refreshRunners(context.Background()) // the runners API answers: still offline
	h.now = h.now.Add(3 * time.Minute)
	h.sd.stopGate, h.sd.stopEntered = make(chan struct{}), make(chan struct{}, 2)
	go func() {
		<-h.sd.stopEntered
		h.m.mu.Lock()
		delete(h.m.insts, "bbbbbb")
		h.m.mu.Unlock()
		close(h.sd.stopGate)
	}()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("stopStartTimedOut panicked on a runner that is gone: %v", r)
		}
	}()
	h.m.stopStartTimedOut(context.Background(), h.cfg, h.now)
	if !reflect.DeepEqual(h.sd.stopped, []string{"ghr-runner-aaaaaa"}) {
		t.Fatalf("stopped %v, want only ghr-runner-aaaaaa", h.sd.stopped)
	}
}

// The runners API is slow. A runner that took a job while its answer was on
// the way must not be put back to idle by that answer.
func TestRunnerMadeBusyDuringTheCheckStaysBusy(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkmem")
	h.gh.setRunner(101, "online", false)
	h.gh.hook = func(method string) {
		if method == "GetRunner" {
			h.m.mu.Lock()
			h.m.insts["aaaaaa"].State = sched.Busy
			h.m.mu.Unlock()
		}
	}
	h.m.refreshRunners(context.Background())
	if h.state("aaaaaa") != "busy" {
		t.Fatalf("state %s", h.state("aaaaaa"))
	}
}

// A runner that could not be checked may be online. Only an answer from the
// runners API in this tick lets the start timeout stop it.
func TestStartTimeoutWaitsForAnAnswerFromTheRunnersAPI(t *testing.T) {
	for name, block := range map[string]func(h *harness) (unblock func()){
		"API pause": func(h *harness) func() {
			h.m.apiErr("darkmem", &github.APIError{Status: 403, Kind: github.ErrRateLimit, RetryAt: h.now.Add(10 * time.Minute)}, h.now)
			return func() { h.now = h.now.Add(10 * time.Minute) }
		},
		"failed call": func(h *harness) func() {
			h.gh.setErr("GetRunner darkmem", &github.APIError{Status: 502, Kind: github.ErrServer})
			return func() { h.gh.setErr("GetRunner darkmem", nil) }
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			if err := h.m.spawn(context.Background(), h.cfg, "darkmem"); err != nil {
				t.Fatal(err)
			}
			unblock := block(h)
			h.now = h.now.Add(3 * time.Minute) // past start_timeout
			h.m.Tick(context.Background())
			if len(h.sd.stopped) != 0 {
				t.Fatalf("stopped without an answer: %v\n%s", h.sd.stopped, h.eventText())
			}
			unblock()
			h.m.Tick(context.Background())
			if !reflect.DeepEqual(h.sd.stopped, []string{"ghr-runner-aaaaaa"}) ||
				!strings.Contains(h.eventText(), "did not come online within 2m0s") {
				t.Fatalf("stopped %v\n%s", h.sd.stopped, h.eventText())
			}
		})
	}
}

func idleOnline(t *testing.T, h *harness, repo string) {
	t.Helper()
	if err := h.m.spawn(context.Background(), h.cfg, repo); err != nil {
		t.Fatal(err)
	}
	h.gh.setRunner(101, "online", false)
	h.m.refreshRunners(context.Background())
	if h.state("aaaaaa") != "idle" {
		t.Fatalf("state %s", h.state("aaaaaa"))
	}
}

// GitHub answers 404 for the runner and its list of the repository's runners
// does not hold it: the registration is gone, the runner can take no job, and
// its unit is stopped at once.
func TestIdleRunnerWhoseRegistrationIsGoneIsStopped(t *testing.T) {
	h := newHarness(t)
	idleOnline(t, h, "darkmem")
	h.gh.mu.Lock()
	delete(h.gh.runners, 101)
	h.gh.mu.Unlock()
	h.m.Tick(context.Background())
	if !reflect.DeepEqual(h.sd.stopped, []string{"ghr-runner-aaaaaa"}) ||
		!strings.Contains(h.eventText(), "warn darkmem runner aaaaaa registration is gone; stopped") {
		t.Fatalf("stopped %v\n%s", h.sd.stopped, h.eventText())
	}
	if n := sched.Covering(h.m.schedInstances(h.cfg), "darkmem", h.now); n != 0 {
		t.Fatalf("a runner without a registration still covers %d jobs", n)
	}
	h.now = h.now.Add(10 * time.Second)
	h.m.Tick(context.Background())
	h.m.Wait()
	if h.state("aaaaaa") != "gone" {
		t.Fatalf("its slot is still held: state %s", h.state("aaaaaa"))
	}
}

// A 404 also means "the token cannot see this repository". When the list
// cannot be read either, nothing is concluded and the runner keeps running.
func TestIdleRunnerIsKeptWhenTheRunnerListCannotBeRead(t *testing.T) {
	h := newHarness(t)
	idleOnline(t, h, "darkmem")
	h.gh.mu.Lock()
	delete(h.gh.runners, 101)
	h.gh.mu.Unlock()
	h.gh.setErr("ListRunners darkmem", &github.APIError{Status: 404, Kind: github.ErrNotFound})
	h.now = h.now.Add(6 * time.Minute) // past idle_timeout, so the idle stop asks too
	h.m.Tick(context.Background())
	if len(h.sd.stopped) != 0 || h.state("aaaaaa") != "idle" {
		t.Fatalf("stopped %v state %s", h.sd.stopped, h.state("aaaaaa"))
	}
	if e := h.m.Status().Repos[1].Error; !strings.Contains(e, "token lacks access") {
		t.Fatalf("repository error %q", e)
	}
	if n := strings.Count(h.gh.callsText(), "ListRunners darkmem"); n != 1 {
		t.Fatalf("the runner list was read %d times in one tick", n)
	}
}

// The list answers and still holds the runner: the 404 proved nothing.
func TestIdleRunnerStillListedIsKept(t *testing.T) {
	h := newHarness(t)
	idleOnline(t, h, "darkmem")
	h.gh.mu.Lock()
	delete(h.gh.runners, 101)
	h.gh.mu.Unlock()
	h.gh.listed["darkmem"] = []github.Runner{{ID: 101, Name: "ghr-darkmem-aaaaaa", Status: "online"}}
	h.now = h.now.Add(6 * time.Minute)
	h.m.Tick(context.Background())
	if len(h.sd.stopped) != 0 || h.state("aaaaaa") != "idle" || len(h.gh.deleted) != 0 {
		t.Fatalf("stopped %v deleted %v state %s", h.sd.stopped, h.gh.deleted, h.state("aaaaaa"))
	}
}

// A list refused for the whole token ends the runner checks of this tick.
func TestRunnerChecksStopWhenTheRunnerListRejectsTheToken(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkcloud") // aaaaaa, runner 101
	h.m.spawn(context.Background(), h.cfg, "darkmem")   // bbbbbb, runner 102
	h.gh.mu.Lock()
	delete(h.gh.runners, 101)
	h.gh.mu.Unlock()
	h.gh.setErr("ListRunners darkcloud", &github.APIError{Status: 401, Kind: github.ErrAuth})
	h.m.refreshRunners(context.Background())
	if !h.m.Status().Degraded || strings.Contains(h.gh.callsText(), "GetRunner darkmem") {
		t.Fatalf("degraded %v, calls %s", h.m.Status().Degraded, h.gh.callsText())
	}
}

// A runner whose registration is gone needs no GitHub call to be stopped, so
// an API pause must not keep it running.
func TestGoneRunnerIsStoppedDuringAnAPIPause(t *testing.T) {
	h := newHarness(t)
	idleOnline(t, h, "darkmem")
	h.gh.mu.Lock()
	delete(h.gh.runners, 101)
	h.gh.mu.Unlock()
	h.gh.setErr("ListRuns darkcloud", &github.APIError{Status: 403, Kind: github.ErrRateLimit, RetryAt: h.now.Add(time.Hour)})
	h.sd.stopErr["ghr-runner-aaaaaa"] = errors.New("unit busy")
	h.m.Tick(context.Background()) // confirmed gone, then the poll is rate-limited; the stop fails
	if h.m.apiAllowed(h.now) || len(h.sd.stopped) != 0 {
		t.Fatalf("API allowed %v, stopped %v", h.m.apiAllowed(h.now), h.sd.stopped)
	}
	delete(h.sd.stopErr, "ghr-runner-aaaaaa")
	h.now = h.now.Add(10 * time.Second)
	h.m.Tick(context.Background()) // still paused: the stop is retried without an API call
	if !reflect.DeepEqual(h.sd.stopped, []string{"ghr-runner-aaaaaa"}) {
		t.Fatalf("stopped %v\n%s", h.sd.stopped, h.eventText())
	}
}
