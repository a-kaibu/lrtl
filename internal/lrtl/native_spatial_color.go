package lrtl

import (
	"encoding/binary"
	"fmt"
)

// nativeSDRPlanes converts an unscaled RGB48 frame to the full-range 10-bit
// working planes observed in the installed SDR encoder. The initial limited
// range fixed-point matrix is followed by expansion at 15-bit precision and
// half-up rounding to 10 bits. Metadata is assigned after this conversion.
func nativeSDRPlanes(rgb []byte, width, height int) ([3][]uint16, error) {
	var planes [3][]uint16
	if width <= 0 || height <= 0 || len(rgb)%6 != 0 || len(rgb)/6/width != height || len(rgb)/6%width != 0 {
		return planes, fmt.Errorf("invalid native SDR RGB48 dimensions")
	}
	// BT.601 limited-range coefficients quantized with a 15-bit scale.
	matrix := [3][3]int64{{8414, 16519, 3208}, {-4865, -9528, 14392}, {14392, -12061, -2332}}
	for p := range planes {
		planes[p] = make([]uint16, len(rgb)/6)
		upper := int64(940)
		bias := int64(0x2001) << 14
		if p > 0 {
			upper = 960
			bias = int64(0x10001) << 14
		}
		// Expand [64,940] luma or [64,960] chroma to [0,1023].
		coefficient := (1023*16384 + upper - 64 - 1) / (upper - 64)
		offset := (int64(1023) << 19) - (upper<<5)*coefficient + 8192
		for i := range planes[p] {
			pixel := rgb[i*6:]
			value := bias
			for c, weight := range matrix[p] {
				value += weight * int64(binary.LittleEndian.Uint16(pixel[c*2:]))
			}
			value = (value >> 15) >> 1
			value = min(32767, (value*coefficient+offset)>>14)
			planes[p][i] = uint16(min(1023, max(0, (value+16)>>5)))
		}
	}
	return planes, nil
}

type nativeSpatialFilter struct {
	width, height int
	sharpen, cas  bool
}

func (f *nativeSpatialFilter) frame(rgb []byte) ([]byte, error) {
	planes, err := nativeSDRPlanes(rgb, f.width, f.height)
	if err != nil {
		return nil, err
	}
	return f.filterPlanes(planes, len(rgb)), nil
}

func (f *nativeSpatialFilter) workingFrame(frame []byte) ([]byte, error) {
	return f.workingFrameDepth(frame, 10)
}

func (f *nativeSpatialFilter) workingFrameDepth(frame []byte, depth int) ([]byte, error) {
	if depth != 8 && depth != 10 {
		return nil, fmt.Errorf("native working depth must be 8 or 10")
	}
	if f.width <= 0 || f.height <= 0 || len(frame)%6 != 0 || len(frame)/6/f.width != f.height || len(frame)/6%f.width != 0 {
		return nil, fmt.Errorf("invalid native working frame dimensions")
	}
	planes := outputFrameColor(true).planes(frame, f.width, f.height)
	return f.filterPlanesDepth(planes, len(frame), depth), nil
}

func (f *nativeSpatialFilter) filterPlanes(planes [3][]uint16, size int) []byte {
	return f.filterPlanesDepth(planes, size, 10)
}

func (f *nativeSpatialFilter) filterPlanesDepth(planes [3][]uint16, size, depth int) []byte {
	if f.sharpen {
		maximum := int64(255)
		if depth == 10 {
			maximum = 65535 // native high-depth unsharp clips to storage width
		}
		planes[0] = sharpenLumaLimit(planes[0], f.width, f.height, depth, maximum)
	}
	if f.cas {
		for p := range planes {
			planes[p] = casPlaneDepth(planes[p], f.width, f.height, .9, depth)
		}
	}
	// FFmpeg receives the already converted and filtered planar samples. No
	// RGB round trip or second matrix conversion can alter chroma or clipping.
	if depth == 8 {
		size /= 2
	}
	out := make([]byte, size)
	for p := range planes {
		for i, v := range planes[p] {
			if depth == 8 {
				out[p*len(planes[p])+i] = byte(v)
			} else {
				binary.LittleEndian.PutUint16(out[(p*len(planes[p])+i)*2:], v)
			}
		}
	}
	return out
}
