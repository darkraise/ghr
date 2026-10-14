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

// hardLink identifies fi when it is a file with more than one name. A
// directory's link count is its subdirectories, not other names.
func hardLink(fi os.FileInfo) (inode, bool) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok || fi.IsDir() || st.Nlink < 2 {
		return inode{}, false
	}
	return inode{uint64(st.Dev), uint64(st.Ino)}, true
}

// device is the filesystem fi is on.
func device(fi os.FileInfo) (uint64, bool) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return uint64(st.Dev), true
}
