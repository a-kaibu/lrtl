package lrtl

import (
	"image"
	"image/color"
	"math"
	"testing"
)

func TestXMPLocalDefringeParsesSignedSliderAndRejectsOutOfRange(t *testing.T) {
	for _, test := range []struct {
		value string
		want  float64
	}{
		{value: "0.7", want: 70},
		{value: "-0.25", want: -25},
	} {
		corrections, err := readXMPLocalCorrections(localAdjustmentPacket(t, "LocalDefringe", test.value))
		if err != nil || len(corrections) != 1 {
			t.Fatalf("value %s: corrections=%d err=%v", test.value, len(corrections), err)
		}
		if corrections[0].defringe != test.want || !corrections[0].active() {
			t.Errorf("value %s: defringe=%g active=%v, want %g and active", test.value, corrections[0].defringe, corrections[0].active(), test.want)
		}
	}
	if _, err := readXMPLocalCorrections(localAdjustmentPacket(t, "LocalDefringe", "1.01")); err == nil {
		t.Fatal("LocalDefringe outside normalized [-1, 1] range was accepted")
	}
	if corrections, err := readXMPLocalCorrections(localAdjustmentPacket(t, "LocalDefringe", "0")); err != nil || len(corrections) != 0 {
		t.Fatalf("neutral correction count=%d err=%v, want no correction", len(corrections), err)
	}
}

func TestXMPLocalDefringeTargetsFringeColorsAtMaskedEdges(t *testing.T) {
	input := image.NewNRGBA64(image.Rect(4, 6, 9, 9))
	for y := input.Bounds().Min.Y; y < input.Bounds().Max.Y; y++ {
		for x := input.Bounds().Min.X; x < input.Bounds().Max.X; x++ {
			input.SetNRGBA64(x, y, color.NRGBA64{R: 10000, G: 10000, B: 10000, A: 50000})
		}
	}
	purple := color.NRGBA64{R: 50000, G: 12000, B: 50000, A: 50000}
	input.SetNRGBA64(5, 7, purple)
	outside := color.NRGBA64{R: 50000, G: 12000, B: 50000, A: 50000}
	input.SetNRGBA64(8, 7, outside)

	correction := xmpLocalCorrection{
		amount:   1,
		defringe: 70,
		masks: []xmpLocalMask{{
			kind: localMaskGradient, maskValue: 1, zeroX: 0.5, zeroY: 0.5, fullX: 0, fullY: 0.5,
		}},
	}
	actual := applyXMPLocalCorrections(input, []xmpLocalCorrection{correction})
	detail := xmpLuminanceDetail(input, 1)
	maskWeight := 1 - 0.3/0.5
	want := applyLocalDefringe(purple, detail[1*input.Bounds().Dx()+1], 0.7*maskWeight)
	got := color.NRGBA64Model.Convert(actual.At(5, 7)).(color.NRGBA64)
	if got != want {
		t.Fatalf("masked purple fringe = %#v, want %#v", got, want)
	}
	if chromaMagnitude(got) >= chromaMagnitude(purple) {
		t.Fatalf("purple fringe chroma = %g, want less than original %g", chromaMagnitude(got), chromaMagnitude(purple))
	}
	originalLuma, _, _ := xmpLinearLumaChroma(purple)
	gotLuma, _, _ := xmpLinearLumaChroma(got)
	if math.Abs(gotLuma-originalLuma) > 2e-5 || got.A != purple.A {
		t.Fatalf("defringe changed luminance or alpha: got luma=%g alpha=%d, want luma=%g alpha=%d", gotLuma, got.A, originalLuma, purple.A)
	}
	gotOutside := color.NRGBA64Model.Convert(actual.At(8, 7)).(color.NRGBA64)
	if gotOutside != outside {
		t.Fatalf("pixel outside mask = %#v, want unchanged %#v", gotOutside, outside)
	}
}

func TestXMPLocalDefringeOnlyTargetsGreenOrPurpleFringesAtEdges(t *testing.T) {
	for _, test := range []struct {
		name  string
		pixel color.NRGBA64
	}{
		{name: "green", pixel: color.NRGBA64{R: 12000, G: 42000, B: 12000, A: 65535}},
		{name: "purple", pixel: color.NRGBA64{R: 50000, G: 12000, B: 50000, A: 65535}},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := applyLocalDefringe(test.pixel, 127, 1)
			if chromaMagnitude(got) >= chromaMagnitude(test.pixel) {
				t.Fatalf("fringe chroma = %g, want less than original %g", chromaMagnitude(got), chromaMagnitude(test.pixel))
			}
			originalLuma, _, _ := xmpLinearLumaChroma(test.pixel)
			gotLuma, _, _ := xmpLinearLumaChroma(got)
			if math.Abs(gotLuma-originalLuma) > 2e-5 || got.A != test.pixel.A {
				t.Fatalf("fringe removal changed luminance/alpha: got (%g,%d), want (%g,%d)", gotLuma, got.A, originalLuma, test.pixel.A)
			}
			amplified := applyLocalDefringe(test.pixel, 127, -1)
			if chromaMagnitude(amplified) <= chromaMagnitude(test.pixel) {
				t.Fatalf("negative Defringe chroma = %g, want more than original %g", chromaMagnitude(amplified), chromaMagnitude(test.pixel))
			}
		})
	}

	red := color.NRGBA64{R: 50000, G: 10000, B: 10000, A: 65535}
	if got := applyLocalDefringe(red, 127, 1); got != red {
		t.Fatalf("non-fringe red edge = %#v, want unchanged %#v", got, red)
	}
}

func TestXMPLocalDefringeLeavesFlatColorUnchanged(t *testing.T) {
	flat := image.NewNRGBA64(image.Rect(0, 0, 4, 3))
	purple := color.NRGBA64{R: 50000, G: 12000, B: 50000, A: 42000}
	for y := 0; y < flat.Bounds().Dy(); y++ {
		for x := 0; x < flat.Bounds().Dx(); x++ {
			flat.SetNRGBA64(x, y, purple)
		}
	}
	correction := xmpLocalCorrection{
		amount:   1,
		defringe: 100,
		masks: []xmpLocalMask{{
			kind: localMaskLuminanceRange, maskValue: 1,
			luminanceRange: &xmpLocalLuminanceRange{lowOuter: 0, lowInner: 0, highInner: 1, highOuter: 1},
		}},
	}
	actual := applyXMPLocalCorrections(flat, []xmpLocalCorrection{correction})
	for y := 0; y < flat.Bounds().Dy(); y++ {
		for x := 0; x < flat.Bounds().Dx(); x++ {
			got := color.NRGBA64Model.Convert(actual.At(x, y)).(color.NRGBA64)
			if got != purple {
				t.Errorf("flat pixel (%d,%d) = %#v, want unchanged %#v", x, y, got, purple)
			}
		}
	}
}

func chromaMagnitude(pixel color.NRGBA64) float64 {
	_, blueDifference, redDifference := xmpLinearLumaChroma(pixel)
	return math.Hypot(blueDifference, redDifference)
}
