package lrtl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"strconv"
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
	preview, err := jpeg.Decode(bytes.NewReader(encoded.Bytes()))
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
