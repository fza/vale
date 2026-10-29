package cache

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// oldLayout writes the one-file-per-entry layout: shard directories of entry
// files and the daily sweep's marker.
func oldLayout(t *testing.T, dir string, shards, perShard int) {
	t.Helper()

	for s := range shards {
		shard := filepath.Join(dir, fmt.Sprintf("%02x", s))
		if err := os.MkdirAll(shard, 0o750); err != nil {
			t.Fatal(err)
		}
		for e := range perShard {
			if err := os.WriteFile(filepath.Join(shard, fmt.Sprintf("%02x%062x", s, e)), []byte("[]"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}

	if err := os.WriteFile(filepath.Join(dir, "trim.txt"), []byte("2026-10-29T15:37:27+01:00\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func countFiles(t *testing.T, dir string) int {
	t.Helper()

	count := 0
	err := filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			count++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	return count
}

// Opening, reading and writing never sweep: upkeep is not on a run's way to
// its result, so a run never waits on it, nor on anyone else's.
func TestRunNeverSweeps(t *testing.T) {
	dir, clock := t.TempDir(), newTestClock()
	putAll(sealed(t, dir, clock), "expiring.md")
	oldLayout(t, dir, 2, 3)
	expired := segmentPath(t, dir)

	clock.advance(3 * epochLength)
	before := countFiles(t, dir)

	c := openRun(t, dir, clock)
	c.Get(keyFor(c, "expiring.md"))
	putAll(c, "new.md")

	if !exists(expired) || !exists(filepath.Join(dir, "trim.txt")) {
		t.Fatal("a run should not sweep")
	}
	if after := countFiles(t, dir); after != before {
		t.Fatalf("a run should add no files, they went from %d to %d", before, after)
	}
}

// Two processes never sweep at once. One that finds the sweep claimed leaves
// it to the claimant, however much is due.
func TestOnlyOneProcessSweeps(t *testing.T) {
	dir, clock := t.TempDir(), newTestClock()
	oldLayout(t, dir, 2, 3)

	claim, claimed := tryAcquire(filepath.Join(dir, sweepName))
	if !claimed {
		t.Fatal("an unclaimed sweep should be claimable")
	}

	openRun(t, dir, clock).Sweep(time.Second, nil)
	if !exists(filepath.Join(dir, "trim.txt")) {
		t.Fatal("a sweep claimed by another process should not run")
	}

	claim.release()
	openRun(t, dir, clock).Sweep(time.Second, nil)
	if exists(filepath.Join(dir, "trim.txt")) {
		t.Fatal("a released sweep should run")
	}
}

func TestSweepDeletesExpiredSegments(t *testing.T) {
	dir, clock := t.TempDir(), newTestClock()
	putAll(sealed(t, dir, clock), "old.md")
	old := segmentPath(t, dir)

	clock.advance(epochLength)
	putAll(sealed(t, dir, clock), "recent.md")

	openRun(t, dir, clock).Sweep(time.Second, nil)
	if !exists(old) {
		t.Fatal("a segment of the previous epoch should be kept")
	}

	clock.advance(epochLength)
	openRun(t, dir, clock).Sweep(time.Second, nil)
	if exists(old) {
		t.Fatal("a segment older than the previous epoch should be deleted")
	}
	if got := missing(t, dir, clock, "recent.md"); len(got) != 0 {
		t.Fatal("a segment of the previous epoch should survive the sweep")
	}
}

// merging returns a cache that merges once an epoch holds more than four
// segments, and seals on every flush.
func merging(t *testing.T, dir string, clock *testClock) *Cache {
	t.Helper()

	c := sealed(t, dir, clock)
	c.maxSegments = 4

	return c
}

func TestMergeBoundsTheFileCount(t *testing.T) {
	dir, clock := t.TempDir(), newTestClock()

	var all []string
	for run := range 30 {
		stored := names(fmt.Sprintf("run%02d", run), 20)
		all = append(all, stored...)

		c := merging(t, dir, clock)
		putAll(c, stored...)
		c.Sweep(time.Second, nil)
	}

	m, err := readManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Segments) > 4 {
		t.Fatalf("sweeps should hold an epoch to its segment limit, got %d", len(m.Segments))
	}
	// Segments, manifest, tail, writer lock, sweep lock, clean lock.
	if files := countFiles(t, dir); files > len(m.Segments)+5 {
		t.Fatalf("the cache should hold only its segments and fixed files, got %d", files)
	}
	if got := missing(t, dir, clock, all...); len(got) != 0 {
		t.Fatalf("a merge should keep every entry, missing %d", len(got))
	}
}

// A merge that does not fit its budget stops after a chunk and resumes from
// there. Every entry stays readable in between.
func TestMergeResumesWhereItStopped(t *testing.T) {
	dir, clock := t.TempDir(), newTestClock()

	var all []string
	for run := range 6 {
		stored := names(fmt.Sprintf("run%02d", run), 300)
		all = append(all, stored...)
		putAll(merging(t, dir, clock), stored...)
	}

	stops := 0
	for ; stops < 50; stops++ {
		merging(t, dir, clock).Sweep(0, nil)
		if !exists(filepath.Join(dir, mergeName)) {
			break
		}

		if got := missing(t, dir, clock, all...); len(got) != 0 {
			t.Fatalf("every entry should stay readable while a merge is under way, missing %d", len(got))
		}
	}
	if stops == 0 {
		t.Fatal("a merge over its budget should stop and resume")
	}

	m, err := readManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Segments) > 4 {
		t.Fatalf("the resumed merge should finish, %d segments left", len(m.Segments))
	}
	if got := missing(t, dir, clock, all...); len(got) != 0 {
		t.Fatalf("a resumed merge should keep every entry, missing %d", len(got))
	}
}

// Whatever a merge wrote after its last checkpoint is cut off and written
// again, so a merge that died mid-chunk still produces each record once.
func TestMergeIgnoresWritesAfterItsCheckpoint(t *testing.T) {
	dir, clock := t.TempDir(), newTestClock()

	var all []string
	for run := range 6 {
		stored := names(fmt.Sprintf("run%02d", run), 300)
		all = append(all, stored...)
		putAll(merging(t, dir, clock), stored...)
	}

	c := merging(t, dir, clock)
	c.Sweep(0, nil)

	state := c.readMergeState()
	if state == nil {
		t.Fatal("a merge over its budget should leave its state behind")
	}
	part, err := os.OpenFile(filepath.Join(dir, state.Output), os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = part.WriteString("half a record from a run that died"); err != nil {
		t.Fatal(err)
	}
	part.Close()

	for range 50 {
		openRun(t, dir, clock).Sweep(time.Second, nil)
		if !exists(filepath.Join(dir, mergeName)) {
			break
		}
	}

	if got := missing(t, dir, clock, all...); len(got) != 0 {
		t.Fatalf("a merge resumed after a crash should keep every entry, missing %d", len(got))
	}
	m, err := readManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, info := range m.Segments {
		total += info.Records
	}
	if total != len(all) {
		t.Fatalf("each record should be merged once, the segments hold %d records", total)
	}
}

// Two runs that linted the same file stored the same entry twice. The merge
// keeps one.
func TestMergeWritesEachKeyOnce(t *testing.T) {
	dir, clock := t.TempDir(), newTestClock()
	stored := names("shared", 20)
	for range 6 {
		putAll(merging(t, dir, clock), stored...)
	}

	merging(t, dir, clock).Sweep(time.Second, nil)

	m, err := readManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	merged := m.Segments[len(m.Segments)-1]
	if merged.Records != len(stored) {
		t.Fatalf("a merge should write each key once, wrote %d records", merged.Records)
	}
	if got := missing(t, dir, clock, stored...); len(got) != 0 {
		t.Fatalf("a merge should keep every key, missing %v", got)
	}
}

// A run that opened the segments before a merge replaced them reads on from
// what it opened.
func TestReaderOutlivesAMerge(t *testing.T) {
	dir, clock := t.TempDir(), newTestClock()

	var all []string
	for run := range 6 {
		stored := names(fmt.Sprintf("run%02d", run), 20)
		all = append(all, stored...)
		putAll(merging(t, dir, clock), stored...)
	}

	reader := openRun(t, dir, clock)
	reader.Get(keyFor(reader, all[0]))

	merging(t, dir, clock).Sweep(time.Second, nil)
	if exists(filepath.Join(dir, mergeName)) {
		t.Fatal("the merge should finish within its budget")
	}

	for _, name := range all {
		if _, found := reader.Get(keyFor(reader, name)); !found {
			t.Fatalf("a reader should find %s in the segments it opened", name)
		}
	}
}

// Whatever the manifest and a merge in progress do not account for is
// removed. A file the cache does not write is not its to remove.
func TestSweepCollectsStrays(t *testing.T) {
	dir, clock := t.TempDir(), newTestClock()
	putAll(sealed(t, dir, clock), "kept.md")
	listed := segmentPath(t, dir)

	strays := []string{"7-0123456789abcdef.seg", "seg-1.tmp", "manifest-2.tmp", "merge-3.part", "merge-3.part.idx"}
	for _, name := range append(strays, "notes.txt") {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	openRun(t, dir, clock).Sweep(time.Second, nil)

	for _, name := range strays {
		if exists(filepath.Join(dir, name)) {
			t.Errorf("the stray %s should be removed", name)
		}
	}
	if !exists(filepath.Join(dir, "notes.txt")) {
		t.Error("a file the cache does not write should be left alone")
	}
	if !exists(listed) {
		t.Error("a listed segment should be kept")
	}
	if got := missing(t, dir, clock, "kept.md"); len(got) != 0 {
		t.Error("a sweep should keep the listed entries")
	}
}

// trashed counts the files in directories moved aside for deletion.
func trashed(t *testing.T, dir string) int {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	count := 0
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), trashPrefix) {
			count += countFiles(t, filepath.Join(dir, entry.Name()))
		}
	}

	return count
}
