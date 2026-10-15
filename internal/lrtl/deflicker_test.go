package lrtl

import (
	"image"
	"image/color"
	"math"
	"path/filepath"
	"testing"
)

func TestParseReferenceArea(t *testing.T) {
	area, err := parseReferenceArea("12, 20, 80, 40")
	if err != nil {
		t.Fatal(err)
	}
	if want := image.Rect(12, 20, 92, 60); area != want {
		t.Fatalf("got %v, want %v", area, want)
	}
	for _, invalid := range []string{"1,2,3", "x,2,3,4", "-1,0,4,4", "0,0,0,1"} {
		if _, err := parseReferenceArea(invalid); err == nil {
			t.Errorf("expected %q to be rejected", invalid)
		}
	}
}

func TestFrameLuminanceUsesReferenceArea(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 16, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 16; x++ {
			value := uint8(32)
			if x >= 8 {
				value = 220
			}
			img.SetNRGBA(x, y, color.NRGBA{R: value, G: value, B: value, A: 255})
		}
	}
	left, err := frameLuminance(img, image.Rect(0, 0, 8, 8))
	if err != nil {
		t.Fatal(err)
	}
	right, err := frameLuminance(img, image.Rect(8, 0, 16, 8))
	if err != nil {
		t.Fatal(err)
	}
	if !(left > 0 && right > left*20) {
		t.Fatalf("reference ROI did not isolate brightness: left=%f right=%f", left, right)
	}
}

func TestDeflickerGainsConvergeAcrossPasses(t *testing.T) {
	observed := []float64{0.3, 0.6, 0.3}
	onePass, err := deflickerGains(observed, 1, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	multiplePasses, err := deflickerGains(observed, 1, 4, true)
	if err != nil {
		t.Fatal(err)
	}
	onePassResidual := math.Abs(observed[1]*onePass[1] - observed[0]*onePass[0])
	multiplePassResidual := math.Abs(observed[1]*multiplePasses[1] - observed[0]*multiplePasses[0])
	if !(multiplePassResidual < onePassResidual && multiplePassResidual < 0.01) {
		t.Fatalf("passes did not reduce flicker enough: one=%f multi=%f", onePassResidual, multiplePassResidual)
	}
	if math.Abs(observed[0]*multiplePasses[0]-observed[2]*multiplePasses[2]) > 1e-9 {
		t.Fatal("equal input luminances received different gains")
	}
}

func TestDeflickerSmoothsShortTermVariationButKeepsTrend(t *testing.T) {
	observed := []float64{0.1, 0.2, 0.4, 0.8, 1.6}
	gains, err := deflickerGains(observed, 2, 2, false)
	if err != nil {
		t.Fatal(err)
	}
	corrected := make([]float64, len(observed))
	for index := range observed {
		corrected[index] = observed[index] * gains[index]
	}
	if !(corrected[len(corrected)-1] > corrected[0]*4) {
		t.Fatalf("long-term brightness trend was flattened: %v", corrected)
	}
}

func TestApplyExposureGainRaisesLinearLuminance(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	img.SetNRGBA(0, 0, color.NRGBA{R: 100, G: 100, B: 100, A: 255})
	result := applyExposureGain(img, 2)
	if got := result.NRGBAAt(0, 0).R; got <= 100 || got > 180 {
		t.Fatalf("unexpected 1 EV output red channel: %d", got)
	}
}

func TestRenderSequenceRejectsOutOfBoundsDeflickerReference(t *testing.T) {
	directory := t.TempDir()
	framePath := filepath.Join(directory, "frame1.jpg")
	writeRenderFrame(t, framePath, "jpeg", 16, 16)
	frames, err := DiscoverSequence(directory)
	if err != nil {
		t.Fatal(err)
	}
	err = RenderSequence(frames, filepath.Join(directory, "out.avi"), RenderOptions{
		FPS: 24, Quality: 90, Deflicker: true, Reference: image.Rect(10, 10, 20, 20),
		SmoothRadius: 2, DeflickerPasses: 1,
	})
	if err == nil {
		t.Fatal("expected out-of-bounds reference area to be rejected")
	}
}
