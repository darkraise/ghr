package storage

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func entries(t *testing.T, dir string) []string {
	t.Helper()
	es, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range es {
		out = append(out, e.Name())
	}
	return out
}

// clearIn clears rel, slash-separated and relative to home, through a root at home.
func clearIn(t *testing.T, home, rel string) error {
	t.Helper()
	r, err := os.OpenRoot(home)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	return clearPath(r, filepath.FromSlash(rel), "op1")
}

func TestCacheTable(t *testing.T) {
	want := map[string]string{
		"nuget": ".nuget/packages", "npm": ".npm", "pnpm": ".local/share/pnpm/store", "yarn": ".cache/yarn",
		"pip": ".cache/pip", "gomod": "go/pkg/mod", "gobuild": ".cache/go-build", "maven": ".m2/repository",
		"gradle": ".gradle/caches", "cargo": ".cargo/registry",
	}
	if len(Caches) != len(want) {
		t.Fatalf("%d caches", len(Caches))
	}
	for _, c := range Caches {
		if want[c.Name] != c.Paths[0] {
			t.Errorf("%s: first path %q", c.Name, c.Paths[0])
		}
	}
	if c, ok := cacheByName("gradle"); !ok || c.Label != "Gradle" || len(c.Paths) != 2 || c.Paths[1] != ".gradle/wrapper/dists" {
		t.Fatalf("gradle %+v", c)
	}
	if _, ok := cacheByName("bogus"); ok {
		t.Fatal("bogus found")
	}
}

func TestPresent(t *testing.T) {
	home := t.TempDir()
	c, _ := cacheByName("cargo")
	if c.present(home) {
		t.Fatal("present with no paths")
	}
	if err := os.MkdirAll(filepath.Join(home, ".cargo", "git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !c.present(home) {
		t.Fatal("not present with its second path")
	}
	if got := c.abs(home); got[0] != filepath.Join(home, ".cargo", "registry") {
		t.Fatalf("abs %v", got)
	}
}

func TestClearPathLeavesAnEmptyDirectoryWithTheOldMode(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".nuget", "packages")
	writeFile(t, filepath.Join(dir, "newtonsoft.json", "13.0.3", "lib.dll"), "x")
	if err := os.Chmod(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := clearIn(t, home, ".nuget/packages"); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(dir)
	if err != nil || !fi.IsDir() {
		t.Fatalf("not recreated: %v", err)
	}
	if got := entries(t, dir); len(got) != 0 {
		t.Fatalf("not empty: %v", got)
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o750 {
		t.Fatalf("mode %v", fi.Mode().Perm())
	}
	if got := entries(t, filepath.Join(home, ".nuget")); len(got) != 1 || got[0] != "packages" {
		t.Fatalf("left behind: %v", got)
	}
}

func TestRecreateLeavesADirectoryAJobAlreadyMade(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".npm")
	writeFile(t, filepath.Join(dir, "_cacache", "index"), "new")
	fi, err := os.Stat(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r, err := os.OpenRoot(home)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := recreate(r, ".npm", fi); err != nil {
		t.Fatalf("an existing directory is not a failure: %v", err)
	}
	if got := entries(t, dir); len(got) != 1 || got[0] != "_cacache" {
		t.Fatalf("a job's files were touched: %v", got)
	}
}

func TestClearPathDeletesReadOnlyFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("read-only files block deletion on Windows; the daemon runs on Linux")
	}
	home := t.TempDir()
	dir := filepath.Join(home, "go", "pkg", "mod")
	mod := filepath.Join(dir, "golang.org", "x", "text@v0.30.0")
	writeFile(t, filepath.Join(mod, "go.mod"), "module golang.org/x/text")
	if err := os.Chmod(filepath.Join(mod, "go.mod"), 0o444); err != nil {
		t.Fatal(err)
	}
	if os.Geteuid() == 0 {
		// Go writes module directories read-only too; only root deletes through them.
		if err := os.Chmod(mod, 0o555); err != nil {
			t.Fatal(err)
		}
	}
	if err := clearIn(t, home, "go/pkg/mod"); err != nil {
		t.Fatal(err)
	}
	if got := entries(t, dir); len(got) != 0 {
		t.Fatalf("not empty: %v", got)
	}
}

func TestClearPathRemovesASymlinkNotItsTarget(t *testing.T) {
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "keep.txt"), "keep")
	home := t.TempDir()
	dir := filepath.Join(home, ".npm")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "link")); err != nil {
		t.Skip("no symlinks here:", err)
	}
	if err := clearIn(t, home, ".npm"); err != nil {
		t.Fatal(err)
	}
	if got := entries(t, dir); len(got) != 0 {
		t.Fatalf("not empty: %v", got)
	}
	if _, err := os.Stat(filepath.Join(outside, "keep.txt")); err != nil {
		t.Fatalf("the link's target was deleted: %v", err)
	}
}

func TestClearPathOnASymlinkedCacheRemovesOnlyTheLink(t *testing.T) {
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "keep.txt"), "keep")
	home := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(home, ".npm")); err != nil {
		t.Skip("no symlinks here:", err)
	}
	if err := clearIn(t, home, ".npm"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(home, ".npm")); !os.IsNotExist(err) {
		t.Fatalf("link still there: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "keep.txt")); err != nil {
		t.Fatalf("the link's target was deleted: %v", err)
	}
}

func TestClearCacheRefusesALinkOutOfTheHome(t *testing.T) {
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "pip", "wheel"), "keep")
	home := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(home, ".cache")); err != nil {
		t.Skip("no symlinks here:", err)
	}
	c, _ := cacheByName("pip")
	if err := clearCache(home, c, "op1"); err == nil {
		t.Fatal("cleared through a link out of the home")
	}
	if _, err := os.Stat(filepath.Join(outside, "pip", "wheel")); err != nil {
		t.Fatalf("cleared through the link: %v", err)
	}
	writeFile(t, filepath.Join(outside, ".pip"+clearingTag+"op9", "x"), "keep")
	if err := sweepClearing(home); err == nil {
		t.Fatal("swept through a link out of the home without a word")
	}
	if _, err := os.Stat(filepath.Join(outside, ".pip"+clearingTag+"op9", "x")); err != nil {
		t.Fatalf("swept through the link: %v", err)
	}
}

func TestClearCacheFollowsALinkInsideTheHome(t *testing.T) {
	home := t.TempDir()
	writeFile(t, filepath.Join(home, "elsewhere", "pip", "wheel"), "x")
	if err := os.Symlink("elsewhere", filepath.Join(home, ".cache")); err != nil {
		t.Skip("no symlinks here:", err)
	}
	c, _ := cacheByName("pip")
	if err := clearCache(home, c, "op1"); err != nil {
		t.Fatal(err)
	}
	if got := entries(t, filepath.Join(home, "elsewhere", "pip")); len(got) != 0 {
		t.Fatalf("not empty: %v", got)
	}
	if fi, err := os.Lstat(filepath.Join(home, ".cache")); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the link itself changed: %v", err)
	}
}

func TestClearPathMissingIsNothing(t *testing.T) {
	if err := clearIn(t, t.TempDir(), "absent"); err != nil {
		t.Fatal(err)
	}
}

func TestClearCacheClearsEveryPath(t *testing.T) {
	home := t.TempDir()
	writeFile(t, filepath.Join(home, ".gradle", "caches", "modules-2", "f"), "x")
	writeFile(t, filepath.Join(home, ".gradle", "wrapper", "dists", "gradle-8.10", "g"), "x")
	c, _ := cacheByName("gradle")
	if err := clearCache(home, c, "op1"); err != nil {
		t.Fatal(err)
	}
	for _, p := range c.abs(home) {
		if got := entries(t, p); len(got) != 0 {
			t.Fatalf("%s not empty: %v", p, got)
		}
	}
}

func TestSweepClearingRemovesInterruptedClears(t *testing.T) {
	home := t.TempDir()
	left1 := filepath.Join(home, ".nuget", ".packages"+clearingTag+"op1")
	left2 := filepath.Join(home, ".cache", ".pip"+clearingTag+"op2")
	writeFile(t, filepath.Join(left1, "a"), "x")
	writeFile(t, filepath.Join(left2, "b"), "x")
	writeFile(t, filepath.Join(home, ".cache", "pip", "c"), "x")
	if err := sweepClearing(home); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{left1, left2} {
		if _, err := os.Lstat(p); !os.IsNotExist(err) {
			t.Fatalf("%s survived: %v", p, err)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".cache", "pip", "c")); err != nil {
		t.Fatalf("the live cache was touched: %v", err)
	}
}

func TestSweepClearingWithoutAHome(t *testing.T) {
	if err := sweepClearing(filepath.Join(t.TempDir(), "nobody")); err != nil {
		t.Fatal(err)
	}
}
