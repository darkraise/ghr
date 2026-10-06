package model

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestStorageJSON(t *testing.T) {
	now := time.Date(2026, 10, 6, 14, 0, 0, 0, time.UTC)
	st := Storage{
		Toolchains:     []Toolchain{{Tool: "node", Version: "22.11.0", Arch: "x64", Path: "/p", Bytes: 1, InstalledAt: now}},
		OtherToolCache: []Folder{{Name: "PyPy", Bytes: 2}},
		PackageCaches:  []PackageCache{{Name: "npm", Label: "npm", Paths: []string{"/h/.npm"}, Present: true, Bytes: 3, Files: 4, LastWritten: &now}},
		Docker: DockerDisk{
			Rows:            []DockerRow{{Type: "Images", Count: 9, Active: 2, Bytes: 5, Reclaimable: 6}},
			BuildCacheTypes: []BuildCacheType{{Type: "regular", Count: 1, Bytes: 7, Reclaimable: 8}},
			DiskPct:         61,
		},
		MeasuredAt:   &now,
		Measuring:    true,
		MeasureError: "x",
		Operations: Operations{
			Current: &Operation{ID: "o1", Kind: "install", Target: "node 22", StartedAt: now, Progress: "extracting"},
			Queued:  2,
			Recent:  []Operation{{ID: "o0", Kind: "clear", Target: "npm", StartedAt: now, FinishedAt: &now, Outcome: "ok", Message: "cleared npm"}},
		},
		LastPrune: &LastPrune{Trigger: "manual", Scope: "standard", StartedAt: now, FinishedAt: &now, Outcome: "ok",
			Steps: []PruneStep{{Name: "dangling images", Freed: 9}, {Name: "disk usage", Error: "df failed"}}},
	}
	b, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"toolchains":[{"tool":"node","version":"22.11.0","arch":"x64","path":"/p","bytes":1,"installed_at":"2026-10-06T14:00:00Z"}],` +
		`"other_tool_cache":[{"name":"PyPy","bytes":2}],` +
		`"package_caches":[{"name":"npm","label":"npm","paths":["/h/.npm"],"present":true,"bytes":3,"files":4,"last_written":"2026-10-06T14:00:00Z"}],` +
		`"docker":{"rows":[{"type":"Images","count":9,"active":2,"bytes":5,"reclaimable":6}],"build_cache_types":[{"type":"regular","count":1,"bytes":7,"reclaimable":8}],"disk_pct":61},` +
		`"measured_at":"2026-10-06T14:00:00Z","measuring":true,"measure_error":"x",` +
		`"operations":{"current":{"id":"o1","kind":"install","target":"node 22","started_at":"2026-10-06T14:00:00Z","progress":"extracting"},"queued":2,` +
		`"recent":[{"id":"o0","kind":"clear","target":"npm","started_at":"2026-10-06T14:00:00Z","finished_at":"2026-10-06T14:00:00Z","outcome":"ok","message":"cleared npm"}]},` +
		`"last_prune":{"trigger":"manual","scope":"standard","started_at":"2026-10-06T14:00:00Z","finished_at":"2026-10-06T14:00:00Z","outcome":"ok",` +
		`"steps":[{"name":"dangling images","freed":9},{"name":"disk usage","freed":0,"error":"df failed"}]}}`
	if string(b) != want {
		t.Fatalf("json\n got %s\nwant %s", b, want)
	}
}

func TestOperationsWithoutCurrentAndNoPrune(t *testing.T) {
	b, err := json.Marshal(Storage{Operations: Operations{Recent: []Operation{}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"current":null`, `"recent":[]`, `"last_prune":null`} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("missing %s in %s", want, b)
		}
	}
}

func TestInstallRequestJSON(t *testing.T) {
	for req, want := range map[InstallRequest]string{
		{Tool: "node", Version: "22"}: `{"tool":"node","version":"22"}`,
		{Preset: "popular"}:           `{"preset":"popular"}`,
	} {
		if b, _ := json.Marshal(req); string(b) != want {
			t.Errorf("%+v → %s", req, b)
		}
	}
	if b, _ := json.Marshal(ToolchainChoice{Spec: "21", Version: "21.0.8+9", LTS: true}); string(b) != `{"spec":"21","version":"21.0.8+9","lts":true}` {
		t.Errorf("choice %s", b)
	}
}

func TestHumanBytes(t *testing.T) {
	for n, want := range map[int64]string{
		0:             "0 B",
		999:           "999 B",
		1000:          "1.0 kB",
		1234567:       "1.2 MB",
		180000000:     "180.0 MB",
		6571000000:    "6.6 GB",
		1500000000000: "1.5 TB",
	} {
		if got := HumanBytes(n); got != want {
			t.Errorf("HumanBytes(%d) = %q, want %q", n, got, want)
		}
	}
}
