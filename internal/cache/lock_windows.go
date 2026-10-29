//go:build windows

package cache

import (
	"os"

	"golang.org/x/sys/windows"
)

// fileLock is a lock on a file's first byte, held until released or until the
// process ends, so a process that dies holding it never blocks the others.
type fileLock struct {
	file *os.File
}

// acquire waits for the lock on path.
func acquire(path string) (*fileLock, error) {
	return lockFile(path, windows.LOCKFILE_EXCLUSIVE_LOCK)
}

// tryAcquire takes the lock on path if nobody holds it.
func tryAcquire(path string) (*fileLock, bool) {
	lock, err := lockFile(path, windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY)
	return lock, err == nil
}

func lockFile(path string, flags uint32) (*fileLock, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}

	err = windows.LockFileEx(windows.Handle(f.Fd()), flags, 0, 1, 0, &windows.Overlapped{})
	if err != nil {
		f.Close()
		return nil, err
	}

	return &fileLock{file: f}, nil
}

func (l *fileLock) release() {
	l.file.Close()
}
