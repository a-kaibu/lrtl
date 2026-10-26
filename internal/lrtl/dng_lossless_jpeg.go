package lrtl

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image/jpeg"
)

func decodeDNGJPEGBlock(encoded, tables []byte, width, height, components, depth int, order binary.ByteOrder) ([]byte, error) {
	if _, ok := dngLosslessJPEGOutputSize(width, height, components, depth, maxDevelopedRAWBytes); !ok || (components != 1 && components != 3) {
		return nil, fmt.Errorf("DNG JPEG image dimensions or channel count exceed limits")
	}
	expectedSamples := uint64(width) * uint64(height) * uint64(components)
	stream, err := joinDNGJPEGTables(encoded, tables)
	if err != nil {
		return nil, err
	}
	marker, precision, frameWidth, frameHeight, frameComponents, err := inspectDNGJPEGFrame(stream)
	if err != nil {
		return nil, err
	}
	if marker == 0xc3 {
		frameSamples := uint64(frameWidth) * uint64(frameHeight) * uint64(frameComponents)
		if precision != depth || frameSamples != expectedSamples {
			return nil, fmt.Errorf("lossless JPEG frame %dx%d %d-bit/%d-channel does not contain the TIFF image's %d samples", frameWidth, frameHeight, precision, frameComponents, expectedSamples)
		}
		outputBytes, ok := dngLosslessJPEGOutputSize(frameWidth, frameHeight, frameComponents, depth, maxDevelopedRAWBytes)
		if !ok {
			return nil, fmt.Errorf("lossless JPEG frame output exceeds limits")
		}
		decoder := dngLosslessJPEGDecoder{data: stream}
		raw, err := decoder.decodeWithOutputLimit(frameWidth, frameHeight, frameComponents, depth, order, maxDevelopedRAWBytes)
		if err != nil {
			return nil, err
		}
		if len(raw) != outputBytes {
			return nil, fmt.Errorf("lossless JPEG decoded sample count does not match the TIFF image")
		}
		return raw, nil
	}
	if frameWidth != width || frameHeight != height || frameComponents != components || precision != depth {
		return nil, fmt.Errorf("JPEG frame %dx%d %d-bit/%d-channel does not match TIFF image %dx%d %d-bit/%d-channel", frameWidth, frameHeight, precision, frameComponents, width, height, depth, components)
	}
	if marker != 0xc0 && marker != 0xc1 && marker != 0xc2 {
		return nil, fmt.Errorf("unsupported DNG JPEG frame marker 0x%02x", marker)
	}
	if depth != 8 {
		return nil, fmt.Errorf("lossy DNG JPEG requires 8-bit samples")
	}
	config, err := jpeg.DecodeConfig(bytes.NewReader(stream))
	if err != nil {
		return nil, fmt.Errorf("read lossy DNG JPEG header: %w", err)
	}
	if config.Width != width || config.Height != height {
		return nil, fmt.Errorf("lossy DNG JPEG dimensions do not match the TIFF image")
	}
	decoded, err := jpeg.Decode(bytes.NewReader(stream))
	if err != nil {
		return nil, fmt.Errorf("decode lossy DNG JPEG: %w", err)
	}
	if decoded.Bounds().Dx() != width || decoded.Bounds().Dy() != height {
		return nil, fmt.Errorf("decoded lossy DNG JPEG dimensions do not match the TIFF image")
	}
	raw := make([]byte, width*height*components)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			red, green, blue, _ := decoded.At(decoded.Bounds().Min.X+x, decoded.Bounds().Min.Y+y).RGBA()
			position := (y*width + x) * components
			raw[position] = byte(red >> 8)
			if components == 3 {
				raw[position+1], raw[position+2] = byte(green>>8), byte(blue>>8)
			}
		}
	}
	return raw, nil
}

func inspectDNGJPEGFrame(data []byte) (marker byte, precision, width, height, components int, err error) {
	if len(data) < 4 || data[0] != 0xff || data[1] != 0xd8 {
		return 0, 0, 0, 0, 0, fmt.Errorf("invalid DNG JPEG SOI")
	}
	decoder := dngLosslessJPEGDecoder{data: data, position: 2}
	for decoder.position < len(decoder.data) {
		marker, err = decoder.nextMarker()
		if err != nil {
			return 0, 0, 0, 0, 0, err
		}
		switch marker {
		case 0xc0, 0xc1, 0xc2, 0xc3:
			segment, segmentErr := decoder.segment()
			if segmentErr != nil {
				return 0, 0, 0, 0, 0, segmentErr
			}
			if len(segment) < 6 {
				return 0, 0, 0, 0, 0, fmt.Errorf("truncated DNG JPEG frame header")
			}
			precision = int(segment[0])
			height = int(segment[1])<<8 | int(segment[2])
			width = int(segment[3])<<8 | int(segment[4])
			components = int(segment[5])
			if width < 1 || height < 1 || components < 1 || components > 4 || len(segment) != 6+3*components {
				return 0, 0, 0, 0, 0, fmt.Errorf("invalid DNG JPEG frame parameters")
			}
			return marker, precision, width, height, components, nil
		case 0xda, 0xd9:
			return 0, 0, 0, 0, 0, fmt.Errorf("DNG JPEG frame header is missing")
		case 0xd8, 0x01, 0xd0, 0xd1, 0xd2, 0xd3, 0xd4, 0xd5, 0xd6, 0xd7:
			return 0, 0, 0, 0, 0, fmt.Errorf("unexpected DNG JPEG marker 0x%02x", marker)
		default:
			if _, err := decoder.segment(); err != nil {
				return 0, 0, 0, 0, 0, err
			}
		}
	}
	return 0, 0, 0, 0, 0, fmt.Errorf("DNG JPEG frame header is missing")
}

// decodeDNGLosslessJPEG decodes the single-component Huffman lossless JPEG
// variant used by DNG semantic masks. The output uses the TIFF byte order;
// samples above 8-bit precision are stored in 16-bit containers.
func decodeDNGLosslessJPEG(encoded, tables []byte, width, height, depth int, order binary.ByteOrder) ([]byte, error) {
	return decodeDNGJPEGLossless(encoded, tables, width, height, 1, depth, order)
}

func decodeDNGJPEGLossless(encoded, tables []byte, width, height, components, depth int, order binary.ByteOrder) ([]byte, error) {
	if _, ok := dngLosslessJPEGOutputSize(width, height, components, depth, maxDNGSemanticMaskBytes); !ok || (components != 1 && components != 3) {
		return nil, fmt.Errorf("invalid lossless JPEG image dimensions, channels, or precision")
	}
	stream, err := joinDNGJPEGTables(encoded, tables)
	if err != nil {
		return nil, err
	}
	decoder := dngLosslessJPEGDecoder{data: stream}
	return decoder.decode(width, height, components, depth, order)
}

func dngLosslessJPEGOutputSize(width, height, components, depth, maxOutputBytes int) (int, bool) {
	if width < 1 || height < 1 || components < 1 || depth < 2 || depth > 16 || maxOutputBytes < 0 {
		return 0, false
	}
	bytesPerSample := 1
	if depth > 8 {
		bytesPerSample = 2
	}
	maxSamples := uint64(maxOutputBytes) / uint64(bytesPerSample)
	if uint64(width) > maxSamples/uint64(height)/uint64(components) {
		return 0, false
	}
	sampleCount := uint64(width) * uint64(height) * uint64(components)
	outputBytes := sampleCount * uint64(bytesPerSample)
	if outputBytes > uint64(^uint(0)>>1) {
		return 0, false
	}
	return int(outputBytes), true
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
	data         []byte
	position     int
	precision    int
	components   int
	componentIDs [4]byte
	tables       [4]*dngLosslessHuffman
	restart      int
}

func (decoder *dngLosslessJPEGDecoder) decode(expectedWidth, expectedHeight, expectedComponents, expectedDepth int, order binary.ByteOrder) ([]byte, error) {
	return decoder.decodeWithOutputLimit(expectedWidth, expectedHeight, expectedComponents, expectedDepth, order, maxDNGSemanticMaskBytes)
}

func (decoder *dngLosslessJPEGDecoder) decodeWithOutputLimit(expectedWidth, expectedHeight, expectedComponents, expectedDepth int, order binary.ByteOrder, maxOutputBytes int) ([]byte, error) {
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
			if width != expectedWidth || height != expectedHeight || decoder.components != expectedComponents || decoder.precision != expectedDepth {
				return nil, fmt.Errorf("lossless JPEG frame %dx%d %d-bit/%d-channel does not match TIFF image %dx%d %d-bit/%d-channel", width, height, decoder.precision, decoder.components, expectedWidth, expectedHeight, expectedDepth, expectedComponents)
			}
			return decoder.decodeScan(expectedWidth, expectedHeight, expectedComponents, expectedDepth, order, maxOutputBytes)
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
	if len(segment) < 9 {
		return 0, 0, fmt.Errorf("truncated lossless JPEG frame header")
	}
	precision := int(segment[0])
	height := int(segment[1])<<8 | int(segment[2])
	width := int(segment[3])<<8 | int(segment[4])
	components := int(segment[5])
	if precision < 2 || precision > 16 || width == 0 || height == 0 || components < 1 || components > 4 || len(segment) != 6+3*components {
		return 0, 0, fmt.Errorf("unsupported lossless JPEG frame parameters")
	}
	for index := 0; index < components; index++ {
		componentOffset := 6 + 3*index
		componentID := segment[componentOffset]
		if segment[componentOffset+1] != 0x11 || segment[componentOffset+2] != 0 {
			return 0, 0, fmt.Errorf("lossless JPEG requires unsampled components without quantization tables")
		}
		for earlier := 0; earlier < index; earlier++ {
			if decoder.componentIDs[earlier] == componentID {
				return 0, 0, fmt.Errorf("duplicate lossless JPEG component identifier")
			}
		}
		decoder.componentIDs[index] = componentID
	}
	decoder.precision, decoder.components = precision, components
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

func (decoder *dngLosslessJPEGDecoder) decodeScan(width, height, components, depth int, order binary.ByteOrder, maxOutputBytes int) ([]byte, error) {
	segment, err := decoder.segment()
	if err != nil {
		return nil, err
	}
	if len(segment) != 1+2*components+3 || int(segment[0]) != components {
		return nil, fmt.Errorf("unsupported lossless JPEG scan parameters")
	}
	componentIndexes := make([]int, components)
	tableIDs := make([]int, components)
	seenComponents := make([]bool, components)
	for scanIndex := 0; scanIndex < components; scanIndex++ {
		componentID := segment[1+2*scanIndex]
		componentIndex := -1
		for frameIndex := 0; frameIndex < components; frameIndex++ {
			if decoder.componentIDs[frameIndex] == componentID {
				componentIndex = frameIndex
				break
			}
		}
		selector := segment[2+2*scanIndex]
		if componentIndex < 0 || seenComponents[componentIndex] || selector>>4 >= 4 || selector&0x0f != 0 {
			return nil, fmt.Errorf("invalid lossless JPEG scan component or table selector")
		}
		componentIndexes[scanIndex] = componentIndex
		tableIDs[scanIndex] = int(selector >> 4)
		seenComponents[componentIndex] = true
	}
	predictor := int(segment[1+2*components])
	pointTransformInfo := segment[3+2*components]
	pointTransform := int(pointTransformInfo & 0x0f)
	if predictor < 1 || predictor > 7 || segment[2+2*components] != 0 || pointTransformInfo>>4 != 0 || pointTransform >= decoder.precision {
		return nil, fmt.Errorf("invalid lossless JPEG predictor or point transform")
	}
	for _, tableID := range tableIDs {
		if decoder.tables[tableID] == nil {
			return nil, fmt.Errorf("lossless JPEG DC Huffman table %d is missing", tableID)
		}
	}
	outputBytes, ok := dngLosslessJPEGOutputSize(width, height, components, depth, maxOutputBytes)
	if !ok {
		return nil, fmt.Errorf("lossless JPEG image output exceeds limits")
	}
	if decoder.restart != 0 && decoder.restart%width != 0 {
		return nil, fmt.Errorf("lossless JPEG restart interval must align to image rows")
	}
	sampleCount := width * height
	samples := make([]uint32, sampleCount*components)
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
		x, y := index%width, index/width
		for scanIndex, componentIndex := range componentIndexes {
			table := decoder.tables[tableIDs[scanIndex]]
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
			sampleIndex := index*components + componentIndex
			prediction := initial
			if index != 0 && !firstAfterRestart {
				ra, rb, rc := initial, initial, initial
				if x > 0 {
					ra = int(samples[sampleIndex-components] >> uint(pointTransform))
				}
				if y > 0 {
					rb = int(samples[sampleIndex-width*components] >> uint(pointTransform))
				}
				if x > 0 && y > 0 {
					rc = int(samples[sampleIndex-(width+1)*components] >> uint(pointTransform))
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
			samples[sampleIndex] = uint32(reconstructed << uint(pointTransform))
		}
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
	raw := make([]byte, outputBytes)
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
