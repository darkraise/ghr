//go:build unix

package toolchain

import (
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"testing"
)

// The runner user may not exist yet when the daemon starts.
func TestLchownLooksTheUserUpAgainAfterAFailure(t *testing.T) {
	dir := t.TempDir()
	touch(t, filepath.Join(dir, "f"))
	r := openRoot(t, dir)
	lookups := 0
	chown := lchownBy(func(name string) (*user.User, error) {
		lookups++
		if lookups == 1 {
			return nil, errors.New("no such user")
		}
		return &user.User{Username: name, Uid: strconv.Itoa(os.Getuid()), Gid: strconv.Itoa(os.Getgid())}, nil
	}, "ghrunner")
	if err := chown(r, "f"); err == nil {
		t.Fatal("chowned without a user")
	}
	for range 2 {
		if err := chown(r, "f"); err != nil {
			t.Fatal(err)
		}
	}
	if lookups != 2 {
		t.Fatalf("%d lookups, want 2: one failed, one kept", lookups)
	}
}
