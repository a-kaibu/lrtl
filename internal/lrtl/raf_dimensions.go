package lrtl

import (
	"encoding/binary"
	"fmt"
	"image/jpeg"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// nativeImageSize supplies metadata dimensions, which can differ from the
// decodable RAW preview size. Zero axes are retained for the native save layer.
func nativeImageSize(path string) (int32, int32, bool, error) {
	if strings.EqualFold(filepath.Ext(path), ".raf") {
		return nativeRAFImageSize(path)
	}
	return nativeTIFFImageSize(path)
}

func nativeRAFImageSize(path string) (int32, int32, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, 0, false, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return 0, 0, false, err
	}
	return nativeRAFImageSizeAt(file, info.Size())
}

func nativeRAFImageSizeAt(file io.ReaderAt, size int64) (int32, int32, bool, error) {
	if size < 16 {
		return 0, 0, false, nil
	}
	var header [136]byte
	if _, err := file.ReadAt(header[:16], 0); err != nil {
		return 0, 0, false, err
	}
	if string(header[:16]) != "FUJIFILMCCD-RAW " {
		return 0, 0, false, nil
	}
	if size < 112 {
		return 0, 0, false, fmt.Errorf("truncated RAF dimension header")
	}
	n := min(size, int64(len(header)))
	if _, err := file.ReadAt(header[:n], 0); err != nil {
		return 0, 0, false, err
	}
	jpos := uint64(binary.BigEndian.Uint32(header[84:88]))
	jlen := uint64(binary.BigEndian.Uint32(header[88:92]))
	// The installed metadata reader rejects the legacy high flag in this field.
	if jpos&0x8000 != 0 || jpos == 0 || jlen == 0 {
		return 0, 0, true, nil
	}
	if !tiffRange(jpos, jlen, uint64(size)) {
		return 0, 0, false, fmt.Errorf("RAF dimension preview range is invalid")
	}
	config, err := jpeg.DecodeConfig(io.NewSectionReader(file, int64(jpos), int64(jlen)))
	if err != nil {
		// No ImageSize composite exists without the preview's image dimensions,
		// even when the RAF directory contains a valid raw crop pair.
		return 0, 0, true, nil
	}
	width, height := int32(config.Width), int32(config.Height)
	seen := false
	var crop []byte
	for _, pointer := range []uint64{92, 120} {
		if pointer >= jpos {
			break
		}
		if pointer+8 > uint64(n) {
			return 0, 0, false, fmt.Errorf("truncated RAF directory pointer")
		}
		offset := uint64(binary.BigEndian.Uint32(header[pointer : pointer+4]))
		if offset == 0 {
			continue
		}
		var count [4]byte
		if !tiffRange(offset, 4, uint64(size)) {
			return 0, 0, false, fmt.Errorf("RAF dimension directory is outside file")
		}
		if _, err := file.ReadAt(count[:], int64(offset)); err != nil {
			return 0, 0, false, err
		}
		entries := binary.BigEndian.Uint32(count[:])
		if entries >= 256 {
			continue
		}
		position := offset + 4
		for i := uint32(0); i < entries; i++ {
			var entry [4]byte
			if !tiffRange(position, 4, uint64(size)) {
				return 0, 0, false, fmt.Errorf("truncated RAF dimension entry")
			}
			if _, err := file.ReadAt(entry[:], int64(position)); err != nil {
				return 0, 0, false, err
			}
			length := uint64(binary.BigEndian.Uint16(entry[2:]))
			position += 4
			if !tiffRange(position, length, uint64(size)) {
				return 0, 0, false, fmt.Errorf("RAF dimension entry data is outside file")
			}
			if binary.BigEndian.Uint16(entry[:2]) == 0x111 && !seen {
				// A partial first value blocks subsequent duplicate crop tags.
				// Empty values fall back to JPEG dimensions; one integer creates
				// a non-pair ImageSize which the native CSV cache reads as zero.
				seen = true
				crop = make([]byte, min(length, 4))
				if len(crop) != 0 {
					if _, err := file.ReadAt(crop, int64(position)); err != nil {
						return 0, 0, false, err
					}
				}
			}
			position += length
		}
	}
	if len(crop) >= 4 {
		height, width = int32(binary.BigEndian.Uint16(crop[:2])), int32(binary.BigEndian.Uint16(crop[2:4]))
	} else if len(crop) >= 2 {
		width, height = 0, 0
	}
	return width, height, true, nil
}
