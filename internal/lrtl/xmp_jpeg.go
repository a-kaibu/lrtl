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
	return readEmbeddedJPEGXMPFromReader(file)
}

func readEmbeddedJPEGXMPFromReader(reader io.Reader) ([]byte, bool, error) {
	var signature [2]byte
	if _, err := io.ReadFull(reader, signature[:]); err != nil {
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			return nil, false, nil
		}
		return nil, false, err
	}
	if signature != [2]byte{0xff, 0xd8} {
		return nil, false, nil
	}

	var standard []byte
	var standardPacket *XMPPacket
	var standardError error
	standardCount, standardBytes := 0, 0
	var extended *extendedXMP
	var extensionGUID string
	linked := false
	extendedBytes := 0
	for {
		marker, err := nextJPEGMarker(reader)
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
		if _, err := io.ReadFull(reader, lengthBytes[:]); err != nil {
			return nil, false, fmt.Errorf("read JPEG segment length: %w", err)
		}
		length := binary.BigEndian.Uint16(lengthBytes[:])
		if length < 2 {
			return nil, false, fmt.Errorf("invalid JPEG segment length %d", length)
		}
		payload := make([]byte, int(length)-2)
		if _, err := io.ReadFull(reader, payload); err != nil {
			return nil, false, fmt.Errorf("read JPEG segment: %w", err)
		}
		if marker != 0xe1 {
			continue
		}
		if bytes.HasPrefix(payload, []byte(jpegXMPHeader)) {
			data := payload[len(jpegXMPHeader):]
			standardBytes += len(data)
			if standardBytes > maxEmbeddedXMPSize {
				return nil, false, fmt.Errorf("standard JPEG XMP packets exceed safety limit")
			}
			packet, err := parseXMP(data)
			if err != nil {
				standardError = err
				continue
			}
			// The native extractor keeps the first link it encounters, even if
			// a later standard packet replaces that property in the final XML.
			if !linked {
				extensionGUID, linked, err = packet.GetRootScalar("xmpNote:HasExtendedXMP")
				if err != nil {
					return nil, false, fmt.Errorf("read extended JPEG XMP link: %w", err)
				}
			}
			standardCount++
			if standardPacket == nil {
				standardPacket = packet
				standard = append([]byte(nil), data...)
			} else if err := mergeEmbeddedXMP(standardPacket, packet); err != nil {
				return nil, false, err
			}
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
		extendedBytes += len(chunk)
		if extendedBytes > maxEmbeddedXMPSize {
			return nil, false, fmt.Errorf("extended JPEG XMP chunks exceed safety limit")
		}
		if size == 0 || size > maxEmbeddedXMPSize || offset > size || uint64(len(chunk)) > uint64(size-offset) {
			return nil, false, fmt.Errorf("invalid extended JPEG XMP chunk bounds")
		}
		// Matching is case sensitive and begins only after the first link.
		if !linked || string(guidBytes) != extensionGUID {
			continue
		}
		if extended == nil {
			extended = &extendedXMP{size: size}
		} else if extended.size != size {
			return nil, false, fmt.Errorf("inconsistent extended JPEG XMP packet size")
		}
		extended.chunks = append(extended.chunks, extendedXMPChunk{offset: offset, data: append([]byte(nil), chunk...)})
	}
	if standardPacket == nil {
		if standardError != nil {
			return nil, false, fmt.Errorf("parse embedded JPEG XMP: %w", standardError)
		}
		return nil, false, nil
	}
	if standardCount > 1 {
		var err error
		standard, err = standardPacket.Serialize()
		if err != nil {
			return nil, false, err
		}
	}
	if linked {
		if len(extensionGUID) != 32 {
			return nil, false, fmt.Errorf("invalid xmpNote:HasExtendedXMP GUID")
		}
		if _, err := hex.DecodeString(extensionGUID); err != nil {
			return nil, false, fmt.Errorf("invalid xmpNote:HasExtendedXMP GUID")
		}
		if extended == nil {
			return nil, false, fmt.Errorf("embedded JPEG XMP references a missing extended packet")
		}
		data, err := assembleExtendedXMP(extensionGUID, extended)
		if err != nil {
			return nil, false, err
		}
		packet, err := parseXMP(data)
		if err != nil {
			return nil, false, err
		}
		if err := mergeEmbeddedXMP(standardPacket, packet); err != nil {
			return nil, false, err
		}
		data, err = standardPacket.Serialize()
		return data, err == nil, err
	}
	return standard, true, nil
}

// Native embedded-directory import replaces whole nonempty root properties in
// encounter order. Empty incoming roots leave earlier values intact; nested
// empty fields/items are removed while copying a replacement.
func mergeEmbeddedXMP(destination, source *XMPPacket) error {
	src, err := pastePacketNodes(source)
	if err != nil {
		return err
	}
	dst, err := pastePacketNodes(destination)
	if err != nil {
		return err
	}
	before, after := map[pasteName]*pasteNode{}, map[pasteName]*pasteNode{}
	for _, n := range dst {
		before[n.Name], after[n.Name] = n, n
	}
	for _, n := range src {
		if pasteNodeEmpty(n) {
			continue
		}
		if copy := clonePasteForAppend(n); copy != nil {
			after[n.Name] = copy
		} else {
			delete(after, n.Name)
		}
	}
	_, err = writeChangedPasteRoots(destination, before, after)
	return err
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
		if chunk.offset < expected {
			// Already assembled identical bytes may be retransmitted.
			// Conflicting overlaps still fail before any sidecar is written.
			end := uint64(chunk.offset) + uint64(len(chunk.data))
			if end <= uint64(expected) && bytes.Equal(data[int(chunk.offset):int(end)], chunk.data) {
				continue
			}
		}
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
	if !strings.EqualFold(hex.EncodeToString(digest[:]), guid) {
		return nil, fmt.Errorf("embedded JPEG extended XMP GUID does not match its packet")
	}
	if _, err := parseXMP(data); err != nil {
		return nil, fmt.Errorf("parse extended JPEG XMP: %w", err)
	}
	return data, nil
}
