//go:build unix

package runner

import (
	"os"
	"syscall"
)

// hasOtherNames reports whether fi is a file with more than one name.
func hasOtherNames(fi os.FileInfo) bool {
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && !fi.IsDir() && uint64(st.Nlink) > 1
}
