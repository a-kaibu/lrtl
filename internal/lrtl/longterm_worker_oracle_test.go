package lrtl

import (
	"encoding/json"
	"fmt"
	"math"
	"testing"
)

type nativeLongTermWorkerCase struct {
	Count                  int                    `json:"count"`
	Pattern                int                    `json:"pattern"`
	Config                 int                    `json:"config"`
	ColorActive            bool                   `json:"colorActive"`
	Metrics                []nativeLongTermMetric `json:"metrics"`
	Preset                 lrtLongTermPreset      `json:"preset"`
	SelectedAfterLuminance []int                  `json:"selectedAfterLuminance"`
	SelectedAfterContrast  []int                  `json:"selectedAfterContrast"`
	SelectedAfterHue       []int                  `json:"selectedAfterHue"`
	LuminanceTargets       []*float64             `json:"luminanceTargets"`
	ContrastTargets        []*float64             `json:"contrastTargets"`
	HueTargets             []*float64             `json:"hueTargets"`
}

type nativeLongTermMetric struct {
	Luminance float64 `json:"luminance"`
	Contrast  float64 `json:"contrast"`
	Hue       float64 `json:"hue"`
}

func TestLRTLongTermNativeWorkerSelections(t *testing.T) {
	var fixture struct {
		Version int                        `json:"version"`
		Cases   []nativeLongTermWorkerCase `json:"cases"`
	}
	if err := json.Unmarshal(denoiseRead(t, "testdata/lrt-longterm-worker-7.5.5.json"), &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Version != 1 || len(fixture.Cases) != 168 {
		t.Fatalf("expected 168 native long-term worker cases, version %d cases %d", fixture.Version, len(fixture.Cases))
	}

	for _, tc := range fixture.Cases {
		t.Run(fmt.Sprintf("n%d/p%d/c%d", tc.Count, tc.Pattern, tc.Config), func(t *testing.T) {
			if len(tc.Metrics) != tc.Count || len(tc.LuminanceTargets) != tc.Count || len(tc.ContrastTargets) != tc.Count || len(tc.HueTargets) != tc.Count {
				t.Fatalf("fixture frame count mismatch: count=%d metrics=%d targets=%d/%d/%d", tc.Count, len(tc.Metrics), len(tc.LuminanceTargets), len(tc.ContrastTargets), len(tc.HueTargets))
			}
			manifest := &sequenceSelectionManifest{
				MetricProfile: longTermMetricProfileLRT755,
				Frames:        make([]longTermFrameMetrics, tc.Count),
			}
			frames := make([]Frame, tc.Count)
			for i, metric := range tc.Metrics {
				hue := metric.Hue * 360
				manifest.Frames[i] = longTermFrameMetrics{
					Included: true,
					LRTMetrics: &lrtLongTermMetrics{
						LuminanceMean:  metric.Luminance,
						ContrastStdDev: metric.Contrast,
						HueMeanDegrees: &hue,
					},
				}
				frames[i] = Frame{Index: i}
			}

			if err := applyLRTLongTermFilter(manifest, frames, &lrtLongTermSavedFilter{Preset: tc.Preset, ColorActive: tc.ColorActive}); err != nil {
				t.Fatal(err)
			}

			var afterLuminance, afterContrast, afterHue []int
			for i, frame := range manifest.Frames {
				reason := ""
				if frame.LRTFilter != nil {
					reason = frame.LRTFilter.ExcludedBy
				}
				if reason != "luminance" {
					afterLuminance = append(afterLuminance, i)
				}
				if reason != "luminance" && (!tc.ColorActive || reason != "contrast") {
					afterContrast = append(afterContrast, i)
				}
				if frame.Included {
					afterHue = append(afterHue, i)
				}
			}
			if !equalIntSlices(afterLuminance, tc.SelectedAfterLuminance) {
				t.Fatalf("after luminance got %v want %v", afterLuminance, tc.SelectedAfterLuminance)
			}
			if !equalIntSlices(afterContrast, tc.SelectedAfterContrast) {
				t.Fatalf("after contrast got %v want %v", afterContrast, tc.SelectedAfterContrast)
			}
			if !equalIntSlices(afterHue, tc.SelectedAfterHue) {
				t.Fatalf("after hue got %v want %v", afterHue, tc.SelectedAfterHue)
			}

			for i, frame := range manifest.Frames {
				if frame.LRTFilter == nil {
					t.Fatalf("frame %d has no long-term decision", i)
				}
				compareNativeLongTermTarget(t, i, "luminance", frame.LRTFilter.LuminanceTarget, tc.LuminanceTargets[i], 1)
				compareNativeLongTermTarget(t, i, "contrast", frame.LRTFilter.ContrastTarget, tc.ContrastTargets[i], 1)
				// The native worker stores hue references normalized to 0..1;
				// the public Go manifest reports degrees.
				compareNativeLongTermTarget(t, i, "hue", frame.LRTFilter.HueTargetDegrees, tc.HueTargets[i], 1.0/360)
			}
		})
	}
}

func compareNativeLongTermTarget(t *testing.T, frame int, axis string, got, want *float64, scale float64) {
	t.Helper()
	if got == nil || want == nil {
		if got != nil || want != nil {
			t.Fatalf("frame %d %s target got %v want %v", frame, axis, got, want)
		}
		return
	}
	if math.IsNaN(*got) || math.IsInf(*got, 0) || math.Abs(*got*scale-*want) > 1e-10 {
		t.Fatalf("frame %d %s target got %.17g want %.17g", frame, axis, *got*scale, *want)
	}
}
