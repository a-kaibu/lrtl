package lrtl

import (
	"encoding/binary"
	"fmt"
	"image"
	"math"
)

const maxDNGOpcodeBytes = 16 << 20

type dngOpcodeArea struct {
	area                              image.Rectangle
	plane, planes, rowPitch, colPitch int
}

type dngGainMap struct {
	dngOpcodeArea
	rows, cols, mapPlanes                int
	spacingV, spacingH, originV, originH float64
	values                               []float32
}

type dngBadPixelConstant struct {
	value      uint16
	bayerPhase uint32
}

type dngPointOpcode struct {
	dngOpcodeArea
	id           uint32
	gainMap      *dngGainMap
	coefficients []float64
	table        []float32
	lookup       []uint16
}

func dngOpcodeBytes(fields *dngFields, tag uint16) ([]byte, error) {
	entry, found := fields.entries[tag]
	if !found {
		return nil, nil
	}
	if (entry.typeID != 7 && entry.typeID != 1) || entry.count > maxDNGOpcodeBytes {
		return nil, fmt.Errorf("invalid Linear DNG opcode tag %d size/type", tag)
	}
	scanner := entry.source
	if scanner == nil {
		scanner = fields.scanner
	}
	if entry.count <= 4 {
		return entry.value[:entry.count], nil
	}
	offset := uint64(scanner.order.Uint32(entry.value))
	if !tiffRange(offset, uint64(entry.count), uint64(len(scanner.data))) {
		return nil, fmt.Errorf("Linear DNG opcode tag %d exceeds file bounds", tag)
	}
	return scanner.data[offset : offset+uint64(entry.count)], nil
}

func readDNGImageOpcodes(fields *dngFields, channels, width, height, activeWidth, activeHeight, depth int) (dngImageOpcodeLists, error) {
	if width < 1 || height < 1 || activeWidth < 1 || activeHeight < 1 {
		return dngImageOpcodeLists{}, fmt.Errorf("invalid Linear DNG opcode image dimensions")
	}
	var opcodes dngImageOpcodeLists
	imageBounds := image.Rect(0, 0, activeWidth, activeHeight)
	for _, tag := range []uint16{51008, 51009, 51022} {
		data, err := dngOpcodeBytes(fields, tag)
		if err != nil {
			return dngImageOpcodeLists{}, err
		}
		if len(data) == 0 {
			continue
		}
		if len(data) < 4 {
			return dngImageOpcodeLists{}, fmt.Errorf("truncated Linear DNG opcode list %d", tag)
		}
		count := uint64(binary.BigEndian.Uint32(data))
		data = data[4:]
		if count > 1024 || count > uint64(len(data))/16 {
			return dngImageOpcodeLists{}, fmt.Errorf("invalid Linear DNG opcode count in tag %d", tag)
		}
		skipNextWarpFallback := false
		for i := uint64(0); i < count; i++ {
			if len(data) < 16 {
				return dngImageOpcodeLists{}, fmt.Errorf("truncated Linear DNG opcode header")
			}
			order := binary.BigEndian
			id, version, flags, size := order.Uint32(data), order.Uint32(data[4:]), order.Uint32(data[8:]), uint64(order.Uint32(data[12:]))
			data = data[16:]
			if size > uint64(len(data)) {
				return dngImageOpcodeLists{}, fmt.Errorf("truncated Linear DNG opcode %d payload", id)
			}
			payload := data[:size]
			data = data[size:]
			if skipNextWarpFallback {
				skipNextWarpFallback = false
				if id == 1 || id == 2 {
					continue
				}
			}
			stage1 := tag == 51008
			unsupportedStage1 := stage1 && id != 4 && id != 5 && (id < 7 || id > 13)
			if (id != 1 && id != 2 && id != 3 && id != 4 && id != 5 && id != 6 && (id < 7 || id > 14)) || unsupportedStage1 || version > 0x01070100 || id == 5 && version < 0x01030000 {
				if flags&1 != 0 {
					continue
				}
				return dngImageOpcodeLists{}, fmt.Errorf("unsupported required Linear DNG opcode %d in tag %d (version %08x)", id, tag, version)
			}
			if stage1 && depth != 16 {
				return dngImageOpcodeLists{}, fmt.Errorf("Linear DNG Stage 1 opcodes require 16-bit samples")
			}
			appendOpcode := func(opcode dngImageOpcode) {
				if stage1 {
					opcodes.stage1 = append(opcodes.stage1, opcode)
				} else {
					opcodes.stage2 = append(opcodes.stage2, opcode)
				}
			}
			if id == 4 {
				if channels != 1 {
					return dngImageOpcodeLists{}, fmt.Errorf("Linear DNG FixBadPixelsConstant requires a single-channel Bayer image")
				}
				badPixel, err := parseDNGBadPixelConstant(payload)
				if err != nil {
					return dngImageOpcodeLists{}, fmt.Errorf("Linear DNG FixBadPixelsConstant in tag %d: %w", tag, err)
				}
				appendOpcode(dngImageOpcode{badPixelConstant: &badPixel})
				continue
			}
			if id == 5 {
				if channels != 1 {
					return dngImageOpcodeLists{}, fmt.Errorf("Linear DNG FixBadPixelsList requires a single-channel Bayer image")
				}
				badPixels, err := parseDNGBadPixelList(payload)
				if err != nil {
					return dngImageOpcodeLists{}, fmt.Errorf("Linear DNG FixBadPixelsList in tag %d: %w", tag, err)
				}
				appendOpcode(dngImageOpcode{badPixelList: &badPixels})
				continue
			}
			if id == 1 || id == 2 || id == 14 {
				var warp *dngWarp
				var err error
				if id == 1 {
					warp, err = parseDNGWarpRectilinear(payload, channels)
				} else if id == 2 {
					warp, err = parseDNGWarpFisheye(payload, channels)
				} else {
					warp, err = parseDNGWarpRectilinear2(payload, channels)
				}
				if err != nil {
					return dngImageOpcodeLists{}, fmt.Errorf("Linear DNG warp in tag %d: %w", tag, err)
				}
				if warp.identity() {
					if id == 14 && flags&1 != 0 {
						skipNextWarpFallback = true
					}
					continue
				}
				appendOpcode(dngImageOpcode{warp: warp})
				if id == 14 && flags&1 != 0 {
					skipNextWarpFallback = true
				}
				continue
			}
			if id == 3 {
				vignette, err := parseDNGVignetteRadial(payload)
				if err != nil {
					return dngImageOpcodeLists{}, fmt.Errorf("Linear DNG radial vignette in tag %d: %w", tag, err)
				}
				if !vignette.identity() {
					appendOpcode(dngImageOpcode{vignette: vignette})
				}
				continue
			}
			if id == 6 {
				trim, err := parseDNGTrimBounds(payload)
				if err != nil {
					return dngImageOpcodeLists{}, fmt.Errorf("Linear DNG TrimBounds in tag %d: %w", tag, err)
				}
				if !trim.In(imageBounds) {
					return dngImageOpcodeLists{}, fmt.Errorf("Linear DNG TrimBounds in tag %d is outside the current image", tag)
				}
				imageBounds = trim
				appendOpcode(dngImageOpcode{trim: &trim})
				continue
			}
			opcode, err := parseDNGPointOpcode(id, payload)
			if err != nil {
				return dngImageOpcodeLists{}, fmt.Errorf("Linear DNG opcode %d in tag %d: %w", id, tag, err)
			}
			if stage1 && (opcode.plane >= channels || opcode.planes > channels-opcode.plane) {
				return dngImageOpcodeLists{}, fmt.Errorf("Linear DNG Stage 1 opcode %d plane range exceeds the image channels", id)
			}
			if opcode.area.Empty() {
				coverageBounds := imageBounds
				if stage1 {
					coverageBounds = image.Rect(0, 0, width, height)
				}
				if (id == 10 || id == 12) && (opcode.area.Min.Y > coverageBounds.Min.Y || opcode.area.Max.Y < coverageBounds.Max.Y) {
					return dngImageOpcodeLists{}, fmt.Errorf("Linear DNG opcode %d in tag %d has an empty area whose row table does not cover the current image", id, tag)
				}
				if (id == 11 || id == 13) && (opcode.area.Min.X > coverageBounds.Min.X || opcode.area.Max.X < coverageBounds.Max.X) {
					return dngImageOpcodeLists{}, fmt.Errorf("Linear DNG opcode %d in tag %d has an empty area whose column table does not cover the current image", id, tag)
				}
			}
			appendOpcode(dngImageOpcode{point: &opcode})
		}
		if len(data) != 0 {
			return dngImageOpcodeLists{}, fmt.Errorf("trailing bytes in Linear DNG opcode list %d", tag)
		}
	}
	return opcodes, nil
}

func parseDNGBadPixelConstant(data []byte) (dngBadPixelConstant, error) {
	if len(data) != 8 {
		return dngBadPixelConstant{}, fmt.Errorf("invalid parameter size")
	}
	phase := binary.BigEndian.Uint32(data[4:])
	if phase > 3 {
		return dngBadPixelConstant{}, fmt.Errorf("invalid Bayer phase %d", phase)
	}
	// DNG SDK behavior truncates Constant to its 16-bit image sample type.
	return dngBadPixelConstant{value: uint16(binary.BigEndian.Uint32(data)), bayerPhase: phase}, nil
}

func parseDNGTrimBounds(data []byte) (image.Rectangle, error) {
	if len(data) != 16 {
		return image.Rectangle{}, fmt.Errorf("invalid TrimBounds parameter size")
	}
	top := int(int32(binary.BigEndian.Uint32(data)))
	left := int(int32(binary.BigEndian.Uint32(data[4:])))
	bottom := int(int32(binary.BigEndian.Uint32(data[8:])))
	right := int(int32(binary.BigEndian.Uint32(data[12:])))
	trim := image.Rect(left, top, right, bottom)
	if trim.Empty() {
		return image.Rectangle{}, fmt.Errorf("empty TrimBounds rectangle")
	}
	return trim, nil
}

func parseDNGOpcodeArea(data []byte) (dngOpcodeArea, error) {
	var result dngOpcodeArea
	if len(data) < 32 {
		return result, fmt.Errorf("truncated area parameters")
	}
	var values [8]uint32
	for i := range values {
		values[i] = binary.BigEndian.Uint32(data[i*4:])
	}
	if values[0] > math.MaxInt32 || values[1] > math.MaxInt32 || values[2] > math.MaxInt32 || values[3] > math.MaxInt32 || uint64(values[4])+uint64(values[5]) > math.MaxInt32 || values[5] == 0 || values[6] == 0 || values[7] == 0 || values[6] > math.MaxInt32 || values[7] > math.MaxInt32 {
		return result, fmt.Errorf("invalid area, plane range or pitch")
	}
	result.plane, result.planes, result.rowPitch, result.colPitch = int(values[4]), int(values[5]), int(values[6]), int(values[7])
	if values[0] > values[2] || values[1] > values[3] {
		return dngOpcodeArea{}, fmt.Errorf("inverted area rectangle")
	}
	if values[0] == values[2] || values[1] == values[3] {
		if values[6] != 1 || values[7] != 1 {
			return dngOpcodeArea{}, fmt.Errorf("empty area requires unit row and column pitch")
		}
		result.area = image.Rect(int(values[1]), int(values[0]), int(values[3]), int(values[2]))
		return result, nil
	}
	height, width := values[2]-values[0], values[3]-values[1]
	if values[6] > height || values[7] > width {
		return dngOpcodeArea{}, fmt.Errorf("area pitch exceeds its dimensions")
	}
	result.area = image.Rect(int(values[1]), int(values[0]), int(values[3]), int(values[2]))
	return result, nil
}

func parseDNGPointOpcode(id uint32, data []byte) (dngPointOpcode, error) {
	result := dngPointOpcode{id: id}
	if id == 9 {
		gainMap, err := parseDNGGainMap(data)
		if err != nil {
			return result, err
		}
		result.dngOpcodeArea, result.gainMap = gainMap.dngOpcodeArea, &gainMap
		return result, nil
	}
	area, err := parseDNGOpcodeArea(data)
	if err != nil {
		return result, err
	}
	result.dngOpcodeArea = area
	if len(data) < 36 {
		return result, fmt.Errorf("truncated mapping parameters")
	}
	count := uint64(binary.BigEndian.Uint32(data[32:]))
	if id == 7 {
		if count < 1 || count > 65536 || uint64(len(data)) != 36+count*2 {
			return result, fmt.Errorf("invalid mapping table size")
		}
		result.lookup = make([]uint16, int(count))
		for i := range result.lookup {
			result.lookup[i] = binary.BigEndian.Uint16(data[36+i*2:])
		}
		return result, nil
	}
	if id == 8 {
		if count > 8 || uint64(len(data)) != 36+(count+1)*8 {
			return result, fmt.Errorf("invalid polynomial degree or coefficient count")
		}
		result.coefficients = make([]float64, int(count)+1)
		for i := range result.coefficients {
			value := math.Float64frombits(binary.BigEndian.Uint64(data[36+i*8:]))
			if math.IsNaN(value) || math.IsInf(float64(float32(value)), 0) {
				return result, fmt.Errorf("non-finite polynomial coefficient")
			}
			result.coefficients[i] = value
		}
		return result, nil
	}
	if id < 10 || id > 13 {
		return result, fmt.Errorf("unsupported point opcode %d", id)
	}
	span, pitch := area.area.Dy(), area.rowPitch
	if id == 11 || id == 13 {
		span, pitch = area.area.Dx(), area.colPitch
	}
	if span < 1 {
		return result, fmt.Errorf("empty area has no row/column span")
	}
	expected := uint64((span-1)/pitch + 1)
	if count != expected || count > uint64((len(data)-36)/4) || uint64(len(data)) != 36+count*4 {
		return result, fmt.Errorf("invalid row/column table count")
	}
	result.table = make([]float32, int(expected))
	for i := range result.table {
		value := math.Float32frombits(binary.BigEndian.Uint32(data[36+i*4:]))
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return result, fmt.Errorf("non-finite row/column entry")
		}
		result.table[i] = value
	}
	return result, nil
}

func parseDNGGainMap(data []byte) (dngGainMap, error) {
	var result dngGainMap
	if len(data) < 76 {
		return result, fmt.Errorf("truncated parameters")
	}
	order := binary.BigEndian
	area, err := parseDNGOpcodeArea(data)
	if err != nil {
		return result, err
	}
	result.dngOpcodeArea = area
	result.rows, result.cols = int(order.Uint32(data[32:])), int(order.Uint32(data[36:]))
	result.spacingV, result.spacingH = math.Float64frombits(order.Uint64(data[40:])), math.Float64frombits(order.Uint64(data[48:]))
	result.originV, result.originH = math.Float64frombits(order.Uint64(data[56:])), math.Float64frombits(order.Uint64(data[64:]))
	result.mapPlanes = int(order.Uint32(data[72:]))
	if result.rows < 1 || result.cols < 1 || result.mapPlanes < 1 {
		return result, fmt.Errorf("empty map")
	}
	// A one-point axis is constant: its encoded spacing/origin are unused.
	if result.rows == 1 {
		result.spacingV, result.originV = 1, 0
	}
	if result.cols == 1 {
		result.spacingH, result.originH = 1, 0
	}
	for _, value := range []float64{result.spacingV, result.spacingH, result.originV, result.originH} {
		if math.IsNaN(value) || math.IsInf(float64(float32(value)), 0) {
			return result, fmt.Errorf("non-finite map geometry")
		}
	}
	if float32(result.spacingV) <= 0 || float32(result.spacingH) <= 0 {
		return result, fmt.Errorf("map spacing must be positive")
	}
	// Bound multiplication by available payload before allocating any map.
	available := uint64((len(data) - 76) / 4)
	rows, cols, planes := uint64(result.rows), uint64(result.cols), uint64(result.mapPlanes)
	if rows > available || cols > available/rows || planes > available/rows/cols || rows*cols*planes != available || len(data) != 76+int(available)*4 {
		return result, fmt.Errorf("invalid gain sample count")
	}
	result.values = make([]float32, int(available))
	for i := range result.values {
		value := math.Float32frombits(order.Uint32(data[76+i*4:]))
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return result, fmt.Errorf("non-finite gain sample")
		}
		result.values[i] = value
	}
	return result, nil
}

func (gainMap *dngGainMap) gain(x, y, plane, width, height int) float64 {
	u := ((float64(x)+.5)/float64(width) - gainMap.originH) / gainMap.spacingH
	v := ((float64(y)+.5)/float64(height) - gainMap.originV) / gainMap.spacingV
	u = min(float64(gainMap.cols-1), max(0, u))
	v = min(float64(gainMap.rows-1), max(0, v))
	x0, y0 := int(u), int(v)
	x1, y1 := min(x0+1, gainMap.cols-1), min(y0+1, gainMap.rows-1)
	fx, fy := float32(u-float64(x0)), float32(v-float64(y0))
	plane = min(plane, gainMap.mapPlanes-1)
	value := func(row, col int) float32 { return gainMap.values[(row*gainMap.cols+col)*gainMap.mapPlanes+plane] }
	left := value(y0, x0)*(1-fy) + value(y1, x0)*fy
	right := value(y0, x1)*(1-fy) + value(y1, x1)*fy
	result := float64(left*(1-fx) + right*fx)
	if math.IsNaN(result) || math.IsInf(result, 0) {
		// Finite extreme entries can overflow the float32 interpolator.
		// Evaluate the same convex combination in double precision so a
		// following lookup cannot receive a non-finite index.
		fx64, fy64 := u-float64(x0), v-float64(y0)
		left64 := float64(value(y0, x0))*(1-fy64) + float64(value(y1, x0))*fy64
		right64 := float64(value(y0, x1))*(1-fy64) + float64(value(y1, x1))*fy64
		result = left64*(1-fx64) + right64*fx64
	}
	return result
}

func applyDNGPointOpcodes(opcodes []dngPointOpcode, camera *[3]float64, x, y, channels, width, height int) {
	applyDNGPointOpcodesAt(opcodes, camera, x, y, 0, 0, channels, width, height)
}

func applyDNGPointOpcodesAt(opcodes []dngPointOpcode, camera *[3]float64, x, y, originX, originY, channels, width, height int) {
	imageX, imageY := x+originX, y+originY
	for i := range opcodes {
		opcode := &opcodes[i]
		areaEmpty := opcode.area.Empty()
		if (!areaEmpty && !image.Pt(imageX, imageY).In(opcode.area)) || (!areaEmpty && ((imageY-opcode.area.Min.Y)%opcode.rowPitch != 0 || (imageX-opcode.area.Min.X)%opcode.colPitch != 0)) {
			continue
		}
		for plane := opcode.plane; plane < min(channels, opcode.plane+opcode.planes); plane++ {
			value := camera[plane]
			switch opcode.id {
			case 7:
				index := min(len(opcode.lookup)-1, int(math.Round(value*65535)))
				value = float64(opcode.lookup[index]) / 65535
			case 8:
				result := opcode.coefficients[len(opcode.coefficients)-1]
				for i := len(opcode.coefficients) - 2; i >= 0; i-- {
					result = result*value + opcode.coefficients[i]
				}
				value = result
			case 9:
				value *= opcode.gainMap.gain(x, y, plane, width, height)
			case 10:
				row := (imageY - opcode.area.Min.Y) / opcode.rowPitch
				value += float64(opcode.table[row])
			case 11:
				col := (imageX - opcode.area.Min.X) / opcode.colPitch
				value += float64(opcode.table[col])
			case 12:
				row := (imageY - opcode.area.Min.Y) / opcode.rowPitch
				value *= float64(opcode.table[row])
			case 13:
				col := (imageX - opcode.area.Min.X) / opcode.colPitch
				value *= float64(opcode.table[col])
			}
			camera[plane] = min(1, max(0, value))
		}
	}
}

// applyDNGPointOpcodeRaw applies Stage 1 point opcodes to interleaved integer
// image samples. MapTable and MapPolynomial operate in the stored 16-bit raw
// domain. GainMap and row/column corrections use the normalized float domain
// used by the DNG SDK, then return to the stored 16-bit range after each opcode.
func applyDNGPointOpcodeRaw(source, destination []uint16, width, height, channels int, opcode dngPointOpcode) error {
	if width < 1 || height < 1 || channels < 1 || uint64(width) > uint64(math.MaxInt)/uint64(height) {
		return fmt.Errorf("invalid Stage 1 point opcode image dimensions")
	}
	pixelCount := uint64(width) * uint64(height)
	if pixelCount > uint64(math.MaxInt)/uint64(channels) {
		return fmt.Errorf("invalid Stage 1 point opcode image dimensions")
	}
	sampleCount := int(pixelCount * uint64(channels))
	if len(source) != sampleCount || len(destination) != sampleCount {
		return fmt.Errorf("invalid Stage 1 point opcode image buffer")
	}
	if opcode.plane < 0 || opcode.plane >= channels || opcode.planes < 1 || opcode.planes > channels-opcode.plane || opcode.rowPitch < 1 || opcode.colPitch < 1 {
		return fmt.Errorf("invalid Stage 1 point opcode plane range or pitch")
	}
	if opcode.id == 7 {
		if len(opcode.lookup) < 1 || len(opcode.lookup) > 65536 {
			return fmt.Errorf("invalid Stage 1 MapTable size")
		}
	} else if opcode.id == 8 {
		if len(opcode.coefficients) < 1 || len(opcode.coefficients) > 9 {
			return fmt.Errorf("invalid Stage 1 MapPolynomial coefficients")
		}
	} else if opcode.id == 9 {
		if opcode.gainMap == nil || opcode.gainMap.mapPlanes < 1 || len(opcode.gainMap.values) == 0 {
			return fmt.Errorf("invalid Stage 1 GainMap")
		}
	} else if opcode.id >= 10 && opcode.id <= 13 {
		span := opcode.area.Dy()
		pitch := opcode.rowPitch
		if opcode.id == 11 || opcode.id == 13 {
			span = opcode.area.Dx()
			pitch = opcode.colPitch
		}
		if span < 1 || pitch < 1 || len(opcode.table) != (span-1)/pitch+1 {
			return fmt.Errorf("invalid Stage 1 row/column opcode table")
		}
	} else {
		return fmt.Errorf("unsupported Stage 1 point opcode %d", opcode.id)
	}

	copy(destination, source)
	area := opcode.area
	if area.Empty() {
		area = image.Rect(0, 0, width, height)
	}
	for y := max(0, area.Min.Y); y < min(height, area.Max.Y); y++ {
		if (y-area.Min.Y)%opcode.rowPitch != 0 {
			continue
		}
		for x := max(0, area.Min.X); x < min(width, area.Max.X); x++ {
			if (x-area.Min.X)%opcode.colPitch != 0 {
				continue
			}
			base := (y*width + x) * channels
			for plane := opcode.plane; plane < opcode.plane+opcode.planes; plane++ {
				index := base + plane
				input := source[index]
				var value float64
				if opcode.id == 7 {
					lookupIndex := min(int(input), len(opcode.lookup)-1)
					value = float64(opcode.lookup[lookupIndex])
				} else if opcode.id == 8 {
					value = opcode.coefficients[len(opcode.coefficients)-1]
					for coefficient := len(opcode.coefficients) - 2; coefficient >= 0; coefficient-- {
						value = value*float64(input) + opcode.coefficients[coefficient]
					}
				} else {
					normalized := float32(input) / 65535
					switch opcode.id {
					case 9:
						normalized *= float32(opcode.gainMap.gain(x, y, plane, width, height))
					case 10:
						row := (y - opcode.area.Min.Y) / opcode.rowPitch
						if row < 0 || row >= len(opcode.table) {
							return fmt.Errorf("Stage 1 DeltaPerRow table index %d is outside %d entries", row, len(opcode.table))
						}
						normalized += opcode.table[row] * (1.0 / 65535.0)
					case 11:
						col := (x - opcode.area.Min.X) / opcode.colPitch
						if col < 0 || col >= len(opcode.table) {
							return fmt.Errorf("Stage 1 DeltaPerColumn table index %d is outside %d entries", col, len(opcode.table))
						}
						normalized += opcode.table[col] * (1.0 / 65535.0)
					case 12:
						row := (y - opcode.area.Min.Y) / opcode.rowPitch
						if row < 0 || row >= len(opcode.table) {
							return fmt.Errorf("Stage 1 ScalePerRow table index %d is outside %d entries", row, len(opcode.table))
						}
						normalized *= opcode.table[row]
					case 13:
						col := (x - opcode.area.Min.X) / opcode.colPitch
						if col < 0 || col >= len(opcode.table) {
							return fmt.Errorf("Stage 1 ScalePerColumn table index %d is outside %d entries", col, len(opcode.table))
						}
						normalized *= opcode.table[col]
					}
					if math.IsNaN(float64(normalized)) {
						return fmt.Errorf("Stage 1 opcode %d produced a non-finite sample", opcode.id)
					}
					value = float64(min(float32(1), max(float32(0), normalized))) * 65535
				}
				if math.IsNaN(value) {
					return fmt.Errorf("Stage 1 opcode %d produced a non-finite sample", opcode.id)
				}
				if value <= 0 {
					destination[index] = 0
				} else if value >= math.MaxUint16 {
					destination[index] = math.MaxUint16
				} else {
					destination[index] = uint16(math.Round(value))
				}
			}
		}
	}
	return nil
}

func applyDNGBadPixelConstant(source, destination []float32, width, height int, originX, originY int, opcode dngBadPixelConstant) error {
	if width < 1 || height < 1 || uint64(width)*uint64(height) != uint64(len(source)) || len(destination) != len(source) || opcode.bayerPhase > 3 {
		return fmt.Errorf("invalid FixBadPixelsConstant image or opcode")
	}
	copy(destination, source)
	readSample := func(index int) uint16 {
		value := min(1, max(0, float64(source[index])))
		return uint16(math.Round(value * 65535))
	}
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			index := y*width + x
			if readSample(index) != opcode.value {
				continue
			}
			// The DNG SDK's BayerPhase convention differs from the published
			// table: its IsGreen() predicate is (row+column+phase+(phase>>1))&1 == 0.
			// Match the SDK because Adobe confirms this is the implemented convention.
			imageX, imageY := x+originX, y+originY
			isGreen := (imageX+imageY+int(opcode.bayerPhase)+int(opcode.bayerPhase>>1))&1 == 0
			offsets := [4][2]int{{-2, 0}, {2, 0}, {0, -2}, {0, 2}}
			if isGreen {
				offsets = [4][2]int{{-1, -1}, {1, -1}, {-1, 1}, {1, 1}}
			}
			var total uint32
			count := uint32(0)
			for _, offset := range offsets {
				sx, sy := x+offset[0], y+offset[1]
				if sx < 0 || sy < 0 || sx >= width || sy >= height {
					continue
				}
				value := readSample(sy*width + sx)
				if value == opcode.value {
					continue
				}
				total += uint32(value)
				count++
			}
			if count != 0 {
				repaired := (total + (count >> 1)) / count
				destination[index] = float32(repaired) / 65535
			}
		}
	}
	return nil
}

func applyDNGBadPixelConstantRaw(source, destination []uint16, width, height int, opcode dngBadPixelConstant) error {
	if width < 1 || height < 1 || uint64(width)*uint64(height) != uint64(len(source)) || len(destination) != len(source) || opcode.bayerPhase > 3 {
		return fmt.Errorf("invalid FixBadPixelsConstant raw image or opcode")
	}
	copy(destination, source)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			index := y*width + x
			if source[index] != opcode.value {
				continue
			}
			isGreen := (x+y+int(opcode.bayerPhase)+int(opcode.bayerPhase>>1))&1 == 0
			offsets := [4][2]int{{-2, 0}, {2, 0}, {0, -2}, {0, 2}}
			if isGreen {
				offsets = [4][2]int{{-1, -1}, {1, -1}, {-1, 1}, {1, 1}}
			}
			var total uint32
			count := uint32(0)
			for _, offset := range offsets {
				sx, sy := x+offset[0], y+offset[1]
				if sx < 0 || sy < 0 || sx >= width || sy >= height {
					continue
				}
				value := source[sy*width+sx]
				if value == opcode.value {
					continue
				}
				total += uint32(value)
				count++
			}
			if count != 0 {
				destination[index] = uint16((total + (count >> 1)) / count)
			}
		}
	}
	return nil
}
