package model

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestArtifactStoreJSON(t *testing.T) {
	now := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	st := ArtifactStore{
		Configured: true, Endpoint: "https://s3.example.sg", Bucket: "gha-artifacts", Region: "us-east-1",
		AdminKey:  &ArtifactKeyInfo{ID: "AKIA…WXYZ", SetAt: now},
		JobKey:    &ArtifactKeyInfo{ID: "…YZ", SetAt: now},
		CheckedAt: &now, Error: "e", UsedBytes: 30, Count: 1, CapacityBytes: 50_000_000_000, OtherBytes: 10,
		Repos: []ArtifactStoreRepo{{Name: "darkmem", Private: true, Enabled: true, State: "error", StateError: "boom",
			Cleanup: "7d", CleanupSource: "default", Bytes: 20, Count: 1,
			Items: []ArtifactStoreItem{{Key: "darkraise/darkmem/1932/dist.zip", Name: "dist", RunID: 1932, Attempt: 2,
				Workflow: "ci", Job: "build", Branch: "master", SHA: "abc", Bytes: 20, Files: 3, Digest: "sha256:00",
				UploadedAt: now, ExpiresAt: &now}}}},
		Operation: &GitHubOperation{ID: "o1", Kind: "delete-objects", Repo: "darkmem", Total: 1, Failed: []OperationFailure{}, StartedAt: now},
		Deletions: []Deletion{{At: now, Repo: "darkmem", Kind: "artifact", Name: "dist", Bytes: 20, Age: "8d", By: "expired", Store: "rustfs"}},
	}
	b, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"configured":true,"endpoint":"https://s3.example.sg","bucket":"gha-artifacts","region":"us-east-1",` +
		`"admin_key":{"id":"AKIA…WXYZ","set_at":"2026-10-15T09:00:00Z"},"job_key":{"id":"…YZ","set_at":"2026-10-15T09:00:00Z"},` +
		`"checked_at":"2026-10-15T09:00:00Z","error":"e","used_bytes":30,"count":1,"capacity_bytes":50000000000,"other_bytes":10,` +
		`"repos":[{"name":"darkmem","private":true,"enabled":true,"state":"error","state_error":"boom",` +
		`"cleanup":"7d","cleanup_source":"default","bytes":20,"count":1,` +
		`"items":[{"key":"darkraise/darkmem/1932/dist.zip","name":"dist","run_id":1932,"attempt":2,` +
		`"workflow":"ci","job":"build","branch":"master","sha":"abc","bytes":20,"files":3,"digest":"sha256:00",` +
		`"uploaded_at":"2026-10-15T09:00:00Z","expires_at":"2026-10-15T09:00:00Z"}]}],` +
		`"operation":{"id":"o1","kind":"delete-objects","repo":"darkmem","total":1,"done":0,"bytes":0,"failed":[],"started_at":"2026-10-15T09:00:00Z"},` +
		`"deletions":[{"at":"2026-10-15T09:00:00Z","repo":"darkmem","kind":"artifact","name":"dist","bytes":20,"age":"8d","by":"expired","store":"rustfs"}]}`
	if string(b) != want {
		t.Fatalf("json\n got %s\nwant %s", b, want)
	}
	b, _ = json.Marshal(ArtifactStore{Repos: []ArtifactStoreRepo{}, Deletions: []Deletion{}})
	if string(b) != `{"configured":false,"used_bytes":0,"count":0,"repos":[],"deletions":[]}` {
		t.Fatalf("not configured %s", b)
	}
}

func TestStatusArtifactStoreSummary(t *testing.T) {
	b, _ := json.Marshal(Status{})
	if strings.Contains(string(b), "artifact_store") {
		t.Fatalf("status without self-hosted storage carries its summary: %s", b)
	}
	b, _ = json.Marshal(ArtifactStoreSummary{UsedBytes: 12_400_000_000, CapacityBytes: 50_000_000_000, ReposOn: 2, Error: "no admin key set"})
	const want = `{"used_bytes":12400000000,"capacity_bytes":50000000000,"repos_on":2,"error":"no admin key set"}`
	if string(b) != want {
		t.Fatalf("summary %s", b)
	}
}

func TestDeletionStoreIsOmittedForGitHub(t *testing.T) {
	b, _ := json.Marshal(Deletion{By: "cleanup"})
	if strings.Contains(string(b), "store") {
		t.Fatalf("a GitHub deletion carries a store: %s", b)
	}
}

func TestArtifactRequestsDecode(t *testing.T) {
	var r ArtifactRepoRequest
	if err := json.Unmarshal([]byte(`{"enabled":false,"cleanup":"off"}`), &r); err != nil {
		t.Fatal(err)
	}
	if r.Enabled == nil || *r.Enabled || r.Cleanup == nil || *r.Cleanup != "off" {
		t.Fatalf("repo request %+v", r)
	}
	var k ArtifactKeyRequest
	if err := json.Unmarshal([]byte(`{"which":"job","id":"AKIA","secret":"s"}`), &k); err != nil || k != (ArtifactKeyRequest{Which: "job", ID: "AKIA", Secret: "s"}) {
		t.Fatalf("key request %+v, %v", k, err)
	}
	var l ArtifactLinkRequest
	if err := json.Unmarshal([]byte(`{"key":"darkraise/darkmem/1/dist.zip","ttl":"24h"}`), &l); err != nil || l.TTL != "24h" {
		t.Fatalf("link request %+v, %v", l, err)
	}
}
