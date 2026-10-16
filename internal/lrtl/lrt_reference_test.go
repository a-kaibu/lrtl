package lrtl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLRTReferenceNativeCoordinates(t *testing.T) {
	data, err := os.ReadFile("testdata/lrt-reference-7.5.5.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Count   int
		Anchors []string
		Values  []lrtReferenceBox
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) != 64 {
		t.Fatal("native reference fixture incomplete")
	}
	for c, tc := range cases {
		anchors, err := parseLRTReferenceAnchors(tc.Anchors, tc.Count)
		if err != nil {
			t.Fatal(err)
		}
		got := interpolateLRTReferences(anchors, tc.Count)
		for i, box := range got {
			for axis, v := range box {
				if v != tc.Values[i][axis] {
					t.Fatalf("case %d frame %d axis %d: %.10g != native %.10g", c, i, axis, v, tc.Values[i][axis])
				}
			}
		}
	}
}

func TestLRTReferenceParsing(t *testing.T) {
	for _, rows := range [][]string{
		nil, {"nope"}, {"5;0;0;1;1"}, {"-1;0;0;1;1"}, {"0;NaN;0;1;1"}, {"0;0;Inf;1;1"}, {"0;0;0;1;0"}, {"0;1;0;0;1"},
	} {
		if _, err := parseLRTReferenceAnchors(rows, 5); err == nil {
			t.Fatalf("invalid rows accepted: %v", rows)
		}
	}
	anchors, err := parseLRTReferenceAnchors([]string{"2;0;0;1;1", "0;-0.2;-0.3;1.2;1.3", "2;0.25;0.25;0.75;0.75"}, 5)
	if err != nil || len(anchors) != 2 || anchors[0].box != (lrtReferenceBox{0, 0, 1, 1}) || anchors[1].box != (lrtReferenceBox{.25, .25, .75, .75}) {
		t.Fatalf("sort/clamp/duplicate handling: %v %v", anchors, err)
	}
}

func movingReferenceFixture(t *testing.T, dir string, cropped bool) []Frame {
	t.Helper()
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for i, value := range []uint16{32000, 40000, 32000, 40000, 32000} {
		width := 64
		if cropped {
			width = 16
		}
		img := image.NewGray16(image.Rect(0, 0, width, 16))
		for y := 0; y < 16; y++ {
			for x := 0; x < width; x++ {
				v := uint16(12000 + i*5000)
				if cropped || x >= i*8 && x < i*8+16 {
					v = value
				}
				img.SetGray16(x, y, color.Gray16{Y: v})
			}
		}
		var data bytes.Buffer
		if err := png.Encode(&data, img); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, fmt.Sprintf("frame%d.png", i))
		if err := os.WriteFile(path, data.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		packet := EmptyXMPPacket()
		if err := packet.SetScalar("crs:Exposure2012", "0"); err != nil {
			t.Fatal(err)
		}
		if err := packet.SetScalar("crs:CameraProfile", "keep"); err != nil {
			t.Fatal(err)
		}
		if _, err := WritePacket(path+".xmp", packet, false); err != nil {
			t.Fatal(err)
		}
	}
	if !cropped {
		writeLRTReferenceSettings(t, dir, map[string]any{"imageCount": 5, "filenameSort": true, "referenceBoxes": []string{"0;0;0;0.25;1", "4;0.5;0;0.75;1"}, "unknown": "preserve"})
	}
	frames, err := DiscoverSequence(dir)
	if err != nil {
		t.Fatal(err)
	}
	return frames
}

func TestLRTReferenceMeasurementsAndPasses(t *testing.T) {
	frames := movingReferenceFixture(t, t.TempDir(), false)
	before := batchFileSnapshot(t, filepath.Dir(frames[0].Image))
	plan, err := planDeflickerSequence(frames, image.Rectangle{}, imageInputOptions{LRTReference: true}, 2, 2, true, "lowess")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Measurements) != 2 {
		t.Fatal("missing reference feedback passes")
	}
	for i, value := range []uint16{32000, 40000, 32000, 40000, 32000} {
		if got, want := plan.Measurements[0][i], srgb16ToLinear[value]; math.Abs(got-want) > 1e-15 {
			t.Fatalf("frame %d measured outside moving reference: %.15g != %.15g", i, got, want)
		}
	}
	spread := func(values []float64) float64 {
		lo, hi := values[0], values[0]
		for _, v := range values {
			lo, hi = min(lo, v), max(hi, v)
		}
		return math.Log2(hi / lo)
	}
	if spread(plan.Measurements[1]) >= spread(plan.Measurements[0]) {
		t.Fatal("moving-reference feedback did not converge")
	}
	full, err := planDeflickerSequence(frames, image.Rectangle{}, imageInputOptions{}, 2, 1, true, "lowess")
	if err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(full.Measurements[0], plan.Measurements[0]) {
		t.Fatal("moving reference was ignored")
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, filepath.Dir(frames[0].Image))) {
		t.Fatal("planning changed source files/settings")
	}
}

func TestLRTReferenceCLIEqualsCroppedSequence(t *testing.T) {
	for _, batch := range []bool{false, true} {
		t.Run(fmt.Sprint("batch=", batch), func(t *testing.T) {
			root := t.TempDir()
			refs := map[string]string{}
			for _, name := range []string{"a", "b"} {
				dir, ref := filepath.Join(root, name), t.TempDir()
				movingReferenceFixture(t, dir, false)
				movingReferenceFixture(t, ref, true)
				refs[dir] = ref
			}
			before := batchFileSnapshot(t, root)
			options := []string{"--constant", "--passes", "2", "--smoother", "lowess", "--write"}
			var out, stderr bytes.Buffer
			if batch {
				if Main(append([]string{"batch-deflicker", root, "--lrt-reference", "--workers", "2"}, options...), &out, &stderr) != 0 {
					t.Fatal(&stderr)
				}
			}
			for dir, ref := range refs {
				if !batch {
					if Main(append([]string{"deflicker", dir, "--lrt-reference"}, options...), &out, &stderr) != 0 {
						t.Fatal(&stderr)
					}
				}
				if Main(append([]string{"deflicker", ref}, options...), &out, &stderr) != 0 {
					t.Fatal(&stderr)
				}
				for i := 0; i < 5; i++ {
					name := fmt.Sprintf("frame%d.png.xmp", i)
					want, err := os.ReadFile(filepath.Join(ref, name))
					if err != nil {
						t.Fatal(err)
					}
					got, err := os.ReadFile(filepath.Join(dir, name))
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(got, want) {
						t.Fatalf("%s mismatch\nGOT %s\nWANT %s", name, got, want)
					}
					path := filepath.Join(dir, name)
					if !bytes.Equal(got, before[path]) {
						backups, err := filepath.Glob(path + "*.bak")
						if err != nil || len(backups) != 1 {
							t.Fatal("missing original XMP backup")
						}
						assertPreviewEVFileBytes(t, backups[0], before[path])
					}
				}
			}
			for path, data := range before {
				if !strings.HasSuffix(path, ".xmp") {
					assertPreviewEVFileBytes(t, path, data)
				}
			}
		})
	}
}

func TestLRTReferenceSequenceIdentity(t *testing.T) {
	frames := movingReferenceFixture(t, t.TempDir(), false)
	for _, subset := range [][]Frame{nil, frames[:4], {frames[1], frames[0], frames[2], frames[3], frames[4]}} {
		if _, err := loadLRTReferences(subset); err == nil {
			t.Fatal("incomplete or reordered sequence accepted")
		}
	}
	// A single box is constant, so a capture-time sort flag cannot change its mapping.
	writeLRTReferenceSettings(t, filepath.Dir(frames[0].Image), map[string]any{"imageCount": 5, "filenameSort": false, "referenceBoxes": []string{"3;0.25;0.25;0.75;0.75"}})
	got, err := loadLRTReferences(frames)
	if err != nil {
		t.Fatal(err)
	}
	for _, box := range got {
		if box != (lrtReferenceBox{.25, .25, .75, .75}) {
			t.Fatal("single reference was not copied")
		}
	}
}

func TestLRTReferenceWithNativeVisualCache(t *testing.T) {
	root := t.TempDir()
	_, data := nativePreviewFixture(t, root)
	writeLRTReferenceSettings(t, root, map[string]any{"imageCount": 1, "referenceBoxes": []string{"0;0.25;0.25;0.75;0.75"}})
	frames, err := DiscoverSequence(root)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := planDeflickerSequence(frames, image.Rectangle{}, imageInputOptions{LRTPreview: "visual", LRTReference: true}, 2, 1, true, "lowess")
	if err != nil {
		t.Fatal(err)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	want, err := frameLuminance(img, image.Rect(8, 4, 24, 12))
	if err != nil {
		t.Fatal(err)
	}
	if plan.Measurements[0][0] != want {
		t.Fatal("native visual reference measured wrong region")
	}
	var out, stderr bytes.Buffer
	if Main([]string{"deflicker", root, "--lrt-preview", "visual", "--lrt-reference"}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
}

func FuzzLRTReferences(f *testing.F) {
	f.Add("0;0;0;0.25;1|4;0.5;0;0.75;1", uint8(5))
	f.Add("0;NaN;0;1;1", uint8(1))
	f.Add("0;-0.1;0;1.5;1", uint8(2))
	f.Fuzz(func(t *testing.T, text string, n uint8) {
		count := 1 + int(n)%64
		anchors, err := parseLRTReferenceAnchors(strings.Split(text, "|"), count)
		if err != nil {
			return
		}
		boxes := interpolateLRTReferences(anchors, count)
		if len(boxes) != count {
			t.Fatal("reference count mismatch")
		}
		bounds := image.Rect(7, 11, 408, 312)
		for _, box := range boxes {
			for _, v := range box {
				if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) || v < 0 || v > 1 {
					t.Fatal("invalid normalized coordinate")
				}
			}
			area, err := box.rectangle(bounds)
			if err == nil && (area.Empty() || area.Intersect(bounds) != area) {
				t.Fatal("reference rectangle escaped image bounds")
			}
		}
	})
}

func TestLRTReferenceFailuresWriteNothing(t *testing.T) {
	for _, kind := range []string{"json", "missing", "count", "order", "empty", "bad-coordinate", "subpixel", "reference-conflict", "reset-conflict"} {
		t.Run(kind, func(t *testing.T) {
			for _, batch := range []bool{false, true} {
				root := t.TempDir()
				movingReferenceFixture(t, filepath.Join(root, "a"), false)
				bad := filepath.Join(root, "z")
				movingReferenceFixture(t, bad, false)
				path := filepath.Join(bad, ".lrt", "lrtsequence.json")
				settings := map[string]any{"imageCount": 5, "filenameSort": true, "referenceBoxes": []string{"0;0;0;0.25;1", "4;0.5;0;0.75;1"}}
				switch kind {
				case "count":
					settings["imageCount"] = 6
				case "order":
					settings["filenameSort"] = false
				case "empty":
					settings["referenceBoxes"] = []string{}
				case "bad-coordinate":
					settings["referenceBoxes"] = []string{"0;NaN;0;1;1"}
				case "subpixel":
					settings["referenceBoxes"] = []string{"0;0;0;0.0001;0.0001"}
				}
				writeLRTReferenceSettings(t, bad, settings)
				if kind == "json" {
					if err := os.WriteFile(path, []byte("{"), 0600); err != nil {
						t.Fatal(err)
					}
				}
				if kind == "missing" {
					if err := os.Remove(path); err != nil {
						t.Fatal(err)
					}
				}
				before := batchFileSnapshot(t, root)
				command, dir := "deflicker", bad
				if batch {
					command, dir = "batch-deflicker", root
				}
				args := []string{command, dir, "--lrt-reference", "--write"}
				if kind == "reference-conflict" {
					args = append(args, "--reference", "0,0,16,16")
				}
				if kind == "reset-conflict" {
					args = append(args, "--reset")
				}
				var out, stderr bytes.Buffer
				if Main(args, &out, &stderr) == 0 {
					t.Fatalf("%s %s accepted invalid settings", command, kind)
				}
				if !reflect.DeepEqual(before, batchFileSnapshot(t, root)) {
					t.Fatalf("%s %s wrote source/settings/backups", command, kind)
				}
			}
		})
	}
}

func TestLRTReferenceRectangle(t *testing.T) {
	box := lrtReferenceBox{.1, .2, .8, .9}
	got, err := box.rectangle(image.Rect(7, 11, 108, 90))
	// Origins and extents truncate separately; this differs from subtracting
	// two truncated endpoint positions for these dimensions.
	want := image.Rect(17, 26, 87, 81)
	if err != nil || got != want {
		t.Fatalf("%v != %v (%v)", got, want, err)
	}
	if _, err := (lrtReferenceBox{.1, .1, .1001, .1001}).rectangle(image.Rect(0, 0, 16, 16)); err == nil {
		t.Fatal("subpixel area accepted")
	}
}

func writeLRTReferenceSettings(t *testing.T, root string, settings any) string {
	t.Helper()
	path := filepath.Join(root, ".lrt", "lrtsequence.json")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
