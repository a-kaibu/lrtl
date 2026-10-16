package lrtl

import (
	"bytes"
	"errors"
	"image"
	"os"
	"path/filepath"
	"testing"
)

func TestFrameInputCacheBoundAndCleanup(t *testing.T) {
	cache := newFrameInputCache(imageInputOptions{RawDevelop: true}, true)
	defer cache.close()
	data := []byte("decoded raw pixels")
	config := image.Config{Width: 12, Height: 8}
	calls := map[string]int{}
	cache.loader = func(frame Frame, _ imageInputOptions) ([]byte, image.Config, string, error) {
		calls[frame.Image]++
		return data, config, "tiff", nil
	}
	cache.limit = int64(len(data))
	for pass := 0; pass < 2; pass++ {
		for i, name := range []string{"first.nef", "second.nef"} {
			got, cfg, format, err := cache.read(i, Frame{Image: name})
			if err != nil || !bytes.Equal(got, data) || cfg != config || format != "tiff" {
				t.Fatalf("cache read failed: %v", err)
			}
		}
	}
	if calls["first.nef"] != 1 || calls["second.nef"] != 2 || cache.used > cache.limit {
		t.Fatalf("cache limit/reuse: calls=%v used=%d", calls, cache.used)
	}
	dir := cache.directory
	if dir == "" {
		t.Fatal("no cache created")
	}
	cache.close()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("cache survived cleanup: %v", err)
	}
}

func TestFrameInputCacheRecoversAndPropagatesLoaderErrors(t *testing.T) {
	cache := newFrameInputCache(imageInputOptions{RawPreview: true}, true)
	defer cache.close()
	calls := 0
	sentinel := errors.New("RAW decoder failure")
	cache.loader = func(Frame, imageInputOptions) ([]byte, image.Config, string, error) {
		calls++
		if calls == 3 {
			return nil, image.Config{}, "", sentinel
		}
		return []byte("pixels"), image.Config{}, "jpeg", nil
	}
	frame := Frame{Image: "source.nef"}
	if _, _, _, err := cache.read(0, frame); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(cache.entries[0].path); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := cache.read(0, frame); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || cache.used != int64(len("pixels")) {
		t.Fatalf("reload bookkeeping: calls=%d used=%d", calls, cache.used)
	}
	if _, _, _, err := cache.read(1, frame); !errors.Is(err, sentinel) {
		t.Fatalf("loader error lost: %v", err)
	}
	dir := cache.directory
	cache.close()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("cache survived loader error cleanup: %v", err)
	}
}

func TestFrameInputCacheWriteFailureIsOptional(t *testing.T) {
	cache := newFrameInputCache(imageInputOptions{RawDevelop: true}, true)
	defer cache.close()
	cache.directory = filepath.Join(t.TempDir(), "missing", "cache")
	cache.loader = func(Frame, imageInputOptions) ([]byte, image.Config, string, error) {
		return []byte("pixels"), image.Config{}, "tiff", nil
	}
	data, _, _, err := cache.read(0, Frame{Image: "source.nef"})
	if err != nil || string(data) != "pixels" || cache.limit != 0 {
		t.Fatalf("optional cache caused source failure: %q %v limit=%d", data, err, cache.limit)
	}
}

func TestFrameInputCacheDoesNotCacheRasterOrDisabledInput(t *testing.T) {
	for _, c := range []struct {
		name    string
		enabled bool
	}{{"source.png", true}, {"source.nef", false}} {
		cache := newFrameInputCache(imageInputOptions{RawDevelop: true}, c.enabled)
		calls := 0
		cache.loader = func(Frame, imageInputOptions) ([]byte, image.Config, string, error) {
			calls++
			return []byte("pixels"), image.Config{}, "png", nil
		}
		for i := 0; i < 2; i++ {
			if _, _, _, err := cache.read(0, Frame{Image: c.name}); err != nil {
				t.Fatal(err)
			}
		}
		if calls != 2 || cache.directory != "" {
			t.Fatalf("unexpected cache: %+v", c)
		}
		cache.close()
	}
}
