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

func TestSelectKeyframeIndicesFollowLuminanceCurve(t *testing.T) {
	curve := []float64{0, 0, 0, 3, 3, 3, 3}
	indices, err := selectKeyframeIndices(curve, 3, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := indices, []int{2, 3, 4}; !slicesEqual(got, want) {
		t.Fatalf("got interior suggestions %v, want %v", got, want)
	}

	linear := []float64{0, 1, 2, 3, 4, 5, 6}
	indices, err = selectKeyframeIndices(linear, 3, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := indices, []int{1, 3, 4}; !slicesEqual(got, want) {
		t.Fatalf("linear curve selected %v, want interior candidates %v", got, want)
	}
}

func TestSelectOneKeyframeUsesMedianLuminance(t *testing.T) {
	indices, err := selectKeyframeIndices([]float64{-2, -1, 0, 1, 2}, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(indices) != 1 || indices[0] != 2 {
		t.Fatalf("got %v, want [2]", indices)
	}
}

func TestSelectKeyframeIndicesValidatesCountAndCurve(t *testing.T) {
	for _, count := range []int{0, 4} {
		if _, err := selectKeyframeIndices([]float64{0, 1, 2}, count, 1); err == nil {
			t.Errorf("expected invalid count %d to fail", count)
		}
	}
	if _, err := selectKeyframeIndices([]float64{0, math.NaN()}, 1, 1); err == nil {
		t.Fatal("expected non-finite luminance to fail")
	}
}

func TestAnalyzeLuminanceSequenceRejectsRAW(t *testing.T) {
	directory := t.TempDir()
	raw := filepath.Join(directory, "frame1.cr3")
	if err := os.WriteFile(raw, []byte("raw"), 0600); err != nil {
		t.Fatal(err)
	}
	frames, err := DiscoverSequence(directory)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := analyzeLuminanceSequence(frames); err == nil || !strings.Contains(err.Error(), "requires --raw-preview or --raw-develop") {
		t.Fatalf("expected unsupported RAW error, got %v", err)
	}
}

func TestKeyframesUsesSelectedLRTPreviewInsteadOfSavedPreviewEV(t *testing.T) {
	root := t.TempDir()
	const frameCount = 151
	for index := 0; index < frameCount; index++ {
		level := uint8(5 + index*240/(frameCount-1))
		path := filepath.Join(root, "frame"+strconvTwo(index+1)+".NEF")
		if err := os.WriteFile(path, []byte("not a decodable RAW; use the selected LRT preview"), 0600); err != nil {
			t.Fatal(err)
		}
		writeTestXMP(t, path, map[string]string{"lrt:ExternalExposureDefault": "0"})
		preview := image.NewGray(image.Rect(0, 0, 32, 16))
		for y := 0; y < preview.Bounds().Dy(); y++ {
			for x := 0; x < preview.Bounds().Dx(); x++ {
				preview.SetGray(x, y, color.Gray{Y: level})
			}
		}
		var encoded bytes.Buffer
		if err := jpeg.Encode(&encoded, preview, &jpeg.Options{Quality: 100}); err != nil {
			t.Fatal(err)
		}
		cache := lrtPreviewPath(path, "visual")
		if err := os.MkdirAll(filepath.Dir(cache), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(cache, encoded.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
	}
	before := batchFileSnapshot(t, root)
	var out, stderr bytes.Buffer
	if code := Main([]string{"keyframes", root}, &out, &stderr); code != 0 {
		t.Fatalf("XMP-only estimate exit=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(out.String(), "source: LRT XMP (lrt:ExternalExposureDefault / 4)): 1 total") {
		t.Fatalf("expected the saved uniform Preview EV estimate, got: %s", out.String())
	}
	out.Reset()
	stderr.Reset()
	if code := Main([]string{"keyframes", root, "--lrt-preview", "visual", "--preview-width-camera", "6"}, &out, &stderr); code != 0 {
		t.Fatalf("LRT-preview estimate exit=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(out.String(), "source: LRT Preview EV histogram): 3 total") {
		t.Fatalf("selected LRT cache did not replace saved Preview EV measurements: %s", out.String())
	}
	out.Reset()
	stderr.Reset()
	if code := Main([]string{"keyframes", root, "--preview-width-camera", "1"}, &out, &stderr); code == 0 || !strings.Contains(stderr.String(), "--preview-width-camera must be at least 2 pixels") {
		t.Fatalf("invalid keyframe preview width did not fail validation: exit=%d stderr=%s", code, stderr.String())
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, root)) {
		t.Fatal("keyframe preview modified XMP or LRT preview caches")
	}
	out.Reset()
	stderr.Reset()
	if code := Main([]string{"keyframes", root, "--evenly-spaced", "--lrt-preview", "visual"}, &out, &stderr); code == 0 {
		t.Fatal("evenly-spaced mode accepted an image input option it does not use")
	}
	out.Reset()
	stderr.Reset()
	if code := Main([]string{"keyframes", root, "--evenly-spaced", "--preview-width-camera", "6"}, &out, &stderr); code == 0 {
		t.Fatal("evenly-spaced mode accepted a preview-width option it does not use")
	}
	if err := os.Remove(lrtPreviewPath(filepath.Join(root, "frame01.NEF"), "visual")); err != nil {
		t.Fatal(err)
	}
	beforeMissingCache := batchFileSnapshot(t, root)
	out.Reset()
	stderr.Reset()
	if code := Main([]string{"keyframes", root, "--lrt-preview", "visual"}, &out, &stderr); code == 0 {
		t.Fatal("missing explicitly selected LRT cache fell back to saved Preview EV")
	}
	if !reflect.DeepEqual(beforeMissingCache, batchFileSnapshot(t, root)) {
		t.Fatal("missing LRT preview cache changed source files")
	}
}

func TestKeyframesAutoQuantityFallsBackToLRTPreviewEV(t *testing.T) {
	root := t.TempDir()
	const frameCount = 201
	for index := 0; index < frameCount; index++ {
		level := uint8(128 + index*10/(frameCount-1))
		path := filepath.Join(root, "frame"+strconvTwo(index+1)+".png")
		writePreviewEVFixture(t, path, previewEVGray(level, 20, 20))
	}
	frames, err := DiscoverSequence(root)
	if err != nil {
		t.Fatal(err)
	}
	previewMeasurements, err := analyzePreviewEVSequence(frames, imageInputOptions{})
	if err != nil {
		t.Fatal(err)
	}
	wantCount := estimateAutoKeyframeCount(previewMeasurements, nil)
	if wantCount != 1 {
		t.Fatalf("fixture Preview EV range produced %d suggestions; want 1", wantCount)
	}
	oldLuminanceMeasurements, err := analyzeLuminanceSequence(frames)
	if err != nil {
		t.Fatal(err)
	}
	if oldCount := estimateAutoKeyframeCount(oldLuminanceMeasurements, nil); oldCount == wantCount {
		t.Fatalf("fixture does not distinguish Preview EV fallback from old log-luminance fallback: both suggest %d", wantCount)
	}
	var stdout, stderr bytes.Buffer
	if code := Main([]string{"keyframes", root}, &stdout, &stderr); code != 0 {
		t.Fatalf("keyframes exit=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "source: LRT Preview EV histogram): 1 total") {
		t.Fatalf("keyframe quantity did not use the LRT Preview EV measurement: %s", stdout.String())
	}
}

func TestKeyframesCLIPreviewsThenMarksSuggestedFrames(t *testing.T) {
	directory := t.TempDir()
	for index := 1; index <= 5; index++ {
		ext := ".jpg"
		format := "jpeg"
		if index == 2 {
			ext = ".png"
			format = "png"
		} else if index == 4 {
			ext = ".tiff"
			format = "tiff"
		}
		path := filepath.Join(directory, "frame"+strconvTwo(index)+ext)
		writeRenderFrame(t, path, format, 16, 16)
	}
	frames, err := DiscoverSequence(directory)
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := Main([]string{"keyframes", directory}, &stdout, &stderr); code != 0 {
		t.Fatalf("keyframes preview exit=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "new suggestions: 1") || !strings.Contains(stdout.String(), "Preview only") {
		t.Fatalf("unexpected keyframe preview: %s", stdout.String())
	}
	for _, frame := range frames {
		if _, err := os.Stat(frame.OutputSidecar()); !os.IsNotExist(err) {
			t.Fatalf("preview unexpectedly wrote %s", frame.OutputSidecar())
		}
	}

	if code := Main([]string{"keyframes", directory, "--count", "3", "--write"}, &stdout, &stderr); code != 0 {
		t.Fatalf("keyframes write exit=%d stderr=%s", code, stderr.String())
	}
	marked := 0
	wantMarked := map[int]bool{0: true, 2: true, 4: true}
	for index, frame := range frames {
		packet, err := PacketForSidecar(frame.OutputSidecar())
		if err != nil {
			t.Fatal(err)
		}
		if packet.IsKeyframe() {
			marked++
			if !wantMarked[index] {
				t.Fatalf("unexpected keyframe at index %d", index)
			}
			if rating, valid := packet.Rating(); !valid || rating != 4 || packet.KeyframeKind() != "default" {
				t.Fatalf("unexpected keyframe marker on %s: rating=%d valid=%v kind=%q", frame.Image, rating, valid, packet.KeyframeKind())
			}
		} else if wantMarked[index] {
			t.Errorf("missing keyframe at index %d", index)
		}
	}
	if marked != 3 {
		t.Fatalf("got %d marked frames, want 3", marked)
	}
}

func slicesEqual(left, right []int) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func TestLRTWizardSpacingIncludesEndpointsAndIntegerRounding(t *testing.T) {
	for _, test := range []struct {
		name          string
		frames, count int
		want          []int
	}{
		{"one", 1, 1, []int{0}},
		{"quantity_one", 10, 1, []int{0}},
		{"three", 5, 3, []int{0, 2, 4}},
		{"integer_step", 10, 4, []int{0, 3, 6, 9}},
		{"short_sequence_exceeds_quantity", 6, 5, []int{0, 1, 2, 3, 4, 5}},
		{"quantity_exceeds_frames", 1, 5, []int{0}},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := selectLRTWizardKeyframeIndices(test.frames, test.count)
			if err != nil || !slicesEqual(got, test.want) {
				t.Fatalf("indices=%v want=%v err=%v", got, test.want, err)
			}
		})
	}
	for _, input := range [][2]int{{0, 1}, {5, 0}, {-1, 2}} {
		if _, err := selectLRTWizardKeyframeIndices(input[0], input[1]); err == nil {
			t.Fatalf("invalid frames/count accepted: %v", input)
		}
	}
}

func TestLRTWizardCountAndHolyGrailStepCompensation(t *testing.T) {
	if count := estimateAutoKeyframeCount(nil, nil); count != 0 {
		t.Fatalf("empty curve quantity=%d", count)
	}
	if count := estimateAutoKeyframeCount(make([]float64, 1001), nil); count != 4 {
		t.Fatalf("1000-frame span should supply 4 minimum anchors, got %d", count)
	}
	for _, test := range []struct{ frames, raw, compensated int }{{500, 9, 2}, {1000, 19, 3}} {
		curve := make([]float64, test.frames)
		for index := test.frames / 2; index < len(curve); index++ {
			curve[index] = 2
		}
		// The raw jump is constrained by 50-frame spacing: floor(span/50).
		// An adjacent HG pair removes it. The remaining flat curve still
		// needs floor(span/250) anchors, raised to at least two for HG.
		if count := estimateAutoKeyframeCount(curve, nil); count != test.raw {
			t.Fatalf("frames=%d uncompensated quantity=%d want %d", test.frames, count, test.raw)
		}
		markers := make([]bool, len(curve))
		markers[test.frames/2-1], markers[test.frames/2] = true, true
		if count := estimateAutoKeyframeCount(curve, markers); count != test.compensated {
			t.Fatalf("frames=%d compensated quantity=%d want %d", test.frames, count, test.compensated)
		}
	}
}
