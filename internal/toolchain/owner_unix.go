//go:build unix

package toolchain

import (
	"os"
	"syscall"
)

func ownedByMe(fi os.FileInfo) bool {
	st, ok := fi.Sys().(*syscall.Stat_t)
	return !ok || st.Uid == uint32(os.Geteuid())
}
