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

	trashPrefix = "trash-"
	partExt     = ".part"
	partIndex   = ".idx"
	tempExt     = ".tmp"
)

// Sweep does the cache's upkeep: it deletes the segments of epochs gone by,
// removes what interrupted runs and earlier layouts of the cache left behind,
// and merges an epoch's smallest segments once it holds too many. The merge
// stops after about budget and is resumed by the next Sweep.
//
// The one-file-per-entry layout of earlier builds is deleted in full by the
// first sweep that finds it, whatever the budget: one run waits for it once,
// rather than every run spending its budget on it until it is gone.
//
// One process sweeps at a time. A run that finds another one sweeping, or
// nothing to do, returns at once.
func (c *Cache) Sweep(budget time.Duration) {
	c.mu.Lock()
	disabled := c.disabled
	c.mu.Unlock()
	if disabled || !c.sweepDue() {
		return
	}

	claim, claimed := tryAcquire(filepath.Join(c.dir, sweepName))
	if !claimed {
		return
	}
	defer claim.release()

	c.dropExpired()
	c.collectStrays()
	c.emptyTrash()
	c.merge(time.Now().Add(budget))
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

// isLeftover reports whether entry is something a sweep removes: a file only
// this cache writes that nothing accounts for, a shard directory of the
// one-file-per-entry layout, or a directory already moved aside for deletion.
func isLeftover(entry os.DirEntry, known map[string]bool) bool {
	name := entry.Name()
	if known[name] {
		return false
	}
	if entry.IsDir() {
		return isShardName(name) || strings.HasPrefix(name, trashPrefix)
	}

	return isStrayName(name)
}

// knownNames are the names in the cache directory a sweep leaves alone.
func knownNames(m manifest, state *mergeState) map[string]bool {
	known := map[string]bool{
		manifestName: true,
		tailName:     true,
		lockName:     true,
		sweepName:    true,
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
		m.Segments = append(kept, info)
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
// account for: segments a seal or a merge wrote and never listed, temporary
// files, and the shard directories of the one-file-per-entry layout, which
// are moved aside to be emptied by emptyTrash.
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

		switch {
		case !isLeftover(entry, known), strings.HasPrefix(name, trashPrefix):
		case entry.IsDir():
			os.Rename(filepath.Join(c.dir, name), filepath.Join(c.dir, trashPrefix+name+"-"+randomHex()))
		default:
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

// isShardName reports whether name is one of the 256 directories the
// one-file-per-entry layout spread its entries over.
func isShardName(name string) bool {
	if len(name) != 2 {
		return false
	}
	_, err := hex.DecodeString(name)
	return err == nil && strings.ToLower(name) == name
}

// isStrayName reports whether name is a file only this cache writes, which a
// sweep may therefore remove once nothing lists it.
func isStrayName(name string) bool {
	for _, ext := range []string{segmentExt, tempExt, partExt, partIndex} {
		if strings.HasSuffix(name, ext) {
			return true
		}
	}

	return name == mergeName || name == "trim.txt"
}

// emptyTrash deletes the directories moved aside for deletion, with
// everything in them.
func (c *Cache) emptyTrash() {
	entries, err := os.ReadDir(c.dir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), trashPrefix) {
			os.RemoveAll(filepath.Join(c.dir, entry.Name()))
		}
	}
}
