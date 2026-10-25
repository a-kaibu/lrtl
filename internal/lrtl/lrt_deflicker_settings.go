package lrtl

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Pointers distinguish saved zero/false values from controls absent in older
// files. Missing values would require the user's separate LRT global defaults.
type lrtDeflickerSettings struct {
	Smoothing    *float64 `json:"deflickerSmoothingFactor"`
	Accuracy     *int     `json:"deflickerAccuracy"`
	Constant     *bool    `json:"deflickerConstant"`
	Multipass    *bool    `json:"deflickerMultipass"`
	Passes       *int     `json:"deflickerPasses"`
	FilenameSort *bool    `json:"filenameSort"`
	ImageCount   int      `json:"imageCount"`
	Ignore       *int     `json:"deflickerIgnoreBlacksWhites"`
}

func loadLRTDeflickerSettings(frames []Frame, histogram *previewHistogramOptions) (nativeDeflickerOptions, int, bool, []int, error) {
	fail := func(err error) (nativeDeflickerOptions, int, bool, []int, error) {
		return nativeDeflickerOptions{}, 0, false, nil, fmt.Errorf("read LRT Deflicker settings: %w", err)
	}
	if histogram == nil {
		return fail(fmt.Errorf("histogram options are required"))
	}
	resolvedHistogram := *histogram
	if len(frames) < 2 {
		return fail(fmt.Errorf("at least two frames are required"))
	}
	root := filepath.Dir(frames[0].Image)
	complete, err := DiscoverSequence(root)
	if err != nil {
		return fail(err)
	}
	if len(complete) != len(frames) {
		return fail(fmt.Errorf("complete sequence in filename order is required"))
	}
	for i, frame := range frames {
		if !sameFilesystemPath(frame.Image, complete[i].Image) {
			return fail(fmt.Errorf("complete sequence in filename order is required"))
		}
	}
	path := filepath.Join(root, ".lrt", "lrtsequence.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return fail(err)
	}
	var saved lrtDeflickerSettings
	if err := json.Unmarshal(data, &saved); err != nil {
		return fail(fmt.Errorf("%s: %w", path, err))
	}
	if saved.Smoothing == nil || saved.Accuracy == nil || saved.Constant == nil || saved.Multipass == nil || saved.Passes == nil || saved.FilenameSort == nil {
		return fail(fmt.Errorf("%s: smoothing, accuracy, constant, multipass, passes and filenameSort must all be saved", path))
	}
	if saved.ImageCount > 0 && saved.ImageCount != len(frames) {
		return fail(fmt.Errorf("imageCount %d does not match %d frames", saved.ImageCount, len(frames)))
	}
	options := nativeDeflickerOptions{true, *saved.Smoothing, *saved.Accuracy}
	if err := options.validate(); err != nil {
		return fail(err)
	}
	passes := 1
	if *saved.Multipass {
		passes = *saved.Passes
		if passes < 1 || passes > 8 {
			return fail(fmt.Errorf("saved multipass count must be 1-8"))
		}
	}
	if saved.Ignore != nil {
		switch *saved.Ignore {
		case 0: // Use the imported global preference or explicit CLI histogram options.
		case 1:
			if !resolvedHistogram.ExplicitThresholds && !resolvedHistogram.FromLRTPreferences {
				return fail(fmt.Errorf("saved black/white exclusion requires --lrt-preferences or explicit --ignore-blacks/--ignore-whites thresholds"))
			}
			resolvedHistogram.Enabled = true
		case 2:
			if resolvedHistogram.Explicit {
				return fail(fmt.Errorf("saved black/white exclusion is disabled; histogram flags conflict"))
			}
			resolvedHistogram.Enabled = false
		default:
			return fail(fmt.Errorf("unsupported deflickerIgnoreBlacksWhites value %d", *saved.Ignore))
		}
	}
	order := make([]int, len(frames))
	for i := range order {
		order[i] = i
	}
	if !*saved.FilenameSort {
		order, err = readLRTReferenceDateOrder(frames)
		if err != nil {
			return fail(err)
		}
	}
	*histogram = resolvedHistogram
	return options, passes, *saved.Constant, order, nil
}
