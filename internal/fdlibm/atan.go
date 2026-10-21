// Copyright (C) 1993 by Sun Microsystems, Inc. All rights reserved.
// Developed at SunSoft, a Sun Microsystems, Inc. business.
// Permission to use, copy, modify, and distribute this software is freely
// granted, provided that this notice is preserved.
//
// Go adaptation of netlib fdlibm s_atan.c and e_atan2.c. See LICENSE.

// Package fdlibm provides the arctangent arithmetic needed by the native
// radial-mask transition. A one-bit difference can select another rotation.
package fdlibm

import "math"

func atan(x float64) float64 {
	hi := [...]float64{4.63647609000806093515e-01, 7.85398163397448278999e-01, 9.82793723247329054082e-01, 1.57079632679489655800e+00}
	lo := [...]float64{2.26987774529616870924e-17, 3.06161699786838301793e-17, 1.39033110312309984516e-17, 6.12323399573676603587e-17}
	a := [...]float64{3.33333333333329318027e-01, -1.99999999998764832476e-01, 1.42857142725034663711e-01, -1.11111104054623557880e-01, 9.09088713343650656196e-02, -7.69187620504482999495e-02, 6.66107313738753120669e-02, -5.83357013379057348645e-02, 4.97687799461593236017e-02, -3.65315727442169155270e-02, 1.62858201153657823623e-02}
	negative := math.Signbit(x)
	ix := uint32(math.Float64bits(x)>>32) & 0x7fffffff
	if ix >= 0x44100000 {
		if math.IsNaN(x) {
			return x + x
		}
		return math.Copysign(hi[3]+lo[3], x)
	}
	id := -1
	if ix < 0x3fdc0000 {
		if ix < 0x3e200000 {
			return x
		}
	} else {
		x = math.Abs(x)
		switch {
		case ix < 0x3fe60000:
			id = 0
			x = (2*x - 1) / (2 + x)
		case ix < 0x3ff30000:
			id = 1
			x = (x - 1) / (x + 1)
		case ix < 0x40038000:
			id = 2
			x = (x - 1.5) / (1 + 1.5*x)
		default:
			id = 3
			x = -1 / x
		}
	}
	z := x * x
	w := z * z
	s1 := z * (a[0] + w*(a[2]+w*(a[4]+w*(a[6]+w*(a[8]+w*a[10])))))
	s2 := w * (a[1] + w*(a[3]+w*(a[5]+w*(a[7]+w*a[9]))))
	if id < 0 {
		return x - x*(s1+s2)
	}
	z = hi[id] - ((x*(s1+s2) - lo[id]) - x)
	if negative {
		return -z
	}
	return z
}

// Atan2 returns the fdlibm arctangent, retaining signed-zero and quadrant
// conventions. It is intentionally separate from Go's Cephes-based atan.
func Atan2(y, x float64) float64 {
	const pi float64 = 0x1.921fb54442d18p1
	const halfPi float64 = 0x1.921fb54442d18p0
	const quarterPi float64 = 0x1.921fb54442d18p-1
	const piLow float64 = 1.2246467991473531772e-16
	if math.IsNaN(x) || math.IsNaN(y) {
		return x + y
	}
	if x == 1 {
		return atan(y)
	}
	negativeX, negativeY := math.Signbit(x), math.Signbit(y)
	if y == 0 {
		if negativeX {
			return math.Copysign(pi, y)
		}
		return y
	}
	if x == 0 {
		return math.Copysign(halfPi, y)
	}
	if math.IsInf(x, 0) {
		if math.IsInf(y, 0) {
			if negativeX {
				return math.Copysign(3*quarterPi, y)
			}
			return math.Copysign(quarterPi, y)
		}
		if negativeX {
			return math.Copysign(pi, y)
		}
		return math.Copysign(0, y)
	}
	if math.IsInf(y, 0) {
		return math.Copysign(halfPi, y)
	}
	ix := int64(uint32(math.Float64bits(x)>>32) & 0x7fffffff)
	iy := int64(uint32(math.Float64bits(y)>>32) & 0x7fffffff)
	k := (iy - ix) >> 20
	var z float64
	if k > 60 {
		z = halfPi + .5*piLow
	} else if negativeX && k < -60 {
		z = 0
	} else {
		z = atan(math.Abs(y / x))
	}
	if !negativeX {
		if negativeY {
			return -z
		}
		return z
	}
	if negativeY {
		return (z - piLow) - pi
	}
	return pi - (z - piLow)
}
