package lrtl

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
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

func TestLRTWizardSelectedRowsValidation(t *testing.T) {
	for _, test := range []struct {
		name  string
		rows  []int
		count int
	}{
		{name: "empty rows", rows: nil, count: 1},
		{name: "negative row", rows: []int{-1, 2}, count: 2},
		{name: "duplicate rows", rows: []int{2, 2}, count: 2},
		{name: "descending rows", rows: []int{3, 2}, count: 2},
		{name: "zero count", rows: []int{1, 3}, count: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := selectLRTWizardKeyframeIndicesForRows(test.rows, test.count); err == nil {
				t.Fatal("invalid selected rows or count were accepted")
			}
		})
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

func TestKeyframesSelectionUsesLRTSelectedRowsAndFullSequenceQuantity(t *testing.T) {
	root := t.TempDir()
	const frameCount = 251
	includedRows := map[int]bool{5: true, 15: true, 25: true, 35: true, 45: true, 55: true}
	for index := 0; index < frameCount; index++ {
		path := filepath.Join(root, "frame"+strconvTwo(index+1)+".NEF")
		if err := os.WriteFile(path, []byte("synthetic RAW; quantity comes from XMP"), 0600); err != nil {
			t.Fatal(err)
		}
		measurement := float64(index) / float64(frameCount-1) * 0.3
		writeTestXMP(t, path, map[string]string{
			"lrt:ExternalExposureDefault": strconv.FormatFloat(measurement*4, 'f', 8, 64),
		})
	}
	frames, err := DiscoverSequence(root)
	if err != nil {
		t.Fatal(err)
	}
	manifest := writeKeyframeSelectionManifest(t, root, frames, includedRows)

	measurements := make([]float64, frameCount)
	for index := range measurements {
		measurements[index] = float64(index) / float64(frameCount-1) * 0.3
	}
	if got := estimateAutoKeyframeCount(measurements, nil); got != 4 {
		t.Fatalf("test fixture quantity=%d, want 4", got)
	}

	var stdout, stderr bytes.Buffer
	if code := Main([]string{"keyframes", root, "--selection", manifest, "--write"}, &stdout, &stderr); code != 0 {
		t.Fatalf("selected keyframes exit=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "source: LRT XMP (lrt:ExternalExposureDefault / 4)): 4 total") {
		t.Fatalf("automatic quantity was not measured from the full sequence: %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "new suggestions: 4") {
		t.Fatalf("selected-row placement did not create four suggestions: %s", stdout.String())
	}
	wantRows := map[int]bool{5: true, 25: true, 45: true, 55: true}
	for index, frame := range frames {
		packet, err := PacketForSidecar(frame.OutputSidecar())
		if err != nil {
			t.Fatal(err)
		}
		if packet.IsKeyframe() != wantRows[index] {
			t.Errorf("frame %d keyframe=%v, want %v", index+1, packet.IsKeyframe(), wantRows[index])
		}
		if !wantRows[index] {
			backups, err := filepath.Glob(frame.OutputSidecar() + ".lrtl-*.bak")
			if err != nil {
				t.Fatal(err)
			}
			if len(backups) != 0 {
				t.Errorf("unselected frame %d unexpectedly got a backup", index+1)
			}
		}
	}
}

func TestKeyframesSelectionWithFewerThanTwoRowsFallsBackToWholeSequence(t *testing.T) {
	for _, selectedRows := range [][]int{{}, {2}} {
		name := "empty"
		if len(selectedRows) == 1 {
			name = "one-row"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			for index := 1; index <= 7; index++ {
				writeRenderFrame(t, filepath.Join(root, "frame"+strconvTwo(index)+".png"), "png", 8, 8)
			}
			frames, err := DiscoverSequence(root)
			if err != nil {
				t.Fatal(err)
			}
			included := make(map[int]bool, len(selectedRows))
			for _, index := range selectedRows {
				included[index] = true
			}
			manifest := writeKeyframeSelectionManifest(t, root, frames, included)
			var invalidOut, invalidErr bytes.Buffer
			invalidArgs := []string{"keyframes", root, "--selection", manifest, "--luminance-deviation"}
			if code := Main(invalidArgs, &invalidOut, &invalidErr); code == 0 || !strings.Contains(invalidErr.String(), "--selection currently applies") {
				t.Fatalf("incompatible selection mode was not rejected: exit=%d stderr=%s", code, invalidErr.String())
			}
			var stdout, stderr bytes.Buffer
			args := []string{"keyframes", root, "--evenly-spaced", "--count", "3", "--selection", manifest}
			if code := Main(args, &stdout, &stderr); code != 0 {
				t.Fatalf("keyframes exit=%d stderr=%s", code, stderr.String())
			}
			for _, selected := range []string{"frame01.png", "frame04.png", "frame07.png"} {
				if !strings.Contains(stdout.String(), selected) {
					t.Errorf("LRT fallback did not place %s across the full sequence: %s", selected, stdout.String())
				}
			}
			if strings.Contains(stdout.String(), "frame03.png") || strings.Contains(stdout.String(), "frame06.png") {
				t.Fatalf("unexpected keyframe position in fallback: %s", stdout.String())
			}
		})
	}
}

func TestKeyframesSelectionReplacementTreatsSubsetEndpointsLikeLRT(t *testing.T) {
	root := t.TempDir()
	for index := 0; index < 20; index++ {
		path := filepath.Join(root, "frame"+strconvTwo(index+1)+".png")
		writeRenderFrame(t, path, "png", 8, 8)
		rating := map[int]string{2: "2", 14: "3", 19: "3"}[index]
		metadata := map[string]string{}
		if rating != "" {
			metadata["xmp:Rating"] = rating
		}
		writeTestXMP(t, path, metadata)
	}
	frames, err := DiscoverSequence(root)
	if err != nil {
		t.Fatal(err)
	}
	manifest := writeKeyframeSelectionManifest(t, root, frames, map[int]bool{
		2: true, 8: true, 14: true, 19: true,
	})
	var stdout, stderr bytes.Buffer
	args := []string{"keyframes", root, "--count", "3", "--selection", manifest, "--replace-existing", "--write"}
	if code := Main(args, &stdout, &stderr); code != 0 {
		t.Fatalf("selected keyframe replacement exit=%d stderr=%s", code, stderr.String())
	}
	wantRatings := map[int]int{2: 4, 13: 4, 14: 3, 19: 4}
	for index, frame := range frames {
		packet, err := PacketForSidecar(frame.OutputSidecar())
		if err != nil {
			t.Fatal(err)
		}
		rating, found := packet.Rating()
		want, wantFound := wantRatings[index]
		if found != wantFound || (found && rating != want) {
			t.Errorf("frame %d rating=(%d,%v), want (%d,%v)", index+1, rating, found, want, wantFound)
		}
		_, shouldBackUp := map[int]bool{2: true, 13: true, 19: true}[index]
		backups, err := filepath.Glob(frame.OutputSidecar() + ".lrtl-*.bak")
		if err != nil {
			t.Fatal(err)
		}
		if (len(backups) == 1) != shouldBackUp {
			t.Errorf("frame %d backup count=%d, want backup=%v", index+1, len(backups), shouldBackUp)
		}
		if index == 2 || index == 19 {
			if err := UnmarkKeyframe(packet, false); err != nil {
				t.Fatal(err)
			}
			wantPreviousRating := 2
			if index == 19 {
				wantPreviousRating = 3
			}
			if restored, found := packet.Rating(); !found || restored != wantPreviousRating {
				t.Fatalf("unmark did not restore selected endpoint's old rating: (%d,%v), want %d", restored, found, wantPreviousRating)
			}
		}
	}
}

func writeKeyframeSelectionManifest(t *testing.T, directory string, frames []Frame, includedRows map[int]bool) string {
	t.Helper()
	manifest := &sequenceSelectionManifest{
		Version:   selectionManifestVersion,
		Directory: directory,
		Frames:    make([]longTermFrameMetrics, len(frames)),
	}
	for index, frame := range frames {
		info, err := os.Stat(frame.Image)
		if err != nil {
			t.Fatal(err)
		}
		manifest.Frames[index] = longTermFrameMetrics{
			Image: filepath.Base(frame.Image), Included: includedRows[frame.Index],
			SizeBytes: info.Size(), ModifiedUnixNano: info.ModTime().UnixNano(),
		}
	}
	path := filepath.Join(directory, "keyframe-selection.json")
	if err := writeSelectionManifest(path, manifest, false); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestKeyframesReplaceExistingMatchesLRTEndpointOverwriteAndCanRestoreHG(t *testing.T) {
	root := t.TempDir()
	initialRatings := map[int]int{0: 2, 2: 4, 5: 3, 7: 5, 9: 5}
	imageContents := make([][]byte, 10)
	for index := range imageContents {
		path := filepath.Join(root, "frame"+strconvTwo(index+1)+".jpg")
		imageContents[index] = []byte("synthetic keyframe test image")
		if err := os.WriteFile(path, imageContents[index], 0600); err != nil {
			t.Fatal(err)
		}
		metadata := map[string]string{}
		if rating, found := initialRatings[index]; found {
			metadata["xmp:Rating"] = strconv.Itoa(rating)
		}
		writeTestXMP(t, path, metadata)
	}

	var stdout, stderr bytes.Buffer
	args := []string{"keyframes", root, "--evenly-spaced", "--count", "3", "--replace-existing", "--write"}
	if code := Main(args, &stdout, &stderr); code != 0 {
		t.Fatalf("keyframes replace exit=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "new suggestions: 3") {
		t.Fatalf("unexpected replacement plan: %s", stdout.String())
	}
	wantRatings := map[int]int{0: 4, 4: 4, 5: 3, 7: 5, 9: 4}
	frames, err := DiscoverSequence(root)
	if err != nil {
		t.Fatal(err)
	}
	for index, frame := range frames {
		backups, err := filepath.Glob(frame.OutputSidecar() + ".lrtl-*.bak")
		if err != nil {
			t.Fatal(err)
		}
		wantBackup := index == 0 || index == 2 || index == 4 || index == 9
		if (len(backups) == 1) != wantBackup {
			t.Fatalf("frame %d has %d backups, want backup=%v", index+1, len(backups), wantBackup)
		}
		if index == 0 {
			backupPacket, err := PacketForSidecar(backups[0])
			if err != nil {
				t.Fatalf("reading original Holy Grail endpoint backup: %v", err)
			}
			if rating, found := backupPacket.Rating(); !found || rating != 2 {
				t.Fatalf("endpoint backup rating=(%d,%v), want original 2-star rating", rating, found)
			}
		}
	}
	for index, frame := range frames {
		packet, err := PacketForSidecar(frame.OutputSidecar())
		if err != nil {
			t.Fatal(err)
		}
		rating, found := packet.Rating()
		want, shouldHaveRating := wantRatings[index]
		if found != shouldHaveRating || (found && rating != want) {
			t.Fatalf("frame %d rating=(%d,%v), want (%d,%v)", index+1, rating, found, want, shouldHaveRating)
		}
		if index == 0 || index == 4 || index == 9 {
			if packet.KeyframeKind() != "default" {
				t.Fatalf("frame %d should be marked as a default keyframe, got %q", index+1, packet.KeyframeKind())
			}
		}
		if index == 0 {
			restored := &XMPPacket{Document: packet.Document.Copy()}
			if err := UnmarkKeyframe(restored, false); err != nil {
				t.Fatalf("unmarking the overwritten Holy Grail endpoint: %v", err)
			}
			if rating, found := restored.Rating(); !found || rating != 2 {
				t.Fatalf("unmarking did not restore the previous 2-star rating: (%d,%v)", rating, found)
			}
		}
		imageData, err := os.ReadFile(frame.Image)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(imageData, imageContents[index]) {
			t.Fatalf("keyframe marking modified source image %s", frame.Image)
		}
	}

	stdout.Reset()
	stderr.Reset()
	if code := Main(args, &stdout, &stderr); code != 0 {
		t.Fatalf("repeated keyframes replace exit=%d stderr=%s", code, stderr.String())
	}
	for index, frame := range frames {
		packet, err := PacketForSidecar(frame.OutputSidecar())
		if err != nil {
			t.Fatal(err)
		}
		rating, found := packet.Rating()
		want, shouldHaveRating := wantRatings[index]
		if found != shouldHaveRating || (found && rating != want) {
			t.Fatalf("rerun changed frame %d rating to (%d,%v), want (%d,%v)", index+1, rating, found, want, shouldHaveRating)
		}
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

func TestLRTQuantityRangeSkipsFirstFrameButUsesItForHGPairCorrection(t *testing.T) {
	for _, test := range []struct {
		name         string
		measurements []float64
		markers      []bool
		wantRange    float64
		wantCount    int
	}{
		{name: "first_frame_only_spike_is_ignored", measurements: []float64{2, 0, 0}, markers: []bool{false, false, false}, wantRange: 0, wantCount: 1},
		{name: "second_frame_spike_is_included", measurements: []float64{0, 2, 0}, markers: []bool{false, false, false}, wantRange: 2, wantCount: 1},
		{name: "last_frame_spike_is_included", measurements: []float64{0, 0, 2}, markers: []bool{false, false, false}, wantRange: 2, wantCount: 1},
		{name: "first_value_compensates_adjacent_hg_pair", measurements: []float64{0, 2, 2}, markers: []bool{true, true, false}, wantRange: 0, wantCount: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := lrtKeyframeMeasurementRange(test.measurements, test.markers); got != test.wantRange {
				t.Fatalf("measurement range=%.17g, want %.17g", got, test.wantRange)
			}
			if got := estimateAutoKeyframeCount(test.measurements, test.markers); got != test.wantCount {
				t.Fatalf("automatic keyframe count=%d, want %d", got, test.wantCount)
			}
		})
	}
}

func TestAutomaticKeyframeCountMatchesLRT755Oracle(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "lrt-keyframe-quantity-7.5.5.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Source string `json:"source"`
		Cases  []struct {
			Name             string    `json:"name"`
			Measurements     []float64 `json:"measurements"`
			HolyGrailRatings []int     `json:"holyGrailRatings"`
			LRTCount         int       `json:"lrtCount"`
			LRTIndices       []int     `json:"lrtIndices"`
		} `json:"cases"`
		SweepCases []struct {
			Name          string  `json:"name"`
			Frames        int     `json:"frames"`
			Offset        float64 `json:"offset"`
			Range         float64 `json:"range"`
			StepAt        int     `json:"stepAt"`
			RatingIndices []int   `json:"ratingIndices"`
			Ratings       []int   `json:"ratings"`
			LRTRange      float64 `json:"lrtRange"`
			LRTCount      int     `json:"lrtCount"`
			LRTIndices    []int   `json:"lrtIndices"`
		} `json:"quantitySweepCases"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Source != "LRTimelapse 7.5.5 build 1059 installed runtime" {
		t.Fatalf("unexpected oracle source %q", fixture.Source)
	}
	if len(fixture.Cases) != 8 {
		t.Fatalf("LRT oracle fixture has %d cases, want 8", len(fixture.Cases))
	}
	for _, test := range fixture.Cases {
		t.Run(test.Name, func(t *testing.T) {
			if len(test.Measurements) == 0 || len(test.HolyGrailRatings) != len(test.Measurements) {
				t.Fatalf("invalid oracle input dimensions: measurements=%d ratings=%d", len(test.Measurements), len(test.HolyGrailRatings))
			}
			markers := make([]bool, len(test.HolyGrailRatings))
			for index, rating := range test.HolyGrailRatings {
				if rating != 0 && rating != 2 && rating != 3 {
					t.Fatalf("unexpected Holy Grail rating at frame %d: %d", index+1, rating)
				}
				markers[index] = rating == 2 || rating == 3
			}
			if got := estimateAutoKeyframeCount(test.Measurements, markers); got != test.LRTCount {
				t.Fatalf("automatic keyframe count=%d, installed LRT 7.5.5 returned %d", got, test.LRTCount)
			}
			indices, err := selectLRTWizardKeyframeIndices(len(test.Measurements), test.LRTCount)
			if err != nil {
				t.Fatal(err)
			}
			if !slicesEqual(indices, test.LRTIndices) {
				t.Fatalf("wizard selected indices %v, installed LRT 7.5.5 returned %v", indices, test.LRTIndices)
			}
		})
	}
	if len(fixture.SweepCases) != 747 {
		t.Fatalf("LRT quantity boundary sweep has %d cases, want 747", len(fixture.SweepCases))
	}
	for _, test := range fixture.SweepCases {
		t.Run(test.Name, func(t *testing.T) {
			if test.Frames < 2 || test.StepAt < -1 || test.StepAt >= test.Frames {
				t.Fatalf("invalid generated input dimensions: frames=%d stepAt=%d", test.Frames, test.StepAt)
			}
			if len(test.RatingIndices) != len(test.Ratings) {
				t.Fatalf("rating index/value counts differ: %d vs %d", len(test.RatingIndices), len(test.Ratings))
			}
			measurements := make([]float64, test.Frames)
			for index := range measurements {
				if test.StepAt >= 0 {
					if index >= test.StepAt {
						measurements[index] = test.Offset + test.Range
					} else {
						measurements[index] = test.Offset
					}
				} else {
					measurements[index] = test.Offset + test.Range*float64(index)/float64(test.Frames-1)
				}
			}
			markers := make([]bool, test.Frames)
			for marker, index := range test.RatingIndices {
				if index < 0 || index >= test.Frames {
					t.Fatalf("rating marker %d has out-of-range frame %d", marker, index)
				}
				rating := test.Ratings[marker]
				if rating != 2 && rating != 3 && rating != 4 && rating != 5 {
					t.Fatalf("unexpected rating at frame %d: %d", index+1, rating)
				}
				markers[index] = rating == 2 || rating == 3
			}
			if got := lrtKeyframeMeasurementRange(measurements, markers); got != test.LRTRange {
				t.Fatalf("measurement range=%.17g, installed LRT 7.5.5 returned %.17g", got, test.LRTRange)
			}
			if got := estimateAutoKeyframeCount(measurements, markers); got != test.LRTCount {
				t.Fatalf("automatic keyframe count=%d, installed LRT 7.5.5 returned %d", got, test.LRTCount)
			}
			indices, err := selectLRTWizardKeyframeIndices(test.Frames, test.LRTCount)
			if err != nil {
				t.Fatal(err)
			}
			if !slicesEqual(indices, test.LRTIndices) {
				t.Fatalf("wizard selected indices %v, installed LRT 7.5.5 returned %v", indices, test.LRTIndices)
			}
		})
	}
}

func TestLRTWizardCollisionHandlingMatchesOracle(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "lrt-keyframe-quantity-7.5.5.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Source string `json:"source"`
		Cases  []struct {
			Name            string `json:"name"`
			Count           int    `json:"count"`
			TargetRating    int    `json:"targetRating"`
			InitialRatings  []int  `json:"initialRatings"`
			LRTFinalRatings []int  `json:"lrtRatings"`
		} `json:"placementCases"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Source != "LRTimelapse 7.5.5 build 1059 installed runtime" {
		t.Fatalf("unexpected oracle source %q", fixture.Source)
	}
	if len(fixture.Cases) != 8 {
		t.Fatalf("LRT placement oracle has %d cases, want 8", len(fixture.Cases))
	}
	for _, test := range fixture.Cases {
		t.Run(test.Name, func(t *testing.T) {
			if len(test.InitialRatings) == 0 || len(test.LRTFinalRatings) != len(test.InitialRatings) {
				t.Fatalf("invalid rating dimensions: initial=%d LRT=%d", len(test.InitialRatings), len(test.LRTFinalRatings))
			}
			packets := make([]*XMPPacket, len(test.InitialRatings))
			for index, rating := range test.InitialRatings {
				if rating != 0 {
					if rating < 2 || rating > 5 {
						t.Fatalf("unexpected initial rating at frame %d: %d", index+1, rating)
					}
					packet := EmptyXMPPacket()
					if err := packet.SetScalar("xmp:Rating", strconv.Itoa(rating)); err != nil {
						t.Fatal(err)
					}
					packets[index] = packet
				} else {
					packets[index] = EmptyXMPPacket()
				}
			}
			indices, err := selectLRTWizardKeyframeIndices(len(packets), test.Count)
			if err != nil {
				t.Fatal(err)
			}
			selected := placeLRTWizardKeyframeIndices(indices, packets, make([]bool, len(packets)), test.TargetRating)
			finalRatings := append([]int(nil), test.InitialRatings...)
			for index, rating := range finalRatings {
				if rating == test.TargetRating {
					finalRatings[index] = 0
				}
			}
			for _, index := range selected {
				finalRatings[index] = test.TargetRating
			}
			if !reflect.DeepEqual(finalRatings, test.LRTFinalRatings) {
				t.Fatalf("final ratings=%v, installed LRT 7.5.5 returned %v", finalRatings, test.LRTFinalRatings)
			}
		})
	}
}

func TestLRTWizardSelectedRowsMatchOracle(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "lrt-keyframe-quantity-7.5.5.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Source string `json:"source"`
		Cases  []struct {
			Name           string `json:"name"`
			Frames         int    `json:"frames"`
			Count          int    `json:"count"`
			TargetRating   int    `json:"targetRating"`
			RowIndices     []int  `json:"rowIndices"`
			InitialRatings []int  `json:"initialRatings"`
			LRTRatings     []int  `json:"lrtRatings"`
		} `json:"selectedRowCases"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Source != "LRTimelapse 7.5.5 build 1059 installed runtime" {
		t.Fatalf("unexpected oracle source %q", fixture.Source)
	}
	if len(fixture.Cases) != 6 {
		t.Fatalf("LRT selected-row oracle has %d cases, want 6", len(fixture.Cases))
	}
	for _, test := range fixture.Cases {
		t.Run(test.Name, func(t *testing.T) {
			if test.Frames < 1 || len(test.InitialRatings) != test.Frames || len(test.LRTRatings) != test.Frames {
				t.Fatalf("invalid oracle frame dimensions: frames=%d initial=%d LRT=%d", test.Frames, len(test.InitialRatings), len(test.LRTRatings))
			}
			indices, err := selectLRTWizardKeyframeIndicesForRows(test.RowIndices, test.Count)
			if err != nil {
				t.Fatal(err)
			}
			packets := make([]*XMPPacket, test.Frames)
			for index, rating := range test.InitialRatings {
				packets[index] = EmptyXMPPacket()
				if rating != 0 {
					if err := packets[index].SetScalar("xmp:Rating", strconv.Itoa(rating)); err != nil {
						t.Fatal(err)
					}
				}
			}
			selected := placeLRTWizardKeyframeIndicesForRows(indices, test.RowIndices, packets,
				make([]bool, test.Frames), test.TargetRating)
			finalRatings := append([]int(nil), test.InitialRatings...)
			for index, rating := range finalRatings {
				if rating == test.TargetRating {
					finalRatings[index] = 0
				}
			}
			for _, index := range selected {
				finalRatings[index] = test.TargetRating
			}
			if !reflect.DeepEqual(finalRatings, test.LRTRatings) {
				t.Fatalf("final ratings=%v, installed LRT 7.5.5 returned %v", finalRatings, test.LRTRatings)
			}
		})
	}
}
