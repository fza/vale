package cache

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func openTest(t *testing.T) *Cache {
	t.Helper()

	c, err := Open(t.TempDir(), []byte("salt"))
	if err != nil {
		t.Fatal(err)
	}

	return c
}

func TestPutGetRoundTrip(t *testing.T) {
	c := openTest(t)
	key := c.Key([]byte("docs/one.md"), []byte("content"))

	if _, found := c.Get(key); found {
		t.Fatal("an unwritten key must not be found")
	}

	c.Put(key, []byte(`[{"check":"Demo.Rule"}]`))

	got, found := c.Get(key)
	if !found {
		t.Fatal("a written key must be found")
	}
	if string(got) != `[{"check":"Demo.Rule"}]` {
		t.Fatalf("got %q", got)
	}
}

// The key has to separate its parts: two files whose path and content
// concatenate to the same bytes are still two files.
func TestKeySeparatesItsParts(t *testing.T) {
	c := openTest(t)

	if c.Key([]byte("ab"), []byte("c")) == c.Key([]byte("a"), []byte("bc")) {
		t.Fatal("a different split must give a different key")
	}
}

// A change to anything shared by every entry leaves the old ones unreachable.
func TestSaltSeparatesCaches(t *testing.T) {
	dir := t.TempDir()

	first, err := Open(dir, []byte("one"))
	if err != nil {
		t.Fatal(err)
	}
	first.Put(first.Key([]byte("f.md"), []byte("x")), []byte("stored"))

	second, err := Open(dir, []byte("two"))
	if err != nil {
		t.Fatal(err)
	}
	if _, found := second.Get(second.Key([]byte("f.md"), []byte("x"))); found {
		t.Fatal("a different salt must not reach an existing entry")
	}
}

// A half-written or damaged entry is a miss, never a wrong answer. The caller
// decides what the bytes mean, so the store's part is to hand back what it has
// and to fail closed when it has nothing.
func TestDamagedEntryIsAMiss(t *testing.T) {
	c := openTest(t)
	key := c.Key([]byte("f.md"), []byte("x"))

	c.Put(key, []byte("stored"))
	if err := os.Remove(c.fileName(key)); err != nil {
		t.Fatal(err)
	}

	if _, found := c.Get(key); found {
		t.Fatal("a removed entry must not be found")
	}
}

func TestClean(t *testing.T) {
	dir := t.TempDir()

	c, err := Open(dir, []byte("salt"))
	if err != nil {
		t.Fatal(err)
	}
	key := c.Key([]byte("f.md"), []byte("x"))
	c.Put(key, []byte("stored"))

	size, err := Size(dir)
	if err != nil {
		t.Fatal(err)
	}
	if size == 0 {
		t.Fatal("a written entry must count toward the size")
	}

	if err = Clean(dir); err != nil {
		t.Fatal(err)
	}
	if _, found := c.Get(key); found {
		t.Fatal("a cleaned entry must not be found")
	}

	if err = Clean(filepath.Join(dir, "gone")); err != nil {
		t.Fatalf("cleaning a directory that is not there: %v", err)
	}
}

// Entries are evicted by last use. One read within the run keeps an entry that
// would otherwise have expired.
func TestTrimEvictsOnlyUnusedEntries(t *testing.T) {
	c := openTest(t)

	stale := c.Key([]byte("stale.md"), []byte("x"))
	fresh := c.Key([]byte("fresh.md"), []byte("x"))
	c.Put(stale, []byte("stored"))
	c.Put(fresh, []byte("stored"))

	old := time.Now().Add(-30 * 24 * time.Hour)
	if err := os.Chtimes(c.fileName(stale), old, old); err != nil {
		t.Fatal(err)
	}

	if err := os.Remove(filepath.Join(c.dir, "trim.txt")); err != nil {
		t.Fatal(err)
	}
	c.trim()

	if _, found := c.Get(stale); found {
		t.Error("an entry unused past the limit must be evicted")
	}
	if _, found := c.Get(fresh); !found {
		t.Error("an entry in use must survive")
	}
}

// The sweep costs a scan of every shard, so it runs once a day rather than
// once a run.
func TestTrimRunsAtMostDaily(t *testing.T) {
	c := openTest(t)

	stale := c.Key([]byte("stale.md"), []byte("x"))
	c.Put(stale, []byte("stored"))

	old := time.Now().Add(-30 * 24 * time.Hour)
	if err := os.Chtimes(c.fileName(stale), old, old); err != nil {
		t.Fatal(err)
	}

	// Read through the filesystem rather than through Get, which would mark
	// the entry as used and so decide the question it is asking.
	exists := func() bool {
		_, err := os.Stat(c.fileName(stale))
		return err == nil
	}

	// Open already swept, so this one is inside the interval.
	c.trim()
	if !exists() {
		t.Fatal("a second sweep within the interval must not run")
	}

	c.now = func() time.Time { return time.Now().Add(2 * trimInterval) }
	c.trim()
	if exists() {
		t.Error("a sweep past the interval must run")
	}
}

// Reading marks an entry as used, but not more than once an hour: a run over a
// large tree would otherwise rewrite metadata for every file it reads.
func TestUseIsMarkedAtMostHourly(t *testing.T) {
	c := openTest(t)
	key := c.Key([]byte("f.md"), []byte("x"))
	c.Put(key, []byte("stored"))

	aged := time.Now().Add(-2 * mtimeInterval)
	if err := os.Chtimes(c.fileName(key), aged, aged); err != nil {
		t.Fatal(err)
	}

	c.Get(key)
	info, err := os.Stat(c.fileName(key))
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(info.ModTime()) >= mtimeInterval {
		t.Fatal("a read past the interval must mark the entry as used")
	}

	before := info.ModTime()
	c.Get(key)

	info, err = os.Stat(c.fileName(key))
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(before) {
		t.Error("a second read within the interval must not rewrite the time")
	}
}
