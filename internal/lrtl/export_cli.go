package lrtl

import (
	"flag"
	"fmt"
	"image"
	"io"
	"path/filepath"
	"strings"
)

func runExport(arguments []string, stdout, stderr io.Writer) int {
	set := newFlagSet("export", stderr)
	output := set.String("output", "", "new output directory for uncompressed 16-bit RGB TIFF frames")
	selection := set.String("selection", "", "use included frames from a long-term selection manifest")
	contains := set.String("filename-contains", "", "export only filenames containing this text (case-insensitive)")
	longEdge := set.Int("long-edge", 0, "downscale so the longest side is at most this many pixels")
	height := set.Int("output-height", 0, "set output height while preserving aspect ratio")
	aspect := set.String("aspect", "", "center-crop to WIDTH:HEIGHT aspect ratio")
	applyBasic := set.Bool("apply-xmp-basic", false, "apply supported XMP tone, color, local and spatial adjustments")
	applyExposure := set.Bool("apply-xmp-exposure", false, "apply XMP Exposure2012")
	applyCrop := set.Bool("apply-xmp-crop", false, "apply XMP crop rectangles and angles")
	autoOrient := set.Bool("auto-orient", true, "apply EXIF orientation")
	rawPreview := set.Bool("raw-preview", false, "use embedded previews from supported RAW files")
	rawDevelop := set.Bool("raw-develop", false, "develop RAW using Adobe DNG Converter or explicit DNGLab")
	dnglab := set.String("dnglab", "", "optional DNGLab executable for --raw-develop")
	converter := set.String("dng-converter", "", "Adobe DNG Converter executable; defaults to installed application")
	deflicker := set.Bool("deflicker", false, "apply global luminance deflicker")
	reference := set.String("reference", "", "reference area x,y,width,height")
	smooth := set.Int("smooth", 5, "deflicker smoothing radius in frames")
	smoother := set.String("deflicker-smoother", "gaussian", "deflicker smoother: gaussian or lowess")
	passes := set.Int("passes", 1, "deflicker passes (1-8)")
	constant := set.Bool("constant", false, "deflicker toward constant luminance")
	motionBlur := set.Int("motion-blur", 0, "temporal blend window (0 off; 2,3,4,5,10,15,20)")
	watermark := set.String("watermark", "", "burn fixed text into exported frames")
	watermarkPosition := set.String("watermark-position", "bottom-left", "top-left, top-right, bottom-left, bottom-right, or center")
	timestamp := set.Bool("timestamp-overlay", false, "burn frame capture time into exported pixels")
	timestampFormat := set.String("timestamp-format", defaultTimestampFormat, "Go time layout for timestamp overlay")
	values := map[string]bool{}
	for _, name := range []string{"output", "selection", "filename-contains", "long-edge", "output-height", "aspect", "dnglab", "dng-converter", "reference", "smooth", "deflicker-smoother", "passes", "motion-blur", "watermark", "watermark-position", "timestamp-format"} {
		values["--"+name] = true
		values["-"+name] = true
	}
	if err := set.Parse(flagFirst(arguments, values)); err != nil {
		if done, code := finishFlagParse(set, err); done {
			return code
		}
		return reportError(stderr, err)
	}
	if set.NArg() != 1 {
		return reportError(stderr, fmt.Errorf("expected one sequence directory"))
	}
	if strings.TrimSpace(*output) == "" {
		return reportError(stderr, fmt.Errorf("--output is required"))
	}
	var ratio float64
	if strings.TrimSpace(*aspect) != "" {
		var err error
		ratio, err = parseAspectRatio(*aspect)
		if err != nil {
			return reportError(stderr, err)
		}
	}
	var area image.Rectangle
	if *reference != "" {
		var err error
		area, err = parseReferenceArea(*reference)
		if err != nil {
			return reportError(stderr, err)
		}
	}
	deflickerEnabled, timestampEnabled := *deflicker, *timestamp
	set.Visit(func(parsed *flag.Flag) {
		switch parsed.Name {
		case "reference", "smooth", "deflicker-smoother", "passes", "constant":
			deflickerEnabled = true
		case "timestamp-format":
			timestampEnabled = true
		}
	})
	frames, err := DiscoverSequence(set.Arg(0))
	if err != nil {
		return reportError(stderr, err)
	}
	if *selection != "" {
		frames, err = loadSelectedFrames(frames, *selection, set.Arg(0))
		if err != nil {
			return reportError(stderr, err)
		}
	}
	if *contains != "" {
		needle := strings.ToLower(*contains)
		matched := make([]Frame, 0, len(frames))
		for _, frame := range frames {
			if strings.Contains(strings.ToLower(filepath.Base(frame.Image)), needle) {
				matched = append(matched, frame)
			}
		}
		frames = matched
	}
	options := RenderOptions{LongEdge: *longEdge, OutputHeight: *height, AspectRatio: ratio, ApplyXMPBasic: *applyBasic, ApplyXMPExposure: *applyExposure, ApplyXMPCrop: *applyCrop, AutoOrient: *autoOrient, RawPreview: *rawPreview, RawDevelop: *rawDevelop, DNGLabPath: *dnglab, DNGConverterPath: *converter, Deflicker: deflickerEnabled, Reference: area, SmoothRadius: *smooth, DeflickerSmoother: *smoother, DeflickerPasses: *passes, Constant: *constant, MotionBlurFrames: *motionBlur, Watermark: *watermark, WatermarkPosition: strings.ToLower(strings.TrimSpace(*watermarkPosition)), TimestampOverlay: timestampEnabled, TimestampFormat: *timestampFormat}
	if err := ExportTIFFSequence(frames, *output, options); err != nil {
		return reportError(stderr, err)
	}
	fmt.Fprintf(stdout, "Exported %d uncompressed 16-bit RGB TIFF frame(s): %s\n", len(frames), *output)
	return 0
}
