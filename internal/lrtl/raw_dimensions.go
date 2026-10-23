package lrtl

import (
	"bytes"
	"fmt"
	"math"
	"path/filepath"
	"strings"
)

type rawDimensionValue struct {
	value   uint64
	present bool
}

func (value *rawDimensionValue) accept(number uint64, priority bool) {
	if !value.present || priority {
		value.value, value.present = number, true
	}
}

// ImageSize uses individually prioritized TIFF dimensions, not the largest IFD.
// The first explicit full-resolution directory wins priority. CR2 headers add
// the ExifImageWidth/Height override. No pixel strips or camera preview are read.
func nativeTIFFImageSize(path string) (int32, int32, bool, error) {
	var dimensions rawDimensionGraph
	var rw2Width, rw2Height int32
	var rw2Present, panasonic bool
	var cr2 bool
	_, _, err := readTIFFMetadata(path, func(reader *tiffXMPReader, first uint64) ([]byte, bool, error) {
		if reader.bigTIFF {
			suffix := strings.ToLower(filepath.Ext(path))
			// The installed ExifTool recognizes BigTIFF as BTF for TIFF files,
			// but its RAW-extension readers do not expose these TIFF dimensions.
			if suffix != ".tif" && suffix != ".tiff" {
				return nil, false, nil
			}
		}
		header, err := reader.readAt(0, 8)
		if err != nil {
			return nil, false, err
		}
		if reader.order.Uint16(header[2:4]) == 85 {
			panasonic = true
			var err error
			rw2Width, rw2Height, rw2Present, err = readNativeRW2Dimensions(reader, first)
			return nil, false, err
		}
		if reader.size >= 12 {
			signature, err := reader.readAt(8, 4)
			if err != nil {
				return nil, false, err
			}
			cr2 = !reader.bigTIFF && bytes.Equal(signature, []byte{'C', 'R', 2, 0})
		}
		return nil, false, dimensions.visit(reader, first, 0)
	})
	if err != nil {
		return 0, 0, false, err
	}
	if panasonic {
		return rw2Width, rw2Height, rw2Present, nil
	}
	width, height, exifWidth, exifHeight := dimensions.width, dimensions.height, dimensions.exifWidth, dimensions.exifHeight
	if !width.present || !height.present {
		return 0, 0, false, nil
	}
	if cr2 && exifWidth.present && exifHeight.present && exifWidth.value != 0 && exifHeight.value != 0 {
		width, height = exifWidth, exifHeight
	}
	if width.value > math.MaxInt32 || height.value > math.MaxInt32 {
		return 0, 0, false, fmt.Errorf("image dimensions exceed native metadata range")
	}
	return int32(width.value), int32(height.value), true, nil
}

type rawDimensionGraph struct {
	width, height, exifWidth, exifHeight rawDimensionValue
	priorityDirectory                    uint64
}

func (graph *rawDimensionGraph) visit(reader *tiffXMPReader, offset uint64, depth int) error {
	if offset == 0 || reader.visited[offset] {
		return nil
	}
	if depth > 32 || reader.directories >= maxPreviewIFDs {
		return fmt.Errorf("TIFF dimension directory graph exceeds safety limits")
	}
	reader.visited[offset] = true
	reader.directories++
	entries, next, err := reader.readDirectory(offset)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		switch entry.tag {
		case 254, 255, 256, 257, 40962, 40963:
			number, err := rawDimensionInteger(reader, entry)
			if err != nil {
				return err
			}
			if (entry.tag == 254 && number == 0 || entry.tag == 255 && number == 1) && graph.priorityDirectory == 0 {
				graph.priorityDirectory = offset
			}
			priority := offset == graph.priorityDirectory
			switch entry.tag {
			case 256:
				graph.width.accept(number, priority)
			case 257:
				graph.height.accept(number, priority)
			case 40962:
				graph.exifWidth.accept(number, true)
			case 40963:
				graph.exifHeight.accept(number, true)
			}
		case 330, 34665:
			offsets, err := reader.readDirectoryOffsets(entry)
			if err != nil {
				return err
			}
			for _, child := range offsets {
				if err := graph.visit(reader, child, depth+1); err != nil {
					return err
				}
			}
		}
	}
	return graph.visit(reader, next, depth+1)
}

func rawDimensionInteger(reader *tiffXMPReader, entry tiffXMPEntry) (uint64, error) {
	if entry.count != 1 {
		return 0, fmt.Errorf("TIFF dimension tag %d must contain one integer", entry.tag)
	}
	var size uint64
	switch entry.typeID {
	case 3:
		size = 2
	case 4:
		size = 4
	case 16:
		size = 8
	default:
		return 0, fmt.Errorf("unsupported TIFF dimension type %d", entry.typeID)
	}
	data, err := reader.readEntryData(entry, size)
	if err != nil {
		return 0, err
	}
	switch size {
	case 2:
		return uint64(reader.order.Uint16(data)), nil
	case 4:
		return uint64(reader.order.Uint32(data)), nil
	default:
		return reader.order.Uint64(data), nil
	}
}
