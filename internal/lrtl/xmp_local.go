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
	localMaskLuminanceRange
	localMaskColorRange
)

type localMaskBlendMode uint8

const (
	// Camera Raw serializes Add as 0 and Intersect as 1.
	localMaskBlendAdd localMaskBlendMode = iota
	localMaskBlendIntersect
)

// xmpLocalCorrection is the renderable subset of a Camera Raw local correction.
// The geometry is evaluated in normalized, oriented image coordinates.
type xmpLocalCorrection struct {
	exposure, contrast, highlights, shadows float64
	whites, blacks, saturation              float64
	toningHue, toningSaturation             float64
	whiteBalance                            [3]float64
	hasWhiteBalance                         bool
	colorGrading                            xmpColorGrading
	mainCurve                               [256]uint8
	hasMainCurve                            bool
	channelCurve                            [3][256]uint8
	hasChannelCurve                         [3]bool
	amount                                  float64
	masks                                   []xmpLocalMask
}

type xmpLocalLuminanceRange struct {
	lowOuter, lowInner, highInner, highOuter float64
	invert                                   bool
}

type xmpLocalColorRange struct {
	samples [][3]float64
	amount  float64
	invert  bool
}

type xmpLocalMask struct {
	kind                  localMaskKind
	blendMode             localMaskBlendMode
	maskValue             float64
	zeroX, zeroY          float64
	fullX, fullY          float64
	left, top             float64
	right, bottom         float64
	angle, feather        float64
	flipped, maskInverted bool
	luminanceRange        *xmpLocalLuminanceRange
	colorRange            *xmpLocalColorRange
}

func readXMPLocalCorrections(packet *XMPPacket) ([]xmpLocalCorrection, error) {
	corrections := make([]xmpLocalCorrection, 0)
	for _, collection := range []string{"MaskGroupBasedCorrections", "GradientBasedCorrections", "CircularGradientBasedCorrections"} {
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
	correction := xmpLocalCorrection{amount: 1}
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
	for _, property := range []struct {
		name   string
		target *float64
	}{
		{name: "LocalContrast2012", target: &correction.contrast},
		{name: "LocalHighlights2012", target: &correction.highlights},
		{name: "LocalShadows2012", target: &correction.shadows},
		{name: "LocalWhites2012", target: &correction.whites},
		{name: "LocalBlacks2012", target: &correction.blacks},
		{name: "LocalSaturation", target: &correction.saturation},
	} {
		*property.target, _, err = xmpLocalSlider(element, property.name)
		if err != nil {
			return correction, false, err
		}
	}
	correction.toningHue, _, err = xmpLocalNumber(element, "LocalToningHue", 230, 0, 360)
	if err != nil {
		return correction, false, err
	}
	correction.toningSaturation, _, err = xmpLocalNumber(element, "LocalToningSaturation", 0, 0, 1)
	if err != nil {
		return correction, false, err
	}
	correction.toningSaturation *= 100
	temperature, _, err := xmpLocalSlider(element, "LocalTemperature")
	if err != nil {
		return correction, false, err
	}
	tint, _, err := xmpLocalSlider(element, "LocalTint")
	if err != nil {
		return correction, false, err
	}
	if temperature != 0 || tint != 0 {
		correction.whiteBalance = approximateWhiteBalanceGains(6500+temperature*40, tint)
		correction.hasWhiteBalance = true
	}
	correction.colorGrading, err = readXMPLocalColorGrading(element)
	if err != nil {
		return correction, false, err
	}
	correction.mainCurve, correction.hasMainCurve, err = readXMPLocalToneCurve(element, "MainCurve")
	if err != nil {
		return correction, false, err
	}
	for index, name := range []string{"RedCurve", "GreenCurve", "BlueCurve"} {
		correction.channelCurve[index], correction.hasChannelCurve[index], err = readXMPLocalToneCurve(element, name)
		if err != nil {
			return correction, false, err
		}
	}
	for _, name := range []string{"ExtendedMainCurve", "ExtendedRedCurve", "ExtendedGreenCurve", "ExtendedBlueCurve"} {
		if hasLocalToneCurveProperty(element, name) {
			// Extended point curves use an HDR-domain representation that the
			// current 8-bit sRGB LUT cannot preserve.
			return correction, false, nil
		}
	}
	for _, token := range element.Child {
		selector, ok := token.(*etree.Element)
		if !ok || selector.NamespaceURI() != crsNamespace {
			continue
		}
		if selector.Tag == "CorrectionRangeMask" || selector.Tag == "CorrectionSubject" {
			// Do not render only the spatial base while silently dropping its selector.
			return correction, false, nil
		}
	}
	if correction.exposure == 0 && correction.contrast == 0 && correction.highlights == 0 && correction.shadows == 0 &&
		correction.whites == 0 && correction.blacks == 0 && correction.saturation == 0 && correction.toningSaturation == 0 &&
		!correction.hasWhiteBalance && !correction.colorGrading.active && !correction.hasMainCurve &&
		!correction.hasChannelCurve[0] && !correction.hasChannelCurve[1] && !correction.hasChannelCurve[2] {
		return correction, false, nil
	}

	var masks []*etree.Element
	var findMasks func(*etree.Element) error
	findMasks = func(node *etree.Element) error {
		what, found, err := crsElementValue(node, "What")
		if err != nil {
			return err
		}
		if found && strings.TrimSpace(what) == "Mask/Aggregate" {
			for _, token := range node.Child {
				if child, ok := token.(*etree.Element); ok {
					if err := findMasks(child); err != nil {
						return err
					}
				}
			}
			return nil
		}
		if found && strings.HasPrefix(strings.TrimSpace(what), "Mask/") {
			masks = append(masks, node)
			return nil
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
		child, ok := token.(*etree.Element)
		if !ok || child.NamespaceURI() != crsNamespace || child.Tag != "CorrectionMasks" {
			continue
		}
		if err := findMasks(child); err != nil {
			return correction, false, err
		}
	}
	if len(masks) == 0 {
		return correction, false, nil
	}
	for _, maskElement := range masks {
		maskActive, _, err := xmpLocalBool(maskElement, "MaskActive", true)
		if err != nil {
			return correction, false, err
		}
		if !maskActive {
			continue
		}
		mask, supported, err := parseXMPLocalMask(maskElement)
		if err != nil {
			return correction, false, err
		}
		if !supported {
			return correction, false, nil
		}
		correction.masks = append(correction.masks, mask)
	}
	if len(correction.masks) == 0 {
		return correction, false, nil
	}
	return correction, true, nil
}

func parseXMPLocalMask(element *etree.Element) (xmpLocalMask, bool, error) {
	mask := xmpLocalMask{maskValue: 1}
	what, _, _ := crsElementValue(element, "What")
	switch strings.TrimSpace(what) {
	case "Mask/Gradient":
		mask.kind = localMaskGradient
	case "Mask/CircularGradient":
		mask.kind = localMaskCircularGradient
	case "Mask/RangeMask":
		mask.kind = localMaskLuminanceRange
	default:
		return mask, false, nil
	}
	blendMode, _, err := xmpLocalNumber(element, "MaskBlendMode", float64(localMaskBlendAdd), 0, 1)
	if err != nil {
		return mask, false, err
	}
	if math.Trunc(blendMode) != blendMode {
		return mask, false, fmt.Errorf("crs:MaskBlendMode must be 0 (add) or 1 (intersect)")
	}
	mask.blendMode = localMaskBlendMode(blendMode)
	if mask.kind == localMaskLuminanceRange {
		// RangeMask is an explicit selector component, not a spatial base.
		// Camera Raw can store MaskValue=0 on this component while the
		// selector still refines the neighboring spatial mask.
		mask.maskValue = 1
		selectorType, found, err := xmpRangeMaskType(element)
		if err != nil {
			return mask, false, err
		}
		if !found {
			return mask, false, nil
		}
		switch selectorType {
		case 1:
			mask.kind = localMaskColorRange
			mask.colorRange, found, err = parseXMPColorRange(element)
		case 2:
			mask.luminanceRange, found, err = parseXMPLuminanceRange(element)
		default:
			return mask, false, nil
		}
		if err != nil || !found {
			return mask, false, err
		}
	} else {
		mask.maskValue, _, err = xmpLocalNumber(element, "MaskValue", 1, 0, 1)
		if err != nil {
			return mask, false, err
		}
	}
	mask.maskInverted, _, err = xmpLocalBool(element, "MaskInverted", false)
	if err != nil {
		return mask, false, err
	}
	mask.flipped, _, err = xmpLocalBool(element, "Flipped", false)
	if err != nil {
		return mask, false, err
	}
	if mask.kind == localMaskLuminanceRange || mask.kind == localMaskColorRange {
		return mask, true, nil
	}
	if mask.kind == localMaskGradient {
		var haveZeroX, haveZeroY, haveFullX, haveFullY bool
		mask.zeroX, haveZeroX, err = xmpLocalNumber(element, "ZeroX", 0, -2, 3)
		if err != nil {
			return mask, false, err
		}
		mask.zeroY, haveZeroY, err = xmpLocalNumber(element, "ZeroY", 0, -2, 3)
		if err != nil {
			return mask, false, err
		}
		mask.fullX, haveFullX, err = xmpLocalNumber(element, "FullX", 0, -2, 3)
		if err != nil {
			return mask, false, err
		}
		mask.fullY, haveFullY, err = xmpLocalNumber(element, "FullY", 0, -2, 3)
		if err != nil {
			return mask, false, err
		}
		if !haveZeroX || !haveZeroY || !haveFullX || !haveFullY || (mask.zeroX == mask.fullX && mask.zeroY == mask.fullY) {
			return mask, false, nil
		}
		return mask, true, nil
	}

	var haveLeft, haveTop, haveRight, haveBottom bool
	mask.left, haveLeft, err = xmpLocalNumber(element, "Left", 0, -2, 3)
	if err != nil {
		return mask, false, err
	}
	mask.top, haveTop, err = xmpLocalNumber(element, "Top", 0, -2, 3)
	if err != nil {
		return mask, false, err
	}
	mask.right, haveRight, err = xmpLocalNumber(element, "Right", 0, -2, 3)
	if err != nil {
		return mask, false, err
	}
	mask.bottom, haveBottom, err = xmpLocalNumber(element, "Bottom", 0, -2, 3)
	if err != nil {
		return mask, false, err
	}
	if !haveLeft || !haveTop || !haveRight || !haveBottom || math.Abs(mask.right-mask.left) <= 1e-12 || math.Abs(mask.bottom-mask.top) <= 1e-12 {
		return mask, false, nil
	}
	mask.angle, _, err = xmpLocalNumber(element, "Angle", 0, -3600, 3600)
	if err != nil {
		return mask, false, err
	}
	mask.feather, _, err = xmpLocalNumber(element, "Feather", 50, -100, 100)
	if err != nil {
		return mask, false, err
	}
	return mask, true, nil
}

func parseXMPLuminanceRange(element *etree.Element) (*xmpLocalLuminanceRange, bool, error) {
	rangeElement := correctionRangeMaskElement(element)
	if rangeElement == nil {
		return nil, false, nil
	}
	typeText, hasType, err := crsElementValue(rangeElement, "Type")
	if err != nil {
		return nil, false, err
	}
	if hasType {
		typeValue, valid := scalarNumber(typeText)
		if !valid || math.Trunc(typeValue) != typeValue {
			return nil, false, fmt.Errorf("crs:CorrectionRangeMask Type must be an integer")
		}
		// Type 1 is Color and Type 3 is Depth in observed Camera Raw XMP.
		if typeValue != 2 {
			return nil, false, nil
		}
	}
	text, found, err := crsElementValue(rangeElement, "LumRange")
	if err != nil {
		return nil, false, err
	}
	if !found {
		return nil, false, nil
	}
	parts := strings.Fields(text)
	if len(parts) != 4 {
		return nil, false, fmt.Errorf("crs:CorrectionRangeMask LumRange must contain four values")
	}
	values := [4]float64{}
	for index, part := range parts {
		value, valid := scalarNumber(part)
		if !valid || value < 0 || value > 1 {
			return nil, false, fmt.Errorf("crs:CorrectionRangeMask LumRange values must be between 0 and 1")
		}
		values[index] = value
	}
	if values[0] > values[1] || values[1] > values[2] || values[2] > values[3] {
		return nil, false, fmt.Errorf("crs:CorrectionRangeMask LumRange values must be ordered")
	}
	invert, _, err := xmpLocalBool(rangeElement, "Invert", false)
	if err != nil {
		return nil, false, err
	}
	return &xmpLocalLuminanceRange{
		lowOuter: values[0], lowInner: values[1], highInner: values[2], highOuter: values[3], invert: invert,
	}, true, nil
}

func xmpRangeMaskType(element *etree.Element) (int, bool, error) {
	rangeElement := correctionRangeMaskElement(element)
	if rangeElement == nil {
		return 0, false, nil
	}
	typeText, found, err := crsElementValue(rangeElement, "Type")
	if err != nil || !found {
		return 0, found, err
	}
	typeValue, valid := scalarNumber(typeText)
	if !valid || math.Trunc(typeValue) != typeValue {
		return 0, false, fmt.Errorf("crs:CorrectionRangeMask Type must be an integer")
	}
	return int(typeValue), true, nil
}

func parseXMPColorRange(element *etree.Element) (*xmpLocalColorRange, bool, error) {
	rangeElement := correctionRangeMaskElement(element)
	if rangeElement == nil {
		return nil, false, nil
	}
	selectorType, found, err := xmpRangeMaskType(element)
	if err != nil || !found || selectorType != 1 {
		return nil, false, err
	}
	amount, _, err := xmpLocalNumber(rangeElement, "ColorAmount", 0.5, 0, 1)
	if err != nil {
		return nil, false, err
	}
	sampleType, found, err := xmpLocalNumber(rangeElement, "SampleType", 0, 0, 5)
	if err != nil {
		return nil, false, err
	}
	if found && math.Trunc(sampleType) != sampleType {
		return nil, false, fmt.Errorf("crs:CorrectionRangeMask SampleType must be an integer")
	}
	if found && sampleType != 0 {
		return nil, false, nil
	}
	invert, _, err := xmpLocalBool(rangeElement, "Invert", false)
	if err != nil {
		return nil, false, err
	}
	pointModels := findCRSDescendant(rangeElement, "PointModels")
	items := rdfArrayItems(pointModels)
	if len(items) == 0 {
		return nil, false, nil
	}
	rangeMask := &xmpLocalColorRange{amount: amount, invert: invert}
	for _, item := range items {
		text, scalar := scalarElementText(item)
		if !scalar {
			return nil, false, fmt.Errorf("crs:CorrectionRangeMask PointModels entries must be strings")
		}
		parts := strings.Fields(text)
		if len(parts) != 6 {
			return nil, false, fmt.Errorf("crs:CorrectionRangeMask PointModels entries must contain six values")
		}
		values := [6]float64{}
		for index, part := range parts {
			value, valid := scalarNumber(part)
			if !valid || value < 0 || value > 1 {
				return nil, false, fmt.Errorf("crs:CorrectionRangeMask PointModels values must be between 0 and 1")
			}
			values[index] = value
		}
		// Observed Camera Raw records contain normalized sample coordinates,
		// three sampled sRGB channels, and a trailing zero-valued discriminator.
		// This renderer uses the sampled color and does not use the coordinates.
		if values[5] != 0 {
			return nil, false, nil
		}
		rangeMask.samples = append(rangeMask.samples, [3]float64{values[2], values[3], values[4]})
	}
	return rangeMask, true, nil
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

func xmpLocalSlider(element *etree.Element, name string) (float64, bool, error) {
	// Observed Camera Raw XMP stores these local sliders as normalized values in [-1, 1].
	value, found, err := xmpLocalNumber(element, name, 0, -1, 1)
	return value * 100, found, err
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

func (correction xmpLocalCorrection) maskWeight(x, y, width, height int, luminance float64, rgb [3]float64) float64 {
	if len(correction.masks) == 0 {
		return 0
	}
	weight := 0.0
	for index, mask := range correction.masks {
		component := mask.maskWeight(x, y, width, height, luminance, rgb)
		if index == 0 {
			weight = component
			continue
		}
		switch mask.blendMode {
		case localMaskBlendAdd:
			// Treat soft mask weights as independent alpha coverage when adding.
			weight += component * (1 - weight)
		case localMaskBlendIntersect:
			weight *= component
		}
	}
	return math.Max(0, math.Min(1, weight))
}

func (mask xmpLocalMask) maskWeight(x, y, width, height int, luminance float64, rgb [3]float64) float64 {
	nx := (float64(x) + 0.5) / float64(width)
	ny := (float64(y) + 0.5) / float64(height)
	weight := 0.0
	switch mask.kind {
	case localMaskGradient:
		dx, dy := mask.zeroX-mask.fullX, mask.zeroY-mask.fullY
		lengthSquared := dx*dx + dy*dy
		position := ((nx-mask.fullX)*dx + (ny-mask.fullY)*dy) / lengthSquared
		weight = 1 - math.Max(0, math.Min(1, position))
	case localMaskCircularGradient:
		radius, decoded := radialMaskWeight(mask.left, mask.top, mask.right, mask.bottom, mask.angle, x, y, width, height)
		if !decoded {
			centerX, centerY := (mask.left+mask.right)/2, (mask.top+mask.bottom)/2
			halfWidth := math.Abs(mask.right-mask.left) * float64(width) / 2
			halfHeight := math.Abs(mask.bottom-mask.top) * float64(height) / 2
			dx, dy := (nx-centerX)*float64(width), (ny-centerY)*float64(height)
			angle := mask.angle * math.Pi / 180
			rotatedX := dx*math.Cos(angle) + dy*math.Sin(angle)
			rotatedY := -dx*math.Sin(angle) + dy*math.Cos(angle)
			radius = math.Sqrt((rotatedX/halfWidth)*(rotatedX/halfWidth) + (rotatedY/halfHeight)*(rotatedY/halfHeight))
		}
		feather := math.Abs(mask.feather) / 100
		if feather < 1e-6 {
			if radius < 1 {
				weight = 1
			}
		} else {
			weight = 1 - smoothstep((radius-(1-feather))/feather)
		}
	case localMaskLuminanceRange:
		weight = luminanceRangeWeight(*mask.luminanceRange, luminance)
	case localMaskColorRange:
		weight = colorRangeWeight(*mask.colorRange, rgb)
	}
	if mask.flipped != mask.maskInverted {
		weight = 1 - weight
	}
	return math.Max(0, math.Min(1, weight*mask.maskValue))
}

func luminanceRangeWeight(luminanceRange xmpLocalLuminanceRange, luminance float64) float64 {
	luminance = math.Max(0, math.Min(1, luminance))
	weight := 1.0
	if luminance < luminanceRange.lowInner {
		if luminanceRange.lowInner <= luminanceRange.lowOuter {
			weight = 0
		} else {
			weight = smoothstep((luminance - luminanceRange.lowOuter) / (luminanceRange.lowInner - luminanceRange.lowOuter))
		}
	}
	if luminance > luminanceRange.highInner {
		if luminanceRange.highOuter <= luminanceRange.highInner {
			weight = 0
		} else {
			weight *= 1 - smoothstep((luminance-luminanceRange.highInner)/(luminanceRange.highOuter-luminanceRange.highInner))
		}
	}
	if luminanceRange.invert {
		weight = 1 - weight
	}
	return weight
}

func colorRangeWeight(colorRange xmpLocalColorRange, rgb [3]float64) float64 {
	if len(colorRange.samples) == 0 {
		return 0
	}
	pixelLab := srgbToLab(rgb)
	// ColorAmount is only known to control the selected range width. This
	// monotone Delta-E 76 falloff is an independent approximation, not Adobe's
	// proprietary color selector.
	radius := 1 + 55*math.Max(0, math.Min(1, colorRange.amount))
	feather := math.Max(1, radius*0.15)
	weight := 0.0
	for _, sample := range colorRange.samples {
		sampleLab := srgbToLab(sample)
		deltaL := pixelLab[0] - sampleLab[0]
		deltaA := pixelLab[1] - sampleLab[1]
		deltaB := pixelLab[2] - sampleLab[2]
		distance := math.Sqrt(deltaL*deltaL + deltaA*deltaA + deltaB*deltaB)
		component := 1 - smoothstep((distance-radius)/feather)
		weight += component * (1 - weight)
	}
	if colorRange.invert {
		weight = 1 - weight
	}
	return math.Max(0, math.Min(1, weight))
}

func srgbToLab(rgb [3]float64) [3]float64 {
	linear := [3]float64{}
	for index, value := range rgb {
		value = math.Max(0, math.Min(1, value))
		if value <= 0.04045 {
			linear[index] = value / 12.92
		} else {
			linear[index] = math.Pow((value+0.055)/1.055, 2.4)
		}
	}
	x := (0.4124564*linear[0] + 0.3575761*linear[1] + 0.1804375*linear[2]) / 0.95047
	y := 0.2126729*linear[0] + 0.7151522*linear[1] + 0.0721750*linear[2]
	z := (0.0193339*linear[0] + 0.1191920*linear[1] + 0.9503041*linear[2]) / 1.08883
	f := func(value float64) float64 {
		if value > 216.0/24389.0 {
			return math.Cbrt(value)
		}
		return (24389.0/27.0*value + 16) / 116
	}
	fx, fy, fz := f(x), f(y), f(z)
	return [3]float64{116*fy - 16, 500 * (fx - fy), 200 * (fy - fz)}
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
				maskLuminance := 0.2126*srgb8ToLinear[pixel.R] + 0.7152*srgb8ToLinear[pixel.G] + 0.0722*srgb8ToLinear[pixel.B]
				pixelRGB := [3]float64{float64(pixel.R) / 255, float64(pixel.G) / 255, float64(pixel.B) / 255}
				weight := correction.maskWeight(x-bounds.Min.X, y-bounds.Min.Y, width, height, maskLuminance, pixelRGB) * correction.amount
				redGain, greenGain, blueGain := 1.0, 1.0, 1.0
				if correction.hasWhiteBalance {
					blend := math.Max(-1, math.Min(1, weight))
					redGain += (correction.whiteBalance[0] - 1) * blend
					greenGain += (correction.whiteBalance[1] - 1) * blend
					blueGain += (correction.whiteBalance[2] - 1) * blend
				}
				red := srgb8ToLinear[pixel.R] * redGain
				green := srgb8ToLinear[pixel.G] * greenGain
				blue := srgb8ToLinear[pixel.B] * blueGain
				if correction.exposure != 0 {
					gain := math.Exp2(correction.exposure * weight)
					red, green, blue = red*gain, green*gain, blue*gain
				}
				luminance := 0.2126*red + 0.7152*green + 0.0722*blue
				localTone := xmpBasicAdjustments{
					contrast: correction.contrast * weight, highlights: correction.highlights * weight,
					shadows: correction.shadows * weight, whites: correction.whites * weight,
					blacks: correction.blacks * weight,
				}
				adjustedLuminance := luminance
				if localTone.contrast != 0 || localTone.highlights != 0 || localTone.shadows != 0 || localTone.whites != 0 || localTone.blacks != 0 {
					adjustedLuminance = basicAdjustedLuminance(luminance, localTone)
					if luminance > 1e-12 {
						scale := adjustedLuminance / luminance
						red, green, blue = red*scale, green*scale, blue*scale
					} else {
						red, green, blue = adjustedLuminance, adjustedLuminance, adjustedLuminance
					}
				}
				if correction.saturation != 0 {
					factor := 1 + correction.saturation/100*weight
					red = adjustedLuminance + (red-adjustedLuminance)*factor
					green = adjustedLuminance + (green-adjustedLuminance)*factor
					blue = adjustedLuminance + (blue-adjustedLuminance)*factor
				}
				if correction.toningSaturation > 0 {
					red, green, blue = applyLocalToningColor(red, green, blue, correction.toningHue, correction.toningSaturation, weight)
				}
				red8, green8, blue8 := encodeLinearSRGB(red), encodeLinearSRGB(green), encodeLinearSRGB(blue)
				curveBlend := math.Max(0, math.Min(1, weight))
				if correction.hasMainCurve {
					red8 = blendLocalCurve(red8, correction.mainCurve, curveBlend)
					green8 = blendLocalCurve(green8, correction.mainCurve, curveBlend)
					blue8 = blendLocalCurve(blue8, correction.mainCurve, curveBlend)
				}
				if correction.hasChannelCurve[0] {
					red8 = blendLocalCurve(red8, correction.channelCurve[0], curveBlend)
				}
				if correction.hasChannelCurve[1] {
					green8 = blendLocalCurve(green8, correction.channelCurve[1], curveBlend)
				}
				if correction.hasChannelCurve[2] {
					blue8 = blendLocalCurve(blue8, correction.channelCurve[2], curveBlend)
				}
				if correction.colorGrading.active {
					red8, green8, blue8 = applyXMPColorGrading(red8, green8, blue8, correction.colorGrading, curveBlend)
				}
				result.SetNRGBA(x, y, color.NRGBA{
					R: red8, G: green8, B: blue8, A: pixel.A,
				})
			}
		}
		current = result
	}
	return current
}

func blendLocalCurve(input uint8, curve [256]uint8, amount float64) uint8 {
	return uint8(math.Round(float64(input)*(1-amount) + float64(curve[input])*amount))
}

func applyLocalToningColor(red, green, blue, hue, saturation, weight float64) (float64, float64, float64) {
	blend := math.Max(0, math.Min(1, saturation/100*weight))
	if blend == 0 {
		return red, green, blue
	}
	hue = math.Mod(hue, 360)
	if hue < 0 {
		hue += 360
	}
	red8, green8, blue8 := encodeLinearSRGB(red), encodeLinearSRGB(green), encodeLinearSRGB(blue)
	redSRGB, greenSRGB, blueSRGB := float64(red8)/255, float64(green8)/255, float64(blue8)/255
	_, _, luminance := srgbToHSL(redSRGB, greenSRGB, blueSRGB)
	tintRed, tintGreen, tintBlue := hslToSRGB(hue, 1, luminance)
	return srgb8ToLinear[encodeSRGBByte(redSRGB*(1-blend)+tintRed*blend)],
		srgb8ToLinear[encodeSRGBByte(greenSRGB*(1-blend)+tintGreen*blend)],
		srgb8ToLinear[encodeSRGBByte(blueSRGB*(1-blend)+tintBlue*blend)]
}
