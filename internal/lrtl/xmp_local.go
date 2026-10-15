package lrtl

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"strings"

	"github.com/beevik/etree"
)

type localMaskKind uint8

const (
	localMaskGradient localMaskKind = iota + 1
	localMaskCircularGradient
)

// xmpLocalCorrection is the renderable subset of a Camera Raw local correction.
// The geometry is evaluated in normalized, oriented image coordinates.
type xmpLocalCorrection struct {
	kind                 localMaskKind
	exposure, saturation float64
	amount, maskValue    float64
	zeroX, zeroY         float64
	fullX, fullY         float64
	left, top            float64
	right, bottom        float64
	angle, feather       float64
	flipped              bool
}

func readXMPLocalCorrections(packet *XMPPacket) ([]xmpLocalCorrection, error) {
	corrections := make([]xmpLocalCorrection, 0)
	for _, collection := range []string{"GradientBasedCorrections", "CircularGradientBasedCorrections"} {
		property := packet.findRootProperty(crsNamespace, collection)
		if property.element == nil {
			continue
		}
		var walk func(*etree.Element) error
		walk = func(element *etree.Element) error {
			what, found, err := crsElementValue(element, "What")
			if err != nil {
				return err
			}
			if found && strings.TrimSpace(what) == "Correction" {
				correction, supported, err := parseXMPLocalCorrection(element)
				if err != nil {
					return err
				}
				if supported {
					corrections = append(corrections, correction)
				}
			}
			for _, token := range element.Child {
				if child, ok := token.(*etree.Element); ok {
					if err := walk(child); err != nil {
						return err
					}
				}
			}
			return nil
		}
		if err := walk(property.element); err != nil {
			return nil, fmt.Errorf("crs:%s: %w", collection, err)
		}
	}
	return corrections, nil
}

func parseXMPLocalCorrection(element *etree.Element) (xmpLocalCorrection, bool, error) {
	correction := xmpLocalCorrection{amount: 1, maskValue: 1}
	active, _, err := xmpLocalBool(element, "CorrectionActive", true)
	if err != nil || !active {
		return correction, false, err
	}
	correction.amount, _, err = xmpLocalNumber(element, "CorrectionAmount", 1, -10, 10)
	if err != nil {
		return correction, false, err
	}
	correction.exposure, _, err = xmpLocalNumber(element, "LocalExposure2012", 0, -10, 10)
	if err != nil {
		return correction, false, err
	}
	correction.saturation, _, err = xmpLocalNumber(element, "LocalSaturation", 0, -100, 100)
	if err != nil {
		return correction, false, err
	}
	if correction.exposure == 0 && correction.saturation == 0 {
		return correction, false, nil
	}

	var masks []*etree.Element
	var findMasks func(*etree.Element) error
	findMasks = func(node *etree.Element) error {
		what, found, err := crsElementValue(node, "What")
		if err != nil {
			return err
		}
		if found && strings.HasPrefix(strings.TrimSpace(what), "Mask/") {
			masks = append(masks, node)
		}
		for _, token := range node.Child {
			if child, ok := token.(*etree.Element); ok {
				if err := findMasks(child); err != nil {
					return err
				}
			}
		}
		return nil
	}
	for _, token := range element.Child {
		if child, ok := token.(*etree.Element); ok {
			if err := findMasks(child); err != nil {
				return correction, false, err
			}
		}
	}
	// Combined masks need mask-compositing semantics that are not modeled yet.
	if len(masks) != 1 {
		return correction, false, nil
	}
	mask := masks[0]
	what, _, _ := crsElementValue(mask, "What")
	switch strings.TrimSpace(what) {
	case "Mask/Gradient":
		correction.kind = localMaskGradient
	case "Mask/CircularGradient":
		correction.kind = localMaskCircularGradient
	default:
		return correction, false, nil
	}
	maskActive, _, err := xmpLocalBool(mask, "MaskActive", true)
	if err != nil || !maskActive {
		return correction, false, err
	}
	correction.maskValue, _, err = xmpLocalNumber(mask, "MaskValue", 1, 0, 1)
	if err != nil {
		return correction, false, err
	}
	if correction.maskValue == 0 {
		return correction, false, nil
	}
	if correction.kind == localMaskGradient {
		var haveZeroX, haveZeroY, haveFullX, haveFullY bool
		correction.zeroX, haveZeroX, err = xmpLocalNumber(mask, "ZeroX", 0, -2, 3)
		if err != nil {
			return correction, false, err
		}
		correction.zeroY, haveZeroY, err = xmpLocalNumber(mask, "ZeroY", 0, -2, 3)
		if err != nil {
			return correction, false, err
		}
		correction.fullX, haveFullX, err = xmpLocalNumber(mask, "FullX", 0, -2, 3)
		if err != nil {
			return correction, false, err
		}
		correction.fullY, haveFullY, err = xmpLocalNumber(mask, "FullY", 0, -2, 3)
		if err != nil {
			return correction, false, err
		}
		if !haveZeroX || !haveZeroY || !haveFullX || !haveFullY || (correction.zeroX == correction.fullX && correction.zeroY == correction.fullY) {
			return correction, false, nil
		}
		correction.flipped, _, err = xmpLocalBool(mask, "Flipped", false)
		return correction, err == nil, err
	}

	var haveLeft, haveTop, haveRight, haveBottom bool
	correction.left, haveLeft, err = xmpLocalNumber(mask, "Left", 0, -2, 3)
	if err != nil {
		return correction, false, err
	}
	correction.top, haveTop, err = xmpLocalNumber(mask, "Top", 0, -2, 3)
	if err != nil {
		return correction, false, err
	}
	correction.right, haveRight, err = xmpLocalNumber(mask, "Right", 0, -2, 3)
	if err != nil {
		return correction, false, err
	}
	correction.bottom, haveBottom, err = xmpLocalNumber(mask, "Bottom", 0, -2, 3)
	if err != nil {
		return correction, false, err
	}
	if !haveLeft || !haveTop || !haveRight || !haveBottom || correction.right <= correction.left || correction.bottom <= correction.top {
		return correction, false, nil
	}
	correction.angle, _, err = xmpLocalNumber(mask, "Angle", 0, -3600, 3600)
	if err != nil {
		return correction, false, err
	}
	correction.feather, _, err = xmpLocalNumber(mask, "Feather", 50, -100, 100)
	if err != nil {
		return correction, false, err
	}
	correction.flipped, _, err = xmpLocalBool(mask, "Flipped", false)
	if err != nil {
		return correction, false, err
	}
	return correction, true, nil
}

func xmpLocalNumber(element *etree.Element, name string, fallback, min, max float64) (float64, bool, error) {
	text, found, err := crsElementValue(element, name)
	if err != nil || !found {
		return fallback, found, err
	}
	value, valid := scalarNumber(text)
	if !valid || value < min || value > max {
		return 0, false, fmt.Errorf("crs:%s must be a finite number between %g and %g", name, min, max)
	}
	return value, true, nil
}

func xmpLocalBool(element *etree.Element, name string, fallback bool) (bool, bool, error) {
	text, found, err := crsElementValue(element, name)
	if err != nil || !found {
		return fallback, found, err
	}
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "true", "1":
		return true, true, nil
	case "false", "0":
		return false, true, nil
	default:
		return false, false, fmt.Errorf("crs:%s must be true or false", name)
	}
}

func (correction xmpLocalCorrection) maskWeight(x, y, width, height int) float64 {
	nx := (float64(x) + 0.5) / float64(width)
	ny := (float64(y) + 0.5) / float64(height)
	weight := 0.0
	switch correction.kind {
	case localMaskGradient:
		dx, dy := correction.zeroX-correction.fullX, correction.zeroY-correction.fullY
		lengthSquared := dx*dx + dy*dy
		position := ((nx-correction.fullX)*dx + (ny-correction.fullY)*dy) / lengthSquared
		weight = 1 - math.Max(0, math.Min(1, position))
	case localMaskCircularGradient:
		centerX, centerY := (correction.left+correction.right)/2, (correction.top+correction.bottom)/2
		halfWidth := (correction.right - correction.left) * float64(width) / 2
		halfHeight := (correction.bottom - correction.top) * float64(height) / 2
		dx, dy := (nx-centerX)*float64(width), (ny-centerY)*float64(height)
		angle := correction.angle * math.Pi / 180
		rotatedX := dx*math.Cos(angle) + dy*math.Sin(angle)
		rotatedY := -dx*math.Sin(angle) + dy*math.Cos(angle)
		radius := math.Sqrt((rotatedX/halfWidth)*(rotatedX/halfWidth) + (rotatedY/halfHeight)*(rotatedY/halfHeight))
		feather := math.Abs(correction.feather) / 100
		if feather < 1e-6 {
			if radius < 1 {
				weight = 1
			}
		} else {
			weight = 1 - smoothstep((radius-(1-feather))/feather)
		}
	}
	if correction.flipped {
		weight = 1 - weight
	}
	return math.Max(0, math.Min(1, weight*correction.maskValue))
}

func applyXMPLocalCorrections(img image.Image, corrections []xmpLocalCorrection) image.Image {
	if len(corrections) == 0 {
		return img
	}
	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width <= 0 || height <= 0 {
		return img
	}
	current := img
	for _, correction := range corrections {
		result := image.NewNRGBA(bounds)
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				pixel := color.NRGBAModel.Convert(current.At(x, y)).(color.NRGBA)
				weight := correction.maskWeight(x-bounds.Min.X, y-bounds.Min.Y, width, height) * correction.amount
				red, green, blue := srgb8ToLinear[pixel.R], srgb8ToLinear[pixel.G], srgb8ToLinear[pixel.B]
				if correction.exposure != 0 {
					gain := math.Exp2(correction.exposure * weight)
					red, green, blue = red*gain, green*gain, blue*gain
				}
				if correction.saturation != 0 {
					luminance := 0.2126*red + 0.7152*green + 0.0722*blue
					factor := 1 + correction.saturation/100*weight
					red = luminance + (red-luminance)*factor
					green = luminance + (green-luminance)*factor
					blue = luminance + (blue-luminance)*factor
				}
				result.SetNRGBA(x, y, color.NRGBA{
					R: encodeLinearSRGB(red), G: encodeLinearSRGB(green), B: encodeLinearSRGB(blue), A: pixel.A,
				})
			}
		}
		current = result
	}
	return current
}
