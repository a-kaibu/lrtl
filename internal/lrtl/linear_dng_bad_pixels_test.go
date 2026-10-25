package lrtl

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image/jpeg"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func testDNGBadPixelConstant(constant, phase uint32) testDNGOpcode {
	payload := binary.BigEndian.AppendUint32(nil, constant)
	payload = binary.BigEndian.AppendUint32(payload, phase)
	return testDNGOpcode{4, 0x01030000, 0, payload}
}

func testDNGNormalizedSample(value uint16) float32 {
	return float32(value) / 65535
}

func testDNGLinearToSRGB16(value uint16) uint16 {
	linear := float64(value) / 65535
	encoded := 12.92 * linear
	if linear > .0031308 {
		encoded = 1.055*math.Pow(linear, 1/2.4) - .055
	}
	return uint16(math.Round(65535 * encoded))
}

func TestParseDNGBadPixelConstant(t *testing.T) {
	t.Run("truncates constant to SDK sample type", func(t *testing.T) {
		parsed, err := parseDNGBadPixelConstant(binary.BigEndian.AppendUint32(binary.BigEndian.AppendUint32(nil, 0x10001), 2))
		if err != nil {
			t.Fatal(err)
		}
		if parsed.value != 1 || parsed.bayerPhase != 2 {
			t.Fatalf("parsed=%+v", parsed)
		}
	})
	for name, payload := range map[string][]byte{
		"short":         []byte{0, 0, 0, 0, 0, 0, 0},
		"trailing":      append(binary.BigEndian.AppendUint32(binary.BigEndian.AppendUint32(nil, 0), 0), 0),
		"invalid phase": binary.BigEndian.AppendUint32(binary.BigEndian.AppendUint32(nil, 0), 4),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseDNGBadPixelConstant(payload); err == nil {
				t.Fatal("invalid FixBadPixelsConstant parameters accepted")
			}
		})
	}
}

func TestApplyDNGBadPixelConstantUsesSDKBayerPhaseAndIsolatedSource(t *testing.T) {
	const width, height = 9, 9
	for _, phase := range []uint32{0, 1, 2, 3} {
		t.Run(fmt.Sprintf("phase-%d", phase), func(t *testing.T) {
			source := make([]float32, width*height)
			for index := range source {
				source[index] = testDNGNormalizedSample(10000)
			}
			const bad = uint16(65535)
			center := 4*width + 4
			source[center] = testDNGNormalizedSample(bad)
			for index, value := range map[int]uint16{
				3*width + 3: 1000, 3*width + 5: 2000, 5*width + 3: 3000, 5*width + 5: 4000,
				2*width + 4: 101, 6*width + 4: 202, 4*width + 2: 303, 4*width + 6: 404,
			} {
				source[index] = testDNGNormalizedSample(value)
			}
			original := append([]float32(nil), source...)
			destination := make([]float32, len(source))
			err := applyDNGBadPixelConstant(source, destination, width, height, 0, 0, dngBadPixelConstant{value: bad, bayerPhase: phase})
			if err != nil {
				t.Fatal(err)
			}
			want := uint16(253)
			// Adobe's SDK IsGreen() convention uses phases 0/3 for the
			// diagonal green samples at this even/even pixel; phases 1/2
			// use the four same-color axial samples.
			if phase == 0 || phase == 3 {
				want = 2500
			}
			if got := uint16(math.Round(float64(destination[center]) * 65535)); got != want {
				t.Fatalf("repaired sample=%d want %d", got, want)
			}
			if !reflect.DeepEqual(source, original) {
				t.Fatal("source buffer was modified while applying the opcode")
			}
			if got := uint16(math.Round(float64(destination[3*width+3]) * 65535)); got != 1000 {
				t.Fatalf("neighbor sample was changed: %d", got)
			}
		})
	}

	t.Run("ignores marked neighbors and leaves unrepairable values", func(t *testing.T) {
		const width, height = 9, 9
		source := make([]float32, width*height)
		for index := range source {
			source[index] = testDNGNormalizedSample(1200)
		}
		const bad = uint16(65535)
		center := 4*width + 4
		source[center] = testDNGNormalizedSample(bad)
		source[3*width+3] = testDNGNormalizedSample(bad)
		for _, point := range []struct{ x, y int }{{5, 3}, {3, 5}, {5, 5}} {
			source[point.y*width+point.x] = testDNGNormalizedSample(3000)
		}
		destination := make([]float32, len(source))
		if err := applyDNGBadPixelConstant(source, destination, width, height, 0, 0, dngBadPixelConstant{value: bad, bayerPhase: 0}); err != nil {
			t.Fatal(err)
		}
		if got := uint16(math.Round(float64(destination[center]) * 65535)); got != 3000 {
			t.Fatalf("sentinel neighbor was not excluded: %d", got)
		}
		edgeSource, edgeDestination := []float32{testDNGNormalizedSample(bad)}, make([]float32, 1)
		if err := applyDNGBadPixelConstant(edgeSource, edgeDestination, 1, 1, 0, 0, dngBadPixelConstant{value: bad, bayerPhase: 0}); err != nil {
			t.Fatal(err)
		}
		if got := uint16(math.Round(float64(edgeDestination[0]) * 65535)); got != bad {
			t.Fatalf("unrepairable marker changed: %d", got)
		}
	})
}

func TestLinearDNGFixBadPixelsConstantAcrossByteOrdersAndLists(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		for _, phase := range []uint32{0, 1, 2, 3} {
			t.Run(fmt.Sprintf("%v-phase-%d", order, phase), func(t *testing.T) {
				const width, height = 9, 9
				pixels := make([]uint16, width*height)
				for index := range pixels {
					pixels[index] = 10000
				}
				center := 4*width + 4
				pixels[center] = 65535
				for index, value := range map[int]uint16{
					3*width + 3: 1000, 3*width + 5: 2000, 5*width + 3: 3000, 5*width + 5: 4000,
					2*width + 4: 101, 6*width + 4: 202, 4*width + 2: 303, 4*width + 6: 404,
				} {
					pixels[index] = value
				}
				fields := map[uint16]dngTestField{
					50717: dngTestLong(order, 65535),
					51009: testDNGOpcodeList(testDNGBadPixelConstant(65535, phase)),
				}
				data := linearDNGFixture(order, width, height, 1, pixels, fields)
				img, err := decodeLinearDNG(data)
				if err != nil {
					t.Fatal(err)
				}
				want := uint16(253)
				if phase == 0 || phase == 3 {
					want = 2500
				}
				want = testDNGLinearToSRGB16(want)
				pixel := img.NRGBA64At(4, 4)
				if pixel.R != want || pixel.G != want || pixel.B != want || pixel.A != 65535 {
					t.Fatalf("pixel=%v want gray %d", pixel, want)
				}
			})
		}
	}

	if _, err := decodeLinearDNG(linearDNGFixture(binary.LittleEndian, 9, 9, 3,
		make([]uint16, 9*9*3), map[uint16]dngTestField{51009: testDNGOpcodeList(testDNGBadPixelConstant(0, 0))})); err == nil {
		t.Fatal("multi-channel FixBadPixelsConstant accepted")
	}
	listPixels := make([]uint16, 5*5)
	for i := range listPixels {
		listPixels[i] = 10000
	}
	listPixels[2*5+2] = 65535
	for _, flags := range []uint32{0, 1} {
		opcode := testDNGBadPixelList(0, [][2]int32{{2, 2}}, nil)
		opcode.flags = flags
		fixture := linearDNGFixture(binary.LittleEndian, 5, 5, 1, listPixels, map[uint16]dngTestField{
			50717: dngTestLong(binary.LittleEndian, 65535),
			51009: testDNGOpcodeList(opcode),
		})
		got, err := decodeLinearDNG(fixture)
		if err != nil {
			t.Fatal(err)
		}
		want := testDNGLinearToSRGB16(10000)
		if pixel := got.NRGBA64At(2, 2); pixel.R != want || pixel.G != want || pixel.B != want {
			t.Fatalf("required/optional FixBadPixelsList flags=%d produced %v, want gray %d", flags, pixel, want)
		}
	}
	oldVersion := testDNGBadPixelList(0, [][2]int32{{2, 2}}, nil)
	oldVersion.version = 0x01020000
	oldVersionFixture := linearDNGFixture(binary.LittleEndian, 5, 5, 1, listPixels, map[uint16]dngTestField{
		50717: dngTestLong(binary.LittleEndian, 65535),
		51009: testDNGOpcodeList(oldVersion),
	})
	if _, err := decodeLinearDNG(oldVersionFixture); err == nil {
		t.Fatal("required FixBadPixelsList with an unsupported version accepted")
	}
	oldVersion.flags = 1
	oldVersionFixture = linearDNGFixture(binary.LittleEndian, 5, 5, 1, listPixels, map[uint16]dngTestField{
		50717: dngTestLong(binary.LittleEndian, 65535),
		51009: testDNGOpcodeList(oldVersion),
	})
	if _, err := decodeLinearDNG(oldVersionFixture); err != nil {
		t.Fatalf("optional FixBadPixelsList with an unsupported version was not skipped: %v", err)
	}
}

func TestLinearDNGStage1BadPixelOpcodesRunOnRawSamplesBeforeLinearization(t *testing.T) {
	for _, opcodeKind := range []string{"constant", "list"} {
		for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
			t.Run(fmt.Sprintf("%s-%s", opcodeKind, order.String()), func(t *testing.T) {
				const width, height = 9, 9
				pixels := make([]uint16, width*height)
				for y := 0; y < height; y++ {
					for x := 0; x < width; x++ {
						pixels[y*width+x] = 10000
						if opcodeKind == "list" {
							pixels[y*width+x] = uint16(1000 + 20*x + 30*y)
						}
					}
				}
				center := 4*width + 4
				pixels[center] = 1234
				if opcodeKind == "constant" {
					pixels[3*width+3] = 1000
					pixels[3*width+5] = 2000
					pixels[5*width+3] = 3000
					pixels[5*width+5] = 4000
				}

				// The marker is present only in the pre-linearized raw values.
				// Mapping it to 50000 proves Stage 1 ran before this lookup.
				linearization := make([]uint16, 50001)
				for index := range linearization {
					linearization[index] = uint16(index)
				}
				linearization[1234] = 50000
				opcode := testDNGBadPixelConstant(1234, 0)
				wantSample := uint16(2500)
				if opcodeKind == "list" {
					opcode = testDNGBadPixelList(0, [][2]int32{{4, 4}}, nil)
					wantSample = 1200
				}
				fixture := linearDNGFixture(order, width, height, 1, pixels, map[uint16]dngTestField{
					50712: dngTestShort(order, linearization...),
					50717: dngTestLong(order, 65535),
					50829: dngTestLong(order, 2, 2, 7, 7),
					51008: testDNGOpcodeList(opcode),
				})
				img, err := decodeLinearDNG(fixture)
				if err != nil {
					t.Fatal(err)
				}
				want := testDNGLinearToSRGB16(wantSample)
				if pixel := img.NRGBA64At(2, 2); pixel.R != want || pixel.G != want || pixel.B != want || pixel.A != 65535 {
					t.Fatalf("Stage 1 %s center=%v want gray %d", opcodeKind, pixel, want)
				}
			})
		}
	}
}

func TestLinearDNGStage1RejectsRequiredUnsupportedOpcodeAndSkipsOptional(t *testing.T) {
	for _, flags := range []uint32{0, 1} {
		opcode := testDNGOpcode{6, 0x01030000, flags, nil}
		fixture := linearDNGFixture(binary.LittleEndian, 2, 2, 1, []uint16{1000, 1000, 1000, 1000}, map[uint16]dngTestField{
			51008: testDNGOpcodeList(opcode),
		})
		_, err := decodeLinearDNG(fixture)
		if flags == 0 && err == nil {
			t.Fatal("unsupported required Stage 1 opcode was accepted")
		}
		if flags == 1 && err != nil {
			t.Fatalf("unsupported optional Stage 1 opcode was not skipped: %v", err)
		}
	}
}

func TestRAWBadPixelConstantCLIThroughTIFFAndAVI(t *testing.T) {
	executable, fixtureDir, _ := fakeDNGConverter(t)
	const width, height = 16, 16
	pixels := make([]uint16, width*height)
	for index := range pixels {
		pixels[index] = 10000
	}
	center := 8*width + 8
	pixels[center] = 65535
	for index, value := range map[int]uint16{
		7*width + 7: 1000, 7*width + 9: 2000, 9*width + 7: 3000, 9*width + 9: 4000,
		6*width + 8: 101, 10*width + 8: 202, 8*width + 6: 303, 8*width + 10: 404,
	} {
		pixels[index] = value
	}
	fixture := linearDNGFixture(binary.LittleEndian, width, height, 1, pixels, map[uint16]dngTestField{
		50717: dngTestLong(binary.LittleEndian, 65535),
		51022: testDNGOpcodeList(testDNGBadPixelConstant(65535, 0)),
	})
	fixturePath := filepath.Join(fixtureDir, "badpixels.dng")
	if err := os.WriteFile(fixturePath, fixture, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LRTL_TEST_CONVERTER_FIXTURE", fixturePath)
	dir := t.TempDir()
	for index := 0; index < 2; index++ {
		path := filepath.Join(dir, fmt.Sprintf("frame%d.nef", index+1))
		if err := os.WriteFile(path, fixture, 0600); err != nil {
			t.Fatal(err)
		}
	}
	before := batchFileSnapshot(t, dir)
	output := filepath.Join(t.TempDir(), "export")
	var stdout, stderr bytes.Buffer
	if Main([]string{"export", dir, "--output", output, "--raw-develop", "--dng-converter", executable}, &stdout, &stderr) != 0 {
		t.Fatalf("RAW export failed: %s", stderr.String())
	}
	want := testDNGLinearToSRGB16(2500)
	for index := 0; index < 2; index++ {
		img := readExportedTIFF(t, filepath.Join(output, fmt.Sprintf("frame%06d.tif", index+1)))
		r, g, b, _ := img.At(8, 8).RGBA()
		if uint16(r) != want || g != r || b != r {
			t.Fatalf("exported frame %d center=(%d,%d,%d), want gray %d", index+1, r, g, b, want)
		}
	}
	video := filepath.Join(t.TempDir(), "bad-pixel.avi")
	if Main([]string{"render", output, "--output", video, "--quality", "100"}, &stdout, &stderr) != 0 {
		t.Fatalf("AVI render failed: %s", stderr.String())
	}
	data, err := os.ReadFile(video)
	if err != nil {
		t.Fatal(err)
	}
	frameCount := 0
	for {
		start := bytes.Index(data, []byte{255, 216})
		if start < 0 {
			break
		}
		data = data[start:]
		end := bytes.Index(data, []byte{255, 217})
		if end < 0 || frameCount >= 2 {
			t.Fatal("bad AVI frame data")
		}
		img, err := jpeg.Decode(bytes.NewReader(data[:end+2]))
		if err != nil {
			t.Fatal(err)
		}
		r, _, _, _ := img.At(8, 8).RGBA()
		if math.Abs(float64(r>>8)-float64(want)/257) > 2 {
			t.Fatalf("AVI center=%d, want approximately %d", r>>8, want/257)
		}
		frameCount++
		data = data[end+2:]
	}
	if frameCount != 2 {
		t.Fatalf("AVI has %d frames, want 2", frameCount)
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("RAW source files were changed")
	}
}
