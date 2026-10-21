package lrtl

import (
	"encoding/binary"
	"image"
)

// sharpenLuma10 reproduces the measured 7x7, amount=0.4 unsharp plane
// operation in LRT 7.5.5's bundled FFmpeg 8.0.1. Its high-depth path uses
// a 24-bit shift, unlike the 16-bit shift observed for 8-bit samples.
// Keep the horizontal convolution unrounded until the vertical pass.
func sharpenLuma10(input []uint16, width, height int) []uint16 {
	return sharpenLumaDepth(input, width, height, 10)
}

func sharpenLumaDepth(input []uint16, width, height, depth int) []uint16 {
	shift := 24
	if depth == 8 {
		shift = 16
	}
	maximum := int64(1<<depth) - 1
	kernel := [...]int32{1, 6, 15, 20, 15, 6, 1}
	horizontal := make([]int32, len(input))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			for k, weight := range kernel {
				xx := min(width-1, max(0, x+k-3))
				horizontal[y*width+x] += int32(input[y*width+xx]) * weight
			}
		}
	}
	output := make([]uint16, len(input))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			sum := int32(0)
			for k, weight := range kernel {
				yy := min(height-1, max(0, y+k-3))
				sum += horizontal[yy*width+x] * weight
			}
			i := y*width + x
			blur := (sum + 2048) >> 12
			value := int64(input[i]) + ((int64(input[i]) - int64(blur)) * 26214 >> shift)
			output[i] = uint16(min(maximum, max(int64(0), value)))
		}
	}
	return output
}

// sharpenOutputRGB48 evaluates a full-range BT.709 luma approximation at
// the native filter's 10-bit precision. Add its delta equally to RGB so that
// chroma is retained except where a channel clips. The surrounding RGB/YUV
// conversion is independent of LRT's color pipeline; plane parity does not
// imply complete native render parity. The RGB48 buffer is owned by the caller.
func sharpenOutputRGB48(rgb []byte, width, height int) {
	luma := make([]uint16, width*height)
	for i := range luma {
		p := rgb[i*6:]
		r := uint64(binary.LittleEndian.Uint16(p))
		g := uint64(binary.LittleEndian.Uint16(p[2:]))
		b := uint64(binary.LittleEndian.Uint16(p[4:]))
		y := (13933*r + 46871*g + 4732*b + 32768) >> 16
		luma[i] = uint16((y*1023 + 32767) / 65535)
	}
	filtered := sharpenLuma10(luma, width, height)
	for i, value := range filtered {
		delta := (int64(value) - int64(luma[i])) * 65535 / 1023
		for channel := 0; channel < 3; channel++ {
			p := rgb[i*6+channel*2:]
			v := int64(binary.LittleEndian.Uint16(p)) + delta
			binary.LittleEndian.PutUint16(p, uint16(min(int64(65535), max(int64(0), v))))
		}
	}
}

func sharpenOutputImage(img image.Image) (image.Image, error) {
	rgb, err := imageRGB48(img)
	if err != nil {
		return nil, err
	}
	width, height := img.Bounds().Dx(), img.Bounds().Dy()
	sharpenOutputRGB48(rgb, width, height)
	return imageFromRGB48(rgb, width, height)
}
