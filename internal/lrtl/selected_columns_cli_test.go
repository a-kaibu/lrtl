package lrtl

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSelectedColumnsCLISelectionAndPreservation(t *testing.T) {
	dir, frames := frameSelectionFixture(t)
	source := pasteReferenceFile(t)
	for _, frame := range frames[2:4] {
		p, err := PacketForFrame(frame)
		if err != nil {
			t.Fatal(err)
		}
		for name, value := range map[string]string{"crs:Contrast2012": "35", "crs:Temperature": "1111", "crs:WhiteBalance": "Custom", "crs:HasCrop": "True"} {
			if err := p.SetScalar(name, value); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := WritePacket(frame.Sidecar, p, false); err != nil {
			t.Fatal(err)
		}
	}
	manifest := filepath.Join(t.TempDir(), "selection.json")
	var out, stderr bytes.Buffer
	if Main([]string{"select", dir, "--ratings", "4,5", "--output", manifest}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	before := batchFileSnapshot(t, dir)
	sourceBefore := denoiseRead(t, source)
	args := []string{"copy-settings", source, dir, "--selection", manifest, "--properties", "Exposure2012,WhiteBalance,HasCrop"}
	if Main(args, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("dry run changed files")
	}
	if Main(append(args, "--write"), &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	for _, frame := range frames[2:4] {
		p, err := PacketForFrame(frame)
		if err != nil {
			t.Fatal(err)
		}
		for name, want := range map[string]string{"crs:Exposure2012": "0.750000", "crs:Contrast2012": "35", "crs:Temperature": "1111"} {
			got, found, err := p.GetScalar(name)
			if err != nil || !found || got != want {
				t.Fatalf("%s=%s expected%s: %v", name, got, want, err)
			}
		}
		for _, name := range []string{"crs:WhiteBalance", "crs:HasCrop", "exif:DateTimeOriginal"} {
			if _, found, err := p.GetScalar(name); err != nil || found {
				t.Fatal("unexpected property", name, found, err)
			}
		}
		backups, err := filepath.Glob(frame.Sidecar + "*.bak")
		if err != nil || len(backups) != 1 {
			t.Fatal("backup", backups, err)
		}
		assertPreviewEVFileBytes(t, backups[0], before[frame.Sidecar])
	}
	for path, data := range before {
		if path != frames[2].Sidecar && path != frames[3].Sidecar {
			assertPreviewEVFileBytes(t, path, data)
		}
	}
	after := batchFileSnapshot(t, dir)
	if Main(append(args, "--write"), &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	if !reflect.DeepEqual(after, batchFileSnapshot(t, dir)) {
		t.Fatal("repeat wrote files or backups")
	}
	assertPreviewEVFileBytes(t, source, sourceBefore)
}

func TestSelectedColumnsCLIReferenceAndSharedProtection(t *testing.T) {
	dir, frames := frameSelectionFixture(t)
	p, err := PacketForFrame(frames[2])
	if err != nil {
		t.Fatal(err)
	}
	if err := p.SetScalar("crs:Exposure2012", ".5"); err != nil {
		t.Fatal(err)
	}
	if _, err := WritePacket(frames[2].Sidecar, p, false); err != nil {
		t.Fatal(err)
	}
	original := denoiseRead(t, frames[2].Sidecar)
	var out, stderr bytes.Buffer
	if Main([]string{"copy-settings", frames[2].Image, dir, "--ratings", "4,5", "--properties", "Exposure2012", "--write"}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	if !strings.Contains(out.String(), "reference files skipped: 1") {
		t.Fatal("reference not excluded")
	}
	assertPreviewEVFileBytes(t, frames[2].Sidecar, original)
	p, err = PacketForFrame(frames[3])
	if err != nil {
		t.Fatal(err)
	}
	if got, _, _ := p.GetScalar("crs:Exposure2012"); got != "0.500000" {
		t.Fatal("missing selective value", got)
	}

	sharedDir := t.TempDir()
	for _, name := range []string{"same.jpg", "same.nef"} {
		if err := os.WriteFile(filepath.Join(sharedDir, name), []byte("opaque"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := WritePacket(filepath.Join(sharedDir, "same.xmp"), EmptyXMPPacket(), false); err != nil {
		t.Fatal(err)
	}
	before := batchFileSnapshot(t, sharedDir)
	if Main([]string{"copy-settings", frames[2].Sidecar, sharedDir, "--filename-contains", "nef", "--properties", "Exposure2012", "--write"}, &out, &stderr) == 0 {
		t.Fatal("shared sidecar leaked to unselected image")
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, sharedDir)) {
		t.Fatal("shared target rejection wrote files")
	}
}

func TestSelectedColumnsCLILateArrayFailurePreflight(t *testing.T) {
	var rows []struct{ Property, Source, Destination string }
	if err := json.Unmarshal(denoiseRead(t, "testdata/lrt-selected-column-paths-7.5.5.json"), &rows); err != nil {
		t.Fatal(err)
	}
	row := rows[5]
	frames := cropFixture(t, 2)
	dir := filepath.Dir(frames[0].Image)
	source := pasteTestPacket(t, row.Source)
	if err := source.SetScalar("crs:Exposure2012", "2"); err != nil {
		t.Fatal(err)
	}
	ref := filepath.Join(t.TempDir(), "reference.xmp")
	if _, err := WritePacket(ref, source, false); err != nil {
		t.Fatal(err)
	}
	if _, err := WritePacket(frames[0].Sidecar, pasteTestPacket(t, row.Destination), false); err != nil {
		t.Fatal(err)
	}
	if _, err := WritePacket(frames[1].Sidecar, EmptyXMPPacket(), false); err != nil {
		t.Fatal(err)
	}
	before := batchFileSnapshot(t, dir)
	var out, stderr bytes.Buffer
	if Main([]string{"copy-settings", ref, dir, "--properties", "Exposure2012," + row.Property, "--write"}, &out, &stderr) == 0 {
		t.Fatal("accepted missing nested parent array")
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("later validation failure changed earlier targets")
	}
}

func TestSelectedColumnsCLIListAndInvalidOptions(t *testing.T) {
	var out, stderr bytes.Buffer
	if Main([]string{"copy-settings", "--list-properties"}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 861 {
		t.Fatal("selectable columns count", len(lines))
	}
	if !strings.Contains(out.String(), "Exposure2012\tDouble") || strings.Contains(out.String(), "lrt:ExternalExposureDefault") {
		t.Fatal("wrong native eligibility")
	}
	dir, _ := frameSelectionFixture(t)
	source := pasteReferenceFile(t)
	for _, args := range [][]string{{"copy-settings", "--list-properties", "--write"}, {"copy-settings", source, dir, "--properties", ""}, {"copy-settings", source, dir, "--properties", "ToneCurvePV2012", "--write"}, {"copy-settings", source, dir, "--properties", "xmp:CreateDate", "--write"}} {
		before := batchFileSnapshot(t, dir)
		if Main(args, &out, &stderr) == 0 {
			t.Fatal("accepted invalid column selection", args)
		}
		if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
			t.Fatal("invalid options changed files")
		}
	}
}

func TestSelectedColumnsThroughAVI(t *testing.T) {
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
		index := bytes.Index(data, []byte("movi"))
		if index < 0 {
			t.Fatal("missing AVI frames")
		}
		return data[index:]
	}
	before := render("before")
	if Main([]string{"copy-settings", source, dir, "--properties", "Exposure2012", "--selection", manifest, "--write"}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	after := render("after")
	if bytes.Equal(before, after) {
		t.Fatal("copied exposure did not affect pixels")
	}
	for _, frame := range frames[2:4] {
		packet, err := PacketForFrame(frame)
		if err != nil {
			t.Fatal(err)
		}
		if _, found, _ := packet.GetScalar("crs:Temperature"); found {
			t.Fatal("copied unselected white balance")
		}
		if err := packet.SetScalar("crs:Exposure2012", "0.75"); err != nil {
			t.Fatal(err)
		}
		if _, err := WritePacket(frame.Sidecar, packet, false); err != nil {
			t.Fatal(err)
		}
	}
	if !bytes.Equal(after, render("explicit")) {
		t.Fatal("AVI differs from explicit exposure-only control")
	}
}

func FuzzSelectedColumns(f *testing.F) {
	var reference struct{ Cases []selectedColumnObservation }
	data, err := os.ReadFile("testdata/lrt-selected-columns-7.5.5.json")
	if err != nil {
		f.Fatal(err)
	}
	if err := json.Unmarshal(data, &reference); err != nil {
		f.Fatal(err)
	}
	empty, _ := EmptyXMPPacket().Serialize()
	for i, row := range reference.Cases {
		if i < 56 || i%50 == 0 {
			f.Add(row.Source, string(empty), row.Property)
		}
	}
	f.Fuzz(func(t *testing.T, sourceText, targetText, properties string) {
		if len(sourceText) > 32768 || len(targetText) > 32768 || len(properties) > 8192 {
			t.Skip()
		}
		columns, err := parseSelectedColumns(properties)
		if err != nil {
			return
		}
		source, err := parseXMP([]byte(sourceText))
		if err != nil {
			return
		}
		target, err := parseXMP([]byte(targetText))
		if err != nil {
			return
		}
		originalSource, err := source.Serialize()
		if err != nil {
			return
		}
		originalTarget, err := target.Serialize()
		if err != nil {
			return
		}
		_, copyErr := copySelectedColumnMetadata(target, source, columns)
		actualSource, err := source.Serialize()
		if err != nil || !bytes.Equal(originalSource, actualSource) {
			t.Fatal("source mutation", err)
		}
		actualTarget, err := target.Serialize()
		if err != nil {
			t.Fatal(err)
		}
		if copyErr != nil {
			if !bytes.Equal(originalTarget, actualTarget) {
				t.Fatal("failed copy changed target")
			}
			return
		}
		reloaded, err := parseXMP(actualTarget)
		if err != nil {
			t.Fatal("invalid generated XMP", err)
		}
		if changed, err := copySelectedColumnMetadata(reloaded, source, columns); err != nil || changed {
			t.Fatal("repeat changed metadata", changed, err)
		}
	})
}
