package lrtl

import (
	"fmt"
	"math"
)

const maxDNGProfileGainTableMapBytes = 16 << 20

// dngProfileGainTableMap is the DNG 1.6 ProfileGainTableMap. Each image-grid
// position stores a one-dimensional gain curve indexed by a weighted RGB
// value.
type dngProfileGainTableMap struct {
	rows, cols, points int
	spacingV, spacingH float64
	originV, originH   float64
	weights            [5]float64
	data               []float32
}

func readDNGProfileGainTableMap(fields *dngFields) (*dngProfileGainTableMap, error) {
	entry, found := fields.entries[52525]
	if !found {
		return nil, nil
	}
	if entry.typeID != 7 || entry.count < 64 || entry.count > maxDNGProfileGainTableMapBytes {
		return nil, fmt.Errorf("invalid DNG ProfileGainTableMap size or type")
	}
	data := entry.value[:]
	if entry.count > 4 {
		offset := uint64(fields.scanner.order.Uint32(entry.value))
		if !tiffRange(offset, uint64(entry.count), uint64(len(fields.scanner.data))) {
			return nil, fmt.Errorf("DNG ProfileGainTableMap exceeds file bounds")
		}
		data = fields.scanner.data[offset : offset+uint64(entry.count)]
	}
	if len(data) < 64 {
		return nil, fmt.Errorf("truncated DNG ProfileGainTableMap header")
	}
	order := fields.scanner.order
	rows, cols, points := uint64(order.Uint32(data)), uint64(order.Uint32(data[4:])), uint64(order.Uint32(data[40:]))
	if rows == 0 || cols == 0 || points == 0 {
		return nil, fmt.Errorf("empty DNG ProfileGainTableMap")
	}
	maxSamples := uint64((maxDNGProfileGainTableMapBytes - 64) / 4)
	samples := rows
	if cols > maxSamples/samples {
		return nil, fmt.Errorf("DNG ProfileGainTableMap exceeds limits")
	}
	samples *= cols
	if points > maxSamples/samples {
		return nil, fmt.Errorf("DNG ProfileGainTableMap exceeds limits")
	}
	samples *= points
	if uint64(len(data)) != 64+samples*4 {
		return nil, fmt.Errorf("DNG ProfileGainTableMap has incorrect sample count")
	}
	if rows > math.MaxInt32 || cols > math.MaxInt32 || points > math.MaxInt32 {
		return nil, fmt.Errorf("DNG ProfileGainTableMap dimensions exceed limits")
	}
	m := &dngProfileGainTableMap{
		rows: int(rows), cols: int(cols), points: int(points),
		spacingV: math.Float64frombits(order.Uint64(data[8:])),
		spacingH: math.Float64frombits(order.Uint64(data[16:])),
		originV:  math.Float64frombits(order.Uint64(data[24:])),
		originH:  math.Float64frombits(order.Uint64(data[32:])),
		data:     make([]float32, int(samples)),
	}
	if m.rows == 1 {
		m.spacingV, m.originV = 1, 0
	}
	if m.cols == 1 {
		m.spacingH, m.originH = 1, 0
	}
	for _, value := range []float64{m.spacingV, m.spacingH, m.originV, m.originH} {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, fmt.Errorf("non-finite DNG ProfileGainTableMap geometry")
		}
	}
	if m.spacingV <= 0 || m.spacingH <= 0 {
		return nil, fmt.Errorf("DNG ProfileGainTableMap spacing must be positive")
	}
	for i := range m.weights {
		value := math.Float32frombits(order.Uint32(data[44+i*4:]))
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return nil, fmt.Errorf("non-finite DNG ProfileGainTableMap input weight")
		}
		m.weights[i] = float64(value)
	}
	for i := range m.data {
		value := math.Float32frombits(order.Uint32(data[64+i*4:]))
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) || value < 0 {
			return nil, fmt.Errorf("invalid DNG ProfileGainTableMap gain")
		}
		m.data[i] = value
	}
	return m, nil
}

func (m *dngProfileGainTableMap) gain(rgb [3]float64, x, y, width, height int) (float64, error) {
	if width < 1 || height < 1 || x < 0 || x >= width || y < 0 || y >= height {
		return 0, fmt.Errorf("invalid DNG ProfileGainTableMap pixel position")
	}
	lo, hi := min(rgb[0], rgb[1], rgb[2]), max(rgb[0], rgb[1], rgb[2])
	input := rgb[0]*m.weights[0] + rgb[1]*m.weights[1] + rgb[2]*m.weights[2] + lo*m.weights[3] + hi*m.weights[4]
	if math.IsNaN(input) {
		return 0, fmt.Errorf("invalid DNG ProfileGainTableMap table input")
	}
	input = min(1, max(0, input))
	// The DNG specification indexes the table directly by input*MapPointsN.
	// Clamp the endpoint to the final stored sample, which is the only valid
	// entry when input is exactly one.
	index := min(float64(m.points-1), input*float64(m.points))
	first, fraction := int(index), index-float64(int(index))
	second := min(first+1, m.points-1)

	u := ((float64(x)+.5)/float64(width) - m.originH) / m.spacingH
	v := ((float64(y)+.5)/float64(height) - m.originV) / m.spacingV
	u, v = min(float64(m.cols-1), max(0, u)), min(float64(m.rows-1), max(0, v))
	x0, y0 := int(u), int(v)
	x1, y1 := min(x0+1, m.cols-1), min(y0+1, m.rows-1)
	fx, fy := u-float64(x0), v-float64(y0)
	sample := func(row, col, point int) float64 {
		base := (row*m.cols + col) * m.points
		return float64(m.data[base+point])
	}
	curveValue := func(row, col int) float64 {
		return sample(row, col, first)*(1-fraction) + sample(row, col, second)*fraction
	}
	upper := curveValue(y0, x0)*(1-fx) + curveValue(y0, x1)*fx
	lower := curveValue(y1, x0)*(1-fx) + curveValue(y1, x1)*fx
	return upper*(1-fy) + lower*fy, nil
}

func (m *dngProfileGainTableMap) apply(rgb [3]float64, x, y, width, height int) ([3]float64, error) {
	gain, err := m.gain(rgb, x, y, width, height)
	if err != nil {
		return rgb, err
	}
	for channel := range rgb {
		rgb[channel] *= gain
	}
	return rgb, nil
}
