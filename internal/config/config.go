// Package config loads, validates and atomically saves /etc/ghr/config.yaml.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

const (
	ModeQueue = "queue"
	ModeAll   = "all"
)

// The floors below keep a one-field mistake from hammering the GitHub API
// (poll_interval) or wiping the job history (history_retention).
const (
	MinPollInterval     = 5 * time.Second
	MinHistoryRetention = 24 * time.Hour
)

// MaxRunners is the most runners global_max, a repository's max or a
// repository's warm may ask for. Parse lowers a larger value in config.yaml;
// Validate refuses one, which is what an API change gets.
const MaxRunners = 99

// SystemLabels are the read-only labels GitHub gives every Linux x64 runner.
var SystemLabels = []string{"self-hosted", "linux", "x64"}

// MaxLabelLength is the longest a custom label may be, in characters.
const MaxLabelLength = 100

// maxJITLabels is the most labels GitHub accepts when generating a runner's
// JIT configuration; the system labels count towards it.
const maxJITLabels = 100

// maxCPUQuotaPercent is 10,000 CPUs; systemd keeps the quota as a 32-bit
// count of parts per million of a CPU.
const maxCPUQuotaPercent = 1000000

type Config struct {
	Owner            string       `yaml:"owner" json:"owner"`
	Mode             string       `yaml:"mode" json:"mode"`
	GlobalMax        int          `yaml:"global_max" json:"global_max"`
	PollInterval     Duration     `yaml:"poll_interval" json:"poll_interval"`
	StartTimeout     Duration     `yaml:"start_timeout" json:"start_timeout"`
	IdleTimeout      Duration     `yaml:"idle_timeout" json:"idle_timeout"`
	DiskHighWater    int          `yaml:"disk_high_water" json:"disk_high_water"`
	BuildCacheKeep   string       `yaml:"build_cache_keep" json:"build_cache_keep"`
	HistoryRetention Duration     `yaml:"history_retention" json:"history_retention"`
	Labels           []string     `yaml:"labels" json:"labels"`
	RunnerLimits     RunnerLimits `yaml:"runner_limits" json:"runner_limits"`
	Repos            []Repo       `yaml:"repos" json:"repos"`
	// WatchRepos are repositories whose workflow runs the Actions page shows;
	// ghr runs no runners for them.
	WatchRepos []string `yaml:"watch_repos,omitempty" json:"watch_repos,omitempty"`
	// Web is read at daemon start only; changing it needs a restart.
	Web Web `yaml:"web,omitempty" json:"web,omitzero"`
	// GitHubStorage sets the artifact storage allowance, warnings and cleanup.
	GitHubStorage GitHubStorage `yaml:"github_storage,omitempty" json:"github_storage,omitzero"`
}

type RunnerLimits struct {
	MemoryMax string `yaml:"memory_max" json:"memory_max"`
	CPUQuota  string `yaml:"cpu_quota" json:"cpu_quota"`
}

// Web is the browser UI's TCP listener. Hosts names the hostnames, besides IP
// literals and localhost, that a request's Host header may carry.
type Web struct {
	Listen string   `yaml:"listen,omitempty" json:"listen,omitempty"`
	Hosts  []string `yaml:"hosts,omitempty" json:"hosts,omitempty"`
}

type Repo struct {
	Name                string   `yaml:"name" json:"name"`
	Max                 *int     `yaml:"max,omitempty" json:"max,omitempty"`
	Warm                *int     `yaml:"warm,omitempty" json:"warm,omitempty"`
	Labels              []string `yaml:"labels,omitempty" json:"labels,omitempty"`
	CleanupNamePrefixes []string `yaml:"cleanup_name_prefixes,omitempty" json:"cleanup_name_prefixes,omitempty"`
	Paused              bool     `yaml:"paused,omitempty" json:"paused,omitempty"`
	// AllowPublic lets ghr serve the repository while GitHub reports it
	// public. A job in a public repository can come from anyone's pull
	// request and runs on this host.
	AllowPublic bool `yaml:"allow_public,omitempty" json:"allow_public,omitempty"`
	// Removing is daemon-managed: set with Paused by DELETE /repos/{name} so a
	// pending removal survives restarts; a per-repo resume cancels it.
	Removing bool `yaml:"removing,omitempty" json:"removing,omitempty"`
}

// Duration is a time.Duration that also accepts a whole-day suffix ("30d").
type Duration time.Duration

func (d Duration) D() time.Duration { return time.Duration(d) }

// maxDays is the largest day count whose nanoseconds fit in an int64.
const maxDays = int64(math.MaxInt64 / int64(24*time.Hour))

func ParseDuration(s string) (Duration, error) {
	if strings.HasSuffix(s, "d") {
		n, err := strconv.ParseInt(strings.TrimSuffix(s, "d"), 10, 64)
		if err != nil || n > maxDays || n < -maxDays {
			return 0, fmt.Errorf("invalid duration %q", s)
		}
		return Duration(time.Duration(n) * 24 * time.Hour), nil
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q", s)
	}
	return Duration(v), nil
}

func (d Duration) String() string {
	v := time.Duration(d)
	if v > 0 && v%(24*time.Hour) == 0 {
		return fmt.Sprintf("%dd", v/(24*time.Hour))
	}
	return v.String()
}

func (d *Duration) UnmarshalYAML(n *yaml.Node) error {
	v, err := ParseDuration(n.Value)
	if err != nil {
		return err
	}
	*d = v
	return nil
}

func (d Duration) MarshalYAML() (any, error) { return d.String(), nil }

func (d Duration) MarshalJSON() ([]byte, error) { return []byte(strconv.Quote(d.String())), nil }

func (d *Duration) UnmarshalJSON(b []byte) error {
	s, err := strconv.Unquote(string(b))
	if err != nil {
		return err
	}
	v, err := ParseDuration(s)
	if err != nil {
		return err
	}
	*d = v
	return nil
}

var (
	sizeRe     = regexp.MustCompile(`^[0-9]+(B|KB|MB|GB|TB)$`)
	memoryRe   = regexp.MustCompile(`^([1-9][0-9]*[KMGT]?|[1-9][0-9]?%|100%|infinity)$`)
	cpuQuotaRe = regexp.MustCompile(`^[1-9][0-9]*%$`)
	repoNameRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)
	// hostnameRe is RFC 1123: dot-separated labels of letters, digits and
	// inner hyphens, each 1 to 63 characters.
	hostnameRe = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?)*$`)
)

func defaults() *Config {
	return &Config{
		Mode:             ModeQueue,
		GlobalMax:        2,
		PollInterval:     Duration(10 * time.Second),
		StartTimeout:     Duration(2 * time.Minute),
		IdleTimeout:      Duration(5 * time.Minute),
		DiskHighWater:    80,
		BuildCacheKeep:   "20GB",
		HistoryRetention: Duration(30 * 24 * time.Hour),
		RunnerLimits:     RunnerLimits{MemoryMax: "6G", CPUQuota: "200%"},
	}
}

// Parse decodes YAML over the defaults and validates the result. Unknown keys
// and extra documents are errors: Save would otherwise drop them silently.
func Parse(data []byte) (*Config, []string, error) {
	c := defaults()
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(c); err != nil && !errors.Is(err, io.EOF) {
		return nil, nil, fmt.Errorf("parse config: %w", err)
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); err == nil {
		return nil, nil, errors.New("parse config: expected exactly one YAML document")
	} else if !errors.Is(err, io.EOF) {
		return nil, nil, fmt.Errorf("parse config: %w", err)
	}
	c.Owner = strings.TrimSpace(c.Owner)
	repaired := c.clampRunners()
	repaired = append(repaired, c.dropBadLabels()...)
	warnings, err := c.Validate()
	if err != nil {
		return nil, nil, err
	}
	return c, append(repaired, warnings...), nil
}

// clampRunners lowers global_max and every repository's max and warm to
// MaxRunners, and returns one warning for each value it lowered.
func (c *Config) clampRunners() []string {
	var warnings []string
	clamp := func(what string, v *int) {
		if *v > MaxRunners {
			warnings = append(warnings, fmt.Sprintf("%s %d is above %d; using %d", what, *v, MaxRunners, MaxRunners))
			*v = MaxRunners
		}
	}
	clamp("global_max", &c.GlobalMax)
	for i := range c.Repos {
		r := &c.Repos[i]
		if r.Max != nil {
			clamp(r.Name+": max", r.Max)
		}
		if r.Warm != nil {
			clamp(r.Name+": warm", r.Warm)
		}
	}
	return warnings
}

// dropBadLabels removes from the global list and from each repository's list
// every label that Validate would refuse, and returns one warning for each.
func (c *Config) dropBadLabels() []string {
	var warnings []string
	drop := func(prefix string, labels []string) []string {
		kept := make([]string, 0, len(labels))
		seen := map[string]bool{}
		for _, l := range labels {
			key := strings.ToLower(strings.TrimSpace(l))
			switch {
			case labelFault(l) != "":
				warnings = append(warnings, fmt.Sprintf("%slabels: %q is not a valid label; ignored", prefix, l))
			case seen[key]:
				warnings = append(warnings, fmt.Sprintf("%slabels: %q is listed twice; ignored", prefix, l))
			default:
				seen[key] = true
				kept = append(kept, l)
			}
		}
		if len(kept) == len(labels) {
			return labels
		}
		return kept
	}
	c.Labels = drop("", c.Labels)
	for i := range c.Repos {
		c.Repos[i].Labels = drop(c.Repos[i].Name+": ", c.Repos[i].Labels)
	}
	return warnings
}

// labelFault says why one label is not valid, or returns "" for a valid one.
func labelFault(l string) string {
	l = strings.TrimSpace(l)
	switch {
	case l == "":
		return "must not be empty"
	case utf8.RuneCountInString(l) > MaxLabelLength:
		return fmt.Sprintf("must be at most %d characters", MaxLabelLength)
	case strings.ContainsFunc(l, func(r rune) bool { return r == ',' || unicode.IsSpace(r) }):
		return "must not contain spaces or commas"
	}
	for _, s := range SystemLabels {
		if strings.EqualFold(l, s) {
			return "is added by ghr"
		}
	}
	return ""
}

// RepoNameRule describes what ValidRepoName accepts, for error messages.
const RepoNameRule = "1 to 100 letters, digits, '.', '_' or '-', and not . or .."

// ValidRepoName reports whether name can be a repository name under an owner.
func ValidRepoName(name string) bool {
	return repoNameRe.MatchString(name) && name != "." && name != ".."
}

// memoryFitsUint64 reports whether a memory_max that memoryRe accepted, bytes
// with an optional K/M/G/T suffix, is below 2^64 bytes.
func memoryFitsUint64(s string) bool {
	if strings.HasSuffix(s, "%") || s == "infinity" {
		return true
	}
	mult := uint64(1)
	if i := strings.IndexAny(s, "KMGT"); i >= 0 {
		mult = uint64(1) << (10 * uint(strings.IndexByte("KMGT", s[i])+1))
		s = s[:i]
	}
	v, err := strconv.ParseUint(s, 10, 64)
	return err == nil && v <= math.MaxUint64/mult
}

// labelProblem describes the first label in the list that is not valid or
// that repeats an earlier one ignoring case, and returns "" for a valid list.
func labelProblem(labels []string) string {
	seen := map[string]bool{}
	for _, l := range labels {
		if f := labelFault(l); f != "" {
			return fmt.Sprintf("labels: %q %s", l, f)
		}
		key := strings.ToLower(strings.TrimSpace(l))
		if seen[key] {
			return fmt.Sprintf("labels: %q is listed twice", l)
		}
		seen[key] = true
	}
	return ""
}

func Load(path string) (*Config, []string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	return Parse(data)
}

// syncFile is os.File.Sync; a test replaces it to see when the sync runs and to
// make it fail.
var syncFile = (*os.File).Sync

// maxLinkHops is how many symbolic links saveTarget follows before it gives up.
const maxLinkHops = 8

// saveTarget returns the file Save replaces: path itself, or the file at the
// end of the symbolic links that start there. That file may not exist yet.
// Anything else that stops the resolution is an error, so Save never writes
// over a link.
//
// The path is split by hand and never cleaned: the kernel resolves a ".." after
// a symlinked directory against the real directory, and filepath.Clean (which
// Dir and Join apply) would drop it lexically and name another file.
func saveTarget(path string) (string, error) {
	orig := path
	for hops := 0; ; hops++ {
		dir, base := ".", path
		if i := strings.LastIndexAny(path, `/`+string(os.PathSeparator)); i >= 0 {
			dir, base = path[:i+1], path[i+1:]
		}
		dir, err := filepath.EvalSymlinks(dir)
		if err != nil {
			return "", err
		}
		path = filepath.Join(dir, base)
		fi, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			return path, nil
		}
		if err != nil {
			return "", err
		}
		if fi.Mode()&os.ModeSymlink == 0 {
			return path, nil
		}
		if hops == maxLinkHops {
			return "", fmt.Errorf("%s: more than %d symbolic links", orig, maxLinkHops)
		}
		dest, err := os.Readlink(path)
		if err != nil {
			return "", err
		}
		if !filepath.IsAbs(dest) {
			dest = dir + string(os.PathSeparator) + dest
		}
		path = dest
	}
}

// Save validates c, then writes it atomically (temp file beside the file, then
// renamed over it). When path is a symbolic link, the file at the end of the
// link is replaced and the link stays.
func Save(path string, c *Config) error {
	if _, err := c.Validate(); err != nil {
		return err
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	path, err = saveTarget(path)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.yaml")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if err := syncFile(tmp); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	// Syncing the directory makes the rename survive a power loss. Some
	// platforms, Windows among them, cannot sync a directory, so it is best
	// effort.
	if d, err := os.Open(filepath.Dir(path)); err == nil {
		d.Sync()
		d.Close()
	}
	return nil
}

// Clone returns a deep copy.
func (c *Config) Clone() *Config {
	data, err := yaml.Marshal(c)
	if err != nil {
		panic(err)
	}
	out := &Config{}
	if err := yaml.Unmarshal(data, out); err != nil {
		panic(err)
	}
	return out
}

// Repo finds a repo by name, ignoring case: GitHub repository names are case-insensitive.
func (c *Config) Repo(name string) *Repo {
	for i := range c.Repos {
		if strings.EqualFold(c.Repos[i].Name, name) {
			return &c.Repos[i]
		}
	}
	return nil
}

// Watched reports whether name is in watch_repos, ignoring case.
func (c *Config) Watched(name string) bool {
	for _, w := range c.WatchRepos {
		if strings.EqualFold(w, name) {
			return true
		}
	}
	return false
}

// EffectiveMax returns the repo's cap; 0 means unlimited.
// An omitted max is 1 in queue mode and unlimited in all mode.
func (c *Config) EffectiveMax(r Repo) int {
	if r.Max != nil {
		return *r.Max
	}
	if c.Mode == ModeAll {
		return 0
	}
	return 1
}

func (c *Config) EffectiveWarm(r Repo) int {
	if r.Warm != nil {
		return *r.Warm
	}
	return 1
}

// CustomLabels are the labels ghr registers (config.labels ∪ repo.labels, lower-cased, deduplicated).
func (c *Config) CustomLabels(r Repo) []string {
	seen := map[string]bool{}
	var out []string
	for _, l := range append(append([]string{}, c.Labels...), r.Labels...) {
		l = strings.ToLower(strings.TrimSpace(l))
		if l == "" || seen[l] {
			continue
		}
		seen[l] = true
		out = append(out, l)
	}
	return out
}

// EffectiveLabels are the system labels plus CustomLabels.
func (c *Config) EffectiveLabels(r Repo) []string {
	return append(append([]string{}, SystemLabels...), c.CustomLabels(r)...)
}

// Validate returns non-fatal warnings, or an error describing every violation.
func (c *Config) Validate() ([]string, error) {
	var errs []string
	var warnings []string
	if c.Mode != ModeQueue && c.Mode != ModeAll {
		errs = append(errs, fmt.Sprintf("mode must be %q or %q", ModeQueue, ModeAll))
	}
	if c.GlobalMax < 1 {
		errs = append(errs, "global_max must be >= 1")
	}
	if c.GlobalMax > MaxRunners {
		errs = append(errs, fmt.Sprintf("global_max must be <= %d", MaxRunners))
	}
	for name, d := range map[string]Duration{
		"poll_interval": c.PollInterval, "start_timeout": c.StartTimeout,
		"idle_timeout": c.IdleTimeout, "history_retention": c.HistoryRetention,
	} {
		if d <= 0 {
			errs = append(errs, name+" must be > 0")
		}
	}
	if c.PollInterval > 0 && c.PollInterval.D() < MinPollInterval {
		errs = append(errs, "poll_interval must be >= 5s")
	}
	if c.HistoryRetention > 0 && c.HistoryRetention.D() < MinHistoryRetention {
		errs = append(errs, "history_retention must be >= 1d")
	}
	if p := labelProblem(c.Labels); p != "" {
		errs = append(errs, p)
	}
	if c.DiskHighWater < 1 || c.DiskHighWater > 100 {
		errs = append(errs, "disk_high_water must be 1..100")
	}
	if !sizeRe.MatchString(c.BuildCacheKeep) {
		errs = append(errs, "build_cache_keep must look like 20GB")
	}
	if !memoryRe.MatchString(c.RunnerLimits.MemoryMax) {
		errs = append(errs, "runner_limits.memory_max must be bytes with an optional K/M/G/T suffix (6G), a percentage (50%) or infinity")
	} else if !memoryFitsUint64(c.RunnerLimits.MemoryMax) {
		errs = append(errs, "runner_limits.memory_max must fit in 64 bits (under 16 EiB)")
	}
	if !cpuQuotaRe.MatchString(c.RunnerLimits.CPUQuota) {
		errs = append(errs, "runner_limits.cpu_quota must be a positive percentage such as 200%")
	} else if n, err := strconv.ParseUint(strings.TrimSuffix(c.RunnerLimits.CPUQuota, "%"), 10, 64); err != nil || n > maxCPUQuotaPercent {
		errs = append(errs, fmt.Sprintf("runner_limits.cpu_quota must be at most %d%%", maxCPUQuotaPercent))
	}
	seen := map[string]bool{}
	for _, r := range c.Repos {
		if strings.TrimSpace(r.Name) == "" {
			errs = append(errs, "repo name is required")
			continue
		}
		if !ValidRepoName(r.Name) {
			errs = append(errs, "repos: "+r.Name+" must be a repository name under owner, "+RepoNameRule)
		}
		key := strings.ToLower(r.Name)
		if seen[key] {
			errs = append(errs, "duplicate repo "+r.Name+" (names are case-insensitive)")
		}
		seen[key] = true
		if r.Max != nil && *r.Max < 0 {
			errs = append(errs, r.Name+": max must be >= 0")
		}
		if r.Warm != nil && *r.Warm < 0 {
			errs = append(errs, r.Name+": warm must be >= 0")
		}
		if r.Max != nil && *r.Max > MaxRunners {
			errs = append(errs, fmt.Sprintf("%s: max must be <= %d", r.Name, MaxRunners))
		}
		if r.Warm != nil && *r.Warm > MaxRunners {
			errs = append(errs, fmt.Sprintf("%s: warm must be <= %d", r.Name, MaxRunners))
		}
		// warm only applies in all mode, where an omitted max is unlimited, so
		// only an explicit max > 0 bounds it, whatever the current mode is.
		if r.Max != nil && *r.Max > 0 && c.EffectiveWarm(r) > *r.Max {
			errs = append(errs, r.Name+": warm must be <= max")
		}
		for _, p := range r.CleanupNamePrefixes {
			if strings.TrimSpace(p) == "" {
				errs = append(errs, r.Name+": cleanup_name_prefixes must not contain an empty prefix")
			}
		}
		if r.Removing && !r.Paused {
			errs = append(errs, r.Name+": a repo being removed must stay paused")
		}
		if p := labelProblem(r.Labels); p != "" {
			errs = append(errs, r.Name+": "+p)
		}
		if len(c.CustomLabels(r)) == 0 {
			errs = append(errs, r.Name+": needs at least one label in labels or repo labels")
		}
		if n := len(c.EffectiveLabels(r)); n > maxJITLabels {
			errs = append(errs, fmt.Sprintf("%s: %d labels; GitHub allows at most %d including self-hosted, linux and x64", r.Name, n, maxJITLabels))
		}
		if len(r.CleanupNamePrefixes) > 0 && c.EffectiveMax(r) != 1 {
			warnings = append(warnings, r.Name+": cleanup_name_prefixes can remove a concurrent job's containers when max is not 1")
		}
	}
	for i, a := range c.Repos {
		for _, b := range c.Repos[i+1:] {
			if p, q, ok := overlappingPrefixes(a.CleanupNamePrefixes, b.CleanupNamePrefixes); ok {
				warnings = append(warnings, fmt.Sprintf("%s: cleanup_name_prefixes %q and %s's %q overlap, so ending a runner can remove the other repository's containers", a.Name, p, b.Name, q))
			}
		}
	}
	watched := map[string]bool{}
	for _, w := range c.WatchRepos {
		name := strings.TrimSpace(w)
		if name == "" {
			errs = append(errs, "watch_repos must not contain an empty name")
			continue
		}
		if name != w {
			errs = append(errs, "watch_repos: "+w+" must not have surrounding spaces")
		}
		if !ValidRepoName(name) {
			errs = append(errs, "watch_repos: "+w+" must be a repository name under owner, "+RepoNameRule)
		}
		key := strings.ToLower(name)
		if watched[key] {
			errs = append(errs, "duplicate watched repo "+w+" (names are case-insensitive)")
		}
		watched[key] = true
		if seen[key] {
			errs = append(errs, "watch_repos: "+w+" is already a configured repo")
		}
	}
	if c.Web.Listen != "" {
		_, port, err := net.SplitHostPort(c.Web.Listen)
		n, perr := strconv.Atoi(port)
		if err != nil || perr != nil || n < 1 || n > 65535 {
			errs = append(errs, "web.listen must be host:port with a port from 1 to 65535, such as 0.0.0.0:8080")
		}
	}
	for _, h := range c.Web.Hosts {
		if len(h) > 253 || !hostnameRe.MatchString(h) {
			errs = append(errs, "web.hosts entries must be bare hostnames such as ghr.lan")
		}
	}
	errs = append(errs, c.GitHubStorage.validate()...)
	if len(errs) > 0 {
		return nil, errors.New(strings.Join(sortedUnique(errs), "; "))
	}
	return warnings, nil
}

// overlappingPrefixes returns the first pair, one from each list, where one
// prefix starts with the other. Blank prefixes are skipped; Validate rejects
// them separately.
func overlappingPrefixes(a, b []string) (string, string, bool) {
	for _, p := range a {
		for _, q := range b {
			if strings.TrimSpace(p) == "" || strings.TrimSpace(q) == "" {
				continue
			}
			if strings.HasPrefix(p, q) || strings.HasPrefix(q, p) {
				return p, q, true
			}
		}
	}
	return "", "", false
}

func sortedUnique(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}
