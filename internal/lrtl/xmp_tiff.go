package lrtl

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const maxEmbeddedTIFFXMPSize = 32 << 20

type tiffXMPDirectory struct {
	offset uint64
	depth  int
}

type tiffXMPEntry struct {
	tag    uint16
	typeID uint16
	count  uint64
	value  []byte
}

type tiffXMPReader struct {
	file        *os.File
	size        uint64
	order       binary.ByteOrder
	bigTIFF     bool
	visited     map[uint64]bool
	directories int
}

func readEmbeddedImageXMP(path string) ([]byte, bool, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg":
		return readEmbeddedJPEGXMP(path)
	case ".raf":
		return readEmbeddedRAFXMP(path)
	case ".tif", ".tiff", ".dng", ".cr2", ".rw2", ".nef", ".nrw", ".arw", ".sr2", ".pef", ".erf", ".orf", ".dcr":
		return readEmbeddedTIFFXMP(path)
	case ".cr3":
		return readEmbeddedCR3XMP(path)
	default:
		return nil, false, nil
	}
}

func readEmbeddedTIFFXMP(path string) ([]byte, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, false, err
	}
	if info.Size() < 8 {
		return nil, false, nil
	}
	reader := &tiffXMPReader{file: file, size: uint64(info.Size()), visited: make(map[uint64]bool)}
	var header [16]byte
	if _, err := file.ReadAt(header[:8], 0); err != nil {
		return nil, false, err
	}
	switch string(header[:2]) {
	case "II":
		reader.order = binary.LittleEndian
	case "MM":
		reader.order = binary.BigEndian
	default:
		return nil, false, nil
	}
	magic := reader.order.Uint16(header[2:4])
	var firstIFD uint64
	switch magic {
	case 42, 85: // Classic TIFF or Panasonic RW2.
		firstIFD = uint64(reader.order.Uint32(header[4:8]))
	case 0x4f52, 0x5352: // Olympus ORF: IIRO, IIRS, or MMOR.
		firstIFD = uint64(reader.order.Uint32(header[4:8]))
	case 43:
		if info.Size() < int64(len(header)) {
			return nil, false, fmt.Errorf("truncated BigTIFF header")
		}
		if _, err := file.ReadAt(header[8:], 8); err != nil {
			return nil, false, err
		}
		if reader.order.Uint16(header[4:6]) != 8 || reader.order.Uint16(header[6:8]) != 0 {
			return nil, false, fmt.Errorf("unsupported BigTIFF offset size or reserved field")
		}
		reader.bigTIFF = true
		firstIFD = reader.order.Uint64(header[8:16])
	default:
		return nil, false, nil
	}
	if firstIFD == 0 {
		return nil, false, nil
	}
	return reader.scan(firstIFD)
}

func (reader *tiffXMPReader) scan(firstIFD uint64) ([]byte, bool, error) {
	queue := []tiffXMPDirectory{{offset: firstIFD}}
	for len(queue) > 0 {
		directory := queue[0]
		queue = queue[1:]
		if directory.offset == 0 || reader.visited[directory.offset] {
			continue
		}
		if directory.depth > 32 || reader.directories >= maxPreviewIFDs {
			return nil, false, fmt.Errorf("TIFF XMP directory graph exceeds safety limits")
		}
		reader.visited[directory.offset] = true
		reader.directories++
		entries, next, err := reader.readDirectory(directory.offset)
		if err != nil {
			return nil, false, err
		}
		for _, entry := range entries {
			if entry.tag != 700 {
				continue
			}
			packet, found, err := reader.readPacket(entry)
			if err != nil {
				return nil, false, err
			}
			if found {
				return packet, true, nil
			}
		}
		for _, entry := range entries {
			if entry.tag != 330 { // SubIFDs.
				continue
			}
			offsets, err := reader.readDirectoryOffsets(entry)
			if err != nil {
				continue
			}
			for _, offset := range offsets {
				if offset == 0 || reader.visited[offset] {
					continue
				}
				if len(queue) >= maxPreviewQueue {
					return nil, false, fmt.Errorf("TIFF XMP directory queue exceeds safety limits")
				}
				queue = append(queue, tiffXMPDirectory{offset: offset, depth: directory.depth + 1})
			}
		}
		if next != 0 && !reader.visited[next] {
			if len(queue) >= maxPreviewQueue {
				return nil, false, fmt.Errorf("TIFF XMP directory queue exceeds safety limits")
			}
			queue = append(queue, tiffXMPDirectory{offset: next, depth: directory.depth + 1})
		}
	}
	return nil, false, nil
}

func (reader *tiffXMPReader) readDirectory(offset uint64) ([]tiffXMPEntry, uint64, error) {
	countWidth, entryWidth, nextWidth := uint64(2), uint64(12), uint64(4)
	if reader.bigTIFF {
		countWidth, entryWidth, nextWidth = 8, 20, 8
	}
	countBytes, err := reader.readAt(offset, countWidth)
	if err != nil {
		return nil, 0, fmt.Errorf("read TIFF directory count: %w", err)
	}
	var count uint64
	if reader.bigTIFF {
		count = reader.order.Uint64(countBytes)
	} else {
		count = uint64(reader.order.Uint16(countBytes))
	}
	if count > maxPreviewIFDEntries {
		return nil, 0, fmt.Errorf("TIFF image directory has too many entries")
	}
	entriesLength := count * entryWidth
	if count != 0 && entriesLength/count != entryWidth {
		return nil, 0, fmt.Errorf("TIFF directory size overflow")
	}
	bodyOffset := offset + countWidth
	if bodyOffset < offset {
		return nil, 0, fmt.Errorf("TIFF directory offset overflow")
	}
	body, err := reader.readAt(bodyOffset, entriesLength+nextWidth)
	if err != nil {
		return nil, 0, fmt.Errorf("read TIFF directory entries: %w", err)
	}
	entries := make([]tiffXMPEntry, 0, int(count))
	for index := uint64(0); index < count; index++ {
		start := index * entryWidth
		encoded := body[start : start+entryWidth]
		entry := tiffXMPEntry{tag: reader.order.Uint16(encoded[:2]), typeID: reader.order.Uint16(encoded[2:4])}
		if reader.bigTIFF {
			entry.count = reader.order.Uint64(encoded[4:12])
			entry.value = append([]byte(nil), encoded[12:20]...)
		} else {
			entry.count = uint64(reader.order.Uint32(encoded[4:8]))
			entry.value = append([]byte(nil), encoded[8:12]...)
		}
		entries = append(entries, entry)
	}
	nextBytes := body[entriesLength : entriesLength+nextWidth]
	var next uint64
	if reader.bigTIFF {
		next = reader.order.Uint64(nextBytes)
	} else {
		next = uint64(reader.order.Uint32(nextBytes))
	}
	return entries, next, nil
}

func (reader *tiffXMPReader) readPacket(entry tiffXMPEntry) ([]byte, bool, error) {
	if entry.typeID != 1 && entry.typeID != 2 && entry.typeID != 7 {
		return nil, false, fmt.Errorf("TIFF XMP tag 700 has unsupported field type %d", entry.typeID)
	}
	if entry.count == 0 || entry.count > maxEmbeddedTIFFXMPSize {
		return nil, false, fmt.Errorf("TIFF XMP packet has an unsupported size")
	}
	data, err := reader.readEntryData(entry, 1)
	if err != nil {
		return nil, false, fmt.Errorf("read TIFF XMP tag 700: %w", err)
	}
	data = bytes.TrimRight(data, "\x00")
	if len(data) == 0 {
		return nil, false, nil
	}
	return data, true, nil
}

func (reader *tiffXMPReader) readDirectoryOffsets(entry tiffXMPEntry) ([]uint64, error) {
	unitWidth := uint64(0)
	switch entry.typeID {
	case 4, 13:
		unitWidth = 4
	case 16, 18:
		unitWidth = 8
	default:
		return nil, fmt.Errorf("unsupported TIFF SubIFD offset type %d", entry.typeID)
	}
	if entry.count == 0 || entry.count > maxPreviewIFDEntries || entry.count > maxPreviewQueue {
		return nil, fmt.Errorf("invalid TIFF SubIFD offset count")
	}
	data, err := reader.readEntryData(entry, unitWidth)
	if err != nil {
		return nil, err
	}
	offsets := make([]uint64, int(entry.count))
	for index := range offsets {
		start := uint64(index) * unitWidth
		if unitWidth == 8 {
			offsets[index] = reader.order.Uint64(data[start : start+unitWidth])
		} else {
			offsets[index] = uint64(reader.order.Uint32(data[start : start+unitWidth]))
		}
	}
	return offsets, nil
}

func (reader *tiffXMPReader) readEntryData(entry tiffXMPEntry, unitWidth uint64) ([]byte, error) {
	if entry.count == 0 || entry.count > maxEmbeddedTIFFXMPSize/unitWidth {
		return nil, fmt.Errorf("TIFF value size exceeds safety limit")
	}
	length := entry.count * unitWidth
	if length/unitWidth != entry.count {
		return nil, fmt.Errorf("TIFF value size overflow")
	}
	inlineWidth := uint64(4)
	if reader.bigTIFF {
		inlineWidth = 8
	}
	if length <= inlineWidth {
		return append([]byte(nil), entry.value[:length]...), nil
	}
	var offset uint64
	if reader.bigTIFF {
		offset = reader.order.Uint64(entry.value)
	} else {
		offset = uint64(reader.order.Uint32(entry.value))
	}
	return reader.readAt(offset, length)
}

func (reader *tiffXMPReader) readAt(offset, length uint64) ([]byte, error) {
	if !tiffRange(offset, length, reader.size) || length > maxEmbeddedTIFFXMPSize {
		return nil, fmt.Errorf("TIFF data range is outside the file or too large")
	}
	data := make([]byte, int(length))
	read, err := reader.file.ReadAt(data, int64(offset))
	if err != nil && err != io.EOF {
		return nil, err
	}
	if uint64(read) != length {
		return nil, io.ErrUnexpectedEOF
	}
	return data, nil
}
