// Package cache remembers what Vale reported for a file, so that a file which
// has not changed need not be linted again.
//
// Entries are addressed by a hash of everything that could change the answer,
// and they live in a handful of files however many there are: an append log
// (the tail) that every run adds to, and sorted read-only segments the tail is
// sealed into once it grows. A manifest lists the segments. Many Vale
// processes share one cache at a time: writers take turns on a lock for the
// few milliseconds an append, a seal or a manifest swap takes, and readers
// take no lock at all.
//
// Every record carries a checksum. A record that is cut short, damaged or
// unreadable is a miss, so an interrupted write costs a re-lint and never a
// wrong result.
//
// Eviction is by last use, counted in epochs of a few days. A record lives in
// the epoch it was written in; a hit on one from the previous epoch writes it
// again in the current one, and segments older than the previous epoch are
// deleted whole. An entry nobody reads is gone after one to two epochs.
//
// Upkeep -- deleting old segments, merging small ones, removing what an
// interrupted run or an older layout left behind -- is done by Sweep, which
// one process at a time claims and which stops after a fixed budget and
// resumes on the next run. Opening the cache never sweeps.
package cache

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	epochLength = 3 * 24 * time.Hour

	// sealBytes is the tail size past which it is sealed into a segment. Every
	// run reads the whole tail, so it is kept small.
	sealBytes = 1 << 20

	// maxSegments is how many segments an epoch holds before a sweep merges
	// the smallest of them.
	maxSegments = 8

	// SweepBudget bounds the time one run spends on upkeep. A merge or a
	// deletion that needs longer is resumed by the next run.
	SweepBudget = 200 * time.Millisecond
)

const (
	manifestName = "manifest"
	tailName     = "tail"
	lockName     = "lock"
	sweepName    = "sweep.lock"
	mergeName    = "merge.json"
)

// EnvVar names the directory to store entries in, overriding the default.
const EnvVar = "VALE_CACHE"

// errUnknownFormat marks a cache written by a build whose layout this one does
// not read. Such a cache is left alone rather than overwritten.
var errUnknownFormat = errors.New("unknown cache format")

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

	sealBytes   int
	maxSegments int

	loadOnce sync.Once
	stored   *snapshot

	mu       sync.Mutex
	pending  []record
	recorded map[Key][]byte
	disabled bool
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

	return &Cache{
		dir:         dir,
		salt:        salt,
		now:         time.Now,
		sealBytes:   sealBytes,
		maxSegments: maxSegments,
		recorded:    map[Key][]byte{},
	}, nil
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
	c.mu.Lock()
	data, found := c.recorded[key]
	c.mu.Unlock()
	if found {
		return data, true
	}

	c.loadOnce.Do(c.load)
	if c.stored == nil {
		return nil, false
	}

	current := c.epoch()
	data, written, found := c.stored.find(key, current-1)
	if !found {
		return nil, false
	}

	// A hit from the previous epoch is written again, so that an entry in use
	// outlives the segment it was first stored in.
	if written < current {
		c.record(key, data)
	}

	return data, true
}

// Put stores data under key. It is written by the next Flush.
func (c *Cache) Put(key Key, data []byte) {
	c.record(key, append([]byte(nil), data...))
}

// Flush writes every entry stored since the last Flush, sealing the tail into
// a segment once it has grown past its limit.
func (c *Cache) Flush() {
	c.mu.Lock()
	batch := c.pending
	c.pending = nil
	disabled := c.disabled
	c.mu.Unlock()

	if len(batch) == 0 || disabled {
		return
	}

	lock, err := acquire(filepath.Join(c.dir, lockName))
	if err != nil {
		return
	}
	defer lock.release()

	if _, err = readManifest(c.dir); err == nil {
		err = c.appendTail(batch)
	}
	if errors.Is(err, errUnknownFormat) {
		c.disable()
	}
}

// Close releases the segments the cache has open.
func (c *Cache) Close() {
	c.loadOnce.Do(func() {})
	if c.stored != nil {
		c.stored.close()
	}
}

func (c *Cache) record(key Key, data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.recorded[key] = data
	c.pending = append(c.pending, record{key: key, data: data})
}

func (c *Cache) disable() {
	c.mu.Lock()
	c.disabled = true
	c.mu.Unlock()
}

// load reads the tail and opens the segments the manifest lists.
//
// The tail is read first. A seal writes the segment and the manifest before it
// empties the tail, so whichever side of a seal the two reads fall on, every
// record is in one of them.
func (c *Cache) load() {
	tail, err := readTail(filepath.Join(c.dir, tailName))
	if errors.Is(err, errUnknownFormat) {
		c.disable()
		return
	}

	segments, err := openSegments(c.dir)
	if err != nil {
		c.disable()
		return
	}

	c.stored = &snapshot{tail: tail, segments: segments}
}

// epoch is the epoch the cache's clock is in.
func (c *Cache) epoch() uint32 {
	return epochAt(c.now())
}

func epochAt(t time.Time) uint32 {
	return uint32(t.Unix() / int64(epochLength/time.Second))
}

// record is one entry on its way to the tail.
type record struct {
	key  Key
	data []byte
}

// snapshot is what the cache held when a run first looked.
type snapshot struct {
	tail     map[Key]tailEntry
	segments []*segment
}

// find returns the entry under key from the newest place that holds it,
// ignoring anything written before oldest.
func (s *snapshot) find(key Key, oldest uint32) ([]byte, uint32, bool) {
	if entry, found := s.tail[key]; found && entry.epoch >= oldest {
		return entry.data, entry.epoch, true
	}

	for i := len(s.segments) - 1; i >= 0; i-- {
		seg := s.segments[i]
		if seg.epoch < oldest {
			continue
		}
		if data, found := seg.find(key); found {
			return data, seg.epoch, true
		}
	}

	return nil, 0, false
}

func (s *snapshot) close() {
	for _, seg := range s.segments {
		seg.close()
	}
}
