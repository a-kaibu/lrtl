package lrtl

import (
	"encoding/binary"
	"image"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func precisionToneImage() *image.NRGBA64 {
	img := image.NewNRGBA64(image.Rect(7, 11, 39, 27))
	for y := 11; y < 27; y++ {
		for x := 7; x < 39; x++ {
			v := uint16(30000)
			if x >= 23 {
				v += 8
			}
			img.SetNRGBA64(x, y, color.NRGBA64{v - 1000, v, v + 1000, 50000})
		}
	}
	return img
}

func tonePrecisionCases() map[string]xmpBasicAdjustments {
	identity := makeToneCurveTable([]toneCurvePoint{{0, 0}, {255, 255}})
	return map[string]xmpBasicAdjustments{
		"neutral": {}, "exposure": {exposure: .2}, "white balance": {hasWhiteBalance: true, whiteBalance: [3]float64{1.1, 1, .9}},
		"contrast": {contrast: -20}, "highlights": {highlights: -20}, "shadows": {shadows: 20}, "whites": {whites: -20}, "blacks": {blacks: 10},
		"saturation": {saturation: 20}, "vibrance": {vibrance: 20},
		"grayscale": {grayscale: true}, "gray mixer": {grayscale: true, grayMix: [8]float64{20, 0, 0, 0, 0, -20}},
		"parametric":   {parametric: xmpParametricCurve{shadows: 10, darks: 10, lights: 10, highlights: 10, shadowSplit: 25, midtoneSplit: 50, highlightSplit: 75}},
		"master curve": {hasMaster: true, masterCurve: identity}, "channel curves": {hasChannel: [3]bool{true, true, true}, channelCurve: [3]toneCurveTable{identity, identity, identity}},
		"HSL":     {hasColorMix: true, colorMix: [8]xmpHSLColorAdjustment{{hue: 10, saturation: 5, luminance: 2}}},
		"grading": {colorGrading: xmpColorGrading{active: true, global: xmpColorGradeTone{hue: 200, saturation: 10}, blending: 50}},
		"texture": {texture: 20}, "clarity": {clarity: 20}, "dehaze": {dehaze: 10},
		"sharpness":       {sharpness: 40, sharpenRadius: 1, sharpenDetail: 25},
		"luminance noise": {luminanceNoise: 30, lumaNoiseDetail: 50, lumaNoiseContrast: 50},
		"color noise":     {colorNoise: 30, colorNoiseDetail: 50, colorNoiseSmoothness: 50},
		"vignette":        {vignetteAmount: -20, vignetteMidpoint: 50, vignetteFeather: 50},
	}
}

func TestAllGlobalXMPControlsKeep16BitPrecision(t *testing.T) {
	for name, adjustments := range tonePrecisionCases() {
		t.Run(name, func(t *testing.T) {
			img := precisionToneImage()
			before := append([]byte(nil), img.Pix...)
			got := applyXMPBasic(img, adjustments)
			if got.Bounds() != img.Bounds() {
				t.Fatal("bounds changed")
			}
			left, right := got.NRGBA64At(15, 19), got.NRGBA64At(30, 19)
			if left.A != 50000 || right.A != 50000 {
				t.Fatal("alpha quantized")
			}
			if left.R == right.R && left.G == right.G && left.B == right.B {
				t.Fatalf("fine input step disappeared: %v", left)
			}
			if left.R%257 == 0 && left.G%257 == 0 && left.B%257 == 0 {
				t.Fatalf("result quantized to 8-bit: %v", left)
			}
			for i, v := range before {
				if img.Pix[i] != v {
					t.Fatal("input modified")
				}
			}
			if name == "neutral" || name == "master curve" || name == "channel curves" {
				if left != img.NRGBA64At(15, 19) {
					t.Fatalf("identity changed pixel: %v", left)
				}
			}
		})
	}
}

func TestToneCurvesEvaluateOriginalPointsAt16BitCoordinates(t *testing.T) {
	identity := makeToneCurveTable([]toneCurvePoint{{0, 0}, {255, 255}})
	for i := 0; i < 65536; i++ {
		if v := identity.sample(uint16(i)); v != uint16(i) {
			t.Fatalf("identity[%d]=%d", i, v)
		}
	}
	linear := makeToneCurveTable([]toneCurvePoint{{0, 0}, {255, 128}})
	if v := linear.sample(1000); v != 502 {
		t.Fatalf("half slope=%d want 502", v)
	}
	curve := makeToneCurveTable([]toneCurvePoint{{32, 10}, {128, 64}, {224, 220}})
	for _, point := range []struct{ in, out uint16 }{{0, 2570}, {32 * 257, 10 * 257}, {128 * 257, 64 * 257}, {224 * 257, 220 * 257}, {65535, 220 * 257}} {
		if v := curve.sample(point.in); v != point.out {
			t.Fatalf("curve[%d]=%d want %d", point.in, v, point.out)
		}
	}
	previous := uint16(0)
	for i := 0; i < 65536; i++ {
		v := curve.sample(uint16(i))
		if v < previous {
			t.Fatal("monotone PCHIP overshot")
		}
		previous = v
	}
	// Local blending must preserve sub-8-bit input differences as well.
	if v := blendLocalCurve(1001, identity, .5); v != 1001 {
		t.Fatalf("local curve neutral=%d", v)
	}
}

func FuzzToneCurvePrecision(f *testing.F) {
	for _, seed := range []string{"0,0 255,255", "0,255 128,0 255,255", "32,10 128,64 224,220", "0,0 64,128 128,128 255,255"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		if len(text) > 4096 {
			return
		}
		points, err := parseToneCurveValues([]string{text})
		if err != nil {
			return
		}
		curve := makeToneCurveTable(points)
		for _, point := range points {
			if value := curve.sample(uint16(point.x) * 257); value != uint16(point.y)*257 {
				t.Fatalf("curve misses declared point: %v -> %d", point, value)
			}
		}
		for i := 0; i < 65536; i += 997 {
			value := curve.sample(uint16(i))
			for j := 0; j+1 < len(points); j++ {
				x := float64(i) / 257
				if x >= points[j].x && x <= points[j+1].x {
					lower, upper := math.Min(points[j].y, points[j+1].y)*257, math.Max(points[j].y, points[j+1].y)*257
					if float64(value) < lower-.5 || float64(value) > upper+.5 {
						t.Fatal("PCHIP exceeds segment endpoint range")
					}
				}
			}
		}
	})
}

func TestSpatialDetailContainsSub8BitInformation(t *testing.T) {
	img := precisionToneImage()
	detail := xmpLuminanceDetail(img, 1)
	var found bool
	for _, v := range detail {
		if v != 0 && math.Abs(float64(v)) < .01 {
			found = true
		}
	}
	if !found {
		t.Fatal("fine detail was quantized away")
	}
	flat := image.NewNRGBA64(image.Rect(7, 11, 12, 15))
	for y := 11; y < 15; y++ {
		for x := 7; x < 12; x++ {
			flat.SetNRGBA64(x, y, color.NRGBA64{30001, 30001, 30001, 12345})
		}
	}
	for _, v := range xmpLuminanceDetail(flat, 20) {
		if math.Abs(float64(v)) > 1e-5 {
			t.Fatalf("constant-field boundary detail=%g", v)
		}
	}
}

func TestLocalMaskControlsKeep16BitPrecision(t *testing.T) {
	identity := makeToneCurveTable([]toneCurvePoint{{0, 0}, {255, 255}})
	for name, c := range map[string]xmpLocalCorrection{
		"exposure": {exposure: .1}, "tone": {contrast: -20, shadows: 10, highlights: -10, whites: 10, blacks: 5},
		"white balance": {hasWhiteBalance: true, whiteBalance: [3]float64{1.1, 1, .9}}, "saturation": {saturation: 10},
		"toning": {toningHue: 220, toningSaturation: 10}, "grading": {colorGrading: xmpColorGrading{active: true, global: xmpColorGradeTone{hue: 40, saturation: 10}, blending: 50}},
		"master": {hasMainCurve: true, mainCurve: identity}, "channels": {hasChannelCurve: [3]bool{true, true, true}, channelCurve: [3]toneCurveTable{identity, identity, identity}},
		"texture": {texture: 20}, "clarity": {clarity: 20}, "dehaze": {dehaze: 10}, "sharpness": {sharpness: 20},
	} {
		t.Run(name, func(t *testing.T) {
			c.amount = 1
			c.masks = []xmpLocalMask{{kind: localMaskGradient, zeroY: 2, fullY: 1, maskValue: 1}}
			img := precisionToneImage()
			got := applyXMPLocalCorrections(img, []xmpLocalCorrection{c, c})
			if !highPrecisionImage(got) {
				t.Fatalf("local result=%T", got)
			}
			left := color.NRGBA64Model.Convert(got.At(15, 19)).(color.NRGBA64)
			right := color.NRGBA64Model.Convert(got.At(30, 19)).(color.NRGBA64)
			if left.A != 50000 {
				t.Fatal("local alpha quantized")
			}
			if left.R == right.R && left.G == right.G && left.B == right.B {
				t.Fatalf("local fine step disappeared: %v", left)
			}
			if left.R%257 == 0 && left.G%257 == 0 && left.B%257 == 0 {
				t.Fatal("local result quantized")
			}
		})
	}
}

func TestXMPBasicCLIPreservesFineStepsWithCurvesAndSpatialEdits(t *testing.T) {
	dir := t.TempDir()
	img := precisionToneImage()
	// Opaque data avoids testing alpha compositing instead of tone precision.
	for y := 11; y < 27; y++ {
		for x := 7; x < 39; x++ {
			p := img.NRGBA64At(x, y)
			p.A = 65535
			img.SetNRGBA64(x, y, p)
		}
	}
	data, err := encodeDevelopedDNGTIFF(img, 1)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "a.tiff")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	writeTestXMP(t, path, map[string]string{"crs:Contrast2012": "-10", "crs:Saturation": "10", "crs:ToneCurvePV2012": "0, 0 255, 255", "crs:Sharpness": "20", "crs:LuminanceSmoothing": "10"})
	frames, err := DiscoverSequence(dir)
	if err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(t.TempDir(), "capture.bin")
	t.Setenv("LRTL_TEST_FFMPEG_CAPTURE", capture)
	if err := RenderSequence(frames, filepath.Join(dir, "out.mov"), RenderOptions{FPS: 24, Quality: 90, FFmpegPath: exe, ApplyXMPBasic: true}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 32*16*6 {
		t.Fatalf("frame length=%d", len(got))
	}
	left := binary.LittleEndian.Uint16(got[(8*32+8)*6:])
	right := binary.LittleEndian.Uint16(got[(8*32+24)*6:])
	if left == right || left%257 == 0 && right%257 == 0 {
		t.Fatalf("CLI quantized %d / %d", left, right)
	}
}
