package lrtl

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func geometryRamp(bounds image.Rectangle) *image.NRGBA64 {
	img := image.NewNRGBA64(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			img.SetNRGBA64(x, y, color.NRGBA64{uint16(123 + (x-bounds.Min.X)*500), uint16(17 + (y-bounds.Min.Y)*700), 2000, 65535})
		}
	}
	return img
}

func TestXMPGeometryInverseCoordinatesAndPrecision(t *testing.T) {
	for _, bounds := range []image.Rectangle{image.Rect(0, 0, 80, 60), image.Rect(11, 17, 91, 77)} {
		img := geometryRamp(bounds)
		for _, g := range []xmpGeometry{{scale: 150}, {rotate: 10, scale: 125}, {rotate: -10, scale: 100}} {
			got := applyXMPGeometry(img, g)
			if !highPrecisionImage(got) || got.Bounds() != bounds {
				t.Fatalf("lost dimensions/precision: %T %v", got, got.Bounds())
			}
			for _, point := range []image.Point{{20, 20}, {50, 35}} {
				x, y := bounds.Min.X+point.X, bounds.Min.Y+point.Y
				cx, cy := float64(bounds.Min.X)+40, float64(bounds.Min.Y)+30
				u, v := float64(x)+.5-cx, float64(y)+.5-cy
				s, c := math.Sincos(g.rotate * math.Pi / 180)
				// Independent inverse map for pixel centers, then the known
				// linear ramp gives the bilinearly interpolated source values.
				sx := (c*u+s*v)/(g.scale/100) + cx - .5 - float64(bounds.Min.X)
				sy := (-s*u+c*v)/(g.scale/100) + cy - .5 - float64(bounds.Min.Y)
				p := color.NRGBA64Model.Convert(got.At(x, y)).(color.NRGBA64)
				if math.Abs(float64(p.R)-(123+sx*500)) > 3 || math.Abs(float64(p.G)-(17+sy*700)) > 3 || p.B != 2000 || p.A != 65535 {
					t.Fatalf("geometry %+v point %v got %v source=(%g,%g)", g, point, p, sx, sy)
				}
			}
		}
		if applyXMPGeometry(img, xmpGeometry{}) != img || applyXMPGeometry(img, xmpGeometry{scale: 100}) != img {
			t.Fatal("identity resampled image")
		}
	}
}

func TestXMPGeometryBordersAlphaAndDistinct16BitLevels(t *testing.T) {
	img := image.NewNRGBA64(image.Rect(0, 0, 1024, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 1024; x++ {
			img.SetNRGBA64(x, y, color.NRGBA64{uint16(100 + x*60), 12345, 23456, 40000})
		}
	}
	got := applyXMPGeometry(img, xmpGeometry{scale: 150})
	levels := map[uint16]bool{}
	for x := 32; x < 992; x++ {
		p := color.NRGBA64Model.Convert(got.At(x, 4)).(color.NRGBA64)
		levels[p.R] = true
		if absInt(int(p.A)-40000) > 2 {
			t.Fatal("lost alpha precision")
		}
	}
	if len(levels) < 900 {
		t.Fatalf("quantized to %d levels", len(levels))
	}
	small := applyXMPGeometry(img, xmpGeometry{scale: 50})
	_, _, _, a := small.At(0, 0).RGBA()
	if a != 0 {
		t.Fatal("border is not transparent")
	}
	_, _, _, a = small.At(512, 4).RGBA()
	if absInt(int(a)-40000) > 2 {
		t.Fatal("scaled center lost alpha")
	}
}

func TestXMPGeometryReadValidationAndAnalysisCropOrder(t *testing.T) {
	p := EmptyXMPPacket()
	basic, err := readXMPBasicPacket(p)
	if err != nil || basic.active() {
		t.Fatalf("default geometry active: %+v %v", basic.geometry, err)
	}
	for _, tc := range []struct{ name, value string }{{"PerspectiveRotate", "NaN"}, {"PerspectiveRotate", "11"}, {"PerspectiveRotate", ""}, {"PerspectiveScale", "0"}, {"PerspectiveScale", "151"}} {
		p := EmptyXMPPacket()
		if err := p.SetScalar("crs:"+tc.name, tc.value); err != nil {
			t.Fatal(err)
		}
		if _, err := readXMPBasicPacket(p); err == nil {
			t.Fatalf("accepted %s=%s", tc.name, tc.value)
		}
	}
	if err := p.SetScalar("crs:PerspectiveScale", "125"); err != nil {
		t.Fatal(err)
	}
	if err := p.SetScalar("crs:PerspectiveRotate", "5"); err != nil {
		t.Fatal(err)
	}
	basic, err = readXMPBasicPacket(p)
	if err != nil || !basic.active() || basic.globalActive() {
		t.Fatalf("geometry activation incorrect: %v", err)
	}
	img := geometryRamp(image.Rect(0, 0, 80, 60))
	crop := cropSpec{Active: true, Bounds: image.Rect(8, 6, 72, 54)}
	adjustments := sequenceXMPAdjustments{basics: []xmpBasicAdjustments{basic}, crops: []cropSpec{crop}}
	got := adjustments.apply(0, img)
	want := applyXMPCrop(applyXMPGeometry(img, basic.geometry), crop)
	actual, err := imageRGB48(got)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := imageRGB48(want)
	if err != nil || !bytes.Equal(actual, expected) {
		t.Fatal("analysis applies geometry/crop in different order")
	}
}

func TestXMPManualPerspectiveCoordinatesAndAspect(t *testing.T) {
	bounds := image.Rect(0, 0, 81, 61)
	img := geometryRamp(bounds)
	focal := float64(max(bounds.Dx(), bounds.Dy())) / (2 * math.Tan(math.Pi/6))
	cx, cy := float64(bounds.Min.X)+float64(bounds.Dx())/2, float64(bounds.Min.Y)+float64(bounds.Dy())/2

	vertical := 10.0
	got := applyXMPGeometry(img, xmpGeometry{scale: 100, vertical: vertical})
	// At the image center, an inverse camera pitch maps y by f*tan(theta).
	// The slider's 20-degree full-scale calibration is documented separately
	// from Adobe's own transform, whose exact calibration is proprietary.
	verticalY := cy + focal*math.Tan(vertical*math.Pi/180*.2)
	verticalPixel := color.NRGBA64Model.Convert(got.At(40, 30)).(color.NRGBA64)
	wantGreen := 17 + (verticalY-.5)*700
	if math.Abs(float64(verticalPixel.G)-wantGreen) > 3 || verticalPixel.R != uint16(123+40*500) {
		t.Fatalf("vertical perspective center got=%v want green=%g", verticalPixel, wantGreen)
	}

	horizontal := -12.0
	got = applyXMPGeometry(img, xmpGeometry{scale: 100, horizontal: horizontal})
	horizontalX := cx - focal*math.Tan(horizontal*math.Pi/180*.2)
	horizontalPixel := color.NRGBA64Model.Convert(got.At(40, 30)).(color.NRGBA64)
	wantRed := 123 + (horizontalX-.5)*500
	if math.Abs(float64(horizontalPixel.R)-wantRed) > 3 || horizontalPixel.G != uint16(17+30*700) {
		t.Fatalf("horizontal perspective center got=%v want red=%g", horizontalPixel, wantRed)
	}

	shiftedBounds := image.Rect(13, 17, 94, 78)
	combined := xmpGeometry{vertical: -9, horizontal: 14, aspect: -30, rotate: 3, scale: 125}
	got = applyXMPGeometry(geometryRamp(shiftedBounds), combined)
	shiftedCX := float64(shiftedBounds.Min.X) + float64(shiftedBounds.Dx())/2
	shiftedCY := float64(shiftedBounds.Min.Y) + float64(shiftedBounds.Dy())/2
	h := combined.horizontal * math.Pi / 180 * .2
	v := combined.vertical * math.Pi / 180 * .2
	combinedX := shiftedCX - focal*math.Tan(h)/math.Cos(v)
	combinedY := shiftedCY + focal*math.Tan(v)
	combinedPixel := color.NRGBA64Model.Convert(got.At(53, 47)).(color.NRGBA64)
	if math.Abs(float64(combinedPixel.R)-(123+(combinedX-.5-float64(shiftedBounds.Min.X))*500)) > 4 ||
		math.Abs(float64(combinedPixel.G)-(17+(combinedY-.5-float64(shiftedBounds.Min.Y))*700)) > 4 {
		t.Fatalf("combined perspective at non-zero origin got=%v source=(%g,%g)", combinedPixel, combinedX, combinedY)
	}

	aspect := 50.0
	got = applyXMPGeometry(img, xmpGeometry{scale: 100, aspect: aspect})
	aspectX := cx + (30.5-cx)/math.Exp2(aspect/100)
	aspectPixel := color.NRGBA64Model.Convert(got.At(30, 30)).(color.NRGBA64)
	wantAspectRed := 123 + (aspectX-.5)*500
	if math.Abs(float64(aspectPixel.R)-wantAspectRed) > 3 {
		t.Fatalf("aspect got=%v want red=%g", aspectPixel, wantAspectRed)
	}
	if applyXMPGeometry(img, xmpGeometry{scale: 100}) != img {
		t.Fatal("default manual perspective resampled image")
	}
}

func TestXMPManualPerspectiveParsingAndLimits(t *testing.T) {
	for _, tc := range []struct{ name, value string }{
		{"PerspectiveVertical", "-100"},
		{"PerspectiveHorizontal", "100"},
		{"PerspectiveAspect", "50"},
		{"PerspectiveX", "100"},
		{"PerspectiveY", "-100"},
	} {
		packet := EmptyXMPPacket()
		if err := packet.SetScalar("crs:"+tc.name, tc.value); err != nil {
			t.Fatal(err)
		}
		basic, err := readXMPBasicPacket(packet)
		if err != nil || !basic.geometry.active() || basic.globalActive() {
			t.Fatalf("%s=%s: geometry=%+v global=%t error=%v", tc.name, tc.value, basic.geometry, basic.globalActive(), err)
		}
	}
	for _, tc := range []struct{ name, value string }{
		{"PerspectiveVertical", "-101"},
		{"PerspectiveHorizontal", "101"},
		{"PerspectiveAspect", "NaN"},
		{"PerspectiveX", "-101"},
		{"PerspectiveY", "101"},
	} {
		packet := EmptyXMPPacket()
		if err := packet.SetScalar("crs:"+tc.name, tc.value); err != nil {
			t.Fatal(err)
		}
		if _, err := readXMPBasicPacket(packet); err == nil {
			t.Fatalf("accepted %s=%s", tc.name, tc.value)
		}
	}
}

func TestXMPManualPerspectiveOffsetCoordinates(t *testing.T) {
	bounds := image.Rect(11, 17, 92, 78)
	img := geometryRamp(bounds)
	got := applyXMPGeometry(img, xmpGeometry{scale: 100, xOffset: 20, yOffset: 20})
	if got.Bounds() != bounds || !highPrecisionImage(got) {
		t.Fatalf("changed output shape/precision: %T %v", got, got.Bounds())
	}
	// Public Lightroom export measurements report +20 as +0.1621 widths on X
	// and -0.1625 heights on Y. The independent linear-ramp oracle checks both
	// directions and their scale in pixel coordinates on a non-zero image origin.
	xShift := 20.0 / 100 * xmpGeometryOffsetPerSlider * float64(bounds.Dx())
	yShift := -20.0 / 100 * xmpGeometryOffsetPerSlider * float64(bounds.Dy())
	pixel := color.NRGBA64Model.Convert(got.At(51, 47)).(color.NRGBA64)
	wantRed := 123 + (40-xShift)*500
	wantGreen := 17 + (30-yShift)*700
	if math.Abs(float64(pixel.R)-wantRed) > 3 || math.Abs(float64(pixel.G)-wantGreen) > 3 || pixel.B != 2000 || pixel.A != 65535 {
		t.Fatalf("offset pixel got=%v want R=%g G=%g", pixel, wantRed, wantGreen)
	}
	if applyXMPGeometry(img, xmpGeometry{scale: 100, xOffset: 0, yOffset: 0}) != img {
		t.Fatal("zero offsets resampled image")
	}
}

func TestXMPManualPerspectiveOffsetsComposeWithProjectiveTransform(t *testing.T) {
	bounds := image.Rect(11, 17, 92, 78)
	img := geometryRamp(bounds)
	g := xmpGeometry{scale: 100, vertical: 10, xOffset: 20, yOffset: 20}
	got := applyXMPGeometry(img, g)
	localX, localY := 40.0, 30.0
	x, y := bounds.Min.X+int(localX), bounds.Min.Y+int(localY)
	cx := float64(bounds.Min.X) + float64(bounds.Dx())/2
	cy := float64(bounds.Min.Y) + float64(bounds.Dy())/2
	focal := float64(max(bounds.Dx(), bounds.Dy())) / (2 * math.Tan(math.Pi/6))
	shiftX := g.xOffset / 100 * xmpGeometryOffsetPerSlider * float64(bounds.Dx())
	shiftY := -g.yOffset / 100 * xmpGeometryOffsetPerSlider * float64(bounds.Dy())
	// Independently undo the final translation, then invert the camera pitch.
	u := (float64(x) + .5 - cx - shiftX) / focal
	v := (float64(y) + .5 - cy - shiftY) / focal
	angle := g.vertical * math.Pi / 9 / 100
	sine, cosine := math.Sincos(angle)
	sourceX := cx + focal*u/(cosine-sine*v)
	sourceY := cy + focal*(cosine*v+sine)/(cosine-sine*v)
	pixel := color.NRGBA64Model.Convert(got.At(x, y)).(color.NRGBA64)
	wantRed := 123 + (sourceX-.5-float64(bounds.Min.X))*500
	wantGreen := 17 + (sourceY-.5-float64(bounds.Min.Y))*700
	if math.Abs(float64(pixel.R)-wantRed) > 4 || math.Abs(float64(pixel.G)-wantGreen) > 4 {
		t.Fatalf("composed transform got=%v want R=%g G=%g", pixel, wantRed, wantGreen)
	}
}

func TestXMPManualPerspectiveOffsetsRenderAVI(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "frame.png")
	source := geometryRamp(image.Rect(0, 0, 81, 61))
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, source); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	writeTestXMP(t, path, nil)
	var out, stderr bytes.Buffer
	for _, setting := range [][2]string{{"PerspectiveX", "20"}, {"PerspectiveY", "20"}} {
		if code := Main([]string{"set", path, setting[0], setting[1]}, &out, &stderr); code != 0 {
			t.Fatalf("set %s: %s", setting[0], &stderr)
		}
	}
	output := filepath.Join(t.TempDir(), "offset.avi")
	if code := Main([]string{"render", dir, "--output", output, "--quality", "100", "--apply-xmp-basic"}, &out, &stderr); code != 0 {
		t.Fatalf("render: %s", &stderr)
	}
	video, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	start := bytes.Index(video, []byte{255, 216})
	if start < 0 {
		t.Fatal("missing AVI JPEG frame")
	}
	video = video[start:]
	end := bytes.Index(video, []byte{255, 217})
	if end < 0 {
		t.Fatal("truncated AVI JPEG frame")
	}
	frame, err := jpeg.Decode(bytes.NewReader(video[:end+2]))
	if err != nil {
		t.Fatal(err)
	}
	xShift := 20.0 / 100 * xmpGeometryOffsetPerSlider * float64(source.Bounds().Dx())
	yShift := -20.0 / 100 * xmpGeometryOffsetPerSlider * float64(source.Bounds().Dy())
	wantRed := (123 + (40-xShift)*500) / 257
	wantGreen := (17 + (30-yShift)*700) / 257
	r, g, _, _ := frame.At(40, 30).RGBA()
	if math.Abs(float64(r>>8)-wantRed) > 3 || math.Abs(float64(g>>8)-wantGreen) > 3 {
		t.Fatalf("AVI offset pixel got=(%d,%d) want=(%g,%g)", r>>8, g>>8, wantRed, wantGreen)
	}
}

func TestXMPStoredUprightParsingAndValidation(t *testing.T) {
	identity := "1,0,0,0,1,0,0,0,1"
	shift := "1,0,0.25,0,1,0,0,0,1"
	packet := EmptyXMPPacket()
	for name, value := range map[string]string{
		"crs:PerspectiveUpright":    "1",
		"crs:UprightTransformCount": "6",
		"crs:UprightTransform_0":    identity,
		"crs:UprightTransform_1":    shift,
	} {
		if err := packet.SetScalar(name, value); err != nil {
			t.Fatal(err)
		}
	}
	basic, err := readXMPBasicPacket(packet)
	if err != nil {
		t.Fatal(err)
	}
	if basic.geometry.upright == nil || !basic.geometry.active() {
		t.Fatalf("selected Upright transform was not loaded: %+v", basic.geometry)
	}
	want := xmpHomography{1, 0, .25, 0, 1, 0, 0, 0, 1}
	if *basic.geometry.upright != want {
		t.Fatalf("selected transform=%v want %v", *basic.geometry.upright, want)
	}

	for _, tc := range []struct {
		name   string
		values map[string]string
	}{
		{name: "mode zero ignores stale matrices", values: map[string]string{"crs:PerspectiveUpright": "0", "crs:UprightTransform_0": "malformed"}},
		{name: "missing mode", values: map[string]string{"crs:UprightTransform_1": shift}},
		{name: "mode selects its own matrix", values: map[string]string{"crs:PerspectiveUpright": "2", "crs:UprightTransformCount": "6", "crs:UprightTransform_1": shift, "crs:UprightTransform_2": identity}},
		{name: "scaled identity does not resample", values: map[string]string{"crs:PerspectiveUpright": "1", "crs:UprightTransform_1": "7,0,0,0,7,0,0,0,7"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := EmptyXMPPacket()
			for name, value := range tc.values {
				if err := p.SetScalar(name, value); err != nil {
					t.Fatal(err)
				}
			}
			got, err := readXMPBasicPacket(p)
			if err != nil {
				t.Fatal(err)
			}
			switch tc.name {
			case "mode zero ignores stale matrices", "missing mode":
				if got.geometry.upright != nil || got.geometry.active() {
					t.Fatalf("unexpected Upright geometry: %+v", got.geometry)
				}
			case "mode selects its own matrix":
				if got.geometry.upright == nil || !got.geometry.upright.isIdentity() {
					t.Fatalf("mode 2 did not select transform 2: %+v", got.geometry)
				}
			case "scaled identity does not resample":
				if got.geometry.upright == nil || !got.geometry.upright.isIdentity() || got.geometry.active() {
					t.Fatalf("scaled identity was not recognized: %+v", got.geometry)
				}
			}
		})
	}

	for _, tc := range []struct {
		name   string
		values map[string]string
	}{
		{name: "fractional mode", values: map[string]string{"crs:PerspectiveUpright": "1.5"}},
		{name: "out of range mode", values: map[string]string{"crs:PerspectiveUpright": "6"}},
		{name: "non-finite mode", values: map[string]string{"crs:PerspectiveUpright": "NaN"}},
		{name: "fractional transform count", values: map[string]string{"crs:PerspectiveUpright": "1", "crs:UprightTransformCount": "6.0", "crs:UprightTransform_1": shift}},
		{name: "count omits selected transform", values: map[string]string{"crs:PerspectiveUpright": "2", "crs:UprightTransformCount": "2", "crs:UprightTransform_2": shift}},
		{name: "missing selected transform", values: map[string]string{"crs:PerspectiveUpright": "1", "crs:UprightTransformCount": "6"}},
		{name: "too few coefficients", values: map[string]string{"crs:PerspectiveUpright": "1", "crs:UprightTransform_1": "1,0,0,0,1,0,0,0"}},
		{name: "invalid coefficient", values: map[string]string{"crs:PerspectiveUpright": "1", "crs:UprightTransform_1": "1,0,0,0,NaN,0,0,0,1"}},
		{name: "singular matrix", values: map[string]string{"crs:PerspectiveUpright": "1", "crs:UprightTransform_1": "1,2,3,2,4,6,0,0,0"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := EmptyXMPPacket()
			for name, value := range tc.values {
				if err := p.SetScalar(name, value); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := readXMPBasicPacket(p); err == nil {
				t.Fatal("accepted invalid Upright transform")
			}
		})
	}
}

func TestXMPStoredUprightInverseCoordinates(t *testing.T) {
	// This projective map sends (u,v) to (u/(0.2u+1), v/(0.2u+1)).
	// At destination (u,v)=(0.5,0.5), its inverse is (5/9,5/9).
	transform := xmpHomography{1, 0, 0, 0, 1, 0, .2, 0, 1}
	bounds := image.Rect(11, 17, 22, 22)
	source := geometryRamp(bounds)
	got := applyXMPGeometry(source, xmpGeometry{scale: 100, upright: &transform})
	if got.Bounds() != bounds || !highPrecisionImage(got) {
		t.Fatalf("changed output shape/precision: %T %v", got, got.Bounds())
	}
	// Pixel index (5,2) is normalized (0.5,0.5); inverse mapping reads
	// source index (50/9,20/9). The ramp gives an independent bilinear oracle.
	pixel := color.NRGBA64Model.Convert(got.At(16, 19)).(color.NRGBA64)
	wantRed := 123 + (50.0/9)*500
	wantGreen := 17 + (20.0/9)*700
	if math.Abs(float64(pixel.R)-wantRed) > 3 || math.Abs(float64(pixel.G)-wantGreen) > 3 || pixel.B != 2000 || pixel.A != 65535 {
		t.Fatalf("projective inverse got=%v want R=%g G=%g", pixel, wantRed, wantGreen)
	}
	_, _, _, alpha := got.At(bounds.Max.X-1, 19).RGBA()
	if alpha != 0 {
		t.Fatal("out-of-frame inverse sample was not transparent")
	}

	identity := xmpHomography{1, 0, 0, 0, 1, 0, 0, 0, 1}
	if applyXMPGeometry(source, xmpGeometry{upright: &identity}) != source {
		t.Fatal("identity Upright transform resampled the image")
	}
}

func TestXMPStoredUprightRenderAVIAndPreservesInputs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "frame.png")
	source := geometryRamp(image.Rect(0, 0, 81, 61))
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, source); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	writeTestXMP(t, path, map[string]string{
		"crs:PerspectiveUpright":    "1",
		"crs:UprightTransformCount": "6",
		"crs:UprightTransform_0":    "1,0,0,0,1,0,0,0,1",
		"crs:UprightTransform_1":    "1,0,0.1,0,1,0,0,0,1",
	})
	imageBefore, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	xmpBefore, err := os.ReadFile(path + ".xmp")
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "upright.avi")
	var out, stderr bytes.Buffer
	if code := Main([]string{"render", dir, "--output", output, "--quality", "100", "--apply-xmp-basic"}, &out, &stderr); code != 0 {
		t.Fatalf("render: %s", &stderr)
	}
	video, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	start := bytes.Index(video, []byte{255, 216})
	if start < 0 {
		t.Fatal("missing AVI JPEG frame")
	}
	video = video[start:]
	end := bytes.Index(video, []byte{255, 217})
	if end < 0 {
		t.Fatal("truncated AVI JPEG frame")
	}
	frame, err := jpeg.Decode(bytes.NewReader(video[:end+2]))
	if err != nil {
		t.Fatal(err)
	}
	// Inverse of the saved +0.1 normalized x translation: destination x=40
	// reads source x=32. The input ramp makes the JPEG pixel expectation direct.
	wantRed := (123 + 32*500) / 257
	r, _, _, _ := frame.At(40, 30).RGBA()
	if math.Abs(float64(r>>8)-float64(wantRed)) > 3 {
		t.Fatalf("rendered red=%d want %d", r>>8, wantRed)
	}
	imageAfter, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(imageBefore, imageAfter) {
		t.Fatal("render modified source image")
	}
	xmpAfter, err := os.ReadFile(path + ".xmp")
	if err != nil || !bytes.Equal(xmpBefore, xmpAfter) {
		t.Fatal("render modified source XMP")
	}
}

func FuzzXMPStoredUprightMatrixParser(f *testing.F) {
	f.Add("1", "6", "1,0,0,0,1,0,0,0,1")
	f.Add("5", "6", "1.02,0.01,-0.02,-0.03,0.98,0.01,0.04,-0.02,1")
	f.Add("1", "6", "1,0,0,0,1,0,0,0,0")
	f.Add("not-a-mode", "not-a-count", "NaN,Inf,-Inf")
	f.Fuzz(func(t *testing.T, mode, count, matrix string) {
		packet := EmptyXMPPacket()
		for name, value := range map[string]string{
			"crs:PerspectiveUpright":    mode,
			"crs:UprightTransformCount": count,
			"crs:UprightTransform_1":    matrix,
		} {
			if err := packet.SetScalar(name, value); err != nil {
				t.Fatal(err)
			}
		}
		_, _ = readXMPBasicPacket(packet)
	})
}

func TestXMPManualPerspectiveExtremeSlidersRemainBounded(t *testing.T) {
	img := geometryRamp(image.Rect(0, 0, 64, 48))
	for _, tc := range []struct{ vertical, horizontal, aspect float64 }{
		{-100, -100, -100}, {-100, 100, 100}, {100, -100, 100}, {100, 100, -100},
	} {
		got := applyXMPGeometry(img, xmpGeometry{scale: 150, vertical: tc.vertical, horizontal: tc.horizontal, aspect: tc.aspect})
		if got.Bounds() != img.Bounds() || !highPrecisionImage(got) {
			t.Fatalf("extreme geometry %+v changed output shape or precision: %T %v", tc, got, got.Bounds())
		}
		for _, point := range []image.Point{{0, 0}, {32, 24}, {63, 47}} {
			p := color.NRGBA64Model.Convert(got.At(point.X, point.Y)).(color.NRGBA64)
			if p.A == 0 && (p.R != 0 || p.G != 0 || p.B != 0) {
				t.Fatalf("transparent sample contains color for %+v at %v: %v", tc, point, p)
			}
		}
	}
}

func TestXMPManualPerspectiveRenderAVIAndPreservesInputs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "frame.png")
	source := geometryRamp(image.Rect(0, 0, 81, 61))
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, source); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	writeTestXMP(t, path, map[string]string{"crs:PerspectiveVertical": "10"})
	imageBefore, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	xmpBefore, err := os.ReadFile(path + ".xmp")
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "perspective.avi")
	var out, stderr bytes.Buffer
	if code := Main([]string{"render", dir, "--output", output, "--quality", "100", "--apply-xmp-basic"}, &out, &stderr); code != 0 {
		t.Fatalf("render: %s", &stderr)
	}
	video, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	start := bytes.Index(video, []byte{255, 216})
	if start < 0 {
		t.Fatal("missing AVI JPEG frame")
	}
	video = video[start:]
	end := bytes.Index(video, []byte{255, 217})
	if end < 0 {
		t.Fatal("truncated AVI JPEG frame")
	}
	frame, err := jpeg.Decode(bytes.NewReader(video[:end+2]))
	if err != nil {
		t.Fatal(err)
	}
	// The odd-sized test image places a pixel center exactly at the canvas
	// center, making the independent vertical-pitch expectation direct.
	focal := float64(max(source.Bounds().Dx(), source.Bounds().Dy())) / (2 * math.Tan(math.Pi/6))
	sy := float64(source.Bounds().Dy())/2 + focal*math.Tan(10*math.Pi/180*.2) - .5
	want := (17 + sy*700) / 257
	_, green, _, _ := frame.At(40, 30).RGBA()
	if math.Abs(float64(green>>8)-want) > 3 {
		t.Fatalf("rendered green=%d want %g", green>>8, want)
	}
	imageAfter, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(imageBefore, imageAfter) {
		t.Fatal("render modified source image")
	}
	xmpAfter, err := os.ReadFile(path + ".xmp")
	if err != nil || !bytes.Equal(xmpBefore, xmpAfter) {
		t.Fatal("render modified source XMP")
	}
}

func TestXMPGeometryCLIInterpolationAndAVIPixels(t *testing.T) {
	frames := cropFixture(t, 3)
	var source bytes.Buffer
	if err := png.Encode(&source, geometryRamp(image.Rect(0, 0, 80, 60))); err != nil {
		t.Fatal(err)
	}
	for _, frame := range frames {
		if err := os.WriteFile(frame.Image, source.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		writeTestXMP(t, frame.Image, map[string]string{"crs:Exposure2012": "0", "xmp:Rating": "0"})
	}
	var out, stderr bytes.Buffer
	for _, tc := range []struct {
		index int
		scale string
	}{{0, "100"}, {2, "150"}} {
		for _, args := range [][]string{{"mark", frames[tc.index].Image}, {"set", frames[tc.index].Image, "PerspectiveScale", tc.scale}} {
			if code := Main(args, &out, &stderr); code != 0 {
				t.Fatalf("%v: %s", args, &stderr)
			}
		}
	}
	dir := filepath.Dir(frames[0].Image)
	if code := Main([]string{"interpolate", dir, "--write"}, &out, &stderr); code != 0 {
		t.Fatalf("interpolate: %s", &stderr)
	}
	before := make([][]byte, 3)
	for i, frame := range frames {
		var err error
		before[i], err = os.ReadFile(frame.OutputSidecar())
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		apply  bool
		motion int
	}{{false, 0}, {true, 0}, {true, 2}} {
		apply := tc.apply
		output := filepath.Join(t.TempDir(), fmt.Sprintf("geometry-%t-%d.avi", apply, tc.motion))
		args := []string{"render", dir, "--output", output, "--quality", "100"}
		if apply {
			args = append(args, "--apply-xmp-basic")
		}
		if tc.motion > 0 {
			args = append(args, "--motion-blur", fmt.Sprint(tc.motion))
		}
		if code := Main(args, &out, &stderr); code != 0 {
			t.Fatalf("render: %s", &stderr)
		}
		data, err := os.ReadFile(output)
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		for {
			start := bytes.Index(data, []byte{255, 216})
			if start < 0 {
				break
			}
			data = data[start:]
			end := bytes.Index(data, []byte{255, 217})
			if end < 0 {
				t.Fatal("truncated frame")
			}
			frame, err := jpeg.Decode(bytes.NewReader(data[:end+2]))
			if err != nil {
				t.Fatal(err)
			}
			scale := 1.0
			sourceIndex := count + tc.motion
			if apply {
				scale = 1 + float64(sourceIndex)*.25
			}
			sx := (20.5-40)/scale + 40 - .5
			want := float64(123+500*sx) / 257
			if tc.motion == 2 {
				previousScale := 1 + float64(sourceIndex-1)*.25
				previous := float64(123+500*((20.5-40)/previousScale+40-.5)) / 257
				// The two-frame causal blend uses weights 0.354 and 1.000.
				want = (.354*previous + want) / 1.354
			}
			r, _, _, _ := frame.At(20, 20).RGBA()
			if math.Abs(float64(r>>8)-want) > 3 {
				t.Fatalf("apply=%t frame=%d red=%d want %g", apply, count, r>>8, want)
			}
			count++
			data = data[end+2:]
		}
		if count != 3-tc.motion {
			t.Fatalf("frames=%d", count)
		}
	}
	for i, frame := range frames {
		imageData, err := os.ReadFile(frame.Image)
		if err != nil || !bytes.Equal(imageData, source.Bytes()) {
			t.Fatal("source image modified")
		}
		sidecar, err := os.ReadFile(frame.OutputSidecar())
		if err != nil || !bytes.Equal(sidecar, before[i]) {
			t.Fatal("render changed sidecar")
		}
	}
}

func TestXMPGeometryInvalidLateFrameDoesNotPublishVideo(t *testing.T) {
	for _, tc := range []struct {
		name   string
		values map[string]string
	}{
		{name: "manual slider out of range", values: map[string]string{"crs:PerspectiveScale": "0"}},
		{name: "selected Upright matrix missing", values: map[string]string{"crs:PerspectiveUpright": "1", "crs:UprightTransformCount": "6"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			frames := cropFixture(t, 3)
			writeTestXMP(t, frames[2].Image, tc.values)
			output := filepath.Join(t.TempDir(), "invalid.avi")
			var out, stderr bytes.Buffer
			if code := Main([]string{"render", filepath.Dir(frames[0].Image), "--output", output, "--apply-xmp-basic"}, &out, &stderr); code == 0 {
				t.Fatal("accepted invalid geometry")
			}
			if _, err := os.Stat(output); !os.IsNotExist(err) {
				t.Fatal("published invalid video")
			}
		})
	}
}

func TestXMPGeometryDisablesJPEGCopyFastPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "frame.jpg")
	img := image.NewNRGBA(image.Rect(0, 0, 80, 60))
	for y := 0; y < 60; y++ {
		for x := 0; x < 80; x++ {
			img.SetNRGBA(x, y, color.NRGBA{uint8(2 * x), uint8(3 * y), 0, 255})
		}
	}
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, img, &jpeg.Options{Quality: 100}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	writeTestXMP(t, path, map[string]string{"crs:PerspectiveScale": "150"})
	output := filepath.Join(t.TempDir(), "jpeg.avi")
	var out, stderr bytes.Buffer
	if code := Main([]string{"render", dir, "--output", output, "--apply-xmp-basic", "--quality", "100"}, &out, &stderr); code != 0 {
		t.Fatalf("render: %s", &stderr)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	start := bytes.Index(data, []byte{255, 216})
	if start < 0 {
		t.Fatal("missing frame")
	}
	data = data[start:]
	end := bytes.Index(data, []byte{255, 217})
	if end < 0 {
		t.Fatal("truncated frame")
	}
	if bytes.Equal(data[:end+2], encoded.Bytes()) {
		t.Fatal("geometry incorrectly reused original JPEG")
	}
	frame, err := jpeg.Decode(bytes.NewReader(data[:end+2]))
	if err != nil {
		t.Fatal(err)
	}
	r, _, _, _ := frame.At(20, 20).RGBA()
	want := 2 * ((20.5-40)/1.5 + 40 - .5)
	if math.Abs(float64(r>>8)-want) > 3 {
		t.Fatalf("red=%d want %g", r>>8, want)
	}
}
