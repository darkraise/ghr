package main

import (
	"strings"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/model"
)

func sampleStorage(now time.Time) model.Storage {
	fin := now.Add(-time.Minute)
	return model.Storage{
		Toolchains:     []model.Toolchain{{Tool: "node", Version: "22.11.0", Arch: "x64", Bytes: 180_000_000, InstalledAt: now}},
		OtherToolCache: []model.Folder{{Name: "PyPy", Bytes: 300_000_000}},
		PackageCaches: []model.PackageCache{
			{Name: "nuget", Label: "NuGet", Paths: []string{"/home/ghrunner/.nuget/packages"}, Present: true, Bytes: 1_200_000_000, Files: 1234, LastWritten: &now},
			{Name: "cargo", Label: "Cargo", Paths: []string{"/home/ghrunner/.cargo/registry", "/home/ghrunner/.cargo/git"}},
		},
		Docker: model.DockerDisk{
			DiskPct: 61,
			Rows: []model.DockerRow{
				{Type: "Images", Count: 9, Active: 2, Bytes: 6_571_000_000, Reclaimable: 5_627_000_000},
				{Type: "Build Cache", Count: 4, Bytes: 3_798_512_500, Reclaimable: 2_568_512_500},
			},
			BuildCacheTypes: []model.BuildCacheType{{Type: "exec.cachemount", Count: 1, Bytes: 2_100_000_000, Reclaimable: 2_100_000_000}},
		},
		MeasuredAt:   &now,
		MeasureError: "docker build cache: boom",
		Operations: model.Operations{
			Current: &model.Operation{ID: "o2", Kind: "install", Target: "node 24", StartedAt: now, Progress: "extracting"},
			Queued:  2,
			Recent:  []model.Operation{{ID: "o1", Kind: "clear", Target: "npm", StartedAt: fin, FinishedAt: &fin, Outcome: "refused", Message: "refused: 1 jobs running"}},
		},
		LastPrune: &model.LastPrune{Trigger: "manual", Scope: "build-cache-all", StartedAt: fin, FinishedAt: &fin, Outcome: "errors",
			Steps: []model.PruneStep{{Name: "all build cache", Freed: 3_100_000_000}, {Name: "disk usage", Error: "df failed"}}},
	}
}

func TestStorageCommandPrintsEverySection(t *testing.T) {
	reqs := fakeDaemon(t)
	code, out, errb := runCLI(t, "", "storage")
	if code != 0 {
		t.Fatalf("code %d stderr %s", code, errb)
	}
	for _, want := range []string{
		"DOCKER DISK (disk 61%)", "Images", "6.6 GB", "5.6 GB", "  exec.cachemount", "2.1 GB",
		"last prune: manual · build-cache-all", "errors — all build cache 3.1 GB, disk usage failed: df failed",
		"TOOLCHAINS", "22.11.0", "180.0 MB", "OTHER TOOL CACHE", "PyPy", "300.0 MB",
		"PACKAGE CACHES", "nuget", "1.2 GB", "1234", "/home/ghrunner/.nuget/packages", "cargo", "not present",
		"measure error: docker build cache: boom",
		"running: install node 24 — extracting (2 queued)", "refused: 1 jobs running",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if got := (*reqs)[0]; got.method != "GET" || got.path != "/storage" {
		t.Fatalf("request %+v", got)
	}
}

func TestPruneLineWithoutAPrune(t *testing.T) {
	if got := pruneLine(nil); got != "last prune: none since ghr started" {
		t.Fatalf("line %q", got)
	}
	start := time.Date(2026, 10, 6, 14, 0, 0, 0, time.UTC)
	if got := pruneLine(&model.LastPrune{Trigger: "auto", Scope: "auto", StartedAt: start}); !strings.HasSuffix(got, " · pruning…") {
		t.Fatalf("running %q", got)
	}
}

func TestStorageMutationCommands(t *testing.T) {
	for _, tc := range []struct {
		args               []string
		method, path, body string
		out                string
	}{
		{[]string{"storage", "refresh"}, "POST", "/storage/refresh", "", "measuring — follow with: ghr storage"},
		{[]string{"toolchain", "install", "node", "22"}, "POST", "/toolchains", `{"tool":"node","version":"22"}`, queuedNote},
		{[]string{"toolchain", "install", "--preset", "popular"}, "POST", "/toolchains", `{"preset":"popular"}`, queuedNote},
		{[]string{"toolchain", "rm", "java", "21.0.8+9"}, "DELETE", "/toolchains/java/21.0.8+9", "", queuedNote},
		{[]string{"cache", "clear", "nuget"}, "POST", "/caches/nuget/clear", "", queuedNote},
		{[]string{"prune"}, "POST", "/prune", "", "prune started — follow with: ghr storage"},
		{[]string{"prune", "--scope", "unused-volumes"}, "POST", "/prune/unused-volumes", "", "prune started — follow with: ghr storage"},
	} {
		reqs := fakeDaemon(t)
		code, out, errb := runCLI(t, "", tc.args...)
		if code != 0 {
			t.Errorf("%v: code %d stderr %s", tc.args, code, errb)
			continue
		}
		if len(*reqs) != 1 {
			t.Errorf("%v: %d requests", tc.args, len(*reqs))
			continue
		}
		got := (*reqs)[0]
		if got.method != tc.method || got.path != tc.path || strings.TrimSpace(got.body) != tc.body {
			t.Errorf("%v: request %+v", tc.args, got)
		}
		if strings.TrimSpace(out) != tc.out {
			t.Errorf("%v: output %q", tc.args, out)
		}
	}
}

func TestToolchainAndCacheListings(t *testing.T) {
	reqs := fakeDaemon(t)
	code, out, _ := runCLI(t, "", "toolchain", "list")
	if code != 0 || !strings.Contains(out, "22.11.0") || !strings.Contains(out, "PyPy") || strings.Contains(out, "PACKAGE CACHES") {
		t.Fatalf("toolchain list:\n%s", out)
	}
	code, out, _ = runCLI(t, "", "toolchain", "available", "java")
	if code != 0 || !strings.Contains(out, "21.0.8+9") || !strings.Contains(out, "lts") || !strings.Contains(out, "24.0.2+12") {
		t.Fatalf("toolchain available:\n%s", out)
	}
	if got := (*reqs)[1].path; got != "/toolchains/available?tool=java" {
		t.Fatalf("available request %s", got)
	}
	code, out, _ = runCLI(t, "", "cache", "list")
	if code != 0 || !strings.Contains(out, "nuget") || !strings.Contains(out, "not present") || strings.Contains(out, "TOOLCHAINS") {
		t.Fatalf("cache list:\n%s", out)
	}
}

func TestStorageCommandUsageErrors(t *testing.T) {
	fakeDaemon(t)
	for _, args := range [][]string{
		{"storage", "bogus"}, {"toolchain"}, {"toolchain", "install", "node"}, {"toolchain", "install", "--preset"},
		{"toolchain", "rm", "node"}, {"toolchain", "bogus"}, {"cache"}, {"cache", "clear"}, {"prune", "extra"},
	} {
		code, _, errb := runCLI(t, "", args...)
		if code != 2 || !strings.Contains(errb, "usage:") {
			t.Errorf("%v: code %d stderr %q", args, code, errb)
		}
	}
}
