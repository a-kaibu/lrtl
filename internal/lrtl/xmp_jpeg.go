package lrtl

import (
	"bytes"
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	jpegXMPHeader         = "http://ns.adobe.com/xap/1.0/\x00"
	jpegExtendedXMPHeader = "http://ns.adobe.com/xmp/extension/\x00"
	maxEmbeddedXMPSize    = 32 << 20
)

type extendedXMPChunk struct {
	offset uint32
	data   []byte
}

type extendedXMP struct {
	size   uint32
	chunks []extendedXMPChunk
}

// readEmbeddedJPEGXMP reads Adobe's standard or extended XMP APP1 packet.
// It never modifies the JPEG; callers write edits to an external sidecar.
func readEmbeddedJPEGXMP(path string) ([]byte, bool, error) {
	if ext := strings.ToLower(filepath.Ext(path)); ext != ".jpg" && ext != ".jpeg" {
		return nil, false, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer file.Close()
	var signature [2]byte
	if _, err := io.ReadFull(file, signature[:]); err != nil {
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			return nil, false, nil
		}
		return nil, false, err
	}
	if signature != [2]byte{0xff, 0xd8} {
		return nil, false, nil
	}

	var standard []byte
	extended := make(map[string]*extendedXMP)
	for {
		marker, err := nextJPEGMarker(file)
		if err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				break
			}
			return nil, false, err
		}
		if marker == 0xda || marker == 0xd9 { // Start of scan or end of image.
			break
		}
		if marker == 0x01 || marker >= 0xd0 && marker <= 0xd7 {
			continue // Standalone markers have no length field.
		}
		var lengthBytes [2]byte
		if _, err := io.ReadFull(file, lengthBytes[:]); err != nil {
			return nil, false, fmt.Errorf("read JPEG segment length: %w", err)
		}
		length := binary.BigEndian.Uint16(lengthBytes[:])
		if length < 2 {
			return nil, false, fmt.Errorf("invalid JPEG segment length %d", length)
		}
		payload := make([]byte, int(length)-2)
		if _, err := io.ReadFull(file, payload); err != nil {
			return nil, false, fmt.Errorf("read JPEG segment: %w", err)
		}
		if marker != 0xe1 {
			continue
		}
		if bytes.HasPrefix(payload, []byte(jpegXMPHeader)) {
			standard = append(standard[:0], payload[len(jpegXMPHeader):]...)
			continue
		}
		if !bytes.HasPrefix(payload, []byte(jpegExtendedXMPHeader)) {
			continue
		}
		headerLength := len(jpegExtendedXMPHeader)
		if len(payload) < headerLength+40 {
			return nil, false, fmt.Errorf("truncated extended JPEG XMP header")
		}
		guidBytes := payload[headerLength : headerLength+32]
		if _, err := hex.DecodeString(string(guidBytes)); err != nil {
			return nil, false, fmt.Errorf("invalid extended JPEG XMP GUID")
		}
		size := binary.BigEndian.Uint32(payload[headerLength+32 : headerLength+36])
		offset := binary.BigEndian.Uint32(payload[headerLength+36 : headerLength+40])
		chunk := payload[headerLength+40:]
		if size == 0 || size > maxEmbeddedXMPSize || offset > size || uint64(len(chunk)) > uint64(size-offset) {
			return nil, false, fmt.Errorf("invalid extended JPEG XMP chunk bounds")
		}
		guid := strings.ToUpper(string(guidBytes))
		packet := extended[guid]
		if packet == nil {
			packet = &extendedXMP{size: size}
			extended[guid] = packet
		} else if packet.size != size {
			return nil, false, fmt.Errorf("inconsistent extended JPEG XMP packet size")
		}
		packet.chunks = append(packet.chunks, extendedXMPChunk{offset: offset, data: append([]byte(nil), chunk...)})
	}
	if len(standard) == 0 {
		return nil, false, nil
	}
	standardPacket, err := parseXMP(standard)
	if err != nil {
		return nil, false, fmt.Errorf("parse embedded JPEG XMP: %w", err)
	}
	guid, linked, err := standardPacket.GetScalar("xmpNote:HasExtendedXMP")
	if err != nil {
		return nil, false, fmt.Errorf("read extended JPEG XMP link: %w", err)
	}
	if linked {
		guid = strings.ToUpper(strings.TrimSpace(guid))
		if len(guid) != 32 {
			return nil, false, fmt.Errorf("invalid xmpNote:HasExtendedXMP GUID")
		}
		if _, err := hex.DecodeString(guid); err != nil {
			return nil, false, fmt.Errorf("invalid xmpNote:HasExtendedXMP GUID")
		}
		packet := extended[guid]
		if packet == nil {
			return nil, false, fmt.Errorf("embedded JPEG XMP references a missing extended packet")
		}
		data, err := assembleExtendedXMP(guid, packet)
		if err != nil {
			return nil, false, err
		}
		return data, true, nil
	}
	return standard, true, nil
}

func nextJPEGMarker(reader io.Reader) (byte, error) {
	var current [1]byte
	for {
		if _, err := io.ReadFull(reader, current[:]); err != nil {
			return 0, err
		}
		if current[0] != 0xff {
			return 0, fmt.Errorf("invalid marker prefix in JPEG header")
		}
		for {
			if _, err := io.ReadFull(reader, current[:]); err != nil {
				return 0, err
			}
			if current[0] != 0xff {
				break
			}
		}
		if current[0] != 0x00 { // 0xff00 is byte stuffing, not a header marker.
			return current[0], nil
		}
	}
}

func assembleExtendedXMP(guid string, packet *extendedXMP) ([]byte, error) {
	chunks := append([]extendedXMPChunk(nil), packet.chunks...)
	sort.Slice(chunks, func(i, j int) bool { return chunks[i].offset < chunks[j].offset })
	data := make([]byte, int(packet.size))
	var expected uint32
	for _, chunk := range chunks {
		if chunk.offset != expected {
			return nil, fmt.Errorf("embedded JPEG extended XMP packet has a gap or overlapping chunks")
		}
		copy(data[int(chunk.offset):], chunk.data)
		expected += uint32(len(chunk.data))
	}
	if expected != packet.size {
		return nil, fmt.Errorf("embedded JPEG extended XMP packet is incomplete")
	}
	digest := md5.Sum(data)
	if strings.ToUpper(hex.EncodeToString(digest[:])) != guid {
		return nil, fmt.Errorf("embedded JPEG extended XMP GUID does not match its packet")
	}
	if _, err := parseXMP(data); err != nil {
		return nil, fmt.Errorf("parse extended JPEG XMP: %w", err)
	}
	return data, nil
}
