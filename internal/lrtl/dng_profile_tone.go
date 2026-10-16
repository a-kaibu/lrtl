package lrtl

import (
	"fmt"
	"math"
	"sort"
)

// The profile curve operates on linear ProPhoto RGB. Normalize the rounded
// primaries to our D50 reference so that a camera-neutral white stays neutral.
var dngProPhotoToXYZ = func() dngMatrix {
	m := dngMatrix{{.7977, .1352, .0313}, {.2880, .7119, .0001}, {0, 0, .8249}}
	for row := range m {
		sum := m[row][0] + m[row][1] + m[row][2]
		for col := range m[row] {
			m[row][col] *= dngWhiteD50[row] / sum
		}
	}
	return m
}()

type dngProfileToneCurve struct {
	x, y, second []float64
}

func readDNGProfileToneCurve(fields *dngFields) (*dngProfileToneCurve, error) {
	entry, found := fields.entries[50940]
	if !found {
		return nil, nil
	}
	if entry.typeID != 11 || entry.count < 4 || entry.count%2 != 0 || entry.count > 8192 {
		return nil, fmt.Errorf("DNG ProfileToneCurve requires 2..4096 FLOAT point pairs")
	}
	values := fields.values(50940)
	if fields.err != nil {
		return nil, fields.err
	}
	return newDNGProfileToneCurve(values)
}

// Solve a natural cubic spline using its tridiagonal second-derivative system.
// Endpoint second derivatives are zero. This is independent of the SDK's
// slope-based solver and does not use the XMP renderer's PCHIP approximation.
func newDNGProfileToneCurve(values []float64) (*dngProfileToneCurve, error) {
	if len(values) < 4 || len(values)%2 != 0 || len(values) > 8192 {
		return nil, fmt.Errorf("invalid DNG profile curve point count")
	}
	n := len(values) / 2
	c := &dngProfileToneCurve{x: make([]float64, n), y: make([]float64, n), second: make([]float64, n)}
	for i := 0; i < n; i++ {
		x, y := values[2*i], values[2*i+1]
		if math.IsNaN(x) || math.IsNaN(y) || x < 0 || x > 1 || y < 0 || y > 1 || (i > 0 && x <= c.x[i-1]) {
			return nil, fmt.Errorf("invalid DNG profile curve point %d", i)
		}
		c.x[i], c.y[i] = x, y
	}
	upper, rhs := make([]float64, n), make([]float64, n)
	for i := 1; i < n-1; i++ {
		left, right := c.x[i]-c.x[i-1], c.x[i+1]-c.x[i]
		diagonal := 2*(left+right) - left*upper[i-1]
		upper[i] = right / diagonal
		rhs[i] = (6*((c.y[i+1]-c.y[i])/right-(c.y[i]-c.y[i-1])/left) - left*rhs[i-1]) / diagonal
		if math.IsNaN(rhs[i]) || math.IsInf(rhs[i], 0) {
			return nil, fmt.Errorf("DNG profile curve spacing exceeds numerical limits")
		}
	}
	for i := n - 2; i > 0; i-- {
		c.second[i] = rhs[i] - upper[i]*c.second[i+1]
	}
	return c, nil
}

func (curve *dngProfileToneCurve) evaluate(value float64) float64 {
	n := len(curve.x)
	if value <= curve.x[0] {
		return curve.y[0]
	}
	if value >= curve.x[n-1] {
		return curve.y[n-1]
	}
	j := sort.Search(n, func(i int) bool { return curve.x[i] >= value })
	h := curve.x[j] - curve.x[j-1]
	b := (value - curve.x[j-1]) / h
	a := 1 - b
	y := a*curve.y[j-1] + b*curve.y[j] + ((a*a*a-a)*curve.second[j-1]+(b*b*b-b)*curve.second[j])*h*h/6
	return min(1, max(0, y))
}

func (curve *dngProfileToneCurve) apply(rgb [3]float64) [3]float64 {
	for i := range rgb {
		rgb[i] = min(1, max(0, rgb[i]))
	}
	lo, hi := min(rgb[0], rgb[1], rgb[2]), max(rgb[0], rgb[1], rgb[2])
	low, high := curve.evaluate(lo), curve.evaluate(hi)
	if lo == hi {
		return [3]float64{low, low, low}
	}
	for i := range rgb {
		rgb[i] = low + (high-low)*((rgb[i]-lo)/(hi-lo))
	}
	return rgb
}
