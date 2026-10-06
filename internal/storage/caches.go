// Package storage measures and manages what the runner LXC keeps on disk
// between jobs: toolchains in the tool cache, package-manager caches in the
// runner home, and Docker's disk.
package storage

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Cache is one package manager's cache at its default paths, relative to the
// runner home. A workflow that relocates a cache is not seen.
type Cache struct {
	Name  string
	Label string
	Paths []string
}

var Caches = []Cache{
	{Name: "nuget", Label: "NuGet", Paths: []string{".nuget/packages"}},
	{Name: "npm", Label: "npm", Paths: []string{".npm"}},
	{Name: "pnpm", Label: "pnpm", Paths: []string{".local/share/pnpm/store", ".cache/pnpm"}},
	{Name: "yarn", Label: "Yarn", Paths: []string{".cache/yarn", ".yarn/berry/cache"}},
	{Name: "pip", Label: "pip", Paths: []string{".cache/pip"}},
	{Name: "gomod", Label: "Go modules", Paths: []string{"go/pkg/mod"}},
	{Name: "gobuild", Label: "Go build", Paths: []string{".cache/go-build"}},
	{Name: "maven", Label: "Maven", Paths: []string{".m2/repository"}},
	{Name: "gradle", Label: "Gradle", Paths: []string{".gradle/caches", ".gradle/wrapper/dists"}},
	{Name: "cargo", Label: "Cargo", Paths: []string{".cargo/registry", ".cargo/git"}},
}

func cacheByName(name string) (Cache, bool) {
	for _, c := range Caches {
		if c.Name == name {
			return c, true
		}
	}
	return Cache{}, false
}

func (c Cache) abs(home string) []string {
	out := make([]string, len(c.Paths))
	for i, p := range c.Paths {
		out[i] = filepath.Join(home, filepath.FromSlash(p))
	}
	return out
}

func (c Cache) present(home string) bool {
	for _, p := range c.abs(home) {
		if _, err := os.Lstat(p); err == nil {
			return true
		}
	}
	return false
}

const clearingTag = ".ghr-clearing-"

// clearPath empties rel so a job sees either the old tree or an empty
// directory, never a half-deleted one: rename it aside (atomic), recreate the
// empty directory with the old owner and mode, then delete the renamed tree. The daemon
// runs as root, so read-only files (Go's module cache) need no chmod, and
// RemoveAll removes a symlink without following it. A path that is itself a
// symlink is removed and not recreated.
func clearPath(r *os.Root, rel, opID string) error {
	fi, err := r.Lstat(rel)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	aside := filepath.Join(filepath.Dir(rel), "."+filepath.Base(rel)+clearingTag+opID)
	if err := r.Rename(rel, aside); err != nil {
		return err
	}
	var recreateErr error
	if fi.IsDir() {
		recreateErr = recreate(r, rel, fi)
	}
	// The renamed tree goes whether or not the directory came back: a failed
	// recreate must not strand it until the next daemon start.
	return errors.Join(recreateErr, r.RemoveAll(aside))
}

// recreate makes the empty directory a clear leaves behind. A directory that
// already exists was made by a job that ran in between, and stays untouched.
func recreate(r *os.Root, rel string, old os.FileInfo) error {
	perm := old.Mode().Perm()
	if err := r.Mkdir(rel, perm); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return nil
		}
		return err
	}
	// Mkdir applies the umask.
	if err := r.Chmod(rel, perm); err != nil {
		return err
	}
	return chownLike(r, rel, old)
}

// clearCache clears every existing path of c. Every step goes through a root
// at home, so root never renames or deletes through a symlink a job planted
// that leads out of the home, even one swapped in mid-clear.
func clearCache(home string, c Cache, opID string) error {
	r, err := os.OpenRoot(home)
	if err != nil {
		return err
	}
	defer r.Close()
	var errs []error
	for _, p := range c.Paths {
		if err := clearPath(r, filepath.FromSlash(p), opID); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// sweepClearing deletes trees a clear renamed aside but did not finish
// deleting, as a daemon stop mid-clear leaves them.
func sweepClearing(home string) error {
	r, err := os.OpenRoot(home)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer r.Close()
	seen := map[string]bool{}
	var errs []error
	for _, c := range Caches {
		for _, p := range c.Paths {
			dir := filepath.Dir(filepath.FromSlash(p))
			if seen[dir] {
				continue
			}
			seen[dir] = true
			es, err := readDirIn(r, dir)
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				errs = append(errs, err)
				continue
			}
			for _, e := range es {
				if strings.HasPrefix(e.Name(), ".") && strings.Contains(e.Name(), clearingTag) {
					if err := r.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
						errs = append(errs, err)
					}
				}
			}
		}
	}
	return errors.Join(errs...)
}

func readDirIn(r *os.Root, dir string) ([]fs.DirEntry, error) {
	f, err := r.Open(dir)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return f.ReadDir(-1)
}
