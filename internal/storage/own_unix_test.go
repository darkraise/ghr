//go:build unix

package storage

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestClearPathKeepsTheOwner(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("only root can give a directory to another user")
	}
	home := t.TempDir()
	dir := filepath.Join(home, ".npm")
	writeFile(t, filepath.Join(dir, "f"), "x")
	if err := os.Chown(dir, 12345, 12345); err != nil {
		t.Fatal(err)
	}
	if err := clearIn(t, home, ".npm"); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Lstat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if st := fi.Sys().(*syscall.Stat_t); st.Uid != 12345 || st.Gid != 12345 {
		t.Fatalf("owner %d:%d", st.Uid, st.Gid)
	}
}
