package lrtl

import (
	"fmt"
	"math"
)

// applyDNGWarpRaw applies a geometric opcode to the interleaved 16-bit Stage 1
// samples. The Adobe SDK runs the warp in normalized float32 space, so this
// converts each source sample on demand and rounds back to the integer Stage 1
// sample type after each opcode without allocating another full image buffer.
func applyDNGWarpRaw(source, destination []uint16, width, height, channels int, verticalScale float64, warp dngWarp) error {
	if width < 1 || height < 1 || channels < 1 || uint64(width)*uint64(height)*uint64(channels) != uint64(len(source)) || len(destination) != len(source) {
		return fmt.Errorf("invalid DNG Stage 1 warp buffer dimensions")
	}
	if verticalScale <= 0 || math.IsNaN(verticalScale) || math.IsInf(verticalScale, 0) {
		return fmt.Errorf("invalid DNG Stage 1 warp pixel aspect")
	}
	if warp.identity() {
		copy(destination, source)
		return nil
	}
	geometry := warp.geometry(width, height, verticalScale)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			for plane := 0; plane < channels; plane++ {
				sx, sy := geometry.source(x, y, plane)
				if math.IsNaN(sx) || math.IsNaN(sy) || math.IsInf(sx, 0) || math.IsInf(sy, 0) {
					return fmt.Errorf("non-finite DNG Stage 1 warp coordinate")
				}
				value := sampleDNGWarpRawPlane(source, width, height, channels, plane, sx, sy)
				if math.IsNaN(value) || math.IsInf(value, 0) {
					return fmt.Errorf("non-finite DNG Stage 1 warp sample")
				}
				destination[(y*width+x)*channels+plane] = uint16(math.Round(value))
			}
		}
	}
	return nil
}

func sampleDNGWarpRawPlane(data []uint16, width, height, channels, plane int, x, y float64) float64 {
	x = min(float64(width-1), max(0, x))
	y = min(float64(height-1), max(0, y))
	left, top := int(math.Floor(x)), int(math.Floor(y))
	value := 0.0
	for row := top - 1; row <= top+2; row++ {
		wy := dngWarpCubicWeight(y - float64(row))
		sy := min(height-1, max(0, row))
		for col := left - 1; col <= left+2; col++ {
			wx := dngWarpCubicWeight(x - float64(col))
			sx := min(width-1, max(0, col))
			sample := float32(data[(sy*width+sx)*channels+plane]) / 65535
			value += float64(sample) * wx * wy
		}
	}
	return float64(float32(min(1, max(0, value)))) * 65535
}
