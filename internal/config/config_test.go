package config

import (
	"encoding/json"
	"errors"
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
	over := MaxRunners + 1
	cases := map[string]func(c *Config){
		"mode must be":                 func(c *Config) { c.Mode = "fast" },
		"global_max must be >= 1":      func(c *Config) { c.GlobalMax = 0 },
		"global_max must be <= 99":     func(c *Config) { c.GlobalMax = over },
		"duplicate repo darkmem":       func(c *Config) { c.Repos = append(c.Repos, Repo{Name: "darkmem"}) },
		"darkmem: max must be >= 0":    func(c *Config) { c.Repos[1].Max = &neg },
		"darkmem: max must be <= 99":   func(c *Config) { c.Repos[1].Max = &over },
		"darkmem: warm must be <= 99":  func(c *Config) { c.Repos[1].Warm = &over },
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

		`labels: "a b" must not contain spaces or commas`:             func(c *Config) { c.Labels = []string{"a b"} },
		`labels: "a,b" must not contain spaces or commas`:             func(c *Config) { c.Labels = []string{"a,b"} },
		`labels: " " must not be empty`:                               func(c *Config) { c.Labels = []string{"homelab", " "} },
		`labels: "Docker" is listed twice`:                            func(c *Config) { c.Labels = []string{"docker", "Docker"} },
		`labels: "x64" is added by ghr`:                               func(c *Config) { c.Labels = []string{"x64"} },
		`darkcloud: labels: "Linux" is added by ghr`:                  func(c *Config) { c.Repos[0].Labels = []string{"darkcloud-linux", "Linux"} },
		`darkcloud: labels: "a\tb" must not contain spaces or commas`: func(c *Config) { c.Repos[0].Labels = []string{"a\tb"} },
		"labels: \"" + strings.Repeat("a", 101) + "\" must be at most 100 characters": func(c *Config) {
			c.Labels = []string{strings.Repeat("a", 101)}
		},
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

// A config.yaml written by hand above the cap is lowered on load, with one
// warning for each value, and what was loaded saves and loads again.
func TestParseClampsRunnerCounts(t *testing.T) {
	c, warnings, err := Parse([]byte(`
owner: darkraise
mode: all
global_max: 500
labels: [homelab]
repos:
  - name: darkcloud
    max: 120
    warm: 100
  - name: darkmem
    max: 99
    warm: 0
`))
	if err != nil {
		t.Fatal(err)
	}
	want := "global_max 500 is above 99; using 99|darkcloud: max 120 is above 99; using 99|darkcloud: warm 100 is above 99; using 99"
	if got := strings.Join(warnings, "|"); got != want {
		t.Fatalf("warnings\n got %s\nwant %s", got, want)
	}
	cloud, mem := c.Repo("darkcloud"), c.Repo("darkmem")
	if c.GlobalMax != MaxRunners || *cloud.Max != MaxRunners || *cloud.Warm != MaxRunners || *mem.Max != 99 || *mem.Warm != 0 {
		t.Fatalf("global_max %d, darkcloud %d/%d, darkmem %d/%d", c.GlobalMax, *cloud.Max, *cloud.Warm, *mem.Max, *mem.Warm)
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := Save(path, c); err != nil {
		t.Fatalf("saving a clamped config: %v", err)
	}
	back, warnings, err := Load(path)
	if err != nil || len(warnings) != 0 {
		t.Fatalf("loading what was saved: warnings %v, err %v", warnings, err)
	}
	if back.GlobalMax != MaxRunners || *back.Repo("darkcloud").Max != MaxRunners || *back.Repo("darkcloud").Warm != MaxRunners {
		t.Fatalf("saved file: %+v", back)
	}
}

func TestValidLabelsPass(t *testing.T) {
	c, _, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	c.Labels = []string{" docker ", strings.Repeat("é", 100), "Docker-2"}
	c.Repos[0].Labels = []string{"docker-2", "darkcloud-linux"}
	if _, err := c.Validate(); err != nil {
		t.Fatalf("valid labels rejected: %v", err)
	}
}

func TestValidateNamesOnlyTheFirstBadLabel(t *testing.T) {
	c, _, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	c.Labels = []string{"a b", "linux", "c,d"}
	_, err = c.Validate()
	if want := `labels: "a b" must not contain spaces or commas`; err == nil || err.Error() != want {
		t.Fatalf("error = %v, want %s", err, want)
	}
}

// A config.yaml written by hand with bad labels loses them on load, with one
// warning for each, and what was loaded saves and loads again unchanged.
func TestParseDropsInvalidLabels(t *testing.T) {
	c, warnings, err := Parse([]byte(`
owner: darkraise
labels: [homelab, "a b", Docker, docker, linux, ""]
repos:
  - name: darkcloud
    labels: [homelab, "x,y", self-hosted]
  - name: darkmem
`))
	if err != nil {
		t.Fatal(err)
	}
	want := `labels: "a b" is not a valid label; ignored|` +
		`labels: "docker" is listed twice; ignored|` +
		`labels: "linux" is not a valid label; ignored|` +
		`labels: "" is not a valid label; ignored|` +
		`darkcloud: labels: "x,y" is not a valid label; ignored|` +
		`darkcloud: labels: "self-hosted" is not a valid label; ignored`
	if got := strings.Join(warnings, "|"); got != want {
		t.Fatalf("warnings\n got %s\nwant %s", got, want)
	}
	if got := strings.Join(c.Labels, ","); got != "homelab,Docker" {
		t.Fatalf("labels = %s", got)
	}
	if got := strings.Join(c.Repo("darkcloud").Labels, ","); got != "homelab" {
		t.Fatalf("darkcloud labels = %s", got)
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := Save(path, c); err != nil {
		t.Fatalf("saving a repaired config: %v", err)
	}
	back, warnings, err := Load(path)
	if err != nil || len(warnings) != 0 {
		t.Fatalf("loading what was saved: warnings %v, err %v", warnings, err)
	}
	if got := strings.Join(back.Labels, ","); got != "homelab,Docker" {
		t.Fatalf("saved labels = %s", got)
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

func saveOwner(t *testing.T, path, owner string) error {
	t.Helper()
	c, _, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	c.Owner = owner
	return Save(path, c)
}

func isSymlink(t *testing.T, path string) bool {
	t.Helper()
	fi, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Mode()&os.ModeSymlink != 0
}

func TestSaveThroughSymlinkReplacesTheTarget(t *testing.T) {
	dir := t.TempDir()
	targetDir := filepath.Join(dir, "elsewhere")
	if err := os.Mkdir(targetDir, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(targetDir, "config.yaml")
	if err := os.WriteFile(target, []byte("owner: before\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "config.yaml")
	if err := os.Symlink(filepath.Join("elsewhere", "config.yaml"), link); err != nil {
		t.Fatal(err)
	}
	if err := saveOwner(t, link, "after"); err != nil {
		t.Fatal(err)
	}
	if !isSymlink(t, link) {
		t.Fatal("the link was replaced by a file")
	}
	if got, _ := os.Readlink(link); got != filepath.Join("elsewhere", "config.yaml") {
		t.Fatalf("link now points at %q", got)
	}
	back, _, err := Load(target)
	if err != nil || back.Owner != "after" {
		t.Fatalf("target after save: owner %q, err %v", back.Owner, err)
	}
	if fi, _ := os.Stat(target); fi.Mode().Perm() != 0o600 {
		t.Fatalf("target mode = %v, want 0600", fi.Mode().Perm())
	}
	for d, want := range map[string]int{dir: 2, targetDir: 1} {
		entries, _ := os.ReadDir(d)
		if len(entries) != want {
			t.Fatalf("%s holds %v, want %d entries (a temp file was left)", d, entries, want)
		}
	}
}

// The target can disappear after the file was loaded; the link must still
// stay a link and the target must be created.
func TestSaveThroughDanglingSymlinkCreatesTheTarget(t *testing.T) {
	dir := t.TempDir()
	targetDir := filepath.Join(dir, "elsewhere")
	if err := os.Mkdir(targetDir, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "config.yaml")
	if err := os.Symlink(filepath.Join("elsewhere", "config.yaml"), link); err != nil {
		t.Fatal(err)
	}
	if err := saveOwner(t, link, "after"); err != nil {
		t.Fatal(err)
	}
	if !isSymlink(t, link) {
		t.Fatal("the link was replaced by a file")
	}
	back, _, err := Load(filepath.Join(targetDir, "config.yaml"))
	if err != nil || back.Owner != "after" {
		t.Fatalf("target after save: owner %q, err %v", back.Owner, err)
	}
}

func TestSaveThroughLinkChain(t *testing.T) {
	dir := t.TempDir()
	realDir := filepath.Join(dir, "real")
	if err := os.Mkdir(realDir, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(realDir, "config.yaml")
	middle := filepath.Join(dir, "middle.yaml")
	if err := os.Symlink(target, middle); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "config.yaml")
	if err := os.Symlink("middle.yaml", link); err != nil {
		t.Fatal(err)
	}
	if err := saveOwner(t, link, "after"); err != nil {
		t.Fatal(err)
	}
	if !isSymlink(t, link) || !isSymlink(t, middle) {
		t.Fatal("a link in the chain was replaced by a file")
	}
	back, _, err := Load(target)
	if err != nil || back.Owner != "after" {
		t.Fatalf("target after save: owner %q, err %v", back.Owner, err)
	}
}

func TestSaveWritesNothingWhenTheLinkCannotBeResolved(t *testing.T) {
	for name, build := range map[string]func(t *testing.T, dir string) string{
		"loop": func(t *testing.T, dir string) string {
			a, b := filepath.Join(dir, "a.yaml"), filepath.Join(dir, "b.yaml")
			if err := os.Symlink("b.yaml", a); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("a.yaml", b); err != nil {
				t.Fatal(err)
			}
			return a
		},
		"missing directory": func(t *testing.T, dir string) string {
			link := filepath.Join(dir, "config.yaml")
			if err := os.Symlink(filepath.Join("missing", "config.yaml"), link); err != nil {
				t.Fatal(err)
			}
			return link
		},
	} {
		dir := t.TempDir()
		link := build(t, dir)
		before, _ := os.ReadDir(dir)
		if err := saveOwner(t, link, "after"); err == nil {
			t.Errorf("%s: Save returned nil", name)
		}
		after, _ := os.ReadDir(dir)
		if len(after) != len(before) {
			t.Errorf("%s: directory held %v, now %v", name, before, after)
		}
		for _, e := range before {
			if fi, err := os.Lstat(filepath.Join(dir, e.Name())); err != nil || fi.Mode()&os.ModeSymlink == 0 {
				t.Errorf("%s: %s is no longer a link (%v)", name, e.Name(), err)
			}
		}
	}
}

func stubSync(t *testing.T, stub func(*os.File) error) {
	t.Helper()
	orig := syncFile
	syncFile = stub
	t.Cleanup(func() { syncFile = orig })
}

func TestSaveSyncsBeforeTheRename(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("owner: before\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var calls int
	var targetAtSync, tempAtSync string
	stubSync(t, func(f *os.File) error {
		calls++
		b, _ := os.ReadFile(path)
		targetAtSync = string(b)
		b, _ = os.ReadFile(f.Name())
		tempAtSync = string(b)
		return nil
	})
	if err := saveOwner(t, path, "after"); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("sync ran %d times, want 1", calls)
	}
	if targetAtSync != "owner: before\n" {
		t.Fatalf("config.yaml held %q when the temp file was synced, want the old content", targetAtSync)
	}
	if !strings.Contains(tempAtSync, "owner: after") {
		t.Fatalf("temp file held %q when synced, want the new config", tempAtSync)
	}
}

func TestSaveKeepsTheFileWhenSyncFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("owner: before\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stubSync(t, func(*os.File) error { return errors.New("disk full") })
	if err := saveOwner(t, path, "after"); err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("Save returned %v, want the sync error", err)
	}
	if b, _ := os.ReadFile(path); string(b) != "owner: before\n" {
		t.Fatalf("config.yaml now holds %q", b)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("directory holds %v, want only config.yaml", entries)
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

func TestParseEmptyFileLoadsDefaults(t *testing.T) {
	for name, y := range map[string]string{
		"empty":         "",
		"blank":         "\n\n",
		"comments only": "# owner: darkraise\n# mode: all\n",
	} {
		c, warnings, err := Parse([]byte(y))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if len(warnings) != 0 || c.Mode != ModeQueue || c.GlobalMax != 2 || c.PollInterval.D() != 10*time.Second {
			t.Errorf("%s: warnings %v, config %+v", name, warnings, c)
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

// allow_public is the only thing that lets ghr serve a public repository, so
// it must survive a load, a clone and a save, and stay out of the file and the
// API's JSON when it is off.
func TestAllowPublicRoundTrips(t *testing.T) {
	c, _, err := Parse([]byte(sample + "    allow_public: true\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !c.Repo("darkmem").AllowPublic || c.Repo("darkcloud").AllowPublic {
		t.Fatalf("parsed %+v", c.Repos)
	}
	if !c.Clone().Repo("darkmem").AllowPublic {
		t.Fatal("clone lost allow_public")
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	back, _, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !back.Repo("darkmem").AllowPublic || back.Repo("darkcloud").AllowPublic {
		t.Fatalf("saved %+v", back.Repos)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(raw), "allow_public: true"); n != 1 || strings.Count(string(raw), "allow_public") != 1 {
		t.Fatalf("allow_public must be written once, for darkmem only:\n%s", raw)
	}
	data, err := json.Marshal(back)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), `"allow_public":true`) != 1 || strings.Count(string(data), "allow_public") != 1 {
		t.Fatalf("JSON %s", data)
	}
}
