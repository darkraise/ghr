// Package storage measures and manages what the runner LXC keeps on disk
// between jobs: toolchains in the tool cache, package-manager caches in the
// runner home, and Docker's disk.
package storage

import (
	"errors"
	"fmt"
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

// clearPath empties path so a job sees either the old tree or an empty
// directory, never a half-deleted one: rename it aside (atomic), recreate the
// empty directory with the old owner and mode, then delete the renamed tree.
// The daemon runs as root, so read-only files (Go's module cache) need no
// chmod, and os.RemoveAll removes a symlink without following it. A path
// that is itself a symlink is removed and not recreated.
func clearPath(path, opID string) error {
	fi, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	aside := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+clearingTag+opID)
	if err := os.Rename(path, aside); err != nil {
		return err
	}
	var recreateErr error
	if fi.IsDir() {
		recreateErr = recreate(path, fi)
	}
	// The renamed tree goes whether or not the directory came back: a failed
	// recreate must not strand it until the next daemon start.
	return errors.Join(recreateErr, os.RemoveAll(aside))
}

// recreate makes the empty directory a clear leaves behind. A directory that
// already exists was made by a job that ran in between, and stays untouched.
func recreate(path string, old os.FileInfo) error {
	if err := os.Mkdir(path, old.Mode().Perm()); err != nil {
		if os.IsExist(err) {
			return nil
		}
		return err
	}
	// Mkdir applies the umask. Chmod through a root at the parent: a job
	// can swap the new directory for a symlink, which os.Chmod would
	// follow anywhere as root, while Root.Chmod refuses to leave the parent.
	r, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return err
	}
	err = r.Chmod(filepath.Base(path), old.Mode().Perm())
	r.Close()
	if err != nil {
		return err
	}
	return chownLike(path, old)
}

// noLinkedParent refuses a path below home when a directory between home and
// the path is a symlink: root must not rename or delete through a link a job
// planted (clearPath already removes a link at the path itself unfollowed).
func noLinkedParent(home, path string) error {
	rel, err := filepath.Rel(home, filepath.Dir(path))
	if err != nil || rel == "." {
		return err
	}
	cur := home
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		cur = filepath.Join(cur, part)
		fi, err := os.Lstat(cur)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s is a symlink; not clearing through it", cur)
		}
	}
	return nil
}

// clearCache clears every existing path of c.
func clearCache(home string, c Cache, opID string) error {
	var errs []error
	for _, p := range c.abs(home) {
		if err := noLinkedParent(home, p); err != nil {
			errs = append(errs, err)
			continue
		}
		if err := clearPath(p, opID); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// sweepClearing deletes trees a clear renamed aside but did not finish
// deleting, as a daemon stop mid-clear leaves them.
func sweepClearing(home string) error {
	seen := map[string]bool{}
	var errs []error
	for _, c := range Caches {
		for _, p := range c.abs(home) {
			dir := filepath.Dir(p)
			if seen[dir] {
				continue
			}
			seen[dir] = true
			if noLinkedParent(home, p) != nil {
				continue
			}
			es, err := os.ReadDir(dir)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				errs = append(errs, err)
				continue
			}
			for _, e := range es {
				if strings.HasPrefix(e.Name(), ".") && strings.Contains(e.Name(), clearingTag) {
					if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
						errs = append(errs, err)
					}
				}
			}
		}
	}
	return errors.Join(errs...)
}
