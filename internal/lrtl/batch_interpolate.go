package lrtl

import (
	"fmt"
	"io"
	"path/filepath"
	"sync"
)

type batchInterpolationResult struct {
	plan    *InterpolationPlan
	outputs []packetOutput
	err     error
}

func runBatchInterpolate(arguments []string, stdout, stderr io.Writer) int {
	set := newFlagSet("batch-interpolate", stderr)
	recursive := set.Bool("recursive", false, "find sequence directories at any depth")
	workers := set.Int("workers", 1, "number of sequences to analyze concurrently (1-64)")
	smoothness := set.Int("smoothness", 50, "LRT-compatible spline smoothness (0-100)")
	maskFormat := set.String("mask-format", "auto", "angle postprocessing layout: auto, legacy or modern")
	write := set.Bool("write", false, "write interpolated sidecars after all sequences pass validation")
	noBackup := set.Bool("no-backup", false, "do not create sidecar backups")
	if err := set.Parse(flagFirst(arguments, map[string]bool{
		"--workers": true, "-workers": true, "--smoothness": true, "-smoothness": true,
		"--mask-format": true, "-mask-format": true,
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
	if *smoothness < 0 || *smoothness > 100 {
		return reportError(stderr, fmt.Errorf("--smoothness must be between 0 and 100"))
	}
	if _, err := transitionMaskFormat(nil, *maskFormat); err != nil {
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
	results := make([]batchInterpolationResult, len(directories))
	queue := make(chan int)
	var wait sync.WaitGroup
	for worker := 0; worker < min(*workers, len(directories)); worker++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for index := range queue {
				frames, err := DiscoverSequence(directories[index])
				if err != nil {
					results[index].err = err
					continue
				}
				plan, err := buildPlanWithMaskFormat(frames, *smoothness, *maskFormat)
				results[index].plan, results[index].err = plan, err
				if err == nil {
					results[index].outputs, results[index].err = applyPlan(plan)
					if results[index].err == nil {
						for _, output := range results[index].outputs {
							if _, err := output.Packet.Serialize(); err != nil {
								results[index].err = fmt.Errorf("%s: %w", output.Path, err)
								break
							}
						}
					}
				}
			}
		}()
	}
	for index := range directories {
		queue <- index
	}
	close(queue)
	wait.Wait()

	failures, frameCount, sidecarCount := 0, 0, 0
	for index, result := range results {
		fmt.Fprintf(stdout, "[%d/%d] %s\n", index+1, len(results), directories[index])
		if result.err != nil {
			failures++
			fmt.Fprintf(stderr, "%s: %v\n", directories[index], result.err)
			continue
		}
		frameCount += result.plan.FrameCount
		sidecarCount += len(result.outputs)
		fmt.Fprintf(stdout, "Frames: %d; effective keyframes: %d; proposed sidecars: %d; properties: %d; spline smoothness: %d\n", result.plan.FrameCount, len(result.plan.KeyframeIndices), len(result.outputs), result.plan.PropertyCount(), *smoothness)
		fmt.Fprintf(stdout, "Mask angle format: %s\n", result.plan.MaskFormat)
	}
	if failures > 0 {
		fmt.Fprintf(stderr, "Batch interpolation: %d sequence(s) failed validation; no sidecars written.\n", failures)
		return 1
	}
	if !*write {
		fmt.Fprintf(stdout, "Dry run. Batch interpolation: %d sequence(s), %d frame(s), %d proposed sidecar(s). Pass --write to save.\n", len(results), frameCount, sidecarCount)
		return 0
	}
	// Save in discovery order after every sequence has passed content validation.
	// WritePacket supplies the same backup and atomic-file policy as interpolate.
	written := 0
	for _, result := range results {
		for _, output := range result.outputs {
			backup, err := WritePacket(output.Path, output.Packet, !*noBackup)
			if err != nil {
				fmt.Fprintf(stderr, "Batch interpolation stopped after %d saved sidecar(s).\n", written)
				return reportError(stderr, err)
			}
			if backup != "" {
				fmt.Fprintf(stdout, "Backup: %s\n", backup)
			}
			fmt.Fprintf(stdout, "Saved: %s\n", output.Path)
			written++
		}
	}
	fmt.Fprintf(stdout, "Batch interpolation: %d sequence(s), %d frame(s), updated %d sidecar(s).\n", len(results), frameCount, written)
	return 0
}
