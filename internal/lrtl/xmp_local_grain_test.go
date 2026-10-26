package lrtl

import (
	"bytes"
	"image"
	"image/color"
	"testing"
)

func TestXMPLocalGrainParsesSignedSliderAndSharesGlobalTextureControls(t *testing.T) {
	for _, test := range []struct {
		value string
		want  float64
	}{
		{value: "0.6", want: 60},
		{value: "-0.25", want: -25},
	} {
		packet := localAdjustmentPacket(t, "LocalGrain", test.value)
		if err := packet.SetScalar("crs:GrainSize", "73"); err != nil {
			t.Fatal(err)
		}
		if err := packet.SetScalar("crs:GrainFrequency", "31"); err != nil {
			t.Fatal(err)
		}
		basic, err := readXMPBasicPacket(packet)
		if err != nil || len(basic.local) != 1 {
			t.Fatalf("value %s: local corrections=%d err=%v", test.value, len(basic.local), err)
		}
		correction := basic.local[0]
		if correction.grain != test.want || !correction.active() {
			t.Errorf("value %s: grain=%g active=%v, want %g and active", test.value, correction.grain, correction.active(), test.want)
		}
		if correction.grainSize != 73 || correction.grainFrequency != 31 {
			t.Errorf("shared grain texture controls = (%g,%g), want (73,31)", correction.grainSize, correction.grainFrequency)
		}
	}
	if _, err := readXMPLocalCorrections(localAdjustmentPacket(t, "LocalGrain", "1.01")); err == nil {
		t.Fatal("LocalGrain outside normalized [-1, 1] range was accepted")
	}
	if corrections, err := readXMPLocalCorrections(localAdjustmentPacket(t, "LocalGrain", "0")); err != nil || len(corrections) != 0 {
		t.Fatalf("neutral correction count=%d err=%v, want no correction", len(corrections), err)
	}
}

func TestXMPLocalGrainBlendsThroughMaskAndPreservesAlpha(t *testing.T) {
	for _, value := range []string{"0.6", "-0.6"} {
		t.Run(value, func(t *testing.T) {
			input := image.NewNRGBA64(image.Rect(4, 6, 9, 9))
			for y := input.Bounds().Min.Y; y < input.Bounds().Max.Y; y++ {
				for x := input.Bounds().Min.X; x < input.Bounds().Max.X; x++ {
					input.SetNRGBA64(x, y, color.NRGBA64{R: 30000, G: 28000, B: 26000, A: uint16(42000 + (x-input.Bounds().Min.X)*1000)})
				}
			}
			original := append([]byte(nil), input.Pix...)
			packet := localAdjustmentPacket(t, "LocalGrain", value)
			if err := packet.SetScalar("crs:GrainSize", "80"); err != nil {
				t.Fatal(err)
			}
			if err := packet.SetScalar("crs:GrainFrequency", "20"); err != nil {
				t.Fatal(err)
			}
			basic, err := readXMPBasicPacket(packet)
			if err != nil {
				t.Fatal(err)
			}
			if len(basic.local) != 1 {
				t.Fatalf("local corrections=%d, want 1", len(basic.local))
			}
			correction := basic.local[0]
			actual := applyXMPSpatialCorrections(input, basic)
			changedInside, unchangedOutside := false, true
			bounds := input.Bounds()
			for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
				for x := bounds.Min.X; x < bounds.Max.X; x++ {
					originalPixel := input.NRGBA64At(x, y)
					luminance := 0.2126*srgb16ToLinear[originalPixel.R] + 0.7152*srgb16ToLinear[originalPixel.G] + 0.0722*srgb16ToLinear[originalPixel.B]
					rgb := [3]float64{float64(originalPixel.R) / 65535, float64(originalPixel.G) / 65535, float64(originalPixel.B) / 65535}
					weight := correction.maskWeight(x-bounds.Min.X, y-bounds.Min.Y, bounds.Dx(), bounds.Dy(), luminance, rgb)
					want := xmpApplyGrainPixel(originalPixel, x, y, correction.grain*weight*correction.amount, 80, 20)
					got := color.NRGBA64Model.Convert(actual.At(x, y)).(color.NRGBA64)
					if got != want {
						t.Errorf("pixel (%d,%d) = %#v, want mask-weighted grain %#v (weight %g)", x, y, got, want, weight)
					}
					if got.A != originalPixel.A {
						t.Errorf("grain changed alpha at (%d,%d): got %d, want %d", x, y, got.A, originalPixel.A)
					}
					if weight > 0 && got != originalPixel {
						changedInside = true
					}
					if weight == 0 && got != originalPixel {
						unchangedOutside = false
					}
				}
			}
			if !changedInside {
				t.Fatal("LocalGrain had no effect inside its mask")
			}
			if !unchangedOutside {
				t.Fatal("LocalGrain changed pixels outside its mask")
			}
			if !bytes.Equal(input.Pix, original) {
				t.Fatal("LocalGrain mutated its source image")
			}
		})
	}
}
