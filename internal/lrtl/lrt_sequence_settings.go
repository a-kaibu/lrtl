package lrtl

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
)

// lrtSequenceSettings contains sequence-level values serialized by LRT 7.5.5
// in .lrt/lrtsequence.json. Pointers distinguish a saved zero/false from an
// omitted field in an older or partial settings file.
type lrtSequenceSettings struct {
	FilenameSort             *bool    `json:"filenameSort"`
	HolyGrailReduceDeflicker *float64 `json:"holyGrailReduceDeflicker"`
	HolyGrailStretch         *float64 `json:"holyGrailStretch"`
	HolyGrailRotate          *float64 `json:"holyGrailRotate"`
}

func readLRTSequenceSettings(frames []Frame) (lrtSequenceSettings, error) {
	if len(frames) == 0 {
		return lrtSequenceSettings{}, fmt.Errorf("at least one frame is required to read LRT sequence settings")
	}
	path := filepath.Join(filepath.Dir(frames[0].Image), ".lrt", "lrtsequence.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return lrtSequenceSettings{}, fmt.Errorf("read LRT sequence settings %s: %w", path, err)
	}
	var settings lrtSequenceSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return lrtSequenceSettings{}, fmt.Errorf("read LRT sequence settings %s: %w", path, err)
	}
	return settings, nil
}

func readLRTSequenceSortMode(frames []Frame) (string, error) {
	settings, err := readLRTSequenceSettings(frames)
	if err != nil {
		return "", err
	}
	if settings.FilenameSort == nil {
		return "", fmt.Errorf("LRT sequence settings have no filenameSort value")
	}
	if *settings.FilenameSort {
		return "lrt-name", nil
	}
	return "lrt-date", nil
}

// applyLRTSequenceHolyGrailSettings follows the sequence's saved LRT order and
// curve controls. LRT's stored values are separate from per-frame XMP.
func applyLRTSequenceHolyGrailSettings(frames []Frame) ([]Frame, *HolyGrailCurveOptions, error) {
	settings, err := readLRTSequenceSettings(frames)
	if err != nil {
		return nil, nil, err
	}
	if settings.FilenameSort == nil || settings.HolyGrailReduceDeflicker == nil || settings.HolyGrailStretch == nil || settings.HolyGrailRotate == nil {
		return nil, nil, fmt.Errorf("LRT sequence settings require filenameSort, holyGrailReduceDeflicker, holyGrailStretch and holyGrailRotate")
	}
	options := &HolyGrailCurveOptions{
		LRTInput:        true,
		ReduceDeflicker: *settings.HolyGrailReduceDeflicker,
		Stretch:         *settings.HolyGrailStretch,
		Rotate:          *settings.HolyGrailRotate,
	}
	if math.IsNaN(options.ReduceDeflicker) || math.IsInf(options.ReduceDeflicker, 0) ||
		math.IsNaN(options.Stretch) || math.IsInf(options.Stretch, 0) ||
		math.IsNaN(options.Rotate) || math.IsInf(options.Rotate, 0) ||
		options.ReduceDeflicker < 0 || options.ReduceDeflicker > 1 || options.Stretch < -1.5 || options.Stretch > 1.5 || options.Rotate < -4 || options.Rotate > 4 {
		return nil, nil, fmt.Errorf("LRT Holy Grail sequence settings are outside their supported ranges")
	}
	mode := "lrt-name"
	if !*settings.FilenameSort {
		mode = "lrt-date"
	}
	ordered, err := orderSequenceFrames(frames, mode)
	if err != nil {
		return nil, nil, err
	}
	return ordered, options, nil
}
