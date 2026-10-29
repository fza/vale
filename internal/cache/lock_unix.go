//go:build darwin || linux || freebsd || openbsd || netbsd || dragonfly

package cache

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// fileLock is an advisory lock on a file, held until released or until the
// process ends, so a process that dies holding it never blocks the others.
type fileLock struct {
	file *os.File
}

// acquire waits for the lock on path.
func acquire(path string) (*fileLock, error) {
	return lockFile(path, unix.LOCK_EX)
}

// acquireShared waits until nobody holds the lock on path exclusively, and
// holds it alongside any other shared holder.
func acquireShared(path string) (*fileLock, error) {
	return lockFile(path, unix.LOCK_SH)
}

// tryAcquire takes the lock on path if nobody holds it.
func tryAcquire(path string) (*fileLock, bool) {
	lock, err := lockFile(path, unix.LOCK_EX|unix.LOCK_NB)
	return lock, err == nil
}

func lockFile(path string, how int) (*fileLock, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}

	// A signal arriving while the call waits interrupts it without the lock
	// being taken.
	for {
		err = unix.Flock(int(f.Fd()), how)
		if !errors.Is(err, unix.EINTR) {
			break
		}
	}
	if err != nil {
		f.Close()
		return nil, err
	}

	return &fileLock{file: f}, nil
}

func (l *fileLock) release() {
	l.file.Close()
}
