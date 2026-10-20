package artifact

import (
	"archive/zip"
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, body := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCollect(t *testing.T) {
	ws := t.TempDir()
	writeTree(t, ws, map[string]string{
		"dist/a.txt":       "a",
		"dist/sub/b.txt":   "b",
		"dist/.hidden":     "h",
		"dist/.git/config": "g",
		"dist/sub/.env":    "e",
		"lib/c.txt":        "c",
		"single.txt":       "s",
		".github/x.yml":    "x",
	})
	if err := os.MkdirAll(filepath.Join(ws, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name     string
		paths    []string
		hidden   bool
		wantRoot string
		want     []string
	}{
		{"directory with a trailing slash", []string{"dist/"}, false, "dist", []string{"a.txt", "sub/b.txt"}},
		{"directory with hidden files", []string{"dist"}, true, "dist", []string{".git/config", ".hidden", "a.txt", "sub/.env", "sub/b.txt"}},
		{"wildcard", []string{"dist/*.txt"}, false, "dist", []string{"a.txt"}},
		{"double star", []string{"dist/**/*.txt"}, false, "dist", []string{"a.txt", "sub/b.txt"}},
		{"wildcard matches dot files when hidden files are included", []string{"dist/*"}, true, "dist", []string{".git/config", ".hidden", "a.txt", "sub/.env", "sub/b.txt"}},
		{"single file keeps no directory", []string{"dist/sub/b.txt"}, false, "dist/sub", []string{"b.txt"}},
		{"two files share their common ancestor", []string{"dist/sub/b.txt", "lib/c.txt"}, false, ".", []string{"dist/sub/b.txt", "lib/c.txt"}},
		{"two wildcards share their common ancestor", []string{"dist/sub/*", "lib/*"}, false, ".", []string{"dist/sub/b.txt", "lib/c.txt"}},
		{"exclude", []string{"dist", "!dist/sub"}, false, "dist", []string{"a.txt"}},
		{"exclude then include again", []string{"dist", "!dist/sub", "dist/sub/b.txt"}, false, "dist", []string{"a.txt", "sub/b.txt"}},
		{"nested search path folds into its ancestor", []string{"dist", "dist/sub"}, false, "dist", []string{"a.txt", "sub/b.txt"}},
		{"comments, blanks and spaces", []string{"# build output", "", "  lib  \r"}, false, "lib", []string{"c.txt"}},
		{"hidden directory with hidden files", []string{".github"}, true, ".github", []string{"x.yml"}},
		{"absolute path", []string{filepath.Join(ws, "lib", "c.txt")}, false, "lib", []string{"c.txt"}},
		{"dot-dot path", []string{"dist/../lib"}, false, "lib", []string{"c.txt"}},
	} {
		root, files, err := Collect(tc.paths, tc.hidden, ws)
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if want := filepath.Join(ws, filepath.FromSlash(tc.wantRoot)); root != want {
			t.Errorf("%s: root %s, want %s", tc.name, root, want)
		}
		if !reflect.DeepEqual(files, tc.want) {
			t.Errorf("%s: files %q, want %q", tc.name, files, tc.want)
		}
	}
	for _, tc := range []struct {
		name  string
		paths []string
	}{
		{"hidden directory", []string{".github"}},
		{"missing path", []string{"missing/"}},
		{"empty directory", []string{"empty"}},
		{"wildcard without a match", []string{"dist/*.exe"}},
		{"only comments", []string{"# nothing", ""}},
		{"only excludes", []string{"!dist"}},
		{"everything excluded", []string{"lib", "!lib/c.txt"}},
	} {
		if _, files, err := Collect(tc.paths, false, ws); !errors.Is(err, ErrNoFiles) {
			t.Errorf("%s: files %q, error %v, want ErrNoFiles", tc.name, files, err)
		}
	}
}

func TestCollectFollowsSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symbolic links need extra rights on Windows")
	}
	ws := t.TempDir()
	writeTree(t, ws, map[string]string{"dist/a.txt": "a", "lib/c.txt": "c"})
	for link, target := range map[string]string{
		"dist/lib":    filepath.Join(ws, "lib"),
		"dist/loop":   filepath.Join(ws, "dist"),
		"dist/broken": filepath.Join(ws, "missing"),
	} {
		if err := os.Symlink(target, filepath.Join(ws, link)); err != nil {
			t.Fatal(err)
		}
	}
	_, files, err := Collect([]string{"dist"}, false, ws)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"a.txt", "lib/c.txt"}; !reflect.DeepEqual(files, want) {
		t.Fatalf("files %q, want %q", files, want)
	}
}

func TestCollectRefusesInvalidFileNames(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows cannot create these names")
	}
	ws := t.TempDir()
	writeTree(t, ws, map[string]string{"dist/ok.txt": "ok", "dist/bad:name.txt": "x"})
	_, _, err := Collect([]string{"dist"}, false, ws)
	if err == nil || !strings.Contains(err.Error(), `"bad:name.txt". Contains the following character: Colon :.`) {
		t.Fatalf("error %v", err)
	}
}

func TestZipRoundTrip(t *testing.T) {
	ws := t.TempDir()
	tree := map[string]string{
		"dist/a.txt":         strings.Repeat("compressible ", 1000),
		"dist/sub/deep/b.md": "b",
		"dist/empty.txt":     "",
	}
	writeTree(t, ws, tree)
	root, files, err := Collect([]string{"dist"}, false, ws)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		level  int
		method uint16
	}{{0, zip.Store}, {1, zip.Deflate}, {6, zip.Deflate}, {9, zip.Deflate}} {
		var buf bytes.Buffer
		if err := WriteZip(&buf, root, files, tc.level); err != nil {
			t.Fatalf("level %d: %v", tc.level, err)
		}
		zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
		if err != nil {
			t.Fatalf("level %d: %v", tc.level, err)
		}
		var names []string
		for _, f := range zr.File {
			names = append(names, f.Name)
			if f.Method != tc.method {
				t.Errorf("level %d: %s method %d, want %d", tc.level, f.Name, f.Method, tc.method)
			}
		}
		if want := []string{"a.txt", "empty.txt", "sub/deep/b.md"}; !reflect.DeepEqual(names, want) {
			t.Fatalf("level %d: entries %q, want %q", tc.level, names, want)
		}
		dest := filepath.Join(t.TempDir(), "out")
		n, err := Extract(bytes.NewReader(buf.Bytes()), int64(buf.Len()), dest)
		if err != nil || n != 3 {
			t.Fatalf("level %d: extracted %d, %v", tc.level, n, err)
		}
		for rel, body := range tree {
			got, err := os.ReadFile(filepath.Join(dest, filepath.FromSlash(strings.TrimPrefix(rel, "dist/"))))
			if err != nil || string(got) != body {
				t.Errorf("level %d: %s = %q, %v", tc.level, rel, got, err)
			}
		}
	}
	if err := WriteZip(&bytes.Buffer{}, root, files, 10); err == nil || !strings.Contains(err.Error(), "compression level 10 is not between 0 and 9") {
		t.Fatalf("level 10: %v", err)
	}
}

func TestExtractOverwritesAndKeepsDirectories(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range []string{"dir/", "dir/a.txt"} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(name, "/") {
			w.Write([]byte("new"))
		}
	}
	zw.Close()
	dest := t.TempDir()
	writeTree(t, dest, map[string]string{"dir/a.txt": "old content", "keep.txt": "k"})
	n, err := Extract(bytes.NewReader(buf.Bytes()), int64(buf.Len()), dest)
	if err != nil || n != 1 {
		t.Fatalf("extracted %d, %v", n, err)
	}
	if got, _ := os.ReadFile(filepath.Join(dest, "dir", "a.txt")); string(got) != "new" {
		t.Fatalf("a.txt %q", got)
	}
	if got, _ := os.ReadFile(filepath.Join(dest, "keep.txt")); string(got) != "k" {
		t.Fatalf("keep.txt %q", got)
	}
}

func TestExtractRefusesEscapingEntries(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mode    fs.FileMode
		wantErr string
	}{
		{"../evil.txt", 0, "would be written outside the destination"},
		{"a/../../evil.txt", 0, "would be written outside the destination"},
		{"/evil.txt", 0, "would be written outside the destination"},
		{`..\evil.txt`, 0, "would be written outside the destination"},
		{"", 0, "would be written outside the destination"},
		{"link", fs.ModeSymlink | 0o777, "is not a regular file or directory"},
	} {
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		w, err := zw.Create("good.txt")
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte("good"))
		hdr := &zip.FileHeader{Name: tc.name, Method: zip.Store}
		if tc.mode != 0 {
			hdr.SetMode(tc.mode)
		}
		if w, err = zw.CreateHeader(hdr); err != nil {
			t.Fatal(err)
		}
		w.Write([]byte("../outside"))
		zw.Close()

		parent := t.TempDir()
		dest := filepath.Join(parent, "dest")
		_, err = Extract(bytes.NewReader(buf.Bytes()), int64(buf.Len()), dest)
		if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
			t.Errorf("%q: error %v, want %q", tc.name, err, tc.wantErr)
		}
		if entries, _ := os.ReadDir(parent); len(entries) != 0 {
			t.Errorf("%q: wrote %v before refusing", tc.name, entries)
		}
	}
}

func TestExtractRefusesNonZip(t *testing.T) {
	body := []byte("not a zip")
	if _, err := Extract(bytes.NewReader(body), int64(len(body)), t.TempDir()); err == nil {
		t.Fatal("accepted a file that is not a zip")
	}
}
