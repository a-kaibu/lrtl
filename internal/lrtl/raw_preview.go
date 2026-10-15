package lrtl

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"os"
	"path/filepath"
	"strings"

	_ "golang.org/x/image/tiff"
)

const (
	maxPreviewIFDEntries = 16384
	maxPreviewIFDs       = 1024
	maxPreviewQueue      = 4096
	maxPreviewSegments   = 8192
	maxPreviewBytes      = 512 << 20
)

var tiffRawPreviewExtensions = map[string]bool{
	".arw": true, ".cr2": true, ".dng": true, ".nef": true,
	".nrw": true, ".pef": true, ".srw": true,
}

type tiffPreviewDirectory struct {
	offset uint32
	depth  int
}

type tiffPreviewSegment struct {
	offset uint64
	length uint64
}

type tiffPreviewScanner struct {
	data        []byte
	order       binary.ByteOrder
	visited     map[uint32]bool
	segments    []tiffPreviewSegment
	directories int
}

func supportsEmbeddedRawPreview(path string) bool {
	return tiffRawPreviewExtensions[strings.ToLower(filepath.Ext(path))]
}

func readRenderInput(frame Frame, useRawPreview bool) ([]byte, image.Config, string, error) {
	path := frame.Image
	var data []byte
	var err error
	if supportedRasterExtension(path) {
		data, err = os.ReadFile(path)
	} else if supportsEmbeddedRawPreview(path) {
		if !useRawPreview {
			return nil, image.Config{}, "", fmt.Errorf("RAW input %s requires --raw-preview; this uses the camera's embedded preview, not RAW development", filepath.Base(path))
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil, image.Config{}, "", fmt.Errorf("read %s: %w", path, readErr)
		}
		data, err = extractTIFFPreview(raw)
	} else {
		return nil, image.Config{}, "", fmt.Errorf("render supports JPEG, PNG, and TIFF; embedded previews from TIFF-based RAW files require --raw-preview: %s", filepath.Base(path))
	}
	if err != nil {
		return nil, image.Config{}, "", fmt.Errorf("read %s: %w", path, err)
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, image.Config{}, "", fmt.Errorf("decode %s: %w", path, err)
	}
	if !supportedRasterFormat(format) {
		return nil, image.Config{}, "", fmt.Errorf("render supports JPEG, PNG, and TIFF data only: %s", path)
	}
	return data, config, format, nil
}

func extractTIFFPreview(data []byte) ([]byte, error) {
	if len(data) < 8 {
		return nil, fmt.Errorf("TIFF-based RAW file is too short")
	}
	var order binary.ByteOrder
	switch string(data[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return nil, fmt.Errorf("RAW preview extraction requires a TIFF-based RAW file")
	}
	if order.Uint16(data[2:4]) != 42 {
		return nil, fmt.Errorf("RAW preview extraction currently supports classic TIFF directories only")
	}
	firstIFD := order.Uint32(data[4:8])
	if firstIFD == 0 {
		return nil, fmt.Errorf("TIFF-based RAW file has no image directory")
	}

	scanner := &tiffPreviewScanner{data: data, order: order, visited: make(map[uint32]bool)}
	if err := scanner.scan(firstIFD); err != nil {
		return nil, err
	}
	var best []byte
	bestArea := uint64(0)
	seenSegments := make(map[tiffPreviewSegment]bool)
	for _, segment := range scanner.segments {
		if seenSegments[segment] || segment.length == 0 || segment.length > maxPreviewBytes ||
			segment.offset > uint64(len(data)) || segment.length > uint64(len(data))-segment.offset {
			continue
		}
		seenSegments[segment] = true
		preview := data[int(segment.offset):int(segment.offset+segment.length)]
		config, format, err := image.DecodeConfig(bytes.NewReader(preview))
		if err != nil || !supportedRasterFormat(format) || config.Width < 1 || config.Height < 1 {
			continue
		}
		area := uint64(config.Width) * uint64(config.Height)
		if area > bestArea {
			bestArea = area
			best = append(best[:0], preview...)
		}
	}
	if len(best) == 0 {
		return nil, fmt.Errorf("no decodable embedded preview was found in this TIFF-based RAW file")
	}
	return best, nil
}

func (scanner *tiffPreviewScanner) scan(first uint32) error {
	queue := []tiffPreviewDirectory{{offset: first}}
	for len(queue) > 0 {
		directory := queue[0]
		queue = queue[1:]
		if directory.offset == 0 || scanner.visited[directory.offset] {
			continue
		}
		if directory.depth > 32 || scanner.directories >= maxPreviewIFDs {
			return fmt.Errorf("TIFF preview directory graph exceeds safety limits")
		}
		scanner.visited[directory.offset] = true
		scanner.directories++
		entries, next, err := scanner.readDirectory(directory.offset)
		if err != nil {
			return err
		}
		var jpegOffset, jpegLength uint64
		var stripOffsets, stripLengths []uint64
		for _, entry := range entries {
			switch entry.tag {
			case 273, 279, 330, 34665, 34853, 40965, 513, 514:
			default:
				continue
			}
			values, err := scanner.values(entry)
			if err != nil {
				continue
			}
			switch entry.tag {
			case 273:
				stripOffsets = values
			case 279:
				stripLengths = values
			case 330, 34665, 34853, 40965:
				for _, offset := range values {
					if offset <= math.MaxUint32 && offset != 0 {
						if len(queue) >= maxPreviewQueue {
							return fmt.Errorf("TIFF preview directory queue exceeds safety limits")
						}
						queue = append(queue, tiffPreviewDirectory{offset: uint32(offset), depth: directory.depth + 1})
					}
				}
			case 513:
				if len(values) > 0 {
					jpegOffset = values[0]
				}
			case 514:
				if len(values) > 0 {
					jpegLength = values[0]
				}
			}
		}
		if jpegOffset > 0 && jpegLength > 0 {
			if err := scanner.addSegment(tiffPreviewSegment{offset: jpegOffset, length: jpegLength}); err != nil {
				return err
			}
		}
		if len(stripOffsets) == len(stripLengths) {
			for index, offset := range stripOffsets {
				if offset > 0 && stripLengths[index] > 0 {
					if err := scanner.addSegment(tiffPreviewSegment{offset: offset, length: stripLengths[index]}); err != nil {
						return err
					}
				}
			}
		}
		if next != 0 {
			if len(queue) >= maxPreviewQueue {
				return fmt.Errorf("TIFF preview directory queue exceeds safety limits")
			}
			queue = append(queue, tiffPreviewDirectory{offset: next, depth: directory.depth + 1})
		}
	}
	return nil
}

func (scanner *tiffPreviewScanner) addSegment(segment tiffPreviewSegment) error {
	if len(scanner.segments) >= maxPreviewSegments {
		return fmt.Errorf("TIFF preview segment list exceeds safety limits")
	}
	scanner.segments = append(scanner.segments, segment)
	return nil
}

type tiffPreviewEntry struct {
	tag    uint16
	typeID uint16
	count  uint32
	value  []byte
}

func (scanner *tiffPreviewScanner) readDirectory(offset uint32) ([]tiffPreviewEntry, uint32, error) {
	if !tiffRange(uint64(offset), 2, uint64(len(scanner.data))) {
		return nil, 0, fmt.Errorf("TIFF image directory offset is outside the file")
	}
	start := uint64(offset)
	count := uint64(scanner.order.Uint16(scanner.data[start : start+2]))
	if count > maxPreviewIFDEntries {
		return nil, 0, fmt.Errorf("TIFF image directory has too many entries")
	}
	entriesStart := start + 2
	entriesBytes := count * 12
	if !tiffRange(entriesStart, entriesBytes+4, uint64(len(scanner.data))) {
		return nil, 0, fmt.Errorf("TIFF image directory entries extend beyond the file")
	}
	entries := make([]tiffPreviewEntry, 0, count)
	for index := uint64(0); index < count; index++ {
		begin := entriesStart + index*12
		encoded := scanner.data[begin : begin+12]
		entries = append(entries, tiffPreviewEntry{
			tag: scanner.order.Uint16(encoded[0:2]), typeID: scanner.order.Uint16(encoded[2:4]),
			count: scanner.order.Uint32(encoded[4:8]), value: encoded[8:12],
		})
	}
	next := scanner.order.Uint32(scanner.data[entriesStart+entriesBytes : entriesStart+entriesBytes+4])
	return entries, next, nil
}

func (scanner *tiffPreviewScanner) values(entry tiffPreviewEntry) ([]uint64, error) {
	unitSize := tiffTypeSize(entry.typeID)
	if unitSize == 0 || entry.count == 0 || entry.count > maxPreviewIFDEntries {
		return nil, fmt.Errorf("unsupported TIFF value field")
	}
	byteCount := uint64(unitSize) * uint64(entry.count)
	if byteCount/uint64(unitSize) != uint64(entry.count) {
		return nil, fmt.Errorf("TIFF value size overflow")
	}
	valueBytes := entry.value
	if byteCount > 4 {
		valueOffset := uint64(scanner.order.Uint32(entry.value))
		if !tiffRange(valueOffset, byteCount, uint64(len(scanner.data))) {
			return nil, fmt.Errorf("TIFF value data is outside the file")
		}
		valueBytes = scanner.data[valueOffset : valueOffset+byteCount]
	}
	values := make([]uint64, entry.count)
	for index := range values {
		begin := uint64(index) * uint64(unitSize)
		value := valueBytes[begin : begin+uint64(unitSize)]
		switch entry.typeID {
		case 1, 2, 6, 7:
			values[index] = uint64(value[0])
		case 3, 8:
			values[index] = uint64(scanner.order.Uint16(value))
		case 4, 9, 13:
			values[index] = uint64(scanner.order.Uint32(value))
		default:
			return nil, fmt.Errorf("TIFF field is not an unsigned integer")
		}
	}
	return values, nil
}

func tiffTypeSize(typeID uint16) uint32 {
	switch typeID {
	case 1, 2, 6, 7:
		return 1
	case 3, 8:
		return 2
	case 4, 9, 11, 13:
		return 4
	case 5, 10, 12:
		return 8
	default:
		return 0
	}
}

func tiffRange(offset, length, size uint64) bool {
	return offset <= size && length <= size-offset
}
