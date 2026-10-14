package runner

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/sched"
)

func makeInstance(t *testing.T, h *harness, id, repo string, runnerID int64, withJob bool) {
	t.Helper()
	dir := h.m.instanceDir(id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	meta := Meta{ID: id, Repo: repo, RunnerID: runnerID, RunnerName: "ghr-" + repo + "-" + id, Labels: []string{"homelab"}, SpawnedAt: h.now}
	if err := h.m.writePrivateMeta(meta); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(filepath.Join(dir, MetaFile), meta); err != nil {
		t.Fatal(err)
	}
	if withJob {
		h.writeJob(t, id, 77)
	}
}

func TestAdopt(t *testing.T) {
	h := newHarness(t)
	makeInstance(t, h, "aaaaaa", "darkcloud", 1, true) // running, mid-job
	makeInstance(t, h, "bbbbbb", "darkmem", 2, false)  // exited while daemon was down
	os.MkdirAll(h.m.instanceDir("cccccc"), 0o755)      // no metadata record, unit active
	h.sd.active["ghr-runner-aaaaaa"] = true
	h.sd.active["ghr-runner-cccccc"] = true
	h.sd.active["ghr-runner-dddddd"] = true // unit without a dir

	if err := h.m.Adopt(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.m.Wait()

	if h.state("aaaaaa") != "busy" {
		t.Fatalf("aaaaaa %s", h.state("aaaaaa"))
	}
	if h.state("bbbbbb") != "gone" || !reflect.DeepEqual(h.gh.deleted, []int64{2}) {
		t.Fatalf("bbbbbb %s deleted %v", h.state("bbbbbb"), h.gh.deleted)
	}
	if _, err := os.Stat(h.m.instanceDir("cccccc")); !os.IsNotExist(err) {
		t.Fatal("cccccc dir kept")
	}
	sort.Strings(h.sd.stopped)
	if !reflect.DeepEqual(h.sd.stopped, []string{"ghr-runner-cccccc", "ghr-runner-dddddd"}) {
		t.Fatalf("stopped %v", h.sd.stopped)
	}
}

// A unit with unreadable metadata whose stop fails keeps its dir; reconciliation
// stops it later and only then deletes its registration and dir.
func TestAdoptKeepsDirWhenStopFails(t *testing.T) {
	h := newHarness(t)
	os.MkdirAll(h.m.instanceDir("cccccc"), 0o755)
	h.sd.active["ghr-runner-cccccc"] = true
	h.sd.stopErr["ghr-runner-cccccc"] = errors.New("job still shutting down")
	h.gh.listed["darkmem"] = []github.Runner{{ID: 9, Name: "ghr-darkmem-cccccc", Status: "offline"}}
	if err := h.m.Adopt(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.m.Reconcile(context.Background(), h.cfg)
	h.m.Reconcile(context.Background(), h.cfg) // the second sighting that would delete an unguarded registration
	if _, err := os.Stat(h.m.instanceDir("cccccc")); err != nil {
		t.Fatal("dir removed while its unit may still run")
	}
	if len(h.gh.deleted) != 0 {
		t.Fatalf("registration deleted under a running unit: %v", h.gh.deleted)
	}
	delete(h.sd.stopErr, "ghr-runner-cccccc")
	h.m.Reconcile(context.Background(), h.cfg)
	if _, err := os.Stat(h.m.instanceDir("cccccc")); !os.IsNotExist(err) {
		t.Fatal("dir kept after the unit stopped")
	}
	if !reflect.DeepEqual(h.gh.deleted, []int64{9}) {
		t.Fatalf("deleted %v", h.gh.deleted)
	}
}

// start_timeout counts from the spawn: a restart does not give a stuck runner more time.
func TestAdoptedStartingKeepsSpawnDeadline(t *testing.T) {
	h := newHarness(t)
	makeInstance(t, h, "aaaaaa", "darkmem", 1, false)
	h.sd.active["ghr-runner-aaaaaa"] = true
	h.now = h.now.Add(3 * time.Minute) // spawned 3m ago; start_timeout is 2m
	if err := h.m.Adopt(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := sched.StartTimedOut(h.cfg, h.m.schedInstances(h.cfg), h.now); !reflect.DeepEqual(got, []string{"aaaaaa"}) {
		t.Fatalf("timed out %v", got)
	}
}

func TestReconcileDeletesStaleRegistrationsAndDirs(t *testing.T) {
	h := newHarness(t)
	makeInstance(t, h, "aaaaaa", "darkcloud", 1, false)
	h.m.mu.Lock()
	h.m.insts["aaaaaa"] = &instance{Meta: Meta{ID: "aaaaaa", Repo: "darkcloud", RunnerID: 1, RunnerName: "ghr-darkcloud-aaaaaa"}, State: "idle"}
	h.m.mu.Unlock()
	h.sd.active["ghr-runner-aaaaaa"] = true
	os.MkdirAll(h.m.instanceDir("eeeeee"), 0o755) // orphan dir, no unit
	h.gh.listed["darkcloud"] = []github.Runner{
		{ID: 1, Name: "ghr-darkcloud-aaaaaa"},                    // ours, live
		{ID: 5, Name: "ghr-darkcloud-bbbbbb", Status: "offline"}, // stale
		{ID: 6, Name: "homelab-darkcloud"},                       // not ghr's
		{ID: 7, Name: "ghr-darkcloud-x-123456"},                  // another repo's naming, not 6 hex
	}
	// Same ID as darkcloud's live instance, but a different repo: stale.
	h.gh.listed["darkmem"] = []github.Runner{{ID: 8, Name: "ghr-darkmem-aaaaaa", Status: "offline"}}
	h.m.Reconcile(context.Background(), h.cfg)
	h.m.Reconcile(context.Background(), h.cfg)
	sort.Slice(h.gh.deleted, func(a, b int) bool { return h.gh.deleted[a] < h.gh.deleted[b] })
	if !reflect.DeepEqual(h.gh.deleted, []int64{5, 8}) {
		t.Fatalf("deleted %v", h.gh.deleted)
	}
	if _, err := os.Stat(h.m.instanceDir("eeeeee")); !os.IsNotExist(err) {
		t.Fatal("orphan dir kept")
	}
	if _, err := os.Stat(h.m.instanceDir("aaaaaa")); err != nil {
		t.Fatal("live dir removed")
	}
}

// An active unit no instance tracks is stopped before its registration is deleted.
func TestReconcileStopsUntrackedUnitBeforeDeletingRegistration(t *testing.T) {
	h := newHarness(t)
	h.sd.active["ghr-runner-ffffff"] = true
	h.gh.listed["darkmem"] = []github.Runner{{ID: 3, Name: "ghr-darkmem-ffffff", Status: "offline"}}
	h.m.Reconcile(context.Background(), h.cfg)
	h.m.Reconcile(context.Background(), h.cfg)
	if !reflect.DeepEqual(h.sd.stopped, []string{"ghr-runner-ffffff"}) || !reflect.DeepEqual(h.gh.deleted, []int64{3}) {
		t.Fatalf("stopped %v deleted %v", h.sd.stopped, h.gh.deleted)
	}
}

// A registration that is gone by the time it is deleted is removed all the same.
func TestReconcileTakesA404FromTheDeleteAsRemoved(t *testing.T) {
	h := newHarness(t)
	h.gh.listed["darkmem"] = []github.Runner{{ID: 3, Name: "ghr-darkmem-ffffff", Status: "offline"}}
	h.gh.setErr("DeleteRunner darkmem", &github.APIError{Status: 404, Kind: github.ErrNotFound})
	h.m.Reconcile(context.Background(), h.cfg)
	h.m.Reconcile(context.Background(), h.cfg)
	if ev := h.eventText(); !strings.Contains(ev, "info darkmem removed stale registration ghr-darkmem-ffffff") || strings.Contains(ev, "warn") {
		t.Fatalf("events:\n%s", ev)
	}
}

// A registration finish could not delete is retried by reconciliation even
// once its repository is no longer served, which no runner list then covers.
func TestReconcileRetriesARegistrationFinishCouldNotDelete(t *testing.T) {
	h := newHarness(t)
	if err := h.m.spawn(context.Background(), h.cfg, "darkmem"); err != nil {
		t.Fatal(err)
	}
	h.gh.setErr("DeleteRunner darkmem", &github.APIError{Status: 502, Kind: github.ErrServer})
	h.sd.active["ghr-runner-aaaaaa"] = false
	h.m.refreshUnits(context.Background())
	h.m.Wait()
	if h.state("aaaaaa") != "gone" || len(h.gh.deleted) != 0 {
		t.Fatalf("state %s deleted %v", h.state("aaaaaa"), h.gh.deleted)
	}
	h.gh.setErr("DeleteRunner darkmem", nil)
	cfg := h.cfg.Clone()
	cfg.Repos = slices.DeleteFunc(cfg.Repos, func(r config.Repo) bool { return r.Name == "darkmem" })
	h.cfg = cfg
	h.m.Reconcile(context.Background(), h.cfg)
	if !reflect.DeepEqual(h.gh.deleted, []int64{101}) {
		t.Fatalf("deleted %v\n%s", h.gh.deleted, h.eventText())
	}
	h.m.Reconcile(context.Background(), h.cfg)
	if !reflect.DeepEqual(h.gh.deleted, []int64{101}) {
		t.Fatalf("deleted again: %v", h.gh.deleted)
	}
}

func addUndeleted(h *harness, id int64, repo string) {
	h.m.mu.Lock()
	defer h.m.mu.Unlock()
	h.m.undeleted[id] = registration{repo: repo, name: fmt.Sprintf("ghr-%s-%06x", repo, id)}
}

func undeletedCount(h *harness) int {
	h.m.mu.Lock()
	defer h.m.mu.Unlock()
	return len(h.m.undeleted)
}

// Retrying a registration is a GitHub call like any other, so it waits out a
// rate limit pause.
func TestReconcileRetriesRegistrationsOnlyWhileTheAPIIsAllowed(t *testing.T) {
	h := newHarness(t)
	addUndeleted(h, 101, "darkmem")
	h.m.mu.Lock()
	h.m.pauseUntil = h.now.Add(time.Minute)
	h.m.mu.Unlock()
	h.m.Reconcile(context.Background(), h.cfg)
	if strings.Contains(h.gh.callsText(), "DeleteRunner") || undeletedCount(h) != 1 {
		t.Fatalf("calls %s, %d kept", h.gh.callsText(), undeletedCount(h))
	}
	h.now = h.now.Add(time.Minute)
	h.m.Reconcile(context.Background(), h.cfg)
	if !reflect.DeepEqual(h.gh.deleted, []int64{101}) || undeletedCount(h) != 0 {
		t.Fatalf("deleted %v, %d kept", h.gh.deleted, undeletedCount(h))
	}
}

// A rate limit met by one retry pauses the rest, like it does anywhere else.
func TestReconcileRateLimitedRetryPausesTheAPI(t *testing.T) {
	h := newHarness(t)
	addUndeleted(h, 101, "darkmem")
	addUndeleted(h, 102, "darkmem")
	until := h.now.Add(time.Minute)
	h.gh.setErr("DeleteRunner darkmem", &github.APIError{Status: 429, Kind: github.ErrRateLimit, RetryAt: until})
	h.m.Reconcile(context.Background(), h.cfg)
	if got := strings.Count(h.gh.callsText(), "DeleteRunner"); got != 1 {
		t.Fatalf("%d delete calls after a rate limit\n%s", got, h.eventText())
	}
	if !h.m.PausedUntil().Equal(until) || undeletedCount(h) != 2 {
		t.Fatalf("paused until %v, %d kept", h.m.PausedUntil(), undeletedCount(h))
	}
}

// A 403 for a repository ghr no longer serves is reported once and given up:
// nothing in ghr will ever make that token allowed to delete it.
func TestReconcileGivesUpOnARemovedRepositoryTheTokenCannotReach(t *testing.T) {
	h := newHarness(t)
	addUndeleted(h, 101, "gone-repo")
	addUndeleted(h, 102, "darkmem")
	forbidden := &github.APIError{Status: 403, Kind: github.ErrAuth, Message: "Resource not accessible by personal access token"}
	h.gh.setErr("DeleteRunner gone-repo", forbidden)
	h.gh.setErr("DeleteRunner darkmem", forbidden)
	h.m.Reconcile(context.Background(), h.cfg)
	h.m.Reconcile(context.Background(), h.cfg)
	if got := strings.Count(h.gh.callsText(), "DeleteRunner gone-repo"); got != 1 {
		t.Fatalf("%d delete calls for the removed repository\n%s", got, h.eventText())
	}
	if got := strings.Count(h.eventText(), "ghr-gone-repo-"); got != 1 {
		t.Fatalf("%d events for the removed repository\n%s", got, h.eventText())
	}
	// A served repository keeps its registration and shows the permission error.
	if got := strings.Count(h.gh.callsText(), "DeleteRunner darkmem"); got != 2 || undeletedCount(h) != 1 {
		t.Fatalf("%d delete calls for darkmem, %d kept", got, undeletedCount(h))
	}
	if !strings.Contains(h.m.Status().Repos[1].Error, "token lacks permission for this repository") {
		t.Fatalf("status %q", h.m.Status().Repos[1].Error)
	}
}

func TestReconcileReportsListRunnersFailure(t *testing.T) {
	h := newHarness(t)
	h.gh.setErr("ListRunners darkmem", errors.New("boom"))
	h.m.Reconcile(context.Background(), h.cfg)
	if !strings.Contains(h.eventText(), "warn darkmem reconcile: list runners: boom") {
		t.Fatalf("events:\n%s", h.eventText())
	}
}

// A daemon that crashed after appending history but before removing the pending
// record and the instance dir finishes the job again without a duplicate line.
func TestFinishAfterCrashAppendsHistoryOnce(t *testing.T) {
	h := newHarness(t)
	makeInstance(t, h, "aaaaaa", "darkmem", 1, true)
	entry := model.HistoryEntry{ID: "aaaaaa", Repo: "darkmem", RunID: 77, Conclusion: "success", FinishedAt: h.now}
	os.MkdirAll(h.m.Paths.Pending, 0o755)
	writeJSON(h.m.pendingPath("aaaaaa"), pending{Entry: entry, RunnerName: "ghr-darkmem-aaaaaa", Deadline: h.now.Add(time.Hour)})
	h.m.History.Append(entry)
	h.gh.setJobs(77, completedJob(h, 77, "ghr-darkmem-aaaaaa", "success"))
	if err := h.m.Adopt(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.m.Wait()
	if h.state("aaaaaa") != "gone" || !reflect.DeepEqual(h.history(t), []string{"aaaaaa success"}) {
		t.Fatalf("state %s history %v", h.state("aaaaaa"), h.history(t))
	}
}

// An adopted runner starts as "starting" from its spawn time. When the first
// check after the restart fails, an online runner must survive until a check
// succeeds.
func TestAdoptedOnlineRunnerSurvivesAFailedFirstCheck(t *testing.T) {
	h := newHarness(t)
	makeInstance(t, h, "aaaaaa", "darkmem", 1, false)
	h.sd.active["ghr-runner-aaaaaa"] = true
	h.gh.setRunner(1, "online", false)
	h.now = h.now.Add(3 * time.Minute) // spawned 3m ago; start_timeout is 2m
	if err := h.m.Adopt(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.gh.setErr("GetRunner darkmem", &github.APIError{Status: 502, Kind: github.ErrServer})
	h.m.Tick(context.Background())
	if len(h.sd.stopped) != 0 || h.state("aaaaaa") != "starting" {
		t.Fatalf("stopped %v state %s", h.sd.stopped, h.state("aaaaaa"))
	}
	h.gh.setErr("GetRunner darkmem", nil)
	h.m.Tick(context.Background())
	if len(h.sd.stopped) != 0 || h.state("aaaaaa") != "idle" {
		t.Fatalf("stopped %v state %s", h.sd.stopped, h.state("aaaaaa"))
	}
}

// A second ghr host on the same repository names its runners the same way.
// Reconcile deletes only a registration it has seen offline twice in a row.
func TestReconcileLeavesAnotherHostsRunners(t *testing.T) {
	h := newHarness(t)
	h.gh.listed["darkmem"] = []github.Runner{
		{ID: 21, Name: "ghr-darkmem-111111", Status: "online"},  // the other host's idle runner
		{ID: 22, Name: "ghr-darkmem-222222", Status: "offline"}, // the other host's runner, just starting
		{ID: 23, Name: "ghr-darkmem-333333", Status: "offline"}, // a leftover
		{ID: 24, Name: "ghr-darkmem-444444"},                    // no status given: not known to be offline
	}
	h.m.Reconcile(context.Background(), h.cfg)
	if len(h.gh.deleted) != 0 {
		t.Fatalf("deleted on first sight: %v", h.gh.deleted)
	}
	h.gh.listed["darkmem"][1].Status = "online" // it came online meanwhile
	h.m.Reconcile(context.Background(), h.cfg)
	if !reflect.DeepEqual(h.gh.deleted, []int64{23}) {
		t.Fatalf("deleted %v, want only the leftover", h.gh.deleted)
	}
}

// GitHub repository names are case-insensitive, so a registration named in
// another case than the config's is still one of ours.
func TestReconcileSweepsARegistrationNamedInAnotherCase(t *testing.T) {
	h := newHarness(t)
	h.cfg.Repos[1].Name = "MyRepo"
	h.gh.listed["MyRepo"] = []github.Runner{{ID: 31, Name: "ghr-myrepo-abc123", Status: "offline"}}
	h.m.Reconcile(context.Background(), h.cfg)
	h.m.Reconcile(context.Background(), h.cfg)
	if !reflect.DeepEqual(h.gh.deleted, []int64{31}) {
		t.Fatalf("deleted %v\n%s", h.gh.deleted, h.eventText())
	}
}

func adoptedMeta(h *harness, id string) Meta {
	h.m.mu.Lock()
	defer h.m.mu.Unlock()
	return h.m.insts[id].Meta
}

// A job can replace ghr.json in its own directory. After a restart the
// repository and runner ID must still be the ones root recorded at spawn:
// they choose whose containers cleanup removes and which registration is deleted.
func TestAdoptTrustsTheRootOwnedRecordOverTheInstanceCopy(t *testing.T) {
	h := newHarness(t)
	if err := h.m.spawn(context.Background(), h.cfg, "darkmem"); err != nil {
		t.Fatal(err)
	}
	forged := Meta{ID: "aaaaaa", Repo: "darkcloud", RunnerID: 999, RunnerName: "ghr-darkcloud-aaaaaa"}
	if err := writeJSON(filepath.Join(h.m.instanceDir("aaaaaa"), MetaFile), forged); err != nil {
		t.Fatal(err)
	}
	h.restart(t)
	if err := h.m.Adopt(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := adoptedMeta(h, "aaaaaa"); got.Repo != "darkmem" || got.RunnerID != 101 {
		t.Fatalf("adopted %+v", got)
	}
}

// An instance an older ghr spawned has only the copy in its own directory,
// which its job can rewrite. Nothing trusts that copy: the instance is
// stopped and removed, and no registration is deleted on its word.
func TestAdoptStopsAnInstanceWithoutARecord(t *testing.T) {
	h := newHarness(t)
	makeInstance(t, h, "aaaaaa", "darkmem", 1, true)
	os.Remove(filepath.Join(h.m.Paths.Private, "aaaaaa.json"))
	h.sd.active["ghr-runner-aaaaaa"] = true
	if err := h.m.Adopt(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.m.Wait()
	if !reflect.DeepEqual(h.sd.stopped, []string{"ghr-runner-aaaaaa"}) || h.state("aaaaaa") != "gone" {
		t.Fatalf("stopped %v, state %s\n%s", h.sd.stopped, h.state("aaaaaa"), h.eventText())
	}
	if _, err := os.Stat(h.m.instanceDir("aaaaaa")); !os.IsNotExist(err) {
		t.Fatalf("instance dir kept: %v", err)
	}
	if _, err := os.Stat(filepath.Join(h.m.Paths.Private, "aaaaaa.json")); !os.IsNotExist(err) {
		t.Fatalf("the directory's copy was recorded: %v", err)
	}
	if len(h.gh.deleted) != 0 || len(h.history(t)) != 0 {
		t.Fatalf("deleted %v, history %v", h.gh.deleted, h.history(t))
	}
}

func TestPrivateRecordsGoWithTheirInstance(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkmem")
	if _, err := os.Stat(filepath.Join(h.m.Paths.Private, "aaaaaa.json")); err != nil {
		t.Fatalf("spawn recorded nothing: %v", err)
	}
	h.sd.active["ghr-runner-aaaaaa"] = false
	h.m.refreshUnits(context.Background())
	h.m.Wait()
	// Records of an instance whose directory is gone are swept at the next start.
	os.WriteFile(filepath.Join(h.m.Paths.Private, "eeeeee.json"), []byte("{}"), 0o600)
	os.WriteFile(filepath.Join(h.m.Paths.Private, "eeeeee.projects"), []byte("x\n"), 0o600)
	if err := h.m.Adopt(context.Background()); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(h.m.Paths.Private); len(entries) != 0 {
		t.Fatalf("left behind: %v", entries)
	}
}

// If the instance directory cannot be removed after a failed spawn, it stays
// runner-writable. Its private record stays too, and is what the next start
// reads, not the directory's copy.
func TestFailedSpawnKeepsTheRecordWhileTheDirStays(t *testing.T) {
	h := newHarness(t)
	origWrite, origRemove := writeFile, removeAll
	t.Cleanup(func() { writeFile, removeAll = origWrite, origRemove })
	copyPath := filepath.Join(h.m.instanceDir("aaaaaa"), MetaFile)
	writeFile = func(name string, data []byte, perm os.FileMode) error {
		if name == copyPath {
			return &fs.PathError{Op: "write", Path: name, Err: syscall.EIO}
		}
		return origWrite(name, data, perm)
	}
	removeAll = func(path string) error {
		if strings.HasPrefix(path, h.m.Paths.Instances) {
			return syscall.EIO
		}
		return origRemove(path)
	}
	if err := h.m.spawn(context.Background(), h.cfg, "darkmem"); err == nil {
		t.Fatal("spawn succeeded although its metadata copy could not be written")
	}
	if _, err := os.Stat(filepath.Join(h.m.Paths.Private, "aaaaaa.json")); err != nil {
		t.Fatalf("private record removed while the dir stays: %v", err)
	}
	writeFile = origWrite
	forged := Meta{ID: "aaaaaa", Repo: "darkcloud", RunnerID: 999, RunnerName: "ghr-darkcloud-aaaaaa"}
	if err := writeJSON(copyPath, forged); err != nil {
		t.Fatal(err)
	}
	got, err := h.m.readMeta("aaaaaa")
	if err != nil {
		t.Fatal(err)
	}
	if got.Repo != "darkmem" || got.RunnerID != 101 {
		t.Fatalf("read %+v", got)
	}
}
