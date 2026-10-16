package lrtl

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func previewCutImage(pattern int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, 32, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 32; x++ {
			var r, g, b int
			switch pattern {
			case 0:
			case 1:
				r, g, b = 255, 255, 255
			case 2:
				r, g, b = 0, 128, 255
			case 3:
				r, g, b = x*8, x*8, x*8
			case 4:
				r, g, b = 16, 16, 16
				if x >= 16 {
					r, g, b = 240, 240, 240
				}
			default:
				r, g, b = (x*37+y*11+17)&255, (x*13+y*41+29)&255, (x*23+y*7+43)&255
			}
			img.SetNRGBA(x, y, color.NRGBA{uint8(r), uint8(g), uint8(b), 255})
		}
	}
	return img
}

func TestPreviewEVCutsNativeValues(t *testing.T) {
	data, err := os.ReadFile("testdata/lrt-preview-cuts-7.5.5.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Pattern, Black, White int
		Hard                  bool
		Value                 float64
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) != 96 {
		t.Fatal("incomplete native cut fixture")
	}
	for c, tc := range cases {
		img := previewCutImage(tc.Pattern)
		options := previewHistogramOptions{Enabled: true, IgnoreBlacks: tc.Black, IgnoreWhites: tc.White, HardCut: tc.Hard}
		got, err := framePreviewEVAreaWithOptions(img, img.Bounds(), options)
		if err != nil || math.Abs(got-tc.Value) > 1e-14 {
			t.Fatalf("case %d: %.17g != native %.17g (%v)", c, got, tc.Value, err)
		}
	}
}

func TestPreviewEVCutsCLIWithReferenceAndBackup(t *testing.T) {
	for _, hard := range []bool{false, true} {
		root := t.TempDir()
		path := filepath.Join(root, "frame.png")
		img := previewCutImage(5)
		writePreviewEVFixture(t, path, img)
		sidecar := writeTestXMP(t, path, map[string]string{"crs:CameraProfile": "preserve", "crs:Exposure2012": "1.25"})
		writeLRTReferenceSettings(t, root, map[string]any{"imageCount": 1, "referenceBoxes": []string{"0;0.25;0.25;0.75;0.75"}})
		before := batchFileSnapshot(t, root)
		options := previewHistogramOptions{Enabled: true, IgnoreBlacks: 10, IgnoreWhites: 25, HardCut: hard}
		want, err := framePreviewEVAreaWithOptions(img, image.Rect(8, 4, 24, 12), options)
		if err != nil {
			t.Fatal(err)
		}
		args := []string{"preview-ev", root, "--lrt-reference", "--ignore-blacks", "10", "--ignore-whites", "25"}
		if hard {
			args = append(args, "--hard-cut")
		}
		var out, stderr bytes.Buffer
		if Main(args, &out, &stderr) != 0 {
			t.Fatal(&stderr)
		}
		if !reflect.DeepEqual(before, batchFileSnapshot(t, root)) {
			t.Fatal("cut preview modified source/settings")
		}
		if Main(append(args, "--write"), &out, &stderr) != 0 {
			t.Fatal(&stderr)
		}
		packet, err := PacketForSidecar(sidecar)
		if err != nil {
			t.Fatal(err)
		}
		for property, value := range map[string]string{"lrt:ExternalExposureDefault": formatNumber(4 * want), "crs:CameraProfile": "preserve", "crs:Exposure2012": "1.25"} {
			got, found, err := packet.GetScalar(property)
			if err != nil || !found || got != value {
				t.Fatalf("%s=%s want %s (%v)", property, got, value, err)
			}
		}
		backups, err := filepath.Glob(sidecar + "*.bak")
		if err != nil || len(backups) != 1 {
			t.Fatal("missing cut XMP backup")
		}
		assertPreviewEVFileBytes(t, backups[0], before[sidecar])
		for path, data := range before {
			if path != sidecar {
				assertPreviewEVFileBytes(t, path, data)
			}
		}
	}
}

func TestPreviewEVCutsExplicitModeAndInvalidOptions(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "frame.png")
	writePreviewEVFixture(t, path, previewCutImage(1))
	before := batchFileSnapshot(t, root)
	var out, stderr bytes.Buffer
	for _, options := range [][]string{{"--ignore-blacks", "-1"}, {"--ignore-whites", "101"}, {"--ignore-blacks", "1.5"}} {
		args := append([]string{"preview-ev", root, "--write"}, options...)
		if Main(args, &out, &stderr) == 0 {
			t.Fatal("invalid cut options accepted")
		}
		if !reflect.DeepEqual(before, batchFileSnapshot(t, root)) {
			t.Fatal("invalid cut options wrote files")
		}
	}
	for _, flags := range [][]string{{"--ignore-blacks", "0"}, {"--ignore-whites", "0"}, {"--hard-cut"}, {"--hard-cut=false"}} {
		out.Reset()
		stderr.Reset()
		if Main(append([]string{"preview-ev", root}, flags...), &out, &stderr) != 0 {
			t.Fatal(&stderr)
		}
		if !bytes.Contains(out.Bytes(), []byte("PreviewEV=0 ")) {
			t.Fatalf("explicit cut mode did not use native empty-band behavior: %s", &out)
		}
	}
	if _, err := framePreviewEVAreaWithOptions(previewCutImage(0), image.Rect(0, 0, 32, 16), previewHistogramOptions{Enabled: true, IgnoreWhites: 101}); err == nil {
		t.Fatal("API accepted invalid cutoff")
	}
}

func TestPreviewEVCutsNativeCache(t *testing.T) {
	root := t.TempDir()
	_, data := nativePreviewFixture(t, root)
	writeLRTReferenceSettings(t, root, map[string]any{"referenceBoxes": []string{"0;0.25;0.25;0.75;0.75"}})
	frames, err := DiscoverSequence(root)
	if err != nil {
		t.Fatal(err)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	options := previewHistogramOptions{Enabled: true, IgnoreBlacks: 10, IgnoreWhites: 25}
	want, err := framePreviewEVAreaWithOptions(img, image.Rect(8, 4, 24, 12), options)
	if err != nil {
		t.Fatal(err)
	}
	got, err := analyzePreviewEVSequence(frames, imageInputOptions{LRTPreview: "camera", LRTReference: true, PreviewHistogram: options})
	if err != nil || len(got) != 1 || got[0] != want {
		t.Fatalf("cached RAW cut measurement differs: %v (%v)", got, err)
	}
}

func FuzzPreviewEVCuts(f *testing.F) {
	f.Add(uint8(5), uint8(10), uint8(25), true)
	f.Add(uint8(1), uint8(100), uint8(100), false)
	f.Fuzz(func(t *testing.T, pattern, black, white uint8, hard bool) {
		img := previewCutImage(int(pattern) % 6)
		before := append([]byte(nil), img.Pix...)
		v, err := framePreviewEVAreaWithOptions(img, img.Bounds(), previewHistogramOptions{Enabled: true, IgnoreBlacks: int(black) % 101, IgnoreWhites: int(white) % 101, HardCut: hard})
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 255.0/256 {
			t.Fatalf("invalid cut measurement %g (%v)", v, err)
		}
		if !bytes.Equal(before, img.Pix) {
			t.Fatal("cut measurement modified image")
		}
	})
}
