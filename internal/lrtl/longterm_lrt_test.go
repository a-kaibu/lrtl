package lrtl

import (
	"bytes"
	"compress/zlib"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

type longTermMetricObservation struct {
	Alpha                    bool
	Width, Height, Pattern   int
	Luminance, Contrast, Hue float64
	Sampled, Blurred         []byte
}

func TestLongTermNativeHueAllRGBColors(t *testing.T) {
	var hashes []string
	if err := json.Unmarshal(denoiseRead(t, "testdata/java-hue-rgb-25.json"), &hashes); err != nil || len(hashes) != 256 {
		t.Fatalf("expected 256 Java hue blocks: %v", err)
	}
	block := make([]byte, 256*256*4)
	for red := 0; red < 256; red++ {
		cursor := 0
		for green := 0; green < 256; green++ {
			for blue := 0; blue < 256; blue++ {
				bits := math.Float32bits(float32(lrtHSBHue(uint8(red), uint8(green), uint8(blue))))
				binary.LittleEndian.PutUint32(block[cursor:], bits)
				cursor += 4
			}
		}
		hash := sha256.Sum256(block)
		if hex.EncodeToString(hash[:]) != hashes[red] {
			t.Fatalf("Java hue bits differ for red=%d", red)
		}
	}
}

func TestLongTermNativeCLISelection(t *testing.T) {
	dir := t.TempDir()
	var rows []longTermMetricObservation
	for _, row := range longTermMetricObservations(t) {
		if row.Width != 64 || row.Alpha {
			continue
		}
		rows = append(rows, row)
		path := filepath.Join(dir, fmt.Sprintf("frame%02d.png", row.Pattern))
		file, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		err = png.Encode(file, longTermMetricSource(64, 64, row.Pattern, false))
		closeErr := file.Close()
		if err != nil || closeErr != nil {
			t.Fatal(err, closeErr)
		}
	}
	if len(rows) != 8 {
		t.Fatal("missing CLI reference inputs")
	}
	before := batchFileSnapshot(t, dir)
	for _, axis := range []string{"luminance", "contrast", "hue"} {
		t.Run(axis, func(t *testing.T) {
			value := func(r longTermMetricObservation) float64 {
				switch axis {
				case "luminance":
					return r.Luminance
				case "contrast":
					return r.Contrast
				default:
					return r.Hue
				}
			}
			minValue, maxValue := value(rows[4])-1e-8, value(rows[4])+1e-8
			output := filepath.Join(t.TempDir(), "filter.json")
			markerCommand(t, "long-term", dir, "--metric-profile", "lrt-7.5.5", "--"+axis+"-range", fmt.Sprintf("%.17g,%.17g", minValue, maxValue), "--output", output)
			var manifest sequenceSelectionManifest
			if err := json.Unmarshal(denoiseRead(t, output), &manifest); err != nil {
				t.Fatal(err)
			}
			if manifest.MetricRevision != "lrt-7.5.5-camera-cache-preview-histogram-v3" || len(manifest.Frames) != len(rows) {
				t.Fatal("missing metric revision or frames", manifest)
			}
			included := 0
			for i, frame := range manifest.Frames {
				r := rows[i]
				m := frame.LRTMetrics
				if m == nil || math.Abs(m.LuminanceMean-r.Luminance) > 1e-12 || math.Abs(m.ContrastStdDev-r.Contrast) > 1e-12 || m.HueMeanDegrees == nil || math.Abs(*m.HueMeanDegrees-r.Hue) > 1e-12 {
					t.Fatalf("saved native metrics differ for frame%d: %+v", i, m)
				}
				want := value(r) >= minValue && value(r) <= maxValue
				if frame.Included != want {
					t.Fatalf("frame%d filter%v want%v", i, frame.Included, want)
				}
				if want {
					included++
				}
			}
			frames, err := DiscoverSequence(dir)
			if err != nil {
				t.Fatal(err)
			}
			selected, err := loadSelectedFrames(frames, output, dir)
			if err != nil || len(selected) != included {
				t.Fatal("selection consumer", len(selected), included, err)
			}
			// Older manifests keep their recorded decisions. Their old metrics
			// must not silently be reinterpreted with the corrected formula.
			legacy := manifest
			legacy.MetricRevision = ""
			for _, version := range []int{1, 2} {
				legacy.Version = version
				data, err := json.Marshal(legacy)
				if err != nil {
					t.Fatal(err)
				}
				oldPath := filepath.Join(t.TempDir(), "legacy.json")
				if err := os.WriteFile(oldPath, data, 0600); err != nil {
					t.Fatal(err)
				}
				oldSelection, err := loadSelectedFrames(frames, oldPath, dir)
				if err != nil || !reflect.DeepEqual(oldSelection, selected) {
					t.Fatal("legacy manifest changed saved decisions", version, err)
				}
			}
			if axis == "contrast" {
				video := filepath.Join(t.TempDir(), "filtered.avi")
				markerCommand(t, "render", dir, "--selection", output, "--output-height", "32", "--output", video)
				if count := bytes.Count(denoiseRead(t, video), []byte{255, 216}); count != included {
					t.Fatalf("filtered AVI has%d frames want%d", count, included)
				}
			}
		})
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("long-term selection modified source files")
	}
	output := filepath.Join(t.TempDir(), "existing.json")
	if err := os.WriteFile(output, []byte("preserve existing output"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "frame99.png"), []byte("broken future image"), 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if Main([]string{"long-term", dir, "--metric-profile", "lrt-7.5.5", "--output", output, "--overwrite"}, &stdout, &stderr) == 0 {
		t.Fatal("accepted corrupt future image")
	}
	assertPreviewEVFileBytes(t, output, []byte("preserve existing output"))
}

func TestLRTLongTermPreviewMetricsMatchNative(t *testing.T) {
	data, err := os.ReadFile("testdata/lrt-longterm-preview-metrics-7.5.5.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		SourceWidth, SourceHeight int
		PreviewWidthCamera        int
		Pattern                   int
		Width, Height             int
		Luminance, Contrast, Hue  float64
	}
	if err := json.Unmarshal(data, &cases); err != nil || len(cases) != 5 {
		t.Fatalf("expected five native preview metric cases: %v", err)
	}
	for index, tc := range cases {
		t.Run(fmt.Sprintf("%dx%d/width%d/p%d", tc.SourceWidth, tc.SourceHeight, tc.PreviewWidthCamera, tc.Pattern), func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "frame.png")
			file, err := os.Create(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := png.Encode(file, longTermMetricSource(tc.SourceWidth, tc.SourceHeight, tc.Pattern, false)); err != nil {
				_ = file.Close()
				t.Fatal(err)
			}
			if err := file.Close(); err != nil {
				t.Fatal(err)
			}
			output := filepath.Join(t.TempDir(), "selection.json")
			args := []string{"long-term", dir, "--metric-profile", "lrt-7.5.5", "--output", output}
			// The first case exercises the 1024 default. The other default-width
			// cases exercise the flag, and the final case verifies a custom width.
			if index != 0 {
				args = append(args, "--preview-width-camera", strconv.Itoa(tc.PreviewWidthCamera))
			}
			markerCommand(t, args...)
			var manifest sequenceSelectionManifest
			if err := json.Unmarshal(denoiseRead(t, output), &manifest); err != nil {
				t.Fatal(err)
			}
			if manifest.MetricRevision != "lrt-7.5.5-camera-cache-preview-histogram-v3" || len(manifest.Frames) != 1 {
				t.Fatalf("unexpected native preview manifest: revision=%q frames=%d", manifest.MetricRevision, len(manifest.Frames))
			}
			got := manifest.Frames[0].LRTMetrics
			if got == nil || math.Abs(got.LuminanceMean-tc.Luminance) > 1e-12 || math.Abs(got.ContrastStdDev-tc.Contrast) > 1e-12 || got.HueMeanDegrees == nil || math.Abs(*got.HueMeanDegrees-tc.Hue) > 1e-12 {
				t.Fatalf("long-term metrics after %dx%d preview resize=%+v, native %dx%d wants %.17g/%.17g/%.17g", tc.SourceWidth, tc.SourceHeight, got, tc.Width, tc.Height, tc.Luminance, tc.Contrast, tc.Hue)
			}
		})
	}
}

func TestLongTermPreviewWidthFlagValidation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "frame.png")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(file, longTermMetricSource(2, 2, 4, false)); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{"too small", []string{"--metric-profile", "lrt-7.5.5", "--preview-width-camera", "1"}, "at least 2 pixels"},
		{"requires native profile", []string{"--preview-width-camera", "512"}, "requires --metric-profile lrt-7.5.5"},
	} {
		t.Run(test.name, func(t *testing.T) {
			args := append([]string{"long-term", dir, "--output", filepath.Join(t.TempDir(), "selection.json")}, test.args...)
			var stdout, stderr bytes.Buffer
			if Main(args, &stdout, &stderr) == 0 || !strings.Contains(stderr.String(), test.want) {
				t.Fatalf("accepted invalid preview-width arguments: stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
		})
	}
}

func TestLongTermNativeOffsetBounds(t *testing.T) {
	for _, alpha := range []bool{false, true} {
		source := longTermMetricSource(17, 11, 4, alpha)
		shifted := image.NewNRGBA(image.Rect(-9, 23, 8, 34))
		copy(shifted.Pix, source.Pix)
		if !reflect.DeepEqual(measureLRTLongTermFrame(source), measureLRTLongTermFrame(shifted)) || !bytes.Equal(sampleLRTLongTermFrame(source).Pix, sampleLRTLongTermFrame(shifted).Pix) {
			t.Fatal("nonzero image bounds changed native measurement")
		}
	}
}

func TestLongTermNativeXMPExposure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "frame.png")
	img := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	for i := 0; i < len(img.Pix); i += 4 {
		copy(img.Pix[i:], []byte{128, 128, 128, 255})
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	err = png.Encode(file, img)
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		t.Fatal(err, closeErr)
	}
	writeTestXMP(t, path, map[string]string{"crs:Exposure2012": "4", "dc:description": "preserve source metadata"})
	before := batchFileSnapshot(t, dir)
	output := filepath.Join(t.TempDir(), "filter.json")
	markerCommand(t, "long-term", dir, "--metric-profile", "lrt-7.5.5", "--apply-xmp-exposure", "--output", output)
	var manifest sequenceSelectionManifest
	if err := json.Unmarshal(denoiseRead(t, output), &manifest); err != nil || len(manifest.Frames) != 1 {
		t.Fatal("invalid exposure manifest", err)
	}
	var white longTermMetricObservation
	for _, row := range longTermMetricObservations(t) {
		if row.Width == 64 && row.Pattern == 0 && !row.Alpha {
			white = row
		}
	}
	got := manifest.Frames[0].LRTMetrics
	if got == nil || math.Abs(got.LuminanceMean-white.Luminance) > 1e-12 || math.Abs(got.ContrastStdDev-white.Contrast) > 1e-12 || got.HueMeanDegrees == nil || *got.HueMeanDegrees != white.Hue {
		t.Fatalf("XMP was not applied before native measurement: %+v want %+v", got, white)
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("measurement changed source images or XMP")
	}
}

func FuzzLongTermNativeMetrics(f *testing.F) {
	f.Add(uint8(17), uint8(11), uint8(4), true)
	f.Fuzz(func(t *testing.T, w, h, p uint8, alpha bool) {
		img := longTermMetricSource(int(w%64)+1, int(h%64)+1, int(p%8), alpha)
		before := bytes.Clone(img.Pix)
		got := measureLRTLongTermFrame(img)
		for _, v := range []struct{ value, limit float64 }{{got.LuminanceMean, 255. / 256}, {got.ContrastStdDev, 1}, {*got.HueMeanDegrees, 360}} {
			if math.IsNaN(v.value) || math.IsInf(v.value, 0) || v.value < 0 || v.value > v.limit+1e-12 {
				t.Fatal("invalid native metric", got)
			}
		}
		if !bytes.Equal(before, img.Pix) || !reflect.DeepEqual(got, measureLRTLongTermFrame(img)) {
			t.Fatal("source mutation or nondeterministic measurement")
		}
	})
}

func longTermMetricObservations(t *testing.T) []longTermMetricObservation {
	t.Helper()
	var rows []longTermMetricObservation
	if err := json.Unmarshal(denoiseRead(t, "testdata/lrt-longterm-metrics-7.5.5.json"), &rows); err != nil || len(rows) != 112 {
		t.Fatalf("expected 112 native long-term measurements: %v", err)
	}
	return rows
}

func longTermMetricSource(width, height, pattern int, alpha bool) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			r, g, b := 0, 0, 0
			switch pattern {
			case 0:
				r, g, b = 255, 255, 255
			case 1:
				r = 255
			case 2:
				r = x * 255 / max(1, width-1)
				g = r
				b = r
			case 3:
				r = (x + y) % 2 * 255
				g = r
				b = r
			case 4:
				r = (x*31 + y*17 + 13) & 255
				g = (x*7 + y*43 + 61) & 255
				b = (x*53 + y*3 + 101) & 255
			case 5:
				if x < width/2 {
					r = 255
				}
				if y < height/2 {
					g = 255
				}
				b = 64
			case 6:
				if x == width/2 && y == height/2 {
					r, g, b = 255, 255, 255
				}
			}
			a := 255
			if alpha {
				a = (x*19 + y*29 + pattern*37) & 255
			}
			img.SetNRGBA(x, y, color.NRGBA{uint8(r), uint8(g), uint8(b), uint8(a)})
		}
	}
	return img
}

func TestLongTermNativeMetrics(t *testing.T) {
	for _, row := range longTermMetricObservations(t) {
		t.Run(fmt.Sprintf("%dx%d/p%d/alpha%v", row.Width, row.Height, row.Pattern, row.Alpha), func(t *testing.T) {
			img := longTermMetricSource(row.Width, row.Height, row.Pattern, row.Alpha)
			got := measureLRTLongTermFrame(img)
			for _, v := range []struct {
				name      string
				got, want float64
			}{{"luminance", got.LuminanceMean, row.Luminance}, {"contrast", got.ContrastStdDev, row.Contrast}, {"hue", *got.HueMeanDegrees, row.Hue}} {
				if math.Abs(v.got-v.want) > 1e-12 {
					t.Errorf("%s %.17g want %.17g", v.name, v.got, v.want)
				}
			}
		})
	}
}

func TestLongTermNativeStages(t *testing.T) {
	for _, row := range longTermMetricObservations(t) {
		t.Run(fmt.Sprintf("%dx%d/p%d/alpha%v", row.Width, row.Height, row.Pattern, row.Alpha), func(t *testing.T) {
			img := longTermMetricSource(row.Width, row.Height, row.Pattern, row.Alpha)
			sampled := sampleLRTLongTermFrame(img)
			blurred := blurLRTLongTermFrame(sampled, lrtGaussianKernel(4))
			for _, stage := range []struct {
				name string
				img  *image.NRGBA
				want []byte
			}{{"sampled", sampled, row.Sampled}, {"blurred", blurred, row.Blurred}} {
				reader, err := zlib.NewReader(bytes.NewReader(stage.want))
				if err != nil {
					t.Fatal(err)
				}
				want, err := io.ReadAll(io.LimitReader(reader, 64*64*4+1))
				reader.Close()
				if err != nil {
					t.Fatal(err)
				}
				if len(want) != 64*64*4 {
					t.Fatal("invalid native stage size")
				}
				mismatch := 0
				maxError := 0
				for i, v := range want {
					got := stage.img.Pix[i]
					d := int(got) - int(v)
					if d != 0 {
						mismatch++
						maxError = max(maxError, int(math.Abs(float64(d))))
					}
				}
				if mismatch > 0 {
					t.Errorf("%s: %d different components; max error %d", stage.name, mismatch, maxError)
				}
			}
		})
	}
}
