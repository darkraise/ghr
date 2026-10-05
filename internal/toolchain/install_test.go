package toolchain

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func demoRelease(f *fixture, body []byte) Release {
	f.serve("/demo.tar.gz", body)
	return Release{Tool: "demo", Version: "1.2.3", Folder: "1.2.3", URL: f.url + "/demo.tar.gz", SHA256: sum256(body)}
}

func TestInstallArchiveWritesTheToolCacheLayout(t *testing.T) {
	f := newFixture(t)
	rel := demoRelease(f, tarGz(map[string]string{"demo-1.2.3/bin/demo": "bin"}))
	var steps []string
	if err := f.installArchive(context.Background(), "demo", rel, func(s string) { steps = append(steps, s) }); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(f.Root, "demo", "1.2.3", "x64")
	if got := toolCacheFind(f.Root, "demo", "1.2"); got != dir {
		t.Fatalf("tool-cache find returned %q", got)
	}
	if !exists(filepath.Join(dir, "bin", "demo")) {
		t.Fatal("the archive's lone top folder must become x64")
	}
	if !reflect.DeepEqual(steps, []string{"downloading", "extracting"}) {
		t.Fatalf("progress %v", steps)
	}
	if got := tmpEntries(t, f.Root); len(got) != 0 {
		t.Fatalf(".tmp holds %v", got)
	}
	want := []string{
		"chown -R -h ghrunner:ghrunner " + filepath.Join(f.Root, ".tmp", "op1", "x", "demo-1.2.3"),
		"chown -h ghrunner:ghrunner " + filepath.Join(f.Root, "demo"),
		"chown -h ghrunner:ghrunner " + filepath.Join(f.Root, "demo", "1.2.3"),
		"chown -h ghrunner:ghrunner " + dir + ".complete",
	}
	if got := f.host.commands(); !reflect.DeepEqual(got, want) {
		t.Fatalf("commands\n got %q\nwant %q", got, want)
	}
}

func TestInstallArchiveKeepsARootWithSeveralEntries(t *testing.T) {
	f := newFixture(t)
	rel := demoRelease(f, tarGz(map[string]string{"bin/demo": "bin", "LICENSE": "l"}))
	if err := f.installArchive(context.Background(), "demo", rel, func(string) {}); err != nil {
		t.Fatal(err)
	}
	if !exists(filepath.Join(f.Root, "demo", "1.2.3", "x64", "bin", "demo")) {
		t.Fatal("a root with several entries must be installed as is")
	}
}

func TestInstallArchiveSkipsACompleteVersion(t *testing.T) {
	f := newFixture(t)
	mkInstall(t, f.Root, "demo", "1.2.3", true)
	rel := demoRelease(f, tarGz(map[string]string{"bin/demo": "bin"}))
	if err := f.installArchive(context.Background(), "demo", rel, func(string) {}); !errors.Is(err, ErrAlreadyInstalled) {
		t.Fatalf("got %v", err)
	}
	if f.hitCount("/demo.tar.gz") != 0 {
		t.Fatal("a complete version must not be downloaded")
	}
}

func TestInstallArchiveReplacesAFolderWithoutMarker(t *testing.T) {
	f := newFixture(t)
	stale := mkInstall(t, f.Root, "demo", "1.2.3", false)
	rel := demoRelease(f, tarGz(map[string]string{"bin/demo": "bin"}))
	if err := f.installArchive(context.Background(), "demo", rel, func(string) {}); err != nil {
		t.Fatal(err)
	}
	if exists(filepath.Join(stale, "file")) {
		t.Fatal("the marker-less folder's content survived")
	}
	if !exists(stale + ".complete") {
		t.Fatal("no marker")
	}
}

func TestInstallArchiveFailuresLeaveNothing(t *testing.T) {
	cases := map[string]func(f *fixture, rel *Release){
		"checksum": func(f *fixture, rel *Release) { rel.SHA256 = strings.Repeat("0", 64) },
		"download": func(f *fixture, rel *Release) { rel.URL = f.url + "/missing.tar.gz" },
		"extract":  func(f *fixture, rel *Release) { f.host.failExtract = errors.New("bad archive") },
	}
	for name, breakIt := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			rel := demoRelease(f, tarGz(map[string]string{"bin/demo": "bin"}))
			breakIt(f, &rel)
			err := f.installArchive(context.Background(), "demo", rel, func(string) {})
			if err == nil || !strings.Contains(err.Error(), name) {
				t.Fatalf("error %v does not name %s", err, name)
			}
			dir := filepath.Join(f.Root, "demo", "1.2.3", "x64")
			if exists(dir) || exists(dir+".complete") {
				t.Fatal("a failed install left the version behind")
			}
			if got := tmpEntries(t, f.Root); len(got) != 0 {
				t.Fatalf(".tmp holds %v", got)
			}
		})
	}
}

func TestInstallArchiveCancelledLateLeavesNoVersionFolder(t *testing.T) {
	f := newFixture(t)
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
	if exists(filepath.Join(f.Root, "demo", "1.2.3")) {
		t.Fatal("the version folder this install created survived")
	}
}

func TestInstallArchiveTmpModes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix modes")
	}
	f := newFixture(t)
	rel := demoRelease(f, tarGz(map[string]string{"bin/demo": "bin"}))
	fetch := f.Fetch
	f.Fetch = func(ctx context.Context, url, dst string) error {
		for path, want := range map[string]os.FileMode{filepath.Join(f.Root, ".tmp"): 0o711, filepath.Dir(dst): 0o700} {
			st, err := os.Stat(path)
			if err != nil {
				t.Errorf("%s: %v", path, err)
				continue
			}
			if st.Mode().Perm() != want {
				t.Errorf("%s: mode %v, want %v", path, st.Mode().Perm(), want)
			}
		}
		return fetch(ctx, url, dst)
	}
	if err := f.installArchive(context.Background(), "demo", rel, func(string) {}); err != nil {
		t.Fatal(err)
	}
}

func TestCleanTmpKeepsMarkerlessVersions(t *testing.T) {
	f := newFixture(t)
	if err := os.MkdirAll(filepath.Join(f.Root, ".tmp", "op9", "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	inProgress := mkInstall(t, f.Root, "node", "22.11.0", false)
	if err := f.CleanTmp(); err != nil {
		t.Fatal(err)
	}
	if exists(filepath.Join(f.Root, ".tmp")) {
		t.Fatal(".tmp survived")
	}
	if !exists(inProgress) {
		t.Fatal("a marker-less version folder may be a job's own download and must stay")
	}
}
