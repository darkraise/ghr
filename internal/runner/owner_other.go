//go:build !unix

package runner

import "os"

func fileOwner(os.FileInfo) (uid, gid int, ok bool) { return 0, 0, false }
