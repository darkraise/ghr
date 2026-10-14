//go:build unix

package toolchain

import (
	"os"
	"os/user"
	"strconv"
	"sync"
	"syscall"
)

func ownedByMe(fi os.FileInfo) bool {
	st, ok := fi.Sys().(*syscall.Stat_t)
	return !ok || st.Uid == uint32(os.Geteuid())
}

// lchownTo returns what gives a path inside a root to the user name and
// their group, without following a link at the path. It looks name up when
// first needed and keeps only a lookup that worked, so a user created after
// the daemon started is found.
func lchownTo(name string) func(r *os.Root, rel string) error {
	return lchownBy(user.Lookup, name)
}

func lchownBy(lookup func(string) (*user.User, error), name string) func(r *os.Root, rel string) error {
	var (
		mu       sync.Mutex
		found    bool
		uid, gid int
	)
	ids := func() (int, int, error) {
		mu.Lock()
		defer mu.Unlock()
		if found {
			return uid, gid, nil
		}
		u, err := lookup(name)
		if err != nil {
			return 0, 0, err
		}
		if uid, err = strconv.Atoi(u.Uid); err != nil {
			return 0, 0, err
		}
		if gid, err = strconv.Atoi(u.Gid); err != nil {
			return 0, 0, err
		}
		found = true
		return uid, gid, nil
	}
	return func(r *os.Root, rel string) error {
		uid, gid, err := ids()
		if err != nil {
			return err
		}
		return r.Lchown(rel, uid, gid)
	}
}
