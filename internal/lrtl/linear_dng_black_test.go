package lrtl

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/jpeg"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDNGBlackModelAgainstBruteForce(t *testing.T) {
	random := rand.New(rand.NewSource(1941))
	for trial := 0; trial < 200; trial++ {
		channels := 1
		if trial%2 != 0 {
			channels = 3
		}
		rows, cols := 1+random.Intn(5), 1+random.Intn(5)
		active := image.Rect(3, 4, 4+random.Intn(10), 5+random.Intn(9))
		black := make([]float64, rows*cols*channels)
		for i := range black {
			black[i] = float64(random.Intn(61)-10) / 4
		}
		horizontal, vertical := make([]float64, active.Dx()), make([]float64, active.Dy())
		for i := range horizontal {
			horizontal[i] = float64(random.Intn(41)-20) / 4
		}
		for i := range vertical {
			vertical[i] = float64(random.Intn(41)-20) / 4
		}
		white := make([]float64, channels)
		for i := range white {
			white[i] = float64(200 + i*100)
		}
		order := binary.ByteOrder(binary.LittleEndian)
		if trial%3 == 0 {
			order = binary.BigEndian
		}
		data := linearDNGFixture(order, 16, 16, channels, make([]uint16, 16*16*channels), map[uint16]dngTestField{50713: dngTestShort(order, uint16(rows), uint16(cols)), 50714: dngTestDouble(order, black...), 50715: dngTestDouble(order, horizontal...), 50716: dngTestDouble(order, vertical...), 50717: dngTestDouble(order, white...)})
		fields, err := findLinearDNG(data)
		if err != nil {
			t.Fatal(err)
		}
		model, err := newDNGBlackModel(fields, active, channels, 16)
		if err != nil {
			t.Fatal(err)
		}
		for channel := 0; channel < channels; channel++ {
			maximum := math.Inf(-1)
			// Scan the full active region, plus each declared pattern phase.
			// A phase absent from the active dimensions has zero delta in SDK.
			for y := 0; y < max(active.Dy(), rows); y++ {
				for x := 0; x < max(active.Dx(), cols); x++ {
					h, v := 0.0, 0.0
					if x < len(horizontal) {
						h = horizontal[x]
					}
					if y < len(vertical) {
						v = vertical[y]
					}
					value := black[((y%rows)*cols+x%cols)*channels+channel] + h + v
					if value > maximum {
						maximum = value
					}
				}
			}
			denominator := white[channel] - maximum
			if model.denominators[channel] != denominator {
				t.Fatalf("trial %d plane %d denominator=%g want %g", trial, channel, model.denominators[channel], denominator)
			}
			for y := 0; y < active.Dy(); y++ {
				for x := 0; x < active.Dx(); x++ {
					for _, raw := range []float64{-1, 0, 10, 100, 1000} {
						blackAt := black[((y%rows)*cols+x%cols)*channels+channel] + horizontal[x] + vertical[y]
						want := (raw - blackAt) / denominator
						if want < 0 {
							want = 0
						}
						if want > 1 {
							want = 1
						}
						if got := model.normalize(raw, x+active.Min.X, y+active.Min.Y, channel); got != want {
							t.Fatalf("trial %d pixel %d,%d plane %d normalized=%g want %g", trial, x, y, channel, got, want)
						}
					}
				}
			}
		}
	}
}

func blackPatternDNGFixture(t testing.TB, order binary.ByteOrder) []byte {
	t.Helper()
	// ActiveArea starts off both pattern phases. Its full maximum is 199,
	// outside the smaller default crop. Every visible raw sample is black+200.
	black := []float64{10, 30, 50, 70, 90, 110}
	horizontal := []float64{0, 5, 10, 15, 20, 25, 30, 35, 40, 45}
	vertical := []float64{0, 7, 14, 21, 28, 35, 42, 49, 56}
	pixels := make([]uint16, 16*16)
	for i := range pixels {
		pixels[i] = 65535
	}
	for y := 0; y < 9; y++ {
		for x := 0; x < 10; x++ {
			pixels[(y+5)*16+x+3] = uint16(black[(y%2)*3+x%3] + horizontal[x] + vertical[y] + 200)
		}
	}
	return linearDNGFixture(order, 16, 16, 1, pixels, map[uint16]dngTestField{50829: dngTestLong(order, 5, 3, 14, 13), 50713: dngTestShort(order, 2, 3), 50714: dngTestDouble(order, black...), 50715: dngTestDouble(order, horizontal...), 50716: dngTestDouble(order, vertical...), 50717: dngTestLong(order, 1000), 50719: dngTestLong(order, 1, 2), 50720: dngTestLong(order, 8, 6)})
}

func TestLinearDNGActiveAreaBlackPatternAndCrop(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		img, err := decodeLinearDNG(blackPatternDNGFixture(t, order))
		if err != nil {
			t.Fatal(err)
		}
		if img.Bounds() != image.Rect(0, 0, 8, 6) {
			t.Fatal(img.Bounds())
		}
		want := uint16(math.Round(65535 * (1.055*math.Pow(200.0/801, 1/2.4) - .055)))
		for y := 0; y < 6; y++ {
			for x := 0; x < 8; x++ {
				p := img.NRGBA64At(x, y)
				if p.R != want || p.G != want || p.B != want || p.A != 65535 {
					t.Fatalf("%s (%d,%d)=%v want %d", order, x, y, p, want)
				}
			}
		}
	}
}

func TestLinearDNGLUTEndpointAndEarlyClipping(t *testing.T) {
	order := binary.LittleEndian
	data := linearDNGFixture(order, 5, 1, 1, []uint16{0, 1, 2, 100, 65535}, map[uint16]dngTestField{50712: dngTestShort(order, 100, 200, 600), 50714: dngTestLong(order, 100), 50717: dngTestLong(order, 1100)})
	img, err := decodeLinearDNG(data)
	if err != nil {
		t.Fatal(err)
	}
	for x, linear := range []float64{0, .1, .5, .5, .5} {
		want := uint16(0)
		if linear > 0 {
			want = uint16(math.Round(65535 * (1.055*math.Pow(linear, 1/2.4) - .055)))
		}
		if img.NRGBA64At(x, 0).R != want {
			t.Fatalf("LUT endpoint x=%d", x)
		}
	}
	data = linearDNGFixture(order, 3, 1, 1, []uint16{500, 1500, 2500}, map[uint16]dngTestField{50714: dngTestLong(order, 100), 50717: dngTestLong(order, 1000), 50730: dngTestDouble(order, -1)})
	img, err = decodeLinearDNG(data)
	if err != nil {
		t.Fatal(err)
	}
	for x, linear := range []float64{.5 * 400 / 900, .5, .5} {
		want := uint16(math.Round(65535 * (1.055*math.Pow(linear, 1/2.4) - .055)))
		if img.NRGBA64At(x, 0).R != want {
			t.Fatalf("sensor clipping must precede baseline exposure x=%d", x)
		}
	}
}

func TestLinearDNGRejectsInvalidBlackModels(t *testing.T) {
	order := binary.LittleEndian
	for name, extra := range map[string]map[uint16]dngTestField{
		"zero-repeat": {50713: dngTestShort(order, 0, 2)}, "fractional-repeat": {50713: dngTestDouble(order, 1.5, 2)}, "large-repeat": {50713: dngTestLong(order, 32768, 32768)}, "short-repeat": {50713: dngTestShort(order, 2)},
		"short-pattern": {50713: dngTestShort(order, 2, 2), 50714: dngTestLong(order, 1, 2, 3)}, "white-below-max": {50717: dngTestLong(order, 10), 50715: dngTestDouble(order, 1, 20)},
		"delta-full-image": {50829: dngTestLong(order, 0, 1, 2, 2), 50715: dngTestDouble(order, 1, 2)}, "short-vertical": {50716: dngTestDouble(order, 1)}, "nan-delta": {50715: dngTestDouble(order, math.NaN(), 0)},
	} {
		t.Run(name, func(t *testing.T) {
			data := linearDNGFixture(order, 2, 2, 1, []uint16{0, 1, 2, 3}, extra)
			if _, err := decodeLinearDNG(data); err == nil {
				t.Fatal("invalid black metadata accepted")
			}
		})
	}
}

func TestRAWBlackPatternCLIExportAndVideo(t *testing.T) {
	executable, fixtureDir, _ := fakeDNGConverter(t)
	fixture := filepath.Join(fixtureDir, "pattern.dng")
	data := blackPatternDNGFixture(t, binary.LittleEndian)
	if err := os.WriteFile(fixture, data, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LRTL_TEST_CONVERTER_FIXTURE", fixture)
	dir := t.TempDir()
	source := filepath.Join(dir, "frame.nef")
	if err := os.WriteFile(source, data, 0600); err != nil {
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
	if img.Bounds() != image.Rect(0, 0, 8, 6) {
		t.Fatal(img.Bounds())
	}
	// Quantization to RGB16 happens before the separate XMP exposure stage.
	sensor := math.Round(65535*(1.055*math.Pow(200.0/801, 1/2.4)-.055)) / 65535
	linear := math.Pow((sensor+.055)/1.055, 2.4) * math.Sqrt2
	want := math.Round(65535 * (1.055*math.Pow(linear, 1/2.4) - .055))
	r, g, b, _ := img.At(4, 3).RGBA()
	if math.Abs(float64(r)-want) > 1 || r != g || g != b {
		t.Fatalf("RAW/XMP gain r=%d g=%d b=%d want %g", r, g, b, want)
	}
	video := filepath.Join(t.TempDir(), "out.avi")
	if Main([]string{"render", output, "--output", video, "--quality", "100"}, &stdout, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	avi, err := os.ReadFile(video)
	if err != nil {
		t.Fatal(err)
	}
	start, end := bytes.Index(avi, []byte{255, 216}), bytes.Index(avi, []byte{255, 217})
	if start < 0 || end <= start {
		t.Fatal("missing AVI JPEG")
	}
	frame, err := jpeg.Decode(bytes.NewReader(avi[start : end+2]))
	if err != nil {
		t.Fatal(err)
	}
	r, _, _, _ = frame.At(4, 3).RGBA()
	if math.Abs(float64(r>>8)-want/257) > 2 {
		t.Fatal("AVI differs from corrected sensor values")
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("RAW export modified source images or XMP")
	}
}
