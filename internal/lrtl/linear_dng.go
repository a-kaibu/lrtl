package lrtl

// This product includes DNG technology under license by Adobe.

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"math"
	"unicode/utf8"

	xdraw "golang.org/x/image/draw"
)

type dngFields struct {
	scanner      *tiffPreviewScanner
	entries      map[uint16]tiffPreviewEntry
	semanticIFDs []map[uint16]tiffPreviewEntry
	err          error
}

func (fields *dngFields) values(tag uint16) []float64 {
	entry, found := fields.entries[tag]
	if !found || fields.err != nil {
		return nil
	}
	scanner := entry.source
	if scanner == nil {
		scanner = fields.scanner
	}
	unit := uint64(tiffTypeSize(entry.typeID))
	if unit == 0 || entry.count == 0 || entry.count > 1<<20 {
		fields.err = fmt.Errorf("invalid Linear DNG tag %d size or type", tag)
		return nil
	}
	length := uint64(entry.count) * unit
	data := entry.value
	if length > 4 {
		offset := uint64(scanner.order.Uint32(entry.value))
		if !tiffRange(offset, length, uint64(len(scanner.data))) {
			fields.err = fmt.Errorf("Linear DNG tag %d extends beyond file", tag)
			return nil
		}
		data = scanner.data[offset : offset+length]
	}
	values := make([]float64, entry.count)
	order := scanner.order
	for index := range values {
		value := data[uint64(index)*unit : uint64(index+1)*unit]
		switch entry.typeID {
		case 1, 7:
			values[index] = float64(value[0])
		case 3:
			values[index] = float64(order.Uint16(value))
		case 4, 13:
			values[index] = float64(order.Uint32(value))
		case 9:
			values[index] = float64(int32(order.Uint32(value)))
		case 5:
			values[index] = float64(order.Uint32(value[:4])) / float64(order.Uint32(value[4:]))
		case 10:
			values[index] = float64(int32(order.Uint32(value[:4]))) / float64(int32(order.Uint32(value[4:])))
		case 11:
			values[index] = float64(math.Float32frombits(order.Uint32(value)))
		case 12:
			values[index] = math.Float64frombits(order.Uint64(value))
		default:
			fields.err = fmt.Errorf("unsupported numeric type for Linear DNG tag %d", tag)
		}
		if math.IsNaN(values[index]) || math.IsInf(values[index], 0) {
			fields.err = fmt.Errorf("non-finite value in Linear DNG tag %d", tag)
		}
	}
	return values
}

func (fields *dngFields) text(tag uint16) string {
	entry, found := fields.entries[tag]
	if !found || fields.err != nil {
		return ""
	}
	if (entry.typeID != 1 && entry.typeID != 2) || entry.count < 1 || entry.count > 1<<20 {
		fields.err = fmt.Errorf("invalid string in Linear DNG tag %d", tag)
		return ""
	}
	scanner := entry.source
	if scanner == nil {
		scanner = fields.scanner
	}
	data := entry.value
	if entry.count > 4 {
		offset := uint64(scanner.order.Uint32(entry.value))
		if !tiffRange(offset, uint64(entry.count), uint64(len(scanner.data))) {
			fields.err = fmt.Errorf("Linear DNG string tag %d extends beyond file", tag)
			return ""
		}
		data = scanner.data[offset : offset+uint64(entry.count)]
	}
	if data[entry.count-1] != 0 || bytes.IndexByte(data[:entry.count-1], 0) >= 0 || !utf8.Valid(data[:entry.count-1]) {
		fields.err = fmt.Errorf("invalid UTF-8 string in Linear DNG tag %d", tag)
		return ""
	}
	return string(data[:entry.count-1])
}

func (fields *dngFields) scalar(tag uint16, fallback float64) float64 {
	values := fields.values(tag)
	if len(values) == 0 {
		return fallback
	}
	if len(values) != 1 {
		fields.err = fmt.Errorf("Linear DNG tag %d must be scalar", tag)
	}
	return values[0]
}

func (fields *dngFields) integer(tag uint16, fallback int) int {
	value := fields.scalar(tag, float64(fallback))
	if value < 0 || value > math.MaxUint32 || value != math.Trunc(value) {
		fields.err = fmt.Errorf("invalid integer in Linear DNG tag %d", tag)
		return 0
	}
	return int(value)
}

func findLinearDNG(data []byte) (*dngFields, error) {
	return findLinearDNGWithProfile(data, "")
}

func findLinearDNGWithProfile(data []byte, cameraProfile string) (*dngFields, error) {
	return findLinearDNGWithProfileAndDirs(data, cameraProfile, nil)
}

func findLinearDNGWithProfileAndDirs(data []byte, cameraProfile string, externalProfileDirs []string) (*dngFields, error) {
	if len(data) < 8 || len(data) > maxDevelopedRAWBytes {
		return nil, fmt.Errorf("invalid Linear DNG file size")
	}
	var order binary.ByteOrder
	switch string(data[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return nil, fmt.Errorf("Linear DNG has no TIFF byte order")
	}
	if order.Uint16(data[2:4]) != 42 {
		return nil, fmt.Errorf("Linear DNG requires classic TIFF output (use -dng1.4)")
	}
	scanner := &tiffPreviewScanner{data: data, order: order}
	queue := []uint32{order.Uint32(data[4:8])}
	visited := make(map[uint32]bool)
	var root, selected map[uint16]tiffPreviewEntry
	var semanticIFDs []map[uint16]tiffPreviewEntry
	for len(queue) > 0 {
		offset := queue[0]
		queue = queue[1:]
		if offset == 0 || visited[offset] {
			continue
		}
		if len(visited) >= maxPreviewIFDs || len(queue) > maxPreviewQueue {
			return nil, fmt.Errorf("Linear DNG directory graph exceeds limits")
		}
		visited[offset] = true
		entries, next, err := scanner.readDirectory(offset)
		if err != nil {
			return nil, err
		}
		mapping := make(map[uint16]tiffPreviewEntry, len(entries))
		for _, entry := range entries {
			if _, duplicate := mapping[entry.tag]; duplicate {
				return nil, fmt.Errorf("duplicate Linear DNG tag %d", entry.tag)
			}
			mapping[entry.tag] = entry
			if entry.tag == 330 {
				offsets, err := scanner.values(entry)
				if err != nil {
					return nil, err
				}
				for _, child := range offsets {
					if child > math.MaxUint32 || len(queue) >= maxPreviewQueue {
						return nil, fmt.Errorf("invalid Linear DNG SubIFD")
					}
					queue = append(queue, uint32(child))
				}
			}
		}
		if root == nil {
			root = mapping
		}
		fields := &dngFields{scanner: scanner, entries: mapping}
		isPrimaryLinear := fields.integer(262, 0) == 34892 && fields.integer(254, 0) == 0
		if fields.err != nil {
			return nil, fields.err
		}
		if fields.integer(254, 0) == 65540 {
			semanticIFDs = append(semanticIFDs, mapping)
		}
		if fields.err != nil {
			return nil, fields.err
		}
		if isPrimaryLinear {
			if selected != nil {
				return nil, fmt.Errorf("multiple primary Linear DNG images are unsupported")
			}
			selected = mapping
		}
		queue = append(queue, next)
	}
	if root == nil || selected == nil {
		return nil, fmt.Errorf("no primary LinearRaw image found; converter must use -l -u")
	}
	if version, found := root[50706]; !found || version.typeID != 1 || version.count != 4 || version.value[0] != 1 {
		return nil, fmt.Errorf("missing or invalid DNGVersion")
	}
	merged := make(map[uint16]tiffPreviewEntry)
	for tag, entry := range root {
		merged[tag] = entry
	}
	for tag, entry := range selected {
		merged[tag] = entry
	}
	// Opcode lists belong to their own image IFD, not to a root preview IFD.
	for _, tag := range []uint16{51008, 51009, 51022} {
		if _, found := selected[tag]; !found {
			delete(merged, tag)
		}
	}
	fields := &dngFields{scanner: scanner, entries: merged, semanticIFDs: semanticIFDs}
	if err := selectDNGCameraProfileWithExternalDirs(fields, root, cameraProfile, externalProfileDirs); err != nil {
		return nil, err
	}
	return fields, nil
}

// decodeLinearDNG reads the actual uncompressed, demosaiced sensor planes,
// not an embedded JPEG. Camera-space values are black/white normalized,
// white-balanced and transformed to display sRGB using the DNG color metadata.
func decodeLinearDNG(data []byte) (*image.NRGBA64, error) {
	return decodeLinearDNGWithProfile(data, "")
}

func decodeLinearDNGWithProfile(data []byte, cameraProfile string) (*image.NRGBA64, error) {
	return decodeLinearDNGWithProfileAndDirs(data, cameraProfile, nil)
}

func decodeLinearDNGWithProfileAndDirs(data []byte, cameraProfile string, externalProfileDirs []string) (*image.NRGBA64, error) {
	fields, err := findLinearDNGWithProfileAndDirs(data, cameraProfile, externalProfileDirs)
	if err != nil {
		return nil, err
	}
	width, height := fields.integer(256, 0), fields.integer(257, 0)
	channels := fields.integer(277, 1)
	bits := fields.values(258)
	compression, predictor := fields.integer(259, 1), fields.integer(317, 1)
	if fields.integer(284, 1) != 1 {
		return nil, fmt.Errorf("Linear DNG requires chunky samples")
	}
	if compression != 1 && compression != 7 && compression != 8 && compression != 34892 {
		return nil, fmt.Errorf("unsupported Linear DNG compression %d", compression)
	}
	if width < 1 || height < 1 || width > 32768 || height > 32768 || uint64(width)*uint64(height)*8 > maxDevelopedRAWBytes {
		return nil, fmt.Errorf("Linear DNG dimensions exceed development limits")
	}
	if (channels != 1 && channels != 3) || len(bits) != channels {
		return nil, fmt.Errorf("Linear DNG requires one or three equal-depth channels")
	}
	formats := fields.values(339)
	if len(formats) != 0 && len(formats) != 1 && len(formats) != channels {
		return nil, fmt.Errorf("invalid Linear DNG SampleFormat channel count")
	}
	for _, format := range formats {
		if format != 1 {
			return nil, fmt.Errorf("Linear DNG requires unsigned integer samples")
		}
	}
	depth := int(bits[0])
	for _, value := range bits {
		if value != float64(depth) || (depth != 8 && depth != 16) {
			return nil, fmt.Errorf("Linear DNG requires 8-bit or 16-bit integer samples")
		}
	}
	if predictor != 1 && predictor != 2 {
		return nil, fmt.Errorf("unsupported Linear DNG predictor %d", predictor)
	}
	if (compression == 7 || compression == 34892) && predictor != 1 {
		return nil, fmt.Errorf("JPEG Linear DNG cannot use a TIFF predictor")
	}
	if compression == 34892 && depth != 8 {
		return nil, fmt.Errorf("lossy JPEG Linear DNG requires 8-bit samples")
	}
	linearization := fields.values(50712)
	blockWidth, blockHeight := width, fields.integer(278, height)
	offsets, lengths := fields.values(273), fields.values(279)
	if _, tiled := fields.entries[324]; tiled {
		blockWidth, blockHeight = fields.integer(322, 0), fields.integer(323, 0)
		offsets, lengths = fields.values(324), fields.values(325)
	}
	if blockWidth < 1 || blockHeight < 1 || blockWidth > 32768 || blockHeight > 32768 {
		return nil, fmt.Errorf("invalid Linear DNG strip/tile size")
	}
	blocksAcross := (width + blockWidth - 1) / blockWidth
	blocksDown := (height + blockHeight - 1) / blockHeight
	if len(offsets) != blocksAcross*blocksDown || len(lengths) != len(offsets) {
		return nil, fmt.Errorf("Linear DNG strip/tile count does not cover the image")
	}
	sampleBytes := depth / 8
	var jpegTables []byte
	if compression == 7 || compression == 34892 {
		jpegTables, err = dngRawTag(fields, 347)
		if err != nil {
			return nil, err
		}
	}
	rawBlocks := make([][]byte, len(offsets))
	var decodedBlockBytes uint64
	for index, offset := range offsets {
		rows := blockHeight
		if _, tiled := fields.entries[324]; !tiled {
			rows = min(blockHeight, height-index*blockHeight)
		}
		required := uint64(blockWidth) * uint64(rows) * uint64(channels*sampleBytes)
		if required > maxDevelopedRAWBytes || decodedBlockBytes > maxDevelopedRAWBytes-required || offset < 0 || offset > float64(len(data)) || lengths[index] <= 0 || lengths[index] > float64(len(data)) || offset != math.Trunc(offset) || lengths[index] != math.Trunc(lengths[index]) || !tiffRange(uint64(offset), uint64(lengths[index]), uint64(len(data))) || (compression == 1 && lengths[index] < float64(required)) {
			return nil, fmt.Errorf("Linear DNG strip/tile %d is truncated or outside the file", index)
		}
		encoded := data[int(offset):int(offset+lengths[index])]
		var block []byte
		switch compression {
		case 1:
			block = encoded[:int(required)]
		case 8:
			block, err = inflateDNGMaskBlock(encoded, int(required))
			if err != nil {
				return nil, fmt.Errorf("inflate Linear DNG block %d: %w", index, err)
			}
		case 7, 34892:
			block, err = decodeDNGJPEGBlock(encoded, jpegTables, blockWidth, rows, channels, depth, fields.scanner.order)
			if err != nil {
				return nil, fmt.Errorf("decode JPEG Linear DNG block %d: %w", index, err)
			}
		}
		if uint64(len(block)) < required {
			return nil, fmt.Errorf("Linear DNG block %d decoded to too few bytes", index)
		}
		if predictor == 2 {
			undoDNGLinearHorizontalPredictor(block, blockWidth, rows, channels, depth, fields.scanner.order)
		}
		rawBlocks[index] = block[:required]
		decodedBlockBytes += required
	}
	crop := image.Rect(0, 0, width, height)
	if active := fields.values(50829); len(active) != 0 {
		if len(active) != 4 || active[0] < 0 || active[1] < 0 || active[2] > float64(height) || active[3] > float64(width) || active[0] >= active[2] || active[1] >= active[3] {
			return nil, fmt.Errorf("invalid Linear DNG ActiveArea")
		}
		for _, value := range active {
			if value != math.Trunc(value) {
				return nil, fmt.Errorf("fractional Linear DNG ActiveArea")
			}
		}
		crop = image.Rect(int(active[1]), int(active[0]), int(active[3]), int(active[2]))
	}
	activeArea := crop
	originalActiveArea := activeArea
	imageOpcodes, err := readDNGImageOpcodesForTags(fields, channels, width, height, activeArea.Dx(), activeArea.Dy(), depth, 51008)
	if err != nil {
		return nil, err
	}
	scale := fields.values(50718)
	if fields.err != nil {
		return nil, fields.err
	}
	if len(scale) != 0 && (len(scale) != 2 || scale[0] <= 0 || scale[1] <= 0 || scale[0] > 8 || scale[1] > 8) {
		return nil, fmt.Errorf("invalid Linear DNG DefaultScale")
	}
	verticalScale := 1.0
	if len(scale) == 2 {
		verticalScale = scale[1] / scale[0]
	}
	readRawSample := func(x, y, channel int) uint16 {
		block := y/blockHeight*blocksAcross + x/blockWidth
		start := ((y%blockHeight)*blockWidth + x%blockWidth) * channels * sampleBytes
		position := start + channel*sampleBytes
		if depth == 16 {
			return fields.scanner.order.Uint16(rawBlocks[block][position : position+2])
		}
		return uint16(rawBlocks[block][position])
	}
	stage1Width, stage1Height := width, height
	var stage1Pixels []uint16
	blackDeltaOffsetX, blackDeltaOffsetY := 0, 0
	if len(imageOpcodes.stage1) != 0 {
		pixelCount := uint64(width) * uint64(height)
		if channels < 1 || pixelCount > ^uint64(0)/uint64(channels) {
			return nil, fmt.Errorf("DNG Stage 1 opcode image dimensions exceed development limits")
		}
		sampleCount := pixelCount * uint64(channels)
		if sampleCount > uint64(maxDevelopedRAWBytes)/4 || sampleCount > uint64(math.MaxInt) {
			return nil, fmt.Errorf("DNG Stage 1 opcode working buffers exceed development limits")
		}
		stage1Pixels = make([]uint16, int(sampleCount))
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				base := (y*width + x) * channels
				for channel := 0; channel < channels; channel++ {
					stage1Pixels[base+channel] = readRawSample(x, y, channel)
				}
			}
		}
		stage1Pixels, stage1Width, stage1Height, activeArea, blackDeltaOffsetX, blackDeltaOffsetY, err = applyDNGStage1Opcodes(imageOpcodes.stage1, stage1Pixels, width, height, channels, activeArea, verticalScale)
		if err != nil {
			return nil, err
		}
		crop = activeArea
	}
	stage2Opcodes, err := readDNGImageOpcodesForTags(fields, channels, width, height, activeArea.Dx(), activeArea.Dy(), depth, 51009, 51022)
	if err != nil {
		return nil, err
	}
	imageOpcodes.stage2 = stage2Opcodes.stage2
	origin, size := fields.values(50719), fields.values(50720)
	if len(origin) == 0 && len(size) == 2 {
		origin = []float64{0, 0}
	}
	if len(origin) != 0 || len(size) != 0 {
		if len(origin) != 2 || len(size) != 2 || size[0] <= 0 || size[1] <= 0 || origin[0] < 0 || origin[1] < 0 || origin[0]+size[0] > float64(crop.Dx()) || origin[1]+size[1] > float64(crop.Dy()) {
			return nil, fmt.Errorf("invalid Linear DNG default crop")
		}
		left, top := crop.Min.X+int(math.Round(origin[0])), crop.Min.Y+int(math.Round(origin[1]))
		cropped := image.Rect(left, top, left+int(math.Round(size[0])), top+int(math.Round(size[1])))
		if !cropped.In(crop) {
			return nil, fmt.Errorf("Linear DNG default crop is outside ActiveArea")
		}
		crop = cropped
	}
	if crop.Empty() || !crop.In(image.Rect(0, 0, stage1Width, stage1Height)) {
		return nil, fmt.Errorf("Linear DNG crop is outside the image")
	}
	blackModel, err := newDNGBlackModelForTrimmedArea(fields, activeArea, originalActiveArea, blackDeltaOffsetX, blackDeltaOffsetY, channels, depth)
	if err != nil {
		return nil, err
	}
	profileTone, err := readDNGProfileToneCurve(fields)
	if err != nil {
		return nil, err
	}
	profileMaps, err := readDNGProfileMaps(fields)
	if err != nil {
		return nil, err
	}
	mainOrientation := fields.integer(274, 1)
	if fields.err != nil {
		return nil, fields.err
	}
	rgbTables, err := readDNGRGBTables(fields, activeArea.Dx(), activeArea.Dy(), mainOrientation)
	if err != nil {
		return nil, err
	}
	if rgbTables.hasSemanticMasks() {
		for _, opcode := range imageOpcodes.stage2 {
			if opcode.warp != nil && !opcode.warp.identity() {
				return nil, fmt.Errorf("DNG RGBTables semantic masks cannot yet be combined with geometric warp opcodes")
			}
		}
	}
	hasProfile := profileTone != nil || profileMaps.first != nil || profileMaps.look != nil || profileMaps.gainTable != nil || rgbTables != nil
	profileWeights := [3]float64{1, 0, 0}
	transform := dngIdentityMatrix
	finalTransform := dngIdentityMatrix
	xyzToRGB := dngXYZD50ToSRGB
	if hasProfile {
		xyzToRGB, err = dngProPhotoToXYZ.inverse()
		if err != nil {
			return nil, err
		}
		finalTransform = dngXYZD50ToSRGB.multiply(dngProPhotoToXYZ)
	}
	if channels == 3 {
		transform, profileWeights, err = dngCameraToSpaceWithWeights(fields, xyzToRGB)
		if err != nil {
			return nil, err
		}
	} else if dngTripleProfilePresent(fields) {
		return nil, fmt.Errorf("triple-illuminant DNG profiles require three color channels")
	}
	if rgbTables != nil && channels != 3 {
		return nil, fmt.Errorf("DNG RGBTables require three color channels")
	}
	for _, row := range transform {
		for _, coefficient := range row {
			if math.IsNaN(coefficient) || math.IsInf(coefficient, 0) {
				return nil, fmt.Errorf("non-finite DNG color transform")
			}
		}
	}
	hueMap := profileMaps.hueMapWeights(profileWeights)
	exposure, err := readDNGBaselineExposure(fields)
	if err != nil {
		return nil, err
	}
	gain := math.Exp2(exposure)
	var pointOpcodes []dngPointOpcode
	needsOpcodeBuffer := false
	for _, opcode := range imageOpcodes.stage2 {
		if (opcode.warp != nil && !opcode.warp.identity()) || (opcode.vignette != nil && !opcode.vignette.identity()) || opcode.trim != nil || opcode.badPixelConstant != nil || opcode.badPixelList != nil {
			needsOpcodeBuffer = true
		}
		if opcode.point != nil {
			pointOpcodes = append(pointOpcodes, *opcode.point)
		}
	}
	readCamera := func(x, y int) [3]float64 {
		var camera [3]float64
		for channel := 0; channel < channels; channel++ {
			var raw float64
			if stage1Pixels != nil {
				raw = float64(stage1Pixels[(y*stage1Width+x)*channels+channel])
			} else {
				raw = float64(readRawSample(x, y, channel))
			}
			if len(linearization) != 0 {
				raw = linearization[min(int(raw), len(linearization)-1)]
			}
			camera[channel] = blackModel.normalize(raw, x, y, channel)
		}
		return camera
	}
	var cameraPixels []float32
	processedBounds := image.Rect(0, 0, activeArea.Dx(), activeArea.Dy())
	if needsOpcodeBuffer {
		count := uint64(activeArea.Dx()) * uint64(activeArea.Dy()) * uint64(channels)
		if count > uint64(maxDevelopedRAWBytes)/8 {
			return nil, fmt.Errorf("DNG image opcode working buffers exceed development limits")
		}
		cameraPixels = make([]float32, int(count))
		for y := 0; y < activeArea.Dy(); y++ {
			for x := 0; x < activeArea.Dx(); x++ {
				camera := readCamera(x+activeArea.Min.X, y+activeArea.Min.Y)
				for channel := 0; channel < channels; channel++ {
					cameraPixels[(y*activeArea.Dx()+x)*channels+channel] = float32(camera[channel])
				}
			}
		}
		cameraPixels, processedBounds, err = applyDNGImageOpcodesWithBounds(imageOpcodes.stage2, cameraPixels, activeArea.Dx(), activeArea.Dy(), channels, verticalScale)
		if err != nil {
			return nil, err
		}
	}
	outputCrop := crop
	if needsOpcodeBuffer {
		outputCrop = crop.Sub(activeArea.Min).Intersect(processedBounds)
		if outputCrop.Empty() {
			return nil, fmt.Errorf("Linear DNG crop does not overlap the TrimBounds image")
		}
	}
	outputWidth, outputHeight := outputCrop.Dx(), outputCrop.Dy()
	scaledWidth, scaledHeight := outputWidth, outputHeight
	if len(scale) == 2 {
		scaledWidth = int(math.Round(float64(outputWidth) * scale[0]))
		scaledHeight = int(math.Round(float64(outputHeight) * scale[1]))
		if scaledWidth < 1 || scaledHeight < 1 || scaledWidth > 32768 || scaledHeight > 32768 || uint64(scaledWidth)*uint64(scaledHeight)*8 > maxDevelopedRAWBytes {
			return nil, fmt.Errorf("scaled Linear DNG exceeds development limits")
		}
	}
	img := image.NewNRGBA64(image.Rect(0, 0, outputWidth, outputHeight))
	for outputY := 0; outputY < outputHeight; outputY++ {
		for outputX := 0; outputX < outputWidth; outputX++ {
			var camera [3]float64
			if needsOpcodeBuffer {
				localX := outputCrop.Min.X - processedBounds.Min.X + outputX
				localY := outputCrop.Min.Y - processedBounds.Min.Y + outputY
				index := (localY*processedBounds.Dx() + localX) * channels
				for channel := 0; channel < channels; channel++ {
					camera[channel] = float64(cameraPixels[index+channel])
				}
			} else {
				sourceX, sourceY := outputCrop.Min.X+outputX, outputCrop.Min.Y+outputY
				camera = readCamera(sourceX, sourceY)
				applyDNGPointOpcodes(pointOpcodes, &camera, sourceX-activeArea.Min.X, sourceY-activeArea.Min.Y, channels, activeArea.Dx(), activeArea.Dy())
			}
			if channels == 1 {
				camera[1], camera[2] = camera[0], camera[0]
			}
			rgb := transform.vector(camera)
			if hasProfile {
				for channel := range rgb {
					if math.IsNaN(rgb[channel]) {
						return nil, fmt.Errorf("non-finite DNG profile input")
					}
					rgb[channel] = min(1, max(0, rgb[channel]))
				}
				if hueMap != nil {
					rgb = hueMap.apply(rgb)
				}
				for channel := range rgb {
					rgb[channel] = min(1, max(0, rgb[channel]*gain))
				}
				if profileMaps.gainTable != nil {
					imageX := outputCrop.Min.X + outputX - activeArea.Min.X
					imageY := outputCrop.Min.Y + outputY - activeArea.Min.Y
					rgb, err = profileMaps.gainTable.apply(rgb, imageX, imageY, activeArea.Dx(), activeArea.Dy())
					if err != nil {
						return nil, fmt.Errorf("apply DNG ProfileGainTableMap: %w", err)
					}
				}
				if profileMaps.look != nil {
					rgb = profileMaps.look.apply(rgb)
				}
				if profileTone != nil {
					rgb = profileTone.apply(rgb)
				}
				if rgbTables != nil {
					imageX := outputCrop.Min.X + outputX
					imageY := outputCrop.Min.Y + outputY
					if !needsOpcodeBuffer {
						imageX -= activeArea.Min.X
						imageY -= activeArea.Min.Y
					}
					rgb = rgbTables.apply(rgb, imageX, imageY, activeArea.Dx(), activeArea.Dy())
				}
				rgb = finalTransform.vector(rgb)
			} else {
				for channel := range rgb {
					rgb[channel] *= gain
				}
			}
			img.SetNRGBA64(outputX, outputY, color.NRGBA64{
				R: encodeDNGSRGB(rgb[0]), G: encodeDNGSRGB(rgb[1]), B: encodeDNGSRGB(rgb[2]), A: 65535,
			})
		}
	}
	if scaledWidth != img.Bounds().Dx() || scaledHeight != img.Bounds().Dy() {
		resized := image.NewNRGBA64(image.Rect(0, 0, scaledWidth, scaledHeight))
		xdraw.ApproxBiLinear.Scale(resized, resized.Bounds(), img, img.Bounds(), xdraw.Src, nil)
		img = resized
	}
	return img, nil
}

func undoDNGLinearHorizontalPredictor(raw []byte, width, height, channels, depth int, order binary.ByteOrder) {
	bytesPerSample := depth / 8
	pixelStride := channels * bytesPerSample
	rowStride := width * pixelStride
	for row := 0; row < height; row++ {
		rowStart := row * rowStride
		for x := 1; x < width; x++ {
			pixelStart := rowStart + x*pixelStride
			previousPixel := pixelStart - pixelStride
			for channel := 0; channel < channels; channel++ {
				position := pixelStart + channel*bytesPerSample
				previous := previousPixel + channel*bytesPerSample
				if depth == 8 {
					raw[position] += raw[previous]
				} else {
					order.PutUint16(raw[position:], order.Uint16(raw[position:])+order.Uint16(raw[previous:]))
				}
			}
		}
	}
}

func encodeDNGSRGB(value float64) uint16 {
	value = min(1, max(0, value))
	if value <= 0.0031308 {
		value *= 12.92
	} else {
		value = 1.055*math.Pow(value, 1/2.4) - 0.055
	}
	return uint16(math.Round(min(1, max(0, value)) * 65535))
}
