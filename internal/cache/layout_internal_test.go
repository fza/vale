package cache

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// starts counts the cleans a sweep starts, without starting any.
type starts struct {
	count int
}

func (s *starts) start() error {
	s.count++
	return nil
}

func TestSweepHandsTheOldLayoutToAClean(t *testing.T) {
	dir, clock := t.TempDir(), newTestClock()
	oldLayout(t, dir, 3, 10)

	var started starts
	openRun(t, dir, clock).Sweep(0, started.start)

	if started.count != 1 {
		t.Fatalf("a sweep finding the old layout should start one clean, started %d", started.count)
	}
	if exists(filepath.Join(dir, "00")) || exists(filepath.Join(dir, filesSweepMark)) {
		t.Fatal("the sweep should move the old layout out of place before the clean starts")
	}
	if trashed(t, dir) != 30 {
		t.Fatal("the sweep should leave the deleting to the clean")
	}
}

func TestSweepLeavesTheOldLayoutToACleanUnderWay(t *testing.T) {
	dir, clock := t.TempDir(), newTestClock()
	oldLayout(t, dir, 3, 10)

	held, err := holdClean(dir, layoutFiles, false)
	if err != nil {
		t.Fatal(err)
	}
	defer held.release()

	var started starts
	openRun(t, dir, clock).Sweep(0, started.start)

	if started.count != 0 {
		t.Fatal("a sweep should not start a clean while one is under way")
	}
	if !exists(filepath.Join(dir, "00")) {
		t.Fatal("a sweep should leave the old layout to the clean under way")
	}
}

// A clean cut short leaves the lock free and files behind. Sweeps start
// cleans until nothing of the old layout is left.
func TestCutShortCleanIsStartedAgain(t *testing.T) {
	dir, clock := t.TempDir(), newTestClock()
	oldLayout(t, dir, 3, 10)

	var started starts
	openRun(t, dir, clock).Sweep(0, started.start)
	openRun(t, dir, clock).Sweep(0, started.start)
	if started.count != 2 {
		t.Fatalf("a clean that left files behind should be started again, started %d", started.count)
	}

	if err := CleanOldLayout(dir); err != nil {
		t.Fatal(err)
	}
	openRun(t, dir, clock).Sweep(0, started.start)
	if started.count != 2 {
		t.Fatal("no clean should start once nothing of the old layout is left")
	}
}

func TestCleanOldLayoutDeletesOnlyTheOldLayout(t *testing.T) {
	dir, clock := t.TempDir(), newTestClock()
	putAll(sealed(t, dir, clock), "kept.md")
	oldLayout(t, dir, 3, 10)
	moveOldLayoutAside(dir)
	oldLayout(t, dir, 2, 10)

	if err := CleanOldLayout(dir); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if isOldLayout(entry) {
			t.Errorf("%s of the old layout should be deleted", entry.Name())
		}
	}
	if got := missing(t, dir, clock, "kept.md"); len(got) != 0 {
		t.Fatal("cleaning the old layout should keep the packed entries")
	}
}

func TestCleanOldLayoutLeavesItToACleanUnderWay(t *testing.T) {
	dir := t.TempDir()
	oldLayout(t, dir, 2, 10)

	held, err := holdClean(dir, layoutFiles, false)
	if err != nil {
		t.Fatal(err)
	}
	defer held.release()

	if err = CleanOldLayout(dir); err != nil {
		t.Fatal(err)
	}
	if !exists(filepath.Join(dir, "00")) {
		t.Fatal("a second clean should not run beside the first")
	}
}

// blocks reports whether do is still waiting after a while, then releases
// held and waits for do to finish.
func blocks(t *testing.T, held *fileLock, do func()) bool {
	t.Helper()

	done := make(chan struct{})
	go func() {
		do()
		close(done)
	}()

	waiting := true
	select {
	case <-done:
		waiting = false
	case <-time.After(200 * time.Millisecond):
	}

	held.release()
	<-done

	return waiting
}

func TestRunsWaitForACleanOfTheirLayout(t *testing.T) {
	dir, clock := t.TempDir(), newTestClock()
	putAll(openRun(t, dir, clock), "a.md")

	held, err := holdClean(dir, layoutPacked, false)
	if err != nil {
		t.Fatal(err)
	}
	if !blocks(t, held, func() {
		c := openRun(t, dir, clock)
		c.Get(keyFor(c, "a.md"))
	}) {
		t.Error("a read should wait for a clean of its layout")
	}

	if held, err = holdClean(dir, layoutPacked, false); err != nil {
		t.Fatal(err)
	}
	if !blocks(t, held, func() { putAll(openRun(t, dir, clock), "b.md") }) {
		t.Error("a write should wait for a clean of its layout")
	}
}

func TestRunsDoNotWaitForACleanOfTheOldLayout(t *testing.T) {
	dir, clock := t.TempDir(), newTestClock()
	putAll(openRun(t, dir, clock), "a.md")

	held, err := holdClean(dir, layoutFiles, false)
	if err != nil {
		t.Fatal(err)
	}
	if blocks(t, held, func() {
		c := openRun(t, dir, clock)
		c.Get(keyFor(c, "a.md"))
		putAll(c, "b.md")
	}) {
		t.Error("a run should not wait for a clean of a layout it does not use")
	}
}

// Clean waits for the runs using the cache, and keeps the locks others may be
// waiting on.
func TestCleanWaitsForRunsAndKeepsItsLocks(t *testing.T) {
	dir, clock := t.TempDir(), newTestClock()
	putAll(sealed(t, dir, clock), "a.md")
	oldLayout(t, dir, 2, 10)

	using, err := usePacked(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !blocks(t, using, func() {
		if cleanErr := Clean(dir); cleanErr != nil {
			t.Error(cleanErr)
		}
	}) {
		t.Error("a clean should wait for the runs using the cache")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "clean-v") {
			t.Errorf("a clean should remove %s", entry.Name())
		}
	}
	for _, layout := range []int{layoutFiles, layoutPacked} {
		if !exists(filepath.Join(dir, cleanLockName(layout))) {
			t.Errorf("a clean should keep the clean lock of layout v%d", layout)
		}
	}
}

func TestCleanLockNamesItsLayout(t *testing.T) {
	dir := t.TempDir()

	held, err := holdClean(dir, layoutFiles, false)
	if err != nil {
		t.Fatal(err)
	}
	defer held.release()

	note, err := os.ReadFile(filepath.Join(dir, "clean-v0.lock"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(note), "layout v0 (one file per entry), pid ") {
		t.Fatalf("the lock should name the layout being cleaned and its cleaner, got %q", note)
	}
}

// Runs hold their layout's clean lock shared: a run never waits for another.
func TestRunsDoNotWaitForEachOther(t *testing.T) {
	dir, clock := t.TempDir(), newTestClock()
	putAll(openRun(t, dir, clock), "a.md")

	using, err := usePacked(dir)
	if err != nil {
		t.Fatal(err)
	}
	if blocks(t, using, func() {
		c := openRun(t, dir, clock)
		c.Get(keyFor(c, "a.md"))
		putAll(c, "b.md")
	}) {
		t.Error("a run should not wait for another run")
	}
}
