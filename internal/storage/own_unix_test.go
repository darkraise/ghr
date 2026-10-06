//go:build unix

package storage

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestClearPathGivesTheDirectoryToTheOwner(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("only root can give a directory to another user")
	}
	home := t.TempDir()
	dir := filepath.Join(home, ".npm")
	writeFile(t, filepath.Join(dir, "f"), "x")
	if err := os.Chown(dir, 0, 0); err != nil {
		t.Fatal(err)
	}
	r, err := os.OpenRoot(home)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := clearPath(r, ".npm", "op1", &owner{uid: 12345, gid: 23456}); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Lstat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if st := fi.Sys().(*syscall.Stat_t); st.Uid != 12345 || st.Gid != 23456 {
		t.Fatalf("owner %d:%d", st.Uid, st.Gid)
	}
}

func TestLookupOwner(t *testing.T) {
	if own, err := lookupOwner(""); own != nil || err != nil {
		t.Fatalf("no user: %v %v", own, err)
	}
	if own, err := lookupOwner("root"); err != nil || *own != (owner{}) {
		t.Fatalf("root: %v %v", own, err)
	}
	if _, err := lookupOwner("ghr-no-such-user"); err == nil {
		t.Fatal("an unknown user was found")
	}
}

func TestClearFailsWhenTheOwnerIsUnknown(t *testing.T) {
	s := newService(t, &fakeTools{}, &fakeDisk{})
	s.User = "ghr-no-such-user"
	pkg := filepath.Join(s.Home, ".npm", "f")
	writeFile(t, pkg, "x")
	startService(t, s)
	if err := s.Clear("npm"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the clear", func() bool { return len(s.Snapshot().Operations.Recent) == 1 })
	if got := recentOutcomes(s); len(got) != 1 || got[0] != "clear npm failed" {
		t.Fatalf("recent %q", got)
	}
	if _, err := os.Stat(pkg); err != nil {
		t.Fatalf("cleared without an owner: %v", err)
	}
}
