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
	"time"

	"github.com/icza/mjpeg"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	_ "golang.org/x/image/tiff"
)

type RenderOptions struct {
	FPS               int
	Quality           int
	FFmpegPath        string
	Codec             string
	CRF               int
	Preset            string
	LongEdge          int
	OutputHeight      int
	AspectRatio       float64
	AudioPath         string
	Overwrite         bool
	Deflicker         bool
	Reference         image.Rectangle
	SmoothRadius      int
	DeflickerPasses   int
	DeflickerSmoother string
	Constant          bool
	ApplyXMPExposure  bool
	ApplyXMPBasic     bool
	ApplyXMPCrop      bool
	AutoOrient        bool
	TimestampOverlay  bool
	TimestampFormat   string
	MotionBlurFrames  int
	RawPreview        bool
	RawDevelop        bool
	DNGLabPath        string
}

func (options RenderOptions) deflickerSmoother() string {
	if options.DeflickerSmoother == "" {
		return "gaussian"
	}
	return options.DeflickerSmoother
}

func validMotionBlurFrames(frames int) bool {
	switch frames {
	case 2, 3, 4, 5, 10, 15, 20:
		return true
	default:
		return false
	}
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

func imageFromRGB24(rgb []byte, width, height int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for source, destination := 0, 0; source+2 < len(rgb) && destination+3 < len(img.Pix); source, destination = source+3, destination+4 {
		img.Pix[destination] = rgb[source]
		img.Pix[destination+1] = rgb[source+1]
		img.Pix[destination+2] = rgb[source+2]
		img.Pix[destination+3] = 255
	}
	return img
}

func lrtMotionBlurWeights(frames int) []float32 {
	weights := make([]float32, frames)
	for i := range weights {
		// LRT formats each weight to three decimals before passing it to FFmpeg's tmix filter.
		weights[i] = float32(math.Round(math.Pow(float64(i+1)/float64(frames), 1.5)*1000) / 1000)
	}
	return weights
}

func weightedRGB24Frames(frames [][]byte, weights []float32) ([]byte, error) {
	if len(frames) == 0 {
		return nil, fmt.Errorf("cannot blend an empty frame window")
	}
	if len(frames) != len(weights) {
		return nil, fmt.Errorf("cannot blend %d frames with %d weights", len(frames), len(weights))
	}
	frameLength := len(frames[0])
	for _, frame := range frames[1:] {
		if len(frame) != frameLength {
			return nil, fmt.Errorf("cannot blend frames with different RGB24 sizes")
		}
	}
	var weightSum float32
	for _, weight := range weights {
		weightSum += weight
	}
	if weightSum <= 0 {
		return nil, fmt.Errorf("cannot blend with a non-positive weight sum")
	}
	result := make([]byte, frameLength)
	inverseWeightSum := 1 / weightSum
	for i := range result {
		var sum float32
		for frameIndex, frame := range frames {
			sum += float32(frame[i]) * weights[frameIndex]
		}
		result[i] = byte(math.RoundToEven(float64(sum * inverseWeightSum)))
	}
	return result, nil
}

func validateLongEdge(longEdge int) error {
	if longEdge < 0 || longEdge > 32768 || (longEdge > 0 && longEdge < 16) {
		return fmt.Errorf("long edge must be 0 or between 16 and 32768 pixels: %d", longEdge)
	}
	return nil
}

func validateRenderDimensions(longEdge, outputHeight int, aspectRatio float64) error {
	if err := validateLongEdge(longEdge); err != nil {
		return err
	}
	if outputHeight < 0 || outputHeight > 32768 || (outputHeight > 0 && outputHeight < 16) {
		return fmt.Errorf("output height must be 0 or between 16 and 32768 pixels: %d", outputHeight)
	}
	if longEdge > 0 && outputHeight > 0 {
		return fmt.Errorf("--long-edge and --output-height cannot be used together")
	}
	if math.IsNaN(aspectRatio) || math.IsInf(aspectRatio, 0) || (aspectRatio != 0 && (aspectRatio < 1.0/16 || aspectRatio > 16)) {
		return fmt.Errorf("aspect ratio must be 0 (source ratio) or between 1:16 and 16:1: %g", aspectRatio)
	}
	return nil
}

func outputDimensions(width, height, longEdge, outputHeight int, aspectRatio float64) (int, int, error) {
	if err := validateRenderDimensions(longEdge, outputHeight, aspectRatio); err != nil {
		return 0, 0, err
	}
	if aspectRatio > 0 {
		cropBounds := aspectCropBounds(image.Rect(0, 0, width, height), aspectRatio)
		width, height = cropBounds.Dx(), cropBounds.Dy()
	}
	if outputHeight > 0 {
		scaledWidth := math.Round(float64(width) * float64(outputHeight) / float64(height))
		if scaledWidth < 1 || scaledWidth > float64(maxVideoDimension) {
			return 0, 0, fmt.Errorf("output dimensions exceed supported limits: %.0fx%d", scaledWidth, outputHeight)
		}
		return int(scaledWidth), outputHeight, nil
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
// RAW frames to MJPEG AVI, H.264/H.265 MP4, or ProRes MOV. It can optionally apply selected
// global XMP tone controls, EXIF orientation, rotated XMP crop bounds, and a metadata timestamp
// overlay, but does not implement full Camera Raw RAW rendering.
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
	if err := validateRenderDimensions(options.LongEdge, options.OutputHeight, options.AspectRatio); err != nil {
		return err
	}
	if options.MotionBlurFrames != 0 && !validMotionBlurFrames(options.MotionBlurFrames) {
		return fmt.Errorf("motion blur window must be 0 (disabled) or one of 2, 3, 4, 5, 10, 15, or 20 frames: %d", options.MotionBlurFrames)
	}
	inputOptions := imageInputOptions{RawPreview: options.RawPreview, RawDevelop: options.RawDevelop, DNGLabPath: options.DNGLabPath}
	if err := validateImageInputOptions(inputOptions); err != nil {
		return err
	}
	if options.ApplyXMPExposure && options.ApplyXMPBasic {
		return fmt.Errorf("--apply-xmp-exposure cannot be combined with --apply-xmp-basic because the basic controls already include Exposure2012")
	}
	outputExtension := strings.ToLower(filepath.Ext(output))
	if outputExtension != ".avi" && outputExtension != ".mp4" && outputExtension != ".mov" {
		return fmt.Errorf("output must use the .avi, .mp4, or .mov extension: %s", output)
	}
	if strings.TrimSpace(options.AudioPath) != "" {
		if outputExtension == ".avi" {
			return fmt.Errorf("--audio requires MP4 or MOV output; MJPEG AVI audio muxing is not supported")
		}
		if _, err := os.Stat(options.AudioPath); err != nil {
			return fmt.Errorf("audio track %s: %w", options.AudioPath, err)
		}
	}
	ffmpegPath := ""
	if outputExtension == ".avi" {
		if options.Codec != "" {
			return fmt.Errorf("--codec is only available for MP4 or MOV output")
		}
	} else {
		if options.Codec == "" {
			if outputExtension == ".mov" {
				options.Codec = "prores"
			} else {
				options.Codec = "h264"
			}
		}
		if outputExtension == ".mp4" && options.Codec != "h264" && options.Codec != "h265" {
			return fmt.Errorf("MP4 codec must be h264 or h265: %q", options.Codec)
		}
		if outputExtension == ".mov" && options.Codec != "prores" && options.Codec != "prores-hq" {
			if _, _, supported := dnxhrProfile(options.Codec); !supported {
				return fmt.Errorf("MOV codec must be prores, prores-hq, or a DNxHR profile: %q", options.Codec)
			}
		}
		if options.Codec == "h264" || options.Codec == "h265" {
			if options.CRF < 0 || options.CRF > 51 {
				return fmt.Errorf("H.264/H.265 CRF must be between 0 and 51: %d", options.CRF)
			}
			if options.Preset == "" {
				options.Preset = "medium"
			}
			if !validH264Preset(options.Preset) {
				return fmt.Errorf("unsupported H.264/H.265 preset %q", options.Preset)
			}
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
	var xmpBasics []xmpBasicAdjustments
	if options.ApplyXMPBasic {
		xmpBasics = make([]xmpBasicAdjustments, len(frames))
		for index, frame := range frames {
			adjustments, err := readXMPBasicAdjustments(frame)
			if err != nil {
				return fmt.Errorf("%s: %w", filepath.Base(frame.Image), err)
			}
			xmpBasics[index] = adjustments
		}
	}
	var captureTimes []time.Time
	var timestampFace font.Face

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
	firstOrientation := uint16(1)
	if options.AutoOrient {
		firstOrientation, err = readEXIFOrientation(first)
		if err != nil {
			return fmt.Errorf("%s: %w", filepath.Base(frames[0].Image), err)
		}
	}
	width, height := orientedDimensions(config.Width, config.Height, firstOrientation)
	outputWidth, outputHeight, err := outputDimensions(width, height, options.LongEdge, options.OutputHeight, options.AspectRatio)
	if err != nil {
		return err
	}
	if options.MotionBlurFrames > 0 {
		outputPixels := uint64(outputWidth) * uint64(outputHeight)
		inputPixels := uint64(width) * uint64(height)
		bytesPerOutputPixel := uint64(options.MotionBlurFrames*3 + 9)
		const maxMotionBlurWorkingSet = uint64(768 * 1024 * 1024)
		if outputPixels > maxMotionBlurWorkingSet/bytesPerOutputPixel {
			return fmt.Errorf("motion blur frame-buffer estimate exceeds 768 MiB; reduce --motion-blur or use --long-edge")
		}
		estimatedBytes := outputPixels * bytesPerOutputPixel
		if inputPixels > (maxMotionBlurWorkingSet-estimatedBytes)/8 {
			return fmt.Errorf("motion blur frame-buffer estimate exceeds 768 MiB; reduce --motion-blur or use --long-edge")
		}
	}
	resizeFrames := outputWidth != width || outputHeight != height
	if options.TimestampOverlay {
		captureTimes, err = readFrameCaptureTimes(frames)
		if err != nil {
			return fmt.Errorf("timestamp overlay: %w", err)
		}
		timestampFace, err = newTimestampOverlayFace(outputWidth, outputHeight)
		if err != nil {
			return err
		}
		if closer, ok := timestampFace.(interface{ Close() error }); ok {
			defer closer.Close()
		}
		if options.TimestampFormat == "" {
			options.TimestampFormat = defaultTimestampFormat
		}
	}
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
			orientation := uint16(1)
			if options.AutoOrient {
				orientation, err = readEXIFOrientation(data)
				if err != nil {
					return fmt.Errorf("%s: %w", filepath.Base(frame.Image), err)
				}
			}
			frameWidth, frameHeight := orientedDimensions(frameConfig.Width, frameConfig.Height, orientation)
			if frameWidth != width || frameHeight != height {
				return fmt.Errorf("inconsistent dimensions in %s", frame.Image)
			}
			img = applyEXIFOrientation(img, orientation)
			if img.Bounds().Dx() != width || img.Bounds().Dy() != height {
				return fmt.Errorf("inconsistent oriented dimensions in %s", frame.Image)
			}
			if xmpBasics != nil {
				img = applyXMPLocalCorrections(img, xmpBasics[index].local)
			}
			if xmpCrops != nil {
				img = applyXMPCrop(img, xmpCrops[index])
			}
			if xmpBasics != nil && xmpBasics[index].globalActive() {
				img = applyXMPBasic(img, xmpBasics[index])
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
		gains, err = deflickerGainsWithSmoother(luminances, options.SmoothRadius, options.DeflickerPasses, options.Constant, options.deflickerSmoother())
		if err != nil {
			return err
		}
	}

	tempFile, err := os.CreateTemp(outputDir, ".lrtl-*"+outputExtension)
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
		writer, err = newFFmpegFrameWriter(ffmpegPath, tempPath, strings.TrimPrefix(outputExtension, "."), options.Codec, outputWidth, outputHeight, options.FPS, len(frames), options.CRF, options.Preset, options.AudioPath)
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

	if options.MotionBlurFrames > 0 {
		prepareMotionBlurFrame := func(index int) ([]byte, error) {
			frame := frames[index]
			data, frameConfig := first, config
			if index > 0 {
				var readErr error
				data, frameConfig, _, readErr = readRenderInputWithOptions(frame, inputOptions)
				if readErr != nil {
					return nil, readErr
				}
			}
			orientation := uint16(1)
			if options.AutoOrient {
				var orientErr error
				orientation, orientErr = readEXIFOrientation(data)
				if orientErr != nil {
					return nil, fmt.Errorf("%s: %w", filepath.Base(frame.Image), orientErr)
				}
			}
			frameWidth, frameHeight := orientedDimensions(frameConfig.Width, frameConfig.Height, orientation)
			if frameWidth != width || frameHeight != height {
				return nil, fmt.Errorf("inconsistent dimensions in %s", frame.Image)
			}
			img, format, decodeErr := image.Decode(bytes.NewReader(data))
			if decodeErr != nil {
				return nil, fmt.Errorf("decode %s: %w", frame.Image, decodeErr)
			}
			if !supportedRasterFormat(format) {
				return nil, fmt.Errorf("render supports JPEG, PNG, and TIFF data only: %s", frame.Image)
			}
			img = applyEXIFOrientation(img, orientation)
			if img.Bounds().Dx() != width || img.Bounds().Dy() != height {
				return nil, fmt.Errorf("inconsistent oriented dimensions in %s", frame.Image)
			}
			if xmpBasics != nil {
				img = applyXMPLocalCorrections(img, xmpBasics[index].local)
			}
			if xmpCrops != nil && xmpCrops[index].Active && (xmpCrops[index].Bounds != image.Rect(0, 0, width, height) || math.Abs(xmpCrops[index].Angle) > 1e-9) {
				img = applyXMPCrop(img, xmpCrops[index])
			}
			if xmpBasics != nil && xmpBasics[index].globalActive() {
				img = applyXMPBasic(img, xmpBasics[index])
			}
			gain := 1.0
			if xmpExposures != nil {
				gain *= math.Exp2(xmpExposures[index])
			}
			if options.Deflicker {
				gain *= gains[index]
			}
			if gain != 1 {
				img = applyExposureGain(img, gain)
			}
			if options.AspectRatio > 0 {
				img = cropImageToAspect(img, options.AspectRatio)
			}
			if resizeFrames {
				img = resizeFrame(img, outputWidth, outputHeight)
			}
			return imageRGB24(img)
		}

		windowFrames := options.MotionBlurFrames
		weights := lrtMotionBlurWeights(windowFrames)
		queuedFrames := make([][]byte, 0, windowFrames)
		for target := range frames {
			rgb, prepareErr := prepareMotionBlurFrame(target)
			if prepareErr != nil {
				return fmt.Errorf("prepare %s: %w", filepath.Base(frames[target].Image), prepareErr)
			}
			queuedFrames = append(queuedFrames, rgb)
			if len(queuedFrames) > windowFrames {
				queuedFrames = queuedFrames[1:]
			}
			blendWindow := make([][]byte, windowFrames)
			padding := windowFrames - len(queuedFrames)
			for i := 0; i < padding; i++ {
				blendWindow[i] = queuedFrames[0]
			}
			copy(blendWindow[padding:], queuedFrames)
			rgb, blendErr := weightedRGB24Frames(blendWindow, weights)
			if blendErr != nil {
				return blendErr
			}
			var img image.Image
			if timestampFace != nil || outputExtension == ".avi" {
				img = imageFromRGB24(rgb, outputWidth, outputHeight)
				if timestampFace != nil {
					img = applyTimestampOverlay(img, captureTimes[target].Format(options.TimestampFormat), timestampFace)
				}
			}
			if outputExtension == ".avi" {
				var encoded bytes.Buffer
				if err := jpeg.Encode(&encoded, img, &jpeg.Options{Quality: options.Quality}); err != nil {
					return fmt.Errorf("encode %s as JPEG: %w", frames[target].Image, err)
				}
				rgb = encoded.Bytes()
			} else if timestampFace != nil {
				rgb, err = imageRGB24(img)
				if err != nil {
					return fmt.Errorf("convert timestamped %s to RGB24: %w", frames[target].Image, err)
				}
			}
			if err := writer.AddFrame(rgb); err != nil {
				closeErr := writer.Close()
				writerClosed = true
				if closeErr != nil {
					return fmt.Errorf("add frame %s: %w; %v", frames[target].Image, err, closeErr)
				}
				return fmt.Errorf("add frame %s: %w", frames[target].Image, err)
			}
		}
		frames = nil
	}

	for index, frame := range frames {
		data, config, format := first, config, firstFormat
		if index > 0 {
			data, config, format, err = readRenderInputWithOptions(frame, inputOptions)
			if err != nil {
				return err
			}
		}
		orientation := uint16(1)
		if options.AutoOrient {
			orientation, err = readEXIFOrientation(data)
			if err != nil {
				return fmt.Errorf("%s: %w", filepath.Base(frame.Image), err)
			}
		}
		frameWidth, frameHeight := orientedDimensions(config.Width, config.Height, orientation)
		if frameWidth != width || frameHeight != height {
			return fmt.Errorf("inconsistent dimensions in %s", frame.Image)
		}

		applyXMP := xmpExposures != nil && xmpExposures[index] != 0
		applyBasic := xmpBasics != nil && xmpBasics[index].active()
		applyCrop := xmpCrops != nil && xmpCrops[index].Active && (xmpCrops[index].Bounds != image.Rect(0, 0, width, height) || math.Abs(xmpCrops[index].Angle) > 1e-9)
		if outputExtension != ".avi" {
			img, format, err := image.Decode(bytes.NewReader(data))
			if err != nil {
				return fmt.Errorf("decode %s: %w", frame.Image, err)
			}
			if !supportedRasterFormat(format) {
				return fmt.Errorf("render supports JPEG, PNG, and TIFF data only: %s", frame.Image)
			}
			img = applyEXIFOrientation(img, orientation)
			if img.Bounds().Dx() != width || img.Bounds().Dy() != height {
				return fmt.Errorf("inconsistent dimensions in %s", frame.Image)
			}
			if xmpBasics != nil {
				img = applyXMPLocalCorrections(img, xmpBasics[index].local)
			}
			if applyCrop {
				img = applyXMPCrop(img, xmpCrops[index])
			}
			if applyBasic && xmpBasics[index].globalActive() {
				img = applyXMPBasic(img, xmpBasics[index])
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
			if options.AspectRatio > 0 {
				img = cropImageToAspect(img, options.AspectRatio)
			}
			if resizeFrames {
				img = resizeFrame(img, outputWidth, outputHeight)
			}
			if timestampFace != nil {
				img = applyTimestampOverlay(img, captureTimes[index].Format(options.TimestampFormat), timestampFace)
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
		if options.Deflicker || applyXMP || applyBasic || applyCrop || (options.AutoOrient && orientation != 1) || resizeFrames || timestampFace != nil || format != "jpeg" {
			img, _, err := image.Decode(bytes.NewReader(data))
			if err != nil {
				return fmt.Errorf("decode %s: %w", frame.Image, err)
			}
			img = applyEXIFOrientation(img, orientation)
			if xmpBasics != nil {
				img = applyXMPLocalCorrections(img, xmpBasics[index].local)
			}
			if applyCrop {
				img = applyXMPCrop(img, xmpCrops[index])
			}
			if applyBasic && xmpBasics[index].globalActive() {
				img = applyXMPBasic(img, xmpBasics[index])
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
			if options.AspectRatio > 0 {
				img = cropImageToAspect(img, options.AspectRatio)
			}
			if resizeFrames {
				img = resizeFrame(img, outputWidth, outputHeight)
			}
			if timestampFace != nil {
				img = applyTimestampOverlay(img, captureTimes[index].Format(options.TimestampFormat), timestampFace)
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
		return fmt.Errorf("finalize %s: %w", strings.ToUpper(options.Codec), err)
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
