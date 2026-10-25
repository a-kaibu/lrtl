package lrtl

import (
	"encoding/binary"
	"fmt"
	"image/jpeg"
	"math"
	"os"
	"sort"
	"testing"
)

type dngTestField struct {
	typeID uint16
	count  uint32
	data   []byte
}

func dngTestLong(order binary.ByteOrder, values ...uint32) dngTestField {
	data := make([]byte, len(values)*4)
	for index, value := range values {
		order.PutUint32(data[index*4:], value)
	}
	return dngTestField{4, uint32(len(values)), data}
}

func dngTestShort(order binary.ByteOrder, values ...uint16) dngTestField {
	data := make([]byte, len(values)*2)
	for index, value := range values {
		order.PutUint16(data[index*2:], value)
	}
	return dngTestField{3, uint32(len(values)), data}
}

func dngTestDouble(order binary.ByteOrder, values ...float64) dngTestField {
	data := make([]byte, len(values)*8)
	for index, value := range values {
		order.PutUint64(data[index*8:], math.Float64bits(value))
	}
	return dngTestField{12, uint32(len(values)), data}
}

// Build an independent minimal TIFF/DNG container with known sensor samples.
// No proprietary camera data or Adobe executable is needed for these tests.
func linearDNGFixture(order binary.ByteOrder, width, height, channels int, pixels []uint16, extra map[uint16]dngTestField) []byte {
	depths := make([]uint16, channels)
	for index := range depths {
		depths[index] = 16
	}
	fields := map[uint16]dngTestField{
		254: dngTestLong(order, 0), 256: dngTestLong(order, uint32(width)), 257: dngTestLong(order, uint32(height)),
		258: dngTestShort(order, depths...), 259: dngTestShort(order, 1), 262: dngTestShort(order, 34892),
		273: dngTestLong(order, 0), 277: dngTestShort(order, uint16(channels)), 278: dngTestLong(order, uint32(height)),
		279: dngTestLong(order, uint32(len(pixels)*2)), 284: dngTestShort(order, 1),
		50706: {1, 4, []byte{1, 4, 0, 0}},
	}
	for tag, field := range extra {
		if field.typeID == 0 {
			delete(fields, tag)
		} else {
			fields[tag] = field
		}
	}
	tags := make([]int, 0, len(fields))
	for tag := range fields {
		tags = append(tags, int(tag))
	}
	sort.Ints(tags)
	data := make([]byte, 8+2+len(tags)*12+4)
	if order == binary.LittleEndian {
		copy(data, "II")
	} else {
		copy(data, "MM")
	}
	order.PutUint16(data[2:], 42)
	order.PutUint32(data[4:], 8)
	order.PutUint16(data[8:], uint16(len(tags)))
	locations := map[uint16]int{}
	for index, tag := range tags {
		field := fields[uint16(tag)]
		start := 10 + index*12
		order.PutUint16(data[start:], uint16(tag))
		order.PutUint16(data[start+2:], field.typeID)
		order.PutUint32(data[start+4:], field.count)
		location := start + 8
		if len(field.data) > 4 {
			location = len(data)
			order.PutUint32(data[start+8:], uint32(location))
			data = append(data, field.data...)
		} else {
			copy(data[start+8:start+12], field.data)
		}
		locations[uint16(tag)] = location
	}
	if location, ok := locations[273]; ok {
		order.PutUint32(data[location:], uint32(len(data)))
	}
	if location, ok := locations[324]; ok {
		lengths := fields[325]
		start := len(data)
		for index := uint32(0); index < fields[324].count; index++ {
			order.PutUint32(data[location+int(index)*4:], uint32(start))
			start += int(order.Uint32(lengths.data[int(index)*4:]))
		}
	}
	for _, pixel := range pixels {
		var encoded [2]byte
		order.PutUint16(encoded[:], pixel)
		data = append(data, encoded[:]...)
	}
	return data
}

func TestLinearDNGBlackWhiteNormalizationAndGamma(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		t.Run(order.String(), func(t *testing.T) {
			data := linearDNGFixture(order, 2, 2, 1, []uint16{1000, 3500, 6000, 11000}, map[uint16]dngTestField{
				50714: dngTestDouble(order, 1000), 50717: dngTestLong(order, 11000),
			})
			img, err := decodeLinearDNG(data)
			if err != nil {
				t.Fatal(err)
			}
			for index, want := range []uint16{0, 35199, 48192, 65535} {
				pixel := img.NRGBA64At(index%2, index/2)
				if pixel.R != want || pixel.G != want || pixel.B != want || pixel.A != 65535 {
					t.Errorf("pixel%d=%v want gray %d", index, pixel, want)
				}
			}
		})
	}
}

func TestLinearDNGDefaultCrop(t *testing.T) {
	order := binary.LittleEndian
	data := linearDNGFixture(order, 2, 2, 1, []uint16{0, 16384, 32768, 65535}, map[uint16]dngTestField{
		50719: dngTestDouble(order, 1, 0), 50720: dngTestDouble(order, 1, 2),
	})
	img, err := decodeLinearDNG(data)
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 1 || img.Bounds().Dy() != 2 || img.NRGBA64At(0, 1).R != 65535 {
		t.Fatalf("incorrect default crop: %v", img.Bounds())
	}
}

func TestLinearDNGTiledPixels(t *testing.T) {
	order := binary.LittleEndian
	// Four 2x2 padded tiles form a 3x3 image. Padding has deliberately
	// different values so a row/column-stride bug changes visible pixels.
	pixels := []uint16{0, 0, 0, 0, 65535, 999, 65535, 999, 0, 0, 999, 999, 65535, 999, 999, 999}
	data := linearDNGFixture(order, 3, 3, 1, pixels, map[uint16]dngTestField{
		273: {}, 279: {}, 322: dngTestLong(order, 2), 323: dngTestLong(order, 2),
		324: dngTestLong(order, 0, 0, 0, 0), 325: dngTestLong(order, 8, 8, 8, 8),
	})
	img, err := decodeLinearDNG(data)
	if err != nil {
		t.Fatal(err)
	}
	for y := 0; y < 3; y++ {
		for x := 0; x < 3; x++ {
			want := uint16(0)
			if x == 2 {
				want = 65535
			}
			if got := img.NRGBA64At(x, y).R; got != want {
				t.Errorf("(%d,%d)=%d want %d", x, y, got, want)
			}
		}
	}
}

func TestLinearDNGRejectsInvalidMetadata(t *testing.T) {
	order := binary.LittleEndian
	for name, extra := range map[string]map[uint16]dngTestField{
		"compressed": {259: dngTestShort(order, 7)}, "mosaic": {262: dngTestShort(order, 32803)},
		"huge_dimensions": {256: dngTestLong(order, 0xffffffff)}, "zero_white": {50717: dngTestLong(order, 0)},
		"nan_black": {50714: dngTestDouble(order, math.NaN())}, "short_strip": {279: dngTestLong(order, 1)},
		"bad_crop":        {50719: dngTestDouble(order, 4, 0), 50720: dngTestDouble(order, 1, 1)},
		"missing_version": {50706: {}}, "cycle": {330: dngTestLong(order, 8), 262: dngTestShort(order, 2)},
		"reversed_active_area": {50829: dngTestLong(order, 2, 0, 0, 2)},
		"huge_crop":            {50719: dngTestDouble(order, 1e100, 0), 50720: dngTestDouble(order, 1, 1)},
		"bad_scale":            {50718: dngTestDouble(order, 9, 1)},
	} {
		t.Run(name, func(t *testing.T) {
			data := linearDNGFixture(order, 2, 2, 1, []uint16{0, 1, 2, 3}, extra)
			if _, err := decodeLinearDNG(data); err == nil {
				t.Fatal("invalid metadata accepted")
			}
		})
	}
	data := linearDNGFixture(order, 2, 2, 1, []uint16{0, 1, 2, 3}, nil)
	for _, length := range []int{0, 7, 15, len(data) - 1} {
		if _, err := decodeLinearDNG(data[:length]); err == nil {
			t.Fatalf("truncated file of %d bytes accepted", length)
		}
	}
}

func TestLinearDNGCameraWhiteBalanceAndColorMatrix(t *testing.T) {
	order := binary.LittleEndian
	for _, forward := range []bool{true, false} {
		t.Run(fmt.Sprint("forward=", forward), func(t *testing.T) {
			metadata := map[uint16]dngTestField{339: dngTestShort(order, 1, 1, 1)}
			var pixels []uint16
			if forward {
				// Camera neutral [.5,1,.25], exposed to one quarter white,
				// should become a display-neutral quarter-linear gray.
				metadata[50728] = dngTestDouble(order, .5, 1, .25)
				metadata[50964] = dngTestDouble(order, .96422, 0, 0, 0, 1, 0, 0, 0, .82521)
				pixels = []uint16{8192, 16384, 4096}
			} else {
				// Identity ColorMatrix puts samples directly in XYZ. A D50
				// neutral uses the fallback matrix/adaptation path.
				metadata[50721] = dngTestDouble(order, 1, 0, 0, 0, 1, 0, 0, 0, 1)
				metadata[50728] = dngTestDouble(order, .96422, 1, .82521)
				pixels = []uint16{15798, 16384, 13520}
			}
			data := linearDNGFixture(order, 1, 1, 3, pixels, metadata)
			img, err := decodeLinearDNG(data)
			if err != nil {
				t.Fatal(err)
			}
			pixel := img.NRGBA64At(0, 0)
			for _, value := range []uint16{pixel.R, pixel.G, pixel.B} {
				if math.Abs(float64(value)-35199) > 50 {
					t.Fatalf("white balance/color transform did not preserve gray: %v", pixel)
				}
			}
		})
	}
}

func FuzzLinearDNG(f *testing.F) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		f.Add(linearDNGFixture(order, 1, 1, 1, []uint16{8192}, map[uint16]dngTestField{50730: dngTestDouble(order, -.5), 51109: dngTestSRational(order, 3, 2)}))
	}
	f.Add(profileMapDNGFixture(binary.LittleEndian))
	f.Add(profileMapDNGFixture(binary.BigEndian))
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		f.Add(linearDNGFixture(order, 1, 1, 3, []uint16{16384, 0, 0}, tripleIlluminantDNGMetadata(order)))
	}
	maskOrder := binary.LittleEndian
	maskRed := dngTestRGBTableData(maskOrder, 0, "subject", 2, 2, 0, 4, 0, func(_, _, _ int) [3]float64 { return [3]float64{1, 0, 0} })
	maskBackground := dngTestRGBTableData(maskOrder, 0, "", 2, 2, 0, 4, 0, func(_, _, _ int) [3]float64 { return [3]float64{0, 0, 1} })
	maskMetadata := dngRGBIdentityCameraMetadata(maskOrder, dngTestRGBTablesData(maskOrder, 0, maskRed, maskBackground))
	maskFixture := linearDNGFixture(maskOrder, 2, 1, 3, []uint16{8192, 16384, 24576, 24576, 32768, 40960}, maskMetadata)
	maskFixture = appendDNGSemanticMaskIFD(f, maskFixture, maskOrder, "subject", 2, 1, 8, 1, 1, 1, 0, 0, []byte{255, 0}, nil)
	f.Add(maskFixture)
	f.Add(profileToneDNGFixture(binary.LittleEndian, 0))
	f.Add(profileToneDNGFixture(binary.BigEndian, 1))
	profileIFD := linearDNGFixture(binary.LittleEndian, 1, 1, 1, []uint16{8192}, map[uint16]dngTestField{
		50933: dngTestLong(binary.LittleEndian, 0), 50934: dngTestASCII("Fuzz Profile"),
		50936: dngTestASCII("Default"), 52544: testProfileGainMap2(binary.LittleEndian, 1),
	})
	profileIFD = appendDNGCameraProfile(f, profileIFD, binary.LittleEndian, binary.BigEndian, "Fuzz Profile", map[uint16]dngTestField{
		52544: testProfileGainMap2(binary.BigEndian, 2),
	})
	f.Add(profileIFD)
	f.Add(warpDNGFixture(binary.LittleEndian))
	f.Add(warpDNGFixture(binary.BigEndian))
	f.Add(mixedDNGPointFixture(binary.LittleEndian, 2, 2))
	f.Add(mixedDNGPointFixture(binary.BigEndian, 2, 2))
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		f.Add(linearDNGFixture(order, 2, 2, 1, []uint16{1024, 2048, 4096, 8192}, map[uint16]dngTestField{51022: testDNGOpcodeList(testDNGGainMap(testDNGGainMapPayload(2, 2, 2, 2, 1, 1, 2, 3, 4)))}))
	}
	f.Add(blackPatternDNGFixture(f, binary.LittleEndian))
	f.Add(blackPatternDNGFixture(f, binary.BigEndian))
	f.Add([]byte("II"))
	f.Add(linearDNGFixture(binary.LittleEndian, 2, 2, 1, []uint16{0, 16384, 32768, 65535}, nil))
	f.Add(linearDNGFixture(binary.BigEndian, 2, 2, 1, []uint16{0, 16384, 32768, 65535}, nil))
	f.Add(linearDNGFixture(binary.LittleEndian, 1, 1, 3, []uint16{8192, 16384, 4096}, map[uint16]dngTestField{
		50728: dngTestDouble(binary.LittleEndian, .5, 1, .25), 50964: dngTestDouble(binary.LittleEndian, .96422, 0, 0, 0, 1, 0, 0, 0, .82521),
	}))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 64<<10 {
			t.Skip()
		}
		_, _ = decodeLinearDNG(data)
	})
}

func TestDNGColorMatrixInversionAndWhiteAdaptation(t *testing.T) {
	matrix := dngMatrix{{1, 2, 3}, {0, 1, 4}, {5, 6, 0}}
	inverse, err := matrix.inverse()
	if err != nil {
		t.Fatal(err)
	}
	product := matrix.multiply(inverse)
	for row := 0; row < 3; row++ {
		for column := 0; column < 3; column++ {
			if math.Abs(product[row][column]-dngIdentityMatrix[row][column]) > 1e-12 {
				t.Fatal("matrix inverse is incorrect")
			}
		}
	}
	if _, err := (dngMatrix{}).inverse(); err == nil {
		t.Fatal("singular matrix accepted")
	}
	d65 := [3]float64{0.95047, 1, 1.08883}
	adaptation, err := dngChromaticAdaptation(d65, dngWhiteD50)
	if err != nil {
		t.Fatal(err)
	}
	result := adaptation.vector(d65)
	for channel := range result {
		if math.Abs(result[channel]-dngWhiteD50[channel]) > 1e-6 {
			t.Fatalf("white adaptation=%v", result)
		}
	}
}

func TestDNGWhiteTemperatureReferenceWhites(t *testing.T) {
	for _, test := range []struct {
		white [3]float64
		want  float64
	}{
		{[3]float64{.95047, 1, 1.08883}, 6500},
		{dngWhiteD50, 5000},
		{[3]float64{}, 6500},
		{[3]float64{.4, .1858, .4142}, 6500},
	} {
		if got := dngWhiteTemperature(test.white); math.Abs(got-test.want) > 50 {
			t.Errorf("CCT(%v)=%g want approximately %g", test.white, got, test.want)
		}
	}
	for code, want := range map[int]float64{17: 2856, 21: 6500, 23: 5000, 0: 0} {
		if got := dngIlluminantTemperature(code); got != want {
			t.Errorf("illuminant %d=%g want %g", code, got, want)
		}
	}
}

func TestDNGDualIlluminantSelectsDaylightMatrix(t *testing.T) {
	order := binary.LittleEndian
	metadata := map[uint16]dngTestField{
		50721: dngTestDouble(order, 1, 0, 0, 0, 1, 0, 0, 0, 1),
		50722: dngTestDouble(order, 1, 0, 0, 0, 1, 0, 0, 0, 1),
		50728: dngTestDouble(order, .95047, 1, 1.08883),
		50778: dngTestShort(order, 17), 50779: dngTestShort(order, 21),
		50964: dngTestDouble(order, .5, .46422, 0, 0, 1, 0, 0, 0, .82521),
		50965: dngTestDouble(order, .96422, 0, 0, 0, 1, 0, 0, 0, .82521),
	}
	// A D65 scene should use the second calibration, retaining this red
	// sample's stronger X component. The tungsten matrix would produce a
	// red sRGB code near 28000 instead of the expected approximately 38400.
	data := linearDNGFixture(order, 1, 1, 3, []uint16{6229, 0, 0}, metadata)
	img, err := decodeLinearDNG(data)
	if err != nil {
		t.Fatal(err)
	}
	pixel := img.NRGBA64At(0, 0)
	if pixel.R < 38000 || pixel.R > 39000 || pixel.G != 0 || pixel.B < 4900 || pixel.B > 5300 {
		t.Fatalf("wrong dual-illuminant color transform: %v", pixel)
	}
}

func TestAdobeLinearDNGRealSample(t *testing.T) {
	path := os.Getenv("LRTL_LINEAR_DNG_SAMPLE")
	if path == "" {
		t.Skip("set LRTL_LINEAR_DNG_SAMPLE to a real Converter -l -u -p0 -dng1.4 output")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	img, err := decodeLinearDNG(data)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("developed real sample: %dx%d", img.Bounds().Dx(), img.Bounds().Dy())
	if output := os.Getenv("LRTL_LINEAR_DNG_PREVIEW"); output != "" {
		file, err := os.Create(output)
		if err != nil {
			t.Fatal(err)
		}
		if err := jpeg.Encode(file, img, &jpeg.Options{Quality: 90}); err != nil {
			file.Close()
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
