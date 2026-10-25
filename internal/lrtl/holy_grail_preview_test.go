package lrtl

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func makeHolyGrailPreviewSequence(t *testing.T, root string) []Frame {
	t.Helper()
	levels := []uint8{32, 96, 184}
	ratings := []string{"2", "3", "4"}
	for index, level := range levels {
		path := filepath.Join(root, "frame"+strconvTwo(index+1)+".NEF")
		if err := os.WriteFile(path, []byte("RAW decode must not be used when selecting an LRT preview cache"), 0600); err != nil {
			t.Fatal(err)
		}
		writeTestXMP(t, path, map[string]string{
			"lrt:ShutterSpeed": "1", "lrt:Iso": "100", "lrt:Aperture": "8",
			"xmp:Rating": ratings[index],
		})
		preview := image.NewGray(image.Rect(0, 0, 32, 16))
		for y := 0; y < preview.Bounds().Dy(); y++ {
			for x := 0; x < preview.Bounds().Dx(); x++ {
				preview.SetGray(x, y, color.Gray{Y: uint8(min(uint16(level)+uint16(x*3), 250))})
			}
		}
		cache := lrtPreviewPath(path, "visual")
		if err := os.MkdirAll(filepath.Dir(cache), 0700); err != nil {
			t.Fatal(err)
		}
		file, err := os.Create(cache)
		if err != nil {
			t.Fatal(err)
		}
		if err := jpeg.Encode(file, preview, &jpeg.Options{Quality: 100}); err != nil {
			file.Close()
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
	}
	frames, err := DiscoverSequence(root)
	if err != nil {
		t.Fatal(err)
	}
	return frames
}

func TestHolyGrailUsesSelectedLRTPreviewWithoutSavedPreviewEV(t *testing.T) {
	root := t.TempDir()
	frames := makeHolyGrailPreviewSequence(t, root)
	before := batchFileSnapshot(t, root)
	values, err := analyzePreviewEVSequence(frames, imageInputOptions{LRTPreview: "visual"})
	if err != nil {
		t.Fatal(err)
	}
	got, ratings, err := readHolyGrailLRTInputs(frames, "visual", false)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, values) {
		t.Fatalf("preview inputs = %v, want measured Preview EV %v", got, values)
	}
	if !reflect.DeepEqual(ratings, []int{2, 3, 4}) {
		t.Fatalf("ratings = %v, want [2 3 4]", ratings)
	}

	var out, stderr bytes.Buffer
	if code := Main([]string{"holy-grail", root, "--lrt-preview", "visual"}, &out, &stderr); code != 0 {
		t.Fatalf("direct preview input exit=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(out.String(), "Curve input: LRT visual preview cache Preview EV") {
		t.Fatalf("CLI did not report the selected cache input: %s", out.String())
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, root)) {
		t.Fatal("preview-only Holy Grail analysis changed RAW, XMP, or cache files")
	}

	out.Reset()
	stderr.Reset()
	if code := Main([]string{"preview-ev", root, "--lrt-preview", "visual", "--write", "--no-backup"}, &out, &stderr); code != 0 {
		t.Fatalf("persist Preview EV through CLI exit=%d stderr=%s", code, stderr.String())
	}
	stored, _, err := readHolyGrailLRTInputs(frames, "", false)
	if err != nil {
		t.Fatal(err)
	}
	for index := range values {
		if math.Abs(stored[index]-got[index]) > 1e-11 {
			t.Fatalf("cache and persisted Preview EV differ at frame %d: %.15g != %.15g", index, got[index], stored[index])
		}
	}
	directPlan, err := buildHolyGrailPlanWithCurveOptions(frames, false, false, false, false, &HolyGrailCurveOptions{LRTInput: true, LRTPreview: "visual"})
	if err != nil {
		t.Fatal(err)
	}
	storedPlan, err := buildHolyGrailPlanWithCurveOptions(frames, false, false, false, false, &HolyGrailCurveOptions{LRTInput: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(directPlan.Changes) != len(storedPlan.Changes) {
		t.Fatalf("direct/cache plan lengths differ: %d != %d", len(directPlan.Changes), len(storedPlan.Changes))
	}
	for index := range directPlan.Changes {
		if math.Abs(directPlan.Changes[index].Compensation-storedPlan.Changes[index].Compensation) > 1e-10 {
			t.Fatalf("cache and persisted Preview EV produce different compensation at frame %d: %.15g != %.15g", index, directPlan.Changes[index].Compensation, storedPlan.Changes[index].Compensation)
		}
	}

	dates := []string{"2026-10-03T00:00:00", "2026-10-01T00:00:00", "2026-10-02T00:00:00"}
	for index, frame := range frames {
		packet, err := PacketForFrame(frame)
		if err != nil {
			t.Fatal(err)
		}
		if err := packet.SetScalar("xmp:CreateDate", dates[index]); err != nil {
			t.Fatal(err)
		}
		data, err := packet.Serialize()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(frame.OutputSidecar(), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	settingsPath := filepath.Join(root, ".lrt", "lrtsequence.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsPath, []byte(`{"imageCount":3,"filenameSort":false,"holyGrailReduceDeflicker":0,"holyGrailStretch":0,"holyGrailRotate":0,"referenceBoxes":["0;0;0;0.5;1"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	fullFrameValues, err := analyzePreviewEVSequence(frames, imageInputOptions{LRTPreview: "visual"})
	if err != nil {
		t.Fatal(err)
	}
	referenceValues, err := analyzePreviewEVSequence(frames, imageInputOptions{LRTPreview: "visual", LRTReference: true})
	if err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(fullFrameValues, referenceValues) {
		t.Fatalf("reference-box input did not restrict the preview measurement: full=%v reference=%v", fullFrameValues, referenceValues)
	}
	if _, _, err := readHolyGrailLRTInputs(frames, "visual", true); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	stderr.Reset()
	if code := Main([]string{"holy-grail", root, "--lrt-holy-grail-settings", "--lrt-preview", "visual", "--lrt-reference"}, &out, &stderr); code != 0 {
		t.Fatalf("LRT settings + cache + reference input exit=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(out.String(), "in saved reference boxes") {
		t.Fatalf("CLI did not apply LRT reference boxes: %s", out.String())
	}

	batchRoot := t.TempDir()
	batchSequence := filepath.Join(batchRoot, "sequence")
	if err := os.MkdirAll(batchSequence, 0700); err != nil {
		t.Fatal(err)
	}
	makeHolyGrailPreviewSequence(t, batchSequence)
	secondSequence := filepath.Join(batchRoot, "sequence-two")
	if err := os.MkdirAll(secondSequence, 0700); err != nil {
		t.Fatal(err)
	}
	makeHolyGrailPreviewSequence(t, secondSequence)
	out.Reset()
	stderr.Reset()
	if code := Main([]string{"batch-holy-grail", batchRoot, "--workers", "2", "--lrt-preview", "visual"}, &out, &stderr); code != 0 {
		t.Fatalf("batch preview input exit=%d stderr=%s", code, stderr.String())
	}
	if count := strings.Count(out.String(), "Frames: 3; proposed sidecars:"); count != 2 {
		t.Fatalf("batch did not analyze both cache-backed sequences: %s", out.String())
	}
}

func TestHolyGrailSelectedLRTPreviewFailureDoesNotWrite(t *testing.T) {
	root := t.TempDir()
	frames := makeHolyGrailPreviewSequence(t, root)
	for _, frame := range frames {
		packet, err := PacketForFrame(frame)
		if err != nil {
			t.Fatal(err)
		}
		if err := packet.SetScalar("lrt:ExternalExposureDefault", "2"); err != nil {
			t.Fatal(err)
		}
		data, err := packet.Serialize()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(frame.OutputSidecar(), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	missing := lrtPreviewPath(frames[len(frames)-1].Image, "visual")
	if err := os.Remove(missing); err != nil {
		t.Fatal(err)
	}
	before := batchFileSnapshot(t, root)
	var out, stderr bytes.Buffer
	if code := Main([]string{"holy-grail", root, "--lrt-preview", "visual", "--write"}, &out, &stderr); code == 0 {
		t.Fatal("selected cache miss silently fell back to persisted lrt:ExternalExposureDefault")
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, root)) {
		t.Fatal("failed Holy Grail analysis changed source files or created backups")
	}

	for _, args := range [][]string{
		{"holy-grail", root, "--lrt-preview", "auto"},
		{"holy-grail", root, "--lrt-reference"},
	} {
		out.Reset()
		stderr.Reset()
		if code := Main(args, &out, &stderr); code == 0 {
			t.Fatalf("accepted invalid Holy Grail preview options %v", args)
		}
	}
}
