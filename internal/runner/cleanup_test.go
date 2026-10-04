package runner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

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
	h.m.checkDisk(context.Background(), h.cfg)
	if len(h.docker.prunes) != 0 || h.m.Status().DiskPct != 61 {
		t.Fatalf("prunes %v disk %d", h.docker.prunes, h.m.Status().DiskPct)
	}
}

func TestCheckDiskPrunesInOrder(t *testing.T) {
	h := newHarness(t)
	h.docker.usage = []int{85, 83, 70}
	h.m.checkDisk(context.Background(), h.cfg)
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
	h.m.checkDisk(context.Background(), h.cfg)
	if !reflect.DeepEqual(h.docker.prunes, []string{"until=72h", "images"}) {
		t.Fatalf("prunes %v", h.docker.prunes)
	}
}

func TestCheckDiskReportsFailuresAndKeepsLastMeasurement(t *testing.T) {
	h := newHarness(t)
	h.docker.usage = []int{85, 83, -1}
	h.docker.setErr("PruneBuildCacheTo", errors.New("unknown flag"))
	h.docker.setErr("PruneDanglingImages", errors.New("daemon busy"))
	h.m.checkDisk(context.Background(), h.cfg)
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
	got, _ := h.m.History.Query("", "", 0)
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

func appendFile(t *testing.T, path, s string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	f.WriteString(s)
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
	if c.Data != "r1\nw1\n" {
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
	if got.String() != "r1\nw1\nr2\nw2\nw3\n" {
		t.Fatalf("stream %q", got.String())
	}
	if c, _ = h.m.RunnerLog("aaaaaa", c.Next); c.Data != "" {
		t.Fatalf("nothing new, got %q", c.Data)
	}
	if _, err := h.m.RunnerLog("../etc", ""); err == nil {
		t.Fatal("path traversal accepted")
	}
}
