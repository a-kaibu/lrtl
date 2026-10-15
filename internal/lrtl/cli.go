package lrtl

import (
	"errors"
	"flag"
	"fmt"
	"image"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const version = "0.9.0"

func flagFirst(arguments []string, valueFlags map[string]bool) []string {
	flags := make([]string, 0, len(arguments))
	operands := make([]string, 0, len(arguments))
	for index := 0; index < len(arguments); index++ {
		argument := arguments[index]
		if argument == "--" {
			operands = append(operands, arguments[index+1:]...)
			break
		}
		if !strings.HasPrefix(argument, "-") || argument == "-" {
			operands = append(operands, argument)
			continue
		}
		flags = append(flags, argument)
		key := argument
		if equals := strings.IndexByte(argument, '='); equals >= 0 {
			key = argument[:equals]
		}
		if valueFlags[key] && !strings.Contains(argument, "=") && index+1 < len(arguments) {
			index++
			flags = append(flags, arguments[index])
		}
	}
	return append(flags, operands...)
}

func Main(arguments []string, stdout, stderr io.Writer) int {
	if len(arguments) == 0 {
		printUsage(stderr)
		return 2
	}
	if arguments[0] == "--version" || arguments[0] == "-version" {
		fmt.Fprintf(stdout, "lrtl %s\n", version)
		return 0
	}
	switch arguments[0] {
	case "help", "--help", "-h":
		printUsage(stdout)
		return 0
	case "scan":
		if len(arguments) == 2 && (arguments[1] == "--help" || arguments[1] == "-h") {
			fmt.Fprintln(stdout, "Usage: lrtl scan DIRECTORY")
			return 0
		}
		if len(arguments) != 2 {
			fmt.Fprintln(stderr, "Usage: lrtl scan DIRECTORY")
			return 2
		}
		return runScan(arguments[1], stdout, stderr)
	case "mark":
		return runMark(arguments[1:], stdout, stderr)
	case "set":
		return runSet(arguments[1:], stdout, stderr)
	case "unmark":
		return runUnmark(arguments[1:], stdout, stderr)
	case "interpolate":
		return runInterpolate(arguments[1:], stdout, stderr)
	case "render":
		return runRender(arguments[1:], stdout, stderr)
	case "keyframes":
		return runKeyframes(arguments[1:], stdout, stderr)
	case "holy-grail":
		return runHolyGrail(arguments[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "lrtl: unknown command %q\n", arguments[0])
		printUsage(stderr)
		return 2
	}
}

func printUsage(writer io.Writer) {
	fmt.Fprintln(writer, "Usage: lrtl [--version] COMMAND")
	fmt.Fprintln(writer)
	fmt.Fprintln(writer, "Commands:")
	fmt.Fprintln(writer, "  scan DIRECTORY                         list images, sidecars, and keyframes")
	fmt.Fprintln(writer, "  mark IMAGE [--kind default|crop]       mark an XMP keyframe")
	fmt.Fprintln(writer, "  set IMAGE CRS_PROPERTY VALUE           set a numeric Camera Raw XMP value")
	fmt.Fprintln(writer, "  unmark IMAGE [--force-rating]          remove a keyframe mark")
	fmt.Fprintln(writer, "  interpolate DIRECTORY [--write]        preview or write keyframe transitions")
	fmt.Fprintln(writer, "  render DIRECTORY --output FILE.avi    render JPEG/PNG/TIFF frames to MJPEG AVI")
	fmt.Fprintln(writer, "  keyframes DIRECTORY [--count N] [--evenly-spaced]  suggest keyframes")
	fmt.Fprintln(writer, "  holy-grail DIRECTORY [--write]          compensate camera exposure changes from EXIF")
}

func newFlagSet(name string, stderr io.Writer) *flag.FlagSet {
	set := flag.NewFlagSet(name, flag.ContinueOnError)
	set.SetOutput(stderr)
	return set
}

func finishFlagParse(set *flag.FlagSet, err error) (bool, int) {
	if err == nil {
		return false, 0
	}
	if errors.Is(err, flag.ErrHelp) {
		return true, 0
	}
	return true, 2
}

func runScan(directory string, stdout, stderr io.Writer) int {
	frames, err := DiscoverSequence(directory)
	if err != nil {
		return reportError(stderr, err)
	}
	fmt.Fprintf(stdout, "Sequence: %s\n", directory)
	fmt.Fprintf(stdout, "Frames: %d\n", len(frames))
	if len(frames) == 0 {
		return 0
	}
	fmt.Fprintln(stdout, "#    image                                      XMP   mark   numeric controls")
	for _, frame := range frames {
		state, marker, controls := "missing", "-", 0
		packet, err := loadPacket(frame)
		if err != nil {
			state, marker = "invalid", err.Error()
		} else if packet != nil {
			state = "yes"
			marker = packet.KeyframeKind()
			if marker == "" {
				if hgKind, found, _ := packet.GetScalar(holyGrailKeyframeProperty); found {
					marker = "HG-" + hgKind
				} else if rating, valid := packet.Rating(); valid && (rating == 2 || rating == 3) {
					marker = fmt.Sprintf("HG-%d", rating)
				} else {
					marker = "-"
				}
			}
			for _, name := range NumericCRSProperties {
				if _, found, _ := packet.GetScalar("crs:" + name); found {
					controls++
				}
			}
		}
		fmt.Fprintf(stdout, "%-4d %-42s %-6s %-6s %d\n",
			frame.Index+1, filepath.Base(frame.Image), state, marker, controls)
	}
	return 0
}

func inputPaths(imageOrSidecar string) (image, sidecar string) {
	if strings.HasSuffix(strings.ToLower(imageOrSidecar), ".xmp") {
		return strings.TrimSuffix(imageOrSidecar, imageOrSidecar[len(imageOrSidecar)-4:]), imageOrSidecar
	}
	image = imageOrSidecar
	sidecar = image + ".xmp"
	if _, err := osStat(sidecar); err != nil {
		alternate := strings.TrimSuffix(image, filepath.Ext(image)) + ".xmp"
		if _, altErr := osStat(alternate); altErr == nil {
			sidecar = alternate
		}
	}
	return image, sidecar
}

func osStat(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	return info.Mode().IsRegular(), nil
}

func parseOnePath(set *flag.FlagSet, arguments []string) (string, error) {
	if err := set.Parse(arguments); err != nil {
		return "", err
	}
	if set.NArg() != 1 {
		return "", fmt.Errorf("expected one image or sidecar path")
	}
	return set.Arg(0), nil
}

func runMark(arguments []string, stdout, stderr io.Writer) int {
	set := newFlagSet("mark", stderr)
	kind := set.String("kind", "default", "keyframe kind: default or crop")
	dryRun := set.Bool("dry-run", false, "show the change without writing")
	noBackup := set.Bool("no-backup", false, "do not create a backup")
	path, err := parseOnePath(set, flagFirst(arguments, map[string]bool{"--kind": true, "-kind": true}))
	if err != nil {
		if done, code := finishFlagParse(set, err); done {
			return code
		}
		return reportError(stderr, err)
	}
	if *kind != "default" && *kind != "crop" {
		return reportError(stderr, fmt.Errorf("unsupported keyframe kind: %s", *kind))
	}
	image, sidecar := inputPaths(path)
	imageExists, imageErr := osStat(image)
	sidecarExists, sidecarErr := osStat(sidecar)
	if imageErr != nil && sidecarErr != nil {
		return reportError(stderr, imageErr)
	}
	if !imageExists && !sidecarExists {
		return reportError(stderr, fmt.Errorf("image and XMP sidecar do not exist: %s", image))
	}
	packet, err := PacketForSidecar(sidecar)
	if err != nil {
		return reportError(stderr, err)
	}
	rating := 4
	if *kind == "crop" {
		rating = 5
	}
	fmt.Fprintf(stdout, "%s: set %s keyframe, xmp:Rating=%d\n", filepath.Base(image), *kind, rating)
	if *dryRun {
		fmt.Fprintf(stdout, "Dry run. Would write %s\n", sidecar)
		return 0
	}
	if _, err := MarkKeyframe(packet, *kind); err != nil {
		return reportError(stderr, err)
	}
	backup, err := WritePacket(sidecar, packet, !*noBackup)
	if err != nil {
		return reportError(stderr, err)
	}
	if backup != "" {
		fmt.Fprintf(stdout, "Backup: %s\n", backup)
	}
	fmt.Fprintf(stdout, "Saved: %s\n", sidecar)
	return 0
}

func runSet(arguments []string, stdout, stderr io.Writer) int {
	if len(arguments) == 1 && (arguments[0] == "--help" || arguments[0] == "-h") {
		fmt.Fprintln(stdout, "Usage: lrtl set IMAGE_OR_XMP CRS_PROPERTY VALUE [--dry-run] [--no-backup]")
		return 0
	}
	dryRun, noBackup := false, false
	operands := make([]string, 0, 3)
	for index := 0; index < len(arguments); index++ {
		argument := arguments[index]
		if argument == "--" {
			operands = append(operands, arguments[index+1:]...)
			break
		}
		switch argument {
		case "--dry-run":
			dryRun = true
		case "--no-backup":
			noBackup = true
		default:
			if strings.HasPrefix(argument, "-") {
				if _, err := strconv.ParseFloat(argument, 64); err != nil {
					return reportError(stderr, fmt.Errorf("unknown set option %q", argument))
				}
			}
			operands = append(operands, argument)
		}
	}
	if len(operands) != 3 {
		return reportError(stderr, fmt.Errorf("expected IMAGE_OR_XMP, CRS_PROPERTY, and VALUE"))
	}
	image, sidecar := inputPaths(operands[0])
	imageExists, imageErr := osStat(image)
	sidecarExists, sidecarErr := osStat(sidecar)
	if imageErr != nil && sidecarErr != nil {
		return reportError(stderr, imageErr)
	}
	if !imageExists && !sidecarExists {
		return reportError(stderr, fmt.Errorf("image and XMP sidecar do not exist: %s", image))
	}
	property := strings.TrimPrefix(operands[1], "crs:")
	if strings.Contains(property, ":") {
		return reportError(stderr, fmt.Errorf("property must be a numeric Camera Raw control, with optional crs: prefix: %s", operands[1]))
	}
	allowed := false
	for _, candidate := range NumericCRSProperties {
		if property == candidate {
			allowed = true
			break
		}
	}
	if !allowed {
		return reportError(stderr, fmt.Errorf("unsupported numeric Camera Raw property: %s", operands[1]))
	}
	value, err := strconv.ParseFloat(operands[2], 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return reportError(stderr, fmt.Errorf("value must be a finite number: %s", operands[2]))
	}
	packet, err := PacketForSidecar(sidecar)
	if err != nil {
		return reportError(stderr, err)
	}
	property = "crs:" + property
	if err := packet.SetScalar(property, formatNumber(value)); err != nil {
		return reportError(stderr, err)
	}
	fmt.Fprintf(stdout, "%s: set %s=%s\n", filepath.Base(image), property, formatNumber(value))
	if dryRun {
		fmt.Fprintf(stdout, "Dry run. Would write %s\n", sidecar)
		return 0
	}
	backup, err := WritePacket(sidecar, packet, !noBackup)
	if err != nil {
		return reportError(stderr, err)
	}
	if backup != "" {
		fmt.Fprintf(stdout, "Backup: %s\n", backup)
	}
	fmt.Fprintf(stdout, "Saved: %s\n", sidecar)
	return 0
}

func runUnmark(arguments []string, stdout, stderr io.Writer) int {
	set := newFlagSet("unmark", stderr)
	forceRating := set.Bool("force-rating", false, "clear an existing 4- or 5-star rating")
	dryRun := set.Bool("dry-run", false, "show the change without writing")
	noBackup := set.Bool("no-backup", false, "do not create a backup")
	path, err := parseOnePath(set, flagFirst(arguments, nil))
	if err != nil {
		if done, code := finishFlagParse(set, err); done {
			return code
		}
		return reportError(stderr, err)
	}
	image, sidecar := inputPaths(path)
	if _, err := osStat(sidecar); err != nil {
		return reportError(stderr, fmt.Errorf("XMP sidecar does not exist: %s", sidecar))
	}
	packet, err := PacketForSidecar(sidecar)
	if err != nil {
		return reportError(stderr, err)
	}
	marked, err := packet.HasProperty("ol:KeyframeKind")
	if err != nil {
		return reportError(stderr, err)
	}
	if !marked {
		if !*forceRating {
			return reportError(stderr, fmt.Errorf("this sidecar has no lrtl marker; use --force-rating to clear an existing 4- or 5-star rating"))
		}
		if rating, valid := packet.Rating(); !valid || (rating != 4 && rating != 5) {
			return reportError(stderr, fmt.Errorf("this sidecar has no 4- or 5-star keyframe rating"))
		}
	}
	fmt.Fprintf(stdout, "%s: remove keyframe mark\n", filepath.Base(image))
	if *dryRun {
		fmt.Fprintf(stdout, "Dry run. Would write %s\n", sidecar)
		return 0
	}
	if err := UnmarkKeyframe(packet, *forceRating); err != nil {
		return reportError(stderr, err)
	}
	backup, err := WritePacket(sidecar, packet, !*noBackup)
	if err != nil {
		return reportError(stderr, err)
	}
	if backup != "" {
		fmt.Fprintf(stdout, "Backup: %s\n", backup)
	}
	fmt.Fprintf(stdout, "Saved: %s\n", sidecar)
	return 0
}

func runInterpolate(arguments []string, stdout, stderr io.Writer) int {
	set := newFlagSet("interpolate", stderr)
	write := set.Bool("write", false, "write interpolated sidecar values")
	noBackup := set.Bool("no-backup", false, "do not create a backup")
	if err := set.Parse(flagFirst(arguments, nil)); err != nil {
		if done, code := finishFlagParse(set, err); done {
			return code
		}
	}
	if set.NArg() != 1 {
		return reportError(stderr, fmt.Errorf("expected one sequence directory"))
	}
	directory := set.Arg(0)
	frames, err := DiscoverSequence(directory)
	if err != nil {
		return reportError(stderr, err)
	}
	plan, err := buildPlan(frames)
	if err != nil {
		return reportError(stderr, err)
	}
	keyframeNames := make([]string, 0, len(plan.KeyframeIndices))
	for _, index := range plan.KeyframeIndices {
		keyframeNames = append(keyframeNames, filepath.Base(frames[index].Image))
	}
	fmt.Fprintf(stdout, "Sequence: %s\n", directory)
	fmt.Fprintf(stdout, "Frames: %d; effective keyframes: %d\n", plan.FrameCount, len(keyframeNames))
	anchors := strings.Join(keyframeNames, ", ")
	if anchors == "" {
		anchors = "none"
	}
	fmt.Fprintf(stdout, "Anchors: %s\n", anchors)
	fmt.Fprintf(stdout, "Proposed frames: %d; numeric values: %d\n", len(plan.Changes), plan.PropertyCount())
	if !*write {
		fmt.Fprintln(stdout, "Dry run. Pass --write to update sidecar XMP files.")
		for _, change := range plan.Changes {
			names := make([]string, 0, len(change.Values))
			for name := range change.Values {
				names = append(names, name)
			}
			sort.Strings(names)
			fmt.Fprintf(stdout, "  %s: %s\n", filepath.Base(change.Frame.Image), strings.Join(names, ", "))
		}
		return 0
	}
	outputs, err := applyPlan(plan)
	if err != nil {
		return reportError(stderr, err)
	}
	written := 0
	for _, output := range outputs {
		backup, err := WritePacket(output.Path, output.Packet, !*noBackup)
		if err != nil {
			return reportError(stderr, err)
		}
		if backup != "" {
			fmt.Fprintf(stdout, "Backup: %s\n", backup)
		}
		fmt.Fprintf(stdout, "Saved: %s\n", output.Path)
		written++
	}
	fmt.Fprintf(stdout, "Updated %d sidecar(s).\n", written)
	return 0
}

func runRender(arguments []string, stdout, stderr io.Writer) int {
	set := newFlagSet("render", stderr)
	output := set.String("output", "", "output MJPEG AVI path")
	fps := set.Int("fps", 24, "integer frame rate (1-240)")
	quality := set.Int("quality", 90, "JPEG quality for PNG/TIFF and deflickered frames (1-100)")
	overwrite := set.Bool("overwrite", false, "replace an existing output file")
	deflicker := set.Bool("deflicker", false, "apply global luminance deflicker while rendering")
	referenceValue := set.String("reference", "", "reference area x,y,width,height; defaults to the full frame")
	smoothRadius := set.Int("smooth", 5, "deflicker smoothing radius in frames")
	passes := set.Int("passes", 1, "deflicker passes (1-8)")
	constant := set.Bool("constant", false, "deflicker toward constant luminance")
	applyXMPExposure := set.Bool("apply-xmp-exposure", false, "apply crs:Exposure2012 from sidecar XMP to raster frames")
	parseArgs := flagFirst(arguments, map[string]bool{
		"--output": true, "-output": true,
		"--fps": true, "-fps": true,
		"--quality": true, "-quality": true,
		"--reference": true, "-reference": true,
		"--smooth": true, "-smooth": true,
		"--passes": true, "-passes": true,
	})
	if err := set.Parse(parseArgs); err != nil {
		if done, code := finishFlagParse(set, err); done {
			return code
		}
	}
	if set.NArg() != 1 {
		return reportError(stderr, fmt.Errorf("expected one sequence directory"))
	}
	if *output == "" {
		return reportError(stderr, fmt.Errorf("--output is required"))
	}
	var reference image.Rectangle
	if *referenceValue != "" {
		var err error
		reference, err = parseReferenceArea(*referenceValue)
		if err != nil {
			return reportError(stderr, err)
		}
	}
	deflickerEnabled := *deflicker
	set.Visit(func(parsed *flag.Flag) {
		switch parsed.Name {
		case "reference", "smooth", "passes", "constant":
			deflickerEnabled = true
		}
	})
	directory := set.Arg(0)
	frames, err := DiscoverSequence(directory)
	if err != nil {
		return reportError(stderr, err)
	}
	options := RenderOptions{
		FPS:              *fps,
		Quality:          *quality,
		Overwrite:        *overwrite,
		Deflicker:        deflickerEnabled,
		Reference:        reference,
		SmoothRadius:     *smoothRadius,
		DeflickerPasses:  *passes,
		Constant:         *constant,
		ApplyXMPExposure: *applyXMPExposure,
	}
	if err := RenderSequence(frames, *output, options); err != nil {
		return reportError(stderr, err)
	}
	if *applyXMPExposure {
		fmt.Fprintf(stdout, "Rendered %d frames at %d fps with XMP Exposure2012: %s\n", len(frames), *fps, *output)
	} else {
		fmt.Fprintf(stdout, "Rendered %d frames at %d fps: %s\n", len(frames), *fps, *output)
	}
	return 0
}

func runKeyframes(arguments []string, stdout, stderr io.Writer) int {
	set := newFlagSet("keyframes", stderr)
	count := set.Int("count", 5, "number of suggested keyframes (default capped to frame count)")
	smoothRadius := set.Int("smooth", 5, "luminance smoothing radius in frames")
	evenlySpaced := set.Bool("evenly-spaced", false, "suggest uniformly spaced interior keyframes without decoding images")
	write := set.Bool("write", false, "mark the suggested frames in XMP sidecars")
	noBackup := set.Bool("no-backup", false, "do not create backups when writing")
	if err := set.Parse(flagFirst(arguments, map[string]bool{"--count": true, "-count": true, "--smooth": true, "-smooth": true})); err != nil {
		if done, code := finishFlagParse(set, err); done {
			return code
		}
	}
	if set.NArg() != 1 {
		return reportError(stderr, fmt.Errorf("expected one sequence directory"))
	}
	countSpecified := false
	set.Visit(func(parsed *flag.Flag) {
		if parsed.Name == "count" {
			countSpecified = true
		}
	})
	directory := set.Arg(0)
	frames, err := DiscoverSequence(directory)
	if err != nil {
		return reportError(stderr, err)
	}
	if *smoothRadius < 0 || *smoothRadius > 1000 {
		return reportError(stderr, fmt.Errorf("smoothing radius must be between 0 and 1000 frames: %d", *smoothRadius))
	}
	var curve []float64
	if !*evenlySpaced {
		curve, err = analyzeLuminanceSequence(frames)
		if err != nil {
			return reportError(stderr, err)
		}
	}
	frameCount := len(frames)
	if frameCount < 3 {
		return reportError(stderr, fmt.Errorf("keyframe suggestions require at least one interior frame between the sequence endpoints"))
	}
	packets := make([]*XMPPacket, frameCount)
	existingKeyframes := make([]int, 0)
	selectionAnchors := make([]int, 0)
	for index, frame := range frames {
		packet, err := PacketForSidecar(frame.OutputSidecar())
		if err != nil {
			return reportError(stderr, fmt.Errorf("%s: %w", frame.OutputSidecar(), err))
		}
		packets[index] = packet
		if packet.IsKeyframe() {
			existingKeyframes = append(existingKeyframes, index)
			selectionAnchors = append(selectionAnchors, index)
			continue
		}
		if _, found, err := packet.GetScalar(holyGrailKeyframeProperty); err != nil {
			return reportError(stderr, fmt.Errorf("%s: %w", frame.OutputSidecar(), err))
		} else if found {
			selectionAnchors = append(selectionAnchors, index)
			continue
		}
		if rating, valid := packet.Rating(); valid && (rating == 2 || rating == 3) {
			selectionAnchors = append(selectionAnchors, index)
		}
	}
	availableSuggestions := frameCount - 2
	for _, index := range selectionAnchors {
		if index > 0 && index < frameCount-1 {
			availableSuggestions--
		}
	}
	if availableSuggestions < 0 {
		availableSuggestions = 0
	}
	suggestionCount := *count
	if !countSpecified {
		suggestionCount = min(suggestionCount, availableSuggestions)
	}
	if suggestionCount == 0 {
		if countSpecified {
			if availableSuggestions == 0 {
				return reportError(stderr, fmt.Errorf("no unmarked interior frames are available for new keyframes"))
			}
			return reportError(stderr, fmt.Errorf("new keyframe count must be at least 1"))
		}
		fmt.Fprintf(stdout, "Sequence: %s\n", directory)
		fmt.Fprintf(stdout, "Frames: %d; existing keyframes: %d; no new keyframes suggested\n", frameCount, len(existingKeyframes))
		return 0
	}
	var indices []int
	if *evenlySpaced {
		indices, err = selectEvenlySpacedKeyframeIndicesWithAnchors(frameCount, suggestionCount, selectionAnchors)
	} else {
		indices, err = selectKeyframeIndicesWithAnchors(curve, suggestionCount, *smoothRadius, selectionAnchors)
	}
	if err != nil {
		return reportError(stderr, err)
	}
	markedPackets := make(map[int]*XMPPacket)
	if *write {
		for _, index := range indices {
			frame := frames[index]
			packet := packets[index]
			if _, err := MarkKeyframe(packet, "default"); err != nil {
				return reportError(stderr, fmt.Errorf("%s: %w", frame.OutputSidecar(), err))
			}
			markedPackets[index] = packet
		}
	}
	fmt.Fprintf(stdout, "Sequence: %s\n", directory)
	fmt.Fprintf(stdout, "Frames: %d; existing keyframes: %d; new suggestions: %d\n", len(frames), len(existingKeyframes), len(indices))
	for _, index := range indices {
		frame := frames[index]
		if *evenlySpaced {
			fmt.Fprintf(stdout, "%4d %-42s evenly spaced\n", index+1, filepath.Base(frame.Image))
		} else {
			fmt.Fprintf(stdout, "%4d %-42s luminance=%.6f\n", index+1, filepath.Base(frame.Image), math.Exp2(curve[index]))
		}
		if !*write {
			continue
		}
		backup, err := WritePacket(frame.OutputSidecar(), markedPackets[index], !*noBackup)
		if err != nil {
			return reportError(stderr, err)
		}
		if backup != "" {
			fmt.Fprintf(stdout, "  Backup: %s\n", backup)
		}
		fmt.Fprintf(stdout, "  Marked: %s (4 stars)\n", frame.OutputSidecar())
	}
	if !*write {
		fmt.Fprintln(stdout, "Preview only. Pass --write to mark these frames; other existing keyframes are left unchanged.")
	}
	return 0
}

func runHolyGrail(arguments []string, stdout, stderr io.Writer) int {
	set := newFlagSet("holy-grail", stderr)
	write := set.Bool("write", false, "write exposure compensation and 2*/3* jump keyframes to XMP sidecars")
	noBackup := set.Bool("no-backup", false, "do not create backups when writing")
	ignoreAperture := set.Bool("ignore-aperture", false, "ignore FNumber when aperture remained fixed")
	forceMarkJumps := set.Bool("mark-jumps", false, "force 2*/3* marks when EXIF does not confirm Manual mode")
	noMarkJumps := set.Bool("no-mark-jumps", false, "do not mark camera-setting changes as 2*/3* keyframe pairs")
	stretch := set.Float64("stretch", 0, "add LRT-style normalized Holy Grail curve stretch (-1.5 to +1.5)")
	rotate := set.Float64("rotate", 0, "add LRT-style linear Holy Grail curve rotation (-4 to +4)")
	autoShape := set.Bool("auto-shape", false, "search curve stretch and rotation using L1 smoothness scores")
	reset := set.Bool("reset", false, "remove lrtl-managed Holy Grail compensation")
	if err := set.Parse(flagFirst(arguments, map[string]bool{
		"--stretch": true, "-stretch": true,
		"--rotate": true, "-rotate": true,
	})); err != nil {
		if done, code := finishFlagParse(set, err); done {
			return code
		}
	}
	if set.NArg() != 1 {
		return reportError(stderr, fmt.Errorf("expected one sequence directory"))
	}
	if *forceMarkJumps && *noMarkJumps {
		return reportError(stderr, fmt.Errorf("--mark-jumps and --no-mark-jumps cannot be used together"))
	}
	explicitCurveOptions := false
	set.Visit(func(parsed *flag.Flag) {
		if parsed.Name == "stretch" || parsed.Name == "rotate" {
			explicitCurveOptions = true
		}
	})
	if *autoShape && explicitCurveOptions {
		return reportError(stderr, fmt.Errorf("--auto-shape cannot be combined with --stretch or --rotate"))
	}
	useCurveOptions := *autoShape || explicitCurveOptions
	if useCurveOptions && (math.IsNaN(*stretch) || math.IsInf(*stretch, 0) || math.IsNaN(*rotate) || math.IsInf(*rotate, 0) || *stretch < -1.5 || *stretch > 1.5 || *rotate < -4 || *rotate > 4) {
		return reportError(stderr, fmt.Errorf("--stretch must be between -1.5 and +1.5 and --rotate between -4 and +4"))
	}
	if useCurveOptions && *reset {
		return reportError(stderr, fmt.Errorf("--stretch and --rotate cannot be used with --reset"))
	}
	directory := set.Arg(0)
	frames, err := DiscoverSequence(directory)
	if err != nil {
		return reportError(stderr, err)
	}
	var curveOptions *HolyGrailCurveOptions
	if useCurveOptions {
		curveOptions = &HolyGrailCurveOptions{Stretch: *stretch, Rotate: *rotate, Auto: *autoShape}
	}
	plan, err := buildHolyGrailPlanWithCurveOptions(frames, *ignoreAperture, *reset, *forceMarkJumps, *noMarkJumps, curveOptions)
	if err != nil {
		return reportError(stderr, err)
	}
	changes := plan.Changes
	fmt.Fprintf(stdout, "Sequence: %s\n", directory)
	if plan.CurveOptions != nil {
		fmt.Fprintf(stdout, "Curve adjustments: stretch=%+.2f rotate=%+.2f\n", plan.CurveOptions.Stretch, plan.CurveOptions.Rotate)
		if plan.AutoCurve {
			fmt.Fprintln(stdout, "Curve adjustments selected automatically with the LRT-style L1/LOWESS approximation.")
		}
	}
	correctionUpdates := 0
	jumpMarks := 0
	for _, change := range changes {
		if change.ExposureChanged {
			correctionUpdates++
		}
		if change.KeyframeRating == 2 || change.KeyframeRating == 3 || change.RestoreKeyframe {
			jumpMarks++
		}
	}
	if *reset {
		fmt.Fprintf(stdout, "Frames: %d; frames to reset: %d\n", len(frames), len(changes))
	} else {
		fmt.Fprintf(stdout, "Frames: %d; exposure corrections: %d\n", len(frames), correctionUpdates)
	}
	if !*reset && plan.MarkJumps {
		fmt.Fprintf(stdout, "HG jump keyframe marks: %d\n", jumpMarks)
	} else if !*reset && !*noMarkJumps && !plan.ManualMode && !*forceMarkJumps {
		fmt.Fprintln(stdout, "EXIF does not confirm Manual mode; 2*/3* marks are omitted. Pass --mark-jumps to force them.")
	}
	for _, change := range changes {
		if *reset {
			fmt.Fprintf(stdout, "  %-42s", filepath.Base(change.Frame.Image))
			if change.ExposureChanged {
				fmt.Fprintf(stdout, " Exposure2012 %+.4f -> %+.4f EV", change.ExposureBefore, change.ExposureAfter)
			}
			if change.RestoreKeyframe {
				fmt.Fprint(stdout, " restore HG rating")
			}
			fmt.Fprintln(stdout)
		} else {
			fmt.Fprintf(stdout, "  %-42s shutter=%.6g s f/%.4g ISO=%.4g",
				filepath.Base(change.Frame.Image), change.Camera.ExposureTime,
				change.Camera.FNumber, change.Camera.ISO)
			if change.ExposureChanged {
				fmt.Fprintf(stdout, " correction=%+.4f EV Exposure2012=%+.4f EV", change.Compensation, change.ExposureAfter)
			}
			if change.KeyframeRating != 0 {
				fmt.Fprintf(stdout, " HG keyframe=%d*", change.KeyframeRating)
			}
			fmt.Fprintln(stdout)
		}
	}
	if len(changes) == 0 {
		if *reset {
			fmt.Fprintln(stdout, "No lrtl-managed Holy Grail corrections to reset.")
		} else {
			fmt.Fprintln(stdout, "No exposure-setting compensation is needed.")
		}
		return 0
	}
	if !*write {
		fmt.Fprintln(stdout, "Preview only. Pass --write to update sidecar XMP files.")
		return 0
	}
	if err := applyHolyGrailPlan(changes); err != nil {
		return reportError(stderr, err)
	}
	for _, change := range changes {
		path := change.Frame.OutputSidecar()
		backup, err := WritePacket(path, change.Packet, !*noBackup)
		if err != nil {
			return reportError(stderr, err)
		}
		if backup != "" {
			fmt.Fprintf(stdout, "Backup: %s\n", backup)
		}
		fmt.Fprintf(stdout, "Saved: %s\n", path)
	}
	fmt.Fprintf(stdout, "Updated %d sidecar(s).\n", len(changes))
	return 0
}

func reportError(stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "lrtl: error: %v\n", err)
	return 2
}
