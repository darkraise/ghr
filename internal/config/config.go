// Package config loads, validates and atomically saves /etc/ghr/config.yaml.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	ModeQueue = "queue"
	ModeAll   = "all"
)

// SystemLabels are the read-only labels GitHub gives every Linux x64 runner.
var SystemLabels = []string{"self-hosted", "linux", "x64"}

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
}

type RunnerLimits struct {
	MemoryMax string `yaml:"memory_max" json:"memory_max"`
	CPUQuota  string `yaml:"cpu_quota" json:"cpu_quota"`
}

type Repo struct {
	Name                string   `yaml:"name" json:"name"`
	Max                 *int     `yaml:"max,omitempty" json:"max,omitempty"`
	Warm                *int     `yaml:"warm,omitempty" json:"warm,omitempty"`
	Labels              []string `yaml:"labels,omitempty" json:"labels,omitempty"`
	CleanupNamePrefixes []string `yaml:"cleanup_name_prefixes,omitempty" json:"cleanup_name_prefixes,omitempty"`
	Paused              bool     `yaml:"paused,omitempty" json:"paused,omitempty"`
	// Removing is daemon-managed: set with Paused by DELETE /repos/{name} so a
	// pending removal survives restarts; a per-repo resume cancels it.
	Removing bool `yaml:"removing,omitempty" json:"removing,omitempty"`
}

// Duration is a time.Duration that also accepts a whole-day suffix ("30d").
type Duration time.Duration

func (d Duration) D() time.Duration { return time.Duration(d) }

func ParseDuration(s string) (Duration, error) {
	if strings.HasSuffix(s, "d") {
		n, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil {
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

// Parse decodes YAML over the defaults and validates the result.
func Parse(data []byte) (*Config, []string, error) {
	c := defaults()
	if err := yaml.Unmarshal(data, c); err != nil {
		return nil, nil, fmt.Errorf("parse config: %w", err)
	}
	warnings, err := c.Validate()
	if err != nil {
		return nil, nil, err
	}
	return c, warnings, nil
}

func Load(path string) (*Config, []string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	return Parse(data)
}

// Save validates c, then writes it atomically (temp file in the same dir + rename).
func Save(path string, c *Config) error {
	if _, err := c.Validate(); err != nil {
		return err
	}
	data, err := yaml.Marshal(c)
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
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
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
	if strings.TrimSpace(c.Owner) == "" {
		errs = append(errs, "owner is required")
	}
	if c.Mode != ModeQueue && c.Mode != ModeAll {
		errs = append(errs, fmt.Sprintf("mode must be %q or %q", ModeQueue, ModeAll))
	}
	if c.GlobalMax < 1 {
		errs = append(errs, "global_max must be >= 1")
	}
	for name, d := range map[string]Duration{
		"poll_interval": c.PollInterval, "start_timeout": c.StartTimeout,
		"idle_timeout": c.IdleTimeout, "history_retention": c.HistoryRetention,
	} {
		if d <= 0 {
			errs = append(errs, name+" must be > 0")
		}
	}
	if c.DiskHighWater < 1 || c.DiskHighWater > 100 {
		errs = append(errs, "disk_high_water must be 1..100")
	}
	if !sizeRe.MatchString(c.BuildCacheKeep) {
		errs = append(errs, "build_cache_keep must look like 20GB")
	}
	if !memoryRe.MatchString(c.RunnerLimits.MemoryMax) {
		errs = append(errs, "runner_limits.memory_max must be bytes with an optional K/M/G/T suffix (6G), a percentage (50%) or infinity")
	}
	if !cpuQuotaRe.MatchString(c.RunnerLimits.CPUQuota) {
		errs = append(errs, "runner_limits.cpu_quota must be a positive percentage such as 200%")
	}
	seen := map[string]bool{}
	for _, r := range c.Repos {
		if strings.TrimSpace(r.Name) == "" {
			errs = append(errs, "repo name is required")
			continue
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
		if len(c.CustomLabels(r)) == 0 {
			errs = append(errs, r.Name+": needs at least one label in labels or repo labels")
		}
		if len(r.CleanupNamePrefixes) > 0 && c.EffectiveMax(r) != 1 {
			warnings = append(warnings, r.Name+": cleanup_name_prefixes can remove a concurrent job's containers when max is not 1")
		}
	}
	if len(errs) > 0 {
		return nil, errors.New(strings.Join(sortedUnique(errs), "; "))
	}
	return warnings, nil
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
