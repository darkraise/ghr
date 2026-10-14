package toolchain

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
	defer op.Close()
	if fi, err := os.Lstat(filepath.Dir(op.path)); err != nil || !fi.IsDir() {
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
	op, err := f.opDir()
	if err != nil {
		t.Fatal(err)
	}
	defer op.Close()
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

// The tool cache belongs to the runner user, so a job can swap .tmp for a
// link at any moment. OpID runs after opDir has looked at .tmp and before it
// makes the operation directory.
func TestOpDirDoesNotFollowATmpSwappedForALink(t *testing.T) {
	f := newFixture(t)
	victim := filepath.Join(t.TempDir(), "victim")
	if err := os.Mkdir(victim, 0o700); err != nil {
		t.Fatal(err)
	}
	symlinkOrSkip(t, victim, filepath.Join(t.TempDir(), "probe"))
	tmp, moved := filepath.Join(f.Root, ".tmp"), filepath.Join(f.Root, ".moved")
	f.OpID = func() string {
		if err := os.Rename(tmp, moved); err != nil {
			t.Error(err)
		}
		if err := os.Symlink(victim, tmp); err != nil {
			t.Error(err)
		}
		return "op1"
	}
	op, err := f.opDir()
	if err != nil {
		t.Fatal(err)
	}
	defer op.Close()
	if es, _ := os.ReadDir(victim); len(es) != 0 {
		t.Fatalf("an operation directory was created through the link: %v", es)
	}
	if runtime.GOOS != "windows" {
		if st, err := os.Stat(victim); err != nil || st.Mode().Perm() != 0o700 {
			t.Fatalf("the link's target was chmodded: %v, %v", st, err)
		}
	}
	if !exists(filepath.Join(moved, "op1")) {
		t.Fatal("the operation directory was not made in the directory opDir had opened")
	}
}

func TestOpenOwnedOpensOnlyTheDirectoryItself(t *testing.T) {
	dir := t.TempDir()
	own := filepath.Join(dir, "own")
	if err := os.Mkdir(own, 0o700); err != nil {
		t.Fatal(err)
	}
	r, err := openOwned(own)
	if err != nil {
		t.Fatal(err)
	}
	r.Close()
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	symlinkOrSkip(t, own, link)
	for _, path := range []string{link, file, filepath.Join(dir, "missing")} {
		if r, err := openOwned(path); err == nil {
			r.Close()
			t.Errorf("%s was opened as a directory of this user", path)
		}
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
	if err := f.installArchive(context.Background(), "demo", Origin{Dir: f.url + "/"}, rel, func(string) {}); !errors.Is(err, ErrAlreadyInstalled) {
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
	if err := f.installArchive(ctx, "demo", Origin{Dir: f.url + "/"}, rel, func(string) {}); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
	if !exists(filepath.Join(existing, "file")) {
		t.Fatal("a folder this install did not create was deleted")
	}
}

func openRoot(t *testing.T, dir string) *os.Root {
	t.Helper()
	r, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	return r
}

func TestMkdirOwnedRemovesWhatItCouldNotChown(t *testing.T) {
	e := &Env{Root: t.TempDir(), User: "u", Chown: func(*os.Root, string) error {
		return errors.New("chown failed")
	}}
	if err := e.mkdirOwned(openRoot(t, e.Root), "demo"); err == nil {
		t.Fatal("no error")
	}
	if exists(filepath.Join(e.Root, "demo")) {
		t.Fatal("a root-owned directory was left for jobs to trip over")
	}
}

func TestRemoveLayoutAndMkdirOwnedRefuseALinkOutOfTheToolCache(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	mkInstall(t, outside, "node", "22.11.0", true)
	symlinkOrSkip(t, filepath.Join(outside, "node"), filepath.Join(root, "node"))
	e := &Env{Root: root, Chown: func(*os.Root, string) error { return nil }}
	if err := e.removeLayout("node", "22.11.0"); err == nil || errors.Is(err, ErrNotInstalled) {
		t.Fatalf("remove through a symlink: %v", err)
	}
	if !exists(filepath.Join(outside, "node", "22.11.0", "x64", "file")) || !exists(filepath.Join(outside, "node", "22.11.0", "x64.complete")) {
		t.Fatal("remove followed the link and deleted outside the tool cache")
	}
	if err := e.mkdirOwned(openRoot(t, root), filepath.Join("node", "24.0.0")); err == nil {
		t.Fatal("mkdirOwned created a directory through a symlink")
	}
	if exists(filepath.Join(outside, "node", "24.0.0")) {
		t.Fatal("mkdirOwned created a directory outside the tool cache")
	}
}

// The runner user owns the tool folder, so a job can swap the version folder
// ghr has just made for a link out of the tool cache.
func TestInstallArchiveDoesNotFollowAVersionFolderSwappedForALink(t *testing.T) {
	f := newFixture(t)
	outside := t.TempDir()
	theirs := mkInstall(t, outside, "", "", false)
	symlinkOrSkip(t, outside, filepath.Join(t.TempDir(), "probe"))
	ver := filepath.Join(f.Root, "demo", "1.2.3")
	f.host.onChown = func(path string) {
		if path != ver {
			return
		}
		if err := os.Remove(ver); err != nil {
			t.Error(err)
		}
		if err := os.Symlink(outside, ver); err != nil {
			t.Error(err)
		}
	}
	rel := demoRelease(f, tarGz(map[string]string{"bin/demo": "bin"}))
	if err := f.installArchive(context.Background(), "demo", Origin{Dir: f.url + "/"}, rel, func(string) {}); err == nil {
		t.Fatal("installed through a link out of the tool cache")
	}
	if !exists(filepath.Join(theirs, "file")) {
		t.Fatal("the install removed a folder outside the tool cache")
	}
	if exists(filepath.Join(theirs, "bin")) || exists(theirs+".complete") {
		t.Fatal("the install wrote outside the tool cache")
	}
}

// The runner user owns the tool cache, so a job can swap .tmp for a link at
// any moment; the operation's id shows in tar's command line.
func TestInstallsDoNotChownThroughATmpSwappedForALink(t *testing.T) {
	cases := map[string]func(t *testing.T, swap func(root string)) *fixture{
		"archive": func(t *testing.T, swap func(string)) *fixture {
			f := newFixture(t)
			f.Extract = afterwards(f.Extract, func() { swap(f.Root) })
			rel := demoRelease(f, tarGz(map[string]string{"demo-1.2.3/bin/demo": "bin"}))
			f.installArchive(context.Background(), "demo", Origin{Dir: f.url + "/"}, rel, func(string) {})
			return f
		},
		"python": func(t *testing.T, swap func(string)) *fixture {
			f := pythonFixture(t)
			f.Extract = afterwards(f.Extract, func() { swap(f.Root) })
			var env map[string]string
			f.host.scripts["setup.sh"] = fakeSetup("3.13.7", &env, false)
			p := newPython(f.Env)
			rel, _ := p.Resolve(context.Background(), "3.13")
			p.Install(context.Background(), rel, func(string) {})
			return f
		},
		"dotnet": func(t *testing.T, swap func(string)) *fixture {
			f := dotnetFixture(t)
			fetch := f.Fetch
			f.Fetch = func(ctx context.Context, url, dst string) error {
				err := fetch(ctx, url, dst)
				swap(f.Root)
				return err
			}
			f.host.scripts["dotnet-install.sh"] = func(string, map[string]string, []string) error { return nil }
			d := newDotnet(f.Env)
			rel, _ := d.Resolve(context.Background(), "8.0")
			d.Install(context.Background(), rel, func(string) {})
			return f
		},
	}
	for name, install := range cases {
		t.Run(name, func(t *testing.T) {
			outside := t.TempDir()
			planted := []string{"op1/x/evil/file", "op1/dotnet-install.sh", "op1/x/setup.sh"}
			for _, p := range planted {
				touch(t, filepath.Join(outside, filepath.FromSlash(p)))
			}
			symlinkOrSkip(t, outside, filepath.Join(t.TempDir(), "probe"))
			var theirs []os.FileInfo
			filepath.Walk(outside, func(_ string, fi os.FileInfo, err error) error {
				if err == nil {
					theirs = append(theirs, fi)
				}
				return nil
			})
			f := install(t, func(root string) {
				tmp := filepath.Join(root, ".tmp")
				if err := os.Rename(tmp, filepath.Join(root, ".moved")); err != nil {
					t.Error(err)
				}
				if err := os.Symlink(outside, tmp); err != nil {
					t.Error(err)
				}
			})
			for _, got := range f.host.chowned {
				for _, fi := range theirs {
					if os.SameFile(got, fi) {
						t.Errorf("%s outside the tool cache was chowned", fi.Name())
					}
				}
			}
			for _, p := range planted {
				if !exists(filepath.Join(outside, filepath.FromSlash(p))) {
					t.Errorf("%s outside the tool cache was removed", p)
				}
			}
		})
	}
}

// afterwards runs then once extract has.
func afterwards(extract func(context.Context, string, string) error, then func()) func(context.Context, string, string) error {
	return func(ctx context.Context, archive, dir string) error {
		err := extract(ctx, archive, dir)
		then()
		return err
	}
}

func TestDotnetFailedInstallRemovesNothingThroughASymlink(t *testing.T) {
	f := dotnetFixture(t)
	outside := t.TempDir()
	touch(t, filepath.Join(outside, "8.0.414", "x"))
	symlinkOrSkip(t, outside, filepath.Join(t.TempDir(), "probe"))
	// dotnet-install.sh runs as the runner user, who owns <Root>/dotnet.
	f.host.scripts["dotnet-install.sh"] = func(string, map[string]string, []string) error {
		if err := os.Symlink(outside, filepath.Join(f.Root, "dotnet", "sdk")); err != nil {
			return err
		}
		return errors.New("download failed")
	}
	d := newDotnet(f.Env)
	rel, _ := d.Resolve(context.Background(), "8.0")
	if err := d.Install(context.Background(), rel, func(string) {}); err == nil || !strings.Contains(err.Error(), "download failed") {
		t.Fatalf("got %v", err)
	}
	if !exists(filepath.Join(outside, "8.0.414", "x")) {
		t.Fatal("the failed install removed a folder outside the tool cache")
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
