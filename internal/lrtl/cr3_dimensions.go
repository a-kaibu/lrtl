package lrtl

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
)

var cr3MetadataUUID = []byte{0x85, 0xc0, 0xb6, 0x87, 0x82, 0x0f, 0x11, 0xe0, 0x81, 0x11, 0xf4, 0xce, 0x46, 0x2b, 0x6a, 0x48}

type cr3DimensionReader struct {
	file          io.ReaderAt
	width, height rawDimensionValue
	boxes         int
}

func nativeCR3ImageSize(path string) (int32, int32, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, 0, false, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return 0, 0, false, err
	}
	return nativeCR3ImageSizeAt(file, info.Size())
}

func nativeCR3ImageSizeAt(file io.ReaderAt, size int64) (int32, int32, bool, error) {
	if size < 16 {
		return 0, 0, false, nil
	}
	var header [16]byte
	if _, err := file.ReadAt(header[:], 0); err != nil {
		return 0, 0, false, err
	}
	if string(header[4:8]) != "ftyp" || string(header[8:12]) != "crx " {
		return 0, 0, false, nil
	}
	reader := cr3DimensionReader{file: file}
	if err := reader.visit(0, uint64(size), "top", 0); err != nil {
		return 0, 0, false, err
	}
	if !reader.width.present || !reader.height.present {
		return 0, 0, true, nil
	}
	if reader.width.value > math.MaxInt32 || reader.height.value > math.MaxInt32 {
		return 0, 0, false, fmt.Errorf("CR3 dimensions exceed native metadata range")
	}
	return int32(reader.width.value), int32(reader.height.value), true, nil
}

// Dimensions have first-value precedence per axis across the encountered
// QuickTime track and Canon TIFF blocks. CR2's full-resolution/Exif overrides
// do not apply to this container.
func (reader *cr3DimensionReader) visit(start, end uint64, context string, depth int) error {
	if depth > 16 {
		return fmt.Errorf("CR3 dimension box nesting exceeds limit")
	}
	for position := start; position < end; {
		reader.boxes++
		if reader.boxes > 4096 {
			return fmt.Errorf("CR3 dimension box count exceeds limit")
		}
		if end-position < 8 {
			return fmt.Errorf("truncated CR3 dimension box header")
		}
		var header [16]byte
		if _, err := reader.file.ReadAt(header[:8], int64(position)); err != nil {
			return err
		}
		length := uint64(binary.BigEndian.Uint32(header[:4]))
		zero := length == 0
		kind := string(header[4:8])
		prefix := uint64(8)
		if length == 1 {
			if end-position < 16 {
				return fmt.Errorf("truncated CR3 extended box size")
			}
			if _, err := reader.file.ReadAt(header[8:], int64(position+8)); err != nil {
				return err
			}
			length, prefix = binary.BigEndian.Uint64(header[8:]), 16
		} else if zero {
			length = end - position
		}
		if length < prefix || length > end-position {
			return fmt.Errorf("CR3 dimension box exceeds parent bounds")
		}
		payload, next := position+prefix, position+length
		switch {
		case kind == "moov" && context == "top":
			if err := reader.visit(payload, next, "movie", depth+1); err != nil {
				return err
			}
		case kind == "trak" && context == "movie":
			if err := reader.visit(payload, next, "track", depth+1); err != nil {
				return err
			}
		case kind == "uuid" && context == "movie" && !zero:
			if next-payload < 16 {
				return fmt.Errorf("truncated CR3 metadata UUID")
			}
			var uuid [16]byte
			if _, err := reader.file.ReadAt(uuid[:], int64(payload)); err != nil {
				return err
			}
			if bytes.Equal(uuid[:], cr3MetadataUUID) {
				if err := reader.visit(payload+16, next, "canon", depth+1); err != nil {
					return err
				}
			}
		case (kind == "CMT1" || kind == "CMT2") && context == "canon":
			if err := reader.readTIFF(payload, next-payload); err != nil {
				return err
			}
		case kind == "tkhd" && context == "track":
			if err := reader.readTrack(payload, next-payload); err != nil {
				return err
			}
		}
		position = next
	}
	return nil
}

func (reader *cr3DimensionReader) readTrack(start, length uint64) error {
	if length == 0 {
		return fmt.Errorf("empty CR3 track header")
	}
	var header [96]byte
	if _, err := reader.file.ReadAt(header[:1], int64(start)); err != nil {
		return err
	}
	offset := 76
	if header[0] == 1 {
		offset = 88
	} else if header[0] != 0 {
		return nil
	}
	if length < uint64(offset+8) {
		return fmt.Errorf("truncated CR3 track dimensions")
	}
	if _, err := reader.file.ReadAt(header[:offset+8], int64(start)); err != nil {
		return err
	}
	w, h := uint64(binary.BigEndian.Uint32(header[offset:offset+4])>>16), uint64(binary.BigEndian.Uint32(header[offset+4:offset+8])>>16)
	if w != 0 {
		reader.width.accept(w, false)
	}
	if h != 0 {
		reader.height.accept(h, false)
	}
	return nil
}

func (reader *cr3DimensionReader) readTIFF(start, length uint64) error {
	section := io.NewSectionReader(reader.file, int64(start), int64(length))
	_, _, err := readTIFFMetadataAt(section, int64(length), func(tiff *tiffXMPReader, first uint64) ([]byte, bool, error) {
		if tiff.bigTIFF {
			return nil, false, nil
		}
		var visit func(uint64, int) error
		visit = func(offset uint64, depth int) error {
			if offset == 0 || tiff.visited[offset] {
				return nil
			}
			if depth > 32 || tiff.directories >= maxPreviewIFDs {
				return fmt.Errorf("CR3 TIFF dimension graph exceeds limits")
			}
			tiff.visited[offset] = true
			tiff.directories++
			entries, next, err := tiff.readDirectory(offset)
			if err != nil {
				return err
			}
			for _, entry := range entries {
				switch entry.tag {
				case 256, 257:
					value, err := rawDimensionInteger(tiff, entry)
					if err != nil {
						return err
					}
					if entry.tag == 256 {
						reader.width.accept(value, false)
					} else {
						reader.height.accept(value, false)
					}
				case 330, 34665:
					offsets, err := tiff.readDirectoryOffsets(entry)
					if err != nil {
						return err
					}
					for _, child := range offsets {
						if err := visit(child, depth+1); err != nil {
							return err
						}
					}
				}
			}
			return visit(next, depth+1)
		}
		return nil, false, visit(first, 0)
	})
	return err
}
