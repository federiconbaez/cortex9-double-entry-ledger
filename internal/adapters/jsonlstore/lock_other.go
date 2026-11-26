//go:build !(linux || darwin || freebsd || netbsd || openbsd || dragonfly)

package jsonlstore

import "os"

// lockSupported is false here: on this platform Open does not lock the file
// and the single-process rule is the operator's responsibility.
const lockSupported = false

func lockFile(*os.File) error { return nil }
