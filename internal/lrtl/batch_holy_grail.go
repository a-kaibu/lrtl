package lrtl

import (
	"fmt"
	"io"
	"path/filepath"
	"sync"
)

type batchHolyGrailResult struct {
	frames int
	plan   *HolyGrailPlan
	err    error
}

func runBatchHolyGrail(arguments []string, stdout, stderr io.Writer) int {
	set := newFlagSet("batch-holy-grail", stderr)
	recursive := set.Bool("recursive", false, "find sequence directories at any depth")
	workers := set.Int("workers", 1, "number of sequences to analyze concurrently (1-64)")
	f := registerHolyGrailFlags(set)
	if err := set.Parse(flagFirst(arguments, map[string]bool{
		"--workers": true, "-workers": true, "--stretch": true, "-stretch": true,
		"--rotate": true, "-rotate": true, "--lrt-reduce-deflicker": true, "-lrt-reduce-deflicker": true,
		"--lrt-preview": true, "-lrt-preview": true,
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
	curveOptions, err := validateHolyGrailFlags(set, f)
	if err != nil {
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
	results := make([]batchHolyGrailResult, len(directories))
	queue := make(chan int)
	var wait sync.WaitGroup
	for worker := 0; worker < min(*workers, len(results)); worker++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for index := range queue {
				result := &results[index]
				frames, err := DiscoverSequence(directories[index])
				result.frames, result.err = len(frames), err
				if err != nil {
					continue
				}
				sequenceCurveOptions := curveOptions
				if *f.lrtSettings {
					frames, sequenceCurveOptions, result.err = applyLRTSequenceHolyGrailSettings(frames)
					if result.err != nil {
						continue
					}
					sequenceCurveOptions.LRTPreview = *f.lrtPreview
					sequenceCurveOptions.LRTReference = *f.lrtReference
				}
				result.plan, result.err = buildHolyGrailPlanWithCurveOptions(frames, *f.ignoreAperture, *f.reset, *f.forceMarkJumps, *f.noMarkJumps, sequenceCurveOptions)
				if result.err == nil {
					result.err = applyHolyGrailPlan(result.plan.Changes)
				}
				if result.err == nil {
					for _, change := range result.plan.Changes {
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
	failures, frameCount, sidecarCount := 0, 0, 0
	for i, result := range results {
		fmt.Fprintf(stdout, "[%d/%d] %s\n", i+1, len(results), directories[i])
		if result.err != nil {
			failures++
			fmt.Fprintf(stderr, "%s: %v\n", directories[i], result.err)
			continue
		}
		frameCount += result.frames
		sidecarCount += len(result.plan.Changes)
		fmt.Fprintf(stdout, "Frames: %d; proposed sidecars: %d; mark jumps: %t; reset: %t\n", result.frames, len(result.plan.Changes), result.plan.MarkJumps, *f.reset)
		if curve := result.plan.CurveOptions; curve != nil {
			fmt.Fprintf(stdout, "Curve: stretch=%+.4f rotate=%+.4f reduce-deflicker=%.4f; lrt-input: %t; lrt-preview: %s; reference: %t; automatic: %t\n", curve.Stretch, curve.Rotate, curve.ReduceDeflicker, curve.LRTInput, curve.LRTPreview, curve.LRTReference, result.plan.AutoCurve)
		}
		for _, change := range result.plan.Changes {
			fmt.Fprintf(stdout, "  %s: %s %+.4f -> %+.4f EV; HG mark: %d; restore rating: %t\n", filepath.Base(change.Frame.Image), xmpExposureTargetLabel(change.Target), change.ExposureBefore, change.ExposureAfter, change.KeyframeRating, change.RestoreKeyframe)
		}
	}
	if failures > 0 {
		fmt.Fprintf(stderr, "Batch Holy Grail: %d sequence(s) failed validation; no sidecars written.\n", failures)
		return 1
	}
	if !*f.write {
		fmt.Fprintf(stdout, "Dry run. Batch Holy Grail: %d sequence(s), %d frame(s), %d proposed sidecar(s). Pass --write to save.\n", len(results), frameCount, sidecarCount)
		return 0
	}
	written := 0
	for _, result := range results {
		for _, change := range result.plan.Changes {
			path := change.Frame.OutputSidecar()
			backup, err := WritePacket(path, change.Packet, !*f.noBackup)
			if err != nil {
				fmt.Fprintf(stderr, "Batch Holy Grail stopped after %d saved sidecar(s).\n", written)
				return reportError(stderr, err)
			}
			if backup != "" {
				fmt.Fprintf(stdout, "Backup: %s\n", backup)
			}
			fmt.Fprintf(stdout, "Saved: %s\n", path)
			written++
		}
	}
	fmt.Fprintf(stdout, "Batch Holy Grail: %d sequence(s), %d frame(s), updated %d sidecar(s).\n", len(results), frameCount, written)
	return 0
}
