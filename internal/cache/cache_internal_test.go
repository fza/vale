package cache

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// testClock is a clock the tests move by hand.
type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func newTestClock() *testClock {
	return &testClock{now: time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)}
}

func (tc *testClock) read() time.Time {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	return tc.now
}

func (tc *testClock) advance(d time.Duration) {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	tc.now = tc.now.Add(d)
}

// openRun opens dir as one run of Vale would, on clock.
func openRun(t *testing.T, dir string, clock *testClock) *Cache {
	t.Helper()

	c, err := Open(dir, []byte("salt"))
	if err != nil {
		t.Fatal(err)
	}
	c.now = clock.read
	t.Cleanup(c.Close)

	return c
}

func keyFor(c *Cache, name string) Key {
	return c.Key([]byte(name), []byte("content"))
}

// putAll stores one entry per name and flushes them.
func putAll(c *Cache, names ...string) {
	for _, name := range names {
		c.Put(keyFor(c, name), []byte("alerts of "+name))
	}
	c.Flush()
}

func names(prefix string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("%s-%04d.md", prefix, i)
	}
	return out
}

// missing returns the names a fresh run does not find. An entry found with
// data other than what was stored fails the test: damage is a miss, never a
// wrong answer.
func missing(t *testing.T, dir string, clock *testClock, names ...string) []string {
	t.Helper()

	c := openRun(t, dir, clock)
	var out []string
	for _, name := range names {
		data, found := c.Get(keyFor(c, name))
		switch {
		case !found:
			out = append(out, name)
		case string(data) != "alerts of "+name:
			t.Errorf("%s should be a miss or its own entry, got %q", name, data)
		}
	}

	return out
}

func TestPutGetRoundTrip(t *testing.T) {
	dir, clock := t.TempDir(), newTestClock()
	c := openRun(t, dir, clock)
	key := keyFor(c, "docs/one.md")

	if _, found := c.Get(key); found {
		t.Fatal("an unwritten key should not be found")
	}

	c.Put(key, []byte(`[{"check":"Demo.Rule"}]`))
	if got, found := c.Get(key); !found || string(got) != `[{"check":"Demo.Rule"}]` {
		t.Fatalf("the run that stored an entry should read it back, got %q", got)
	}

	if got := missing(t, dir, clock, "docs/one.md"); len(got) != 1 {
		t.Fatal("an entry should reach other runs only once flushed")
	}

	c.Flush()
	next := openRun(t, dir, clock)
	if got, found := next.Get(key); !found || string(got) != `[{"check":"Demo.Rule"}]` {
		t.Fatalf("a flushed entry should be read by the next run, got %q", got)
	}
}

// The key has to separate its parts: two files whose path and content
// concatenate to the same bytes are still two files.
func TestKeySeparatesItsParts(t *testing.T) {
	c := openRun(t, t.TempDir(), newTestClock())

	if c.Key([]byte("ab"), []byte("c")) == c.Key([]byte("a"), []byte("bc")) {
		t.Fatal("a different split should give a different key")
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
	first.Flush()

	second, err := Open(dir, []byte("two"))
	if err != nil {
		t.Fatal(err)
	}
	if _, found := second.Get(second.Key([]byte("f.md"), []byte("x"))); found {
		t.Fatal("a different salt should not reach an existing entry")
	}
}

func TestClean(t *testing.T) {
	dir, clock := t.TempDir(), newTestClock()
	putAll(openRun(t, dir, clock), "f.md")

	size, err := Size(dir)
	if err != nil {
		t.Fatal(err)
	}
	if size == 0 {
		t.Fatal("a written entry should count toward the size")
	}

	if err = Clean(dir); err != nil {
		t.Fatal(err)
	}
	if len(missing(t, dir, clock, "f.md")) != 1 {
		t.Fatal("a cleaned entry should not be found")
	}

	if err = Clean(filepath.Join(dir, "gone")); err != nil {
		t.Fatalf("cleaning a directory that is not there: %v", err)
	}
}

// A write cut short leaves part of a record at the end of the tail. Readers
// stop before it, and the next writer cuts it off, so that what it appends is
// read too.
func TestCutShortTailRecordIsCutOff(t *testing.T) {
	dir, clock := t.TempDir(), newTestClock()
	putAll(openRun(t, dir, clock), "a.md", "b.md")

	tail := filepath.Join(dir, tailName)
	whole, err := os.ReadFile(tail)
	if err != nil {
		t.Fatal(err)
	}

	cut := encodeRecord(nil, 0, record{key: Key{1}, data: []byte("never finished")})
	if err = os.WriteFile(tail, append(whole, cut[:len(cut)-3]...), 0o600); err != nil {
		t.Fatal(err)
	}

	if got := missing(t, dir, clock, "a.md", "b.md"); len(got) != 0 {
		t.Fatalf("records before a cut-short one should be read, missing %v", got)
	}

	putAll(openRun(t, dir, clock), "c.md")
	if got := missing(t, dir, clock, "a.md", "b.md", "c.md"); len(got) != 0 {
		t.Fatalf("a record appended after a cut-short one should be read, missing %v", got)
	}
}

// A damaged record is a miss, never a wrong answer.
func TestDamagedTailRecordIsAMiss(t *testing.T) {
	dir, clock := t.TempDir(), newTestClock()
	putAll(openRun(t, dir, clock), "a.md", "b.md")

	tail := filepath.Join(dir, tailName)
	content, err := os.ReadFile(tail)
	if err != nil {
		t.Fatal(err)
	}
	content[len(content)-1] ^= 0xff
	if err = os.WriteFile(tail, content, 0o600); err != nil {
		t.Fatal(err)
	}

	got := missing(t, dir, clock, "a.md", "b.md")
	if len(got) != 1 || got[0] != "b.md" {
		t.Fatalf("only the damaged record should be a miss, missing %v", got)
	}
}

// sealed returns a cache whose every flush seals the tail into a segment.
func sealed(t *testing.T, dir string, clock *testClock) *Cache {
	t.Helper()

	c := openRun(t, dir, clock)
	c.sealBytes = 1

	return c
}

func TestSealMovesTheTailIntoASegment(t *testing.T) {
	dir, clock := t.TempDir(), newTestClock()
	stored := names("doc", 300)
	putAll(sealed(t, dir, clock), stored...)

	m, err := readManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Segments) != 1 || m.Segments[0].Records != len(stored) {
		t.Fatalf("the tail should be sealed into one segment, got %+v", m.Segments)
	}

	info, err := os.Stat(filepath.Join(dir, tailName))
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != int64(len(tailMagic)) {
		t.Fatal("a sealed tail should be emptied")
	}

	if got := missing(t, dir, clock, stored...); len(got) != 0 {
		t.Fatalf("every sealed entry should be found, missing %d", len(got))
	}
}

// segmentPath returns the path of the only segment in dir.
func segmentPath(t *testing.T, dir string) string {
	t.Helper()

	m, err := readManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Segments) != 1 {
		t.Fatalf("expected one segment, got %d", len(m.Segments))
	}

	return filepath.Join(dir, m.Segments[0].Name)
}

func TestDamagedSegmentRecordIsAMiss(t *testing.T) {
	dir, clock := t.TempDir(), newTestClock()
	stored := names("doc", 3)
	putAll(sealed(t, dir, clock), stored...)

	path := segmentPath(t, dir)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	// The first record's data ends where the second record's key begins.
	length := binary.LittleEndian.Uint32(content[keyBytes:])
	content[recordHead+int(length)-1] ^= 0xff
	if err = os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}

	if got := missing(t, dir, clock, stored...); len(got) != 1 {
		t.Fatalf("exactly the damaged record should be a miss, missing %v", got)
	}
}

func TestDamagedSegmentIndexDropsTheSegment(t *testing.T) {
	dir, clock := t.TempDir(), newTestClock()
	stored := names("doc", 3)
	putAll(sealed(t, dir, clock), stored...)

	path := segmentPath(t, dir)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	// A zeroed first key still sorts before every record, so only the
	// checksum tells the index is not the one written.
	indexAt := binary.LittleEndian.Uint64(content[len(content)-footerBytes:])
	clear(content[indexAt : indexAt+keyBytes])
	if err = os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}

	if got := missing(t, dir, clock, stored...); len(got) != len(stored) {
		t.Fatalf("a segment with a damaged index should be left out, missing %v", got)
	}
}

func TestDamagedSegmentFooterDropsTheSegment(t *testing.T) {
	dir, clock := t.TempDir(), newTestClock()
	stored := names("doc", 3)
	putAll(sealed(t, dir, clock), stored...)

	path := segmentPath(t, dir)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	// The epoch is the one footer field nothing else cross-checks.
	content[len(content)-footerBytes+16] ^= 0x01
	if err = os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}

	if got := missing(t, dir, clock, stored...); len(got) != len(stored) {
		t.Fatalf("a segment with a damaged footer should be left out, missing %v", got)
	}
}

// A cache another build laid out differently is left alone: neither read nor
// written over.
func TestUnknownFormatIsLeftAlone(t *testing.T) {
	dir, clock := t.TempDir(), newTestClock()
	putAll(openRun(t, dir, clock), "a.md")

	manifestPath := filepath.Join(dir, manifestName)
	if err := os.WriteFile(manifestPath, []byte(`{"format":99}`), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(dir, tailName))
	if err != nil {
		t.Fatal(err)
	}

	c := openRun(t, dir, clock)
	if _, found := c.Get(keyFor(c, "a.md")); found {
		t.Error("a cache of another format should not be read")
	}
	putAll(c, "b.md")
	c.Sweep(time.Second, nil)

	after, err := os.ReadFile(filepath.Join(dir, tailName))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Error("a cache of another format should not be written")
	}
	if m, _ := os.ReadFile(manifestPath); string(m) != `{"format":99}` {
		t.Error("the manifest of another format should be left as it is")
	}

	// A run that stores without having looked anything up learns of the
	// format when it flushes.
	putAll(openRun(t, dir, clock), "c.md")
	if after, _ = os.ReadFile(filepath.Join(dir, tailName)); string(after) != string(before) {
		t.Error("a flush into a cache of another format should write nothing")
	}
}

// An entry read in the previous epoch is written again, and so outlives the
// one nobody read.
func TestEntryInUseOutlivesUnusedOne(t *testing.T) {
	dir, clock := t.TempDir(), newTestClock()
	putAll(sealed(t, dir, clock), "used.md", "unused.md")

	clock.advance(epochLength)
	reader := sealed(t, dir, clock)
	if _, found := reader.Get(keyFor(reader, "used.md")); !found {
		t.Fatal("an entry from the previous epoch should still be found")
	}
	reader.Flush()

	clock.advance(epochLength)
	got := missing(t, dir, clock, "used.md", "unused.md")
	if len(got) != 1 || got[0] != "unused.md" {
		t.Fatalf("only the entry nobody read should have expired, missing %v", got)
	}
}

// The tail ages the way segments do: an entry nobody read is gone after the
// epoch following its own.
func TestUnusedTailEntryExpires(t *testing.T) {
	dir, clock := t.TempDir(), newTestClock()
	putAll(openRun(t, dir, clock), "unused.md")

	clock.advance(epochLength)
	if got := missing(t, dir, clock, "unused.md"); len(got) != 0 {
		t.Fatal("an entry of the previous epoch should still be found")
	}

	clock.advance(epochLength)
	if got := missing(t, dir, clock, "unused.md"); len(got) != 1 {
		t.Fatal("an entry unused since the epoch before last should be gone")
	}
}

// Several processes flushing at once lose nothing. Each Cache holds its own
// lock file handle, which is what two processes would hold.
func TestConcurrentWritersLoseNothing(t *testing.T) {
	dir, clock := t.TempDir(), newTestClock()

	var wg sync.WaitGroup
	var all []string
	for w := range 8 {
		stored := names(fmt.Sprintf("writer%d", w), 200)
		all = append(all, stored...)

		c := openRun(t, dir, clock)
		c.sealBytes = 8 << 10
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < len(stored); i += 25 {
				putAll(c, stored[i:i+25]...)
			}
		}()
	}
	wg.Wait()

	if got := missing(t, dir, clock, all...); len(got) != 0 {
		t.Fatalf("concurrent writers should lose nothing, missing %d of %d", len(got), len(all))
	}
}
