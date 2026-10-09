package config

import (
	"encoding/json"
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
		"darkmem: a repo being removed must stay paused":       func(c *Config) { c.Repos[1].Removing = true },
		"runner_limits.memory_max":                             func(c *Config) { c.RunnerLimits.MemoryMax = "6 gigs" },
		"runner_limits.cpu_quota":                              func(c *Config) { c.RunnerLimits.CPUQuota = "2" },
		"poll_interval must be >= 5s":                          func(c *Config) { c.PollInterval = Duration(time.Millisecond) },
		"history_retention must be >= 1d":                      func(c *Config) { c.HistoryRetention = Duration(time.Second) },
		"watch_repos must not contain an empty name":           func(c *Config) { c.WatchRepos = []string{" "} },
		"watch_repos:  docs  must not have surrounding spaces": func(c *Config) { c.WatchRepos = []string{" docs "} },
		"watch_repos: other/docs must be a repository name":    func(c *Config) { c.WatchRepos = []string{"other/docs"} },
		"duplicate watched repo Docs":                          func(c *Config) { c.WatchRepos = []string{"docs", "Docs"} },
		"watch_repos: DarkMem is already a configured repo":    func(c *Config) { c.WatchRepos = []string{"DarkMem"} },
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

// The floors themselves are valid; only values below them are rejected.
func TestDurationFloorsAreInclusive(t *testing.T) {
	c, _, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	c.PollInterval, c.HistoryRetention = Duration(MinPollInterval), Duration(MinHistoryRetention)
	if _, err := c.Validate(); err != nil {
		t.Fatalf("floor values rejected: %v", err)
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

func TestParseRejectsUnknownKeysAndExtraDocuments(t *testing.T) {
	for name, y := range map[string]string{
		"unknown repo key": sample + "    maax: 1\n",
		"unknown top key":  "poll_intervall: 5s\n" + sample,
		"second document":  sample + "---\nowner: other\n",
	} {
		if _, _, err := Parse([]byte(y)); err == nil {
			t.Errorf("%s accepted", name)
		}
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

func TestParseDurationDayBounds(t *testing.T) {
	if d, err := ParseDuration("106751d"); err != nil || d.D() != 106751*24*time.Hour {
		t.Fatalf("106751d = %v, %v", d, err)
	}
	for _, s := range []string{"106752d", "213504d", "-106752d", "xd"} {
		if _, err := ParseDuration(s); err == nil {
			t.Errorf("%s accepted", s)
		}
	}
}

func TestWebBlockRoundTrips(t *testing.T) {
	c, _, err := Parse([]byte(sample + "web:\n  listen: 0.0.0.0:8080\n  hosts: [ghr.lan]\n"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Web.Listen != "0.0.0.0:8080" || len(c.Web.Hosts) != 1 || c.Web.Hosts[0] != "ghr.lan" {
		t.Fatalf("web %+v", c.Web)
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	back, _, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if back.Web.Listen != "0.0.0.0:8080" || len(back.Web.Hosts) != 1 || back.Web.Hosts[0] != "ghr.lan" {
		t.Fatalf("round trip lost web: %+v", back.Web)
	}
	clone := c.Clone()
	if clone.Web.Listen != "0.0.0.0:8080" || len(clone.Web.Hosts) != 1 {
		t.Fatalf("clone lost web: %+v", clone.Web)
	}
}

func TestEmptyWebIsOmitted(t *testing.T) {
	c, _, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	js, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(js), `"web"`) {
		t.Fatalf("empty web serialised to JSON: %s", js)
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "web:") {
		t.Fatalf("empty web saved to YAML:\n%s", data)
	}
}

func TestWebValidation(t *testing.T) {
	bad := []Web{
		{Listen: "0.0.0.0:http"},
		{Listen: "8080"},
		{Listen: "0.0.0.0:0"},
		{Listen: "0.0.0.0:70000"},
		{Listen: "0.0.0.0:8080", Hosts: []string{"ghr.lan:8080"}},
		{Listen: "0.0.0.0:8080", Hosts: []string{""}},
		{Listen: "0.0.0.0:8080", Hosts: []string{"https://ghr.lan"}},
		{Listen: "0.0.0.0:8080", Hosts: []string{"ghr lan"}},
		{Listen: "0.0.0.0:8080", Hosts: []string{"ghr.lan\t"}},
		{Listen: "0.0.0.0:8080", Hosts: []string{"user@ghr.lan"}},
		{Listen: "0.0.0.0:8080", Hosts: []string{"-bad.lan"}},
		{Listen: "0.0.0.0:8080", Hosts: []string{"bad_name.lan"}},
		{Listen: "0.0.0.0:8080", Hosts: []string{"ghr..lan"}},
		{Listen: "0.0.0.0:8080", Hosts: []string{strings.Repeat("a.", 127) + "lan"}},
	}
	for _, w := range bad {
		c, _, err := Parse([]byte(sample))
		if err != nil {
			t.Fatal(err)
		}
		c.Web = w
		if _, err := c.Validate(); err == nil || !strings.Contains(err.Error(), "web.") {
			t.Errorf("%+v: want a web error, got %v", w, err)
		}
	}
	good := []Web{
		{},
		{Listen: ":8080"},
		{Listen: "[::]:8080"},
		{Listen: "0.0.0.0:8080", Hosts: []string{"ghr.lan", "GHR.example.com", "ghr-1", "localhost"}},
	}
	for _, w := range good {
		c, _, err := Parse([]byte(sample))
		if err != nil {
			t.Fatal(err)
		}
		c.Web = w
		if _, err := c.Validate(); err != nil {
			t.Errorf("%+v rejected: %v", w, err)
		}
	}
}

func TestEmptyOwnerValidates(t *testing.T) {
	c, _, err := Parse([]byte(strings.Replace(sample, "owner: darkraise", `owner: ""`, 1)))
	if err != nil || c.Owner != "" {
		t.Fatalf("empty owner: err %v owner %q", err, c.Owner)
	}
	for owner, want := range map[string]string{`"  darkraise "`: "darkraise", `" "`: ""} {
		c, _, err := Parse([]byte(strings.Replace(sample, "owner: darkraise", "owner: "+owner, 1)))
		if err != nil {
			t.Fatalf("owner %s: %v", owner, err)
		}
		if c.Owner != want {
			t.Fatalf("owner %s: got %q, want %q", owner, c.Owner, want)
		}
	}
}

func TestWatchReposRoundTripAndLookup(t *testing.T) {
	c, _, err := Parse([]byte(sample + "watch_repos: [docs]\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !c.Watched("DOCS") || c.Watched("darkmem") {
		t.Fatalf("Watched: %v", c.WatchRepos)
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	back, _, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(back.WatchRepos) != 1 || back.WatchRepos[0] != "docs" {
		t.Fatalf("round trip: %v", back.WatchRepos)
	}
	if d := back.Clone(); len(d.WatchRepos) != 1 {
		t.Fatalf("clone lost watch_repos: %v", d.WatchRepos)
	}

	plain, _, _ := Parse([]byte(sample))
	data, _ := json.Marshal(plain)
	if strings.Contains(string(data), "watch_repos") {
		t.Fatalf("empty watch_repos marshalled: %s", data)
	}
	if err := Save(path, plain); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "watch_repos") {
		t.Fatalf("empty watch_repos saved:\n%s", raw)
	}
}
