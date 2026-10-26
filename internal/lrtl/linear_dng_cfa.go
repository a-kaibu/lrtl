package lrtl

import (
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"math"

	xdraw "golang.org/x/image/draw"
)

type dngCFABayer struct {
	colors             [4]uint8
	planeIndexForColor [3]int
	neighbors          [4][3][]image.Point
}

func readDNGCFABayer(fields *dngFields) (dngCFABayer, error) {
	var pattern dngCFABayer
	dimensions := fields.values(33421)  // CFARepeatPatternDim
	planeColors := fields.values(50710) // CFAPlaneColor
	cfaValues := fields.values(33422)   // CFAPattern
	if fields.integer(50711, 1) != 1 || fields.err != nil {
		return pattern, fmt.Errorf("CFA DNG requires rectangular CFA layout")
	}
	if len(dimensions) != 2 || dimensions[0] != 2 || dimensions[1] != 2 || len(planeColors) != 3 || len(cfaValues) != 4 {
		return pattern, fmt.Errorf("CFA DNG requires a 2x2 RGB Bayer pattern")
	}
	var planeToColor [3]int
	seenPlanes := [3]bool{}
	for plane, value := range planeColors {
		colorIndex := int(value)
		if value != float64(colorIndex) || colorIndex < 0 || colorIndex > 2 || seenPlanes[colorIndex] {
			return pattern, fmt.Errorf("CFAPlaneColor must contain each RGB color exactly once")
		}
		seenPlanes[colorIndex] = true
		planeToColor[plane] = colorIndex
		pattern.planeIndexForColor[colorIndex] = plane
	}
	counts := [3]int{}
	for index, value := range cfaValues {
		plane := int(value)
		if value != float64(plane) || plane < 0 || plane >= len(planeToColor) {
			return pattern, fmt.Errorf("CFAPattern contains an invalid plane index")
		}
		pattern.colors[index] = uint8(planeToColor[plane])
		counts[pattern.colors[index]]++
	}
	if counts != [3]int{1, 2, 1} {
		return pattern, fmt.Errorf("CFAPattern must contain one red, two green, and one blue sample")
	}
	for phaseY := 0; phaseY < 2; phaseY++ {
		for phaseX := 0; phaseX < 2; phaseX++ {
			phase := phaseY*2 + phaseX
			for targetColor := 0; targetColor < 3; targetColor++ {
				bestDistance := math.MaxInt
				for dy := -2; dy <= 2; dy++ {
					for dx := -2; dx <= 2; dx++ {
						if int(pattern.color(phaseX+dx, phaseY+dy)) != targetColor {
							continue
						}
						distance := dx*dx + dy*dy
						if distance < bestDistance {
							bestDistance = distance
							pattern.neighbors[phase][targetColor] = pattern.neighbors[phase][targetColor][:0]
						}
						if distance == bestDistance {
							pattern.neighbors[phase][targetColor] = append(pattern.neighbors[phase][targetColor], image.Pt(dx, dy))
						}
					}
				}
				if len(pattern.neighbors[phase][targetColor]) == 0 {
					return dngCFABayer{}, fmt.Errorf("CFAPattern does not provide all RGB planes")
				}
			}
		}
	}
	return pattern, nil
}

func (pattern dngCFABayer) color(x, y int) uint8 {
	x, y = x%2, y%2
	if x < 0 {
		x += 2
	}
	if y < 0 {
		y += 2
	}
	return pattern.colors[y*2+x]
}

type dngCFABlackModel struct {
	active, originalActive image.Rectangle
	rows, cols             int
	black                  []float64
	horizontal, vertical   []float64
	denominators           [3]float64
	pattern                dngCFABayer
	deltaOffsetX           int
	deltaOffsetY           int
}

func newDNGCFABlackModel(fields *dngFields, active, originalActive image.Rectangle, deltaOffsetX, deltaOffsetY, depth int, pattern dngCFABayer) (*dngCFABlackModel, error) {
	if active.Empty() || originalActive.Empty() || deltaOffsetX < 0 || deltaOffsetY < 0 || deltaOffsetX+active.Dx() > originalActive.Dx() || deltaOffsetY+active.Dy() > originalActive.Dy() {
		return nil, fmt.Errorf("invalid CFA DNG ActiveArea after Stage 1 trimming")
	}
	model := &dngCFABlackModel{active: active, originalActive: originalActive, rows: 1, cols: 1, pattern: pattern, deltaOffsetX: deltaOffsetX, deltaOffsetY: deltaOffsetY}
	if repeat := fields.values(50713); len(repeat) != 0 {
		if len(repeat) != 2 || repeat[0] < 1 || repeat[1] < 1 || repeat[0] > 32768 || repeat[1] > 32768 || repeat[0] != math.Trunc(repeat[0]) || repeat[1] != math.Trunc(repeat[1]) {
			return nil, fmt.Errorf("invalid CFA DNG BlackLevelRepeatDim")
		}
		model.rows, model.cols = int(repeat[0]), int(repeat[1])
	}
	if uint64(model.rows)*uint64(model.cols) > 1<<20 {
		return nil, fmt.Errorf("CFA DNG black pattern exceeds development limits")
	}
	model.black = fields.values(50714)
	if len(model.black) == 0 {
		model.black = []float64{0}
	} else if len(model.black) != 1 && len(model.black) != model.rows*model.cols {
		return nil, fmt.Errorf("invalid CFA DNG black level layout")
	}
	white := fields.values(50717)
	if len(white) == 0 {
		white = []float64{float64((uint64(1) << uint(depth)) - 1)}
	}
	if len(white) != 1 && len(white) != 3 {
		return nil, fmt.Errorf("invalid CFA DNG white level layout")
	}
	model.horizontal, model.vertical = fields.values(50715), fields.values(50716)
	if fields.err != nil {
		return nil, fields.err
	}
	if (len(model.horizontal) != 0 && len(model.horizontal) != originalActive.Dx()) || (len(model.vertical) != 0 && len(model.vertical) != originalActive.Dy()) {
		return nil, fmt.Errorf("invalid CFA DNG black delta dimensions for ActiveArea")
	}
	if len(model.horizontal) != 0 {
		model.horizontal = model.horizontal[deltaOffsetX : deltaOffsetX+active.Dx()]
	}
	if len(model.vertical) != 0 {
		model.vertical = model.vertical[deltaOffsetY : deltaOffsetY+active.Dy()]
	}
	horizontalMax, verticalMax := make([]float64, model.cols), make([]float64, model.rows)
	horizontalSeen, verticalSeen := make([]bool, model.cols), make([]bool, model.rows)
	for x := 0; x < active.Dx(); x++ {
		if len(model.horizontal) != 0 {
			phase := x % model.cols
			if !horizontalSeen[phase] {
				horizontalMax[phase], horizontalSeen[phase] = model.horizontal[x], true
			} else {
				horizontalMax[phase] = max(horizontalMax[phase], model.horizontal[x])
			}
		}
	}
	for y := 0; y < active.Dy(); y++ {
		if len(model.vertical) != 0 {
			phase := y % model.rows
			if !verticalSeen[phase] {
				verticalMax[phase], verticalSeen[phase] = model.vertical[y], true
			} else {
				verticalMax[phase] = max(verticalMax[phase], model.vertical[y])
			}
		}
	}
	for channel := 0; channel < 3; channel++ {
		maximum := math.Inf(-1)
		// Repeat each black-level phase across the 2x2 CFA period so a
		// scalar BlackLevel still produces a denominator for every color.
		for row := 0; row < model.rows*2; row++ {
			for col := 0; col < model.cols*2; col++ {
				if int(pattern.color(col, row)) != channel {
					continue
				}
				black := model.blackAt(row%model.rows, col%model.cols) + horizontalMax[col%model.cols] + verticalMax[row%model.rows]
				maximum = max(maximum, black)
			}
		}
		whiteValue := white[0]
		if len(white) == 3 {
			whiteValue = white[pattern.planeIndexForColor[channel]]
		}
		model.denominators[channel] = whiteValue - maximum
		if maximum == math.Inf(-1) || model.denominators[channel] <= 0 || math.IsNaN(model.denominators[channel]) || math.IsInf(model.denominators[channel], 0) {
			return nil, fmt.Errorf("CFA DNG white level must exceed maximum black level for color plane %d", channel)
		}
	}
	return model, nil
}

func (model *dngCFABlackModel) blackAt(row, col int) float64 {
	if len(model.black) == 1 {
		return model.black[0]
	}
	return model.black[(row%model.rows)*model.cols+col%model.cols]
}

func (model *dngCFABlackModel) normalize(raw float64, x, y, channel int) float64 {
	localX, localY := x-model.active.Min.X, y-model.active.Min.Y
	originalX, originalY := x+model.deltaOffsetX, y+model.deltaOffsetY
	blackX, blackY := originalX-model.originalActive.Min.X, originalY-model.originalActive.Min.Y
	black := model.blackAt(positiveMod(blackY, model.rows), positiveMod(blackX, model.cols))
	if len(model.horizontal) != 0 {
		black += model.horizontal[min(len(model.horizontal)-1, max(0, localX))]
	}
	if len(model.vertical) != 0 {
		black += model.vertical[min(len(model.vertical)-1, max(0, localY))]
	}
	return min(1, max(0, (raw-black)/model.denominators[channel]))
}

func decodeCFADNG(fields *dngFields) (*image.NRGBA64, error) {
	width, height := fields.integer(256, 0), fields.integer(257, 0)
	if fields.integer(262, 0) != 32803 || fields.integer(277, 1) != 1 || fields.integer(284, 1) != 1 {
		return nil, fmt.Errorf("CFA DNG requires one chunky mosaic sample")
	}
	if fields.err != nil || width < 2 || height < 2 || width > 32768 || height > 32768 || uint64(width)*uint64(height)*8 > maxDevelopedRAWBytes {
		return nil, fmt.Errorf("CFA DNG dimensions or layout exceed development limits")
	}
	pattern, err := readDNGCFABayer(fields)
	if err != nil {
		return nil, err
	}
	bits := fields.values(258)
	if len(bits) != 1 || bits[0] != math.Trunc(bits[0]) || bits[0] < 8 || bits[0] > 16 {
		return nil, fmt.Errorf("CFA DNG requires one 8- to 16-bit sample")
	}
	depth := int(bits[0])
	compression, predictor := fields.integer(259, 1), fields.integer(317, 1)
	if fields.integer(339, 1) != 1 || fields.err != nil {
		return nil, fmt.Errorf("CFA DNG requires unsigned integer samples")
	}
	if compression != 1 && compression != 7 && compression != 8 {
		return nil, fmt.Errorf("unsupported CFA DNG compression %d", compression)
	}
	if compression == 7 {
		if predictor != 1 {
			return nil, fmt.Errorf("JPEG CFA DNG cannot use a TIFF predictor")
		}
	} else if predictor != 1 && predictor != 2 {
		return nil, fmt.Errorf("unsupported CFA DNG predictor %d", predictor)
	}
	packed := compression != 7 && depth != 8 && depth != 16
	if fillOrder := fields.integer(266, 1); fillOrder != 1 || fields.err != nil {
		return nil, fmt.Errorf("CFA DNG requires TIFF FillOrder 1")
	}
	bytesPerSample := 1
	if depth > 8 {
		bytesPerSample = 2
	}
	blockWidth, blockHeight := width, fields.integer(278, height)
	var offsets, lengths []float64
	tiled := false
	if _, tiled = fields.entries[324]; tiled {
		blockWidth, blockHeight = fields.integer(322, 0), fields.integer(323, 0)
		offsets, lengths = fields.values(324), fields.values(325)
	} else {
		offsets, lengths = fields.values(273), fields.values(279)
	}
	if fields.err != nil || blockWidth < 1 || blockHeight < 1 || blockWidth > 32768 || blockHeight > 32768 {
		return nil, fmt.Errorf("invalid CFA DNG strip or tile dimensions")
	}
	blocksAcross, blocksDown := (width+blockWidth-1)/blockWidth, (height+blockHeight-1)/blockHeight
	if len(offsets) != blocksAcross*blocksDown || len(lengths) != len(offsets) {
		return nil, fmt.Errorf("CFA DNG strip or tile count does not cover the image")
	}
	var jpegTables []byte
	if compression == 7 {
		jpegTables, err = dngRawTag(fields, 347)
		if err != nil {
			return nil, err
		}
	}
	raw := make([]uint16, width*height)
	var decodedBytes uint64
	for index := range offsets {
		blockX, blockY := index%blocksAcross, index/blocksAcross
		storedWidth, storedHeight := min(blockWidth, width-blockX*blockWidth), min(blockHeight, height-blockY*blockHeight)
		if tiled {
			storedWidth, storedHeight = blockWidth, blockHeight
		}
		decodedBlockBytes := uint64(storedWidth) * uint64(storedHeight) * uint64(bytesPerSample)
		encodedRowBytes := uint64(storedWidth) * uint64(depth)
		if packed {
			encodedRowBytes = (encodedRowBytes + 7) / 8
		} else {
			encodedRowBytes = uint64(storedWidth) * uint64(bytesPerSample)
		}
		encodedBlockBytes := encodedRowBytes * uint64(storedHeight)
		offset, length := offsets[index], lengths[index]
		if decodedBlockBytes > maxDevelopedRAWBytes || decodedBytes > maxDevelopedRAWBytes-decodedBlockBytes || encodedBlockBytes > maxDevelopedRAWBytes || offset < 0 || length <= 0 || offset != math.Trunc(offset) || length != math.Trunc(length) || offset > float64(len(fields.scanner.data)) || length > float64(len(fields.scanner.data)) || !tiffRange(uint64(offset), uint64(length), uint64(len(fields.scanner.data))) {
			return nil, fmt.Errorf("CFA DNG block %d is truncated or outside the file", index)
		}
		encoded := fields.scanner.data[int(offset):int(offset+length)]
		var blockBytes []byte
		switch compression {
		case 1:
			if uint64(len(encoded)) < encodedBlockBytes {
				return nil, fmt.Errorf("CFA DNG block %d is truncated", index)
			}
			blockBytes = encoded[:int(encodedBlockBytes)]
		case 8:
			blockBytes, err = inflateDNGMaskBlock(encoded, int(encodedBlockBytes))
			if err != nil {
				return nil, fmt.Errorf("inflate CFA DNG block %d: %w", index, err)
			}
		case 7:
			blockBytes, err = decodeDNGJPEGBlock(encoded, jpegTables, storedWidth, storedHeight, 1, depth, fields.scanner.order)
			if err != nil {
				return nil, fmt.Errorf("decode CFA DNG JPEG block %d: %w", index, err)
			}
		}
		blockSamples, err := decodeDNGCFABlockSamples(blockBytes, storedWidth, storedHeight, depth, fields.scanner.order, packed && compression != 7)
		if err != nil {
			return nil, fmt.Errorf("decode CFA DNG block %d samples: %w", index, err)
		}
		if predictor == 2 {
			undoDNGCFASamplePredictor(blockSamples, storedWidth, storedHeight, depth)
		}
		for y := 0; y < min(blockHeight, height-blockY*blockHeight); y++ {
			for x := 0; x < min(blockWidth, width-blockX*blockWidth); x++ {
				raw[(blockY*blockHeight+y)*width+blockX*blockWidth+x] = blockSamples[y*storedWidth+x]
			}
		}
		decodedBytes += decodedBlockBytes
	}
	linearization := fields.values(50712)
	activeArea := image.Rect(0, 0, width, height)
	if active := fields.values(50829); len(active) != 0 {
		if len(active) != 4 || active[0] < 0 || active[1] < 0 || active[2] > float64(height) || active[3] > float64(width) || active[0] >= active[2] || active[1] >= active[3] {
			return nil, fmt.Errorf("invalid CFA DNG ActiveArea")
		}
		for _, value := range active {
			if value != math.Trunc(value) {
				return nil, fmt.Errorf("fractional CFA DNG ActiveArea")
			}
		}
		activeArea = image.Rect(int(active[1]), int(active[0]), int(active[3]), int(active[2]))
	}
	originalActiveArea := activeArea
	scale := fields.values(50718)
	if fields.err != nil || (len(scale) != 0 && (len(scale) != 2 || scale[0] <= 0 || scale[1] <= 0 || scale[0] > 8 || scale[1] > 8)) {
		return nil, fmt.Errorf("invalid CFA DNG DefaultScale")
	}
	verticalScale := 1.0
	if len(scale) == 2 {
		verticalScale = scale[1] / scale[0]
	}
	stage1Width, stage1Height := width, height
	blackDeltaOffsetX, blackDeltaOffsetY := 0, 0
	stage1, err := readDNGImageOpcodesForTags(fields, 1, width, height, activeArea.Dx(), activeArea.Dy(), depth, 51008)
	if err != nil {
		return nil, err
	}
	stage1Pixels := raw
	if len(stage1.stage1) != 0 {
		stage1Pixels, stage1Width, stage1Height, activeArea, blackDeltaOffsetX, blackDeltaOffsetY, err = applyDNGStage1Opcodes(stage1.stage1, raw, width, height, 1, activeArea, verticalScale)
		if err != nil {
			return nil, err
		}
	}
	blackModel, err := newDNGCFABlackModel(fields, activeArea, originalActiveArea, blackDeltaOffsetX, blackDeltaOffsetY, depth, pattern)
	if err != nil {
		return nil, err
	}
	readSample := func(x, y int) uint16 { return stage1Pixels[y*stage1Width+x] }
	readNormalized := func(x, y, channel int) float64 {
		rawValue := float64(readSample(x, y))
		if len(linearization) != 0 {
			rawValue = linearization[min(int(rawValue), len(linearization)-1)]
		}
		return blackModel.normalize(rawValue, x, y, channel)
	}
	demosaic := func(x, y int) ([3]float64, error) {
		var camera [3]float64
		phaseX, phaseY := x+blackDeltaOffsetX-originalActiveArea.Min.X, y+blackDeltaOffsetY-originalActiveArea.Min.Y
		phase := ((phaseY%2+2)%2)*2 + (phaseX%2+2)%2
		for channel := 0; channel < 3; channel++ {
			var total float64
			count := 0
			for _, offset := range pattern.neighbors[phase][channel] {
				sampleX, sampleY := x+offset.X, y+offset.Y
				if sampleX < 0 || sampleY < 0 || sampleX >= stage1Width || sampleY >= stage1Height {
					continue
				}
				sampleColor := int(pattern.color(sampleX+blackDeltaOffsetX-originalActiveArea.Min.X, sampleY+blackDeltaOffsetY-originalActiveArea.Min.Y))
				total += readNormalized(sampleX, sampleY, sampleColor)
				count++
			}
			if count == 0 {
				return [3]float64{}, fmt.Errorf("CFA DNG image is too small to interpolate color plane %d", channel)
			}
			camera[channel] = total / float64(count)
		}
		return camera, nil
	}
	stage2, err := readDNGImageOpcodesForTags(fields, 3, stage1Width, stage1Height, activeArea.Dx(), activeArea.Dy(), depth, 51009, 51022)
	if err != nil {
		return nil, err
	}
	origin, cropSize := fields.values(50719), fields.values(50720)
	crop := activeArea
	if len(origin) == 0 && len(cropSize) == 2 {
		origin = []float64{0, 0}
	}
	if len(origin) != 0 || len(cropSize) != 0 {
		if len(origin) != 2 || len(cropSize) != 2 || cropSize[0] <= 0 || cropSize[1] <= 0 || origin[0] < 0 || origin[1] < 0 || origin[0]+cropSize[0] > float64(crop.Dx()) || origin[1]+cropSize[1] > float64(crop.Dy()) {
			return nil, fmt.Errorf("invalid CFA DNG DefaultCrop")
		}
		left, top := crop.Min.X+int(math.Round(origin[0])), crop.Min.Y+int(math.Round(origin[1]))
		crop = image.Rect(left, top, left+int(math.Round(cropSize[0])), top+int(math.Round(cropSize[1])))
		if !crop.In(activeArea) {
			return nil, fmt.Errorf("CFA DNG DefaultCrop is outside ActiveArea")
		}
	}
	if crop.Empty() {
		return nil, fmt.Errorf("empty CFA DNG crop")
	}
	if fields.err != nil {
		return nil, fields.err
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
		for _, opcode := range stage2.stage2 {
			if opcode.warp != nil && !opcode.warp.identity() {
				return nil, fmt.Errorf("DNG RGBTables semantic masks cannot yet be combined with geometric warp opcodes")
			}
		}
	}
	hasProfile := profileTone != nil || profileMaps.first != nil || profileMaps.look != nil || profileMaps.gainTable != nil || rgbTables != nil
	profileWeights := [3]float64{1, 0, 0}
	xyzToRGB, finalTransform := dngXYZD50ToSRGB, dngIdentityMatrix
	if hasProfile {
		xyzToRGB, err = dngProPhotoToXYZ.inverse()
		if err != nil {
			return nil, err
		}
		finalTransform = dngXYZD50ToSRGB.multiply(dngProPhotoToXYZ)
	}
	transform, profileWeights, err := dngCameraToSpaceWithWeights(fields, xyzToRGB)
	if err != nil {
		return nil, err
	}
	hueMap := profileMaps.hueMapWeights(profileWeights)
	exposure, err := readDNGBaselineExposure(fields)
	if err != nil {
		return nil, err
	}
	gain := math.Exp2(exposure)
	var pointOpcodes []dngPointOpcode
	needsOpcodeBuffer := false
	for _, opcode := range stage2.stage2 {
		if (opcode.warp != nil && !opcode.warp.identity()) || (opcode.vignette != nil && !opcode.vignette.identity()) || opcode.trim != nil || opcode.badPixelConstant != nil || opcode.badPixelList != nil {
			needsOpcodeBuffer = true
		}
		if opcode.point != nil {
			pointOpcodes = append(pointOpcodes, *opcode.point)
		}
	}
	var cameraPixels []float32
	processedBounds := image.Rect(0, 0, activeArea.Dx(), activeArea.Dy())
	if needsOpcodeBuffer {
		count := uint64(activeArea.Dx()) * uint64(activeArea.Dy()) * 3
		if count > uint64(maxDevelopedRAWBytes)/8 {
			return nil, fmt.Errorf("CFA DNG opcode working buffers exceed development limits")
		}
		cameraPixels = make([]float32, int(count))
		for y := 0; y < activeArea.Dy(); y++ {
			for x := 0; x < activeArea.Dx(); x++ {
				camera, err := demosaic(x+activeArea.Min.X, y+activeArea.Min.Y)
				if err != nil {
					return nil, err
				}
				base := (y*activeArea.Dx() + x) * 3
				for channel := range camera {
					cameraPixels[base+channel] = float32(camera[channel])
				}
			}
		}
		cameraPixels, processedBounds, err = applyDNGImageOpcodesWithBounds(stage2.stage2, cameraPixels, activeArea.Dx(), activeArea.Dy(), 3, verticalScale)
		if err != nil {
			return nil, err
		}
	}
	outputCrop := crop
	if needsOpcodeBuffer {
		outputCrop = crop.Sub(activeArea.Min).Intersect(processedBounds)
		if outputCrop.Empty() {
			return nil, fmt.Errorf("CFA DNG crop does not overlap the Stage 2 image")
		}
	}
	outputWidth, outputHeight := outputCrop.Dx(), outputCrop.Dy()
	scaledWidth, scaledHeight := outputWidth, outputHeight
	if len(scale) == 2 {
		scaledWidth, scaledHeight = int(math.Round(float64(outputWidth)*scale[0])), int(math.Round(float64(outputHeight)*scale[1]))
		if scaledWidth < 1 || scaledHeight < 1 || scaledWidth > 32768 || scaledHeight > 32768 || uint64(scaledWidth)*uint64(scaledHeight)*8 > maxDevelopedRAWBytes {
			return nil, fmt.Errorf("scaled CFA DNG exceeds development limits")
		}
	}
	img := image.NewNRGBA64(image.Rect(0, 0, outputWidth, outputHeight))
	for outputY := 0; outputY < outputHeight; outputY++ {
		for outputX := 0; outputX < outputWidth; outputX++ {
			var camera [3]float64
			if needsOpcodeBuffer {
				localX := outputCrop.Min.X - processedBounds.Min.X + outputX
				localY := outputCrop.Min.Y - processedBounds.Min.Y + outputY
				base := (localY*processedBounds.Dx() + localX) * 3
				for channel := range camera {
					camera[channel] = float64(cameraPixels[base+channel])
				}
			} else {
				sourceX, sourceY := outputCrop.Min.X+outputX, outputCrop.Min.Y+outputY
				camera, err = demosaic(sourceX, sourceY)
				if err != nil {
					return nil, err
				}
				applyDNGPointOpcodes(pointOpcodes, &camera, sourceX-activeArea.Min.X, sourceY-activeArea.Min.Y, 3, activeArea.Dx(), activeArea.Dy())
			}
			rgb := transform.vector(camera)
			if hasProfile {
				for channel := range rgb {
					if math.IsNaN(rgb[channel]) {
						return nil, fmt.Errorf("non-finite CFA DNG profile input")
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
					rgb, err = profileMaps.gainTable.apply(rgb, outputCrop.Min.X+outputX-activeArea.Min.X, outputCrop.Min.Y+outputY-activeArea.Min.Y, activeArea.Dx(), activeArea.Dy())
					if err != nil {
						return nil, fmt.Errorf("apply CFA DNG ProfileGainTableMap: %w", err)
					}
				}
				if profileMaps.look != nil {
					rgb = profileMaps.look.apply(rgb)
				}
				if profileTone != nil {
					rgb = profileTone.apply(rgb)
				}
				if rgbTables != nil {
					imageX, imageY := outputCrop.Min.X+outputX, outputCrop.Min.Y+outputY
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
			img.SetNRGBA64(outputX, outputY, color.NRGBA64{R: encodeDNGSRGB(rgb[0]), G: encodeDNGSRGB(rgb[1]), B: encodeDNGSRGB(rgb[2]), A: 65535})
		}
	}
	if scaledWidth != outputWidth || scaledHeight != outputHeight {
		resized := image.NewNRGBA64(image.Rect(0, 0, scaledWidth, scaledHeight))
		xdraw.ApproxBiLinear.Scale(resized, resized.Bounds(), img, img.Bounds(), xdraw.Src, nil)
		img = resized
	}
	return img, nil
}

func positiveMod(value, modulus int) int {
	value %= modulus
	if value < 0 {
		value += modulus
	}
	return value
}

func decodeDNGCFABlockSamples(data []byte, width, height, depth int, order binary.ByteOrder, packed bool) ([]uint16, error) {
	if width < 1 || height < 1 || depth < 8 || depth > 16 || uint64(width)*uint64(height)*2 > maxDevelopedRAWBytes {
		return nil, fmt.Errorf("invalid CFA block dimensions or precision")
	}
	sampleCount := width * height
	samples := make([]uint16, sampleCount)
	if packed {
		rowBytes := (width*depth + 7) / 8
		if uint64(rowBytes)*uint64(height) > uint64(len(data)) {
			return nil, fmt.Errorf("truncated packed CFA sample data")
		}
		for y := 0; y < height; y++ {
			row := data[y*rowBytes : (y+1)*rowBytes]
			for x := 0; x < width; x++ {
				bitOffset := x * depth
				var value uint16
				for bit := 0; bit < depth; bit++ {
					position := bitOffset + bit
					value = value<<1 | uint16((row[position/8]>>(7-uint(position%8)))&1)
				}
				samples[y*width+x] = value
			}
		}
		return samples, nil
	}
	bytesPerSample := 1
	if depth > 8 {
		bytesPerSample = 2
	}
	if uint64(sampleCount)*uint64(bytesPerSample) > uint64(len(data)) {
		return nil, fmt.Errorf("truncated CFA sample data")
	}
	for index := range samples {
		position := index * bytesPerSample
		if depth == 8 {
			samples[index] = uint16(data[position])
		} else {
			samples[index] = order.Uint16(data[position : position+2])
		}
	}
	return samples, nil
}

func undoDNGCFASamplePredictor(samples []uint16, width, height, depth int) {
	mask := uint32((uint64(1) << uint(depth)) - 1)
	for y := 0; y < height; y++ {
		row := y * width
		for x := 1; x < width; x++ {
			index := row + x
			samples[index] = uint16((uint32(samples[index]) + uint32(samples[index-1])) & mask)
		}
	}
}
