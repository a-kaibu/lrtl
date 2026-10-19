package lrtl

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSetSequenceMatchesSingleEditsAndPreservesSelection(t *testing.T) {
	for _, tc := range []struct{ property, value, stored, expected string }{
		{"Exposure2012", "-0.75", "crs:Exposure2012", "-0.75"},
		{"Highlights2012", "20", "crs:Highlights2012", "20"},
		{"Temperature", "5500", "crs:Temperature", "5500"},
		{"PreviewEV", "0.2", "lrt:ExternalExposureDefault", "0.8"},
		{"lrt:PreviewEV", "-0.1", "lrt:ExternalExposureDefault", "-0.4"},
	} {
		t.Run(tc.property, func(t *testing.T) {
			dir, frames := frameSelectionFixture(t)
			before := batchFileSnapshot(t, dir)
			var out, stderr bytes.Buffer
			args := []string{"set-sequence", dir, tc.property, tc.value, "--ratings", "4,5"}
			if Main(args, &out, &stderr) != 0 {
				t.Fatal(&stderr)
			}
			if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
				t.Fatal("dry run changed source")
			}
			if Main(append(args, "--write"), &out, &stderr) != 0 {
				t.Fatal(&stderr)
			}
			changed := map[string]bool{}
			for _, frame := range frames[2:4] {
				changed[frame.Sidecar] = true
				packet, err := PacketForFrame(frame)
				if err != nil {
					t.Fatal(err)
				}
				value, found, err := packet.GetScalar(tc.stored)
				if err != nil || !found || value != tc.expected {
					t.Fatalf("stored %s: %q %v", tc.stored, value, err)
				}
				expected := filepath.Join(t.TempDir(), "expected.xmp")
				if err := os.WriteFile(expected, before[frame.Sidecar], 0600); err != nil {
					t.Fatal(err)
				}
				if Main([]string{"set", expected, tc.property, tc.value, "--no-backup"}, &out, &stderr) != 0 {
					t.Fatal(&stderr)
				}
				data, err := os.ReadFile(expected)
				if err != nil {
					t.Fatal(err)
				}
				assertPreviewEVFileBytes(t, frame.Sidecar, data)
				backups, err := filepath.Glob(frame.Sidecar + "*.bak")
				if err != nil || len(backups) != 1 {
					t.Fatalf("original backup: %v %v", backups, err)
				}
				assertPreviewEVFileBytes(t, backups[0], before[frame.Sidecar])
			}
			for path, data := range before {
				if !changed[path] {
					assertPreviewEVFileBytes(t, path, data)
				}
			}
			after := batchFileSnapshot(t, dir)
			if Main(append(args, "--write"), &out, &stderr) != 0 {
				t.Fatal(&stderr)
			}
			if !reflect.DeepEqual(after, batchFileSnapshot(t, dir)) {
				t.Fatal("repeat changed XMP/backups")
			}
		})
	}
}

func TestSetSequenceManifestInverseAndNegativeValues(t *testing.T) {
	dir, frames := frameSelectionFixture(t)
	selection := filepath.Join(t.TempDir(), "frames.json")
	var out, stderr bytes.Buffer
	if Main([]string{"select", dir, "--ratings", "4,5", "--output", selection}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	if Main([]string{"set-sequence", dir, "Exposure2012", "-1", "--selection", selection, "--invert", "--write"}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	for i, frame := range frames {
		packet, err := PacketForFrame(frame)
		if err != nil {
			t.Fatal(err)
		}
		value, found, _ := packet.GetScalar("crs:Exposure2012")
		if i == 2 || i == 3 {
			if found {
				t.Fatal("inverse edited originally selected frame")
			}
		} else if !found || value != "-1" {
			t.Fatal("negative edit/inverse missing")
		}
	}
	if Main([]string{"set-sequence", dir, "Exposure2012", "-2", "--ratings", "-1", "--write"}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	packet, err := PacketForFrame(frames[4])
	if err != nil {
		t.Fatal(err)
	}
	if value, _, _ := packet.GetScalar("crs:Exposure2012"); value != "-2" {
		t.Fatal("negative rating and value were parsed as options")
	}
}

func TestSetSequenceMaskEditsAndLateFailures(t *testing.T) {
	for _, tc := range []struct{ field, value string }{{"LocalExposure2012", "-.5"}, {"CorrectionActive", "false"}, {"CorrectionMasks[1].FullY", ".8"}} {
		t.Run(tc.field, func(t *testing.T) {
			frames := cropFixture(t, 3)
			for i, frame := range frames {
				packet := localMissingPacket(t, "MaskGroupBasedCorrections", map[string]string{"CorrectionSyncID": fmt.Sprint("own-", i)}, nil)
				persistExposurePacket(t, frame, packet)
			}
			dir := filepath.Dir(frames[0].Image)
			var out, stderr bytes.Buffer
			property := "MaskGroupBasedCorrections[1]." + tc.field
			if Main([]string{"set-sequence", dir, property, tc.value, "--write"}, &out, &stderr) != 0 {
				t.Fatal(&stderr)
			}
			for i, frame := range frames {
				packet, err := PacketForFrame(frame)
				if err != nil {
					t.Fatal(err)
				}
				path, err := parseMaskEditPath(property)
				if err != nil {
					t.Fatal(err)
				}
				element, err := maskEditElement(packet, path)
				if err != nil {
					t.Fatal(err)
				}
				got, found, _ := crsElementValue(element, path.property)
				want := map[string]string{"-.5": "-0.5", "false": "false", ".8": "0.8"}[tc.value]
				if !found || got != want {
					t.Fatalf("mask value %q expected %q", got, want)
				}
				root := maskGroupCorrectionAt(packet, 1)
				if id, _, _ := crsElementValue(root, "CorrectionSyncID"); id != fmt.Sprint("own-", i) {
					t.Fatal("changed mask ID")
				}
			}
		})
	}
	for _, mode := range []string{"malformed", "missing-mask", "internal"} {
		t.Run(mode, func(t *testing.T) {
			frames := cropFixture(t, 3)
			for i, frame := range frames {
				packet := localMissingPacket(t, "MaskGroupBasedCorrections", nil, nil)
				if i == 2 && mode == "missing-mask" {
					packet = EmptyXMPPacket()
				}
				if i == 2 && mode == "internal" {
					if err := setCRSElementValue(maskGroupCorrectionAt(packet, 1), "CorrectionName", "#LRT internal use (HG)"); err != nil {
						t.Fatal(err)
					}
				}
				persistExposurePacket(t, frame, packet)
			}
			if mode == "malformed" {
				if err := os.WriteFile(frames[2].Sidecar, []byte("<broken>"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			dir := filepath.Dir(frames[0].Image)
			before := batchFileSnapshot(t, dir)
			var out, stderr bytes.Buffer
			if Main([]string{"set-sequence", dir, "MaskGroupBasedCorrections[1].LocalExposure2012", ".5", "--write"}, &out, &stderr) == 0 {
				t.Fatal("invalid late candidate accepted")
			}
			if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
				t.Fatal("late error changed earlier XMP/backups/images")
			}
		})
	}
}

func TestSetSequenceRejectsInvalidInputs(t *testing.T) {
	dir, _ := frameSelectionFixture(t)
	before := batchFileSnapshot(t, dir)
	for _, tc := range [][2]string{{"Exposure2012", "NaN"}, {"Exposure2012", "-Inf"}, {"PreviewEV", "1e308"}, {"xmp:Rating", "4"}, {"Unknown", "1"}, {"MaskGroupBasedCorrections[0].LocalExposure2012", "1"}} {
		var out, stderr bytes.Buffer
		if Main([]string{"set-sequence", dir, tc[0], tc[1], "--write"}, &out, &stderr) == 0 {
			t.Fatalf("accepted %v", tc)
		}
		if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
			t.Fatal("invalid edit wrote files")
		}
	}
	for _, flags := range [][]string{{"--no-backup"}, {"--filename-contains", "no match", "--write"}} {
		var out, stderr bytes.Buffer
		args := append([]string{"set-sequence", dir, "Exposure2012", "1"}, flags...)
		if Main(args, &out, &stderr) == 0 {
			t.Fatalf("accepted %v", flags)
		}
	}
}

func TestSetSequenceSharedSidecarSelection(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"same.nef", "same.jpg"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("opaque image"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	packet := EmptyXMPPacket()
	if err := packet.SetScalar("crs:Exposure2012", "0"); err != nil {
		t.Fatal(err)
	}
	sidecar := filepath.Join(dir, "same.xmp")
	if _, err := WritePacket(sidecar, packet, false); err != nil {
		t.Fatal(err)
	}
	before := batchFileSnapshot(t, dir)
	var out, stderr bytes.Buffer
	if Main([]string{"set-sequence", dir, "Exposure2012", "1", "--filename-contains", "nef", "--write"}, &out, &stderr) == 0 {
		t.Fatal("edited an unselected image's shared sidecar")
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("shared-sidecar rejection changed files")
	}
	if Main([]string{"set-sequence", dir, "Exposure2012", "1", "--write"}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	backups, err := filepath.Glob(sidecar + "*.bak")
	if err != nil || len(backups) != 1 {
		t.Fatalf("shared sidecar was not written once: %v %v", backups, err)
	}
	if !strings.Contains(out.String(), "Updated 1 sidecar(s)") {
		t.Fatal("wrong shared output count")
	}
	assertPreviewEVFileBytes(t, backups[0], before[sidecar])
}

func TestSetSequenceProtectsUnselectedAlternateSidecar(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"same.nef", "same.jpg"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("opaque image"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	shared := filepath.Join(dir, "same.xmp")
	private := filepath.Join(dir, "same.jpg.xmp")
	for path, value := range map[string]string{shared: "0", private: "0.9"} {
		packet := EmptyXMPPacket()
		if err := packet.SetScalar("crs:Exposure2012", value); err != nil {
			t.Fatal(err)
		}
		if _, err := WritePacket(path, packet, false); err != nil {
			t.Fatal(err)
		}
	}
	base := time.Now().Add(-time.Hour)
	if err := os.Chtimes(shared, base, base); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(private, base.Add(time.Minute), base.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	before := batchFileSnapshot(t, dir)
	var out, stderr bytes.Buffer
	if Main([]string{"set-sequence", dir, "Exposure2012", "1", "--filename-contains", "nef", "--write"}, &out, &stderr) == 0 {
		t.Fatal("editing common stem XMP could switch an unselected JPEG away from its private XMP")
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("alternate-sidecar rejection wrote files")
	}
	// A private RAW sidecar can be edited without touching either JPEG candidate.
	packet := EmptyXMPPacket()
	if err := packet.SetScalar("crs:Exposure2012", "0"); err != nil {
		t.Fatal(err)
	}
	if _, err := WritePacket(filepath.Join(dir, "same.nef.xmp"), packet, false); err != nil {
		t.Fatal(err)
	}
	if Main([]string{"set-sequence", dir, "Exposure2012", "1", "--filename-contains", "nef", "--write"}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	frames, err := DiscoverSequence(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, frame := range frames {
		packet, err := PacketForFrame(frame)
		if err != nil {
			t.Fatal(err)
		}
		value, _, _ := packet.GetScalar("crs:Exposure2012")
		want := "1"
		if strings.HasSuffix(frame.Image, ".jpg") {
			want = "0.9"
		}
		if value != want {
			t.Fatalf("wrong effective per-image value: %s / %s", value, want)
		}
	}
}

func TestSetSequenceEmbeddedMetadataAndNoBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "embedded.jpg")
	packet := EmptyXMPPacket()
	if err := packet.SetScalar("crs:Exposure2012", ".25"); err != nil {
		t.Fatal(err)
	}
	if err := packet.SetScalar("dc:description", "preserve embedded text"); err != nil {
		t.Fatal(err)
	}
	data, err := packet.Serialize()
	if err != nil {
		t.Fatal(err)
	}
	imageBytes := writeEmbeddedTransitionJPEG(t, path, data)
	var out, stderr bytes.Buffer
	if Main([]string{"set-sequence", dir, "Exposure2012", "-.5", "--write"}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	assertPreviewEVFileBytes(t, path, imageBytes)
	if Main([]string{"set-sequence", dir, "Exposure2012", ".75", "--write", "--no-backup"}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	packet, err = PacketForSidecar(path + ".xmp")
	if err != nil {
		t.Fatal(err)
	}
	if value, _, _ := packet.GetScalar("crs:Exposure2012"); value != "0.75" {
		t.Fatal("missing new sidecar value")
	}
	if value, _, _ := packet.GetScalar("dc:description"); value != "preserve embedded text" {
		t.Fatal("lost embedded metadata")
	}
	backups, err := filepath.Glob(path + ".xmp*.bak")
	if err != nil || len(backups) != 0 {
		t.Fatal("created backup with --no-backup or for nonexistent sidecar")
	}
	assertPreviewEVFileBytes(t, path, imageBytes)
}

func TestSetSequenceGlobalEditThroughAVIPixels(t *testing.T) {
	dir, frames := frameSelectionFixture(t)
	var out, stderr bytes.Buffer
	args := []string{"set-sequence", dir, "Exposure2012", "-1", "--ratings", "4,5", "--write"}
	if Main(args, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	wantDir := t.TempDir()
	for _, frame := range frames[2:4] {
		name := filepath.Base(frame.Image)
		data, err := os.ReadFile(frame.Image)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(wantDir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
		packet, err := PacketForFrame(frame)
		if err != nil {
			t.Fatal(err)
		}
		if err := packet.SetScalar("crs:Exposure2012", "-1"); err != nil {
			t.Fatal(err)
		}
		if _, err := WritePacket(filepath.Join(wantDir, name)+".xmp", packet, false); err != nil {
			t.Fatal(err)
		}
	}
	manifest := filepath.Join(t.TempDir(), "selection.json")
	if Main([]string{"select", dir, "--ratings", "4,5", "--output", manifest}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	aPath, bPath := filepath.Join(t.TempDir(), "selected.avi"), filepath.Join(t.TempDir(), "expected.avi")
	if Main([]string{"render", dir, "--selection", manifest, "--output", aPath, "--apply-xmp-basic"}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	if Main([]string{"render", wantDir, "--output", bPath, "--apply-xmp-basic"}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	a, errA := os.ReadFile(aPath)
	b, errB := os.ReadFile(bPath)
	i, j := bytes.Index(a, []byte("movi")), bytes.Index(b, []byte("movi"))
	if errA != nil || errB != nil || i < 0 || j < 0 || !bytes.Equal(a[i:], b[j:]) {
		t.Fatal("selected edit's AVI differs from explicit per-frame control")
	}
}

func TestSetSequenceKeepsEffectiveFutureDatedSidecar(t *testing.T) {
	frames := cropFixture(t, 1)
	image := frames[0].Image
	appended := image + ".xmp"
	stem := strings.TrimSuffix(image, filepath.Ext(image)) + ".xmp"
	packet := EmptyXMPPacket()
	if err := packet.SetScalar("xmp:Rating", "5"); err != nil {
		t.Fatal(err)
	}
	if err := packet.SetScalar("crs:Exposure2012", ".25"); err != nil {
		t.Fatal(err)
	}
	if _, err := WritePacket(stem, packet, false); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(48 * time.Hour).Truncate(time.Second)
	if err := os.Chtimes(appended, future, future); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(stem, future.Add(time.Hour), future.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	if Main([]string{"set-sequence", filepath.Dir(image), "Exposure2012", "-1", "--ratings", "5", "--write"}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	actual, err := DiscoverSequence(filepath.Dir(image))
	if err != nil {
		t.Fatal(err)
	}
	if actual[0].Sidecar != stem {
		t.Fatalf("edit switched to stale sidecar %s", actual[0].Sidecar)
	}
	info, err := os.Stat(stem)
	if err != nil || !info.ModTime().After(future.Add(time.Hour)) {
		t.Fatal("future-dated XMP timestamp did not advance")
	}
	packet, err = PacketForFrame(actual[0])
	if err != nil {
		t.Fatal(err)
	}
	if value, _, _ := packet.GetScalar("crs:Exposure2012"); value != "-1" {
		t.Fatal("effective metadata lost the edit")
	}
}
