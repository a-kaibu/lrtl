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

var embeddedRawPreviewExtensions = map[string]bool{
	".arw": true, ".cr2": true, ".cr3": true, ".dng": true, ".nef": true,
	".nrw": true, ".orf": true, ".pef": true, ".raf": true,
	".rw2": true, ".srw": true,
}

var cr3PreviewUUID = []byte{
	0xea, 0xf4, 0x2b, 0x5e, 0x1c, 0x98, 0x4b, 0x88,
	0xb9, 0xfb, 0xb7, 0xdc, 0x40, 0x6e, 0x4d, 0x16,
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
	return embeddedRawPreviewExtensions[strings.ToLower(filepath.Ext(path))]
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
		data, err = extractRawPreview(path, raw)
	} else {
		return nil, image.Config{}, "", fmt.Errorf("render supports JPEG, PNG, TIFF, and selected RAW previews; unsupported image type: %s", filepath.Base(path))
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

func extractRawPreview(path string, data []byte) ([]byte, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".cr3":
		return extractCR3Preview(data)
	case ".raf":
		return extractRAFPreview(data)
	default:
		return extractTIFFPreview(data)
	}
}

func extractCR3Preview(data []byte) ([]byte, error) {
	var best []byte
	bestArea := uint64(0)
	searchFrom := 0
	for searchFrom < len(data) {
		relative := bytes.Index(data[searchFrom:], cr3PreviewUUID)
		if relative < 0 {
			break
		}
		uuidStart := searchFrom + relative
		searchFrom = uuidStart + len(cr3PreviewUUID)
		if !tiffRange(uint64(uuidStart), 0x40, uint64(len(data))) {
			continue
		}
		previewLength := uint64(binary.BigEndian.Uint32(data[uuidStart+0x3c : uuidStart+0x40]))
		if previewLength == 0 || previewLength > maxPreviewBytes ||
			!tiffRange(uint64(uuidStart)+0x40, previewLength, uint64(len(data))) {
			continue
		}
		previewEnd := uuidStart + 0x40 + int(previewLength)
		previewRegion := data[uuidStart+0x30 : previewEnd]
		jpegStart := bytes.Index(previewRegion, []byte{0xff, 0xd8, 0xff})
		if jpegStart < 0 {
			continue
		}
		preview := previewRegion[jpegStart:]
		config, format, err := image.DecodeConfig(bytes.NewReader(preview))
		if err != nil || format != "jpeg" || config.Width < 1 || config.Height < 1 {
			continue
		}
		area := uint64(config.Width) * uint64(config.Height)
		if area > bestArea {
			bestArea = area
			best = append(best[:0], preview...)
		}
	}
	if len(best) == 0 {
		return nil, fmt.Errorf("no decodable Canon CR3 UUID preview was found")
	}
	return best, nil
}

func extractRAFPreview(data []byte) ([]byte, error) {
	const rafHeaderSize = 92
	if len(data) < rafHeaderSize || string(data[:16]) != "FUJIFILMCCD-RAW " {
		return nil, fmt.Errorf("invalid Fujifilm RAF header")
	}
	offset := uint64(binary.BigEndian.Uint32(data[84:88]))
	length := uint64(binary.BigEndian.Uint32(data[88:92]))
	if length == 0 || length > maxPreviewBytes || !tiffRange(offset, length, uint64(len(data))) {
		return nil, fmt.Errorf("Fujifilm RAF preview range is invalid")
	}
	preview := data[int(offset):int(offset+length)]
	config, format, err := image.DecodeConfig(bytes.NewReader(preview))
	if err != nil || format != "jpeg" || config.Width < 1 || config.Height < 1 {
		return nil, fmt.Errorf("Fujifilm RAF embedded preview is not a decodable JPEG")
	}
	return preview, nil
}

func extractTIFFPreview(data []byte) ([]byte, error) {
	if len(data) < 8 {
		return nil, fmt.Errorf("TIFF-layout RAW file is too short")
	}
	var order binary.ByteOrder
	switch string(data[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return nil, fmt.Errorf("RAW preview extraction requires a TIFF-layout directory header")
	}
	magic := order.Uint16(data[2:4])
	if magic != 42 && magic != 85 && magic != 0x4f52 && magic != 0x5352 {
		return nil, fmt.Errorf("RAW preview extraction currently supports classic TIFF, Olympus ORF, and Panasonic RW2 directories only")
	}
	firstIFD := order.Uint32(data[4:8])
	if firstIFD == 0 {
		return nil, fmt.Errorf("TIFF-layout RAW file has no image directory")
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
		return nil, fmt.Errorf("no decodable embedded preview was found in this TIFF-layout RAW file")
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
			case 46, 273, 279, 330, 34665, 34853, 40965, 513, 514:
			default:
				continue
			}
			if entry.tag == 46 {
				segment, err := scanner.dataSegment(entry)
				if err == nil {
					if err := scanner.addSegment(segment); err != nil {
						return err
					}
				}
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

func (scanner *tiffPreviewScanner) dataSegment(entry tiffPreviewEntry) (tiffPreviewSegment, error) {
	unitSize := tiffTypeSize(entry.typeID)
	if unitSize == 0 || entry.count == 0 {
		return tiffPreviewSegment{}, fmt.Errorf("unsupported TIFF image data field")
	}
	byteCount := uint64(unitSize) * uint64(entry.count)
	if byteCount/uint64(unitSize) != uint64(entry.count) || byteCount <= 4 || byteCount > maxPreviewBytes {
		return tiffPreviewSegment{}, fmt.Errorf("invalid TIFF image data size")
	}
	offset := uint64(scanner.order.Uint32(entry.value))
	if !tiffRange(offset, byteCount, uint64(len(scanner.data))) {
		return tiffPreviewSegment{}, fmt.Errorf("TIFF image data is outside the file")
	}
	return tiffPreviewSegment{offset: offset, length: byteCount}, nil
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
