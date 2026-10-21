package lrtl

import (
	"math"
	"math/big"
)

// Resolve the almost-equal rectangle errors used to choose a radial rotation.
// Ordinary binary64 sine can differ by one bit from the native Java intrinsic;
// that bit can change the chosen quadrant. Arguments here have already been
// reduced to [-pi/2, pi], so a high-precision Taylor sum is small and bounded.
// This is used for anchor selection only, not for every rendered image pixel.
func radialExactSinCos(x float64) (float64, float64) {
	if math.Abs(x) < 0x1p-27 {
		return x, 1
	}
	const precision = 256
	makeFloat := func() *big.Float { return new(big.Float).SetPrec(precision) }
	xf := makeFloat().SetFloat64(x)
	negativeSquare := makeFloat().Mul(xf, xf)
	negativeSquare.Neg(negativeSquare)
	sum := func(cosine bool) float64 {
		term, total := makeFloat(), makeFloat()
		if cosine {
			term.SetInt64(1)
		} else {
			term.Set(xf)
		}
		total.Set(term)
		denominator := makeFloat()
		for k := int64(1); k <= 100; k++ {
			a, b := 2*k, 2*k+1
			if cosine {
				a, b = 2*k-1, 2*k
			}
			denominator.SetInt64(a * b)
			term.Mul(term, negativeSquare)
			term.Quo(term, denominator)
			total.Add(total, term)
			if term.MantExp(nil) < -260 {
				break
			}
		}
		value, _ := total.Float64()
		return value
	}
	return sum(false), sum(true)
}
