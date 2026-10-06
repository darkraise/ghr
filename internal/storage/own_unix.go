//go:build unix

package storage

import (
	"os"
	"os/user"
	"strconv"
	"syscall"
)

// lookupOwner finds the uid and gid of the user name; "" is no owner.
func lookupOwner(name string) (*owner, error) {
	if name == "" {
		return nil, nil
	}
	u, err := user.Lookup(name)
	if err != nil {
		return nil, err
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return nil, err
	}
	gid, err := strconv.Atoi(u.Gid)
	if err != nil {
		return nil, err
	}
	return &owner{uid: uid, gid: gid}, nil
}

// chownTo gives rel to own without following a symlink.
func chownTo(r *os.Root, rel string, own *owner) error {
	if own == nil {
		return nil
	}
	return r.Lchown(rel, own.uid, own.gid)
}

// allocated is the space fi takes on disk, as du counts it.
func allocated(fi os.FileInfo) int64 {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return st.Blocks * 512
	}
	return fi.Size()
}
