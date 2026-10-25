package lrtl

import (
	"fmt"
	"math"
)

type dngProfileMap struct {
	hues, sats, vals int
	srgb             bool
	data             [][3]float64 // value, hue, saturation, then shift/scale/scale
}

type dngProfileMaps struct {
	first, second, third, look *dngProfileMap
	gainTable                  *dngProfileGainTableMap
}

func readDNGProfileMap(fields *dngFields, dimsTag, dataTag, encodingTag uint16) (*dngProfileMap, error) {
	entry, hasData := fields.entries[dataTag]
	if !hasData {
		return nil, nil
	}
	dimsEntry, hasDims := fields.entries[dimsTag]
	if !hasDims || dimsEntry.typeID != 4 || (dimsEntry.count != 2 && dimsEntry.count != 3) || entry.typeID != 11 {
		return nil, fmt.Errorf("invalid DNG profile table %d dimensions or type", dataTag)
	}
	dims := fields.values(dimsTag)
	if fields.err != nil {
		return nil, fields.err
	}
	if len(dims) == 2 {
		dims = append(dims, 1)
	}
	if dims[2] == 0 {
		dims[2] = 1 // Legacy two-dimensional table representation.
	}
	if dims[0] < 1 || dims[1] < 2 || dims[0] > 1<<20 || dims[1] > 1<<20 || dims[2] > 1<<20 {
		return nil, fmt.Errorf("invalid DNG profile table %d divisions", dataTag)
	}
	count := uint64(dims[0]) * uint64(dims[1]) * uint64(dims[2])
	if count > (1<<20)/3 {
		return nil, fmt.Errorf("DNG profile table %d exceeds limits", dataTag)
	}
	fullCount := uint32(count * 3)
	omittedZero := entry.count == uint32(uint64(dims[0])*uint64(dims[1]-1)*uint64(dims[2])*3)
	if entry.count != fullCount && !omittedZero {
		return nil, fmt.Errorf("DNG profile table %d has incorrect sample count", dataTag)
	}
	encoding := 0
	if e, ok := fields.entries[encodingTag]; ok {
		if e.typeID != 4 || e.count != 1 {
			return nil, fmt.Errorf("invalid DNG profile table encoding %d", encodingTag)
		}
		encoding = fields.integer(encodingTag, 0)
	}
	if encoding > 1 {
		return nil, fmt.Errorf("unsupported DNG profile table encoding %d", encoding)
	}
	values := fields.values(dataTag)
	if fields.err != nil {
		return nil, fields.err
	}
	m := &dngProfileMap{int(dims[0]), int(dims[1]), int(dims[2]), encoding == 1, make([][3]float64, int(count))}
	i := 0
	for v := 0; v < m.vals; v++ {
		for h := 0; h < m.hues; h++ {
			start := 0
			if omittedZero {
				start = 1
			}
			base := (v*m.hues + h) * m.sats
			for s := start; s < m.sats; s++ {
				p := [3]float64{values[i], values[i+1], values[i+2]}
				i += 3
				if p[1] < 0 || p[2] < 0 {
					return nil, fmt.Errorf("negative DNG profile table scale in tag %d", dataTag)
				}
				if s == 0 {
					p[2] = 1 // DNG requires neutral input to retain its value.
				}
				m.data[base+s] = p
			}
			if omittedZero {
				m.data[base] = m.data[base+1]
				m.data[base][2] = 1
			}
		}
	}
	return m, nil
}

func readDNGProfileMaps(fields *dngFields) (dngProfileMaps, error) {
	var maps dngProfileMaps
	var err error
	if maps.first, err = readDNGProfileMap(fields, 50937, 50938, 51107); err != nil {
		return maps, err
	}
	if maps.second, err = readDNGProfileMap(fields, 50937, 50939, 51107); err != nil {
		return maps, err
	}
	if maps.second != nil && maps.first == nil {
		return maps, fmt.Errorf("DNG profile has second hue/saturation table without first")
	}
	maps.third, err = readDNGProfileMap(fields, 50937, 52537, 51107)
	if err != nil {
		return maps, err
	}
	if maps.third != nil && (maps.first == nil || maps.second == nil) {
		return maps, fmt.Errorf("DNG profile with a third hue/saturation table requires all three tables")
	}
	maps.look, err = readDNGProfileMap(fields, 50981, 50982, 51108)
	if err != nil {
		return maps, err
	}
	maps.gainTable, err = readDNGProfileGainTableMap(fields)
	return maps, err
}

func (maps dngProfileMaps) hueMap(weight float64) *dngProfileMap {
	return maps.hueMapWeights([3]float64{1 - weight, weight, 0})
}

func (maps dngProfileMaps) hueMapWeights(weights [3]float64) *dngProfileMap {
	if maps.first == nil || maps.second == nil {
		return maps.first
	}
	if maps.third == nil {
		weight := weights[1]
		if weight <= 0 {
			return maps.first
		}
		if weight >= 1 {
			return maps.second
		}
	}
	m := *maps.first
	m.data = make([][3]float64, len(m.data))
	for i := range m.data {
		for c := 0; c < 3; c++ {
			m.data[i][c] = weights[0]*maps.first.data[i][c] + weights[1]*maps.second.data[i][c]
			if maps.third != nil {
				m.data[i][c] += weights[2] * maps.third.data[i][c]
			}
		}
	}
	return &m
}

// Interpolate on the cylindrical HSV grid. Hue wraps to the first column;
// saturation and value use their two neighboring rows, including endpoint 1.
func (m *dngProfileMap) adjustment(h, s, v float64) (result [3]float64) {
	h = math.Mod(h, 6)
	if h < 0 {
		h += 6
	}
	hPos := h * float64(m.hues) / 6
	if m.hues == 1 {
		hPos = 0
	}
	h0 := min(int(hPos), m.hues-1)
	h1 := (h0 + 1) % m.hues
	hf := hPos - float64(h0)
	sPos := min(1, max(0, s)) * float64(m.sats-1)
	s0 := min(int(sPos), m.sats-2)
	sf := sPos - float64(s0)
	vPos := min(1, max(0, v)) * float64(m.vals-1)
	v0 := min(int(vPos), max(0, m.vals-2))
	v1 := min(v0+1, m.vals-1)
	vf := vPos - float64(v0)
	for z, vi := range []int{v0, v1} {
		vw := []float64{1 - vf, vf}[z]
		for y, si := range []int{s0, s0 + 1} {
			sw := []float64{1 - sf, sf}[y]
			for x, hi := range []int{h0, h1} {
				hw := []float64{1 - hf, hf}[x]
				p := m.data[(vi*m.hues+hi)*m.sats+si]
				for c := range result {
					result[c] += p[c] * vw * sw * hw
				}
			}
		}
	}
	return result
}

func (m *dngProfileMap) apply(rgb [3]float64) [3]float64 {
	hi, lo := max(rgb[0], rgb[1], rgb[2]), min(rgb[0], rgb[1], rgb[2])
	h, s := 0.0, 0.0
	if delta := hi - lo; delta > 0 {
		s = delta / hi
		switch hi {
		case rgb[0]:
			h = (rgb[1] - rgb[2]) / delta
		case rgb[1]:
			h = 2 + (rgb[2]-rgb[0])/delta
		default:
			h = 4 + (rgb[0]-rgb[1])/delta
		}
		if h < 0 {
			h += 6
		}
	}
	v := hi
	if m.srgb {
		if v <= .0031308 {
			v *= 12.92
		} else {
			v = 1.055*math.Pow(v, 1/2.4) - .055
		}
	}
	adjustment := m.adjustment(h, s, v)
	h = math.Mod(h+adjustment[0]/60, 6)
	if h < 0 {
		h += 6
	}
	s = min(1, max(0, s*adjustment[1]))
	v = min(1, max(0, v*adjustment[2]))
	if m.srgb {
		if v <= .04045 {
			v /= 12.92
		} else {
			v = math.Pow((v+.055)/1.055, 2.4)
		}
	}
	chroma := v * s
	x := chroma * (1 - math.Abs(math.Mod(h, 2)-1))
	switch int(h) {
	case 0:
		rgb = [3]float64{chroma, x, 0}
	case 1:
		rgb = [3]float64{x, chroma, 0}
	case 2:
		rgb = [3]float64{0, chroma, x}
	case 3:
		rgb = [3]float64{0, x, chroma}
	case 4:
		rgb = [3]float64{x, 0, chroma}
	default:
		rgb = [3]float64{chroma, 0, x}
	}
	for c := range rgb {
		rgb[c] = min(1, max(0, rgb[c]+v-chroma))
	}
	return rgb
}
