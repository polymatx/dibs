//go:build unix

package store

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// tryLock attempts a non-blocking exclusive flock on f. It reports whether
// the lock was acquired; "already held elsewhere" is not an error.
func tryLock(f *os.File) (bool, error) {
	err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
		return false, nil
	}
	return false, err
}

func unlock(f *os.File) {
	_ = unix.Flock(int(f.Fd()), unix.LOCK_UN)
}
