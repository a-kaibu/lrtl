package lrtl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type selectedDateReference struct {
	Completed bool   `json:"store_completed"`
	Marker    string `json:"clock_marker"`
	Cases     []struct {
		Target, Source, Properties, Status, Saved string
		Cache, Error                              *string
	}
}

func selectedDateObservations(t *testing.T) selectedDateReference {
	t.Helper()
	var r selectedDateReference
	if err := json.Unmarshal(denoiseRead(t, "testdata/lrt-selected-date-7.5.5.json"), &r); err != nil {
		t.Fatal(err)
	}
	if r.Completed || r.Marker == "" || len(r.Cases) != 640 {
		t.Fatal("incomplete native selected-date observations")
	}
	return r
}

func TestSelectedDateNativeReference(t *testing.T) {
	r := selectedDateObservations(t)
	failures := 0
	for i, row := range r.Cases {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			packet := pasteTestPacket(t, row.Target)
			columns, err := parseSelectedColumns(row.Properties)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := applyCopySettingsProfile(packet, pasteTestPacket(t, row.Source), columns, nativeSaveStateProfile); err != nil {
				t.Fatal(err)
			}
			date, _, err := captureTimestampScalar(packet, "xmp:CreateDate")
			if err != nil {
				t.Fatal(err)
			}
			wantDate := ""
			if row.Cache != nil {
				wantDate = *row.Cache
			}
			if date != wantDate {
				t.Fatalf("target cache changed: %q != %q", date, wantDate)
			}
			_, err = applyNativeMetadataDate(packet, date, r.Marker)
			if row.Status == "error" {
				failures++
				if err == nil || row.Error == nil || !strings.Contains(strings.ToLower(*row.Error), "composite") {
					t.Fatalf("native error not reproduced: %v; %v", err, row.Error)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			assertPastedMetadata(t, packet, row.Saved)
		})
	}
	if failures != 48 {
		t.Fatalf("unexpected structured-clock error coverage: %d", failures)
	}
}

func TestSelectedDatePreparationAtomicAndClock(t *testing.T) {
	r := selectedDateObservations(t)
	var good, bad *XMPPacket
	for _, row := range r.Cases {
		if row.Cache == nil || *row.Cache == "" {
			continue
		}
		packet := pasteTestPacket(t, row.Target)
		if row.Status == "error" {
			bad = packet
		} else if eligible, _ := nativeMetadataDateEligible(packet, *row.Cache); eligible {
			good = packet
		}
	}
	if good == nil || bad == nil {
		t.Fatal("missing test packets")
	}
	outputs := []packetOutput{{Path: "one.xmp", Packet: good}, {Path: "two.xmp", Packet: bad}}
	first, _ := good.Serialize()
	second, _ := bad.Serialize()
	calls := 0
	clock := func() time.Time {
		calls++
		return time.Date(2026, 10, 24, 3, 4, calls, 123, time.FixedZone("test", 32400))
	}
	if _, err := prepareCopySettingsWrites(outputs, clock); err == nil {
		t.Fatal("accepted late composite MetadataDate")
	}
	if calls != 0 {
		t.Fatal("used clock before validating batch")
	}
	if data, _ := good.Serialize(); !bytes.Equal(data, first) {
		t.Fatal("changed first input on error")
	}
	if data, _ := bad.Serialize(); !bytes.Equal(data, second) {
		t.Fatal("changed invalid input on error")
	}
	outputs[1].Packet = &XMPPacket{Document: good.Document.Copy()}
	prepared, err := prepareCopySettingsWrites(outputs, clock)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatal("missing per-frame clock", calls)
	}
	for i, output := range prepared {
		value, _, err := captureTimestampScalar(output.Packet, "xmp:MetadataDate")
		if err != nil || value != formatNativeMetadataDate(time.Date(2026, 10, 24, 3, 4, i+1, 0, time.FixedZone("test", 32400))) {
			t.Fatal("incorrect save clock", value, err)
		}
	}
	if data, _ := good.Serialize(); !bytes.Equal(data, first) {
		t.Fatal("changed plan packet on success")
	}
	if _, err := prepareCopySettingsWrites(nil, func() time.Time { t.Fatal("clock for empty plan"); return time.Time{} }); err != nil {
		t.Fatal(err)
	}
	for _, row := range r.Cases {
		packet := pasteTestPacket(t, row.Target)
		date, _, _ := captureTimestampScalar(packet, "xmp:CreateDate")
		if eligible, _ := nativeMetadataDateEligible(packet, date); eligible {
			continue
		}
		if _, err := prepareCopySettingsWrites([]packetOutput{{Path: "inactive.xmp", Packet: packet}}, func() time.Time { t.Fatal("clock for ineligible packet"); return time.Time{} }); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSelectedDateCLIPreviewBackupRepeatAndLateError(t *testing.T) {
	for _, profile := range []string{nativeSaveStateProfile, "preserve"} {
		t.Run(profile, func(t *testing.T) {
			dir := t.TempDir()
			frame := metadataDateFrame(t, dir, "one.jpg", "True", "qualified")
			source := filepath.Join(t.TempDir(), "source.xmp")
			p := EmptyXMPPacket()
			for key, value := range map[string]string{"crs:Exposure2012": ".75", "xmp:CreateDate": "2001-source", "crs:HasSettings": "True"} {
				if err := p.SetScalar(key, value); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := WritePacket(source, p, false); err != nil {
				t.Fatal(err)
			}
			sourceBytes := denoiseRead(t, source)
			before := batchFileSnapshot(t, dir)
			var out, stderr bytes.Buffer
			args := []string{"copy-settings", source, dir, "--properties", "Exposure2012", "--save-state", profile}
			if Main(args, &out, &stderr) != 0 {
				t.Fatal(&stderr)
			}
			if profile == nativeSaveStateProfile && !strings.Contains(out.String(), "MetadataDate: updated when saved") {
				t.Fatal("preview omitted save clock update")
			}
			if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
				t.Fatal("preview changed files")
			}
			start := time.Now().Add(-time.Second)
			if Main(append(args, "--write"), &out, &stderr) != 0 {
				t.Fatal(&stderr)
			}
			end := time.Now().Add(time.Second)
			saved, err := PacketForFrame(frame)
			if err != nil {
				t.Fatal(err)
			}
			date, _, _ := captureTimestampScalar(saved, "xmp:CreateDate")
			if date == "2001-source" || date == "" {
				t.Fatal("lost target capture date", date)
			}
			clock, _, err := captureTimestampScalar(saved, "xmp:MetadataDate")
			if err != nil {
				t.Fatal(err)
			}
			if profile == nativeSaveStateProfile {
				stamp, err := parseMetadataDateClock(clock)
				if err != nil || stamp.Before(start) || stamp.After(end) {
					t.Fatal("save clock outside window", clock, err)
				}
				for _, property := range []string{"exif:DateTimeOriginal", "xmp:ModifyDate", "photoshop:DateCreated"} {
					value, _, _ := captureTimestampScalar(saved, property)
					if value != date {
						t.Fatal("capture cache not saved", property, value, date)
					}
				}
			} else if clock != "previous" {
				t.Fatal("preserve profile changed clock", clock)
			}
			backups, err := filepath.Glob(frame.Sidecar + ".lrtl-*")
			if err != nil || len(backups) != 1 {
				t.Fatal("backup count", backups, err)
			}
			if !bytes.Equal(denoiseRead(t, backups[0]), before[frame.Sidecar]) {
				t.Fatal("backup not exact")
			}
			after := batchFileSnapshot(t, dir)
			if Main(append(args, "--write"), &out, &stderr) != 0 {
				t.Fatal(&stderr)
			}
			if !reflect.DeepEqual(after, batchFileSnapshot(t, dir)) {
				t.Fatal("repeat rewrote files")
			}
			if !bytes.Equal(denoiseRead(t, source), sourceBytes) {
				t.Fatal("changed reference")
			}
			if profile == nativeSaveStateProfile {
				metadataDateFrame(t, dir, "two.jpg", "True", "struct")
				// Make the earlier valid frame require a new save too. A late
				// validation error must prevent that earlier write and backup.
				if err := p.SetScalar("crs:Exposure2012", ".5"); err != nil {
					t.Fatal(err)
				}
				if _, err := WritePacket(source, p, false); err != nil {
					t.Fatal(err)
				}
				before = batchFileSnapshot(t, dir)
				if Main(append(args, "--write"), &out, &stderr) == 0 {
					t.Fatal("accepted late composite clock")
				}
				if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
					t.Fatal("late error partially wrote batch")
				}
			}
		})
	}
}

func TestSelectedDateInstalledReplay(t *testing.T) {
	jar := os.Getenv("LRTL_NATIVE_JAR")
	if jar == "" {
		t.Skip("set LRTL_NATIVE_JAR for native selected-date replay")
	}
	dir := t.TempDir()
	root, err := filepath.Abs(filepath.Join("..", "..", "scripts", "probes"))
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"-encoding", "UTF-8", "-d", dir}
	for _, name := range []string{"MetadataPasteOracle.java", "SelectedColumnOracle.java", "RegisteredSaveOracle.java", "SelectedDateOracle.java"} {
		args = append(args, filepath.Join(root, name))
	}
	if out, err := exec.Command("javac", args...).CombinedOutput(); err != nil {
		t.Fatalf("compile observer: %v\n%s", err, out)
	}
	path := filepath.Join(dir, "replay.json")
	if out, err := exec.Command("java", "--enable-native-access=ALL-UNNAMED", "-cp", jar+string(os.PathListSeparator)+dir, "lrtlprobe.SelectedDateOracle", path).CombinedOutput(); err != nil {
		t.Fatalf("native observer: %v\n%s", err, out)
	}
	var replay selectedDateReference
	if err := json.Unmarshal(denoiseRead(t, path), &replay); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(replay, selectedDateObservations(t)) {
		t.Fatal("native selected-date observations changed")
	}
}

func FuzzSelectedDateCopy(f *testing.F) {
	data, err := os.ReadFile("testdata/lrt-selected-date-7.5.5.json")
	if err != nil {
		f.Fatal(err)
	}
	var reference selectedDateReference
	if err := json.Unmarshal(data, &reference); err != nil {
		f.Fatal(err)
	}
	for _, row := range reference.Cases {
		f.Add([]byte(row.Target), []byte(row.Source), row.Properties)
	}
	f.Fuzz(func(t *testing.T, targetData, sourceData []byte, properties string) {
		if len(targetData) > 1<<16 || len(sourceData) > 1<<16 || len(properties) > 1024 {
			t.Skip()
		}
		target, err := parseXMP(targetData)
		if err != nil {
			return
		}
		source, err := parseXMP(sourceData)
		if err != nil {
			return
		}
		columns, err := parseSelectedColumns(properties)
		if err != nil || len(columns) == 0 {
			return
		}
		before, err := target.Serialize()
		if err != nil {
			return
		}
		sourceBefore, err := source.Serialize()
		if err != nil {
			return
		}
		date, found, dateErr := captureTimestampScalar(target, "xmp:CreateDate")
		_, copyErr := applyCopySettingsProfile(target, source, columns, nativeSaveStateProfile)
		after, _ := target.Serialize()
		if copyErr != nil {
			if !bytes.Equal(before, after) {
				t.Fatal("failed copy changed target")
			}
		} else {
			got, present, err := captureTimestampScalar(target, "xmp:CreateDate")
			if dateErr == nil && (err != nil || got != date || present != found) {
				t.Fatal("selected copy replaced target capture cache")
			}
			prepared, err := prepareCopySettingsWrites([]packetOutput{{Path: "fuzz.xmp", Packet: target}}, func() time.Time { return time.Unix(0, 0).UTC() })
			if err == nil {
				eligible, _ := nativeMetadataDateEligible(target, date)
				if eligible {
					value, present, err := captureTimestampScalar(prepared[0].Packet, "xmp:MetadataDate")
					if err != nil || !present || value != "1970-01-01T00:00:00+0000" {
						t.Fatal("incorrect prepared clock", value, err)
					}
				}
			}
			if current, _ := target.Serialize(); !bytes.Equal(current, after) {
				t.Fatal("preparing output changed planned packet")
			}
		}
		if current, _ := source.Serialize(); !bytes.Equal(current, sourceBefore) {
			t.Fatal("copy changed reference")
		}
	})
}
