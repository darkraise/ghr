package storage

import (
	"context"
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

// walk sums the space taken under path (st_blocks × 512, as du counts),
// counts regular files and finds the newest file modification time. It
// never follows a symlink: WalkDir reports entries with Lstat. ok is false
// when path does not exist. Entries that vanish mid-walk, as a running job
// deletes them, are skipped; the first other error is returned with the
// partial sums.
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
	var first error
	walkErr := filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		if err == nil {
			var fi fs.FileInfo
			if fi, err = d.Info(); err == nil {
				u.Bytes += allocated(fi)
				if fi.Mode().IsRegular() {
					u.Files++
					if fi.ModTime().After(u.Last) {
						u.Last = fi.ModTime()
					}
				}
				return nil
			}
		}
		if !os.IsNotExist(err) && first == nil {
			first = err
		}
		return nil
	})
	if first == nil {
		first = walkErr
	}
	return u, true, first
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

func emptyMeasured() measured {
	return measured{
		toolchains: []model.Toolchain{},
		other:      []model.Folder{},
		caches:     []model.PackageCache{},
		docker:     []model.DockerRow{},
		cacheTypes: []model.BuildCacheType{},
	}
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
	for _, c := range Caches {
		pc := model.PackageCache{Name: c.Name, Label: c.Label, Paths: c.abs(home)}
		for _, p := range pc.Paths {
			u, ok, err := walkCtx(ctx, p)
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
		m.caches = append(m.caches, pc)
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
