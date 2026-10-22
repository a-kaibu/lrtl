package lrtl

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
)

type lrtLongTermPreset struct {
	LumSmoothing        int  `json:"lumSmoothing"`
	LumLow              int  `json:"lumLow"`
	LumHigh             int  `json:"lumHigh"`
	LumConstOffset      int  `json:"lumConstOffset"`
	LumConst            bool `json:"lumConst"`
	ContrastSmoothing   int  `json:"contrastSmoothing"`
	ContrastLow         int  `json:"contrastLow"`
	ContrastHigh        int  `json:"contrastHigh"`
	ContrastConstOffset int  `json:"contrastConstOffset"`
	ContrastConst       bool `json:"contrastConst"`
	HueSmoothing        int  `json:"hueSmoothing"`
	HueLow              int  `json:"hueLow"`
	HueHigh             int  `json:"hueHigh"`
	HueConstOffset      int  `json:"hueConstOffset"`
	HueConst            bool `json:"hueConst"`
	TimeFilterActive    bool `json:"timeFilterActive"`
	StartHour           int  `json:"startHour"`
	StartMin            int  `json:"startMin"`
	EndHour             int  `json:"endHour"`
	EndMin              int  `json:"endMin"`
	DayMon              bool `json:"dayMon"`
	DayTue              bool `json:"dayTue"`
	DayWed              bool `json:"dayWed"`
	DayThu              bool `json:"dayThu"`
	DayFri              bool `json:"dayFri"`
	DaySat              bool `json:"daySat"`
	DaySun              bool `json:"daySun"`
}

type lrtLongTermSavedFilter struct {
	Name           string            `json:"preset_name,omitempty"`
	Preset         lrtLongTermPreset `json:"preset"`
	ColorActive    bool              `json:"contrast_and_hue_active"`
	TimeApplied    bool              `json:"time_filter_applied"`
	TimeSkipReason string            `json:"time_filter_skip_reason,omitempty"`
}

func defaultLRTLongTermPreset() lrtLongTermPreset {
	return lrtLongTermPreset{LumHigh: 100, ContrastHigh: 100, HueHigh: 100, EndHour: 24,
		DayMon: true, DayTue: true, DayWed: true, DayThu: true, DayFri: true, DaySat: true, DaySun: true}
}

func validateLRTLongTermPreset(p lrtLongTermPreset) error {
	for _, axis := range []struct{ smoothing, low, high, offset int }{{p.LumSmoothing, p.LumLow, p.LumHigh, p.LumConstOffset}, {p.ContrastSmoothing, p.ContrastLow, p.ContrastHigh, p.ContrastConstOffset}, {p.HueSmoothing, p.HueLow, p.HueHigh, p.HueConstOffset}} {
		if axis.smoothing < 0 || axis.smoothing > 50 || axis.offset < -100 || axis.offset > 100 {
			return fmt.Errorf("saved smoothing must be 0..50 and Constant offset -100..100")
		}
		if _, _, err := nativeLongTermThresholds(axis.low, axis.high); err != nil {
			return err
		}
	}
	if p.StartHour < 0 || p.StartHour > 24 || p.EndHour < 0 || p.EndHour > 24 || p.StartMin < 0 || p.StartMin > 59 || p.EndMin < 0 || p.EndMin > 59 || (p.StartHour == 24 && p.StartMin != 0) || (p.EndHour == 24 && p.EndMin != 0) {
		return fmt.Errorf("saved time bounds must be 00:00..24:00")
	}
	return nil
}

func loadLRTLongTermPreset(path, name string) (*lrtLongTermSavedFilter, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	const limit = 16 << 20
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, fmt.Errorf("filter preset must be a regular JSON file of at most 16 MiB")
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if len(data) > limit {
		return nil, fmt.Errorf("filter preset exceeds 16 MiB")
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil || object == nil {
		return nil, fmt.Errorf("expected a filter preset or LRT settings object: %v", err)
	}
	result := &lrtLongTermSavedFilter{Preset: defaultLRTLongTermPreset()}
	fromSettings := false
	if raw, ok := object["filterPresets"]; ok {
		fromSettings = true
		var presets map[string]json.RawMessage
		if err := json.Unmarshal(raw, &presets); err != nil || len(presets) == 0 {
			return nil, fmt.Errorf("filterPresets must be a nonempty object")
		}
		if name == "" {
			if _, ok := presets["default"]; ok {
				name = "default"
			} else if len(presets) == 1 {
				for key := range presets {
					name = key
				}
			} else {
				return nil, fmt.Errorf("choose an exact filter preset name; available: %v", sortedLongTermPresetNames(presets))
			}
		}
		selected, ok := presets[name]
		if !ok {
			return nil, fmt.Errorf("filter preset %q was not found; available: %v", name, sortedLongTermPresetNames(presets))
		}
		data = selected
		result.Name = name
		if raw, ok := object["filterContrastAndHueActive"]; ok {
			if err := json.Unmarshal(raw, &result.ColorActive); err != nil {
				return nil, fmt.Errorf("filterContrastAndHueActive: %w", err)
			}
		}
	} else if name != "" {
		return nil, fmt.Errorf("--lrt-filter-preset-name requires a settings object containing filterPresets")
	}
	var selected map[string]json.RawMessage
	if err := json.Unmarshal(data, &selected); err != nil || selected == nil {
		return nil, fmt.Errorf("selected filter preset must be an object")
	}
	knownData, _ := json.Marshal(result.Preset)
	var known map[string]json.RawMessage
	_ = json.Unmarshal(knownData, &known)
	identified := false
	for field := range known {
		if _, ok := selected[field]; ok {
			identified = true
			break
		}
	}
	if !identified && !fromSettings {
		return nil, fmt.Errorf("selected object contains no native filter settings")
	}
	if err := json.Unmarshal(data, &result.Preset); err != nil {
		return nil, fmt.Errorf("filter preset: %w", err)
	}
	clampLRTLongTermSliders(&result.Preset)
	if err := validateLRTLongTermPreset(result.Preset); err != nil {
		return nil, err
	}
	return result, nil
}

func clampLRTLongTermSliders(p *lrtLongTermPreset) {
	for _, v := range []*int{&p.LumSmoothing, &p.ContrastSmoothing, &p.HueSmoothing} {
		*v = max(0, min(50, *v))
	}
	for _, v := range []*int{&p.LumLow, &p.LumHigh, &p.ContrastLow, &p.ContrastHigh, &p.HueLow, &p.HueHigh} {
		*v = max(0, min(100, *v))
	}
	for _, v := range []*int{&p.LumConstOffset, &p.ContrastConstOffset, &p.HueConstOffset} {
		*v = max(-100, min(100, *v))
	}
}

func sortedLongTermPresetNames(presets map[string]json.RawMessage) []string {
	names := make([]string, 0, len(presets))
	for name := range presets {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
