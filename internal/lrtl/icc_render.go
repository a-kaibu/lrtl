package lrtl

import (
	"fmt"
	"strings"
)

// Only native video output interprets ICC labels. Other render paths retain
// their existing sRGB interpretation. The first selected source is authoritative,
// matching the native intermediary sequence reader.
func resolveRenderICC(frames []Frame, options RenderOptions, nativeVideo bool) (RenderOptions, error) {
	input := strings.ToLower(strings.TrimSpace(options.InputColor))
	// The CLI resolves options before calling RenderSequence, which performs its
	// own resolution. Preserve an already-resolved explicit transform so this
	// step is idempotent.
	if options.inputProfile != "" {
		if namedRenderInputProfile(options.inputProfile) == "" {
			return options, fmt.Errorf("invalid resolved input profile %q", options.inputProfile)
		}
		options.InputColor = "bt709"
		return options, nil
	}
	if profile := namedRenderInputProfile(input); profile != "" {
		options.inputProfile = profile
		options.InputColor = "bt709"
		return options, nil
	}
	if input != "" && input != "auto" {
		return options, nil
	}
	options.InputColor = "bt709"
	if !nativeVideo || options.RawDevelop || options.RawPreview || options.LRTPreview != "" || len(frames) == 0 {
		return options, nil
	}
	description, err := readImageICCDescription(frames[0].Image)
	if err != nil {
		return options, err
	}
	if strings.Contains(description, "2020") {
		options.InputColor = "bt2020"
	}
	if options.DetectDynamicRange && strings.HasSuffix(description, "PQ") {
		options.DynamicRange = "pq"
	}
	return options, nil
}
