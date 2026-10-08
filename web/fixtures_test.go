package web

import (
	"bytes"
	"encoding/json"
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
			Web: config.Web{Listen: "0.0.0.0:8080", Hosts: []string{"ghr.lan"}},
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
			MeasuredAt: ptr(at(-2 * time.Minute)),
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
		"available-repos":   []model.AvailableRepo{{Name: "darkmem", Private: true, Configured: true}, {Name: "new-repo", Private: true}},
		"toolchain-choices": []model.ToolchainChoice{{Spec: "22.11.0", Version: "22.11.0", LTS: true}, {Spec: "24.9.0", Version: "24.9.0"}},
	}
}
