package lrtl

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func metadataInputFrames(path string) ([]Frame, bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, false, err
	}
	if info.IsDir() {
		frames, err := DiscoverSequence(path)
		if err == nil && len(frames) == 0 {
			err = fmt.Errorf("sequence contains no images")
		}
		return frames, true, err
	}
	image, sidecar := inputPaths(path)
	if strings.EqualFold(filepath.Ext(path), ".xmp") {
		return []Frame{{Image: image, Sidecar: sidecar}}, false, nil
	}
	if !imageExtensions[strings.ToLower(filepath.Ext(path))] {
		return nil, false, fmt.Errorf("unsupported image extension: %s", path)
	}
	return []Frame{{Image: image, Sidecar: sidecar}}, false, nil
}
