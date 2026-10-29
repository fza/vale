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

// acquireShared waits until nobody holds the lock on path exclusively, and
// holds it alongside any other shared holder.
func acquireShared(path string) (*fileLock, error) {
	return lockFile(path, 0)
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

	// A lock on Windows keeps other handles from reading the bytes it covers,
	// so it covers one byte far past the note a cleaner writes into the file.
	err = windows.LockFileEx(windows.Handle(f.Fd()), flags, 0, 1, 0, &windows.Overlapped{OffsetHigh: 1})
	if err != nil {
		f.Close()
		return nil, err
	}

	return &fileLock{file: f}, nil
}

func (l *fileLock) release() {
	l.file.Close()
}
