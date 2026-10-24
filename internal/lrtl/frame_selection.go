package lrtl

import (
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type frameSelectionOptions struct {
	Manifest, FilenameContains string
	Ratings                    []int
	Invert                     bool
}

type frameSelectionFlags struct {
	manifest, filename, ratings *string
	invert                      *bool
}

func registerFrameSelectionFlags(set *flag.FlagSet) frameSelectionFlags {
	return frameSelectionFlags{
		set.String("selection", "", "included frames from an existing selection manifest"),
		set.String("filename-contains", "", "case-insensitive filename substring"),
		set.String("ratings", "", "comma-separated XMP ratings (-1 through 5); missing rating is 0"),
		set.Bool("invert", false, "invert the intersection of selection filters"),
	}
}

func (flags frameSelectionFlags) options(set *flag.FlagSet) (frameSelectionOptions, error) {
	options := frameSelectionOptions{Manifest: *flags.manifest, FilenameContains: *flags.filename, Invert: *flags.invert}
	emptyRatings := false
	set.Visit(func(f *flag.Flag) {
		if f.Name == "ratings" && strings.TrimSpace(*flags.ratings) == "" {
			emptyRatings = true
		}
	})
	if emptyRatings {
		return options, fmt.Errorf("--ratings requires at least one rating in -1..5")
	}
	if *flags.ratings != "" {
		seen := map[int]bool{}
		for _, part := range strings.Split(*flags.ratings, ",") {
			n, err := strconv.Atoi(strings.TrimSpace(part))
			if err != nil || n < -1 || n > 5 || seen[n] {
				return options, fmt.Errorf("--ratings must contain distinct integers in -1..5")
			}
			seen[n] = true
			options.Ratings = append(options.Ratings, n)
		}
	}
	return options, nil
}

func filterSequenceFrames(frames []Frame, directory string, options frameSelectionOptions) ([]Frame, error) {
	allowed := map[string]bool{}
	if options.Manifest != "" {
		included, err := loadSelectionFrames(frames, options.Manifest, directory, true)
		if err != nil {
			return nil, err
		}
		for _, frame := range included {
			allowed[frame.Image] = true
		}
	}
	ratings := map[int]bool{}
	for _, rating := range options.Ratings {
		if rating < -1 || rating > 5 || ratings[rating] {
			return nil, fmt.Errorf("selection ratings must be distinct integers in -1..5")
		}
		ratings[rating] = true
	}
	selected := make([]Frame, 0, len(frames))
	for _, frame := range frames {
		include := options.Manifest == "" || allowed[frame.Image]
		include = include && strings.Contains(strings.ToLower(filepath.Base(frame.Image)), strings.ToLower(options.FilenameContains))
		if len(ratings) > 0 && include {
			packet, err := PacketForFrame(frame)
			if err != nil {
				return nil, err
			}
			text, found, err := readRootRating(packet)
			if err != nil {
				return nil, err
			}
			rating := 0
			if found {
				n, valid := scalarNumber(text)
				if !valid || math.Trunc(n) != n || n < -1 || n > 5 {
					return nil, fmt.Errorf("%s: invalid xmp:Rating %q", frame.Image, text)
				}
				rating = int(n)
			} else if property := packet.findRootProperty(xmpNamespace, "Rating"); property.element != nil || property.attribute != nil {
				return nil, fmt.Errorf("%s: xmp:Rating must be scalar", frame.Image)
			}
			include = include && ratings[rating]
		}
		if options.Invert {
			include = !include
		}
		if include {
			selected = append(selected, frame)
		}
	}
	return selected, nil
}

func runSelect(arguments []string, stdout, stderr io.Writer) int {
	set := newFlagSet("select", stderr)
	filters := registerFrameSelectionFlags(set)
	output := set.String("output", "", "write a selection manifest usable by render/export/copy-sequence")
	overwrite := set.Bool("overwrite", false, "replace an existing output manifest")
	if err := set.Parse(flagFirst(arguments, map[string]bool{"--selection": true, "-selection": true, "--filename-contains": true, "-filename-contains": true, "--ratings": true, "-ratings": true, "--output": true, "-output": true})); err != nil {
		if done, code := finishFlagParse(set, err); done {
			return code
		}
	}
	if set.NArg() != 1 || *overwrite && *output == "" {
		return reportError(stderr, fmt.Errorf("Usage: lrtl select DIRECTORY [--selection INPUT.json] [--filename-contains TEXT] [--ratings LIST] [--invert] [--output FILTER.json] [--overwrite]"))
	}
	options, err := filters.options(set)
	if err != nil {
		return reportError(stderr, err)
	}
	frames, err := DiscoverSequence(set.Arg(0))
	if err != nil {
		return reportError(stderr, err)
	}
	if len(frames) == 0 {
		return reportError(stderr, fmt.Errorf("sequence contains no images"))
	}
	selected, err := filterSequenceFrames(frames, set.Arg(0), options)
	if err != nil {
		return reportError(stderr, err)
	}
	fmt.Fprintf(stdout, "Selected: %d/%d frames\n", len(selected), len(frames))
	for _, frame := range selected {
		fmt.Fprintln(stdout, filepath.Base(frame.Image))
	}
	if *output != "" {
		included := map[string]bool{}
		for _, frame := range selected {
			included[frame.Image] = true
		}
		manifest := &sequenceSelectionManifest{Version: selectionManifestVersion, Directory: set.Arg(0), Frames: make([]longTermFrameMetrics, len(frames))}
		for i, frame := range frames {
			info, err := os.Stat(frame.Image)
			if err != nil {
				return reportError(stderr, err)
			}
			if !info.Mode().IsRegular() {
				return reportError(stderr, fmt.Errorf("not a regular image file: %s", frame.Image))
			}
			manifest.Frames[i] = longTermFrameMetrics{Image: filepath.Base(frame.Image), Included: included[frame.Image], SizeBytes: info.Size(), ModifiedUnixNano: info.ModTime().UnixNano()}
		}
		if err := writeSelectionManifest(*output, manifest, *overwrite); err != nil {
			return reportError(stderr, err)
		}
		fmt.Fprintf(stdout, "Wrote selection: %s\n", *output)
	}
	return 0
}
