// Package cache remembers what Vale reported for a file, so that a file which
// has not changed need not be linted again.
//
// The design follows the Go build cache: entries are addressed by a hash of
// everything that could change the answer, each lives in its own file, and
// nothing indexes them. A missing, truncated or unreadable entry is a miss, so
// an interrupted write or a half-deleted directory costs a re-lint and never a
// wrong result.
//
// Eviction is by last use rather than by size. Reading an entry touches its
// modification time, at most once an hour so that a run does not rewrite
// metadata for every file it reads, and a sweep deletes whatever has gone
// unused for five days. The sweep runs at most once a day, which one marker
// file records.
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// shards is how many directories the entries are spread over, one per leading
// byte of the key.
const shards = 256

const (
	mtimeInterval = 1 * time.Hour
	trimInterval  = 24 * time.Hour
	trimLimit     = 5 * 24 * time.Hour
)

// EnvVar names the directory to store entries in, overriding the default.
const EnvVar = "VALE_CACHE"

// Key addresses one entry.
type Key [sha256.Size]byte

// Cache is a directory of entries, all salted by the same configuration.
//
// Every method is safe for concurrent use and none of them fails: a cache that
// cannot be read or written is a cache that never hits.
type Cache struct {
	dir  string
	salt []byte
	now  func() time.Time
}

// Dir reports where entries are stored.
func Dir() (string, error) {
	if dir := os.Getenv(EnvVar); dir != "" {
		return dir, nil
	}

	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(base, "vale"), nil
}

// Open prepares dir to hold entries salted with salt.
//
// The salt stands for everything shared by every entry -- the version of Vale,
// the configuration, the styles -- so that a change to any of it leaves the
// old entries unreachable rather than wrong.
func Open(dir string, salt []byte) (*Cache, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}

	// Every shard is created up front. Creating one on demand meant a stat
	// for every entry written, which on a first run over a large tree is one
	// system call per file for a directory that already exists.
	for i := range shards {
		if err := os.MkdirAll(filepath.Join(dir, shardName(i)), 0o750); err != nil {
			return nil, err
		}
	}

	c := &Cache{dir: dir, salt: salt, now: time.Now}
	c.trim()

	return c, nil
}

// Clean removes every entry.
func Clean(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}

	for _, entry := range entries {
		if rmErr := os.RemoveAll(filepath.Join(dir, entry.Name())); rmErr != nil {
			return rmErr
		}
	}

	return nil
}

// Size reports how many bytes the entries occupy.
func Size(dir string) (int64, error) {
	var total int64

	err := filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil //nolint:nilerr // an unreadable entry contributes nothing
		}
		if info, statErr := d.Info(); statErr == nil {
			total += info.Size()
		}
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return 0, err
	}

	return total, nil
}

// Key derives the address of an entry from the cache's salt and parts.
//
// Each part is written with its length, so that two different splits of the
// same bytes cannot collide.
func (c *Cache) Key(parts ...[]byte) Key {
	h := sha256.New()
	h.Write(c.salt)

	for _, part := range parts {
		fmt.Fprintf(h, "%d:", len(part))
		h.Write(part)
	}

	return Key(h.Sum(nil))
}

// Get returns the entry stored under key, reporting whether one was found.
func (c *Cache) Get(key Key) ([]byte, bool) {
	name := c.fileName(key)

	data, err := os.ReadFile(name)
	if err != nil {
		return nil, false
	}
	c.markUsed(name)

	return data, true
}

// Put stores data under key, reporting nothing: a cache that cannot be written
// is a cache that does not hit.
func (c *Cache) Put(key Key, data []byte) {
	name := c.fileName(key)

	// Written beside its destination and renamed, so that a reader sees either
	// the whole entry or none of it. A run interrupted mid-write leaves a
	// temporary file, which the sweep collects like any other.
	f, err := os.CreateTemp(filepath.Dir(name), "tmp-")
	if err != nil {
		return
	}

	if _, err = f.Write(data); err != nil {
		f.Close()
		os.Remove(f.Name())
		return
	}
	if err = f.Close(); err != nil {
		os.Remove(f.Name())
		return
	}

	if err = os.Rename(f.Name(), name); err != nil {
		os.Remove(f.Name())
	}
}

// fileName returns the path holding key, sharded by its first byte so that no
// one directory holds every entry.
func (c *Cache) fileName(key Key) string {
	name := hex.EncodeToString(key[:])
	return filepath.Join(c.dir, name[:2], name)
}

// markUsed records that name was read, at most once per mtimeInterval.
func (c *Cache) markUsed(name string) {
	info, err := os.Stat(name)
	if err != nil {
		return
	}

	if now := c.now(); now.Sub(info.ModTime()) >= mtimeInterval {
		os.Chtimes(name, now, now)
	}
}

// trim deletes entries unused for trimLimit, at most once per trimInterval.
func (c *Cache) trim() {
	now := c.now()
	marker := filepath.Join(c.dir, "trim.txt")

	if info, err := os.Stat(marker); err == nil {
		if since := now.Sub(info.ModTime()); since < trimInterval && since > -mtimeInterval {
			return
		}
	}

	cutoff := now.Add(-trimLimit - mtimeInterval)
	for i := range shards {
		c.trimSubdir(filepath.Join(c.dir, shardName(i)), cutoff)
	}

	// The marker is written last, so a sweep that dies part way through is
	// repeated rather than skipped for a day.
	os.WriteFile(marker, []byte(now.Format(time.RFC3339)+"\n"), 0o600)
}

func (c *Cache) trimSubdir(dir string, cutoff time.Time) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		info, infoErr := entry.Info()
		if infoErr != nil || info.ModTime().After(cutoff) {
			continue
		}
		os.Remove(filepath.Join(dir, entry.Name()))
	}
}

func shardName(i int) string {
	return fmt.Sprintf("%02x", i)
}
