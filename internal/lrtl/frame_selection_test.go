package lrtl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func frameSelectionFixture(t *testing.T) (string, []Frame) {
	t.Helper()
	frames := cropFixture(t, 6)
	ratings := []string{"", "0", "4", "5", "-1", "3"}
	for i, frame := range frames {
		if i == 0 {
			if err := os.Remove(frame.Sidecar); err != nil {
				t.Fatal(err)
			}
			continue
		}
		path := frame.OutputSidecar()
		if i == 3 {
			path = strings.TrimSuffix(frame.Image, filepath.Ext(frame.Image)) + ".xmp"
		}
		packet := EmptyXMPPacket()
		if err := packet.SetScalar("xmp:Rating", ratings[i]); err != nil {
			t.Fatal(err)
		}
		if err := packet.SetScalar("dc:description", fmt.Sprint("preserve-", i)); err != nil {
			t.Fatal(err)
		}
		if _, err := WritePacket(path, packet, false); err != nil {
			t.Fatal(err)
		}
		stamp := time.Unix(1_800_000_000+int64(i), 0)
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(frame.Image, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	dir := filepath.Dir(frames[0].Image)
	actual, err := DiscoverSequence(dir)
	if err != nil {
		t.Fatal(err)
	}
	return dir, actual
}

func TestFrameSelectionIntersectionAndInverse(t *testing.T) {
	dir, frames := frameSelectionFixture(t)
	for _, tc := range []struct {
		options frameSelectionOptions
		want    []int
	}{
		{frameSelectionOptions{}, []int{0, 1, 2, 3, 4, 5}},
		{frameSelectionOptions{Ratings: []int{0}}, []int{0, 1}},
		{frameSelectionOptions{Ratings: []int{4, 5}}, []int{2, 3}},
		{frameSelectionOptions{Ratings: []int{-1}}, []int{4}},
		{frameSelectionOptions{Ratings: []int{3}, Invert: true}, []int{0, 1, 2, 3, 4}},
		{frameSelectionOptions{FilenameContains: strings.ToUpper(filepath.Base(frames[2].Image))}, []int{2}},
		{frameSelectionOptions{FilenameContains: filepath.Base(frames[2].Image), Ratings: []int{5}}, []int{}},
		{frameSelectionOptions{Invert: true}, []int{}},
	} {
		selected, err := filterSequenceFrames(frames, dir, tc.options)
		if err != nil {
			t.Fatal(err)
		}
		indices := make([]int, len(selected))
		for i, frame := range selected {
			indices[i] = frame.Index
		}
		if !reflect.DeepEqual(indices, tc.want) {
			t.Fatalf("options %+v got %v want %v", tc.options, indices, tc.want)
		}
	}
}

func TestSelectManifestCompatibilityAndEmptyInverse(t *testing.T) {
	dir, frames := frameSelectionFixture(t)
	path := filepath.Join(t.TempDir(), "selection.json")
	var out, stderr bytes.Buffer
	if Main([]string{"select", dir, "--ratings", "4,5", "--output", path}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	selected, err := loadSelectedFrames(frames, path, dir)
	if err != nil || len(selected) != 2 || selected[0].Index != 2 || selected[1].Index != 3 {
		t.Fatalf("selection manifest: %v %v", selected, err)
	}
	for _, command := range []string{"export", "preview-images"} {
		destination := filepath.Join(t.TempDir(), "frames")
		if Main([]string{command, dir, "--selection", path, "--output", destination}, &out, &stderr) != 0 {
			t.Fatalf("%s selection: %s", command, &stderr)
		}
		data, err := os.ReadFile(filepath.Join(destination, "frames.json"))
		if err != nil {
			t.Fatal(err)
		}
		var manifest struct {
			Frames []exportedFrame `json:"frames"`
		}
		if err := json.Unmarshal(data, &manifest); err != nil {
			t.Fatal(err)
		}
		if len(manifest.Frames) != 2 || manifest.Frames[0].Source != filepath.Base(frames[2].Image) || manifest.Frames[1].Source != filepath.Base(frames[3].Image) {
			t.Fatalf("%s used wrong frames: %v", command, manifest.Frames)
		}
	}
	selected, err = filterSequenceFrames(frames, dir, frameSelectionOptions{Manifest: path, FilenameContains: filepath.Base(frames[2].Image), Invert: true})
	if err != nil || len(selected) != 5 {
		t.Fatalf("intersection inverse: %v %v", selected, err)
	}
	for _, frame := range selected {
		if frame.Index == 2 {
			t.Fatal("inverted the filters independently instead of their intersection")
		}
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if Main([]string{"select", dir, "--output", path}, &out, &stderr) == 0 {
		t.Fatal("overwrote manifest without explicit option")
	}
	assertPreviewEVFileBytes(t, path, before)
	if Main([]string{"select", dir, "--filename-contains", "no match", "--output", path, "--overwrite"}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	if _, err := loadSelectedFrames(frames, path, dir); err == nil {
		t.Fatal("render's all-excluded manifest guard regressed")
	}
	selected, err = filterSequenceFrames(frames, dir, frameSelectionOptions{Manifest: path, Invert: true})
	if err != nil || len(selected) != 6 {
		t.Fatal("cannot invert an empty input selection")
	}
	destination := filepath.Join(t.TempDir(), "inverted-copy")
	if Main([]string{"copy-sequence", dir, "--selection", path, "--output", destination, "--write"}, &out, &stderr) == 0 {
		t.Fatal("copied an empty selection")
	}
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		t.Fatal("empty copy created a destination")
	}
	if Main([]string{"copy-sequence", dir, "--selection", path, "--invert", "--output", destination, "--write"}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	copied, err := DiscoverSequence(destination)
	if err != nil || len(copied) != 6 {
		t.Fatal("empty-manifest inverse did not copy every frame")
	}
	if err := os.WriteFile(frames[0].Image, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := filterSequenceFrames(frames, dir, frameSelectionOptions{Manifest: path, Invert: true}); err == nil {
		t.Fatal("inversion bypassed stale-manifest validation")
	}
}

func TestFrameSelectionInvalidRatingsAndCLI(t *testing.T) {
	dir, frames := frameSelectionFixture(t)
	before := batchFileSnapshot(t, dir)
	for _, ratings := range []string{"", "6", "-2", "4.5", "NaN", "4,4", "4,", "invalid"} {
		var out, stderr bytes.Buffer
		if Main([]string{"select", dir, "--ratings", ratings}, &out, &stderr) == 0 {
			t.Fatalf("accepted rating filter %q", ratings)
		}
		if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
			t.Fatal("invalid filter wrote source files")
		}
	}
	for _, bad := range []string{"NaN", "6", "-2", "4.5", "invalid"} {
		packet := EmptyXMPPacket()
		if err := packet.SetScalar("xmp:Rating", bad); err != nil {
			t.Fatal(err)
		}
		persistExposurePacket(t, frames[5], packet)
		if _, err := filterSequenceFrames(frames, dir, frameSelectionOptions{Ratings: []int{4}}); err == nil {
			t.Fatalf("accepted source rating %s", bad)
		}
	}
}

func TestFrameSelectionEmbeddedRatingAndSidecarPriority(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "embedded.jpg")
	packet := EmptyXMPPacket()
	if err := packet.SetScalar("xmp:Rating", "4"); err != nil {
		t.Fatal(err)
	}
	data, err := packet.Serialize()
	if err != nil {
		t.Fatal(err)
	}
	imageBytes := writeEmbeddedTransitionJPEG(t, path, data)
	frames, err := DiscoverSequence(dir)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := filterSequenceFrames(frames, dir, frameSelectionOptions{Ratings: []int{4}})
	if err != nil || len(selected) != 1 {
		t.Fatal("embedded rating was not selected")
	}
	destination := filepath.Join(t.TempDir(), "copy")
	plan, err := planSequenceCopy(dir, destination, selected)
	if err != nil {
		t.Fatal(err)
	}
	if err := executeSequenceCopy(plan, nil); err != nil {
		t.Fatal(err)
	}
	assertPreviewEVFileBytes(t, filepath.Join(destination, "embedded.jpg"), imageBytes)
	if err := packet.SetScalar("xmp:Rating", "5"); err != nil {
		t.Fatal(err)
	}
	if _, err := WritePacket(path+".xmp", packet, false); err != nil {
		t.Fatal(err)
	}
	frames, err = DiscoverSequence(dir)
	if err != nil {
		t.Fatal(err)
	}
	selected, err = filterSequenceFrames(frames, dir, frameSelectionOptions{Ratings: []int{4}})
	if err != nil || len(selected) != 0 {
		t.Fatal("embedded rating overrode external XMP")
	}
	selected, err = filterSequenceFrames(frames, dir, frameSelectionOptions{Ratings: []int{5}})
	if err != nil || len(selected) != 1 {
		t.Fatal("external rating not selected")
	}
}

func TestFrameSelectionDoesNotReadExcludedMetadata(t *testing.T) {
	dir, frames := frameSelectionFixture(t)
	if err := os.WriteFile(frames[5].Sidecar, []byte("broken XML"), 0600); err != nil {
		t.Fatal(err)
	}
	selected, err := filterSequenceFrames(frames, dir, frameSelectionOptions{FilenameContains: filepath.Base(frames[2].Image), Ratings: []int{4}})
	if err != nil || len(selected) != 1 || selected[0].Index != 2 {
		t.Fatalf("unrelated metadata blocked selected frames: %v", err)
	}
}
