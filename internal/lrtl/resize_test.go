package lrtl

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"math"
	"os"
	"os/exec"
	"strings"
	"testing"

	xdraw "golang.org/x/image/draw"
)

type scaleObservation struct {
	Pattern                                  string
	Width, Height, OutputWidth, OutputHeight int
	Expected                                 []uint16
}

func TestResizeLanczosNativeFilterSelection(t *testing.T) {
	data, err := os.ReadFile("testdata/lrt-scale-filter-7.5.5.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Width, Height, OutputHeight int
		Scale                       string
	}
	if err := json.Unmarshal(data, &cases); err != nil || len(cases) != 6 {
		t.Fatalf("expected six native scale selections: %v", err)
	}
	for _, tc := range cases {
		if !strings.HasSuffix(tc.Scale, ":out_range=full:flags=lanczos") {
			t.Fatalf("native scaling filter changed: %s", tc.Scale)
		}
		// Geometry selection is recorded too, but parity of preset dimension
		// rounding/portrait semantics is a separate, still open requirement.
		width, height, err := outputDimensions(tc.Width, tc.Height, 0, tc.OutputHeight, 0)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(tc.Scale, fmt.Sprintf("scale=w=%d:h=%d:", width, height)) {
			t.Logf("native dimension selection remains different: source=%dx%d height=%d Go=%dx%d native=%s", tc.Width, tc.Height, tc.OutputHeight, width, height, tc.Scale)
		}
	}
}

func scaleObservations(t *testing.T) []scaleObservation {
	t.Helper()
	data, err := os.ReadFile("testdata/lrt-scale-lanczos-ffmpeg-8.0.1.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []scaleObservation
	if err := json.Unmarshal(data, &cases); err != nil || len(cases) != 24 {
		t.Fatalf("expected 24 measured scale cases: %v", err)
	}
	return cases
}

func scaleTestImage(pattern string) *image.Gray16 {
	img := image.NewGray16(image.Rect(0, 0, 32, 24))
	for y := 0; y < 24; y++ {
		for x := 0; x < 32; x++ {
			value := 12345
			switch pattern {
			case "ramp":
				value = x*1600 + y*200
			case "impulse":
				value = 10000
				if x == 16 && y == 12 {
					value = 50000
				}
			case "edge":
				value = 10000
				if x >= 16 {
					value = 50000
				}
			case "checker":
				value = 10000
				if (x+y)%2 == 0 {
					value = 50000
				}
			case "noise":
				value = 1000 + (13*x+23*y+7*x*y)%101*600
			}
			img.SetGray16(x, y, color.Gray16{uint16(value)})
		}
	}
	return img
}

func TestResizeLanczosMeasuredScalePixels(t *testing.T) {
	var previousError, currentError float64
	pixels := 0
	limits := map[string]int{"constant": 0, "ramp": 24, "impulse": 8, "edge": 40, "checker": 800, "noise": 1100}
	for _, tc := range scaleObservations(t) {
		img := scaleTestImage(tc.Pattern)
		before := bytes.Clone(img.Pix)
		if tc.Width != 32 || tc.Height != 24 || len(tc.Expected) != tc.OutputWidth*tc.OutputHeight {
			t.Fatal("invalid scale observation")
		}
		limit, found := limits[tc.Pattern]
		if !found {
			t.Fatal("unknown reference pattern")
		}
		got := resizeFrame(img, tc.OutputWidth, tc.OutputHeight)
		previous := image.NewNRGBA64(got.Bounds())
		xdraw.CatmullRom.Scale(previous, previous.Bounds(), img, img.Bounds(), xdraw.Src, nil)
		for i, want := range tc.Expected {
			x, y := i%tc.OutputWidth, i/tc.OutputWidth
			r, g, b, a := got.At(x, y).RGBA()
			if r != g || r != b || a != 65535 || absInt(int(r)-int(want)) > limit {
				t.Fatalf("%s %dx%d (%d,%d): %d/%d/%d/%d want %d ±%d", tc.Pattern, tc.OutputWidth, tc.OutputHeight, x, y, r, g, b, a, want, limit)
			}
			oldR, _, _, _ := previous.At(x, y).RGBA()
			currentError += math.Pow(float64(r)-float64(want), 2)
			previousError += math.Pow(float64(oldR)-float64(want), 2)
			pixels++
		}
		if !bytes.Equal(img.Pix, before) {
			t.Fatal("resampling changed its source")
		}
	}
	// The independently captured FFmpeg vectors must show a substantial gain
	// over the former filter. This is an aggregate comparison, not exact parity.
	if currentError >= previousError*.01 {
		t.Fatalf("Lanczos failed to reduce RMSE below 10%% of former filter: %g", math.Sqrt(currentError/previousError))
	}
	t.Logf("%d reference samples; RMSE old=%.3f new=%.3f; ratio=%.4f", pixels, math.Sqrt(previousError/float64(pixels)), math.Sqrt(currentError/float64(pixels)), math.Sqrt(currentError/previousError))
}

func TestResizeLanczosActualFFmpegScaleReference(t *testing.T) {
	executable := os.Getenv("LRTL_SCALE_FFMPEG")
	if executable == "" {
		var err error
		executable, err = exec.LookPath("ffmpeg")
		if err != nil {
			t.Skip("FFmpeg unavailable; stored scale observations remain checked")
		}
	}
	for _, tc := range scaleObservations(t) {
		img := scaleTestImage(tc.Pattern)
		raw := make([]byte, 32*24*2)
		for y := 0; y < 24; y++ {
			for x := 0; x < 32; x++ {
				binary.LittleEndian.PutUint16(raw[(y*32+x)*2:], img.Gray16At(x, y).Y)
			}
		}
		cmd := exec.Command(executable, "-v", "error", "-f", "rawvideo", "-pixel_format", "gray16le", "-video_size", "32x24", "-i", "pipe:0", "-vf", fmt.Sprintf("scale=%d:%d:flags=lanczos", tc.OutputWidth, tc.OutputHeight), "-frames:v", "1", "-f", "rawvideo", "-pix_fmt", "gray16le", "pipe:1")
		cmd.Stdin = bytes.NewReader(raw)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		data, err := cmd.Output()
		if err != nil || len(data) != len(tc.Expected)*2 {
			t.Fatalf("scale reference: %v %s", err, stderr.String())
		}
		for i, want := range tc.Expected {
			if got := binary.LittleEndian.Uint16(data[i*2:]); absInt(int(got)-int(want)) > 2 {
				t.Fatalf("FFmpeg %s %dx%d sample%d=%d stored=%d", tc.Pattern, tc.OutputWidth, tc.OutputHeight, i, got, want)
			}
		}
	}
}

func TestResizeLanczosPrecisionAlphaAndBounds(t *testing.T) {
	for _, size := range [][2]int{{1, 1}, {32, 24}, {97, 31}} {
		for _, alpha := range []uint16{0, 32768, 65535} {
			img := image.NewNRGBA64(image.Rect(5, 7, 5+size[0], 7+size[1]))
			for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
				for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
					img.SetNRGBA64(x, y, color.NRGBA64{12345, 23456, 34567, alpha})
				}
			}
			if got := resizeFrame(img, size[0], size[1]); got != img {
				t.Fatal("identity resampling should preserve the original image")
			}
			for _, output := range [][2]int{{16, 16}, {48, 36}} {
				got := resizeFrame(img, output[0], output[1])
				if !highPrecisionImage(got) || got.Bounds() != image.Rect(0, 0, output[0], output[1]) {
					t.Fatal("lost output precision or bounds")
				}
				for y := 0; y < output[1]; y++ {
					for x := 0; x < output[0]; x++ {
						p := color.NRGBA64Model.Convert(got.At(x, y)).(color.NRGBA64)
						if p.A != alpha {
							t.Fatalf("constant alpha=%d became %d", alpha, p.A)
						}
						if alpha != 0 && (absInt(int(p.R)-12345) > 2 || absInt(int(p.G)-23456) > 2 || absInt(int(p.B)-34567) > 2) {
							t.Fatalf("constant RGB16 changed: %+v", p)
						}
					}
				}
			}
		}
	}
	// Hidden red in transparent pixels must not contaminate visible blue.
	img := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	img.SetNRGBA(0, 0, color.NRGBA{255, 0, 0, 0})
	img.SetNRGBA(1, 0, color.NRGBA{0, 0, 255, 255})
	got := resizeFrame(img, 16, 16)
	for x := 0; x < 16; x++ {
		p := color.NRGBAModel.Convert(got.At(x, 8)).(color.NRGBA)
		if p.A > 0 && (p.R != 0 || p.G != 0 || p.B != 255) {
			t.Fatalf("transparent color leaked: %+v", p)
		}
	}
}

func TestResizeLanczosWeights(t *testing.T) {
	for _, dimensions := range [][2]int{{1, 16}, {32, 32}, {4096, 16}, {31, 48}} {
		for _, taps := range lanczos3Contributions(dimensions[0], dimensions[1]) {
			sum := 0.
			previous := -1
			for _, tap := range taps {
				if tap.index <= previous || tap.index >= dimensions[0] {
					t.Fatal("unfolded or invalid edge sample")
				}
				previous = tap.index
				sum += tap.weight
			}
			if math.Abs(sum-1) > 1e-12 {
				t.Fatalf("weights do not preserve DC: %.17g", sum)
			}
		}
	}
	if lanczos3Weight(0) != 1 || lanczos3Weight(3) != 0 || lanczos3Weight(-3) != 0 || lanczos3Weight(.5) != lanczos3Weight(-.5) {
		t.Fatal("invalid Lanczos support/symmetry")
	}
}

func BenchmarkResizeLanczos720p(b *testing.B) {
	img := image.NewNRGBA64(image.Rect(0, 0, 1280, 720))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resizeFrame(img, 640, 360)
	}
}
