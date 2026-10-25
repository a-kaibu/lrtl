package lrtl

import (
	"encoding/json"
	"image"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// These native samples bound the remaining decoder and rounding delta to 0.0024 EV.
const maxNativeJPEGDecodeEVDelta = 0.0024

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
			img, format, err := decodeLRTMeteringImage(jpegData)
			if err != nil {
				t.Fatal(err)
			}
			if format != "jpeg" {
				t.Fatalf("cached source format=%q, want jpeg", format)
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
			meanPixelDelta, maxPixelDelta := lrtRGBDifference(img, nativeImage)
			if meanPixelDelta > 0.25 || maxPixelDelta > 3 {
				t.Fatalf("decoded JPEG RGB delta: mean %.3f, max %d; want mean <= 0.25 and max <= 3", meanPixelDelta, maxPixelDelta)
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

	// Exercise both production analysis entry points on a full-frame native
	// fixture, in addition to the area-level oracle checks above.
	frame := Frame{Image: filepath.Join("testdata", cases[0].File)}
	preview, err := analyzePreviewEVSequence([]Frame{frame}, imageInputOptions{})
	if err != nil || len(preview) != 1 || math.Abs(preview[0]-cases[0].PreviewEV) > maxNativeJPEGDecodeEVDelta {
		t.Fatalf("Preview EV pipeline=%v, LRT 7.5.5=%.17g, err=%v", preview, cases[0].PreviewEV, err)
	}
	packet, err := PacketForFrame(frame)
	if err != nil {
		t.Fatal(err)
	}
	options := imageInputOptions{NativeDeflicker: nativeDeflickerOptions{Enabled: true}}
	source := newFrameInputCache(options, false)
	defer source.close()
	measurements, err := measureDeflickerPackets([]Frame{frame}, image.Rectangle{}, options, []*XMPPacket{packet}, source, nil)
	if err != nil || len(measurements) != 1 || math.Abs(measurements[0]-cases[0].PreviewEV) > maxNativeJPEGDecodeEVDelta {
		t.Fatalf("Deflicker measurement pipeline=%v, LRT 7.5.5=%.17g, err=%v", measurements, cases[0].PreviewEV, err)
	}
}

func lrtRGBDifference(a, b image.Image) (mean float64, max uint8) {
	if a.Bounds().Size() != b.Bounds().Size() {
		return math.Inf(1), 255
	}
	var sum uint64
	count := 0
	for y := 0; y < a.Bounds().Dy(); y++ {
		for x := 0; x < a.Bounds().Dx(); x++ {
			ar, ag, ab, _ := a.At(a.Bounds().Min.X+x, a.Bounds().Min.Y+y).RGBA()
			br, bg, bb, _ := b.At(b.Bounds().Min.X+x, b.Bounds().Min.Y+y).RGBA()
			for _, values := range [][2]uint32{{ar, br}, {ag, bg}, {ab, bb}} {
				first, second := values[0]>>8, values[1]>>8
				var delta uint32
				if first > second {
					delta = first - second
				} else {
					delta = second - first
				}
				sum += uint64(delta)
				count++
				if delta > uint32(max) {
					max = uint8(delta)
				}
			}
		}
	}
	return float64(sum) / float64(count), max
}
