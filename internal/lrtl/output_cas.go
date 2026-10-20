package lrtl

import (
	"encoding/binary"
	"image"
	"math"
)

// casSample10 evaluates an independent scalar CAS kernel at one location.
// The cross and square extrema control the negative cross-neighbor weight.
// Replicated edges, float32 arithmetic and truncation follow measured 10-bit
// planes from the native bundled filter. This is not the native color pipeline.
func casSample10(plane []uint16, width, height, x, y int, peak float32) uint16 {
	left, right := max(0, x-1), min(width-1, x+1)
	top, bottom := max(0, y-1), min(height-1, y+1)
	center := int(plane[y*width+x])
	north, south := int(plane[top*width+x]), int(plane[bottom*width+x])
	west, east := int(plane[y*width+left]), int(plane[y*width+right])
	lo, hi := min(center, north, south, west, east), max(center, north, south, west, east)
	squareLo, squareHi := lo, hi
	for _, row := range [2]int{top, bottom} {
		for _, col := range [2]int{left, right} {
			v := int(plane[row*width+col])
			squareLo, squareHi = min(squareLo, v), max(squareHi, v)
		}
	}
	lower, upper := lo+squareLo, hi+squareHi
	if upper == 0 {
		return 0
	}
	contrast := float32(min(lower, 2047-upper)) / float32(upper)
	contrast = min(float32(1), max(float32(0), contrast))
	weight := float32(math.Sqrt(float64(contrast))) / peak
	neighbors := float32(float32(north+south+west+east) * weight)
	numerator := float32(neighbors + float32(center))
	denominator := float32(1 + float32(4*weight))
	return uint16(min(1023, max(0, int(numerator/denominator))))
}

func casPeak(strength float32) float32 {
	slope := float32(4.01) - float32(16)
	change := float32(slope * strength)
	return -float32(16 + change)
}

func casPlane10(input []uint16, width, height int, strength float32) []uint16 {
	output := make([]uint16, len(input))
	peak := casPeak(strength)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			output[y*width+x] = casSample10(input, width, height, x, y, peak)
		}
	}
	return output
}

// casOutputRGB48 sharpens all three approximate full-range BT.709 Y/Cb/Cr
// planes at the native filter's 10-bit precision. Apply only their deltas to
// the original RGB16 samples, retaining detail below a 10-bit code step.
// RGB/YUV conversion is independent of LRT's color management.
func casOutputRGB48(rgb []byte, width, height int) {
	planes := outputYUV10(rgb, width, height)
	peak := casPeak(0.9)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			i := y*width + x
			var delta [3]float64
			for p := range planes {
				filtered := casSample10(planes[p], width, height, x, y, peak)
				delta[p] = float64(int(filtered)-int(planes[p][i])) * (65535.0 / 1023)
			}
			addOutputYUVDelta(rgb[i*6:], delta)
		}
	}
}

func outputYUV10(rgb []byte, width, height int) [3][]uint16 {
	var planes [3][]uint16
	for i := range planes {
		planes[i] = make([]uint16, width*height)
	}
	quantize := func(value float64) uint16 {
		return uint16(math.Round(min(1, max(0, value)) * 1023))
	}
	for i := range planes[0] {
		p := rgb[i*6:]
		r := float64(binary.LittleEndian.Uint16(p)) / 65535
		g := float64(binary.LittleEndian.Uint16(p[2:])) / 65535
		b := float64(binary.LittleEndian.Uint16(p[4:])) / 65535
		luma := 0.2126*r + 0.7152*g + 0.0722*b
		planes[0][i] = quantize(luma)
		planes[1][i] = quantize(0.5 + (b-luma)/1.8556)
		planes[2][i] = quantize(0.5 + (r-luma)/1.5748)
	}
	return planes
}

func addOutputYUVDelta(rgb []byte, delta [3]float64) {
	changes := [3]float64{
		delta[0] + 1.5748*delta[2],
		delta[0] - (0.0722*1.8556/0.7152)*delta[1] - (0.2126*1.5748/0.7152)*delta[2],
		delta[0] + 1.8556*delta[1],
	}
	for channel, change := range changes {
		p := rgb[channel*2:]
		value := int(binary.LittleEndian.Uint16(p)) + int(math.Round(change))
		binary.LittleEndian.PutUint16(p, uint16(min(65535, max(0, value))))
	}
}

func filterOutputImage(img image.Image, options RenderOptions) (image.Image, error) {
	rgb, err := imageRGB48(img)
	if err != nil {
		return nil, err
	}
	width, height := img.Bounds().Dx(), img.Bounds().Dy()
	filterOutputRGB48(rgb, width, height, options)
	return imageFromRGB48(rgb, width, height)
}

func filterOutputRGB48(rgb []byte, width, height int, options RenderOptions) {
	if options.OutputSharpen {
		sharpenOutputRGB48(rgb, width, height)
	}
	if options.OutputCAS {
		casOutputRGB48(rgb, width, height)
	}
}
