package lrtl

import (
	"fmt"
	"math"
)

type nativeDeflickerPass struct {
	Target, Correction []float64
}

// calculateNativeDeflickerPass models one observed dense, additive native pass.
// Inputs are measured Preview EV and current native Deflicker slot values.
// It does not render images or mutate XMP; callers must provide fresh inputs.
func calculateNativeDeflickerPass(measured, current []float64, smoothing float64, constant bool, accuracy, pass int) (*nativeDeflickerPass, error) {
	if len(measured) < 2 || len(current) != len(measured) {
		return nil, fmt.Errorf("native Deflicker requires at least two measurements and matching current corrections")
	}
	if math.IsNaN(smoothing) || math.IsInf(smoothing, 0) || smoothing < 0 || smoothing > 2 {
		return nil, fmt.Errorf("native smoothing must be finite and between 0 and 2")
	}
	if accuracy < -1 || accuracy > 1 || pass < 0 {
		return nil, fmt.Errorf("native accuracy must be -1, 0 or 1 and pass index must be nonnegative")
	}
	for i, value := range measured {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 1 || math.IsNaN(current[i]) || math.IsInf(current[i], 0) {
			return nil, fmt.Errorf("invalid native Deflicker sample at frame %d", i+1)
		}
	}
	target := append([]float64(nil), measured...)
	if constant {
		mean := 0.0
		for _, value := range measured {
			mean += value
		}
		mean /= float64(len(measured))
		for i := range target {
			target[i] = mean
		}
	} else if len(measured) < 8 {
		step := (measured[len(measured)-1] - measured[0]) / float64(len(measured)-1)
		for i := range target {
			target[i] = measured[0] + step*float64(i)
		}
	} else {
		target = lrtHolyGrailLowess(measured, smoothing)
	}
	result := &nativeDeflickerPass{Target: target, Correction: append([]float64(nil), current...)}
	nonzero := false
	for i, value := range measured {
		nonzero = nonzero || value-target[i] != 0
	}
	if !nonzero {
		// The native worker clears all corrections when every residual is zero.
		clear(result.Correction)
		return result, nil
	}
	threshold := float64(float32(.002))
	if accuracy < 0 {
		threshold = float64(float32(.005))
	}
	if accuracy > 0 {
		threshold = float64(float32(.0005))
	}
	for i, value := range measured {
		delta := nativeDeflickerTransfer(value - target[i] + .5)
		if accuracy > 0 {
			delta *= .5 + .5*math.Pow(float64(float32(.6)), float64(pass))
		}
		if math.Abs(delta) > threshold {
			result.Correction[i] = max(-1, min(1, current[i]+delta))
		}
	}
	return result, nil
}

func nativeDeflickerTransfer(value float64) float64 {
	// Preserve the observed endpoint branches, including their sign jump.
	if value >= 1 {
		return 5
	}
	if value <= 0 {
		return -5
	}
	return max(-5, min(5, (math.Log10(value)-math.Log10(1-value))*-.8))
}
