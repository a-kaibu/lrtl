package lrtl

import (
	"encoding/binary"
	"io"
	"os"
)

func readEmbeddedRAFXMP(path string) ([]byte, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil, false, err
	}
	const rafHeaderSize = 92
	if info.Size() < rafHeaderSize {
		return nil, false, nil
	}
	var header [rafHeaderSize]byte
	if _, err := file.ReadAt(header[:], 0); err != nil {
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			return nil, false, nil
		}
		return nil, false, err
	}
	if string(header[:16]) != "FUJIFILMCCD-RAW " {
		return nil, false, nil
	}
	offset := uint64(binary.BigEndian.Uint32(header[84:88]))
	length := uint64(binary.BigEndian.Uint32(header[88:92]))
	if length == 0 || length > maxPreviewBytes || !tiffRange(offset, length, uint64(info.Size())) {
		return nil, false, nil
	}
	reader := io.NewSectionReader(file, int64(offset), int64(length))
	return readEmbeddedJPEGXMPFromReader(reader)
}
