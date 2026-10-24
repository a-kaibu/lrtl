package lrtl

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"unicode/utf16"
)

// These are string-cache operations, not calendar validation. Java indices
// count UTF-16 code units; invalid date text is retained by the native reader.
func nativeCaptureDateText(value string) string {
	u := utf16.Encode([]rune(value))
	if len(u) < 18 {
		return value
	}
	for _, i := range []int{5, 8, 11, 14, 17} {
		if u[i] == ' ' {
			u[i] = '0'
		}
	}
	if u[4] == ':' && u[7] == ':' && (u[10] == ' ' || u[10] == 'T') {
		u[4], u[7], u[10] = '-', '-', 'T'
	}
	if len(u) == 25 && (u[19] == '+' || u[19] == '-') {
		u = u[:19]
	}
	if len(u) > 22 {
		u = u[:22]
	}
	return string(utf16.Decode(u))
}

func nativeCaptureDateFields(seed *string, date, subsecond, fallback string) string {
	value, present := "", seed != nil
	if seed != nil {
		value = *seed
	}
	if date != "-" {
		candidate := date
		u := utf16.Encode([]rune(subsecond))
		if len(u) >= 22 && u[19] == '.' {
			u = u[:22]
			if u[21] == '+' || u[21] == '-' || u[21] == 'Z' {
				u[21] = '0'
			}
			candidate = string(utf16.Decode(u))
		}
		value, present = nativeCaptureDateText(candidate), true
	}
	if !present && fallback != "-" {
		value = nativeCaptureDateText(fallback)
	}
	if value == "" {
		return "n/a"
	}
	return value
}

type nativeCaptureDateTags struct{ values map[uint16]string }

func (tags *nativeCaptureDateTags) scan(reader *tiffXMPReader, first uint64) ([]byte, bool, error) {
	var visit func(uint64, int) error
	visit = func(offset uint64, depth int) error {
		if offset == 0 || reader.visited[offset] {
			return nil
		}
		if depth > 32 || reader.directories >= maxPreviewIFDs {
			return fmt.Errorf("EXIF date directory graph exceeds safety limits")
		}
		reader.visited[offset] = true
		reader.directories++
		entries, next, err := reader.readDirectory(offset)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			switch entry.tag {
			case 36867, 36868, 37521, 36881:
				if _, exists := tags.values[entry.tag]; exists {
					continue
				}
				if entry.typeID != 2 || entry.count == 0 {
					continue
				}
				if entry.count > 256 {
					return fmt.Errorf("EXIF capture date string exceeds safety limit")
				}
				data, err := reader.readEntryData(entry, 1)
				if err != nil {
					return err
				}
				tags.values[entry.tag] = string(bytes.TrimRight(data, "\x00"))
			case 330, 34665:
				offsets, err := reader.readDirectoryOffsets(entry)
				if err != nil {
					continue
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
}

// Read raw date strings so trailing zeroes and the original subsecond precision
// survive. Converting to time.Time first would erase information used by LRT.
func readNativeCaptureDate(path string) (string, bool, error) {
	return readNativeCaptureDateWithSeed(path, nil)
}

func readNativeCaptureDateWithSeed(path string, seed *string) (string, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", false, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", false, err
	}
	tags := nativeCaptureDateTags{values: map[uint16]string{}}
	var signature [2]byte
	if _, err := file.ReadAt(signature[:], 0); err != nil {
		if err == io.EOF {
			return "n/a", false, nil
		}
		return "", false, err
	}
	if signature == [2]byte{137, 'P'} {
		var pngSignature [8]byte
		if _, err := file.ReadAt(pngSignature[:], 0); err != nil {
			return "", false, err
		}
		if string(pngSignature[:]) != "\x89PNG\r\n\x1a\n" {
			return "n/a", false, nil
		}
		data, found, err := readNativePNGExif(file)
		if err != nil {
			return "", false, err
		}
		if found {
			if _, _, err := readTIFFMetadataAt(bytes.NewReader(data), int64(len(data)), tags.scan); err != nil {
				return "", false, err
			}
		}
	} else if signature == [2]byte{255, 216} {
		if _, err := file.Seek(2, io.SeekStart); err != nil {
			return "", false, err
		}
		for {
			marker, err := nextJPEGMarker(file)
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				break
			}
			if err != nil {
				return "", false, err
			}
			if marker == 0xda || marker == 0xd9 {
				break
			}
			if marker == 1 || marker >= 0xd0 && marker <= 0xd7 {
				continue
			}
			var length [2]byte
			if _, err := io.ReadFull(file, length[:]); err != nil {
				return "", false, err
			}
			size := int64(binary.BigEndian.Uint16(length[:])) - 2
			if size < 0 {
				return "", false, fmt.Errorf("invalid JPEG EXIF segment length")
			}
			start, err := file.Seek(0, io.SeekCurrent)
			if err != nil {
				return "", false, err
			}
			if start > info.Size()-size {
				return "", false, fmt.Errorf("truncated JPEG EXIF segment")
			}
			var prefix [6]byte
			if marker == 0xe1 && size >= 6 {
				if _, err := file.ReadAt(prefix[:], start); err != nil {
					return "", false, err
				}
				if string(prefix[:]) == "Exif\x00\x00" {
					_, _, err = readTIFFMetadataAt(io.NewSectionReader(file, start+6, size-6), size-6, tags.scan)
					if err != nil {
						return "", false, err
					}
					break
				}
			}
			if _, err := file.Seek(start+size, io.SeekStart); err != nil {
				return "", false, err
			}
		}
	} else {
		if _, _, err := readTIFFMetadataAt(file, info.Size(), tags.scan); err != nil {
			return "", false, err
		}
	}
	date, hasOriginal := tags.values[36867]
	fallback, hasFallback := tags.values[36868]
	// ExifTool's named date queries also use image XMP when the EXIF tag is
	// absent. Read its original property names: ExifTool does not apply Adobe's
	// DateTimeDigitized -> CreateDate alias at this stage.
	if !hasOriginal || !hasFallback {
		data, found, err := readEmbeddedImageXMP(path)
		if err != nil {
			return "", false, err
		}
		if found {
			packet, err := parseXMPWithCaptureAliases(data, false)
			if err != nil {
				return "", false, err
			}
			read := func(property string) (string, bool, error) {
				value, exists, err := captureTimestampScalar(packet, property)
				if len(value) >= 19 && value[4] == '-' && value[7] == '-' && (value[10] == 'T' || value[10] == ' ') {
					u := []byte(value)
					u[4], u[7], u[10] = ':', ':', ' '
					value = string(u)
				}
				return value, exists, err
			}
			if !hasOriginal {
				date, hasOriginal, err = read("exif:DateTimeOriginal")
				if err != nil {
					return "", false, err
				}
			}
			if !hasFallback {
				fallback, hasFallback, err = read("xmp:CreateDate")
				if err != nil {
					return "", false, err
				}
			}
		}
	}
	if !hasOriginal {
		date = "-"
	}
	if !hasFallback {
		fallback = "-"
	}
	sub := "-"
	if hasOriginal {
		fraction, hasFraction := tags.values[37521]
		zone, hasZone := tags.values[36881]
		if hasFraction || hasZone {
			sub = date
			if hasFraction {
				sub += "." + fraction
			}
			sub += zone
		}
	}
	return nativeCaptureDateFields(seed, date, sub, fallback), hasOriginal || hasFallback, nil
}

func applyNativeCaptureDate(packet *XMPPacket, date string, active bool) error {
	if !active || date == "" {
		return nil
	}
	for _, property := range []string{"exif:DateTimeOriginal", "xmp:CreateDate", "xmp:ModifyDate", "photoshop:DateCreated"} {
		if err := setCaptureTimestampScalar(packet, property, date); err != nil {
			return err
		}
	}
	return nil
}
