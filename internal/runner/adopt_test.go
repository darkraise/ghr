package runner

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"

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
	if err := writeJSON(filepath.Join(dir, MetaFile), Meta{ID: id, Repo: repo, RunnerID: runnerID, RunnerName: "ghr-" + repo + "-" + id,
		Labels: []string{"homelab"}, SpawnedAt: h.now}); err != nil {
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
	os.MkdirAll(h.m.instanceDir("cccccc"), 0o755)      // no ghr.json, unit active
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

// An instance an older ghr spawned has only the copy in its own directory.
// It is adopted from that copy once, and recorded.
func TestAdoptRecordsAnOlderInstancesMetadataOnce(t *testing.T) {
	h := newHarness(t)
	makeInstance(t, h, "aaaaaa", "darkmem", 1, false)
	h.sd.active["ghr-runner-aaaaaa"] = true
	if err := h.m.Adopt(context.Background()); err != nil {
		t.Fatal(err)
	}
	var rec Meta
	if err := readJSON(filepath.Join(h.m.Paths.Private, "aaaaaa.json"), &rec); err != nil || rec.Repo != "darkmem" || rec.RunnerID != 1 {
		t.Fatalf("recorded %+v: %v", rec, err)
	}
	forged := Meta{ID: "aaaaaa", Repo: "darkcloud", RunnerID: 999, RunnerName: "ghr-darkcloud-aaaaaa"}
	if err := writeJSON(filepath.Join(h.m.instanceDir("aaaaaa"), MetaFile), forged); err != nil {
		t.Fatal(err)
	}
	h.restart(t)
	if err := h.m.Adopt(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := adoptedMeta(h, "aaaaaa"); got.Repo != "darkmem" || got.RunnerID != 1 {
		t.Fatalf("adopted %+v", got)
	}
}

// When the record cannot be written, adopting from the instance's own copy
// would mean trusting that copy again at every later start. Adoption fails
// instead, and nothing is stopped or removed.
func TestAdoptFailsRatherThanTrustAnUnrecordedCopy(t *testing.T) {
	h := newHarness(t)
	makeInstance(t, h, "aaaaaa", "darkmem", 1, false)
	h.sd.active["ghr-runner-aaaaaa"] = true
	orig := writeFile
	t.Cleanup(func() { writeFile = orig })
	writeFile = func(name string, data []byte, perm os.FileMode) error {
		if strings.HasPrefix(name, h.m.Paths.Private) {
			return &fs.PathError{Op: "write", Path: name, Err: syscall.EIO}
		}
		return orig(name, data, perm)
	}
	if err := h.m.Adopt(context.Background()); err == nil {
		t.Fatal("adopted an instance whose metadata could not be recorded")
	}
	if len(h.sd.stopped) != 0 {
		t.Fatalf("stopped %v", h.sd.stopped)
	}
	if _, err := os.Stat(h.m.instanceDir("aaaaaa")); err != nil {
		t.Fatalf("instance dir removed: %v", err)
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
