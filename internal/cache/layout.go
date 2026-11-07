package cache

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The layouts a cache directory has held. Each has a clean lock of its own:
// whoever deletes a layout's files holds it exclusively, and whoever reads or
// writes them holds it shared. A run therefore never reads a layout half
// deleted, a layout is never deleted by two processes at once, and a run is
// never held up by the deletion of a layout it does not use.
const (
	// layoutFiles is one file per entry, spread over 256 shard directories,
	// with a marker recording the last daily sweep.
	layoutFiles = 0

	// layoutPacked is the tail, the segments and the manifest.
	layoutPacked = 1
)

var layoutNames = map[int]string{
	layoutFiles:  "one file per entry",
	layoutPacked: fmt.Sprintf("packed, format %d", manifestFormat),
}

const (
	trashPrefix    = "trash-"
	filesSweepMark = "trim.txt"
)

func cleanLockName(layout int) string {
	return fmt.Sprintf("clean-v%d.lock", layout)
}

// holdClean takes layout's clean lock exclusively, waiting for it when wait is
// set, and writes into it which layout is being cleaned, by whom, since when.
func holdClean(dir string, layout int, wait bool) (*fileLock, error) {
	path := filepath.Join(dir, cleanLockName(layout))

	var lock *fileLock
	if wait {
		held, err := acquire(path)
		if err != nil {
			return nil, err
		}
		lock = held
	} else {
		held, claimed := tryAcquire(path)
		if !claimed {
			return nil, errCleanUnderWay
		}
		lock = held
	}

	note := fmt.Sprintf("layout v%d (%s), pid %d, since %s\n",
		layout, layoutNames[layout], os.Getpid(), time.Now().Format(time.RFC3339))
	if err := lock.file.Truncate(0); err == nil {
		lock.file.WriteAt([]byte(note), 0)
	}

	return lock, nil
}

var errCleanUnderWay = errors.New("another process is cleaning this layout")

// usePacked holds the packed layout's clean lock shared, waiting out a clean
// under way. The one-file-per-entry layout has no reader left to hold it.
func usePacked(dir string) (*fileLock, error) {
	return acquireShared(filepath.Join(dir, cleanLockName(layoutPacked)))
}

// Clean removes every entry of every layout.
//
// It waits for the runs reading or writing the cache and for a clean already
// under way, and keeps the clean locks, which other processes may be waiting
// on: a lock file removed while another process waits on it would let a
// third take a lock of the same name at once.
func Clean(dir string) error {
	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		return nil
	}

	packed, err := holdClean(dir, layoutPacked, true)
	if err != nil {
		return err
	}
	defer packed.release()

	files, err := holdClean(dir, layoutFiles, true)
	if err != nil {
		return err
	}
	defer files.release()

	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	locks := map[string]bool{cleanLockName(layoutPacked): true, cleanLockName(layoutFiles): true}
	for _, entry := range entries {
		if locks[entry.Name()] {
			continue
		}
		if rmErr := os.RemoveAll(filepath.Join(dir, entry.Name())); rmErr != nil {
			return rmErr
		}
	}

	return nil
}

// CleanOldLayout deletes the one-file-per-entry layout from dir.
//
// It returns at once when another process is cleaning that layout. A clean
// that is cut short leaves the rest in place, and the next sweep that finds
// it starts another, until nothing of the layout is left.
func CleanOldLayout(dir string) error {
	lock, err := holdClean(dir, layoutFiles, false)
	if errors.Is(err, errCleanUnderWay) {
		return nil
	}
	if err != nil {
		return err
	}
	defer lock.release()

	moveOldLayoutAside(dir)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), trashPrefix) {
			if rmErr := os.RemoveAll(filepath.Join(dir, entry.Name())); rmErr != nil {
				return rmErr
			}
		}
	}

	return nil
}

// moveOldLayoutAside renames the shard directories of the one-file-per-entry
// layout out of the way, which takes one rename each, and removes its sweep
// marker. An earlier build that still shares the directory then starts afresh
// in shard directories of its own, rather than in ones being deleted.
func moveOldLayoutAside(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		name := entry.Name()
		switch {
		case entry.IsDir() && isShardName(name):
			os.Rename(filepath.Join(dir, name), filepath.Join(dir, trashPrefix+name+"-"+randomHex()))
		case !entry.IsDir() && name == filesSweepMark:
			os.Remove(filepath.Join(dir, name))
		}
	}
}

// isOldLayout reports whether entry belongs to the one-file-per-entry layout,
// in place or moved aside for deletion.
func isOldLayout(entry os.DirEntry) bool {
	name := entry.Name()
	if entry.IsDir() {
		return isShardName(name) || strings.HasPrefix(name, trashPrefix)
	}

	return name == filesSweepMark
}

// isShardName reports whether name is one of the 256 directories the
// one-file-per-entry layout spread its entries over.
func isShardName(name string) bool {
	if len(name) != 2 {
		return false
	}
	for _, r := range name {
		if !strings.ContainsRune("0123456789abcdef", r) {
			return false
		}
	}

	return true
}
