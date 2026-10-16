package lrtl

import (
	"flag"
	"fmt"
	"math"
)

type holyGrailCommandFlags struct {
	write              *bool
	noBackup           *bool
	lrtInput           *bool
	lrtReduceDeflicker *float64
	ignoreAperture     *bool
	forceMarkJumps     *bool
	noMarkJumps        *bool
	stretch            *float64
	rotate             *float64
	autoShape          *bool
	reset              *bool
}

func registerHolyGrailFlags(set *flag.FlagSet) *holyGrailCommandFlags {
	f := &holyGrailCommandFlags{}
	f.write = set.Bool("write", false, "write exposure compensation and 2*/3* jump keyframes to XMP sidecars")
	f.noBackup = set.Bool("no-backup", false, "do not create backups when writing")
	f.lrtInput = set.Bool("lrt-input", false, "use existing lrt:ExternalExposureDefault values and 2*/3*/4* ratings as Holy Grail inputs")
	f.lrtReduceDeflicker = set.Float64("lrt-reduce-deflicker", 0, "LRT Holy Grail smoothing control for --lrt-input (0 to 1; default 0)")
	f.ignoreAperture = set.Bool("ignore-aperture", false, "ignore FNumber when aperture remained fixed")
	f.forceMarkJumps = set.Bool("mark-jumps", false, "force 2*/3* marks when EXIF does not confirm Manual mode")
	f.noMarkJumps = set.Bool("no-mark-jumps", false, "do not mark camera-setting changes as 2*/3* keyframe pairs")
	f.stretch = set.Float64("stretch", 0, "add LRT-style normalized Holy Grail curve stretch (-1.5 to +1.5)")
	f.rotate = set.Float64("rotate", 0, "add LRT-style linear Holy Grail curve rotation (-4 to +4)")
	f.autoShape = set.Bool("auto-shape", false, "search curve stretch and rotation using L1 smoothness scores")
	f.reset = set.Bool("reset", false, "remove lrtl-managed Holy Grail compensation")
	return f
}

func validateHolyGrailFlags(set *flag.FlagSet, f *holyGrailCommandFlags) (*HolyGrailCurveOptions, error) {
	if *f.forceMarkJumps && *f.noMarkJumps {
		return nil, fmt.Errorf("--mark-jumps and --no-mark-jumps cannot be used together")
	}
	if *f.lrtInput && *f.forceMarkJumps {
		return nil, fmt.Errorf("--lrt-input uses existing 2*/3*/4* XMP ratings and cannot be combined with --mark-jumps")
	}
	lrtReduceDeflickerSet := false
	set.Visit(func(parsed *flag.Flag) {
		lrtReduceDeflickerSet = lrtReduceDeflickerSet || parsed.Name == "lrt-reduce-deflicker"
	})
	if lrtReduceDeflickerSet && !*f.lrtInput {
		return nil, fmt.Errorf("--lrt-reduce-deflicker requires --lrt-input")
	}
	if math.IsNaN(*f.lrtReduceDeflicker) || math.IsInf(*f.lrtReduceDeflicker, 0) || *f.lrtReduceDeflicker < 0 || *f.lrtReduceDeflicker > 1 {
		return nil, fmt.Errorf("--lrt-reduce-deflicker must be between 0 and 1")
	}
	explicitCurveOptions := false
	set.Visit(func(parsed *flag.Flag) {
		if parsed.Name == "stretch" || parsed.Name == "rotate" {
			explicitCurveOptions = true
		}
	})
	if *f.autoShape && explicitCurveOptions {
		return nil, fmt.Errorf("--auto-shape cannot be combined with --stretch or --rotate")
	}
	useCurveOptions := *f.lrtInput || *f.autoShape || explicitCurveOptions
	if useCurveOptions && (math.IsNaN(*f.stretch) || math.IsInf(*f.stretch, 0) || math.IsNaN(*f.rotate) || math.IsInf(*f.rotate, 0) || *f.stretch < -1.5 || *f.stretch > 1.5 || *f.rotate < -4 || *f.rotate > 4) {
		return nil, fmt.Errorf("--stretch must be between -1.5 and +1.5 and --rotate between -4 and +4")
	}
	if useCurveOptions && *f.reset {
		return nil, fmt.Errorf("--stretch and --rotate cannot be used with --reset")
	}
	var curveOptions *HolyGrailCurveOptions
	if useCurveOptions {
		curveOptions = &HolyGrailCurveOptions{
			Stretch: *f.stretch, Rotate: *f.rotate, ReduceDeflicker: *f.lrtReduceDeflicker,
			Auto: *f.autoShape, LRTInput: *f.lrtInput,
		}
	}
	return curveOptions, nil
}
