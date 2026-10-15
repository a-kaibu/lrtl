package lrtl

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	_ "image/png"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/icza/mjpeg"
	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/tiff"
)

type RenderOptions struct {
	FPS              int
	Quality          int
	FFmpegPath       string
	CRF              int
	Preset           string
	LongEdge         int
	Overwrite        bool
	Deflicker        bool
	Reference        image.Rectangle
	SmoothRadius     int
	DeflickerPasses  int
	Constant         bool
	ApplyXMPExposure bool
	ApplyXMPCrop     bool
	RawPreview       bool
	RawDevelop       bool
	DNGLabPath       string
}

type renderFrameWriter interface {
	AddFrame([]byte) error
	Close() error
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

func imageRGB24(img image.Image) ([]byte, error) {
	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	maxInt := uint64(^uint(0) >> 1)
	pixels := uint64(width) * uint64(height)
	if width <= 0 || height <= 0 || pixels > maxInt/3 {
		return nil, fmt.Errorf("frame dimensions are too large for RGB24 encoding: %dx%d", width, height)
	}
	rgb := make([]byte, int(pixels*3))
	switch source := img.(type) {
	case *image.YCbCr:
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				sample := source.YCbCrAt(bounds.Min.X+x, bounds.Min.Y+y)
				r, g, b := color.YCbCrToRGB(sample.Y, sample.Cb, sample.Cr)
				index := (y*width + x) * 3
				rgb[index], rgb[index+1], rgb[index+2] = r, g, b
			}
		}
		return rgb, nil
	case *image.RGBA:
		sourceOffset := source.PixOffset(bounds.Min.X, bounds.Min.Y)
		for y := 0; y < height; y++ {
			row := sourceOffset + y*source.Stride
			for x := 0; x < width; x++ {
				src, dst := row+x*4, (y*width+x)*3
				rgb[dst], rgb[dst+1], rgb[dst+2] = source.Pix[src], source.Pix[src+1], source.Pix[src+2]
			}
		}
		return rgb, nil
	case *image.NRGBA:
		sourceOffset := source.PixOffset(bounds.Min.X, bounds.Min.Y)
		for y := 0; y < height; y++ {
			row := sourceOffset + y*source.Stride
			for x := 0; x < width; x++ {
				src, dst := row+x*4, (y*width+x)*3
				if source.Pix[src+3] == 255 {
					rgb[dst], rgb[dst+1], rgb[dst+2] = source.Pix[src], source.Pix[src+1], source.Pix[src+2]
				} else {
					r, g, b, _ := source.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
					rgb[dst], rgb[dst+1], rgb[dst+2] = uint8(r>>8), uint8(g>>8), uint8(b>>8)
				}
			}
		}
		return rgb, nil
	case *image.Gray:
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				value := source.GrayAt(bounds.Min.X+x, bounds.Min.Y+y).Y
				index := (y*width + x) * 3
				rgb[index], rgb[index+1], rgb[index+2] = value, value, value
			}
		}
		return rgb, nil
	}
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			r, g, b, _ := img.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
			index := (y*width + x) * 3
			rgb[index], rgb[index+1], rgb[index+2] = uint8(r>>8), uint8(g>>8), uint8(b>>8)
		}
	}
	return rgb, nil
}

func validateLongEdge(longEdge int) error {
	if longEdge < 0 || longEdge > 32768 || (longEdge > 0 && longEdge < 16) {
		return fmt.Errorf("long edge must be 0 or between 16 and 32768 pixels: %d", longEdge)
	}
	return nil
}

func outputDimensions(width, height, longEdge int) (int, int, error) {
	if err := validateLongEdge(longEdge); err != nil {
		return 0, 0, err
	}
	if longEdge == 0 || max(width, height) <= longEdge {
		return width, height, nil
	}
	if width >= height {
		return longEdge, max(1, int(math.Round(float64(height)*float64(longEdge)/float64(width)))), nil
	}
	return max(1, int(math.Round(float64(width)*float64(longEdge)/float64(height)))), longEdge, nil
}

func resizeFrame(img image.Image, width, height int) image.Image {
	if img.Bounds().Dx() == width && img.Bounds().Dy() == height {
		return img
	}
	resized := image.NewNRGBA(image.Rect(0, 0, width, height))
	xdraw.CatmullRom.Scale(resized, resized.Bounds(), img, img.Bounds(), xdraw.Src, nil)
	return resized
}

const maxVideoDimension = int64(1<<31 - 1)

// RenderSequence writes JPEG, PNG, TIFF, selected embedded RAW previews, or DNGLab-developed
// RAW frames to MJPEG AVI or H.264 MP4. It can optionally apply the global XMP Exposure2012
// slider and rotated XMP crop bounds, but does not implement full Camera Raw RAW rendering.
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
	if err := validateLongEdge(options.LongEdge); err != nil {
		return err
	}
	inputOptions := imageInputOptions{RawPreview: options.RawPreview, RawDevelop: options.RawDevelop, DNGLabPath: options.DNGLabPath}
	if err := validateImageInputOptions(inputOptions); err != nil {
		return err
	}
	outputExtension := strings.ToLower(filepath.Ext(output))
	if outputExtension != ".avi" && outputExtension != ".mp4" {
		return fmt.Errorf("output must use the .avi or .mp4 extension: %s", output)
	}
	ffmpegPath := ""
	if outputExtension == ".mp4" {
		if options.CRF < 0 || options.CRF > 51 {
			return fmt.Errorf("H.264 CRF must be between 0 and 51: %d", options.CRF)
		}
		if options.Preset == "" {
			options.Preset = "medium"
		}
		if !validH264Preset(options.Preset) {
			return fmt.Errorf("unsupported H.264 preset %q", options.Preset)
		}
		resolvedPath, err := resolveFFmpeg(options.FFmpegPath)
		if err != nil {
			return err
		}
		ffmpegPath = resolvedPath
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
		if supportedRasterExtension(frame.Image) {
			continue
		}
		if !supportsRawDevelopment(frame.Image) {
			return fmt.Errorf("render does not support this image type: %s", filepath.Base(frame.Image))
		}
		if options.RawDevelop {
			continue
		}
		if options.RawPreview && supportsEmbeddedRawPreview(frame.Image) {
			continue
		}
		if options.RawPreview {
			return fmt.Errorf("RAW input %s has no embedded-preview reader; use --raw-develop with DNGLab", filepath.Base(frame.Image))
		}
		return fmt.Errorf("RAW input %s requires --raw-preview or --raw-develop with DNGLab", filepath.Base(frame.Image))
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

	first, config, firstFormat, err := readRenderInputWithOptions(frames[0], inputOptions)
	if err != nil {
		return err
	}
	if config.Width <= 0 || config.Height <= 0 {
		return fmt.Errorf("invalid dimensions in %s: %dx%d", frames[0].Image, config.Width, config.Height)
	}
	if int64(config.Width) > maxVideoDimension || int64(config.Height) > maxVideoDimension {
		return fmt.Errorf("image dimensions exceed supported limits in %s", frames[0].Image)
	}
	width, height := config.Width, config.Height
	outputWidth, outputHeight, err := outputDimensions(width, height, options.LongEdge)
	if err != nil {
		return err
	}
	resizeFrames := outputWidth != width || outputHeight != height
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
				data, frameConfig, format, err = readRenderInputWithOptions(frame, inputOptions)
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

	tempExtension := ".avi"
	if outputExtension == ".mp4" {
		tempExtension = ".mp4"
	}
	tempFile, err := os.CreateTemp(outputDir, ".lrtl-*"+tempExtension)
	if err != nil {
		return fmt.Errorf("create temporary video: %w", err)
	}
	tempPath := tempFile.Name()
	if err := tempFile.Close(); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("close temporary video: %w", err)
	}
	if err := os.Remove(tempPath); err != nil {
		return fmt.Errorf("prepare temporary video: %w", err)
	}
	indexPath := ""
	if outputExtension == ".avi" {
		indexPath = tempPath + ".idx_"
	}
	cleanup := func() {
		_ = os.Remove(tempPath)
		if indexPath != "" {
			_ = os.Remove(indexPath)
		}
	}

	var writer renderFrameWriter
	if outputExtension == ".avi" {
		writer, err = mjpeg.New(tempPath, int32(outputWidth), int32(outputHeight), int32(options.FPS))
		if err != nil {
			cleanup()
			return fmt.Errorf("create MJPEG AVI: %w", err)
		}
	} else {
		writer, err = newFFmpegFrameWriter(ffmpegPath, tempPath, outputWidth, outputHeight, options.FPS, len(frames), options.CRF, options.Preset)
		if err != nil {
			cleanup()
			return err
		}
	}
	writerClosed := false
	completed := false
	defer func() {
		if !writerClosed {
			if aborter, ok := writer.(interface{ Abort() }); ok {
				aborter.Abort()
			} else {
				_ = writer.Close()
			}
		}
		if !completed {
			cleanup()
		}
	}()

	for index, frame := range frames {
		data, config, format := first, config, firstFormat
		if index > 0 {
			data, config, format, err = readRenderInputWithOptions(frame, inputOptions)
			if err != nil {
				return err
			}
		}
		if config.Width != width || config.Height != height {
			return fmt.Errorf("inconsistent dimensions in %s", frame.Image)
		}

		applyXMP := xmpExposures != nil && xmpExposures[index] != 0
		applyCrop := xmpCrops != nil && xmpCrops[index].Active && (xmpCrops[index].Bounds != image.Rect(0, 0, width, height) || math.Abs(xmpCrops[index].Angle) > 1e-9)
		if outputExtension == ".mp4" {
			img, format, err := image.Decode(bytes.NewReader(data))
			if err != nil {
				return fmt.Errorf("decode %s: %w", frame.Image, err)
			}
			if !supportedRasterFormat(format) {
				return fmt.Errorf("render supports JPEG, PNG, and TIFF data only: %s", frame.Image)
			}
			if img.Bounds().Dx() != width || img.Bounds().Dy() != height {
				return fmt.Errorf("inconsistent dimensions in %s", frame.Image)
			}
			if applyCrop {
				img = applyXMPCrop(img, xmpCrops[index])
			}
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
			if resizeFrames {
				img = resizeFrame(img, outputWidth, outputHeight)
			}
			rgb, err := imageRGB24(img)
			if err != nil {
				return fmt.Errorf("convert %s to RGB24: %w", frame.Image, err)
			}
			if err := writer.AddFrame(rgb); err != nil {
				closeErr := writer.Close()
				writerClosed = true
				if closeErr != nil {
					return fmt.Errorf("add frame %s: %w; %v", frame.Image, err, closeErr)
				}
				return fmt.Errorf("add frame %s: %w", frame.Image, err)
			}
			continue
		}
		jpegData := data
		if options.Deflicker || applyXMP || applyCrop || resizeFrames || format != "jpeg" {
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
			if resizeFrames {
				img = resizeFrame(img, outputWidth, outputHeight)
			}
			if err := jpeg.Encode(&encoded, img, &jpeg.Options{Quality: options.Quality}); err != nil {
				return fmt.Errorf("encode %s as JPEG: %w", frame.Image, err)
			}
			jpegData = encoded.Bytes()
		}
		if err := writer.AddFrame(jpegData); err != nil {
			closeErr := writer.Close()
			writerClosed = true
			if closeErr != nil {
				return fmt.Errorf("add frame %s: %w; %v", frame.Image, err, closeErr)
			}
			return fmt.Errorf("add frame %s: %w", frame.Image, err)
		}
	}
	if err := writer.Close(); err != nil {
		writerClosed = true
		if outputExtension == ".avi" {
			return fmt.Errorf("finalize AVI: %w", err)
		}
		return fmt.Errorf("finalize H.264 MP4: %w", err)
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
		return fmt.Errorf("save video to %s: %w", output, err)
	}
	completed = true
	return nil
}
