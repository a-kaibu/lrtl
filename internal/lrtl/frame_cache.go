package lrtl

import (
	"fmt"
	"image"
	"os"
	"path/filepath"
)

const deflickerRawCacheLimit int64 = 512 << 20

type cachedFrameInput struct {
	path   string
	config image.Config
	format string
	size   int64
}
type frameInputCache struct {
	options     imageInputOptions
	limit, used int64
	directory   string
	entries     map[int]cachedFrameInput
	paths       map[string]bool
	loader      func(Frame, imageInputOptions) ([]byte, image.Config, string, error)
}

func newFrameInputCache(options imageInputOptions, enabled bool) *frameInputCache {
	limit := int64(0)
	if enabled {
		limit = deflickerRawCacheLimit
	}
	return &frameInputCache{options: options, limit: limit, entries: make(map[int]cachedFrameInput), paths: make(map[string]bool), loader: readRenderInputWithOptions}
}

func (cache *frameInputCache) read(index int, frame Frame) ([]byte, image.Config, string, error) {
	if entry, found := cache.entries[index]; found {
		if data, err := os.ReadFile(entry.path); err == nil {
			return data, entry.config, entry.format, nil
		}
		delete(cache.entries, index)
		cache.used -= entry.size
	}
	data, config, format, err := cache.loader(frame, cache.options)
	if err != nil {
		return nil, config, format, err
	}
	if cache.limit <= 0 || supportedRasterExtension(frame.Image) || !(cache.options.RawDevelop || cache.options.RawPreview) || int64(len(data)) > cache.limit-cache.used {
		return data, config, format, nil
	}
	if cache.directory == "" {
		cache.directory, err = os.MkdirTemp("", "lrtl-frame-cache-")
		if err != nil {
			cache.limit = 0
			return data, config, format, nil
		}
	}
	path := filepath.Join(cache.directory, fmt.Sprintf("%08d.frame", index))
	cache.paths[path] = true
	if err := os.WriteFile(path, data, 0600); err != nil {
		_ = os.Remove(path)
		cache.limit = 0
		return data, config, format, nil
	}
	cache.entries[index] = cachedFrameInput{path, config, format, int64(len(data))}
	cache.used += int64(len(data))
	return data, config, format, nil
}

func (cache *frameInputCache) close() {
	// Only our recorded files and our empty private directory are removed.
	for path := range cache.paths {
		_ = os.Remove(path)
	}
	if cache.directory != "" {
		_ = os.Remove(cache.directory)
	}
}
