package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const storageBlock = `github_storage:
  included_mb: 2048
  warn_pct: [60, 80]
  cleanup_max_age: 2d
  repos:
    darkmem: { cleanup: 6h }
    homelab: { cleanup: off }
`

func TestGitHubStorageDefaults(t *testing.T) {
	c, _, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	g := c.GitHubStorage
	if g.Included() != 500 || g.Warn() != [2]int{70, 90} {
		t.Fatalf("defaults: included %d warn %v", g.Included(), g.Warn())
	}
	if age, source := g.CleanupFor("darkmem"); age != 0 || source != "none" {
		t.Fatalf("cleanup %v %q", age, source)
	}
	js, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(js), "github_storage") {
		t.Fatalf("default block serialised to JSON: %s", js)
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "github_storage") {
		t.Fatalf("default block saved to YAML:\n%s", data)
	}
}

func TestGitHubStorageRoundTrips(t *testing.T) {
	c, _, err := Parse([]byte(sample + storageBlock))
	if err != nil {
		t.Fatal(err)
	}
	check := func(what string, c *Config) {
		t.Helper()
		g := c.GitHubStorage
		if g.Included() != 2048 || g.Warn() != [2]int{60, 80} || g.CleanupMaxAge.D() != 48*time.Hour {
			t.Fatalf("%s: %+v", what, g)
		}
		for _, tc := range []struct {
			repo   string
			age    time.Duration
			source string
		}{
			{"DarkMem", 6 * time.Hour, "override"},
			{"homelab", 0, "override"},
			{"darkcloud", 48 * time.Hour, "global"},
		} {
			if age, source := g.CleanupFor(tc.repo); age != tc.age || source != tc.source {
				t.Fatalf("%s: %s cleanup %v %q, want %v %q", what, tc.repo, age, source, tc.age, tc.source)
			}
		}
		if o, ok := g.Override("HOMELAB"); !ok || o.Cleanup != "off" {
			t.Fatalf("%s: override %+v %v", what, o, ok)
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
	js, _ := json.Marshal(c.GitHubStorage)
	const want = `{"included_mb":2048,"warn_pct":[60,80],"cleanup_max_age":"2d","repos":{"darkmem":{"cleanup":"6h"},"homelab":{"cleanup":"off"}}}`
	if string(js) != want {
		t.Fatalf("json %s", js)
	}
}

func TestGitHubStorageValidation(t *testing.T) {
	for _, tc := range []struct {
		block string
		want  string
	}{
		{"  included_mb: -1\n", "github_storage.included_mb must be at least 1"},
		{"  warn_pct: [90, 70]\n", "github_storage.warn_pct must be two increasing percentages from 1 to 100, such as [70, 90]"},
		{"  warn_pct: [70]\n", "github_storage.warn_pct must be two increasing percentages from 1 to 100, such as [70, 90]"},
		{"  warn_pct: [0, 90]\n", "github_storage.warn_pct must be two increasing percentages from 1 to 100, such as [70, 90]"},
		{"  warn_pct: [70, 101]\n", "github_storage.warn_pct must be two increasing percentages from 1 to 100, such as [70, 90]"},
		{"  cleanup_max_age: 23h\n", "github_storage.cleanup_max_age must be at least 24h"},
		{"  repos:\n    darkmem: { cleanup: 59m }\n", "github_storage.repos: darkmem: cleanup must be off or a duration of at least 1h"},
		{"  repos:\n    darkmem: { cleanup: never }\n", "github_storage.repos: darkmem: cleanup must be off or a duration of at least 1h"},
		{"  repos:\n    \"\": { cleanup: off }\n", "github_storage.repos must not contain an empty name"},
		{"  repos:\n    darkraise/darkmem: { cleanup: off }\n", "github_storage.repos: darkraise/darkmem must be a repository name under owner, without a slash"},
		{"  repos:\n    darkmem: { cleanup: off }\n    DarkMem: { cleanup: 2h }\n", "github_storage.repos: DarkMem and darkmem differ only by case"},
	} {
		_, _, err := Parse([]byte(sample + "github_storage:\n" + tc.block))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: got %v, want %q", tc.block, err, tc.want)
		}
	}
	for _, block := range []string{
		"  included_mb: 1\n  warn_pct: [1, 100]\n  cleanup_max_age: 24h\n",
		"  repos:\n    darkmem: { cleanup: 1h }\n",
	} {
		if _, _, err := Parse([]byte(sample + "github_storage:\n" + block)); err != nil {
			t.Errorf("%q rejected: %v", block, err)
		}
	}
}

func TestCleanupText(t *testing.T) {
	for d, want := range map[time.Duration]string{
		0:                    "off",
		48 * time.Hour:       "2d",
		6 * time.Hour:        "6h",
		90 * time.Minute:     "90m",
		90*time.Minute + 1e9: "1h30m1s",
	} {
		if got := CleanupText(d); got != want {
			t.Errorf("CleanupText(%v) = %q, want %q", d, got, want)
		}
	}
	for s, want := range map[string]time.Duration{"off": 0, "2d": 48 * time.Hour, "6h": 6 * time.Hour} {
		if got, err := ParseCleanup(s); err != nil || got != want {
			t.Errorf("ParseCleanup(%q) = %v, %v", s, got, err)
		}
	}
	if _, err := ParseCleanup("soon"); err == nil {
		t.Error("ParseCleanup accepted soon")
	}
}
