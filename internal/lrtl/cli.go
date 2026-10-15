package lrtl

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const version = "0.3.0"

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
	case "unmark":
		return runUnmark(arguments[1:], stdout, stderr)
	case "interpolate":
		return runInterpolate(arguments[1:], stdout, stderr)
	case "render":
		return runRender(arguments[1:], stdout, stderr)
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
	fmt.Fprintln(writer, "  unmark IMAGE [--force-rating]          remove a keyframe mark")
	fmt.Fprintln(writer, "  interpolate DIRECTORY [--write]        preview or write keyframe transitions")
	fmt.Fprintln(writer, "  render DIRECTORY --output FILE.avi    render JPEG/PNG frames to MJPEG AVI")
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
				marker = "-"
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
	quality := set.Int("quality", 90, "JPEG quality for PNG frames (1-100)")
	overwrite := set.Bool("overwrite", false, "replace an existing output file")
	parseArgs := flagFirst(arguments, map[string]bool{
		"--output": true, "-output": true,
		"--fps": true, "-fps": true,
		"--quality": true, "-quality": true,
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
	directory := set.Arg(0)
	frames, err := DiscoverSequence(directory)
	if err != nil {
		return reportError(stderr, err)
	}
	options := RenderOptions{FPS: *fps, Quality: *quality, Overwrite: *overwrite}
	if err := RenderSequence(frames, *output, options); err != nil {
		return reportError(stderr, err)
	}
	fmt.Fprintf(stdout, "Rendered %d frames at %d fps: %s\n", len(frames), *fps, *output)
	return 0
}

func reportError(stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "lrtl: error: %v\n", err)
	return 2
}
