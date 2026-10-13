package model

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestGitHubStorageJSON(t *testing.T) {
	now := time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)
	st := GitHubStorage{
		CheckedAt: &now,
		Quota: &GitHubQuota{Month: "2026-10", ReadAt: now, IncludedGBHours: 363.28, UsedGBHours: 384.5, ActionsGBHours: 373.3,
			PackagesGBHours: 11.2, RateGBHoursPerHour: 0.17, EstimatedGBHours: 386.54,
			ProjectedGBHours: 474.5, Level: "exhausted", ResetsAt: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)},
		QuotaError: "q",
		Error:      "e",
		Repos: []RepoStorage{{Name: "darkmem", Visibility: "private", Covered: true, ArtifactCount: 1, ArtifactBytes: 2, CacheCount: 1, CacheBytes: 3,
			RetentionDays: 90, RetentionMaxDays: 400, Cleanup: "2d", CleanupSource: "global",
			Artifacts: []ArtifactItem{{ID: 7, Name: "dist", Bytes: 2, CreatedAt: now, ExpiresAt: &now, RunID: 102, Branch: "master"}},
			Caches:    []CacheItem{{ID: 9, Key: "node", Ref: "refs/heads/master", Bytes: 3, LastUsedAt: now}},
			Error:     "r"}},
		Operation: &GitHubOperation{ID: "o1", Kind: "delete-artifacts", Repo: "darkmem", Total: 2, Done: 1, Bytes: 2,
			Failed: []OperationFailure{{Repo: "darkmem", ID: 8, Error: "not in the last inventory"}}, StartedAt: now, FinishedAt: &now},
		Deletions: []Deletion{{At: now, Repo: "darkmem", Kind: "artifact", Name: "dist", Bytes: 2, Age: "3d", By: "cleanup"}},
	}
	b, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"checked_at":"2026-10-13T09:00:00Z",` +
		`"quota":{"month":"2026-10","read_at":"2026-10-13T09:00:00Z","included_gb_hours":363.28,"used_gb_hours":384.5,"actions_gb_hours":373.3,` +
		`"packages_gb_hours":11.2,"rate_gb_hours_per_hour":0.17,"estimated_gb_hours":386.54,` +
		`"projected_gb_hours":474.5,"level":"exhausted","resets_at":"2026-11-01T00:00:00Z"},` +
		`"quota_error":"q","error":"e",` +
		`"repos":[{"name":"darkmem","visibility":"private","covered":true,"artifact_count":1,"artifact_bytes":2,"cache_count":1,"cache_bytes":3,` +
		`"retention_days":90,"retention_max_days":400,"cleanup":"2d","cleanup_source":"global",` +
		`"artifacts":[{"id":7,"name":"dist","bytes":2,"created_at":"2026-10-13T09:00:00Z","expires_at":"2026-10-13T09:00:00Z","run_id":102,"branch":"master"}],` +
		`"caches":[{"id":9,"key":"node","ref":"refs/heads/master","bytes":3,"last_used_at":"2026-10-13T09:00:00Z"}],"error":"r"}],` +
		`"operation":{"id":"o1","kind":"delete-artifacts","repo":"darkmem","total":2,"done":1,"bytes":2,` +
		`"failed":[{"repo":"darkmem","id":8,"error":"not in the last inventory"}],"started_at":"2026-10-13T09:00:00Z","finished_at":"2026-10-13T09:00:00Z"},` +
		`"deletions":[{"at":"2026-10-13T09:00:00Z","repo":"darkmem","kind":"artifact","name":"dist","bytes":2,"age":"3d","by":"cleanup"}]}`
	if string(b) != want {
		t.Fatalf("json\n got %s\nwant %s", b, want)
	}
	b, _ = json.Marshal(GitHubStorage{Repos: []RepoStorage{}, Deletions: []Deletion{}})
	if string(b) != `{"repos":[],"deletions":[]}` {
		t.Fatalf("empty snapshot %s", b)
	}
	b, _ = json.Marshal(GitHubQuota{})
	if !strings.Contains(string(b), `"level":""`) || strings.Contains(string(b), "runs_out_at") {
		t.Fatalf("zero quota %s", b)
	}
}

func TestStatusStorageAlert(t *testing.T) {
	b, _ := json.Marshal(Status{})
	if strings.Contains(string(b), "github_storage") {
		t.Fatalf("status without a quota carries the alert: %s", b)
	}
	at := time.Date(2026, 10, 24, 6, 0, 0, 0, time.UTC)
	b, _ = json.Marshal(StorageAlert{Level: "warn", Month: "2026-10", UsedGBHours: 260, IncludedGBHours: 363.28, UsedPct: 72,
		RunsOutAt: &at, ResetsAt: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)})
	const want = `{"level":"warn","month":"2026-10","used_gb_hours":260,"included_gb_hours":363.28,"used_pct":72,` +
		`"runs_out_at":"2026-10-24T06:00:00Z","resets_at":"2026-11-01T00:00:00Z"}`
	if string(b) != want {
		t.Fatalf("alert %s", b)
	}
}

// A null repository value removes that override, so it must reach the
// daemon as a key with a nil value.
func TestGitHubStoragePatchDecodes(t *testing.T) {
	var p ConfigPatch
	err := json.Unmarshal([]byte(`{"github_storage":{"included_mb":2048,"warn_pct":[60,80],"cleanup_max_age":"","repos":{"darkmem":"6h","homelab":null}}}`), &p)
	if err != nil {
		t.Fatal(err)
	}
	g := p.GitHubStorage
	if g == nil || *g.IncludedMB != 2048 || len(*g.WarnPct) != 2 || (*g.WarnPct)[1] != 80 || *g.CleanupMaxAge != "" {
		t.Fatalf("patch %+v", g)
	}
	v, ok := g.Repos["homelab"]
	if *g.Repos["darkmem"] != "6h" || !ok || v != nil {
		t.Fatalf("repos %+v", g.Repos)
	}
	for _, tc := range []struct {
		v    any
		want string
	}{
		{DeleteArtifactsRequest{IDs: []int64{1, 2}}, `{"ids":[1,2]}`},
		{DeleteArtifactsRequest{OlderThan: "24h"}, `{"older_than":"24h"}`},
		{DeleteCachesRequest{All: true}, `{"all":true}`},
		{RetentionRequest{Days: 3}, `{"days":3}`},
		{OperationStarted{ID: "o1"}, `{"id":"o1"}`},
		{StoragePreview{Repo: "darkmem", Count: 2, Bytes: 9}, `{"repo":"darkmem","count":2,"bytes":9}`},
	} {
		if b, _ := json.Marshal(tc.v); string(b) != tc.want {
			t.Errorf("%+v → %s", tc.v, b)
		}
	}
}
