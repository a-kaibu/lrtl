package lrtl

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

type reloadDefaultsReference struct {
	Completed bool `json:"factory_completed"`
	Cases     []struct {
		Filename string
		Quality  *string
		Packet   string
	}
	Merges []struct{ Filename, Quality, Input, Packet string }
}

func assertReloadDefaults(t *testing.T, packet *XMPPacket, expected string) {
	t.Helper()
	want, err := pastePacketNodes(pasteTestPacket(t, expected))
	if err != nil {
		t.Fatal(err)
	}
	got, err := pastePacketNodes(packet)
	if err != nil {
		t.Fatal(err)
	}
	if len(want) != len(got) {
		t.Fatalf("default roots: got %d, want %d", len(got), len(want))
	}
	for _, node := range want {
		actual := selectedField(got, node.Name)
		if actual == nil || !pasteNodesEqual(actual, node) {
			t.Fatal(preparedMaskDifference(actual, node, node.Name.Local))
		}
	}
}

func reloadDefaultsObservations(t testing.TB) reloadDefaultsReference {
	t.Helper()
	file, err := os.Open("testdata/lrt-reload-defaults-7.5.5.json.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	var r reloadDefaultsReference
	if err := json.NewDecoder(reader).Decode(&r); err != nil {
		t.Fatal(err)
	}
	if !r.Completed || len(r.Cases) != 84 || len(r.Merges) != 432 {
		t.Fatal("incomplete reload defaults reference")
	}
	return r
}

func TestFullCopyDefaultsFactoryReference(t *testing.T) {
	for i, row := range reloadDefaultsObservations(t).Cases {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			quality := ""
			if row.Quality != nil {
				quality = *row.Quality
			}
			packet := EmptyXMPPacket()
			if _, err := writeChangedPasteRoots(packet, map[pasteName]*pasteNode{}, fullCopyDefaultNodes(row.Filename, quality)); err != nil {
				t.Fatal(err)
			}
			assertReloadDefaults(t, packet, row.Packet)
		})
	}
}

func TestFullCopyDefaultsMergeReference(t *testing.T) {
	for i, row := range reloadDefaultsObservations(t).Merges {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			packet := pasteTestPacket(t, row.Input)
			if _, err := prepareFullCopyDefaults(packet, row.Filename); err != nil {
				t.Fatal(err)
			}
			assertReloadDefaults(t, packet, row.Packet)
			before, _ := packet.Serialize()
			if changed, err := prepareFullCopyDefaults(packet, row.Filename); err != nil || changed {
				t.Fatal("default merge is not stable", changed, err)
			}
			after, _ := packet.Serialize()
			if !bytes.Equal(before, after) {
				t.Fatal("repeat defaults changed XML")
			}
		})
	}
}

func TestFullCopyDefaultsInstalledReplay(t *testing.T) {
	jar := os.Getenv("LRTL_NATIVE_JAR")
	if jar == "" {
		t.Skip("set LRTL_NATIVE_JAR for default factory replay")
	}
	dir := t.TempDir()
	root, err := filepath.Abs(filepath.Join("..", "..", "scripts", "probes"))
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"-encoding", "UTF-8", "-d", dir}
	for _, name := range []string{"MetadataPasteOracle.java", "SelectedColumnOracle.java", "RegisteredSaveOracle.java", "ReloadDefaultsOracle.java"} {
		args = append(args, filepath.Join(root, name))
	}
	if out, err := exec.Command("javac", args...).CombinedOutput(); err != nil {
		t.Fatalf("compile observer: %v\n%s", err, out)
	}
	path := filepath.Join(dir, "reference.json")
	if out, err := exec.Command("java", "--enable-native-access=ALL-UNNAMED", "-cp", jar+string(os.PathListSeparator)+dir, "lrtlprobe.ReloadDefaultsOracle", path).CombinedOutput(); err != nil {
		t.Fatalf("native observer: %v\n%s", err, out)
	}
	var replay reloadDefaultsReference
	if err := json.Unmarshal(denoiseRead(t, path), &replay); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(replay, reloadDefaultsObservations(t)) {
		t.Fatal("native default values/merge rules changed")
	}
	packets := map[string]*XMPPacket{}
	for i, row := range replay.Merges {
		if i%11 != 0 {
			continue
		}
		packet := pasteTestPacket(t, row.Input)
		if _, err := prepareFullCopyDefaults(packet, row.Filename); err != nil {
			t.Fatal(err)
		}
		data, err := packet.Serialize()
		if err != nil {
			t.Fatal(err)
		}
		stem := fmt.Sprintf("defaults-%03d", i)
		if err := os.WriteFile(filepath.Join(dir, stem+".input.xmp"), data, 0600); err != nil {
			t.Fatal(err)
		}
		packets[stem] = packet
	}
	if out, err := exec.Command("java", "--enable-native-access=ALL-UNNAMED", "-cp", jar+string(os.PathListSeparator)+dir, "lrtlprobe.MetadataPasteOracle", "--roundtrip", dir).CombinedOutput(); err != nil {
		t.Fatalf("Adobe parser roundtrip: %v\n%s", err, out)
	}
	for stem, packet := range packets {
		assertReloadDefaults(t, packet, string(denoiseRead(t, filepath.Join(dir, stem+".native.xmp"))))
	}
}

func TestFullCopyDefaultsCLIFrameContextAndRepeat(t *testing.T) {
	for _, ext := range []string{".jpg", ".png", ".nef"} {
		for _, quality := range []string{"RAW", "FLAT"} {
			for _, profile := range []string{nativeSaveStateProfile, "preserve"} {
				t.Run(ext+"/"+quality+"/"+profile, func(t *testing.T) {
					dir := t.TempDir()
					frame := metadataDateFrame(t, dir, "target"+ext, "False", "qualified")
					packet, err := PacketForFrame(frame)
					if err != nil {
						t.Fatal(err)
					}
					if err := packet.SetScalar("lrt:Quality", quality); err != nil {
						t.Fatal(err)
					}
					if _, err := WritePacket(frame.Sidecar, packet, false); err != nil {
						t.Fatal(err)
					}
					source := EmptyXMPPacket()
					if err := source.SetScalar("crs:Exposure2012", ".75"); err != nil {
						t.Fatal(err)
					}
					path := filepath.Join(t.TempDir(), "reference.xmp")
					if _, err := WritePacket(path, source, false); err != nil {
						t.Fatal(err)
					}
					sourceBefore := denoiseRead(t, path)
					before := batchFileSnapshot(t, dir)
					var out, stderr bytes.Buffer
					args := []string{"copy-settings", path, dir, "--save-state", profile}
					if Main(args, &out, &stderr) != 0 {
						t.Fatal(&stderr)
					}
					if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
						t.Fatal("default preview changed files")
					}
					if Main(append(args, "--write"), &out, &stderr) != 0 {
						t.Fatal(&stderr)
					}
					saved, err := PacketForFrame(frame)
					if err != nil {
						t.Fatal(err)
					}
					sharp, present, err := saved.GetRootScalar("crs:Sharpness")
					if err != nil {
						t.Fatal(err)
					}
					if profile == nativeSaveStateProfile {
						want := "25"
						if ext == ".jpg" || quality == "FLAT" {
							want = "0"
						}
						if !present || sharp != want {
							t.Fatal("frame defaults differ", sharp, want)
						}
						filename, found, err := saved.GetRootScalar("crs:RawFileName")
						if err != nil || found != (ext != ".jpg" && quality != "FLAT") || found && filename != "target"+ext {
							t.Fatal("incorrect target filename default", filename, found, err)
						}
						if value, found, _ := saved.GetRootScalar("crs:Exposure2012"); !found || value != "0.750000" {
							t.Fatal("defaults replaced source exposure", value)
						}
					} else if present {
						t.Fatal("preserve added defaults", sharp)
					}
					after := batchFileSnapshot(t, dir)
					if Main(append(args, "--write"), &out, &stderr) != 0 {
						t.Fatal(&stderr)
					}
					if !reflect.DeepEqual(after, batchFileSnapshot(t, dir)) {
						t.Fatal("repeat defaults rewrote files")
					}
					backups, _ := filepath.Glob(frame.Sidecar + ".lrtl-*.bak")
					if len(backups) != 1 || !bytes.Equal(denoiseRead(t, backups[0]), before[frame.Sidecar]) {
						t.Fatal("missing exact default-stage backup")
					}
					if !bytes.Equal(sourceBefore, denoiseRead(t, path)) {
						t.Fatal("changed default source")
					}
					assertPreviewEVFileBytes(t, frame.Image, before[frame.Image])
				})
			}
		}
	}
}

func FuzzFullCopyDefaults(f *testing.F) {
	r := reloadDefaultsObservations(f)
	for _, row := range r.Merges {
		f.Add([]byte(row.Input), row.Filename)
	}
	f.Fuzz(func(t *testing.T, data []byte, image string) {
		if len(data) > 1<<16 || len(image) > 1024 {
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
		_, prepareErr := prepareFullCopyDefaults(packet, image)
		after, err := packet.Serialize()
		if err != nil {
			t.Fatal(err)
		}
		if prepareErr != nil {
			if !bytes.Equal(before, after) {
				t.Fatal("failed default merge changed packet")
			}
			return
		}
		if _, err := parseXMP(after); err != nil {
			t.Fatal("invalid generated defaults", err)
		}
		if changed, err := prepareFullCopyDefaults(packet, image); err != nil || changed {
			t.Fatal("repeated default merge changed semantics", changed, err)
		}
		if current, _ := packet.Serialize(); !bytes.Equal(current, after) {
			t.Fatal("repeat defaults changed XML bytes")
		}
	})
}

func TestFullCopyDefaultsPreserveEmptyReference(t *testing.T) {
	dir := t.TempDir()
	frame := metadataDateFrame(t, dir, "frame.jpg", "False", "qualified")
	source := filepath.Join(t.TempDir(), "empty.xmp")
	if _, err := WritePacket(source, EmptyXMPPacket(), false); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	if Main([]string{"copy-settings", source, dir, "--save-state", "preserve", "--write"}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	saved, err := PacketForFrame(frame)
	if err != nil {
		t.Fatal(err)
	}
	for _, property := range []string{"crs:Exposure2012", "crs:Sharpness", "crs:Version"} {
		if _, found, err := saved.GetRootScalar(property); err != nil || found {
			t.Fatal("preserve-empty copied defaults", property, found, err)
		}
	}
}
