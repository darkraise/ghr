//go:build unix

package toolchain

import (
	"os"
	"os/user"
	"strconv"
	"syscall"
)

func ownedByMe(fi os.FileInfo) bool {
	st, ok := fi.Sys().(*syscall.Stat_t)
	return !ok || st.Uid == uint32(os.Geteuid())
}

// lchownTo looks name up once and returns what gives a path inside a root to
// that user and their group, without following a link at the path.
func lchownTo(name string) func(r *os.Root, rel string) error {
	var uid, gid int
	u, err := user.Lookup(name)
	if err == nil {
		uid, err = strconv.Atoi(u.Uid)
	}
	if err == nil {
		gid, err = strconv.Atoi(u.Gid)
	}
	return func(r *os.Root, rel string) error {
		if err != nil {
			return err
		}
		return r.Lchown(rel, uid, gid)
	}
}
