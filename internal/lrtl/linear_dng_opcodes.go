package lrtl

import (
	"encoding/binary"
	"fmt"
	"image"
	"math"
)

const maxDNGOpcodeBytes = 16 << 20

type dngGainMap struct {
	area                                 image.Rectangle
	plane, planes, rowPitch, colPitch    int
	rows, cols, mapPlanes                int
	spacingV, spacingH, originV, originH float64
	values                               []float32
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

func readDNGGainMaps(fields *dngFields) ([]dngGainMap, error) {
	var maps []dngGainMap
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
			// Stage 1 uses a different raw-value domain. Only normalized
			// post-linearization and post-demosaic GainMap are supported here.
			if id != 9 || tag == 51008 || version > 0x01070100 {
				if flags&1 != 0 {
					continue
				}
				return nil, fmt.Errorf("unsupported required Linear DNG opcode %d in tag %d (version %08x)", id, tag, version)
			}
			gainMap, err := parseDNGGainMap(payload)
			if err != nil {
				return nil, fmt.Errorf("Linear DNG GainMap in tag %d: %w", tag, err)
			}
			maps = append(maps, gainMap)
		}
		if len(data) != 0 {
			return nil, fmt.Errorf("trailing bytes in Linear DNG opcode list %d", tag)
		}
	}
	return maps, nil
}

func parseDNGGainMap(data []byte) (dngGainMap, error) {
	var result dngGainMap
	if len(data) < 76 {
		return result, fmt.Errorf("truncated parameters")
	}
	order := binary.BigEndian
	ints := [10]uint32{}
	for i := range ints {
		ints[i] = order.Uint32(data[i*4:])
	}
	if ints[0] >= ints[2] || ints[1] >= ints[3] || ints[2] > math.MaxInt32 || ints[3] > math.MaxInt32 || ints[4] > math.MaxInt32 || ints[5] == 0 || uint64(ints[4])+uint64(ints[5]) > math.MaxInt32 || ints[6] == 0 || ints[7] == 0 || ints[6] > math.MaxInt32 || ints[7] > math.MaxInt32 {
		return result, fmt.Errorf("invalid area, plane range or pitch")
	}
	result.area = image.Rect(int(ints[1]), int(ints[0]), int(ints[3]), int(ints[2]))
	result.plane, result.planes, result.rowPitch, result.colPitch = int(ints[4]), int(ints[5]), int(ints[6]), int(ints[7])
	result.rows, result.cols = int(ints[8]), int(ints[9])
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
	return float64(left*(1-fx) + right*fx)
}

func applyDNGGainMaps(maps []dngGainMap, camera *[3]float64, x, y, channels, width, height int) {
	for i := range maps {
		gainMap := &maps[i]
		if !image.Pt(x, y).In(gainMap.area) || (y-gainMap.area.Min.Y)%gainMap.rowPitch != 0 || (x-gainMap.area.Min.X)%gainMap.colPitch != 0 {
			continue
		}
		for plane := gainMap.plane; plane < min(channels, gainMap.plane+gainMap.planes); plane++ {
			camera[plane] = min(1, max(0, camera[plane]*gainMap.gain(x, y, plane, width, height)))
		}
	}
}
