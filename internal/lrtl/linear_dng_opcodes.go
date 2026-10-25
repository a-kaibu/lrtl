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
	if entry.count <= 4 {
		return entry.value[:entry.count], nil
	}
	offset := uint64(fields.scanner.order.Uint32(entry.value))
	if !tiffRange(offset, uint64(entry.count), uint64(len(fields.scanner.data))) {
		return nil, fmt.Errorf("Linear DNG opcode tag %d exceeds file bounds", tag)
	}
	return fields.scanner.data[offset : offset+uint64(entry.count)], nil
}

func readDNGImageOpcodes(fields *dngFields, channels int) ([]dngImageOpcode, error) {
	var opcodes []dngImageOpcode
	for _, tag := range []uint16{51008, 51009, 51022} {
		data, err := dngOpcodeBytes(fields, tag)
		if err != nil {
			return nil, err
		}
		if len(data) == 0 {
			continue
		}
		if len(data) < 4 {
			return nil, fmt.Errorf("truncated Linear DNG opcode list %d", tag)
		}
		count := uint64(binary.BigEndian.Uint32(data))
		data = data[4:]
		if count > 1024 || count > uint64(len(data))/16 {
			return nil, fmt.Errorf("invalid Linear DNG opcode count in tag %d", tag)
		}
		skipNextWarpFallback := false
		for i := uint64(0); i < count; i++ {
			if len(data) < 16 {
				return nil, fmt.Errorf("truncated Linear DNG opcode header")
			}
			order := binary.BigEndian
			id, version, flags, size := order.Uint32(data), order.Uint32(data[4:]), order.Uint32(data[8:]), uint64(order.Uint32(data[12:]))
			data = data[16:]
			if size > uint64(len(data)) {
				return nil, fmt.Errorf("truncated Linear DNG opcode %d payload", id)
			}
			payload := data[:size]
			data = data[size:]
			if skipNextWarpFallback {
				skipNextWarpFallback = false
				if id == 1 || id == 2 {
					continue
				}
			}
			// Stage 1 uses a different raw-value domain. Only normalized
			// post-linearization and post-demosaic operations are supported.
			if (id != 1 && id != 2 && id != 3 && id != 6 && (id < 7 || id > 14)) || tag == 51008 || version > 0x01070100 {
				if flags&1 != 0 {
					continue
				}
				return nil, fmt.Errorf("unsupported required Linear DNG opcode %d in tag %d (version %08x)", id, tag, version)
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
					return nil, fmt.Errorf("Linear DNG warp in tag %d: %w", tag, err)
				}
				if warp.identity() {
					if id == 14 && flags&1 != 0 {
						skipNextWarpFallback = true
					}
					continue
				}
				opcodes = append(opcodes, dngImageOpcode{warp: warp})
				if id == 14 && flags&1 != 0 {
					skipNextWarpFallback = true
				}
				continue
			}
			if id == 3 {
				vignette, err := parseDNGVignetteRadial(payload)
				if err != nil {
					return nil, fmt.Errorf("Linear DNG radial vignette in tag %d: %w", tag, err)
				}
				if !vignette.identity() {
					opcodes = append(opcodes, dngImageOpcode{vignette: vignette})
				}
				continue
			}
			if id == 6 {
				trim, err := parseDNGTrimBounds(payload)
				if err != nil {
					return nil, fmt.Errorf("Linear DNG TrimBounds in tag %d: %w", tag, err)
				}
				opcodes = append(opcodes, dngImageOpcode{trim: &trim})
				continue
			}
			opcode, err := parseDNGPointOpcode(id, payload)
			if err != nil {
				return nil, fmt.Errorf("Linear DNG opcode %d in tag %d: %w", id, tag, err)
			}
			opcodes = append(opcodes, dngImageOpcode{point: &opcode})
		}
		if len(data) != 0 {
			return nil, fmt.Errorf("trailing bytes in Linear DNG opcode list %d", tag)
		}
	}
	return opcodes, nil
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
	if values[0] >= values[2] || values[1] >= values[3] || values[2] > math.MaxInt32 || values[3] > math.MaxInt32 || uint64(values[4])+uint64(values[5]) > math.MaxInt32 || values[5] == 0 || values[6] == 0 || values[7] == 0 || values[6] > math.MaxInt32 || values[7] > math.MaxInt32 {
		return result, fmt.Errorf("invalid area, plane range or pitch")
	}
	result.area = image.Rect(int(values[1]), int(values[0]), int(values[3]), int(values[2]))
	result.plane, result.planes, result.rowPitch, result.colPitch = int(values[4]), int(values[5]), int(values[6]), int(values[7])
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
	expected := (span-1)/pitch + 1
	if count != uint64(expected) || count > uint64((len(data)-36)/4) || uint64(len(data)) != 36+count*4 {
		return result, fmt.Errorf("invalid row/column table count")
	}
	result.table = make([]float32, expected)
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
		if !image.Pt(imageX, imageY).In(opcode.area) || (imageY-opcode.area.Min.Y)%opcode.rowPitch != 0 || (imageX-opcode.area.Min.X)%opcode.colPitch != 0 {
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
				value += float64(opcode.table[(imageY-opcode.area.Min.Y)/opcode.rowPitch])
			case 11:
				value += float64(opcode.table[(imageX-opcode.area.Min.X)/opcode.colPitch])
			case 12:
				value *= float64(opcode.table[(imageY-opcode.area.Min.Y)/opcode.rowPitch])
			case 13:
				value *= float64(opcode.table[(imageX-opcode.area.Min.X)/opcode.colPitch])
			}
			camera[plane] = min(1, max(0, value))
		}
	}
}
