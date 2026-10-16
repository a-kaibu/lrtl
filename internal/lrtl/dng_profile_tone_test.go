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

func dngTestFloat(order binary.ByteOrder, values ...float32) dngTestField {
	data := make([]byte, 4*len(values))
	for i, v := range values {
		order.PutUint32(data[4*i:], math.Float32bits(v))
	}
	return dngTestField{11, uint32(len(values)), data}
}

func TestDNGProfileToneNaturalCubic(t *testing.T) {
	c, err := newDNGProfileToneCurve([]float64{0, 0, .5, .25, 1, 1})
	if err != nil {
		t.Fatal(err)
	}
	// The independent analytic solution is x/4+x^3 on [0,.5] and
	// 1+1.75*(x-1)-(x-1)^3 on [.5,1], with zero endpoint curvature.
	for i := 0; i <= 65535; i++ {
		x := float64(i) / 65535
		want := x/4 + x*x*x
		if x > .5 {
			u := x - 1
			want = 1 + 1.75*u - u*u*u
		}
		if math.Abs(c.evaluate(x)-want) > 5e-16 {
			t.Fatalf("natural spline at %g got %g want %g", x, c.evaluate(x), want)
		}
	}
	for _, points := range [][]float64{{0, 0, 1, 1}, {0, .25, 1, .75}, {.25, .125, .75, .875}} {
		c, err := newDNGProfileToneCurve(points)
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i <= 100; i++ {
			x := float64(i) / 100
			u := min(1, max(0, (x-points[0])/(points[2]-points[0])))
			want := points[1] + u*(points[3]-points[1])
			if math.Abs(c.evaluate(x)-want) > 2e-16 {
				t.Fatalf("two-point curve %v at %g", points, x)
			}
		}
	}
	c, err = newDNGProfileToneCurve([]float64{0, 0, .25, .5, .75, .625, 1, 1})
	if err != nil {
		t.Fatal(err)
	}
	// Unequal spacing: 1.5*M1+.5*M2=-10.5 and .5*M1+1.5*M2=7.5.
	if !reflect.DeepEqual(c.second, []float64{0, -9.75, 8.25, 0}) || c.evaluate(.125) != .2880859375 {
		t.Fatalf("unequal knot spacing: curvature=%v value=%g", c.second, c.evaluate(.125))
	}
}

func TestDNGProfileToneHuePreservation(t *testing.T) {
	c, err := newDNGProfileToneCurve([]float64{0, 0, .5, .25, 1, 1})
	if err != nil {
		t.Fatal(err)
	}
	values, mapped := []float64{.125, .4375, .75}, []float64{.033203125, .3056640625, .578125}
	for _, permutation := range [][3]int{{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0}, {0, 0, 2}, {2, 2, 0}, {1, 1, 1}} {
		input := [3]float64{values[permutation[0]], values[permutation[1]], values[permutation[2]]}
		want := [3]float64{mapped[permutation[0]], mapped[permutation[1]], mapped[permutation[2]]}
		if permutation == ([3]int{1, 1, 1}) {
			v := .4375/4 + .4375*.4375*.4375
			want = [3]float64{v, v, v}
		}
		if got := c.apply(input); got != want {
			t.Fatalf("hue-preserving tone %v got %v want %v", input, got, want)
		}
	}
	if got := c.apply([3]float64{-1, .5, 2}); got != ([3]float64{0, .5, 1}) {
		t.Fatal("tone input was not clipped", got)
	}
}

func profileToneDNGFixture(order binary.ByteOrder, exposure float64) []byte {
	// Linear ProPhoto input [.75,.4375,.125]. D50-normalized primaries
	// independently calculated from the published matrix, not production helpers.
	metadata := map[uint16]dngTestField{
		50728: dngTestDouble(order, 1, 1, 1), 50717: dngTestLong(order, 65536),
		50964: dngTestDouble(order, .7977165463596764, .13520280439742792, .031300649242895665, .288, .7119, .0001, 0, 0, .82521),
		50940: dngTestFloat(order, 0, 0, .5, .25, 1, 1), 50730: dngTestDouble(order, exposure),
	}
	pixels := []uint16{49152, 28672, 8192}
	if exposure == 1 {
		pixels = []uint16{24576, 14336, 4096}
	}
	return linearDNGFixture(order, 1, 1, 3, pixels, metadata)
}

func TestLinearDNGProfileToneBeforeSRGB(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		for _, exposure := range []float64{0, 1} {
			img, err := decodeLinearDNG(profileToneDNGFixture(order, exposure))
			if err != nil {
				t.Fatal(err)
			}
			p := img.NRGBA64At(0, 0)
			// Independently calculated ProPhoto->XYZ->linear sRGB result:
			// [.9435499457566469,.24406252748309165,-.01322609931011013].
			if p.R != 63881 || p.G != 34812 || p.B != 0 || p.A != 65535 {
				t.Fatalf("profile tone (%s, EV %g) got %v", order, exposure, p)
			}
		}
	}
}

func TestLinearDNGProfileToneRootInheritance(t *testing.T) {
	order := binary.LittleEndian
	curve := dngTestFloat(order, 0, 0, .5, .25, 1, 1)
	for _, childOverride := range []bool{false, true} {
		child := map[uint16]dngTestField{}
		if childOverride {
			child[50940] = dngTestFloat(order, 0, 0, 1, 1)
		}
		data := linearDNGFixture(order, 1, 1, 1, []uint16{16384}, child)
		root := len(data)
		data = append(data, make([]byte, 2+4*12+4)...)
		order.PutUint32(data[4:], uint32(root))
		order.PutUint16(data[root:], 4)
		for i, entry := range []struct {
			tag, kind    uint16
			count, value uint32
		}{{262, 3, 1, 2}, {330, 4, 1, 8}, {50706, 1, 4, 0x00000401}, {50940, 11, curve.count, uint32(len(data))}} {
			offset := root + 2 + i*12
			order.PutUint16(data[offset:], entry.tag)
			order.PutUint16(data[offset+2:], entry.kind)
			order.PutUint32(data[offset+4:], entry.count)
			order.PutUint32(data[offset+8:], entry.value)
		}
		data = append(data, curve.data...)
		got, err := decodeLinearDNG(data)
		if err != nil {
			t.Fatal(err)
		}
		if !childOverride {
			child[50940] = curve
		}
		want, err := decodeLinearDNG(linearDNGFixture(order, 1, 1, 1, []uint16{16384}, child))
		if err != nil || !bytes.Equal(got.Pix, want.Pix) {
			t.Fatalf("root profile inheritance override=%t: %v", childOverride, err)
		}
	}
}

func TestLinearDNGProfileToneIdentityAndMalformed(t *testing.T) {
	order := binary.LittleEndian
	pixels := make([]uint16, 65536)
	for i := range pixels {
		pixels[i] = uint16(i)
	}
	plain, err := decodeLinearDNG(linearDNGFixture(order, 256, 256, 1, pixels, nil))
	if err != nil {
		t.Fatal(err)
	}
	// The wide-gamut round trip introduces tiny matrix rounding differences;
	// allow one 16-bit code while checking every original sensor value.
	img, err := decodeLinearDNG(linearDNGFixture(order, 256, 256, 1, pixels, map[uint16]dngTestField{50940: dngTestFloat(order, 0, 0, .25, .25, .5, .5, 1, 1)}))
	if err != nil {
		t.Fatal(err)
	}
	for y := 0; y < 256; y++ {
		for x := 0; x < 256; x++ {
			got, want := img.NRGBA64At(x, y), plain.NRGBA64At(x, y)
			for _, v := range []uint16{got.R, got.G, got.B} {
				if math.Abs(float64(v)-float64(want.R)) > 1 {
					t.Fatalf("identity tone changed sensor code %d: %v vs %v", y*256+x, got, want)
				}
			}
		}
	}
	for _, field := range []dngTestField{
		dngTestLong(order, 0, 0, 1, 1), dngTestFloat(order, 0, 0), dngTestFloat(order, 0, 0, 1),
		dngTestFloat(order, 0, 0, 0, 1), dngTestFloat(order, .5, 0, .25, 1), dngTestFloat(order, 0, 0, 1, 2),
		dngTestFloat(order, -1, 0, 1, 1), dngTestFloat(order, 0, float32(math.NaN()), 1, 1), dngTestFloat(order, 0, 0, float32(math.Inf(1)), 1),
	} {
		if _, err := decodeLinearDNG(linearDNGFixture(order, 1, 1, 1, []uint16{1}, map[uint16]dngTestField{50940: field})); err == nil {
			t.Fatalf("invalid profile curve accepted: %v", field)
		}
	}
}

func TestRAWProfileToneCLIThroughTIFFAndVideo(t *testing.T) {
	executable, fixtureDir, _ := fakeDNGConverter(t)
	fixturePath := filepath.Join(fixtureDir, "tone.dng")
	fixture := linearDNGFixture(binary.LittleEndian, 8, 8, 1, make([]uint16, 64), map[uint16]dngTestField{
		50714: dngTestDouble(binary.LittleEndian, -8192), 50717: dngTestLong(binary.LittleEndian, 57344),
		50940: dngTestFloat(binary.LittleEndian, 0, 0, .5, .25, 1, 1),
		51022: testDNGOpcodeList(testDNGPolynomial(testDNGArea(image.Rect(0, 0, 8, 8), 0, 1, 1, 1), 0, 2)),
	}) // .125 normalized -> .25 after opcode -> .078125 profile tone.
	if err := os.WriteFile(fixturePath, fixture, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LRTL_TEST_CONVERTER_FIXTURE", fixturePath)
	dir := t.TempDir()
	source := filepath.Join(dir, "frame.nef")
	if err := os.WriteFile(source, fixture, 0600); err != nil {
		t.Fatal(err)
	}
	writeTestXMP(t, source, map[string]string{"crs:Exposure2012": "1"})
	before := batchFileSnapshot(t, dir)
	out := filepath.Join(t.TempDir(), "export")
	var stdout, stderr bytes.Buffer
	if Main([]string{"export", dir, "--output", out, "--raw-develop", "--dng-converter", executable, "--apply-xmp-exposure"}, &stdout, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	img := readExportedTIFF(t, filepath.Join(out, "frame000001.tif"))
	// Neutral profile tone followed by XMP +1 EV -> .15625 linear.
	want := math.Round(65535 * (1.055*math.Pow(.15625, 1/2.4) - .055))
	r, g, b, _ := img.At(4, 4).RGBA()
	if math.Abs(float64(r)-want) > 2 || math.Abs(float64(g)-want) > 2 || math.Abs(float64(b)-want) > 2 {
		t.Fatalf("profile + opcode + XMP got %d,%d,%d want %g", r, g, b, want)
	}
	video := filepath.Join(t.TempDir(), "out.avi")
	if Main([]string{"render", out, "--output", video, "--quality", "100"}, &stdout, &stderr) != 0 {
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
	r, _, _, _ = frame.At(4, 4).RGBA()
	if math.Abs(float64(r>>8)-want/257) > 2 {
		t.Fatal("profile tone did not reach video")
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("profile rendering changed RAW or XMP")
	}
	bad := linearDNGFixture(binary.LittleEndian, 1, 1, 1, []uint16{1000}, map[uint16]dngTestField{50940: dngTestFloat(binary.LittleEndian, 0, 0, 0, 1)})
	if err := os.WriteFile(fixturePath, bad, 0600); err != nil {
		t.Fatal(err)
	}
	parent := t.TempDir()
	if Main([]string{"export", dir, "--output", filepath.Join(parent, "failed"), "--raw-develop", "--dng-converter", executable}, &stdout, &stderr) == 0 {
		t.Fatal("invalid profile curve export succeeded")
	}
	entries, err := os.ReadDir(parent)
	if err != nil || len(entries) != 0 {
		t.Fatalf("failed profile left export files: %v %v", entries, err)
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("failed profile export changed sources")
	}
}

func FuzzDNGProfileTone(f *testing.F) {
	f.Add(dngTestFloat(binary.LittleEndian, 0, 0, .5, .25, 1, 1).data)
	f.Add(dngTestFloat(binary.LittleEndian, .1, .9, .3, .7, .4, .2, .8, .1).data)
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 8192*4 || len(data)%4 != 0 {
			return
		}
		values := make([]float64, len(data)/4)
		for i := range values {
			values[i] = float64(math.Float32frombits(binary.LittleEndian.Uint32(data[4*i:])))
		}
		c, err := newDNGProfileToneCurve(values)
		if err != nil {
			return
		}
		for i := 0; i <= 64; i++ {
			x := float64(i) / 64
			got := c.apply([3]float64{x, .3, .7})
			for _, value := range got {
				if math.IsNaN(value) || value < 0 || value > 1 {
					t.Fatalf("non-finite or out-of-range tone %g", value)
				}
			}
		}
	})
}
