package lrtl

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/jpeg"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func testDNGArea(rect image.Rectangle, plane, planes, rowPitch, colPitch uint32) []byte {
	var data []byte
	for _, v := range []uint32{uint32(rect.Min.Y), uint32(rect.Min.X), uint32(rect.Max.Y), uint32(rect.Max.X), plane, planes, rowPitch, colPitch} {
		data = binary.BigEndian.AppendUint32(data, v)
	}
	return data
}

func testDNGPolynomial(area []byte, coefficients ...float64) testDNGOpcode {
	data := binary.BigEndian.AppendUint32(append([]byte(nil), area...), uint32(len(coefficients)-1))
	for _, v := range coefficients {
		data = binary.BigEndian.AppendUint64(data, math.Float64bits(v))
	}
	return testDNGOpcode{8, 0x01030000, 0, data}
}

func testDNGMapTable(area []byte, values ...uint16) testDNGOpcode {
	data := binary.BigEndian.AppendUint32(append([]byte(nil), area...), uint32(len(values)))
	for _, v := range values {
		data = binary.BigEndian.AppendUint16(data, v)
	}
	return testDNGOpcode{7, 0x01030000, 0, data}
}

func TestDNGMapTableIdentityAndEndpoint(t *testing.T) {
	area := testDNGArea(image.Rect(0, 0, 1, 1), 0, 1, 1, 1)
	identity := make([]uint16, 65536)
	for i := range identity {
		identity[i] = uint16(i)
	}
	opcode, err := parseDNGPointOpcode(7, testDNGMapTable(area, identity...).payload)
	if err != nil {
		t.Fatal(err)
	}
	for sample := 0; sample <= 65535; sample++ {
		value := float64(sample) / 65535
		camera := [3]float64{value, 0, 0}
		applyDNGPointOpcodes([]dngPointOpcode{opcode}, &camera, 0, 0, 1, 1, 1)
		if camera[0] != value {
			t.Fatalf("identity MapTable changed sample %d", sample)
		}
	}
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		data := linearDNGFixture(order, 6, 1, 1, []uint16{0, 1, 2, 3, 65534, 65535}, map[uint16]dngTestField{51022: testDNGOpcodeList(testDNGMapTable(testDNGArea(image.Rect(0, 0, 6, 1), 0, 1, 1, 1), 0, 16384, 32768, 65535))})
		img, err := decodeLinearDNG(data)
		if err != nil {
			t.Fatal(err)
		}
		for x, linear := range []float64{0, 16384.0 / 65535, 32768.0 / 65535, 1, 1, 1} {
			want := 0.0
			if linear > 0 {
				want = math.Round(65535 * (1.055*math.Pow(linear, 1/2.4) - .055))
			}
			if float64(img.NRGBA64At(x, 0).R) != want {
				t.Fatalf("MapTable endpoint %s x=%d", order, x)
			}
		}
	}
}

func TestDNGExtremeFiniteGainBeforeMapTable(t *testing.T) {
	payload := testDNGGainMapPayload(1, 32768, 2, 2, 1, math.MaxFloat32, math.MaxFloat32, math.MaxFloat32, math.MaxFloat32)
	gain, err := parseDNGPointOpcode(9, payload)
	if err != nil {
		t.Fatal(err)
	}
	lookup, err := parseDNGPointOpcode(7, testDNGMapTable(testDNGArea(image.Rect(0, 0, 1, 32768), 0, 1, 1, 1), 0, 65535).payload)
	if err != nil {
		t.Fatal(err)
	}
	for y := 0; y < 32768; y++ {
		for _, input := range []float64{0, .5} {
			camera := [3]float64{input, 0, 0}
			applyDNGPointOpcodes([]dngPointOpcode{gain, lookup}, &camera, 0, y, 1, 1, 32768)
			want := 0.0
			if input > 0 {
				want = 1
			}
			if camera[0] != want {
				t.Fatalf("finite gain y=%d input=%g result=%g", y, input, camera[0])
			}
		}
	}
}

func testDNGAxisOpcode(id uint32, area []byte, values ...float32) testDNGOpcode {
	data := binary.BigEndian.AppendUint32(append([]byte(nil), area...), uint32(len(values)))
	for _, v := range values {
		data = binary.BigEndian.AppendUint32(data, math.Float32bits(v))
	}
	return testDNGOpcode{id, 0x01030000, 0, data}
}

func TestDNGPointOpcodesEmptyAreaCoversWholeImage(t *testing.T) {
	empty := testDNGArea(image.Rectangle{}, 0, 1, 1, 1)
	area, err := parseDNGOpcodeArea(empty)
	if err != nil || !area.area.Empty() {
		t.Fatalf("empty opcode area: area=%+v err=%v", area, err)
	}
	for name, malformed := range map[string][]byte{
		"row pitch":    testDNGArea(image.Rectangle{}, 0, 1, 2, 1),
		"column pitch": testDNGArea(image.Rectangle{}, 0, 1, 1, 2),
		"inverted row": func() []byte {
			data := append([]byte(nil), empty...)
			binary.BigEndian.PutUint32(data[0:], 1)
			return data
		}(),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseDNGOpcodeArea(malformed); err == nil {
				t.Fatal("invalid empty area accepted")
			}
		})
	}

	for _, test := range []struct {
		id     uint32
		values []float32
	}{
		{10, []float32{.125, .25}},
		{11, []float32{.0625, .125, .1875}},
		{12, []float32{2, 3}},
		{13, []float32{.5, 1, 2}},
	} {
		area := image.Rect(0, 0, 0, len(test.values))
		if test.id == 11 || test.id == 13 {
			area = image.Rect(0, 0, len(test.values), 0)
		}
		opcode, err := parseDNGPointOpcode(test.id, testDNGAxisOpcode(test.id, testDNGArea(area, 0, 1, 1, 1), test.values...).payload)
		if err != nil {
			t.Fatalf("opcode %d: %v", test.id, err)
		}
		for y := 0; y < 2; y++ {
			for x := 0; x < 3; x++ {
				camera := [3]float64{.125, .25, .5}
				applyDNGPointOpcodes([]dngPointOpcode{opcode}, &camera, x, y, 3, 3, 2)
				for plane := range camera {
					want := []float64{.125, .25, .5}[plane]
					if plane == 0 {
						index := y
						if test.id == 11 || test.id == 13 {
							index = x
						}
						factor := float64(test.values[index])
						switch test.id {
						case 10, 11:
							want += factor
						case 12, 13:
							want *= factor
						}
						want = min(1, max(0, want))
					}
					if camera[plane] != want {
						t.Fatalf("opcode %d pixel (%d,%d) plane %d=%g want %g", test.id, x, y, plane, camera[plane], want)
					}
				}
			}
		}
	}
	if _, err := parseDNGPointOpcode(10, testDNGAxisOpcode(10, empty).payload); err == nil {
		t.Fatal("empty-area row opcode without an encoded row span accepted")
	}

	for _, test := range []struct {
		name string
		id   uint32
		make func() testDNGOpcode
		want float64
	}{
		{"map table", 7, func() testDNGOpcode { return testDNGMapTable(empty, 0, 65535) }, 1},
		{"polynomial", 8, func() testDNGOpcode { return testDNGPolynomial(empty, .125, .5) }, .25},
		{"gain map", 9, func() testDNGOpcode { return testDNGGainMap(testDNGGainMapPayload(0, 0, 1, 1, 1, 2)) }, .5},
	} {
		t.Run(test.name, func(t *testing.T) {
			encoded := test.make()
			opcode, err := parseDNGPointOpcode(test.id, encoded.payload)
			if err != nil {
				t.Fatal(err)
			}
			for y := 0; y < 2; y++ {
				for x := 0; x < 3; x++ {
					camera := [3]float64{.25, .25, .25}
					applyDNGPointOpcodes([]dngPointOpcode{opcode}, &camera, x, y, 1, 3, 2)
					if camera[0] != test.want || camera[1] != .25 || camera[2] != .25 {
						t.Fatalf("pixel (%d,%d)=%v want channel 0 %g", x, y, camera, test.want)
					}
				}
			}
		})
	}
}

func TestLinearDNGEmptyAreaTablesUseEncodedAxisExtentAndPostTrimCoordinates(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		const width, height = 5, 4
		pixels := make([]uint16, width*height)
		for i := range pixels {
			pixels[i] = 16384
		}
		emptyRows := testDNGArea(image.Rect(0, 0, 0, height), 0, 1, 1, 1)
		emptyColumns := testDNGArea(image.Rect(0, 0, width, 0), 0, 1, 1, 1)
		trim := testDNGTrimBounds(1, 1, 3, 4)
		fields := map[uint16]dngTestField{
			50717: dngTestLong(order, 65535),
			51009: testDNGOpcodeList(trim,
				testDNGAxisOpcode(10, emptyRows, 0, .125, .25, 0),
				testDNGAxisOpcode(11, emptyColumns, 0, .0625, .125, .1875, 0),
				testDNGAxisOpcode(12, emptyRows, 1, 2, 3, 1),
				testDNGAxisOpcode(13, emptyColumns, 1, .5, 1, 2, 1)),
		}
		img, err := decodeLinearDNG(linearDNGFixture(order, width, height, 1, pixels, fields))
		if err != nil {
			t.Fatalf("%s: %v", order, err)
		}
		if img.Bounds() != image.Rect(0, 0, 3, 2) {
			t.Fatalf("%s bounds=%v", order, img.Bounds())
		}
		for y := 0; y < 2; y++ {
			for x := 0; x < 3; x++ {
				linear := float64(float32(float64(16384)/65535)) + float64(float32([]float32{.125, .25}[y]))
				linear = min(1, max(0, linear))
				linear += float64(float32([]float32{.0625, .125, .1875}[x]))
				linear = min(1, max(0, linear))
				linear *= []float64{2, 3}[y]
				linear = min(1, max(0, linear))
				linear *= []float64{.5, 1, 2}[x]
				linear = min(1, max(0, linear))
				want := encodeDNGSRGB(float64(float32(linear)))
				pixel := img.NRGBA64At(x, y)
				if pixel.R != want || pixel.G != want || pixel.B != want {
					t.Fatalf("%s (%d,%d)=%v want %d", order, x, y, pixel, want)
				}
			}
		}

		// Empty AreaSpec tables use their encoded axis extent, and their
		// coordinates remain based on the pre-trim image.
		for _, bad := range []testDNGOpcode{
			testDNGAxisOpcode(10, emptyRows, .125, .25, .375),
			testDNGAxisOpcode(11, emptyColumns, .0625, .125, .1875, .25),
		} {
			badFixture := linearDNGFixture(order, width, height, 1, pixels, map[uint16]dngTestField{
				51009: testDNGOpcodeList(trim, bad),
			})
			if _, err := decodeLinearDNG(badFixture); err == nil {
				t.Fatalf("%s opcode %d accepted a table count that disagrees with the encoded axis extent", order, bad.id)
			}
		}
		for _, bad := range []testDNGOpcode{
			testDNGAxisOpcode(10, testDNGArea(image.Rect(2, 0, 0, 4), 0, 1, 1, 1), .125, .25),
			testDNGAxisOpcode(11, testDNGArea(image.Rect(0, 2, 4, 0), 0, 1, 1, 1), .0625, .125),
		} {
			badFixture := linearDNGFixture(order, width, height, 1, pixels, map[uint16]dngTestField{
				51009: testDNGOpcodeList(trim, bad),
			})
			if _, err := decodeLinearDNG(badFixture); err == nil {
				t.Fatalf("%s opcode %d accepted an empty-area table that does not cover the trimmed image", order, bad.id)
			}
		}
	}
}

func TestDNGPolynomialDegreesAgainstPowerSum(t *testing.T) {
	area := testDNGArea(image.Rect(0, 0, 1, 1), 0, 1, 1, 1)
	coefficients := []float64{.0625, .5, -.125, .25, -.125, .0625, -.03125, .015625, -.0078125}
	for degree := 0; degree <= 8; degree++ {
		opcode, err := parseDNGPointOpcode(8, testDNGPolynomial(area, coefficients[:degree+1]...).payload)
		if err != nil {
			t.Fatal(err)
		}
		for step := 0; step <= 256; step++ {
			x := float64(step) / 256
			want := 0.0
			// Independent direct power sum; implementation uses Horner evaluation.
			for power, coefficient := range coefficients[:degree+1] {
				want += coefficient * math.Pow(x, float64(power))
			}
			want = math.Min(1, math.Max(0, want))
			camera := [3]float64{x, 0, 0}
			applyDNGPointOpcodes([]dngPointOpcode{opcode}, &camera, 0, 0, 1, 1, 1)
			if math.Abs(camera[0]-want) > 1e-14 {
				t.Fatalf("degree=%d x=%g got %g want %g", degree, x, camera[0], want)
			}
		}
	}
}

func TestDNGPointOpcodesAreaPlanesAndPitch(t *testing.T) {
	area := testDNGArea(image.Rect(1, 2, 8, 7), 1, 2, 2, 3)
	for _, id := range []uint32{7, 8, 10, 11, 12, 13} {
		encoded := testDNGAxisOpcode(id, area, .5, 2, -1)
		if id == 8 {
			encoded = testDNGPolynomial(area, .125, .5, .5)
		}
		if id == 7 {
			encoded = testDNGMapTable(area, 0, 16384, 32768)
		}
		opcode, err := parseDNGPointOpcode(id, encoded.payload)
		if err != nil {
			t.Fatal(err)
		}
		for y := 0; y < 9; y++ {
			for x := 0; x < 9; x++ {
				original := [3]float64{.125, .25, .5}
				camera := original
				applyDNGPointOpcodes([]dngPointOpcode{opcode}, &camera, x, y, 3, 9, 9)
				for plane := 0; plane < 3; plane++ {
					want := original[plane]
					if x >= 1 && x < 8 && y >= 2 && y < 7 && (x-1)%3 == 0 && (y-2)%2 == 0 && plane >= 1 {
						index := (y - 2) / 2
						if id == 11 || id == 13 {
							index = (x - 1) / 3
						}
						table := []float64{.5, 2, -1}
						switch id {
						case 7:
							want = 32768.0 / 65535
						case 8:
							want = .125 + .5*want + .5*want*want
						case 10, 11:
							want += table[index]
						case 12, 13:
							want *= table[index]
						}
						if want > 1 {
							want = 1
						}
						if want < 0 {
							want = 0
						}
					}
					if camera[plane] != want {
						t.Fatalf("opcode %d (%d,%d) plane %d got %g want %g", id, x, y, plane, camera[plane], want)
					}
				}
			}
		}
	}
}

func mixedDNGPointFixture(order binary.ByteOrder, width, height int) []byte {
	area := testDNGArea(image.Rect(0, 0, width, height), 0, 1, 1, 1)
	rowsDelta, rowsScale := make([]float32, height), make([]float32, height)
	colsDelta, colsScale := make([]float32, width), make([]float32, width)
	for i := range rowsDelta {
		rowsDelta[i] = -.25
		rowsScale[i] = .5
	}
	for i := range colsDelta {
		colsDelta[i] = .125
		colsScale[i] = 2
	}
	pixels := make([]uint16, width*height)
	for i := range pixels {
		pixels[i] = 49152
	}
	return linearDNGFixture(order, width, height, 1, pixels, map[uint16]dngTestField{
		50717: dngTestLong(order, 65536), 50730: dngTestDouble(order, 1),
		51009: testDNGOpcodeList(testDNGMapTable(area, 0, 49152), testDNGGainMap(testDNGGainMapPayload(width, height, 1, 1, 1, 2)), testDNGPolynomial(area, .25, .25)),
		51022: testDNGOpcodeList(testDNGAxisOpcode(10, area, rowsDelta...), testDNGAxisOpcode(13, area, colsScale...), testDNGAxisOpcode(11, area, colsDelta...), testDNGAxisOpcode(12, area, rowsScale...)),
	})
}

func TestLinearDNGMixedPointOrderAndBaselineExposure(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		img, err := decodeLinearDNG(mixedDNGPointFixture(order, 3, 2))
		if err != nil {
			t.Fatal(err)
		}
		// .75 * 2 clips to 1; polynomial gives .5; row delta -> .25;
		// column scale -> .5; column delta -> .625; row scale -> .3125;
		// baseline +1 EV gives .625 before sRGB encoding.
		want := uint16(math.Round(65535 * (1.055*math.Pow(.625, 1/2.4) - .055)))
		for y := 0; y < 2; y++ {
			for x := 0; x < 3; x++ {
				p := img.NRGBA64At(x, y)
				if p.R != want || p.G != want || p.B != want {
					t.Fatalf("mixed opcode order %s got %v want %d", order, p, want)
				}
			}
		}
	}
}

func TestLinearDNGAxisOpcodeActiveAreaAndCrop(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		area := testDNGArea(image.Rect(0, 0, 6, 5), 0, 1, 1, 1)
		pixels := make([]uint16, 8*8)
		for i := range pixels {
			pixels[i] = 512
		}
		data := linearDNGFixture(order, 8, 8, 1, pixels, map[uint16]dngTestField{
			50829: dngTestLong(order, 2, 1, 7, 7), 50719: dngTestLong(order, 2, 1), 50720: dngTestLong(order, 3, 3), 50717: dngTestLong(order, 16384),
			51009: testDNGOpcodeList(testDNGAxisOpcode(10, area, 0, 1.0/64, 2.0/64, 3.0/64, 4.0/64), testDNGAxisOpcode(13, area, .5, 1, 1.5, 2, 2.5, 3)), 51022: testDNGOpcodeList(testDNGPolynomial(area, 0, .5)),
		})
		img, err := decodeLinearDNG(data)
		if err != nil {
			t.Fatal(err)
		}
		if img.Bounds() != image.Rect(0, 0, 3, 3) {
			t.Fatal(img.Bounds())
		}
		for y := 0; y < 3; y++ {
			for x := 0; x < 3; x++ {
				linear := (1.0/32 + float64(y+1)/64) * (.5 + .5*float64(x+2)) * .5
				want := uint16(math.Round(65535 * (1.055*math.Pow(linear, 1/2.4) - .055)))
				if img.NRGBA64At(x, y).R != want {
					t.Fatalf("active/crop coordinate %s (%d,%d)", order, x, y)
				}
			}
		}
	}
}

func TestDNGPointOpcodesRejectMalformedParameters(t *testing.T) {
	area := testDNGArea(image.Rect(0, 0, 5, 5), 0, 1, 2, 2)
	for _, encoded := range []testDNGOpcode{
		testDNGMapTable(area), testDNGMapTable(area, make([]uint16, 65537)...),
		testDNGPolynomial(area), testDNGPolynomial(area, make([]float64, 10)...), testDNGPolynomial(area, math.NaN()), testDNGPolynomial(area, math.Inf(1)), testDNGPolynomial(area, 1e300),
		testDNGAxisOpcode(10, area, 1, 2), testDNGAxisOpcode(11, area, 1, 2, 3, 4), testDNGAxisOpcode(12, area, 1, float32(math.NaN()), 3), testDNGAxisOpcode(13, area, 1, 2, float32(math.Inf(1))),
	} {
		if _, err := parseDNGPointOpcode(encoded.id, encoded.payload); err == nil {
			t.Fatalf("invalid opcode %d parameters accepted", encoded.id)
		}
	}
	for _, encoded := range []testDNGOpcode{testDNGMapTable(area, 0, 65535), testDNGPolynomial(area, 0, 1), testDNGAxisOpcode(10, area, 1, 2, 3)} {
		for _, length := range []int{0, 31, 33, len(encoded.payload) - 1} {
			if _, err := parseDNGPointOpcode(encoded.id, encoded.payload[:length]); err == nil {
				t.Fatal("truncated mapping accepted")
			}
		}
		if _, err := parseDNGPointOpcode(encoded.id, append(append([]byte(nil), encoded.payload...), 0)); err == nil {
			t.Fatal("trailing mapping bytes accepted")
		}
	}
	if _, err := parseDNGPointOpcode(6, area); err == nil {
		t.Fatal("unsupported opcode accepted")
	}
}

func TestRAWPointOpcodesCLIThroughTIFFAndAVI(t *testing.T) {
	executable, fixtureDir, _ := fakeDNGConverter(t)
	fixture := mixedDNGPointFixture(binary.LittleEndian, 16, 16)
	fixturePath := filepath.Join(fixtureDir, "point-ops.dng")
	if err := os.WriteFile(fixturePath, fixture, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LRTL_TEST_CONVERTER_FIXTURE", fixturePath)
	dir := t.TempDir()
	source := filepath.Join(dir, "frame.nef")
	if err := os.WriteFile(source, fixture, 0600); err != nil {
		t.Fatal(err)
	}
	writeTestXMP(t, source, map[string]string{"crs:Exposure2012": ".5"})
	before := batchFileSnapshot(t, dir)
	output := filepath.Join(t.TempDir(), "export")
	var stdout, stderr bytes.Buffer
	if Main([]string{"export", dir, "--output", output, "--raw-develop", "--dng-converter", executable, "--apply-xmp-exposure"}, &stdout, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	img := readExportedTIFF(t, filepath.Join(output, "frame000001.tif"))
	encoded := math.Round(65535*(1.055*math.Pow(.625, 1/2.4)-.055)) / 65535
	linear := math.Pow((encoded+.055)/1.055, 2.4) * math.Sqrt2
	want := math.Round(65535 * (1.055*math.Pow(linear, 1/2.4) - .055))
	r, g, b, _ := img.At(8, 8).RGBA()
	if math.Abs(float64(r)-want) > 1 || r != g || g != b {
		t.Fatalf("mixed RAW/XMP pixel=%d,%d,%d want %g", r, g, b, want)
	}
	video := filepath.Join(t.TempDir(), "out.avi")
	if Main([]string{"render", output, "--output", video, "--quality", "100"}, &stdout, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	data, err := os.ReadFile(video)
	if err != nil {
		t.Fatal(err)
	}
	start, end := bytes.Index(data, []byte{255, 216}), bytes.Index(data, []byte{255, 217})
	if start < 0 || end <= start {
		t.Fatal("missing video JPEG")
	}
	frame, err := jpeg.Decode(bytes.NewReader(data[start : end+2]))
	if err != nil {
		t.Fatal(err)
	}
	r, _, _, _ = frame.At(8, 8).RGBA()
	if math.Abs(float64(r>>8)-want/257) > 2 {
		t.Fatal("AVI differs from mixed RAW opcodes")
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("RAW/XMP source changed")
	}
}
