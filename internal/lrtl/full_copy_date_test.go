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

type fullCopyDateReference struct {
	Completed bool `json:"paste_completed"`
	Stop      string
	Cases     []struct {
		Target, Source, Cache string
		Active                string `json:"loaded_has_settings"`
	}
}

func fullCopyDateObservations(t *testing.T) fullCopyDateReference {
	t.Helper()
	var r fullCopyDateReference
	if err := json.Unmarshal(denoiseRead(t, "testdata/lrt-full-paste-date-7.5.5.json"), &r); err != nil {
		t.Fatal(err)
	}
	if r.Completed || !strings.Contains(r.Stop, "after date cache assignment") || len(r.Cases) != 2080 {
		t.Fatal("invalid full-paste date observations")
	}
	return r
}

func TestFullCopyDateNativeReference(t *testing.T) {
	for i, row := range fullCopyDateObservations(t).Cases {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			source := pasteTestPacket(t, row.Source)
			sourceBefore, _ := source.Serialize()
			merged := pasteTestPacket(t, row.Target)
			if _, err := pasteXMPMetadata(merged, source); err != nil {
				t.Fatal(err)
			}
			date, err := fullCopyCaptureDate(merged)
			if err != nil || date != row.Cache {
				t.Fatal("reload date differs", date, row.Cache, err)
			}
			if err := prepareFullCopyHasSettings(merged); err != nil {
				t.Fatal(err)
			}
			active, _, err := captureTimestampScalar(merged, "crs:HasSettings")
			if err != nil || active != row.Active {
				t.Fatal("reload HasSettings differs", active, row.Active, err)
			}
			packet := pasteTestPacket(t, row.Target)
			if _, err := applyCopySettingsProfile(packet, source, nil, nativeSaveStateProfile); err != nil {
				t.Fatal(err)
			}
			eligible := strings.EqualFold(row.Active, "true")
			for _, property := range []string{"exif:DateTimeOriginal", "xmp:CreateDate", "xmp:ModifyDate", "photoshop:DateCreated"} {
				got, present, err := captureTimestampScalar(packet, property)
				if err != nil {
					t.Fatal(err)
				}
				if eligible {
					if !present || got != row.Cache {
						t.Fatal("save date differs", property, got, row.Cache)
					}
				} else {
					want, found, err := captureTimestampScalar(merged, property)
					if err != nil || present != found || got != want {
						t.Fatal("inactive save changed date", property, got, want, err)
					}
				}
			}
			before, _ := packet.Serialize()
			calls := 0
			prepared, err := prepareCopySettingsWrites([]packetOutput{{Path: "full.xmp", Packet: packet}}, func() time.Time { calls++; return time.Unix(0, 0).UTC() })
			if err != nil {
				t.Fatal(err)
			}
			if eligible {
				clock, found, err := captureTimestampScalar(prepared[0].Packet, "xmp:MetadataDate")
				if err != nil || !found || clock != "1970-01-01T00:00:00+0000" || calls != 1 {
					t.Fatal("incorrect full-copy clock", clock, calls, err)
				}
			} else if calls != 0 {
				t.Fatal("used clock for native inactive flag")
			}
			if current, _ := packet.Serialize(); !bytes.Equal(before, current) {
				t.Fatal("save preparation changed plan")
			}
			if current, _ := source.Serialize(); !bytes.Equal(sourceBefore, current) {
				t.Fatal("full copy changed source")
			}
		})
	}
}

func TestFullCopyDateInstalledReplay(t *testing.T) {
	jar := os.Getenv("LRTL_NATIVE_JAR")
	if jar == "" {
		t.Skip("set LRTL_NATIVE_JAR for full-paste reload observations")
	}
	dir := t.TempDir()
	root, err := filepath.Abs(filepath.Join("..", "..", "scripts", "probes"))
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"-encoding", "UTF-8", "-d", dir}
	for _, name := range []string{"MetadataPasteOracle.java", "SelectedColumnOracle.java", "RegisteredSaveOracle.java", "FullPasteDateOracle.java"} {
		args = append(args, filepath.Join(root, name))
	}
	if out, err := exec.Command("javac", args...).CombinedOutput(); err != nil {
		t.Fatalf("compile observer: %v\n%s", err, out)
	}
	path := filepath.Join(dir, "replay.json")
	if out, err := exec.Command("java", "--enable-native-access=ALL-UNNAMED", "-XX:-OmitStackTraceInFastThrow", "-cp", jar+string(os.PathListSeparator)+dir, "lrtlprobe.FullPasteDateOracle", path).CombinedOutput(); err != nil {
		t.Fatalf("native full-paste observer: %v\n%s", err, out)
	}
	var replay fullCopyDateReference
	if err := json.Unmarshal(denoiseRead(t, path), &replay); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(replay, fullCopyDateObservations(t)) {
		t.Fatal("native full-paste date observations changed")
	}
}

func TestFullCopyDateCLIPreviewSaveRepeat(t *testing.T) {
	for _, active := range []string{"False", "True", "1", ""} {
		for _, profile := range []string{nativeSaveStateProfile, "preserve"} {
			t.Run(active+"/"+profile, func(t *testing.T) {
				dir := t.TempDir()
				frame := metadataDateFrame(t, dir, "frame.jpg", "False", "qualified")
				target := pasteTestPacket(t, string(denoiseRead(t, frame.Sidecar)))
				if err := setCaptureTimestampScalar(target, "xmp:CreateDate", "2031-02-03T04:05:06.123456+09:00"); err != nil {
					t.Fatal(err)
				}
				if _, err := WritePacket(frame.Sidecar, target, false); err != nil {
					t.Fatal(err)
				}
				source := EmptyXMPPacket()
				for key, value := range map[string]string{"crs:HasSettings": active, "crs:Exposure2012": ".75", "xmp:CreateDate": "2001-source"} {
					if err := source.SetScalar(key, value); err != nil {
						t.Fatal(err)
					}
				}
				path := filepath.Join(t.TempDir(), "reference.xmp")
				if _, err := WritePacket(path, source, false); err != nil {
					t.Fatal(err)
				}
				sourceBefore := denoiseRead(t, path)
				before := batchFileSnapshot(t, dir)
				args := []string{"copy-settings", path, dir, "--save-state", profile}
				var out, stderr bytes.Buffer
				if Main(args, &out, &stderr) != 0 {
					t.Fatal(&stderr)
				}
				if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
					t.Fatal("full-copy preview changed files")
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
				clock, _, err := captureTimestampScalar(saved, "xmp:MetadataDate")
				if err != nil {
					t.Fatal(err)
				}
				date, _, err := captureTimestampScalar(saved, "xmp:CreateDate")
				if err != nil {
					t.Fatal(err)
				}
				if profile == nativeSaveStateProfile && active != "1" {
					stamp, err := parseMetadataDateClock(clock)
					if err != nil || stamp.Before(start) || stamp.After(end) {
						t.Fatal("full copy clock outside save window", clock, err)
					}
					if date != "2031-02-03T04:05:06.12" {
						t.Fatal("full-copy cache not normalized", date)
					}
					for _, property := range []string{"exif:DateTimeOriginal", "xmp:ModifyDate", "photoshop:DateCreated"} {
						value, _, _ := captureTimestampScalar(saved, property)
						if value != date {
							t.Fatal("full-copy capture date not saved", property, value)
						}
					}
				} else if clock != "previous" || date != "2031-02-03T04:05:06.123456+09:00" {
					t.Fatal("preserved clock/date changed", clock, date)
				}
				backups, _ := filepath.Glob(frame.Sidecar + ".lrtl-*.bak")
				if len(backups) != 1 || !bytes.Equal(denoiseRead(t, backups[0]), before[frame.Sidecar]) {
					t.Fatal("missing/exact backup", backups)
				}
				after := batchFileSnapshot(t, dir)
				if Main(append(args, "--write"), &out, &stderr) != 0 {
					t.Fatal(&stderr)
				}
				if !reflect.DeepEqual(after, batchFileSnapshot(t, dir)) {
					t.Fatal("repeat full copy changed files")
				}
				if !bytes.Equal(denoiseRead(t, path), sourceBefore) {
					t.Fatal("changed reference")
				}
			})
		}
	}
}

func TestFullCopyDateCLILateClockError(t *testing.T) {
	dir := t.TempDir()
	metadataDateFrame(t, dir, "one.jpg", "False", "qualified")
	metadataDateFrame(t, dir, "two.jpg", "False", "struct")
	source := filepath.Join(t.TempDir(), "reference.xmp")
	p := EmptyXMPPacket()
	if err := p.SetScalar("crs:Exposure2012", ".75"); err != nil {
		t.Fatal(err)
	}
	if _, err := WritePacket(source, p, false); err != nil {
		t.Fatal(err)
	}
	before := batchFileSnapshot(t, dir)
	for _, write := range []bool{false, true} {
		var out, stderr bytes.Buffer
		args := []string{"copy-settings", source, dir}
		if write {
			args = append(args, "--write")
		}
		if Main(args, &out, &stderr) == 0 || !strings.Contains(stderr.String(), "MetadataDate") {
			t.Fatal("accepted late full-copy clock error", &stderr)
		}
		if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
			t.Fatal("late full-copy error changed earlier sidecars or backups")
		}
	}
}

func TestFullCopyDateUnknownAndFallbackCLI(t *testing.T) {
	for _, original := range []string{"", "2030:02:03 04:05:06"} {
		dir := t.TempDir()
		frame := metadataDateFrame(t, dir, "frame.jpg", "False", "scalar")
		packet := EmptyXMPPacket()
		if original != "" {
			if err := packet.SetScalar("exif:DateTimeOriginal", original); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := WritePacket(frame.Sidecar, packet, false); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "empty.xmp")
		if _, err := WritePacket(path, EmptyXMPPacket(), false); err != nil {
			t.Fatal(err)
		}
		var out, stderr bytes.Buffer
		if Main([]string{"copy-settings", path, dir, "--write"}, &out, &stderr) != 0 {
			t.Fatal(&stderr)
		}
		saved, err := PacketForFrame(frame)
		if err != nil {
			t.Fatal(err)
		}
		want := "n/a"
		if original != "" {
			want = "2030-02-03T04:05:06"
		}
		for _, property := range []string{"exif:DateTimeOriginal", "xmp:CreateDate", "xmp:ModifyDate", "photoshop:DateCreated"} {
			value, present, err := captureTimestampScalar(saved, property)
			if err != nil || !present || value != want {
				t.Fatal("unknown/fallback date differs", property, value, want, err)
			}
		}
		if value, present, err := captureTimestampScalar(saved, "xmp:MetadataDate"); err != nil || !present || value == "" {
			t.Fatal("missing fallback save clock", value, err)
		}
	}
}
