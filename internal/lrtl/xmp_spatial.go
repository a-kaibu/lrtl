package lrtl

import (
	"image"
	"image/color"
	"math"
)

// xmpLuminanceDetail extracts a signed high-pass luminance layer with a
// separable box blur. It is used as a bounded sRGB-domain approximation for
// Camera Raw Texture, Clarity, and Dehaze controls.
func xmpLuminanceDetail(img image.Image, radius int) []float32 {
	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width <= 0 || height <= 0 {
		return nil
	}
	radius = max(1, radius)
	window := radius*2 + 1
	horizontal := make([]float32, width*height)
	blurred := make([]float32, width*height)

	for y := 0; y < height; y++ {
		sum := 0.0
		for offset := -radius; offset <= radius; offset++ {
			x := min(width-1, max(0, offset))
			sum += xmpLuminanceEncodedAt(img, bounds.Min.X+x, bounds.Min.Y+y)
		}
		for x := 0; x < width; x++ {
			horizontal[y*width+x] = float32(sum / float64(window))
			leaving := min(width-1, max(0, x-radius))
			entering := min(width-1, max(0, x+radius+1))
			sum -= xmpLuminanceEncodedAt(img, bounds.Min.X+leaving, bounds.Min.Y+y)
			sum += xmpLuminanceEncodedAt(img, bounds.Min.X+entering, bounds.Min.Y+y)
		}
	}
	for x := 0; x < width; x++ {
		sum := 0.0
		for offset := -radius; offset <= radius; offset++ {
			y := min(height-1, max(0, offset))
			sum += float64(horizontal[y*width+x])
		}
		for y := 0; y < height; y++ {
			blurred[y*width+x] = float32(sum / float64(window))
			leaving := min(height-1, max(0, y-radius))
			entering := min(height-1, max(0, y+radius+1))
			sum -= float64(horizontal[leaving*width+x])
			sum += float64(horizontal[entering*width+x])
		}
	}

	// Each blurred sample is consumed once; reuse it for the returned detail
	// layer rather than allocating a third full-size floating-point buffer.
	detail := blurred
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			index := y*width + x
			original := decodeSRGBFloat(xmpLuminanceEncodedAt(img, bounds.Min.X+x, bounds.Min.Y+y))
			lowFrequency := decodeSRGBFloat(float64(blurred[index]))
			value := (original - lowFrequency) * 127
			value = math.Max(-127, math.Min(127, value))
			detail[index] = float32(value)
		}
	}
	return detail
}

func xmpLuminanceEncodedAt(img image.Image, x, y int) float64 {
	pixel := color.NRGBA64Model.Convert(img.At(x, y)).(color.NRGBA64)
	luminance := 0.2126*srgb16ToLinear[pixel.R] + 0.7152*srgb16ToLinear[pixel.G] + 0.0722*srgb16ToLinear[pixel.B]
	return encodeLinearSRGBFloat(luminance)
}

func xmpDehazeRadius(width, height int) int {
	return max(4, min(48, min(width, height)/40))
}

// xmpApplyDehaze uses stable display-referred contrast and veil adjustments rather than estimating atmospheric light per frame.
func xmpApplyDehaze(luminance, amount float64, detail float32) float64 {
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

func xmpApplySharpness(luminance, amount, detail, edgeMasking float64, highpass float32) float64 {
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

func xmpApplyImageSharpness(img *image.NRGBA64, amount, radius, detail, edgeMasking float64) *image.NRGBA64 {
	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	highpass := xmpLuminanceDetail(img, xmpSharpenRadius(width, height, radius))
	result := image.NewNRGBA64(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			pixel := color.NRGBA64Model.Convert(img.At(x, y)).(color.NRGBA64)
			index := (y-bounds.Min.Y)*width + (x - bounds.Min.X)
			luminance := 0.2126*srgb16ToLinear[pixel.R] + 0.7152*srgb16ToLinear[pixel.G] + 0.0722*srgb16ToLinear[pixel.B]
			sharpened := xmpApplySharpness(luminance, amount, detail, edgeMasking, highpass[index])
			if luminance > 1e-12 {
				scale := sharpened / luminance
				pixel.R = encodeLinearSRGB16(srgb16ToLinear[pixel.R] * scale)
				pixel.G = encodeLinearSRGB16(srgb16ToLinear[pixel.G] * scale)
				pixel.B = encodeLinearSRGB16(srgb16ToLinear[pixel.B] * scale)
			} else {
				value := encodeLinearSRGB16(sharpened)
				pixel.R, pixel.G, pixel.B = value, value, value
			}
			result.SetNRGBA64(x, y, pixel)
		}
	}
	return result
}

// xmpApplyNoiseReduction is a fixed-neighborhood sRGB approximation, not a RAW denoiser.
func xmpApplyNoiseReduction(img *image.NRGBA64, luminanceAmount, luminanceDetail, luminanceContrast, colorAmount, colorDetail, colorSmoothness float64) *image.NRGBA64 {
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
	result := image.NewNRGBA64(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			pixel := img.NRGBA64At(x, y)
			centerY, centerU, centerV := xmpLinearLumaChroma(pixel)
			lumaSum, lumaWeightSum := 0.0, 0.0
			chromaUSum, chromaVSum, chromaWeightSum := 0.0, 0.0, 0.0
			for dy := -1; dy <= 1; dy++ {
				neighborY := max(bounds.Min.Y, min(bounds.Max.Y-1, y+dy))
				for dx := -1; dx <= 1; dx++ {
					neighborX := max(bounds.Min.X, min(bounds.Max.X-1, x+dx))
					neighbor := img.NRGBA64At(neighborX, neighborY)
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
			result.SetNRGBA64(x, y, color.NRGBA64{
				R: encodeLinearSRGB16(red), G: encodeLinearSRGB16(green), B: encodeLinearSRGB16(blue), A: pixel.A,
			})
		}
	}
	return result
}

func xmpLinearLumaChroma(pixel color.NRGBA64) (luminance, blueDifference, redDifference float64) {
	red, green, blue := srgb16ToLinear[pixel.R], srgb16ToLinear[pixel.G], srgb16ToLinear[pixel.B]
	luminance = 0.2126*red + 0.7152*green + 0.0722*blue
	return luminance, blue - luminance, red - luminance
}

// xmpApplyPostCropVignette approximates Lightroom's crop-relative radial exposure effect.
func xmpApplyPostCropVignette(img *image.NRGBA64, amount, midpoint, roundness, feather, highlightContrast float64) *image.NRGBA64 {
	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width <= 0 || height <= 0 {
		return img
	}
	aspect := float64(width) / float64(height)
	xScale, yScale := 1.0, 1.0
	if aspect >= 1 {
		xScale = math.Pow(aspect, roundness/100)
	} else {
		yScale = math.Pow(1/aspect, roundness/100)
	}
	outerRadius := math.Hypot(xScale, yScale)
	startRadius := outerRadius * (0.18 + 0.66*math.Max(0, math.Min(100, midpoint))/100)
	featherWidth := outerRadius * (0.02 + 0.48*math.Max(0, math.Min(100, feather))/100)
	amount = math.Max(-100, math.Min(100, amount))
	highlightContrast = math.Max(0, math.Min(100, highlightContrast)) / 100
	result := image.NewNRGBA64(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		normalizedY := (2*(float64(y-bounds.Min.Y)+0.5)/float64(height) - 1) * yScale
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			normalizedX := (2*(float64(x-bounds.Min.X)+0.5)/float64(width) - 1) * xScale
			radius := math.Hypot(normalizedX, normalizedY)
			coverage := smoothstep((radius - startRadius) / featherWidth)
			gain := math.Exp2(amount / 100 * 1.5 * coverage)
			pixel := img.NRGBA64At(x, y)
			red, green, blue := srgb16ToLinear[pixel.R], srgb16ToLinear[pixel.G], srgb16ToLinear[pixel.B]
			if amount < 0 && highlightContrast > 0 {
				luminance := 0.2126*red + 0.7152*green + 0.0722*blue
				highlightWeight := smoothstep((luminance - 0.35) / 0.65)
				gain = 1 + (gain-1)*(1-highlightContrast*highlightWeight)
			}
			result.SetNRGBA64(x, y, color.NRGBA64{
				R: encodeLinearSRGB16(red * gain), G: encodeLinearSRGB16(green * gain), B: encodeLinearSRGB16(blue * gain), A: pixel.A,
			})
		}
	}
	return result
}

func xmpTextureRadius(width, height int) int {
	return max(1, min(3, min(width, height)/600))
}

func xmpClarityRadius(width, height int) int {
	return max(3, min(24, min(width, height)/120))
}

func xmpApplyDetail(luminance, texture, clarity float64, textureDetail, clarityDetail float32) float64 {
	delta := float64(textureDetail) / 127 * texture / 100 * 0.75
	if clarity != 0 {
		midtoneWeight := 4 * luminance * (1 - luminance)
		delta += float64(clarityDetail) / 127 * clarity / 100 * 1.25 * midtoneWeight
	}
	return math.Max(0, math.Min(1, luminance+delta))
}
