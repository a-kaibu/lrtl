package lrtl

import (
	"bytes"
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
		values[index], err = framePreviewEV(adjustments.apply(index, img))
		if err != nil {
			return nil, fmt.Errorf("analyze %s: %w", frame.Image, err)
		}
	}
	return values, nil
}

func framePreviewEV(img image.Image) (float64, error) {
	bounds := img.Bounds()
	if bounds.Empty() {
		return 0, fmt.Errorf("empty image")
	}
	var histogram [3][256]uint64
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			pixel := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			histogram[0][pixel.R]++
			histogram[1][pixel.G]++
			histogram[2][pixel.B]++
		}
	}

	var weighted, count uint64
	for channel := range histogram {
		var smoothed [256]uint64
		for bin := range smoothed {
			first, last := max(0, bin-2), min(255, bin+2)
			var sum uint64
			for sample := first; sample <= last; sample++ {
				sum += histogram[channel][sample]
			}
			smoothed[bin] = sum / uint64(last-first+1)
		}
		for bin, occurrences := range smoothed {
			weighted += uint64(bin) * occurrences
			count += occurrences
		}
	}
	if count == 0 {
		// LRTimelapse returns 255 when its ROI contains no pixels.
		return 255.0 / 256.0, nil
	}
	value := float64(weighted) / float64(count) / 256.0
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, fmt.Errorf("non-finite Preview EV")
	}
	return value, nil
}

func runPreviewEV(arguments []string, stdout, stderr io.Writer) int {
	set := newFlagSet("preview-ev", stderr)
	rawPreview := set.Bool("raw-preview", false, "analyze embedded previews from supported RAW files")
	rawDevelop := set.Bool("raw-develop", false, "analyze RAW files developed by DNGLab")
	dnglab := set.String("dnglab", "", "DNGLab executable for --raw-develop; defaults to PATH")
	applyXMPExposure := set.Bool("apply-xmp-exposure", false, "apply crs:Exposure2012 from sidecar XMP during analysis")
	applyXMPBasic := set.Bool("apply-xmp-basic", false, "apply supported Camera Raw tone and color controls during analysis")
	applyXMPCrop := set.Bool("apply-xmp-crop", false, "apply rotated crop rectangles from sidecar XMP during analysis")
	write := set.Bool("write", false, "write lrt:ExternalExposureDefault to XMP sidecars")
	noBackup := set.Bool("no-backup", false, "do not create backups when writing")
	if err := set.Parse(flagFirst(arguments, map[string]bool{"--dnglab": true, "-dnglab": true})); err != nil {
		if done, code := finishFlagParse(set, err); done {
			fmt.Fprintln(stdout, "Usage: lrtl preview-ev DIRECTORY [--raw-preview|--raw-develop] [--dnglab PATH] [--apply-xmp-exposure|--apply-xmp-basic] [--apply-xmp-crop] [--write] [--no-backup]")
			return code
		}
	}
	if set.NArg() != 1 {
		return reportError(stderr, fmt.Errorf("Usage: lrtl preview-ev DIRECTORY [--raw-preview|--raw-develop] [--dnglab PATH] [--apply-xmp-exposure|--apply-xmp-basic] [--apply-xmp-crop] [--write] [--no-backup]"))
	}
	if *noBackup && !*write {
		return reportError(stderr, fmt.Errorf("--no-backup requires --write"))
	}
	frames, err := DiscoverSequence(set.Arg(0))
	if err != nil {
		return reportError(stderr, err)
	}
	options := imageInputOptions{
		RawPreview: *rawPreview, RawDevelop: *rawDevelop, DNGLabPath: *dnglab,
		ApplyXMPExposure: *applyXMPExposure, ApplyXMPBasic: *applyXMPBasic,
		ApplyXMPCrop: *applyXMPCrop,
	}
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
