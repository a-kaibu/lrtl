package lrtl

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/image/font"
	_ "golang.org/x/image/tiff"
)

type RenderOptions struct {
	LRTPreview            string
	FPS                   int
	FrameRate             string
	FadeOutSeconds        float64
	PlaybackSpeed         float64
	Interpolation         int
	VideoRange            string
	OutputSharpen         bool
	OutputCAS             bool
	OutputDenoise         int
	RemoveAirplanes       int
	OutputDeflickerLuma   bool
	OutputDeflickerChroma bool
	Quality               int
	FFmpegPath            string
	Codec                 string
	Subsampling           string
	CRF                   int
	Preset                string
	LongEdge              int
	OutputHeight          int
	NativeGeometry        bool
	NativeTiming          bool
	CropPosition          *int
	AspectRatio           float64
	AudioPath             string
	Overwrite             bool
	Deflicker             bool
	Reference             image.Rectangle
	SmoothRadius          int
	DeflickerPasses       int
	DeflickerSmoother     string
	Constant              bool
	ApplyXMPExposure      bool
	ApplyXMPBasic         bool
	ApplyXMPCrop          bool
	AutoOrient            bool
	TimestampOverlay      bool
	TimestampFormat       string
	Watermark             string
	WatermarkPosition     string
	MotionBlurFrames      int
	RawPreview            bool
	RawDevelop            bool
	DNGLabPath            string
	DNGConverterPath      string
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

func lrtMotionBlurWeights(frames int) []float32 {
	weights := make([]float32, frames)
	for i := range weights {
		// LRT formats each weight to three decimals before passing it to FFmpeg's tmix filter.
		weights[i] = float32(math.Round(math.Pow(float64(i+1)/float64(frames), 1.5)*1000) / 1000)
	}
	return weights
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

const maxVideoDimension = int64(1<<31 - 1)

// RenderSequence writes JPEG, PNG, TIFF, selected embedded RAW previews, or DNGLab-developed
// RAW frames to MJPEG AVI, H.264/H.265 MP4, or ProRes MOV. It can optionally apply selected
// global XMP tone controls, EXIF orientation, rotated XMP crop bounds, metadata timestamps,
// and fixed-text watermarks, but does not implement full Camera Raw RAW rendering.
func RenderSequence(frames []Frame, output string, options RenderOptions) error {
	return renderSequence(frames, output, options, nil)
}

// A custom RGB48 destination shares all image processing with video rendering.
// Its owner stages and publishes the image directory separately.
func renderSequence(frames []Frame, output string, options RenderOptions, imageWriter func(int, int) (renderFrameWriter, error)) error {
	if len(frames) == 0 {
		return errors.New("sequence contains no images")
	}
	if err := validateOutputDenoise(options.OutputDenoise); err != nil {
		return err
	}
	if err := validateRemoveAirplanes(options.RemoveAirplanes); err != nil {
		return err
	}
	cropPosition, err := options.cropPosition()
	if err != nil {
		return err
	}
	rate, err := options.frameRate()
	if err != nil {
		return err
	}
	speed, err := options.playbackSpeed()
	if err != nil {
		return err
	}
	trimWindow := options.MotionBlurFrames
	airplaneLevel := options.RemoveAirplanes
	interpolationMode := options.Interpolation
	if imageWriter != nil {
		trimWindow = 0 // Intermediate images retain their source correspondence.
		airplaneLevel = 0
		interpolationMode = 0
	}
	label := options.FrameRate
	if label == "" {
		label = rate.String()
	}
	timeline, err := planInterpolatedVideoFrames(len(frames), speed, trimWindow, airplaneLevel, interpolationMode, rate, label)
	if err != nil {
		return err
	}
	outputCount := timeline.count()
	videoRange, err := normalizeVideoRange(options.VideoRange)
	if err != nil {
		return err
	}
	if _, err := normalizeSubsampling(options.Subsampling); err != nil {
		return err
	}
	headTrim, tailTrim := nativeFilterTrim(trimWindow, airplaneLevel)
	timing, err := planRenderTiming(options, len(frames), outputCount, headTrim+tailTrim, rate, label)
	if err != nil {
		return err
	}
	fade := timing.fade
	if options.Quality < 1 || options.Quality > 100 {
		return fmt.Errorf("JPEG quality must be between 1 and 100: %d", options.Quality)
	}
	if err := validateRenderDimensions(options.LongEdge, options.OutputHeight, options.AspectRatio); err != nil {
		return err
	}
	if options.MotionBlurFrames != 0 && !validMotionBlurFrames(options.MotionBlurFrames) {
		return fmt.Errorf("motion blur window must be 0 (disabled) or one of 2, 3, 4, 5, 10, 15, or 20 frames: %d", options.MotionBlurFrames)
	}
	inputOptions := imageInputOptions{LRTPreview: options.LRTPreview, RawPreview: options.RawPreview, RawDevelop: options.RawDevelop, DNGLabPath: options.DNGLabPath, DNGConverterPath: options.DNGConverterPath}
	if err := validateImageInputOptions(inputOptions); err != nil {
		return err
	}
	frameSource := newFrameInputCache(inputOptions, options.Deflicker)
	defer frameSource.close()
	if options.Deflicker {
		if _, err := deflickerGainsWithSmoother([]float64{1}, options.SmoothRadius, options.DeflickerPasses, options.Constant, options.deflickerSmoother()); err != nil {
			return err
		}
	}
	if options.ApplyXMPExposure && options.ApplyXMPBasic {
		return fmt.Errorf("--apply-xmp-exposure cannot be combined with --apply-xmp-basic because the basic controls already include Exposure2012")
	}
	outputExtension := strings.ToLower(filepath.Ext(output))
	if imageWriter != nil {
		outputExtension = ".tiff"
	}
	if imageWriter == nil && outputExtension != ".avi" && outputExtension != ".mp4" && outputExtension != ".mov" {
		return fmt.Errorf("output must use the .avi, .mp4, or .mov extension: %s", output)
	}
	if options.Subsampling != "" && (imageWriter != nil || outputExtension == ".avi") {
		return fmt.Errorf("--subsampling requires H.264 or H.265 output")
	}
	if outputExtension == ".avi" && videoRange == "limited" {
		return fmt.Errorf("limited video range requires MP4 or MOV; MJPEG AVI uses full-range JPEG")
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
	if imageWriter != nil {
		if options.AudioPath != "" || options.Codec != "" {
			return fmt.Errorf("image export does not accept audio or video codecs")
		}
	} else if outputExtension == ".avi" {
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
		if _, _, prores := proresProfile(options.Codec); outputExtension == ".mov" && !prores && options.Codec != "h265" {
			if _, _, supported := dnxhrProfile(options.Codec); !supported {
				return fmt.Errorf("MOV codec must be h265 or a supported ProRes or DNxHR profile: %q", options.Codec)
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
		if options.Codec == "h264" || options.Codec == "h265" {
			format, err := h26xPixelFormat(options.Subsampling)
			if err != nil {
				return err
			}
			encoder := "libx264"
			if options.Codec == "h265" {
				encoder = "libx265"
			}
			if err := requireFFmpegPixelFormat(ffmpegPath, encoder, format); err != nil {
				return err
			}
		} else if options.Subsampling != "" {
			return fmt.Errorf("--subsampling requires H.264 or H.265 output")
		}
	}

	outputDir := filepath.Dir(output)
	if info, err := os.Stat(outputDir); err != nil {
		return fmt.Errorf("output directory: %w", err)
	} else if !info.IsDir() {
		return fmt.Errorf("output parent is not a directory: %s", outputDir)
	}
	if imageWriter == nil && !options.Overwrite {
		if _, err := os.Lstat(output); err == nil {
			return fmt.Errorf("output already exists (pass --overwrite to replace it): %s", output)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("check output: %w", err)
		}
	}

	for _, frame := range frames {
		if options.LRTPreview != "" || supportedRasterExtension(frame.Image) {
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
			return fmt.Errorf("RAW input %s has no embedded-preview reader; use --raw-develop with Adobe DNG Converter (or --dnglab PATH)", filepath.Base(frame.Image))
		}
		return fmt.Errorf("RAW input %s requires --raw-preview or --raw-develop with Adobe DNG Converter (or --dnglab PATH)", filepath.Base(frame.Image))
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
	var overlayFace font.Face

	first, config, firstFormat, err := frameSource.read(0, frames[0])
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
	var nativeGeometry nativeRenderGeometry
	if options.NativeGeometry {
		nativeGeometry, err = planNativeRenderGeometryAtPosition(width, height, options.OutputHeight, options.AspectRatio, cropPosition)
		outputWidth, outputHeight = nativeGeometry.crop.Dx(), nativeGeometry.crop.Dy()
	}
	if err != nil {
		return err
	}
	if err := validateOutputDenoiseMemory(outputWidth, outputHeight, len(frames), options.OutputDenoise, options.MotionBlurFrames); err != nil {
		return err
	}
	if err := validateAirplaneMemory(outputWidth, outputHeight, len(frames), airplaneLevel, options.OutputDenoise, options.MotionBlurFrames); err != nil {
		return err
	}
	outputDeflickerEnabled := imageWriter == nil && (options.OutputDeflickerLuma || options.OutputDeflickerChroma)
	deflickerWidth, deflickerHeight := outputWidth, outputHeight
	if outputDeflickerEnabled && options.NativeGeometry {
		deflickerWidth, deflickerHeight = nativeGeometry.scaleWidth, nativeGeometry.scaleHeight
	}
	if outputDeflickerEnabled {
		if err := validateOutputDeflickerMemory(deflickerWidth, deflickerHeight, outputWidth, outputHeight, len(frames), options.OutputDenoise, airplaneLevel, options.MotionBlurFrames); err != nil {
			return err
		}
	}
	if err := validateInterpolationMemory(outputWidth, outputHeight, deflickerWidth, deflickerHeight, len(frames), interpolationMode, speed, options); err != nil {
		return err
	}
	if options.MotionBlurFrames > 0 {
		outputPixels := uint64(outputWidth) * uint64(outputHeight)
		inputPixels := uint64(width) * uint64(height)
		bytesPerOutputPixel := uint64(options.MotionBlurFrames*6 + 24)
		const maxMotionBlurWorkingSet = uint64(768 * 1024 * 1024)
		if outputPixels > maxMotionBlurWorkingSet/bytesPerOutputPixel {
			return fmt.Errorf("motion blur frame-buffer estimate exceeds 768 MiB; reduce --motion-blur or use --long-edge")
		}
		estimatedBytes := outputPixels * bytesPerOutputPixel
		inputBytesPerPixel := uint64(32)
		if options.ApplyXMPBasic {
			inputBytesPerPixel = 64
		}
		if inputPixels > (maxMotionBlurWorkingSet-estimatedBytes)/inputBytesPerPixel {
			return fmt.Errorf("motion blur frame-buffer estimate exceeds 768 MiB; reduce --motion-blur or use --long-edge")
		}
	}
	resizeFrames := options.NativeGeometry || outputWidth != width || outputHeight != height
	if options.TimestampOverlay || options.Watermark != "" {
		if options.Watermark != "" {
			if options.WatermarkPosition == "" {
				options.WatermarkPosition = "bottom-left"
			}
			switch options.WatermarkPosition {
			case "top-left", "top-right", "bottom-left", "bottom-right", "center":
			default:
				return fmt.Errorf("unsupported watermark position %q (use top-left, top-right, bottom-left, bottom-right, or center)", options.WatermarkPosition)
			}
		}
	}
	if options.TimestampOverlay {
		captureTimes, err = readFrameCaptureTimes(frames)
		if err != nil {
			return fmt.Errorf("timestamp overlay: %w", err)
		}
	}
	if options.TimestampOverlay || options.Watermark != "" {
		overlayFace, err = newTimestampOverlayFace(outputWidth, outputHeight)
		if err != nil {
			return err
		}
		if closer, ok := overlayFace.(interface{ Close() error }); ok {
			defer closer.Close()
		}
	}
	if options.TimestampOverlay {
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
		gains, err = pixelFeedbackGains(len(frames), options.SmoothRadius, options.DeflickerPasses, options.Constant, options.deflickerSmoother(), func(currentGains []float64) ([]float64, error) {
			luminances := make([]float64, len(frames))
			for index, frame := range frames {
				data, frameConfig, format := first, config, firstFormat
				if index > 0 {
					data, frameConfig, format, err = frameSource.read(index, frame)
					if err != nil {
						return nil, err
					}
				}
				img, format, decodeErr := image.Decode(bytes.NewReader(data))
				if decodeErr != nil {
					return nil, fmt.Errorf("decode %s: %w", frame.Image, decodeErr)
				}
				if !supportedRasterFormat(format) {
					return nil, fmt.Errorf("render supports JPEG, PNG, and TIFF data only: %s", frame.Image)
				}
				orientation := uint16(1)
				if options.AutoOrient {
					orientation, err = readEXIFOrientation(data)
					if err != nil {
						return nil, fmt.Errorf("%s: %w", filepath.Base(frame.Image), err)
					}
				}
				frameWidth, frameHeight := orientedDimensions(frameConfig.Width, frameConfig.Height, orientation)
				if frameWidth != width || frameHeight != height {
					return nil, fmt.Errorf("inconsistent dimensions in %s", frame.Image)
				}
				img = applyEXIFOrientation(img, orientation)
				if img.Bounds().Dx() != width || img.Bounds().Dy() != height {
					return nil, fmt.Errorf("inconsistent oriented dimensions in %s", frame.Image)
				}
				if xmpBasics != nil {
					img = applyXMPSpatialCorrections(img, xmpBasics[index])
				}
				if xmpCrops != nil {
					img = applyXMPCrop(img, xmpCrops[index])
				}
				if xmpBasics != nil && xmpBasics[index].globalActive() {
					img = applyXMPBasicFrame(img, xmpBasics[index])
				}
				gain := currentGains[index]
				if xmpExposures != nil {
					gain *= math.Exp2(xmpExposures[index])
				}
				if gain != 1 {
					img = applyExposureGainFrame(img, gain)
				}
				reference := options.Reference
				if reference.Empty() {
					reference = img.Bounds()
				} else {
					reference = reference.Add(img.Bounds().Min)
				}
				luminances[index], err = frameLuminance(img, reference)
				if err != nil {
					return nil, fmt.Errorf("analyze %s: %w", frame.Image, err)
				}
			}
			return luminances, nil
		})
		if err != nil {
			return err
		}
	}

	tempPath, indexPath := "", ""
	cleanup := func() {
		if tempPath != "" {
			_ = os.Remove(tempPath)
		}
		if indexPath != "" {
			_ = os.Remove(indexPath)
		}
	}

	var writer renderFrameWriter
	if imageWriter != nil {
		writer, err = imageWriter(outputWidth, outputHeight)
	} else {
		tempFile, createErr := os.CreateTemp(outputDir, ".lrtl-*"+outputExtension)
		if createErr != nil {
			return fmt.Errorf("create temporary video: %w", createErr)
		}
		tempPath = tempFile.Name()
		if closeErr := tempFile.Close(); closeErr != nil {
			cleanup()
			return fmt.Errorf("close temporary video: %w", closeErr)
		}
		if removeErr := os.Remove(tempPath); removeErr != nil {
			return fmt.Errorf("prepare temporary video: %w", removeErr)
		}
		if outputExtension == ".avi" {
			indexPath = tempPath + ".idx_"
		}
	}
	if imageWriter == nil && outputExtension == ".avi" {
		rate, _ := options.frameRate() // validated before creating any output
		writer, err = newRatedMJPEGWriter(tempPath, int32(outputWidth), int32(outputHeight), rate)
		if err != nil {
			cleanup()
			return fmt.Errorf("create MJPEG AVI: %w", err)
		}
	} else if imageWriter == nil {
		rate, _ := options.frameRate()
		writer, err = newFFmpegFrameWriter(ffmpegPath, tempPath, strings.TrimPrefix(outputExtension, "."), options.Codec, outputWidth, outputHeight, rate, outputCount, options.CRF, options.Preset, options.AudioPath, fade, videoRange, timing.audioDuration, options.Subsampling)
		if err != nil {
			cleanup()
			return err
		}
	}
	if err != nil {
		cleanup()
		return err
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

	if options.MotionBlurFrames > 0 || speed != 1 || options.OutputDenoise > 0 || airplaneLevel > 0 || outputDeflickerEnabled || interpolationMode > 0 {
		prepareMotionBlurFrame := func(index int) ([]byte, error) {
			frame := frames[index]
			data, frameConfig := first, config
			if index > 0 {
				var readErr error
				data, frameConfig, _, readErr = frameSource.read(index, frame)
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
				img = applyXMPSpatialCorrections(img, xmpBasics[index])
			}
			if xmpCrops != nil && xmpCrops[index].Active && (xmpCrops[index].Bounds != image.Rect(0, 0, width, height) || math.Abs(xmpCrops[index].Angle) > 1e-9) {
				img = applyXMPCrop(img, xmpCrops[index])
			}
			if xmpBasics != nil && xmpBasics[index].globalActive() {
				img = applyXMPBasicFrame(img, xmpBasics[index])
			}
			gain := 1.0
			if xmpExposures != nil {
				gain *= math.Exp2(xmpExposures[index])
			}
			if options.Deflicker {
				gain *= gains[index]
			}
			if gain != 1 {
				img = applyExposureGainFrame(img, gain)
			}
			if options.NativeGeometry {
				if outputDeflickerEnabled {
					img = resizeFrame(cropNativeEvenFrame(img, orientation), deflickerWidth, deflickerHeight)
				} else {
					img = applyNativeRenderGeometry(img, orientation, nativeGeometry)
				}
			} else {
				if options.AspectRatio > 0 {
					img = cropImageToAspectAtPosition(img, options.AspectRatio, cropPosition)
				}
				if resizeFrames {
					img = resizeFrame(img, outputWidth, outputHeight)
				}
			}
			return imageRGB48(img)
		}
		if outputDeflickerEnabled {
			filter := newOutputDeflicker(deflickerWidth, deflickerHeight, len(frames), options.OutputDeflickerLuma, options.OutputDeflickerChroma, prepareMotionBlurFrame)
			prepareMotionBlurFrame = filter.frame
			if options.NativeGeometry {
				prepareMotionBlurFrame = func(index int) ([]byte, error) {
					rgb, err := filter.frame(index)
					if err != nil {
						return nil, err
					}
					return cropOutputRGB48(rgb, deflickerWidth, nativeGeometry.crop), nil
				}
			}
		}
		if options.OutputDenoise > 0 {
			denoiser := newOutputDenoiser(outputWidth, outputHeight, len(frames), options.OutputDenoise, prepareMotionBlurFrame)
			prepareMotionBlurFrame = denoiser.frame
		}
		if airplaneLevel > 0 {
			remover := newAirplaneRemover(outputWidth, outputHeight, len(frames), airplaneLevel, prepareMotionBlurFrame)
			prepareMotionBlurFrame = remover.frame
		}

		windowFrames := options.MotionBlurFrames
		if windowFrames == 0 {
			windowFrames = 1
		}
		weights := lrtMotionBlurWeights(windowFrames)
		queuedFrames := make([][]byte, 0, windowFrames)
		stream, err := newPlaybackFrameStream(outputWidth, outputHeight, len(frames), airplaneLevel, interpolationMode, speed, rate, label, prepareMotionBlurFrame)
		if err != nil {
			return err
		}
		for target := 0; target < stream.count; target++ {
			rgb, sourceIndex, prepareErr := stream.frame(target)
			if prepareErr != nil {
				return prepareErr
			}
			preparedRGB := rgb
			queuedFrames = append(queuedFrames, rgb)
			if len(queuedFrames) > windowFrames {
				queuedFrames = queuedFrames[1:]
			}
			if target < timeline.start || target >= timeline.end {
				continue
			}
			blendWindow := make([][]byte, windowFrames)
			padding := windowFrames - len(queuedFrames)
			for i := 0; i < padding; i++ {
				blendWindow[i] = queuedFrames[0]
			}
			copy(blendWindow[padding:], queuedFrames)
			if windowFrames == 1 {
				// Duplication without Motion Blur needs no per-channel blend.
				// Own the output buffer because fade may modify it in place.
				rgb = bytes.Clone(preparedRGB)
			} else {
				var blendErr error
				rgb, blendErr = weightedRGB48Frames(blendWindow, weights)
				if blendErr != nil {
					return blendErr
				}
			}
			if options.OutputSharpen || options.OutputCAS {
				filterOutputRGB48(rgb, outputWidth, outputHeight, options)
			}
			var img image.Image
			if overlayFace != nil || outputExtension == ".avi" {
				img, err = imageFromRGB48(rgb, outputWidth, outputHeight)
				if err != nil {
					return err
				}
				if options.TimestampOverlay {
					img = applyTimestampOverlay(img, captureTimes[sourceIndex].Format(options.TimestampFormat), overlayFace)
				}
				if options.Watermark != "" {
					img = applyWatermarkOverlay(img, options.Watermark, options.WatermarkPosition, overlayFace)
				}
			}
			if img != nil {
				img = fadeVideoImage(img, fade.gain(target-timeline.start, rate))
			}
			if outputExtension == ".avi" {
				var encoded bytes.Buffer
				if err := jpeg.Encode(&encoded, img, &jpeg.Options{Quality: options.Quality}); err != nil {
					return fmt.Errorf("encode %s as JPEG: %w", frames[sourceIndex].Image, err)
				}
				rgb = encoded.Bytes()
			} else if overlayFace != nil {
				rgb, err = imageRGB48(img)
				if err != nil {
					return fmt.Errorf("convert overlaid %s to RGB48: %w", frames[sourceIndex].Image, err)
				}
			}
			if outputExtension != ".avi" && img == nil {
				fadeRGB48(rgb, fade.gain(target-timeline.start, rate))
			}
			if err := writer.AddFrame(rgb); err != nil {
				closeErr := writer.Close()
				writerClosed = true
				if closeErr != nil {
					return fmt.Errorf("add frame %s: %w; %v", frames[sourceIndex].Image, err, closeErr)
				}
				return fmt.Errorf("add frame %s: %w", frames[sourceIndex].Image, err)
			}
		}
		if err := stream.finish(); err != nil {
			return err
		}
		frames = nil
	}

	for index, frame := range frames {
		data, config, format := first, config, firstFormat
		if index > 0 {
			data, config, format, err = frameSource.read(index, frame)
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
				img = applyXMPSpatialCorrections(img, xmpBasics[index])
			}
			if applyCrop {
				img = applyXMPCrop(img, xmpCrops[index])
			}
			if applyBasic && xmpBasics[index].globalActive() {
				img = applyXMPBasicFrame(img, xmpBasics[index])
			}
			gain := 1.0
			if applyXMP {
				gain *= math.Exp2(xmpExposures[index])
			}
			if options.Deflicker {
				gain *= gains[index]
			}
			if applyXMP || options.Deflicker {
				img = applyExposureGainFrame(img, gain)
			}
			if options.NativeGeometry {
				img = applyNativeRenderGeometry(img, orientation, nativeGeometry)
			} else {
				if options.AspectRatio > 0 {
					img = cropImageToAspectAtPosition(img, options.AspectRatio, cropPosition)
				}
				if resizeFrames {
					img = resizeFrame(img, outputWidth, outputHeight)
				}
			}
			if options.OutputSharpen || options.OutputCAS {
				img, err = filterOutputImage(img, options)
				if err != nil {
					return err
				}
			}
			if options.TimestampOverlay {
				img = applyTimestampOverlay(img, captureTimes[index].Format(options.TimestampFormat), overlayFace)
			}
			if options.Watermark != "" {
				img = applyWatermarkOverlay(img, options.Watermark, options.WatermarkPosition, overlayFace)
			}
			rgb, err := imageRGB48(img)
			if err != nil {
				return fmt.Errorf("convert %s to RGB48: %w", frame.Image, err)
			}
			fadeRGB48(rgb, fade.gain(index, rate))
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
		if options.Deflicker || applyXMP || applyBasic || applyCrop || (options.AutoOrient && orientation != 1) || resizeFrames || overlayFace != nil || options.OutputSharpen || options.OutputCAS || format != "jpeg" || fade.gain(index, rate) < 1 {
			img, _, err := image.Decode(bytes.NewReader(data))
			if err != nil {
				return fmt.Errorf("decode %s: %w", frame.Image, err)
			}
			img = applyEXIFOrientation(img, orientation)
			if xmpBasics != nil {
				img = applyXMPSpatialCorrections(img, xmpBasics[index])
			}
			if applyCrop {
				img = applyXMPCrop(img, xmpCrops[index])
			}
			if applyBasic && xmpBasics[index].globalActive() {
				img = applyXMPBasicFrame(img, xmpBasics[index])
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
				img = applyExposureGainFrame(img, gain)
			}
			if options.NativeGeometry {
				img = applyNativeRenderGeometry(img, orientation, nativeGeometry)
			} else {
				if options.AspectRatio > 0 {
					img = cropImageToAspectAtPosition(img, options.AspectRatio, cropPosition)
				}
				if resizeFrames {
					img = resizeFrame(img, outputWidth, outputHeight)
				}
			}
			if options.OutputSharpen || options.OutputCAS {
				img, err = filterOutputImage(img, options)
				if err != nil {
					return err
				}
			}
			if options.TimestampOverlay {
				img = applyTimestampOverlay(img, captureTimes[index].Format(options.TimestampFormat), overlayFace)
			}
			if options.Watermark != "" {
				img = applyWatermarkOverlay(img, options.Watermark, options.WatermarkPosition, overlayFace)
			}
			img = fadeVideoImage(img, fade.gain(index, rate))
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
	if imageWriter != nil {
		completed = true
		return nil
	}

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
