package lrtl

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/jpeg"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

type testDNGOpcode struct {
	id, version, flags uint32
	payload            []byte
}

func testDNGOpcodeList(opcodes ...testDNGOpcode) dngTestField {
	data := binary.BigEndian.AppendUint32(nil, uint32(len(opcodes)))
	for _, opcode := range opcodes {
		for _, v := range []uint32{opcode.id, opcode.version, opcode.flags, uint32(len(opcode.payload))} {
			data = binary.BigEndian.AppendUint32(data, v)
		}
		data = append(data, opcode.payload...)
	}
	return dngTestField{7, uint32(len(data)), data}
}

func testDNGGainMapPayload(width, height, rows, cols, planes int, values ...float32) []byte {
	var data []byte
	for _, v := range []uint32{0, 0, uint32(height), uint32(width), 0, 1, 1, 1, uint32(rows), uint32(cols)} {
		data = binary.BigEndian.AppendUint32(data, v)
	}
	for _, v := range []float64{1, 1, 0, 0} {
		data = binary.BigEndian.AppendUint64(data, math.Float64bits(v))
	}
	data = binary.BigEndian.AppendUint32(data, uint32(planes))
	for _, v := range values {
		data = binary.BigEndian.AppendUint32(data, math.Float32bits(v))
	}
	return data
}

func testDNGGainMap(payload []byte) testDNGOpcode { return testDNGOpcode{9, 0x01030000, 0, payload} }

func TestLinearDNGGainMapActiveCoordinatesAndCrop(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		pixels := make([]uint16, 8*8)
		for i := range pixels {
			pixels[i] = 4096
		}
		payload := testDNGGainMapPayload(4, 4, 2, 2, 1, 1, 2, 3, 4)
		data := linearDNGFixture(order, 8, 8, 1, pixels, map[uint16]dngTestField{50829: dngTestLong(order, 2, 1, 6, 5), 50719: dngTestLong(order, 1, 1), 50720: dngTestLong(order, 2, 2), 50717: dngTestLong(order, 16384), 51009: testDNGOpcodeList(testDNGGainMap(payload))})
		img, err := decodeLinearDNG(data)
		if err != nil {
			t.Fatal(err)
		}
		if img.Bounds() != image.Rect(0, 0, 2, 2) {
			t.Fatal(img.Bounds())
		}
		for y := 0; y < 2; y++ {
			for x := 0; x < 2; x++ {
				// The map 1,2 / 3,4 is the plane 1+u+2v, with centers
				// measured in the full 4x4 active area before the 2x2 crop.
				gain := 1 + (float64(x)+1.5)/4 + 2*(float64(y)+1.5)/4
				want := math.Round(65535 * (1.055*math.Pow(.25*gain, 1/2.4) - .055))
				p := img.NRGBA64At(x, y)
				if float64(p.R) != want || p.R != p.G || p.G != p.B {
					t.Fatalf("%s (%d,%d)=%v want %g", order, x, y, p, want)
				}
			}
		}
	}
}

func TestDNGGainMapEdgesPlaneSelectionAndPitch(t *testing.T) {
	payload := testDNGGainMapPayload(8, 8, 2, 2, 1, 1, 2, 3, 4)
	for _, offset := range []int{40, 48, 56, 64} {
		binary.BigEndian.PutUint64(payload[offset:], math.Float64bits(.25))
	}
	gainMap, err := parseDNGGainMap(payload)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		x, y int
		gain float64
	}{{0, 0, 1}, {7, 0, 2}, {0, 7, 3}, {7, 7, 4}, {3, 3, 3.25}} {
		if got := gainMap.gain(c.x, c.y, 0, 8, 8); got != c.gain {
			t.Fatalf("edge gain %d,%d=%g want %g", c.x, c.y, got, c.gain)
		}
	}
	for _, mapPlanes := range []int{1, 3} {
		values := []float32{2}
		if mapPlanes == 3 {
			values = []float32{2, 3, 4}
		}
		payload = testDNGGainMapPayload(8, 7, 1, 1, mapPlanes, values...)
		for offset, v := range map[int]uint32{0: 1, 4: 1, 16: 1, 20: 2, 24: 2, 28: 3} {
			binary.BigEndian.PutUint32(payload[offset:], v)
		}
		// Constant axes ignore their raw geometry, as the SDK does.
		for _, offset := range []int{40, 48, 56, 64} {
			binary.BigEndian.PutUint64(payload[offset:], math.Float64bits(math.NaN()))
		}
		gainMap, err = parseDNGGainMap(payload)
		if err != nil {
			t.Fatal(err)
		}
		for y := 0; y < 8; y++ {
			for x := 0; x < 8; x++ {
				camera := [3]float64{.1, .1, .1}
				applyDNGGainMaps([]dngGainMap{gainMap}, &camera, x, y, 3, 8, 8)
				for plane := 0; plane < 3; plane++ {
					want := .1
					if x >= 1 && x < 8 && y >= 1 && y < 7 && (x-1)%3 == 0 && (y-1)%2 == 0 && plane >= 1 {
						gain := 2.0
						if mapPlanes == 3 {
							gain = float64(plane + 2)
						}
						want *= gain
					}
					if math.Abs(camera[plane]-want) > 1e-12 {
						t.Fatalf("planes=%d (%d,%d) plane %d=%g want %g", mapPlanes, x, y, plane, camera[plane], want)
					}
				}
			}
		}
	}
}

func TestDNGGainMapListOrderClippingAndOptionalFlags(t *testing.T) {
	order := binary.LittleEndian
	first := testDNGGainMap(testDNGGainMapPayload(1, 1, 1, 1, 1, 2))
	second := testDNGGainMap(testDNGGainMapPayload(1, 1, 1, 1, 1, .25))
	for _, flags := range []uint32{0, 1, 2, 3} {
		first.flags = flags
		data := linearDNGFixture(order, 1, 1, 1, []uint16{49152}, map[uint16]dngTestField{51009: testDNGOpcodeList(first), 51022: testDNGOpcodeList(second), 50717: dngTestLong(order, 65536)})
		img, err := decodeLinearDNG(data)
		if err != nil {
			t.Fatal(err)
		}
		want := uint16(math.Round(65535 * (1.055*math.Pow(.25, 1/2.4) - .055)))
		if img.NRGBA64At(0, 0).R != want {
			t.Fatal("gain maps must clip between lists and apply supported optional/preview-only flags")
		}
	}
	data := linearDNGFixture(order, 1, 1, 1, []uint16{49152}, map[uint16]dngTestField{51022: testDNGOpcodeList(first, second), 50717: dngTestLong(order, 65536)})
	img, err := decodeLinearDNG(data)
	if err != nil {
		t.Fatal(err)
	}
	if img.NRGBA64At(0, 0).R != 35199 {
		t.Fatal("same-list opcode order/clipping changed")
	}
	for _, c := range []struct {
		tag    uint16
		opcode testDNGOpcode
		skip   bool
	}{
		{51022, testDNGOpcode{999, 0x01030000, 1, []byte{1, 2, 3}}, true},
		{51022, testDNGOpcode{999, 0x01030000, 0, nil}, false},
		{51022, testDNGOpcode{999, 0x01030000, 2, nil}, false},
		{51008, testDNGGainMap(testDNGGainMapPayload(1, 1, 1, 1, 1, 2)), false},
		{51008, testDNGOpcode{9, 0x01030000, 1, testDNGGainMapPayload(1, 1, 1, 1, 1, 2)}, true},
		{51022, testDNGOpcode{9, 0x01080000, 1, nil}, true},
		{51022, testDNGOpcode{9, 0x01080000, 0, nil}, false},
		{51022, testDNGOpcode{9, 0x01030000, 1, nil}, false},
	} {
		data := linearDNGFixture(order, 1, 1, 1, []uint16{16384}, map[uint16]dngTestField{c.tag: testDNGOpcodeList(c.opcode)})
		img, err := decodeLinearDNG(data)
		if !c.skip {
			if err == nil {
				t.Fatalf("unsupported required opcode accepted: %+v", c)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		plain, err := decodeLinearDNG(linearDNGFixture(order, 1, 1, 1, []uint16{16384}, nil))
		if err != nil || img.NRGBA64At(0, 0) != plain.NRGBA64At(0, 0) {
			t.Fatal("optional unsupported opcode affected pixels")
		}
	}
}

func TestDNGGainMapRejectsMalformedData(t *testing.T) {
	payload := testDNGGainMapPayload(4, 4, 2, 2, 1, 1, 2, 3, 4)
	for name, mutate := range map[string]func([]byte) []byte{
		"truncated": func(p []byte) []byte { return p[:70] }, "missing-sample": func(p []byte) []byte { return p[:len(p)-4] }, "trailing": func(p []byte) []byte { return append(p, 0) },
		"zero-row-pitch": func(p []byte) []byte { binary.BigEndian.PutUint32(p[24:], 0); return p }, "empty-area": func(p []byte) []byte { binary.BigEndian.PutUint32(p[8:], 0); return p },
		"huge-grid": func(p []byte) []byte { binary.BigEndian.PutUint32(p[32:], 0xffffffff); return p }, "zero-planes": func(p []byte) []byte { binary.BigEndian.PutUint32(p[72:], 0); return p },
		"nan-spacing": func(p []byte) []byte { binary.BigEndian.PutUint64(p[40:], math.Float64bits(math.NaN())); return p }, "underflow-spacing": func(p []byte) []byte { binary.BigEndian.PutUint64(p[48:], math.Float64bits(1e-300)); return p },
		"negative-spacing": func(p []byte) []byte { binary.BigEndian.PutUint64(p[40:], math.Float64bits(-1)); return p }, "nan-origin": func(p []byte) []byte { binary.BigEndian.PutUint64(p[56:], math.Float64bits(math.NaN())); return p },
		"nan-sample": func(p []byte) []byte {
			binary.BigEndian.PutUint32(p[76:], math.Float32bits(float32(math.NaN())))
			return p
		}, "infinite-sample": func(p []byte) []byte {
			binary.BigEndian.PutUint32(p[76:], math.Float32bits(float32(math.Inf(1))))
			return p
		},
	} {
		t.Run(name, func(t *testing.T) {
			p := mutate(append([]byte(nil), payload...))
			if _, err := parseDNGGainMap(p); err == nil {
				t.Fatal("malformed GainMap accepted")
			}
		})
	}
	valid := testDNGOpcodeList(testDNGGainMap(payload))
	badSize := append([]byte(nil), valid.data...)
	binary.BigEndian.PutUint32(badSize[16:], 0xffffffff)
	for name, field := range map[string]dngTestField{
		"bad-type": {3, 2, []byte{0, 0, 0, 0}}, "short-count": {7, 2, []byte{0, 0}}, "large-tag": {7, maxDNGOpcodeBytes + 1, []byte{0, 0, 0, 0}},
		"short-header": {7, 5, []byte{0, 0, 0, 1, 0}}, "huge-count": {7, 4, []byte{255, 255, 255, 255}},
		"trailing-list":     {7, valid.count + 1, append(append([]byte(nil), valid.data...), 0)},
		"oversized-payload": {7, uint32(len(badSize)), badSize},
	} {
		t.Run(name, func(t *testing.T) {
			data := linearDNGFixture(binary.LittleEndian, 1, 1, 1, []uint16{0}, map[uint16]dngTestField{51022: field})
			if _, err := decodeLinearDNG(data); err == nil {
				t.Fatal("malformed opcode list accepted")
			}
		})
	}
	if _, err := parseDNGGainMap(testDNGGainMapPayload(1, 1, 1, 1, 1, -2)); err != nil {
		t.Fatal("finite negative gain is allowed and clipped after application")
	}
}

func TestRAWRequiredOpcodeFailureDoesNotPublishExport(t *testing.T) {
	executable, fixtureDir, _ := fakeDNGConverter(t)
	fixture := linearDNGFixture(binary.LittleEndian, 16, 16, 1, make([]uint16, 256), map[uint16]dngTestField{51022: testDNGOpcodeList(testDNGOpcode{999, 0x01030000, 0, nil})})
	fixturePath := filepath.Join(fixtureDir, "required.dng")
	if err := os.WriteFile(fixturePath, fixture, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LRTL_TEST_CONVERTER_FIXTURE", fixturePath)
	dir, parent := t.TempDir(), t.TempDir()
	writeRenderFrame(t, filepath.Join(dir, "frame1.png"), "png", 16, 16)
	if err := os.WriteFile(filepath.Join(dir, "frame2.nef"), fixture, 0600); err != nil {
		t.Fatal(err)
	}
	before := batchFileSnapshot(t, dir)
	var stdout, stderr bytes.Buffer
	if Main([]string{"export", dir, "--output", filepath.Join(parent, "export"), "--raw-develop", "--dng-converter", executable}, &stdout, &stderr) == 0 {
		t.Fatal("required unsupported opcode was ignored")
	}
	entries, err := os.ReadDir(parent)
	if err != nil || len(entries) != 0 {
		t.Fatalf("partial or temporary export retained: %v %v", entries, err)
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("failed RAW export modified source")
	}
}

func TestDNGOpcodeListsBelongToSelectedIFD(t *testing.T) {
	order := binary.LittleEndian
	data := linearDNGFixture(order, 1, 1, 1, []uint16{16384}, nil)
	root := len(data)
	data = append(data, make([]byte, 2+4*12+4)...)
	order.PutUint32(data[4:], uint32(root))
	order.PutUint16(data[root:], 4)
	opcodes := testDNGOpcodeList(testDNGOpcode{999, 0x01030000, 0, nil})
	for i, entry := range []struct {
		tag, kind    uint16
		count, value uint32
	}{{262, 3, 1, 2}, {330, 4, 1, 8}, {50706, 1, 4, 0x00000401}, {51022, 7, opcodes.count, uint32(len(data))}} {
		offset := root + 2 + i*12
		order.PutUint16(data[offset:], entry.tag)
		order.PutUint16(data[offset+2:], entry.kind)
		order.PutUint32(data[offset+4:], entry.count)
		order.PutUint32(data[offset+8:], entry.value)
	}
	data = append(data, opcodes.data...)
	if _, err := decodeLinearDNG(data); err != nil {
		t.Fatalf("root preview opcode leaked into raw SubIFD: %v", err)
	}
}

func TestRAWGainMapCLIThroughTIFFAndVideo(t *testing.T) {
	executable, fixtureDir, _ := fakeDNGConverter(t)
	pixels := make([]uint16, 16*16)
	for i := range pixels {
		pixels[i] = 4096
	}
	fixture := linearDNGFixture(binary.LittleEndian, 16, 16, 1, pixels, map[uint16]dngTestField{50717: dngTestLong(binary.LittleEndian, 16384), 51022: testDNGOpcodeList(testDNGGainMap(testDNGGainMapPayload(16, 16, 2, 2, 1, 1, 2, 3, 4)))})
	fixturePath := filepath.Join(fixtureDir, "gainmap.dng")
	if err := os.WriteFile(fixturePath, fixture, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LRTL_TEST_CONVERTER_FIXTURE", fixturePath)
	dir := t.TempDir()
	for i := 0; i < 2; i++ {
		path := filepath.Join(dir, fmt.Sprintf("frame%d.nef", i))
		if err := os.WriteFile(path, fixture, 0600); err != nil {
			t.Fatal(err)
		}
		writeTestXMP(t, path, map[string]string{"crs:Exposure2012": fmt.Sprint(float64(i) / 2)})
	}
	before := batchFileSnapshot(t, dir)
	output := filepath.Join(t.TempDir(), "export")
	var stdout, stderr bytes.Buffer
	if Main([]string{"export", dir, "--output", output, "--raw-develop", "--dng-converter", executable, "--apply-xmp-exposure"}, &stdout, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	var expected []float64
	for i := 0; i < 2; i++ {
		img := readExportedTIFF(t, filepath.Join(output, fmt.Sprintf("frame%06d.tif", i+1)))
		for _, point := range []image.Point{{0, 0}, {8, 8}, {15, 15}} {
			gain := 1 + (float64(point.X)+.5)/16 + 2*(float64(point.Y)+.5)/16
			encoded := math.Round(65535*(1.055*math.Pow(.25*gain, 1/2.4)-.055)) / 65535
			linear := math.Pow((encoded+.055)/1.055, 2.4) * math.Exp2(float64(i)/2)
			linear = math.Min(1, linear)
			want := math.Round(65535 * (1.055*math.Pow(linear, 1/2.4) - .055))
			r, g, b, _ := img.At(point.X, point.Y).RGBA()
			if math.Abs(float64(r)-want) > 1 || r != g || g != b {
				t.Fatalf("frame %d point %v got %d want %g", i, point, r, want)
			}
			if point == (image.Point{8, 8}) {
				expected = append(expected, want/257)
			}
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
	count := 0
	for {
		start := bytes.Index(data, []byte{255, 216})
		if start < 0 {
			break
		}
		data = data[start:]
		end := bytes.Index(data, []byte{255, 217})
		if end < 0 || count >= len(expected) {
			t.Fatal("bad AVI frame count")
		}
		img, err := jpeg.Decode(bytes.NewReader(data[:end+2]))
		if err != nil {
			t.Fatal(err)
		}
		r, _, _, _ := img.At(8, 8).RGBA()
		if math.Abs(float64(r>>8)-expected[count]) > 2 {
			t.Fatal("AVI differs from gain map + XMP correction")
		}
		count++
		data = data[end+2:]
	}
	if count != 2 {
		t.Fatal("missing AVI frames")
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("source image or XMP changed")
	}
}
