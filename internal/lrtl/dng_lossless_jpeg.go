package lrtl

import (
	"encoding/binary"
	"fmt"
)

// decodeDNGLosslessJPEG decodes the single-component Huffman lossless JPEG
// variant used by DNG semantic masks. The output uses the TIFF byte order.
func decodeDNGLosslessJPEG(encoded, tables []byte, width, height, depth int, order binary.ByteOrder) ([]byte, error) {
	if width < 1 || height < 1 || (depth != 8 && depth != 16) || uint64(width)*uint64(height)*uint64(depth/8) > maxDNGSemanticMaskBytes {
		return nil, fmt.Errorf("invalid lossless JPEG mask dimensions or precision")
	}
	stream, err := joinDNGJPEGTables(encoded, tables)
	if err != nil {
		return nil, err
	}
	decoder := dngLosslessJPEGDecoder{data: stream}
	return decoder.decode(width, height, depth, order)
}

// TIFF/EP stores JPEG tables separately from tile/strip data. Normalize the
// optional SOI/EOI wrappers and place those table segments before the image.
func joinDNGJPEGTables(encoded, tables []byte) ([]byte, error) {
	strip := func(data []byte, label string, requireSOI bool) ([]byte, error) {
		if len(data) >= 2 && data[0] == 0xff && data[1] == 0xd8 {
			data = data[2:]
		} else if requireSOI {
			return nil, fmt.Errorf("%s is missing JPEG SOI", label)
		}
		if len(data) >= 2 && data[len(data)-2] == 0xff && data[len(data)-1] == 0xd9 {
			data = data[:len(data)-2]
		}
		return data, nil
	}
	var body []byte
	if len(tables) != 0 {
		var err error
		body, err = strip(tables, "DNG JPEGTables", true)
		if err != nil {
			return nil, err
		}
	}
	imageBody, err := strip(encoded, "DNG JPEG block", len(tables) == 0)
	if err != nil {
		return nil, err
	}
	stream := make([]byte, 0, 2+len(body)+len(imageBody)+2)
	stream = append(stream, 0xff, 0xd8)
	stream = append(stream, body...)
	stream = append(stream, imageBody...)
	stream = append(stream, 0xff, 0xd9)
	return stream, nil
}

type dngLosslessHuffman struct {
	minCode [17]int
	maxCode [17]int
	valueAt [17]int
	symbols []byte
}

func parseDNGLosslessHuffman(data []byte) (*dngLosslessHuffman, error) {
	if len(data) < 17 {
		return nil, fmt.Errorf("truncated lossless JPEG Huffman table")
	}
	total := 0
	for _, count := range data[:16] {
		total += int(count)
	}
	if total == 0 || total > 256 || len(data) != 16+total {
		return nil, fmt.Errorf("invalid lossless JPEG Huffman table length")
	}
	table := &dngLosslessHuffman{symbols: append([]byte(nil), data[16:]...)}
	for size := range table.minCode {
		table.minCode[size], table.maxCode[size] = -1, -1
	}
	code, index := 0, 0
	for size := 1; size <= 16; size++ {
		count := int(data[size-1])
		if code+count > 1<<uint(size) {
			return nil, fmt.Errorf("oversubscribed lossless JPEG Huffman table")
		}
		if count != 0 {
			if code+count == 1<<uint(size) {
				return nil, fmt.Errorf("lossless JPEG Huffman table uses an all-ones code")
			}
			table.minCode[size] = code
			table.maxCode[size] = code + count - 1
			table.valueAt[size] = index - code
			index += count
			code += count
		}
		code <<= 1
	}
	return table, nil
}

type dngLosslessEntropy struct {
	data      []byte
	position  int
	current   byte
	remaining uint8
}

func (reader *dngLosslessEntropy) readBit() (uint8, error) {
	if reader.remaining == 0 {
		if reader.position >= len(reader.data) {
			return 0, fmt.Errorf("truncated lossless JPEG entropy data")
		}
		value := reader.data[reader.position]
		reader.position++
		if value == 0xff {
			if reader.position >= len(reader.data) || reader.data[reader.position] != 0x00 {
				return 0, fmt.Errorf("unexpected marker in lossless JPEG entropy data")
			}
			reader.position++
		}
		reader.current, reader.remaining = value, 8
	}
	reader.remaining--
	return (reader.current >> reader.remaining) & 1, nil
}

func (reader *dngLosslessEntropy) readBits(count int) (int, error) {
	value := 0
	for range count {
		bit, err := reader.readBit()
		if err != nil {
			return 0, err
		}
		value = value<<1 | int(bit)
	}
	return value, nil
}

func (reader *dngLosslessEntropy) marker() (byte, error) {
	reader.remaining = 0 // Ignore the entropy byte's fill bits.
	if reader.position >= len(reader.data) || reader.data[reader.position] != 0xff {
		return 0, fmt.Errorf("lossless JPEG marker is missing")
	}
	for reader.position < len(reader.data) && reader.data[reader.position] == 0xff {
		reader.position++
	}
	if reader.position >= len(reader.data) {
		return 0, fmt.Errorf("truncated lossless JPEG marker")
	}
	marker := reader.data[reader.position]
	reader.position++
	if marker == 0x00 {
		return 0, fmt.Errorf("unexpected stuffed byte where lossless JPEG marker was expected")
	}
	return marker, nil
}

type dngLosslessJPEGDecoder struct {
	data      []byte
	position  int
	precision int
	component byte
	tables    [4]*dngLosslessHuffman
	restart   int
}

func (decoder *dngLosslessJPEGDecoder) decode(expectedWidth, expectedHeight, expectedDepth int, order binary.ByteOrder) ([]byte, error) {
	if len(decoder.data) < 4 || decoder.data[0] != 0xff || decoder.data[1] != 0xd8 {
		return nil, fmt.Errorf("invalid lossless JPEG SOI")
	}
	decoder.position = 2
	var width, height int
	for decoder.position < len(decoder.data) {
		marker, err := decoder.nextMarker()
		if err != nil {
			return nil, err
		}
		switch marker {
		case 0xc3:
			if width != 0 {
				return nil, fmt.Errorf("multiple lossless JPEG frames are unsupported")
			}
			width, height, err = decoder.parseFrame()
		case 0xc4:
			err = decoder.parseHuffmanTables()
		case 0xdd:
			err = decoder.parseRestartInterval()
		case 0xda:
			if width == 0 {
				return nil, fmt.Errorf("lossless JPEG scan precedes frame header")
			}
			if width != expectedWidth || height != expectedHeight || decoder.precision != expectedDepth {
				return nil, fmt.Errorf("lossless JPEG frame %dx%d %d-bit does not match TIFF mask %dx%d %d-bit", width, height, decoder.precision, expectedWidth, expectedHeight, expectedDepth)
			}
			return decoder.decodeScan(expectedWidth, expectedHeight, expectedDepth, order)
		case 0xd9:
			return nil, fmt.Errorf("lossless JPEG ended before scan data")
		default:
			if marker >= 0xd0 && marker <= 0xd7 || marker == 0x01 || marker == 0xd8 {
				return nil, fmt.Errorf("unexpected lossless JPEG marker 0x%02x", marker)
			}
			_, err = decoder.segment()
		}
		if err != nil {
			return nil, err
		}
	}
	return nil, fmt.Errorf("lossless JPEG scan is missing")
}

func (decoder *dngLosslessJPEGDecoder) nextMarker() (byte, error) {
	if decoder.position >= len(decoder.data) || decoder.data[decoder.position] != 0xff {
		return 0, fmt.Errorf("lossless JPEG marker is missing")
	}
	for decoder.position < len(decoder.data) && decoder.data[decoder.position] == 0xff {
		decoder.position++
	}
	if decoder.position >= len(decoder.data) {
		return 0, fmt.Errorf("truncated lossless JPEG marker")
	}
	marker := decoder.data[decoder.position]
	decoder.position++
	if marker == 0x00 {
		return 0, fmt.Errorf("unexpected stuffed byte outside lossless JPEG entropy data")
	}
	return marker, nil
}

func (decoder *dngLosslessJPEGDecoder) segment() ([]byte, error) {
	if decoder.position+2 > len(decoder.data) {
		return nil, fmt.Errorf("truncated lossless JPEG segment length")
	}
	length := int(decoder.data[decoder.position])<<8 | int(decoder.data[decoder.position+1])
	decoder.position += 2
	if length < 2 || length-2 > len(decoder.data)-decoder.position {
		return nil, fmt.Errorf("invalid lossless JPEG segment length")
	}
	segment := decoder.data[decoder.position : decoder.position+length-2]
	decoder.position += length - 2
	return segment, nil
}

func (decoder *dngLosslessJPEGDecoder) parseFrame() (int, int, error) {
	segment, err := decoder.segment()
	if err != nil {
		return 0, 0, err
	}
	if len(segment) != 9 || segment[5] != 1 {
		return 0, 0, fmt.Errorf("lossless JPEG masks require one component")
	}
	precision := int(segment[0])
	height := int(segment[1])<<8 | int(segment[2])
	width := int(segment[3])<<8 | int(segment[4])
	if precision < 2 || precision > 16 || width == 0 || height == 0 || segment[7] != 0x11 {
		return 0, 0, fmt.Errorf("unsupported lossless JPEG frame parameters")
	}
	decoder.precision, decoder.component = precision, segment[6]
	return width, height, nil
}

func (decoder *dngLosslessJPEGDecoder) parseHuffmanTables() error {
	segment, err := decoder.segment()
	if err != nil {
		return err
	}
	for offset := 0; offset < len(segment); {
		if len(segment)-offset < 17 {
			return fmt.Errorf("truncated lossless JPEG Huffman table")
		}
		selector := segment[offset]
		offset++
		class, identifier := selector>>4, selector&0x0f
		if class != 0 || identifier >= 4 {
			return fmt.Errorf("unsupported lossless JPEG Huffman table selector 0x%02x", selector)
		}
		total := 0
		for _, count := range segment[offset : offset+16] {
			total += int(count)
		}
		if total > len(segment)-offset-16 {
			return fmt.Errorf("truncated lossless JPEG Huffman symbols")
		}
		table, err := parseDNGLosslessHuffman(segment[offset : offset+16+total])
		if err != nil {
			return err
		}
		for _, symbol := range table.symbols {
			if symbol > 16 {
				return fmt.Errorf("invalid lossless JPEG difference category %d", symbol)
			}
		}
		decoder.tables[identifier] = table
		offset += 16 + total
	}
	if len(segment) == 0 {
		return fmt.Errorf("empty lossless JPEG Huffman table segment")
	}
	return nil
}

func (decoder *dngLosslessJPEGDecoder) parseRestartInterval() error {
	segment, err := decoder.segment()
	if err != nil {
		return err
	}
	if len(segment) != 2 {
		return fmt.Errorf("invalid lossless JPEG restart interval")
	}
	decoder.restart = int(segment[0])<<8 | int(segment[1])
	return nil
}

func (decoder *dngLosslessJPEGDecoder) decodeScan(width, height, depth int, order binary.ByteOrder) ([]byte, error) {
	segment, err := decoder.segment()
	if err != nil {
		return nil, err
	}
	if len(segment) != 6 || segment[0] != 1 || segment[1] != decoder.component || segment[2]>>4 >= 4 || segment[2]&0x0f != 0 || segment[4] != 0 {
		return nil, fmt.Errorf("unsupported lossless JPEG scan parameters")
	}
	predictor := int(segment[3])
	pointTransform := int(segment[5] & 0x0f)
	if predictor < 1 || predictor > 7 || segment[5]>>4 != 0 || pointTransform >= decoder.precision {
		return nil, fmt.Errorf("invalid lossless JPEG predictor or point transform")
	}
	table := decoder.tables[segment[2]>>4]
	if table == nil {
		return nil, fmt.Errorf("lossless JPEG DC Huffman table %d is missing", segment[2]>>4)
	}
	if uint64(width)*uint64(height)*uint64(depth/8) > maxDNGSemanticMaskBytes {
		return nil, fmt.Errorf("lossless JPEG mask output exceeds limits")
	}
	if decoder.restart != 0 && decoder.restart%width != 0 {
		return nil, fmt.Errorf("lossless JPEG restart interval must align to mask rows")
	}
	sampleCount := width * height
	samples := make([]uint32, sampleCount)
	entropy := dngLosslessEntropy{data: decoder.data, position: decoder.position}
	restartSequence, sinceRestart := byte(0), 0
	firstAfterRestart := false
	restartFirstRowEnd := 0
	initial := 1 << uint(decoder.precision-pointTransform-1)
	modulus := 1 << uint(decoder.precision-pointTransform)
	for index := 0; index < sampleCount; index++ {
		if decoder.restart != 0 && sinceRestart == decoder.restart {
			marker, err := entropy.marker()
			if err != nil {
				return nil, err
			}
			if marker != 0xd0+restartSequence {
				return nil, fmt.Errorf("lossless JPEG restart marker 0x%02x, want RST%d", marker, restartSequence)
			}
			restartSequence = (restartSequence + 1) & 7
			sinceRestart = 0
			firstAfterRestart = true
			restartFirstRowEnd = index + width
		}
		category, err := table.decode(&entropy)
		if err != nil {
			return nil, fmt.Errorf("decode lossless JPEG difference: %w", err)
		}
		if int(category) > decoder.precision-pointTransform {
			return nil, fmt.Errorf("invalid lossless JPEG difference category %d", category)
		}
		difference := 0
		if category == 16 {
			difference = -32768
		} else if category != 0 {
			bits, err := entropy.readBits(int(category))
			if err != nil {
				return nil, err
			}
			difference = bits
			if bits < 1<<uint(category-1) {
				difference -= (1 << uint(category)) - 1
			}
		}
		x, y := index%width, index/width
		prediction := initial
		if index != 0 && !firstAfterRestart {
			ra, rb, rc := initial, initial, initial
			if x > 0 {
				ra = int(samples[index-1] >> uint(pointTransform))
			}
			if y > 0 {
				rb = int(samples[index-width] >> uint(pointTransform))
			}
			if x > 0 && y > 0 {
				rc = int(samples[index-width-1] >> uint(pointTransform))
			}
			if index < restartFirstRowEnd || y == 0 {
				prediction = ra
			} else if x == 0 {
				prediction = rb
			} else {
				prediction = dngLosslessPredictor(predictor, ra, rb, rc)
			}
		}
		reconstructed := (prediction + difference) % modulus
		if reconstructed < 0 {
			reconstructed += modulus
		}
		samples[index] = uint32(reconstructed << uint(pointTransform))
		sinceRestart++
		firstAfterRestart = false
	}
	marker, err := entropy.marker()
	if err != nil {
		return nil, err
	}
	if marker != 0xd9 {
		return nil, fmt.Errorf("lossless JPEG ended with marker 0x%02x, want EOI", marker)
	}
	bytesPerSample := depth / 8
	raw := make([]byte, sampleCount*bytesPerSample)
	for index, sample := range samples {
		if depth == 8 {
			raw[index] = byte(sample)
		} else {
			order.PutUint16(raw[index*2:], uint16(sample))
		}
	}
	return raw, nil
}

func (table *dngLosslessHuffman) decode(entropy *dngLosslessEntropy) (byte, error) {
	code := 0
	for size := 1; size <= 16; size++ {
		bit, err := entropy.readBit()
		if err != nil {
			return 0, err
		}
		code = code<<1 | int(bit)
		if table.maxCode[size] >= 0 && code >= table.minCode[size] && code <= table.maxCode[size] {
			index := table.valueAt[size] + code
			if index < 0 || index >= len(table.symbols) {
				return 0, fmt.Errorf("invalid lossless JPEG Huffman code")
			}
			return table.symbols[index], nil
		}
	}
	return 0, fmt.Errorf("invalid lossless JPEG Huffman code")
}

func dngLosslessPredictor(selector, ra, rb, rc int) int {
	switch selector {
	case 1:
		return ra
	case 2:
		return rb
	case 3:
		return rc
	case 4:
		return ra + rb - rc
	case 5:
		return ra + ((rb - rc) >> 1)
	case 6:
		return rb + ((ra - rc) >> 1)
	default:
		return (ra + rb) >> 1
	}
}
