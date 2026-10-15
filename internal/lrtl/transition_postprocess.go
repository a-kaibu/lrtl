package lrtl

import (
	"math"
	"math/big"
	"sort"
)

type transitionAnchor struct {
	index int
	value float64
}

// Java constructs BigDecimal from the exact binary double, then uses HALF_EVEN.
// Multiplying a float by 10000 before rounding loses the relevant tie bits.
func roundLRTFourDecimals(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return value
	}
	r := new(big.Rat).SetFloat64(value)
	n := new(big.Int).Mul(r.Num(), big.NewInt(10000))
	q, rem := new(big.Int), new(big.Int)
	q.QuoRem(n, r.Denom(), rem)
	twice := new(big.Int).Lsh(new(big.Int).Abs(rem), 1)
	cmp := twice.Cmp(r.Denom())
	if cmp > 0 || cmp == 0 && q.Bit(0) == 1 {
		if n.Sign() < 0 {
			q.Sub(q, big.NewInt(1))
		} else {
			q.Add(q, big.NewInt(1))
		}
	}
	result, _ := new(big.Rat).SetFrac(q, big.NewInt(10000)).Float64()
	return result
}

// The observed numeric worker first clamps/rounds spline samples, then makes
// close anchor intervals linear and equal-valued intervals constant. Preserve
// original keyframe doubles for the four-decimal comparison.
func postprocessLRTTransition(values []float64, filled, keyframes []bool, anchors []transitionAnchor, spec numericTransitionSpec) {
	if len(values) == 0 || len(anchors) == 0 {
		return
	}
	for i := range values {
		if filled[i] && !keyframes[i] {
			values[i] = adjustTransitionValue(values[i], spec)
		}
	}
	boundaries := append([]transitionAnchor(nil), anchors...)
	sort.Slice(boundaries, func(i, j int) bool { return boundaries[i].index < boundaries[j].index })
	if boundaries[0].index > 0 {
		boundaries = append([]transitionAnchor{{0, values[0]}}, boundaries...)
	}
	last := len(values) - 1
	if boundaries[len(boundaries)-1].index < last {
		boundaries = append(boundaries, transitionAnchor{last, values[last]})
	}
	threshold := max(2, len(values)*4/100)
	for i := 0; i+1 < len(boundaries); i++ {
		left, right := boundaries[i], boundaries[i+1]
		gap := right.index - left.index
		if gap <= 0 {
			continue
		}
		equal := left.value == right.value
		if !spec.integer {
			equal = roundLRTFourDecimals(left.value) == roundLRTFourDecimals(right.value)
		}
		for frame := left.index + 1; frame < right.index; frame++ {
			if keyframes[frame] {
				continue
			}
			if equal {
				values[frame] = adjustTransitionValue(left.value, spec)
				filled[frame] = true
			} else if gap < threshold {
				value := left.value + (right.value-left.value)*float64(frame-left.index)/float64(gap)
				values[frame] = adjustTransitionValue(value, spec)
				filled[frame] = true
			}
		}
	}
}
