//go:build !unix

package toolchain

import "os"

func ownedByMe(os.FileInfo) bool { return true }
