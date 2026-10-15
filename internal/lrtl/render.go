package lrtl

import (
	"bytes"
	"errors"
	"fmt"
	_ "golang.org/x/image/tiff"
	"image"
	"image/jpeg"
	_ "image/png"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/icza/mjpeg"
)

type RenderOptions struct {
	FPS              int
	Quality          int
	Overwrite        bool
	Deflicker        bool
	Reference        image.Rectangle
	SmoothRadius     int
	DeflickerPasses  int
	Constant         bool
	ApplyXMPExposure bool
	ApplyXMPCrop     bool
	RawPreview       bool
}

func supportedRasterExtension(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg", ".png", ".tif", ".tiff":
		return true
	default:
		return false
	}
}

func supportedRasterFormat(format string) bool {
	return format == "jpeg" || format == "png" || format == "tiff"
}

const maxAVIDimension = int64(1<<31 - 1)

// RenderSequence writes JPEG, PNG, TIFF, or explicitly selected embedded RAW previews
// to an MJPEG AVI file. It can optionally apply the global XMP Exposure2012 slider and
// axis-aligned XMP crop bounds, but does not implement full Camera Raw RAW rendering.
func RenderSequence(frames []Frame, output string, options RenderOptions) error {
	if len(frames) == 0 {
		return errors.New("sequence contains no images")
	}
	if options.FPS < 1 || options.FPS > 240 {
		return fmt.Errorf("fps must be between 1 and 240: %d", options.FPS)
	}
	if options.Quality < 1 || options.Quality > 100 {
		return fmt.Errorf("JPEG quality must be between 1 and 100: %d", options.Quality)
	}
	if strings.ToLower(filepath.Ext(output)) != ".avi" {
		return fmt.Errorf("output must use the .avi extension: %s", output)
	}

	outputDir := filepath.Dir(output)
	if info, err := os.Stat(outputDir); err != nil {
		return fmt.Errorf("output directory: %w", err)
	} else if !info.IsDir() {
		return fmt.Errorf("output parent is not a directory: %s", outputDir)
	}
	if !options.Overwrite {
		if _, err := os.Lstat(output); err == nil {
			return fmt.Errorf("output already exists (pass --overwrite to replace it): %s", output)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("check output: %w", err)
		}
	}

	for _, frame := range frames {
		if !supportedRasterExtension(frame.Image) && !supportsEmbeddedRawPreview(frame.Image) {
			return fmt.Errorf("render does not support this image type: %s", filepath.Base(frame.Image))
		}
		if supportsEmbeddedRawPreview(frame.Image) && !options.RawPreview {
			return fmt.Errorf("RAW input %s requires --raw-preview; this uses the camera's embedded preview, not RAW development", filepath.Base(frame.Image))
		}
	}

	var xmpExposures []float64
	if options.ApplyXMPExposure {
		xmpExposures = make([]float64, len(frames))
		for index, frame := range frames {
			exposure, err := readXMPExposure(frame)
			if err != nil {
				return fmt.Errorf("%s: %w", filepath.Base(frame.Image), err)
			}
			xmpExposures[index] = exposure
		}
	}

	first, config, firstFormat, err := readRenderInput(frames[0], options.RawPreview)
	if err != nil {
		return err
	}
	if config.Width <= 0 || config.Height <= 0 {
		return fmt.Errorf("invalid dimensions in %s: %dx%d", frames[0].Image, config.Width, config.Height)
	}
	if int64(config.Width) > maxAVIDimension || int64(config.Height) > maxAVIDimension {
		return fmt.Errorf("image dimensions exceed AVI limits in %s", frames[0].Image)
	}
	width, height := config.Width, config.Height
	var xmpCrops []cropSpec
	if options.ApplyXMPCrop {
		xmpCrops, err = readXMPCrops(frames, width, height)
		if err != nil {
			return err
		}
	}

	var gains []float64
	if options.Deflicker {
		if !options.Reference.Empty() {
			bounds := image.Rect(0, 0, width, height)
			if bounds.Intersect(options.Reference) != options.Reference {
				return fmt.Errorf("reference area %v is outside frame dimensions %dx%d", options.Reference, width, height)
			}
		}
		luminances := make([]float64, len(frames))
		for index, frame := range frames {
			data, frameConfig, format := first, config, firstFormat
			if index > 0 {
				data, frameConfig, format, err = readRenderInput(frame, options.RawPreview)
				if err != nil {
					return err
				}
			}
			img, format, decodeErr := image.Decode(bytes.NewReader(data))
			if decodeErr != nil {
				return fmt.Errorf("decode %s: %w", frame.Image, decodeErr)
			}
			if !supportedRasterFormat(format) {
				return fmt.Errorf("render supports JPEG, PNG, and TIFF data only: %s", frame.Image)
			}
			if frameConfig.Width != width || frameConfig.Height != height || img.Bounds().Dx() != width || img.Bounds().Dy() != height {
				return fmt.Errorf("inconsistent dimensions in %s", frame.Image)
			}
			if xmpCrops != nil {
				img = applyXMPCrop(img, xmpCrops[index])
			}
			if xmpExposures != nil && xmpExposures[index] != 0 {
				img = applyExposureGain(img, math.Exp2(xmpExposures[index]))
			}
			reference := options.Reference
			if reference.Empty() {
				reference = img.Bounds()
			} else {
				reference = reference.Add(img.Bounds().Min)
			}
			luminances[index], err = frameLuminance(img, reference)
			if err != nil {
				return fmt.Errorf("analyze %s: %w", frame.Image, err)
			}
		}
		gains, err = deflickerGains(luminances, options.SmoothRadius, options.DeflickerPasses, options.Constant)
		if err != nil {
			return err
		}
	}

	tempFile, err := os.CreateTemp(outputDir, ".lrtl-*.avi")
	if err != nil {
		return fmt.Errorf("create temporary AVI: %w", err)
	}
	tempPath := tempFile.Name()
	if err := tempFile.Close(); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("close temporary AVI: %w", err)
	}
	if err := os.Remove(tempPath); err != nil {
		return fmt.Errorf("prepare temporary AVI: %w", err)
	}
	indexPath := tempPath + ".idx_"
	cleanup := func() {
		_ = os.Remove(tempPath)
		_ = os.Remove(indexPath)
	}

	writer, err := mjpeg.New(tempPath, int32(config.Width), int32(config.Height), int32(options.FPS))
	if err != nil {
		cleanup()
		return fmt.Errorf("create MJPEG AVI: %w", err)
	}
	writerClosed := false
	completed := false
	defer func() {
		if !writerClosed {
			_ = writer.Close()
		}
		if !completed {
			cleanup()
		}
	}()

	for index, frame := range frames {
		data, config, format := first, config, firstFormat
		if index > 0 {
			data, config, format, err = readRenderInput(frame, options.RawPreview)
			if err != nil {
				return err
			}
		}
		if config.Width != width || config.Height != height {
			return fmt.Errorf("inconsistent dimensions in %s", frame.Image)
		}

		applyXMP := xmpExposures != nil && xmpExposures[index] != 0
		applyCrop := xmpCrops != nil && xmpCrops[index].Active && xmpCrops[index].Bounds != image.Rect(0, 0, width, height)
		jpegData := data
		if options.Deflicker || applyXMP || applyCrop || format != "jpeg" {
			img, _, err := image.Decode(bytes.NewReader(data))
			if err != nil {
				return fmt.Errorf("decode %s: %w", frame.Image, err)
			}
			if applyCrop {
				img = applyXMPCrop(img, xmpCrops[index])
			}
			var encoded bytes.Buffer
			gain := 1.0
			if applyXMP {
				gain *= math.Exp2(xmpExposures[index])
			}
			if options.Deflicker {
				gain *= gains[index]
			}
			if applyXMP || options.Deflicker {
				img = applyExposureGain(img, gain)
			}
			if err := jpeg.Encode(&encoded, img, &jpeg.Options{Quality: options.Quality}); err != nil {
				return fmt.Errorf("encode %s as JPEG: %w", frame.Image, err)
			}
			jpegData = encoded.Bytes()
		}
		if err := writer.AddFrame(jpegData); err != nil {
			return fmt.Errorf("add frame %s: %w", frame.Image, err)
		}
	}
	if err := writer.Close(); err != nil {
		writerClosed = true
		return fmt.Errorf("finalize AVI: %w", err)
	}
	writerClosed = true

	if !options.Overwrite {
		if _, err := os.Lstat(output); err == nil {
			return fmt.Errorf("output appeared while rendering (pass --overwrite to replace it): %s", output)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("check output: %w", err)
		}
	}
	if err := os.Rename(tempPath, output); err != nil {
		return fmt.Errorf("save AVI to %s: %w", output, err)
	}
	completed = true
	return nil
}
