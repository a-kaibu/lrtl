package lrtl

import (
	"encoding/binary"
	"image"
	"math"
	"testing"
)

func TestTrimDNGStage1PixelsInterleavedPlanes(t *testing.T) {
	const width, height, channels = 4, 3, 3
	source := make([]uint16, width*height*channels)
	for index := range source {
		source[index] = uint16(index + 1)
	}
	got, err := trimDNGStage1Pixels(source, width, height, channels, image.Rect(1, 1, 4, 3))
	if err != nil {
		t.Fatal(err)
	}
	want := []uint16{16, 17, 18, 19, 20, 21, 22, 23, 24, 28, 29, 30, 31, 32, 33, 34, 35, 36}
	if len(got) != len(want) {
		t.Fatalf("trim length=%d want %d", len(got), len(want))
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("trim sample %d=%d want %d", index, got[index], want[index])
		}
	}
	if _, err := trimDNGStage1Pixels(source, width, height, channels, image.Rect(-1, 0, 2, 2)); err == nil {
		t.Fatal("out-of-image trim accepted")
	}
}

func TestDNGStage1TrimRebasesFollowingOpcodeCoordinates(t *testing.T) {
	const width, height = 6, 5
	pixels := make([]uint16, width*height)
	for index := range pixels {
		pixels[index] = uint16(1000 + index*100)
	}
	originalPixels := append([]uint16(nil), pixels...)
	first := image.Rect(1, 1, 6, 5)
	second := image.Rect(1, 0, 4, 3)
	trim1, trim2 := first, second
	point := dngPointOpcode{
		dngOpcodeArea: dngOpcodeArea{area: image.Rect(2, 1, 3, 2), plane: 0, planes: 1, rowPitch: 1, colPitch: 1},
		id:            8,
		coefficients:  []float64{500, 1},
	}
	got, gotWidth, gotHeight, active, offsetX, offsetY, err := applyDNGStage1Opcodes([]dngImageOpcode{
		{trim: &trim1}, {trim: &trim2}, {point: &point},
	}, pixels, width, height, 1, image.Rect(1, 1, 5, 5))
	if err != nil {
		t.Fatal(err)
	}
	if gotWidth != 3 || gotHeight != 3 || active != image.Rect(0, 0, 3, 3) || offsetX != 1 || offsetY != 0 {
		t.Fatalf("dimensions=%dx%d ActiveArea=%v black delta offset=(%d,%d)", gotWidth, gotHeight, active, offsetX, offsetY)
	}
	// The point opcode uses the second trim's zero-based coordinate frame.
	for y := 0; y < gotHeight; y++ {
		for x := 0; x < gotWidth; x++ {
			originalX, originalY := 2+x, 1+y
			want := originalPixels[originalY*width+originalX]
			if x == 2 && y == 1 {
				want += 500
			}
			if got[y*gotWidth+x] != want {
				t.Fatalf("sample (%d,%d)=%d want %d", x, y, got[y*gotWidth+x], want)
			}
		}
	}
}

func TestLinearDNGStage1TrimAndDefaultCrop(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		t.Run(order.String(), func(t *testing.T) {
			const width, height = 6, 5
			pixels := make([]uint16, width*height)
			for y := 0; y < height; y++ {
				for x := 0; x < width; x++ {
					pixels[y*width+x] = uint16(5000 + y*500 + x*100)
				}
			}
			data := linearDNGFixture(order, width, height, 1, pixels, map[uint16]dngTestField{
				50829: dngTestLong(order, 1, 1, 5, 5),
				50719: dngTestDouble(order, 1, 1),
				50720: dngTestDouble(order, 2, 2),
				51008: testDNGOpcodeList(
					testDNGTrimBounds(1, 1, 5, 6),
					testDNGPolynomial(testDNGArea(image.Rect(0, 0, 5, 4), 0, 1, 1, 1), 0, 2),
				),
			})
			img, err := decodeLinearDNG(data)
			if err != nil {
				t.Fatal(err)
			}
			if img.Bounds() != image.Rect(0, 0, 2, 2) {
				t.Fatalf("bounds=%v want 2x2", img.Bounds())
			}
			for y := 0; y < 2; y++ {
				for x := 0; x < 2; x++ {
					sourceX, sourceY := x+2, y+2
					linear := uint16(pixels[sourceY*width+sourceX] * 2)
					want := testDNGLinearToSRGB16(linear)
					pixel := img.NRGBA64At(x, y)
					if pixel.R != want || pixel.G != want || pixel.B != want {
						t.Fatalf("(%d,%d)=%v want gray %d from Stage 1 source (%d,%d)", x, y, pixel, want, sourceX, sourceY)
					}
				}
			}
		})
	}
}

func TestLinearDNGStage1TrimSlicesBlackDeltas(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		t.Run(order.String(), func(t *testing.T) {
			const width, height = 6, 5
			pixels := make([]uint16, width*height)
			for index := range pixels {
				pixels[index] = 10000
			}
			data := linearDNGFixture(order, width, height, 1, pixels, map[uint16]dngTestField{
				50829: dngTestLong(order, 1, 1, 5, 5),
				50715: dngTestDouble(order, 10, 20, 30, 40),
				50716: dngTestDouble(order, 1, 2, 3, 4),
				51008: testDNGOpcodeList(
					testDNGTrimBounds(1, 2, 5, 6),
					testDNGPolynomial(testDNGArea(image.Rect(1, 1, 2, 2), 0, 1, 1, 1), 1000, 1),
				),
			})
			img, err := decodeLinearDNG(data)
			if err != nil {
				t.Fatal(err)
			}
			if img.Bounds() != image.Rect(0, 0, 3, 4) {
				t.Fatalf("bounds=%v want 3x4", img.Bounds())
			}
			for y := 0; y < 4; y++ {
				for x := 0; x < 3; x++ {
					raw := 10000.0
					if x == 1 && y == 1 {
						raw += 1000
					}
					linear := (raw - float64([]float64{20, 30, 40}[x]) - float64([]float64{1, 2, 3, 4}[y])) / (65535 - 40 - 4)
					encoded := linear
					if encoded <= .0031308 {
						encoded *= 12.92
					} else {
						encoded = 1.055*math.Pow(encoded, 1/2.4) - .055
					}
					want := uint16(math.Round(encoded * 65535))
					pixel := img.NRGBA64At(x, y)
					if pixel.R != want || pixel.G != want || pixel.B != want {
						t.Fatalf("(%d,%d)=%v want gray %d; normalized linear=%g", x, y, pixel, want, linear)
					}
				}
			}
		})
	}
}

func FuzzDNGStage1TrimPixels(f *testing.F) {
	f.Add([]byte{1, 2, 3, 4, 5, 6}, uint8(1), uint8(2), uint8(1), uint8(2))
	f.Add([]byte{9}, uint8(0), uint8(0), uint8(1), uint8(1))
	f.Fuzz(func(t *testing.T, samples []byte, left, top, right, bottom uint8) {
		const width, height, channels = 6, 4, 1
		pixels := make([]uint16, width*height)
		for index := range pixels {
			if index < len(samples) {
				pixels[index] = uint16(samples[index])
			}
		}
		bounds := image.Rect(int(left), int(top), int(right), int(bottom))
		got, err := trimDNGStage1Pixels(pixels, width, height, channels, bounds)
		if err == nil {
			if bounds.Empty() || !bounds.In(image.Rect(0, 0, width, height)) || len(got) != bounds.Dx()*bounds.Dy() {
				t.Fatalf("invalid accepted trim %v with %d samples", bounds, len(got))
			}
		}
	})
}
