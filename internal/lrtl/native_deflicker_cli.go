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
	preferences            *string
	previewWidth           *int
	smoothing              *float64
	accuracy, black, white *int
	hard                   *bool
}

func registerNativeDeflickerFlags(set *flag.FlagSet) nativeDeflickerFlags {
	return nativeDeflickerFlags{
		saved:        set.Bool("lrt-deflicker-settings", false, "read saved LRT Deflicker controls and sequence order"),
		calculation:  set.String("calculation", "lrtl", "Deflicker calculation: lrtl or lrt-7.5.5"),
		preferences:  set.String("lrt-preferences", "", "read LRT global Deflicker histogram preferences from a JSON file"),
		previewWidth: set.Int("preview-width-camera", 0, "LRT camera-preview width for lrt-7.5.5 measurements"),
		smoothing:    set.Float64("lrt-smoothing", .5, "LRT bandwidth factor (0-2), with --calculation lrt-7.5.5"),
		accuracy:     set.Int("lrt-accuracy", 0, "LRT accuracy: -1 low, 0 normal, 1 high"),
		black:        set.Int("ignore-blacks", 0, "black histogram range percentage (0-100) in LRT calculation"),
		white:        set.Int("ignore-whites", 0, "white histogram range percentage (0-100) in LRT calculation"),
		hard:         set.Bool("hard-cut", false, "use hard histogram cutoffs in LRT calculation"),
	}
}

func (f nativeDeflickerFlags) apply(set *flag.FlagSet, input *imageInputOptions) error {
	if *f.calculation != "lrtl" && *f.calculation != "lrt-7.5.5" {
		return fmt.Errorf("--calculation must be lrtl or lrt-7.5.5")
	}
	native := *f.calculation == "lrt-7.5.5" || *f.saved
	var problem error
	explicitHistogram := false
	explicitThresholds := false
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
		case "lrt-smoothing", "lrt-accuracy", "ignore-blacks", "ignore-whites", "hard-cut", "preview-width-camera":
			if !native {
				problem = fmt.Errorf("--%s requires --calculation lrt-7.5.5", flag.Name)
			}
		case "lrt-preferences":
			if !native {
				problem = fmt.Errorf("--lrt-preferences requires --calculation lrt-7.5.5")
			}
		case "smooth", "smoother":
			if native {
				problem = fmt.Errorf("--%s is for lrtl calculation; use --lrt-smoothing with lrt-7.5.5", flag.Name)
			}
		}
		if flag.Name == "ignore-blacks" || flag.Name == "ignore-whites" || flag.Name == "hard-cut" {
			explicitHistogram = true
		}
		if flag.Name == "ignore-blacks" || flag.Name == "ignore-whites" {
			explicitThresholds = true
		}
	})
	if problem != nil {
		return problem
	}
	input.NativeDeflicker = nativeDeflickerOptions{native, *f.smoothing, *f.accuracy}
	input.PreviewWidthCamera = *f.previewWidth
	input.LRTDeflickerSettings = *f.saved
	histogram := previewHistogramOptions{}
	if *f.preferences != "" {
		preferences, err := readLRTDeflickerPreferences(*f.preferences)
		if err != nil {
			return err
		}
		input.LRTDeflickerPreferences = &preferences
		histogram = preferences.histogramOptions()
	}
	if explicitHistogram {
		if !histogram.FromLRTPreferences {
			histogram.IgnoreBlacks, histogram.IgnoreWhites, histogram.HardCut = *f.black, *f.white, *f.hard
		} else {
			set.Visit(func(parsed *flag.Flag) {
				switch parsed.Name {
				case "ignore-blacks":
					histogram.IgnoreBlacks = *f.black
				case "ignore-whites":
					histogram.IgnoreWhites = *f.white
				case "hard-cut":
					histogram.HardCut = *f.hard
				}
			})
		}
		histogram.Enabled = true
		histogram.Explicit = true
		histogram.ExplicitThresholds = explicitThresholds
	}
	input.PreviewHistogram = histogram
	return validateImageInputOptions(*input)
}
