package lrtl

import (
	"image"
	"image/color"
	"math"

	xdraw "golang.org/x/image/draw"
)

const lrtLongTermSampleSize = 64
const lrtLongTermBlurScale = 4

// measureLRTLongTermFrame follows the 7.5.5 frame-analysis path observed in
// the installed application: bilinear 64x64 resize, Gaussian blur, then mean
// grayscale luminance, grayscale standard deviation, and arithmetic HSB hue.
// It processes the full image; an XMP crop can be applied by the caller first.
func measureLRTLongTermFrame(img image.Image) lrtLongTermMetrics {
	sampled := image.NewNRGBA(image.Rect(0, 0, lrtLongTermSampleSize, lrtLongTermSampleSize))
	xdraw.BiLinear.Scale(sampled, sampled.Bounds(), img, img.Bounds(), xdraw.Src, nil)
	blurred := blurLRTLongTermFrame(sampled, lrtGaussianKernel(lrtLongTermBlurScale))

	count := float64(lrtLongTermSampleSize * lrtLongTermSampleSize)
	luminanceSum, luminanceSquaredSum, hueSum := 0.0, 0.0, 0.0
	for y := 0; y < lrtLongTermSampleSize; y++ {
		for x := 0; x < lrtLongTermSampleSize; x++ {
			pixel := blurred.NRGBAAt(x, y)
			gray := float64(lrtGray(pixel.R, pixel.G, pixel.B)) / 255
			luminanceSum += gray
			luminanceSquaredSum += gray * gray
			hueSum += lrtHSBHue(pixel.R, pixel.G, pixel.B)
		}
	}
	luminance := luminanceSum / count
	variance := math.Max(0, luminanceSquaredSum/count-luminance*luminance)
	hue := hueSum / count * 360
	return lrtLongTermMetrics{
		LuminanceMean:  luminance,
		ContrastStdDev: math.Sqrt(variance),
		HueMeanDegrees: &hue,
	}
}

func lrtGray(red, green, blue uint8) int {
	return (2*int(red) + 3*int(green) + int(blue)) / 6
}

func lrtHSBHue(red, green, blue uint8) float64 {
	r, g, b := int(red), int(green), int(blue)
	maximum := max(r, max(g, b))
	minimum := min(r, min(g, b))
	delta := maximum - minimum
	if delta == 0 {
		return 0
	}
	var hue float32
	switch maximum {
	case r:
		hue = float32(g-b) / float32(delta)
		if g < b {
			hue += 6
		}
	case g:
		hue = float32(b-r)/float32(delta) + 2
	default:
		hue = float32(r-g)/float32(delta) + 4
	}
	hue /= 6
	return float64(hue)
}

func lrtGaussianKernel(scale float32) []float32 {
	radius := int(math.Ceil(float64(scale)))
	sigma := scale / 3
	denominator := 2 * sigma * sigma
	weights := make([]float32, 2*radius+1)
	sum := float32(0)
	for offset := -radius; offset <= radius; offset++ {
		squared := float32(offset * offset)
		if squared > scale*scale {
			continue
		}
		weight := float32(math.Exp(float64(-squared / denominator)))
		weights[offset+radius] = weight
		sum += weight
	}
	for index := range weights {
		weights[index] /= sum
	}
	return weights
}

func blurLRTLongTermFrame(source *image.NRGBA, kernel []float32) *image.NRGBA {
	horizontal := blurLRTLongTermAxis(source, kernel, true)
	return blurLRTLongTermAxis(horizontal, kernel, false)
}

func blurLRTLongTermAxis(source *image.NRGBA, kernel []float32, horizontal bool) *image.NRGBA {
	bounds := source.Bounds()
	result := image.NewNRGBA(bounds)
	radius := len(kernel) / 2
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			var red, green, blue, alpha float32
			for offset := -radius; offset <= radius; offset++ {
				sampleX, sampleY := x, y
				if horizontal {
					sampleX = max(bounds.Min.X, min(bounds.Max.X-1, x+offset))
				} else {
					sampleY = max(bounds.Min.Y, min(bounds.Max.Y-1, y+offset))
				}
				pixel := source.NRGBAAt(sampleX, sampleY)
				weight := kernel[offset+radius]
				red += float32(pixel.R) * weight
				green += float32(pixel.G) * weight
				blue += float32(pixel.B) * weight
				alpha += float32(pixel.A) * weight
			}
			result.SetNRGBA(x, y, color.NRGBA{
				R: roundedLRTChannel(red), G: roundedLRTChannel(green),
				B: roundedLRTChannel(blue), A: roundedLRTChannel(alpha),
			})
		}
	}
	return result
}

func roundedLRTChannel(value float32) uint8 {
	rounded := int(value + 0.5)
	return uint8(max(0, min(255, rounded)))
}
