package lrtl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
)

func writeLongTermPreset(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "filter.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLRTLongTermPresetDefaultsAndLoading(t *testing.T) {
	var reference struct {
		DefaultKey, DateFormat, MissingDate string
		Defaults                            map[string]any
	}
	if err := json.Unmarshal(denoiseRead(t, "testdata/lrt-longterm-curves-7.5.5.json"), &reference); err != nil {
		t.Fatal(err)
	}
	if reference.DefaultKey != "default" || reference.DateFormat != "yyyy-MM-dd HH:mm:ss" || reference.MissingDate != "n/a" {
		t.Fatal("unexpected native settings identity", reference)
	}
	data, _ := json.Marshal(defaultLRTLongTermPreset())
	var defaults map[string]any
	_ = json.Unmarshal(data, &defaults)
	for name, value := range defaults {
		if !reflect.DeepEqual(reference.Defaults[name], value) {
			t.Fatalf("default %s=%v native%v", name, value, reference.Defaults[name])
		}
	}
	direct := writeLongTermPreset(t, map[string]any{"lumSmoothing": 17, "contrastConst": true, "hueHigh": 80, "ID": "preserve source file", "lumLock": true})
	before := denoiseRead(t, direct)
	loaded, err := loadLRTLongTermPreset(direct, "")
	if err != nil || loaded.Preset.LumSmoothing != 17 || !loaded.Preset.ContrastConst || loaded.Preset.HueHigh != 80 || loaded.ColorActive {
		t.Fatal(loaded, err)
	}
	assertPreviewEVFileBytes(t, direct, before)
	settings := writeLongTermPreset(t, map[string]any{"filterContrastAndHueActive": true, "filterPresets": map[string]any{"default": map[string]any{"lumConst": true}, "named": map[string]any{"hueSmoothing": 33}}})
	loaded, err = loadLRTLongTermPreset(settings, "")
	if err != nil || loaded.Name != "default" || !loaded.ColorActive || !loaded.Preset.LumConst {
		t.Fatal(loaded, err)
	}
	loaded, err = loadLRTLongTermPreset(settings, "named")
	if err != nil || loaded.Name != "named" || loaded.Preset.HueSmoothing != 33 {
		t.Fatal(loaded, err)
	}
	single := writeLongTermPreset(t, map[string]any{"filterPresets": map[string]any{"only": map[string]any{}}})
	loaded, err = loadLRTLongTermPreset(single, "")
	if err != nil || loaded.Name != "only" || loaded.Preset != defaultLRTLongTermPreset() {
		t.Fatal(loaded, err)
	}
	clipped := writeLongTermPreset(t, map[string]any{"lumSmoothing": 200, "lumLow": -1, "lumHigh": 110, "contrastConstOffset": -1000, "hueConstOffset": 1000})
	loaded, err = loadLRTLongTermPreset(clipped, "")
	if err != nil || loaded.Preset.LumSmoothing != 50 || loaded.Preset.LumLow != 0 || loaded.Preset.LumHigh != 100 || loaded.Preset.ContrastConstOffset != -100 || loaded.Preset.HueConstOffset != 100 {
		t.Fatal("saved sliders did not clamp", loaded, err)
	}
}

func TestLRTLongTermPresetRejectsMalformed(t *testing.T) {
	for i, row := range []struct {
		value any
		name  string
	}{
		{nil, ""}, {[]any{}, ""}, {map[string]any{}, ""},
		{map[string]any{"filterPresets": map[string]any{}}, ""},
		{map[string]any{"filterPresets": map[string]any{"a": nil}}, ""},
		{map[string]any{"filterPresets": map[string]any{"a": map[string]any{}, "b": map[string]any{}}}, ""},
		{map[string]any{"filterPresets": map[string]any{"a": map[string]any{}}}, "missing"},
		{map[string]any{"lumConst": true}, "named"},
		{map[string]any{"lumSmoothing": 1.5}, ""}, {map[string]any{"lumConst": "true"}, ""},
		{map[string]any{"startHour": 25}, ""}, {map[string]any{"endHour": 24, "endMin": 1}, ""},
		{map[string]any{"filterContrastAndHueActive": "yes", "filterPresets": map[string]any{"default": map[string]any{}}}, ""},
	} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			if _, err := loadLRTLongTermPreset(writeLongTermPreset(t, row.value), row.name); err == nil {
				t.Fatal("accepted malformed saved filter", row)
			}
		})
	}
	path := filepath.Join(t.TempDir(), "large.json")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate((16 << 20) + 1); err != nil {
		t.Fatal(err)
	}
	file.Close()
	if _, err := loadLRTLongTermPreset(path, ""); err == nil {
		t.Fatal("accepted oversized filter file")
	}
}

func syntheticLongTermFilterManifest(count int) (*sequenceSelectionManifest, []Frame) {
	m := &sequenceSelectionManifest{MetricProfile: longTermMetricProfileLRT755, Frames: make([]longTermFrameMetrics, count)}
	for i := range m.Frames {
		hue := 108.0
		m.Frames[i] = longTermFrameMetrics{Included: true, LRTMetrics: &lrtLongTermMetrics{LuminanceMean: .4, ContrastStdDev: .25, HueMeanDegrees: &hue}}
	}
	return m, make([]Frame, count)
}

func TestLRTLongTermOrderedStagesAndRanges(t *testing.T) {
	for _, colorActive := range []bool{false, true} {
		t.Run(fmt.Sprint(colorActive), func(t *testing.T) {
			m, frames := syntheticLongTermFilterManifest(12)
			m.Frames[3].LRTMetrics.LuminanceMean = .9
			m.Frames[3].LRTMetrics.ContrastStdDev = .95
			m.Frames[5].LRTMetrics.ContrastStdDev = .7
			for _, i := range []int{3, 5, 7} {
				*m.Frames[i].LRTMetrics.HueMeanDegrees = 324
			}
			m.Frames[1].Included = false
			p := defaultLRTLongTermPreset()
			p.LumConst = true
			p.ContrastConst = true
			p.HueConst = true
			p.LumHigh = 50
			p.ContrastHigh = 50
			p.HueHigh = 50
			if err := applyLRTLongTermFilter(m, frames, &lrtLongTermSavedFilter{Preset: p, ColorActive: colorActive}); err != nil {
				t.Fatal(err)
			}
			for i, f := range m.Frames {
				want := i != 1 && i != 3 && (!colorActive || (i != 5 && i != 7))
				if f.Included != want {
					t.Fatalf("frame%d selected%v want%v", i, f.Included, want)
				}
				if i == 1 && f.LRTFilter.ExcludedBy != "range" {
					t.Fatal("static range did not intersect after native filtering")
				}
				if i == 3 && f.LRTFilter.ExcludedBy != "luminance" {
					t.Fatal("wrong luminance decision")
				}
			}
			if colorActive {
				if m.Frames[5].LRTFilter.ExcludedBy != "contrast" || m.Frames[7].LRTFilter.ExcludedBy != "hue" {
					t.Fatal("native stage order changed")
				}
				if m.Frames[3].LRTFilter.ContrastTarget != nil || m.Frames[5].LRTFilter.HueTargetDegrees != nil {
					t.Fatal("later stage restored an excluded frame")
				}
				if got := *m.Frames[0].LRTFilter.ContrastTarget; math.Abs(got-(.25*10+.7)/11) > 1e-14 {
					t.Fatalf("contrast used excluded frames or lost original selection: %.17g", got)
				}
				if got := *m.Frames[0].LRTFilter.HueTargetDegrees; math.Abs(got-(108.*9+324)/10) > 1e-12 {
					t.Fatalf("hue used excluded contrast frames: %.17g", got)
				}
			}
		})
	}
}

func TestLRTLongTermDegenerateTargetsAndThresholds(t *testing.T) {
	var gold struct{ ThresholdBits [][2]string }
	if err := json.Unmarshal(denoiseRead(t, "testdata/lrt-longterm-curves-7.5.5.json"), &gold); err != nil || len(gold.ThresholdBits) != 101 {
		t.Fatalf("expected 101 JVM threshold pairs: %v", err)
	}
	for slider, row := range gold.ThresholdBits {
		lo, hi, err := nativeLongTermThresholds(slider, slider)
		if err != nil {
			t.Fatal(err)
		}
		for i, got := range []float64{lo, hi} {
			bits, err := strconv.ParseUint(row[i], 16, 32)
			if err != nil {
				t.Fatal(err)
			}
			if math.Float32bits(float32(got)) != uint32(bits) {
				t.Fatalf("threshold%d/%d has different float32 bits", slider, i)
			}
		}
	}
	for low := 0; low <= 100; low++ {
		for high := 0; high <= 100; high++ {
			lo, hi, err := nativeLongTermThresholds(low, high)
			if err != nil || lo > 0 || hi < 0 || lo < -.200001 || hi > .200001 {
				t.Fatal("invalid native thresholds", lo, hi, err)
			}
		}
	}
	p := defaultLRTLongTermPreset()
	p.LumSmoothing = 5
	p.LumLow = 100
	p.LumHigh = 0
	m, frames := syntheticLongTermFilterManifest(1)
	if err := applyLRTLongTermFilter(m, frames, &lrtLongTermSavedFilter{Preset: p}); err != nil {
		t.Fatal(err)
	}
	if !m.Frames[0].Included || m.Frames[0].LRTFilter.LuminanceTarget != nil {
		t.Fatal("single native NaN target changed the selection")
	}
	data, err := json.Marshal(m)
	if err != nil || bytes.Contains(data, []byte("NaN")) {
		t.Fatal("native NaN leaked into output", err)
	}
	m, frames = syntheticLongTermFilterManifest(8)
	p.LumConst = true
	if err := applyLRTLongTermFilter(m, frames, &lrtLongTermSavedFilter{Preset: p, ColorActive: true}); err != nil {
		t.Fatal(err)
	}
	for _, f := range m.Frames {
		if f.Included || f.LRTFilter.ExcludedBy != "luminance" {
			t.Fatal("inclusive native zero-width threshold did not reject", f)
		}
	}
	p.LumConst = false
	p.LumSmoothing = 0
	m, frames = syntheticLongTermFilterManifest(8)
	if err := applyLRTLongTermFilter(m, frames, &lrtLongTermSavedFilter{Preset: p}); err != nil {
		t.Fatal(err)
	}
	for _, f := range m.Frames {
		if !f.Included || f.LRTFilter.LuminanceTarget != nil {
			t.Fatal("inactive smoothing applied thresholds")
		}
	}
}
