package cache

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
)

// The tail is a header followed by records, each framed as
//
//	length u32 | crc32c u32 | epoch u32 | key [32]byte | data
//
// where length counts everything after the checksum and the checksum covers
// the length and everything after it. Reading stops at the first record that
// runs past the end or fails its checksum: a write cut short leaves exactly
// such a record, and the next writer cuts it off before it appends.
var tailMagic = []byte("VALETL\x00\x01")

const (
	frameBytes = 8
	keyBytes   = sha256.Size

	// recordLimit stops a damaged length from reading as a huge record.
	recordLimit = 64 << 20
)

var castagnoli = crc32.MakeTable(crc32.Castagnoli)

// tailEntry is what the tail holds for one key.
type tailEntry struct {
	epoch uint32
	data  []byte
}

// tailRecord is one decoded record, in the order it was written.
type tailRecord struct {
	epoch uint32
	key   Key
	data  []byte
}

// readTail returns the newest record per key.
func readTail(path string) (map[Key]tailEntry, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return map[Key]tailEntry{}, nil
		}
		return nil, err
	}

	records, _, err := decodeTail(content)
	if err != nil {
		return nil, err
	}

	entries := make(map[Key]tailEntry, len(records))
	for _, rec := range records {
		entries[rec.key] = tailEntry{epoch: rec.epoch, data: rec.data}
	}

	return entries, nil
}

// decodeTail returns the records content holds and the offset where the last
// whole one ends. Content too short to hold a header is an empty tail.
func decodeTail(content []byte) ([]tailRecord, int, error) {
	if len(content) < len(tailMagic) {
		return nil, 0, nil
	}
	if !bytes.Equal(content[:len(tailMagic)], tailMagic) {
		return nil, 0, errUnknownFormat
	}

	var records []tailRecord
	offset := len(tailMagic)

	for len(content)-offset >= frameBytes {
		length := int(binary.LittleEndian.Uint32(content[offset:]))
		sum := binary.LittleEndian.Uint32(content[offset+4:])
		end := offset + frameBytes + length

		if length < 4+keyBytes || length > recordLimit || end > len(content) {
			break
		}

		h := crc32.New(castagnoli)
		h.Write(content[offset : offset+4])
		h.Write(content[offset+frameBytes : end])
		if h.Sum32() != sum {
			break
		}

		body := content[offset+frameBytes : end]
		rec := tailRecord{epoch: binary.LittleEndian.Uint32(body)}
		copy(rec.key[:], body[4:4+keyBytes])
		rec.data = body[4+keyBytes:]
		records = append(records, rec)

		offset = end
	}

	return records, offset, nil
}

// encodeRecord appends one framed record to buf.
func encodeRecord(buf []byte, epoch uint32, rec record) []byte {
	start := len(buf)
	buf = binary.LittleEndian.AppendUint32(buf, uint32(4+keyBytes+len(rec.data))) //nolint:gosec // one file's alerts, far below 4 GiB
	buf = binary.LittleEndian.AppendUint32(buf, 0)
	buf = binary.LittleEndian.AppendUint32(buf, epoch)
	buf = append(buf, rec.key[:]...)
	buf = append(buf, rec.data...)

	h := crc32.New(castagnoli)
	h.Write(buf[start : start+4])
	h.Write(buf[start+frameBytes:])
	binary.LittleEndian.PutUint32(buf[start+4:], h.Sum32())

	return buf
}

// appendTail writes batch to the tail in the current epoch. The caller holds
// the writer lock.
func (c *Cache) appendTail(batch []record) error {
	f, err := os.OpenFile(filepath.Join(c.dir, tailName), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()

	content, err := io.ReadAll(f)
	if err != nil {
		return err
	}

	_, end, err := decodeTail(content)
	if err != nil {
		return err
	}

	content = content[:end]
	if end == 0 {
		content = append(content, tailMagic...)
	}

	epoch := c.epoch()
	appended := len(content)
	for _, rec := range batch {
		content = encodeRecord(content, epoch, rec)
	}

	// Anything a cut-short write left past the last whole record is
	// overwritten here and cut off below, so readers never stop early again.
	if end == 0 {
		appended = 0
	}
	if _, err = f.WriteAt(content[appended:], int64(appended)); err != nil {
		return err
	}
	if err = f.Truncate(int64(len(content))); err != nil {
		return err
	}

	if len(content) >= c.sealBytes {
		return c.seal(f, content)
	}

	return nil
}

// seal moves every live record of the tail into segments, one per epoch, and
// empties the tail. The caller holds the writer lock.
//
// The segments and the manifest naming them are written before the tail is
// emptied. A seal that stops in between leaves the records in both places,
// which costs a duplicate the next merge drops and never a lost entry.
func (c *Cache) seal(f *os.File, content []byte) error {
	records, _, err := decodeTail(content)
	if err != nil {
		return err
	}

	oldest := c.epoch() - 1
	byEpoch := map[uint32]map[Key][]byte{}
	for _, rec := range records {
		if rec.epoch < oldest {
			continue
		}
		if byEpoch[rec.epoch] == nil {
			byEpoch[rec.epoch] = map[Key][]byte{}
		}
		byEpoch[rec.epoch][rec.key] = rec.data
	}

	m, err := readManifest(c.dir)
	if err != nil {
		return err
	}

	epochs := make([]uint32, 0, len(byEpoch))
	for epoch := range byEpoch {
		epochs = append(epochs, epoch)
	}
	slices.Sort(epochs)

	for _, epoch := range epochs {
		info, writeErr := writeSegment(c.dir, epoch, sortedRecords(byEpoch[epoch]))
		if writeErr != nil {
			return writeErr
		}
		m.Segments = append(m.Segments, info)
	}

	if err = writeManifest(c.dir, m); err != nil {
		return err
	}

	return f.Truncate(int64(len(tailMagic)))
}

func sortedRecords(entries map[Key][]byte) []record {
	records := make([]record, 0, len(entries))
	for key, data := range entries {
		records = append(records, record{key: key, data: data})
	}
	slices.SortFunc(records, func(a, b record) int {
		return bytes.Compare(a.key[:], b.key[:])
	})

	return records
}
