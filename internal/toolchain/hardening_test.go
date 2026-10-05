package toolchain

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func symlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("cannot create symlinks here: %v", err)
	}
}

func TestOpDirReplacesAForeignTmp(t *testing.T) {
	f := newFixture(t)
	if err := os.WriteFile(filepath.Join(f.Root, ".tmp"), []byte("planted"), 0o644); err != nil {
		t.Fatal(err)
	}
	op, err := f.opDir()
	if err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(filepath.Dir(op)); err != nil || !fi.IsDir() {
		t.Fatalf(".tmp is %v, %v", fi, err)
	}
}

func TestOpDirDoesNotFollowASymlinkedTmp(t *testing.T) {
	f := newFixture(t)
	victim := filepath.Join(t.TempDir(), "victim")
	if err := os.Mkdir(victim, 0o700); err != nil {
		t.Fatal(err)
	}
	symlinkOrSkip(t, victim, filepath.Join(f.Root, ".tmp"))
	if _, err := f.opDir(); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(filepath.Join(f.Root, ".tmp")); err != nil || !fi.IsDir() {
		t.Fatalf(".tmp is still a link: %v, %v", fi, err)
	}
	if runtime.GOOS != "windows" {
		if st, err := os.Stat(victim); err != nil || st.Mode().Perm() != 0o700 {
			t.Fatalf("the link's target was chmodded: %v, %v", st, err)
		}
	}
	if es, _ := os.ReadDir(victim); len(es) != 0 {
		t.Fatalf("an operation directory was created through the link: %v", es)
	}
}

func TestInstallArchiveKeepsAVersionACompletedMeanwhile(t *testing.T) {
	f := newFixture(t)
	rel := demoRelease(f, tarGz(map[string]string{"bin/demo": "bin"}))
	fetch := f.Fetch
	f.Fetch = func(ctx context.Context, url, dst string) error {
		if err := fetch(ctx, url, dst); err != nil {
			return err
		}
		mkInstall(t, f.Root, "demo", "1.2.3", true)
		return nil
	}
	if err := f.installArchive(context.Background(), "demo", rel, func(string) {}); !errors.Is(err, ErrAlreadyInstalled) {
		t.Fatalf("got %v", err)
	}
	if !exists(filepath.Join(f.Root, "demo", "1.2.3", "x64", "file")) {
		t.Fatal("the job's completed version was replaced")
	}
	if got := tmpEntries(t, f.Root); len(got) != 0 {
		t.Fatalf(".tmp holds %v", got)
	}
}

func TestPythonInstallKeepsAVersionACompletedMeanwhile(t *testing.T) {
	f := pythonFixture(t)
	p := newPython(f.Env)
	rel, _ := p.Resolve(context.Background(), "3.13")
	fetch := f.Fetch
	f.Fetch = func(ctx context.Context, url, dst string) error {
		if err := fetch(ctx, url, dst); err != nil {
			return err
		}
		mkInstall(t, f.Root, "Python", "3.13.7", true)
		return nil
	}
	if err := p.Install(context.Background(), rel, func(string) {}); !errors.Is(err, ErrAlreadyInstalled) {
		t.Fatalf("got %v; setup.sh must not run over a completed version", err)
	}
	if !exists(filepath.Join(f.Root, "Python", "3.13.7", "x64", "file")) {
		t.Fatal("the job's completed version was replaced")
	}
}

func TestInstallArchiveCancelledKeepsAnExistingMarkerlessFolder(t *testing.T) {
	f := newFixture(t)
	existing := mkInstall(t, f.Root, "demo", "1.2.3", false)
	rel := demoRelease(f, tarGz(map[string]string{"bin/demo": "bin"}))
	ctx, cancel := context.WithCancel(context.Background())
	extract := f.Extract
	f.Extract = func(c context.Context, archive, dir string) error {
		defer cancel()
		return extract(c, archive, dir)
	}
	if err := f.installArchive(ctx, "demo", rel, func(string) {}); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
	if !exists(filepath.Join(existing, "file")) {
		t.Fatal("a folder this install did not create was deleted")
	}
}

func TestMkdirOwnedRemovesWhatItCouldNotChown(t *testing.T) {
	e := &Env{Root: t.TempDir(), User: "u", Run: func(context.Context, string, ...string) ([]byte, error) {
		return nil, errors.New("chown failed")
	}}
	dir := filepath.Join(e.Root, "demo")
	if err := e.mkdirOwned(context.Background(), dir); err == nil {
		t.Fatal("no error")
	}
	if exists(dir) {
		t.Fatal("a root-owned directory was left for jobs to trip over")
	}
}

func TestNoSymlinksAndRefusals(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	mkInstall(t, outside, "node", "22.11.0", true)
	symlinkOrSkip(t, filepath.Join(outside, "node"), filepath.Join(root, "node"))
	if err := noSymlinks(root, filepath.Join(root, "node", "22.11.0", "x64")); err == nil {
		t.Fatal("a symlinked tool folder passed")
	}
	if err := noSymlinks(root, filepath.Join(root, "missing", "x")); err != nil {
		t.Fatalf("a missing path: %v", err)
	}
	if err := noSymlinks(root, filepath.Join(outside, "x")); err == nil {
		t.Fatal("a path outside root passed")
	}
	e := &Env{Root: root}
	if err := e.removeLayout("node", "22.11.0"); err == nil || errors.Is(err, ErrNotInstalled) {
		t.Fatalf("remove through a symlink: %v", err)
	}
	if !exists(filepath.Join(outside, "node", "22.11.0", "x64", "file")) {
		t.Fatal("remove followed the link and deleted outside the tool cache")
	}
	if err := e.mkdirOwned(context.Background(), filepath.Join(root, "node", "24.0.0")); err == nil {
		t.Fatal("mkdirOwned created a directory through a symlink")
	}
}

func TestDotnetRemoveDoesNotFollowSymlinkedRuntimeFolders(t *testing.T) {
	f := dotnetFixture(t)
	root := filepath.Join(f.Root, "dotnet")
	dotnetTree(t, root, "8.0.414")
	outside := t.TempDir()
	touch(t, filepath.Join(outside, "8.0.20", "x"))
	symlinkOrSkip(t, outside, filepath.Join(root, "shared", "evil"))
	if err := newDotnet(f.Env).Remove("8.0.414"); err == nil {
		t.Fatal("no error for a symlinked runtime folder")
	}
	if !exists(filepath.Join(outside, "8.0.20", "x")) {
		t.Fatal("remove followed a symlink out of the tool cache")
	}
}

func TestDotnetResolveNormalizesFullVersions(t *testing.T) {
	d := newDotnet(dotnetFixture(t).Env)
	for spec, want := range map[string]string{"v8.0.400": "8.0.400", "08.0.400": "8.0.400"} {
		rel, err := d.Resolve(context.Background(), spec)
		if err != nil || rel.Version != want || rel.Folder != want {
			t.Errorf("%s resolved %+v, %v; want %s", spec, rel, err, want)
		}
	}
}

func TestPickTreatsMissingPartsAsZero(t *testing.T) {
	vs := []string{"1.20", "1.20.14", "1.19"}
	for spec, want := range map[string]string{"1.20.0": "1.20", "1.20": "1.20.14", "1.20.14": "1.20.14", "1.19.0": "1.19"} {
		if got, _ := pick(vs, spec); got != want {
			t.Errorf("pick(%q) = %q, want %q", spec, got, want)
		}
	}
}

func TestGoPickerCanChooseTheDotZeroRelease(t *testing.T) {
	f := newFixture(t)
	f.serve("/go.json", []byte(`[
 {"version":"1.20.14","stable":true,"files":[{"filename":"a","arch":"x64","platform":"linux","download_url":"https://example.invalid/a"}]},
 {"version":"1.20","stable":true,"files":[{"filename":"b","arch":"x64","platform":"linux","download_url":"https://example.invalid/b"}]}
]`))
	f.serve("/godl.json", goReleases(map[string]string{"go1.20.linux-amd64.tar.gz": "20", "go1.20.14.linux-amd64.tar.gz": "2014"}))
	g := newGo(f.Env)
	cs, err := g.Available(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var spec string
	for _, c := range cs {
		if c.Version == "1.20" {
			spec = c.Spec
		}
	}
	if spec != "1.20.0" {
		t.Fatalf("the 1.20 entry offers spec %q; %+v", spec, cs)
	}
	rel, err := g.Resolve(context.Background(), spec)
	if err != nil || rel.URL != f.url+"/dl/go1.20.linux-amd64.tar.gz" || rel.Folder != "1.20.0" {
		t.Fatalf("picker spec resolved %+v, %v", rel, err)
	}
}

func TestJavaResolveNeedsAChecksum(t *testing.T) {
	f := newFixture(t)
	f.serve(javaQuery, []byte(fmt.Sprintf(`[{"version_data":{"semver":"21.0.12+101.0.LTS"},"binaries":[{"package":{"link":%q,"checksum":""}}]}]`, f.url+"/jdk.tar.gz")))
	if _, err := newJava(f.Env).Resolve(context.Background(), "21"); err == nil {
		t.Fatal("a JDK without a checksum resolved")
	}
}

func TestSetAvailableReturnsACopy(t *testing.T) {
	f := newFixture(t)
	f.serve("/node.json", nodeManifest(f.url))
	s := New(f.Env)
	first, err := s.Available(context.Background(), "node")
	if err != nil || len(first) == 0 {
		t.Fatalf("%v, %v", first, err)
	}
	first[0].Version = "tampered"
	second, _ := s.Available(context.Background(), "node")
	if second[0].Version == "tampered" {
		t.Fatal("the cached list was edited through a returned slice")
	}
	second[0].Version = "tampered"
	if third, _ := s.Available(context.Background(), "node"); third[0].Version == "tampered" {
		t.Fatal("the cached list was edited through a cached return")
	}
}
