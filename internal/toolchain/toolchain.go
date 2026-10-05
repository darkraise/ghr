// Package toolchain installs and lists toolchains in the shared GitHub
// Actions tool cache, in the layout each setup-* action reads before it
// downloads anything.
package toolchain

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/darkraise/ghr/internal/github"
)

const arch = "x64"

var (
	ErrUnknownTool      = errors.New("unknown toolchain")
	ErrNotInstalled     = errors.New("toolchain version not installed")
	ErrAlreadyInstalled = errors.New("toolchain version already installed")
)

// Sources are the upstream URLs the installers read.
type Sources struct {
	NodeManifest   string
	GoManifest     string
	PythonManifest string
	GoReleases     string // go.dev's JSON release list, with SHA-256s
	GoDownload     string // prefix of go.dev archive URLs, ending in "/"
	Adoptium       string // API base, without /v3
	DotnetIndex    string
	DotnetScript   string
}

// Env is what the installers need from the host.
type Env struct {
	Root    string // the tool cache (RUNNER_TOOL_CACHE)
	Home    string // the runner user's home
	User    string // the runner user, owner of everything installed
	Sources Sources
	Get     func(ctx context.Context, url string) ([]byte, error)
	Fetch   func(ctx context.Context, url, dst string) error
	Run     func(ctx context.Context, name string, args ...string) ([]byte, error)
	Extract func(ctx context.Context, archive, dir string) error
	OpID    func() string
}

// Release is one resolved toolchain version, ready to install.
type Release struct {
	Tool    string
	Version string // as shown
	Folder  string // the version folder in the tool cache
	URL     string
	SHA256  string // empty when the source publishes none
}

// Choice is one entry the Install dialog offers: Spec is what an install
// request takes, Version what the picker shows.
type Choice struct {
	Spec    string `json:"spec"`
	Version string `json:"version"`
	LTS     bool   `json:"lts,omitempty"`
}

type Installed struct {
	Tool        string
	Version     string
	Arch        string
	Path        string
	InstalledAt time.Time
}

type Installer interface {
	Available(ctx context.Context) ([]Choice, error)
	Resolve(ctx context.Context, spec string) (Release, error)
	// Install returns ErrAlreadyInstalled when rel is already complete.
	Install(ctx context.Context, rel Release, progress func(step string)) error
	Installed() ([]Installed, error)
	// Remove returns ErrNotInstalled for a version that is not installed.
	Remove(version string) error
}

// prefixOf compares a missing part of v as 0, so "1.20.0" matches Go's "1.20".
func prefixOf(p, v github.Version) bool {
	for i := range p {
		n := 0
		if i < len(v) {
			n = v[i]
		}
		if p[i] != n {
			return false
		}
	}
	return true
}

// pick returns the newest of vs matching spec: "latest", or a version whose
// leading parts equal spec's ("22" matches 22.11.0, "3.13" matches 3.13.7).
func pick(vs []string, spec string) (string, bool) {
	want, ok := github.ParseVersion(spec)
	if spec != "latest" && !ok {
		return "", false
	}
	best, bestV := "", github.Version(nil)
	for _, v := range vs {
		pv, ok := github.ParseVersion(v)
		if !ok || (spec != "latest" && !prefixOf(want, pv)) {
			continue
		}
		if best == "" || pv.Compare(bestV) > 0 {
			best, bestV = v, pv
		}
	}
	return best, best != ""
}

func newestFirst(vs []string) {
	sort.SliceStable(vs, func(i, j int) bool { return looseCompare(vs[i], vs[j]) > 0 })
}

// looseCompare orders by the runs of digits in a and b, so Java's
// 21.0.12+101.0.LTS sorts after 21.0.8+9.
func looseCompare(a, b string) int {
	na, nb := numbers(a), numbers(b)
	for i := range max(len(na), len(nb)) {
		x, y := 0, 0
		if i < len(na) {
			x = na[i]
		}
		if i < len(nb) {
			y = nb[i]
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return strings.Compare(a, b)
}

func numbers(s string) []int {
	var out []int
	for _, f := range strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsDigit(r) }) {
		if n, err := strconv.Atoi(f); err == nil {
			out = append(out, n)
		}
	}
	return out
}

// makeSemver pads a stable Go version to three parts, as setup-go's
// makeSemver does (1.25 → 1.25.0).
func makeSemver(v string) string {
	parts := strings.Split(v, ".")
	for len(parts) < 3 {
		parts = append(parts, "0")
	}
	return strings.Join(parts, ".")
}

// validFolder reports whether name is one path element ghr may use as a
// version folder.
func validFolder(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, `/\`)
}

func identity(s string) string { return s }
