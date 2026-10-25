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
