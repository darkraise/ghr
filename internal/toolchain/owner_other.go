//go:build !unix

package toolchain

import "os"

func ownedByMe(os.FileInfo) bool { return true }

func lchownTo(string) func(*os.Root, string) error {
	return func(*os.Root, string) error { return nil }
}
