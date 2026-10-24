package lrtl

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func pasteReferenceFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "reference.xmp")
	if err := os.WriteFile(path, []byte(metadataPasteObservations(t)[0].Source), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCopySettingsSelectionBackupAndRepeat(t *testing.T) {
	dir, frames := frameSelectionFixture(t)
	source := pasteReferenceFile(t)
	sourceBefore := denoiseRead(t, source)
	before := batchFileSnapshot(t, dir)
	var out, stderr bytes.Buffer
	args := []string{"copy-settings", source, dir, "--ratings", "4,5"}
	if Main(args, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("dry run wrote files")
	}
	if Main(append(args, "--write"), &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	for path, data := range before {
		if path != frames[2].Sidecar && path != frames[3].Sidecar {
			assertPreviewEVFileBytes(t, path, data)
		}
	}
	for path := range batchFileSnapshot(t, dir) {
		if _, existed := before[path]; !existed && !strings.HasPrefix(path, frames[2].Sidecar+".lrtl-") && !strings.HasPrefix(path, frames[3].Sidecar+".lrtl-") {
			t.Fatalf("unexpected output outside selected sidecars: %s", path)
		}
	}
	for i, frame := range frames {
		if i != 2 && i != 3 {
			if data, ok := before[frame.Sidecar]; ok {
				assertPreviewEVFileBytes(t, frame.Sidecar, data)
			}
			continue
		}
		packet, err := PacketForFrame(frame)
		if err != nil {
			t.Fatal(err)
		}
		for key, want := range map[string]string{"crs:Exposure2012": "0.750000", "crs:WhiteBalance": "As Shot", "crs:HasCrop": "False", "exif:DateTimeOriginal": "2026-10-01T01:00:00"} {
			if got, found, err := packet.GetScalar(key); err != nil || !found || got != want {
				t.Fatalf("%s=%q want %q: %v", key, got, want, err)
			}
		}
		if _, found, err := packet.GetScalar("crs:Temperature"); err != nil || found {
			t.Fatal("native As Shot cleanup retained absolute WB", err)
		}
		rating, _, _ := packet.GetScalar("xmp:Rating")
		if rating != []string{"4", "5"}[i-2] {
			t.Fatal("overwrote recipient rating")
		}
		backups, err := filepath.Glob(frame.Sidecar + "*.bak")
		if err != nil || len(backups) != 1 {
			t.Fatalf("backup %v: %v", backups, err)
		}
		assertPreviewEVFileBytes(t, backups[0], before[frame.Sidecar])
	}
	after := batchFileSnapshot(t, dir)
	out.Reset()
	if Main(append(args, "--write"), &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	if !reflect.DeepEqual(after, batchFileSnapshot(t, dir)) || !strings.Contains(out.String(), "Updated 0 sidecar(s)") {
		t.Fatal("ordinary repeat wrote metadata or backups")
	}
	assertPreviewEVFileBytes(t, source, sourceBefore)
}

func TestCopySettingsRejectsBeforeWriting(t *testing.T) {
	for _, mode := range []string{"late-invalid", "no-match", "no-backup-dry", "missing-source", "image-without-xmp", "directory-source"} {
		t.Run(mode, func(t *testing.T) {
			dir, frames := frameSelectionFixture(t)
			source := pasteReferenceFile(t)
			args := []string{"copy-settings", source, dir, "--write"}
			switch mode {
			case "late-invalid":
				if err := os.WriteFile(frames[5].Sidecar, []byte("<broken>"), 0600); err != nil {
					t.Fatal(err)
				}
			case "no-match":
				args = append(args, "--filename-contains", "not-present")
			case "no-backup-dry":
				args = []string{"copy-settings", source, dir, "--no-backup"}
			case "missing-source":
				args[1] = source + "missing"
			case "image-without-xmp":
				args[1] = frames[0].Image
			case "directory-source":
				args[1] = dir
			}
			before := batchFileSnapshot(t, dir)
			var out, stderr bytes.Buffer
			if Main(args, &out, &stderr) == 0 {
				t.Fatal("accepted invalid copy", mode)
			}
			if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
				t.Fatal("failed prevalidation wrote files")
			}
		})
	}
}

func TestCopySettingsSharedSourceAndHardlinkProtection(t *testing.T) {
	for _, mode := range []string{"unselected-shared", "all-shared", "all-shared-common", "reference-shared", "reference-hardlink"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			for _, name := range []string{"same.nef", "same.jpg"} {
				if err := os.WriteFile(filepath.Join(dir, name), []byte("opaque image"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			source := pasteReferenceFile(t)
			shared := filepath.Join(dir, "same.xmp")
			initial := EmptyXMPPacket()
			if mode == "all-shared-common" {
				// A shared FLAT cache gives the same defaults to both extensions.
				// Otherwise NEF/JPEG now require different native default packets.
				if err := initial.SetScalar("lrt:Quality", "FLAT"); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := WritePacket(shared, initial, false); err != nil {
				t.Fatal(err)
			}
			if mode == "reference-shared" {
				source = shared
			}
			if mode == "reference-hardlink" {
				if err := os.Remove(shared); err != nil {
					t.Fatal(err)
				}
				if err := os.Link(source, shared); err != nil {
					t.Fatal(err)
				}
			}
			before := batchFileSnapshot(t, dir)
			args := []string{"copy-settings", source, dir, "--write"}
			if mode == "unselected-shared" {
				args = append(args, "--filename-contains", "nef")
			}
			var out, stderr bytes.Buffer
			code := Main(args, &out, &stderr)
			if mode == "unselected-shared" || mode == "all-shared" {
				if code == 0 {
					t.Fatal("changed protected or conflicting shared sidecar")
				}
			} else if code != 0 {
				t.Fatal(&stderr)
			}
			if mode != "all-shared-common" {
				if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
					t.Fatal("protected reference or shared target changed")
				}
			} else {
				if !strings.Contains(out.String(), "Updated 1 sidecar(s)") {
					t.Fatal("shared target was not deduplicated")
				}
				backups, _ := filepath.Glob(shared + "*.bak")
				if len(backups) != 1 {
					t.Fatal("shared backup count", backups)
				}
			}
		})
	}
}

func TestCopySettingsEmbeddedReferenceAndSidecarPriority(t *testing.T) {
	for _, external := range []bool{false, true} {
		t.Run(map[bool]string{false: "embedded", true: "newest-sidecar"}[external], func(t *testing.T) {
			dir, frames := frameSelectionFixture(t)
			image := filepath.Join(dir, "reference.jpg")
			original := writeEmbeddedTransitionJPEG(t, image, []byte(metadataPasteObservations(t)[0].Source))
			sourcePath := image
			if external {
				appended := image + ".xmp"
				stem := strings.TrimSuffix(image, ".jpg") + ".xmp"
				for path, value := range map[string]string{appended: "-1", stem: "-2"} {
					p := EmptyXMPPacket()
					if err := p.SetScalar("crs:Exposure2012", value); err != nil {
						t.Fatal(err)
					}
					if _, err := WritePacket(path, p, false); err != nil {
						t.Fatal(err)
					}
				}
				now := time.Now()
				if err := os.Chtimes(stem, now.Add(time.Hour), now.Add(time.Hour)); err != nil {
					t.Fatal(err)
				}
				sourcePath = stem
			}
			sourceBefore := denoiseRead(t, sourcePath)
			var out, stderr bytes.Buffer
			if Main([]string{"copy-settings", image, dir, "--write", "--no-backup"}, &out, &stderr) != 0 {
				t.Fatal(&stderr)
			}
			if !strings.Contains(out.String(), "reference files skipped: 1") {
				t.Fatal("reference frame was not excluded")
			}
			assertPreviewEVFileBytes(t, image, original)
			assertPreviewEVFileBytes(t, sourcePath, sourceBefore)
			p, err := PacketForSidecar(frames[0].OutputSidecar())
			if err != nil {
				t.Fatal(err)
			}
			got, _, _ := p.GetScalar("crs:Exposure2012")
			want := "0.750000"
			if external {
				want = "-2.000000"
			}
			if got != want {
				t.Fatalf("exposure %s want %s", got, want)
			}
			matches, _ := filepath.Glob(filepath.Join(dir, "*.bak"))
			if len(matches) != 0 {
				t.Fatal("no-backup created backups")
			}
		})
	}
}

func TestCopySettingsThroughAVIPixels(t *testing.T) {
	dir, frames := frameSelectionFixture(t)
	source := pasteReferenceFile(t)
	var out, stderr bytes.Buffer
	manifest := filepath.Join(t.TempDir(), "selected.json")
	if Main([]string{"select", dir, "--ratings", "4,5", "--output", manifest}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	render := func(name string) []byte {
		path := filepath.Join(t.TempDir(), name+".avi")
		if Main([]string{"render", dir, "--selection", manifest, "--output", path, "--apply-xmp-basic"}, &out, &stderr) != 0 {
			t.Fatal(&stderr)
		}
		data := denoiseRead(t, path)
		i := bytes.Index(data, []byte("movi"))
		if i < 0 {
			t.Fatal("no AVI frames")
		}
		return data[i:]
	}
	before := render("before")
	if Main([]string{"copy-settings", source, dir, "--ratings", "4,5", "--write"}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	after := render("after")
	if bytes.Equal(before, after) {
		t.Fatal("copied exposure did not change video pixels")
	}
	// Independent explicit per-frame settings are the video control.
	for _, frame := range frames[2:4] {
		p, err := PacketForFrame(frame)
		if err != nil {
			t.Fatal(err)
		}
		p.RemoveProperty("crs:Exposure2012")
		p.RemoveProperty("crs:Temperature")
		if err := p.SetScalar("crs:Exposure2012", "0.75"); err != nil {
			t.Fatal(err)
		}
		if err := p.SetScalar("crs:WhiteBalance", "As Shot"); err != nil {
			t.Fatal(err)
		}
		if _, err := WritePacket(frame.OutputSidecar(), p, false); err != nil {
			t.Fatal(err)
		}
	}
	if !bytes.Equal(after, render("explicit")) {
		t.Fatal("copied settings video differs from explicit control")
	}
}

func TestCopySettingsHelp(t *testing.T) {
	var out, stderr bytes.Buffer
	if Main([]string{"copy-settings", "--help"}, &out, &stderr) != 0 || !strings.Contains(out.String(), "Usage: lrtl copy-settings") {
		t.Fatal(&out, &stderr)
	}
}

func TestCopySettingsManifestInverseAndEmptyReference(t *testing.T) {
	dir, frames := frameSelectionFixture(t)
	source := filepath.Join(t.TempDir(), "empty.xmp")
	if _, err := WritePacket(source, EmptyXMPPacket(), false); err != nil {
		t.Fatal(err)
	}
	for _, frame := range frames {
		packet, err := PacketForFrame(frame)
		if err != nil {
			t.Fatal(err)
		}
		if err := packet.SetScalar("crs:Exposure2012", "1"); err != nil {
			t.Fatal(err)
		}
		if _, err := WritePacket(frame.OutputSidecar(), packet, false); err != nil {
			t.Fatal(err)
		}
	}
	manifest := filepath.Join(t.TempDir(), "frames.json")
	var out, stderr bytes.Buffer
	if Main([]string{"select", dir, "--ratings", "4,5", "--output", manifest}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	if Main([]string{"copy-settings", source, dir, "--selection", manifest, "--invert", "--write"}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	for i, frame := range frames {
		packet, err := PacketForFrame(frame)
		if err != nil {
			t.Fatal(err)
		}
		value, found, err := packet.GetScalar("crs:Exposure2012")
		want := "0.000000"
		if i == 2 || i == 3 {
			want = "1"
		}
		if err != nil || !found || value != want {
			t.Fatalf("frame %d: exposure %q, want %q; found %v error %v", i, value, want, found, err)
		}
	}
}
