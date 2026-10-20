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

func normalizeOutputColor(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		value = "standard"
	}
	if value != "standard" && value != "lrt-7.5.5" {
		return "", fmt.Errorf("output-color must be standard or lrt-7.5.5")
	}
	return value, nil
}

func nativeSDRColorFilter(value, pixelFormat string) string {
	// Match the observed final SDR encoder path, including swscale's initial
	// RGB conversion. This does not replace the Go image filters' color model.
	return "pad=ceil(iw/2)*2:ceil(ih/2)*2,scale=iw:ih:out_range=full:flags=lanczos,format=yuv444p10le," + nativeSDRPlanarColorFilter(value, pixelFormat)
}

func nativeSDRPlanarColorFilter(value, pixelFormat string) string {
	if value == "auto" {
		value = "limited"
	}
	return "setparams=color_primaries=bt709:color_trc=bt709:colorspace=bt709,setsar=1/1,zscale=rangein=full:range=" + value + ":matrixin=709:matrix=709:primariesin=709:primaries=709:transferin=709:transfer=709,format=" + pixelFormat
}
