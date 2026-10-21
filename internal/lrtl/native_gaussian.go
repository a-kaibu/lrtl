package lrtl

import (
	"math"

	"golang.org/x/sys/cpu"
)

// The measured native AVX2 Gaussian kernels fuse each feedback multiply/add.
// Go evaluates that numerical convention without calling native filter code.
// Other CPU configurations retain the existing scalar convention.
func nativeGaussianFused() bool {
	return cpu.X86.HasAVX2 && cpu.X86.HasFMA
}

// A float64 FMA followed by float32 conversion can double-round when the
// double result is exactly a float32 midpoint. The float32 product is exact
// in float64, so TwoSum recovers the residual only for those rare candidates.
func gaussianFeedbackFMA32(feedback, previous, current float32) float32 {
	sum := math.FMA(float64(feedback), float64(previous), float64(current))
	rounded := float32(sum)
	encoded := math.Float64bits(sum)
	exponent := int(encoded>>52&0x7ff) - 1023
	if exponent > 127 {
		return rounded
	}
	discard := 29
	if exponent < -126 {
		discard = -97 - exponent
	}
	if discard > 53 {
		return rounded
	}
	mantissa := encoded&((1<<52)-1) | (1 << 52)
	if mantissa&((uint64(1)<<discard)-1) != uint64(1)<<(discard-1) {
		return rounded
	}
	product := float64(float64(feedback) * float64(previous))
	recovered := float64(sum - product)
	residual := float64(float64(product-float64(sum-recovered)) + float64(float64(current)-recovered))
	if residual == 0 {
		return rounded
	}
	lower, upper := rounded, rounded
	if float64(rounded) < sum {
		upper = math.Nextafter32(rounded, float32(math.Inf(1)))
	} else {
		lower = math.Nextafter32(rounded, float32(math.Inf(-1)))
	}
	if residual > 0 {
		return upper
	}
	return lower
}
