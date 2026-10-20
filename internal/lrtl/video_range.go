package lrtl

import (
	"fmt"
	"strings"
)

func normalizeVideoRange(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		value = "auto"
	}
	switch value {
	case "auto", "limited", "full":
		return value, nil
	}
	return "", fmt.Errorf("video range must be auto, limited or full: %s", value)
}

func videoRangeFilter(value, pixelFormat string) string {
	const padding = "pad=ceil(iw/2)*2:ceil(ih/2)*2"
	if value == "auto" {
		return padding
	}
	// Convert the known RGB48 input directly at 16-bit precision. The explicit
	// matrix and range also avoid swscale's endpoint overshoot for RGB16 input.
	return "format=gbrp16le," + padding + ",zscale=matrixin=gbr:matrix=709:rangein=full:range=" + value + ",format=" + pixelFormat
}
