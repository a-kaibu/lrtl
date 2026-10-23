package lrtl

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestWBEditCLIThroughAVIPixels(t *testing.T) {
	frames := cropFixture(t, 3)
	dir := filepath.Dir(frames[0].Image)
	for _, frame := range frames {
		packet, err := PacketForFrame(frame)
		if err != nil {
			t.Fatal(err)
		}
		for key, value := range map[string]string{"crs:WhiteBalance": "As Shot", "crs:Temperature": "4500", "crs:Tint": "20", "crs:IncrementalTemperature": "10", "crs:IncrementalTint": "-5"} {
			if err := packet.SetScalar(key, value); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := WritePacket(frame.Sidecar, packet, false); err != nil {
			t.Fatal(err)
		}
	}
	original := batchFileSnapshot(t, dir)
	var out, stderr bytes.Buffer
	render := func(name string) []byte {
		path := filepath.Join(t.TempDir(), name+".avi")
		if Main([]string{"render", dir, "--output", path, "--apply-xmp-basic"}, &out, &stderr) != 0 {
			t.Fatal(&stderr)
		}
		data := denoiseRead(t, path)
		index := bytes.Index(data, []byte("movi"))
		if index < 0 {
			t.Fatal("missing video frames")
		}
		return data[index:]
	}
	before := render("before")
	args := []string{"set-sequence", dir, "Temperature", "6500.9"}
	if Main(args, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	if !strings.Contains(out.String(), `WhiteBalance="Custom" Temperature="6500" Tint="20"`) || !strings.Contains(out.String(), "IncrementalTemperature=(absent)") {
		t.Fatal("WB side effects absent from dry-run preview", &out)
	}
	if !reflect.DeepEqual(original, batchFileSnapshot(t, dir)) {
		t.Fatal("dry run changed files")
	}
	if Main(append(args, "--write"), &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	after := render("after")
	if bytes.Equal(before, after) {
		t.Fatal("WB edit did not change pixels")
	}
	for _, frame := range frames {
		packet, err := PacketForFrame(frame)
		if err != nil {
			t.Fatal(err)
		}
		for name, expected := range map[string]string{"WhiteBalance": "Custom", "Temperature": "6500", "Tint": "20"} {
			if value, found, _ := packet.GetRootScalar("crs:" + name); !found || value != expected {
				t.Fatal(name, value)
			}
		}
		for _, name := range []string{"IncrementalTemperature", "IncrementalTint"} {
			if _, present, _ := packet.GetRootScalar("crs:" + name); present {
				t.Fatal("inactive pair retained", name)
			}
		}
		backups, err := filepath.Glob(frame.Sidecar + "*.bak")
		if err != nil || len(backups) != 1 {
			t.Fatal(backups, err)
		}
		assertPreviewEVFileBytes(t, backups[0], original[frame.Sidecar])
		assertPreviewEVFileBytes(t, frame.Image, original[frame.Image])
	}
	saved := batchFileSnapshot(t, dir)
	if Main(append(args, "--write"), &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	if !reflect.DeepEqual(saved, batchFileSnapshot(t, dir)) {
		t.Fatal("repeat WB edit wrote more files")
	}
	for _, frame := range frames {
		packet := pasteTestPacket(t, string(original[frame.Sidecar]))
		for name, value := range map[string]string{"WhiteBalance": "Custom", "Temperature": "6500", "Tint": "20"} {
			if err := packet.SetScalar("crs:"+name, value); err != nil {
				t.Fatal(err)
			}
		}
		for _, name := range []string{"IncrementalTemperature", "IncrementalTint"} {
			if err := packet.RemoveProperty("crs:" + name); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := WritePacket(frame.Sidecar, packet, false); err != nil {
			t.Fatal(err)
		}
	}
	if !bytes.Equal(after, render("control")) {
		t.Fatal("edited WB differs from explicit metadata control")
	}
	// A following native-profile copy must keep the edited WB active.
	ref := filepath.Join(t.TempDir(), "reference.xmp")
	if err := os.WriteFile(ref, original[frames[0].Sidecar], 0600); err != nil {
		t.Fatal(err)
	}
	if Main([]string{"copy-settings", ref, dir, "--properties", "Contrast2012", "--write"}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	for _, frame := range frames {
		packet, err := PacketForFrame(frame)
		if err != nil {
			t.Fatal(err)
		}
		if value, present, _ := packet.GetRootScalar("crs:Temperature"); !present || value != "6500" {
			t.Fatal("later save lost edited WB", value)
		}
	}
}

func TestWBEditCLILateValidationAndSinglePreview(t *testing.T) {
	dir, frames := frameSelectionFixture(t)
	packet, err := PacketForFrame(frames[len(frames)-1])
	if err != nil {
		t.Fatal(err)
	}
	description := packet.rootDescriptions()[0]
	namespace := ensurePrefix(description, "crs", crsNamespace)
	structured := description.CreateElement(namespace + ":Tint")
	structured.CreateAttr("rdf:parseType", "Resource")
	structured.CreateElement(namespace + ":Keep").SetText("20")
	if _, err := WritePacket(frames[len(frames)-1].Sidecar, packet, false); err != nil {
		t.Fatal(err)
	}
	before := batchFileSnapshot(t, dir)
	var out, stderr bytes.Buffer
	if Main([]string{"set-sequence", dir, "Temperature", "6000", "--write"}, &out, &stderr) == 0 {
		t.Fatal("accepted late invalid WB companion")
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("late validation left earlier writes")
	}
	for _, frame := range frames[:1] {
		out.Reset()
		if Main([]string{"set", frame.Image, "Temperature", "999999", "--dry-run"}, &out, &stderr) != 0 {
			t.Fatal(&stderr)
		}
		if !strings.Contains(out.String(), `Temperature="50000"`) || !strings.Contains(out.String(), `WhiteBalance="Custom"`) {
			t.Fatal("single preview missing bounded state", &out)
		}
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("single dry run wrote metadata")
	}
}
