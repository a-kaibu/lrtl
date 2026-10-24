package lrtl

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type maskPreparationReference struct {
	Completed bool `json:"helpers_completed"`
	Cases     []struct {
		Mode                       int
		Name, Input, Status, Saved string
		Error, Repeated            *string
	}
}

func maskPreparationObservations(t testing.TB) maskPreparationReference {
	t.Helper()
	file, err := os.Open("testdata/lrt-mask-preparation-7.5.5.json.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	var r maskPreparationReference
	if err := json.NewDecoder(io.LimitReader(reader, 32<<20)).Decode(&r); err != nil {
		t.Fatal(err)
	}
	if !r.Completed || len(r.Cases) != 684 {
		t.Fatal("incomplete native mask observations")
	}
	return r
}

func maskReferenceIDs(t *testing.T, r maskPreparationReference) map[string]int {
	t.Helper()
	for _, row := range r.Cases {
		if row.Mode == 2 && row.Name == "empty" {
			packet := pasteTestPacket(t, row.Saved)
			nodes, err := pastePacketNodes(packet)
			if err != nil {
				t.Fatal(err)
			}
			array := selectedField(nodes, pasteName{crsNamespace, "MaskGroupBasedCorrections"})
			ids := map[string]int{}
			for i, item := range array.Items {
				ids[fullMaskField(item, "CorrectionSyncID").Value] = i + 1
			}
			if len(ids) != 9 {
				t.Fatal("native default identity count", len(ids))
			}
			return ids
		}
	}
	t.Fatal("missing default identity sample")
	return nil
}

func normalizeMaskReference(nodes []*pasteNode, ids map[string]int) {
	for _, node := range nodes {
		if node.Name.URI == crsNamespace {
			if node.Name.Local == "CorrectionSyncID" {
				if slot, ok := ids[node.Value]; ok {
					node.Value = fullCopyMaskID(slot)
				}
			}
			if node.Name.Local == "CorrectionName" && strings.HasPrefix(node.Value, "LRT Mask ") {
				node.Value = "LumaFrame Mask " + strings.TrimPrefix(node.Value, "LRT Mask ")
			}
			if node.Name.Local == "MaskName" && node.Value == "LRT: edit me" {
				node.Value = "LumaFrame: edit me"
			}
		}
		normalizeMaskReference(node.Fields, ids)
		normalizeMaskReference(node.Items, ids)
		normalizeMaskReference(node.Qualifiers, ids)
	}
}

func assertPreparedMasks(t *testing.T, packet *XMPPacket, expected string, ids map[string]int) {
	t.Helper()
	want := pasteTestPacket(t, expected)
	wantNodes, err := pastePacketNodes(want)
	if err != nil {
		t.Fatal(err)
	}
	normalizeMaskReference(wantNodes, ids)
	gotNodes, err := pastePacketNodes(packet)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(pasteTestCanonical(gotNodes), pasteTestCanonical(wantNodes)) {
		for _, property := range []string{"MaskGroupBasedCorrections", "GradientBasedCorrections", "CircularGradientBasedCorrections", "PaintBasedCorrections"} {
			a, b := selectedField(gotNodes, pasteName{crsNamespace, property}), selectedField(wantNodes, pasteName{crsNamespace, property})
			if (a == nil) != (b == nil) || a != nil && !pasteNodesEqual(a, b) {
				t.Fatalf("native prepared %s differs: %s", property, preparedMaskDifference(a, b, property))
			}
		}
		t.Fatal("native prepared metadata differs")
	}
}

func preparedMaskDifference(a, b *pasteNode, path string) string {
	if a == nil || b == nil {
		return fmt.Sprintf("%s: missing node", path)
	}
	if a.Kind != b.Kind || a.Value != b.Value || a.Name != b.Name {
		return fmt.Sprintf("%s: got %s %q, want %s %q", path, a.Kind, a.Value, b.Kind, b.Value)
	}
	if len(a.Fields) != len(b.Fields) {
		return fmt.Sprintf("%s: %d fields, want %d", path, len(a.Fields), len(b.Fields))
	}
	if len(a.Items) != len(b.Items) {
		return fmt.Sprintf("%s: %d items, want %d", path, len(a.Items), len(b.Items))
	}
	for _, left := range a.Fields {
		right := selectedField(b.Fields, left.Name)
		if right == nil || !pasteNodesEqual(left, right) {
			return preparedMaskDifference(left, right, path+"/"+left.Name.Local)
		}
	}
	for i, left := range a.Items {
		if !pasteNodesEqual(left, b.Items[i]) {
			return preparedMaskDifference(left, b.Items[i], fmt.Sprintf("%s[%d]", path, i+1))
		}
	}
	return path + ": qualifiers differ"
}

func TestFullCopyMasksNativeReference(t *testing.T) {
	r := maskPreparationObservations(t)
	ids := maskReferenceIDs(t, r)
	failures := 0
	for _, row := range r.Cases {
		t.Run(fmt.Sprintf("%d/%s", row.Mode, row.Name), func(t *testing.T) {
			packet := pasteTestPacket(t, row.Input)
			before, _ := packet.Serialize()
			format := "legacy"
			if row.Mode == 2 {
				format = "modern"
			} else if row.Mode == 3 {
				format = "reload"
			}
			_, err := prepareFullCopyMasks(packet, format)
			if row.Status == "error" {
				failures++
				if err == nil {
					t.Fatal("accepted native-invalid masks")
				}
				after, _ := packet.Serialize()
				if !bytes.Equal(before, after) {
					t.Fatal("failed preparation changed packet")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			assertPreparedMasks(t, packet, row.Saved, ids)
			if _, err := prepareFullCopyMasks(packet, format); err != nil {
				t.Fatal(err)
			}
			if row.Repeated == nil {
				t.Fatal("missing native repeat")
			}
			assertPreparedMasks(t, packet, *row.Repeated, ids)
		})
	}
	if failures != 94 {
		t.Fatal("native error coverage changed", failures)
	}
}

func TestFullCopyMasksInstalledReplay(t *testing.T) {
	jar := os.Getenv("LRTL_NATIVE_JAR")
	if jar == "" {
		t.Skip("set LRTL_NATIVE_JAR for mask preparation replay")
	}
	dir := t.TempDir()
	root, err := filepath.Abs(filepath.Join("..", "..", "scripts", "probes"))
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"-encoding", "UTF-8", "-d", dir}
	for _, name := range []string{"MetadataPasteOracle.java", "SelectedColumnOracle.java", "RegisteredSaveOracle.java", "MaskPreparationOracle.java"} {
		args = append(args, filepath.Join(root, name))
	}
	if out, err := exec.Command("javac", args...).CombinedOutput(); err != nil {
		t.Fatalf("compile observer: %v\n%s", err, out)
	}
	path := filepath.Join(dir, "observations.json")
	if out, err := exec.Command("java", "--enable-native-access=ALL-UNNAMED", "-XX:-OmitStackTraceInFastThrow", "-cp", jar+string(os.PathListSeparator)+dir, "lrtlprobe.MaskPreparationOracle", path).CombinedOutput(); err != nil {
		t.Fatalf("native observer: %v\n%s", err, out)
	}
	var replay maskPreparationReference
	if err := json.Unmarshal(denoiseRead(t, path), &replay); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(replay, maskPreparationObservations(t)) {
		t.Fatal("native mask preparation observations changed")
	}
	// Also pass independently generated Go packets through the installed Adobe
	// SDK. These samples cover all three helpers and several populated layouts.
	packets := map[string]*XMPPacket{}
	for i, row := range replay.Cases {
		if row.Status != "ok" || i%23 != 0 {
			continue
		}
		packet := pasteTestPacket(t, row.Input)
		format := []string{"reload", "legacy", "modern"}[row.Mode%3]
		if _, err := prepareFullCopyMasks(packet, format); err != nil {
			t.Fatal(err)
		}
		data, err := packet.Serialize()
		if err != nil {
			t.Fatal(err)
		}
		stem := fmt.Sprintf("packet-%03d", i)
		if err := os.WriteFile(filepath.Join(dir, stem+".input.xmp"), data, 0600); err != nil {
			t.Fatal(err)
		}
		packets[stem] = packet
	}
	if len(packets) < 15 {
		t.Fatal("insufficient Adobe parser sample coverage", len(packets))
	}
	if out, err := exec.Command("java", "--enable-native-access=ALL-UNNAMED", "-cp", jar+string(os.PathListSeparator)+dir, "lrtlprobe.MetadataPasteOracle", "--roundtrip", dir).CombinedOutput(); err != nil {
		t.Fatalf("Adobe parser roundtrip: %v\n%s", err, out)
	}
	for stem, packet := range packets {
		assertPastedMetadata(t, packet, string(denoiseRead(t, filepath.Join(dir, stem+".native.xmp"))))
	}
}

func fullCopyMaskSource(t *testing.T, mode int) *XMPPacket {
	t.Helper()
	for _, row := range maskPreparationObservations(t).Cases {
		if row.Mode == mode && row.Name == "empty" {
			packet := pasteTestPacket(t, row.Saved)
			for key, value := range map[string]string{"crs:Exposure2012": ".75", "crs:HasSettings": "False", "xmp:CreateDate": "2001-source"} {
				if err := packet.SetScalar(key, value); err != nil {
					t.Fatal(err)
				}
			}
			format := "legacy"
			if mode == 2 {
				format = "modern"
			}
			for slot, value := range map[int]string{5: ".25", 6: ".05", 7: ".125"} {
				correction := existingInitializationSlot(packet, format, slot)
				if err := setCRSElementValue(correction, "LocalExposure2012", value); err != nil {
					t.Fatal(err)
				}
			}
			return packet
		}
	}
	t.Fatal("missing native mask source")
	return nil
}

func TestFullCopyMasksDeterministic(t *testing.T) {
	for _, format := range []string{"modern", "legacy", "reload"} {
		var first []byte
		for i := 0; i < 25; i++ {
			packet := EmptyXMPPacket()
			if _, err := prepareFullCopyMasks(packet, format); err != nil {
				t.Fatal(err)
			}
			data, err := packet.Serialize()
			if err != nil {
				t.Fatal(err)
			}
			if i == 0 {
				first = data
			} else if !bytes.Equal(data, first) {
				t.Fatal("nondeterministic mask XML would conflict on shared sidecars", format)
			}
			if changed, err := prepareFullCopyMasks(packet, format); err != nil || changed {
				t.Fatal("default preparation is not stable", format, changed, err)
			}
		}
	}
	ids := map[string]bool{}
	for i := 1; i <= 9; i++ {
		id := fullCopyMaskID(i)
		if len(id) != 32 || ids[id] {
			t.Fatal("invalid/colliding mask identity", id)
		}
		ids[id] = true
	}
}

func TestFullCopyMasksCLISelectionBackupAndRepeat(t *testing.T) {
	for _, mode := range []int{1, 2} {
		for _, profile := range []string{nativeSaveStateProfile, "preserve"} {
			t.Run(fmt.Sprintf("%d/%s", mode, profile), func(t *testing.T) {
				dir := t.TempDir()
				frame := metadataDateFrame(t, dir, "one.jpg", "False", "qualified")
				metadataDateFrame(t, dir, "two.jpg", "False", "qualified")
				source := fullCopyMaskSource(t, mode)
				path := filepath.Join(t.TempDir(), "reference.xmp")
				if _, err := WritePacket(path, source, false); err != nil {
					t.Fatal(err)
				}
				sourceBefore := denoiseRead(t, path)
				before := batchFileSnapshot(t, dir)
				var out, stderr bytes.Buffer
				args := []string{"copy-settings", path, dir, "--filename-contains", "one", "--save-state", profile}
				if Main(args, &out, &stderr) != 0 {
					t.Fatal(&stderr)
				}
				if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
					t.Fatal("mask preview changed files")
				}
				if profile == nativeSaveStateProfile && !strings.Contains(out.String(), "MaskGroupBasedCorrections: 9 correction(s)") {
					t.Fatal("preview omitted mask reconstruction", &out)
				}
				if Main(append(args, "--write"), &out, &stderr) != 0 {
					t.Fatal(&stderr)
				}
				saved, err := PacketForFrame(frame)
				if err != nil {
					t.Fatal(err)
				}
				format := "modern"
				if profile == "preserve" && mode == 1 {
					format = "legacy"
				}
				if packetMaskFormat(saved) != format {
					t.Fatal("unexpected prepared layout", packetMaskFormat(saved))
				}
				for index, role := range []string{"HG", "Deflicker", "Global"} {
					value := []float64{.25, .05, .125}[index]
					target := findLRTExposureTarget(saved, role, index+5)
					got, present, err := getXMPExposureTargetValue(saved, target)
					if err != nil || !present || got != value {
						t.Fatal("mask exposure changed", role, got, value, err)
					}
				}
				if profile == nativeSaveStateProfile {
					for slot := 1; slot <= 9; slot++ {
						element := existingInitializationSlot(saved, "modern", slot)
						id, found, err := crsElementValue(element, "CorrectionSyncID")
						if err != nil || !found || id != fullCopyMaskID(slot) {
							t.Fatal("incorrect mask identity", slot, id, err)
						}
					}
				}
				assertPreviewEVFileBytes(t, filepath.Join(dir, "two.jpg.xmp"), before[filepath.Join(dir, "two.jpg.xmp")])
				backups, _ := filepath.Glob(frame.Sidecar + ".lrtl-*.bak")
				if len(backups) != 1 || !bytes.Equal(denoiseRead(t, backups[0]), before[frame.Sidecar]) {
					t.Fatal("missing exact original mask backup")
				}
				after := batchFileSnapshot(t, dir)
				if Main(append(args, "--write"), &out, &stderr) != 0 {
					t.Fatal(&stderr)
				}
				if !reflect.DeepEqual(after, batchFileSnapshot(t, dir)) {
					t.Fatal("repeat mask copy rewrote files")
				}
				if !bytes.Equal(sourceBefore, denoiseRead(t, path)) {
					t.Fatal("mask source changed")
				}
			})
		}
	}
}

func TestFullCopyMasksLegacyToModernAVIPixels(t *testing.T) {
	actual, control := t.TempDir(), t.TempDir()
	frame := metadataDateFrame(t, actual, "frame.jpg", "False", "qualified")
	controlFrame := metadataDateFrame(t, control, "frame.jpg", "False", "qualified")
	source := fullCopyMaskSource(t, 1)
	if _, err := WritePacket(controlFrame.Sidecar, source, false); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "reference.xmp")
	if _, err := WritePacket(path, source, false); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	render := func(dir string) []byte {
		output := filepath.Join(t.TempDir(), "render.avi")
		if Main([]string{"render", dir, "--output", output, "--apply-xmp-basic"}, &out, &stderr) != 0 {
			t.Fatal(&stderr)
		}
		data := denoiseRead(t, output)
		offset := bytes.Index(data, []byte("movi"))
		if offset < 0 {
			t.Fatal("missing AVI frames")
		}
		return data[offset:]
	}
	imageBefore := denoiseRead(t, frame.Image)
	before := render(actual)
	if Main([]string{"copy-settings", path, actual, "--write"}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	after := render(actual)
	if bytes.Equal(before, after) {
		t.Fatal("nonzero copied exposures did not affect video")
	}
	if !bytes.Equal(after, render(control)) {
		t.Fatal("modern copied masks differ from legacy native-shape control pixels")
	}
	assertPreviewEVFileBytes(t, frame.Image, imageBefore)
}

func FuzzFullCopyMasks(f *testing.F) {
	for _, row := range maskPreparationObservations(f).Cases {
		f.Add([]byte(row.Input), uint8(row.Mode))
	}
	f.Fuzz(func(t *testing.T, data []byte, mode uint8) {
		if len(data) > 1<<16 {
			t.Skip()
		}
		packet, err := parseXMP(data)
		if err != nil {
			return
		}
		before, err := packet.Serialize()
		if err != nil {
			return
		}
		format := []string{"reload", "legacy", "modern"}[mode%3]
		_, prepareErr := prepareFullCopyMasks(packet, format)
		after, err := packet.Serialize()
		if err != nil {
			t.Fatal(err)
		}
		if prepareErr != nil {
			if !bytes.Equal(before, after) {
				t.Fatal("failed mask preparation mutated source")
			}
			return
		}
		reloaded, err := parseXMP(after)
		if err != nil {
			t.Fatal("invalid prepared XML", err)
		}
		a, err := pastePacketNodes(packet)
		if err != nil {
			t.Fatal(err)
		}
		b, err := pastePacketNodes(reloaded)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(pasteTestCanonical(a), pasteTestCanonical(b)) {
			t.Fatal("prepared masks changed on serialization")
		}
	})
}
