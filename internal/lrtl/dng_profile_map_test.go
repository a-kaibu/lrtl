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

func testDNGProfileMapFields(order binary.ByteOrder, dimsTag, dataTag, encodingTag uint16, h, s, v int, omittedZero bool, fn func(int, int, int) [3]float32) map[uint16]dngTestField {
	var data []float32
	for vi := 0; vi < v; vi++ {
		for hi := 0; hi < h; hi++ {
			for si := 0; si < s; si++ {
				if omittedZero && si == 0 {
					continue
				}
				p := fn(hi, si, vi)
				data = append(data, p[:]...)
			}
		}
	}
	return map[uint16]dngTestField{dimsTag: dngTestLong(order, uint32(h), uint32(s), uint32(v)), dataTag: dngTestFloat(order, data...), encodingTag: dngTestLong(order, 0)}
}

func readTestDNGProfileMap(t *testing.T, order binary.ByteOrder, metadata map[uint16]dngTestField, dims, data, encoding uint16) *dngProfileMap {
	t.Helper()
	f, err := findLinearDNG(linearDNGFixture(order, 1, 1, 1, []uint16{1}, metadata))
	if err != nil {
		t.Fatal(err)
	}
	m, err := readDNGProfileMap(f, dims, data, encoding)
	if err != nil || m == nil {
		t.Fatalf("profile map: %v", err)
	}
	return m
}

func TestDNGProfileMapTrilinearGridAndHueWrap(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		metadata := testDNGProfileMapFields(order, 50937, 50938, 51107, 4, 3, 3, false, func(h, s, v int) [3]float32 {
			return [3]float32{float32(30*h + 6*s + 12*v), 1 + float32(h)/16 + float32(s)/8 + float32(v)/4, 1 + float32(s*(h+v))/16}
		})
		m := readTestDNGProfileMap(t, order, metadata, 50937, 50938, 51107)
		for _, c := range []struct{ h, s, v, effectiveH float64 }{{.75, .25, .25, .5}, {5.25, .25, .25, 1.5}, {6, 1, 1, 0}, {-6, .5, .5, 0}, {3, 0, .75, 2}} {
			s, v, h := c.s*2, c.v*2, c.effectiveH
			want := [3]float64{30*h + 6*s + 12*v, 1 + h/16 + s/8 + v/4, 1 + s*(h+v)/16}
			if got := m.adjustment(c.h, c.s, c.v); got != want {
				t.Fatalf("grid (%g,%g,%g)=%v want %v", c.h, c.s, c.v, got, want)
			}
		}
	}
}

func TestDNGProfileMapHSVAndNeutral(t *testing.T) {
	for _, shift := range []float32{60, -60, 780} {
		metadata := testDNGProfileMapFields(binary.LittleEndian, 50937, 50938, 51107, 1, 2, 1, false, func(_, _, _ int) [3]float32 { return [3]float32{shift, .5, .5} })
		m := readTestDNGProfileMap(t, binary.LittleEndian, metadata, 50937, 50938, 51107)
		want := [3]float64{.375, .375, .1875}
		if shift == -60 {
			want = [3]float64{.375, .1875, .375}
		}
		if got := m.apply([3]float64{.75, 0, 0}); got != want {
			t.Fatalf("hue shift %g: %v vs %v", shift, got, want)
		}
		for _, gray := range []float64{0, .125, .5, 1} {
			if got := m.apply([3]float64{gray, gray, gray}); got != ([3]float64{gray, gray, gray}) {
				t.Fatal("neutral value changed", got)
			}
		}
	}
	metadata := testDNGProfileMapFields(binary.LittleEndian, 50937, 50938, 51107, 3, 3, 2, false, func(_, _, _ int) [3]float32 { return [3]float32{0, 1, 1} })
	m := readTestDNGProfileMap(t, binary.LittleEndian, metadata, 50937, 50938, 51107)
	for r := 0; r <= 16; r++ {
		for g := 0; g <= 16; g++ {
			for b := 0; b <= 16; b++ {
				input := [3]float64{float64(r) / 16, float64(g) / 16, float64(b) / 16}
				got := m.apply(input)
				for c := range got {
					if math.Abs(got[c]-input[c]) > 5e-16 {
						t.Fatalf("identity HSV %v -> %v", input, got)
					}
				}
			}
		}
	}
}

func TestDNGProfileMapEncodingAndOmittedNeutral(t *testing.T) {
	for _, vals := range []int{1, 2} {
		metadata := testDNGProfileMapFields(binary.LittleEndian, 50981, 50982, 51108, 1, 2, vals, true, func(_, _, _ int) [3]float32 { return [3]float32{0, 1, .5} })
		metadata[51108] = dngTestLong(binary.LittleEndian, 1)
		m := readTestDNGProfileMap(t, binary.LittleEndian, metadata, 50981, 50982, 51108)
		if m.data[0] != ([3]float64{0, 1, 1}) {
			t.Fatal("omitted neutral entry was not extrapolated")
		}
		encoded := 1.055*math.Pow(.25, 1/2.4) - .055
		want := math.Pow((encoded*.5+.055)/1.055, 2.4)
		got := m.apply([3]float64{.25, 0, 0})
		if math.Abs(got[0]-want) > 1e-16 || got[1] != 0 || got[2] != 0 {
			t.Fatalf("sRGB V encoding with %d value rows: %v vs %g", vals, got, want)
		}
		// Neutral V also goes through encoding and decoding, without scaling.
		gray := m.apply([3]float64{.25, .25, .25})
		if math.Abs(gray[0]-.25) > 1e-15 {
			t.Fatal("sRGB neutral changed", gray)
		}
	}
	for _, dims := range [][]uint32{{1, 2}, {1, 2, 0}} {
		metadata := testDNGProfileMapFields(binary.LittleEndian, 50937, 50938, 51107, 1, 2, 1, true, func(_, _, _ int) [3]float32 { return [3]float32{30, 2, .5} })
		metadata[50937] = dngTestLong(binary.LittleEndian, dims...)
		m := readTestDNGProfileMap(t, binary.LittleEndian, metadata, 50937, 50938, 51107)
		if m.vals != 1 || m.data[0] != ([3]float64{30, 2, 1}) {
			t.Fatal("legacy 2D dimensions", m)
		}
	}
}

func TestDNGProfileMapDualInterpolation(t *testing.T) {
	first := &dngProfileMap{1, 2, 1, false, [][3]float64{{0, 1, 1}, {0, 1, .5}}}
	second := &dngProfileMap{1, 2, 1, false, [][3]float64{{120, 2, 1}, {120, 2, 1.5}}}
	maps := dngProfileMaps{first: first, second: second}
	for _, w := range []float64{0, .25, .5, .75, 1} {
		m := maps.hueMap(w)
		if m.data[1] != ([3]float64{120 * w, 1 + w, .5 + w}) || m.data[0][2] != 1 {
			t.Fatalf("dual table weight %g: %v", w, m.data)
		}
	}
	if first.data[1] != ([3]float64{0, 1, .5}) || second.data[1] != ([3]float64{120, 2, 1.5}) {
		t.Fatal("interpolation mutated source tables")
	}
}

func TestLinearDNGProfileMapDualWhiteWeight(t *testing.T) {
	order := binary.LittleEndian
	for _, illuminants := range [][2]uint16{{17, 21}, {21, 17}, {0, 0}, {21, 21}} {
		metadata := profileMapDNGFields(order)
		for _, tag := range []uint16{50940, 50981, 50982, 51108} {
			delete(metadata, tag)
		}
		metadata[50730] = dngTestDouble(order, 0)
		metadata[50721] = dngTestDouble(order, 1, 0, 0, 0, 1, 0, 0, 0, 1)
		metadata[50722] = metadata[50721]
		metadata[50778], metadata[50779] = dngTestShort(order, illuminants[0]), dngTestShort(order, illuminants[1])
		first := testDNGProfileMapFields(order, 50937, 50938, 51107, 1, 2, 1, false, func(_, _, _ int) [3]float32 { return [3]float32{0, 1, 1} })
		second := testDNGProfileMapFields(order, 50937, 50939, 51107, 1, 2, 1, false, func(_, _, _ int) [3]float32 { return [3]float32{120, 1, 1} })
		for tag, field := range first {
			metadata[tag] = field
		}
		metadata[50939] = second[50939]
		img, err := decodeLinearDNG(linearDNGFixture(order, 1, 1, 3, []uint16{16384, 0, 0}, metadata))
		if err != nil {
			t.Fatal(err)
		}
		// Identity color matrices and neutral [1,1,1] give x=y=1/3.
		// Calculate McCamy temperature and reciprocal-temperature weighting
		// independently; reversing the illuminants reverses table weights.
		n := (1.0/3 - .332) / (1.0/3 - .1858)
		temperature := -449*n*n*n + 3525*n*n - 6823.3*n + 5520.33
		weight := (1/temperature - 1.0/2856) / (1.0/6500 - 1.0/2856)
		if illuminants[0] == 21 && illuminants[1] == 17 {
			weight = 1 - weight
		}
		if illuminants[0] == illuminants[1] {
			weight = 0
		}
		hue := 2 * weight
		r, g := .25, .25*hue
		if hue >= 1 {
			r, g = .25*(2-hue), .25
		}
		linear := [3]float64{2.0342712552802047*r - .7273412704320137*g, -.2289309957339863*r + 1.2317689013144165*g, -.008557566957189172*r - .15329177133678573*g}
		got := img.NRGBA64At(0, 0)
		for c, value := range []uint16{got.R, got.G, got.B} {
			x := min(1, max(0, linear[c]))
			encoded := 12.92 * x
			if x > .0031308 {
				encoded = 1.055*math.Pow(x, 1/2.4) - .055
			}
			if math.Abs(float64(value)-math.Round(encoded*65535)) > 1 {
				t.Fatalf("dual white %v: pixel=%v expected linear=%v weight=%g", illuminants, got, linear, weight)
			}
		}
	}
}

func profileMapDNGFields(order binary.ByteOrder) map[uint16]dngTestField {
	metadata := testDNGProfileMapFields(order, 50937, 50938, 51107, 1, 2, 2, false, func(_, s, v int) [3]float32 { return [3]float32{60, 1, 1 + float32(s*v)} })
	look := testDNGProfileMapFields(order, 50981, 50982, 51108, 1, 2, 1, false, func(_, _, _ int) [3]float32 { return [3]float32{60, 1, .5} })
	for tag, field := range look {
		metadata[tag] = field
	}
	metadata[50728] = dngTestDouble(order, 1, 1, 1)
	metadata[50717] = dngTestLong(order, 65536)
	metadata[50964] = dngTestDouble(order, .7977165463596764, .13520280439742792, .031300649242895665, .288, .7119, .0001, 0, 0, .82521)
	metadata[50730] = dngTestDouble(order, 1)
	metadata[50940] = dngTestFloat(order, 0, 0, .5, .25, 1, 1)
	return metadata
}

func profileMapDNGFixture(order binary.ByteOrder) []byte {
	pixels := make([]uint16, 8*8*3)
	for i := 0; i < len(pixels); i += 3 {
		pixels[i] = 16384
	}
	return linearDNGFixture(order, 8, 8, 3, pixels, profileMapDNGFields(order))
}

func TestLinearDNGProfileMapWithoutToneCurve(t *testing.T) {
	metadata := profileMapDNGFields(binary.LittleEndian)
	delete(metadata, 50940)
	img, err := decodeLinearDNG(linearDNGFixture(binary.LittleEndian, 1, 1, 3, []uint16{16384, 0, 0}, metadata))
	if err != nil {
		t.Fatal(err)
	}
	linear := .3125 * 1.2317689013144165
	want := math.Round(65535 * (1.055*math.Pow(linear, 1/2.4) - .055))
	p := img.NRGBA64At(0, 0)
	if p.R != 0 || math.Abs(float64(p.G)-want) > 1 || p.B != 0 {
		t.Fatalf("profile maps without curve: %v want green %g", p, want)
	}
}

func TestLinearDNGProfileMapMalformed(t *testing.T) {
	order := binary.LittleEndian
	for name, edit := range map[string]func(map[uint16]dngTestField){
		"missing-dims":    func(m map[uint16]dngTestField) { delete(m, 50937) },
		"short-dims":      func(m map[uint16]dngTestField) { m[50937] = dngTestLong(order, 1) },
		"wrong-dims-type": func(m map[uint16]dngTestField) { m[50937] = dngTestShort(order, 1, 2, 2) },
		"zero-hues":       func(m map[uint16]dngTestField) { m[50937] = dngTestLong(order, 0, 2, 2) },
		"one-sat":         func(m map[uint16]dngTestField) { m[50937] = dngTestLong(order, 1, 1, 2) },
		"large-dims":      func(m map[uint16]dngTestField) { m[50937] = dngTestLong(order, 65536, 65536, 65536) },
		"wrong-data-type": func(m map[uint16]dngTestField) { m[50938] = dngTestLong(order, 0, 1, 1) },
		"wrong-count":     func(m map[uint16]dngTestField) { m[50938] = dngTestFloat(order, 0, 1, 1) },
		"nan-data": func(m map[uint16]dngTestField) {
			f := m[50938]
			order.PutUint32(f.data, math.Float32bits(float32(math.NaN())))
			m[50938] = f
		},
		"negative-scale": func(m map[uint16]dngTestField) {
			f := m[50938]
			order.PutUint32(f.data[4:], math.Float32bits(-1))
			m[50938] = f
		},
		"second-without-first": func(m map[uint16]dngTestField) { m[50939] = m[50938]; delete(m, 50938) },
		"third-table":          func(m map[uint16]dngTestField) { m[52537] = m[50938] },
		"encoding":             func(m map[uint16]dngTestField) { m[51107] = dngTestLong(order, 2) },
		"encoding-type":        func(m map[uint16]dngTestField) { m[51107] = dngTestShort(order, 1) },
		"encoding-count":       func(m map[uint16]dngTestField) { m[51107] = dngTestLong(order, 0, 0) },
		"color-overflow": func(m map[uint16]dngTestField) {
			m[50964] = dngTestDouble(order, math.MaxFloat64, 0, 0, 0, 1, 0, 0, 0, 1)
		},
	} {
		t.Run(name, func(t *testing.T) {
			metadata := profileMapDNGFields(order)
			edit(metadata)
			if _, err := decodeLinearDNG(linearDNGFixture(order, 1, 1, 3, []uint16{16384, 0, 0}, metadata)); err == nil {
				t.Fatal("malformed profile table accepted")
			}
		})
	}
}

func TestLinearDNGProfileMapPipelineOrder(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		img, err := decodeLinearDNG(profileMapDNGFixture(order))
		if err != nil {
			t.Fatal(err)
		}
		// .25 red -> HueSat: .3125 yellow -> +1 EV: .625 yellow ->
		// Look: .3125 green -> natural tone: .108642578125 green.
		linear := .108642578125 * 1.2317689013144165
		want := math.Round(65535 * (1.055*math.Pow(linear, 1/2.4) - .055))
		p := img.NRGBA64At(4, 4)
		if p.R != 0 || math.Abs(float64(p.G)-want) > 1 || p.B != 0 {
			t.Fatalf("profile map order: %v want green %g", p, want)
		}
	}
}

func TestLinearDNGProfileMapRootInheritance(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		metadata := profileMapDNGFields(order)
		rootFields := []struct {
			tag   uint16
			field dngTestField
		}{{262, dngTestShort(order, 2)}, {330, dngTestLong(order, 8)}, {50706, dngTestField{1, 4, []byte{1, 4, 0, 0}}}}
		for _, tag := range []uint16{50937, 50938, 51107} {
			rootFields = append(rootFields, struct {
				tag   uint16
				field dngTestField
			}{tag, metadata[tag]})
			delete(metadata, tag)
		}
		pixels := []uint16{16384, 0, 0}
		data := linearDNGFixture(order, 1, 1, 3, pixels, metadata)
		root := len(data)
		data = append(data, make([]byte, 2+len(rootFields)*12+4)...)
		order.PutUint32(data[4:], uint32(root))
		order.PutUint16(data[root:], uint16(len(rootFields)))
		for i, e := range rootFields {
			offset := root + 2 + 12*i
			order.PutUint16(data[offset:], e.tag)
			order.PutUint16(data[offset+2:], e.field.typeID)
			order.PutUint32(data[offset+4:], e.field.count)
			if len(e.field.data) <= 4 {
				copy(data[offset+8:offset+12], e.field.data)
			} else {
				order.PutUint32(data[offset+8:], uint32(len(data)))
				data = append(data, e.field.data...)
			}
		}
		got, err := decodeLinearDNG(data)
		if err != nil {
			t.Fatal(err)
		}
		want, err := decodeLinearDNG(linearDNGFixture(order, 1, 1, 3, pixels, profileMapDNGFields(order)))
		if err != nil || !bytes.Equal(got.Pix, want.Pix) {
			t.Fatalf("root profile map inheritance (%s): %v", order, err)
		}
	}
}

func TestRAWProfileMapCLIThroughTIFFAndVideo(t *testing.T) {
	executable, fixtureDir, _ := fakeDNGConverter(t)
	fixturePath := filepath.Join(fixtureDir, "profile.dng")
	fixture := profileMapDNGFixture(binary.LittleEndian)
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
	linear := .108642578125 * 1.2317689013144165 * 2
	want := math.Round(65535 * (1.055*math.Pow(linear, 1/2.4) - .055))
	r, g, b, _ := img.At(4, 4).RGBA()
	if r != 0 || math.Abs(float64(g)-want) > 2 || b != 0 {
		t.Fatalf("profile map + XMP output %d,%d,%d want green %g", r, g, b, want)
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
	r, g, b, _ = frame.At(4, 4).RGBA()
	if r>>8 > 2 || math.Abs(float64(g>>8)-want/257) > 2 || b>>8 > 2 {
		t.Fatalf("profile map AVI %d,%d,%d", r, g, b)
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("profile maps changed RAW or XMP")
	}
	metadata := profileMapDNGFields(binary.LittleEndian)
	metadata[51107] = dngTestLong(binary.LittleEndian, 2)
	if err := os.WriteFile(fixturePath, linearDNGFixture(binary.LittleEndian, 1, 1, 3, []uint16{16384, 0, 0}, metadata), 0600); err != nil {
		t.Fatal(err)
	}
	parent := t.TempDir()
	if Main([]string{"export", dir, "--output", filepath.Join(parent, "failed"), "--raw-develop", "--dng-converter", executable}, &stdout, &stderr) == 0 {
		t.Fatal("unsupported encoding export succeeded")
	}
	entries, err := os.ReadDir(parent)
	if err != nil || len(entries) != 0 {
		t.Fatalf("failed map export left files: %v %v", entries, err)
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("failed map export changed sources")
	}
}

func FuzzDNGProfileMap(f *testing.F) {
	for _, selector := range []uint8{0, 1, 27} {
		h, s, v := 1+int(selector%4), 2+int(selector/4%3), 1+int(selector/12%3)
		metadata := testDNGProfileMapFields(binary.LittleEndian, 50937, 50938, 51107, h, s, v, false, func(_, _, _ int) [3]float32 { return [3]float32{60, .5, .75} })
		f.Add(metadata[50938].data, selector)
	}
	f.Fuzz(func(t *testing.T, data []byte, selector uint8) {
		if len(data) > 4096 || len(data)%4 != 0 {
			return
		}
		h, s, v := 1+int(selector%4), 2+int(selector/4%3), 1+int(selector/12%3)
		metadata := map[uint16]dngTestField{50937: dngTestLong(binary.LittleEndian, uint32(h), uint32(s), uint32(v)), 50938: {11, uint32(len(data) / 4), data}, 51107: dngTestLong(binary.LittleEndian, uint32(selector>>7))}
		fields, err := findLinearDNG(linearDNGFixture(binary.LittleEndian, 1, 1, 1, []uint16{0}, metadata))
		if err != nil {
			return
		}
		m, err := readDNGProfileMap(fields, 50937, 50938, 51107)
		if err != nil {
			return
		}
		for i := 0; i <= 16; i++ {
			x := float64(i) / 16
			for _, rgb := range [][3]float64{{x, .3, .7}, {.7, x, .3}, {.3, .7, x}, {x, x, x}, {x, 0, 0}, {0, 0, x}} {
				for _, got := range m.apply(rgb) {
					if math.IsNaN(got) || got < 0 || got > 1 {
						t.Fatal("invalid profile map output", got)
					}
				}
			}
		}
	})
}
