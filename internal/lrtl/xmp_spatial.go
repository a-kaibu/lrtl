package lrtl

import (
	"image"
	"image/color"
	"math"
)

// xmpLuminanceDetail extracts a signed high-pass luminance layer with a
// separable box blur. It is used as a bounded sRGB-domain approximation for
// Camera Raw Texture, Clarity, and Dehaze controls.
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

func xmpDehazeRadius(width, height int) int {
	return max(4, min(48, min(width, height)/40))
}

// xmpApplyDehaze uses stable display-referred contrast and veil adjustments rather than estimating atmospheric light per frame.
func xmpApplyDehaze(luminance, amount float64, detail int8) float64 {
	amount = math.Max(-100, math.Min(100, amount)) / 100
	if amount < 0 {
		haze := -amount * 0.18
		luminance = luminance*(1-haze) + 0.78*haze
	} else {
		luminance = 0.18 + (luminance-0.18)*(1+0.20*amount) + float64(detail)/127*amount*0.6
	}
	return math.Max(0, math.Min(1, luminance))
}

func xmpSharpenRadius(width, height int, radius float64) int {
	scale := float64(min(width, height)) / 1080
	return max(1, min(24, int(math.Round(radius*scale))))
}

func xmpApplySharpness(luminance, amount, detail, edgeMasking float64, highpass int8) float64 {
	amount = math.Max(-100, math.Min(150, amount)) / 100
	detail = math.Max(0, math.Min(100, detail))
	edgeMasking = math.Max(0, math.Min(100, edgeMasking))
	edgeWeight := 1.0
	if edgeMasking > 0 {
		threshold := edgeMasking / 100 * 0.12
		edgeWeight = smoothstep((math.Abs(float64(highpass)/127) - threshold) / 0.12)
	}
	gain := (0.35 + detail/100*0.65) * edgeWeight * 0.75
	return math.Max(0, math.Min(1, luminance+float64(highpass)/127*amount*gain))
}

func xmpApplyImageSharpness(img *image.NRGBA, amount, radius, detail, edgeMasking float64) *image.NRGBA {
	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	highpass := xmpLuminanceDetail(img, xmpSharpenRadius(width, height, radius))
	result := image.NewNRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			pixel := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			index := (y-bounds.Min.Y)*width + (x - bounds.Min.X)
			luminance := 0.2126*srgb8ToLinear[pixel.R] + 0.7152*srgb8ToLinear[pixel.G] + 0.0722*srgb8ToLinear[pixel.B]
			sharpened := xmpApplySharpness(luminance, amount, detail, edgeMasking, highpass[index])
			if luminance > 1e-12 {
				scale := sharpened / luminance
				pixel.R = encodeLinearSRGB(srgb8ToLinear[pixel.R] * scale)
				pixel.G = encodeLinearSRGB(srgb8ToLinear[pixel.G] * scale)
				pixel.B = encodeLinearSRGB(srgb8ToLinear[pixel.B] * scale)
			} else {
				value := encodeLinearSRGB(sharpened)
				pixel.R, pixel.G, pixel.B = value, value, value
			}
			result.SetNRGBA(x, y, pixel)
		}
	}
	return result
}

// xmpApplyNoiseReduction is a fixed-neighborhood sRGB approximation, not a RAW denoiser.
func xmpApplyNoiseReduction(img *image.NRGBA, luminanceAmount, luminanceDetail, luminanceContrast, colorAmount, colorDetail, colorSmoothness float64) *image.NRGBA {
	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width <= 0 || height <= 0 {
		return img
	}
	luminanceAmount = math.Max(0, math.Min(100, luminanceAmount)) / 100
	colorAmount = math.Max(0, math.Min(100, colorAmount)) / 100
	luminanceDetail = math.Max(0, math.Min(100, luminanceDetail))
	luminanceContrast = math.Max(0, math.Min(100, luminanceContrast))
	colorDetail = math.Max(0, math.Min(100, colorDetail))
	colorSmoothness = math.Max(0, math.Min(100, colorSmoothness))
	luminanceSigma := 0.015 + (100-luminanceDetail)/100*0.09
	chromaSigma := 0.012 + colorSmoothness/100*0.09 + (100-colorDetail)/100*0.03
	result := image.NewNRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			pixel := img.NRGBAAt(x, y)
			centerY, centerU, centerV := xmpLinearLumaChroma(pixel)
			lumaSum, lumaWeightSum := 0.0, 0.0
			chromaUSum, chromaVSum, chromaWeightSum := 0.0, 0.0, 0.0
			for dy := -1; dy <= 1; dy++ {
				neighborY := max(bounds.Min.Y, min(bounds.Max.Y-1, y+dy))
				for dx := -1; dx <= 1; dx++ {
					neighborX := max(bounds.Min.X, min(bounds.Max.X-1, x+dx))
					neighbor := img.NRGBAAt(neighborX, neighborY)
					neighborLuma, neighborU, neighborV := xmpLinearLumaChroma(neighbor)
					spatialWeight := 1.0
					if dx != 0 && dy != 0 {
						spatialWeight = 0.7
					} else if dx == 0 && dy == 0 {
						spatialWeight = 2
					}
					lumaAffinity := math.Max(0, 1-math.Abs(neighborLuma-centerY)/luminanceSigma)
					lumaWeight := spatialWeight * lumaAffinity
					lumaSum += neighborLuma * lumaWeight
					lumaWeightSum += lumaWeight
					chromaDifference := math.Max(math.Abs(neighborU-centerU), math.Abs(neighborV-centerV))
					chromaAffinity := math.Max(0, 1-chromaDifference/chromaSigma)
					chromaWeight := spatialWeight * lumaAffinity * chromaAffinity
					chromaUSum += neighborU * chromaWeight
					chromaVSum += neighborV * chromaWeight
					chromaWeightSum += chromaWeight
				}
			}
			lumaAverage := centerY
			if lumaWeightSum > 0 {
				lumaAverage = lumaSum / lumaWeightSum
			}
			chromaUAverage, chromaVAverage := centerU, centerV
			if chromaWeightSum > 0 {
				chromaUAverage = chromaUSum / chromaWeightSum
				chromaVAverage = chromaVSum / chromaWeightSum
			}
			adjustedY := centerY + (lumaAverage-centerY)*luminanceAmount
			adjustedY += (centerY - lumaAverage) * luminanceAmount * luminanceContrast / 100 * 0.35
			adjustedU := centerU + (chromaUAverage-centerU)*colorAmount
			adjustedV := centerV + (chromaVAverage-centerV)*colorAmount
			red := adjustedY + adjustedV
			blue := adjustedY + adjustedU
			green := (adjustedY - 0.2126*red - 0.0722*blue) / 0.7152
			result.SetNRGBA(x, y, color.NRGBA{
				R: encodeLinearSRGB(red), G: encodeLinearSRGB(green), B: encodeLinearSRGB(blue), A: pixel.A,
			})
		}
	}
	return result
}

func xmpLinearLumaChroma(pixel color.NRGBA) (luminance, blueDifference, redDifference float64) {
	red, green, blue := srgb8ToLinear[pixel.R], srgb8ToLinear[pixel.G], srgb8ToLinear[pixel.B]
	luminance = 0.2126*red + 0.7152*green + 0.0722*blue
	return luminance, blue - luminance, red - luminance
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
