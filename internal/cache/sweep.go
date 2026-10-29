package cache

import (
	"bytes"
	"cmp"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const (
	// sweepChunk is how many records a merge writes between two looks at the
	// clock. A merge always writes at least one chunk, so it advances however
	// short the budget.
	sweepChunk = 256

	partExt   = ".part"
	partIndex = ".idx"
	tempExt   = ".tmp"
)

// Sweep does the cache's upkeep: it deletes the segments of epochs gone by,
// removes what interrupted runs and earlier layouts of the cache left behind,
// and merges an epoch's smallest segments once it holds too many. The merge
// stops after about budget and is resumed by the next Sweep.
//
// The one-file-per-entry layout of earlier builds takes minutes to delete
// when it has grown to millions of files, longer than a run may wait. The
// sweep moves it aside and calls startClean, which is to start a process
// running CleanOldLayout that outlives the run. That process holds the
// layout's clean lock; while it does, sweeps leave the layout to it, and once
// it has ended, whether done or cut short, the next sweep that finds anything
// of the layout left starts another.
//
// One process sweeps at a time. A run that finds another one sweeping, or
// nothing to do, returns at once.
func (c *Cache) Sweep(budget time.Duration, startClean func() error) {
	c.mu.Lock()
	disabled := c.disabled
	c.mu.Unlock()
	if disabled {
		return
	}

	use, err := usePacked(c.dir)
	if err != nil {
		return
	}
	defer use.release()

	if !c.sweepDue() {
		return
	}

	claim, claimed := tryAcquire(filepath.Join(c.dir, sweepName))
	if !claimed {
		return
	}
	defer claim.release()

	c.dropExpired()
	c.collectStrays()
	c.handOverOldLayout(startClean)
	c.merge(time.Now().Add(budget))
}

// handOverOldLayout moves what is left of the one-file-per-entry layout aside
// and starts a clean of it, unless a clean is under way already.
func (c *Cache) handOverOldLayout(startClean func() error) {
	entries, err := os.ReadDir(c.dir)
	if err != nil || !slices.ContainsFunc(entries, isOldLayout) {
		return
	}

	lock, err := holdClean(c.dir, layoutFiles, false)
	if err != nil {
		return
	}
	moveOldLayoutAside(c.dir)
	lock.release()

	if startClean != nil {
		startClean()
	}
}

// sweepDue reports whether a sweep would find anything to do. It costs a read
// of the manifest and a listing of the cache directory, which holds a few
// dozen names.
func (c *Cache) sweepDue() bool {
	m, err := readManifest(c.dir)
	if err != nil {
		return false
	}

	oldest := c.epoch() - 1
	perEpoch := map[uint32]int{}
	for _, info := range m.Segments {
		if info.Epoch < oldest {
			return true
		}
		perEpoch[info.Epoch]++
		if perEpoch[info.Epoch] > c.maxSegments {
			return true
		}
	}

	entries, err := os.ReadDir(c.dir)
	if err != nil {
		return false
	}

	known := knownNames(m, nil)
	for _, entry := range entries {
		if isLeftover(entry, known) {
			return true
		}
	}

	return false
}

// isLeftover reports whether entry is something a sweep sees to: a file only
// this cache writes that nothing accounts for, or anything of the
// one-file-per-entry layout.
func isLeftover(entry os.DirEntry, known map[string]bool) bool {
	if known[entry.Name()] {
		return false
	}

	return isOldLayout(entry) || (!entry.IsDir() && isStrayName(entry.Name()))
}

// knownNames are the names in the cache directory a sweep leaves alone.
func knownNames(m manifest, state *mergeState) map[string]bool {
	known := map[string]bool{
		manifestName: true,
		tailName:     true,
		lockName:     true,
		sweepName:    true,

		cleanLockName(layoutFiles):  true,
		cleanLockName(layoutPacked): true,
	}
	for _, info := range m.Segments {
		known[info.Name] = true
	}
	if state != nil {
		known[mergeName] = true
		known[state.Output] = true
		known[state.Output+partIndex] = true
	}

	return known
}

// dropExpired deletes the segments of epochs before the previous one.
func (c *Cache) dropExpired() {
	lock, err := acquire(filepath.Join(c.dir, lockName))
	if err != nil {
		return
	}

	m, err := readManifest(c.dir)
	if err != nil {
		lock.release()
		return
	}

	oldest := c.epoch() - 1
	var live, expired []segmentInfo
	for _, info := range m.Segments {
		if info.Epoch < oldest {
			expired = append(expired, info)
		} else {
			live = append(live, info)
		}
	}

	if len(expired) > 0 {
		m.Segments = live
		err = writeManifest(c.dir, m)
	}
	lock.release()

	// A segment another process still has open cannot be deleted everywhere.
	// Unlisted, it is a stray, and a later sweep tries again.
	if err == nil {
		for _, info := range expired {
			os.Remove(filepath.Join(c.dir, info.Name))
		}
	}
}

// mergeState is how far a merge got. It is replaced whole after every chunk,
// so a merge stopped by its budget or by the process ending resumes from the
// last chunk written.
type mergeState struct {
	Epoch  uint32   `json:"epoch"`
	Inputs []string `json:"inputs"`
	Output string   `json:"output"`

	// Cursor is the last key written, hex-encoded; empty before the first.
	Cursor string `json:"cursor"`

	Offset      int64 `json:"offset"`
	Records     int   `json:"records"`
	IndexBytes  int   `json:"index_bytes"`
	SinceIndex  int   `json:"since_index"`
	LastIndexed int64 `json:"last_indexed"`
}

// merge runs or resumes a merge until it finishes or the deadline passes.
func (c *Cache) merge(deadline time.Time) {
	m, err := readManifest(c.dir)
	if err != nil {
		return
	}

	state := c.readMergeState()
	if state != nil && !listsAll(m, state.Inputs) {
		c.discardMerge(state)
		state = nil
	}
	if state == nil {
		state = c.planMerge(m)
	}
	if state == nil {
		return
	}

	if _, err = c.mergeChunks(state, deadline); err != nil {
		c.discardMerge(state)
	}
}

// planMerge picks the smallest segments of the first epoch holding more than
// maxSegments, enough of them to bring it back to half of that.
func (c *Cache) planMerge(m manifest) *mergeState {
	oldest := c.epoch() - 1
	perEpoch := map[uint32][]segmentInfo{}
	for _, info := range m.Segments {
		if info.Epoch >= oldest {
			perEpoch[info.Epoch] = append(perEpoch[info.Epoch], info)
		}
	}

	epochs := make([]uint32, 0, len(perEpoch))
	for epoch := range perEpoch {
		epochs = append(epochs, epoch)
	}
	slices.Sort(epochs)

	for _, epoch := range epochs {
		segments := perEpoch[epoch]
		if len(segments) <= c.maxSegments {
			continue
		}

		slices.SortStableFunc(segments, func(a, b segmentInfo) int {
			return cmp.Compare(a.Bytes, b.Bytes)
		})

		count := min(len(segments), max(2, len(segments)-c.maxSegments/2+1))
		inputs := make([]string, 0, count)
		for _, info := range segments[:count] {
			inputs = append(inputs, info.Name)
		}

		return &mergeState{Epoch: epoch, Inputs: inputs, Output: "merge-" + randomHex() + partExt}
	}

	return nil
}

// mergeChunks writes the merged records after the state's cursor until the
// inputs run out or the deadline passes, at least one chunk either way.
func (c *Cache) mergeChunks(state *mergeState, deadline time.Time) (bool, error) {
	inputs := make([]*segment, 0, len(state.Inputs))
	defer func() {
		for _, seg := range inputs {
			seg.close()
		}
	}()
	for _, name := range state.Inputs {
		seg, err := openSegment(filepath.Join(c.dir, name))
		if err != nil {
			return false, err
		}
		inputs = append(inputs, seg)
	}

	var cursor *Key
	if state.Cursor != "" {
		raw, err := hex.DecodeString(state.Cursor)
		if err != nil || len(raw) != keyBytes {
			return false, errDamagedSegment
		}
		cursor = new(Key)
		copy(cursor[:], raw)
	}

	out, w, err := c.resumeOutput(state)
	if err != nil {
		return false, err
	}
	defer out.Close()

	cursors := make([]*segmentCursor, len(inputs))
	for i, seg := range inputs {
		cursors[i] = seg.recordsAfter(cursor)
	}

	for written := 0; ; written++ {
		next, found := smallest(cursors)
		if !found {
			break
		}

		if err = w.add(next.key, next.data); err != nil {
			return false, err
		}
		for _, it := range cursors {
			if rec, ok := it.peek(); ok && rec.key == next.key {
				it.advance()
			}
		}

		if written%sweepChunk == sweepChunk-1 && time.Now().After(deadline) {
			state.Cursor = hex.EncodeToString(next.key[:])
			return false, c.checkpoint(state, w)
		}
	}

	if err = w.finish(); err != nil {
		return false, err
	}
	if err = out.Close(); err != nil {
		return false, err
	}

	return true, c.commitMerge(state, w)
}

// smallest returns the record with the lowest key under any cursor.
func smallest(cursors []*segmentCursor) (record, bool) {
	var best record
	found := false

	for _, it := range cursors {
		rec, ok := it.peek()
		if ok && (!found || bytes.Compare(rec.key[:], best.key[:]) < 0) {
			best, found = rec, true
		}
	}

	return best, found
}

// resumeOutput opens the merge's output where its last checkpoint left it.
// Anything written after that checkpoint is cut off and written again.
func (c *Cache) resumeOutput(state *mergeState) (*os.File, *segmentWriter, error) {
	out, err := os.OpenFile(filepath.Join(c.dir, state.Output), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, nil, err
	}

	index, err := os.ReadFile(filepath.Join(c.dir, state.Output+partIndex))
	switch {
	case state.IndexBytes == 0:
		index, err = nil, nil
	case err == nil && len(index) < state.IndexBytes:
		err = errDamagedSegment
	}
	if err == nil {
		err = out.Truncate(state.Offset)
	}
	if err == nil {
		_, err = out.Seek(state.Offset, io.SeekStart)
	}
	if err != nil {
		out.Close()
		return nil, nil, err
	}

	w := newSegmentWriter(out, state.Epoch)
	w.offset = state.Offset
	w.records = state.Records
	w.index = index[:state.IndexBytes:state.IndexBytes]
	w.sinceIndex = state.SinceIndex
	w.lastIndexed = state.LastIndexed

	return out, w, nil
}

// checkpoint saves how far the merge got.
func (c *Cache) checkpoint(state *mergeState, w *segmentWriter) error {
	if err := w.out.Flush(); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(c.dir, state.Output+partIndex), w.index, 0o600); err != nil {
		return err
	}

	state.Offset = w.offset
	state.Records = w.records
	state.IndexBytes = len(w.index)
	state.SinceIndex = w.sinceIndex
	state.LastIndexed = w.lastIndexed

	return c.writeMergeState(state)
}

// commitMerge publishes the merged segment in place of its inputs.
func (c *Cache) commitMerge(state *mergeState, w *segmentWriter) error {
	info, err := publishSegment(c.dir, filepath.Join(c.dir, state.Output), w)
	if err != nil {
		return err
	}

	lock, err := acquire(filepath.Join(c.dir, lockName))
	if err != nil {
		os.Remove(filepath.Join(c.dir, info.Name))
		return err
	}

	m, err := readManifest(c.dir)
	if err == nil && !listsAll(m, state.Inputs) {
		err = errDamagedSegment
	}
	if err == nil {
		kept := m.Segments[:0]
		for _, seg := range m.Segments {
			if !slices.Contains(state.Inputs, seg.Name) {
				kept = append(kept, seg)
			}
		}
		kept = append(kept, info)
		m.Segments = kept
		err = writeManifest(c.dir, m)
	}
	lock.release()

	if err != nil {
		os.Remove(filepath.Join(c.dir, info.Name))
		return err
	}

	for _, name := range state.Inputs {
		os.Remove(filepath.Join(c.dir, name))
	}
	os.Remove(filepath.Join(c.dir, state.Output+partIndex))
	os.Remove(filepath.Join(c.dir, mergeName))

	return nil
}

func listsAll(m manifest, names []string) bool {
	for _, name := range names {
		if !slices.ContainsFunc(m.Segments, func(info segmentInfo) bool { return info.Name == name }) {
			return false
		}
	}

	return true
}

func (c *Cache) readMergeState() *mergeState {
	content, err := os.ReadFile(filepath.Join(c.dir, mergeName))
	if err != nil {
		return nil
	}

	var state mergeState
	if json.Unmarshal(content, &state) != nil || state.Output == "" || len(state.Inputs) == 0 {
		os.Remove(filepath.Join(c.dir, mergeName))
		return nil
	}

	return &state
}

func (c *Cache) writeMergeState(state *mergeState) error {
	content, err := json.Marshal(state)
	if err != nil {
		return err
	}

	f, err := os.CreateTemp(c.dir, "merge-*"+tempExt)
	if err != nil {
		return err
	}

	_, err = f.Write(content)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(f.Name(), filepath.Join(c.dir, mergeName))
	}
	if err != nil {
		os.Remove(f.Name())
	}

	return err
}

func (c *Cache) discardMerge(state *mergeState) {
	os.Remove(filepath.Join(c.dir, mergeName))
	os.Remove(filepath.Join(c.dir, state.Output))
	os.Remove(filepath.Join(c.dir, state.Output+partIndex))
}

// collectStrays removes what the manifest and a merge in progress do not
// account for: segments a seal or a merge wrote and never listed, and
// temporary files.
//
// It runs under the writer lock, since a seal writes its segment before it
// lists it.
func (c *Cache) collectStrays() {
	lock, err := acquire(filepath.Join(c.dir, lockName))
	if err != nil {
		return
	}
	defer lock.release()

	m, err := readManifest(c.dir)
	if err != nil {
		return
	}

	entries, err := os.ReadDir(c.dir)
	if err != nil {
		return
	}

	present := map[string]bool{}
	known := knownNames(m, c.readMergeState())
	for _, entry := range entries {
		name := entry.Name()
		present[name] = true

		if isLeftover(entry, known) && !isOldLayout(entry) {
			os.Remove(filepath.Join(c.dir, name))
		}
	}

	listed := m.Segments[:0]
	for _, info := range m.Segments {
		if present[info.Name] {
			listed = append(listed, info)
		}
	}
	if len(listed) < len(m.Segments) {
		m.Segments = listed
		writeManifest(c.dir, m)
	}
}

// isStrayName reports whether name is a file only this cache writes, which a
// sweep may therefore remove once nothing lists it.
func isStrayName(name string) bool {
	for _, ext := range []string{segmentExt, tempExt, partExt, partIndex} {
		if strings.HasSuffix(name, ext) {
			return true
		}
	}

	return name == mergeName
}
