package cache

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// A segment is a sealed, read-only run of records sorted by key, followed by a
// sparse index and a footer:
//
//	record: key [32]byte | length u32 | crc32c u32 | data
//	index:  key [32]byte | offset u64, one per indexStride records
//	footer: index offset u64 | index entries u32 | records u32 | epoch u32 |
//	        index crc32c u32 | footer crc32c u32 | reserved u32 | magic [8]byte
//
// A lookup searches the index and reads the one stretch of records between two
// index entries. Nothing is mapped into memory, so a segment another process
// deletes or replaces never faults a reader.
var segmentMagic = []byte("VALESG\x00\x01")

const (
	recordHead  = keyBytes + 8
	indexEntry  = keyBytes + 8
	footerBytes = 40

	indexStride = 64
	indexBytes  = 4096

	segmentExt = ".seg"
)

// segmentInfo is how the manifest lists a segment.
type segmentInfo struct {
	Name    string `json:"name"`
	Epoch   uint32 `json:"epoch"`
	Records int    `json:"records"`
	Bytes   int64  `json:"bytes"`
}

// segmentWriter writes one segment. Its counters are what a merge saves to
// resume from.
type segmentWriter struct {
	file  *os.File
	out   *bufio.Writer
	epoch uint32

	offset      int64
	records     int
	index       []byte
	sinceIndex  int
	lastIndexed int64
}

func newSegmentWriter(file *os.File, epoch uint32) *segmentWriter {
	return &segmentWriter{file: file, out: bufio.NewWriter(file), epoch: epoch}
}

func (w *segmentWriter) add(key Key, data []byte) error {
	if len(w.index) == 0 || w.sinceIndex >= indexStride || w.offset-w.lastIndexed >= indexBytes {
		w.index = append(w.index, key[:]...)
		w.index = binary.LittleEndian.AppendUint64(w.index, uint64(w.offset))
		w.sinceIndex = 0
		w.lastIndexed = w.offset
	}

	head := make([]byte, recordHead)
	copy(head, key[:])
	binary.LittleEndian.PutUint32(head[keyBytes:], uint32(len(data)))
	binary.LittleEndian.PutUint32(head[keyBytes+4:], recordSum(head[:keyBytes+4], data))

	if _, err := w.out.Write(head); err != nil {
		return err
	}
	if _, err := w.out.Write(data); err != nil {
		return err
	}

	w.offset += int64(recordHead + len(data))
	w.records++
	w.sinceIndex++

	return nil
}

// finish writes the index and the footer.
func (w *segmentWriter) finish() error {
	if _, err := w.out.Write(w.index); err != nil {
		return err
	}

	footer := make([]byte, 0, footerBytes)
	footer = binary.LittleEndian.AppendUint64(footer, uint64(w.offset))
	footer = binary.LittleEndian.AppendUint32(footer, uint32(len(w.index)/indexEntry))
	footer = binary.LittleEndian.AppendUint32(footer, uint32(w.records))
	footer = binary.LittleEndian.AppendUint32(footer, w.epoch)
	footer = binary.LittleEndian.AppendUint32(footer, crc32.Checksum(w.index, castagnoli))
	footer = binary.LittleEndian.AppendUint32(footer, crc32.Checksum(footer, castagnoli))
	footer = binary.LittleEndian.AppendUint32(footer, 0)
	footer = append(footer, segmentMagic...)

	if _, err := w.out.Write(footer); err != nil {
		return err
	}

	return w.out.Flush()
}

func (w *segmentWriter) size() int64 {
	return w.offset + int64(len(w.index)) + footerBytes
}

func recordSum(head, data []byte) uint32 {
	h := crc32.New(castagnoli)
	h.Write(head)
	h.Write(data)
	return h.Sum32()
}

// writeSegment writes records, sorted by key, as a new segment of epoch.
func writeSegment(dir string, epoch uint32, records []record) (segmentInfo, error) {
	f, err := os.CreateTemp(dir, "seg-*.tmp")
	if err != nil {
		return segmentInfo{}, err
	}

	w := newSegmentWriter(f, epoch)
	for _, rec := range records {
		if err = w.add(rec.key, rec.data); err != nil {
			break
		}
	}
	if err == nil {
		err = w.finish()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(f.Name())
		return segmentInfo{}, err
	}

	return publishSegment(dir, f.Name(), w)
}

// publishSegment renames a finished segment to its final name.
func publishSegment(dir, from string, w *segmentWriter) (segmentInfo, error) {
	info := segmentInfo{
		Name:    segmentName(w.epoch),
		Epoch:   w.epoch,
		Records: w.records,
		Bytes:   w.size(),
	}

	if err := os.Rename(from, filepath.Join(dir, info.Name)); err != nil {
		os.Remove(from)
		return segmentInfo{}, err
	}

	return info, nil
}

func segmentName(epoch uint32) string {
	return fmt.Sprintf("%d-%s%s", epoch, randomHex(), segmentExt)
}

func randomHex() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// segment is an open segment.
type segment struct {
	name    string
	file    *os.File
	epoch   uint32
	index   []indexKey
	dataEnd int64
}

type indexKey struct {
	key    Key
	offset int64
}

var errDamagedSegment = errors.New("damaged segment")

// openSegments opens the segments the manifest lists, oldest first.
//
// A segment a sweep deletes between the manifest being read and the segment
// being opened has been replaced, so the manifest is read again. A segment
// that stays missing, or is damaged, is left out: its entries are misses.
func openSegments(dir string) ([]*segment, error) {
	var segments []*segment

	for attempt := 0; attempt < 3; attempt++ {
		m, err := readManifest(dir)
		if err != nil {
			return nil, err
		}

		segments = segments[:0]
		moved := false
		for _, info := range m.Segments {
			seg, openErr := openSegment(filepath.Join(dir, info.Name))
			if errors.Is(openErr, fs.ErrNotExist) {
				moved = true
				continue
			}
			if openErr != nil {
				continue
			}
			segments = append(segments, seg)
		}

		if !moved {
			break
		}
		for _, seg := range segments {
			seg.close()
		}
	}

	return segments, nil
}

func openSegment(path string) (*segment, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	seg, err := readSegment(f)
	if err != nil {
		f.Close()
		return nil, err
	}
	seg.name = filepath.Base(path)

	return seg, nil
}

func readSegment(f *os.File) (*segment, error) {
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() < footerBytes {
		return nil, errDamagedSegment
	}

	footer := make([]byte, footerBytes)
	if _, err = f.ReadAt(footer, info.Size()-footerBytes); err != nil {
		return nil, err
	}
	if !bytes.Equal(footer[32:], segmentMagic) ||
		crc32.Checksum(footer[:24], castagnoli) != binary.LittleEndian.Uint32(footer[24:]) {
		return nil, errDamagedSegment
	}

	dataEnd := int64(binary.LittleEndian.Uint64(footer))
	entries := int64(binary.LittleEndian.Uint32(footer[8:]))
	if dataEnd+entries*indexEntry+footerBytes != info.Size() {
		return nil, errDamagedSegment
	}

	raw := make([]byte, entries*indexEntry)
	if _, err = f.ReadAt(raw, dataEnd); err != nil {
		return nil, err
	}
	if crc32.Checksum(raw, castagnoli) != binary.LittleEndian.Uint32(footer[20:]) {
		return nil, errDamagedSegment
	}

	index := make([]indexKey, entries)
	for i := range index {
		entry := raw[i*indexEntry:]
		copy(index[i].key[:], entry[:keyBytes])
		index[i].offset = int64(binary.LittleEndian.Uint64(entry[keyBytes:]))
	}

	return &segment{
		file:    f,
		epoch:   binary.LittleEndian.Uint32(footer[16:]),
		index:   index,
		dataEnd: dataEnd,
	}, nil
}

// find returns the data stored under key.
func (s *segment) find(key Key) ([]byte, bool) {
	i := sort.Search(len(s.index), func(i int) bool {
		return bytes.Compare(s.index[i].key[:], key[:]) > 0
	})
	if i == 0 {
		return nil, false
	}

	start, end := s.index[i-1].offset, s.dataEnd
	if i < len(s.index) {
		end = s.index[i].offset
	}
	if start < 0 || end > s.dataEnd || start >= end {
		return nil, false
	}

	stretch := make([]byte, end-start)
	if _, err := s.file.ReadAt(stretch, start); err != nil {
		return nil, false
	}

	for len(stretch) >= recordHead {
		length := int(binary.LittleEndian.Uint32(stretch[keyBytes:]))
		if length > len(stretch)-recordHead {
			return nil, false
		}

		switch bytes.Compare(stretch[:keyBytes], key[:]) {
		case 0:
			data := stretch[recordHead : recordHead+length]
			if recordSum(stretch[:keyBytes+4], data) != binary.LittleEndian.Uint32(stretch[keyBytes+4:]) {
				return nil, false
			}
			return data, true
		case 1:
			return nil, false
		}

		stretch = stretch[recordHead+length:]
	}

	return nil, false
}

// recordsAfter reads the segment's records in order, starting after cursor, or
// at the first record when cursor is nil.
func (s *segment) recordsAfter(cursor *Key) *segmentCursor {
	var start int64
	if cursor != nil {
		i := sort.Search(len(s.index), func(i int) bool {
			return bytes.Compare(s.index[i].key[:], cursor[:]) > 0
		})
		if i > 0 {
			start = s.index[i-1].offset
		}
	}

	it := &segmentCursor{
		in:        bufio.NewReader(io.NewSectionReader(s.file, start, s.dataEnd-start)),
		remaining: s.dataEnd - start,
	}
	for cursor != nil {
		rec, ok := it.peek()
		if !ok || bytes.Compare(rec.key[:], cursor[:]) > 0 {
			break
		}
		it.advance()
	}

	return it
}

func (s *segment) close() {
	s.file.Close()
}

// segmentCursor walks a segment's records in key order.
type segmentCursor struct {
	in        *bufio.Reader
	remaining int64
	head      *record
	done      bool
}

// peek returns the record under the cursor. A record that fails its checksum
// ends the walk: nothing after a damaged length can be framed.
func (it *segmentCursor) peek() (record, bool) {
	if it.head != nil {
		return *it.head, true
	}
	if it.done || it.remaining < recordHead {
		it.done = true
		return record{}, false
	}

	head := make([]byte, recordHead)
	if _, err := io.ReadFull(it.in, head); err != nil {
		it.done = true
		return record{}, false
	}

	length := int64(binary.LittleEndian.Uint32(head[keyBytes:]))
	if length > it.remaining-recordHead {
		it.done = true
		return record{}, false
	}

	data := make([]byte, length)
	if _, err := io.ReadFull(it.in, data); err != nil {
		it.done = true
		return record{}, false
	}
	if recordSum(head[:keyBytes+4], data) != binary.LittleEndian.Uint32(head[keyBytes+4:]) {
		it.done = true
		return record{}, false
	}

	it.remaining -= recordHead + length
	rec := record{data: data}
	copy(rec.key[:], head[:keyBytes])
	it.head = &rec

	return rec, true
}

func (it *segmentCursor) advance() {
	it.head = nil
}
