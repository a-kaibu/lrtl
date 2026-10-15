package lrtl

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

const cr3XMPScanChunk = 1 << 20

var isoBaseMediaXMPUUID = []byte{
	0xbe, 0x7a, 0xcf, 0xcb, 0x97, 0xa9, 0x42, 0xe8,
	0x9c, 0x71, 0x99, 0x94, 0x91, 0xe3, 0xaf, 0xac,
}

// readEmbeddedCR3XMP scans ISO Base Media File Format UUID boxes for Adobe's
// XMP UUID. The file is scanned in bounded chunks; only the XMP packet itself
// is retained in memory, and callers continue to write changes to sidecars.
func readEmbeddedCR3XMP(path string) ([]byte, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, false, err
	}
	if info.Size() <= 0 {
		return nil, false, nil
	}

	fileSize := uint64(info.Size())
	for offset := uint64(0); offset < fileSize; {
		chunkLength := uint64(cr3XMPScanChunk)
		if remaining := fileSize - offset; chunkLength > remaining {
			chunkLength = remaining
		}
		readLength := chunkLength
		if remaining := fileSize - offset - chunkLength; remaining > 0 {
			readLength += uint64(len(isoBaseMediaXMPUUID) - 1)
			if readLength > fileSize-offset {
				readLength = fileSize - offset
			}
		}
		buffer := make([]byte, int(readLength))
		read, readErr := file.ReadAt(buffer, int64(offset))
		if readErr != nil && readErr != io.EOF {
			return nil, false, readErr
		}
		buffer = buffer[:read]
		newBytes := int(chunkLength)
		if newBytes > len(buffer) {
			newBytes = len(buffer)
		}
		for cursor := 0; cursor < newBytes; {
			relative := bytes.Index(buffer[cursor:], isoBaseMediaXMPUUID)
			if relative < 0 {
				break
			}
			match := cursor + relative
			if match >= newBytes {
				break
			}
			packet, found, packetErr := readCR3XMPBoxAt(file, fileSize, offset+uint64(match))
			if packetErr != nil {
				return nil, false, packetErr
			}
			if found {
				return packet, true, nil
			}
			cursor = match + 1
		}
		offset += chunkLength
	}
	return nil, false, nil
}

func readCR3XMPBoxAt(file *os.File, fileSize, uuidOffset uint64) ([]byte, bool, error) {
	var boxStart, boxSize uint64
	var header [16]byte

	if uuidOffset >= 16 {
		if _, err := file.ReadAt(header[:], int64(uuidOffset-16)); err == nil &&
			binary.BigEndian.Uint32(header[:4]) == 1 && string(header[4:8]) == "uuid" {
			boxStart = uuidOffset - 16
			boxSize = binary.BigEndian.Uint64(header[8:16])
		}
	}
	if boxSize == 0 {
		if uuidOffset < 8 {
			return nil, false, nil
		}
		if _, err := file.ReadAt(header[:8], int64(uuidOffset-8)); err != nil || string(header[4:8]) != "uuid" {
			return nil, false, nil
		}
		boxStart = uuidOffset - 8
		size32 := binary.BigEndian.Uint32(header[:4])
		switch size32 {
		case 0:
			boxSize = fileSize - boxStart
		case 1:
			return nil, false, nil // The extended-size header was not complete.
		default:
			boxSize = uint64(size32)
		}
	}

	payloadOffset := uuidOffset + uint64(len(isoBaseMediaXMPUUID))
	if payloadOffset < uuidOffset || boxStart > payloadOffset || boxSize < payloadOffset-boxStart ||
		boxStart > fileSize || boxSize > fileSize-boxStart {
		return nil, false, fmt.Errorf("invalid CR3 XMP UUID box bounds")
	}
	payloadSize := boxSize - (payloadOffset - boxStart)
	if payloadSize == 0 || payloadSize > maxEmbeddedXMPSize {
		return nil, false, fmt.Errorf("CR3 XMP packet has an unsupported size")
	}
	packet := make([]byte, int(payloadSize))
	read, err := file.ReadAt(packet, int64(payloadOffset))
	if err != nil && err != io.EOF {
		return nil, false, fmt.Errorf("read CR3 XMP packet: %w", err)
	}
	if uint64(read) != payloadSize {
		return nil, false, fmt.Errorf("read CR3 XMP packet: %w", io.ErrUnexpectedEOF)
	}
	packet = bytes.TrimRight(packet, "\x00")
	if _, err := parseXMP(packet); err != nil {
		return nil, false, fmt.Errorf("parse embedded CR3 XMP: %w", err)
	}
	return packet, true, nil
}
