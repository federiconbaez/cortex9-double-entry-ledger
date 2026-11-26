//go:build linux || darwin || freebsd || netbsd || openbsd || dragonfly

package jsonlstore

import (
	"errors"
	"os"
	"syscall"
)

// lockSupported reports whether Open takes an exclusive cross-process lock.
const lockSupported = true

// lockFile takes a non-blocking exclusive flock(2) on f. The lock belongs
// to the open file description, so it is released when f is closed (or the
// process dies) and a second Open of the same path, in this process or in
// another one, fails with ErrLocked.
func lockFile(f *os.File) error {
	err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) //nolint:gosec // a file descriptor fits in int
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return ErrLocked
	}
	return err
}
