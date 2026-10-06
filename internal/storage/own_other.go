//go:build !unix

package storage

import "os"

// Ownership is a Linux concern, and ghr runs on Linux.

func lookupOwner(string) (*owner, error) { return nil, nil }

func chownTo(*os.Root, string, *owner) error { return nil }

func allocated(fi os.FileInfo) int64 { return fi.Size() }
