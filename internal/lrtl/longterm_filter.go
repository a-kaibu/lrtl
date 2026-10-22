package lrtl

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// nativeLongTermTarget models the observed native curve helpers. Selection
// positions affect bandwidth through the original/selected integer count
// ratio; regression itself runs on the selected samples' ordinal positions.
func nativeLongTermTarget(values []float64, count, smoothing, offset int, constant bool) ([]float64, error) {
	if count < len(values) || count < 1 || smoothing < 0 || smoothing > 50 || offset < -100 || offset > 100 {
		return nil, fmt.Errorf("invalid native long-term curve parameters")
	}
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 1 {
			return nil, fmt.Errorf("native long-term samples must be finite and between zero and one")
		}
	}
	if len(values) == 0 || (!constant && smoothing == 0) {
		return nil, nil
	}
	target := append([]float64(nil), values...)
	if constant {
		mean := 0.0
		for _, value := range values {
			mean += value
		}
		mean = max(0, min(1, mean/float64(len(values))+float64(offset)/150))
		for i := range target {
			target[i] = mean
		}
		return target, nil
	}
	if len(values) < 8 {
		step := (values[len(values)-1] - values[0]) / float64(len(values)-1)
		for i := range target {
			target[i] = values[0] + step*float64(i)
		}
		// One selected sample produces native NaN. Comparisons retain that
		// frame; the public manifest converts the missing target to null.
		return target, nil
	}
	return lrtHolyGrailLowess(values, float64(smoothing)/80*float64(count/len(values))), nil
}

type lrtLongTermFrameDecision struct {
	LuminanceTarget  *float64 `json:"luminance_target,omitempty"`
	ContrastTarget   *float64 `json:"contrast_target,omitempty"`
	HueTargetDegrees *float64 `json:"hue_target_degrees,omitempty"`
	ExcludedBy       string   `json:"excluded_by,omitempty"`
}

type nativeLongTermAxisSettings struct {
	smoothing, low, high, offset int
	constant                     bool
	name                         string
}

func applyLRTLongTermFilter(manifest *sequenceSelectionManifest, frames []Frame, saved *lrtLongTermSavedFilter) error {
	if manifest == nil || saved == nil || len(frames) != len(manifest.Frames) || len(frames) == 0 || manifest.MetricProfile != longTermMetricProfileLRT755 {
		return fmt.Errorf("native long-term filter needs matching frames and native metrics")
	}
	if err := validateLRTLongTermPreset(saved.Preset); err != nil {
		return err
	}
	metrics := make([][3]float64, len(frames))
	for i, frame := range manifest.Frames {
		m := frame.LRTMetrics
		if m == nil || m.HueMeanDegrees == nil {
			return fmt.Errorf("frame%d has no native long-term metrics", i+1)
		}
		metrics[i] = [3]float64{m.LuminanceMean, m.ContrastStdDev, *m.HueMeanDegrees / 360}
		for _, v := range metrics[i] {
			if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 1 {
				return fmt.Errorf("invalid native long-term metric at frame%d", i+1)
			}
		}
	}
	selection := make([]bool, len(frames))
	decisions := make([]lrtLongTermFrameDecision, len(frames))
	for i := range selection {
		selection[i] = true
	}
	config := *saved
	if err := applyLRTLongTermTimeFilter(frames, &config, selection, decisions); err != nil {
		return err
	}
	p := saved.Preset
	axes := []nativeLongTermAxisSettings{{p.LumSmoothing, p.LumLow, p.LumHigh, p.LumConstOffset, p.LumConst, "luminance"}}
	if saved.ColorActive {
		axes = append(axes, nativeLongTermAxisSettings{p.ContrastSmoothing, p.ContrastLow, p.ContrastHigh, p.ContrastConstOffset, p.ContrastConst, "contrast"}, nativeLongTermAxisSettings{p.HueSmoothing, p.HueLow, p.HueHigh, p.HueConstOffset, p.HueConst, "hue"})
	}
	for axis, settings := range axes {
		var indices []int
		var values []float64
		for i, selected := range selection {
			if selected {
				indices = append(indices, i)
				values = append(values, metrics[i][axis])
			}
		}
		target, err := nativeLongTermTarget(values, len(frames), settings.smoothing, settings.offset, settings.constant)
		if err != nil {
			return err
		}
		if len(target) == 0 {
			continue
		}
		lower, upper, err := nativeLongTermThresholds(settings.low, settings.high)
		if err != nil {
			return err
		}
		for j, i := range indices {
			if !math.IsNaN(target[j]) {
				v := target[j]
				switch axis {
				case 0:
					decisions[i].LuminanceTarget = &v
				case 1:
					decisions[i].ContrastTarget = &v
				case 2:
					v *= 360
					decisions[i].HueTargetDegrees = &v
				}
			}
			delta := values[j] - target[j]
			if delta <= lower || delta >= upper {
				selection[i] = false
				decisions[i].ExcludedBy = settings.name
			}
		}
	}
	for i := range selection {
		if selection[i] && !manifest.Frames[i].Included {
			decisions[i].ExcludedBy = "range"
		}
		manifest.Frames[i].Included = selection[i] && manifest.Frames[i].Included
		manifest.Frames[i].LRTFilter = &decisions[i]
	}
	manifest.LRTFilter = &config
	return nil
}

func defaultLRTLongTermTime(p lrtLongTermPreset) bool {
	return p.DayMon && p.DayTue && p.DayWed && p.DayThu && p.DayFri && p.DaySat && p.DaySun && p.StartHour == 0 && p.StartMin == 0 && p.EndHour == 24 && p.EndMin == 0
}

// A native unavailable date disables the entire time filter. A present date
// that cannot be parsed is retained by that stage. File modification time is
// never used as a capture timestamp.
func longTermCaptureClock(frame Frame) (time.Time, bool, bool, error) {
	packet, err := PacketForFrame(frame)
	if err != nil {
		return time.Time{}, false, false, err
	}
	for _, property := range []string{"exif:DateTimeOriginal", "xmp:CreateDate", "xmp:ModifyDate"} {
		value, found, err := packet.GetScalar(property)
		if err != nil {
			return time.Time{}, false, false, err
		}
		if !found {
			continue
		}
		value = strings.TrimSpace(value)
		if value == "" || value == "n/a" {
			return time.Time{}, false, false, nil
		}
		stamp, valid := parseLRTLongTermDate(value)
		return stamp, true, valid, nil
	}
	stamp, found, err := captureTimeFromImage(frame)
	if err != nil || !found {
		return time.Time{}, false, false, nil
	}
	return stamp, true, true, nil
}

var lrtLongTermDatePrefix = regexp.MustCompile(`^([0-9]{1,4})-([0-9]+)-([0-9]+) ([0-9]+):([0-9]+):([0-9]+)`)

func parseLRTLongTermDate(value string) (time.Time, bool) {
	if stamp, valid := parseCaptureTimestamp(value); valid {
		return stamp, true
	}
	// Native SimpleDateFormat is lenient and accepts a parsed prefix. Keep
	// wall-clock fields rather than converting an offset to this host's zone.
	parts := lrtLongTermDatePrefix.FindStringSubmatch(strings.ReplaceAll(strings.TrimSpace(value), "T", " "))
	if parts == nil {
		return time.Time{}, false
	}
	var v [6]int
	for i := range v {
		n, err := strconv.Atoi(parts[i+1])
		if err != nil || n > 1000000 {
			return time.Time{}, false
		}
		v[i] = n
	}
	if v[0] < 1 || v[0] > 9999 {
		return time.Time{}, false
	}
	return time.Date(v[0], time.Month(v[1]), v[2], v[3], v[4], v[5], 0, time.UTC), true
}

func applyLRTLongTermTimeFilter(frames []Frame, saved *lrtLongTermSavedFilter, selected []bool, decisions []lrtLongTermFrameDecision) error {
	p := saved.Preset
	saved.TimeApplied = false
	saved.TimeSkipReason = ""
	if defaultLRTLongTermTime(p) {
		return nil
	}
	stamps := make([]time.Time, len(frames))
	valid := make([]bool, len(frames))
	for i, frame := range frames {
		stamp, present, parsed, err := longTermCaptureClock(frame)
		if err != nil {
			return err
		}
		if !present {
			saved.TimeSkipReason = "capture times incomplete"
			return nil
		}
		stamps[i], valid[i] = stamp, parsed
	}
	saved.TimeApplied = true
	weekdays := [7]bool{p.DaySun, p.DayMon, p.DayTue, p.DayWed, p.DayThu, p.DayFri, p.DaySat}
	start, end := p.StartHour*60+p.StartMin, p.EndHour*60+p.EndMin
	for i, stamp := range stamps {
		if !valid[i] {
			continue
		}
		minute := stamp.Hour()*60 + stamp.Minute()
		if !weekdays[stamp.Weekday()] || minute <= start || minute >= end {
			selected[i] = false
			decisions[i].ExcludedBy = "time"
		}
	}
	return nil
}

func nativeLongTermThresholds(low, high int) (float64, float64, error) {
	if low < 0 || low > 100 || high < 0 || high > 100 {
		return 0, 0, fmt.Errorf("native long-term thresholds must be between 0 and 100")
	}
	lower := -float32(2 - float32(low)/50)
	upper := float32(2 - float32(100-high)/50)
	return float64(lower / 10), float64(upper / 10), nil
}
