package lrtl

import (
	"fmt"
	"math"
)

type previewHistogramOptions struct {
	Enabled                    bool
	IgnoreBlacks, IgnoreWhites int
	HardCut                    bool
}

func (options previewHistogramOptions) validate() error {
	if options.IgnoreBlacks < 0 || options.IgnoreBlacks > 100 || options.IgnoreWhites < 0 || options.IgnoreWhites > 100 {
		return fmt.Errorf("--ignore-blacks and --ignore-whites must be integers from 0 to 100")
	}
	return nil
}

// These are the two observed post-smoothing native histogram measurements.
// Hard cut averages per-band intensities using the native bin width. Soft cut
// pools the band counts and weights raw bin indices with normalized edge ramps.
func filteredPreviewEV(bins [3][256]uint64, options previewHistogramOptions) float64 {
	if options.HardCut {
		first := int(math.Floor(256*float64(options.IgnoreBlacks)/100 + .5))
		last := 256 - int(math.Floor(256*float64(options.IgnoreWhites)/100+.5))
		mean := 0.0
		for _, band := range bins {
			var total uint64
			for i := first; i < last; i++ {
				total += band[i]
			}
			if total == 0 {
				continue
			}
			value := float64(first) * 255 / 256
			for i := first; i < last; i++ {
				mean += float64(band[i]) / float64(total) * value
				value += 255.0 / 256
			}
		}
		return mean / 3 / 256
	}
	first, last := 256*options.IgnoreBlacks/100, 256-256*options.IgnoreWhites/100
	var weights [256]float64
	for i := 0; i < first; i++ {
		weights[i] = float64(i) / float64(first)
	}
	for i := first; i < last; i++ {
		weights[i] = 1
	}
	for i := last; i < 256; i++ {
		weights[i] = float64(256-i) / float64(256-last)
	}
	totalWeight := 0.0
	for _, w := range weights {
		totalWeight += w
	}
	for i := range weights {
		weights[i] /= totalWeight
	}
	weighted, count := 0.0, 0.0
	for i, w := range weights {
		population := (float64(bins[0][i]) + float64(bins[1][i]) + float64(bins[2][i])) / 3
		weighted += population * float64(i) * w
		count += population * w
	}
	if count <= 0 {
		return 0
	}
	return weighted / count / 256
}
