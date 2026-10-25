package lrtl

import (
	"encoding/binary"
	"math"
	"testing"
)

var testDNGTripleIlluminants = [3][2]float64{
	{0.4476, 0.4074}, // Standard Light A.
	{0.3457, 0.3585}, // D50.
	{0.3127, 0.3290}, // D65.
}

func TestDNGTripleIlluminantWeightsAdobeReference(t *testing.T) {
	for _, test := range []struct {
		name  string
		white [2]float64
		want  [3]float64
	}{
		{"exact middle illuminant", testDNGTripleIlluminants[1], [3]float64{0, 1, 0}},
		{"exact third illuminant", testDNGTripleIlluminants[2], [3]float64{0, 0, 1}},
		{"between D50 and D65", [2]float64{0.3292, 0.3426}, [3]float64{0, 0.5294527197751462, 0.4705472802248538}},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := dngTripleIlluminantWeights(test.white, testDNGTripleIlluminants)
			if err != nil {
				t.Fatal(err)
			}
			for i := range got {
				if math.Abs(got[i]-test.want[i]) > 1e-10 {
					t.Fatalf("weights=%v want %v", got, test.want)
				}
			}
			if math.Abs(got[0]+got[1]+got[2]-1) > 1e-15 {
				t.Fatalf("weights do not sum to one: %v", got)
			}
		})
	}
	if _, err := dngTripleIlluminantWeights([2]float64{0, 0}, testDNGTripleIlluminants); err == nil {
		t.Fatal("invalid white point accepted")
	}
}

func FuzzDNGTripleIlluminantWeights(f *testing.F) {
	f.Add(.3292, .3426)
	f.Add(.3127, .3290)
	f.Add(0.0, 0.0)
	f.Fuzz(func(t *testing.T, x, y float64) {
		if math.IsNaN(x) || math.IsNaN(y) || math.IsInf(x, 0) || math.IsInf(y, 0) || math.Abs(x) > 2 || math.Abs(y) > 2 {
			return
		}
		weights, err := dngTripleIlluminantWeights([2]float64{x, y}, testDNGTripleIlluminants)
		if err != nil {
			return
		}
		sum := 0.0
		for _, weight := range weights {
			if math.IsNaN(weight) || math.IsInf(weight, 0) || weight < 0 || weight > 1 {
				t.Fatalf("invalid weight %g for xy (%g,%g)", weight, x, y)
			}
			sum += weight
		}
		if math.Abs(sum-1) > 1e-12 {
			t.Fatalf("weights sum to %g for xy (%g,%g)", sum, x, y)
		}
	})
}

func TestDNGProfileMapTripleIlluminantInterpolation(t *testing.T) {
	first := &dngProfileMap{1, 2, 1, false, [][3]float64{{0, 1, 1}, {0, 1, .5}}}
	second := &dngProfileMap{1, 2, 1, false, [][3]float64{{60, 2, 1}, {60, 2, 1.5}}}
	third := &dngProfileMap{1, 2, 1, false, [][3]float64{{120, 4, 1}, {120, 4, 2}}}
	maps := dngProfileMaps{first: first, second: second, third: third}
	weights := [3]float64{.25, .25, .5}
	got := maps.hueMapWeights(weights)
	if got == nil {
		t.Fatal("triple table interpolation returned nil")
	}
	for i, want := range [][3]float64{{75, 2.75, 1}, {75, 2.75, 1.5}} {
		if got.data[i] != want {
			t.Fatalf("interpolated entry %d=%v want %v", i, got.data[i], want)
		}
	}
	if first.data[1] != [3]float64{0, 1, .5} || second.data[1] != [3]float64{60, 2, 1.5} || third.data[1] != [3]float64{120, 4, 2} {
		t.Fatal("interpolation mutated the source tables")
	}
}

func tripleHueMapFields(order binary.ByteOrder) map[uint16]dngTestField {
	metadata := testDNGProfileMapFields(order, 50937, 50938, 51107, 1, 2, 1, false, func(_, _, _ int) [3]float32 {
		return [3]float32{0, 1, 1}
	})
	second := testDNGProfileMapFields(order, 50937, 50939, 51107, 1, 2, 1, false, func(_, _, _ int) [3]float32 {
		return [3]float32{60, 1, 1}
	})
	third := testDNGProfileMapFields(order, 50937, 52537, 51107, 1, 2, 1, false, func(_, _, _ int) [3]float32 {
		return [3]float32{120, 1, 1}
	})
	metadata[50939], metadata[52537] = second[50939], third[52537]
	return metadata
}

func tripleIlluminantDNGMetadata(order binary.ByteOrder) map[uint16]dngTestField {
	metadata := tripleHueMapFields(order)
	identity := dngTestSRationalMatrix(order, [9]int32{1, 0, 0, 0, 1, 0, 0, 0, 1})
	metadata[50721], metadata[50722], metadata[52531] = identity, identity, identity
	metadata[50728] = dngTestDouble(order, 1, 1, 1)
	metadata[50778], metadata[50779], metadata[52529] = dngTestShort(order, 17), dngTestShort(order, 23), dngTestShort(order, 21)
	return metadata
}

func TestLinearDNGTripleProfileMapsMatchWeightedReference(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		metadata := tripleIlluminantDNGMetadata(order)
		pixels := []uint16{16384, 0, 0}
		tripleData := linearDNGFixture(order, 1, 1, 3, pixels, metadata)
		fields, err := findLinearDNG(tripleData)
		if err != nil {
			t.Fatal(err)
		}
		_, weights, err := dngCameraToSpaceWithWeights(fields, dngXYZD50ToSRGB)
		if err != nil {
			t.Fatal(err)
		}

		// Build an equivalent one-table DNG from the three weighted H/S tables.
		// This independently checks that the decoder selects and applies Data3
		// with the exact weights returned by its color-calibration stage.
		shift := float32(60*weights[1] + 120*weights[2])
		baselineMetadata := map[uint16]dngTestField{
			50721: metadata[50721], 50728: metadata[50728], 50778: metadata[50778],
		}
		baselineMap := testDNGProfileMapFields(order, 50937, 50938, 51107, 1, 2, 1, false, func(_, _, _ int) [3]float32 {
			return [3]float32{shift, 1, 1}
		})
		for tag, field := range baselineMap {
			baselineMetadata[tag] = field
		}
		baselineData := linearDNGFixture(order, 1, 1, 3, pixels, baselineMetadata)

		got, err := decodeLinearDNG(tripleData)
		if err != nil {
			t.Fatalf("triple profile decode: %v", err)
		}
		want, err := decodeLinearDNG(baselineData)
		if err != nil {
			t.Fatalf("weighted reference decode: %v", err)
		}
		gotPixel, wantPixel := got.NRGBA64At(0, 0), want.NRGBA64At(0, 0)
		for channel, pair := range [][2]uint16{{gotPixel.R, wantPixel.R}, {gotPixel.G, wantPixel.G}, {gotPixel.B, wantPixel.B}} {
			if delta := math.Abs(float64(pair[0]) - float64(pair[1])); delta > 1 {
				t.Fatalf("byte order %T channel %d: triple pixel=%v weighted reference=%v weights=%v", order, channel, gotPixel, wantPixel, weights)
			}
		}
	}
}

func TestLinearDNGTripleProfileMapRequiresThreeTablesAndMatrices(t *testing.T) {
	order := binary.LittleEndian
	for name, edit := range map[string]func(map[uint16]dngTestField){
		"missing second map":       func(m map[uint16]dngTestField) { delete(m, 50939) },
		"missing first map":        func(m map[uint16]dngTestField) { delete(m, 50938) },
		"missing third matrix":     func(m map[uint16]dngTestField) { delete(m, 52531) },
		"missing third illuminant": func(m map[uint16]dngTestField) { delete(m, 52529) },
		"duplicate illuminants":    func(m map[uint16]dngTestField) { m[52529] = m[50779] },
		"bad third table count": func(m map[uint16]dngTestField) {
			m[52537] = dngTestFloat(order, 120, 1)
		},
		"partial forward matrices": func(m map[uint16]dngTestField) { m[50964] = dngTestDouble(order, 1, 0, 0, 0, 1, 0, 0, 0, 1) },
		"partial reduction matrices": func(m map[uint16]dngTestField) {
			m[50725] = dngTestSRationalMatrix(order, [9]int32{1, 0, 0, 0, 1, 0, 0, 0, 1})
		},
	} {
		t.Run(name, func(t *testing.T) {
			metadata := tripleIlluminantDNGMetadata(order)
			edit(metadata)
			if _, err := decodeLinearDNG(linearDNGFixture(order, 1, 1, 3, []uint16{16384, 0, 0}, metadata)); err == nil {
				t.Fatal("incomplete triple-illuminant profile was accepted")
			}
		})
	}
}

func TestLinearDNGTripleForwardMatricesUseSharedWeights(t *testing.T) {
	order := binary.LittleEndian
	metadata := tripleIlluminantDNGMetadata(order)
	metadata[50964] = dngTestDouble(order, 1, 0, 0, 0, 1, 0, 0, 0, 1)
	metadata[50965] = dngTestDouble(order, 2, 0, 0, 0, 1, 0, 0, 0, 1)
	metadata[52532] = dngTestDouble(order, 3, 0, 0, 0, 1, 0, 0, 0, 1)
	fields, err := findLinearDNG(linearDNGFixture(order, 1, 1, 3, []uint16{16384, 0, 0}, metadata))
	if err != nil {
		t.Fatal(err)
	}
	transform, weights, err := dngCameraToSpaceWithWeights(fields, dngXYZD50ToSRGB)
	if err != nil {
		t.Fatal(err)
	}
	wantScale := weights[0] + 2*weights[1] + 3*weights[2]
	wantForward := dngMatrix{{wantScale, 0, 0}, {0, 1, 0}, {0, 0, 1}}
	want := dngXYZD50ToSRGB.multiply(wantForward)
	for row := range transform {
		for column := range transform[row] {
			if math.Abs(transform[row][column]-want[row][column]) > 1e-12 {
				t.Fatalf("triple ForwardMatrix[%d][%d]=%g want %g (weights %v)", row, column, transform[row][column], want[row][column], weights)
			}
		}
	}
}

func TestLinearDNGTripleColorMatricesUseSharedWeights(t *testing.T) {
	order := binary.LittleEndian
	metadata := tripleIlluminantDNGMetadata(order)
	metadata[50721] = dngTestDouble(order, 1, 0, 0, 0, 1, 0, 0, 0, 1)
	metadata[50722] = dngTestDouble(order, 2, 0, 0, 0, 1, 0, 0, 0, 1)
	metadata[52531] = dngTestDouble(order, 3, 0, 0, 0, 1, 0, 0, 0, 1)
	fields, err := findLinearDNG(linearDNGFixture(order, 1, 1, 3, []uint16{16384, 0, 0}, metadata))
	if err != nil {
		t.Fatal(err)
	}
	transform, weights, err := dngCameraToSpaceWithWeights(fields, dngXYZD50ToSRGB)
	if err != nil {
		t.Fatal(err)
	}
	color := dngMatrix{{weights[0] + 2*weights[1] + 3*weights[2], 0, 0}, {0, 1, 0}, {0, 0, 1}}
	xyzFromCamera, err := color.inverse()
	if err != nil {
		t.Fatal(err)
	}
	white := xyzFromCamera.vector([3]float64{1, 1, 1})
	for row := range xyzFromCamera {
		for column := range xyzFromCamera[row] {
			xyzFromCamera[row][column] /= white[1]
		}
	}
	white = [3]float64{white[0] / white[1], 1, white[2] / white[1]}
	adaptation, err := dngChromaticAdaptation(white, dngWhiteD50)
	if err != nil {
		t.Fatal(err)
	}
	want := dngXYZD50ToSRGB.multiply(adaptation).multiply(xyzFromCamera)
	for row := range transform {
		for column := range transform[row] {
			if math.Abs(transform[row][column]-want[row][column]) > 1e-12 {
				t.Fatalf("triple ColorMatrix[%d][%d]=%g want %g (weights %v)", row, column, transform[row][column], want[row][column], weights)
			}
		}
	}
}

func TestLinearDNGTripleCameraCalibrationUsesSharedWeights(t *testing.T) {
	order := binary.LittleEndian
	metadata := tripleIlluminantDNGMetadata(order)
	calibrations := [3]dngMatrix{
		dngIdentityMatrix,
		{{2, .1, 0}, {0, 1, .1}, {0, 0, 1}},
		{{1, 0, .1}, {.1, 2, 0}, {0, 0, 1}},
	}
	metadata[50723] = dngTestDouble(order, 1, 0, 0, 0, 1, 0, 0, 0, 1)
	metadata[50724] = dngTestDouble(order, 2, .1, 0, 0, 1, .1, 0, 0, 1)
	metadata[52530] = dngTestDouble(order, 1, 0, .1, .1, 2, 0, 0, 0, 1)
	metadata[50964] = dngTestDouble(order, 1, 0, 0, 0, 1, 0, 0, 0, 1)
	metadata[50965] = metadata[50964]
	metadata[52532] = metadata[50964]
	metadata[50728] = dngTestDouble(order, 1, 2, 3)
	fields, err := findLinearDNG(linearDNGFixture(order, 1, 1, 3, []uint16{16384, 0, 0}, metadata))
	if err != nil {
		t.Fatal(err)
	}
	transform, weights, err := dngCameraToSpaceWithWeights(fields, dngXYZD50ToSRGB)
	if err != nil {
		t.Fatal(err)
	}
	var mixed dngMatrix
	for row := range mixed {
		for column := range mixed[row] {
			for index := range weights {
				mixed[row][column] += weights[index] * calibrations[index][row][column]
			}
		}
	}
	nativeToReference, err := mixed.inverse()
	if err != nil {
		t.Fatal(err)
	}
	neutral := [3]float64{1, 2, 3}
	referenceNeutral := nativeToReference.vector(neutral)
	var whiteBalance [3]float64
	for index, value := range referenceNeutral {
		whiteBalance[index] = 1 / value
	}
	want := dngXYZD50ToSRGB.multiply(dngDiagonal(whiteBalance)).multiply(nativeToReference)
	for row := range transform {
		for column := range transform[row] {
			if math.Abs(transform[row][column]-want[row][column]) > 1e-12 {
				t.Fatalf("triple CameraCalibration[%d][%d]=%g want %g (weights %v)", row, column, transform[row][column], want[row][column], weights)
			}
		}
	}
}

func TestLinearDNGTripleAsShotNeutralPrecedesWhiteXY(t *testing.T) {
	order := binary.LittleEndian
	metadata := tripleIlluminantDNGMetadata(order)
	metadata[50729] = dngTestDouble(order, testDNGTripleIlluminants[2][0], testDNGTripleIlluminants[2][1])
	fields, err := findLinearDNG(linearDNGFixture(order, 1, 1, 3, []uint16{16384, 0, 0}, metadata))
	if err != nil {
		t.Fatal(err)
	}
	_, got, err := dngCameraToSpaceWithWeights(fields, dngXYZD50ToSRGB)
	if err != nil {
		t.Fatal(err)
	}
	want := [3]float64{0.006694578082653913, 0.5423545910114284, 0.4509508309059177}
	for index := range got {
		if math.Abs(got[index]-want[index]) > 1e-9 {
			t.Fatalf("weights with both AsShot tags=%v want neutral-derived %v", got, want)
		}
	}
}

func TestLinearDNGTripleProfileSelectedFromCameraProfileIFD(t *testing.T) {
	mainOrder, profileOrder := binary.LittleEndian, binary.BigEndian
	rootMetadata := map[uint16]dngTestField{
		50933: dngTestLong(mainOrder, 0), 50934: dngTestASCII("Night"), 50936: dngTestASCII("Default"),
		50728: dngTestDouble(mainOrder, 1, 1, 1),
		52530: dngTestSRationalMatrix(mainOrder, [9]int32{1, 0, 0, 0, 1, 0, 0, 0, 1}),
	}
	profileFields := tripleIlluminantDNGMetadata(profileOrder)
	delete(profileFields, 50728) // AsShotNeutral belongs to the raw IFD.
	profileFields[50936] = dngTestASCII("Night")
	profileFields[50778] = dngTestShort(profileOrder, 255)
	profileFields[52533] = dngTestIlluminantXY(profileOrder, .4476, .4074)
	data := linearDNGFixture(mainOrder, 1, 1, 3, []uint16{16384, 0, 0}, rootMetadata)
	data = appendDNGCameraProfile(t, data, mainOrder, profileOrder, "Night", profileFields)

	got, err := decodeLinearDNGWithProfile(data, "Night")
	if err != nil {
		t.Fatal(err)
	}

	// The same tags in the root IFD provide a byte-order-independent reference.
	referenceMetadata := tripleIlluminantDNGMetadata(mainOrder)
	referenceMetadata[50778] = dngTestShort(mainOrder, 255)
	referenceMetadata[52533] = dngTestIlluminantXY(mainOrder, .4476, .4074)
	reference, err := decodeLinearDNG(linearDNGFixture(mainOrder, 1, 1, 3, []uint16{16384, 0, 0}, referenceMetadata))
	if err != nil {
		t.Fatal(err)
	}
	gotPixel, wantPixel := got.NRGBA64At(0, 0), reference.NRGBA64At(0, 0)
	for channel, pair := range [][2]uint16{{gotPixel.R, wantPixel.R}, {gotPixel.G, wantPixel.G}, {gotPixel.B, wantPixel.B}} {
		if delta := math.Abs(float64(pair[0]) - float64(pair[1])); delta > 1 {
			t.Fatalf("selected profile IFD channel %d: got %v want %v", channel, gotPixel, wantPixel)
		}
	}
}

func dngTestIlluminantXY(order binary.ByteOrder, x, y float64) dngTestField {
	data := make([]byte, 18)
	order.PutUint16(data, 0)
	order.PutUint32(data[2:], uint32(math.Round(x*10000)))
	order.PutUint32(data[6:], 10000)
	order.PutUint32(data[10:], uint32(math.Round(y*10000)))
	order.PutUint32(data[14:], 10000)
	return dngTestField{typeID: 7, count: uint32(len(data)), data: data}
}

func TestDNGCustomIlluminantDataWhitePoint(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		data := dngTestIlluminantXY(order, .3292, .3426)
		metadata := map[uint16]dngTestField{
			52529: dngTestShort(order, 255),
			52535: data,
		}
		fields, err := findLinearDNG(linearDNGFixture(order, 1, 1, 1, []uint16{1}, metadata))
		if err != nil {
			t.Fatal(err)
		}
		got, err := dngIlluminantWhiteXY(fields, 52529, 52535)
		if err != nil {
			t.Fatal(err)
		}
		if got != [2]float64{.3292, .3426} {
			t.Fatalf("custom illuminant white=%v", got)
		}

		spectral := data
		order.PutUint16(spectral.data, 1) // Spectral data is deliberately rejected for now.
		metadata[52535] = spectral
		fields, err = findLinearDNG(linearDNGFixture(order, 1, 1, 1, []uint16{1}, metadata))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := dngIlluminantWhiteXY(fields, 52529, 52535); err == nil {
			t.Fatal("unsupported spectral IlluminantData accepted")
		}
	}
}

func TestDNGStandardLightISOStudioTungstenWhite(t *testing.T) {
	order := binary.BigEndian
	fields, err := findLinearDNG(linearDNGFixture(order, 1, 1, 1, []uint16{1}, map[uint16]dngTestField{
		50778: dngTestShort(order, 24),
	}))
	if err != nil {
		t.Fatal(err)
	}
	white, err := dngIlluminantWhiteXY(fields, 50778, 52533)
	if err != nil {
		t.Fatal(err)
	}
	temperature, tint, err := dngTemperatureTint(white)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(temperature-3200) > 1 || math.Abs(tint) > .01 {
		t.Fatalf("ISO Studio Tungsten white gives %g K, tint %g: %v", temperature, tint, white)
	}
}
