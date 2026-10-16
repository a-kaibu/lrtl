package lrtl

import (
	"flag"
	"fmt"
	"math"
)

type nativeDeflickerOptions struct {
	Enabled   bool
	Smoothing float64
	Accuracy  int
}

func (o nativeDeflickerOptions) validate() error {
	if !o.Enabled {
		return nil
	}
	if math.IsNaN(o.Smoothing) || math.IsInf(o.Smoothing, 0) || o.Smoothing < 0 || o.Smoothing > 2 || o.Accuracy < -1 || o.Accuracy > 1 {
		return fmt.Errorf("--lrt-smoothing must be finite and 0-2; --lrt-accuracy must be -1, 0 or 1")
	}
	return nil
}

type nativeDeflickerFlags struct {
	saved                  *bool
	calculation            *string
	smoothing              *float64
	accuracy, black, white *int
	hard                   *bool
}

func registerNativeDeflickerFlags(set *flag.FlagSet) nativeDeflickerFlags {
	return nativeDeflickerFlags{
		saved:       set.Bool("lrt-deflicker-settings", false, "read saved LRT Deflicker controls and sequence order"),
		calculation: set.String("calculation", "lrtl", "Deflicker calculation: lrtl or lrt-7.5.5"),
		smoothing:   set.Float64("lrt-smoothing", .5, "LRT bandwidth factor (0-2), with --calculation lrt-7.5.5"),
		accuracy:    set.Int("lrt-accuracy", 0, "LRT accuracy: -1 low, 0 normal, 1 high"),
		black:       set.Int("ignore-blacks", 0, "black histogram range percentage (0-100) in LRT calculation"),
		white:       set.Int("ignore-whites", 0, "white histogram range percentage (0-100) in LRT calculation"),
		hard:        set.Bool("hard-cut", false, "use hard histogram cutoffs in LRT calculation"),
	}
}

func (f nativeDeflickerFlags) apply(set *flag.FlagSet, input *imageInputOptions) error {
	if *f.calculation != "lrtl" && *f.calculation != "lrt-7.5.5" {
		return fmt.Errorf("--calculation must be lrtl or lrt-7.5.5")
	}
	native := *f.calculation == "lrt-7.5.5" || *f.saved
	var problem error
	histogram := false
	set.Visit(func(flag *flag.Flag) {
		if *f.saved {
			switch flag.Name {
			case "lrt-smoothing", "lrt-accuracy", "constant", "passes", "smooth", "smoother":
				problem = fmt.Errorf("--%s cannot be combined with --lrt-deflicker-settings", flag.Name)
			case "calculation":
				if *f.calculation != "lrt-7.5.5" {
					problem = fmt.Errorf("--lrt-deflicker-settings requires lrt-7.5.5 calculation")
				}
			}
		}
		switch flag.Name {
		case "lrt-smoothing", "lrt-accuracy", "ignore-blacks", "ignore-whites", "hard-cut":
			if !native {
				problem = fmt.Errorf("--%s requires --calculation lrt-7.5.5", flag.Name)
			}
		case "smooth", "smoother":
			if native {
				problem = fmt.Errorf("--%s is for lrtl calculation; use --lrt-smoothing with lrt-7.5.5", flag.Name)
			}
		}
		if flag.Name == "ignore-blacks" || flag.Name == "ignore-whites" || flag.Name == "hard-cut" {
			histogram = true
		}
	})
	if problem != nil {
		return problem
	}
	input.NativeDeflicker = nativeDeflickerOptions{native, *f.smoothing, *f.accuracy}
	input.LRTDeflickerSettings = *f.saved
	input.PreviewHistogram = previewHistogramOptions{Enabled: histogram, IgnoreBlacks: *f.black, IgnoreWhites: *f.white, HardCut: *f.hard}
	return validateImageInputOptions(*input)
}
