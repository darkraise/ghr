package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/activity"
	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/model"
)

var fixtureTime = time.Date(2026, 10, 3, 14, 5, 0, 0, time.UTC)

func at(d time.Duration) time.Time { return fixtureTime.Add(d) }

func ptr[T any](v T) *T { return &v }

// TestTypeFixtures keeps web/src/api/fixtures in step with the Go types the
// web UI reads; the frontend's tests parse each fixture into its TypeScript
// type.
func TestTypeFixtures(t *testing.T) {
	update := os.Getenv("GHR_UPDATE_FIXTURES") == "1"
	for name, v := range fixtureValues() {
		path := filepath.Join("src", "api", "fixtures", name+".json")
		want, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		want = append(want, '\n')
		if update {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, want, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(bytes.ReplaceAll(got, []byte("\r\n"), []byte("\n")), want) {
			t.Errorf("web/%s is out of date with the Go types; regenerate with: GHR_UPDATE_FIXTURES=1 go test ./web/", filepath.ToSlash(path))
		}
	}
}

func fixtureValues() map[string]any {
	lastJob := model.HistoryEntry{ID: "h1", Repo: "darkmem", RunID: 101, RunNumber: "41", Workflow: "ci", JobName: "build",
		Conclusion: "success", StartedAt: at(-30 * time.Minute), FinishedAt: at(-25 * time.Minute),
		HTMLURL: "https://github.com/darkraise/darkmem/actions/runs/101/job/1"}
	return map[string]any{
		"status": model.Status{
			Now: fixtureTime, Epoch: "lz3k9a", Mode: "queue", GlobalMax: 2, RateRemaining: 4980, RateLimit: 5000, DiskPct: 61, DiskUsedBytes: 146_000_000_000, DiskTotalBytes: 240_000_000_000, DiskRoot: "/var/lib/docker",
			Repos: []model.RepoStatus{
				{Name: "darkmem", Max: 2, Active: 1, Queued: 3, OldestQueuedAt: ptr(at(-3 * time.Minute)), LastJob: &lastJob},
				{Name: "darkcloud", Paused: true, Max: 1},
				{Name: "old-repo", Paused: true, Removing: true, Max: 1, Error: "GitHub: not found"},
			},
			Instances: []model.InstanceStatus{
				{ID: "aaaaaa", Repo: "darkmem", RunnerName: "ghr-aaaaaa", State: "busy", Since: at(-5 * time.Minute),
					Job: &model.JobInfo{RunID: 102, RunNumber: "42", Workflow: "ci", Name: "test",
						HTMLURL: "https://github.com/darkraise/darkmem/actions/runs/102/job/2", StartedAt: at(-4 * time.Minute)}},
				{ID: "bbbbbb", Repo: "darkmem", RunnerName: "ghr-bbbbbb", State: "idle", Since: at(-time.Minute)},
			},
			Maintenance: model.MaintenanceStatus{LastStarted: ptr(at(-2 * time.Hour)), LastFinished: ptr(at(-2*time.Hour + time.Minute)),
				LastOutcome: "ok"},
			RunnerUpdate: model.RunnerUpdate{Installed: "2.337.0", Latest: "2.338.0", LatestPublished: ptr(at(-48 * time.Hour)),
				Deadline: ptr(at(20 * 24 * time.Hour)), CheckedAt: ptr(at(-time.Hour)), Queued: true,
				QueuedAt: ptr(at(-10 * time.Minute)), LastOutcome: "current", LastFinished: ptr(at(-24 * time.Hour))},
		},
		"status-degraded": model.Status{
			Now: fixtureTime, Epoch: "lz3k9b", Mode: "all", GlobalMax: 2, Degraded: true,
			DegradedReason: "GitHub rejected the token: 401 Bad credentials", DiskPct: 91,
			Repos: []model.RepoStatus{{Name: "darkmem", Paused: true, Removing: true, Max: 1, Error: "GitHub: not found"}},
			Instances: []model.InstanceStatus{
				{ID: "cccccc", Repo: "darkmem", RunnerName: "ghr-cccccc", State: "starting", Since: at(-10 * time.Second)},
			},
			Maintenance: model.MaintenanceStatus{Running: true, LastStarted: ptr(at(-time.Minute))},
			RunnerUpdate: model.RunnerUpdate{Installed: "2.337.0", Latest: "2.338.0", CheckedAt: ptr(at(-time.Hour)),
				CheckError: "GitHub rate limit", Running: true, LastOutcome: "failed", LastError: "download failed: 502",
				LastFinished: ptr(at(-2 * time.Hour))},
		},
		"events": []model.Event{
			{Seq: 1, Time: at(-3 * time.Minute), Level: "info", Msg: "ghr daemon started (owner darkraise, mode queue)"},
			{Seq: 2, Time: at(-2 * time.Minute), Level: "ok", Repo: "darkmem", Msg: "runner aaaaaa started"},
			{Seq: 3, Time: at(-time.Minute), Level: "warn", Repo: "darkcloud", Msg: "repo paused"},
		},
		"history": []model.HistoryEntry{lastJob,
			{ID: "h2", Repo: "darkcloud", RunID: 90, RunNumber: "7", Workflow: "deploy", JobName: "deploy", Conclusion: "failure",
				StartedAt: at(-3 * time.Hour), FinishedAt: at(-3*time.Hour + 90*time.Second)},
		},
		"log": model.LogChunk{
			Data: "[2026-10-03 14:01:00Z INFO Runner] Listening for Jobs\n[2026-10-03 14:01:05Z INFO Worker] Running job: test\n",
			Next: "Worker_1.log:96;Runner_1.log:52",
		},
		"steps": []model.Step{
			{Number: 1, Name: "Set up job", Status: "completed", Conclusion: "success",
				StartedAt: ptr(at(-4 * time.Minute)), CompletedAt: ptr(at(-4*time.Minute + 6*time.Second))},
			{Number: 2, Name: "Run tests", Status: "in_progress", StartedAt: ptr(at(-4*time.Minute + 6*time.Second))},
			{Number: 3, Name: "Post checkout", Status: "queued"},
		},
		"containers": []model.Container{{ID: "c0ffee12", Name: "ghr-aaaaaa-db-1", Image: "postgres:17", State: "running", Project: "ghr-aaaaaa"}},
		"activity-lanes": activity.Build(activity.Input{
			Window: "1h", Now: fixtureTime, Capacity: ptr(2), Retention: 30 * 24 * time.Hour,
			Repos: []string{"darkmem", "darkcloud"},
			History: []model.HistoryEntry{
				{ID: "h1", Repo: "darkmem", RunID: 101, RunNumber: "41", Workflow: "ci", JobName: "build", Conclusion: "success",
					StartedAt: at(-30 * time.Minute), FinishedAt: at(-25 * time.Minute),
					HTMLURL: "https://github.com/darkraise/darkmem/actions/runs/101/job/1"},
				{ID: "h3", Repo: "darkmem", RunID: 99, RunNumber: "40", Workflow: "ci", JobName: "lint", Conclusion: "failure",
					StartedAt: at(-50 * time.Minute), FinishedAt: at(-44 * time.Minute)},
			},
			Instances: []model.InstanceStatus{
				{ID: "aaaaaa", Repo: "darkmem", RunnerName: "ghr-aaaaaa", State: "busy", Since: at(-5 * time.Minute),
					Job: &model.JobInfo{RunID: 102, RunNumber: "42", Workflow: "ci", Name: "test",
						HTMLURL: "https://github.com/darkraise/darkmem/actions/runs/102/job/2", StartedAt: at(-4 * time.Minute)}},
				{ID: "bbbbbb", Repo: "darkmem", RunnerName: "ghr-bbbbbb", State: "idle", Since: at(-2 * time.Minute)},
			},
			Minutes: []model.MetricSample{
				{At: at(-3 * time.Minute), Live: 1, Queued: 2, CPU: ptr(12.5), Mem: ptr(int64(2 << 30))},
				{At: at(-2 * time.Minute), Live: 2, Queued: 1, CPU: ptr(48.0), Mem: ptr(int64(3 << 30))},
				{At: at(-time.Minute), Live: 2, Queued: 3},
			},
		}),
		"activity-buckets": activity.Build(activity.Input{
			Window: "24h", Now: fixtureTime, Retention: 30 * 24 * time.Hour,
			Repos: []string{"darkmem"},
			History: []model.HistoryEntry{
				{ID: "h1", Repo: "darkmem", RunID: 101, RunNumber: "41", Workflow: "ci", JobName: "build", Conclusion: "success",
					StartedAt: at(-30 * time.Minute), FinishedAt: at(-25 * time.Minute)},
				{ID: "h2", Repo: "darkmem", RunID: 90, RunNumber: "39", Workflow: "ci", JobName: "build", Conclusion: "failure",
					StartedAt: at(-3 * time.Hour), FinishedAt: at(-170 * time.Minute)},
			},
			Hours: []model.MetricRollup{
				{At: time.Date(2026, 10, 3, 13, 0, 0, 0, time.UTC), Samples: 60, QueuedMax: 3, CPUAvg: ptr(22.5), MemAvg: ptr(int64(3 << 30))},
			},
		}),
		"metrics": model.Metrics{
			Samples: []model.MetricSample{
				{At: at(-3 * time.Minute), Live: 1, Queued: 2, CPU: ptr(12.5), Mem: ptr(int64(2 << 30))},
				{At: at(-2 * time.Minute), Live: 2, Queued: 1, CPU: ptr(48.0), Mem: ptr(int64(3 << 30))},
				{At: at(-time.Minute), Live: 1, Queued: 3},
			},
			CPU: ptr(31.0), MemUsed: ptr(int64(3 << 30)), MemTotal: ptr(int64(16 << 30)), DiskPct: 61,
		},
		"config": config.Config{Owner: "darkraise", Mode: "queue", GlobalMax: 2,
			PollInterval: config.Duration(10 * time.Second), StartTimeout: config.Duration(2 * time.Minute),
			IdleTimeout: config.Duration(5 * time.Minute), DiskHighWater: 80, BuildCacheKeep: "20GB",
			HistoryRetention: config.Duration(30 * 24 * time.Hour), Labels: []string{"homelab"},
			RunnerLimits: config.RunnerLimits{MemoryMax: "6G", CPUQuota: "200%"},
			Repos: []config.Repo{
				{Name: "darkmem", Max: ptr(2), Warm: ptr(1), Labels: []string{"gpu"}, CleanupNamePrefixes: []string{"darkmem-"}},
				{Name: "darkcloud", Paused: true},
			},
			WatchRepos: []string{"docs", "old-docs"},
			Web:        config.Web{Listen: "0.0.0.0:8080", Hosts: []string{"ghr.lan"}},
			GitHubStorage: config.GitHubStorage{CleanupMaxAge: config.Duration(48 * time.Hour),
				Repos: map[string]config.StorageOverride{"darkmem": {Cleanup: "6h"}}},
		},
		"storage": model.Storage{
			Toolchains: []model.Toolchain{{Tool: "node", Version: "22.11.0", Arch: "x64",
				Path: "/var/lib/ghr/toolcache/node/22.11.0/x64", Bytes: 190_000_000, InstalledAt: at(-72 * time.Hour)}},
			OtherToolCache: []model.Folder{{Name: "PyPy", Bytes: 80_000_000}},
			PackageCaches: []model.PackageCache{
				{Name: "nuget", Label: "NuGet", Paths: []string{".nuget/packages"}, Present: true, Bytes: 3_600_000_000,
					Files: 41000, LastWritten: ptr(at(-time.Hour))},
				{Name: "cargo", Label: "Cargo", Paths: []string{".cargo/registry", ".cargo/git"}},
			},
			Docker: model.DockerDisk{
				Rows: []model.DockerRow{
					{Type: "Images", Count: 10, Active: 2, Bytes: 8_100_000_000, Reclaimable: 7_100_000_000},
					{Type: "Build Cache", Count: 12, Bytes: 420_000_000, Reclaimable: 380_000_000},
				},
				BuildCacheTypes: []model.BuildCacheType{{Type: "regular", Count: 10, Bytes: 334_000_000, Reclaimable: 300_000_000}},
				DiskPct:         61,
			},
			CachesOnDataRoot: true,
			MeasuredAt:       ptr(at(-2 * time.Minute)),
			Operations: model.Operations{
				Current: &model.Operation{ID: "op3", Kind: "install", Target: "node 24", StartedAt: at(-30 * time.Second), Progress: "extracting"},
				Queued:  1,
				Recent: []model.Operation{{ID: "op2", Kind: "clear", Target: "nuget", StartedAt: at(-time.Hour),
					FinishedAt: ptr(at(-time.Hour + time.Minute)), Outcome: "ok", Message: "cleared NuGet (3.6 GB freed)"}},
			},
			LastPrune: &model.LastPrune{Trigger: "manual", Scope: "build-cache-all", StartedAt: at(-50 * time.Minute),
				FinishedAt: ptr(at(-49 * time.Minute)), Outcome: "ok", Steps: []model.PruneStep{{Name: "all build cache", Freed: 6_800_000}}},
		},
		"token": model.TokenStatus{State: "ok", CheckedAt: ptr(at(-time.Minute)), RateRemaining: ptr(4980), RateLimit: ptr(5000),
			RateReset: ptr(at(40 * time.Minute)), ExpiresAt: ptr(at(60 * 24 * time.Hour))},
		"label-check": model.LabelCheck{State: "done", CheckedAt: ptr(at(-time.Minute)),
			Groups: []model.LabelGroup{{Labels: []string{"homelab", "self-hosted"}, Jobs: []string{"ci / build", "ci / test"},
				Count: 12, LastSeen: at(-time.Hour)}}},
		"registrations": []model.Registration{{ID: 5, Name: "ghr-aaaaaa", Status: "online", Busy: true,
			Labels: []string{"self-hosted", "homelab"}, GHR: true}},
		"available-repos": []model.AvailableRepo{
			{Name: "darkmem", Private: true, Configured: true},
			{Name: "docs", Watched: true},
			{Name: "new-repo", Private: true},
		},
		"actions": model.Actions{
			FetchedAt: fixtureTime,
			Runs: []model.ActionsRun{
				{Repo: "docs", ID: 300, RunNumber: 9, Workflow: "pages", Title: "Publish the guide", Branch: "main",
					Event: "push", Actor: "darkraise", Status: "queued", StartedAt: at(-time.Minute),
					UpdatedAt: at(-time.Minute), HTMLURL: "https://github.com/darkraise/docs/actions/runs/300", Watched: true},
				{Repo: "darkmem", ID: 102, RunNumber: 42, Workflow: "ci", Title: "Fix the cache key", Branch: "master",
					Event: "push", Actor: "darkraise", Status: "in_progress", StartedAt: at(-4 * time.Minute),
					UpdatedAt: at(-time.Minute), HTMLURL: "https://github.com/darkraise/darkmem/actions/runs/102", GHR: true},
				{Repo: "darkmem", ID: 101, RunNumber: 41, Workflow: "ci", Title: "Bump the runner", Branch: "renovate/runner",
					Event: "pull_request", Actor: "renovate[bot]", Status: "completed", Conclusion: "success",
					StartedAt: at(-30 * time.Minute), UpdatedAt: at(-25 * time.Minute),
					HTMLURL: "https://github.com/darkraise/darkmem/actions/runs/101", GHR: true},
				{Repo: "darkcloud", ID: 90, RunNumber: 7, Workflow: "deploy", Title: "deploy", Branch: "master",
					Event: "workflow_dispatch", Actor: "darkraise", Status: "completed", Conclusion: "failure",
					StartedAt: at(-24 * time.Hour), UpdatedAt: at(-24*time.Hour + 90*time.Second),
					HTMLURL: "https://github.com/darkraise/darkcloud/actions/runs/90"},
			},
			Repos: []model.ActionsRepo{
				{Repo: "darkmem"},
				{Repo: "darkcloud"},
				{Repo: "docs", Watched: true},
				{Repo: "old-docs", Watched: true, Error: "GitHub rate limit; API calls are paused", RetryAt: ptr(at(25 * time.Minute))},
			},
		},
		"github-storage": githubStorageFixture(),
		"storage-alert": model.StorageAlert{Level: "critical", Month: "2026-10", UsedGBHours: 330.5, IncludedGBHours: 363.28125,
			UsedPct: 91, RunsOutAt: ptr(at(60 * time.Hour)), ResetsAt: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)},
		"toolchain-choices": []model.ToolchainChoice{{Spec: "22.11.0", Version: "22.11.0", LTS: true}, {Spec: "24.9.0", Version: "24.9.0"}},
		"run-detail":        runDetailFixture(),
		"run-annotations":   runAnnotationsFixture(),
		"job-log":           jobLogFixture(),
	}
}

// runAt is a time of the run-detail fixture's run, on the fixture day.
func runAt(min, sec int) *time.Time {
	t := time.Date(2026, 10, 3, 13, min, sec, 0, time.UTC)
	return &t
}

// fixtureStep is a completed step; a nil start leaves both times out.
func fixtureStep(n int, name, conclusion string, start, end *time.Time) model.Step {
	return model.Step{Number: n, Name: name, Status: "completed", Conclusion: conclusion, StartedAt: start, CompletedAt: end}
}

func fixtureJob(id int64, name, conclusion string, start, end *time.Time, runner string, labels []string, ghr bool, steps ...model.Step) model.RunJob {
	return model.RunJob{ID: id, Name: name, Status: "completed", Conclusion: conclusion, StartedAt: start, CompletedAt: end,
		RunnerName: runner, Labels: labels, HTMLURL: fmt.Sprintf("https://github.com/darkraise/darkmem/actions/runs/102/job/%d", id),
		GHR: ghr, Steps: steps}
}

func runDetailFixture() model.RunDetail {
	ghrLabels := []string{"self-hosted", "homelab"}
	hosted := []string{"ubuntu-latest"}
	return model.RunDetail{
		Repo: "darkmem", ID: 102, RunNumber: 42, Attempt: 2, Attempts: 2, Workflow: "ci", Title: "Fix the cache key",
		Branch: "fix/cache-key", Event: "pull_request", Actor: "octo-dev", Status: "completed", Conclusion: "failure",
		HeadSHA: "a1f9c3e0d4b5c6a7b8c9d0e1f2a3b4c5d6e7f8a9", Path: ".github/workflows/ci.yml", PullRequests: []int{87},
		StartedAt: *runAt(55, 0), UpdatedAt: *runAt(59, 10), HTMLURL: "https://github.com/darkraise/darkmem/actions/runs/102",
		Jobs: []model.RunJob{
			fixtureJob(1001, "Pick runner", "success", runAt(55, 0), runAt(55, 20), "ghr-darkmem-a1b2c3", ghrLabels, true,
				fixtureStep(1, "Set up job", "success", runAt(55, 0), runAt(55, 5)),
				fixtureStep(2, "Pick", "success", runAt(55, 5), runAt(55, 19)),
				fixtureStep(3, "Complete job", "success", runAt(55, 19), runAt(55, 20))),
			fixtureJob(1002, "Backend tests", "failure", runAt(55, 25), runAt(57, 40), "ghr-darkmem-d4e5f6", ghrLabels, true,
				fixtureStep(1, "Set up job", "success", runAt(55, 25), runAt(55, 30)),
				fixtureStep(2, "Install", "success", runAt(55, 30), runAt(56, 10)),
				fixtureStep(3, "Lint (ruff)", "failure", runAt(56, 10), runAt(57, 35)),
				fixtureStep(4, "Test", "skipped", nil, nil),
				fixtureStep(5, "Complete job", "success", runAt(57, 35), runAt(57, 40))),
			fixtureJob(1003, "Frontend tests (1/2)", "success", runAt(55, 25), runAt(58, 0), "GitHub Actions 2", hosted, false,
				fixtureStep(1, "Set up job", "success", runAt(55, 25), runAt(55, 30)),
				fixtureStep(2, "Test", "success", runAt(55, 30), runAt(57, 55)),
				fixtureStep(3, "Complete job", "success", runAt(57, 55), runAt(58, 0))),
			fixtureJob(1004, "Frontend tests (2/2)", "success", runAt(55, 26), runAt(58, 30), "GitHub Actions 3", hosted, false,
				fixtureStep(1, "Set up job", "success", runAt(55, 26), runAt(55, 31)),
				fixtureStep(2, "Test", "success", runAt(55, 31), runAt(58, 25)),
				fixtureStep(3, "Complete job", "success", runAt(58, 25), runAt(58, 30))),
			fixtureJob(1005, "E2E (api)", "success", runAt(55, 30), runAt(59, 10), "GitHub Actions 4", hosted, false,
				fixtureStep(1, "Set up job", "success", runAt(55, 30), runAt(55, 35)),
				fixtureStep(2, "Test", "success", runAt(55, 35), runAt(59, 5)),
				fixtureStep(3, "Complete job", "success", runAt(59, 5), runAt(59, 10))),
		},
		Graph: &model.RunGraph{Nodes: []model.GraphNode{
			{Key: "pick-runner", Name: "Pick runner", Column: 0, Needs: []string{}, JobIDs: []int64{1001}},
			{Key: "e2e", Name: "E2E (${{ matrix.suite }})", Column: 0, Needs: []string{}, JobIDs: []int64{1005}, Group: true},
			{Key: "backend", Name: "Backend tests", Column: 1, Needs: []string{"pick-runner"}, JobIDs: []int64{1002}},
			{Key: "frontend-tests", Name: "Frontend tests (${{ matrix.shard }}/2)", Column: 1, Needs: []string{"pick-runner"},
				JobIDs: []int64{1003, 1004}, Group: true},
		}},
		Artifacts: []model.RunArtifact{
			{ID: 11, Name: "frontend-dist", SizeBytes: 2_400_000, ExpiresAt: ptr(time.Date(2026, 10, 17, 13, 59, 0, 0, time.UTC))},
			{ID: 12, Name: "coverage-backend", SizeBytes: 51_200, Expired: true, ExpiresAt: ptr(time.Date(2026, 10, 2, 13, 59, 0, 0, time.UTC))},
		},
		FetchedAt: fixtureTime,
	}
}

func runAnnotationsFixture() model.RunAnnotations {
	return model.RunAnnotations{
		Items: []model.Annotation{
			{Level: "error", JobID: 1002, JobName: "Backend tests", Step: 3, StepName: "Lint (ruff)", Line: 5,
				Message: "src/cache.py:12:5: F401 'os' imported but unused"},
			{Level: "error", JobID: 1002, JobName: "Backend tests", Step: 3, StepName: "Lint (ruff)", Line: 7,
				Message: "Process completed with exit code 1."},
			{Level: "warning", JobID: 1003, JobName: "Frontend tests (1/2)", Step: 2, StepName: "Test", Line: 2,
				Message: "Node.js 16 actions are deprecated."},
			{Level: "notice", JobID: 1005, JobName: "E2E (api)", Step: 2, StepName: "Test", Line: 3, Message: "Retried 1 flaky test"},
		},
		Omitted: map[int64]int{1002: 3},
	}
}

func jobLogFixture() model.JobLog {
	line := func(n int, kind, text string) model.LogLine { return model.LogLine{N: n, Kind: kind, Text: text} }
	return model.JobLog{JobID: 1002, Steps: []model.LogStep{
		{Number: 1, Name: "Set up job", Lines: []model.LogLine{
			line(1, "", "Current runner version: '2.338.0'"), line(2, "group", "Runner Image"),
			line(3, "", "Image: ubuntu-24.04"), line(4, "endgroup", ""),
		}},
		{Number: 2, Name: "Install", Lines: []model.LogLine{
			line(1, "command", "pip install -r requirements.txt"), line(2, "", "Successfully installed ruff-0.6.9"),
		}},
		{Number: 3, Name: "Lint (ruff)", Lines: []model.LogLine{
			line(1, "group", "Run ruff check ."), line(2, "", "ruff check ."), line(3, "endgroup", ""),
			line(4, "", "\x1b[31mFound 1 error.\x1b[0m"),
			line(5, "error", "src/cache.py:12:5: F401 'os' imported but unused"),
			line(6, "debug", "Finishing: Lint (ruff)"),
			line(7, "error", "Process completed with exit code 1."),
		}},
		{Number: 4, Name: "Test", Lines: []model.LogLine{}},
		{Number: 5, Name: "Complete job", Lines: []model.LogLine{line(1, "", "Cleaning up orphan processes")}},
	}}
}

// githubStorageFixture is a snapshot at the warn level: one repository with
// an override, one on the global age, and one the token cannot read.
func githubStorageFixture() model.GitHubStorage {
	return model.GitHubStorage{
		CheckedAt: ptr(at(-5 * time.Minute)),
		Quota: &model.GitHubQuota{Month: "2026-10", ReadAt: at(-20 * time.Minute), IncludedGBHours: 363.28125,
			UsedGBHours: 260.5, ActionsGBHours: 250.25, PackagesGBHours: 10.25, RateGBHoursPerHour: 0.2,
			EstimatedGBHours: 262.9, ProjectedGBHours: 396.95,
			RunsOutAt: ptr(time.Date(2026, 10, 24, 11, 45, 0, 0, time.UTC)), Level: "warn",
			ResetsAt: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)},
		Repos: []model.RepoStorage{
			{Name: "darkmem", Visibility: "private", Covered: true, ArtifactCount: 2, ArtifactBytes: 209_715_200,
				CacheCount: 1, CacheBytes: 71_431_401, RetentionDays: 90, RetentionMaxDays: 400, Cleanup: "6h", CleanupSource: "override",
				Artifacts: []model.ArtifactItem{
					{ID: 9001, Name: "frontend-dist", Bytes: 157_286_400, CreatedAt: at(-24 * time.Hour),
						ExpiresAt: ptr(at(89 * 24 * time.Hour)), RunID: 102, Branch: "master"},
					{ID: 9002, Name: "coverage", Bytes: 52_428_800, CreatedAt: at(-3 * time.Hour),
						ExpiresAt: ptr(at(90*24*time.Hour - 3*time.Hour)), RunID: 103, Branch: "feature/x"},
				},
				Caches: []model.CacheItem{{ID: 8787967584, Key: "node-cache-Linux-x64-npm-a4cc85d3", Ref: "refs/heads/master",
					Bytes: 71_431_401, LastUsedAt: at(-time.Hour)}}},
			{Name: "darkcloud", Visibility: "private", Covered: true, ArtifactCount: 1, ArtifactBytes: 20_971_520,
				RetentionDays: 7, RetentionMaxDays: 90, Cleanup: "2d", CleanupSource: "global",
				Artifacts: []model.ArtifactItem{{ID: 9101, Name: "dist", Bytes: 20_971_520, CreatedAt: at(-30 * time.Hour),
					ExpiresAt: ptr(at(7*24*time.Hour - 30*time.Hour)), RunID: 501, Branch: "main"}},
				Caches: []model.CacheItem{}},
			{Name: "sandbox", Visibility: "public", Cleanup: "2d", CleanupSource: "global",
				Artifacts: []model.ArtifactItem{}, Caches: []model.CacheItem{},
				Error: "token lacks actions=read on darkraise/sandbox: grant it in the PAT settings"},
		},
		Operation: &model.GitHubOperation{ID: "op-1", Kind: "delete-artifacts", Repo: "darkmem", Total: 3, Done: 3, Bytes: 52_428_800,
			Failed:    []model.OperationFailure{{Repo: "darkmem", ID: 9003, Error: "not in the last inventory"}},
			StartedAt: at(-10 * time.Minute), FinishedAt: ptr(at(-9 * time.Minute))},
		Deletions: []model.Deletion{
			{At: at(-9 * time.Minute), Repo: "darkmem", Kind: "artifact", Name: "coverage-old", Bytes: 52_428_800, Age: "3d", By: "cleanup"},
			{At: at(-2 * time.Hour), Repo: "darkcloud", Kind: "cache", Name: "node-cache-Linux-x64-npm-0b1c", Bytes: 31_457_280, Age: "5d", By: "manual"},
		},
	}
}
