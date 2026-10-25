package lrtl

import (
	"encoding/binary"
	"image"
	"math"
	"testing"
)

func testDNGTrimBounds(top, left, bottom, right int32) testDNGOpcode {
	data := binary.BigEndian.AppendUint32(nil, uint32(top))
	data = binary.BigEndian.AppendUint32(data, uint32(left))
	data = binary.BigEndian.AppendUint32(data, uint32(bottom))
	data = binary.BigEndian.AppendUint32(data, uint32(right))
	return testDNGOpcode{6, 0x01030000, 0, data}
}

func TestDNGTrimBoundsParsesSignedImageRectangle(t *testing.T) {
	trim, err := parseDNGTrimBounds(testDNGTrimBounds(2, 3, 7, 11).payload)
	if err != nil {
		t.Fatal(err)
	}
	if trim != image.Rect(3, 2, 11, 7) {
		t.Fatalf("trim=%v", trim)
	}
	if _, err := parseDNGTrimBounds(testDNGTrimBounds(0, 0, 0, 5).payload); err == nil {
		t.Fatal("empty TrimBounds accepted")
	}
	if _, err := parseDNGTrimBounds(testDNGTrimBounds(0, 0, 4, 5).payload[:15]); err == nil {
		t.Fatal("truncated TrimBounds accepted")
	}
}

func TestDNGTrimBoundsPreservesCoordinatesForFollowingOpcodes(t *testing.T) {
	const width, height = 5, 4
	input := make([]float32, width*height)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			input[y*width+x] = float32(y*width+x) / 100
		}
	}
	trim1, _ := parseDNGTrimBounds(testDNGTrimBounds(1, 1, 4, 4).payload)
	trim2, _ := parseDNGTrimBounds(testDNGTrimBounds(2, 2, 4, 4).payload)
	point := dngPointOpcode{
		dngOpcodeArea: dngOpcodeArea{area: image.Rect(3, 3, 4, 4), plane: 0, planes: 1, rowPitch: 1, colPitch: 1},
		id:            8, coefficients: []float64{.25},
	}
	got, bounds, err := applyDNGImageOpcodesWithBounds([]dngImageOpcode{
		{trim: &trim1}, {trim: &trim2}, {point: &point},
	}, input, width, height, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if bounds != image.Rect(2, 2, 4, 4) || len(got) != 4 {
		t.Fatalf("bounds=%v len=%d", bounds, len(got))
	}
	want := []float32{input[2*width+2], input[2*width+3], input[3*width+2], .25}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("pixel %d=%g want %g", index, got[index], want[index])
		}
	}
}

func TestLinearDNGTrimBoundsIntersectsActiveAndDefaultCrop(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		for _, tag := range []uint16{51009, 51022} {
			for flags := uint32(0); flags < 4; flags++ {
				const width, height = 6, 5
				pixels := make([]uint16, width*height)
				for y := 0; y < height; y++ {
					for x := 0; x < width; x++ {
						pixels[y*width+x] = uint16(1200 + y*300 + x*20)
					}
				}
				trim := testDNGTrimBounds(0, 2, 4, 5)
				trim.flags = flags
				fields := map[uint16]dngTestField{
					50829: dngTestLong(order, 1, 1, 5, 6),
					50719: dngTestLong(order, 1, 1),
					50720: dngTestLong(order, 3, 2),
					50717: dngTestLong(order, 65535),
					tag:   testDNGOpcodeList(trim),
				}
				img, err := decodeLinearDNG(linearDNGFixture(order, width, height, 1, pixels, fields))
				if err != nil {
					t.Fatalf("order=%s tag=%d flags=%d: %v", order, tag, flags, err)
				}
				if img.Bounds() != image.Rect(0, 0, 2, 2) {
					t.Fatalf("order=%s tag=%d flags=%d bounds=%v", order, tag, flags, img.Bounds())
				}
				for y := 0; y < 2; y++ {
					for x := 0; x < 2; x++ {
						sourceX, sourceY := 3+x, 2+y
						linear := float64(pixels[sourceY*width+sourceX]) / 65535
						var encoded float64
						if linear <= .0031308 {
							encoded = linear * 12.92
						} else {
							encoded = 1.055*math.Pow(linear, 1/2.4) - .055
						}
						want := uint16(math.Round(encoded * 65535))
						pixel := img.NRGBA64At(x, y)
						if pixel.R != want || pixel.G != want || pixel.B != want {
							t.Fatalf("order=%s tag=%d flags=%d (%d,%d)=%v want gray %d from source (%d,%d)", order, tag, flags, x, y, pixel, want, sourceX, sourceY)
						}
					}
				}
			}
		}
	}
}

func TestLinearDNGTrimBoundsRejectsOutOfImageRectangle(t *testing.T) {
	fixture := linearDNGFixture(binary.LittleEndian, 4, 3, 1, make([]uint16, 12), map[uint16]dngTestField{
		51009: testDNGOpcodeList(testDNGTrimBounds(1, 1, 4, 5)),
	})
	if _, err := decodeLinearDNG(fixture); err == nil {
		t.Fatal("out-of-image TrimBounds accepted")
	}
}

func FuzzDNGTrimBoundsParameters(f *testing.F) {
	f.Add(testDNGTrimBounds(0, 0, 1, 1).payload)
	f.Add(testDNGTrimBounds(-1, -1, 4, 4).payload)
	f.Add([]byte{1, 2, 3})
	f.Fuzz(func(t *testing.T, data []byte) {
		trim, err := parseDNGTrimBounds(data)
		if err == nil {
			pixels := make([]float32, 64)
			_, _, _ = applyDNGImageOpcodesWithBounds([]dngImageOpcode{{trim: &trim}}, pixels, 8, 8, 1, 1)
		}
	})
}
