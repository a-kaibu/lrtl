package lrtl

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
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

func TestLRTJPEGChromaSubsamplingMatchesNativeRaster(t *testing.T) {
	data, err := os.ReadFile("testdata/lrt-native-jpeg-subsampling-7.5.5.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		File, DecodedFile  string
		Encoder            string
		Subsampling        string
		DirectEV, StaticEV float64
		MaxRGBMeanDelta    float64
		MaxRGBChannelDelta uint8
		MaxEVDelta         float64
	}
	if err := json.Unmarshal(data, &cases); err != nil || len(cases) != 8 {
		t.Fatalf("invalid native JPEG subsampling oracle: cases=%d err=%v", len(cases), err)
	}
	for _, tc := range cases {
		t.Run(tc.Encoder+"/"+tc.File, func(t *testing.T) {
			// Keep fixture-specific oracle bounds explicit and capped. The
			// ImageMagick samples expose small but real decoder differences that
			// do not appear in the FFmpeg samples, especially in Preview EV.
			if tc.Encoder == "" || tc.MaxRGBMeanDelta <= 0 || tc.MaxRGBMeanDelta > 0.3 || tc.MaxRGBChannelDelta == 0 || tc.MaxRGBChannelDelta > 3 || tc.MaxEVDelta <= 0 || tc.MaxEVDelta > 0.0015 {
				t.Fatalf("invalid JPEG oracle bounds: encoder=%q mean=%g channel=%d EV=%g", tc.Encoder, tc.MaxRGBMeanDelta, tc.MaxRGBChannelDelta, tc.MaxEVDelta)
			}
			data, err := os.ReadFile("testdata/" + tc.File)
			if err != nil {
				t.Fatal(err)
			}
			standard, err := jpeg.Decode(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			decodedYCbCr, ok := standard.(*image.YCbCr)
			ratio := image.YCbCrSubsampleRatio(-1)
			switch tc.Subsampling {
			case "4:2:0":
				ratio = image.YCbCrSubsampleRatio420
			case "4:2:2":
				ratio = image.YCbCrSubsampleRatio422
			case "4:4:4":
				ratio = image.YCbCrSubsampleRatio444
			case "4:4:0":
				ratio = image.YCbCrSubsampleRatio440
			case "4:1:1":
				ratio = image.YCbCrSubsampleRatio411
			case "4:1:0":
				ratio = image.YCbCrSubsampleRatio410
			default:
				t.Fatalf("unexpected native subsampling %q", tc.Subsampling)
			}
			if !ok {
				t.Fatalf("JPEG decoder returned %T, want *image.YCbCr with subsampling=%v", standard, ratio)
			}
			if decodedYCbCr.SubsampleRatio != ratio {
				t.Fatalf("JPEG subsampling=%v, want %v", decodedYCbCr.SubsampleRatio, ratio)
			}
			got, format, err := decodeLRTMeteringImage(data)
			if err != nil || format != "jpeg" {
				t.Fatalf("decode LRT metering JPEG: format=%q err=%v", format, err)
			}
			file, err := os.Open("testdata/" + tc.DecodedFile)
			if err != nil {
				t.Fatal(err)
			}
			want, err := png.Decode(file)
			file.Close()
			if err != nil {
				t.Fatal(err)
			}
			if got.Bounds().Size() != want.Bounds().Size() {
				t.Fatalf("decoded JPEG size=%v, LRT raster size=%v", got.Bounds().Size(), want.Bounds().Size())
			}
			meanDelta, maxDelta := lrtRGBDifference(got, want)
			gotEV, err := framePreviewEVArea(got, got.Bounds())
			if err != nil {
				t.Fatal(err)
			}
			if math.Abs(tc.DirectEV-tc.StaticEV) > 1e-12 {
				t.Fatalf("LRT measurement paths disagree: cached %.17g, static %.17g", tc.DirectEV, tc.StaticEV)
			}
			lrtRasterEV, err := framePreviewEVArea(want, want.Bounds())
			if err != nil {
				t.Fatal(err)
			}
			if delta := math.Abs(lrtRasterEV - tc.DirectEV); delta > 1e-10 {
				t.Fatalf("LRT decoded raster Preview EV=%.17g, LRT cached Preview EV=%.17g (delta %.3g)", lrtRasterEV, tc.DirectEV, delta)
			}
			if meanDelta > tc.MaxRGBMeanDelta || maxDelta > tc.MaxRGBChannelDelta {
				t.Fatalf("decoded JPEG RGB delta: mean %.3f, max %d; want mean <= %.3f and max <= %d", meanDelta, maxDelta, tc.MaxRGBMeanDelta, tc.MaxRGBChannelDelta)
			}
			if delta := math.Abs(gotEV - tc.DirectEV); delta > tc.MaxEVDelta {
				t.Fatalf("Preview EV %.17g differs from LRT 7.5.5 %.17g (delta %.3g, limit %.3g)", gotEV, tc.DirectEV, delta, tc.MaxEVDelta)
			}

			frame := Frame{Image: filepath.Join("testdata", tc.File)}
			preview, err := analyzePreviewEVSequence([]Frame{frame}, imageInputOptions{})
			if err != nil || len(preview) != 1 || math.Abs(preview[0]-tc.DirectEV) > tc.MaxEVDelta {
				t.Fatalf("Preview EV pipeline=%v, LRT 7.5.5=%.17g, limit=%.3g, err=%v", preview, tc.DirectEV, tc.MaxEVDelta, err)
			}
			packet, err := PacketForFrame(frame)
			if err != nil {
				t.Fatal(err)
			}
			options := imageInputOptions{NativeDeflicker: nativeDeflickerOptions{Enabled: true}}
			source := newFrameInputCache(options, false)
			defer source.close()
			measurements, err := measureDeflickerPackets([]Frame{frame}, image.Rectangle{}, options, []*XMPPacket{packet}, source, nil)
			if err != nil || len(measurements) != 1 || math.Abs(measurements[0]-tc.DirectEV) > tc.MaxEVDelta {
				t.Fatalf("Deflicker measurement pipeline=%v, LRT 7.5.5=%.17g, limit=%.3g, err=%v", measurements, tc.DirectEV, tc.MaxEVDelta, err)
			}
		})
	}
}

func TestNativeDeflickerJPEGICCIgnoredByLRT(t *testing.T) {
	var oracle struct {
		File                 string
		DecodedFile          string
		ICCDescription       string
		DirectEV             float64
		TaggedJPEGHash       string
		UntaggedJPEGHash     string
		LRTDecodedRasterHash string
	}
	data, err := os.ReadFile("testdata/lrt-icc-jpeg-metering-7.5.5.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &oracle); err != nil {
		t.Fatal(err)
	}
	tagged, err := os.ReadFile("testdata/" + oracle.File)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(tagged)); got != oracle.TaggedJPEGHash {
		t.Fatalf("tagged JPEG SHA-256=%s, want %s", got, oracle.TaggedJPEGHash)
	}
	profile, err := readJPEGICC(bytes.NewReader(tagged))
	if err != nil {
		t.Fatalf("read embedded ICC profile: %v", err)
	}
	if got, err := iccDescription(profile); err != nil || got != oracle.ICCDescription {
		t.Fatalf("embedded ICC description=%q, err=%v; want %q", got, err, oracle.ICCDescription)
	}

	untagged, err := stripJPEGICCForMeteringTest(tagged)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(untagged)); got != oracle.UntaggedJPEGHash {
		t.Fatalf("untagged JPEG SHA-256=%s, want %s", got, oracle.UntaggedJPEGHash)
	}
	withProfile, format, err := decodeLRTMeteringImage(tagged)
	if err != nil || format != "jpeg" {
		t.Fatalf("decode ICC-tagged JPEG: format=%q err=%v", format, err)
	}
	withoutProfile, format, err := decodeLRTMeteringImage(untagged)
	if err != nil || format != "jpeg" {
		t.Fatalf("decode ICC-free JPEG: format=%q err=%v", format, err)
	}
	if mean, max := lrtRGBDifference(withProfile, withoutProfile); mean != 0 || max != 0 {
		t.Fatalf("ICC changed Go metering raster: mean RGB delta %.6f, max %d", mean, max)
	}
	withEV, err := framePreviewEVArea(withProfile, withProfile.Bounds())
	if err != nil {
		t.Fatal(err)
	}
	withoutEV, err := framePreviewEVArea(withoutProfile, withoutProfile.Bounds())
	if err != nil {
		t.Fatal(err)
	}
	if withEV != withoutEV {
		t.Fatalf("ICC changed Go metering EV: with profile %.17g, without %.17g", withEV, withoutEV)
	}

	file, err := os.Open("testdata/" + oracle.DecodedFile)
	if err != nil {
		t.Fatal(err)
	}
	nativeRaster, err := png.Decode(file)
	file.Close()
	if err != nil {
		t.Fatal(err)
	}
	rasterBytes, err := os.ReadFile("testdata/" + oracle.DecodedFile)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(rasterBytes)); got != oracle.LRTDecodedRasterHash {
		t.Fatalf("LRT decoded raster SHA-256=%s, want %s", got, oracle.LRTDecodedRasterHash)
	}
	lrtEV, err := framePreviewEVArea(nativeRaster, nativeRaster.Bounds())
	if err != nil {
		t.Fatal(err)
	}
	if delta := math.Abs(lrtEV - oracle.DirectEV); delta > 1e-10 {
		t.Fatalf("LRT raster EV=%.17g, oracle EV=%.17g (delta %.3g)", lrtEV, oracle.DirectEV, delta)
	}
	if delta := math.Abs(withEV - oracle.DirectEV); delta > maxNativeJPEGDecodeEVDelta {
		t.Fatalf("Go Preview EV=%.17g, LRT 7.5.5=%.17g (delta %.3g exceeds %.3g EV)", withEV, oracle.DirectEV, delta, maxNativeJPEGDecodeEVDelta)
	}
}

func stripJPEGICCForMeteringTest(data []byte) ([]byte, error) {
	if len(data) < 4 || data[0] != 0xff || data[1] != 0xd8 {
		return nil, fmt.Errorf("invalid JPEG SOI")
	}
	output := bytes.NewBuffer(append([]byte(nil), data[:2]...))
	found := false
	for position := 2; position < len(data); {
		if data[position] != 0xff {
			return nil, fmt.Errorf("invalid JPEG marker at byte %d", position)
		}
		start := position
		for position < len(data) && data[position] == 0xff {
			position++
		}
		if position >= len(data) {
			return nil, fmt.Errorf("truncated JPEG marker")
		}
		marker := data[position]
		position++
		if marker == 0xda {
			output.Write(data[start:])
			break
		}
		if marker == 0x01 || marker >= 0xd0 && marker <= 0xd9 {
			output.Write(data[start:position])
			continue
		}
		if position+2 > len(data) {
			return nil, fmt.Errorf("truncated JPEG segment length")
		}
		length := int(data[position])<<8 | int(data[position+1])
		end := position + length
		if length < 2 || end > len(data) {
			return nil, fmt.Errorf("invalid JPEG segment length")
		}
		payload := data[position+2 : end]
		if marker == 0xe2 && bytes.HasPrefix(payload, []byte("ICC_PROFILE\x00")) {
			found = true
		} else {
			output.Write(data[start:end])
		}
		position = end
	}
	if !found {
		return nil, fmt.Errorf("JPEG has no ICC APP2 segment")
	}
	return output.Bytes(), nil
}
