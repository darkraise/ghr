//go:build unix

package daemon

import (
	"errors"
	"os"
	"syscall"
)

// tryLock takes an exclusive lock on f for as long as f stays open, and
// reports held when another open file already has it.
func tryLock(f *os.File) (held bool, err error) {
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return true, nil
	}
	return false, err
}
