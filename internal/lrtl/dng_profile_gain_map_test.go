package lrtl

import (
	"encoding/binary"
	"math"
	"testing"
)

func testDNGProfileGainTableMapField(order binary.ByteOrder, rows, cols, points uint32, spacingV, spacingH, originV, originH float64, weights [5]float32, gains ...float32) dngTestField {
	data := make([]byte, 64+len(gains)*4)
	order.PutUint32(data, rows)
	order.PutUint32(data[4:], cols)
	order.PutUint64(data[8:], math.Float64bits(spacingV))
	order.PutUint64(data[16:], math.Float64bits(spacingH))
	order.PutUint64(data[24:], math.Float64bits(originV))
	order.PutUint64(data[32:], math.Float64bits(originH))
	order.PutUint32(data[40:], points)
	for i, weight := range weights {
		order.PutUint32(data[44+i*4:], math.Float32bits(weight))
	}
	for i, gain := range gains {
		order.PutUint32(data[64+i*4:], math.Float32bits(gain))
	}
	return dngTestField{typeID: 7, count: uint32(len(data)), data: data}
}

func testDNGProfileGainTableMap2Field(order binary.ByteOrder, rows, cols, points uint32, spacingV, spacingH, originV, originH float64, weights [5]float32, dataType uint32, gamma, gainMin, gainMax float32, samples []byte) dngTestField {
	data := make([]byte, 80+len(samples))
	order.PutUint32(data, rows)
	order.PutUint32(data[4:], cols)
	order.PutUint64(data[8:], math.Float64bits(spacingV))
	order.PutUint64(data[16:], math.Float64bits(spacingH))
	order.PutUint64(data[24:], math.Float64bits(originV))
	order.PutUint64(data[32:], math.Float64bits(originH))
	order.PutUint32(data[40:], points)
	for i, weight := range weights {
		order.PutUint32(data[44+i*4:], math.Float32bits(weight))
	}
	order.PutUint32(data[64:], dataType)
	order.PutUint32(data[68:], math.Float32bits(gamma))
	order.PutUint32(data[72:], math.Float32bits(gainMin))
	order.PutUint32(data[76:], math.Float32bits(gainMax))
	copy(data[80:], samples)
	return dngTestField{typeID: 7, count: uint32(len(data)), data: data}
}

func readTestDNGProfileGainTableMap(t *testing.T, order binary.ByteOrder, field dngTestField) *dngProfileGainTableMap {
	t.Helper()
	fields, err := findLinearDNG(linearDNGFixture(order, 1, 1, 1, []uint16{1}, map[uint16]dngTestField{52525: field}))
	if err != nil {
		t.Fatal(err)
	}
	got, err := readDNGProfileGainTableMap(fields)
	if err != nil || got == nil {
		t.Fatalf("read profile gain table map: %v", err)
	}
	return got
}

func TestDNGProfileGainTableMapSpatialAndInputInterpolation(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		field := testDNGProfileGainTableMapField(order, 2, 2, 2, 1, 1, 0, 0,
			[5]float32{1.0 / 3, 1.0 / 3, 1.0 / 3, 0, 0},
			1, 2, 1, 4,
			1, 6, 1, 8)
		m := readTestDNGProfileGainTableMap(t, order, field)
		// Mean RGB is 0.25, which maps halfway through each 2-sample table
		// under the DNG input*MapPointsN convention. The top-left pixel center
		// maps to (0.25, 0.25) in the 2x2 spatial grid.
		got, err := m.gain([3]float64{.125, .25, .375}, 0, 0, 2, 2)
		if err != nil || math.Abs(got-2.25) > 1e-6 {
			t.Fatalf("%s interpolated gain=%g err=%v want 2.25", order, got, err)
		}
		// The lower-right sample is a different spatial curve and verifies
		// that image coordinates, not just color, select the map.
		got, err = m.gain([3]float64{.125, .25, .375}, 1, 1, 2, 2)
		if err != nil || math.Abs(got-3.75) > 1e-6 {
			t.Fatalf("%s lower-right gain=%g err=%v want 3.75", order, got, err)
		}
	}
}

func TestDNGProfileGainTableMapWeightsClampAndSingleAxes(t *testing.T) {
	field := testDNGProfileGainTableMapField(binary.LittleEndian, 1, 1, 3, 0, 0, -99, 99,
		[5]float32{0, 0, 0, 1, 0}, 2, 4, 8)
	m := readTestDNGProfileGainTableMap(t, binary.LittleEndian, field)
	if m.spacingV != 1 || m.spacingH != 1 || m.originV != 0 || m.originH != 0 {
		t.Fatal("single-point map axes did not normalize unused geometry")
	}
	for _, tc := range []struct {
		input, want float64
	}{{-1, 2}, {0, 2}, {.5, 6}, {1, 8}, {2, 8}} {
		got, err := m.gain([3]float64{tc.input, tc.input, tc.input}, 0, 0, 10, 10)
		if err != nil || math.Abs(got-tc.want) > 1e-6 {
			t.Fatalf("input %g gain=%g err=%v want %g", tc.input, got, err, tc.want)
		}
	}
	if _, err := m.gain([3]float64{}, 1, 0, 1, 1); err == nil {
		t.Fatal("out-of-range pixel accepted")
	}
}

func TestDNGProfileGainTableMap2AllDataTypesAndPrecedence(t *testing.T) {
	for _, orderCase := range []struct {
		name  string
		order binary.ByteOrder
	}{{"little-endian", binary.LittleEndian}, {"big-endian", binary.BigEndian}} {
		order := orderCase.order
		cases := []struct {
			name     string
			dataType uint32
			samples  []byte
		}{
			{"uint8", 0, []byte{0, 255}},
			{"uint16", 1, func() []byte {
				data := make([]byte, 4)
				order.PutUint16(data, 0)
				order.PutUint16(data[2:], 65535)
				return data
			}()},
			{"float16", 2, func() []byte {
				data := make([]byte, 4)
				order.PutUint16(data, 0x3c00)
				order.PutUint16(data[2:], 0x4200)
				return data
			}()},
			{"float32", 3, func() []byte {
				data := make([]byte, 8)
				order.PutUint32(data, math.Float32bits(1))
				order.PutUint32(data[4:], math.Float32bits(3))
				return data
			}()},
		}
		for _, tc := range cases {
			t.Run(orderCase.name+"/"+tc.name, func(t *testing.T) {
				map2 := testDNGProfileGainTableMap2Field(order, 1, 1, 2, 0, 0, 0, 0,
					[5]float32{1, 0, 0, 0, 0}, tc.dataType, 2, 1, 3, tc.samples)
				legacy := testDNGProfileGainTableMapField(order, 1, 1, 1, 0, 0, 0, 0,
					[5]float32{1, 0, 0, 0, 0}, 9)
				fields, err := findLinearDNG(linearDNGFixture(order, 1, 1, 1, []uint16{1}, map[uint16]dngTestField{
					52525: legacy,
					52544: map2,
				}))
				if err != nil {
					t.Fatal(err)
				}
				m, err := readDNGProfileGainTableMap(fields)
				if err != nil {
					t.Fatal(err)
				}
				got, err := m.gain([3]float64{.5, 0, 0}, 0, 0, 1, 1)
				if err != nil || math.Abs(got-2) > 1e-6 {
					t.Fatalf("%s map2 gain=%g err=%v want 2 (gamma and integer range included)", tc.name, got, err)
				}
			})
		}
	}
}

func TestDNGProfileGainTableMap2RejectsMalformedData(t *testing.T) {
	order := binary.LittleEndian
	validSamples := []byte{0, 255}
	valid := testDNGProfileGainTableMap2Field(order, 1, 1, 2, 0, 0, 0, 0,
		[5]float32{1, 0, 0, 0, 0}, 0, 1, 1, 2, validSamples)
	for name, mutate := range map[string]func(*dngTestField){
		"unsupported-type": func(field *dngTestField) { order.PutUint32(field.data[64:], 4) },
		"gamma-too-low":    func(field *dngTestField) { order.PutUint32(field.data[68:], math.Float32bits(.1)) },
		"nan-gamma":        func(field *dngTestField) { order.PutUint32(field.data[68:], math.Float32bits(float32(math.NaN()))) },
		"reversed-gain-range": func(field *dngTestField) {
			order.PutUint32(field.data[72:], math.Float32bits(3))
			order.PutUint32(field.data[76:], math.Float32bits(2))
		},
		"wrong-sample-count": func(field *dngTestField) {
			field.data = field.data[:len(field.data)-1]
			field.count = uint32(len(field.data))
		},
	} {
		t.Run(name, func(t *testing.T) {
			field := dngTestField{typeID: valid.typeID, count: valid.count, data: append([]byte(nil), valid.data...)}
			mutate(&field)
			fields, err := findLinearDNG(linearDNGFixture(order, 1, 1, 1, []uint16{1}, map[uint16]dngTestField{52544: field}))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := readDNGProfileGainTableMap(fields); err == nil {
				t.Fatal("malformed ProfileGainTableMap2 accepted")
			}
		})
	}
	nanHalf := testDNGProfileGainTableMap2Field(order, 1, 1, 1, 0, 0, 0, 0,
		[5]float32{1, 0, 0, 0, 0}, 2, 1, 0, 0, []byte{0, 0x7e})
	fields, err := findLinearDNG(linearDNGFixture(order, 1, 1, 1, []uint16{1}, map[uint16]dngTestField{52544: nanHalf}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := readDNGProfileGainTableMap(fields); err == nil {
		t.Fatal("NaN half-float gain accepted")
	}
}

func TestLinearDNGProfileGainTableMapAffectsDevelopedPixels(t *testing.T) {
	order := binary.LittleEndian
	field := testDNGProfileGainTableMapField(order, 1, 1, 1, 0, 0, 0, 0,
		[5]float32{1, 0, 0, 0, 0}, 2)
	img, err := decodeLinearDNG(linearDNGFixture(order, 1, 1, 1, []uint16{8192}, map[uint16]dngTestField{52525: field}))
	if err != nil {
		t.Fatal(err)
	}
	const linear = 2 * (8192.0 / 65535)
	encoded := 12.92 * linear
	if linear > .0031308 {
		encoded = 1.055*math.Pow(linear, 1.0/2.4) - .055
	}
	want := math.Round(encoded * 65535)
	pixel := img.NRGBA64At(0, 0)
	for _, channel := range []uint16{pixel.R, pixel.G, pixel.B} {
		if math.Abs(float64(channel)-want) > 2 {
			t.Fatalf("profile gain map pixel=%v want neutral %g", pixel, want)
		}
	}
}

func TestLinearDNGProfileGainTableMap2DevelopsActiveAreaPixels(t *testing.T) {
	order := binary.LittleEndian
	field := testDNGProfileGainTableMap2Field(order, 1, 1, 1, 0, 0, 0, 0,
		[5]float32{1, 0, 0, 0, 0}, 0, 1, 2, 2, []byte{0})
	pixels := make([]uint16, 3*3)
	for index := range pixels {
		pixels[index] = 8192
	}
	img, err := decodeLinearDNG(linearDNGFixture(order, 3, 3, 1, pixels, map[uint16]dngTestField{
		50829: dngTestLong(order, 1, 1, 2, 2),
		52525: testDNGProfileGainTableMapField(order, 1, 1, 1, 0, 0, 0, 0, [5]float32{1, 0, 0, 0, 0}, 9),
		52544: field,
	}))
	if err != nil {
		t.Fatal(err)
	}
	const linear = 2 * (8192.0 / 65535)
	encoded := 12.92 * linear
	if linear > .0031308 {
		encoded = 1.055*math.Pow(linear, 1.0/2.4) - .055
	}
	want := math.Round(encoded * 65535)
	pixel := img.NRGBA64At(0, 0)
	for _, channel := range []uint16{pixel.R, pixel.G, pixel.B} {
		if math.Abs(float64(channel)-want) > 2 {
			t.Fatalf("ProfileGainTableMap2 pixel=%v want neutral %g", pixel, want)
		}
	}
}

func TestLinearDNGProfileGainTableMapUsesActiveAreaRelativeCoordinates(t *testing.T) {
	order := binary.LittleEndian
	field := testDNGProfileGainTableMapField(order, 1, 1, 1, 0, 0, 0, 0,
		[5]float32{1, 0, 0, 0, 0}, 2)
	pixels := make([]uint16, 3*3)
	for index := range pixels {
		pixels[index] = 8192
	}
	img, err := decodeLinearDNG(linearDNGFixture(order, 3, 3, 1, pixels, map[uint16]dngTestField{
		50829: dngTestLong(order, 1, 1, 2, 2),
		52525: field,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 1 || img.Bounds().Dy() != 1 {
		t.Fatalf("developed bounds=%v, want 1x1 active area", img.Bounds())
	}
	const linear = 2 * (8192.0 / 65535)
	encoded := 12.92 * linear
	if linear > .0031308 {
		encoded = 1.055*math.Pow(linear, 1.0/2.4) - .055
	}
	want := math.Round(encoded * 65535)
	pixel := img.NRGBA64At(0, 0)
	if math.Abs(float64(pixel.R)-want) > 2 || math.Abs(float64(pixel.G)-want) > 2 || math.Abs(float64(pixel.B)-want) > 2 {
		t.Fatalf("profile gain map pixel=%v want neutral %g", pixel, want)
	}
}

func TestDNGProfileGainTableMapRejectsMalformedData(t *testing.T) {
	order := binary.LittleEndian
	valid := testDNGProfileGainTableMapField(order, 2, 1, 2, 1, 0, 0, 0, [5]float32{1, 0, 0, 0, 0}, 1, 2, 1, 2)
	for name, mutate := range map[string]func(*dngTestField){
		"wrong-type": func(field *dngTestField) { field.typeID = 4 },
		"short-header": func(field *dngTestField) {
			field.data, field.count = field.data[:63], 63
		},
		"wrong-sample-count": func(field *dngTestField) {
			field.data = field.data[:len(field.data)-4]
			field.count = uint32(len(field.data))
		},
		"zero-rows":       func(field *dngTestField) { order.PutUint32(field.data, 0) },
		"zero-columns":    func(field *dngTestField) { order.PutUint32(field.data[4:], 0) },
		"zero-points":     func(field *dngTestField) { order.PutUint32(field.data[40:], 0) },
		"zero-spacing":    func(field *dngTestField) { order.PutUint64(field.data[8:], 0) },
		"nan-origin":      func(field *dngTestField) { order.PutUint64(field.data[24:], math.Float64bits(math.NaN())) },
		"infinite-weight": func(field *dngTestField) { order.PutUint32(field.data[44:], math.Float32bits(float32(math.Inf(1)))) },
		"negative-gain":   func(field *dngTestField) { order.PutUint32(field.data[64:], math.Float32bits(-1)) },
		"nan-gain":        func(field *dngTestField) { order.PutUint32(field.data[64:], math.Float32bits(float32(math.NaN()))) },
		"oversized": func(field *dngTestField) {
			order.PutUint32(field.data, math.MaxUint32)
			order.PutUint32(field.data[4:], math.MaxUint32)
			order.PutUint32(field.data[40:], math.MaxUint32)
		},
	} {
		t.Run(name, func(t *testing.T) {
			field := dngTestField{typeID: valid.typeID, count: valid.count, data: append([]byte(nil), valid.data...)}
			mutate(&field)
			fields, err := findLinearDNG(linearDNGFixture(order, 1, 1, 1, []uint16{1}, map[uint16]dngTestField{52525: field}))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := readDNGProfileGainTableMap(fields); err == nil {
				t.Fatal("malformed profile gain map accepted")
			}
		})
	}
}

func FuzzDNGProfileGainTableMap(f *testing.F) {
	for _, count := range []uint32{0, 63, 64, 68, 128} {
		f.Add(count, []byte{0, 0, 0, 1})
	}
	f.Fuzz(func(t *testing.T, count uint32, payload []byte) {
		if count > maxDNGProfileGainTableMapBytes || uint64(len(payload)) > maxDNGProfileGainTableMapBytes {
			t.Skip()
		}
		if uint64(count) > uint64(len(payload)) {
			payload = append(payload, make([]byte, int(count)-len(payload))...)
		}
		if uint64(len(payload)) > uint64(count) {
			payload = payload[:count]
		}
		fields, err := findLinearDNG(linearDNGFixture(binary.LittleEndian, 1, 1, 1, []uint16{1}, map[uint16]dngTestField{
			52525: {typeID: 7, count: count, data: payload},
		}))
		if err != nil {
			return
		}
		_, _ = readDNGProfileGainTableMap(fields)
	})
}

func FuzzDNGProfileGainTableMap2(f *testing.F) {
	for _, count := range []uint32{0, 79, 80, 81, 82, 84} {
		f.Add(count, []byte{0, 0, 0, 1})
	}
	f.Fuzz(func(t *testing.T, count uint32, payload []byte) {
		if count > maxDNGProfileGainTableMapBytes || uint64(len(payload)) > maxDNGProfileGainTableMapBytes {
			t.Skip()
		}
		if uint64(count) > uint64(len(payload)) {
			payload = append(payload, make([]byte, int(count)-len(payload))...)
		}
		if uint64(len(payload)) > uint64(count) {
			payload = payload[:count]
		}
		fields, err := findLinearDNG(linearDNGFixture(binary.LittleEndian, 1, 1, 1, []uint16{1}, map[uint16]dngTestField{
			52544: {typeID: 7, count: count, data: payload},
		}))
		if err != nil {
			return
		}
		_, _ = readDNGProfileGainTableMap(fields)
	})
}
