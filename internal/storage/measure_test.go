package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/system"
	"github.com/darkraise/ghr/internal/toolchain"
)

func TestWalkSumsSizeFilesAndNewestTime(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a"), strings.Repeat("x", 5000))
	writeFile(t, filepath.Join(dir, "sub", "b"), "y")
	writeFile(t, filepath.Join(dir, "sub", "c"), "z")
	newest := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	older := newest.Add(-time.Hour)
	for _, p := range []string{"a", filepath.Join("sub", "b")} {
		if err := os.Chtimes(filepath.Join(dir, p), older, older); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chtimes(filepath.Join(dir, "sub", "c"), newest, newest); err != nil {
		t.Fatal(err)
	}
	u, ok, err := walk(dir)
	if err != nil || !ok {
		t.Fatalf("ok %v, err %v", ok, err)
	}
	if u.Files != 3 || !u.Last.Equal(newest) {
		t.Fatalf("usage %+v", u)
	}
	if u.Bytes < 5002 {
		t.Fatalf("%d bytes is less than the files hold", u.Bytes)
	}
}

func TestWalkMissingPath(t *testing.T) {
	u, ok, err := walk(filepath.Join(t.TempDir(), "absent"))
	if ok || err != nil || u != (usage{}) {
		t.Fatalf("usage %+v, ok %v, err %v", u, ok, err)
	}
}

func TestWalkDoesNotFollowSymlinks(t *testing.T) {
	target := t.TempDir()
	writeFile(t, filepath.Join(target, "f1"), "x")
	writeFile(t, filepath.Join(target, "f2"), "x")
	dir := t.TempDir()
	if err := os.Symlink(target, filepath.Join(dir, "link")); err != nil {
		t.Skip("no symlinks here:", err)
	}
	u, _, err := walk(dir)
	if err != nil || u.Files != 0 {
		t.Fatalf("usage %+v, err %v", u, err)
	}
}

func TestMeasureFillsEveryPart(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	node := filepath.Join(root, "node", "22.11.0", "x64")
	writeFile(t, filepath.Join(node, "bin", "node"), "n")
	writeFile(t, filepath.Join(root, "PyPy", "3.10.14", "x64", "bin", "pypy"), "p")
	writeFile(t, filepath.Join(home, ".npm", "_cacache", "index"), "i")
	at := time.Date(2026, 10, 6, 14, 0, 0, 0, time.UTC)
	tools := &fakeTools{root: root, other: []string{"PyPy"},
		installed: []toolchain.Installed{{Tool: "node", Version: "22.11.0", Arch: "x64", Path: node, InstalledAt: at}}}
	disk := &fakeDisk{
		rows:  []system.DiskRow{{Type: "Images", Count: 9, Active: 2, Bytes: 6571000000, Reclaimable: 5627000000}},
		types: []system.CacheTypeUsage{{Type: "regular", Count: 2, Bytes: 1686000000, Reclaimable: 456000000}},
	}
	m := measure(context.Background(), tools, disk, home, at)
	if m.err != "" {
		t.Fatal(m.err)
	}
	if !m.at.Equal(at) {
		t.Fatalf("at %v", m.at)
	}
	if len(m.toolchains) != 1 || m.toolchains[0].Bytes == 0 || m.toolchains[0].Version != "22.11.0" ||
		m.toolchains[0].Path != node || !m.toolchains[0].InstalledAt.Equal(at) {
		t.Fatalf("toolchains %+v", m.toolchains)
	}
	if len(m.other) != 1 || m.other[0].Name != "PyPy" || m.other[0].Bytes == 0 {
		t.Fatalf("other %+v", m.other)
	}
	if len(m.caches) != len(Caches) {
		t.Fatalf("%d caches", len(m.caches))
	}
	by := map[string]model.PackageCache{}
	for _, c := range m.caches {
		by[c.Name] = c
	}
	npm := by["npm"]
	if !npm.Present || npm.Files != 1 || npm.Bytes == 0 || npm.LastWritten == nil || npm.Label != "npm" || npm.Paths[0] != filepath.Join(home, ".npm") {
		t.Fatalf("npm %+v", npm)
	}
	if by["nuget"].Present || by["nuget"].LastWritten != nil {
		t.Fatalf("nuget %+v", by["nuget"])
	}
	if len(m.docker) != 1 || m.docker[0] != (model.DockerRow{Type: "Images", Count: 9, Active: 2, Bytes: 6571000000, Reclaimable: 5627000000}) {
		t.Fatalf("docker %+v", m.docker)
	}
	if len(m.cacheTypes) != 1 || m.cacheTypes[0] != (model.BuildCacheType{Type: "regular", Count: 2, Bytes: 1686000000, Reclaimable: 456000000}) {
		t.Fatalf("cache types %+v", m.cacheTypes)
	}
}

func TestMeasureKeepsFilesystemResultsWhenDockerFails(t *testing.T) {
	home := t.TempDir()
	writeFile(t, filepath.Join(home, ".cache", "pip", "wheels", "w"), "w")
	disk := &fakeDisk{err: errors.New("Cannot connect to the Docker daemon")}
	m := measure(context.Background(), &fakeTools{root: t.TempDir()}, disk, home, time.Now())
	if !strings.Contains(m.err, "docker system df: Cannot connect to the Docker daemon") {
		t.Fatalf("err %q", m.err)
	}
	for _, c := range m.caches {
		if c.Name == "pip" && !c.Present {
			t.Fatal("pip lost with the Docker failure")
		}
	}
	if m.docker == nil || len(m.docker) != 0 || m.cacheTypes == nil {
		t.Fatalf("docker %+v, types %+v", m.docker, m.cacheTypes)
	}
}

func TestEmptyMeasuredHasNoNilSlices(t *testing.T) {
	m := emptyMeasured()
	if m.toolchains == nil || m.other == nil || m.caches == nil || m.docker == nil || m.cacheTypes == nil {
		t.Fatalf("%+v", m)
	}
}
