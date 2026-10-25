package lrtl

import (
	"bytes"
	"encoding/json"
	"image"
	_ "image/jpeg"
	"image/png"
	"math"
	"os"
	"testing"
)

// Go's JPEG decoder and LRT's AWT decoder use different chroma upsampling.
// These native samples currently bound the resulting meter delta to 0.007 EV.
const maxNativeJPEGDecodeEVDelta = 0.007

func TestNativeDeflickerCachedImageMeasurementsMatchLRT(t *testing.T) {
	data, err := os.ReadFile("testdata/lrt-native-deflicker-image-7.5.5.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		File                       string
		DecodedFile                string
		Width, Height              int
		ROI                        [4]float32
		Ignore                     bool
		IgnoreBlacks, IgnoreWhites int
		HardCut                    bool
		DirectEV, StaticEV         float64
		PreviewEV                  float64
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) != 6 {
		t.Fatalf("got %d native cached-image measurements, want 6", len(cases))
	}
	for _, tc := range cases {
		t.Run(tc.File, func(t *testing.T) {
			jpegData, err := os.ReadFile("testdata/" + tc.File)
			if err != nil {
				t.Fatal(err)
			}
			img, _, err := image.Decode(bytes.NewReader(jpegData))
			if err != nil {
				t.Fatal(err)
			}
			decodedFile, err := os.Open("testdata/" + tc.DecodedFile)
			if err != nil {
				t.Fatal(err)
			}
			nativeImage, err := png.Decode(decodedFile)
			decodedFile.Close()
			if err != nil {
				t.Fatal(err)
			}
			if got := img.Bounds().Size(); got != image.Pt(tc.Width, tc.Height) {
				t.Fatalf("cached image size=%v, want %dx%d", got, tc.Width, tc.Height)
			}
			if got := nativeImage.Bounds().Size(); got != image.Pt(tc.Width, tc.Height) {
				t.Fatalf("LRT decoded image size=%v, want %dx%d", got, tc.Width, tc.Height)
			}
			if math.Abs(tc.DirectEV-tc.PreviewEV) > 1e-12 || math.Abs(tc.StaticEV-tc.PreviewEV) > 1e-12 {
				t.Fatalf("LRT measurement paths disagree: cached %.17g, static %.17g, preview %.17g", tc.DirectEV, tc.StaticEV, tc.PreviewEV)
			}
			area, err := (lrtReferenceBox(tc.ROI)).rectangle(img.Bounds())
			if err != nil {
				t.Fatal(err)
			}
			got, err := framePreviewEVAreaWithOptions(img, area, previewHistogramOptions{
				Enabled: tc.Ignore, IgnoreBlacks: tc.IgnoreBlacks,
				IgnoreWhites: tc.IgnoreWhites, HardCut: tc.HardCut,
			})
			if err != nil {
				t.Fatal(err)
			}
			nativeValue, err := framePreviewEVAreaWithOptions(nativeImage, area, previewHistogramOptions{
				Enabled: tc.Ignore, IgnoreBlacks: tc.IgnoreBlacks,
				IgnoreWhites: tc.IgnoreWhites, HardCut: tc.HardCut,
			})
			if err != nil {
				t.Fatal(err)
			}
			if delta := math.Abs(nativeValue - tc.PreviewEV); delta > 1e-10 {
				t.Fatalf("LRT decoded raster Preview EV=%.17g, LRT 7.5.5=%.17g (delta %.3g)", nativeValue, tc.PreviewEV, delta)
			}
			if delta := math.Abs(got - tc.PreviewEV); delta > maxNativeJPEGDecodeEVDelta {
				t.Fatalf("Go JPEG cached Preview EV=%.17g, LRT 7.5.5=%.17g (delta %.3g exceeds %.3g EV)", got, tc.PreviewEV, delta, maxNativeJPEGDecodeEVDelta)
			}
		})
	}
}
