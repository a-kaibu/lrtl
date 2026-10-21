package lrtl

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"strings"
	"unicode/utf16"
)

const maxICCSize = 32 << 20

// ICC descriptions label prepared pixels; this does not apply an ICC transform.
func readImageICCDescription(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	var sig [8]byte
	if _, err := io.ReadFull(f, sig[:]); err != nil {
		return "", err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	var profile []byte
	switch {
	case sig[0] == 0xff && sig[1] == 0xd8:
		profile, err = readJPEGICC(f)
	case string(sig[:]) == "\x89PNG\r\n\x1a\n":
		profile, err = readPNGICC(f)
	case string(sig[:2]) == "II" || string(sig[:2]) == "MM":
		profile, err = readTIFFICC(f)
	}
	if err != nil {
		return "", fmt.Errorf("ICC in %s: %w", path, err)
	}
	if len(profile) == 0 {
		return "", nil
	}
	return iccDescription(profile)
}

func readJPEGICC(r io.Reader) ([]byte, error) {
	var soi [2]byte
	if _, err := io.ReadFull(r, soi[:]); err != nil {
		return nil, err
	}
	chunks := map[int][]byte{}
	count, total := 0, 0
	for {
		marker, err := nextJPEGMarker(r)
		if err != nil {
			return nil, err
		}
		if marker == 0xda || marker == 0xd9 {
			break
		}
		if marker == 1 || marker >= 0xd0 && marker <= 0xd7 {
			continue
		}
		var n [2]byte
		if _, err := io.ReadFull(r, n[:]); err != nil {
			return nil, err
		}
		length := int(binary.BigEndian.Uint16(n[:]))
		if length < 2 {
			return nil, fmt.Errorf("invalid JPEG segment length")
		}
		payload := make([]byte, length-2)
		if _, err := io.ReadFull(r, payload); err != nil {
			return nil, err
		}
		if marker != 0xe2 || !bytes.HasPrefix(payload, []byte("ICC_PROFILE\x00")) {
			continue
		}
		if len(payload) < 14 {
			return nil, fmt.Errorf("truncated JPEG ICC chunk")
		}
		index, parts := int(payload[12]), int(payload[13])
		if index < 1 || index > parts || (count != 0 && count != parts) || chunks[index] != nil {
			return nil, fmt.Errorf("invalid JPEG ICC chunk sequence")
		}
		count = parts
		total += len(payload) - 14
		if total > maxICCSize {
			return nil, fmt.Errorf("ICC profile exceeds size limit")
		}
		chunks[index] = payload[14:]
	}
	if len(chunks) != count {
		return nil, fmt.Errorf("missing JPEG ICC chunks")
	}
	profile := make([]byte, 0, total)
	for i := 1; i <= count; i++ {
		profile = append(profile, chunks[i]...)
	}
	return profile, nil
}

func readPNGICC(r io.Reader) ([]byte, error) {
	var signature [8]byte
	if _, err := io.ReadFull(r, signature[:]); err != nil {
		return nil, err
	}
	for {
		var header [8]byte
		if _, err := io.ReadFull(r, header[:]); err != nil {
			return nil, err
		}
		n := int64(binary.BigEndian.Uint32(header[:4]))
		kind := string(header[4:])
		if kind == "IDAT" || kind == "IEND" {
			return nil, nil
		}
		if kind != "iCCP" {
			if _, err := io.CopyN(io.Discard, r, n+4); err != nil {
				return nil, err
			}
			continue
		}
		if n > maxICCSize {
			return nil, fmt.Errorf("compressed ICC profile exceeds size limit")
		}
		payload := make([]byte, int(n))
		if _, err := io.ReadFull(r, payload); err != nil {
			return nil, err
		}
		var checksum [4]byte
		if _, err := io.ReadFull(r, checksum[:]); err != nil {
			return nil, err
		}
		hash := crc32.NewIEEE()
		hash.Write(header[4:])
		hash.Write(payload)
		if hash.Sum32() != binary.BigEndian.Uint32(checksum[:]) {
			return nil, fmt.Errorf("invalid PNG ICC checksum")
		}
		zero := bytes.IndexByte(payload, 0)
		if zero < 1 || zero > 79 || zero+2 > len(payload) || payload[zero+1] != 0 {
			return nil, fmt.Errorf("invalid PNG ICC compression header")
		}
		z, err := zlib.NewReader(bytes.NewReader(payload[zero+2:]))
		if err != nil {
			return nil, err
		}
		defer z.Close()
		profile, err := io.ReadAll(io.LimitReader(z, maxICCSize+1))
		if err != nil {
			return nil, err
		}
		if len(profile) > maxICCSize {
			return nil, fmt.Errorf("ICC profile exceeds size limit")
		}
		return profile, nil
	}
}

func readTIFFICC(f *os.File) ([]byte, error) {
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	r := &tiffXMPReader{file: f, size: uint64(info.Size()), visited: map[uint64]bool{}}
	h, err := r.readAt(0, 8)
	if err != nil {
		return nil, err
	}
	if string(h[:2]) == "II" {
		r.order = binary.LittleEndian
	} else {
		r.order = binary.BigEndian
	}
	offset := uint64(r.order.Uint32(h[4:]))
	switch r.order.Uint16(h[2:]) {
	case 42:
	case 43:
		if r.order.Uint16(h[4:]) != 8 || r.order.Uint16(h[6:]) != 0 {
			return nil, fmt.Errorf("invalid BigTIFF header")
		}
		h, err = r.readAt(8, 8)
		if err != nil {
			return nil, err
		}
		r.bigTIFF = true
		offset = r.order.Uint64(h)
	default:
		return nil, nil
	}
	queue := []tiffXMPDirectory{{offset: offset}}
	for len(queue) > 0 {
		d := queue[0]
		queue = queue[1:]
		if d.offset == 0 || r.visited[d.offset] {
			continue
		}
		if d.depth > 32 || len(r.visited) >= maxPreviewIFDs {
			return nil, fmt.Errorf("ICC TIFF directory limit exceeded")
		}
		r.visited[d.offset] = true
		entries, next, err := r.readDirectory(d.offset)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if e.tag == 34675 {
				if e.typeID != 7 && e.typeID != 1 {
					return nil, fmt.Errorf("invalid TIFF ICC field type")
				}
				return r.readEntryData(e, 1)
			}
			if e.tag == 330 {
				offsets, err := r.readDirectoryOffsets(e)
				if err != nil {
					return nil, err
				}
				for _, off := range offsets {
					queue = append(queue, tiffXMPDirectory{offset: off, depth: d.depth + 1})
				}
			}
		}
		queue = append(queue, tiffXMPDirectory{offset: next, depth: d.depth + 1})
		if len(queue) > maxPreviewQueue {
			return nil, fmt.Errorf("ICC TIFF directory queue limit exceeded")
		}
	}
	return nil, nil
}

func iccDescription(profile []byte) (string, error) {
	bad := func() (string, error) { return "", fmt.Errorf("invalid ICC profile description") }
	if len(profile) < 132 || len(profile) > maxICCSize || string(profile[36:40]) != "acsp" {
		return bad()
	}
	size := uint64(binary.BigEndian.Uint32(profile[:4]))
	if size < 132 || size > uint64(len(profile)) {
		return bad()
	}
	profile = profile[:size]
	count := uint64(binary.BigEndian.Uint32(profile[128:132]))
	if count > (size-132)/12 {
		return bad()
	}
	for i := uint64(0); i < count; i++ {
		tag := profile[132+i*12 : 144+i*12]
		if string(tag[:4]) != "desc" {
			continue
		}
		off, n := uint64(binary.BigEndian.Uint32(tag[4:8])), uint64(binary.BigEndian.Uint32(tag[8:12]))
		if off > size || n > size-off || n < 8 {
			return bad()
		}
		data := profile[off : off+n]
		var text string
		switch string(data[:4]) {
		case "desc":
			if len(data) < 12 {
				return bad()
			}
			n := uint64(binary.BigEndian.Uint32(data[8:12]))
			if n > uint64(len(data)-12) {
				return bad()
			}
			text = string(data[12 : 12+n])
			if end := strings.IndexByte(text, 0); end >= 0 {
				text = text[:end]
			}
		case "text":
			text = string(data[8:])
		case "mluc":
			if len(data) < 16 {
				return bad()
			}
			count, width := uint64(binary.BigEndian.Uint32(data[8:12])), uint64(binary.BigEndian.Uint32(data[12:16]))
			if width < 12 || count > uint64(len(data)-16)/width {
				return bad()
			}
			for j := uint64(0); j < count; j++ {
				rec := data[16+j*width : 28+j*width]
				n, off := uint64(binary.BigEndian.Uint32(rec[4:8])), uint64(binary.BigEndian.Uint32(rec[8:12]))
				if off > uint64(len(data)) || n > uint64(len(data))-off || n%2 != 0 {
					return bad()
				}
				// ExifTool's base ProfileDescription is the en-US record.
				if !strings.EqualFold(string(rec[:4]), "enUS") {
					continue
				}
				units := make([]uint16, n/2)
				for k := range units {
					units[k] = binary.BigEndian.Uint16(data[off+uint64(k)*2:])
				}
				text = string(utf16.Decode(units))
				break
			}
		default:
			return "", nil
		}
		text = strings.ReplaceAll(text, "\r\n", " ")
		return strings.TrimFunc(text, func(r rune) bool { return r <= 32 }), nil
	}
	return "", nil
}
