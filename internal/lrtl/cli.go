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
	case "import":
		return runImport(arguments[1:], stdout, stderr)
	case "import-video":
		return runImportVideo(arguments[1:], stdout, stderr)
	case "timeshift":
		return runTimeShift(arguments[1:], stdout, stderr)
	case "lens-tag":
		return runLensTag(arguments[1:], stdout, stderr)
	case "mark":
		return runMark(arguments[1:], stdout, stderr)
	case "set":
		return runSet(arguments[1:], stdout, stderr)
	case "unmark":
		return runUnmark(arguments[1:], stdout, stderr)
	case "interpolate":
		return runInterpolate(arguments[1:], stdout, stderr)
	case "init":
		return runInitialize(arguments[1:], stdout, stderr)
	case "masks":
		return runMasks(arguments[1:], stdout, stderr)
	case "crop":
		return runCrop(arguments[1:], stdout, stderr)
	case "render":
		return runRender(arguments[1:], stdout, stderr)
	case "batch-render":
		return runBatchRender(arguments[1:], stdout, stderr)
	case "subtitles":
		return runSubtitles(arguments[1:], stdout, stderr)
	case "long-term":
		return runLongTerm(arguments[1:], stdout, stderr)
	case "preview":
		return runPreview(arguments[1:], stdout, stderr)
	case "preview-ev":
		return runPreviewEV(arguments[1:], stdout, stderr)
	case "deflicker":
		return runDeflicker(arguments[1:], stdout, stderr)
	case "keyframes":
		return runKeyframes(arguments[1:], stdout, stderr)
	case "holy-grail":
		return runHolyGrail(arguments[1:], stdout, stderr)
	case "snapshot":
		return runSnapshot(arguments[1:], stdout, stderr)
	case "gui":
		return runGUI(arguments[1:], stdout, stderr)
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
	fmt.Fprintln(writer, "  import SOURCE --output DIRECTORY [--recursive] [--sort name|capture-time] [--skip-jpeg] [--rename-by-date] [--convert-dng] [--dng-compression lossless|uncompressed] [--dng-converter PATH|--dnglab PATH] [--dry-run]")
	fmt.Fprintln(writer, "  import-video VIDEO --output DIRECTORY [--fps N] [--format jpg|png] [--quality 1-31] [--ffmpeg PATH]")
	fmt.Fprintln(writer, "  timeshift DIRECTORY --offset DURATION [--write] [--no-backup]")
	fmt.Fprintln(writer, "  lens-tag DIRECTORY --aperture f/8 [--write] [--no-backup]")
	fmt.Fprintln(writer, "  mark IMAGE [--kind default|crop]       mark an XMP keyframe")
	fmt.Fprintln(writer, "  set IMAGE PROPERTY|MASK_PATH VALUE    edit global/local Camera Raw values or LRT Preview EV")
	fmt.Fprintln(writer, "  masks IMAGE_OR_XMP [--json]            inspect correction paths and mask values")
	fmt.Fprintln(writer, "  crop IMAGE_OR_XMP|DIRECTORY [--rect L,T,R,B] [--angle DEGREES] [--enable|--clear] [--mark] [--dry-run] [--no-backup] [--json]")
	fmt.Fprintln(writer, "  unmark IMAGE [--force-rating]          remove a keyframe mark")
	fmt.Fprintln(writer, "  interpolate DIRECTORY [--write]        preview or write keyframe transitions")
	fmt.Fprintln(writer, "  init DIRECTORY [--mask-format auto|modern|legacy] [--write] [--no-backup] initialize correction slots")
	fmt.Fprintln(writer, "  render DIRECTORY --output FILE.avi|mp4|mov [--selection FILTER.json] [--filename-contains TEXT] [--watermark TEXT] [--watermark-position POS] [--aspect W:H] [--output-height N|--long-edge N] [--audio FILE] [--auto-orient] [--apply-xmp-basic] [--motion-blur N]")
	fmt.Fprintln(writer, "  batch-render ROOT --output DIRECTORY [--format avi|mp4|mov] [--workers N] [--recursive] [-- RENDER_OPTIONS...]")
	fmt.Fprintln(writer, "  long-term DIRECTORY --output FILTER.json [--metric-profile lrtl|lrt-7.5.5] [--luminance-range MIN,MAX] [--contrast-range MIN,MAX] [--hue-range MIN,MAX] [--apply-xmp-basic|--apply-xmp-exposure] [--apply-xmp-crop]")
	fmt.Fprintln(writer, "  subtitles DIRECTORY --output FILE.srt export capture timestamps as timed subtitles")
	fmt.Fprintln(writer, "  preview DIRECTORY --output FILE.svg [--raw-develop] [--apply-xmp-basic|--apply-xmp-exposure] [--apply-xmp-crop]")
	fmt.Fprintln(writer, "  preview-ev DIRECTORY [--raw-preview|--raw-develop] [--dng-converter PATH|--dnglab PATH] [--apply-xmp-basic|--apply-xmp-exposure] [--apply-xmp-crop] [--write] [--no-backup]")
	fmt.Fprintln(writer, "  deflicker DIRECTORY [--write] [--smoother gaussian|lowess] [--raw-develop] [--apply-xmp-basic|--apply-xmp-exposure] [--apply-xmp-crop]")
	fmt.Fprintln(writer, "  keyframes DIRECTORY [--count N] [--rating 4|5] [--replace-existing] [--evenly-spaced] [--luminance-deviation] [--raw-develop] [--apply-xmp-basic|--apply-xmp-exposure] [--apply-xmp-crop]")
	fmt.Fprintln(writer, "  holy-grail DIRECTORY [--write]          compensate camera exposure changes from EXIF")
	fmt.Fprintln(writer, "  snapshot create DIRECTORY [--name NAME] save XMP sidecars to a ZIP snapshot")
	fmt.Fprintln(writer, "  snapshot list DIRECTORY                list saved XMP snapshots")
	fmt.Fprintln(writer, "  snapshot restore DIRECTORY FILE.zip [--write] [--no-backup] [--allow-moved]")
	fmt.Fprintln(writer, "  gui [--listen 127.0.0.1:8787]           open the local sequence and render UI")
}

func runImport(arguments []string, stdout, stderr io.Writer) int {
	set := newFlagSet("import", stderr)
	output := set.String("output", "", "destination image sequence directory")
	sortBy := set.String("sort", "name", "image order: name or capture-time")
	recursive := set.Bool("recursive", false, "include images in subdirectories")
	skipJPEG := set.Bool("skip-jpeg", false, "omit JPEG and JPG images (keep RAW and other formats)")
	renameByDate := set.Bool("rename-by-date", false, "rename images to capture time and sort by capture time")
	dryRun := set.Bool("dry-run", false, "show planned copies without writing files")
	convertDNG := set.Bool("convert-dng", false, "convert supported RAW files to DNG during import")
	dngCompression := set.String("dng-compression", "", "DNG compression for --convert-dng: lossless or uncompressed (default lossless)")
	dnglab := set.String("dnglab", "", "optional DNGLab executable for --convert-dng")
	dngConverter := set.String("dng-converter", "", "Adobe DNG Converter executable; defaults to the installed application")
	if err := set.Parse(flagFirst(arguments, map[string]bool{
		"--output": true, "-output": true,
		"--sort": true, "-sort": true,
		"--dng-compression": true, "-dng-compression": true,
		"--dnglab": true, "-dnglab": true, "--dng-converter": true, "-dng-converter": true,
	})); err != nil {
		if done, code := finishFlagParse(set, err); done {
			return code
		}
	}
	if set.NArg() != 1 || *output == "" {
		return reportError(stderr, fmt.Errorf("Usage: lrtl import SOURCE --output DIRECTORY [--recursive] [--sort name|capture-time] [--skip-jpeg] [--rename-by-date] [--convert-dng] [--dng-compression lossless|uncompressed] [--dng-converter PATH|--dnglab PATH] [--dry-run]"))
	}
	sortValue := strings.ToLower(strings.TrimSpace(*sortBy))
	sortWasSpecified := false
	set.Visit(func(parsed *flag.Flag) {
		if parsed.Name == "sort" {
			sortWasSpecified = true
		}
	})
	if sortValue != "name" && sortValue != "capture-time" {
		return reportError(stderr, fmt.Errorf("--sort must be name or capture-time: %q", *sortBy))
	}
	if *renameByDate && sortWasSpecified && sortValue != "capture-time" {
		return reportError(stderr, fmt.Errorf("--rename-by-date requires --sort capture-time"))
	}
	dnglabWasSpecified, dngCompressionWasSpecified := false, false
	set.Visit(func(parsed *flag.Flag) {
		if parsed.Name == "dnglab" {
			dnglabWasSpecified = true
		}
		if parsed.Name == "dng-compression" {
			dngCompressionWasSpecified = true
		}
	})
	if !*convertDNG && (dnglabWasSpecified || dngCompressionWasSpecified) {
		return reportError(stderr, fmt.Errorf("--dnglab and --dng-compression require --convert-dng"))
	}
	options := ImportOptions{
		Recursive: *recursive, SkipJPEG: *skipJPEG,
		SortByCapture: sortValue == "capture-time" || *renameByDate,
		RenameByDate:  *renameByDate, DryRun: *dryRun,
		ConvertToDNG: *convertDNG, DNGCompression: *dngCompression, DNGLabPath: *dnglab, DNGConverterPath: *dngConverter,
	}
	if err := ImportSequence(set.Arg(0), *output, options, stdout); err != nil {
		return reportError(stderr, err)
	}
	return 0
}

func runImportVideo(arguments []string, stdout, stderr io.Writer) int {
	set := newFlagSet("import-video", stderr)
	output := set.String("output", "", "destination image sequence directory")
	fps := set.Float64("fps", 0, "sample frames at this rate; default extracts every decoded frame")
	format := set.String("format", "jpg", "output image format: jpg or png")
	quality := set.Int("quality", 2, "JPEG quality for --format jpg (1 is best, 31 is worst)")
	ffmpeg := set.String("ffmpeg", "", "FFmpeg executable; defaults to PATH")
	if err := set.Parse(flagFirst(arguments, map[string]bool{
		"--output": true, "-output": true,
		"--fps": true, "-fps": true,
		"--format": true, "-format": true,
		"--quality": true, "-quality": true,
		"--ffmpeg": true, "-ffmpeg": true,
	})); err != nil {
		if done, code := finishFlagParse(set, err); done {
			return code
		}
	}
	if set.NArg() != 1 || *output == "" {
		return reportError(stderr, fmt.Errorf("Usage: lrtl import-video VIDEO --output DIRECTORY [--fps N] [--format jpg|png] [--quality 1-31] [--ffmpeg PATH]"))
	}
	options := VideoImportOptions{
		FFmpegPath: *ffmpeg, Format: *format, FPS: *fps, JPEGQuality: *quality,
	}
	if err := ImportVideo(set.Arg(0), *output, options, stdout); err != nil {
		return reportError(stderr, err)
	}
	return 0
}

func runTimeShift(arguments []string, stdout, stderr io.Writer) int {
	set := newFlagSet("timeshift", stderr)
	offsetValue := set.String("offset", "", "capture time shift, such as 2h30m, -45s, or 1d")
	write := set.Bool("write", false, "write shifted timestamps to XMP sidecars")
	noBackup := set.Bool("no-backup", false, "do not create backups when writing")
	if err := set.Parse(flagFirst(arguments, map[string]bool{
		"--offset": true, "-offset": true,
	})); err != nil {
		if done, code := finishFlagParse(set, err); done {
			return code
		}
	}
	if set.NArg() != 1 || strings.TrimSpace(*offsetValue) == "" {
		return reportError(stderr, fmt.Errorf("Usage: lrtl timeshift DIRECTORY --offset DURATION [--write] [--no-backup]"))
	}
	if *noBackup && !*write {
		return reportError(stderr, fmt.Errorf("--no-backup requires --write"))
	}
	offset, err := parseTimeShiftOffset(*offsetValue)
	if err != nil {
		return reportError(stderr, err)
	}
	if err := TimeShiftSequence(set.Arg(0), offset, *write, !*noBackup, stdout); err != nil {
		return reportError(stderr, err)
	}
	return 0
}

func runLensTag(arguments []string, stdout, stderr io.Writer) int {
	set := newFlagSet("lens-tag", stderr)
	aperture := set.String("aperture", "", "LRT Lens Tagger aperture stop, such as f/8")
	write := set.Bool("write", false, "write lrt:Aperture to every frame's XMP sidecar")
	noBackup := set.Bool("no-backup", false, "do not create backups when writing")
	if err := set.Parse(flagFirst(arguments, map[string]bool{
		"--aperture": true, "-aperture": true,
	})); err != nil {
		if done, code := finishFlagParse(set, err); done {
			return code
		}
	}
	if set.NArg() != 1 || strings.TrimSpace(*aperture) == "" {
		return reportError(stderr, fmt.Errorf("Usage: lrtl lens-tag DIRECTORY --aperture f/8 [--write] [--no-backup]"))
	}
	if *noBackup && !*write {
		return reportError(stderr, fmt.Errorf("--no-backup requires --write"))
	}
	canonical, err := normalizeLensAperture(*aperture)
	if err != nil {
		return reportError(stderr, err)
	}
	directory := set.Arg(0)
	frames, err := DiscoverSequence(directory)
	if err != nil {
		return reportError(stderr, err)
	}
	changes, err := buildLensTagPlan(frames, canonical)
	if err != nil {
		return reportError(stderr, err)
	}
	fmt.Fprintf(stdout, "Sequence: %s\nFrames: %d\nAperture: f/%s\n", directory, len(changes), canonical)
	for _, change := range changes {
		previous := "unset"
		if change.HadPrevious {
			previous = "f/" + change.Previous
		}
		fmt.Fprintf(stdout, "  %-42s %s -> f/%s\n", filepath.Base(change.Frame.Image), previous, canonical)
	}
	if !*write {
		fmt.Fprintln(stdout, "Preview only. Pass --write to update XMP sidecars.")
		return 0
	}
	for _, change := range changes {
		backup, err := WritePacket(change.Frame.OutputSidecar(), change.Packet, !*noBackup)
		if err != nil {
			return reportError(stderr, fmt.Errorf("%s: %w", change.Frame.OutputSidecar(), err))
		}
		if backup != "" {
			fmt.Fprintf(stdout, "  Backup: %s\n", backup)
		}
	}
	fmt.Fprintf(stdout, "Wrote lrt:Aperture=f/%s to %d XMP sidecars.\n", canonical, len(changes))
	return 0
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

func runSnapshot(arguments []string, stdout, stderr io.Writer) int {
	if len(arguments) == 0 || arguments[0] == "--help" || arguments[0] == "-h" {
		fmt.Fprintln(stdout, "Usage:")
		fmt.Fprintln(stdout, "  lrtl snapshot create DIRECTORY [--name NAME]")
		fmt.Fprintln(stdout, "  lrtl snapshot list DIRECTORY")
		fmt.Fprintln(stdout, "  lrtl snapshot restore DIRECTORY FILE.zip [--write] [--no-backup] [--allow-moved]")
		return 0
	}
	switch arguments[0] {
	case "create":
		set := newFlagSet("snapshot create", stderr)
		name := set.String("name", "", "snapshot name (letters, numbers, dot, dash, underscore)")
		if err := set.Parse(flagFirst(arguments[1:], map[string]bool{"--name": true, "-name": true})); err != nil {
			if done, code := finishFlagParse(set, err); done {
				return code
			}
			return reportError(stderr, err)
		}
		if set.NArg() != 1 {
			return reportError(stderr, fmt.Errorf("expected one sequence directory"))
		}
		path, count, err := createXMPSnapshot(set.Arg(0), *name)
		if err != nil {
			return reportError(stderr, err)
		}
		fmt.Fprintf(stdout, "Saved %d XMP sidecars: %s\n", count, path)
		return 0
	case "list":
		set := newFlagSet("snapshot list", stderr)
		if err := set.Parse(arguments[1:]); err != nil {
			if done, code := finishFlagParse(set, err); done {
				return code
			}
			return reportError(stderr, err)
		}
		if set.NArg() != 1 {
			return reportError(stderr, fmt.Errorf("expected one sequence directory"))
		}
		listed, err := listXMPSnapshots(set.Arg(0))
		if err != nil {
			return reportError(stderr, err)
		}
		if len(listed) == 0 {
			fmt.Fprintf(stdout, "No snapshots in %s\n", snapshotDirectory(set.Arg(0)))
			return 0
		}
		for _, snapshot := range listed {
			fmt.Fprintf(stdout, "%s  %s  %d sidecars\n",
				filepath.Base(snapshot.path), snapshot.manifest.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"), len(snapshot.manifest.Sidecars))
		}
		return 0
	case "restore":
		set := newFlagSet("snapshot restore", stderr)
		write := set.Bool("write", false, "restore the XMP sidecars")
		noBackup := set.Bool("no-backup", false, "replace existing sidecars without backup")
		allowMoved := set.Bool("allow-moved", false, "restore a snapshot into a sequence at a different path")
		if err := set.Parse(flagFirst(arguments[1:], nil)); err != nil {
			if done, code := finishFlagParse(set, err); done {
				return code
			}
			return reportError(stderr, err)
		}
		if set.NArg() != 2 {
			return reportError(stderr, fmt.Errorf("expected sequence directory and snapshot ZIP file"))
		}
		if *noBackup && !*write {
			return reportError(stderr, fmt.Errorf("--no-backup requires --write"))
		}
		if err := restoreXMPSnapshot(set.Arg(0), set.Arg(1), *write, !*noBackup, *allowMoved, stdout); err != nil {
			return reportError(stderr, err)
		}
		return 0
	default:
		return reportError(stderr, fmt.Errorf("unknown snapshot action %q (use create, list, or restore)", arguments[0]))
	}
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
	packet, err := PacketForImage(image, sidecar)
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
		fmt.Fprintln(stdout, "Usage: lrtl set IMAGE_OR_XMP CRS_PROPERTY|PreviewEV|MASK_PATH VALUE [--dry-run] [--no-backup]")
		fmt.Fprintln(stdout, "  PreviewEV is in EV and is stored as lrt:ExternalExposureDefault multiplied by 4.")
		fmt.Fprintln(stdout, "  Local controls: set IMAGE 'MaskGroupBasedCorrections[1].LocalExposure2012' 0.5")
		fmt.Fprintln(stdout, "  Geometry: set IMAGE 'MaskGroupBasedCorrections[1].CorrectionMasks[1].FullY' 0.8")
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
		return reportError(stderr, fmt.Errorf("expected IMAGE_OR_XMP, PROPERTY, and VALUE"))
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
	if strings.Contains(operands[1], "[") {
		return runSetMaskProperty(image, sidecar, operands[1], operands[2], dryRun, noBackup, stdout, stderr)
	}
	property := strings.TrimPrefix(operands[1], "crs:")
	storedProperty := "crs:" + property
	previewEV := property == "PreviewEV" || property == "lrt:PreviewEV"
	if previewEV {
		storedProperty = "lrt:ExternalExposureDefault"
	} else {
		if strings.Contains(property, ":") {
			return reportError(stderr, fmt.Errorf("property must be a numeric Camera Raw control or PreviewEV: %s", operands[1]))
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
	}
	value, err := strconv.ParseFloat(operands[2], 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return reportError(stderr, fmt.Errorf("value must be a finite number: %s", operands[2]))
	}
	storedValue := value
	if previewEV {
		storedValue *= 4
		if math.IsInf(storedValue, 0) {
			return reportError(stderr, fmt.Errorf("PreviewEV is outside the supported numeric range: %s", operands[2]))
		}
	}
	packet, err := PacketForImage(image, sidecar)
	if err != nil {
		return reportError(stderr, err)
	}
	if err := packet.SetScalar(storedProperty, formatNumber(storedValue)); err != nil {
		return reportError(stderr, err)
	}
	if previewEV {
		fmt.Fprintf(stdout, "%s: set PreviewEV=%s (%s=%s)\n", filepath.Base(image), formatNumber(value), storedProperty, formatNumber(storedValue))
	} else {
		fmt.Fprintf(stdout, "%s: set %s=%s\n", filepath.Base(image), storedProperty, formatNumber(storedValue))
	}
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
	imageExists, imageErr := osStat(image)
	sidecarExists, sidecarErr := osStat(sidecar)
	if imageErr != nil && sidecarErr != nil {
		return reportError(stderr, imageErr)
	}
	if !imageExists && !sidecarExists {
		return reportError(stderr, fmt.Errorf("image and XMP sidecar do not exist: %s", image))
	}
	packet, err := PacketForImage(image, sidecar)
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
	smoothness := set.Int("smoothness", 50, "LRT-compatible spline smoothness (0-100; default 50)")
	if err := set.Parse(flagFirst(arguments, map[string]bool{"--smoothness": true, "-smoothness": true})); err != nil {
		if done, code := finishFlagParse(set, err); done {
			return code
		}
	}
	if set.NArg() != 1 {
		return reportError(stderr, fmt.Errorf("expected one sequence directory"))
	}
	if *smoothness < 0 || *smoothness > 100 {
		return reportError(stderr, fmt.Errorf("--smoothness must be between 0 and 100"))
	}
	directory := set.Arg(0)
	frames, err := DiscoverSequence(directory)
	if err != nil {
		return reportError(stderr, err)
	}
	plan, err := buildPlanWithSmoothness(frames, *smoothness)
	if err != nil {
		return reportError(stderr, err)
	}
	keyframeNames := make([]string, 0, len(plan.KeyframeIndices))
	for _, index := range plan.KeyframeIndices {
		keyframeNames = append(keyframeNames, filepath.Base(frames[index].Image))
	}
	fmt.Fprintf(stdout, "Sequence: %s\n", directory)
	fmt.Fprintf(stdout, "Frames: %d; effective keyframes: %d; spline smoothness: %d\n", plan.FrameCount, len(keyframeNames), *smoothness)
	anchors := strings.Join(keyframeNames, ", ")
	if anchors == "" {
		anchors = "none"
	}
	fmt.Fprintf(stdout, "Anchors: %s\n", anchors)
	numericCount, structuredCount, maskCount := 0, 0, 0
	scalarCount := 0
	for _, change := range plan.Changes {
		numericCount += len(change.Values)
		scalarCount += len(change.ScalarValues) + len(change.ScalarRemovals)
		structuredCount += len(change.StructuredProperties) + len(change.MaskStructuredProperties)
		for _, transition := range change.MaskTransitions {
			maskCount += len(transition.values)
			if transition.hasLuminanceRange {
				maskCount++
			}
		}
	}
	fmt.Fprintf(stdout, "Proposed frames: %d; numeric values: %d; first-frame XMP structures: %d; local mask values: %d\n", len(plan.Changes), numericCount, structuredCount, maskCount)
	if scalarCount > 0 {
		fmt.Fprintf(stdout, "Scalar metadata values: %d\n", scalarCount)
	}
	if !*write {
		fmt.Fprintln(stdout, "Dry run. Pass --write to update sidecar XMP files.")
		for _, change := range plan.Changes {
			capacity := len(change.Values) + len(change.ScalarValues) + len(change.ScalarRemovals) + len(change.StructuredProperties) + len(change.MaskStructuredProperties)
			for _, transition := range change.MaskTransitions {
				capacity += len(transition.values)
				if transition.hasLuminanceRange {
					capacity++
				}
			}
			names := make([]string, 0, capacity)
			for name := range change.Values {
				names = append(names, name)
			}
			for name := range change.ScalarValues {
				names = append(names, name)
			}
			for _, name := range change.ScalarRemovals {
				names = append(names, name+" (remove)")
			}
			names = append(names, change.StructuredProperties...)
			for _, property := range change.MaskStructuredProperties {
				names = append(names, property.String())
			}
			for _, transition := range change.MaskTransitions {
				for name := range transition.values {
					names = append(names, maskTransitionKeyLabel(transition.key)+"."+name)
				}
				if transition.hasLuminanceRange {
					names = append(names, maskTransitionKeyLabel(transition.key)+".RangeMaskLumRange")
				}
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
	output := set.String("output", "", "output AVI, MP4, or MOV path")
	selectionPath := set.String("selection", "", "use included frames from a long-term selection manifest")
	filenameContains := set.String("filename-contains", "", "render only frames whose filename contains this text (case-insensitive)")
	codec := set.String("codec", "", "MP4: h264|h265; MOV: prores|prores-hq|dnxhr-lb|dnxhr-sq|dnxhr-hq|dnxhr-hqx|dnxhr-444; defaults by extension")
	fps := set.Int("fps", 24, "integer frame rate (1-240)")
	quality := set.Int("quality", 90, "JPEG quality for converted AVI frames (1-100)")
	ffmpeg := set.String("ffmpeg", "", "FFmpeg executable for MP4 or MOV output; defaults to PATH")
	crf := set.Int("crf", 18, "H.264/H.265 quality (0-51; lower is higher quality)")
	preset := set.String("preset", "medium", "H.264/H.265 speed/size preset")
	longEdge := set.Int("long-edge", 0, "downscale so the longest side is at most this many pixels (0 keeps input size)")
	outputHeight := set.Int("output-height", 0, "set the output height and preserve aspect ratio (0 keeps input size)")
	aspectValue := set.String("aspect", "", "center-crop to WIDTH:HEIGHT aspect ratio (for example 16:9)")
	audioPath := set.String("audio", "", "add an audio file to MP4/MOV output and fit it to video duration")
	overwrite := set.Bool("overwrite", false, "replace an existing output file")
	deflicker := set.Bool("deflicker", false, "apply global luminance deflicker while rendering")
	referenceValue := set.String("reference", "", "reference area x,y,width,height; defaults to the full frame")
	smoothRadius := set.Int("smooth", 5, "deflicker smoothing radius in frames")
	deflickerSmoother := set.String("deflicker-smoother", "gaussian", "deflicker smoother: gaussian or lowess")
	passes := set.Int("passes", 1, "deflicker passes (1-8)")
	constant := set.Bool("constant", false, "deflicker toward constant luminance")
	applyXMPExposure := set.Bool("apply-xmp-exposure", false, "apply crs:Exposure2012 from sidecar XMP to rendered frames")
	applyXMPBasic := set.Bool("apply-xmp-basic", false, "apply supported global Camera Raw tone and color controls from sidecar XMP")
	applyXMPCrop := set.Bool("apply-xmp-crop", false, "apply rotated crop rectangles from sidecar XMP")
	autoOrient := set.Bool("auto-orient", false, "apply EXIF orientation before crop, analysis, and rendering")
	timestampOverlay := set.Bool("timestamp-overlay", false, "burn each frame's EXIF/XMP capture time into the video")
	timestampFormat := set.String("timestamp-format", defaultTimestampFormat, "Go time layout used by --timestamp-overlay")
	watermark := set.String("watermark", "", "burn fixed text into every frame")
	watermarkPosition := set.String("watermark-position", "bottom-left", "watermark position: top-left, top-right, bottom-left, bottom-right, or center")
	motionBlurFrames := set.Int("motion-blur", 0, "blend this many adjacent frames (0 off; 2, 3, 4, 5, 10, 15, 20)")
	rawPreview := set.Bool("raw-preview", false, "use embedded previews from supported RAW files")
	rawDevelop := set.Bool("raw-develop", false, "develop RAW with installed Adobe DNG Converter (or explicit DNGLab)")
	dnglab := set.String("dnglab", "", "optional DNGLab executable for --raw-develop")
	dngConverter := set.String("dng-converter", "", "Adobe DNG Converter executable; defaults to the installed application")
	parseArgs := flagFirst(arguments, map[string]bool{
		"--output": true, "-output": true,
		"--selection": true, "-selection": true,
		"--filename-contains": true, "-filename-contains": true,
		"--codec": true, "-codec": true,
		"--fps": true, "-fps": true,
		"--quality": true, "-quality": true,
		"--ffmpeg": true, "-ffmpeg": true,
		"--crf": true, "-crf": true,
		"--preset": true, "-preset": true,
		"--long-edge": true, "-long-edge": true,
		"--output-height": true, "-output-height": true,
		"--aspect": true, "-aspect": true,
		"--audio": true, "-audio": true,
		"--reference": true, "-reference": true,
		"--smooth": true, "-smooth": true,
		"--deflicker-smoother": true, "-deflicker-smoother": true,
		"--passes": true, "-passes": true,
		"--dnglab": true, "-dnglab": true, "--dng-converter": true, "-dng-converter": true,
		"--timestamp-format": true, "-timestamp-format": true,
		"--watermark": true, "-watermark": true,
		"--watermark-position": true, "-watermark-position": true,
		"--motion-blur": true, "-motion-blur": true,
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
	if *applyXMPExposure && *applyXMPBasic {
		return reportError(stderr, fmt.Errorf("--apply-xmp-exposure cannot be combined with --apply-xmp-basic because the basic controls already include Exposure2012"))
	}
	var aspectRatio float64
	if strings.TrimSpace(*aspectValue) != "" {
		var err error
		aspectRatio, err = parseAspectRatio(*aspectValue)
		if err != nil {
			return reportError(stderr, err)
		}
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
	timestampOverlayEnabled := *timestampOverlay
	set.Visit(func(parsed *flag.Flag) {
		switch parsed.Name {
		case "reference", "smooth", "deflicker-smoother", "passes", "constant":
			deflickerEnabled = true
		case "timestamp-format":
			timestampOverlayEnabled = true
		}
	})
	directory := set.Arg(0)
	frames, err := DiscoverSequence(directory)
	if err != nil {
		return reportError(stderr, err)
	}
	if *selectionPath != "" {
		frames, err = loadSelectedFrames(frames, *selectionPath, directory)
		if err != nil {
			return reportError(stderr, err)
		}
	}
	if *filenameContains != "" {
		needle := strings.ToLower(*filenameContains)
		matched := make([]Frame, 0, len(frames))
		for _, frame := range frames {
			if strings.Contains(strings.ToLower(filepath.Base(frame.Image)), needle) {
				matched = append(matched, frame)
			}
		}
		if len(matched) == 0 {
			return reportError(stderr, fmt.Errorf("--filename-contains %q matched no frames in %s", *filenameContains, directory))
		}
		frames = matched
	}
	options := RenderOptions{
		FPS:               *fps,
		Quality:           *quality,
		FFmpegPath:        *ffmpeg,
		Codec:             strings.ToLower(strings.TrimSpace(*codec)),
		CRF:               *crf,
		Preset:            *preset,
		LongEdge:          *longEdge,
		OutputHeight:      *outputHeight,
		AspectRatio:       aspectRatio,
		AudioPath:         *audioPath,
		Overwrite:         *overwrite,
		Deflicker:         deflickerEnabled,
		Reference:         reference,
		SmoothRadius:      *smoothRadius,
		DeflickerSmoother: *deflickerSmoother,
		DeflickerPasses:   *passes,
		Constant:          *constant,
		ApplyXMPExposure:  *applyXMPExposure,
		ApplyXMPBasic:     *applyXMPBasic,
		ApplyXMPCrop:      *applyXMPCrop,
		AutoOrient:        *autoOrient,
		TimestampOverlay:  timestampOverlayEnabled,
		TimestampFormat:   *timestampFormat,
		Watermark:         *watermark,
		WatermarkPosition: strings.ToLower(strings.TrimSpace(*watermarkPosition)),
		MotionBlurFrames:  *motionBlurFrames,
		RawPreview:        *rawPreview,
		RawDevelop:        *rawDevelop,
		DNGLabPath:        *dnglab,
		DNGConverterPath:  *dngConverter,
	}
	if err := RenderSequence(frames, *output, options); err != nil {
		return reportError(stderr, err)
	}
	features := make([]string, 0, 8)
	selectedCodec := options.Codec
	if selectedCodec == "" {
		switch strings.ToLower(filepath.Ext(*output)) {
		case ".avi":
			selectedCodec = "mjpeg"
		case ".mp4":
			selectedCodec = "h264"
		case ".mov":
			selectedCodec = "prores"
		}
	}
	features = append(features, strings.ToUpper(selectedCodec)+" codec")
	if *applyXMPExposure {
		features = append(features, "XMP Exposure2012")
	}
	if *applyXMPBasic {
		features = append(features, "XMP basic tone and color controls")
	}
	if *applyXMPCrop {
		features = append(features, "XMP crop")
	}
	if *autoOrient {
		features = append(features, "EXIF orientation")
	}
	if timestampOverlayEnabled {
		features = append(features, "EXIF/XMP timestamp overlay")
	}
	if *motionBlurFrames > 0 {
		features = append(features, fmt.Sprintf("%d-frame temporal blend", *motionBlurFrames))
	}
	if *rawPreview {
		features = append(features, "embedded RAW previews")
	}
	if *rawDevelop {
		backend := "Adobe DNG Converter RAW development"
		if strings.TrimSpace(*dnglab) != "" {
			backend = "DNGLab RAW development"
		}
		features = append(features, backend)
	}
	if *longEdge > 0 {
		features = append(features, fmt.Sprintf("long-edge limit of %d pixels", *longEdge))
	}
	if *outputHeight > 0 {
		features = append(features, fmt.Sprintf("%d-pixel output height", *outputHeight))
	}
	if aspectRatio > 0 {
		features = append(features, fmt.Sprintf("%s aspect crop", strings.TrimSpace(*aspectValue)))
	}
	if strings.TrimSpace(*audioPath) != "" {
		features = append(features, "AAC audio track")
	}
	if *selectionPath != "" {
		features = append(features, "long-term selection")
	}
	if *filenameContains != "" {
		features = append(features, fmt.Sprintf("filename contains %q", *filenameContains))
	}
	if *watermark != "" {
		features = append(features, "text watermark")
	}
	if len(features) > 0 {
		fmt.Fprintf(stdout, "Rendered %d frames at %d fps with %s: %s\n", len(frames), *fps, strings.Join(features, " and "), *output)
	} else {
		fmt.Fprintf(stdout, "Rendered %d frames at %d fps: %s\n", len(frames), *fps, *output)
	}
	return 0
}

func runPreview(arguments []string, stdout, stderr io.Writer) int {
	set := newFlagSet("preview", stderr)
	output := set.String("output", "", "write an SVG brightness preview")
	rawPreview := set.Bool("raw-preview", false, "analyze embedded previews from supported RAW files")
	rawDevelop := set.Bool("raw-develop", false, "analyze RAW developed by Adobe DNG Converter (or explicit DNGLab)")
	dnglab := set.String("dnglab", "", "optional DNGLab executable for --raw-develop")
	dngConverter := set.String("dng-converter", "", "Adobe DNG Converter executable; defaults to the installed application")
	applyXMPExposure := set.Bool("apply-xmp-exposure", false, "apply crs:Exposure2012 from sidecar XMP during analysis")
	applyXMPBasic := set.Bool("apply-xmp-basic", false, "apply supported global Camera Raw tone and color controls during analysis")
	applyXMPCrop := set.Bool("apply-xmp-crop", false, "apply rotated crop rectangles from sidecar XMP during analysis")
	overwrite := set.Bool("overwrite", false, "replace an existing SVG preview")
	if err := set.Parse(flagFirst(arguments, map[string]bool{"--output": true, "-output": true, "--dnglab": true, "-dnglab": true, "--dng-converter": true, "-dng-converter": true})); err != nil {
		help, code := finishFlagParse(set, err)
		if help {
			fmt.Fprintln(stdout, "Usage: lrtl preview DIRECTORY --output FILE.svg [--raw-preview|--raw-develop] [--dng-converter PATH|--dnglab PATH] [--apply-xmp-exposure|--apply-xmp-basic] [--apply-xmp-crop] [--overwrite]")
		}
		return code
	}
	if set.NArg() != 1 || *output == "" {
		return reportError(stderr, fmt.Errorf("Usage: lrtl preview DIRECTORY --output FILE.svg [--raw-preview|--raw-develop] [--dng-converter PATH|--dnglab PATH] [--apply-xmp-exposure|--apply-xmp-basic] [--apply-xmp-crop] [--overwrite]"))
	}
	frames, err := DiscoverSequence(set.Arg(0))
	if err != nil {
		return reportError(stderr, err)
	}
	inputOptions := imageInputOptions{
		RawPreview: *rawPreview, RawDevelop: *rawDevelop, DNGLabPath: *dnglab, DNGConverterPath: *dngConverter,
		ApplyXMPExposure: *applyXMPExposure, ApplyXMPBasic: *applyXMPBasic, ApplyXMPCrop: *applyXMPCrop,
	}
	if err := writeSequencePreviewWithOptions(frames, *output, inputOptions, *overwrite); err != nil {
		return reportError(stderr, err)
	}
	fmt.Fprintf(stdout, "Wrote brightness preview for %d frames: %s\n", len(frames), *output)
	return 0
}

func runSubtitles(arguments []string, stdout, stderr io.Writer) int {
	set := newFlagSet("subtitles", stderr)
	output := set.String("output", "", "write timestamp subtitles to an SRT file")
	fps := set.Int("fps", 24, "video frame rate used for subtitle timing (1-240)")
	timestampFormat := set.String("timestamp-format", defaultTimestampFormat, "Go time layout used for subtitle text")
	overwrite := set.Bool("overwrite", false, "replace an existing SRT file")
	if err := set.Parse(flagFirst(arguments, map[string]bool{
		"--output": true, "-output": true,
		"--fps": true, "-fps": true,
		"--timestamp-format": true, "-timestamp-format": true,
	})); err != nil {
		if help, code := finishFlagParse(set, err); help {
			fmt.Fprintln(stdout, "Usage: lrtl subtitles DIRECTORY --output FILE.srt [--fps N] [--timestamp-format LAYOUT] [--overwrite]")
			return code
		} else {
			return code
		}
	}
	if set.NArg() != 1 || *output == "" {
		return reportError(stderr, fmt.Errorf("Usage: lrtl subtitles DIRECTORY --output FILE.srt [--fps N] [--timestamp-format LAYOUT] [--overwrite]"))
	}
	frames, err := DiscoverSequence(set.Arg(0))
	if err != nil {
		return reportError(stderr, err)
	}
	if err := writeSequenceSubtitles(frames, *output, *fps, *timestampFormat, *overwrite); err != nil {
		return reportError(stderr, err)
	}
	fmt.Fprintf(stdout, "Wrote timestamp subtitles for %d frames at %d fps: %s\n", len(frames), *fps, *output)
	return 0
}

func runLongTerm(arguments []string, stdout, stderr io.Writer) int {
	set := newFlagSet("long-term", stderr)
	output := set.String("output", "", "write per-frame metrics and an include/exclude selection manifest")
	metricProfileName := set.String("metric-profile", "lrtl", "metric model: lrtl or lrt-7.5.5")
	luminanceRange := set.String("luminance-range", "", "keep frames with the selected profile's luminance in MIN,MAX (0-1)")
	contrastRange := set.String("contrast-range", "", "keep frames with the selected profile's contrast in MIN,MAX")
	hueRange := set.String("hue-range", "", "keep frames with the selected profile's mean hue in MIN,MAX degrees; MIN>MAX wraps at 360")
	rawPreview := set.Bool("raw-preview", false, "analyze embedded previews from supported RAW files")
	rawDevelop := set.Bool("raw-develop", false, "analyze RAW developed by Adobe DNG Converter (or explicit DNGLab)")
	dnglab := set.String("dnglab", "", "optional DNGLab executable for --raw-develop")
	dngConverter := set.String("dng-converter", "", "Adobe DNG Converter executable; defaults to the installed application")
	applyXMPExposure := set.Bool("apply-xmp-exposure", false, "include crs:Exposure2012 in image analysis")
	applyXMPBasic := set.Bool("apply-xmp-basic", false, "include supported global Camera Raw tone and color controls in image analysis")
	applyXMPCrop := set.Bool("apply-xmp-crop", false, "include XMP crop rectangles in image analysis")
	overwrite := set.Bool("overwrite", false, "replace an existing selection manifest")
	parseArgs := flagFirst(arguments, map[string]bool{
		"--output": true, "-output": true,
		"--metric-profile": true, "-metric-profile": true,
		"--luminance-range": true, "-luminance-range": true,
		"--contrast-range": true, "-contrast-range": true,
		"--hue-range": true, "-hue-range": true,
		"--dnglab": true, "-dnglab": true, "--dng-converter": true, "-dng-converter": true,
	})
	if err := set.Parse(parseArgs); err != nil {
		if done, code := finishFlagParse(set, err); done {
			fmt.Fprintln(stdout, "Usage: lrtl long-term DIRECTORY --output FILTER.json [--metric-profile lrtl|lrt-7.5.5] [--luminance-range MIN,MAX] [--contrast-range MIN,MAX] [--hue-range MIN,MAX] [--raw-preview|--raw-develop] [--apply-xmp-exposure|--apply-xmp-basic] [--apply-xmp-crop] [--overwrite]")
			return code
		}
	}
	if set.NArg() != 1 || *output == "" {
		return reportError(stderr, fmt.Errorf("Usage: lrtl long-term DIRECTORY --output FILTER.json [--metric-profile lrtl|lrt-7.5.5] [--luminance-range MIN,MAX] [--contrast-range MIN,MAX] [--hue-range MIN,MAX] [--raw-preview|--raw-develop] [--apply-xmp-exposure|--apply-xmp-basic] [--apply-xmp-crop] [--overwrite]"))
	}
	profile := longTermMetricProfile(*metricProfileName)
	if profile != longTermMetricProfileLRTL && profile != longTermMetricProfileLRT755 {
		return reportError(stderr, fmt.Errorf("--metric-profile must be lrtl or lrt-7.5.5"))
	}
	ranges := longTermRanges{}
	var err error
	if *luminanceRange != "" {
		ranges.Luminance, err = parseMetricRange(*luminanceRange, "luminance range", 0, 1, false)
		if err != nil {
			return reportError(stderr, err)
		}
	}
	if *contrastRange != "" {
		ranges.Contrast, err = parseMetricRange(*contrastRange, "contrast range", 0, math.Inf(1), false)
		if err != nil {
			return reportError(stderr, err)
		}
	}
	if *hueRange != "" {
		ranges.Hue, err = parseMetricRange(*hueRange, "hue range", 0, 360, true)
		if err != nil {
			return reportError(stderr, err)
		}
	}
	frames, err := DiscoverSequence(set.Arg(0))
	if err != nil {
		return reportError(stderr, err)
	}
	inputOptions := imageInputOptions{
		RawPreview: *rawPreview, RawDevelop: *rawDevelop, DNGLabPath: *dnglab, DNGConverterPath: *dngConverter,
		ApplyXMPExposure: *applyXMPExposure, ApplyXMPBasic: *applyXMPBasic, ApplyXMPCrop: *applyXMPCrop,
	}
	manifest, err := analyzeLongTermSequence(frames, inputOptions, ranges, profile)
	if err != nil {
		return reportError(stderr, err)
	}
	manifest.Directory = set.Arg(0)
	if err := writeSelectionManifest(*output, manifest, *overwrite); err != nil {
		return reportError(stderr, err)
	}
	included := 0
	for _, frame := range manifest.Frames {
		if frame.Included {
			included++
		}
	}
	fmt.Fprintf(stdout, "Analyzed %d frames; selected %d and excluded %d: %s\n", len(manifest.Frames), included, len(manifest.Frames)-included, *output)
	return 0
}

func runDeflicker(arguments []string, stdout, stderr io.Writer) int {
	set := newFlagSet("deflicker", stderr)
	write := set.Bool("write", false, "write exposure corrections to XMP sidecars")
	noBackup := set.Bool("no-backup", false, "do not create backups when writing")
	reset := set.Bool("reset", false, "remove lrtl-managed deflicker exposure corrections")
	referenceValue := set.String("reference", "", "reference area x,y,width,height; defaults to the full frame")
	smoothRadius := set.Int("smooth", 5, "deflicker smoothing radius in frames")
	smoother := set.String("smoother", "gaussian", "deflicker smoother: gaussian or lowess")
	passes := set.Int("passes", 1, "deflicker passes (1-8)")
	constant := set.Bool("constant", false, "deflicker toward constant luminance")
	rawPreview := set.Bool("raw-preview", false, "analyze embedded previews from supported TIFF-based RAW files")
	rawDevelop := set.Bool("raw-develop", false, "analyze RAW developed by Adobe DNG Converter (or explicit DNGLab)")
	dnglab := set.String("dnglab", "", "optional DNGLab executable for --raw-develop")
	dngConverter := set.String("dng-converter", "", "Adobe DNG Converter executable; defaults to the installed application")
	applyXMPExposure := set.Bool("apply-xmp-exposure", false, "include crs:Exposure2012 from sidecar XMP in luminance analysis")
	applyXMPBasic := set.Bool("apply-xmp-basic", false, "include supported global Camera Raw tone and color controls in luminance analysis")
	applyXMPCrop := set.Bool("apply-xmp-crop", false, "include XMP crop rectangles in luminance analysis")
	parseArgs := flagFirst(arguments, map[string]bool{
		"--reference": true, "-reference": true,
		"--smooth": true, "-smooth": true,
		"--smoother": true, "-smoother": true,
		"--passes": true, "-passes": true,
		"--dnglab": true, "-dnglab": true, "--dng-converter": true, "-dng-converter": true,
	})
	if err := set.Parse(parseArgs); err != nil {
		if done, code := finishFlagParse(set, err); done {
			return code
		}
	}
	if set.NArg() != 1 {
		return reportError(stderr, fmt.Errorf("expected one sequence directory"))
	}
	if *smoothRadius < 1 || *smoothRadius > 1000 {
		return reportError(stderr, fmt.Errorf("smoothing radius must be between 1 and 1000 frames: %d", *smoothRadius))
	}
	if *passes < 1 || *passes > 8 {
		return reportError(stderr, fmt.Errorf("deflicker passes must be between 1 and 8: %d", *passes))
	}
	if *reset {
		analysisOption := false
		set.Visit(func(parsed *flag.Flag) {
			switch parsed.Name {
			case "reference", "smooth", "smoother", "passes", "constant", "raw-preview", "raw-develop", "dnglab", "dng-converter", "apply-xmp-exposure", "apply-xmp-basic", "apply-xmp-crop":
				analysisOption = true
			}
		})
		if analysisOption {
			return reportError(stderr, fmt.Errorf("--reset cannot be combined with analysis options"))
		}
	}
	frames, err := DiscoverSequence(set.Arg(0))
	if err != nil {
		return reportError(stderr, err)
	}
	var reference image.Rectangle
	if *referenceValue != "" {
		reference, err = parseReferenceArea(*referenceValue)
		if err != nil {
			return reportError(stderr, err)
		}
	}
	inputOptions := imageInputOptions{
		RawPreview: *rawPreview, RawDevelop: *rawDevelop, DNGLabPath: *dnglab, DNGConverterPath: *dngConverter,
		ApplyXMPExposure: *applyXMPExposure, ApplyXMPBasic: *applyXMPBasic, ApplyXMPCrop: *applyXMPCrop,
	}
	var changes []DeflickerChange
	measuredPasses := 0
	if *reset {
		changes, err = buildDeflickerChangesForInput(frames, nil, *smoothRadius, *passes, *constant, true, *smoother, false)
	} else {
		var plan *deflickerSequencePlan
		plan, err = planDeflickerSequence(frames, reference, inputOptions, *smoothRadius, *passes, *constant, *smoother)
		if err == nil {
			changes = plan.Changes
			measuredPasses = len(plan.Measurements)
		}
	}
	if err != nil {
		return reportError(stderr, err)
	}
	fmt.Fprintf(stdout, "Sequence: %s\n", set.Arg(0))
	if *reset {
		fmt.Fprintf(stdout, "Frames: %d; deflicker corrections to reset: %d\n", len(frames), len(changes))
	} else {
		target := "smoothed luminance"
		if *constant {
			target = "constant median luminance"
		}
		fmt.Fprintf(stdout, "Frames: %d; corrections: %d; target: %s; smoother: %s; smooth radius: %d; passes: %d\n",
			len(frames), len(changes), target, strings.ToLower(strings.TrimSpace(*smoother)), *smoothRadius, measuredPasses)
	}
	for _, change := range changes {
		fmt.Fprintf(stdout, "  %-42s", filepath.Base(change.Frame.Image))
		if *reset {
			fmt.Fprintf(stdout, " %s %+.4f -> %+.4f EV (remove lrtl correction)", xmpExposureTargetLabel(change.Target), change.ExposureBefore, change.ExposureAfter)
		} else {
			fmt.Fprintf(stdout, " luminance=%.6g correction=%+.4f EV %s=%+.4f EV",
				change.Luminance, change.Correction, xmpExposureTargetLabel(change.Target), change.ExposureAfter)
		}
		fmt.Fprintln(stdout)
	}
	if len(changes) == 0 {
		if *reset {
			fmt.Fprintln(stdout, "No lrtl-managed deflicker corrections to reset.")
		} else {
			fmt.Fprintln(stdout, "No exposure changes are needed.")
		}
		return 0
	}
	if !*write {
		fmt.Fprintln(stdout, "Preview only. Pass --write to update sidecar XMP files.")
		return 0
	}
	if err := applyDeflickerChanges(changes); err != nil {
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

func runKeyframes(arguments []string, stdout, stderr io.Writer) int {
	set := newFlagSet("keyframes", stderr)
	count := set.Int("count", 0, "number of new keyframe suggestions (default estimated from luminance; 5 for --evenly-spaced; maximum 20)")
	ratingOption := set.Int("rating", 4, "keyframe star rating to mark: 4 (LRT default) or 5")
	replaceExisting := set.Bool("replace-existing", false, "clear existing keyframes of the selected rating and shift star conflicts backward where possible")
	smoothRadius := set.Int("smooth", 5, "luminance smoothing radius for --luminance-deviation")
	evenlySpaced := set.Bool("evenly-spaced", false, "use the LRT helper's uniform frame positions without decoding images")
	luminanceDeviation := set.Bool("luminance-deviation", false, "use lrtl's luminance-deviation frame selection instead of LRT spacing")
	rawPreview := set.Bool("raw-preview", false, "analyze embedded previews from supported TIFF-based RAW files")
	rawDevelop := set.Bool("raw-develop", false, "analyze RAW developed by Adobe DNG Converter (or explicit DNGLab)")
	dnglab := set.String("dnglab", "", "optional DNGLab executable for --raw-develop")
	dngConverter := set.String("dng-converter", "", "Adobe DNG Converter executable; defaults to the installed application")
	applyXMPExposure := set.Bool("apply-xmp-exposure", false, "include crs:Exposure2012 from sidecar XMP in luminance analysis")
	applyXMPBasic := set.Bool("apply-xmp-basic", false, "include supported global Camera Raw tone and color controls in luminance analysis")
	applyXMPCrop := set.Bool("apply-xmp-crop", false, "include XMP crop rectangles in luminance analysis")
	write := set.Bool("write", false, "mark the suggested frames in XMP sidecars")
	noBackup := set.Bool("no-backup", false, "do not create backups when writing")
	if err := set.Parse(flagFirst(arguments, map[string]bool{
		"--count": true, "-count": true, "--smooth": true, "-smooth": true,
		"--rating": true, "-rating": true,
		"--dnglab": true, "-dnglab": true, "--dng-converter": true, "-dng-converter": true,
	})); err != nil {
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
	if countSpecified && (*count < 1 || *count > 20) {
		return reportError(stderr, fmt.Errorf("keyframe count must be between 1 and 20: %d", *count))
	}
	if *ratingOption != 4 && *ratingOption != 5 {
		return reportError(stderr, fmt.Errorf("keyframe rating must be 4 or 5: %d", *ratingOption))
	}
	if *evenlySpaced && *luminanceDeviation {
		return reportError(stderr, fmt.Errorf("--evenly-spaced and --luminance-deviation select different placement methods"))
	}
	directory := set.Arg(0)
	frames, err := DiscoverSequence(directory)
	if err != nil {
		return reportError(stderr, err)
	}
	if *smoothRadius < 0 || *smoothRadius > 1000 {
		return reportError(stderr, fmt.Errorf("smoothing radius must be between 0 and 1000 frames: %d", *smoothRadius))
	}
	inputOptions := imageInputOptions{
		RawPreview: *rawPreview, RawDevelop: *rawDevelop, DNGLabPath: *dnglab, DNGConverterPath: *dngConverter,
		ApplyXMPExposure: *applyXMPExposure, ApplyXMPBasic: *applyXMPBasic, ApplyXMPCrop: *applyXMPCrop,
	}
	if err := validateImageInputOptions(inputOptions); err != nil {
		return reportError(stderr, err)
	}
	if *evenlySpaced && (*rawPreview || *rawDevelop || strings.TrimSpace(*dnglab) != "" || *applyXMPExposure || *applyXMPBasic || *applyXMPCrop) {
		return reportError(stderr, fmt.Errorf("image processing options cannot be combined with --evenly-spaced because that mode does not decode images"))
	}
	frameCount := len(frames)
	if frameCount < 1 {
		return reportError(stderr, fmt.Errorf("sequence contains no images"))
	}
	packets := make([]*XMPPacket, frameCount)
	existingKeyframes := make([]int, 0)
	existingTargetRating := 0
	selectionAnchors := make([]int, 0)
	reservedRows := make([]bool, frameCount)
	holyGrailMarkers := make([]bool, frameCount)
	for index, frame := range frames {
		packet, err := PacketForFrame(frame)
		if err != nil {
			return reportError(stderr, fmt.Errorf("%s: %w", frame.OutputSidecar(), err))
		}
		packets[index] = packet
		rating, hasRating := packet.Rating()
		if hasRating && (rating == 2 || rating == 3) {
			holyGrailMarkers[index] = true
		}
		hasHolyGrailProperty, err := packet.HasProperty(holyGrailKeyframeProperty)
		if err != nil {
			return reportError(stderr, fmt.Errorf("%s: %w", frame.OutputSidecar(), err))
		}
		reservedRows[index] = hasHolyGrailProperty
		if packet.IsKeyframe() {
			existingKeyframes = append(existingKeyframes, index)
			if hasRating && rating == *ratingOption {
				existingTargetRating++
				if !*replaceExisting {
					selectionAnchors = append(selectionAnchors, index)
				}
			} else {
				selectionAnchors = append(selectionAnchors, index)
			}
			continue
		}
		if hasHolyGrailProperty {
			selectionAnchors = append(selectionAnchors, index)
			continue
		}
		if hasRating && (rating == 2 || rating == 3) {
			selectionAnchors = append(selectionAnchors, index)
		}
	}
	lrtMeasurements, hasLRTMeasurements, err := readLRTKeyframeMeasurements(frames, packets)
	if err != nil {
		return reportError(stderr, err)
	}
	useLRTMeasurements := hasLRTMeasurements && !*applyXMPExposure && !*applyXMPBasic && !*applyXMPCrop && !*rawPreview && !*rawDevelop && strings.TrimSpace(*dnglab) == ""
	var curve []float64
	if !*evenlySpaced && (*luminanceDeviation || (!countSpecified && !useLRTMeasurements)) {
		curve, err = analyzeLuminanceSequenceWithInputOptions(frames, inputOptions)
		if err != nil {
			return reportError(stderr, err)
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
	autoTargetCount := 0
	countSource := ""
	if !countSpecified {
		if *evenlySpaced {
			suggestionCount = 5
		} else {
			countMeasurements := curve
			countSource = "image luminance"
			if useLRTMeasurements {
				countMeasurements = lrtMeasurements
				countSource = "LRT XMP (lrt:ExternalExposureDefault / 4)"
			}
			autoTargetCount = min(20, estimateAutoKeyframeCount(countMeasurements, holyGrailMarkers))
			suggestionCount = autoTargetCount
			if !*replaceExisting {
				suggestionCount = max(0, suggestionCount-existingTargetRating)
			}
			if *luminanceDeviation {
				suggestionCount = min(suggestionCount, max(0, availableSuggestions))
			}
		}
	}
	if !countSpecified && !*evenlySpaced {
		if *replaceExisting {
			fmt.Fprintf(stdout, "Keyframe quantity estimate (LRT formula, source: %s): %d total; replacing %d existing %d-star keyframe(s), suggesting up to %d\n", countSource, autoTargetCount, existingTargetRating, *ratingOption, suggestionCount)
		} else {
			fmt.Fprintf(stdout, "Keyframe quantity estimate (LRT formula, source: %s): %d total; preserving %d existing %d-star keyframe(s), suggesting up to %d new\n", countSource, autoTargetCount, existingTargetRating, *ratingOption, suggestionCount)
		}
	}
	if suggestionCount == 0 {
		if countSpecified {
			if *luminanceDeviation && availableSuggestions <= 0 {
				return reportError(stderr, fmt.Errorf("no unmarked interior frames are available for new keyframes"))
			}
			return reportError(stderr, fmt.Errorf("new keyframe count must be at least 1"))
		}
		fmt.Fprintf(stdout, "Sequence: %s\n", directory)
		fmt.Fprintf(stdout, "Frames: %d; existing keyframes: %d; no new keyframes suggested\n", frameCount, len(existingKeyframes))
		return 0
	}
	var indices []int
	if *luminanceDeviation {
		indices, err = selectKeyframeIndicesWithAnchors(curve, suggestionCount, *smoothRadius, selectionAnchors)
	} else {
		indices, err = selectLRTWizardKeyframeIndices(frameCount, suggestionCount)
		if err == nil && !*replaceExisting && len(selectionAnchors) > 0 {
			blocked := make(map[int]struct{}, len(selectionAnchors))
			for _, index := range selectionAnchors {
				blocked[index] = struct{}{}
			}
			newIndices := indices[:0]
			for _, index := range indices {
				if _, exists := blocked[index]; exists {
					continue
				}
				newIndices = append(newIndices, index)
			}
			indices = newIndices
		}
	}
	if err != nil {
		return reportError(stderr, err)
	}
	if *replaceExisting {
		indices = placeLRTWizardKeyframeIndices(indices, packets, reservedRows, *ratingOption)
	}
	modifiedPackets := make(map[int]*XMPPacket)
	if *write {
		if *replaceExisting {
			for index, packet := range packets {
				currentRating, hasRating := packet.Rating()
				if !hasRating || currentRating != *ratingOption {
					continue
				}
				frame := frames[index]
				if err := UnmarkKeyframe(packet, true); err != nil {
					return reportError(stderr, fmt.Errorf("%s: %w", frame.OutputSidecar(), err))
				}
				if restoredRating, found := packet.Rating(); found && restoredRating == *ratingOption {
					if err := packet.RemoveProperty("xmp:Rating"); err != nil {
						return reportError(stderr, fmt.Errorf("%s: %w", frame.OutputSidecar(), err))
					}
				}
				modifiedPackets[index] = packet
			}
		}
		for _, index := range indices {
			frame := frames[index]
			packet := packets[index]
			if _, err := MarkKeyframeWithRating(packet, "default", *ratingOption); err != nil {
				return reportError(stderr, fmt.Errorf("%s: %w", frame.OutputSidecar(), err))
			}
			modifiedPackets[index] = packet
		}
	}
	backups := make(map[int]string, len(modifiedPackets))
	markedIndices := make(map[int]struct{}, len(indices))
	for _, index := range indices {
		markedIndices[index] = struct{}{}
	}
	if *write {
		for index, frame := range frames {
			packet, modified := modifiedPackets[index]
			if !modified {
				continue
			}
			backup, err := WritePacket(frame.OutputSidecar(), packet, !*noBackup)
			if err != nil {
				return reportError(stderr, err)
			}
			backups[index] = backup
		}
	}
	fmt.Fprintf(stdout, "Sequence: %s\n", directory)
	fmt.Fprintf(stdout, "Frames: %d; existing keyframes: %d; new suggestions: %d\n", len(frames), len(existingKeyframes), len(indices))
	if *replaceExisting {
		fmt.Fprintf(stdout, "Selected rating: %d stars; existing marks to replace: %d\n", *ratingOption, existingTargetRating)
	}
	for _, index := range indices {
		frame := frames[index]
		if !*luminanceDeviation {
			fmt.Fprintf(stdout, "%4d %-42s LRT wizard spacing\n", index+1, filepath.Base(frame.Image))
		} else {
			fmt.Fprintf(stdout, "%4d %-42s luminance=%.6f\n", index+1, filepath.Base(frame.Image), math.Exp2(curve[index]))
		}
		if !*write {
			continue
		}
		backup := backups[index]
		if backup != "" {
			fmt.Fprintf(stdout, "  Backup: %s\n", backup)
		}
		fmt.Fprintf(stdout, "  Marked: %s (%d stars)\n", frame.OutputSidecar(), *ratingOption)
	}
	if *replaceExisting && *write {
		for index, frame := range frames {
			if _, marked := markedIndices[index]; marked {
				continue
			}
			if _, changed := modifiedPackets[index]; !changed {
				continue
			}
			backup := backups[index]
			if backup != "" {
				fmt.Fprintf(stdout, "  Backup: %s\n", backup)
			}
			fmt.Fprintf(stdout, "  Cleared: %s (%d-star marker replaced)\n", filepath.Base(frame.OutputSidecar()), *ratingOption)
		}
	}
	if !*write {
		if *replaceExisting {
			fmt.Fprintf(stdout, "Preview only. Pass --write to replace %d existing %d-star marker(s) and mark these frames.\n", existingTargetRating, *ratingOption)
		} else {
			fmt.Fprintln(stdout, "Preview only. Pass --write to mark these frames; other existing keyframes are left unchanged.")
		}
	}
	return 0
}

func runHolyGrail(arguments []string, stdout, stderr io.Writer) int {
	set := newFlagSet("holy-grail", stderr)
	write := set.Bool("write", false, "write exposure compensation and 2*/3* jump keyframes to XMP sidecars")
	noBackup := set.Bool("no-backup", false, "do not create backups when writing")
	lrtInput := set.Bool("lrt-input", false, "use existing lrt:ExternalExposureDefault values and 2*/3*/4* ratings as Holy Grail inputs")
	lrtReduceDeflicker := set.Float64("lrt-reduce-deflicker", 0, "LRT Holy Grail smoothing control for --lrt-input (0 to 1; default 0)")
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
	if *lrtInput && *forceMarkJumps {
		return reportError(stderr, fmt.Errorf("--lrt-input uses existing 2*/3*/4* XMP ratings and cannot be combined with --mark-jumps"))
	}
	lrtReduceDeflickerSet := false
	set.Visit(func(parsed *flag.Flag) {
		lrtReduceDeflickerSet = lrtReduceDeflickerSet || parsed.Name == "lrt-reduce-deflicker"
	})
	if lrtReduceDeflickerSet && !*lrtInput {
		return reportError(stderr, fmt.Errorf("--lrt-reduce-deflicker requires --lrt-input"))
	}
	if math.IsNaN(*lrtReduceDeflicker) || math.IsInf(*lrtReduceDeflicker, 0) || *lrtReduceDeflicker < 0 || *lrtReduceDeflicker > 1 {
		return reportError(stderr, fmt.Errorf("--lrt-reduce-deflicker must be between 0 and 1"))
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
	useCurveOptions := *lrtInput || *autoShape || explicitCurveOptions
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
		curveOptions = &HolyGrailCurveOptions{
			Stretch: *stretch, Rotate: *rotate, ReduceDeflicker: *lrtReduceDeflicker,
			Auto: *autoShape, LRTInput: *lrtInput,
		}
	}
	plan, err := buildHolyGrailPlanWithCurveOptions(frames, *ignoreAperture, *reset, *forceMarkJumps, *noMarkJumps, curveOptions)
	if err != nil {
		return reportError(stderr, err)
	}
	changes := plan.Changes
	fmt.Fprintf(stdout, "Sequence: %s\n", directory)
	if plan.CurveOptions != nil {
		fmt.Fprintf(stdout, "Curve adjustments: stretch=%+.2f rotate=%+.2f\n", plan.CurveOptions.Stretch, plan.CurveOptions.Rotate)
		if plan.CurveOptions.LRTInput {
			fmt.Fprintf(stdout, "Curve input: lrt:ExternalExposureDefault / 4; reduce-deflicker=%0.2f\n", plan.CurveOptions.ReduceDeflicker)
			fmt.Fprintln(stdout, "Existing 2*/3*/4* ratings are used for HG correction transitions; no ratings will be changed.")
		}
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
	} else if !*reset && !*lrtInput && !*noMarkJumps && !plan.ManualMode && !*forceMarkJumps {
		fmt.Fprintln(stdout, "EXIF does not confirm Manual mode; 2*/3* marks are omitted. Pass --mark-jumps to force them.")
	}
	for _, change := range changes {
		if *reset {
			fmt.Fprintf(stdout, "  %-42s", filepath.Base(change.Frame.Image))
			if change.ExposureChanged {
				fmt.Fprintf(stdout, " %s %+.4f -> %+.4f EV", xmpExposureTargetLabel(change.Target), change.ExposureBefore, change.ExposureAfter)
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
				fmt.Fprintf(stdout, " correction=%+.4f EV %s=%+.4f EV", change.Compensation, xmpExposureTargetLabel(change.Target), change.ExposureAfter)
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
