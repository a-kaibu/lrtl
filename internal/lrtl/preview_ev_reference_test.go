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
	"strings"
	"testing"
)

func TestPreviewEVNativeReferenceHistogram(t *testing.T) {
	data, err := os.ReadFile("testdata/lrt-preview-ev-roi-7.5.5.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Width, Height, Pattern int
		Box                    lrtReferenceBox
		PreviewEV              float64
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) != 59 {
		t.Fatal("incomplete native histogram fixture")
	}
	for _, tc := range cases {
		img := image.NewNRGBA(image.Rect(7, 11, 7+tc.Width, 11+tc.Height))
		for y := 0; y < tc.Height; y++ {
			for x := 0; x < tc.Width; x++ {
				var r, g, b int
				c := tc.Pattern
				switch c {
				case 0:
					r, g, b = 0, 128, 255
				case 1:
					r, g, b = 255, 255, 255
				case 2, 50:
					r, g, b = 0, 0, 0
				case 3:
					r, g, b = 255, 0, 0
				case 48, 51, 54, 58:
					r, g, b = 128, 128, 128
				case 49, 52:
					r, g, b = 64, 128, 192
				case 53:
					r, g, b = 254, 254, 254
				case 55:
					r, g, b = 64, 64, 64
				case 56:
					if x >= 6 {
						r, g, b = 255, 255, 255
					}
				case 57:
					r, g, b = 254, 128, 0
				default:
					r, g, b = (x*37+y*11+c*17)&255, (x*13+y*41+c*29)&255, (x*23+y*7+c*43)&255
				}
				img.SetNRGBA(x+7, y+11, color.NRGBA{uint8(r), uint8(g), uint8(b), 255})
			}
		}
		area, err := tc.Box.rectangle(img.Bounds())
		if err != nil {
			t.Fatal(err)
		}
		got, err := framePreviewEVArea(img, area)
		if err != nil || math.Abs(got-tc.PreviewEV) > 1e-14 {
			t.Fatalf("pattern %d: %.17g != native %.17g (%v)", tc.Pattern, got, tc.PreviewEV, err)
		}
	}
}

func TestPreviewEVSavedReferenceWritesCorrectFrames(t *testing.T) {
	for _, dateOrder := range []bool{false, true} {
		root, ref := t.TempDir(), t.TempDir()
		var frames, reference []Frame
		order := []int{0, 1, 2, 3, 4}
		if dateOrder {
			frames, reference, order = dateReferenceFixture(t, root, ref)
		} else {
			frames = movingReferenceFixture(t, root, false)
			reference = movingReferenceFixture(t, ref, true)
		}
		before := batchFileSnapshot(t, root)
		var out, stderr bytes.Buffer
		if Main([]string{"preview-ev", root, "--lrt-reference"}, &out, &stderr) != 0 {
			t.Fatal(&stderr)
		}
		if !reflect.DeepEqual(before, batchFileSnapshot(t, root)) {
			t.Fatal("reference preview changed source/settings")
		}
		if Main([]string{"preview-ev", root, "--lrt-reference", "--write"}, &out, &stderr) != 0 {
			t.Fatal(&stderr)
		}
		if Main([]string{"preview-ev", ref, "--write"}, &out, &stderr) != 0 {
			t.Fatal(&stderr)
		}
		for rank, original := range order {
			frame := frames[original]
			want, err := os.ReadFile(reference[rank].Sidecar)
			if err != nil {
				t.Fatal(err)
			}
			assertPreviewEVFileBytes(t, frame.Sidecar, want)
			backups, err := filepath.Glob(frame.Sidecar + "*.bak")
			if err != nil || len(backups) != 1 {
				t.Fatal("wrong preview backup mapping")
			}
			assertPreviewEVFileBytes(t, backups[0], before[frame.Sidecar])
			assertPreviewEVFileBytes(t, frame.Image, before[frame.Image])
		}
		path := filepath.Join(root, ".lrt", "lrtsequence.json")
		assertPreviewEVFileBytes(t, path, before[path])
	}
}

func TestPreviewEVReferenceNativeCacheAndErrors(t *testing.T) {
	root := t.TempDir()
	_, data := nativePreviewFixture(t, root)
	writeLRTReferenceSettings(t, root, map[string]any{"imageCount": 1, "referenceBoxes": []string{"0;0.25;0.25;0.75;0.75"}})
	frames, err := DiscoverSequence(root)
	if err != nil {
		t.Fatal(err)
	}
	img, _, err := decodeLRTMeteringImage(data)
	if err != nil {
		t.Fatal(err)
	}
	want, err := framePreviewEVArea(img, image.Rect(8, 4, 24, 12))
	if err != nil {
		t.Fatal(err)
	}
	values, err := analyzePreviewEVSequence(frames, imageInputOptions{LRTReference: true, LRTPreview: "visual"})
	if err != nil || values[0] != want {
		t.Fatalf("wrong native cache ROI: %v %v", values, err)
	}
	var out, stderr bytes.Buffer
	if Main([]string{"preview-ev", root, "--lrt-preview", "visual", "--lrt-reference", "--write", "--no-backup"}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	packet, err := PacketForFrame(frames[0])
	if err != nil {
		t.Fatal(err)
	}
	value, found, err := packet.GetScalar("lrt:ExternalExposureDefault")
	if err != nil || !found || value != formatNumber(4*want) {
		t.Fatal("cache measurement saved to wrong XMP")
	}
	for _, mode := range []string{"missing", "json", "subpixel", "partial-date"} {
		dir := t.TempDir()
		movingReferenceFixture(t, dir, false)
		path := filepath.Join(dir, ".lrt", "lrtsequence.json")
		switch mode {
		case "missing":
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		case "json":
			if err := os.WriteFile(path, []byte("{"), 0600); err != nil {
				t.Fatal(err)
			}
		case "subpixel":
			writeLRTReferenceSettings(t, dir, map[string]any{"referenceBoxes": []string{"0;0;0;0.0001;0.0001"}})
		case "partial-date":
			writeLRTReferenceSettings(t, dir, map[string]any{"filenameSort": false, "referenceBoxes": []string{"0;0;0;1;1"}})
			writeTestXMP(t, filepath.Join(dir, "frame0.png"), map[string]string{"xmp:CreateDate": "2026-10-17T00:00:00"})
		}
		before := batchFileSnapshot(t, dir)
		if Main([]string{"preview-ev", dir, "--lrt-reference", "--write"}, &out, &stderr) == 0 {
			t.Fatalf("%s invalid ROI accepted", mode)
		}
		if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
			t.Fatalf("%s wrote XMP/settings/backups", mode)
		}
	}
	for _, area := range []image.Rectangle{{}, image.Rect(-1, 0, 2, 2), image.Rect(0, 0, 1000, 1000)} {
		if _, err := framePreviewEVArea(img, area); err == nil {
			t.Fatal("invalid Preview EV area accepted")
		}
	}
	if !strings.Contains(out.String(), "PreviewEV") {
		t.Fatal("missing measurement output")
	}
}

func FuzzPreviewEVReferenceArea(f *testing.F) {
	f.Add(uint16(0), uint16(32768), uint16(65535), uint8(16))
	f.Add(uint16(65535), uint16(65535), uint16(65535), uint8(1))
	f.Fuzz(func(t *testing.T, r, g, b uint16, size uint8) {
		width, height := 1+int(size)%32, 1+int(size/3)%16
		img := image.NewNRGBA64(image.Rect(7, 11, 7+width, 11+height))
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				img.SetNRGBA64(7+x, 11+y, color.NRGBA64{r + uint16(x*131), g + uint16(y*97), b + uint16((x+y)*17), 45000})
			}
		}
		before := append([]byte(nil), img.Pix...)
		area := image.Rect(7, 11, 7+max(1, width/2), 11+max(1, height/2))
		got, err := framePreviewEVArea(img, area)
		if err != nil || math.IsNaN(got) || math.IsInf(got, 0) || got < 0 || got > 255.0/256 {
			t.Fatalf("invalid measurement %g: %v", got, err)
		}
		cropped, err := framePreviewEV(img.SubImage(area))
		if err != nil || got != cropped {
			t.Fatal("ROI differs from the same cropped pixels")
		}
		if !bytes.Equal(before, img.Pix) {
			t.Fatal("measurement modified input pixels")
		}
	})
}
