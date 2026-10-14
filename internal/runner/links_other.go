//go:build !unix

package runner

import "os"

func hasOtherNames(os.FileInfo) bool { return false }
