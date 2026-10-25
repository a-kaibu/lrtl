package lrtl

import (
	"encoding/json"
	"fmt"
	"os"
)

// lrtDeflickerPreferences contains only the global LRT preferences that
// control native Preview EV histogram filtering. Other application settings
// are deliberately ignored.
type lrtDeflickerPreferences struct {
	Enabled      *bool `json:"defaultDeflickerIgnoreBlacksWhites"`
	IgnoreBlacks *int  `json:"defaultDeflickerIgnoreBlacks"`
	IgnoreWhites *int  `json:"defaultDeflickerIgnoreWhites"`
	HardCut      *bool `json:"deflickerIgnoreBlacksWhitesHardCut"`
}

func readLRTDeflickerPreferences(path string) (previewHistogramOptions, error) {
	fail := func(err error) (previewHistogramOptions, error) {
		return previewHistogramOptions{}, fmt.Errorf("read LRT Deflicker preferences %s: %w", path, err)
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
	options := previewHistogramOptions{
		Enabled:            *saved.Enabled,
		IgnoreBlacks:       *saved.IgnoreBlacks,
		IgnoreWhites:       *saved.IgnoreWhites,
		HardCut:            *saved.HardCut,
		FromLRTPreferences: true,
	}
	if err := options.validate(); err != nil {
		return fail(err)
	}
	return options, nil
}
