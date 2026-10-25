package lrtl

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestReadLRTDeflickerPreferences(t *testing.T) {
	path := "testdata/lrt-deflicker-preferences-7.5.5.json"
	preferences, err := readLRTDeflickerPreferences(path)
	if err != nil {
		t.Fatal(err)
	}
	want := previewHistogramOptions{Enabled: true, IgnoreBlacks: 10, IgnoreWhites: 15, HardCut: true, FromLRTPreferences: true}
	if got := preferences.histogramOptions(); got != want {
		t.Fatalf("loaded histogram preferences %#v, want %#v", got, want)
	}
	if preferences.Smoothing == nil || *preferences.Smoothing != .75 || preferences.Accuracy == nil || *preferences.Accuracy != 1 ||
		preferences.Constant == nil || !*preferences.Constant || preferences.Multipass == nil || !*preferences.Multipass ||
		preferences.Passes == nil || *preferences.Passes != 3 {
		t.Fatalf("loaded global Deflicker defaults %#v", preferences)
	}

	for _, data := range []string{
		"{",
		`{"defaultDeflickerIgnoreBlacksWhites":true}`,
		`{"defaultDeflickerIgnoreBlacksWhites":true,"defaultDeflickerIgnoreBlacks":-1,"defaultDeflickerIgnoreWhites":0,"deflickerIgnoreBlacksWhitesHardCut":false}`,
		`{"defaultDeflickerIgnoreBlacksWhites":true,"defaultDeflickerIgnoreBlacks":0,"defaultDeflickerIgnoreWhites":101,"deflickerIgnoreBlacksWhitesHardCut":false}`,
	} {
		invalid := filepath.Join(t.TempDir(), "LRTimelapse.config")
		if err := os.WriteFile(invalid, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := readLRTDeflickerPreferences(invalid); err == nil {
			t.Fatalf("accepted invalid global preferences: %s", data)
		}
	}
	base, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		field string
		value any
	}{
		{"defaultDeflickerSmoothingFactor", 2.1},
		{"defaultDeflickerAccuracy", 2},
		{"defaultDeflickerPasses", 0},
	} {
		var values map[string]any
		if err := json.Unmarshal(base, &values); err != nil {
			t.Fatal(err)
		}
		values[tc.field] = tc.value
		data, err := json.Marshal(values)
		if err != nil {
			t.Fatal(err)
		}
		invalid := filepath.Join(t.TempDir(), "LRTimelapse.config")
		if err := os.WriteFile(invalid, data, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := readLRTDeflickerPreferences(invalid); err == nil {
			t.Fatalf("accepted invalid global default %s=%v", tc.field, tc.value)
		}
	}
}

func TestLRTDeflickerMissingSequenceControlsUseGlobalDefaults(t *testing.T) {
	fallbackDir, explicitDir := filepath.Join(t.TempDir(), "fallback"), filepath.Join(t.TempDir(), "explicit")
	frames, _, wantOrder := dateReferenceFixture(t, fallbackDir, t.TempDir())
	settingsPath := filepath.Join(fallbackDir, ".lrt", "lrtsequence.json")
	var saved map[string]any
	if err := json.Unmarshal(nativeSavedSettings(t)[0], &saved); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{
		"deflickerSmoothingFactor", "deflickerAccuracy", "deflickerConstant",
		"deflickerMultipass", "deflickerPasses", "filenameSort", "deflickerIgnoreBlacksWhites",
	} {
		delete(saved, field)
	}
	data, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	copyBatchDeflickerTree(t, fallbackDir, explicitDir)
	var explicit map[string]any
	if err := json.Unmarshal(data, &explicit); err != nil {
		t.Fatal(err)
	}
	explicit["deflickerSmoothingFactor"] = .75
	explicit["deflickerAccuracy"] = 1
	explicit["deflickerConstant"] = true
	explicit["deflickerMultipass"] = true
	explicit["deflickerPasses"] = 3
	explicit["filenameSort"] = false
	explicit["deflickerIgnoreBlacksWhites"] = 0
	data, err = json.Marshal(explicit)
	if err != nil {
		t.Fatal(err)
	}
	writeNativeSettings(t, explicitDir, data)

	preferences, err := readLRTDeflickerPreferences("testdata/lrt-deflicker-preferences-7.5.5.json")
	if err != nil {
		t.Fatal(err)
	}
	histogram := preferences.histogramOptions()
	controls, passes, constant, order, err := loadLRTDeflickerSettings(frames, &histogram, &preferences)
	if err != nil {
		t.Fatal(err)
	}
	if controls != (nativeDeflickerOptions{Enabled: true, Smoothing: .75, Accuracy: 1}) || passes != 3 || !constant || !reflect.DeepEqual(order, wantOrder) {
		t.Fatalf("resolved controls/order: %#v passes=%d constant=%t order=%v, want preference values and date order %v", controls, passes, constant, order, wantOrder)
	}

	preferencesPath := "testdata/lrt-deflicker-preferences-7.5.5.json"
	for _, dir := range []string{fallbackDir, explicitDir} {
		var out, stderr bytes.Buffer
		args := []string{"deflicker", dir, "--lrt-deflicker-settings", "--lrt-preferences", preferencesPath, "--write", "--no-backup"}
		if Main(args, &out, &stderr) != 0 {
			t.Fatalf("Deflicker %s: %s", dir, &stderr)
		}
	}
	for _, frame := range mustDiscoverSequence(t, fallbackDir) {
		got, err := os.ReadFile(frame.OutputSidecar())
		if err != nil {
			t.Fatal(err)
		}
		want, err := os.ReadFile(filepath.Join(explicitDir, filepath.Base(frame.OutputSidecar())))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("missing-field LRT fallback differs from explicit controls: %s", frame.Image)
		}
	}
}

func TestLRTDeflickerPreferencesAndSequenceOverridesInBatch(t *testing.T) {
	actual, manual := t.TempDir(), t.TempDir()
	prefs, err := os.ReadFile("testdata/lrt-deflicker-preferences-7.5.5.json")
	if err != nil {
		t.Fatal(err)
	}
	preferencesPath := filepath.Join(t.TempDir(), "LRTimelapse.config")
	if err := os.WriteFile(preferencesPath, prefs, 0600); err != nil {
		t.Fatal(err)
	}
	for _, sequence := range []struct {
		name string
		mode int
	}{
		{"global-default", 0},
		{"sequence-disabled", 2},
	} {
		dir := filepath.Join(actual, sequence.name)
		frames := nativeCLISequence(t, dir)
		writeNativePreferenceRamp(t, frames)
		writeNativeSettingsWithHistogramMode(t, dir, sequence.mode)
	}
	copyBatchDeflickerTree(t, actual, manual)
	// Each source is deliberately nonuniform and spans the excluded bins.
	// The global-default mode must therefore use the imported preference.
	plain := mustHistogramMeasurements(t, filepath.Join(actual, "global-default"), previewHistogramOptions{})
	filtered := mustHistogramMeasurements(t, filepath.Join(actual, "global-default"), previewHistogramOptions{Enabled: true, IgnoreBlacks: 10, IgnoreWhites: 15, HardCut: true})
	if reflect.DeepEqual(plain, filtered) {
		t.Fatal("histogram fixture does not distinguish the LRT preference")
	}
	var out, stderr bytes.Buffer
	if Main([]string{"batch-deflicker", actual, "--workers", "2", "--lrt-deflicker-settings", "--lrt-preferences", preferencesPath, "--write", "--no-backup"}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	if Main([]string{"deflicker", filepath.Join(manual, "global-default"), "--lrt-deflicker-settings", "--ignore-blacks", "10", "--ignore-whites", "15", "--hard-cut", "--write", "--no-backup"}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	if Main([]string{"deflicker", filepath.Join(manual, "sequence-disabled"), "--lrt-deflicker-settings", "--write", "--no-backup"}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	for _, name := range []string{"global-default", "sequence-disabled"} {
		for _, frame := range mustDiscoverSequence(t, filepath.Join(actual, name)) {
			got, err := os.ReadFile(frame.OutputSidecar())
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(filepath.Join(manual, name, filepath.Base(frame.OutputSidecar())))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("batch LRT preference result differs from explicit/sequence mode: %s", frame.Image)
			}
		}
	}
}

func TestLRTDeflickerPreferencesCLIValidation(t *testing.T) {
	root := t.TempDir()
	for _, args := range [][]string{
		{"deflicker", root, "--lrt-preferences", "missing.json"},
		{"deflicker", root, "--calculation", "lrt-7.5.5", "--lrt-preferences", "missing.json"},
	} {
		var out, stderr bytes.Buffer
		code := Main(args, &out, &stderr)
		if code == 0 {
			t.Fatalf("accepted invalid preference invocation: %v", args)
		}
	}
}

func TestLRTDeflickerPreferencesPartialCLIOverrides(t *testing.T) {
	set := flag.NewFlagSet("deflicker", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	flags := registerNativeDeflickerFlags(set)
	preferencesPath := "testdata/lrt-deflicker-preferences-7.5.5.json"
	if err := set.Parse([]string{"--calculation", "lrt-7.5.5", "--lrt-preferences", preferencesPath, "--ignore-blacks", "22", "--hard-cut=false"}); err != nil {
		t.Fatal(err)
	}
	var input imageInputOptions
	if err := flags.apply(set, &input); err != nil {
		t.Fatal(err)
	}
	want := previewHistogramOptions{Enabled: true, IgnoreBlacks: 22, IgnoreWhites: 15, HardCut: false, Explicit: true, ExplicitThresholds: true, FromLRTPreferences: true}
	if input.PreviewHistogram != want {
		t.Fatalf("resolved partial overrides %#v, want %#v", input.PreviewHistogram, want)
	}
}

func TestLRTDeflickerSequenceHistogramModePrecedence(t *testing.T) {
	root := t.TempDir()
	frames := nativeCLISequence(t, root)
	var settings map[string]any
	if err := json.Unmarshal(nativeSavedSettings(t)[0], &settings); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []int{0, 1, 2} {
		settings["deflickerIgnoreBlacksWhites"] = mode
		data, err := json.Marshal(settings)
		if err != nil {
			t.Fatal(err)
		}
		writeNativeSettings(t, root, data)
		for _, globalEnabled := range []bool{false, true} {
			histogram := previewHistogramOptions{Enabled: globalEnabled, IgnoreBlacks: 10, IgnoreWhites: 15, HardCut: true, FromLRTPreferences: true}
			_, _, _, _, err := loadLRTDeflickerSettings(frames, &histogram, nil)
			if err != nil {
				t.Fatalf("mode %d with global preference enabled=%t: %v", mode, globalEnabled, err)
			}
			wantEnabled := globalEnabled
			if mode == 1 {
				wantEnabled = true
			} else if mode == 2 {
				wantEnabled = false
			}
			if histogram.Enabled != wantEnabled || histogram.IgnoreBlacks != 10 || histogram.IgnoreWhites != 15 || !histogram.HardCut {
				t.Fatalf("mode %d resolved histogram %#v", mode, histogram)
			}
		}
	}
}

func writeNativeSettingsWithHistogramMode(t *testing.T, dir string, mode int) {
	t.Helper()
	var values map[string]any
	if err := json.Unmarshal(nativeSavedSettings(t)[0], &values); err != nil {
		t.Fatal(err)
	}
	values["deflickerIgnoreBlacksWhites"] = mode
	data, err := json.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	writeNativeSettings(t, dir, data)
}

func writeNativePreferenceRamp(t *testing.T, frames []Frame) {
	t.Helper()
	for index, frame := range frames {
		img := image.NewNRGBA(image.Rect(0, 0, 32, 32))
		for y := 0; y < 32; y++ {
			for x := 0; x < 32; x++ {
				value := uint8((x*17 + y*31 + index*29) % 256)
				img.SetNRGBA(x, y, color.NRGBA{R: value, G: value*3 + 17, B: value*7 + 13, A: 255})
			}
		}
		file, err := os.Create(frame.Image)
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(file, img); err != nil {
			file.Close()
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func mustHistogramMeasurements(t *testing.T, dir string, histogram previewHistogramOptions) []float64 {
	t.Helper()
	frames := mustDiscoverSequence(t, dir)
	values, err := analyzePreviewEVSequence(frames, imageInputOptions{PreviewHistogram: histogram})
	if err != nil {
		t.Fatal(fmt.Errorf("measure %s: %w", dir, err))
	}
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			t.Fatalf("non-finite Preview EV from %s", dir)
		}
	}
	return values
}
