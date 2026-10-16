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

func dngTestSRational(order binary.ByteOrder, numerator, denominator int32) dngTestField {
	data := make([]byte, 8)
	order.PutUint32(data, uint32(numerator))
	order.PutUint32(data[4:], uint32(denominator))
	return dngTestField{10, 1, data}
}

func TestDNGBaselineExposureOffsetRationals(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		for base := -4; base <= 4; base++ {
			for numerator := -9; numerator <= 9; numerator++ {
				for _, denominator := range []int32{3, -7} {
					metadata := map[uint16]dngTestField{50730: dngTestDouble(order, float64(base)/2), 51109: dngTestSRational(order, int32(numerator), denominator)}
					fields, err := findLinearDNG(linearDNGFixture(order, 1, 1, 1, []uint16{4096}, metadata))
					if err != nil {
						t.Fatal(err)
					}
					got, err := readDNGBaselineExposure(fields)
					want := float64(base)/2 + float64(numerator)/float64(denominator)
					if err != nil || got != want {
						t.Fatalf("exposure sum %g want %g: %v", got, want, err)
					}
					img, err := decodeLinearDNG(linearDNGFixture(order, 1, 1, 1, []uint16{4096}, metadata))
					if err != nil {
						t.Fatal(err)
					}
					linear := min(1, (4096.0/65535)*math.Exp2(want))
					encoded := 12.92 * linear
					if linear > .0031308 {
						encoded = 1.055*math.Pow(linear, 1/2.4) - .055
					}
					p := img.NRGBA64At(0, 0)
					if p.R != uint16(math.Round(encoded*65535)) || p.G != p.R || p.B != p.R {
						t.Fatalf("exposure pixel %v for EV %g", p, want)
					}
				}
			}
		}
	}
}

func TestLinearDNGProfileExposureOrderAndCancellation(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		// Different combinations with equal total EV must produce identical
		// images through the value-dependent Hue/Sat map, Look, and tone curve.
		var reference []byte
		for _, c := range []struct {
			baseline               float64
			numerator, denominator int32
		}{{1, 0, 1}, {0, 1, 1}, {2, -1, 1}, {1.5, -1, 2}, {-2, 3, 1}} {
			metadata := profileMapDNGFields(order)
			metadata[50730] = dngTestDouble(order, c.baseline)
			metadata[51109] = dngTestSRational(order, c.numerator, c.denominator)
			img, err := decodeLinearDNG(linearDNGFixture(order, 1, 1, 3, []uint16{16384, 0, 0}, metadata))
			if err != nil {
				t.Fatal(err)
			}
			if reference == nil {
				reference = append([]byte(nil), img.Pix...)
			} else if !bytes.Equal(img.Pix, reference) {
				t.Fatalf("offset applied at a different stage: %+v", c)
			}
		}
		pixels := make([]uint16, 65536)
		for i := range pixels {
			pixels[i] = uint16(i)
		}
		plain, err := decodeLinearDNG(linearDNGFixture(order, 256, 256, 1, pixels, nil))
		if err != nil {
			t.Fatal(err)
		}
		for _, base := range []int32{-16, -1, 0, 1, 16} {
			img, err := decodeLinearDNG(linearDNGFixture(order, 256, 256, 1, pixels, map[uint16]dngTestField{50730: dngTestDouble(order, float64(base)), 51109: dngTestSRational(order, -base, 1)}))
			if err != nil || !bytes.Equal(img.Pix, plain.Pix) {
				t.Fatalf("exposure cancellation changed samples: %d: %v", base, err)
			}
		}
	}
}

func TestLinearDNGProfileExposureMalformed(t *testing.T) {
	order := binary.LittleEndian
	for name, metadata := range map[string]map[uint16]dngTestField{
		"zero-denominator":    {51109: dngTestSRational(order, 1, 0)},
		"wrong-type":          {51109: dngTestDouble(order, .5)},
		"wrong-count":         {51109: {10, 2, make([]byte, 16)}},
		"empty":               {51109: {10, 0, nil}},
		"too-large":           {51109: dngTestSRational(order, 17, 1)},
		"too-negative":        {51109: dngTestSRational(order, -17, 1)},
		"total-outside-range": {50730: dngTestDouble(order, 16), 51109: dngTestSRational(order, 1, 1)},
		"bad-baseline":        {50730: dngTestDouble(order, math.NaN()), 51109: dngTestSRational(order, 1, 1)},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeLinearDNG(linearDNGFixture(order, 1, 1, 1, []uint16{4096}, metadata)); err == nil {
				t.Fatal("invalid profile exposure accepted")
			}
		})
	}
}

func TestLinearDNGProfileExposureRootInheritance(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		for _, override := range []bool{false, true} {
			child := map[uint16]dngTestField{}
			if override {
				child[51109] = dngTestSRational(order, -1, 2)
			}
			data := linearDNGFixture(order, 1, 1, 1, []uint16{8192}, child)
			root := len(data)
			data = append(data, make([]byte, 2+5*12+4)...)
			order.PutUint32(data[4:], uint32(root))
			order.PutUint16(data[root:], 5)
			for i, entry := range []struct {
				tag   uint16
				field dngTestField
			}{{262, dngTestShort(order, 2)}, {330, dngTestLong(order, 8)}, {50706, dngTestField{1, 4, []byte{1, 4, 0, 0}}}, {50730, dngTestSRational(order, -1, 2)}, {51109, dngTestSRational(order, 3, 2)}} {
				offset := root + 2 + 12*i
				order.PutUint16(data[offset:], entry.tag)
				order.PutUint16(data[offset+2:], entry.field.typeID)
				order.PutUint32(data[offset+4:], entry.field.count)
				if len(entry.field.data) <= 4 {
					copy(data[offset+8:offset+12], entry.field.data)
				} else {
					order.PutUint32(data[offset+8:], uint32(len(data)))
					data = append(data, entry.field.data...)
				}
			}
			got, err := decodeLinearDNG(data)
			if err != nil {
				t.Fatal(err)
			}
			exposure := 1.0
			if override {
				exposure = -1
			}
			want, err := decodeLinearDNG(linearDNGFixture(order, 1, 1, 1, []uint16{8192}, map[uint16]dngTestField{50730: dngTestDouble(order, exposure)}))
			if err != nil || !bytes.Equal(got.Pix, want.Pix) {
				t.Fatalf("profile exposure inheritance override=%t (%s): %v", override, order, err)
			}
		}
	}
}

func TestRAWProfileExposureCLIThroughTIFFAndVideo(t *testing.T) {
	executable, fixtureDir, _ := fakeDNGConverter(t)
	fixturePath := filepath.Join(fixtureDir, "exposure.dng")
	metadata := map[uint16]dngTestField{50717: dngTestLong(binary.LittleEndian, 65536), 50730: dngTestDouble(binary.LittleEndian, -.5), 51109: dngTestSRational(binary.LittleEndian, 3, 2)}
	pixels := make([]uint16, 64)
	for i := range pixels {
		pixels[i] = 8192
	}
	fixture := linearDNGFixture(binary.LittleEndian, 8, 8, 1, pixels, metadata)
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
	// Sensor .125 -> total baseline +1 EV -> .25 -> XMP +1 EV -> .5.
	want := math.Round(65535 * (1.055*math.Pow(.5, 1/2.4) - .055))
	r, g, b, _ := img.At(4, 4).RGBA()
	if math.Abs(float64(r)-want) > 2 || r != g || r != b {
		t.Fatalf("profile exposure + XMP %d,%d,%d want %g", r, g, b, want)
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
		t.Fatal("profile exposure did not reach AVI")
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("profile exposure changed sources")
	}
	metadata[51109] = dngTestSRational(binary.LittleEndian, 1, 0)
	if err := os.WriteFile(fixturePath, linearDNGFixture(binary.LittleEndian, 8, 8, 1, pixels, metadata), 0600); err != nil {
		t.Fatal(err)
	}
	parent := t.TempDir()
	if Main([]string{"export", dir, "--output", filepath.Join(parent, "failed"), "--raw-develop", "--dng-converter", executable}, &stdout, &stderr) == 0 {
		t.Fatal("invalid profile exposure export succeeded")
	}
	entries, err := os.ReadDir(parent)
	if err != nil || len(entries) != 0 || !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatalf("failed exposure export left files or changed sources: %v %v", entries, err)
	}
}

func FuzzDNGBaselineExposureOffset(f *testing.F) {
	f.Add(int32(3), int32(2), int8(-4), false)
	f.Add(int32(-1), int32(0), int8(0), true)
	f.Add(int32(math.MinInt32), int32(-1), int8(127), false)
	f.Fuzz(func(t *testing.T, numerator, denominator int32, base int8, bigEndian bool) {
		var order binary.ByteOrder = binary.LittleEndian
		if bigEndian {
			order = binary.BigEndian
		}
		baseline := float64(base) / 8
		fields, err := findLinearDNG(linearDNGFixture(order, 1, 1, 1, []uint16{8192}, map[uint16]dngTestField{50730: dngTestDouble(order, baseline), 51109: dngTestSRational(order, numerator, denominator)}))
		if err != nil {
			t.Fatal(err)
		}
		got, err := readDNGBaselineExposure(fields)
		offset := float64(numerator) / float64(denominator)
		want := baseline + offset
		if denominator == 0 || math.Abs(offset) > 16 || math.Abs(want) > 16 {
			if err == nil {
				t.Fatal("invalid exposure accepted")
			}
		} else if err != nil || got != want {
			t.Fatalf("exposure offset: got %g want %g: %v", got, want, err)
		}
	})
}
