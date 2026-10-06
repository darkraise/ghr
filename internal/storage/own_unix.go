//go:build unix

package storage

import (
	"os"
	"syscall"
)

// chownLike gives path the owner and group fi records, without following a symlink.
func chownLike(path string, fi os.FileInfo) error {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}
	return os.Lchown(path, int(st.Uid), int(st.Gid))
}

// allocated is the space fi takes on disk, as du counts it.
func allocated(fi os.FileInfo) int64 {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return st.Blocks * 512
	}
	return fi.Size()
}
