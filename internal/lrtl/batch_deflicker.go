package lrtl

import (
	"flag"
	"fmt"
	"image"
	"io"
	"path/filepath"
	"sync"
)

type batchDeflickerResult struct {
	frames, passes int
	changes        []DeflickerChange
	err            error
	native         nativeDeflickerOptions
	constant       bool
}

func runBatchDeflicker(arguments []string, stdout, stderr io.Writer) int {
	set := newFlagSet("batch-deflicker", stderr)
	nativeFlags := registerNativeDeflickerFlags(set)
	lrtReference := set.Bool("lrt-reference", false, "use each sequence's saved LRT referenceBoxes")
	lrtPreview := set.String("lrt-preview", "", "analyze existing LRT camera or visual preview cache")
	recursive := set.Bool("recursive", false, "find sequence directories at any depth")
	workers := set.Int("workers", 1, "number of sequences to analyze concurrently (1-64)")
	write := set.Bool("write", false, "save final corrections after all sequences pass content validation")
	noBackup := set.Bool("no-backup", false, "do not create sidecar backups")
	reset := set.Bool("reset", false, "remove lrtl-managed deflicker corrections without decoding images")
	referenceValue := set.String("reference", "", "reference area x,y,width,height; defaults to the full frame")
	radius := set.Int("smooth", 5, "deflicker smoothing radius in frames (1-1000)")
	smoother := set.String("smoother", "gaussian", "deflicker smoother: gaussian or lowess")
	passes := set.Int("passes", 1, "deflicker passes (1-8)")
	constant := set.Bool("constant", false, "deflicker toward constant luminance")
	rawPreview := set.Bool("raw-preview", false, "analyze embedded RAW previews")
	rawDevelop := set.Bool("raw-develop", false, "analyze RAW developed by Adobe DNG Converter or explicit DNGLab")
	dnglab := set.String("dnglab", "", "optional DNGLab executable for --raw-develop")
	dngConverter := set.String("dng-converter", "", "Adobe DNG Converter executable")
	applyXMPExposure := set.Bool("apply-xmp-exposure", false, "include XMP exposure in analysis")
	applyXMPBasic := set.Bool("apply-xmp-basic", false, "include supported XMP basic development in analysis")
	applyXMPCrop := set.Bool("apply-xmp-crop", false, "include XMP crop in analysis")
	if err := set.Parse(flagFirst(arguments, map[string]bool{
		"--workers": true, "-workers": true, "--reference": true, "-reference": true,
		"--lrt-preview": true, "-lrt-preview": true,
		"--preview-width-camera": true, "-preview-width-camera": true,
		"--calculation": true, "-calculation": true, "--lrt-smoothing": true, "-lrt-smoothing": true, "--lrt-accuracy": true, "-lrt-accuracy": true, "--ignore-blacks": true, "-ignore-blacks": true, "--ignore-whites": true, "-ignore-whites": true, "--lrt-preferences": true, "-lrt-preferences": true,
		"--smooth": true, "-smooth": true, "--smoother": true, "-smoother": true,
		"--passes": true, "-passes": true, "--dnglab": true, "-dnglab": true,
		"--dng-converter": true, "-dng-converter": true,
	})); err != nil {
		if done, code := finishFlagParse(set, err); done {
			return code
		}
		return reportError(stderr, err)
	}
	if set.NArg() != 1 {
		return reportError(stderr, fmt.Errorf("expected one root directory containing image sequences"))
	}
	if *workers < 1 || *workers > 64 {
		return reportError(stderr, fmt.Errorf("--workers must be between 1 and 64"))
	}
	if *radius < 1 || *radius > 1000 || *passes < 1 || *passes > 8 {
		return reportError(stderr, fmt.Errorf("--smooth must be 1-1000 and --passes must be 1-8"))
	}
	if *reset {
		analysisOption := false
		set.Visit(func(f *flag.Flag) {
			switch f.Name {
			case "reset", "write", "no-backup", "recursive", "workers":
			default:
				analysisOption = true
			}
		})
		if analysisOption {
			return reportError(stderr, fmt.Errorf("--reset cannot be combined with analysis options"))
		}
	}
	var reference image.Rectangle
	var err error
	if *referenceValue != "" {
		reference, err = parseReferenceArea(*referenceValue)
		if err != nil {
			return reportError(stderr, err)
		}
	}
	input := imageInputOptions{LRTPreview: *lrtPreview, RawPreview: *rawPreview, RawDevelop: *rawDevelop, DNGLabPath: *dnglab, DNGConverterPath: *dngConverter,
		ApplyXMPExposure: *applyXMPExposure, ApplyXMPBasic: *applyXMPBasic, ApplyXMPCrop: *applyXMPCrop}
	input.LRTReference = *lrtReference
	if err := nativeFlags.apply(set, &input); err != nil {
		return reportError(stderr, err)
	}
	if err := validateImageInputOptions(input); err != nil {
		return reportError(stderr, err)
	}
	if _, err := deflickerGainsWithSmoother([]float64{1}, *radius, *passes, *constant, *smoother); err != nil {
		return reportError(stderr, err)
	}
	root, err := filepath.Abs(set.Arg(0))
	if err != nil {
		return reportError(stderr, err)
	}
	directories, err := discoverBatchSequences(root, "", *recursive)
	if err != nil {
		return reportError(stderr, err)
	}
	if len(directories) == 0 {
		return reportError(stderr, fmt.Errorf("no image sequence directories found under %s", root))
	}
	results := make([]batchDeflickerResult, len(directories))
	queue := make(chan int)
	var wait sync.WaitGroup
	for worker := 0; worker < min(*workers, len(results)); worker++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for index := range queue {
				result := &results[index]
				frames, err := DiscoverSequence(directories[index])
				result.err, result.frames = err, len(frames)
				if err != nil {
					continue
				}
				if *reset {
					result.changes, result.err = buildDeflickerChangesForInput(frames, nil, *radius, *passes, *constant, true, *smoother, false)
				} else {
					var plan *deflickerSequencePlan
					plan, result.err = planDeflickerSequence(frames, reference, input, *radius, *passes, *constant, *smoother)
					if result.err == nil {
						result.changes, result.passes = plan.Changes, len(plan.Measurements)
						result.native, result.constant = plan.NativeOptions, plan.Constant
					}
				}
				if result.err == nil {
					result.err = applyDeflickerChanges(result.changes)
				}
				if result.err == nil {
					for _, change := range result.changes {
						if _, err := change.Packet.Serialize(); err != nil {
							result.err = fmt.Errorf("%s: %w", change.Frame.OutputSidecar(), err)
							break
						}
					}
				}
			}
		}()
	}
	for i := range results {
		queue <- i
	}
	close(queue)
	wait.Wait()
	failures, frames, corrections := 0, 0, 0
	for i, result := range results {
		fmt.Fprintf(stdout, "[%d/%d] %s\n", i+1, len(results), directories[i])
		if result.err != nil {
			failures++
			fmt.Fprintf(stderr, "%s: %v\n", directories[i], result.err)
			continue
		}
		frames += result.frames
		corrections += len(result.changes)
		fmt.Fprintf(stdout, "Frames: %d; proposed sidecars: %d; measured passes: %d; reset: %t\n", result.frames, len(result.changes), result.passes, *reset)
		if result.native.Enabled {
			fmt.Fprintf(stdout, "Calculation: lrt-7.5.5; bandwidth: %g; accuracy: %d; constant: %t\n", result.native.Smoothing, result.native.Accuracy, result.constant)
		}
		for _, change := range result.changes {
			fmt.Fprintf(stdout, "  %s: %s %+.4f -> %+.4f EV\n", filepath.Base(change.Frame.Image), xmpExposureTargetLabel(change.Target), change.ExposureBefore, change.ExposureAfter)
		}
	}
	if failures > 0 {
		fmt.Fprintf(stderr, "Batch deflicker: %d sequence(s) failed validation; no sidecars written.\n", failures)
		return 1
	}
	if !*write {
		fmt.Fprintf(stdout, "Dry run. Batch deflicker: %d sequence(s), %d frame(s), %d proposed sidecar(s). Pass --write to save.\n", len(results), frames, corrections)
		return 0
	}
	written := 0
	for _, result := range results {
		for _, change := range result.changes {
			path := change.Frame.OutputSidecar()
			backup, err := WritePacket(path, change.Packet, !*noBackup)
			if err != nil {
				fmt.Fprintf(stderr, "Batch deflicker stopped after %d saved sidecar(s).\n", written)
				return reportError(stderr, err)
			}
			if backup != "" {
				fmt.Fprintf(stdout, "Backup: %s\n", backup)
			}
			fmt.Fprintf(stdout, "Saved: %s\n", path)
			written++
		}
	}
	fmt.Fprintf(stdout, "Batch deflicker: %d sequence(s), %d frame(s), updated %d sidecar(s).\n", len(results), frames, written)
	return 0
}
