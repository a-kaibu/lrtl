package lrtl

import (
	"image"
	"image/color"
	"math"
)

const lrtLongTermSampleSize = 64
const lrtLongTermBlurScale = 4

// measureLRTLongTermFrame follows the 7.5.5 frame-analysis path observed in
// the installed application: smoothed source RGB histograms for luminance,
// and bilinear 64x64 sampling plus Gaussian blur for contrast and hue.
// The caller applies the native camera-preview width resize and any selected
// XMP adjustments before measuring this image.
func measureLRTLongTermFrame(img image.Image) lrtLongTermMetrics {
	sampled := sampleLRTLongTermFrame(img)
	blurred := blurLRTLongTermFrame(sampled, lrtGaussianKernel(lrtLongTermBlurScale))

	count := float64(lrtLongTermSampleSize * lrtLongTermSampleSize)
	luminanceSum, luminanceSquaredSum, hueSum := 0.0, 0.0, 0.0
	for x := 0; x < lrtLongTermSampleSize; x++ {
		for y := 0; y < lrtLongTermSampleSize; y++ {
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
	var histogram [3][256]uint64
	bounds := img.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			p := colorNRGBA(img.At(x, y))
			histogram[0][p.R]++
			histogram[1][p.G]++
			histogram[2][p.B]++
		}
	}
	var weighted, total uint64
	for _, channel := range histogram {
		for bin := range channel {
			lo, hi := max(0, bin-2), min(255, bin+2)
			var sum uint64
			for _, count := range channel[lo : hi+1] {
				sum += count
			}
			count := sum / uint64(hi-lo+1)
			weighted += count * uint64(bin)
			total += count
		}
	}
	mean := 255.0
	if total != 0 {
		mean = float64(weighted) / float64(total)
	}
	return lrtLongTermMetrics{
		LuminanceMean:  mean / 256,
		ContrastStdDev: math.Sqrt(variance) * 2,
		HueMeanDegrees: &hue,
	}
}

// Java's single-pass bilinear sampling does not widen its support when
// shrinking an image. The general Go scaler does, which suppresses detail
// that the native long-term measurement retains.
func sampleLRTLongTermFrame(source image.Image) *image.NRGBA {
	bounds := source.Bounds()
	out := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		sy := (float64(y)+.5)*float64(bounds.Dy())/64 - .5
		y0 := int(math.Floor(sy))
		fy := int((sy - float64(y0)) * 256)
		for x := 0; x < 64; x++ {
			sx := (float64(x)+.5)*float64(bounds.Dx())/64 - .5
			x0 := int(math.Floor(sx))
			fx := int((sx - float64(x0)) * 256)
			var values [4]int
			for j := 0; j < 2; j++ {
				for i := 0; i < 2; i++ {
					p := colorNRGBA(source.At(bounds.Min.X+max(0, min(bounds.Dx()-1, x0+i)), bounds.Min.Y+max(0, min(bounds.Dy()-1, y0+j))))
					if p.A != 255 {
						p.R = uint8((int(p.R)*int(p.A) + 127) / 255)
						p.G = uint8((int(p.G)*int(p.A) + 127) / 255)
						p.B = uint8((int(p.B)*int(p.A) + 127) / 255)
					}
					wx, wy := 256-fx, 256-fy
					if i == 1 {
						wx = fx
					}
					if j == 1 {
						wy = fy
					}
					for k, v := range []uint8{p.R, p.G, p.B, p.A} {
						values[k] += int(v) * wx * wy
					}
				}
			}
			for i := range values {
				values[i] = (values[i] + 32768) >> 16
			}
			if values[3] > 0 && values[3] < 255 {
				// Use the native table's 24-bit reciprocal. Exact rational
				// division differs at some half-integer results.
				reciprocal := ((uint64(255) << 24) + uint64(values[3]/2)) / uint64(values[3])
				for i := 0; i < 3; i++ {
					values[i] = min(255, int((uint64(values[i])*reciprocal+(1<<23))>>24))
				}
			}
			out.SetNRGBA(x, y, color.NRGBA{uint8(values[0]), uint8(values[1]), uint8(values[2]), uint8(values[3])})
		}
	}
	return out
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
	redDistance := float32(maximum-r) / float32(delta)
	greenDistance := float32(maximum-g) / float32(delta)
	blueDistance := float32(maximum-b) / float32(delta)
	var hue float32
	switch maximum {
	case r:
		hue = blueDistance - greenDistance
	case g:
		hue = 2 + redDistance - blueDistance
	default:
		hue = 4 + greenDistance - redDistance
	}
	hue /= 6
	if hue < 0 {
		hue++
	}
	return float64(hue)
}

func lrtGaussianKernel(scale float32) []float32 {
	radius := int(math.Ceil(float64(scale)))
	sigma := scale / 3
	denominator := 2 * sigma * sigma
	normalizer := float32(math.Sqrt(float64(float32(2*math.Pi) * sigma)))
	weights := make([]float32, 2*radius+1)
	sum := float32(0)
	for offset := -radius; offset <= radius; offset++ {
		squared := float32(offset * offset)
		if squared > scale*scale {
			continue
		}
		weight := float32(math.Exp(float64(-squared/denominator))) / normalizer
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
			var red, green, blue float32
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
			}
			result.SetNRGBA(x, y, color.NRGBA{
				R: roundedLRTChannel(red), G: roundedLRTChannel(green),
				B: roundedLRTChannel(blue), A: 255,
			})
		}
	}
	return result
}

func roundedLRTChannel(value float32) uint8 {
	rounded := int(float64(value) + 0.5)
	return uint8(max(0, min(255, rounded)))
}
