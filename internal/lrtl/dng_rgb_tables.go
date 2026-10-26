package lrtl

import (
	"bytes"
	"compress/flate"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"image/color"
	"image/jpeg"
	"io"
	"math"
)

const (
	dngRGBTablesTag         = 52543
	dngSemanticNameTag      = 52526
	dngMaskSubAreaTag       = 52536
	maxDNGRGBTableBytes     = 16 << 20
	maxDNGSemanticMaskBytes = 256 << 20
)

type dngRGBTables struct {
	composite uint32
	tables    []dngRGBTable
}

func (tables *dngRGBTables) hasSemanticMasks() bool {
	if tables == nil {
		return false
	}
	for _, table := range tables.tables {
		if !table.background && len(table.masks) != 0 {
			return true
		}
	}
	return false
}

type dngRGBTable struct {
	semanticName string
	background   bool
	divisions    int
	gamma        byte
	gamutExtend  bool
	toTable      dngMatrix
	fromTable    dngMatrix
	values       [][3]float64
	masks        []*dngSemanticMask
}

type dngSemanticMask struct {
	width, height int
	fullWidth     int
	fullHeight    int
	left, top     int
	values        []float32
}

func readDNGRGBTables(fields *dngFields, activeWidth, activeHeight, mainOrientation int) (*dngRGBTables, error) {
	entry, found := fields.entries[dngRGBTablesTag]
	if !found {
		return nil, nil
	}
	if entry.typeID != 7 || uint64(entry.count) < 8 || uint64(entry.count) > maxDNGRGBTableBytes {
		return nil, fmt.Errorf("invalid DNG RGBTables tag size or type")
	}
	raw, err := dngRawTag(fields, dngRGBTablesTag)
	if err != nil {
		return nil, err
	}
	if len(raw) < 8 {
		return nil, fmt.Errorf("truncated DNG RGBTables header")
	}
	scanner := entry.source
	if scanner == nil {
		scanner = fields.scanner
	}
	order := scanner.order
	count, method := order.Uint32(raw), order.Uint32(raw[4:])
	if count < 1 || count > 20 {
		return nil, fmt.Errorf("DNG RGBTables table count must be between 1 and 20")
	}
	if method > 1 {
		return nil, fmt.Errorf("unsupported DNG RGBTables composite method %d", method)
	}
	result := &dngRGBTables{composite: method, tables: make([]dngRGBTable, 0, count)}
	cursor := 8
	backgrounds := 0
	for tableIndex := uint32(0); tableIndex < count; tableIndex++ {
		if cursor+2 > len(raw) {
			return nil, fmt.Errorf("truncated DNG RGBTables table %d name length", tableIndex)
		}
		nameLength := int(order.Uint16(raw[cursor:]))
		cursor += 2
		if cursor+nameLength+5 > len(raw) {
			return nil, fmt.Errorf("truncated DNG RGBTables table %d header", tableIndex)
		}
		nameBytes := raw[cursor : cursor+nameLength]
		for _, value := range nameBytes {
			if value < 0x20 || value > 0x7e {
				return nil, fmt.Errorf("DNG RGBTables semantic name must contain printable ASCII")
			}
		}
		cursor += nameLength
		table := dngRGBTable{semanticName: string(nameBytes), background: nameLength == 0}
		table.divisions = int(raw[cursor])
		pixelType, gamma, primaries, gamut := raw[cursor+1], raw[cursor+2], raw[cursor+3], raw[cursor+4]
		cursor += 5
		if table.divisions < 2 || table.divisions > 32 {
			return nil, fmt.Errorf("DNG RGBTables table %d divisions must be between 2 and 32", tableIndex)
		}
		if pixelType > 2 || gamma > 4 || primaries > 4 || gamut > 1 {
			return nil, fmt.Errorf("invalid DNG RGBTables table %d encoding", tableIndex)
		}
		table.gamma, table.gamutExtend = gamma, gamut == 1
		bytesPerSample := 1
		if pixelType == 1 {
			bytesPerSample = 2
		} else if pixelType == 2 {
			bytesPerSample = 4
		}
		valueCount := table.divisions * table.divisions * table.divisions
		byteCount := valueCount * 3 * bytesPerSample
		if byteCount > len(raw)-cursor {
			return nil, fmt.Errorf("truncated DNG RGBTables table %d data", tableIndex)
		}
		table.values = make([][3]float64, valueCount)
		for index := range table.values {
			for channel := 0; channel < 3; channel++ {
				position := cursor + (index*3+channel)*bytesPerSample
				var value float64
				switch pixelType {
				case 0:
					value = float64(raw[position]) / 255
				case 1:
					value = float64(order.Uint16(raw[position:])) / 65535
				case 2:
					value = float64(math.Float32frombits(order.Uint32(raw[position:])))
				}
				if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 1 {
					return nil, fmt.Errorf("invalid DNG RGBTables table %d value", tableIndex)
				}
				table.values[index][channel] = value
			}
		}
		cursor += byteCount
		table.toTable, table.fromTable, err = dngRGBTablePrimaries(primaries)
		if err != nil {
			return nil, fmt.Errorf("DNG RGBTables table %d: %w", tableIndex, err)
		}
		if table.background {
			backgrounds++
			if backgrounds > 1 {
				return nil, fmt.Errorf("DNG RGBTables cannot contain more than one background table")
			}
		} else {
			for _, directory := range fields.semanticIFDs {
				maskFields := &dngFields{scanner: fields.scanner, entries: directory}
				name := maskFields.text(dngSemanticNameTag)
				if maskFields.err != nil {
					return nil, maskFields.err
				}
				if name != table.semanticName {
					continue
				}
				mask, err := readDNGSemanticMask(maskFields, activeWidth, activeHeight, mainOrientation)
				if err != nil {
					return nil, fmt.Errorf("DNG semantic mask %q: %w", name, err)
				}
				table.masks = append(table.masks, mask)
			}
		}
		result.tables = append(result.tables, table)
	}
	if cursor != len(raw) {
		return nil, fmt.Errorf("DNG RGBTables has %d trailing bytes", len(raw)-cursor)
	}
	active := result.tables[:0]
	for _, table := range result.tables {
		if table.background || len(table.masks) != 0 {
			active = append(active, table)
		}
	}
	result.tables = active
	if len(result.tables) == 0 {
		return nil, nil
	}
	return result, nil
}

func dngRawTag(fields *dngFields, tag uint16) ([]byte, error) {
	entry, found := fields.entries[tag]
	if !found {
		return nil, nil
	}
	scanner := entry.source
	if scanner == nil {
		scanner = fields.scanner
	}
	if entry.typeID != 7 || entry.count == 0 {
		return nil, fmt.Errorf("DNG tag %d must be UNDEFINED", tag)
	}
	if entry.count <= 4 {
		return entry.value[:entry.count], nil
	}
	offset := uint64(scanner.order.Uint32(entry.value))
	if !tiffRange(offset, uint64(entry.count), uint64(len(scanner.data))) {
		return nil, fmt.Errorf("DNG tag %d data extends beyond file", tag)
	}
	return scanner.data[offset : offset+uint64(entry.count)], nil
}

func (table dngRGBTable) apply(rgb [3]float64) [3]float64 {
	linear := table.toTable.vector(rgb)
	var delta [3]float64
	for channel := range linear {
		clipped := min(1, max(0, linear[channel]))
		if table.gamutExtend {
			delta[channel] = linear[channel] - clipped
		}
		linear[channel] = dngRGBTableEncodeGamma(clipped, table.gamma)
	}
	encoded := table.lookup(linear)
	for channel := range encoded {
		encoded[channel] = dngRGBTableDecodeGamma(encoded[channel], table.gamma) + delta[channel]
	}
	return table.fromTable.vector(encoded)
}

func (table dngRGBTable) lookup(rgb [3]float64) [3]float64 {
	var low, high [3]int
	var fraction [3]float64
	for channel := range rgb {
		position := min(1, max(0, rgb[channel])) * float64(table.divisions-1)
		low[channel] = int(math.Floor(position))
		high[channel] = min(low[channel]+1, table.divisions-1)
		fraction[channel] = position - float64(low[channel])
	}
	var result [3]float64
	for r := 0; r < 2; r++ {
		for g := 0; g < 2; g++ {
			for b := 0; b < 2; b++ {
				indices := [3]int{low[0], low[1], low[2]}
				weight := 1.0
				for channel, highIndex := range []int{r, g, b} {
					if highIndex == 1 {
						indices[channel] = high[channel]
						weight *= fraction[channel]
					} else {
						weight *= 1 - fraction[channel]
					}
				}
				index := (indices[0]*table.divisions+indices[1])*table.divisions + indices[2]
				for channel := range result {
					result[channel] += table.values[index][channel] * weight
				}
			}
		}
	}
	return result
}

func (tables *dngRGBTables) apply(rgb [3]float64, x, y, activeWidth, activeHeight int) [3]float64 {
	if tables == nil || len(tables.tables) == 0 {
		return rgb
	}
	// The format caps NumTables at 20. Keep per-pixel weights on the stack so
	// full-resolution RAW rendering does not allocate once for every pixel.
	var weights [20]float64
	maskSum := 0.0
	backgroundIndex := -1
	for index, table := range tables.tables {
		if table.background {
			backgroundIndex = index
			continue
		}
		weight := 0.0
		for _, mask := range table.masks {
			weight += mask.weight(x, y, activeWidth, activeHeight)
		}
		weights[index] = min(1, weight)
		maskSum += weights[index]
	}
	backgroundWeight := 1 - min(1, maskSum)
	if tables.composite == 1 {
		result := rgb
		for index, table := range tables.tables {
			weight := weights[index]
			if index == backgroundIndex {
				weight = backgroundWeight
			}
			if weight == 0 {
				continue
			}
			transformed := table.apply(result)
			for channel := range result {
				result[channel] += weight * (transformed[channel] - result[channel])
			}
		}
		return result
	}
	denominator := maskSum
	if backgroundIndex >= 0 {
		denominator += backgroundWeight
	}
	if denominator <= 0 {
		return rgb
	}
	var result [3]float64
	for index, table := range tables.tables {
		weight := weights[index]
		if index == backgroundIndex {
			weight = backgroundWeight
		}
		if weight == 0 {
			continue
		}
		transformed := table.apply(rgb)
		for channel := range result {
			result[channel] += weight * transformed[channel]
		}
	}
	if backgroundIndex < 0 && maskSum < 1 {
		for channel := range result {
			result[channel] += (1 - maskSum) * rgb[channel]
		}
		denominator = 1
	}
	for channel := range result {
		result[channel] /= denominator
	}
	return result
}

func dngRGBTableEncodeGamma(value float64, gamma byte) float64 {
	switch gamma {
	case 1:
		if value <= 0.0031308 {
			return 12.92 * value
		}
		return 1.055*math.Pow(value, 1/2.4) - .055
	case 2:
		return math.Pow(value, 1/1.8)
	case 3:
		return math.Pow(value, 1/2.2)
	case 4:
		if value < 0.018053968510807 {
			return 4.5 * value
		}
		return 1.09929682680944*math.Pow(value, .45) - 0.09929682680944
	default:
		return value
	}
}

func dngRGBTableDecodeGamma(value float64, gamma byte) float64 {
	switch gamma {
	case 1:
		if value <= 0.04045 {
			return value / 12.92
		}
		return math.Pow((value+.055)/1.055, 2.4)
	case 2:
		return math.Pow(value, 1.8)
	case 3:
		return math.Pow(value, 2.2)
	case 4:
		if value < 0.0812428582986315 {
			return value / 4.5
		}
		return math.Pow((value+0.09929682680944)/1.09929682680944, 1/.45)
	default:
		return value
	}
}

func dngRGBTablePrimaries(index byte) (toTable, fromTable dngMatrix, err error) {
	var red, green, blue, white [2]float64
	switch index {
	case 0: // sRGB
		red, green, blue, white = [2]float64{.64, .33}, [2]float64{.30, .60}, [2]float64{.15, .06}, [2]float64{.3127, .3290}
	case 1: // Adobe RGB
		red, green, blue, white = [2]float64{.64, .33}, [2]float64{.21, .71}, [2]float64{.15, .06}, [2]float64{.3127, .3290}
	case 2: // Display P3
		red, green, blue, white = [2]float64{.68, .32}, [2]float64{.265, .69}, [2]float64{.15, .06}, [2]float64{.3127, .3290}
	case 3: // Rec. 2020
		red, green, blue, white = [2]float64{.708, .292}, [2]float64{.170, .797}, [2]float64{.131, .046}, [2]float64{.3127, .3290}
	case 4: // ProPhoto RGB
		red, green, blue, white = [2]float64{.7347, .2653}, [2]float64{.1596, .8404}, [2]float64{.0366, .0001}, [2]float64{.3457, .3585}
	default:
		return dngMatrix{}, dngMatrix{}, fmt.Errorf("unsupported RGB primary set %d", index)
	}
	primaryXYZ := func(xy [2]float64) [3]float64 {
		return [3]float64{xy[0] / xy[1], 1, (1 - xy[0] - xy[1]) / xy[1]}
	}
	columns := [3][3]float64{primaryXYZ(red), primaryXYZ(green), primaryXYZ(blue)}
	base := dngMatrix{{columns[0][0], columns[1][0], columns[2][0]}, {columns[0][1], columns[1][1], columns[2][1]}, {columns[0][2], columns[1][2], columns[2][2]}}
	baseInverse, err := base.inverse()
	if err != nil {
		return dngMatrix{}, dngMatrix{}, err
	}
	whiteXYZ := primaryXYZ(white)
	rgbToXYZ := base.multiply(dngDiagonal(baseInverse.vector(whiteXYZ)))
	xyzToRGB, err := rgbToXYZ.inverse()
	if err != nil {
		return dngMatrix{}, dngMatrix{}, err
	}
	adaptation, err := dngChromaticAdaptation(dngWhiteD50, whiteXYZ)
	if err != nil {
		return dngMatrix{}, dngMatrix{}, err
	}
	inverseAdaptation, err := adaptation.inverse()
	if err != nil {
		return dngMatrix{}, dngMatrix{}, err
	}
	xyzToProPhoto, err := dngProPhotoToXYZ.inverse()
	if err != nil {
		return dngMatrix{}, dngMatrix{}, err
	}
	toTable = xyzToRGB.multiply(adaptation).multiply(dngProPhotoToXYZ)
	fromTable = xyzToProPhoto.multiply(inverseAdaptation).multiply(rgbToXYZ)
	return toTable, fromTable, nil
}

func readDNGSemanticMask(fields *dngFields, activeWidth, activeHeight, mainOrientation int) (*dngSemanticMask, error) {
	if fields.integer(254, 0) != 65540 || fields.integer(262, 0) != 52527 {
		return nil, fmt.Errorf("IFD is not a semantic mask")
	}
	width, height := fields.integer(256, 0), fields.integer(257, 0)
	if fields.err != nil {
		return nil, fields.err
	}
	if width < 1 || height < 1 || width > 32768 || height > 32768 || uint64(width)*uint64(height)*4 > maxDNGSemanticMaskBytes {
		return nil, fmt.Errorf("semantic mask dimensions exceed limits")
	}
	orientation := fields.integer(274, 1)
	if fields.err != nil {
		return nil, fields.err
	}
	if orientation != mainOrientation {
		return nil, fmt.Errorf("semantic mask orientation does not match the main image")
	}
	bits := fields.values(258)
	if len(bits) != 1 || (bits[0] != 8 && bits[0] != 16 && bits[0] != 32) {
		return nil, fmt.Errorf("semantic masks require 8-, 16-, or 32-bit samples")
	}
	depth := int(bits[0])
	sampleFormat := fields.integer(339, 1)
	if sampleFormat != 1 && sampleFormat != 3 {
		return nil, fmt.Errorf("semantic masks require unsigned integer or floating-point samples")
	}
	if sampleFormat == 3 && depth != 32 {
		return nil, fmt.Errorf("semantic mask floating-point samples must be 32-bit")
	}
	if fields.integer(277, 1) != 1 || fields.integer(284, 1) != 1 {
		return nil, fmt.Errorf("semantic masks require one chunky channel")
	}
	compression := fields.integer(259, 1)
	predictor := fields.integer(317, 1)
	if fields.err != nil {
		return nil, fields.err
	}
	if predictor != 1 && !(predictor == 2 && sampleFormat == 1) {
		return nil, fmt.Errorf("unsupported semantic mask predictor %d", predictor)
	}
	if compression == 7 && (sampleFormat != 1 || (depth != 8 && depth != 16) || predictor != 1) {
		return nil, fmt.Errorf("lossless JPEG semantic masks require 8- or 16-bit integer samples without a TIFF predictor")
	}
	left, top, fullWidth, fullHeight := 0, 0, width, height
	if _, found := fields.entries[dngMaskSubAreaTag]; found {
		area := fields.values(dngMaskSubAreaTag)
		if len(area) != 4 || fields.err != nil || area[0] > float64(math.MaxInt) || area[1] > float64(math.MaxInt) || area[2] > float64(math.MaxInt) || area[3] > float64(math.MaxInt) {
			return nil, fmt.Errorf("invalid MaskSubArea")
		}
		// The specification says to ignore an invalid MaskSubArea and treat the
		// stored mask as covering the full ActiveArea.
		if area[2] > 0 && area[3] > 0 && uint64(area[0])+uint64(height) <= uint64(area[3]) && uint64(area[1])+uint64(width) <= uint64(area[2]) {
			top, left, fullWidth, fullHeight = int(area[0]), int(area[1]), int(area[2]), int(area[3])
		}
	}
	if fullWidth < 1 || fullHeight < 1 || uint64(fullWidth)*uint64(fullHeight)*4 > maxDNGSemanticMaskBytes {
		return nil, fmt.Errorf("semantic mask full dimensions exceed limits")
	}
	values, err := readDNGSemanticMaskSamples(fields, width, height, depth, sampleFormat, compression, predictor)
	if err != nil {
		return nil, err
	}
	if activeWidth < 1 || activeHeight < 1 {
		return nil, fmt.Errorf("invalid active image dimensions for semantic mask")
	}
	return &dngSemanticMask{width: width, height: height, fullWidth: fullWidth, fullHeight: fullHeight, left: left, top: top, values: values}, nil
}

func readDNGSemanticMaskSamples(fields *dngFields, width, height, depth, sampleFormat, compression, predictor int) ([]float32, error) {
	var blockWidth, blockHeight int
	var offsets, lengths []float64
	if _, tiled := fields.entries[324]; tiled {
		blockWidth, blockHeight = fields.integer(322, 0), fields.integer(323, 0)
		offsets, lengths = fields.values(324), fields.values(325)
	} else {
		blockWidth, blockHeight = width, fields.integer(278, height)
		offsets, lengths = fields.values(273), fields.values(279)
	}
	if fields.err != nil {
		return nil, fields.err
	}
	if blockWidth < 1 || blockHeight < 1 || blockWidth > 32768 || blockHeight > 32768 {
		return nil, fmt.Errorf("invalid semantic mask strip/tile dimensions")
	}
	across, down := (width+blockWidth-1)/blockWidth, (height+blockHeight-1)/blockHeight
	if len(offsets) != across*down || len(lengths) != len(offsets) {
		return nil, fmt.Errorf("semantic mask strip/tile count does not cover the image")
	}
	bytesPerSample := depth / 8
	values := make([]float32, width*height)
	order := fields.scanner.order
	var jpegTables []byte
	if compression == 7 {
		var err error
		jpegTables, err = dngRawTag(fields, 347)
		if err != nil {
			return nil, err
		}
	}
	for block := range offsets {
		blockX, blockY := block%across, block/across
		actualWidth, actualHeight := min(blockWidth, width-blockX*blockWidth), min(blockHeight, height-blockY*blockHeight)
		storedWidth, storedHeight := blockWidth, actualHeight
		if _, tiled := fields.entries[324]; tiled {
			storedHeight = blockHeight
		}
		uncompressedBytes := uint64(storedWidth) * uint64(storedHeight) * uint64(bytesPerSample)
		if uncompressedBytes > maxDNGSemanticMaskBytes || offsets[block] < 0 || lengths[block] < 0 || offsets[block] != math.Trunc(offsets[block]) || lengths[block] != math.Trunc(lengths[block]) || offsets[block] > float64(len(fields.scanner.data)) || lengths[block] > float64(len(fields.scanner.data)) || !tiffRange(uint64(offsets[block]), uint64(lengths[block]), uint64(len(fields.scanner.data))) {
			return nil, fmt.Errorf("semantic mask block %d is outside the file", block)
		}
		encoded := fields.scanner.data[int(offsets[block]):int(offsets[block]+lengths[block])]
		var raw []byte
		switch compression {
		case 1:
			if uint64(len(encoded)) < uncompressedBytes {
				return nil, fmt.Errorf("semantic mask block %d is truncated", block)
			}
			raw = encoded[:uncompressedBytes]
		case 8:
			var err error
			raw, err = inflateDNGMaskBlock(encoded, int(uncompressedBytes))
			if err != nil {
				return nil, fmt.Errorf("inflate semantic mask block %d: %w", block, err)
			}
		case 34892:
			if depth != 8 || sampleFormat != 1 {
				return nil, fmt.Errorf("lossy JPEG semantic masks require 8-bit integer samples")
			}
			decoded, err := jpeg.Decode(bytes.NewReader(encoded))
			if err != nil {
				return nil, fmt.Errorf("decode JPEG semantic mask block %d: %w", block, err)
			}
			bounds := decoded.Bounds()
			if bounds.Dx() != storedWidth || bounds.Dy() != storedHeight {
				return nil, fmt.Errorf("JPEG semantic mask block %d dimensions do not match", block)
			}
			raw = make([]byte, int(uncompressedBytes))
			for row := 0; row < storedHeight; row++ {
				for col := 0; col < storedWidth; col++ {
					gray, _, _, _ := color.GrayModel.Convert(decoded.At(bounds.Min.X+col, bounds.Min.Y+row)).RGBA()
					raw[row*storedWidth+col] = byte(gray >> 8)
				}
			}
		case 7:
			var err error
			raw, err = decodeDNGLosslessJPEG(encoded, jpegTables, storedWidth, storedHeight, depth, order)
			if err != nil {
				return nil, fmt.Errorf("decode lossless JPEG semantic mask block %d: %w", block, err)
			}
		default:
			return nil, fmt.Errorf("unsupported semantic mask compression %d", compression)
		}
		if uint64(len(raw)) < uncompressedBytes {
			return nil, fmt.Errorf("semantic mask block %d decoded to too few bytes", block)
		}
		if predictor == 2 {
			undoDNGMaskHorizontalPredictor(raw, storedWidth, storedHeight, depth, order)
		}
		for row := 0; row < actualHeight; row++ {
			for col := 0; col < actualWidth; col++ {
				position := (row*storedWidth + col) * bytesPerSample
				var value float64
				if sampleFormat == 3 {
					value = float64(math.Float32frombits(order.Uint32(raw[position:])))
				} else if depth == 8 {
					value = float64(raw[position]) / 255
				} else if depth == 16 {
					value = float64(order.Uint16(raw[position:])) / 65535
				} else {
					value = float64(order.Uint32(raw[position:])) / math.MaxUint32
				}
				if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 1 {
					return nil, fmt.Errorf("semantic mask sample is outside [0,1]")
				}
				values[(blockY*blockHeight+row)*width+blockX*blockWidth+col] = float32(value)
			}
		}
	}
	return values, nil
}

func inflateDNGMaskBlock(encoded []byte, expected int) ([]byte, error) {
	read := func(reader io.ReadCloser) ([]byte, error) {
		defer reader.Close()
		data, err := io.ReadAll(io.LimitReader(reader, int64(expected)+1))
		if err != nil {
			return nil, err
		}
		if len(data) < expected {
			return nil, fmt.Errorf("decoded data is truncated")
		}
		return data[:expected], nil
	}
	if reader, err := zlib.NewReader(bytes.NewReader(encoded)); err == nil {
		return read(reader)
	}
	return read(flate.NewReader(bytes.NewReader(encoded)))
}

func undoDNGMaskHorizontalPredictor(raw []byte, width, height, depth int, order binary.ByteOrder) {
	bytesPerSample := depth / 8
	for row := 0; row < height; row++ {
		for column := 1; column < width; column++ {
			index := (row*width + column) * bytesPerSample
			previous := index - bytesPerSample
			switch depth {
			case 8:
				raw[index] += raw[previous]
			case 16:
				order.PutUint16(raw[index:], order.Uint16(raw[index:])+order.Uint16(raw[previous:]))
			case 32:
				order.PutUint32(raw[index:], order.Uint32(raw[index:])+order.Uint32(raw[previous:]))
			}
		}
	}
}

func (mask *dngSemanticMask) weight(x, y, activeWidth, activeHeight int) float64 {
	if mask == nil || x < 0 || y < 0 || x >= activeWidth || y >= activeHeight || activeWidth < 1 || activeHeight < 1 {
		return 0
	}
	u := (float64(x)+.5)*float64(mask.fullWidth)/float64(activeWidth) - float64(mask.left) - .5
	v := (float64(y)+.5)*float64(mask.fullHeight)/float64(activeHeight) - float64(mask.top) - .5
	if u < -.5 || v < -.5 || u > float64(mask.width)-.5 || v > float64(mask.height)-.5 {
		return 0
	}
	u, v = min(float64(mask.width-1), max(0, u)), min(float64(mask.height-1), max(0, v))
	x0, y0 := int(math.Floor(u)), int(math.Floor(v))
	x1, y1 := min(x0+1, mask.width-1), min(y0+1, mask.height-1)
	fx, fy := u-float64(x0), v-float64(y0)
	value := func(row, col int) float64 { return float64(mask.values[row*mask.width+col]) }
	left := value(y0, x0)*(1-fy) + value(y1, x0)*fy
	right := value(y0, x1)*(1-fy) + value(y1, x1)*fy
	return min(1, max(0, left*(1-fx)+right*fx))
}
