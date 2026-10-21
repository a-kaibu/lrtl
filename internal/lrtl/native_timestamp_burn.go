package lrtl

import (
	"fmt"
	"path/filepath"
	"strconv"
)

type nativeTimestampBurn struct {
	file  string
	start int
}

func (burn nativeTimestampBurn) filter() (string, error) {
	if burn.file == "" {
		return "", nil
	}
	name := filepath.Base(burn.file)
	for _, c := range name {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-') {
			return "", fmt.Errorf("unsafe native subtitle basename")
		}
	}
	if burn.start < 0 {
		return "", fmt.Errorf("native subtitle offset must be nonnegative")
	}
	// The encoder works in the temporary subtitle directory. Its generated
	// ASCII basename needs no platform-dependent filtergraph path escaping.
	graph := ""
	if burn.start > 0 {
		graph += "setpts=PTS+" + strconv.Itoa(burn.start) + ","
	}
	graph += "subtitles=filename='" + name + "':force_style='FontName=Arial.ttf,Fontsize=16,PrimaryColour=&He0e0e0'"
	if burn.start > 0 {
		graph += ",setpts=PTS-STARTPTS"
	}
	return graph, nil
}

func nativeEncoderFadeFilter(fade videoFade) string {
	if fade.duration <= 0 {
		return ""
	}
	return ",fade=t=out:st=" + strconv.FormatFloat(fade.start, 'f', 3, 64) + ":d=" + strconv.FormatFloat(fade.duration, 'f', -1, 32)
}
