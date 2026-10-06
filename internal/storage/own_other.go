//go:build !unix

package storage

import "os"

// chownLike does nothing: ownership is a Linux concern, and ghr runs on Linux.
func chownLike(*os.Root, string, os.FileInfo) error { return nil }

func allocated(fi os.FileInfo) int64 { return fi.Size() }
