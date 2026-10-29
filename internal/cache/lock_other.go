//go:build !(darwin || linux || freebsd || openbsd || netbsd || dragonfly || windows)

package cache

import "os"

// fileLock stands in for a lock on a platform without one, where Vale runs as
// the only process using its cache: the browser build, say.
type fileLock struct {
	file *os.File
}

func acquire(path string) (*fileLock, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}

	return &fileLock{file: f}, nil
}

func tryAcquire(path string) (*fileLock, bool) {
	lock, err := acquire(path)
	return lock, err == nil
}

func (l *fileLock) release() {
	l.file.Close()
}
