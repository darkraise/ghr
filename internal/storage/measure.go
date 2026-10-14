package storage

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/system"
	"github.com/darkraise/ghr/internal/toolchain"
)

// Toolchains is the part of *toolchain.Set the service uses.
type Toolchains interface {
	Get(tool string) (toolchain.Installer, error)
	Available(ctx context.Context, tool string) ([]toolchain.Choice, error)
	Installed() ([]toolchain.Installed, error)
	Other() ([]string, error)
	Root() string
	CleanTmp() error
}

// Docker is the Docker disk reporting the measurer reads.
type Docker interface {
	DiskUsage(ctx context.Context) ([]system.DiskRow, error)
	BuildCacheUsage(ctx context.Context) ([]system.CacheTypeUsage, error)
}

type usage struct {
	Bytes int64
	Files int64
	Last  time.Time
}

// inode names one file on one filesystem.
type inode struct{ dev, ino uint64 }

// walk sums the space taken under path as du counts it (st_blocks × 512, a
// file with several names once), counts regular files, every name as one,
// and finds the newest file modification time. It never follows a symlink:
// WalkDir reports entries with Lstat. ok is false when path does not exist.
// Entries that vanish mid-walk, as a running job deletes them, are skipped;
// the first other error is returned with the partial sums.
func walk(path string) (usage, bool, error) { return walkCtx(context.Background(), path) }

// walkCtx stops with ctx's error when ctx ends, so a daemon shutting down is
// not held by a walk of a module cache with hundreds of thousands of files.
func walkCtx(ctx context.Context, path string) (usage, bool, error) {
	var u usage
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		return u, false, nil
	} else if err != nil {
		return u, false, err
	}
	z := newSizer()
	walkErr := filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error { return z.visit(ctx, d, err) })
	return z.result(walkErr)
}

// walkFS is walkCtx through fsys, which a caller holding an os.Root passes as
// r.FS(), so that no link met on the way can lead the walk out of the root.
// name is a slash-separated path under fsys.
func walkFS(ctx context.Context, fsys fs.FS, name string) (usage, bool, error) {
	if _, err := fs.Stat(fsys, name); errors.Is(err, fs.ErrNotExist) {
		return usage{}, false, nil
	} else if err != nil {
		return usage{}, false, err
	}
	z := newSizer()
	walkErr := fs.WalkDir(fsys, name, func(_ string, d fs.DirEntry, err error) error { return z.visit(ctx, d, err) })
	return z.result(walkErr)
}

// sizer sums what a directory walk meets.
type sizer struct {
	u     usage
	first error
	seen  map[inode]bool
}

func newSizer() *sizer { return &sizer{seen: map[inode]bool{}} }

func (z *sizer) visit(ctx context.Context, d fs.DirEntry, err error) error {
	if cerr := ctx.Err(); cerr != nil {
		return cerr
	}
	if err == nil {
		var fi fs.FileInfo
		if fi, err = d.Info(); err == nil {
			if id, linked := hardLink(fi); !linked {
				z.u.Bytes += allocated(fi)
			} else if !z.seen[id] {
				z.seen[id] = true
				z.u.Bytes += allocated(fi)
			}
			if fi.Mode().IsRegular() {
				z.u.Files++
				if fi.ModTime().After(z.u.Last) {
					z.u.Last = fi.ModTime()
				}
			}
			return nil
		}
	}
	if !os.IsNotExist(err) && z.first == nil {
		z.first = err
	}
	return nil
}

func (z *sizer) result(walkErr error) (usage, bool, error) {
	if z.first == nil {
		z.first = walkErr
	}
	return z.u, true, z.first
}

// measured is one complete measurement; the service swaps it into its
// snapshot whole, so readers never see half of one.
type measured struct {
	at         time.Time
	toolchains []model.Toolchain
	other      []model.Folder
	caches     []model.PackageCache
	docker     []model.DockerRow
	cacheTypes []model.BuildCacheType
	err        string
}

// inHome refuses a cache path that resolves out of the home through a
// symlinked directory, so sizes from elsewhere on the LXC are not reported as
// a cache's. A nil root (no home yet) has nothing to refuse.
func inHome(r *os.Root, rel string) error {
	if r == nil {
		return nil
	}
	_, err := r.Lstat(filepath.FromSlash(rel))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

func emptyMeasured() measured {
	return measured{
		toolchains: []model.Toolchain{},
		other:      []model.Folder{},
		caches:     []model.PackageCache{},
		docker:     []model.DockerRow{},
		cacheTypes: []model.BuildCacheType{},
	}
}

// measureCache walks each of c's paths under home through r, skipping one
// that resolves out of it, and names every failure through note.
func measureCache(ctx context.Context, r *os.Root, home string, c Cache, note func(string, error)) model.PackageCache {
	pc := model.PackageCache{Name: c.Name, Label: c.Label, Paths: c.abs(home)}
	for i, p := range pc.Paths {
		if err := inHome(r, c.Paths[i]); err != nil {
			note(c.Name, err)
			continue
		}
		var u usage
		var ok bool
		var err error
		if r != nil {
			u, ok, err = walkFS(ctx, r.FS(), c.Paths[i])
		} else {
			u, ok, err = walkCtx(ctx, p)
		}
		note(c.Name, err)
		if !ok {
			continue
		}
		pc.Present = true
		pc.Bytes += u.Bytes
		pc.Files += u.Files
		if !u.Last.IsZero() && (pc.LastWritten == nil || u.Last.After(*pc.LastWritten)) {
			last := u.Last
			pc.LastWritten = &last
		}
	}
	return pc
}

// measure walks every installed toolchain, other tool-cache folder and
// package cache path, then reads Docker's disk usage last. A failed part is
// named in err and the rest is kept.
func measure(ctx context.Context, tools Toolchains, docker Docker, home string, at time.Time) measured {
	m := emptyMeasured()
	m.at = at
	var errs []string
	note := func(what string, err error) {
		if err != nil {
			errs = append(errs, what+": "+err.Error())
		}
	}
	installed, err := tools.Installed()
	note("toolchains", err)
	for _, in := range installed {
		u, _, err := walkCtx(ctx, in.Path)
		note(in.Tool+" "+in.Version, err)
		m.toolchains = append(m.toolchains, model.Toolchain{Tool: in.Tool, Version: in.Version, Arch: in.Arch,
			Path: in.Path, Bytes: u.Bytes, InstalledAt: in.InstalledAt})
	}
	other, err := tools.Other()
	note("tool cache", err)
	for _, name := range other {
		u, _, err := walkCtx(ctx, filepath.Join(tools.Root(), name))
		note(name, err)
		m.other = append(m.other, model.Folder{Name: name, Bytes: u.Bytes})
	}
	r, err := os.OpenRoot(home)
	if err == nil {
		defer r.Close()
	} else if !errors.Is(err, fs.ErrNotExist) {
		note("runner home", err)
	}
	for _, c := range Caches {
		m.caches = append(m.caches, measureCache(ctx, r, home, c, note))
	}
	rows, err := docker.DiskUsage(ctx)
	note("docker system df", err)
	for _, r := range rows {
		m.docker = append(m.docker, model.DockerRow{Type: r.Type, Count: r.Count, Active: r.Active, Bytes: r.Bytes, Reclaimable: r.Reclaimable})
	}
	types, err := docker.BuildCacheUsage(ctx)
	note("docker build cache", err)
	for _, t := range types {
		m.cacheTypes = append(m.cacheTypes, model.BuildCacheType{Type: t.Type, Count: t.Count, Bytes: t.Bytes, Reclaimable: t.Reclaimable})
	}
	m.err = strings.Join(errs, "; ")
	return m
}
