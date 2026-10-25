package lrtl

import (
	"fmt"
	"math"
)

func readXMPGrayscale(packet *XMPPacket, adjustments *xmpBasicAdjustments) error {
	value, found, err := packet.GetRootScalar("crs:ConvertToGrayscale")
	if err != nil {
		return err
	}
	if found {
		adjustments.grayscale, err = parseCameraRawBool(value)
		if err != nil {
			return fmt.Errorf("crs:ConvertToGrayscale: %w", err)
		}
	}
	for i, band := range xmpHSLBands {
		value, _, err := readXMPBasicNumber(packet, "GrayMixer"+band.name, -100, 100)
		if err != nil {
			return err
		}
		adjustments.grayMix[i] = value
	}
	return nil
}

// This is an independent sRGB approximation, not Adobe's monochrome profile.
// With neutral sliders, preserve linear Rec.709 luminance. Interpolate the
// eight mixer values around the existing hue centers, then map +/-100 to up
// to +/-2 stops, attenuated by encoded chroma so neutral pixels stay neutral.
// White balance/exposure precede this stage; tone curves, grading and local
// corrections follow it and can intentionally tint the monochrome image.
func xmpGrayscaleLuminance(red, green, blue float64, mixer [8]float64) float64 {
	luminance := 0.2126*red + 0.7152*green + 0.0722*blue
	r, g, b := float64(encodeLinearSRGB16(red))/65535, float64(encodeLinearSRGB16(green))/65535, float64(encodeLinearSRGB16(blue))/65535
	hue, _, _ := srgbToHSL(r, g, b)
	var bands [8]xmpHSLColorAdjustment
	for i, value := range mixer {
		bands[i].luminance = value
	}
	amount := interpolateHSLBandAdjustments(bands, hue).luminance
	chroma := math.Max(r, math.Max(g, b)) - math.Min(r, math.Min(g, b))
	return luminance * math.Exp2(2*amount/100*chroma)
}
