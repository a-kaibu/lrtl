package lrtl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestLongTermNativeTimeFilters(t *testing.T) {
	var rows []struct {
		Start, End          int
		Days                []bool
		Available, Shortcut bool
		Dates               []*string
		Selected            []int
	}
	if err := json.Unmarshal(denoiseRead(t, "testdata/lrt-longterm-time-7.5.5.json"), &rows); err != nil || len(rows) != 36 {
		t.Fatalf("expected 36 native time-filter cases: %v", err)
	}
	for caseIndex, row := range rows {
		t.Run(fmt.Sprint(caseIndex), func(t *testing.T) {
			dir := t.TempDir()
			frames := make([]Frame, len(row.Dates))
			manifest := &sequenceSelectionManifest{MetricProfile: longTermMetricProfileLRT755, Frames: make([]longTermFrameMetrics, len(frames))}
			for i, date := range row.Dates {
				path := filepath.Join(dir, fmt.Sprintf("frame%02d.png", i))
				if err := os.WriteFile(path, []byte("metadata-only fixture"), 0600); err != nil {
					t.Fatal(err)
				}
				values := map[string]string{}
				if date != nil {
					values["exif:DateTimeOriginal"] = *date
				}
				writeTestXMP(t, path, values)
				frames[i] = Frame{Index: i, Image: path, Sidecar: path + ".xmp"}
				hue := 120.0
				manifest.Frames[i] = longTermFrameMetrics{Included: true, LRTMetrics: &lrtLongTermMetrics{LuminanceMean: .4, ContrastStdDev: .3, HueMeanDegrees: &hue}}
			}
			p := defaultLRTLongTermPreset()
			p.StartHour = row.Start / 60
			p.StartMin = row.Start % 60
			p.EndHour = row.End / 60
			p.EndMin = row.End % 60
			p.DayMon = row.Days[0]
			p.DayTue = row.Days[1]
			p.DayWed = row.Days[2]
			p.DayThu = row.Days[3]
			p.DayFri = row.Days[4]
			p.DaySat = row.Days[5]
			p.DaySun = row.Days[6]
			if err := applyLRTLongTermFilter(manifest, frames, &lrtLongTermSavedFilter{Preset: p}); err != nil {
				t.Fatal(err)
			}
			var got []int
			for i, frame := range manifest.Frames {
				if frame.Included {
					got = append(got, i)
				}
			}
			if !equalIntSlices(got, row.Selected) {
				t.Fatalf("selected%v want%v", got, row.Selected)
			}
			if manifest.LRTFilter.TimeApplied != (row.Available && !row.Shortcut) {
				t.Fatalf("wrong time-filter status %+v", manifest.LRTFilter)
			}
			serialized, err := json.Marshal(manifest)
			if err != nil || bytes.Contains(serialized, []byte("NaN")) {
				t.Fatal("invalid filter manifest", err)
			}
		})
	}
}

func equalIntSlices(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

type longTermCurveObservation struct {
	Count, Pattern, Setting int
	Indices                 []int
	Constant                bool
	Input                   []float64
	Target                  []*float64
}

func TestLongTermNativeCurveHelpers(t *testing.T) {
	var data struct {
		Defaults map[string]any
		Rows     []longTermCurveObservation
	}
	if err := json.Unmarshal(denoiseRead(t, "testdata/lrt-longterm-curves-7.5.5.json"), &data); err != nil || len(data.Rows) != 924 {
		t.Fatalf("expected 924 native long-term curves: %v", err)
	}
	for _, row := range data.Rows {
		t.Run(fmt.Sprintf("n%d/k%d/p%d/c%v/s%d", row.Count, len(row.Indices), row.Pattern, row.Constant, row.Setting), func(t *testing.T) {
			smoothing, offset := row.Setting, 0
			if row.Constant {
				smoothing = 0
				offset = row.Setting
			}
			got, err := nativeLongTermTarget(row.Input, row.Count, smoothing, offset, row.Constant)
			if err != nil || len(got) != len(row.Target) {
				t.Fatal("native target size", len(got), len(row.Target), err)
			}
			for i, want := range row.Target {
				if want == nil {
					if !math.IsNaN(got[i]) {
						t.Fatalf("frame%d expected native NaN", i)
					}
					continue
				}
				if math.IsNaN(got[i]) || math.Abs(got[i]-*want) > 1e-10 {
					t.Fatalf("frame%d got %.17g want %.17g", i, got[i], *want)
				}
			}
		})
	}
}
