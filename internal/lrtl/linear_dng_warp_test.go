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

func testDNGWarp(centerX, centerY float64, coefficients ...[6]float64) testDNGOpcode {
	data := binary.BigEndian.AppendUint32(nil, uint32(len(coefficients)))
	for _, c := range coefficients {
		for _, v := range c {
			data = binary.BigEndian.AppendUint64(data, math.Float64bits(v))
		}
	}
	data = binary.BigEndian.AppendUint64(data, math.Float64bits(centerX))
	data = binary.BigEndian.AppendUint64(data, math.Float64bits(centerY))
	return testDNGOpcode{1, 0x01030000, 0, data}
}

func TestDNGWarpSourceCoordinatesAndPixelAspect(t *testing.T) {
	warp, err := parseDNGWarpRectilinear(testDNGWarp(.5, .5, [6]float64{.5}).payload, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, scale := range []float64{.5, 1, 2} {
		geometry := warp.geometry(8, 6, scale)
		for y := 0; y < 6; y++ {
			for x := 0; x < 8; x++ {
				sx, sy := geometry.source(x, y, 0)
				if sx != 2+float64(x)/2 || sy != 1.5+float64(y)/2 {
					t.Fatalf("integer warp coordinates (%d,%d) -> (%g,%g)", x, y, sx, sy)
				}
			}
		}
	}
	warp, err = parseDNGWarpRectilinear(testDNGWarp(.5, .5, [6]float64{1, .25}).payload, 1)
	if err != nil {
		t.Fatal(err)
	}
	geometry := warp.geometry(8, 6, 2)
	for _, c := range []struct {
		x, y   int
		sx, sy float64
	}{{0, 0, -1, -.75}, {4, 0, 4, -27.0 / 52}, {0, 3, -4.0 / 13, 3}} {
		sx, sy := geometry.source(c.x, c.y, 0)
		if math.Abs(sx-c.sx) > 1e-13 || math.Abs(sy-c.sy) > 1e-13 {
			t.Fatalf("non-square pixel warp (%d,%d) -> (%g,%g) want (%g,%g)", c.x, c.y, sx, sy, c.sx, c.sy)
		}
	}
	// Five rows at half physical height round to three square-pixel rows.
	geometry = warp.geometry(8, 5, .5)
	sx, sy := geometry.source(0, 0, 0)
	if math.Abs(geometry.radius-math.Sqrt(73)/2) > 1e-14 || math.Abs(sx+281.0/292) > 1e-14 || math.Abs(sy+(5.0/8)*(281.0/292)) > 1e-14 {
		t.Fatalf("rounded pixel aspect geometry: radius=%g source=%g,%g", geometry.radius, sx, sy)
	}
	warp, err = parseDNGWarpRectilinear(testDNGWarp(.5, .5, [6]float64{1}, [6]float64{1, 0, 0, 0, .125}, [6]float64{1, 0, 0, 0, 0, .125}).payload, 3)
	if err != nil {
		t.Fatal(err)
	}
	geometry = warp.geometry(8, 8, 1)
	for plane, c := range [][2]float64{{6, 4}, {6, 4 + math.Sqrt(32)/64}, {6 + 3*math.Sqrt(32)/64, 4}} {
		sx, sy := geometry.source(6, 4, plane)
		if math.Abs(sx-c[0]) > 1e-14 || math.Abs(sy-c[1]) > 1e-14 {
			t.Fatalf("tangential plane %d -> %g,%g want %v", plane, sx, sy, c)
		}
	}
}

func TestDNGWarpBicubicWeightsAndEdges(t *testing.T) {
	for _, c := range []struct{ x, w float64 }{{0, 1}, {.5, .59375}, {1, 0}, {1.5, -.09375}, {2, 0}, {3, 0}} {
		if dngWarpCubicWeight(c.x) != c.w || dngWarpCubicWeight(-c.x) != c.w {
			t.Fatalf("cubic weight %g", c.x)
		}
	}
	data := []float32{0, 0, 1, 0, 0}
	for _, c := range []struct{ x, w float64 }{{1.5, .59375}, {2.25, .87890625}, {2, 1}, {-100, 0}, {100, 0}} {
		got := sampleDNGWarpPlane(data, 5, 1, 1, 0, c.x, 0)
		if float64(got) != c.w {
			t.Fatalf("cubic sample %g got %g want %g", c.x, got, c.w)
		}
	}
	constant := make([]float32, 5*4)
	for i := range constant {
		constant[i] = .375
	}
	for _, x := range []float64{-10, 0, .1, 1.2, 3.9, 10} {
		for _, y := range []float64{-10, 0, .3, 2.7, 10} {
			if got := sampleDNGWarpPlane(constant, 5, 4, 1, 0, x, y); got != .375 {
				t.Fatal("constant field changed")
			}
		}
	}
}

func TestDNGWarpIndependentColorPlanesAndOrder(t *testing.T) {
	warp, err := parseDNGWarpRectilinear(testDNGWarp(.5, .5, [6]float64{1}, [6]float64{.5}, [6]float64{1.5}).payload, 3)
	if err != nil {
		t.Fatal(err)
	}
	data := make([]float32, 8*8*3)
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			for p := 0; p < 3; p++ {
				data[(y*8+x)*3+p] = float32(.0625 + float64(x)/128 + float64(y)/64 + float64(p)/16)
			}
		}
	}
	got, err := applyDNGImageOpcodes([]dngImageOpcode{{warp: warp}}, data, 8, 8, 3, 1)
	if err != nil {
		t.Fatal(err)
	}
	for y := 3; y <= 5; y++ {
		for x := 3; x <= 5; x++ {
			for p, factor := range []float64{1, .5, 1.5} {
				sx, sy := 4+(float64(x)-4)*factor, 4+(float64(y)-4)*factor
				want := .0625 + sx/128 + sy/64 + float64(p)/16
				if float64(got[(y*8+x)*3+p]) != want {
					t.Fatalf("color plane %d (%d,%d)", p, x, y)
				}
			}
		}
	}
}

func warpDNGFixture(order binary.ByteOrder) []byte {
	pixels := make([]uint16, 12*10)
	for y := 0; y < 6; y++ {
		for x := 0; x < 8; x++ {
			pixels[(y+2)*12+x+3] = uint16(4096 + (x+2*y)*1024)
		}
	}
	area := testDNGArea(image.Rect(0, 0, 8, 6), 0, 1, 1, 1)
	return linearDNGFixture(order, 12, 10, 1, pixels, map[uint16]dngTestField{
		50829: dngTestLong(order, 2, 3, 8, 11), 50719: dngTestLong(order, 2, 1), 50720: dngTestLong(order, 4, 4), 50717: dngTestLong(order, 65536),
		51009: testDNGOpcodeList(testDNGGainMap(testDNGGainMapPayload(8, 6, 1, 1, 1, 2)), testDNGWarp(.5, .5, [6]float64{.5})), 51022: testDNGOpcodeList(testDNGPolynomial(area, 0, 0, 1)),
	})
}

func TestLinearDNGWarpBeforeColorAndCrop(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		img, err := decodeLinearDNG(warpDNGFixture(order))
		if err != nil {
			t.Fatal(err)
		}
		if img.Bounds() != image.Rect(0, 0, 4, 4) {
			t.Fatal(img.Bounds())
		}
		for y := 0; y < 4; y++ {
			for x := 0; x < 4; x++ {
				sx, sy := 2+float64(x+2)/2, 1.5+float64(y+1)/2
				linear := 2 * (.0625 + (sx+2*sy)/64)
				linear *= linear
				want := uint16(math.Round(65535 * (1.055*math.Pow(linear, 1/2.4) - .055)))
				p := img.NRGBA64At(x, y)
				if p.R != want || p.G != want || p.B != want {
					t.Fatalf("warped crop %s (%d,%d)=%v want %d", order, x, y, p, want)
				}
			}
		}
	}
}

func TestLinearDNGIdentityWarpPreservesAllSamples(t *testing.T) {
	pixels := make([]uint16, 65536)
	for i := range pixels {
		pixels[i] = uint16(i)
	}
	plain, err := decodeLinearDNG(linearDNGFixture(binary.LittleEndian, 256, 256, 1, pixels, nil))
	if err != nil {
		t.Fatal(err)
	}
	for _, center := range [][2]float64{{0, 0}, {.5, .5}, {1, 1}} {
		data := linearDNGFixture(binary.LittleEndian, 256, 256, 1, pixels, map[uint16]dngTestField{51022: testDNGOpcodeList(testDNGWarp(center[0], center[1], [6]float64{1}))})
		img, err := decodeLinearDNG(data)
		if err != nil || !bytes.Equal(img.Pix, plain.Pix) {
			t.Fatalf("identity warp changed 16-bit samples: %v", err)
		}
	}
}

func TestDNGWarpRejectsMalformedParameters(t *testing.T) {
	valid := testDNGWarp(.5, .5, [6]float64{.5}).payload
	for name, mutate := range map[string]func([]byte) []byte{
		"short": func(p []byte) []byte { return p[:3] }, "truncated": func(p []byte) []byte { return p[:len(p)-1] }, "trailing": func(p []byte) []byte { return append(p, 0) },
		"zero-sets": func(p []byte) []byte { binary.BigEndian.PutUint32(p, 0); return p }, "wrong-sets": func(p []byte) []byte { binary.BigEndian.PutUint32(p, 2); return p },
		"nan-coefficient": func(p []byte) []byte { binary.BigEndian.PutUint64(p[4:], math.Float64bits(math.NaN())); return p }, "infinite-coefficient": func(p []byte) []byte { binary.BigEndian.PutUint64(p[12:], math.Float64bits(math.Inf(1))); return p },
		"nan-center": func(p []byte) []byte { binary.BigEndian.PutUint64(p[52:], math.Float64bits(math.NaN())); return p }, "outside-center": func(p []byte) []byte { binary.BigEndian.PutUint64(p[60:], math.Float64bits(1.01)); return p },
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseDNGWarpRectilinear(mutate(append([]byte(nil), valid...)), 3); err == nil {
				t.Fatal("malformed warp accepted")
			}
		})
	}
	warp, err := parseDNGWarpRectilinear(testDNGWarp(.5, .5, [6]float64{math.MaxFloat64, math.MaxFloat64}).payload, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := applyDNGImageOpcodes([]dngImageOpcode{{warp: warp}}, make([]float32, 16), 4, 4, 1, 1); err == nil {
		t.Fatal("non-finite mapped coordinate accepted")
	}
	if _, err := applyDNGImageOpcodes([]dngImageOpcode{{}}, make([]float32, 1), 1, 1, 1, 1); err == nil {
		t.Fatal("empty image opcode accepted")
	}
}

func TestDNGWarpRepeatedBuffers(t *testing.T) {
	warp, err := parseDNGWarpRectilinear(testDNGWarp(.5, .5, [6]float64{.5}).payload, 1)
	if err != nil {
		t.Fatal(err)
	}
	for count := 2; count <= 4; count++ {
		data := make([]float32, 16*16)
		for y := 0; y < 16; y++ {
			for x := 0; x < 16; x++ {
				data[y*16+x] = float32(.125 + float64(x+2*y)/128)
			}
		}
		opcodes := make([]dngImageOpcode, count)
		for i := range opcodes {
			opcodes[i].warp = warp
		}
		got, err := applyDNGImageOpcodes(opcodes, data, 16, 16, 1, 1)
		if err != nil {
			t.Fatal(err)
		}
		factor := math.Exp2(-float64(count))
		for y := 6; y <= 10; y++ {
			for x := 6; x <= 10; x++ {
				sx, sy := 8+float64(x-8)*factor, 8+float64(y-8)*factor
				want := float32(.125 + (sx+2*sy)/128)
				if got[y*16+x] != want {
					t.Fatalf("%d warps (%d,%d) got %g want %g", count, x, y, got[y*16+x], want)
				}
			}
		}
	}
}

func FuzzDNGWarpParameters(f *testing.F) {
	f.Add(testDNGWarp(.5, .5, [6]float64{.5}).payload, uint8(0))
	f.Add(testDNGWarp(0, 1, [6]float64{1, .1, -.01, .001, .02, -.03}, [6]float64{.75}, [6]float64{1.25}).payload, uint8(1))
	f.Add(testDNGWarp(.5, .5, [6]float64{math.MaxFloat64, math.MaxFloat64}).payload, uint8(0))
	f.Fuzz(func(t *testing.T, payload []byte, selector uint8) {
		if len(payload) > 4096 {
			return
		}
		channels := 1 + 2*int(selector&1)
		warp, err := parseDNGWarpRectilinear(payload, channels)
		if err != nil {
			return
		}
		pixels := make([]float32, 8*6*channels)
		for i := range pixels {
			pixels[i] = float32(i) / float32(len(pixels))
		}
		got, err := applyDNGImageOpcodes([]dngImageOpcode{{warp: warp}}, pixels, 8, 6, channels, []float64{.5, .7, 1, 2}[selector%4])
		if err != nil {
			return // Finite coefficients may overflow during coordinate evaluation.
		}
		for _, v := range got {
			if math.IsNaN(float64(v)) || v < 0 || v > 1 {
				t.Fatalf("invalid normalized warp sample %g", v)
			}
		}
	})
}

func TestLinearDNGWarpFallbackAfterUnsupportedOptional(t *testing.T) {
	pixels := make([]uint16, 8*8)
	for i := range pixels {
		pixels[i] = uint16(i * 512)
	}
	warp := testDNGWarp(.5, .5, [6]float64{.5})
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		baseline, err := decodeLinearDNG(linearDNGFixture(order, 8, 8, 1, pixels, map[uint16]dngTestField{51022: testDNGOpcodeList(warp)}))
		if err != nil {
			t.Fatal(err)
		}
		for _, flags := range []uint32{0, 1} {
			data := linearDNGFixture(order, 8, 8, 1, pixels, map[uint16]dngTestField{51022: testDNGOpcodeList(testDNGOpcode{14, 0x01070100, flags, nil}, warp)})
			got, err := decodeLinearDNG(data)
			if flags == 0 {
				if err == nil {
					t.Fatal("required unsupported warp accepted")
				}
			} else if err != nil || !bytes.Equal(got.Pix, baseline.Pix) {
				t.Fatalf("optional warp prevented fallback: %v", err)
			}
		}
	}
}

func TestRAWRectilinearWarpCLIThroughTIFFAndVideo(t *testing.T) {
	executable, fixtureDir, _ := fakeDNGConverter(t)
	fixture := warpDNGFixture(binary.LittleEndian)
	fixturePath := filepath.Join(fixtureDir, "warp.dng")
	if err := os.WriteFile(fixturePath, fixture, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LRTL_TEST_CONVERTER_FIXTURE", fixturePath)
	dir := t.TempDir()
	source := filepath.Join(dir, "frame.nef")
	if err := os.WriteFile(source, fixture, 0600); err != nil {
		t.Fatal(err)
	}
	writeTestXMP(t, source, map[string]string{"crs:Exposure2012": ".5"})
	before := batchFileSnapshot(t, dir)
	output := filepath.Join(t.TempDir(), "export")
	var stdout, stderr bytes.Buffer
	if Main([]string{"export", dir, "--output", output, "--raw-develop", "--dng-converter", executable, "--apply-xmp-exposure"}, &stdout, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	img := readExportedTIFF(t, filepath.Join(output, "frame000001.tif"))
	if img.Bounds() != image.Rect(0, 0, 4, 4) {
		t.Fatal(img.Bounds())
	}
	// Pixel (2,2) maps from active pixel (4,3) to (4,3), so the
	// gain-before-warp value is .4375 and the following polynomial squares it.
	linear := .4375 * .4375
	encoded := math.Round(65535*(1.055*math.Pow(linear, 1/2.4)-.055)) / 65535
	want := math.Round(65535 * (1.055*math.Pow(math.Pow((encoded+.055)/1.055, 2.4)*math.Sqrt2, 1/2.4) - .055))
	r, g, b, _ := img.At(2, 2).RGBA()
	if math.Abs(float64(r)-want) > 1 || r != g || g != b {
		t.Fatalf("RAW warp + XMP pixel %d,%d,%d want %g", r, g, b, want)
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
	r, _, _, _ = frame.At(2, 2).RGBA()
	if math.Abs(float64(r>>8)-want/257) > 2 {
		t.Fatal("AVI differs from warped RAW")
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("RAW or XMP source changed")
	}
	// A finite payload can still overflow while evaluating the warp. Export
	// must report the failure without publishing a partial sequence.
	bad := linearDNGFixture(binary.LittleEndian, 4, 4, 1, make([]uint16, 16), map[uint16]dngTestField{51022: testDNGOpcodeList(testDNGWarp(.5, .5, [6]float64{math.MaxFloat64, math.MaxFloat64}))})
	if err := os.WriteFile(fixturePath, bad, 0600); err != nil {
		t.Fatal(err)
	}
	failedParent := t.TempDir()
	if Main([]string{"export", dir, "--output", filepath.Join(failedParent, "failed"), "--raw-develop", "--dng-converter", executable}, &stdout, &stderr) == 0 {
		t.Fatal("overflowing warp export succeeded")
	}
	entries, err := os.ReadDir(failedParent)
	if err != nil || len(entries) != 0 {
		t.Fatalf("failed warp left export files: %v, %v", entries, err)
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("failed warp changed source files")
	}
}
