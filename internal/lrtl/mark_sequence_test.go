package lrtl

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func markerCommand(t *testing.T, args ...string) string {
	t.Helper()
	var out, stderr bytes.Buffer
	if Main(args, &out, &stderr) != 0 {
		t.Fatalf("%v: %s", args, &stderr)
	}
	return out.String()
}

func TestMarkSequenceRoundTripRatingsAndSelection(t *testing.T) {
	dir, frames := frameSelectionFixture(t)
	manifest := filepath.Join(t.TempDir(), "selected.json")
	// Inversion selects absent, zero, four, five and rejected ratings, excluding HG.
	markerCommand(t, "select", dir, "--ratings", "3", "--output", manifest)
	selection := []string{"--selection", manifest, "--invert"}
	before := batchFileSnapshot(t, dir)
	args := append([]string{"mark-sequence", dir}, selection...)
	markerCommand(t, args...)
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("dry run changed source")
	}
	for _, kind := range []string{"default", "crop"} {
		markerCommand(t, append(args, "--kind", kind, "--write")...)
		for i, frame := range frames[:5] {
			packet, err := PacketForFrame(frame)
			if err != nil {
				t.Fatal(err)
			}
			rating, valid := packet.Rating()
			want := 4
			if kind == "crop" {
				want = 5
			}
			if !valid || rating != want {
				t.Fatalf("frame %d rating %d, want %d", i, rating, want)
			}
			previous, found, err := packet.GetScalar("ol:PreviousRating")
			if err != nil || !found || previous != []string{absentRating, "0", "4", "5", "-1"}[i] {
				t.Fatalf("frame %d previous %q: %v", i, previous, err)
			}
		}
		after := batchFileSnapshot(t, dir)
		markerCommand(t, append(args, "--kind", kind, "--write")...)
		if !reflect.DeepEqual(after, batchFileSnapshot(t, dir)) {
			t.Fatal("repeat changed packets or backups")
		}
	}
	markerCommand(t, append([]string{"unmark-sequence", dir, "--write"}, selection...)...)
	for i, frame := range frames {
		packet, err := PacketForFrame(frame)
		if err != nil {
			t.Fatal(err)
		}
		value, found, err := packet.GetScalar("xmp:Rating")
		if err != nil || found != (i != 0) || value != []string{"", "0", "4", "5", "-1", "3"}[i] {
			t.Fatalf("restored frame %d: %q %v %v", i, value, found, err)
		}
		for _, property := range []string{"ol:KeyframeKind", "ol:PreviousRating"} {
			if exists, err := packet.HasProperty(property); err != nil || exists {
				t.Fatalf("left %s: %v", property, err)
			}
		}
		assertPreviewEVFileBytes(t, frame.Image, before[frame.Image])
	}
	assertPreviewEVFileBytes(t, frames[5].Sidecar, before[frames[5].Sidecar])
	after := batchFileSnapshot(t, dir)
	markerCommand(t, "unmark-sequence", dir, "--write")
	if !reflect.DeepEqual(after, batchFileSnapshot(t, dir)) {
		t.Fatal("unowned markers were changed")
	}
}

func TestUnmarkSequenceExternalRatings(t *testing.T) {
	dir, frames := frameSelectionFixture(t)
	before := batchFileSnapshot(t, dir)
	markerCommand(t, "unmark-sequence", dir, "--write")
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("cleared external markers without force")
	}
	markerCommand(t, "unmark-sequence", dir, "--force-rating", "--write", "--no-backup")
	for i, frame := range frames {
		if i == 2 || i == 3 {
			packet, err := PacketForFrame(frame)
			if err != nil {
				t.Fatal(err)
			}
			if _, found, err := packet.GetScalar("xmp:Rating"); err != nil || found {
				t.Fatalf("external rating remains: %v", err)
			}
		} else if frame.Sidecar != "" {
			assertPreviewEVFileBytes(t, frame.Sidecar, before[frame.Sidecar])
		}
	}
	backups, err := filepath.Glob(filepath.Join(dir, "*.bak"))
	if err != nil || len(backups) != 0 {
		t.Fatalf("unexpected backup: %v %v", backups, err)
	}
}

func TestMarkSequencePreflightPreservesAllFiles(t *testing.T) {
	for _, scenario := range []string{"hg-rating", "hg-internal", "malformed", "owned-hg-unmark"} {
		t.Run(scenario, func(t *testing.T) {
			frames := cropFixture(t, 3)
			dir := filepath.Dir(frames[0].Image)
			last := frames[2]
			packet, err := PacketForFrame(last)
			if err != nil {
				t.Fatal(err)
			}
			command := "mark-sequence"
			switch scenario {
			case "hg-rating":
				err = packet.SetScalar("xmp:Rating", "2")
			case "hg-internal":
				err = packet.SetScalar(holyGrailKeyframeProperty, "1")
			case "owned-hg-unmark":
				for _, frame := range frames {
					p, e := PacketForFrame(frame)
					if e != nil {
						t.Fatal(e)
					}
					if _, e = MarkKeyframe(p, "default"); e != nil {
						t.Fatal(e)
					}
					if _, e = WritePacket(frame.Sidecar, p, false); e != nil {
						t.Fatal(e)
					}
				}
				packet, err = PacketForFrame(last)
				if err != nil {
					t.Fatal(err)
				}
				err = packet.SetScalar("xmp:Rating", "3")
				command = "unmark-sequence"
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err = WritePacket(last.Sidecar, packet, false); err != nil {
				t.Fatal(err)
			}
			if scenario == "malformed" {
				if err = os.WriteFile(last.Sidecar, []byte("<broken>"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			before := batchFileSnapshot(t, dir)
			var out, stderr bytes.Buffer
			if Main([]string{command, dir, "--write"}, &out, &stderr) == 0 {
				t.Fatal("accepted invalid late candidate")
			}
			if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
				t.Fatal("preflight failure changed files/backups")
			}
		})
	}
}

func TestMarkSequenceSharedSidecarAndBackup(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"same.nef", "same.jpg"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("opaque image"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(dir, "same.xmp")
	if _, err := WritePacket(path, EmptyXMPPacket(), false); err != nil {
		t.Fatal(err)
	}
	before := batchFileSnapshot(t, dir)
	var out, stderr bytes.Buffer
	if Main([]string{"mark-sequence", dir, "--filename-contains", "nef", "--write"}, &out, &stderr) == 0 {
		t.Fatal("edited unselected shared image")
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("shared rejection wrote files")
	}
	if result := markerCommand(t, "mark-sequence", dir, "--write"); !strings.Contains(result, "Updated 1 sidecar(s)") {
		t.Fatal(result)
	}
	backups, err := filepath.Glob(path + "*.bak")
	if err != nil || len(backups) != 1 {
		t.Fatalf("expected one backup: %v %v", backups, err)
	}
	assertPreviewEVFileBytes(t, backups[0], before[path])
}

func TestMarkSequenceEmbeddedAndInvalidArguments(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "embedded.jpg")
	packet := EmptyXMPPacket()
	if err := packet.SetScalar("dc:description", "keep embedded metadata"); err != nil {
		t.Fatal(err)
	}
	data, err := packet.Serialize()
	if err != nil {
		t.Fatal(err)
	}
	imageBytes := writeEmbeddedTransitionJPEG(t, path, data)
	for _, flags := range [][]string{{"--kind", "invalid", "--write"}, {"--no-backup"}, {"--filename-contains", "missing", "--write"}, {"--ratings", "6", "--write"}, {"--force-rating"}} {
		before := batchFileSnapshot(t, dir)
		var out, stderr bytes.Buffer
		if Main(append([]string{"mark-sequence", dir}, flags...), &out, &stderr) == 0 {
			t.Fatalf("accepted %v", flags)
		}
		if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
			t.Fatal("invalid arguments changed source")
		}
	}
	markerCommand(t, "mark-sequence", dir, "--kind", "crop", "--write")
	frames, err := DiscoverSequence(dir)
	if err != nil {
		t.Fatal(err)
	}
	marked, err := PacketForFrame(frames[0])
	if err != nil {
		t.Fatal(err)
	}
	if value, found, err := marked.GetScalar("dc:description"); err != nil || !found || value != "keep embedded metadata" {
		t.Fatalf("embedded metadata lost: %q %v", value, err)
	}
	markerCommand(t, "unmark-sequence", dir, "--write")
	assertPreviewEVFileBytes(t, path, imageBytes)
}
