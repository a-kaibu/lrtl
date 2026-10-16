package lrtl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLRTReferenceDateOrderNative(t *testing.T) {
	data, err := os.ReadFile("testdata/lrt-reference-order-7.5.5.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Names, Dates []string
		Order        []int
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) != 6 {
		t.Fatal("native date-order fixture incomplete")
	}
	for c, tc := range cases {
		got, err := lrtReferenceDateOrder(tc.Names, tc.Dates)
		if err != nil || !reflect.DeepEqual(got, tc.Order) {
			t.Fatalf("case %d: %v != native %v (%v)", c, got, tc.Order, err)
		}
	}
	if _, err := lrtReferenceDateOrder([]string{"a", "b"}, []string{"date"}); err == nil {
		t.Fatal("mismatched metadata lengths accepted")
	}
	if _, err := lrtReferenceDateOrder([]string{"a", "b"}, []string{"date", ""}); err == nil {
		t.Fatal("partial date metadata accepted")
	}
}

func dateReferenceFixture(t *testing.T, root, reference string) ([]Frame, []Frame, []int) {
	t.Helper()
	positions := []int{2, 0, 4, 1, 3}
	order := []int{1, 3, 0, 4, 2}
	// Each frame's bright region follows its position in the date-sorted
	// native sequence. Use a permutation distinct from its inverse.
	frames := movingReferenceFixtureAt(t, root, false, positions)
	refs := movingReferenceFixture(t, reference, true)
	images := make([][]byte, len(refs))
	for i, frame := range refs {
		data, err := os.ReadFile(frame.Image)
		if err != nil {
			t.Fatal(err)
		}
		images[i] = data
	}
	for i, frame := range frames {
		packet, err := PacketForFrame(frame)
		if err != nil {
			t.Fatal(err)
		}
		if err := packet.SetScalar("xmp:CreateDate", fmt.Sprintf("2026-10-17T00:00:%02d", positions[i])); err != nil {
			t.Fatal(err)
		}
		// Deliberately disagree with DateTimeOriginal: native sorting uses CreateDate.
		if err := packet.SetScalar("exif:DateTimeOriginal", fmt.Sprintf("2026-10-17T00:00:%02d", 4-positions[i])); err != nil {
			t.Fatal(err)
		}
		if _, err := WritePacket(frame.Sidecar, packet, false); err != nil {
			t.Fatal(err)
		}
	}
	for rank, original := range order {
		if err := os.WriteFile(refs[rank].Image, images[original], 0600); err != nil {
			t.Fatal(err)
		}
		xmp, err := os.ReadFile(frames[original].Sidecar)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(refs[rank].Sidecar, xmp, 0600); err != nil {
			t.Fatal(err)
		}
	}
	writeLRTReferenceSettings(t, root, map[string]any{"imageCount": 5, "filenameSort": false, "referenceBoxes": []string{"0;0;0;0.25;1", "4;0.5;0;0.75;1"}})
	return frames, refs, order
}

func TestLRTReferenceDateMeasurementsAndIdentity(t *testing.T) {
	root := t.TempDir()
	frames, _, order := dateReferenceFixture(t, root, t.TempDir())
	before := batchFileSnapshot(t, root)
	boxes, gotOrder, err := loadLRTReferences(frames)
	if err != nil || !reflect.DeepEqual(gotOrder, order) {
		t.Fatalf("wrong date mapping: %v %v", gotOrder, err)
	}
	for rank, box := range boxes {
		area, err := box.rectangle(image.Rect(0, 0, 64, 16))
		if err != nil || area.Min.X != rank*8 {
			t.Fatalf("wrong native reference position: %v %v", area, err)
		}
	}
	plan, err := planDeflickerSequence(frames, image.Rectangle{}, imageInputOptions{LRTReference: true}, 2, 2, false, "gaussian")
	if err != nil {
		t.Fatal(err)
	}
	values := []uint16{32000, 40000, 32000, 40000, 32000}
	for rank, original := range order {
		if math.Abs(plan.Measurements[0][rank]-srgb16ToLinear[values[original]]) > 1e-15 {
			t.Fatal("smoothing measurements stayed in filename order")
		}
	}
	for _, change := range plan.Changes {
		if frames[change.Frame.Index].Image != change.Frame.Image {
			t.Fatal("date order lost original frame index")
		}
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, root)) {
		t.Fatal("date-order planning modified source")
	}
}

func TestLRTReferenceDateCLIEqualsOrderedCroppedSequence(t *testing.T) {
	for _, batch := range []bool{false, true} {
		t.Run(fmt.Sprint("batch=", batch), func(t *testing.T) {
			root := t.TempDir()
			type pair struct {
				frames, refs []Frame
				order        []int
			}
			var pairs []pair
			for _, name := range []string{"a", "b"} {
				frames, refs, order := dateReferenceFixture(t, filepath.Join(root, name), t.TempDir())
				pairs = append(pairs, pair{frames, refs, order})
			}
			before := batchFileSnapshot(t, root)
			options := []string{"--smooth", "2", "--passes", "2", "--smoother", "gaussian", "--write"}
			var out, stderr bytes.Buffer
			if batch {
				if Main(append([]string{"batch-deflicker", root, "--lrt-reference", "--workers", "2"}, options...), &out, &stderr) != 0 {
					t.Fatal(&stderr)
				}
			}
			for _, pair := range pairs {
				dir := filepath.Dir(pair.frames[0].Image)
				if !batch {
					if Main(append([]string{"deflicker", dir, "--lrt-reference"}, options...), &out, &stderr) != 0 {
						t.Fatal(&stderr)
					}
				}
				if Main(append([]string{"deflicker", filepath.Dir(pair.refs[0].Image)}, options...), &out, &stderr) != 0 {
					t.Fatal(&stderr)
				}
				for rank, original := range pair.order {
					frame := pair.frames[original]
					want, err := os.ReadFile(pair.refs[rank].Sidecar)
					if err != nil {
						t.Fatal(err)
					}
					assertPreviewEVFileBytes(t, frame.Sidecar, want)
					if !bytes.Equal(want, before[frame.Sidecar]) {
						backups, err := filepath.Glob(frame.Sidecar + "*.bak")
						if err != nil || len(backups) != 1 {
							t.Fatal("wrong date-mapped XMP backup")
						}
						assertPreviewEVFileBytes(t, backups[0], before[frame.Sidecar])
					}
					assertPreviewEVFileBytes(t, frame.Image, before[frame.Image])
				}
				path := filepath.Join(dir, ".lrt", "lrtsequence.json")
				assertPreviewEVFileBytes(t, path, before[path])
			}
		})
	}
}
