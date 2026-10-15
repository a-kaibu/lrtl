package lrtl

import (
	"image"
	"image/color"
	"math"
)

// xmpLuminanceDetail extracts a signed high-pass luminance layer with a
// separable box blur. It is used as a bounded sRGB-domain approximation for
// Camera Raw Texture and Clarity controls.
func xmpLuminanceDetail(img image.Image, radius int) []int8 {
	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width <= 0 || height <= 0 {
		return nil
	}
	radius = max(1, radius)
	window := radius*2 + 1
	horizontal := make([]uint8, width*height)
	blurred := make([]uint8, width*height)

	for y := 0; y < height; y++ {
		sum := 0
		for offset := -radius; offset <= radius; offset++ {
			x := min(width-1, max(0, offset))
			sum += int(xmpLuminanceByteAt(img, bounds.Min.X+x, bounds.Min.Y+y))
		}
		for x := 0; x < width; x++ {
			horizontal[y*width+x] = uint8((sum + window/2) / window)
			leaving := min(width-1, max(0, x-radius))
			entering := min(width-1, max(0, x+radius+1))
			sum -= int(xmpLuminanceByteAt(img, bounds.Min.X+leaving, bounds.Min.Y+y))
			sum += int(xmpLuminanceByteAt(img, bounds.Min.X+entering, bounds.Min.Y+y))
		}
	}
	for x := 0; x < width; x++ {
		sum := 0
		for offset := -radius; offset <= radius; offset++ {
			y := min(height-1, max(0, offset))
			sum += int(horizontal[y*width+x])
		}
		for y := 0; y < height; y++ {
			blurred[y*width+x] = uint8((sum + window/2) / window)
			leaving := min(height-1, max(0, y-radius))
			entering := min(height-1, max(0, y+radius+1))
			sum -= int(horizontal[leaving*width+x])
			sum += int(horizontal[entering*width+x])
		}
	}

	detail := make([]int8, width*height)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			index := y*width + x
			original := srgb8ToLinear[xmpLuminanceByteAt(img, bounds.Min.X+x, bounds.Min.Y+y)]
			lowFrequency := srgb8ToLinear[blurred[index]]
			value := math.Round((original - lowFrequency) * 127)
			value = math.Max(-127, math.Min(127, value))
			detail[index] = int8(value)
		}
	}
	return detail
}

func xmpLuminanceByteAt(img image.Image, x, y int) uint8 {
	pixel := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
	luminance := 0.2126*srgb8ToLinear[pixel.R] + 0.7152*srgb8ToLinear[pixel.G] + 0.0722*srgb8ToLinear[pixel.B]
	return encodeLinearSRGB(luminance)
}

func xmpTextureRadius(width, height int) int {
	return max(1, min(3, min(width, height)/600))
}

func xmpClarityRadius(width, height int) int {
	return max(3, min(24, min(width, height)/120))
}

func xmpApplyDetail(luminance, texture, clarity float64, textureDetail, clarityDetail int8) float64 {
	delta := float64(textureDetail) / 127 * texture / 100 * 0.75
	if clarity != 0 {
		midtoneWeight := 4 * luminance * (1 - luminance)
		delta += float64(clarityDetail) / 127 * clarity / 100 * 1.25 * midtoneWeight
	}
	return math.Max(0, math.Min(1, luminance+delta))
}
