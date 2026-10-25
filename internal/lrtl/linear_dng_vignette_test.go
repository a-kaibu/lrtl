package lrtl

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/jpeg"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func testDNGVignetteRadial(centerX, centerY float64, coefficients [5]float64) testDNGOpcode {
	var payload []byte
	for _, coefficient := range coefficients {
		payload = binary.BigEndian.AppendUint64(payload, math.Float64bits(coefficient))
	}
	payload = binary.BigEndian.AppendUint64(payload, math.Float64bits(centerX))
	payload = binary.BigEndian.AppendUint64(payload, math.Float64bits(centerY))
	return testDNGOpcode{3, 0x01030000, 0, payload}
}

// This reference evaluates the DNG radial gain as an explicit sum of powers,
// independently of the production Horner evaluation.
func referenceDNGVignetteGain(x, y, width, height int, centerX, centerY float64, coefficients [5]float64) float64 {
	cx, cy := float64(width)*centerX, float64(height)*centerY
	radiusX := max(math.Abs(cx), math.Abs(float64(width)-cx))
	radiusY := max(math.Abs(cy), math.Abs(float64(height)-cy))
	radius := math.Hypot(radiusX, radiusY)
	r := math.Hypot(float64(x)-cx, float64(y)-cy) / radius
	r2, power, gain := r*r, r*r, 1.0
	for _, coefficient := range coefficients {
		gain += coefficient * power
		power *= r2
	}
	return gain
}

func referenceDNGVignetteSRGB(linear float64) uint16 {
	linear = min(1, max(0, linear))
	if linear <= 0.0031308 {
		linear *= 12.92
	} else {
		linear = 1.055*math.Pow(linear, 1/2.4) - 0.055
	}
	return uint16(math.Round(min(1, max(0, linear)) * 65535))
}

func TestDNGVignetteRadialGainAgainstSpecification(t *testing.T) {
	const width, height = 13, 7
	const centerX, centerY = .31, .72
	coefficients := [5]float64{.24, -.08, .035, -.012, .004}
	vignette, err := parseDNGVignetteRadial(testDNGVignetteRadial(centerX, centerY, coefficients).payload)
	if err != nil {
		t.Fatal(err)
	}
	for _, point := range [][2]int{{0, 0}, {4, 5}, {8, 2}, {12, 6}, {6, 3}} {
		got := vignette.gain(point[0], point[1], width, height)
		want := referenceDNGVignetteGain(point[0], point[1], width, height, centerX, centerY, coefficients)
		if math.Abs(got-want) > 1e-14 {
			t.Fatalf("gain(%d,%d)=%.16g want %.16g", point[0], point[1], got, want)
		}
	}
	center, err := parseDNGVignetteRadial(testDNGVignetteRadial(.5, .5, coefficients).payload)
	if err != nil {
		t.Fatal(err)
	}
	if got := center.gain(5, 3, 10, 6); got != 1 {
		t.Fatalf("gain at optical center=%g want 1", got)
	}
}

func TestDNGVignetteRadialApplyChannelsClippingAndDimensions(t *testing.T) {
	vignette, err := parseDNGVignetteRadial(testDNGVignetteRadial(.5, .5, [5]float64{.5}).payload)
	if err != nil {
		t.Fatal(err)
	}
	source := []float32{
		.8, .6, .1, .4, .3, .2,
		.2, .3, .4, .5, .6, .7,
		.1, .2, .3, .4, .5, .6,
		.7, .8, .9, .3, .2, .1,
	}
	data := append([]float32(nil), source...)
	if err := vignette.apply(data, 2, 4, 3); err != nil {
		t.Fatal(err)
	}
	for y := 0; y < 4; y++ {
		for x := 0; x < 2; x++ {
			gain := referenceDNGVignetteGain(x, y, 2, 4, .5, .5, [5]float64{.5})
			for channel := 0; channel < 3; channel++ {
				index := (y*2+x)*3 + channel
				want := float32(min(1, max(0, float64(source[index])*gain)))
				if data[index] != want {
					t.Fatalf("pixel (%d,%d) channel %d=%g want %g", x, y, channel, data[index], want)
				}
			}
		}
	}
	if data[0] != 1 {
		t.Fatalf("corner should clip to one, got %g", data[0])
	}
	mono := []float32{.25, .5, .75, 1}
	if err := vignette.apply(mono, 2, 2, 1); err != nil {
		t.Fatal(err)
	}
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			index := y*2 + x
			gain := referenceDNGVignetteGain(x, y, 2, 2, .5, .5, [5]float64{.5})
			want := float32(min(1, max(0, float64([]float32{.25, .5, .75, 1}[index])*gain)))
			if mono[index] != want {
				t.Fatalf("mono pixel (%d,%d)=%g want %g", x, y, mono[index], want)
			}
		}
	}
	for _, invalid := range []struct {
		data                  []float32
		width, height, planes int
	}{{nil, 0, 1, 1}, {[]float32{1}, 1, 1, 2}, {[]float32{1, 2}, 1, 1, 1}} {
		if err := vignette.apply(invalid.data, invalid.width, invalid.height, invalid.planes); err == nil {
			t.Fatalf("accepted invalid dimensions %+v", invalid)
		}
	}
}

func TestDNGStage1VignetteRadialUsesRawPixelCenters(t *testing.T) {
	const width, height = 4, 2
	vignette, err := parseDNGVignetteRadial(testDNGVignetteRadial(.5, .5, [5]float64{1}).payload)
	if err != nil {
		t.Fatal(err)
	}
	source := make([]uint16, width*height)
	for i := range source {
		source[i] = 16384
	}
	source[0] = 50000 // The radial gain at this corner clips to 65535.
	destination := make([]uint16, len(source))
	if err := vignette.applyRaw(source, destination, width, height, 1); err != nil {
		t.Fatal(err)
	}
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			index := y*width + x
			want := uint16(18022)
			if x == 0 || x == width-1 {
				want = 24576
			}
			if index == 0 {
				want = 65535
			}
			if got := destination[index]; got != want {
				t.Errorf("raw pixel (%d,%d)=%d want %d", x, y, got, want)
			}
		}
	}
}

func TestLinearDNGStage1VignetteRadialBeforeActiveAreaBothByteOrders(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		t.Run(order.String(), func(t *testing.T) {
			pixels := make([]uint16, 4*2)
			for i := range pixels {
				pixels[i] = 16384
			}
			metadata := map[uint16]dngTestField{
				50829: dngTestLong(order, 0, 1, 2, 3),
				50717: dngTestLong(order, 65535),
				51008: testDNGOpcodeList(testDNGVignetteRadial(.5, .5, [5]float64{1})),
			}
			decoded, err := decodeLinearDNG(linearDNGFixture(order, 4, 2, 1, pixels, metadata))
			if err != nil {
				t.Fatal(err)
			}
			if decoded.Bounds() != image.Rect(0, 0, 2, 2) {
				t.Fatalf("active bounds=%v", decoded.Bounds())
			}
			want := testDNGLinearToSRGB16(18022)
			for y := 0; y < 2; y++ {
				for x := 0; x < 2; x++ {
					if pixel := decoded.NRGBA64At(x, y); pixel.R != want || pixel.G != want || pixel.B != want {
						t.Errorf("pixel (%d,%d)=%v want gray %d", x, y, pixel, want)
					}
				}
			}
		})
	}
}

func TestDNGVignetteRadialOrderedWithPointOpcodesAndClips(t *testing.T) {
	area := testDNGArea(image.Rect(0, 0, 4, 4), 0, 1, 1, 1)
	point, err := parseDNGPointOpcode(8, testDNGPolynomial(area, 0, 0, 1).payload)
	if err != nil {
		t.Fatal(err)
	}
	vignette, err := parseDNGVignetteRadial(testDNGVignetteRadial(.5, .5, [5]float64{1}).payload)
	if err != nil {
		t.Fatal(err)
	}
	forward := []float32{.5, .5, .5, .5}
	forward, err = applyDNGImageOpcodes([]dngImageOpcode{{vignette: vignette}, {point: &point}}, forward, 2, 2, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	backward := []float32{.5, .5, .5, .5}
	backward, err = applyDNGImageOpcodes([]dngImageOpcode{{point: &point}, {vignette: vignette}}, backward, 2, 2, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if forward[0] != 1 || backward[0] != .5 {
		t.Fatalf("vignette ordering/clipping: forward=%g backward=%g", forward[0], backward[0])
	}
}

func TestDNGVignetteRadialRejectsMalformedParameters(t *testing.T) {
	valid := testDNGVignetteRadial(.5, .5, [5]float64{.1}).payload
	for name, mutate := range map[string]func([]byte) []byte{
		"short":    func(p []byte) []byte { return p[:len(p)-1] },
		"trailing": func(p []byte) []byte { return append(p, 0) },
		"nan-coefficient": func(p []byte) []byte {
			binary.BigEndian.PutUint64(p, math.Float64bits(math.NaN()))
			return p
		},
		"infinite-coefficient": func(p []byte) []byte {
			binary.BigEndian.PutUint64(p[8:], math.Float64bits(math.Inf(-1)))
			return p
		},
		"nan-center": func(p []byte) []byte {
			binary.BigEndian.PutUint64(p[40:], math.Float64bits(math.NaN()))
			return p
		},
		"infinite-center": func(p []byte) []byte {
			binary.BigEndian.PutUint64(p[48:], math.Float64bits(math.Inf(1)))
			return p
		},
		"center-below-range": func(p []byte) []byte {
			binary.BigEndian.PutUint64(p[40:], math.Float64bits(-.01))
			return p
		},
		"center-above-range": func(p []byte) []byte {
			binary.BigEndian.PutUint64(p[48:], math.Float64bits(1.01))
			return p
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseDNGVignetteRadial(mutate(append([]byte(nil), valid...))); err == nil {
				t.Fatal("malformed radial vignette accepted")
			}
		})
	}
	if _, err := parseDNGVignetteRadial(nil); err == nil {
		t.Fatal("empty radial vignette accepted")
	}
}

func TestLinearDNGVignetteOpcodePipeline(t *testing.T) {
	const width, height = 8, 6
	const centerX, centerY = .25, .75
	coefficients := [5]float64{.4, .1, 0, 0, 0}
	pixels := make([]uint16, width*height)
	for i := range pixels {
		pixels[i] = 32768
	}
	vignette := testDNGVignetteRadial(centerX, centerY, coefficients)
	polynomial := testDNGPolynomial(testDNGArea(image.Rect(0, 0, width, height), 0, 1, 1, 1), 0, 0, 1)
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		data := linearDNGFixture(order, width, height, 1, pixels, map[uint16]dngTestField{
			51009: testDNGOpcodeList(vignette),
			51022: testDNGOpcodeList(polynomial),
		})
		img, err := decodeLinearDNG(data)
		if err != nil {
			t.Fatal(err)
		}
		for _, point := range [][2]int{{2, 4}, {7, 0}, {0, 0}, {6, 5}} {
			gain := referenceDNGVignetteGain(point[0], point[1], width, height, centerX, centerY, coefficients)
			linearAfterVignette := float32((32768.0 / 65535) * gain)
			linearAfterPolynomial := float32(float64(linearAfterVignette) * float64(linearAfterVignette))
			want := referenceDNGVignetteSRGB(float64(linearAfterPolynomial))
			pixel := img.NRGBA64At(point[0], point[1])
			if pixel.R != want || pixel.R != pixel.G || pixel.G != pixel.B {
				t.Fatalf("%s pixel %v=%v want gray %d", order, point, pixel, want)
			}
		}
	}
}

func TestRAWVignetteRadialCLIThroughTIFFAndVideo(t *testing.T) {
	executable, fixtureDir, _ := fakeDNGConverter(t)
	const width, height = 16, 16
	const centerX, centerY = .31, .63
	coefficients := [5]float64{.3, -.04, .015, 0, 0}
	pixels := make([]uint16, width*height)
	for i := range pixels {
		pixels[i] = 24000
	}
	fixture := linearDNGFixture(binary.LittleEndian, width, height, 1, pixels, map[uint16]dngTestField{
		51022: testDNGOpcodeList(testDNGVignetteRadial(centerX, centerY, coefficients)),
	})
	fixturePath := filepath.Join(fixtureDir, "vignette.dng")
	if err := os.WriteFile(fixturePath, fixture, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LRTL_TEST_CONVERTER_FIXTURE", fixturePath)
	dir := t.TempDir()
	source := filepath.Join(dir, "frame.nef")
	if err := os.WriteFile(source, []byte("fixture RAW"), 0600); err != nil {
		t.Fatal(err)
	}
	before := batchFileSnapshot(t, dir)
	output := filepath.Join(t.TempDir(), "export")
	var stdout, stderr bytes.Buffer
	if Main([]string{"export", dir, "--output", output, "--raw-develop", "--dng-converter", executable}, &stdout, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	img := readExportedTIFF(t, filepath.Join(output, "frame000001.tif"))
	for _, point := range [][2]int{{1, 1}, {5, 10}, {12, 3}, {14, 14}} {
		gain := referenceDNGVignetteGain(point[0], point[1], width, height, centerX, centerY, coefficients)
		linear := float32((24000.0 / 65535) * gain)
		want := referenceDNGVignetteSRGB(float64(linear))
		r, g, b, _ := img.At(point[0], point[1]).RGBA()
		if math.Abs(float64(r)-float64(want)) > 1 || r != g || g != b {
			t.Fatalf("TIFF pixel %v=%d,%d,%d want %d", point, r, g, b, want)
		}
	}
	video := filepath.Join(t.TempDir(), "out.avi")
	if Main([]string{"render", output, "--output", video, "--quality", "100"}, &stdout, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	data, err := os.ReadFile(video)
	if err != nil {
		t.Fatal(err)
	}
	start, end := bytes.Index(data, []byte{255, 216}), bytes.Index(data, []byte{255, 217})
	if start < 0 || end <= start {
		t.Fatal("missing AVI JPEG")
	}
	frame, err := jpeg.Decode(bytes.NewReader(data[start : end+2]))
	if err != nil {
		t.Fatal(err)
	}
	point := [2]int{5, 10}
	r, _, _, _ := frame.At(point[0], point[1]).RGBA()
	gain := referenceDNGVignetteGain(point[0], point[1], width, height, centerX, centerY, coefficients)
	want := referenceDNGVignetteSRGB((24000.0 / 65535) * gain)
	if math.Abs(float64(r>>8)-float64(want)/257) > 2 {
		t.Fatalf("AVI pixel=%d want %d", r>>8, want/257)
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("RAW source changed")
	}

	badFixture := linearDNGFixture(binary.LittleEndian, 16, 16, 1, pixels, map[uint16]dngTestField{
		51022: testDNGOpcodeList(testDNGVignetteRadial(.5, .5, [5]float64{math.MaxFloat64, math.MaxFloat64, math.MaxFloat64, math.MaxFloat64, math.MaxFloat64})),
	})
	if err := os.WriteFile(fixturePath, badFixture, 0600); err != nil {
		t.Fatal(err)
	}
	failedParent := t.TempDir()
	if Main([]string{"export", dir, "--output", filepath.Join(failedParent, "failed"), "--raw-develop", "--dng-converter", executable}, &stdout, &stderr) == 0 {
		t.Fatal("non-finite vignette gain export succeeded")
	}
	entries, err := os.ReadDir(failedParent)
	if err != nil || len(entries) != 0 {
		t.Fatalf("failed vignette export left files: %v, %v", err, entries)
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("failed export changed RAW source")
	}
}

func FuzzDNGVignetteRadialParameters(f *testing.F) {
	f.Add(testDNGVignetteRadial(.5, .5, [5]float64{.2, -.03, .01, 0, 0}).payload, uint8(0))
	f.Add(testDNGVignetteRadial(0, 1, [5]float64{math.MaxFloat64}).payload, uint8(1))
	f.Add([]byte{}, uint8(0))
	f.Fuzz(func(t *testing.T, payload []byte, selector uint8) {
		if len(payload) > 4096 {
			return
		}
		vignette, err := parseDNGVignetteRadial(payload)
		if err != nil {
			return
		}
		channels := 1 + 2*int(selector&1)
		pixels := make([]float32, 8*6*channels)
		for i := range pixels {
			pixels[i] = float32(i%17) / 16
		}
		if err := vignette.apply(pixels, 8, 6, channels); err != nil {
			return // Finite coefficients can overflow when evaluated.
		}
		for _, pixel := range pixels {
			if math.IsNaN(float64(pixel)) || math.IsInf(float64(pixel), 0) || pixel < 0 || pixel > 1 {
				t.Fatalf("invalid normalized vignette sample %g", pixel)
			}
		}
	})
}
