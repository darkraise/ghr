package config

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
)

const (
	// DefaultIncludedMB is the Free plan's storage shared by Actions
	// artifacts and Packages.
	DefaultIncludedMB = 500
	// MinCleanupMaxAge keeps a host clock running fast, or an artifact a
	// later run reads, from being cleaned within a day.
	MinCleanupMaxAge   = 24 * time.Hour
	MinCleanupOverride = time.Hour
)

var DefaultWarnPct = [2]int{70, 90}

// GitHubStorage is the github_storage block. A zero field takes its default,
// so a block left at its defaults is not written.
type GitHubStorage struct {
	IncludedMB    int                        `yaml:"included_mb,omitempty" json:"included_mb,omitempty"`
	WarnPct       []int                      `yaml:"warn_pct,omitempty" json:"warn_pct,omitempty"`
	CleanupMaxAge Duration                   `yaml:"cleanup_max_age,omitempty" json:"cleanup_max_age,omitempty"`
	Repos         map[string]StorageOverride `yaml:"repos,omitempty" json:"repos,omitempty"`
}

type StorageOverride struct {
	Cleanup string `yaml:"cleanup" json:"cleanup"`
}

func (g GitHubStorage) Included() int {
	if g.IncludedMB == 0 {
		return DefaultIncludedMB
	}
	return g.IncludedMB
}

func (g GitHubStorage) Warn() [2]int {
	if len(g.WarnPct) != 2 {
		return DefaultWarnPct
	}
	return [2]int{g.WarnPct[0], g.WarnPct[1]}
}

// Override finds a repository's override, ignoring case.
func (g GitHubStorage) Override(repo string) (StorageOverride, bool) {
	for k, o := range g.Repos {
		if strings.EqualFold(k, repo) {
			return o, true
		}
	}
	return StorageOverride{}, false
}

// CleanupFor is the repository's effective cleanup age, 0 when off, and
// where it comes from: "override", "global" or "none".
func (g GitHubStorage) CleanupFor(repo string) (age time.Duration, source string) {
	if o, ok := g.Override(repo); ok {
		d, _ := ParseCleanup(o.Cleanup)
		return d, "override"
	}
	if g.CleanupMaxAge > 0 {
		return g.CleanupMaxAge.D(), "global"
	}
	return 0, "none"
}

// ParseCleanup reads "off" as 0 and anything else as a duration.
func ParseCleanup(s string) (time.Duration, error) {
	if s == "off" {
		return 0, nil
	}
	d, err := ParseDuration(s)
	return d.D(), err
}

func CleanupText(d time.Duration) string {
	if d == 0 {
		return "off"
	}
	return ShortDuration(d)
}

// ShortDuration writes whole days, hours or minutes with one unit, where
// time.Duration.String would write "6h0m0s".
func ShortDuration(d time.Duration) string {
	switch {
	case d > 0 && d%(24*time.Hour) == 0:
		return fmt.Sprintf("%dd", d/(24*time.Hour))
	case d > 0 && d%time.Hour == 0:
		return fmt.Sprintf("%dh", d/time.Hour)
	case d > 0 && d%time.Minute == 0:
		return fmt.Sprintf("%dm", d/time.Minute)
	}
	return d.String()
}

func (g GitHubStorage) validate() []string {
	var errs []string
	if g.IncludedMB < 0 {
		errs = append(errs, "github_storage.included_mb must be at least 1")
	}
	if w := g.WarnPct; w != nil && (len(w) != 2 || w[0] < 1 || w[0] >= w[1] || w[1] > 100) {
		errs = append(errs, "github_storage.warn_pct must be two increasing percentages from 1 to 100, such as [70, 90]")
	}
	if g.CleanupMaxAge != 0 && g.CleanupMaxAge.D() < MinCleanupMaxAge {
		errs = append(errs, "github_storage.cleanup_max_age must be at least 24h")
	}
	// Sorted, so the pair named for a case clash is always the same.
	first := map[string]string{}
	for _, k := range slices.Sorted(maps.Keys(g.Repos)) {
		if strings.TrimSpace(k) == "" {
			errs = append(errs, "github_storage.repos must not contain an empty name")
			continue
		}
		if strings.Contains(k, "/") {
			errs = append(errs, "github_storage.repos: "+k+" must be a repository name under owner, without a slash")
		}
		if a, ok := first[strings.ToLower(k)]; ok {
			errs = append(errs, "github_storage.repos: "+a+" and "+k+" differ only by case")
		} else {
			first[strings.ToLower(k)] = k
		}
		if c := g.Repos[k].Cleanup; c != "off" {
			if d, err := ParseDuration(c); err != nil || d.D() < MinCleanupOverride {
				errs = append(errs, "github_storage.repos: "+k+": cleanup must be off or a duration of at least 1h")
			}
		}
	}
	return errs
}
