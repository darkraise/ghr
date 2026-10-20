package config

import (
	"maps"
	"net"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
)

const (
	DefaultArtifactCleanup = 7 * 24 * time.Hour
	MinArtifactCleanup     = 24 * time.Hour
	MinArtifactOverride    = time.Hour
	DefaultArtifactRegion  = "us-east-1"
)

var bucketRe = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)

// ArtifactStore is the artifact_store block: the S3 server that switched-on
// repositories keep their Actions artifacts on. CleanupMaxAge and each
// repository's Cleanup are "", "off" or a duration; "" takes the default.
type ArtifactStore struct {
	Endpoint      string                  `yaml:"endpoint,omitempty" json:"endpoint,omitempty"`
	Bucket        string                  `yaml:"bucket,omitempty" json:"bucket,omitempty"`
	Region        string                  `yaml:"region,omitempty" json:"region,omitempty"`
	CleanupMaxAge string                  `yaml:"cleanup_max_age,omitempty" json:"cleanup_max_age,omitempty"`
	CapacityGB    int                     `yaml:"capacity_gb,omitempty" json:"capacity_gb,omitempty"`
	Repos         map[string]ArtifactRepo `yaml:"repos,omitempty" json:"repos,omitempty"`
}

type ArtifactRepo struct {
	Enabled bool   `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Cleanup string `yaml:"cleanup,omitempty" json:"cleanup,omitempty"`
}

func (a ArtifactStore) Configured() bool { return a.Endpoint != "" && a.Bucket != "" }

func (a ArtifactStore) RegionOrDefault() string {
	if a.Region == "" {
		return DefaultArtifactRegion
	}
	return a.Region
}

// Repo finds a repository's entry, ignoring case.
func (a ArtifactStore) Repo(name string) (ArtifactRepo, bool) {
	for k, r := range a.Repos {
		if strings.EqualFold(k, name) {
			return r, true
		}
	}
	return ArtifactRepo{}, false
}

func (a ArtifactStore) Enabled(name string) bool {
	r, ok := a.Repo(name)
	return ok && r.Enabled
}

// GlobalCleanup is the cleanup age of a repository without an override, 0
// when off.
func (a ArtifactStore) GlobalCleanup() time.Duration {
	if a.CleanupMaxAge == "" {
		return DefaultArtifactCleanup
	}
	d, _ := ParseCleanup(a.CleanupMaxAge)
	return d
}

// CleanupFor is the repository's cleanup age, 0 when off, and where it
// comes from: "override", "global" or "default".
func (a ArtifactStore) CleanupFor(repo string) (age time.Duration, source string) {
	if r, ok := a.Repo(repo); ok && r.Cleanup != "" {
		d, _ := ParseCleanup(r.Cleanup)
		return d, "override"
	}
	if a.CleanupMaxAge != "" {
		return a.GlobalCleanup(), "global"
	}
	return DefaultArtifactCleanup, "default"
}

func validEndpoint(s string) bool {
	u, err := url.Parse(s)
	return err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil && u.Opaque == "" &&
		u.Path == "" && u.RawQuery == "" && !u.ForceQuery && u.Fragment == ""
}

func validBucket(s string) bool {
	return bucketRe.MatchString(s) && !strings.Contains(s, "..") && net.ParseIP(s) == nil
}

// validCleanup reports whether s is "", "off" or a duration of at least min.
func validCleanup(s string, min time.Duration) bool {
	if s == "" || s == "off" {
		return true
	}
	d, err := ParseDuration(s)
	return err == nil && d.D() >= min
}

func (a ArtifactStore) validate() []string {
	var errs []string
	if a.Endpoint != "" && !validEndpoint(a.Endpoint) {
		errs = append(errs, "artifact_store.endpoint must be an https URL without a path, query or user info, such as https://s3.example.com")
	}
	if a.Bucket != "" && !validBucket(a.Bucket) {
		errs = append(errs, "artifact_store.bucket must be 3 to 63 lower-case letters, digits, '.' or '-', starting and ending with a letter or digit, such as gha-artifacts")
	}
	if a.CapacityGB < 0 {
		errs = append(errs, "artifact_store.capacity_gb must not be negative")
	}
	if !validCleanup(a.CleanupMaxAge, MinArtifactCleanup) {
		errs = append(errs, "artifact_store.cleanup_max_age must be off or a duration of at least 24h")
	}
	// An entry for a repository that is in neither repos nor watch_repos is
	// not an error: removing a repository by hand must not stop the config
	// loading, and nothing acts on such an entry.
	// Sorted, so the pair named for a case clash is always the same.
	first := map[string]string{}
	for _, k := range slices.Sorted(maps.Keys(a.Repos)) {
		if strings.TrimSpace(k) == "" {
			errs = append(errs, "artifact_store.repos must not contain an empty name")
			continue
		}
		if !ValidRepoName(k) {
			errs = append(errs, "artifact_store.repos: "+k+" must be a repository name under owner, "+RepoNameRule)
		}
		if b, ok := first[strings.ToLower(k)]; ok {
			errs = append(errs, "artifact_store.repos: "+b+" and "+k+" differ only by case")
		} else {
			first[strings.ToLower(k)] = k
		}
		if !validCleanup(a.Repos[k].Cleanup, MinArtifactOverride) {
			errs = append(errs, "artifact_store.repos: "+k+": cleanup must be off or a duration of at least 1h")
		}
	}
	return errs
}
