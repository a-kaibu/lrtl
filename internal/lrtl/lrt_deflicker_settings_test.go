package lrtl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func nativeSavedSettings(t *testing.T) []json.RawMessage {
	t.Helper()
	data, err := os.ReadFile("testdata/lrt-deflicker-settings-7.5.5.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []json.RawMessage
	if err := json.Unmarshal(data, &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 6 {
		t.Fatal("expected six native settings observations")
	}
	return rows
}

func writeNativeSettings(t *testing.T, dir string, data []byte) {
	t.Helper()
	path := filepath.Join(dir, ".lrt", "lrtsequence.json")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestLRTDeflickerSettingsNativeSerializationAndCLI(t *testing.T) {
	for i, row := range nativeSavedSettings(t) {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			actual, explicit := t.TempDir(), t.TempDir()
			frames := nativeCLISequence(t, actual)
			copyBatchDeflickerTree(t, actual, explicit)
			writeNativeSettings(t, actual, row)
			options, passes, constant, order, err := loadLRTDeflickerSettings(frames, previewHistogramOptions{})
			if err != nil {
				t.Fatal(err)
			}
			want := nativeDeflickerOptions{true, []float64{.25, .5, 1.1}[i/2], i/2 - 1}
			wantPasses := 1
			if i%2 == 1 {
				wantPasses = 3
			}
			if options != want || passes != wantPasses || constant != (i >= 4) || !reflect.DeepEqual(order, []int{0, 1, 2, 3, 4}) {
				t.Fatalf("loaded %v %d %v %v", options, passes, constant, order)
			}
			before := batchFileSnapshot(t, actual)
			var out, stderr bytes.Buffer
			args := []string{"deflicker", actual, "--lrt-deflicker-settings"}
			if Main(args, &out, &stderr) != 0 {
				t.Fatal(&stderr)
			}
			if !reflect.DeepEqual(before, batchFileSnapshot(t, actual)) {
				t.Fatal("settings dry run wrote files")
			}
			if !strings.Contains(out.String(), fmt.Sprintf("bandwidth: %g; accuracy: %d", want.Smoothing, want.Accuracy)) {
				t.Fatal("CLI reported default controls instead of saved controls")
			}
			if constant && !strings.Contains(out.String(), "native mean Preview EV") {
				t.Fatal("saved constant was not reported")
			}
			if Main(append(args, "--write"), &out, &stderr) != 0 {
				t.Fatal(&stderr)
			}
			manual := []string{"deflicker", explicit, "--calculation", "lrt-7.5.5", "--lrt-smoothing", fmt.Sprint(want.Smoothing), "--lrt-accuracy", fmt.Sprint(want.Accuracy), "--passes", fmt.Sprint(wantPasses), "--write"}
			if constant {
				manual = append(manual, "--constant")
			}
			if Main(manual, &out, &stderr) != 0 {
				t.Fatal(&stderr)
			}
			for _, frame := range frames {
				wantData, err := os.ReadFile(filepath.Join(explicit, filepath.Base(frame.Sidecar)))
				if err != nil {
					t.Fatal(err)
				}
				assertPreviewEVFileBytes(t, frame.Sidecar, wantData)
				assertPreviewEVFileBytes(t, frame.Image, before[frame.Image])
			}
			assertPreviewEVFileBytes(t, filepath.Join(actual, ".lrt", "lrtsequence.json"), row)
		})
	}
}

func TestLRTDeflickerSettingsBatchUsesEachSequence(t *testing.T) {
	rows := nativeSavedSettings(t)
	root, explicit := t.TempDir(), t.TempDir()
	for i, name := range []string{"a", "b"} {
		dir := filepath.Join(root, name)
		nativeCLISequence(t, dir)
		writeNativeSettings(t, dir, rows[i*5])
	}
	copyBatchDeflickerTree(t, root, explicit)
	var out, stderr bytes.Buffer
	if Main([]string{"batch-deflicker", root, "--lrt-deflicker-settings", "--workers", "2", "--write"}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	for _, controls := range []string{"bandwidth: 0.25; accuracy: -1; constant: false", "bandwidth: 1.1; accuracy: 1; constant: true"} {
		if !strings.Contains(out.String(), controls) {
			t.Fatalf("batch did not report each sequence's controls: %s", &out)
		}
	}
	for _, name := range []string{"a", "b"} {
		dir := filepath.Join(explicit, name)
		if Main([]string{"deflicker", dir, "--lrt-deflicker-settings", "--write"}, &out, &stderr) != 0 {
			t.Fatal(&stderr)
		}
		frames, err := DiscoverSequence(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, frame := range frames {
			data, err := os.ReadFile(frame.Sidecar)
			if err != nil {
				t.Fatal(err)
			}
			assertPreviewEVFileBytes(t, filepath.Join(root, name, filepath.Base(frame.Sidecar)), data)
		}
	}
}

func TestLRTDeflickerSettingsDateOrderAndROI(t *testing.T) {
	frames, reference, order := dateReferenceFixture(t, t.TempDir(), t.TempDir())
	root := filepath.Dir(frames[0].Image)
	path := filepath.Join(root, ".lrt", "lrtsequence.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var settings map[string]json.RawMessage
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}
	var controls map[string]json.RawMessage
	if err := json.Unmarshal(nativeSavedSettings(t)[1], &controls); err != nil {
		t.Fatal(err)
	}
	for key, value := range controls {
		if strings.HasPrefix(key, "deflicker") {
			settings[key] = value
		}
	}
	data, err = json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	writeNativeSettings(t, root, data)
	for _, roi := range []bool{false, true} {
		t.Run(fmt.Sprint(roi), func(t *testing.T) {
			input := imageInputOptions{LRTDeflickerSettings: true, LRTReference: roi}
			got, err := planDeflickerSequence(frames, image.Rectangle{}, input, 5, 1, false, "gaussian")
			if err != nil {
				t.Fatal(err)
			}
			ordered := make([]Frame, len(frames))
			for i, source := range order {
				ordered[i] = frames[source]
			}
			if roi {
				ordered = reference
			}
			want, err := planDeflickerSequence(ordered, image.Rectangle{}, imageInputOptions{NativeDeflicker: nativeDeflickerOptions{true, .25, -1}}, 5, 3, false, "gaussian")
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got.Measurements, want.Measurements) {
				t.Fatalf("saved sequence order/ROI mismatch: %v / %v", got.Measurements, want.Measurements)
			}
			if len(got.Changes) == 0 {
				t.Fatal("no changes")
			}
			for _, change := range got.Changes {
				if change.Frame.Image != frames[change.Frame.Index].Image {
					t.Fatal("saved ordering lost original sidecar identity")
				}
			}
		})
	}
}

func TestLRTDeflickerSettingsValidationPreservesFiles(t *testing.T) {
	base := nativeSavedSettings(t)[1]
	cases := []struct {
		key   string
		value any
	}{
		{"deflickerSmoothingFactor", nil}, {"deflickerSmoothingFactor", -0.1}, {"deflickerSmoothingFactor", 2.1},
		{"deflickerSmoothingFactor", "NaN"}, {"deflickerAccuracy", nil}, {"deflickerAccuracy", 2},
		{"deflickerAccuracy", .5}, {"deflickerConstant", nil}, {"deflickerConstant", "false"},
		{"deflickerMultipass", nil}, {"deflickerPasses", nil}, {"deflickerPasses", 0}, {"deflickerPasses", 9},
		{"filenameSort", nil}, {"imageCount", 6}, {"deflickerIgnoreBlacksWhites", 1}, {"deflickerIgnoreBlacksWhites", 3},
	}
	for i, tc := range cases {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, "a")
			nativeCLISequence(t, dir)
			var settings map[string]any
			if err := json.Unmarshal(base, &settings); err != nil {
				t.Fatal(err)
			}
			settings[tc.key] = tc.value
			data, err := json.Marshal(settings)
			if err != nil {
				t.Fatal(err)
			}
			writeNativeSettings(t, dir, data)
			before := batchFileSnapshot(t, root)
			for _, invocation := range [][]string{{"deflicker", dir}, {"batch-deflicker", root}} {
				var out, stderr bytes.Buffer
				if Main(append(invocation, "--lrt-deflicker-settings", "--write"), &out, &stderr) == 0 {
					t.Fatalf("accepted %s=%v", tc.key, tc.value)
				}
				if !reflect.DeepEqual(before, batchFileSnapshot(t, root)) {
					t.Fatal("invalid settings changed images/XMP/settings/backups")
				}
			}
		})
	}
	for _, content := range [][]byte{nil, []byte("null"), []byte("<broken>"), []byte("{}"), []byte("[]")} {
		root := t.TempDir()
		nativeCLISequence(t, root)
		if content != nil {
			writeNativeSettings(t, root, content)
		}
		before := batchFileSnapshot(t, root)
		var out, stderr bytes.Buffer
		if Main([]string{"deflicker", root, "--lrt-deflicker-settings", "--write"}, &out, &stderr) == 0 {
			t.Fatal("missing/malformed settings accepted")
		}
		if !reflect.DeepEqual(before, batchFileSnapshot(t, root)) {
			t.Fatal("malformed settings changed files")
		}
	}
}

func TestLRTDeflickerSettingsConflictingFlags(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "a")
	nativeCLISequence(t, dir)
	writeNativeSettings(t, dir, nativeSavedSettings(t)[1])
	before := batchFileSnapshot(t, root)
	for _, invocation := range [][]string{{"deflicker", dir}, {"batch-deflicker", root}} {
		for _, flags := range [][]string{{"--calculation", "lrtl"}, {"--smooth", "5"}, {"--smoother", "gaussian"}, {"--lrt-smoothing", ".5"}, {"--lrt-accuracy", "0"}, {"--passes", "1"}, {"--constant=false"}, {"--reset"}} {
			var out, stderr bytes.Buffer
			args := append(append(append([]string{}, invocation...), "--lrt-deflicker-settings", "--write"), flags...)
			if Main(args, &out, &stderr) == 0 {
				t.Fatalf("accepted conflicting flags %v", flags)
			}
			if !reflect.DeepEqual(before, batchFileSnapshot(t, root)) {
				t.Fatal("conflicting flags changed files")
			}
		}
	}
}

func TestLRTDeflickerSettingsBatchPreflight(t *testing.T) {
	root := t.TempDir()
	rows := nativeSavedSettings(t)
	for i, name := range []string{"a", "z"} {
		dir := filepath.Join(root, name)
		nativeCLISequence(t, dir)
		data := rows[i]
		if i == 1 {
			data = []byte(`{"deflickerSmoothingFactor":0.5}`)
		}
		writeNativeSettings(t, dir, data)
	}
	before := batchFileSnapshot(t, root)
	var out, stderr bytes.Buffer
	if Main([]string{"batch-deflicker", root, "--lrt-deflicker-settings", "--workers", "2", "--write"}, &out, &stderr) == 0 {
		t.Fatal("invalid later sequence accepted")
	}
	if !strings.Contains(stderr.String(), "no sidecars written") {
		t.Fatalf("unexpected error: %s", &stderr)
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, root)) {
		t.Fatal("valid earlier sequence was saved before other settings failed")
	}
}

func TestLRTDeflickerSettingsHistogramAndDisabledMultipass(t *testing.T) {
	root := t.TempDir()
	frames := nativeCLISequence(t, root)
	var settings map[string]any
	if err := json.Unmarshal(nativeSavedSettings(t)[0], &settings); err != nil {
		t.Fatal(err)
	}
	settings["deflickerPasses"] = 99 // ignored while multipass is disabled
	for _, mode := range []int{0, 1, 2} {
		settings["deflickerIgnoreBlacksWhites"] = mode
		data, err := json.Marshal(settings)
		if err != nil {
			t.Fatal(err)
		}
		writeNativeSettings(t, root, data)
		for _, enabled := range []bool{false, true} {
			opts, passes, _, _, err := loadLRTDeflickerSettings(frames, previewHistogramOptions{Enabled: enabled, IgnoreBlacks: 10, IgnoreWhites: 15, HardCut: true})
			wantError := mode == 1 && !enabled || mode == 2 && enabled
			if (err != nil) != wantError {
				t.Fatalf("mode %d explicit %v: %v", mode, enabled, err)
			}
			if !wantError && (!opts.Enabled || passes != 1) {
				t.Fatal("disabled multipass did not reduce to one pass")
			}
		}
	}
	// The full sequence is required so saved sort/ROI indices cannot drift.
	if _, _, _, _, err := loadLRTDeflickerSettings(frames[:2], previewHistogramOptions{}); err == nil {
		t.Fatal("partial sequence accepted")
	}
	reversed := append([]Frame(nil), frames...)
	reversed[0], reversed[1] = reversed[1], reversed[0]
	if _, _, _, _, err := loadLRTDeflickerSettings(reversed, previewHistogramOptions{}); err == nil {
		t.Fatal("preordered sequence accepted")
	}
}
