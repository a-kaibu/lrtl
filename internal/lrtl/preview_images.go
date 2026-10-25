package lrtl

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type PreviewImageFrame struct {
	Source                  string  `json:"source"`
	File                    string  `json:"file"`
	SourceIndex             int     `json:"sourceIndex"`
	PreviewEV               float64 `json:"previewEV"`
	ExternalExposureDefault float64 `json:"externalExposureDefault"`
	Luminance               float64 `json:"luminance"`
}

type PreviewImageManifest struct {
	Version    int                 `json:"version"`
	ColorSpace string              `json:"colorSpace"`
	BitDepth   int                 `json:"bitDepth"`
	Width      int                 `json:"width"`
	Height     int                 `json:"height"`
	Frames     []PreviewImageFrame `json:"frames"`
}

type previewImageWriter struct {
	directory      string
	manifest       *PreviewImageManifest
	quality, index int
}

func (writer *previewImageWriter) AddFrame(rgb []byte) error {
	if writer.index >= len(writer.manifest.Frames) {
		return fmt.Errorf("too many preview frames")
	}
	img, err := imageFromRGB48(rgb, writer.manifest.Width, writer.manifest.Height)
	if err != nil {
		return err
	}
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, img, &jpeg.Options{Quality: writer.quality}); err != nil {
		return err
	}
	// Measure the exact decoded JPEG that the user will see. Measuring the
	// high-precision input would disagree after lossy encoding and subsampling.
	preview, _, err := decodeLRTMeteringImage(encoded.Bytes())
	if err != nil {
		return err
	}
	frame := &writer.manifest.Frames[writer.index]
	frame.PreviewEV, err = framePreviewEV(preview)
	if err != nil {
		return err
	}
	frame.ExternalExposureDefault = 4 * frame.PreviewEV
	frame.Luminance, err = frameLuminance(preview, image.Rect(0, 0, writer.manifest.Width, writer.manifest.Height))
	if err != nil {
		return err
	}
	path := filepath.Join(writer.directory, frame.File)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(encoded.Bytes())
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	writer.index++
	return nil
}

func (writer *previewImageWriter) Close() error {
	if writer.index != len(writer.manifest.Frames) {
		return fmt.Errorf("incomplete preview generation: %d of %d frames", writer.index, len(writer.manifest.Frames))
	}
	return nil
}

// GeneratePreviewImages publishes a complete, independent JPEG snapshot of the
// renderer's processed frames. Source images and sidecars are never modified.
func GeneratePreviewImages(frames []Frame, output string, options RenderOptions) (*PreviewImageManifest, error) {
	if len(frames) == 0 || output == "" {
		return nil, fmt.Errorf("preview generation requires images and an output directory")
	}
	if options.Overwrite {
		return nil, fmt.Errorf("preview generation requires a new directory")
	}
	if options.Quality < 1 || options.Quality > 100 {
		return nil, fmt.Errorf("preview JPEG quality must be 1-100")
	}
	destination, err := filepath.Abs(output)
	if err != nil {
		return nil, err
	}
	if _, err := os.Lstat(destination); err == nil {
		return nil, fmt.Errorf("output already exists: %s", destination)
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	parent := filepath.Dir(destination)
	if err := os.MkdirAll(parent, 0755); err != nil {
		return nil, err
	}
	staging, err := os.MkdirTemp(parent, ".lrtl-preview-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(staging)
	manifest := &PreviewImageManifest{Version: 1, ColorSpace: "sRGB", BitDepth: 8, Frames: make([]PreviewImageFrame, len(frames))}
	digits := max(6, len(strconv.Itoa(len(frames))))
	for i, frame := range frames {
		manifest.Frames[i] = PreviewImageFrame{Source: filepath.Base(frame.Image), SourceIndex: frame.Index, File: fmt.Sprintf("frame%0*d.jpg", digits, i+1)}
	}
	options.FPS = 24
	options.FrameRate = ""
	options.NativeFPS = false
	options.FadeOutSeconds = 0
	options.PlaybackSpeed = 1
	options.Interpolation = 0
	options.VideoRange = "auto"
	options.OutputColor = "standard"
	options.OutputSharpen = false
	options.OutputCAS = false
	options.OutputDenoise = 0
	options.RemoveAirplanes = 0
	options.OutputDeflickerLuma = false
	options.OutputDeflickerChroma = false
	options.NativeTiming = false
	options.Subsampling = ""
	if err := renderSequence(frames, staging, options, func(width, height int) (renderFrameWriter, error) {
		manifest.Width, manifest.Height = width, height
		return &previewImageWriter{directory: staging, manifest: manifest, quality: options.Quality}, nil
	}); err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(staging, "frames.json"), append(data, '\n'), 0644); err != nil {
		return nil, err
	}
	if _, err := os.Lstat(destination); err == nil {
		return nil, fmt.Errorf("output appeared during preview generation: %s", destination)
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if err := os.Rename(staging, destination); err != nil {
		return nil, fmt.Errorf("publish previews: %w", err)
	}
	return manifest, nil
}

type lrtPreviewCacheFile struct {
	path string
	data []byte
}

// writeLRTVisualPreviewCaches stores the generated JPEGs at LRT's observed
// .lrt/visual/<source-stem>.lrtpreview paths. It never replaces an existing
// cache: stale caches must be removed explicitly before writing new ones.
func writeLRTVisualPreviewCaches(frames []Frame, output string, manifest *PreviewImageManifest) error {
	files, err := planLRTVisualPreviewCaches(frames, output, manifest)
	if err != nil {
		return err
	}
	return publishNewLRTPreviewCaches(files, writeLRTPreviewCacheFile)
}

func planLRTVisualPreviewCaches(frames []Frame, output string, manifest *PreviewImageManifest) ([]lrtPreviewCacheFile, error) {
	if manifest == nil || len(frames) == 0 || len(manifest.Frames) != len(frames) {
		return nil, fmt.Errorf("preview manifest does not match source frames")
	}
	files := make([]lrtPreviewCacheFile, len(frames))
	seen := make(map[string]bool, len(frames))
	for i, frame := range frames {
		item := manifest.Frames[i]
		if item.Source != filepath.Base(frame.Image) || item.SourceIndex != frame.Index {
			return nil, fmt.Errorf("preview/source mapping differs at frame %d", i+1)
		}
		if item.File == "" || filepath.Base(item.File) != item.File || filepath.Clean(item.File) != item.File {
			return nil, fmt.Errorf("invalid preview filename for frame %d", i+1)
		}
		previewPath := filepath.Join(output, item.File)
		data, err := os.ReadFile(previewPath)
		if err != nil {
			return nil, fmt.Errorf("read generated preview %s: %w", previewPath, err)
		}
		config, format, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil || format != "jpeg" || config.Width != manifest.Width || config.Height != manifest.Height {
			return nil, fmt.Errorf("generated preview %s is not a %dx%d JPEG", previewPath, manifest.Width, manifest.Height)
		}
		cachePath := lrtPreviewPath(frame.Image, "visual")
		key := strings.ToLower(filepath.Clean(cachePath))
		if seen[key] {
			return nil, fmt.Errorf("multiple frames map to the same LRT visual cache: %s", cachePath)
		}
		seen[key] = true
		files[i] = lrtPreviewCacheFile{path: cachePath, data: data}
	}
	return files, nil
}

// publishNewLRTPreviewCaches preflights the whole set and removes all files
// successfully written by this call if a later write fails. writeFile must
// leave no file behind when it returns an error. It is injectable so the
// rollback contract can be exercised without OS-specific failures.
func publishNewLRTPreviewCaches(files []lrtPreviewCacheFile, writeFile func(string, []byte) error) error {
	if len(files) == 0 || writeFile == nil {
		return fmt.Errorf("no LRT preview cache files to publish")
	}
	if err := validateNewLRTPreviewCacheFiles(files); err != nil {
		return err
	}

	createdDirectories := make([]string, 0)
	cleanupDirectories := func() {
		for i := len(createdDirectories) - 1; i >= 0; i-- {
			_ = os.Remove(createdDirectories[i])
		}
	}
	for _, file := range files {
		created, err := ensureLRTCacheDirectory(filepath.Dir(file.path))
		createdDirectories = append(createdDirectories, created...)
		if err != nil {
			cleanupDirectories()
			return err
		}
	}

	written := make([]string, 0, len(files))
	for _, file := range files {
		if err := writeFile(file.path, file.data); err != nil {
			for i := len(written) - 1; i >= 0; i-- {
				_ = os.Remove(written[i])
			}
			cleanupDirectories()
			return fmt.Errorf("write LRT preview cache %s: %w", file.path, err)
		}
		written = append(written, file.path)
	}
	return nil
}

func validateNewLRTPreviewCacheFiles(files []lrtPreviewCacheFile) error {
	if len(files) == 0 {
		return fmt.Errorf("no LRT preview cache files to publish")
	}
	seen := make(map[string]bool, len(files))
	for _, file := range files {
		key := strings.ToLower(filepath.Clean(file.path))
		if seen[key] {
			return fmt.Errorf("duplicate LRT preview cache path: %s", file.path)
		}
		seen[key] = true
		if _, err := os.Lstat(file.path); err == nil {
			return fmt.Errorf("LRT preview cache already exists: %s", file.path)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("check LRT preview cache %s: %w", file.path, err)
		}
	}
	return nil
}

func ensureLRTCacheDirectory(directory string) ([]string, error) {
	var missing []string
	for current := directory; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err == nil {
			if !info.IsDir() {
				return nil, fmt.Errorf("LRT cache parent is not a directory: %s", current)
			}
			break
		}
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("inspect LRT cache directory %s: %w", current, err)
		}
		missing = append(missing, current)
		parent := filepath.Dir(current)
		if parent == current {
			return nil, fmt.Errorf("could not find an existing parent for LRT cache directory %s", directory)
		}
	}
	created := make([]string, 0, len(missing))
	for i := len(missing) - 1; i >= 0; i-- {
		if err := os.Mkdir(missing[i], 0755); err != nil {
			if os.IsExist(err) {
				info, statErr := os.Lstat(missing[i])
				if statErr == nil && info.IsDir() {
					continue
				}
			}
			for j := len(created) - 1; j >= 0; j-- {
				_ = os.Remove(created[j])
			}
			return nil, fmt.Errorf("create LRT cache directory %s: %w", missing[i], err)
		}
		created = append(created, missing[i])
	}
	return created, nil
}

func writeLRTPreviewCacheFile(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	n, writeErr := file.Write(data)
	if writeErr == nil && n != len(data) {
		writeErr = io.ErrShortWrite
	}
	syncErr := file.Sync()
	closeErr := file.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		_ = os.Remove(path)
		return errors.Join(writeErr, syncErr, closeErr)
	}
	return nil
}
