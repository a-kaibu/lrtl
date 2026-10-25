package lrtl

import (
	"encoding/binary"
	"image"
	"testing"
)

func TestDNGStage1PointOpcodeAreaPlaneAndPitch(t *testing.T) {
	const width, height, channels = 4, 3, 3
	source := make([]uint16, width*height*channels)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			base := (y*width + x) * channels
			source[base] = uint16(100 + x)
			source[base+1] = 2
			source[base+2] = uint16(300 + y)
			if x == 3 && y == 2 {
				source[base+1] = 99 // Short LUTs extend their last entry.
			}
		}
	}
	destination := make([]uint16, len(source))
	opcode, err := parseDNGPointOpcode(7, testDNGMapTable(
		testDNGArea(image.Rect(1, 0, 4, 3), 1, 1, 2, 2), 10, 20, 30,
	).payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := applyDNGPointOpcodeRaw(source, destination, width, height, channels, opcode); err != nil {
		t.Fatal(err)
	}
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			base := (y*width + x) * channels
			wantGreen := uint16(2)
			if x >= 1 && y%2 == 0 && (x-1)%2 == 0 {
				wantGreen = 30
			}
			want := []uint16{uint16(100 + x), wantGreen, uint16(300 + y)}
			for plane := range want {
				if got := destination[base+plane]; got != want[plane] {
					t.Fatalf("pixel (%d,%d) plane %d=%d want %d", x, y, plane, got, want[plane])
				}
			}
		}
	}
}

func TestDNGStage1PolynomialUsesRawDomainAndClipsEachOpcode(t *testing.T) {
	source := []uint16{1000, 20000, 40000}
	destination := make([]uint16, len(source))
	double, err := parseDNGPointOpcode(8, testDNGPolynomial(
		testDNGArea(image.Rectangle{}, 0, 1, 1, 1), 0, 2,
	).payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := applyDNGPointOpcodeRaw(source, destination, 3, 1, 1, double); err != nil {
		t.Fatal(err)
	}
	for index, want := range []uint16{2000, 40000, 65535} {
		if destination[index] != want {
			t.Errorf("doubled sample %d=%d want %d", index, destination[index], want)
		}
	}

	// A following operation sees the clipped 65535 result, not the unclipped
	// polynomial value of 80000.
	sequence := dngPointOpcode{
		dngOpcodeArea: dngOpcodeArea{plane: 0, planes: 1, rowPitch: 1, colPitch: 1},
		id:            8, coefficients: []float64{0, 1},
	}
	if err := applyDNGPointOpcodeRaw(destination, source, 3, 1, 1, sequence); err != nil {
		t.Fatal(err)
	}
	if source[2] != 65535 {
		t.Fatalf("second opcode received %d; want clipped 65535", source[2])
	}

	negative, err := parseDNGPointOpcode(8, testDNGPolynomial(
		testDNGArea(image.Rectangle{}, 0, 1, 1, 1), 1000, -2,
	).payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := applyDNGPointOpcodeRaw([]uint16{2000}, destination[:1], 1, 1, 1, negative); err != nil {
		t.Fatal(err)
	}
	if destination[0] != 0 {
		t.Fatalf("negative polynomial output=%d want 0", destination[0])
	}
}

func TestLinearDNGStage1MapTableAndPolynomialRunBeforeLinearization(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		t.Run(order.String(), func(t *testing.T) {
			const width = 4
			raw := []uint16{0, 1, 2, 3}
			identityArea := testDNGArea(image.Rectangle{}, 0, 1, 1, 1)
			linearization := dngTestShort(order, 0, 10000, 20000, 30000)
			mapOpcode := testDNGMapTable(identityArea, 3, 2, 1, 0)
			mapFixture := linearDNGFixture(order, width, 1, 1, raw, map[uint16]dngTestField{
				50712: linearization,
				50717: dngTestLong(order, 65535),
				51008: testDNGOpcodeList(mapOpcode),
			})
			mapImage, err := decodeLinearDNG(mapFixture)
			if err != nil {
				t.Fatalf("MapTable: %v", err)
			}
			for x, linear := range []uint16{30000, 20000, 10000, 0} {
				want := testDNGLinearToSRGB16(linear)
				if got := mapImage.NRGBA64At(x, 0); got.R != want || got.G != want || got.B != want || got.A != 65535 {
					t.Fatalf("MapTable pixel %d=%v want gray %d", x, got, want)
				}
			}

			polynomial := testDNGPolynomial(identityArea, 0, 0, 16000)
			polynomialFixture := linearDNGFixture(order, width, 1, 1, raw, map[uint16]dngTestField{
				50717: dngTestLong(order, 65535),
				51008: testDNGOpcodeList(polynomial),
			})
			polynomialImage, err := decodeLinearDNG(polynomialFixture)
			if err != nil {
				t.Fatalf("MapPolynomial: %v", err)
			}
			for x, linear := range []uint16{0, 16000, 64000, 65535} {
				want := testDNGLinearToSRGB16(linear)
				if got := polynomialImage.NRGBA64At(x, 0); got.R != want || got.G != want || got.B != want || got.A != 65535 {
					t.Fatalf("MapPolynomial pixel %d=%v want gray %d", x, got, want)
				}
			}
		})
	}
}

func TestLinearDNGStage1PointOpcodeUsesEachLinearPlane(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		t.Run(order.String(), func(t *testing.T) {
			raw := []uint16{16384, 8192, 4096}
			metadata := dngRGBIdentityCameraMetadata(order, nil)
			metadata[51008] = testDNGOpcodeList(testDNGPolynomial(
				testDNGArea(image.Rectangle{}, 1, 1, 1, 1), 0, 2,
			))
			got, err := decodeLinearDNG(linearDNGFixture(order, 1, 1, 3, raw, metadata))
			if err != nil {
				t.Fatal(err)
			}
			want, err := decodeLinearDNG(linearDNGFixture(order, 1, 1, 3, []uint16{16384, 16384, 4096}, dngRGBIdentityCameraMetadata(order, nil)))
			if err != nil {
				t.Fatal(err)
			}
			original, err := decodeLinearDNG(linearDNGFixture(order, 1, 1, 3, raw, dngRGBIdentityCameraMetadata(order, nil)))
			if err != nil {
				t.Fatal(err)
			}
			if got.NRGBA64At(0, 0) != want.NRGBA64At(0, 0) {
				t.Fatalf("Stage 1 changed wrong channel: got %v want %v", got.NRGBA64At(0, 0), want.NRGBA64At(0, 0))
			}
			if got.NRGBA64At(0, 0) == original.NRGBA64At(0, 0) {
				t.Fatal("Stage 1 point opcode had no effect on the selected green plane")
			}
		})
	}
}

func TestLinearDNGStage1PointOpcodeRejectsPlaneRangeOutsideChannels(t *testing.T) {
	for _, opcode := range []testDNGOpcode{
		testDNGMapTable(testDNGArea(image.Rectangle{}, 1, 1, 1, 1), 0),
		testDNGPolynomial(testDNGArea(image.Rectangle{}, 0, 2, 1, 1), 0, 1),
	} {
		fixture := linearDNGFixture(binary.LittleEndian, 1, 1, 1, []uint16{1000}, map[uint16]dngTestField{
			51008: testDNGOpcodeList(opcode),
		})
		if _, err := decodeLinearDNG(fixture); err == nil {
			t.Fatalf("Stage 1 opcode %d with out-of-range planes was accepted", opcode.id)
		}
	}
}

func TestLinearDNGStage1GainMapAndRowColumnOpcodes(t *testing.T) {
	const width, height = 4, 2
	input := make([]uint16, width*height)
	for i := range input {
		input[i] = 16384
	}
	area := testDNGArea(image.Rect(0, 0, width, height), 0, 1, 1, 1)
	tests := []struct {
		name   string
		opcode testDNGOpcode
		want   []uint16
	}{
		{
			name:   "GainMap",
			opcode: testDNGGainMap(testDNGGainMapPayload(width, height, 2, 2, 1, 1, 2, 3, 4)),
			want:   []uint16{26624, 30720, 34816, 38912, 43008, 47104, 51200, 55296},
		},
		{
			name:   "DeltaPerRow",
			opcode: testDNGAxisOpcode(10, area, 8192, -8192),
			want:   []uint16{24576, 24576, 24576, 24576, 8192, 8192, 8192, 8192},
		},
		{
			name:   "DeltaPerColumn",
			opcode: testDNGAxisOpcode(11, area, 0, 8192, -8192, 16384),
			want:   []uint16{16384, 24576, 8192, 32768, 16384, 24576, 8192, 32768},
		},
		{
			name:   "ScalePerRow",
			opcode: testDNGAxisOpcode(12, area, 2, .5),
			want:   []uint16{32768, 32768, 32768, 32768, 8192, 8192, 8192, 8192},
		},
		{
			name:   "ScalePerColumn",
			opcode: testDNGAxisOpcode(13, area, .5, 1, 2, 4),
			want:   []uint16{8192, 16384, 32768, 65535, 8192, 16384, 32768, 65535},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			opcode, err := parseDNGPointOpcode(test.opcode.id, test.opcode.payload)
			if err != nil {
				t.Fatal(err)
			}
			destination := make([]uint16, len(input))
			if err := applyDNGPointOpcodeRaw(input, destination, width, height, 1, opcode); err != nil {
				t.Fatalf("raw opcode: %v", err)
			}
			for i, want := range test.want {
				if got := destination[i]; got != want {
					t.Errorf("raw sample %d=%d want %d", i, got, want)
				}
			}

			for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
				t.Run(order.String(), func(t *testing.T) {
					fixture := linearDNGFixture(order, width, height, 1, input, map[uint16]dngTestField{
						50717: dngTestLong(order, 65535),
						51008: testDNGOpcodeList(test.opcode),
					})
					decoded, err := decodeLinearDNG(fixture)
					if err != nil {
						t.Fatalf("decode: %v", err)
					}
					for y := 0; y < height; y++ {
						for x := 0; x < width; x++ {
							index := y*width + x
							want := testDNGLinearToSRGB16(test.want[index])
							got := decoded.NRGBA64At(x, y)
							if got.R != want || got.G != want || got.B != want || got.A != 65535 {
								t.Errorf("pixel (%d,%d)=%v want gray %d", x, y, got, want)
							}
						}
					}
				})
			}
		})
	}
}

func TestLinearDNGStage1RowOpcodeCoverageUsesFullRawImage(t *testing.T) {
	order := binary.LittleEndian
	// The encoded row table only covers y=0..1. It covers the cropped active
	// height, but not all four raw rows that Stage 1 processes.
	partialRows := testDNGAxisOpcode(10, testDNGArea(image.Rect(0, 0, 0, 2), 0, 1, 1, 1), 0, 0)
	fixture := linearDNGFixture(order, 4, 4, 1, make([]uint16, 16), map[uint16]dngTestField{
		50829: dngTestLong(order, 1, 0, 3, 4),
		51008: testDNGOpcodeList(partialRows),
	})
	if _, err := decodeLinearDNG(fixture); err == nil {
		t.Fatal("Stage 1 row table that misses raw rows was accepted")
	}
}

func TestLinearDNGStage1RowOpcodeRejectsOutOfRangeTableIndex(t *testing.T) {
	opcode, err := parseDNGPointOpcode(10, testDNGAxisOpcode(10,
		testDNGArea(image.Rect(0, 0, 0, 1), 0, 1, 1, 1), 0).payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := applyDNGPointOpcodeRaw([]uint16{1, 1}, make([]uint16, 2), 1, 2, 1, opcode); err == nil {
		t.Fatal("row table index outside its encoded span was accepted")
	}
}

func TestLinearDNGStage1NormalizedOpcodeOrderAndClipping(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		t.Run(order.String(), func(t *testing.T) {
			area := testDNGArea(image.Rect(0, 0, 1, 1), 0, 1, 1, 1)
			fixture := linearDNGFixture(order, 1, 1, 1, []uint16{40000}, map[uint16]dngTestField{
				50717: dngTestLong(order, 65535),
				51008: testDNGOpcodeList(
					testDNGAxisOpcode(10, area, 32768),
					testDNGAxisOpcode(12, area, .5),
				),
			})
			got, err := decodeLinearDNG(fixture)
			if err != nil {
				t.Fatal(err)
			}
			want := testDNGLinearToSRGB16(32768)
			if pixel := got.NRGBA64At(0, 0); pixel.R != want || pixel.G != want || pixel.B != want {
				t.Fatalf("DeltaPerRow then ScalePerRow got %v want gray %d", pixel, want)
			}
		})
	}
}

func TestDNGStage1AxisOpcodePitchSkipsUnlistedSamples(t *testing.T) {
	const width, height = 4, 4
	input := make([]uint16, width*height)
	for i := range input {
		input[i] = 16384
	}
	for _, test := range []struct {
		name   string
		opcode testDNGOpcode
		want   func(x, y int) uint16
	}{
		{
			name: "row pitch",
			opcode: testDNGAxisOpcode(10, testDNGArea(image.Rect(0, 0, width, height), 0, 1, 2, 1),
				8192, -8192),
			want: func(_, y int) uint16 {
				if y == 0 {
					return 24576
				}
				if y == 2 {
					return 8192
				}
				return 16384
			},
		},
		{
			name: "column pitch",
			opcode: testDNGAxisOpcode(13, testDNGArea(image.Rect(0, 0, width, height), 0, 1, 1, 2),
				.5, 2),
			want: func(x, _ int) uint16 {
				if x == 0 {
					return 8192
				}
				if x == 2 {
					return 32768
				}
				return 16384
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			opcode, err := parseDNGPointOpcode(test.opcode.id, test.opcode.payload)
			if err != nil {
				t.Fatal(err)
			}
			got := make([]uint16, len(input))
			if err := applyDNGPointOpcodeRaw(input, got, width, height, 1, opcode); err != nil {
				t.Fatal(err)
			}
			for y := 0; y < height; y++ {
				for x := 0; x < width; x++ {
					if want := test.want(x, y); got[y*width+x] != want {
						t.Errorf("(%d,%d)=%d want %d", x, y, got[y*width+x], want)
					}
				}
			}
		})
	}
}
