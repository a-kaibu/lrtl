package lrtl

import (
	"bytes"
	"encoding/binary"
	"image/jpeg"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

type testDNGRectilinear2Parameters struct {
	radial                         [15]float64
	tangential                     [2]float64
	minValidRadius, maxValidRadius float64
	centerX, centerY               float64
}

func testDNGWarpRectilinear2(reciprocal bool, sets ...testDNGRectilinear2Parameters) testDNGOpcode {
	payload := binary.BigEndian.AppendUint32(nil, uint32(len(sets)))
	for _, set := range sets {
		for _, coefficient := range set.radial {
			payload = binary.BigEndian.AppendUint64(payload, math.Float64bits(coefficient))
		}
		for _, coefficient := range set.tangential {
			payload = binary.BigEndian.AppendUint64(payload, math.Float64bits(coefficient))
		}
		for _, value := range []float64{set.minValidRadius, set.maxValidRadius, set.centerX, set.centerY} {
			payload = binary.BigEndian.AppendUint64(payload, math.Float64bits(value))
		}
	}
	var reciprocalFlag uint32
	if reciprocal {
		reciprocalFlag = 1
	}
	payload = binary.BigEndian.AppendUint32(payload, reciprocalFlag)
	return testDNGOpcode{14, 0x01060000, 0, payload}
}

// Independent explicit-power implementation of the DNG WarpRectilinear2 map.
func referenceDNGWarpRectilinear2Source(width, height int, verticalScale float64, set testDNGRectilinear2Parameters, reciprocal bool, x, y int) (float64, float64) {
	squareHeight := math.Round(float64(height) * verticalScale)
	cx, cy := float64(width)*set.centerX, float64(height)*set.centerY
	squareCenterY := squareHeight * set.centerY
	radiusX := max(math.Abs(cx), math.Abs(float64(width)-cx))
	radiusY := max(math.Abs(squareCenterY), math.Abs(squareHeight-squareCenterY))
	radius := math.Hypot(radiusX, radiusY)
	dx, dy := float64(x)-cx, float64(y)-cy
	u, v := dx/radius, dy*verticalScale/radius
	r := min(1, math.Hypot(u, v))
	r = min(set.maxValidRadius, max(set.minValidRadius, r))
	factor, power := 0.0, 1.0
	for _, coefficient := range set.radial {
		factor += coefficient * power
		power *= r
	}
	if reciprocal {
		factor = 1 / factor
	}
	r2 := u*u + v*v
	tangentX := 2*set.tangential[0]*u*v + set.tangential[1]*(r2+2*u*u)
	tangentY := 2*set.tangential[1]*u*v + set.tangential[0]*(r2+2*v*v)
	return cx + dx*factor + radius*tangentX, cy + dy*factor + radius*tangentY/verticalScale
}

func referenceWarpRectilinear2SRGB(linear float64) uint16 {
	linear = min(1, max(0, linear))
	if linear <= 0.0031308 {
		linear *= 12.92
	} else {
		linear = 1.055*math.Pow(linear, 1/2.4) - 0.055
	}
	return uint16(math.Round(min(1, max(0, linear)) * 65535))
}

func TestDNGWarpRectilinear2CoordinatesAndPerPlaneParameters(t *testing.T) {
	const width, height, verticalScale = 13, 9, 1.5
	sets := []testDNGRectilinear2Parameters{
		{radial: [15]float64{.95, .08, -.025, .006, -.001}, tangential: [2]float64{.012, -.009}, minValidRadius: .12, maxValidRadius: .88, centerX: .31, centerY: .69},
		{radial: [15]float64{1.04, -.04, .015, -.003}, tangential: [2]float64{-.006, .014}, minValidRadius: 0, maxValidRadius: .72, centerX: .62, centerY: .27},
		{radial: [15]float64{.9, .1, -.02, .004}, tangential: [2]float64{.003, .005}, minValidRadius: .2, maxValidRadius: 1, centerX: .48, centerY: .52},
	}
	warp, err := parseDNGWarpRectilinear2(testDNGWarpRectilinear2(false, sets...).payload, len(sets))
	if err != nil {
		t.Fatal(err)
	}
	geometry := warp.geometry(width, height, verticalScale)
	for plane, set := range sets {
		for _, point := range [][2]int{{0, 0}, {3, 5}, {8, 2}, {12, 8}, {6, 4}} {
			gotX, gotY := geometry.source(point[0], point[1], plane)
			wantX, wantY := referenceDNGWarpRectilinear2Source(width, height, verticalScale, set, false, point[0], point[1])
			if math.Abs(gotX-wantX) > 2e-12 || math.Abs(gotY-wantY) > 2e-12 {
				t.Fatalf("plane %d source(%d,%d)=(%.16g,%.16g) want (%.16g,%.16g)", plane, point[0], point[1], gotX, gotY, wantX, wantY)
			}
		}
	}
	centerSet := testDNGRectilinear2Parameters{radial: [15]float64{.8, .05}, minValidRadius: 0, maxValidRadius: 1, centerX: .5, centerY: .5}
	centerWarp, err := parseDNGWarpRectilinear2(testDNGWarpRectilinear2(false, centerSet).payload, 1)
	if err != nil {
		t.Fatal(err)
	}
	cx, cy := centerWarp.geometry(10, 8, 1).source(5, 4, 0)
	if cx != 5 || cy != 4 {
		t.Fatalf("optical center moved to (%g,%g)", cx, cy)
	}
}

func TestDNGWarpRectilinear2ReciprocalRadialAndIdentity(t *testing.T) {
	set := testDNGRectilinear2Parameters{radial: [15]float64{1, .15, -.02, .004}, tangential: [2]float64{.008, -.01}, minValidRadius: .25, maxValidRadius: .75, centerX: .42, centerY: .56}
	warp, err := parseDNGWarpRectilinear2(testDNGWarpRectilinear2(true, set).payload, 1)
	if err != nil {
		t.Fatal(err)
	}
	geometry := warp.geometry(11, 7, 1)
	for _, point := range [][2]int{{0, 0}, {2, 4}, {7, 1}, {10, 6}} {
		gotX, gotY := geometry.source(point[0], point[1], 0)
		wantX, wantY := referenceDNGWarpRectilinear2Source(11, 7, 1, set, true, point[0], point[1])
		if math.Abs(gotX-wantX) > 2e-12 || math.Abs(gotY-wantY) > 2e-12 {
			t.Fatalf("reciprocal source(%d,%d)=(%g,%g) want (%g,%g)", point[0], point[1], gotX, gotY, wantX, wantY)
		}
	}
	identity := testDNGRectilinear2Parameters{radial: [15]float64{1}, minValidRadius: .1, maxValidRadius: .9, centerX: 0, centerY: 1}
	for _, reciprocal := range []bool{false, true} {
		warp, err = parseDNGWarpRectilinear2(testDNGWarpRectilinear2(reciprocal, identity).payload, 1)
		if err != nil || !warp.identity() {
			t.Fatalf("identity reciprocal=%t: warp=%+v err=%v", reciprocal, warp, err)
		}
	}
}

func TestDNGWarpRectilinear2RejectsMalformedParameters(t *testing.T) {
	set := testDNGRectilinear2Parameters{radial: [15]float64{1, .1}, minValidRadius: 0, maxValidRadius: 1, centerX: .5, centerY: .5}
	valid := testDNGWarpRectilinear2(false, set).payload
	for name, mutate := range map[string]func([]byte) []byte{
		"short":                   func(p []byte) []byte { return p[:3] },
		"truncated-set":           func(p []byte) []byte { return p[:len(p)-9] },
		"trailing":                func(p []byte) []byte { return append(p, 0) },
		"zero-sets":               func(p []byte) []byte { binary.BigEndian.PutUint32(p, 0); return p },
		"too-many-sets":           func(p []byte) []byte { binary.BigEndian.PutUint32(p, 2); return p },
		"nan-radial":              func(p []byte) []byte { binary.BigEndian.PutUint64(p[4:], math.Float64bits(math.NaN())); return p },
		"infinite-tangential":     func(p []byte) []byte { binary.BigEndian.PutUint64(p[4+15*8:], math.Float64bits(math.Inf(1))); return p },
		"nan-min-radius":          func(p []byte) []byte { binary.BigEndian.PutUint64(p[4+17*8:], math.Float64bits(math.NaN())); return p },
		"negative-min-radius":     func(p []byte) []byte { binary.BigEndian.PutUint64(p[4+17*8:], math.Float64bits(-.1)); return p },
		"equal-valid-radii":       func(p []byte) []byte { binary.BigEndian.PutUint64(p[4+18*8:], math.Float64bits(0)); return p },
		"valid-radius-over-one":   func(p []byte) []byte { binary.BigEndian.PutUint64(p[4+18*8:], math.Float64bits(1.1)); return p },
		"center-outside":          func(p []byte) []byte { binary.BigEndian.PutUint64(p[4+19*8:], math.Float64bits(1.01)); return p },
		"nan-center":              func(p []byte) []byte { binary.BigEndian.PutUint64(p[4+20*8:], math.Float64bits(math.NaN())); return p },
		"missing-reciprocal-flag": func(p []byte) []byte { return p[:len(p)-4] },
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseDNGWarpRectilinear2(mutate(append([]byte(nil), valid...)), 1); err == nil {
				t.Fatal("malformed WarpRectilinear2 accepted")
			}
		})
	}
	if _, err := parseDNGWarpRectilinear2(testDNGWarpRectilinear2(false, set, set).payload, 3); err == nil {
		t.Fatal("coefficient count that is neither one nor the plane count accepted")
	}
}

func TestLinearDNGWarpRectilinear2OpcodePipeline(t *testing.T) {
	const width, height = 13, 9
	set := testDNGRectilinear2Parameters{radial: [15]float64{.94, .12, -.025, .006, -.001, .0002}, tangential: [2]float64{.009, -.007}, minValidRadius: .08, maxValidRadius: .91, centerX: .37, centerY: .62}
	pixels := make([]uint16, width*height)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			pixels[y*width+x] = uint16(7000 + 2100*x + 1300*y)
		}
	}
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		fixture := linearDNGFixture(order, width, height, 1, pixels, map[uint16]dngTestField{
			51022: testDNGOpcodeList(testDNGWarpRectilinear2(false, set)),
		})
		img, err := decodeLinearDNG(fixture)
		if err != nil {
			t.Fatal(err)
		}
		for _, point := range [][2]int{{3, 2}, {6, 4}, {9, 6}} {
			sx, sy := referenceDNGWarpRectilinear2Source(width, height, 1, set, false, point[0], point[1])
			linear := referenceDNGCubicSample(pixels, width, height, sx, sy) / 65535
			want := referenceWarpRectilinear2SRGB(linear)
			pixel := img.NRGBA64At(point[0], point[1])
			if pixel.R != want || pixel.R != pixel.G || pixel.G != pixel.B {
				t.Fatalf("%s pixel %v=%v want %d", order, point, pixel, want)
			}
		}
	}
}

func TestLinearDNGWarpRectilinear2OptionalFallbackRule(t *testing.T) {
	const width, height = 8, 8
	pixels := make([]uint16, width*height)
	for i := range pixels {
		pixels[i] = uint16(4000 + i*900)
	}
	set := testDNGRectilinear2Parameters{radial: [15]float64{.72}, minValidRadius: 0, maxValidRadius: 1, centerX: .5, centerY: .5}
	warp2 := testDNGWarpRectilinear2(false, set)
	warp2.flags = 1
	fallback := testDNGWarp(.5, .5, [6]float64{.45})
	base, err := decodeLinearDNG(linearDNGFixture(binary.LittleEndian, width, height, 1, pixels, map[uint16]dngTestField{51022: testDNGOpcodeList(warp2)}))
	if err != nil {
		t.Fatal(err)
	}
	withFallback, err := decodeLinearDNG(linearDNGFixture(binary.LittleEndian, width, height, 1, pixels, map[uint16]dngTestField{51022: testDNGOpcodeList(warp2, fallback)}))
	if err != nil || !bytes.Equal(base.Pix, withFallback.Pix) {
		t.Fatalf("optional WarpRectilinear fallback was not skipped: %v", err)
	}

	warp2.flags = 0
	requiredAndFallback, err := decodeLinearDNG(linearDNGFixture(binary.LittleEndian, width, height, 1, pixels, map[uint16]dngTestField{51022: testDNGOpcodeList(warp2, fallback)}))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(base.Pix, requiredAndFallback.Pix) {
		t.Fatal("required WarpRectilinear2 incorrectly skipped following WarpRectilinear")
	}
}

func TestRAWWarpRectilinear2CLIThroughTIFFAndVideo(t *testing.T) {
	executable, fixtureDir, _ := fakeDNGConverter(t)
	const width, height = 16, 16
	set := testDNGRectilinear2Parameters{radial: [15]float64{.94, .1, -.02, .003, -.0003}, tangential: [2]float64{.002, -.003}, minValidRadius: .05, maxValidRadius: .93, centerX: .34, centerY: .59}
	pixels := make([]uint16, width*height)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			pixels[y*width+x] = uint16(6000 + 1700*x + 1100*y)
		}
	}
	fixture := linearDNGFixture(binary.LittleEndian, width, height, 1, pixels, map[uint16]dngTestField{
		51022: testDNGOpcodeList(testDNGWarpRectilinear2(false, set)),
	})
	fixturePath := filepath.Join(fixtureDir, "warp-rectilinear2.dng")
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
	for _, point := range [][2]int{{3, 4}, {6, 9}, {12, 11}} {
		sx, sy := referenceDNGWarpRectilinear2Source(width, height, 1, set, false, point[0], point[1])
		linear := referenceDNGCubicSample(pixels, width, height, sx, sy) / 65535
		want := referenceWarpRectilinear2SRGB(linear)
		r, g, b, _ := img.At(point[0], point[1]).RGBA()
		if math.Abs(float64(r)-float64(want)) > 1 || r != g || g != b {
			t.Fatalf("TIFF pixel %v=%d,%d,%d want %d", point, r, g, b, want)
		}
	}
	video := filepath.Join(t.TempDir(), "warp-rectilinear2.avi")
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
	point := [2]int{6, 9}
	sx, sy := referenceDNGWarpRectilinear2Source(width, height, 1, set, false, point[0], point[1])
	want := referenceWarpRectilinear2SRGB(referenceDNGCubicSample(pixels, width, height, sx, sy) / 65535)
	r, _, _, _ := frame.At(point[0], point[1]).RGBA()
	if math.Abs(float64(r>>8)-float64(want)/257) > 2 {
		t.Fatalf("AVI pixel=%d want %d", r>>8, want/257)
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("RAW source changed")
	}
}

func FuzzDNGWarpRectilinear2Parameters(f *testing.F) {
	seed := testDNGRectilinear2Parameters{radial: [15]float64{1, .08, -.01, .002}, minValidRadius: .05, maxValidRadius: .9, centerX: .5, centerY: .5}
	f.Add(testDNGWarpRectilinear2(false, seed).payload, uint8(0))
	f.Add(testDNGWarpRectilinear2(true, testDNGRectilinear2Parameters{radial: [15]float64{1, .1}, minValidRadius: .2, maxValidRadius: .8, centerX: .2, centerY: .8}).payload, uint8(1))
	f.Add([]byte{}, uint8(0))
	f.Fuzz(func(t *testing.T, payload []byte, selector uint8) {
		if len(payload) > 4096 {
			return
		}
		channels := 1 + 2*int(selector&1)
		warp, err := parseDNGWarpRectilinear2(payload, channels)
		if err != nil {
			return
		}
		pixels := make([]float32, 8*6*channels)
		for i := range pixels {
			pixels[i] = float32(i%19) / 18
		}
		result, err := applyDNGImageOpcodes([]dngImageOpcode{{warp: warp}}, pixels, 8, 6, channels, []float64{.5, .75, 1, 1.5}[selector%4])
		if err != nil {
			return // A finite but non-invertible polynomial can produce invalid coordinates.
		}
		for _, pixel := range result {
			if math.IsNaN(float64(pixel)) || math.IsInf(float64(pixel), 0) || pixel < 0 || pixel > 1 {
				t.Fatalf("invalid normalized WarpRectilinear2 sample %g", pixel)
			}
		}
	})
}
