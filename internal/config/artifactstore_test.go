package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/endpoint"
)

const artifactBlock = `watch_repos: [docs]
artifact_store:
  endpoint: https://s3.example.sg:9000
  bucket: gha-artifacts
  region: ap-southeast-1
  cleanup_max_age: 2d
  capacity_gb: 50
  repos:
    darkmem: { enabled: true, cleanup: 3d }
    darkcloud: { cleanup: off }
    docs: { enabled: true }
`

func TestArtifactStoreDefaults(t *testing.T) {
	c, _, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	a := c.ArtifactStore
	if a.Configured() || a.RegionOrDefault() != "us-east-1" || a.GlobalCleanup() != 7*24*time.Hour || a.Enabled("darkmem") {
		t.Fatalf("defaults: configured %v region %q cleanup %v enabled %v", a.Configured(), a.RegionOrDefault(), a.GlobalCleanup(), a.Enabled("darkmem"))
	}
	if age, source := a.CleanupFor("darkmem"); age != 7*24*time.Hour || source != "default" {
		t.Fatalf("cleanup %v %q", age, source)
	}
	js, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(js), "artifact_store") {
		t.Fatalf("empty block serialised to JSON: %s", js)
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "artifact_store") {
		t.Fatalf("empty block saved to YAML:\n%s", data)
	}
}

func TestArtifactStoreRoundTrips(t *testing.T) {
	c, _, err := Parse([]byte(sample + artifactBlock))
	if err != nil {
		t.Fatal(err)
	}
	check := func(what string, c *Config) {
		t.Helper()
		a := c.ArtifactStore
		if !a.Configured() || a.RegionOrDefault() != "ap-southeast-1" || a.CapacityGB != 50 || a.GlobalCleanup() != 48*time.Hour {
			t.Fatalf("%s: %+v", what, a)
		}
		for _, tc := range []struct {
			repo    string
			enabled bool
			age     time.Duration
			source  string
		}{
			{"DarkMem", true, 72 * time.Hour, "override"},
			{"darkcloud", false, 0, "override"},
			{"docs", true, 48 * time.Hour, "global"},
			{"homelab", false, 48 * time.Hour, "global"},
		} {
			if got := a.Enabled(tc.repo); got != tc.enabled {
				t.Fatalf("%s: %s enabled %v, want %v", what, tc.repo, got, tc.enabled)
			}
			if age, source := a.CleanupFor(tc.repo); age != tc.age || source != tc.source {
				t.Fatalf("%s: %s cleanup %v %q, want %v %q", what, tc.repo, age, source, tc.age, tc.source)
			}
		}
		if r, ok := a.Repo("DARKCLOUD"); !ok || r != (ArtifactRepo{Cleanup: "off"}) {
			t.Fatalf("%s: repo %+v %v", what, r, ok)
		}
		if _, ok := a.Repo("homelab"); ok {
			t.Fatalf("%s: found an entry for homelab", what)
		}
	}
	check("parsed", c)
	check("clone", c.Clone())
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	back, _, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	check("loaded", back)
	js, _ := json.Marshal(c.ArtifactStore)
	const want = `{"endpoint":"https://s3.example.sg:9000","bucket":"gha-artifacts","region":"ap-southeast-1","cleanup_max_age":"2d","capacity_gb":50,` +
		`"repos":{"darkcloud":{"cleanup":"off"},"darkmem":{"enabled":true,"cleanup":"3d"},"docs":{"enabled":true}}}`
	if string(js) != want {
		t.Fatalf("json %s", js)
	}
}

func TestArtifactStoreGlobalCleanup(t *testing.T) {
	for _, tc := range []struct {
		maxAge string
		age    time.Duration
		source string
	}{
		{"", 7 * 24 * time.Hour, "default"},
		{"off", 0, "global"},
		{"36h", 36 * time.Hour, "global"},
		{"30d", 30 * 24 * time.Hour, "global"},
	} {
		a := ArtifactStore{CleanupMaxAge: tc.maxAge}
		if got := a.GlobalCleanup(); got != tc.age {
			t.Errorf("%q: GlobalCleanup %v, want %v", tc.maxAge, got, tc.age)
		}
		if age, source := a.CleanupFor("darkmem"); age != tc.age || source != tc.source {
			t.Errorf("%q: CleanupFor %v %q, want %v %q", tc.maxAge, age, source, tc.age, tc.source)
		}
	}
	a := ArtifactStore{CleanupMaxAge: "off", Repos: map[string]ArtifactRepo{"darkmem": {Enabled: true}}}
	if age, source := a.CleanupFor("darkmem"); age != 0 || source != "global" {
		t.Errorf("an entry without cleanup: %v %q, want the global age", age, source)
	}
}

func TestArtifactStoreValidation(t *testing.T) {
	const bucketErr = "artifact_store.bucket must be 3 to 63 lower-case letters, digits, '.' or '-', starting and ending with a letter or digit, such as gha-artifacts"
	for _, tc := range []struct {
		block string
		want  string
	}{
		{"  endpoint: http://s3.example.sg\n", "artifact_store.endpoint: " + endpoint.MsgHTTPName},
		{"  endpoint: http://100.64.0.1:9000\n", "artifact_store.endpoint: " + endpoint.MsgHTTPAddr},
		{"  endpoint: s3.example.sg\n", "artifact_store.endpoint: " + endpoint.MsgForm},
		{"  endpoint: https://\n", "artifact_store.endpoint: " + endpoint.MsgForm},
		{"  endpoint: https://s3.example.sg/\n", "artifact_store.endpoint: " + endpoint.MsgExtra},
		{"  endpoint: https://s3.example.sg/minio\n", "artifact_store.endpoint: " + endpoint.MsgExtra},
		{"  endpoint: https://s3.example.sg?x=1\n", "artifact_store.endpoint: " + endpoint.MsgExtra},
		{"  endpoint: https://key:secret@s3.example.sg\n", "artifact_store.endpoint: " + endpoint.MsgExtra},
		{"  endpoint: https://s3.example.sg#top\n", "artifact_store.endpoint: " + endpoint.MsgExtra},
		{"  bucket: ab\n", bucketErr},
		{"  bucket: GHA-Artifacts\n", bucketErr},
		{"  bucket: gha_artifacts\n", bucketErr},
		{"  bucket: -gha\n", bucketErr},
		{"  bucket: gha..artifacts\n", bucketErr},
		{"  bucket: 192.168.1.10\n", bucketErr},
		{"  bucket: " + strings.Repeat("a", 64) + "\n", bucketErr},
		{"  capacity_gb: -1\n", "artifact_store.capacity_gb must not be negative"},
		{"  cleanup_max_age: 23h\n", "artifact_store.cleanup_max_age must be off or a duration of at least 24h"},
		{"  cleanup_max_age: never\n", "artifact_store.cleanup_max_age must be off or a duration of at least 24h"},
		{"  repos:\n    darkmem: { cleanup: 59m }\n", "artifact_store.repos: darkmem: cleanup must be off or a duration of at least 1h"},
		{"  repos:\n    darkmem: { cleanup: soon }\n", "artifact_store.repos: darkmem: cleanup must be off or a duration of at least 1h"},
		{"  repos:\n    \"\": { enabled: true }\n", "artifact_store.repos must not contain an empty name"},
		{"  repos:\n    darkraise/darkmem: { enabled: true }\n", "artifact_store.repos: darkraise/darkmem must be a repository name under owner, " + RepoNameRule},
		{"  repos:\n    darkmem: { enabled: true }\n    DarkMem: { cleanup: 2h }\n", "artifact_store.repos: DarkMem and darkmem differ only by case"},
	} {
		_, _, err := Parse([]byte(sample + "artifact_store:\n" + tc.block))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: got %v, want %q", tc.block, err, tc.want)
		}
	}
	for _, block := range []string{
		"  endpoint: https://s3.example.sg\n  bucket: gha-artifacts\n",
		"  endpoint: https://[2001:db8::1]:9000\n  bucket: a.b-c\n",
		"  endpoint: http://192.168.0.105:9000\n  bucket: gha-artifacts\n",
		"  bucket: " + strings.Repeat("a", 63) + "\n  capacity_gb: 0\n",
		"  cleanup_max_age: off\n",
		"  cleanup_max_age: 24h\n",
		"  repos:\n    DarkMem: { enabled: true, cleanup: 1h }\n    darkcloud: { cleanup: off }\n",
		"  repos:\n    homelab: { enabled: true }\n",
	} {
		if _, _, err := Parse([]byte(sample + "artifact_store:\n" + block)); err != nil {
			t.Errorf("%q rejected: %v", block, err)
		}
	}
}

func TestArtifactStoreEndpointVectors(t *testing.T) {
	b, err := os.ReadFile("../endpoint/testdata/vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var vectors []struct {
		Endpoint string `json:"endpoint"`
		Error    string `json:"error"`
	}
	if err := json.Unmarshal(b, &vectors); err != nil {
		t.Fatal(err)
	}
	for _, v := range vectors {
		// An empty endpoint means the store is not set up, which is not an error here.
		if v.Endpoint == "" {
			continue
		}
		var want []string
		if v.Error != "" {
			want = []string{"artifact_store.endpoint: " + v.Error}
		}
		if got := (ArtifactStore{Endpoint: v.Endpoint}).validate(); !slices.Equal(got, want) {
			t.Errorf("endpoint %q: got %q, want %q", v.Endpoint, got, want)
		}
	}
}
