package lrtl

import (
	"bytes"
	"fmt"
	"image/jpeg"
	"math"
)

// Panasonic RAW/RW2/RWL uses sensor borders in its main directory chain.
// Standard EXIF directories and the embedded JPEG can fill missing axes;
// computed sensor-border dimensions take precedence, including zero/negative.
func readNativeRW2Dimensions(reader *tiffXMPReader, first uint64) (int32, int32, bool, error) {
	borders := map[uint16]uint64{}
	var fallback rawDimensionGraph
	var previewWidth, previewHeight int
	var previews []tiffXMPEntry
	var visit func(uint64, int) error
	visit = func(offset uint64, depth int) error {
		if offset == 0 || reader.visited[offset] {
			return nil
		}
		if depth > 32 || reader.directories >= maxPreviewIFDs {
			return fmt.Errorf("RW2 dimension directory graph exceeds safety limits")
		}
		reader.visited[offset] = true
		reader.directories++
		entries, next, err := reader.readDirectory(offset)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			switch entry.tag {
			case 4, 5, 6, 7:
				if entry.typeID != 3 && entry.typeID != 4 {
					return fmt.Errorf("unsupported RW2 border type %d", entry.typeID)
				}
				number, err := rawDimensionInteger(reader, entry)
				if err != nil {
					return err
				}
				borders[entry.tag] = number
			case 46:
				if len(previews) >= maxPreviewIFDEntries {
					return fmt.Errorf("RW2 JPEG field count exceeds safety limit")
				}
				previews = append(previews, entry)
			case 34665:
				offsets, err := reader.readDirectoryOffsets(entry)
				if err != nil {
					return err
				}
				for _, child := range offsets {
					if err := fallback.visit(reader, child, depth+1); err != nil {
						return err
					}
				}
			}
		}
		return visit(next, depth+1)
	}
	if err := visit(first, 0); err != nil {
		return 0, 0, false, err
	}
	_, left := borders[5]
	_, right := borders[7]
	_, top := borders[4]
	_, bottom := borders[6]
	if (!(left && right) && !fallback.width.present) || (!(top && bottom) && !fallback.height.present) {
		for _, entry := range previews {
			if entry.typeID != 1 && entry.typeID != 7 {
				return 0, 0, false, fmt.Errorf("unsupported RW2 JPEG field type %d", entry.typeID)
			}
			data, err := reader.readEntryData(entry, 1)
			if err != nil {
				return 0, 0, false, err
			}
			if config, err := jpeg.DecodeConfig(bytes.NewReader(data)); err == nil {
				previewWidth, previewHeight = config.Width, config.Height
			}
		}
	}
	var values [2]int64
	var present, overflow [2]bool
	for axis, pair := range [][2]uint16{{5, 7}, {4, 6}} {
		low, haveLow := borders[pair[0]]
		high, haveHigh := borders[pair[1]]
		if haveLow && haveHigh {
			values[axis] = int64(high) - int64(low)
			present[axis] = true
			continue
		}
		ordinary := fallback.width
		preview := previewWidth
		if axis == 1 {
			ordinary, preview = fallback.height, previewHeight
		}
		if ordinary.present {
			present[axis] = true
			overflow[axis] = ordinary.value > math.MaxInt32
			if !overflow[axis] {
				values[axis] = int64(ordinary.value)
			}
		} else if preview > 0 {
			values[axis], present[axis] = int64(preview), true
		}
	}
	if !present[0] || !present[1] {
		return 0, 0, false, nil
	}
	for axis, value := range values {
		if overflow[axis] || value < math.MinInt32 || value > math.MaxInt32 {
			return 0, 0, false, fmt.Errorf("RW2 image dimensions exceed native metadata range")
		}
	}
	return int32(values[0]), int32(values[1]), true, nil
}
