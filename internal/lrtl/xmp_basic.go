package lrtl

import (
	"fmt"
	"image"
	"image/color"
	"math"
)

// xmpBasicAdjustments is the subset of global Camera Raw controls that can be
// applied meaningfully to an already-rendered sRGB image. It is deliberately
// not a replacement for Adobe Camera Raw's RAW pipeline.
type xmpBasicAdjustments struct {
	exposure   float64
	contrast   float64
	highlights float64
	shadows    float64
	whites     float64
	blacks     float64
	vibrance   float64
	saturation float64
}

func readXMPBasicAdjustments(frame Frame) (xmpBasicAdjustments, error) {
	packet, err := PacketForSidecar(frame.OutputSidecar())
	if err != nil {
		return xmpBasicAdjustments{}, err
	}
	adjustments := xmpBasicAdjustments{}
	properties := []struct {
		name   string
		target *float64
		min    float64
		max    float64
	}{
		{"Exposure2012", &adjustments.exposure, -maxRenderXMPExposureStops, maxRenderXMPExposureStops},
		{"Contrast2012", &adjustments.contrast, -100, 100},
		{"Highlights2012", &adjustments.highlights, -100, 100},
		{"Shadows2012", &adjustments.shadows, -100, 100},
		{"Whites2012", &adjustments.whites, -100, 100},
		{"Blacks2012", &adjustments.blacks, -100, 100},
		{"Vibrance", &adjustments.vibrance, -100, 100},
		{"Saturation", &adjustments.saturation, -100, 100},
	}
	for _, property := range properties {
		value, found, err := packet.GetScalar("crs:" + property.name)
		if err != nil {
			return xmpBasicAdjustments{}, err
		}
		if !found {
			continue
		}
		number, valid := scalarNumber(value)
		if !valid || number < property.min || number > property.max {
			return xmpBasicAdjustments{}, fmt.Errorf("crs:%s must be a finite number between %g and %g", property.name, property.min, property.max)
		}
		*property.target = number
	}
	return adjustments, nil
}

func (adjustments xmpBasicAdjustments) active() bool {
	return adjustments.exposure != 0 || adjustments.contrast != 0 || adjustments.highlights != 0 ||
		adjustments.shadows != 0 || adjustments.whites != 0 || adjustments.blacks != 0 ||
		adjustments.vibrance != 0 || adjustments.saturation != 0
}

func smoothstep(value float64) float64 {
	value = math.Max(0, math.Min(1, value))
	return value * value * (3 - 2*value)
}

func basicAdjustedLuminance(luminance float64, adjustments xmpBasicAdjustments) float64 {
	if adjustments.contrast != 0 {
		factor := math.Exp2(adjustments.contrast / 50)
		luminance = 0.18 + (luminance-0.18)*factor
	}
	if adjustments.shadows != 0 {
		mask := math.Pow(1-smoothstep(luminance/0.75), 2)
		luminance *= math.Exp2(adjustments.shadows / 100 * 1.5 * mask)
	}
	if adjustments.highlights != 0 {
		mask := math.Pow(smoothstep((luminance-0.25)/0.75), 2)
		luminance *= math.Exp2(adjustments.highlights / 100 * 1.5 * mask)
	}
	if adjustments.blacks != 0 {
		mask := 1 - smoothstep(luminance/0.45)
		luminance += adjustments.blacks / 100 * 0.12 * mask
	}
	if adjustments.whites != 0 {
		mask := smoothstep((luminance - 0.55) / 0.45)
		luminance += adjustments.whites / 100 * 0.12 * mask
	}
	return math.Max(0, math.Min(1, luminance))
}

func encodeLinearSRGB(value float64) uint8 {
	value = math.Max(0, math.Min(1, value))
	encoded := 12.92 * value
	if value > 0.0031308 {
		encoded = 1.055*math.Pow(value, 1.0/2.4) - 0.055
	}
	return uint8(math.Round(encoded * 255))
}

func applyXMPBasic(img image.Image, adjustments xmpBasicAdjustments) *image.NRGBA {
	bounds := img.Bounds()
	result := image.NewNRGBA(bounds)
	exposure := math.Exp2(adjustments.exposure)
	saturation := 1 + adjustments.saturation/100
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			pixel := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			red := srgb8ToLinear[pixel.R] * exposure
			green := srgb8ToLinear[pixel.G] * exposure
			blue := srgb8ToLinear[pixel.B] * exposure
			luminance := 0.2126*red + 0.7152*green + 0.0722*blue
			adjustedLuminance := basicAdjustedLuminance(luminance, adjustments)
			if luminance > 1e-12 {
				scale := adjustedLuminance / luminance
				red, green, blue = red*scale, green*scale, blue*scale
			} else {
				red, green, blue = adjustedLuminance, adjustedLuminance, adjustedLuminance
			}
			if adjustments.vibrance != 0 || adjustments.saturation != 0 {
				chroma := math.Max(red, math.Max(green, blue)) - math.Min(red, math.Min(green, blue))
				vibrance := 1 + adjustments.vibrance/100*(1-math.Min(1, chroma))
				scale := saturation * vibrance
				red = adjustedLuminance + (red-adjustedLuminance)*scale
				green = adjustedLuminance + (green-adjustedLuminance)*scale
				blue = adjustedLuminance + (blue-adjustedLuminance)*scale
			}
			result.SetNRGBA(x, y, color.NRGBA{
				R: encodeLinearSRGB(red), G: encodeLinearSRGB(green), B: encodeLinearSRGB(blue), A: pixel.A,
			})
		}
	}
	return result
}
