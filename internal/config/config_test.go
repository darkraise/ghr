package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const sample = `
owner: darkraise
mode: queue
global_max: 2
history_retention: 30d
labels: [homelab, Docker]
repos:
  - name: darkcloud
    max: 1
    labels: [darkcloud-linux]
    cleanup_name_prefixes: [dc-e2e-]
  - name: darkmem
`

func TestParseAppliesDefaults(t *testing.T) {
	c, warnings, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings %v", warnings)
	}
	if c.PollInterval.D() != 10*time.Second || c.IdleTimeout.D() != 5*time.Minute {
		t.Fatalf("defaults not applied: %+v", c)
	}
	if c.HistoryRetention.D() != 30*24*time.Hour {
		t.Fatalf("day suffix not parsed: %v", c.HistoryRetention)
	}
	if c.BuildCacheKeep != "20GB" || c.RunnerLimits.MemoryMax != "6G" {
		t.Fatalf("defaults not applied: %+v", c)
	}
}

func TestEffectiveMaxDependsOnMode(t *testing.T) {
	c, _, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	mem := *c.Repo("darkmem")
	if got := c.EffectiveMax(mem); got != 1 {
		t.Fatalf("queue default max = %d, want 1", got)
	}
	c.Mode = ModeAll
	if got := c.EffectiveMax(mem); got != 0 {
		t.Fatalf("all default max = %d, want 0 (unlimited)", got)
	}
	if got := c.EffectiveMax(*c.Repo("darkcloud")); got != 1 {
		t.Fatalf("explicit max = %d, want 1", got)
	}
	if got := c.EffectiveWarm(mem); got != 1 {
		t.Fatalf("default warm = %d, want 1", got)
	}
}

func TestLabels(t *testing.T) {
	c, _, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(c.EffectiveLabels(*c.Repo("darkcloud")), ",")
	if got != "self-hosted,linux,x64,homelab,docker,darkcloud-linux" {
		t.Fatalf("effective labels = %s", got)
	}
}

func TestValidateErrors(t *testing.T) {
	neg := -1
	two := 2
	three := 3
	cases := map[string]func(c *Config){
		"owner is required":            func(c *Config) { c.Owner = "" },
		"mode must be":                 func(c *Config) { c.Mode = "fast" },
		"global_max must be >= 1":      func(c *Config) { c.GlobalMax = 0 },
		"duplicate repo darkmem":       func(c *Config) { c.Repos = append(c.Repos, Repo{Name: "darkmem"}) },
		"darkmem: max must be >= 0":    func(c *Config) { c.Repos[1].Max = &neg },
		"darkmem: warm must be <= max": func(c *Config) { c.Repos[1].Max = &two; c.Repos[1].Warm = &three },
		"needs at least one label":     func(c *Config) { c.Labels = nil; c.Repos[1].Labels = nil },
		"build_cache_keep":             func(c *Config) { c.BuildCacheKeep = "lots" },
		"disk_high_water":              func(c *Config) { c.DiskHighWater = 0 },
		"duplicate repo DarkMem":       func(c *Config) { c.Repos = append(c.Repos, Repo{Name: "DarkMem"}) },
		"darkcloud: warm must be <= max": func(c *Config) {
			one := 1
			c.Repos[0].Max = &one
			c.Repos[0].Warm = &two
		},
		"darkcloud: cleanup_name_prefixes must not contain an empty prefix": func(c *Config) {
			c.Repos[0].CleanupNamePrefixes = []string{"dc-e2e-", " "}
		},
		"darkmem: a repo being removed must stay paused": func(c *Config) { c.Repos[1].Removing = true },
		"runner_limits.memory_max":                       func(c *Config) { c.RunnerLimits.MemoryMax = "6 gigs" },
		"runner_limits.cpu_quota":                        func(c *Config) { c.RunnerLimits.CPUQuota = "2" },
	}
	for want, mutate := range cases {
		c, _, err := Parse([]byte(sample))
		if err != nil {
			t.Fatal(err)
		}
		mutate(c)
		if _, err := c.Validate(); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("want error containing %q, got %v", want, err)
		}
	}
}

func TestValidRunnerLimits(t *testing.T) {
	for _, l := range []RunnerLimits{
		{MemoryMax: "6G", CPUQuota: "200%"},
		{MemoryMax: "512M", CPUQuota: "50%"},
		{MemoryMax: "1073741824", CPUQuota: "100%"},
		{MemoryMax: "50%", CPUQuota: "400%"},
		{MemoryMax: "infinity", CPUQuota: "1%"},
	} {
		c, _, _ := Parse([]byte(sample))
		c.RunnerLimits = l
		if _, err := c.Validate(); err != nil {
			t.Errorf("%+v rejected: %v", l, err)
		}
	}
	for _, l := range []RunnerLimits{
		{MemoryMax: "", CPUQuota: "200%"},
		{MemoryMax: "0", CPUQuota: "200%"},
		{MemoryMax: "6GB", CPUQuota: "200%"},
		{MemoryMax: "150%", CPUQuota: "200%"},
		{MemoryMax: "6G", CPUQuota: ""},
		{MemoryMax: "6G", CPUQuota: "0%"},
		{MemoryMax: "6G", CPUQuota: "2.5"},
	} {
		c, _, _ := Parse([]byte(sample))
		c.RunnerLimits = l
		if _, err := c.Validate(); err == nil {
			t.Errorf("%+v accepted", l)
		}
	}
}

// warm is bounded only by an explicit max: an omitted max is unlimited in
// all mode (the only mode that uses warm), whatever the current mode is.
func TestWarmBoundOnlyByExplicitMax(t *testing.T) {
	two := 2
	for _, mode := range []string{ModeQueue, ModeAll} {
		c, _, _ := Parse([]byte(sample))
		c.Mode = mode
		c.Repos[1].Warm = &two
		if _, err := c.Validate(); err != nil {
			t.Errorf("%s mode, omitted max, warm 2: %v", mode, err)
		}
	}
}

func TestRepoLookupIgnoresCase(t *testing.T) {
	c, _, _ := Parse([]byte(sample))
	if r := c.Repo("DarkCloud"); r == nil || r.Name != "darkcloud" {
		t.Fatalf("Repo(DarkCloud) = %+v", r)
	}
}

func TestRemovingRepoRoundTrips(t *testing.T) {
	c, _, _ := Parse([]byte(sample))
	c.Repos[1].Paused = true
	c.Repos[1].Removing = true
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	back, _, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if r := back.Repo("darkmem"); !r.Paused || !r.Removing {
		t.Fatalf("removal intent lost: %+v", r)
	}
}

func TestPrefixWarningWhenMaxNotOne(t *testing.T) {
	c, _, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	two := 2
	c.Repos[0].Max = &two
	warnings, err := c.Validate()
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "darkcloud") {
		t.Fatalf("warnings = %v", warnings)
	}
}

func TestSaveRoundTrip(t *testing.T) {
	c, _, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	back, _, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if back.HistoryRetention.String() != "30d" || *back.Repo("darkcloud").Max != 1 || back.Repo("darkmem").Max != nil {
		t.Fatalf("round trip lost data: %+v", back)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatalf("temp file left behind: %v", entries)
	}
}

func TestSaveRejectsInvalid(t *testing.T) {
	c, _, _ := Parse([]byte(sample))
	c.GlobalMax = 0
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := Save(path, c); err == nil {
		t.Fatal("expected validation error")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("invalid config was written")
	}
}

func TestCloneIsDeep(t *testing.T) {
	c, _, _ := Parse([]byte(sample))
	d := c.Clone()
	*d.Repos[0].Max = 5
	d.Labels[0] = "x"
	if *c.Repos[0].Max != 1 || c.Labels[0] != "homelab" {
		t.Fatal("clone shares memory")
	}
}
