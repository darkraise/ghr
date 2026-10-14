package runner

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/sched"
	"github.com/darkraise/ghr/internal/system"
)

func TestCleanupDockerProjectsAndPrefixes(t *testing.T) {
	h := newHarness(t)
	dir := h.m.instanceDir("aaaaaa")
	h.docker.compose = []system.ComposeContainer{
		{ID: "c1", Project: "custom-p", WorkingDir: filepath.Join(dir, "_work", "darkcloud", "darkcloud")},
		{ID: "c2", Project: "elsewhere", WorkingDir: filepath.Join(h.root, "instances", "bbbbbb", "_work")},
	}
	h.docker.byLabel["com.docker.compose.project=ghr-aaaaaa"] = []string{"c0"}
	h.docker.byLabel["com.docker.compose.project=custom-p"] = []string{"c1"}
	h.docker.named = []system.NamedContainer{{ID: "c3", Name: "dc-e2e-web"}, {ID: "c4", Name: "unrelated"}}

	removed, err := h.m.cleanupDocker(context.Background(), h.cfg, "aaaaaa", "darkcloud")
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(h.docker.removed)
	if removed != 3 || !reflect.DeepEqual(h.docker.removed, []string{"c0", "c1", "c3"}) {
		t.Fatalf("removed %d %v", removed, h.docker.removed)
	}
	want := []string{"com.docker.compose.project=custom-p", "com.docker.compose.project=ghr-aaaaaa"}
	if !reflect.DeepEqual(h.docker.netLabels, want) || !reflect.DeepEqual(h.docker.volLabels, want) {
		t.Fatalf("networks %v volumes %v", h.docker.netLabels, h.docker.volLabels)
	}
}

// A .runner file in the shape the runner writes, and the label the runner puts
// on its service containers and job network: the first six hex digits of the
// sha256 of the lower-cased file (DockerInstanceLabel in actions/runner's
// DockerCommandManager.cs).
const (
	runnerFile  = `{"AgentId":"1","AgentName":"example-runner-abc123","DisableUpdate":"True","Ephemeral":"True","PoolId":"1","PoolName":null,"ServerUrl":"https://pipelines.actions.example/token/","WorkFolder":"_work","GitHubUrl":"https://github.example/example/repo","ServerUrlV2":"https://broker.actions.example/","UseV2Flow":"true"}`
	runnerLabel = "c6b47a"
)

func TestCleanupRemovesTheRunnersOwnContainersAndNetworks(t *testing.T) {
	h := newHarness(t)
	dir := h.m.instanceDir("abc123")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".runner"), []byte(runnerFile), 0o644); err != nil {
		t.Fatal(err)
	}
	h.docker.byLabel[runnerLabel] = []string{"svc1", "job1"}

	removed, err := h.m.cleanupDocker(context.Background(), h.cfg, "abc123", "darkcloud")
	if err != nil {
		t.Fatal(err)
	}
	if removed != 2 || !reflect.DeepEqual(h.docker.removed, []string{"svc1", "job1"}) {
		t.Fatalf("removed %d %v", removed, h.docker.removed)
	}
	want := []string{"com.docker.compose.project=ghr-abc123", runnerLabel}
	if !reflect.DeepEqual(h.docker.netLabels, want) {
		t.Fatalf("networks %v, want %v", h.docker.netLabels, want)
	}
}

func TestCleanupWithoutRunnerFileSkipsTheRunnerLabel(t *testing.T) {
	h := newHarness(t)
	if err := os.MkdirAll(h.m.instanceDir("abc123"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := h.m.cleanupDocker(context.Background(), h.cfg, "abc123", "darkcloud"); err != nil {
		t.Fatal(err)
	}
	if want := []string{"com.docker.compose.project=ghr-abc123"}; !reflect.DeepEqual(h.docker.netLabels, want) {
		t.Fatalf("networks %v, want %v", h.docker.netLabels, want)
	}
}

func TestCleanupReportsAnUnreadableRunnerFile(t *testing.T) {
	h := newHarness(t)
	if err := os.MkdirAll(filepath.Join(h.m.instanceDir("abc123"), ".runner"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := h.m.cleanupDocker(context.Background(), h.cfg, "abc123", "darkcloud"); err == nil || !strings.Contains(err.Error(), ".runner") {
		t.Fatalf("err %v", err)
	}
}

func TestCleanupSkipsPrefixesForOtherRepos(t *testing.T) {
	h := newHarness(t)
	h.docker.named = []system.NamedContainer{{ID: "c3", Name: "dc-e2e-web"}}
	if removed, err := h.m.cleanupDocker(context.Background(), h.cfg, "aaaaaa", "darkmem"); removed != 0 || err != nil {
		t.Fatalf("darkmem has no prefixes, removed %d err %v", removed, err)
	}
}

// Validation rejects a blank prefix; cleanup must still never treat one as "match all".
func TestCleanupIgnoresBlankPrefix(t *testing.T) {
	h := newHarness(t)
	h.cfg.Repos[0].CleanupNamePrefixes = []string{"", "  "}
	h.docker.named = []system.NamedContainer{{ID: "c4", Name: "another-jobs-db"}}
	if _, err := h.m.cleanupDocker(context.Background(), h.cfg, "aaaaaa", "darkcloud"); err != nil {
		t.Fatal(err)
	}
	if len(h.docker.removed) != 0 {
		t.Fatalf("unrelated containers removed: %v", h.docker.removed)
	}
}

func TestCleanupReportsEveryFailure(t *testing.T) {
	for _, method := range []string{"ComposeContainers", "ContainerIDsByLabel", "RemoveContainers", "RemoveNetworksByLabel", "RemoveVolumesByLabel", "Containers"} {
		h := newHarness(t)
		h.docker.named = []system.NamedContainer{{ID: "c3", Name: "dc-e2e-web"}}
		h.docker.setErr(method, errors.New(method+" broke"))
		if _, err := h.m.cleanupDocker(context.Background(), h.cfg, "aaaaaa", "darkcloud"); err == nil || !strings.Contains(err.Error(), method+" broke") {
			t.Errorf("%s: err %v", method, err)
		}
	}
}

func TestCleanupRetryRemembersCustomProjects(t *testing.T) {
	h := newHarness(t)
	dir := h.m.instanceDir("aaaaaa")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	h.docker.compose = []system.ComposeContainer{{ID: "c1", Project: "custom-p", WorkingDir: filepath.Join(dir, "_work", "r", "r")}}
	h.docker.setErr("RemoveNetworksByLabel", errors.New("network busy"))
	if _, err := h.m.cleanupDocker(context.Background(), h.cfg, "aaaaaa", "darkmem"); err == nil {
		t.Fatal("network failure not reported")
	}
	h.docker.compose = nil
	h.docker.setErr("RemoveNetworksByLabel", nil)
	if _, err := h.m.cleanupDocker(context.Background(), h.cfg, "aaaaaa", "darkmem"); err != nil {
		t.Fatal(err)
	}
	want := []string{"com.docker.compose.project=custom-p", "com.docker.compose.project=ghr-aaaaaa"}
	if !reflect.DeepEqual(h.docker.netLabels, want) {
		t.Fatalf("retry networks %v", h.docker.netLabels)
	}
}

// A rewrite of the remembered projects that dies partway leaves the previous
// list readable, so a later retry still reaches the projects it names.
func TestFailedProjectRecordKeepsThePreviousOne(t *testing.T) {
	h := newHarness(t)
	dir := h.m.instanceDir("aaaaaa")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	h.docker.compose = []system.ComposeContainer{{ID: "c1", Project: "custom-p", WorkingDir: filepath.Join(dir, "_work", "r", "r")}}
	h.docker.setErr("RemoveNetworksByLabel", errors.New("network busy"))
	if _, err := h.m.cleanupDocker(context.Background(), h.cfg, "aaaaaa", "darkmem"); err == nil {
		t.Fatal("network failure not reported")
	}
	h.docker.compose = nil
	orig := writeFile
	t.Cleanup(func() { writeFile = orig })
	writeFile = func(name string, data []byte, perm os.FileMode) error {
		if strings.HasPrefix(name, h.m.privateProjects("aaaaaa")) {
			orig(name, data[:len(data)/2], perm)
			return &fs.PathError{Op: "write", Path: name, Err: syscall.EIO}
		}
		return orig(name, data, perm)
	}
	if _, err := h.m.cleanupDocker(context.Background(), h.cfg, "aaaaaa", "darkmem"); err == nil || !strings.Contains(err.Error(), "record compose projects") {
		t.Fatalf("failed record not reported: %v", err)
	}
	if b, err := os.ReadFile(h.m.privateProjects("aaaaaa")); err != nil || !reflect.DeepEqual(strings.Fields(string(b)), []string{"custom-p", "ghr-aaaaaa"}) {
		t.Fatalf("remembered list %q, %v", b, err)
	}
}

func TestRunnerContainersListsOnlyInstanceProjects(t *testing.T) {
	h := newHarness(t)
	h.m.mu.Lock()
	h.m.insts["aaaaaa"] = &instance{Meta: Meta{ID: "aaaaaa", Repo: "darkmem"}, State: sched.Busy}
	h.m.mu.Unlock()
	dir := h.m.instanceDir("aaaaaa")
	h.docker.compose = []system.ComposeContainer{
		{ID: "c1", Project: "custom-p", WorkingDir: filepath.Join(dir, "_work", "r", "r")},
		{ID: "c2", Project: "other-job", WorkingDir: filepath.Join(h.root, "instances", "bbbbbb", "_work")},
	}
	h.docker.byProject["ghr-aaaaaa"] = []system.ProjectContainer{{ID: "c0", Name: "ghr-aaaaaa-web-1", Image: "nginx", State: "running"}}
	h.docker.byProject["custom-p"] = []system.ProjectContainer{{ID: "c1", Name: "custom-p-db-1", Image: "postgres", State: "running"}}
	h.docker.byProject["other-job"] = []system.ProjectContainer{{ID: "c2", Name: "other-job-db-1"}}
	got, err := h.m.RunnerContainers(context.Background(), "aaaaaa")
	if err != nil {
		t.Fatal(err)
	}
	want := []model.Container{
		{ID: "c1", Name: "custom-p-db-1", Image: "postgres", State: "running", Project: "custom-p"},
		{ID: "c0", Name: "ghr-aaaaaa-web-1", Image: "nginx", State: "running", Project: "ghr-aaaaaa"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
	var u ErrUnknownRunner
	if _, err := h.m.RunnerContainers(context.Background(), "zzzzzz"); !errors.As(err, &u) {
		t.Fatalf("unknown runner err %v", err)
	}
}

func TestCheckDiskBelowHighWaterDoesNothing(t *testing.T) {
	h := newHarness(t)
	h.docker.usage = []int{61}
	h.checkDisk(context.Background())
	if len(h.docker.prunes) != 0 || h.m.Status().DiskPct != 61 {
		t.Fatalf("prunes %v disk %d", h.docker.prunes, h.m.Status().DiskPct)
	}
}

func TestCheckDiskPrunesInOrder(t *testing.T) {
	h := newHarness(t)
	h.docker.usage = []int{85, 83, 70}
	h.checkDisk(context.Background())
	if !reflect.DeepEqual(h.docker.prunes, []string{"until=72h", "keep=20GB", "images"}) {
		t.Fatalf("prunes %v", h.docker.prunes)
	}
	if h.m.Status().DiskPct != 70 || !strings.Contains(h.eventText(), "disk 85% > high-water 80%") || !strings.Contains(h.eventText(), "now 70%") {
		t.Fatalf("disk %d events %s", h.m.Status().DiskPct, h.eventText())
	}
}

func TestCheckDiskSkipsKeepPruneWhenAgeFilterSuffices(t *testing.T) {
	h := newHarness(t)
	h.docker.usage = []int{85, 75}
	h.checkDisk(context.Background())
	if !reflect.DeepEqual(h.docker.prunes, []string{"until=72h", "images"}) {
		t.Fatalf("prunes %v", h.docker.prunes)
	}
}

func TestCheckDiskReportsFailuresAndKeepsLastMeasurement(t *testing.T) {
	h := newHarness(t)
	h.docker.usage = []int{85, 83, -1}
	h.docker.setErr("PruneBuildCacheTo", errors.New("unknown flag"))
	h.docker.setErr("PruneDanglingImages", errors.New("daemon busy"))
	h.checkDisk(context.Background())
	ev := h.eventText()
	for _, want := range []string{"prune build cache to 20GB failed: unknown flag", "prune dangling images failed: daemon busy",
		"disk usage after pruning failed", "now unknown"} {
		if !strings.Contains(ev, want) {
			t.Errorf("missing %q in %s", want, ev)
		}
	}
	if h.m.Status().DiskPct != 85 {
		t.Fatalf("a failed read must keep the last good value, got %d", h.m.Status().DiskPct)
	}
}

func TestPruneHistoryAndLogs(t *testing.T) {
	h := newHarness(t)
	old := h.now.Add(-40 * 24 * time.Hour)
	h.m.History.Append(model.HistoryEntry{ID: "old", Repo: "darkmem", FinishedAt: old})
	h.m.History.Append(model.HistoryEntry{ID: "new", Repo: "darkmem", FinishedAt: h.now})
	oldLog := filepath.Join(h.m.Paths.Logs, "aaaaaa")
	newLog := filepath.Join(h.m.Paths.Logs, "bbbbbb")
	os.MkdirAll(oldLog, 0o755)
	os.MkdirAll(newLog, 0o755)
	os.Chtimes(oldLog, old, old)
	h.m.prune(h.cfg, h.now)
	got, _ := h.m.History.Query("", "", time.Time{}, 0)
	if len(got) != 1 || got[0].ID != "new" {
		t.Fatalf("history %v", got)
	}
	if _, err := os.Stat(oldLog); !os.IsNotExist(err) {
		t.Fatal("old log dir kept")
	}
	if _, err := os.Stat(newLog); err != nil {
		t.Fatal("new log dir removed")
	}
}

// The runner creates _diag when it starts, and a long job can outlast the
// retention; its archive ages from the job's end, as its history entry does.
func TestArchivedLogsAgeFromTheJobsEnd(t *testing.T) {
	h := newHarness(t)
	h.cfg.HistoryRetention = config.Duration(24 * time.Hour)
	h.m.spawn(context.Background(), h.cfg, "darkmem")
	diag := filepath.Join(h.m.instanceDir("aaaaaa"), "_diag")
	os.WriteFile(filepath.Join(diag, "Worker_1.log"), []byte("log"), 0o644)
	started := h.now.Add(-72 * time.Hour)
	os.Chtimes(diag, started, started)
	h.sd.active["ghr-runner-aaaaaa"] = false
	h.m.refreshUnits(context.Background())
	h.m.Wait()
	archive := filepath.Join(h.m.Paths.Logs, "aaaaaa")
	h.m.prune(h.cfg, h.now)
	if _, err := os.Stat(archive); err != nil {
		t.Fatalf("archive of a job that just ended was pruned: %v", err)
	}
	h.now = h.now.Add(25 * time.Hour)
	h.m.prune(h.cfg, h.now)
	if _, err := os.Stat(archive); !os.IsNotExist(err) {
		t.Fatalf("archive kept past the retention: %v", err)
	}
}

func TestPruneReportsLogArchiveFailure(t *testing.T) {
	// Windows reports a file used as a directory as "not found", which prune rightly treats as an absent archive.
	if runtime.GOOS == "windows" {
		t.Skip("a file in place of the archive dir is a not-found error on Windows")
	}
	h := newHarness(t)
	os.RemoveAll(h.m.Paths.Logs)
	h.m.prune(h.cfg, h.now)
	if strings.Contains(h.eventText(), "log archive prune") {
		t.Fatalf("missing archive dir reported: %s", h.eventText())
	}
	if err := os.WriteFile(h.m.Paths.Logs, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.m.prune(h.cfg, h.now)
	if !strings.Contains(h.eventText(), "log archive prune") {
		t.Fatalf("unreadable archive dir not reported: %s", h.eventText())
	}
}

func appendFile(t *testing.T, path, s string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	f.WriteString(s)
}

// A chunk boundary or a half-flushed write must not split a UTF-8 character.
func TestRunnerLogKeepsCharactersWhole(t *testing.T) {
	h := newHarness(t)
	diag := filepath.Join(h.m.instanceDir("aaaaaa"), "_diag")
	os.MkdirAll(diag, 0o755)
	runnerLog := filepath.Join(diag, "Runner_1.log")
	appendFile(t, runnerLog, "ok \xe2\x9c")
	c, err := h.m.RunnerLog("aaaaaa", "")
	if err != nil {
		t.Fatal(err)
	}
	if c.Data != "==> Runner_1.log <==\nok " {
		t.Fatalf("half-flushed chunk %q", c.Data)
	}
	appendFile(t, runnerLog, "\x93\n")
	if c, _ = h.m.RunnerLog("aaaaaa", c.Next); c.Data != "✓\n" {
		t.Fatalf("completed chunk %q", c.Data)
	}
	pad := strings.Repeat("a", maxLogChunk-1)
	os.WriteFile(filepath.Join(diag, "Worker_2.log"), []byte(pad+"✓"), 0o644)
	if c, _ = h.m.RunnerLog("aaaaaa", c.Next); c.Data != "\n==> Worker_2.log <==\n"+pad {
		t.Fatalf("boundary chunk has %d bytes", len(c.Data))
	}
	if c, _ = h.m.RunnerLog("aaaaaa", c.Next); c.Data != "✓" {
		t.Fatalf("after boundary %q", c.Data)
	}
}

// Both logs grow between polls and the logs are archived mid-follow:
// every byte arrives exactly once.
func TestRunnerLogCursorFollowsGrowingFiles(t *testing.T) {
	h := newHarness(t)
	diag := filepath.Join(h.m.instanceDir("aaaaaa"), "_diag")
	os.MkdirAll(diag, 0o755)
	runnerLog := filepath.Join(diag, "Runner_1.log")
	workerLog := filepath.Join(diag, "Worker_2.log")
	appendFile(t, runnerLog, "r1\n")
	appendFile(t, workerLog, "w1\n")
	var got strings.Builder
	c, err := h.m.RunnerLog("aaaaaa", "")
	if err != nil {
		t.Fatal(err)
	}
	if c.Data != "==> Runner_1.log <==\nr1\n\n==> Worker_2.log <==\nw1\n" {
		t.Fatalf("first chunk %q", c.Data)
	}
	got.WriteString(c.Data)
	appendFile(t, runnerLog, "r2\n")
	appendFile(t, workerLog, "w2\n")
	c, _ = h.m.RunnerLog("aaaaaa", c.Next)
	got.WriteString(c.Data)
	appendFile(t, workerLog, "w3\n")
	os.MkdirAll(h.m.Paths.Logs, 0o755)
	if err := os.Rename(diag, filepath.Join(h.m.Paths.Logs, "aaaaaa")); err != nil {
		t.Fatal(err)
	}
	c, err = h.m.RunnerLog("aaaaaa", c.Next)
	if err != nil {
		t.Fatal(err)
	}
	got.WriteString(c.Data)
	if got.String() != "==> Runner_1.log <==\nr1\n\n==> Worker_2.log <==\nw1\n"+
		"\n==> Runner_1.log <==\nr2\n\n==> Worker_2.log <==\nw2\nw3\n" {
		t.Fatalf("stream %q", got.String())
	}
	if c, _ = h.m.RunnerLog("aaaaaa", c.Next); c.Data != "" {
		t.Fatalf("nothing new, got %q", c.Data)
	}
	if _, err := h.m.RunnerLog("../etc", ""); err == nil {
		t.Fatal("path traversal accepted")
	}
}

func TestRunnerLogKeepsOffsetsOfUnlistedFiles(t *testing.T) {
	h := newHarness(t)
	diag := filepath.Join(h.m.instanceDir("aaaaaa"), "_diag")
	os.MkdirAll(diag, 0o755)
	appendFile(t, filepath.Join(diag, "Runner_1.log"), "r1\n")
	c, err := h.m.RunnerLog("aaaaaa", "Worker_2.log=7")
	if err != nil {
		t.Fatal(err)
	}
	if c.Data != "==> Runner_1.log <==\nr1\n" || c.Next != "Runner_1.log=3&Worker_2.log=7&last=Runner_1.log" {
		t.Fatalf("data %q next %q", c.Data, c.Next)
	}
}

// A log longer than one chunk arrives whole, and a partial last line in the
// Runner log stays apart from the Worker log that follows it.
func TestRunnerLogMarksFileSwitchesAcrossChunks(t *testing.T) {
	h := newHarness(t)
	diag := filepath.Join(h.m.instanceDir("aaaaaa"), "_diag")
	os.MkdirAll(diag, 0o755)
	runnerLog := filepath.Join(diag, "Runner_1.log")
	long := strings.Repeat("r", maxLogChunk+5)
	appendFile(t, runnerLog, long)
	appendFile(t, filepath.Join(diag, "Worker_2.log"), "w1\n")
	var got strings.Builder
	cursor, chunks := "", 0
	for {
		c, err := h.m.RunnerLog("aaaaaa", cursor)
		if err != nil {
			t.Fatal(err)
		}
		if c.Data == "" {
			break
		}
		got.WriteString(c.Data)
		cursor = c.Next
		if chunks++; chunks > 3 {
			t.Fatal("the log never ends")
		}
	}
	if want := "==> Runner_1.log <==\n" + long + "\n==> Worker_2.log <==\nw1\n"; chunks != 2 || got.String() != want {
		t.Fatalf("%d chunks, stream of %d bytes, want 2 chunks of %d bytes", chunks, got.Len(), len(want))
	}
	appendFile(t, runnerLog, "r\n")
	if c, _ := h.m.RunnerLog("aaaaaa", cursor); c.Data != "\n==> Runner_1.log <==\nr\n" {
		t.Fatalf("switch back %q", c.Data)
	}
}

// bytesDocker is a Docker that also reports the data root in bytes.
type bytesDocker struct{ *fakeDocker }

func (b bytesDocker) DataRootBytes(ctx context.Context) (system.DiskUsage, error) {
	pct, err := b.DataRootUsage(ctx)
	return system.DiskUsage{Pct: pct, Used: int64(pct) * 1_000_000_000, Total: 100_000_000_000, Root: "/var/lib/docker"}, err
}

func TestStatusCarriesDiskBytes(t *testing.T) {
	h := newHarness(t)
	h.docker.usage = []int{61}
	h.checkDisk(context.Background())
	if st := h.m.Status(); st.DiskPct != 61 || st.DiskUsedBytes != 0 || st.DiskTotalBytes != 0 || st.DiskRoot != "" {
		t.Fatalf("percent-only docker: %d %d %d %q", st.DiskPct, st.DiskUsedBytes, st.DiskTotalBytes, st.DiskRoot)
	}
	h.m.Docker = bytesDocker{h.docker}
	h.docker.usage = []int{62}
	h.checkDisk(context.Background())
	if st := h.m.Status(); st.DiskPct != 62 || st.DiskUsedBytes != 62_000_000_000 || st.DiskTotalBytes != 100_000_000_000 || st.DiskRoot != "/var/lib/docker" {
		t.Fatalf("bytes docker: %d %d %d %q", st.DiskPct, st.DiskUsedBytes, st.DiskTotalBytes, st.DiskRoot)
	}
}

// The instance directory is the job's to write. A project list found there
// must not make root remove someone else's compose project.
func TestCleanupIgnoresAProjectListTheJobWrote(t *testing.T) {
	h := newHarness(t)
	dir := h.m.instanceDir("aaaaaa")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ghr-projects"), []byte("immich\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := h.m.cleanupDocker(context.Background(), h.cfg, "aaaaaa", "darkmem"); err != nil {
		t.Fatal(err)
	}
	if want := []string{"com.docker.compose.project=ghr-aaaaaa"}; !reflect.DeepEqual(h.docker.netLabels, want) {
		t.Fatalf("networks removed for %v", h.docker.netLabels)
	}
}

// The remembered list lives in the root-only directory while a cleanup is
// being retried, and goes when the instance does.
func TestRememberedProjectsLiveInThePrivateDir(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkmem")
	h.docker.setErr("RemoveNetworksByLabel", errors.New("network busy"))
	h.sd.active["ghr-runner-aaaaaa"] = false
	h.m.refreshUnits(context.Background())
	h.m.Wait()
	if _, err := os.Stat(h.m.privateProjects("aaaaaa")); err != nil {
		t.Fatalf("no remembered list while the cleanup is retried: %v", err)
	}
	h.docker.setErr("RemoveNetworksByLabel", nil)
	h.now = h.now.Add(finishRetry)
	h.m.refreshUnits(context.Background())
	h.m.Wait()
	if _, err := os.Stat(h.m.privateProjects("aaaaaa")); !os.IsNotExist(err) || h.state("aaaaaa") != "gone" {
		t.Fatalf("state %s, remembered list: %v", h.state("aaaaaa"), err)
	}
}
