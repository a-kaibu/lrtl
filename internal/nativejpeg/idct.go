// This separable cosine-matrix IDCT is independent of the upstream decoder.
package nativejpeg

import "math"

const blockSize = 64

type block [blockSize]int32

var idctBasis = func() (m [8][8]int32) {
	for x := 0; x < 8; x++ {
		for k := 0; k < 8; k++ {
			if k == 0 {
				m[x][k] = 16383
				continue
			}
			m[x][k] = int32(math.Round(math.Sqrt2 * 16384 * math.Cos(float64((2*x+1)*k)*math.Pi/16)))
			if k == 4 {
				if m[x][k] > 0 {
					m[x][k] = 16383
				} else {
					m[x][k] = -16383
				}
			}
		}
	}
	return
}()

func idct(src *block) {
	var rows block
	// Fold JPEG's level shift into the DC coefficient before row rounding.
	src[0] += 1024
	dcOnly := true
	for _, value := range src[1:] {
		if value != 0 {
			dcOnly = false
			break
		}
	}
	if dcOnly {
		row := int32(int16(src[0] * 8))
		value := (int32(int64(row)*16383+16383*32) >> 20) - 128
		for i := range src {
			src[i] = value
		}
		return
	}
	for row := 0; row < 8; row++ {
		ac := int32(0)
		for k := 1; k < 8; k++ {
			ac |= src[row*8+k]
		}
		for x := 0; x < 8; x++ {
			if ac == 0 {
				rows[row*8+x] = int32(int16(src[row*8] * 8))
				continue
			}
			sum := int64(1024)
			for k := 0; k < 8; k++ {
				sum += int64(idctBasis[x][k]) * int64(int16(src[row*8+k]))
			}
			rows[row*8+x] = int32(int16(int32(sum) >> 11))
		}
	}
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			sum := int64(16383 * 32)
			for k := 0; k < 8; k++ {
				sum += int64(idctBasis[y][k]) * int64(rows[k*8+x])
			}
			src[y*8+x] = (int32(sum) >> 20) - 128
		}
	}
}
