package lrtl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeHolyGrailSequenceSettings(t *testing.T, dir string, filenameSort bool, reduce, stretch, rotate float64) []byte {
	t.Helper()
	data, err := json.Marshal(map[string]any{
		"filenameSort":             filenameSort,
		"holyGrailReduceDeflicker": reduce,
		"holyGrailStretch":         stretch,
		"holyGrailRotate":          rotate,
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, ".lrt", "lrtsequence.json")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return data
}

func reverseHolyGrailDates(t *testing.T, frames []Frame) {
	t.Helper()
	for index, frame := range frames {
		packet, err := PacketForFrame(frame)
		if err != nil {
			t.Fatal(err)
		}
		hour := len(frames) - index
		if err := packet.SetScalar("xmp:CreateDate", fmt.Sprintf("2026-01-01T%02d:00:00Z", hour)); err != nil {
			t.Fatal(err)
		}
		if _, err := WritePacket(frame.OutputSidecar(), packet, false); err != nil {
			t.Fatal(err)
		}
	}
}

func TestHolyGrailUsesLRTSequenceOrderAndCurveSettings(t *testing.T) {
	for _, tc := range []struct {
		name         string
		filenameSort bool
		reduce       float64
		stretch      float64
		rotate       float64
	}{
		{"filename", true, 0, .18, .32},
		{"date", false, .35, -.4, .18},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			dir := batchHolyGrailFixture(t, root, "sequence", true)
			frames, err := DiscoverSequence(dir)
			if err != nil {
				t.Fatal(err)
			}
			reverseHolyGrailDates(t, frames)
			settingsBytes := writeHolyGrailSequenceSettings(t, dir, tc.filenameSort, tc.reduce, tc.stretch, tc.rotate)

			ordered, options, err := applyLRTSequenceHolyGrailSettings(frames)
			if err != nil {
				t.Fatal(err)
			}
			wantMode := "lrt-name"
			if !tc.filenameSort {
				wantMode = "lrt-date"
			}
			wantOrder, err := orderSequenceFrames(frames, wantMode)
			if err != nil || !reflect.DeepEqual(ordered, wantOrder) {
				t.Fatalf("settings frame order mismatch: %v", err)
			}
			if !options.LRTInput || options.Stretch != tc.stretch || options.Rotate != tc.rotate || options.ReduceDeflicker != tc.reduce {
				t.Fatalf("loaded curve settings: %#v", options)
			}
			manualPlan, err := buildHolyGrailPlanWithCurveOptions(ordered, false, false, false, true, options)
			if err != nil {
				t.Fatal(err)
			}
			wantExposure := make(map[string]float64, len(manualPlan.Changes))
			for _, change := range manualPlan.Changes {
				wantExposure[change.Frame.Image] = change.ExposureAfter
			}

			beforeSettings, err := os.ReadFile(filepath.Join(dir, ".lrt", "lrtsequence.json"))
			if err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer
			args := []string{"holy-grail", dir, "--lrt-holy-grail-settings", "--write", "--no-backup"}
			if Main(args, &stdout, &stderr) != 0 {
				t.Fatal(&stderr)
			}
			for _, frame := range frames {
				packet, err := PacketForFrame(frame)
				if err != nil {
					t.Fatal(err)
				}
				text, found, err := packet.GetScalar("crs:Exposure2012")
				got, valid := scalarNumber(text)
				want, updated := wantExposure[frame.Image]
				if !updated {
					want = .25
				}
				if err != nil || !found || !valid || math.Abs(got-want) > 2e-8 {
					t.Fatalf("%s exposure=%q, want %.15g, found=%v err=%v", filepath.Base(frame.Image), text, want, found, err)
				}
			}
			afterSettings, err := os.ReadFile(filepath.Join(dir, ".lrt", "lrtsequence.json"))
			if err != nil || !bytes.Equal(beforeSettings, settingsBytes) || !bytes.Equal(afterSettings, settingsBytes) {
				t.Fatal("changed LRT sequence settings", err)
			}
		})
	}
}

func TestBatchHolyGrailUsesEachSavedSequenceSettings(t *testing.T) {
	root, reference := t.TempDir(), t.TempDir()
	configs := []struct {
		name         string
		filenameSort bool
		reduce       float64
		stretch      float64
		rotate       float64
	}{
		{"a", true, 0, .18, .32},
		{"b", false, .35, -.4, .18},
	}
	for _, config := range configs {
		dir := batchHolyGrailFixture(t, root, config.name, true)
		frames, err := DiscoverSequence(dir)
		if err != nil {
			t.Fatal(err)
		}
		reverseHolyGrailDates(t, frames)
		writeHolyGrailSequenceSettings(t, dir, config.filenameSort, config.reduce, config.stretch, config.rotate)
	}
	copyBatchDeflickerTree(t, root, reference)
	badSettings := filepath.Join(root, "b", ".lrt", "lrtsequence.json")
	if err := os.WriteFile(badSettings, []byte(`{"filenameSort":false}`), 0600); err != nil {
		t.Fatal(err)
	}
	before := batchFileSnapshot(t, root)
	var stdout, stderr bytes.Buffer
	args := []string{"batch-holy-grail", root, "--workers", "2", "--lrt-holy-grail-settings", "--write", "--no-backup"}
	if Main(args, &stdout, &stderr) == 0 {
		t.Fatal("accepted sequence with incomplete saved HG settings")
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, root)) {
		t.Fatal("batch wrote XMP before every sequence's settings were validated")
	}
	for _, config := range configs {
		settings := writeHolyGrailSequenceSettings(t, filepath.Join(root, config.name), config.filenameSort, config.reduce, config.stretch, config.rotate)
		if err := os.WriteFile(filepath.Join(reference, config.name, ".lrt", "lrtsequence.json"), settings, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if Main(args, &stdout, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	for _, config := range configs {
		dir := filepath.Join(reference, config.name)
		if Main([]string{"holy-grail", dir, "--lrt-holy-grail-settings", "--write", "--no-backup"}, &stdout, &stderr) != 0 {
			t.Fatal(&stderr)
		}
		for _, frame := range mustDiscoverSequence(t, filepath.Join(root, config.name)) {
			batchPacket, err := PacketForFrame(frame)
			if err != nil {
				t.Fatal(err)
			}
			manualPath := filepath.Join(dir, filepath.Base(frame.Image)) + ".xmp"
			manualBytes, err := os.ReadFile(manualPath)
			if err != nil {
				t.Fatal(err)
			}
			manualPacket, err := parseXMP(manualBytes)
			if err != nil {
				t.Fatal(err)
			}
			batchExposure, _, _ := batchPacket.GetScalar("crs:Exposure2012")
			manualExposure, _, _ := manualPacket.GetScalar("crs:Exposure2012")
			if batchExposure != manualExposure {
				t.Fatalf("%s batch exposure %q differs from individual %q", frame.Image, batchExposure, manualExposure)
			}
		}
	}
}

func TestApplyLRTSequenceHolyGrailSettingsRejectsInvalidSettings(t *testing.T) {
	dir := batchHolyGrailFixture(t, t.TempDir(), "sequence", true)
	frames := mustDiscoverSequence(t, dir)
	settingsPath := filepath.Join(dir, ".lrt", "lrtsequence.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0700); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		data string
	}{
		{"invalid-json", `{`},
		{"missing-values", `{"filenameSort":true}`},
		{"missing-sort", `{"holyGrailReduceDeflicker":0,"holyGrailStretch":0,"holyGrailRotate":0}`},
		{"reduce-below-range", `{"filenameSort":true,"holyGrailReduceDeflicker":-0.01,"holyGrailStretch":0,"holyGrailRotate":0}`},
		{"reduce-above-range", `{"filenameSort":true,"holyGrailReduceDeflicker":1.01,"holyGrailStretch":0,"holyGrailRotate":0}`},
		{"stretch-below-range", `{"filenameSort":true,"holyGrailReduceDeflicker":0,"holyGrailStretch":-1.5001,"holyGrailRotate":0}`},
		{"stretch-above-range", `{"filenameSort":true,"holyGrailReduceDeflicker":0,"holyGrailStretch":1.5001,"holyGrailRotate":0}`},
		{"rotate-below-range", `{"filenameSort":true,"holyGrailReduceDeflicker":0,"holyGrailStretch":0,"holyGrailRotate":-4.01}`},
		{"rotate-above-range", `{"filenameSort":true,"holyGrailReduceDeflicker":0,"holyGrailStretch":0,"holyGrailRotate":4.01}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(settingsPath, []byte(tc.data), 0600); err != nil {
				t.Fatal(err)
			}
			if _, _, err := applyLRTSequenceHolyGrailSettings(frames); err == nil {
				t.Fatal("accepted invalid LRT Holy Grail sequence settings")
			}
		})
	}
}

func TestApplyLRTSequenceHolyGrailSettingsAcceptsRangeBoundaries(t *testing.T) {
	dir := batchHolyGrailFixture(t, t.TempDir(), "sequence", true)
	frames := mustDiscoverSequence(t, dir)
	for _, tc := range []struct {
		name                    string
		reduce, stretch, rotate float64
	}{
		{"lower", 0, -1.5, -4},
		{"upper", 1, 1.5, 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writeHolyGrailSequenceSettings(t, dir, true, tc.reduce, tc.stretch, tc.rotate)
			_, options, err := applyLRTSequenceHolyGrailSettings(frames)
			if err != nil {
				t.Fatalf("rejected inclusive range boundary: %v", err)
			}
			if options.ReduceDeflicker != tc.reduce || options.Stretch != tc.stretch || options.Rotate != tc.rotate {
				t.Fatalf("loaded settings %#v, want reduce=%v stretch=%v rotate=%v", options, tc.reduce, tc.stretch, tc.rotate)
			}
		})
	}
}

func TestHolyGrailSettingsFlagConflictsFailBeforeSequenceDiscovery(t *testing.T) {
	root := t.TempDir()
	cases := []struct {
		name string
		args []string
	}{
		{"stretch", []string{"holy-grail", root, "--lrt-holy-grail-settings", "--stretch", "0.1"}},
		{"rotate", []string{"holy-grail", root, "--lrt-holy-grail-settings", "--rotate", "0.1"}},
		{"reduce-deflicker", []string{"holy-grail", root, "--lrt-holy-grail-settings", "--lrt-reduce-deflicker", "0.1"}},
		{"auto-shape", []string{"holy-grail", root, "--lrt-holy-grail-settings", "--auto-shape"}},
		{"reset", []string{"holy-grail", root, "--lrt-holy-grail-settings", "--reset"}},
		{"batch-stretch", []string{"batch-holy-grail", root, "--lrt-holy-grail-settings", "--stretch", "0.1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := Main(tc.args, &stdout, &stderr); code == 0 {
				t.Fatal("accepted conflicting LRT Holy Grail settings flags")
			}
			if !strings.Contains(stderr.String(), "--lrt-holy-grail-settings cannot be combined") {
				t.Fatalf("unexpected error: %s", stderr.String())
			}
		})
	}
}

func mustDiscoverSequence(t *testing.T, dir string) []Frame {
	t.Helper()
	frames, err := DiscoverSequence(dir)
	if err != nil {
		t.Fatal(err)
	}
	return frames
}
