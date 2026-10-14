//go:build !unix

package daemon

import "os"

// ghr runs on Linux; elsewhere the probe of the socket is the only guard.
func tryLock(*os.File) (bool, error) { return false, nil }
