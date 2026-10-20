package fsutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWritePrivate(t *testing.T) {
	for _, tc := range []struct {
		name     string
		existing []byte
	}{
		{"new file", nil},
		{"replaces a readable file", []byte("old contents\n")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "artifact-store-keys")
			if tc.existing != nil {
				if err := os.WriteFile(path, tc.existing, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if err := WritePrivate(path, []byte("secret\n")); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != "secret\n" {
				t.Fatalf("contents %q %v", data, err)
			}
			if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
				t.Fatalf("mode %v %v", fi.Mode(), err)
			}
			if left, _ := filepath.Glob(filepath.Join(dir, ".artifact-store-keys-*")); len(left) != 0 {
				t.Fatalf("temp files left: %v", left)
			}
		})
	}
}

func TestWritePrivateFailsInAMissingDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "token")
	if err := WritePrivate(path, []byte("x")); err == nil {
		t.Fatal("wrote into a missing directory")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("stat %v", err)
	}
}
