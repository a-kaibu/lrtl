package lrtl

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestRegisteredInternalExposureFormat(t *testing.T) {
	data := denoiseRead(t, "testdata/internal-exposure-format-jdk25.tsv")
	rows := strings.Split(strings.TrimSpace(strings.ReplaceAll(string(data), "\r\n", "\n")), "\n")
	if len(rows) != 1426 {
		t.Fatal("formatter observation count", len(rows))
	}
	for _, row := range rows {
		fields := strings.Split(row, "\t")
		if len(fields) != 2 {
			t.Fatal("invalid format observation", row)
		}
		bits, err := strconv.ParseUint(fields[0], 16, 64)
		if err != nil {
			t.Fatal(err)
		}
		if got := registeredInternalExposureText(math.Float64frombits(bits)); got != fields[1] {
			t.Errorf("bits %s: got %s want %s", fields[0], got, fields[1])
		}
	}
}

func TestRegisteredInternalExposureInstalledFormat(t *testing.T) {
	if os.Getenv("LRTL_NATIVE_JAR") == "" {
		t.Skip("set LRTL_NATIVE_JAR for the JDK formatter replay")
	}
	path := filepath.Join(t.TempDir(), "format.tsv")
	probe, err := filepath.Abs(filepath.Join("..", "..", "scripts", "probes", "InternalExposureFormatOracle.java"))
	if err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("java", probe, path).CombinedOutput(); err != nil {
		t.Fatalf("formatter observer: %v\n%s", err, out)
	}
	replay := strings.ReplaceAll(string(denoiseRead(t, path)), "\r\n", "\n")
	frozen := strings.ReplaceAll(string(denoiseRead(t, "testdata/internal-exposure-format-jdk25.tsv")), "\r\n", "\n")
	if replay != frozen {
		t.Fatal("installed JDK formatting changed")
	}
}

func TestRegisteredSaveReferenceCoverage(t *testing.T) {
	rows := registeredSaveObservations(t).Cases
	names := map[string]bool{}
	for _, row := range rows {
		if names[row.Name] || row.SavedAgain == "" {
			t.Fatal("duplicate or incomplete observation", row.Name)
		}
		names[row.Name] = true
	}
	columns, err := selectedColumnRegistry()
	if err != nil {
		t.Fatal(err)
	}
	numeric := 0
	for _, column := range columns {
		if column.Index >= 5 && (column.Kind == "Double" || column.Kind == "Integer") {
			numeric++
			if !names["numeric-"+strconv.Itoa(column.Index)] {
				t.Fatal("missing numeric column", column.Property)
			}
		}
	}
	if numeric != 755 {
		t.Fatal("numeric registry coverage changed", numeric)
	}
}

func TestRegisteredSaveCLIProfilesAndBackups(t *testing.T) {
	for _, profile := range []string{nativeSaveStateProfile, "preserve"} {
		t.Run(profile, func(t *testing.T) {
			frames := cropFixture(t, 2)
			dir := filepath.Dir(frames[0].Image)
			for _, frame := range frames {
				packet := EmptyXMPPacket()
				for key, value := range map[string]string{"crs:Exposure2012": "1.23456789123", "crs:Contrast2012": "2.5", "crs:HasSettings": "True", "xmp:Rating": "-1", "lrt:Quality": "RAW"} {
					if err := packet.SetScalar(key, value); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := WritePacket(frame.Sidecar, packet, false); err != nil {
					t.Fatal(err)
				}
			}
			source := EmptyXMPPacket()
			if err := source.SetScalar("crs:WhiteBalance", "As Shot"); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "source.xmp")
			if _, err := WritePacket(path, source, false); err != nil {
				t.Fatal(err)
			}
			sourceBefore := denoiseRead(t, path)
			before := batchFileSnapshot(t, dir)
			args := []string{"copy-settings", path, dir, "--properties", "WhiteBalance", "--save-state", profile}
			var out, stderr bytes.Buffer
			if Main(args, &out, &stderr) != 0 {
				t.Fatal(&stderr)
			}
			if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
				t.Fatal("preview wrote files")
			}
			if Main(append(args, "--write"), &out, &stderr) != 0 {
				t.Fatal(&stderr)
			}
			for _, frame := range frames {
				packet, err := PacketForFrame(frame)
				if err != nil {
					t.Fatal(err)
				}
				want := map[string]string{"crs:Exposure2012": "1.234568", "crs:Contrast2012": "3", "xmp:Rating": "0"}
				if profile == "preserve" {
					want = map[string]string{"crs:Exposure2012": "1.23456789123", "crs:Contrast2012": "2.5", "xmp:Rating": "-1"}
				}
				for key, value := range want {
					got, present, err := packet.GetRootScalar(key)
					if err != nil || !present || got != value {
						t.Fatal(key, got, value, err)
					}
				}
				assertPreviewEVFileBytes(t, frame.Image, before[frame.Image])
				backups, err := filepath.Glob(frame.Sidecar + "*.bak")
				if err != nil || len(backups) != 1 {
					t.Fatal("backup missing", backups, err)
				}
				assertPreviewEVFileBytes(t, backups[0], before[frame.Sidecar])
			}
			assertPreviewEVFileBytes(t, path, sourceBefore)
		})
	}
}

func TestRegisteredSaveCLINamespaceCarryPreview(t *testing.T) {
	frames := cropFixture(t, 1)
	packet := EmptyXMPPacket()
	for key, value := range map[string]string{"lrt:Quality": "RAW", "crs:ProcessVersion": "15.4", "lrt:ProcessVersion": "foreign"} {
		if err := packet.SetScalar(key, value); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := WritePacket(frames[0].Sidecar, packet, false); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "empty.xmp")
	if _, err := WritePacket(source, EmptyXMPPacket(), false); err != nil {
		t.Fatal(err)
	}
	before := denoiseRead(t, frames[0].Sidecar)
	var out, stderr bytes.Buffer
	args := []string{"copy-settings", source, filepath.Dir(frames[0].Image), "--properties", "ProcessVersion"}
	if Main(args, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	if !strings.Contains(out.String(), `final ProcessVersion: "15.4"`) {
		t.Fatal("preview hid retained native value", &out)
	}
	assertPreviewEVFileBytes(t, frames[0].Sidecar, before)
	if Main(append(args, "--write"), &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	packet, err := PacketForFrame(frames[0])
	if err != nil {
		t.Fatal(err)
	}
	if value, present, err := packet.GetRootScalar("crs:ProcessVersion"); err != nil || !present || value != "15.4" {
		t.Fatal(value, present, err)
	}
	if _, present, err := packet.GetRootScalar("lrt:ProcessVersion"); err != nil || present {
		t.Fatal("wrong schema carry", present, err)
	}
}

func TestRegisteredSaveInvalidCacheDoesNotMutate(t *testing.T) {
	packet := EmptyXMPPacket()
	before, err := packet.Serialize()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := applyRegisteredSaveState(packet, registeredSaveContext{}); err == nil {
		t.Fatal("accepted incomplete cache")
	}
	after, err := packet.Serialize()
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("invalid cache changed packet", err)
	}
}

func TestHolyGrailExplicitUnratedInitialization(t *testing.T) {
	for _, text := range []string{"0", "0.0", "-0"} {
		packet := EmptyXMPPacket()
		if err := packet.SetScalar("xmp:Rating", text); err != nil {
			t.Fatal(err)
		}
		rating, reset, previous, err := holyGrailKeyframePlan(packet, 2)
		if err != nil || reset || rating != 2 || previous != text {
			t.Fatal(rating, reset, previous, err)
		}
		if err := packet.SetScalar(holyGrailKeyframeProperty, "2"); err != nil {
			t.Fatal(err)
		}
		if err := packet.SetScalar(holyGrailPreviousRatingProperty, previous); err != nil {
			t.Fatal(err)
		}
		if err := packet.SetScalar("xmp:Rating", "2"); err != nil {
			t.Fatal(err)
		}
		_, reset, previous, err = holyGrailKeyframePlan(packet, 0)
		if err != nil || !reset || previous != text {
			t.Fatal(reset, previous, err)
		}
	}
}

func FuzzRegisteredSave(f *testing.F) {
	data, err := os.ReadFile("testdata/lrt-registered-save-7.5.5.json")
	if err != nil {
		f.Fatal(err)
	}
	var reference registeredSaveReference
	if err := json.Unmarshal(data, &reference); err != nil {
		f.Fatal(err)
	}
	for i, row := range reference.Cases {
		if i%29 == 0 || strings.HasPrefix(row.Name, "clear-") || strings.HasPrefix(row.Name, "internal-") {
			f.Add(row.Target)
		}
	}
	f.Fuzz(func(t *testing.T, text string) {
		if len(text) > 32768 {
			return
		}
		packet, err := parseXMP([]byte(text))
		if err != nil {
			return
		}
		before, err := packet.Serialize()
		if err != nil {
			return
		}
		context, err := readRegisteredSaveContext(packet)
		if err != nil {
			return
		}
		_, editErr := applyRegisteredSaveState(packet, context)
		after, err := packet.Serialize()
		if err != nil {
			t.Fatal(err)
		}
		if editErr != nil {
			if !bytes.Equal(before, after) {
				t.Fatal("failed save mutated packet")
			}
			return
		}
		if _, err := parseXMP(after); err != nil {
			t.Fatal("invalid saved XML", err)
		}
		// The first save updates cached HasCrop, changing later null-cell
		// namespace carry on the second save. The cache then stabilizes.
		if _, err := applyRegisteredSaveState(packet, context); err != nil {
			t.Fatal(err)
		}
		settled, err := packet.Serialize()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := applyRegisteredSaveState(packet, context); err != nil {
			t.Fatal(err)
		}
		repeated, err := packet.Serialize()
		if err != nil || !bytes.Equal(settled, repeated) {
			t.Fatal("cached save did not stabilize", err)
		}
	})
}

type registeredSaveObservation struct {
	Name, Target       string
	Source, Properties *string
	Saved              string
	SavedAgain         string `json:"saved_again"`
}
type registeredSaveReference struct {
	StoreCompleted bool `json:"store_completed"`
	Stop           string
	Cases          []registeredSaveObservation
}

func registeredSaveObservations(t *testing.T) registeredSaveReference {
	t.Helper()
	var reference registeredSaveReference
	if err := json.Unmarshal(denoiseRead(t, "testdata/lrt-registered-save-7.5.5.json"), &reference); err != nil {
		t.Fatal(err)
	}
	if reference.StoreCompleted || reference.Stop != "history: null LRTimelapseMain.m()" || len(reference.Cases) != 864 {
		t.Fatal("invalid registered save reference provenance")
	}
	return reference
}

func TestRegisteredSaveNativePrefix(t *testing.T) {
	for _, row := range registeredSaveObservations(t).Cases {
		t.Run(row.Name, func(t *testing.T) {
			packet := pasteTestPacket(t, row.Target)
			var context registeredSaveContext
			var err error
			if row.Source != nil {
				source := pasteTestPacket(t, *row.Source)
				columns, err := parseSelectedColumns(*row.Properties)
				if err != nil {
					t.Fatal(err)
				}
				context, err = copiedRegisteredSaveContext(packet, source, columns)
				if err != nil {
					t.Fatal(err)
				}
			} else {
				context, err = readRegisteredSaveContext(packet)
				if err != nil {
					t.Fatal(err)
				}
			}
			if _, err := applyRegisteredSaveState(packet, context); err != nil {
				t.Fatal(err)
			}
			assertPastedMetadata(t, packet, row.Saved)
			if row.SavedAgain == "" {
				t.Fatal("missing second native store observation")
			}
			if _, err := applyRegisteredSaveState(packet, context); err != nil {
				t.Fatal(err)
			}
			assertPastedMetadata(t, packet, row.SavedAgain)
		})
	}
}

func TestRegisteredSaveInstalledPrefix(t *testing.T) {
	jar := os.Getenv("LRTL_NATIVE_JAR")
	if jar == "" {
		t.Skip("set LRTL_NATIVE_JAR for the installed registered-column save prefix")
	}
	dir := t.TempDir()
	root, err := filepath.Abs(filepath.Join("..", "..", "scripts", "probes"))
	if err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("javac", "-encoding", "UTF-8", "-d", dir, filepath.Join(root, "MetadataPasteOracle.java"), filepath.Join(root, "SelectedColumnOracle.java"), filepath.Join(root, "RegisteredSaveOracle.java")).CombinedOutput(); err != nil {
		t.Fatalf("compile observer: %v\n%s", err, out)
	}
	path := filepath.Join(dir, "replay.json")
	if out, err := exec.Command("java", "--enable-native-access=ALL-UNNAMED", "-cp", dir+string(os.PathListSeparator)+jar, "lrtlprobe.RegisteredSaveOracle", path).CombinedOutput(); err != nil {
		t.Fatalf("native prefix: %v\n%s", err, out)
	}
	var replay registeredSaveReference
	if err := json.Unmarshal(denoiseRead(t, path), &replay); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(replay, registeredSaveObservations(t)) {
		t.Fatal("installed save prefix changed")
	}
}
