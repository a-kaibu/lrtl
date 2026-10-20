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
	frames := cropFixture(t, 3)
	writeTestXMP(t, frames[2].Image, map[string]string{"crs:PerspectiveScale": "0"})
	output := filepath.Join(t.TempDir(), "invalid.avi")
	var out, stderr bytes.Buffer
	if code := Main([]string{"render", filepath.Dir(frames[0].Image), "--output", output, "--apply-xmp-basic"}, &out, &stderr); code == 0 {
		t.Fatal("accepted invalid geometry")
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatal("published invalid video")
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
