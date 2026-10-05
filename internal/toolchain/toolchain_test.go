package toolchain

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestPortedFinders(t *testing.T) {
	root := t.TempDir()
	mkInstall(t, root, "node", "22.11.0", true)
	mkInstall(t, root, "node", "22.12.0", false)
	mkInstall(t, root, "node", "24.1.0", true)
	if got := toolCacheFind(root, "node", "22"); got != filepath.Join(root, "node", "22.11.0", "x64") {
		t.Fatalf("22 found %q; a folder without a marker must not count", got)
	}
	if got := toolCacheFind(root, "node", "24.1.0"); got == "" {
		t.Fatal("explicit 24.1.0 not found")
	}
	mkInstall(t, root, "Java_Temurin-Hotspot_jdk", "21.0.8-9", true)
	mkInstall(t, root, "Java_Temurin-Hotspot_jdk", "21.0.12.1-1", true)
	if got := toolCacheFind(root, "Java_Temurin-Hotspot_jdk", "21"); got != "" {
		t.Fatalf("tool-cache find accepted a prerelease folder: %q", got)
	}
	if got := javaFind(root, "21"); got != filepath.Join(root, "Java_Temurin-Hotspot_jdk", "21.0.8-9", "x64") {
		t.Fatalf("javaFind 21 found %q; a four-part folder is not semver", got)
	}
	dn := filepath.Join(root, "dotnet")
	if err := os.MkdirAll(filepath.Join(dn, "sdk", "8.0.100"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := dotnetFind(dn, "8.0.x"); got != "" {
		t.Fatalf("an SDK folder without dotnet.dll counted: %q", got)
	}
	if err := os.WriteFile(filepath.Join(dn, "sdk", "8.0.100", "dotnet.dll"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := dotnetFind(dn, "8.0.x"); got == "" {
		t.Fatal("8.0.x not found")
	}
}

func TestPick(t *testing.T) {
	vs := []string{"22.11.0", "24.9.0", "22.9.1", "1.25", "3.13.7", "3.14.0"}
	cases := []struct{ spec, want string }{
		{"latest", "24.9.0"},
		{"22", "22.11.0"},
		{"22.9", "22.9.1"},
		{"22.9.1", "22.9.1"},
		{"3.13", "3.13.7"},
		{"1.25", "1.25"},
		{"20", ""},
		{"x", ""},
	}
	for _, c := range cases {
		got, ok := pick(vs, c.spec)
		if got != c.want || ok != (c.want != "") {
			t.Errorf("pick(%q) = %q, %v; want %q", c.spec, got, ok, c.want)
		}
	}
}

func TestNewestFirstAndLooseCompare(t *testing.T) {
	vs := []string{"21.0.8+9", "21.0.12+101.0.LTS", "8.0.414", "10.0.105", "9.0.311"}
	newestFirst(vs)
	want := []string{"21.0.12+101.0.LTS", "21.0.8+9", "10.0.105", "9.0.311", "8.0.414"}
	if !reflect.DeepEqual(vs, want) {
		t.Fatalf("newestFirst = %v", vs)
	}
}

func TestMakeSemver(t *testing.T) {
	for in, want := range map[string]string{"1.25": "1.25.0", "1.25.1": "1.25.1", "1": "1.0.0"} {
		if got := makeSemver(in); got != want {
			t.Errorf("makeSemver(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidFolder(t *testing.T) {
	for name, want := range map[string]bool{"22.11.0": true, "": false, ".": false, "..": false, "a/b": false, `a\b`: false} {
		if got := validFolder(name); got != want {
			t.Errorf("validFolder(%q) = %v", name, got)
		}
	}
}

func TestListLayout(t *testing.T) {
	e := &Env{Root: t.TempDir()}
	if got, err := e.listLayout("node", "node", identity); err != nil || got != nil {
		t.Fatalf("missing tool folder: %v, %v", got, err)
	}
	mkInstall(t, e.Root, "node", "22.11.0", true)
	mkInstall(t, e.Root, "node", "24.9.0", true)
	mkInstall(t, e.Root, "node", "23.0.0", false)
	if err := os.WriteFile(filepath.Join(e.Root, "node", "stray"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := e.listLayout("node", "node", identity)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Version != "24.9.0" || got[1].Version != "22.11.0" {
		t.Fatalf("listed %+v", got)
	}
	g := got[0]
	if g.Tool != "node" || g.Arch != "x64" || g.Path != filepath.Join(e.Root, "node", "24.9.0", "x64") || g.InstalledAt.IsZero() {
		t.Fatalf("entry %+v", g)
	}
}

func TestRemoveLayout(t *testing.T) {
	e := &Env{Root: t.TempDir()}
	dir := mkInstall(t, e.Root, "node", "22.11.0", true)
	if err := e.removeLayout("node", "22.11.0"); err != nil {
		t.Fatal(err)
	}
	if exists(dir) || exists(dir+".complete") || exists(filepath.Dir(dir)) {
		t.Fatal("version left behind")
	}
	if err := e.removeLayout("node", "22.11.0"); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("second remove: %v", err)
	}
	if err := e.removeLayout("node", ".."); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("remove ..: %v", err)
	}
	dir = mkInstall(t, e.Root, "node", "24.9.0", true)
	if err := os.WriteFile(filepath.Join(filepath.Dir(dir), "x86.complete"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := e.removeLayout("node", "24.9.0"); err != nil {
		t.Fatal(err)
	}
	if !exists(filepath.Join(filepath.Dir(dir), "x86.complete")) {
		t.Fatal("a version folder holding other files must be kept")
	}
}

func TestRemoveLayoutDeletesTheMarkerFirst(t *testing.T) {
	e := &Env{Root: t.TempDir()}
	dir := mkInstall(t, e.Root, "node", "22.11.0", false)
	// A non-empty directory in place of the marker makes deleting it fail,
	// which shows whether the version was touched before the marker.
	if err := os.MkdirAll(filepath.Join(dir+".complete", "stuck"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := e.removeLayout("node", "22.11.0"); err == nil {
		t.Fatal("no error deleting a marker that cannot be deleted")
	}
	if !exists(filepath.Join(dir, "file")) {
		t.Fatal("the version was deleted before its marker")
	}
}
