package storage

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"runtime"
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

func TestWalkStopsWhenCancelled(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"a", "b", "c"} {
		writeFile(t, filepath.Join(dir, f), "x")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	u, ok, err := walkCtx(ctx, dir)
	if !ok || !errors.Is(err, context.Canceled) || u.Files == 3 {
		t.Fatalf("usage %+v, ok %v, err %v", u, ok, err)
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

// A package store keeps one file under several names. It takes its space
// once, as du reports it, while each name still counts as a file.
func TestWalkCountsAHardLinkedFileOnce(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("links are told apart by device and inode")
	}
	const size = 256 << 10
	// Random, so a compressing filesystem cannot shrink it.
	body := make([]byte, size)
	rand.Read(body)
	dir := t.TempDir()
	stored := filepath.Join(dir, "store", "a")
	writeFile(t, stored, string(body))
	if err := os.Mkdir(filepath.Join(dir, "links"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(stored, filepath.Join(dir, "links", "b")); err != nil {
		t.Skip("no hard links here:", err)
	}
	written := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	if err := os.Chtimes(stored, written, written); err != nil {
		t.Fatal(err)
	}
	u, ok, err := walk(dir)
	if err != nil || !ok {
		t.Fatalf("ok %v, err %v", ok, err)
	}
	if u.Files != 2 || !u.Last.Equal(written) {
		t.Fatalf("usage %+v, want both names counted as files", u)
	}
	if u.Bytes < size || u.Bytes >= 2*size {
		t.Fatalf("%d bytes for one %d-byte file with two names", u.Bytes, size)
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

func TestMeasureSkipsACacheBehindALinkOutOfTheHome(t *testing.T) {
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "pip", "wheels", "w"), "w")
	home := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(home, ".cache")); err != nil {
		t.Skip("no symlinks here:", err)
	}
	m := measure(context.Background(), &fakeTools{root: t.TempDir()}, &fakeDisk{}, home, time.Now())
	if !strings.Contains(m.err, "pip: ") {
		t.Fatalf("err %q", m.err)
	}
	for _, c := range m.caches {
		if c.Name == "pip" && (c.Present || c.Bytes != 0) {
			t.Fatalf("measured outside the home: %+v", c)
		}
	}
}

// The link is swapped in after a check by path would have passed, so the walk
// itself must stay inside the root it was given.
func TestMeasureCacheWalksThroughTheOpenRoot(t *testing.T) {
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "w"), "w")
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".cache"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(home, ".cache", "pip")); err != nil {
		t.Skip("no symlinks here:", err)
	}
	r, err := os.OpenRoot(home)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	pip, _ := cacheByName("pip")
	var noted []string
	c := measureCache(context.Background(), r, home, pip, func(name string, err error) {
		if err != nil {
			noted = append(noted, name+": "+err.Error())
		}
	})
	if c.Present || c.Bytes != 0 || c.Files != 0 {
		t.Fatalf("measured outside the home: %+v", c)
	}
	if len(noted) != 1 || !strings.HasPrefix(noted[0], "pip: ") {
		t.Fatalf("noted %q, want the refused walk named once", noted)
	}
}

func TestWalkFSMatchesWalkInsideTheRoot(t *testing.T) {
	home := t.TempDir()
	writeFile(t, filepath.Join(home, "c", "a"), strings.Repeat("x", 5000))
	writeFile(t, filepath.Join(home, "c", "sub", "b"), "y")
	r, err := os.OpenRoot(home)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	want, _, err := walk(filepath.Join(home, "c"))
	if err != nil {
		t.Fatal(err)
	}
	got, ok, err := walkFS(context.Background(), r.FS(), "c")
	if err != nil || !ok || got != want {
		t.Fatalf("usage %+v, ok %v, err %v, want %+v", got, ok, err, want)
	}
	if _, ok, err := walkFS(context.Background(), r.FS(), "absent"); ok || err != nil {
		t.Fatalf("a missing path: ok %v, err %v", ok, err)
	}
}

func TestMeasureFollowsALinkInsideTheHome(t *testing.T) {
	home := t.TempDir()
	writeFile(t, filepath.Join(home, "elsewhere", "pip", "wheels", "w"), "w")
	if err := os.Symlink("elsewhere", filepath.Join(home, ".cache")); err != nil {
		t.Skip("no symlinks here:", err)
	}
	m := measure(context.Background(), &fakeTools{root: t.TempDir()}, &fakeDisk{}, home, time.Now())
	if m.err != "" {
		t.Fatal(m.err)
	}
	for _, c := range m.caches {
		if c.Name == "pip" && (!c.Present || c.Files != 1) {
			t.Fatalf("pip %+v", c)
		}
	}
}

// rootedDisk is a Docker that can also name its data root, as system.Docker does.
type rootedDisk struct {
	*fakeDisk
	root string
	err  error
}

func (d rootedDisk) DataRootBytes(context.Context) (system.DiskUsage, error) {
	return system.DiskUsage{Root: d.root}, d.err
}

// otherFilesystem is a directory that is not on the temp dir's filesystem.
func otherFilesystem(t *testing.T) string {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("needs /proc, a filesystem of its own")
	}
	return "/proc"
}

func TestMeasureSaysWhetherTheCachesShareTheDataRootFilesystem(t *testing.T) {
	home, tools := t.TempDir(), t.TempDir()
	for name, c := range map[string]struct {
		docker Docker
		want   bool
	}{
		"the data root is beside the caches":     {rootedDisk{fakeDisk: &fakeDisk{}, root: home}, true},
		"the data root is on another filesystem": {rootedDisk{fakeDisk: &fakeDisk{}, root: otherFilesystem(t)}, false},
		"the data root cannot be read":           {rootedDisk{fakeDisk: &fakeDisk{}, err: errors.New("docker is down")}, true},
		"the data root is gone":                  {rootedDisk{fakeDisk: &fakeDisk{}, root: filepath.Join(home, "absent")}, true},
		"docker does not name its data root":     {&fakeDisk{}, true},
	} {
		m := measure(context.Background(), &fakeTools{root: tools}, c.docker, home, time.Now())
		if m.cachesOnDataRoot != c.want {
			t.Errorf("%s: cachesOnDataRoot = %v, want %v", name, m.cachesOnDataRoot, c.want)
		}
	}
}

func TestMeasureSeesACacheRootOnAnotherFilesystem(t *testing.T) {
	// The tool cache is the one root on another filesystem: the data root is
	// beside the home.
	home := t.TempDir()
	m := measure(context.Background(), &fakeTools{root: otherFilesystem(t)}, rootedDisk{fakeDisk: &fakeDisk{}, root: home}, home, time.Now())
	if m.cachesOnDataRoot {
		t.Fatal("a tool cache on another filesystem was counted on the data root")
	}
}

func TestEmptyMeasuredHasNoNilSlices(t *testing.T) {
	m := emptyMeasured()
	if !m.cachesOnDataRoot {
		t.Error("before a measurement the caches are assumed to be on the data root")
	}
	if m.toolchains == nil || m.other == nil || m.caches == nil || m.docker == nil || m.cacheTypes == nil {
		t.Fatalf("%+v", m)
	}
}
