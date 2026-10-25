package lrtl

import (
	"encoding/json"
	"fmt"
	"os"
)

// lrtDeflickerPreferences contains the global LRT settings used by native
// Deflicker when older sequence files omit a value. Other application settings
// are deliberately ignored.
type lrtDeflickerPreferences struct {
	Enabled      *bool    `json:"defaultDeflickerIgnoreBlacksWhites"`
	IgnoreBlacks *int     `json:"defaultDeflickerIgnoreBlacks"`
	IgnoreWhites *int     `json:"defaultDeflickerIgnoreWhites"`
	HardCut      *bool    `json:"deflickerIgnoreBlacksWhitesHardCut"`
	Smoothing    *float64 `json:"defaultDeflickerSmoothingFactor"`
	Accuracy     *int     `json:"defaultDeflickerAccuracy"`
	Constant     *bool    `json:"defaultDeflickerConstant"`
	Multipass    *bool    `json:"defaultDeflickerMultipass"`
	Passes       *int     `json:"defaultDeflickerPasses"`
}

func (p lrtDeflickerPreferences) histogramOptions() previewHistogramOptions {
	return previewHistogramOptions{
		Enabled:            valueOr(p.Enabled, false),
		IgnoreBlacks:       valueOr(p.IgnoreBlacks, 0),
		IgnoreWhites:       valueOr(p.IgnoreWhites, 0),
		HardCut:            valueOr(p.HardCut, false),
		FromLRTPreferences: true,
	}
}

func readLRTDeflickerPreferences(path string) (lrtDeflickerPreferences, error) {
	fail := func(err error) (lrtDeflickerPreferences, error) {
		return lrtDeflickerPreferences{}, fmt.Errorf("read LRT Deflicker preferences %s: %w", path, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fail(err)
	}
	var saved lrtDeflickerPreferences
	if err := json.Unmarshal(data, &saved); err != nil {
		return fail(err)
	}
	if saved.Enabled == nil || saved.IgnoreBlacks == nil || saved.IgnoreWhites == nil || saved.HardCut == nil {
		return fail(fmt.Errorf("defaultDeflickerIgnoreBlacksWhites, defaultDeflickerIgnoreBlacks, defaultDeflickerIgnoreWhites and deflickerIgnoreBlacksWhitesHardCut are required"))
	}
	if err := saved.histogramOptions().validate(); err != nil {
		return fail(err)
	}
	if saved.Smoothing != nil {
		options := nativeDeflickerOptions{Enabled: true, Smoothing: *saved.Smoothing, Accuracy: valueOr(saved.Accuracy, 0)}
		if err := options.validate(); err != nil {
			return fail(fmt.Errorf("defaultDeflickerSmoothingFactor: %w", err))
		}
	}
	if saved.Accuracy != nil && (*saved.Accuracy < -1 || *saved.Accuracy > 1) {
		return fail(fmt.Errorf("defaultDeflickerAccuracy must be -1, 0 or 1"))
	}
	if saved.Passes != nil && (*saved.Passes < 1 || *saved.Passes > 8) {
		return fail(fmt.Errorf("defaultDeflickerPasses must be 1-8"))
	}
	return saved, nil
}

func valueOr[T any](value *T, fallback T) T {
	if value == nil {
		return fallback
	}
	return *value
}
