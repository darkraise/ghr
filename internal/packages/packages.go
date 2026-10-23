// Package packages manages the host's apt packages: the names ghr manages,
// the base set deploy/setup.sh installs, and the apt and dpkg calls.
package packages

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"strings"
)

var (
	ErrUnavailable    = errors.New("apt is not available on this host")
	ErrInvalidName    = errors.New("not a valid package name")
	ErrInvalidTerm    = errors.New("a search term is 2 to 64 characters of a-z, 0-9, +, . and -")
	ErrBase           = errors.New("setup.sh installs this package; ghr does not change it")
	ErrNotManaged     = errors.New("package is not managed by ghr")
	ErrAlreadyManaged = errors.New("package is already managed by ghr")
	ErrNotAdoptable   = errors.New("ghr adopts only installed packages of priority optional or extra outside the libs and oldlibs sections")
	ErrBusy           = errors.New("an operation on this package is running; try again when it ends")
	ErrNoNames        = errors.New("name at least one package")
)

// The name rule is Debian policy's, without an architecture suffix, so a name
// can never be read as an option or carry a shell metacharacter.
var (
	nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9+.-]{1,62}$`)
	termRe = regexp.MustCompile(`^[a-z0-9+.-]{2,64}$`)
)

// ValidName reports whether s is a package name ghr passes to apt.
func ValidName(s string) bool { return nameRe.MatchString(s) }

// ValidTerm reports whether s is a search term ghr passes to apt-cache.
func ValidTerm(s string) bool { return termRe.MatchString(s) }

// Info is one package as dpkg and apt see it. Version is empty when the
// package is not installed; Candidate is empty when apt has none or was not asked.
type Info struct {
	Name, Version, Candidate, Description string
	Priority, Section                     string
	Installed, Upgradable                 bool
	Bytes                                 int64
}

// Adoptable reports whether an installed package's own fields let ghr adopt
// it: priority optional or extra, outside the libs and oldlibs sections, so
// the runner's libraries and the base system cannot be adopted and then
// uninstalled. Callers also exclude base and managed names.
func Adoptable(i Info) bool {
	if !i.Installed || (i.Priority != "optional" && i.Priority != "extra") {
		return false
	}
	section := i.Section[strings.LastIndex(i.Section, "/")+1:]
	return section != "libs" && section != "oldlibs"
}

// Manager runs apt and dpkg. Names reaching it have passed ValidName.
type Manager interface {
	Available() bool // apt-get, apt-cache, apt-mark, dpkg and dpkg-query on PATH
	// Info reads dpkg-query; with candidates it also reads apt-cache policy.
	Info(ctx context.Context, names []string, candidates bool) ([]Info, error)
	Search(ctx context.Context, term string) ([]Info, error) // at most 50, exact name first
	Detected(ctx context.Context, exclude []string) ([]Info, error)
	Refresh(ctx context.Context) error // apt-get update
	UpgradesOthers(ctx context.Context, names []string) (bool, error)
	Install(ctx context.Context, names []string) error
	Upgrade(ctx context.Context, names []string) error
	MarkManual(ctx context.Context, name string) error
	Remove(ctx context.Context, name string) error
}

// setupPackages are the names on deploy/setup.sh's apt-get install lines;
// TestBaseMatchesSetup keeps them in step.
var setupPackages = []string{
	"ca-certificates", "curl", "gnupg",
	"docker-ce", "docker-ce-cli", "containerd.io", "docker-compose-plugin", "docker-buildx-plugin",
	"git", "jq", "unzip", "build-essential",
}

// pythonLibs is setup.sh's PYTHON_RUNTIME_LIBS; pythonLibsJammy is its
// Ubuntu 22.04 array, from before the 64-bit time_t renames.
var (
	pythonLibs      = []string{"libssl3t64", "libffi8", "libsqlite3-0", "liblzma5", "libbz2-1.0", "libgdbm6t64", "libncursesw6", "libreadline8t64", "libtcl8.6", "libtk8.6"}
	pythonLibsJammy = []string{"libssl3", "libffi8", "libsqlite3-0", "liblzma5", "libbz2-1.0", "libgdbm6", "libncursesw6", "libreadline8", "libtcl8.6", "libtk8.6"}
)

// Base returns what deploy/setup.sh installs on the release that os-release
// names by ID and VERSION_ID, sorted and without duplicates.
func Base(id, versionID string) []string {
	libs := pythonLibs
	if id == "ubuntu" && versionID == "22.04" {
		libs = pythonLibsJammy
	}
	out := slices.Concat(setupPackages, libs)
	slices.Sort(out)
	return slices.Compact(out)
}

// ParseOSRelease returns the ID and VERSION_ID of an os-release file's text.
func ParseOSRelease(text string) (id, versionID string) {
	for _, line := range strings.Split(text, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		value = strings.Trim(value, `"'`)
		switch key {
		case "ID":
			id = value
		case "VERSION_ID":
			versionID = value
		}
	}
	return id, versionID
}
