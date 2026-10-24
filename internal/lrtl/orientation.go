package lrtl

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"image"

	"github.com/evanoberholster/imagemeta"
)

func readEXIFOrientation(data []byte) (uint16, error) {
	if bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")) {
		payload, found, err := pngEXIFPayload(data)
		if err != nil {
			return 0, err
		}
		if !found {
			return 1, nil
		}
		orientation, err := pngTIFFOrientation(payload)
		if err != nil {
			return 0, err
		}
		return normalizeEXIFOrientation(orientation)
	}
	metadata, err := decodeImageMetadata(bytes.NewReader(data))
	if errors.Is(err, imagemeta.ErrNoExif) {
		return 1, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read EXIF orientation: %w", err)
	}
	return normalizeEXIFOrientation(uint16(metadata.Orientation))
}

func normalizeEXIFOrientation(orientation uint16) (uint16, error) {
	if orientation == 0 {
		return 1, nil
	}
	if orientation > 8 {
		return 0, fmt.Errorf("unsupported EXIF orientation value %d", orientation)
	}
	return orientation, nil
}

func pngTIFFOrientation(data []byte) (uint16, error) {
	if len(data) < 8 {
		return 0, fmt.Errorf("truncated PNG EXIF header")
	}
	var order binary.ByteOrder
	switch string(data[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 0, fmt.Errorf("invalid PNG EXIF byte order")
	}
	if order.Uint16(data[2:]) != 42 {
		return 0, fmt.Errorf("unsupported PNG EXIF TIFF header")
	}
	offset := uint64(order.Uint32(data[4:]))
	if offset == 0 {
		return 1, nil
	}
	if offset < 8 || offset+2 > uint64(len(data)) {
		return 0, fmt.Errorf("invalid PNG EXIF directory offset")
	}
	count := uint64(order.Uint16(data[offset:]))
	if offset+2+count*12+4 > uint64(len(data)) {
		return 0, fmt.Errorf("truncated PNG EXIF directory")
	}
	for i := uint64(0); i < count; i++ {
		entry := data[offset+2+i*12:]
		if order.Uint16(entry) != 274 {
			continue
		}
		if order.Uint16(entry[2:]) != 3 || order.Uint32(entry[4:]) != 1 {
			return 0, fmt.Errorf("invalid PNG EXIF orientation field")
		}
		return order.Uint16(entry[8:]), nil
	}
	return 1, nil
}

// PNG's eXIf chunk contains an ordinary TIFF header, including its byte order.
func pngEXIFPayload(data []byte) ([]byte, bool, error) {
	for offset := 8; offset < len(data); {
		if len(data)-offset < 12 {
			return nil, false, fmt.Errorf("truncated PNG metadata chunk")
		}
		length := uint64(binary.BigEndian.Uint32(data[offset:]))
		if length+12 > uint64(len(data)-offset) {
			return nil, false, fmt.Errorf("PNG metadata chunk exceeds input")
		}
		end := offset + 8 + int(length)
		kind := string(data[offset+4 : offset+8])
		if kind == "eXIf" {
			if crc32.ChecksumIEEE(data[offset+4:end]) != binary.BigEndian.Uint32(data[end:]) {
				return nil, false, fmt.Errorf("invalid PNG eXIf checksum")
			}
			return data[offset+8 : end], true, nil
		}
		if kind == "IEND" {
			return nil, false, nil
		}
		offset = end + 4
	}
	return nil, false, fmt.Errorf("truncated PNG metadata")
}

func orientedDimensions(width, height int, orientation uint16) (int, int) {
	if orientation >= 5 && orientation <= 8 {
		return height, width
	}
	return width, height
}

func applyEXIFOrientation(source image.Image, orientation uint16) image.Image {
	if orientation <= 1 || orientation > 8 {
		return source
	}
	bounds := source.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	outputWidth, outputHeight := orientedDimensions(width, height, orientation)
	result := newFrameImage(image.Rect(0, 0, outputWidth, outputHeight), source)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			var destinationX, destinationY int
			switch orientation {
			case 2: // Mirror horizontal.
				destinationX, destinationY = width-1-x, y
			case 3: // Rotate 180 degrees.
				destinationX, destinationY = width-1-x, height-1-y
			case 4: // Mirror vertical.
				destinationX, destinationY = x, height-1-y
			case 5: // Transpose across the top-left to bottom-right diagonal.
				destinationX, destinationY = y, x
			case 6: // Rotate 90 degrees clockwise.
				destinationX, destinationY = height-1-y, x
			case 7: // Transverse across the top-right to bottom-left diagonal.
				destinationX, destinationY = height-1-y, width-1-x
			case 8: // Rotate 270 degrees clockwise.
				destinationX, destinationY = y, width-1-x
			}
			result.Set(destinationX, destinationY, source.At(bounds.Min.X+x, bounds.Min.Y+y))
		}
	}
	return result
}
