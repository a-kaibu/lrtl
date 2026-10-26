package lrtl

import (
	"bytes"
	"image"
	"image/color"
	"testing"
)

func TestReadXMPBasicGrain(t *testing.T) {
	packet := EmptyXMPPacket()
	for name, value := range map[string]string{
		"GrainAmount":    "73",
		"GrainSize":      "61",
		"GrainFrequency": "28",
	} {
		if err := packet.SetScalar("crs:"+name, value); err != nil {
			t.Fatal(err)
		}
	}
	got, err := readXMPBasicPacket(packet)
	if err != nil {
		t.Fatal(err)
	}
	if got.grainAmount != 73 || got.grainSize != 61 || got.grainFrequency != 28 {
		t.Fatalf("grain controls = amount %v, size %v, frequency %v", got.grainAmount, got.grainSize, got.grainFrequency)
	}

	defaults, err := readXMPBasicPacket(EmptyXMPPacket())
	if err != nil {
		t.Fatal(err)
	}
	if defaults.grainAmount != 0 || defaults.grainSize != 25 || defaults.grainFrequency != 50 {
		t.Fatalf("default grain controls = amount %v, size %v, frequency %v", defaults.grainAmount, defaults.grainSize, defaults.grainFrequency)
	}
}

func TestReadXMPBasicGrainRejectsInvalidValues(t *testing.T) {
	for _, property := range []string{"GrainAmount", "GrainSize", "GrainFrequency"} {
		for _, value := range []string{"-1", "101", "NaN", "Infinity"} {
			t.Run(property+"/"+value, func(t *testing.T) {
				packet := EmptyXMPPacket()
				if err := packet.SetScalar("crs:"+property, value); err != nil {
					t.Fatal(err)
				}
				if _, err := readXMPBasicPacket(packet); err == nil {
					t.Fatalf("accepted crs:%s=%s", property, value)
				}
			})
		}
	}
}

func TestXMPGrainDeterministicAndPreservesAlpha(t *testing.T) {
	input := image.NewNRGBA64(image.Rect(0, 0, 32, 24))
	for y := 0; y < input.Bounds().Dy(); y++ {
		for x := 0; x < input.Bounds().Dx(); x++ {
			alpha := uint16(65535)
			if (x+y)%7 == 0 {
				alpha = 32768
			}
			input.SetNRGBA64(x, y, color.NRGBA64{R: 30000, G: 30000, B: 30000, A: alpha})
		}
	}
	original := append([]byte(nil), input.Pix...)
	got := applyXMPBasic(input, xmpBasicAdjustments{grainAmount: 80, grainSize: 25, grainFrequency: 50})
	gotAgain := applyXMPBasic(input, xmpBasicAdjustments{grainAmount: 80, grainSize: 25, grainFrequency: 50})
	if !bytes.Equal(got.Pix, gotAgain.Pix) {
		t.Fatal("same image and grain controls produced different pixels")
	}
	if !bytes.Equal(input.Pix, original) {
		t.Fatal("grain application mutated its source image")
	}
	if bytes.Equal(got.Pix, original) {
		t.Fatal("non-zero grain amount did not change pixels")
	}
	for y := 0; y < input.Bounds().Dy(); y++ {
		for x := 0; x < input.Bounds().Dx(); x++ {
			if got.NRGBA64At(x, y).A != input.NRGBA64At(x, y).A {
				t.Fatalf("grain changed alpha at (%d,%d)", x, y)
			}
		}
	}
}

func TestXMPGrainOnlyEntersFrameRenderPipeline(t *testing.T) {
	input := image.NewNRGBA64(image.Rect(0, 0, 16, 16))
	for y := 0; y < input.Bounds().Dy(); y++ {
		for x := 0; x < input.Bounds().Dx(); x++ {
			input.SetNRGBA64(x, y, color.NRGBA64{R: 30000, G: 30000, B: 30000, A: 65535})
		}
	}
	packet := EmptyXMPPacket()
	for name, value := range map[string]string{"GrainAmount": "100", "GrainSize": "25", "GrainFrequency": "50"} {
		if err := packet.SetScalar("crs:"+name, value); err != nil {
			t.Fatal(err)
		}
	}
	adjustments, err := readXMPBasicPacket(packet)
	if err != nil {
		t.Fatal(err)
	}
	got := applyXMPBasicFrame(input, adjustments)
	if bytes.Equal(got.(*image.NRGBA64).Pix, input.Pix) {
		t.Fatal("XMP grain-only adjustments were skipped by the frame render path")
	}
}

func TestXMPGrainControlsChangePatternAndZeroIsNoOp(t *testing.T) {
	input := image.NewNRGBA64(image.Rect(0, 0, 32, 24))
	for y := 0; y < input.Bounds().Dy(); y++ {
		for x := 0; x < input.Bounds().Dx(); x++ {
			input.SetNRGBA64(x, y, color.NRGBA64{R: 30000, G: 30000, B: 30000, A: 65535})
		}
	}

	baseline := applyXMPBasic(input, xmpBasicAdjustments{})
	neutral := applyXMPBasic(input, xmpBasicAdjustments{grainSize: 100, grainFrequency: 100})
	if !bytes.Equal(baseline.Pix, neutral.Pix) {
		t.Fatal("zero grain amount changed pixels")
	}
	baseGrain := applyXMPBasic(input, xmpBasicAdjustments{grainAmount: 70, grainSize: 25, grainFrequency: 50})
	sizeGrain := applyXMPBasic(input, xmpBasicAdjustments{grainAmount: 70, grainSize: 80, grainFrequency: 50})
	frequencyGrain := applyXMPBasic(input, xmpBasicAdjustments{grainAmount: 70, grainSize: 25, grainFrequency: 95})
	if bytes.Equal(baseGrain.Pix, sizeGrain.Pix) {
		t.Fatal("GrainSize did not change the grain pattern")
	}
	if bytes.Equal(baseGrain.Pix, frequencyGrain.Pix) {
		t.Fatal("GrainFrequency did not change the grain pattern")
	}
}

func TestXMPGrainAddsBlueComponentAtDocumentedSizeThreshold(t *testing.T) {
	neutral := color.NRGBA64{R: 30000, G: 30000, B: 30000, A: 65535}
	blueComponentPixels := 0
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			belowThreshold := xmpApplyGrainPixel(neutral, x, y, 100, 24, 50)
			if belowThreshold.R != belowThreshold.G || belowThreshold.G != belowThreshold.B {
				t.Fatalf("size 24 unexpectedly added chroma at (%d,%d): %#v", x, y, belowThreshold)
			}
			atThreshold := xmpApplyGrainPixel(neutral, x, y, 100, 25, 50)
			if atThreshold.B != atThreshold.R || atThreshold.B != atThreshold.G {
				blueComponentPixels++
			}
		}
	}
	if blueComponentPixels == 0 {
		t.Fatal("size 25 did not add a blue grain component")
	}
}
