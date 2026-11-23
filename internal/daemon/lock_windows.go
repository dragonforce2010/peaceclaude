//go:build windows

package daemon

import "os"

func tryExclusiveLockFile(_ *os.File) error {
	// Exclusive lock file creation/open handles single-instance coordination on Windows.
	return nil
}

func unlockExclusiveFile(_ *os.File) {}
