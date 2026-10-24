package lrtl

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func TestFrameMetadataExposureLabels(t *testing.T) {
	for i, row := range measuredHolyGrailExifCases(t) {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "frame.jpg")
			if err := os.WriteFile(path, holyGrailExifJPEG(t, row), 0600); err != nil {
				t.Fatal(err)
			}
			state, err := readNativeCameraMetadata(Frame{Image: path}, EmptyXMPPacket())
			if err != nil || state == nil {
				t.Fatal(state, err)
			}
			if state.Width != 64 || state.Height != 32 || state.Aperture != row.ApertureLabel || state.Shutter != row.ShutterLabel {
				t.Fatal(state, row)
			}
		})
	}
}

func TestFrameMetadataStrictCameraNumbers(t *testing.T) {
	for _, text := range []string{"+ 6", "++6", "+-6", " 6 "} {
		if _, valid := nativeCameraInteger(text); valid {
			t.Fatal("accepted non-Java integer", text)
		}
	}
	for _, text := range []string{"+ 5.6", "++5.6", "+-5.6", "-+5.6"} {
		fields := make([]string, 16)
		fields[3] = text
		state, err := parseNativeCameraFields(fields)
		if err != nil || state.Aperture != "n/a" {
			t.Fatal(text, state, err)
		}
	}
}

func TestFrameMetadataPNGOrientationAndManualFallback(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		t.Run(order.String(), func(t *testing.T) {
			data, _ := pngWithTestOrientation(t, order, 6)
			path := filepath.Join(t.TempDir(), "frame.png")
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			packet := EmptyXMPPacket()
			for key, value := range map[string]string{"lrt:Aperture": "8", "lrt:ShutterSpeed": "1/125", "lrt:Iso": "200", "lrt:IsMergedHDR": "true", "lrt:ShootingMode": "Manual", "tiff:Orientation": "1"} {
				if err := packet.SetScalar(key, value); err != nil {
					t.Fatal(err)
				}
			}
			state, err := readNativeCameraMetadata(Frame{Image: path}, packet)
			if err != nil || state == nil {
				t.Fatal(state, err)
			}
			if state.Width != 32 || state.Height != 16 || state.Orientation != 6 || state.Aperture != "8" || state.Shutter != "1/125" || state.ISO != "200" || state.Mode != "Manual" || state.HDR != "true" {
				t.Fatal(state)
			}
			assertPreviewEVFileBytes(t, path, data)
		})
	}
}

func TestFrameMetadataLateInvalidPNGPreflight(t *testing.T) {
	frames := cropFixture(t, 2)
	data, _ := pngWithTestOrientation(t, binary.LittleEndian, 6)
	data[33+8+26] ^= 1 // eXIf CRC, after the 26-byte synthetic TIFF payload.
	if err := os.WriteFile(frames[1].Image, data, 0600); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(frames[0].Image)
	before := batchFileSnapshot(t, dir)
	var out, stderr bytes.Buffer
	if Main([]string{"init", dir, "--write"}, &out, &stderr) == 0 {
		t.Fatal("accepted corrupt PNG EXIF")
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("later metadata error wrote earlier frames")
	}
}

type frameMetadataReference struct {
	MetadataCompleted bool `json:"metadata_completed"`
	StoreCompleted    bool `json:"store_completed"`
	Stop              string
	Cases             []struct {
		Name          string
		Fields        []string
		Invalid       bool
		Target, Saved string
	}
}

func frameMetadataObservations(t *testing.T) frameMetadataReference {
	t.Helper()
	var reference frameMetadataReference
	if err := json.Unmarshal(denoiseRead(t, "testdata/lrt-frame-metadata-save-7.5.5.json"), &reference); err != nil {
		t.Fatal(err)
	}
	if !reference.MetadataCompleted || reference.StoreCompleted || reference.Stop != "history: null LRTimelapseMain.m()" || len(reference.Cases) != 75 {
		t.Fatal("invalid metadata save observations")
	}
	return reference
}

func TestFrameMetadataNativeCameraCache(t *testing.T) {
	for _, row := range frameMetadataObservations(t).Cases {
		t.Run(row.Name, func(t *testing.T) {
			packet := pasteTestPacket(t, row.Target)
			state, err := parseNativeCameraFields(row.Fields)
			if (err != nil) != row.Invalid {
				t.Fatal("metadata parse", err, row.Invalid)
			}
			if err != nil {
				return
			}
			if err := applyNativeCameraMetadata(packet, state); err != nil {
				t.Fatal(err)
			}
			if err := prepareNativeFrameState(packet, "RAW"); err != nil {
				t.Fatal(err)
			}
			assertPastedMetadata(t, packet, row.Saved)
		})
	}
}

func TestFrameMetadataInstalledCameraCache(t *testing.T) {
	jar := os.Getenv("LRTL_NATIVE_JAR")
	if jar == "" {
		t.Skip("set LRTL_NATIVE_JAR for metadata-reader/save comparison")
	}
	dir := t.TempDir()
	root, err := filepath.Abs(filepath.Join("..", "..", "scripts", "probes"))
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"-encoding", "UTF-8", "-d", dir}
	for _, name := range []string{"MetadataPasteOracle.java", "SelectedColumnOracle.java", "RegisteredSaveOracle.java", "FrameMetadataSaveOracle.java"} {
		args = append(args, filepath.Join(root, name))
	}
	if out, err := exec.Command("javac", args...).CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, out)
	}
	path := filepath.Join(dir, "replay.json")
	if out, err := exec.Command("java", "--enable-native-access=ALL-UNNAMED", "-cp", dir+string(os.PathListSeparator)+jar, "lrtlprobe.FrameMetadataSaveOracle", path).CombinedOutput(); err != nil {
		t.Fatalf("observer: %v\n%s", err, out)
	}
	var replay frameMetadataReference
	if err := json.Unmarshal(denoiseRead(t, path), &replay); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(replay, frameMetadataObservations(t)) {
		t.Fatal("installed metadata behavior changed")
	}
}

func TestFrameMetadataInitializationEXIFAndPreserve(t *testing.T) {
	c := measuredHolyGrailExifCases(t)[0]
	for _, profile := range []string{nativeSaveStateProfile, "preserve"} {
		t.Run(profile, func(t *testing.T) {
			dir := t.TempDir()
			image := filepath.Join(dir, "frame.jpg")
			original := holyGrailExifJPEG(t, c)
			if err := os.WriteFile(image, original, 0600); err != nil {
				t.Fatal(err)
			}
			packet := EmptyXMPPacket()
			if err := packet.SetScalar("exif:DateTimeOriginal", "2026-01-02T03:04:05"); err != nil {
				t.Fatal(err)
			}
			if err := packet.SetScalar("lrt:Width", "99"); err != nil {
				t.Fatal(err)
			}
			if _, err := WritePacket(filepath.Join(dir, "frame.xmp"), packet, false); err != nil {
				t.Fatal(err)
			}
			ensureTestSidecarNewer(t, image, filepath.Join(dir, "frame.xmp"))
			before := batchFileSnapshot(t, dir)
			var out, stderr bytes.Buffer
			args := []string{"init", dir, "--frame-state", profile}
			if Main(args, &out, &stderr) != 0 {
				t.Fatal(&stderr)
			}
			if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
				t.Fatal("preview changed files")
			}
			if Main(append(args, "--write"), &out, &stderr) != 0 {
				t.Fatal(&stderr)
			}
			frames, err := DiscoverSequence(dir)
			if err != nil {
				t.Fatal(err)
			}
			packet, err = PacketForFrame(frames[0])
			if err != nil {
				t.Fatal(err)
			}
			if profile == nativeSaveStateProfile {
				for name, want := range map[string]string{"lrt:ShutterSpeed": c.ShutterLabel, "lrt:Aperture": c.ApertureLabel, "lrt:ShootingMode": "-", "exif:DateTimeOriginal": "2026-01-02T03:04:05"} {
					got, present, err := packet.GetRootScalar(name)
					if err != nil || !present || got != want {
						t.Fatal(name, got, want, err)
					}
				}
				if got, _, _ := packet.GetRootScalar("lrt:Width"); got == "99" || got == "" {
					t.Fatal("image dimensions were not refreshed", got)
				}
			} else if got, _, _ := packet.GetRootScalar("lrt:Width"); got != "99" {
				t.Fatal("preserve profile refreshed metadata", got)
			}
			assertPreviewEVFileBytes(t, image, original)
			backups, err := filepath.Glob(filepath.Join(dir, "frame.xmp*.bak"))
			if err != nil || len(backups) != 1 {
				t.Fatal(backups, err)
			}
			assertPreviewEVFileBytes(t, backups[0], before[filepath.Join(dir, "frame.xmp")])
			settled := batchFileSnapshot(t, dir)
			if Main(append(args, "--write"), &out, &stderr) != 0 {
				t.Fatal(&stderr)
			}
			if !reflect.DeepEqual(settled, batchFileSnapshot(t, dir)) {
				t.Fatal("repeat changed files")
			}
		})
	}
}

func FuzzFrameMetadataCameraFields(f *testing.F) {
	f.Add("64x32", "5.6", "1/125", "200", "Manual", "1", "true")
	f.Add("badx2", "NaN", "", "", "", "６", "-")
	f.Fuzz(func(t *testing.T, dimensions, aperture, shutter, iso, mode, orientation, hdr string) {
		if len(dimensions)+len(aperture)+len(shutter)+len(iso)+len(mode)+len(orientation)+len(hdr) > 16384 {
			return
		}
		fields := make([]string, 16)
		fields[1], fields[3], fields[4], fields[5], fields[7], fields[10], fields[13] = dimensions, aperture, shutter, iso, mode, orientation, hdr
		state, err := parseNativeCameraFields(fields)
		if err != nil {
			return
		}
		packet := EmptyXMPPacket()
		before, _ := packet.Serialize()
		editErr := applyNativeCameraMetadata(packet, state)
		after, err := packet.Serialize()
		if err != nil {
			t.Fatal(err)
		}
		if editErr != nil {
			if !bytes.Equal(before, after) {
				t.Fatal("failed cache write mutated XMP")
			}
			return
		}
		if _, err := parseXMP(after); err != nil {
			t.Fatal(err)
		}
		if err := applyNativeCameraMetadata(packet, state); err != nil {
			t.Fatal(err)
		}
		repeated, err := packet.Serialize()
		if err != nil || !bytes.Equal(after, repeated) {
			t.Fatal("cache write not stable", err)
		}
	})
}
