package lrtl

import (
	"image"
	"image/color"
	"math"
	"testing"
)

func TestXMPLocalMoireParsesSignedSliderAndRejectsOutOfRange(t *testing.T) {
	for _, test := range []struct {
		value string
		want  float64
	}{
		{value: "0.6", want: 60},
		{value: "-0.25", want: -25},
	} {
		corrections, err := readXMPLocalCorrections(localAdjustmentPacket(t, "LocalMoire", test.value))
		if err != nil || len(corrections) != 1 {
			t.Fatalf("value %s: corrections=%d err=%v", test.value, len(corrections), err)
		}
		if corrections[0].moire != test.want || !corrections[0].active() {
			t.Errorf("value %s: moire=%g active=%v, want %g and active", test.value, corrections[0].moire, corrections[0].active(), test.want)
		}
	}
	if _, err := readXMPLocalCorrections(localAdjustmentPacket(t, "LocalMoire", "-1.01")); err == nil {
		t.Fatal("LocalMoire outside normalized [-1, 1] range was accepted")
	}
	if corrections, err := readXMPLocalCorrections(localAdjustmentPacket(t, "LocalMoire", "0")); err != nil || len(corrections) != 0 {
		t.Fatalf("neutral correction count=%d err=%v, want no correction", len(corrections), err)
	}
}

func TestXMPLocalMoireSmoothsOnlyMaskedChroma(t *testing.T) {
	input := image.NewNRGBA64(image.Rect(4, 6, 9, 9))
	for y := input.Bounds().Min.Y; y < input.Bounds().Max.Y; y++ {
		for x := input.Bounds().Min.X; x < input.Bounds().Max.X; x++ {
			pixel := color.NRGBA64{R: 36000, G: 30000, B: 26000, A: uint16(42000 + (x-input.Bounds().Min.X)*900)}
			if (x+y)%2 != 0 {
				pixel = color.NRGBA64{R: 34000, G: 31000, B: 29000, A: pixel.A}
			}
			input.SetNRGBA64(x, y, pixel)
		}
	}

	correction := xmpLocalCorrection{
		amount: 1,
		moire:  60,
		masks: []xmpLocalMask{{
			kind: localMaskGradient, maskValue: 1, zeroX: 0.5, zeroY: 0.5, fullX: 0, fullY: 0.5,
		}},
	}
	actual := applyXMPLocalCorrections(input, []xmpLocalCorrection{correction})
	filtered := xmpApplyNoiseReduction(input, 0, 50, 0, 100, 50, 100)
	bounds := input.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			original := input.NRGBA64At(x, y)
			candidate := filtered.NRGBA64At(x, y)
			normalizedX := (float64(x-bounds.Min.X) + 0.5) / float64(bounds.Dx())
			maskWeight := math.Max(0, math.Min(1, 1-normalizedX/0.5))
			blend := 0.6 * maskWeight
			want := blendLocalMoirePixel(original, candidate, blend)
			got := color.NRGBA64Model.Convert(actual.At(x, y)).(color.NRGBA64)
			if got != want {
				t.Errorf("pixel (%d,%d) = %#v, want mask-weighted chroma smoothing %#v", x, y, got, want)
			}

			originalY, originalU, originalV := xmpLinearLumaChroma(original)
			_, candidateU, candidateV := xmpLinearLumaChroma(candidate)
			gotY, gotU, gotV := xmpLinearLumaChroma(got)
			wantU := originalU + (candidateU-originalU)*blend
			wantV := originalV + (candidateV-originalV)*blend
			if math.Abs(gotY-originalY) > 2e-5 || math.Abs(gotU-wantU) > 2e-5 || math.Abs(gotV-wantV) > 2e-5 {
				t.Errorf("pixel (%d,%d) luma/chroma = (%g,%g,%g), want (%g,%g,%g)", x, y, gotY, gotU, gotV, originalY, wantU, wantV)
			}
			if got.A != original.A {
				t.Errorf("pixel (%d,%d) alpha = %d, want %d", x, y, got.A, original.A)
			}
			if x == bounds.Min.X && y == bounds.Min.Y && got == original {
				t.Fatal("positive LocalMoire had no effect inside the mask")
			}
			if maskWeight == 0 && got != original {
				t.Errorf("pixel (%d,%d) outside the mask = %#v, want unchanged %#v", x, y, got, original)
			}
		}
	}
}
