package lrtl

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func iiqDimensionsObservations(t *testing.T) rawDimensionsReference {
	t.Helper()
	return loadRawDimensionsReference(t, "testdata/lrt-iiq-dimensions-7.5.5.json", 52, 2)
}

func iiqDimensionInput(t *testing.T, name string) []byte {
	t.Helper()
	for _, row := range iiqDimensionsObservations(t).Cases {
		if row.Name == name {
			return append([]byte(nil), row.Data...)
		}
	}
	t.Fatal("missing IIQ fixture", name)
	return nil
}

func TestIIQDimensionsNativeImageSizeAndSave(t *testing.T) {
	assertRawDimensionsReference(t, iiqDimensionsObservations(t))
}
func TestIIQDimensionsInstalledImageSizeAndSave(t *testing.T) {
	assertInstalledRawDimensions(t, iiqDimensionsObservations(t))
}

func iiqSourceObservations(t *testing.T) xmpSourceReference {
	t.Helper()
	var r xmpSourceReference
	if err := json.Unmarshal(denoiseRead(t, "testdata/lrt-iiq-xmp-source-7.5.5.json"), &r); err != nil {
		t.Fatal(err)
	}
	if !r.Completed || len(r.Cases) != 6 {
		t.Fatal("invalid IIQ file reader observations")
	}
	return r
}

func TestIIQSourceNativeFileReader(t *testing.T) {
	for _, row := range iiqSourceObservations(t).Cases {
		t.Run(row.Name, func(t *testing.T) {
			frame := writeXMPSourceObservation(t, t.TempDir(), row)
			packet, found, _, err := loadFramePacketSource(frame, true)
			if err != nil {
				t.Fatal(err)
			}
			if row.Result == nil {
				if found {
					t.Fatal("native IIQ reader ignored embedded XMP")
				}
			} else {
				if !found {
					t.Fatal("missing IIQ sidecar")
				}
				assertPastedMetadata(t, packet, *row.Result)
			}
			if row.Sidecar == nil {
				packet, found, _, err = loadFramePacketSource(frame, false)
				if err != nil || !found {
					t.Fatal("general read lost embedded XMP", found, err)
				}
				if value, _, _ := packet.GetRootScalar("crs:Exposure2012"); value != "0.5" {
					t.Fatal(value)
				}
			}
		})
	}
}

func TestIIQSourceInstalledFileReader(t *testing.T) {
	jar := os.Getenv("LRTL_NATIVE_JAR")
	if jar == "" {
		t.Skip("set LRTL_NATIVE_JAR for IIQ file reader replay")
	}
	dir := t.TempDir()
	inputs := filepath.Join(dir, "inputs")
	for _, row := range iiqSourceObservations(t).Cases {
		writeXMPSourceObservation(t, filepath.Join(inputs, row.Name), row)
	}
	root, err := filepath.Abs(filepath.Join("..", "..", "scripts", "probes"))
	if err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("javac", "-encoding", "UTF-8", "-d", dir, filepath.Join(root, "MetadataPasteOracle.java"), filepath.Join(root, "XMPSourceOracle.java")).CombinedOutput(); err != nil {
		t.Fatalf("compile: %v %s", err, out)
	}
	path := filepath.Join(dir, "replay.json")
	if out, err := exec.Command("java", "--enable-native-access=ALL-UNNAMED", "-cp", dir+string(os.PathListSeparator)+jar, "lrtlprobe.XMPSourceOracle", path, inputs).CombinedOutput(); err != nil {
		t.Fatalf("native IIQ source: %v %s", err, out)
	}
	var replay xmpSourceReference
	if err := json.Unmarshal(denoiseRead(t, path), &replay); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(replay, iiqSourceObservations(t)) {
		t.Fatal("native IIQ file reader changed")
	}
}

func TestIIQEmbeddedXMPInstalled(t *testing.T) {
	exiftool := os.Getenv("LRTL_ICC_EXIFTOOL")
	if exiftool == "" {
		t.Skip("set LRTL_ICC_EXIFTOOL for installed embedded XMP replay")
	}
	for _, name := range []string{"le-preview.iiq", "be-preview.iiq"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), name)
			data := iiqDimensionInput(t, name)
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			native, err := exec.Command(exiftool, "-b", "-XMP", path).Output()
			if err != nil {
				t.Fatal(err)
			}
			ours, present, err := readEmbeddedImageXMP(path)
			if err != nil || !present {
				t.Fatal(present, err)
			}
			assertPastedMetadata(t, pasteTestPacket(t, string(ours)), string(native))
			assertPreviewEVFileBytes(t, path, data)
		})
	}
}

func TestIIQDimensionsPreviewXMPInitializationAndAVI(t *testing.T) {
	for _, endian := range []string{"le", "be"} {
		t.Run(endian, func(t *testing.T) {
			data := iiqDimensionInput(t, endian+"-preview.iiq")
			dir, control := t.TempDir(), t.TempDir()
			var out, stderr bytes.Buffer
			for i := 0; i < 3; i++ {
				name := fmt.Sprintf("frame-%d", i)
				path := filepath.Join(dir, name+".IIQ")
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
				if !supportsEmbeddedRawPreview(path) || !supportsRawDevelopment(path) {
					t.Fatal("IIQ RAW support not connected")
				}
				preview, err := extractRawPreview(path, data)
				if err != nil {
					t.Fatal(err)
				}
				config, format, err := image.DecodeConfig(bytes.NewReader(preview))
				if err != nil || format != "jpeg" || config.Width != 64 || config.Height != 32 {
					t.Fatal(config, format, err)
				}
				if err := os.WriteFile(filepath.Join(control, name+".jpg"), preview, 0600); err != nil {
					t.Fatal(err)
				}
				embedded, present, err := readEmbeddedImageXMP(path)
				if err != nil || !present {
					t.Fatal(present, err)
				}
				packet := pasteTestPacket(t, string(embedded))
				if _, err := WritePacket(filepath.Join(control, name+".xmp"), packet, false); err != nil {
					t.Fatal(err)
				}
				sidecar := filepath.Join(dir, name+".xmp")
				if _, err := WritePacket(sidecar, packet, false); err != nil {
					t.Fatal(err)
				}
				stamp := time.Now().Add(-time.Hour)
				if err := os.Chtimes(sidecar, stamp, stamp); err != nil {
					t.Fatal(err)
				}
			}
			before := batchFileSnapshot(t, dir)
			if Main([]string{"init", dir}, &out, &stderr) != 0 || !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
				t.Fatal("preview changed files", &stderr)
			}
			if Main([]string{"init", dir, "--write"}, &out, &stderr) != 0 {
				t.Fatal(&stderr)
			}
			frames, err := DiscoverSequence(dir)
			if err != nil || len(frames) != 3 {
				t.Fatal(frames, err)
			}
			for _, frame := range frames {
				packet, err := PacketForFrame(frame)
				if err != nil {
					t.Fatal(err)
				}
				for key, want := range map[string]string{"lrt:Width": "6000", "lrt:Height": "4000", "crs:Exposure2012": "0.500000"} {
					value, found, err := packet.GetRootScalar(key)
					if err != nil || !found || value != want {
						t.Fatal(key, value, found, err)
					}
				}
				nodes, err := pastePacketNodes(packet)
				if err != nil {
					t.Fatal(err)
				}
				keep := selectedField(nodes, pasteName{"urn:lrtl:iiq:fixture", "Keep"})
				if keep == nil || keep.Kind != "scalar" || keep.Value != "embedded" {
					t.Fatal("unknown embedded XMP lost", keep)
				}
				backups, err := filepath.Glob(frame.Sidecar + "*.bak")
				if err != nil || len(backups) != 1 {
					t.Fatal(backups, err)
				}
				assertPreviewEVFileBytes(t, backups[0], before[frame.Sidecar])
				assertPreviewEVFileBytes(t, frame.Image, data)
			}
			after := batchFileSnapshot(t, dir)
			if Main([]string{"init", dir, "--write"}, &out, &stderr) != 0 || !reflect.DeepEqual(after, batchFileSnapshot(t, dir)) {
				t.Fatal("repeat changed files", &stderr)
			}
			render := func(d string, raw bool) []byte {
				path := filepath.Join(t.TempDir(), "video.avi")
				args := []string{"render", d, "--output", path, "--apply-xmp-basic"}
				if raw {
					args = append(args, "--raw-preview")
				}
				if Main(args, &out, &stderr) != 0 {
					t.Fatal(&stderr)
				}
				avi := denoiseRead(t, path)
				offset := bytes.Index(avi, []byte("movi"))
				if offset < 0 {
					t.Fatal("missing AVI payload")
				}
				return avi[offset:]
			}
			if !bytes.Equal(render(dir, true), render(control, false)) {
				t.Fatal("IIQ preview/XMP AVI differs from extracted JPEG control")
			}
		})
	}
}

func TestIIQDimensionsPreserveAndPreflight(t *testing.T) {
	for _, bad := range []bool{false, true} {
		t.Run(fmt.Sprint(bad), func(t *testing.T) {
			dir := t.TempDir()
			data := iiqDimensionInput(t, "le-preview.iiq")
			if err := os.WriteFile(filepath.Join(dir, "frame-0.iiq"), data, 0600); err != nil {
				t.Fatal(err)
			}
			var out, stderr bytes.Buffer
			if !bad {
				packet, err := PacketForFrame(Frame{Image: filepath.Join(dir, "frame-0.iiq")})
				if err != nil {
					t.Fatal(err)
				}
				if value, found, err := packet.GetRootScalar("crs:Exposure2012"); err != nil || !found || value != "0.5" {
					t.Fatal("missing embedded IIQ exposure", value, found, err)
				}
				if Main([]string{"init", dir, "--frame-state", "preserve", "--write"}, &out, &stderr) != 0 {
					t.Fatal(&stderr)
				}
				frames, _ := DiscoverSequence(dir)
				packet, err = PacketForFrame(frames[0])
				if err != nil {
					t.Fatal(err)
				}
				if _, found, _ := packet.GetRootScalar("lrt:Width"); found {
					t.Fatal("preserve added camera dimensions")
				}
				return
			}
			for _, kind := range []string{"overflow", "outside-ifd"} {
				input := iiqDimensionInput(t, "le-overflow-exif.iiq")
				if kind == "outside-ifd" {
					input = append([]byte(nil), data...)
					binary.LittleEndian.PutUint32(input[4:8], 0x7fffffff)
				}
				path := filepath.Join(dir, "frame-1.iiq")
				if err := os.WriteFile(path, input, 0600); err != nil {
					t.Fatal(err)
				}
				before := batchFileSnapshot(t, dir)
				if Main([]string{"init", dir, "--write"}, &out, &stderr) == 0 {
					t.Fatal("accepted malformed later IIQ", kind)
				}
				if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
					t.Fatal("failure changed files")
				}
			}
		})
	}
}

func FuzzIIQDimensions(f *testing.F) {
	data, err := os.ReadFile("testdata/lrt-iiq-dimensions-7.5.5.json")
	if err != nil {
		f.Fatal(err)
	}
	var r rawDimensionsReference
	if err := json.Unmarshal(data, &r); err != nil {
		f.Fatal(err)
	}
	for _, row := range r.Cases {
		f.Add(row.Data)
	}
	path := filepath.Join(f.TempDir(), "frame.iiq")
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 32768 {
			return
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		w, h, found, err := nativeImageSize(path)
		if err == nil && found {
			state, err := parseNativeCameraFields([]string{"frame", fmt.Sprintf("%dx%d", w, h)})
			if err != nil || state.Width != w || state.Height != h {
				t.Fatal("cache changed", state, err)
			}
		}
		if preview, err := extractRawPreview(path, data); err == nil {
			config, format, err := image.DecodeConfig(bytes.NewReader(preview))
			if err != nil || format != "jpeg" || config.Width < 1 || config.Height < 1 {
				t.Fatal(config, format, err)
			}
		}
		assertPreviewEVFileBytes(t, path, data)
	})
}
