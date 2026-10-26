package lrtl

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"strings"

	"github.com/beevik/etree"
)

type localMaskKind uint16

const (
	localMaskGradient localMaskKind = iota + 1
	localMaskCircularGradient
	localMaskLuminanceRange
	localMaskColorRange
	localMaskBrush
)

type localMaskBlendMode uint16

const (
	// Observed Camera Raw XMP serializes Add, Subtract, and Intersect as 0, 1, and 2.
	localMaskBlendAdd localMaskBlendMode = iota
	localMaskBlendSubtract
	localMaskBlendIntersect
)

const maxLocalBrushDabs = 262144

// xmpLocalCorrection is the renderable subset of a Camera Raw local correction.
// The geometry is evaluated in normalized, oriented image coordinates.
type xmpLocalCorrection struct {
	exposure, contrast, highlights, shadows float64
	whites, blacks, saturation, texture     float64
	hue                                     float64
	clarity, dehaze                         float64
	sharpness, luminanceNoise, moire        float64
	defringe                                float64
	grain                                   float64
	grainSize, grainFrequency               float64
	toningHue, toningSaturation             float64
	whiteBalance                            [3]float64
	hasWhiteBalance                         bool
	colorGrading                            xmpColorGrading
	mainCurve                               toneCurveTable
	hasMainCurve                            bool
	channelCurve                            [3]toneCurveTable
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
	brush                 *xmpLocalBrush
}

type xmpLocalBrush struct {
	centerWeight float64
	dabs         []xmpLocalBrushDab
}

type xmpLocalBrushDab struct {
	x, y, radius float64
	flow         float64
}

func readXMPLocalCorrections(packet *XMPPacket) ([]xmpLocalCorrection, error) {
	corrections := make([]xmpLocalCorrection, 0)
	_, managedHolyGrailElement, err := readHolyGrailRenderCorrection(packet)
	if err != nil {
		return nil, err
	}
	_, managedDeflickerElement, err := readDeflickerRenderCorrection(packet)
	if err != nil {
		return nil, err
	}
	for _, collection := range []string{"MaskGroupBasedCorrections", "GradientBasedCorrections", "CircularGradientBasedCorrections", "PaintBasedCorrections"} {
		property := packet.findRootProperty(crsNamespace, collection)
		if property.element == nil {
			continue
		}
		var walk func(*etree.Element) error
		walk = func(element *etree.Element) error {
			if element == managedHolyGrailElement || element == managedDeflickerElement {
				return nil
			}
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
		{name: "LocalHue", target: &correction.hue},
		{name: "LocalTexture", target: &correction.texture},
		{name: "LocalClarity2012", target: &correction.clarity},
		{name: "LocalDehaze", target: &correction.dehaze},
		{name: "LocalSharpness", target: &correction.sharpness},
		{name: "LocalLuminanceNoise", target: &correction.luminanceNoise},
		{name: "LocalMoire", target: &correction.moire},
		{name: "LocalDefringe", target: &correction.defringe},
		{name: "LocalGrain", target: &correction.grain},
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
			// current bounded SDR sRGB pipeline cannot preserve.
			return correction, false, nil
		}
	}
	for _, token := range element.Child {
		selector, ok := token.(*etree.Element)
		if !ok || selector.NamespaceURI() != crsNamespace {
			continue
		}
		if selector.Tag == "CorrectionSubject" {
			// Do not render only the spatial base while silently dropping an AI selector.
			return correction, false, nil
		}
		if selector.Tag == "CorrectionRangeMask" {
			// Older brush XMP commonly writes a neutral Type=0 range structure.
			// Spatial-only brushes remain renderable; non-neutral selectors do not.
			typeValue, found, err := xmpLocalNumber(selector, "Type", 0, 0, 5)
			if err != nil {
				return correction, false, err
			}
			if found && math.Trunc(typeValue) != typeValue {
				return correction, false, fmt.Errorf("crs:CorrectionRangeMask Type must be an integer")
			}
			if found && typeValue != 0 {
				return correction, false, nil
			}
		}
	}
	if !correction.active() {
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
	case "Mask/Paint":
		mask.kind = localMaskBrush
	default:
		return mask, false, nil
	}
	blendMode, _, err := xmpLocalNumber(element, "MaskBlendMode", float64(localMaskBlendAdd), 0, 2)
	if err != nil {
		return mask, false, err
	}
	if math.Trunc(blendMode) != blendMode {
		return mask, false, fmt.Errorf("crs:MaskBlendMode must be 0 (add), 1 (subtract), or 2 (intersect)")
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
	if mask.kind == localMaskBrush {
		brush, supported, brushErr := parseXMPLocalBrush(element)
		if brushErr != nil || !supported {
			return mask, false, brushErr
		}
		mask.brush = brush
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

func parseXMPLocalBrush(element *etree.Element) (*xmpLocalBrush, bool, error) {
	radius, foundRadius, err := xmpLocalNumber(element, "Radius", 0, 0, 2)
	if err != nil {
		return nil, false, err
	}
	if !foundRadius || radius <= 0 {
		return nil, false, nil
	}
	flow, _, err := xmpLocalNumber(element, "Flow", 1, 0, 1)
	if err != nil {
		return nil, false, err
	}
	centerWeight, _, err := xmpLocalNumber(element, "CenterWeight", 0, 0, 1)
	if err != nil {
		return nil, false, err
	}
	dabsProperty := directCRSProperty(element, "Dabs")
	items := rdfArrayItems(dabsProperty.element)
	if len(items) == 0 || len(items) > maxLocalBrushDabs {
		return nil, false, nil
	}
	brush := &xmpLocalBrush{centerWeight: centerWeight, dabs: make([]xmpLocalBrushDab, 0, len(items))}
	for index, item := range items {
		text, scalar := scalarElementText(unwrapRDFArrayItem(item))
		if !scalar {
			return nil, false, fmt.Errorf("crs:Dabs[%d] must be a text command", index+1)
		}
		command := strings.Fields(strings.TrimSpace(text))
		if len(command) == 0 {
			return nil, false, fmt.Errorf("crs:Dabs[%d] is empty", index+1)
		}
		switch command[0] {
		case "r":
			if len(command) != 2 {
				return nil, false, fmt.Errorf("crs:Dabs[%d] radius command must contain one value", index+1)
			}
			radius, err = localBrushNumber(command[1], 0, 2)
			if err != nil || radius <= 0 {
				return nil, false, fmt.Errorf("crs:Dabs[%d] radius must be between 0 and 2", index+1)
			}
		case "f":
			if len(command) != 2 {
				return nil, false, fmt.Errorf("crs:Dabs[%d] flow command must contain one value", index+1)
			}
			flow, err = localBrushNumber(command[1], 0, 1)
			if err != nil {
				return nil, false, fmt.Errorf("crs:Dabs[%d] flow must be between 0 and 1", index+1)
			}
		case "d":
			if len(command) != 3 {
				return nil, false, fmt.Errorf("crs:Dabs[%d] dab command must contain x and y", index+1)
			}
			x, parseErr := localBrushNumber(command[1], -2, 3)
			if parseErr != nil {
				return nil, false, fmt.Errorf("crs:Dabs[%d] x coordinate is invalid", index+1)
			}
			y, parseErr := localBrushNumber(command[2], -2, 3)
			if parseErr != nil {
				return nil, false, fmt.Errorf("crs:Dabs[%d] y coordinate is invalid", index+1)
			}
			brush.dabs = append(brush.dabs, xmpLocalBrushDab{x: x, y: y, radius: radius, flow: flow})
		default:
			// Do not partially render brush paths with an unknown command.
			return nil, false, nil
		}
	}
	if len(brush.dabs) == 0 {
		return nil, false, nil
	}
	return brush, true, nil
}

func localBrushNumber(text string, minimum, maximum float64) (float64, error) {
	value, valid := scalarNumber(text)
	if !valid || value < minimum || value > maximum {
		return 0, fmt.Errorf("number must be between %g and %g", minimum, maximum)
	}
	return value, nil
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
		component := mask.correctionWeight(x, y, width, height, luminance, rgb)
		blendMode := mask.correctionBlendMode()
		if index == 0 {
			if blendMode == localMaskBlendSubtract {
				weight = 0
			} else {
				weight = component
			}
			continue
		}
		switch blendMode {
		case localMaskBlendAdd:
			// Treat soft mask weights as independent alpha coverage when adding.
			weight += component * (1 - weight)
		case localMaskBlendIntersect:
			weight *= component
		case localMaskBlendSubtract:
			weight *= 1 - component
		}
	}
	return math.Max(0, math.Min(1, weight))
}

func (mask xmpLocalMask) correctionBlendMode() localMaskBlendMode {
	// Camera Raw records eraser brush strokes as Paint with MaskValue=0,
	// even when MaskBlendMode remains Add.
	if mask.kind == localMaskBrush && mask.maskValue == 0 {
		return localMaskBlendSubtract
	}
	return mask.blendMode
}

func (mask xmpLocalMask) correctionWeight(x, y, width, height int, luminance float64, rgb [3]float64) float64 {
	if mask.kind != localMaskBrush || mask.maskValue != 0 {
		return mask.maskWeight(x, y, width, height, luminance, rgb)
	}
	nx := (float64(x) + 0.5) / float64(width)
	ny := (float64(y) + 0.5) / float64(height)
	weight := localBrushMaskWeight(mask.brush, nx, ny, width, height)
	if mask.flipped != mask.maskInverted {
		weight = 1 - weight
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
	case localMaskBrush:
		weight = localBrushMaskWeight(mask.brush, nx, ny, width, height)
	}
	if mask.flipped != mask.maskInverted {
		weight = 1 - weight
	}
	return math.Max(0, math.Min(1, weight*mask.maskValue))
}

func localBrushMaskWeight(brush *xmpLocalBrush, nx, ny float64, width, height int) float64 {
	if brush == nil || len(brush.dabs) == 0 || width <= 0 || height <= 0 {
		return 0
	}
	weight := 0.0
	for _, dab := range brush.dabs {
		coverage := localBrushDabWeight(brush, dab, nx, ny, width, height)
		weight += coverage * (1 - weight)
	}
	return math.Max(0, math.Min(1, weight))
}

func localBrushDabWeight(brush *xmpLocalBrush, dab xmpLocalBrushDab, nx, ny float64, width, height int) float64 {
	radius := dab.radius * float64(width)
	if radius <= 0 {
		return 0
	}
	dx := (nx - dab.x) * float64(width)
	dy := (ny - dab.y) * float64(height)
	distance := math.Hypot(dx, dy) / radius
	if distance >= 1 {
		return 0
	}
	coverage := 1.0
	if brush.centerWeight < 1 && distance > brush.centerWeight {
		coverage = 1 - smoothstep((distance-brush.centerWeight)/(1-brush.centerWeight))
	}
	return coverage * dab.flow
}

func rasterizeLocalBrushMaskRow(brush *xmpLocalBrush, width, height, y int, weights []float32) {
	clear(weights)
	if brush == nil || width <= 0 || height <= 0 || y < 0 || y >= height || len(weights) < width {
		return
	}
	for _, dab := range brush.dabs {
		radius := dab.radius * float64(width)
		if radius <= 0 {
			continue
		}
		centerX, centerY := dab.x*float64(width)-0.5, dab.y*float64(height)-0.5
		dy := float64(y) - centerY
		if math.Abs(dy) >= radius {
			continue
		}
		left := max(0, int(math.Ceil(centerX-radius)))
		right := min(width-1, int(math.Floor(centerX+radius)))
		ny := (float64(y) + 0.5) / float64(height)
		for x := left; x <= right; x++ {
			nx := (float64(x) + 0.5) / float64(width)
			coverage := localBrushDabWeight(brush, dab, nx, ny, width, height)
			if coverage > 0 {
				previous := float64(weights[x])
				weights[x] = float32(previous + coverage*(1-previous))
			}
		}
	}
}

func rasterizeXMPLocalCorrectionMask(img image.Image, correction xmpLocalCorrection) []float32 {
	if len(correction.masks) == 0 {
		return nil
	}
	hasBrush := false
	for _, mask := range correction.masks {
		if mask.kind == localMaskBrush {
			hasBrush = true
			break
		}
	}
	if !hasBrush {
		return nil
	}
	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width <= 0 || height <= 0 || uint64(width)*uint64(height) > uint64(maxDevelopedRAWBytes)/4 {
		return nil
	}
	weights := make([]float32, width*height)
	brushRow := make([]float32, width)
	for maskIndex, mask := range correction.masks {
		for y := 0; y < height; y++ {
			if mask.kind == localMaskBrush {
				rasterizeLocalBrushMaskRow(mask.brush, width, height, y, brushRow)
			}
			for x := 0; x < width; x++ {
				component := 0.0
				blendMode := mask.correctionBlendMode()
				if mask.kind == localMaskBrush {
					component = float64(brushRow[x])
					if mask.flipped != mask.maskInverted {
						component = 1 - component
					}
					if mask.maskValue != 0 {
						component *= mask.maskValue
					}
					component = math.Max(0, math.Min(1, component))
				} else {
					pixel := color.NRGBA64Model.Convert(img.At(bounds.Min.X+x, bounds.Min.Y+y)).(color.NRGBA64)
					luminance := 0.2126*srgb16ToLinear[pixel.R] + 0.7152*srgb16ToLinear[pixel.G] + 0.0722*srgb16ToLinear[pixel.B]
					rgb := [3]float64{float64(pixel.R) / 65535, float64(pixel.G) / 65535, float64(pixel.B) / 65535}
					component = mask.maskWeight(x, y, width, height, luminance, rgb)
				}
				index := y*width + x
				if maskIndex == 0 {
					if blendMode != localMaskBlendSubtract {
						weights[index] = float32(component)
					}
					continue
				}
				weight := float64(weights[index])
				switch blendMode {
				case localMaskBlendAdd:
					weight += component * (1 - weight)
				case localMaskBlendIntersect:
					weight *= component
				case localMaskBlendSubtract:
					weight *= 1 - component
				}
				weights[index] = float32(math.Max(0, math.Min(1, weight)))
			}
		}
	}
	return weights
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
		if !correction.active() {
			continue
		}
		var textureDetail, clarityDetail, dehazeDetail, sharpnessDetail, defringeDetail []float32
		var noiseReduced, moireReduced *image.NRGBA64
		if correction.luminanceNoise != 0 || correction.moire != 0 {
			noiseInput, ok := current.(*image.NRGBA64)
			if !ok {
				noiseInput = image.NewNRGBA64(bounds)
				for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
					for x := bounds.Min.X; x < bounds.Max.X; x++ {
						noiseInput.SetNRGBA64(x, y, color.NRGBA64Model.Convert(current.At(x, y)).(color.NRGBA64))
					}
				}
			}
			// Full-strength candidates are blended through the local mask below.
			// Negative serialized values extrapolate away from that candidate to
			// approximate sharpening or increasing color aliasing in the selected area.
			if correction.luminanceNoise != 0 {
				noiseReduced = xmpApplyNoiseReduction(noiseInput, 100, 50, 0, 0, 50, 100)
			}
			if correction.moire != 0 {
				moireReduced = xmpApplyNoiseReduction(noiseInput, 0, 50, 0, 100, 50, 100)
			}
		}
		if correction.dehaze != 0 {
			dehazeDetail = xmpLuminanceDetail(current, xmpDehazeRadius(width, height))
		}
		if correction.texture != 0 {
			textureDetail = xmpLuminanceDetail(current, xmpTextureRadius(width, height))
		}
		if correction.clarity != 0 {
			clarityDetail = xmpLuminanceDetail(current, xmpClarityRadius(width, height))
		}
		if correction.sharpness != 0 {
			sharpnessDetail = xmpLuminanceDetail(current, xmpSharpenRadius(width, height, 1))
		}
		if correction.defringe != 0 {
			defringeDetail = xmpLuminanceDetail(current, 1)
		}
		maskWeights := rasterizeXMPLocalCorrectionMask(current, correction)
		result := image.NewNRGBA64(bounds)
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				pixel := color.NRGBA64Model.Convert(current.At(x, y)).(color.NRGBA64)
				weightIndex := (y-bounds.Min.Y)*width + x - bounds.Min.X
				maskWeight := 0.0
				if weightIndex < len(maskWeights) {
					maskWeight = float64(maskWeights[weightIndex])
				} else {
					maskLuminance := 0.2126*srgb16ToLinear[pixel.R] + 0.7152*srgb16ToLinear[pixel.G] + 0.0722*srgb16ToLinear[pixel.B]
					pixelRGB := [3]float64{float64(pixel.R) / 65535, float64(pixel.G) / 65535, float64(pixel.B) / 65535}
					maskWeight = correction.maskWeight(x-bounds.Min.X, y-bounds.Min.Y, width, height, maskLuminance, pixelRGB)
				}
				weight := maskWeight * correction.amount
				if correction.luminanceNoise != 0 {
					strength := math.Max(-1, math.Min(1, correction.luminanceNoise/100*weight))
					pixel = blendLocalNoisePixel(pixel, noiseReduced.NRGBA64At(x, y), strength)
				}
				if correction.moire != 0 {
					strength := math.Max(-1, math.Min(1, correction.moire/100*weight))
					pixel = blendLocalMoirePixel(pixel, moireReduced.NRGBA64At(x, y), strength)
				}
				if correction.defringe != 0 {
					strength := correction.defringe / 100 * weight
					index := (y-bounds.Min.Y)*width + (x - bounds.Min.X)
					pixel = applyLocalDefringe(pixel, defringeDetail[index], strength)
				}
				redGain, greenGain, blueGain := 1.0, 1.0, 1.0
				if correction.hasWhiteBalance {
					blend := math.Max(-1, math.Min(1, weight))
					redGain += (correction.whiteBalance[0] - 1) * blend
					greenGain += (correction.whiteBalance[1] - 1) * blend
					blueGain += (correction.whiteBalance[2] - 1) * blend
				}
				red := srgb16ToLinear[pixel.R] * redGain
				green := srgb16ToLinear[pixel.G] * greenGain
				blue := srgb16ToLinear[pixel.B] * blueGain
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
				if correction.texture != 0 || correction.clarity != 0 {
					index := (y-bounds.Min.Y)*width + (x - bounds.Min.X)
					var fineDetail, broadDetail float32
					if textureDetail != nil {
						fineDetail = textureDetail[index]
					}
					if clarityDetail != nil {
						broadDetail = clarityDetail[index]
					}
					spatialLuminance := xmpApplyDetail(adjustedLuminance, correction.texture*weight, correction.clarity*weight, fineDetail, broadDetail)
					if adjustedLuminance > 1e-12 {
						scale := spatialLuminance / adjustedLuminance
						red, green, blue = red*scale, green*scale, blue*scale
					} else {
						red, green, blue = spatialLuminance, spatialLuminance, spatialLuminance
					}
					adjustedLuminance = spatialLuminance
				}
				if correction.dehaze != 0 {
					index := (y-bounds.Min.Y)*width + (x - bounds.Min.X)
					spatialLuminance := xmpApplyDehaze(adjustedLuminance, correction.dehaze*weight, dehazeDetail[index])
					if adjustedLuminance > 1e-12 {
						scale := spatialLuminance / adjustedLuminance
						red, green, blue = red*scale, green*scale, blue*scale
					} else {
						red, green, blue = spatialLuminance, spatialLuminance, spatialLuminance
					}
					adjustedLuminance = spatialLuminance
				}
				if correction.sharpness != 0 {
					index := (y-bounds.Min.Y)*width + (x - bounds.Min.X)
					spatialLuminance := xmpApplySharpness(adjustedLuminance, correction.sharpness*weight, 50, 0, sharpnessDetail[index])
					if adjustedLuminance > 1e-12 {
						scale := spatialLuminance / adjustedLuminance
						red, green, blue = red*scale, green*scale, blue*scale
					} else {
						red, green, blue = spatialLuminance, spatialLuminance, spatialLuminance
					}
					adjustedLuminance = spatialLuminance
				}
				if correction.saturation != 0 || correction.dehaze != 0 {
					dehazeAmount := math.Max(-100, math.Min(100, correction.dehaze*weight))
					factor := (1 + correction.saturation/100*weight) * (1 + dehazeAmount/100*0.15)
					red = adjustedLuminance + (red-adjustedLuminance)*factor
					green = adjustedLuminance + (green-adjustedLuminance)*factor
					blue = adjustedLuminance + (blue-adjustedLuminance)*factor
				}
				if correction.toningSaturation > 0 {
					red, green, blue = applyLocalToningColor(red, green, blue, correction.toningHue, correction.toningSaturation, weight)
				}
				if correction.hue != 0 {
					red, green, blue = applyLocalHueShift(red, green, blue, correction.hue*weight)
				}
				red16, green16, blue16 := encodeLinearSRGB16(red), encodeLinearSRGB16(green), encodeLinearSRGB16(blue)
				curveBlend := math.Max(0, math.Min(1, weight))
				if correction.hasMainCurve {
					red16 = blendLocalCurve(red16, correction.mainCurve, curveBlend)
					green16 = blendLocalCurve(green16, correction.mainCurve, curveBlend)
					blue16 = blendLocalCurve(blue16, correction.mainCurve, curveBlend)
				}
				if correction.hasChannelCurve[0] {
					red16 = blendLocalCurve(red16, correction.channelCurve[0], curveBlend)
				}
				if correction.hasChannelCurve[1] {
					green16 = blendLocalCurve(green16, correction.channelCurve[1], curveBlend)
				}
				if correction.hasChannelCurve[2] {
					blue16 = blendLocalCurve(blue16, correction.channelCurve[2], curveBlend)
				}
				if correction.colorGrading.active {
					red16, green16, blue16 = applyXMPColorGrading(red16, green16, blue16, correction.colorGrading, curveBlend)
				}
				if correction.grain != 0 {
					grainAmount := math.Max(-100, math.Min(100, correction.grain*weight))
					pixel = xmpApplyGrainPixel(color.NRGBA64{R: red16, G: green16, B: blue16, A: pixel.A}, x, y,
						grainAmount, correction.grainSize, correction.grainFrequency)
					red16, green16, blue16 = pixel.R, pixel.G, pixel.B
				}
				result.SetNRGBA64(x, y, color.NRGBA64{
					R: red16, G: green16, B: blue16, A: pixel.A,
				})
			}
		}
		current = result
	}
	return current
}

func (correction xmpLocalCorrection) active() bool {
	return correction.amount != 0 && (correction.exposure != 0 || correction.contrast != 0 || correction.highlights != 0 || correction.shadows != 0 || correction.whites != 0 || correction.blacks != 0 || correction.saturation != 0 || correction.hue != 0 || correction.texture != 0 || correction.clarity != 0 || correction.dehaze != 0 || correction.sharpness != 0 || correction.luminanceNoise != 0 || correction.moire != 0 || correction.defringe != 0 || correction.grain != 0 || correction.toningSaturation != 0 || correction.hasWhiteBalance || correction.colorGrading.active || correction.hasMainCurve || correction.hasChannelCurve[0] || correction.hasChannelCurve[1] || correction.hasChannelCurve[2])
}

func blendLocalNoisePixel(original, filtered color.NRGBA64, strength float64) color.NRGBA64 {
	originalLuma, blueDifference, redDifference := xmpLinearLumaChroma(original)
	filteredLuma, _, _ := xmpLinearLumaChroma(filtered)
	adjustedLuma := math.Max(0, math.Min(1, originalLuma+(filteredLuma-originalLuma)*strength))
	red := adjustedLuma + redDifference
	blue := adjustedLuma + blueDifference
	green := (adjustedLuma - 0.2126*red - 0.0722*blue) / 0.7152
	return color.NRGBA64{
		R: encodeLinearSRGB16(red), G: encodeLinearSRGB16(green), B: encodeLinearSRGB16(blue), A: original.A,
	}
}

func blendLocalMoirePixel(original, filtered color.NRGBA64, strength float64) color.NRGBA64 {
	luminance, originalU, originalV := xmpLinearLumaChroma(original)
	_, filteredU, filteredV := xmpLinearLumaChroma(filtered)
	blueDifference := originalU + (filteredU-originalU)*strength
	redDifference := originalV + (filteredV-originalV)*strength
	red := luminance + redDifference
	blue := luminance + blueDifference
	green := (luminance - 0.2126*red - 0.0722*blue) / 0.7152
	return color.NRGBA64{
		R: encodeLinearSRGB16(red), G: encodeLinearSRGB16(green), B: encodeLinearSRGB16(blue), A: original.A,
	}
}

func applyLocalDefringe(pixel color.NRGBA64, detail float32, strength float64) color.NRGBA64 {
	if strength == 0 {
		return pixel
	}
	red, green, blue := float64(pixel.R)/65535, float64(pixel.G)/65535, float64(pixel.B)/65535
	hue, saturation, _ := srgbToHSL(red, green, blue)
	if saturation < 0.1 {
		return pixel
	}
	purpleDistance := math.Abs(hue - 300)
	purpleDistance = math.Min(purpleDistance, 360-purpleDistance)
	purpleWeight := 1 - smoothstep((purpleDistance-25)/17)
	greenDistance := math.Abs(hue - 120)
	greenWeight := 1 - smoothstep((greenDistance-25)/15)
	hueWeight := math.Max(purpleWeight, greenWeight)
	if hueWeight <= 0 {
		return pixel
	}
	edgeStrength := math.Abs(float64(detail)) / 127
	edgeWeight := smoothstep((edgeStrength - 0.015) / 0.12)
	amount := math.Min(1, math.Abs(strength)*hueWeight*edgeWeight)
	if amount <= 0 {
		return pixel
	}
	if strength < 0 {
		amount = -amount
	}
	luminance, blueDifference, redDifference := xmpLinearLumaChroma(pixel)
	chromaScale := 1 - amount
	red = luminance + redDifference*chromaScale
	blue = luminance + blueDifference*chromaScale
	green = (luminance - 0.2126*red - 0.0722*blue) / 0.7152
	return color.NRGBA64{
		R: encodeLinearSRGB16(red), G: encodeLinearSRGB16(green), B: encodeLinearSRGB16(blue), A: pixel.A,
	}
}

func applyLocalHueShift(red, green, blue, slider float64) (float64, float64, float64) {
	// Camera Raw's local hue control is approximated as a rotation of up to
	// half the color wheel at the slider limits. Convert through encoded sRGB
	// because HSL hue is defined on gamma-encoded channel values.
	red16, green16, blue16 := encodeLinearSRGB16(red), encodeLinearSRGB16(green), encodeLinearSRGB16(blue)
	redSRGB, greenSRGB, blueSRGB := float64(red16)/65535, float64(green16)/65535, float64(blue16)/65535
	hue, saturation, luminance := srgbToHSL(redSRGB, greenSRGB, blueSRGB)
	if saturation == 0 {
		return red, green, blue
	}
	hue = math.Mod(hue+slider*1.8, 360)
	if hue < 0 {
		hue += 360
	}
	redSRGB, greenSRGB, blueSRGB = hslToSRGB(hue, saturation, luminance)
	return srgb16ToLinear[encodeSRGB16(redSRGB)],
		srgb16ToLinear[encodeSRGB16(greenSRGB)],
		srgb16ToLinear[encodeSRGB16(blueSRGB)]
}

func blendLocalCurve(input uint16, curve toneCurveTable, amount float64) uint16 {
	return uint16(math.Round(float64(input)*(1-amount) + float64(curve.sample(input))*amount))
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
	red16, green16, blue16 := encodeLinearSRGB16(red), encodeLinearSRGB16(green), encodeLinearSRGB16(blue)
	redSRGB, greenSRGB, blueSRGB := float64(red16)/65535, float64(green16)/65535, float64(blue16)/65535
	_, _, luminance := srgbToHSL(redSRGB, greenSRGB, blueSRGB)
	tintRed, tintGreen, tintBlue := hslToSRGB(hue, 1, luminance)
	return srgb16ToLinear[encodeSRGB16(redSRGB*(1-blend)+tintRed*blend)],
		srgb16ToLinear[encodeSRGB16(greenSRGB*(1-blend)+tintGreen*blend)],
		srgb16ToLinear[encodeSRGB16(blueSRGB*(1-blend)+tintBlue*blend)]
}
