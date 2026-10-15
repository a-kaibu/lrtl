package lrtl

import (
	"math"
	"sort"
)

var srgb16ToLinear [65536]float64

func init() {
	for i := range srgb16ToLinear {
		srgb16ToLinear[i] = decodeSRGBFloat(float64(i) / 65535)
	}
}

func decodeSRGBFloat(value float64) float64 {
	if value <= .04045 {
		return value / 12.92
	}
	return math.Pow((value+.055)/1.055, 2.4)
}

func encodeLinearSRGBFloat(value float64) float64 {
	value = math.Max(0, math.Min(1, value))
	if value <= .0031308 {
		return value * 12.92
	}
	return 1.055*math.Pow(value, 1/2.4) - .055
}

// PCHIP coefficients use the original XMP 0..255 coordinate system. Keeping
// segments avoids an 8-bit intermediate and a 128 KiB LUT per curve per frame.
type toneCurveSegment struct{ start, width, a, b, c, d float64 }
type toneCurveTable struct {
	first, last toneCurvePoint
	segments    []toneCurveSegment
}

func (curve toneCurveTable) sample(input uint16) uint16 {
	if len(curve.segments) == 0 {
		return input
	}
	x := float64(input) / 257
	value := curve.first.y
	if x >= curve.last.x {
		value = curve.last.y
	} else if x > curve.first.x {
		i := sort.Search(len(curve.segments), func(i int) bool { return x <= curve.segments[i].start+curve.segments[i].width })
		segment := curve.segments[min(i, len(curve.segments)-1)]
		t := (x - segment.start) / segment.width
		value = ((segment.a*t+segment.b)*t+segment.c)*t + segment.d
	}
	return uint16(math.Round(math.Max(0, math.Min(255, value)) * 257))
}
