package lrtl

import (
	"bytes"
	"flag"
	"fmt"
	"image"
	"image/color"
	"io"
	"math"
	"path/filepath"
)

// analyzePreviewEVSequence measures each supplied display raster using the
// channel histogram used by LRTimelapse's Preview EV analysis path. The LRT
// field is four times the UI value, which is the mean intensity divided by 64.
func analyzePreviewEVSequence(frames []Frame, options imageInputOptions) ([]float64, error) {
	if len(frames) == 0 {
		return nil, fmt.Errorf("sequence contains no images")
	}
	if err := validateImageInputOptions(options); err != nil {
		return nil, err
	}
	var references []lrtReferenceBox
	if options.LRTReference {
		boxes, order, err := loadLRTReferences(frames)
		if err != nil {
			return nil, err
		}
		references = make([]lrtReferenceBox, len(frames))
		for nativeIndex, sourceIndex := range order {
			references[sourceIndex] = boxes[nativeIndex]
		}
	}
	values := make([]float64, len(frames))
	width, height := 0, 0
	var adjustments sequenceXMPAdjustments
	for index, frame := range frames {
		data, _, _, err := readRenderInputWithOptions(frame, options)
		if err != nil {
			return nil, fmt.Errorf("Preview EV analysis: %w", err)
		}
		img, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("decode %s: %w", frame.Image, err)
		}
		bounds := img.Bounds()
		if bounds.Empty() {
			return nil, fmt.Errorf("invalid image dimensions in %s", frame.Image)
		}
		if index == 0 {
			width, height = bounds.Dx(), bounds.Dy()
			adjustments, err = readSequenceXMPAdjustments(frames, width, height, options)
			if err != nil {
				return nil, err
			}
		} else if bounds.Dx() != width || bounds.Dy() != height {
			return nil, fmt.Errorf("inconsistent dimensions in %s", frame.Image)
		}
		img = adjustments.apply(index, img)
		previewWidthCamera := options.PreviewWidthCamera
		if previewWidthCamera == 0 {
			previewWidthCamera = lrtDefaultCameraPreviewWidth
		}
		img, err = resizeLRTCameraPreviewWidth(img, previewWidthCamera)
		if err != nil {
			return nil, fmt.Errorf("prepare LRT Preview EV image %s: %w", frame.Image, err)
		}
		area := img.Bounds()
		if references != nil {
			area, err = references[index].rectangle(img.Bounds())
			if err != nil {
				return nil, fmt.Errorf("analyze %s: %w", frame.Image, err)
			}
		}
		values[index], err = framePreviewEVAreaWithOptions(img, area, options.PreviewHistogram)
		if err != nil {
			return nil, fmt.Errorf("analyze %s: %w", frame.Image, err)
		}
	}
	return values, nil
}

func framePreviewEV(img image.Image) (float64, error) {
	return framePreviewEVArea(img, img.Bounds())
}

func framePreviewEVArea(img image.Image, area image.Rectangle) (float64, error) {
	return framePreviewEVAreaWithOptions(img, area, previewHistogramOptions{})
}

func framePreviewEVAreaWithOptions(img image.Image, area image.Rectangle, options previewHistogramOptions) (float64, error) {
	if err := options.validate(); err != nil {
		return 0, err
	}
	if area.Empty() || area.Intersect(img.Bounds()) != area {
		return 0, fmt.Errorf("Preview EV reference area %v is empty or outside image bounds %v", area, img.Bounds())
	}
	var histogram [3][256]uint64
	for y := area.Min.Y; y < area.Max.Y; y++ {
		for x := area.Min.X; x < area.Max.X; x++ {
			pixel := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			for channel, value := range [3]uint8{pixel.R, pixel.G, pixel.B} {
				// Native histogram's high bound (255) is exclusive; its 256
				// bins span [0,255). Every integer below 255 stays in its bin.
				if value < 255 {
					histogram[channel][value]++
				}
			}
		}
	}
	// LRT uses JAI's weighted radius-2 smoothing. The upper sample bound is
	// exclusive. Round counts, restore each band's original population, round
	// again, then average the three band means with bin width 255/256.
	weights := [5]float64{1.0 / 25, 2.0 / 25, 3.0 / 25, 2.0 / 25, 1.0 / 25}
	mean := 0.0
	var filtered [3][256]uint64
	for channel := range histogram {
		var smoothed [256]uint64
		var original, total uint64
		for _, n := range histogram[channel] {
			original += n
		}
		for bin := range smoothed {
			first, last := max(0, bin-2), min(256, bin+2)
			weightIndex := max(0, 2-bin)
			sum, weightSum := 0.0, 0.0
			for sample := first; sample < last; sample++ {
				weight := weights[weightIndex]
				weightIndex++
				sum += float64(histogram[channel][sample]) * weight
				weightSum += weight
			}
			smoothed[bin] = uint64(sum/weightSum + .5)
			total += smoothed[bin]
		}
		if total == 0 || original == 0 {
			if options.Enabled {
				continue
			}
			return 255.0 / 256, nil
		}
		scale := float64(original) / float64(total)
		total = 0
		for bin, n := range smoothed {
			smoothed[bin] = uint64(float64(n)*scale + .5)
			total += smoothed[bin]
		}
		if total == 0 {
			if options.Enabled {
				continue
			}
			return 255.0 / 256, nil
		}
		if options.Enabled {
			filtered[channel] = smoothed
			continue
		}
		channelMean := 0.0
		for bin, n := range smoothed {
			channelMean += float64(n) / float64(total) * (float64(bin) * 255 / 256)
		}
		mean += channelMean
	}
	if options.Enabled {
		return filteredPreviewEV(filtered, options), nil
	}
	value := mean / 3 / 256
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, fmt.Errorf("non-finite Preview EV")
	}
	return value, nil
}

func runPreviewEV(arguments []string, stdout, stderr io.Writer) int {
	set := newFlagSet("preview-ev", stderr)
	previewWidthCamera := set.Int("preview-width-camera", lrtDefaultCameraPreviewWidth, "LRT camera preview width threshold and resize target")
	ignoreBlacks := set.Int("ignore-blacks", 0, "black histogram range percentage (0-100); enables soft exclusion")
	ignoreWhites := set.Int("ignore-whites", 0, "white histogram range percentage (0-100); enables soft exclusion")
	hardCut := set.Bool("hard-cut", false, "use a hard histogram cutoff instead of soft exclusion")
	lrtReference := set.Bool("lrt-reference", false, "measure saved referenceBoxes from .lrt/lrtsequence.json")
	lrtPreview := set.String("lrt-preview", "", "measure existing LRT camera or visual preview cache")
	rawPreview := set.Bool("raw-preview", false, "analyze embedded previews from supported RAW files")
	rawDevelop := set.Bool("raw-develop", false, "analyze RAW developed by Adobe DNG Converter (or explicit DNGLab)")
	dnglab := set.String("dnglab", "", "optional DNGLab executable for --raw-develop")
	dngConverter := set.String("dng-converter", "", "Adobe DNG Converter executable; defaults to the installed application")
	applyXMPExposure := set.Bool("apply-xmp-exposure", false, "apply crs:Exposure2012 from sidecar XMP during analysis")
	applyXMPBasic := set.Bool("apply-xmp-basic", false, "apply supported Camera Raw tone and color controls during analysis")
	applyXMPCrop := set.Bool("apply-xmp-crop", false, "apply rotated crop rectangles from sidecar XMP during analysis")
	write := set.Bool("write", false, "write lrt:ExternalExposureDefault to XMP sidecars")
	noBackup := set.Bool("no-backup", false, "do not create backups when writing")
	if err := set.Parse(flagFirst(arguments, map[string]bool{"--preview-width-camera": true, "-preview-width-camera": true, "--ignore-blacks": true, "-ignore-blacks": true, "--ignore-whites": true, "-ignore-whites": true, "--lrt-preview": true, "-lrt-preview": true, "--dnglab": true, "-dnglab": true, "--dng-converter": true, "-dng-converter": true})); err != nil {
		if done, code := finishFlagParse(set, err); done {
			fmt.Fprintln(stdout, "Usage: lrtl preview-ev DIRECTORY [--preview-width-camera N] [--lrt-reference] [--lrt-preview camera|visual] [--raw-preview|--raw-develop] [--dng-converter PATH|--dnglab PATH] [--apply-xmp-exposure|--apply-xmp-basic] [--apply-xmp-crop] [--write] [--no-backup]")
			return code
		}
	}
	if set.NArg() != 1 {
		return reportError(stderr, fmt.Errorf("Usage: lrtl preview-ev DIRECTORY [--preview-width-camera N] [--lrt-reference] [--lrt-preview camera|visual] [--raw-preview|--raw-develop] [--dng-converter PATH|--dnglab PATH] [--apply-xmp-exposure|--apply-xmp-basic] [--apply-xmp-crop] [--write] [--no-backup]"))
	}
	if *noBackup && !*write {
		return reportError(stderr, fmt.Errorf("--no-backup requires --write"))
	}
	frames, err := DiscoverSequence(set.Arg(0))
	if err != nil {
		return reportError(stderr, err)
	}
	options := imageInputOptions{
		PreviewWidthCamera: *previewWidthCamera,
		LRTReference:       *lrtReference,
		LRTPreview:         *lrtPreview,
		RawPreview:         *rawPreview, RawDevelop: *rawDevelop, DNGLabPath: *dnglab, DNGConverterPath: *dngConverter,
		ApplyXMPExposure: *applyXMPExposure, ApplyXMPBasic: *applyXMPBasic,
		ApplyXMPCrop: *applyXMPCrop,
	}
	options.PreviewHistogram = previewHistogramOptions{IgnoreBlacks: *ignoreBlacks, IgnoreWhites: *ignoreWhites, HardCut: *hardCut}
	set.Visit(func(f *flag.Flag) {
		if f.Name == "ignore-blacks" || f.Name == "ignore-whites" || f.Name == "hard-cut" {
			options.PreviewHistogram.Enabled = true
		}
	})
	values, err := analyzePreviewEVSequence(frames, options)
	if err != nil {
		return reportError(stderr, err)
	}
	type change struct {
		frame  Frame
		packet *XMPPacket
	}
	changes := make([]change, len(frames))
	fmt.Fprintf(stdout, "Sequence: %s\nFrames: %d\n", set.Arg(0), len(frames))
	for index, frame := range frames {
		storedValue := values[index] * 4
		fmt.Fprintf(stdout, "  %-42s PreviewEV=%s  lrt:ExternalExposureDefault=%s\n", filepath.Base(frame.Image), formatNumber(values[index]), formatNumber(storedValue))
		changes[index] = change{frame: frame}
		if *write {
			packet, err := PacketForFrame(frame)
			if err != nil {
				return reportError(stderr, fmt.Errorf("read XMP for %s: %w", frame.Image, err))
			}
			if err := packet.SetScalar("lrt:ExternalExposureDefault", formatNumber(storedValue)); err != nil {
				return reportError(stderr, err)
			}
			changes[index].packet = packet
		}
	}
	if !*write {
		fmt.Fprintln(stdout, "Preview only. Pass --write to update XMP sidecars.")
		return 0
	}
	for _, change := range changes {
		backup, err := WritePacket(change.frame.OutputSidecar(), change.packet, !*noBackup)
		if err != nil {
			return reportError(stderr, fmt.Errorf("save XMP for %s: %w", change.frame.Image, err))
		}
		if backup != "" {
			fmt.Fprintf(stdout, "  Backup: %s\n", backup)
		}
		fmt.Fprintf(stdout, "  Saved: %s\n", change.frame.OutputSidecar())
	}
	fmt.Fprintf(stdout, "Updated %d sidecar(s).\n", len(changes))
	return 0
}
