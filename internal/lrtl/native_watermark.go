package lrtl

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// nativeWatermarkFilter reproduces the measured helper, including NBSP
// substitutions and drawtext's native expression/escaping behavior.
func nativeWatermarkFilter(text string, width int, font string) string {
	text = strings.Map(func(r rune) rune {
		switch r {
		case ' ', ';', ':', '"', '\'':
			return '\u00a0'
		}
		return r
	}, text)
	size := 400
	for _, step := range []struct{ limit, size int }{{1280, 60}, {1920, 90}, {3072, 150}, {4096, 200}, {5120, 250}, {7680, 320}} {
		if width <= step.limit {
			size = step.size
			break
		}
	}
	return ",drawtext=" + font + ":fontsize=" + strconv.Itoa(size) + ":fontcolor=0xf0f0f040:shadowcolor=0x00000020:shadowx=-2:shadowy=-2:x=(w-text_w)/2:y=h/4*2.5-max_glyph_h:text='" + text + "'"
}

func nativeWatermarkFont() (string, error) {
	paths := []string{"/Library/Fonts/Arial.ttf", "/System/Library/Fonts/Supplemental/Arial.ttf"}
	if runtime.GOOS == "windows" {
		root := os.Getenv("WINDIR")
		if root == "" {
			root = "C:\\Windows"
		}
		paths = []string{filepath.Join(root, "Fonts", "arial.ttf")}
	}
	for _, path := range paths {
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			path, err = filepath.Abs(path)
			if err != nil {
				return "", err
			}
			path = filepath.ToSlash(path)
			// Keep a drive colon escaped through filtergraph tokenization.
			path = strings.ReplaceAll(path, ":", "\\:")
			path = strings.ReplaceAll(path, "'", "'\\''")
			return "fontfile='" + path + "'", nil
		}
	}
	return "", fmt.Errorf("native watermark requires the system Arial.ttf font")
}
