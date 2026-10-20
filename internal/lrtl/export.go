package lrtl

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

type exportedFrame struct {
	Source string `json:"source"`
	File   string `json:"file"`
}

type tiffSequenceWriter struct {
	directory            string
	width, height, index int
	frames               []exportedFrame
}

func (writer *tiffSequenceWriter) AddFrame(rgb []byte) error {
	if writer.index >= len(writer.frames) {
		return fmt.Errorf("too many exported frames")
	}
	img, err := imageFromRGB48(rgb, writer.width, writer.height)
	if err != nil {
		return err
	}
	data, err := encodeDevelopedDNGTIFF(img, 1)
	if err != nil {
		return err
	}
	path := filepath.Join(writer.directory, writer.frames[writer.index].File)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(data)
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

func (writer *tiffSequenceWriter) Close() error {
	if writer.index != len(writer.frames) {
		return fmt.Errorf("incomplete TIFF export: %d of %d frames", writer.index, len(writer.frames))
	}
	return nil
}

// ExportTIFFSequence writes uncompressed RGB16 sRGB intermediate frames using
// the video renderer's image processing. A complete new directory is published
// after all frames succeed; existing output directories are never replaced.
func ExportTIFFSequence(frames []Frame, output string, options RenderOptions) error {
	if len(frames) == 0 {
		return fmt.Errorf("sequence contains no images")
	}
	if output == "" {
		return fmt.Errorf("output directory is required")
	}
	if options.Overwrite {
		return fmt.Errorf("TIFF export requires a new output directory; overwrite is not supported")
	}
	destination, err := filepath.Abs(output)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(destination); err == nil {
		return fmt.Errorf("output already exists: %s", destination)
	} else if !os.IsNotExist(err) {
		return err
	}
	parent := filepath.Dir(destination)
	if err := os.MkdirAll(parent, 0755); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(parent, ".lrtl-export-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	manifest := struct {
		Version    int             `json:"version"`
		ColorSpace string          `json:"colorSpace"`
		BitDepth   int             `json:"bitDepth"`
		Frames     []exportedFrame `json:"frames"`
	}{Version: 1, ColorSpace: "sRGB", BitDepth: 16, Frames: make([]exportedFrame, len(frames))}
	digits := max(6, len(strconv.Itoa(len(frames))))
	for i, frame := range frames {
		manifest.Frames[i] = exportedFrame{Source: filepath.Base(frame.Image), File: fmt.Sprintf("frame%0*d.tif", digits, i+1)}
	}
	// These video-only values do not affect the image processing, but the shared
	// renderer validates them before starting any destination.
	options.FPS, options.Quality = 24, 90
	options.FrameRate = ""
	options.FadeOutSeconds = 0
	options.PlaybackSpeed = 1
	options.VideoRange = "auto"
	options.OutputSharpen = false
	options.OutputCAS = false
	options.OutputDenoise = 0
	options.RemoveAirplanes = 0
	options.NativeTiming = false
	options.Subsampling = ""
	if err := renderSequence(frames, staging, options, func(width, height int) (renderFrameWriter, error) {
		return &tiffSequenceWriter{directory: staging, width: width, height: height, frames: manifest.Frames}, nil
	}); err != nil {
		return err
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(staging, "frames.json"), append(data, '\n'), 0644); err != nil {
		return err
	}
	if _, err := os.Lstat(destination); err == nil {
		return fmt.Errorf("output appeared during export: %s", destination)
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(staging, destination); err != nil {
		return fmt.Errorf("publish TIFF sequence: %w", err)
	}
	return nil
}
