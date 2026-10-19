package lrtl

import (
	"encoding/binary"
	"fmt"
	"os"

	"github.com/icza/mjpeg"
)

type ratedMJPEGWriter struct {
	mjpeg.AviWriter
	path string
	rate frameRate
}

func newRatedMJPEGWriter(path string, width, height int32, rate frameRate) (renderFrameWriter, error) {
	// The dependency only exposes integer FPS. Finalize its own completed
	// temporary file's timing fields before the renderer publishes that file.
	fps := int32(24)
	if rate.denominator == 1 {
		fps = int32(rate.numerator)
	}
	writer, err := mjpeg.New(path, width, height, fps)
	if err != nil {
		return nil, err
	}
	return &ratedMJPEGWriter{AviWriter: writer, path: path, rate: rate}, nil
}

func (writer *ratedMJPEGWriter) Close() error {
	if err := writer.AviWriter.Close(); err != nil {
		return err
	}
	if writer.rate.denominator == 1 {
		return nil
	}
	return setGeneratedAVIRate(writer.path, writer.rate)
}

func setGeneratedAVIRate(path string, rate frameRate) error {
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	header := make([]byte, 164)
	if _, err := file.ReadAt(header, 0); err != nil {
		return fmt.Errorf("read generated AVI timing header: %w", err)
	}
	// This is the fixed header layout produced by our pinned MJPEG writer,
	// never a parser for arbitrary AVI input. Reject any unexpected layout.
	if string(header[:4]) != "RIFF" || string(header[8:12]) != "AVI " || string(header[24:28]) != "avih" || binary.LittleEndian.Uint32(header[28:32]) != 56 || string(header[100:104]) != "strh" || binary.LittleEndian.Uint32(header[104:108]) != 56 || string(header[108:116]) != "vidsMJPG" {
		return fmt.Errorf("unexpected generated AVI header; cannot set fractional fps")
	}
	microseconds := (uint64(rate.denominator)*1_000_000 + uint64(rate.numerator)/2) / uint64(rate.numerator)
	binary.LittleEndian.PutUint32(header[32:36], uint32(microseconds))
	binary.LittleEndian.PutUint32(header[128:132], rate.denominator)
	binary.LittleEndian.PutUint32(header[132:136], rate.numerator)
	if _, err := file.WriteAt(header, 0); err != nil {
		return err
	}
	return file.Sync()
}
