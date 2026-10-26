package lrtl

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"strconv"
	"strings"

	"github.com/beevik/etree"
)

// xmpBasicAdjustments is the subset of global Camera Raw controls that can be
// applied meaningfully to an already-rendered sRGB image. It is deliberately
// not a replacement for Adobe Camera Raw's RAW pipeline.
type xmpBasicAdjustments struct {
	exposure             float64
	whiteBalance         [3]float64
	hasWhiteBalance      bool
	contrast             float64
	highlights           float64
	shadows              float64
	whites               float64
	blacks               float64
	texture              float64
	clarity              float64
	dehaze               float64
	sharpness            float64
	sharpenRadius        float64
	sharpenDetail        float64
	sharpenMasking       float64
	luminanceNoise       float64
	lumaNoiseDetail      float64
	lumaNoiseContrast    float64
	colorNoise           float64
	colorNoiseDetail     float64
	colorNoiseSmoothness float64
	vignetteAmount       float64
	vignetteMidpoint     float64
	vignetteRoundness    float64
	vignetteFeather      float64
	vignetteHighlights   float64
	grainAmount          float64
	grainSize            float64
	grainFrequency       float64
	parametric           xmpParametricCurve
	vibrance             float64
	saturation           float64
	masterCurve          toneCurveTable
	hasMaster            bool
	channelCurve         [3]toneCurveTable
	hasChannel           [3]bool
	colorMix             [8]xmpHSLColorAdjustment
	hasColorMix          bool
	grayscale            bool
	grayMix              [8]float64
	colorGrading         xmpColorGrading
	local                []xmpLocalCorrection
	geometry             xmpGeometry
}

type xmpParametricCurve struct {
	shadows, darks, lights, highlights        float64
	shadowSplit, midtoneSplit, highlightSplit float64
}

func (curve xmpParametricCurve) active() bool {
	return curve.shadows != 0 || curve.darks != 0 || curve.lights != 0 || curve.highlights != 0
}

type xmpHSLColorAdjustment struct {
	hue        float64
	saturation float64
	luminance  float64
}

type xmpColorGradeTone struct {
	hue, saturation, luminance float64
}

type xmpColorGrading struct {
	shadows, midtones, highlights, global xmpColorGradeTone
	blending, balance                     float64
	active                                bool
}

var xmpHSLBands = []struct {
	name             string
	legacyHue        string
	legacySaturation string
}{
	{name: "Red", legacyHue: "RedHue", legacySaturation: "RedSaturation"},
	{name: "Orange", legacyHue: "OrangeHue", legacySaturation: "OrangeSaturation"},
	{name: "Yellow", legacyHue: "YellowHue", legacySaturation: "YellowSaturation"},
	{name: "Green", legacyHue: "GreenHue", legacySaturation: "GreenSaturation"},
	{name: "Aqua", legacyHue: "AquaHue", legacySaturation: "AquaSaturation"},
	{name: "Blue", legacyHue: "BlueHue", legacySaturation: "BlueSaturation"},
	{name: "Purple", legacyHue: "PurpleHue", legacySaturation: "PurpleSaturation"},
	{name: "Magenta", legacyHue: "MagentaHue", legacySaturation: "MagentaSaturation"},
}

type toneCurvePoint struct {
	x float64
	y float64
}

func readXMPBasicAdjustments(frame Frame) (xmpBasicAdjustments, error) {
	packet, err := PacketForFrame(frame)
	if err != nil {
		return xmpBasicAdjustments{}, err
	}
	return readXMPBasicPacket(packet)
}

func readXMPBasicPacket(packet *XMPPacket) (xmpBasicAdjustments, error) {
	adjustments := xmpBasicAdjustments{parametric: xmpParametricCurve{shadowSplit: 25, midtoneSplit: 50, highlightSplit: 75}}
	if err := readXMPGrayscale(packet, &adjustments); err != nil {
		return xmpBasicAdjustments{}, err
	}
	adjustments.lumaNoiseDetail = 50
	adjustments.colorNoiseDetail = 50
	adjustments.colorNoiseSmoothness = 50
	adjustments.vignetteMidpoint = 50
	adjustments.vignetteFeather = 50
	adjustments.grainSize = 25
	adjustments.grainFrequency = 50
	adjustments.geometry.scale = 100
	properties := []struct {
		name   string
		target *float64
		min    float64
		max    float64
	}{
		{"Exposure2012", &adjustments.exposure, -maxRenderXMPExposureStops, maxRenderXMPExposureStops},
		{"PerspectiveRotate", &adjustments.geometry.rotate, -10, 10},
		{"PerspectiveScale", &adjustments.geometry.scale, 50, 150},
		{"PerspectiveVertical", &adjustments.geometry.vertical, -100, 100},
		{"PerspectiveHorizontal", &adjustments.geometry.horizontal, -100, 100},
		{"PerspectiveAspect", &adjustments.geometry.aspect, -100, 100},
		{"PerspectiveX", &adjustments.geometry.xOffset, -100, 100},
		{"PerspectiveY", &adjustments.geometry.yOffset, -100, 100},
		{"Contrast2012", &adjustments.contrast, -100, 100},
		{"Highlights2012", &adjustments.highlights, -100, 100},
		{"Shadows2012", &adjustments.shadows, -100, 100},
		{"Whites2012", &adjustments.whites, -100, 100},
		{"Blacks2012", &adjustments.blacks, -100, 100},
		{"Texture", &adjustments.texture, -100, 100},
		{"Clarity2012", &adjustments.clarity, -100, 100},
		{"Dehaze", &adjustments.dehaze, -100, 100},
		{"LuminanceSmoothing", &adjustments.luminanceNoise, 0, 100},
		{"LuminanceNoiseReductionDetail", &adjustments.lumaNoiseDetail, 0, 100},
		{"LuminanceNoiseReductionContrast", &adjustments.lumaNoiseContrast, 0, 100},
		{"ColorNoiseReduction", &adjustments.colorNoise, 0, 100},
		{"ColorNoiseReductionDetail", &adjustments.colorNoiseDetail, 0, 100},
		{"ColorNoiseReductionSmoothness", &adjustments.colorNoiseSmoothness, 0, 100},
		{"PostCropVignetteAmount", &adjustments.vignetteAmount, -100, 100},
		{"PostCropVignetteMidpoint", &adjustments.vignetteMidpoint, 0, 100},
		{"PostCropVignetteRoundness", &adjustments.vignetteRoundness, -100, 100},
		{"PostCropVignetteFeather", &adjustments.vignetteFeather, 0, 100},
		{"PostCropVignetteHighlightContrast", &adjustments.vignetteHighlights, 0, 100},
		{"GrainAmount", &adjustments.grainAmount, 0, 100},
		{"GrainSize", &adjustments.grainSize, 0, 100},
		{"GrainFrequency", &adjustments.grainFrequency, 0, 100},
		{"ParametricShadows", &adjustments.parametric.shadows, -100, 100},
		{"ParametricDarks", &adjustments.parametric.darks, -100, 100},
		{"ParametricLights", &adjustments.parametric.lights, -100, 100},
		{"ParametricHighlights", &adjustments.parametric.highlights, -100, 100},
		{"ParametricShadowSplit", &adjustments.parametric.shadowSplit, 10, 70},
		{"ParametricMidtoneSplit", &adjustments.parametric.midtoneSplit, 20, 80},
		{"ParametricHighlightSplit", &adjustments.parametric.highlightSplit, 30, 90},
		{"Vibrance", &adjustments.vibrance, -100, 100},
		{"Saturation", &adjustments.saturation, -100, 100},
	}
	for _, property := range properties {
		value, found, err := packet.GetRootScalar("crs:" + property.name)
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
	upright, err := readXMPStoredUpright(packet)
	if err != nil {
		return xmpBasicAdjustments{}, err
	}
	adjustments.geometry.upright = upright
	holyGrailCorrection, _, err := readHolyGrailRenderCorrection(packet)
	if err != nil {
		return xmpBasicAdjustments{}, err
	}
	adjustments.exposure += holyGrailCorrection
	deflickerCorrection, _, err := readDeflickerRenderCorrection(packet)
	if err != nil {
		return xmpBasicAdjustments{}, err
	}
	adjustments.exposure += deflickerCorrection
	if math.Abs(adjustments.exposure) > maxRenderXMPExposureStops {
		return xmpBasicAdjustments{}, fmt.Errorf("combined XMP exposure must be between -%d and +%d stops", maxRenderXMPExposureStops, maxRenderXMPExposureStops)
	}
	adjustments.sharpenRadius = 1
	adjustments.sharpenDetail = 25
	for _, property := range []struct {
		name   string
		target *float64
		min    float64
		max    float64
	}{
		{name: "Sharpness", target: &adjustments.sharpness, min: 0, max: 150},
		{name: "SharpenRadius", target: &adjustments.sharpenRadius, min: 0.5, max: 3},
		{name: "SharpenDetail", target: &adjustments.sharpenDetail, min: 0, max: 100},
		{name: "SharpenEdgeMasking", target: &adjustments.sharpenMasking, min: 0, max: 100},
	} {
		value, found, err := readXMPBasicNumber(packet, property.name, property.min, property.max)
		if err != nil {
			return xmpBasicAdjustments{}, err
		}
		if found {
			*property.target = value
		}
	}
	for index, band := range xmpHSLBands {
		for _, property := range []struct {
			current string
			legacy  string
			target  *float64
		}{
			{current: "HueAdjustment" + band.name, legacy: band.legacyHue, target: &adjustments.colorMix[index].hue},
			{current: "SaturationAdjustment" + band.name, legacy: band.legacySaturation, target: &adjustments.colorMix[index].saturation},
			{current: "LuminanceAdjustment" + band.name, target: &adjustments.colorMix[index].luminance},
		} {
			value, found, err := readXMPBasicNumber(packet, property.current, -100, 100)
			if err != nil {
				return xmpBasicAdjustments{}, err
			}
			if !found && property.legacy != "" {
				value, found, err = readXMPBasicNumber(packet, property.legacy, -100, 100)
				if err != nil {
					return xmpBasicAdjustments{}, err
				}
			}
			if found {
				*property.target = value
				adjustments.hasColorMix = adjustments.hasColorMix || value != 0
			}
		}
	}
	temperatureName, tintName := "Temperature", "Tint"
	temperatureMin, temperatureMax, tintLimit := 2000.0, 50000.0, 150.0
	quality, _, err := packet.GetRootScalar("lrt:Quality")
	if err != nil {
		return xmpBasicAdjustments{}, err
	}
	relativeWB := quality == "FLAT"
	if relativeWB {
		temperatureName, tintName = "IncrementalTemperature", "IncrementalTint"
		temperatureMin, temperatureMax, tintLimit = -100, 100, 100
	}
	temperature, hasTemperature, err := readXMPBasicNumber(packet, temperatureName, temperatureMin, temperatureMax)
	if err != nil {
		return xmpBasicAdjustments{}, err
	}
	tint, hasTint, err := readXMPBasicNumber(packet, tintName, -tintLimit, tintLimit)
	if err != nil {
		return xmpBasicAdjustments{}, err
	}
	if hasTemperature || hasTint {
		if relativeWB {
			// Independent sRGB approximation, using the existing local-WB
			// convention. This is not Adobe's incremental-WB transfer curve.
			if temperature == 0 && tint == 0 {
				hasTemperature, hasTint = false, false
			}
			temperature = 6500 + temperature*40
		} else if !hasTemperature {
			temperature = 6500
		}
		adjustments.whiteBalance = approximateWhiteBalanceGains(temperature, tint)
		adjustments.hasWhiteBalance = hasTemperature || hasTint
	}
	adjustments.colorGrading, err = readXMPColorGrading(func(name string, min, max float64) (float64, bool, error) {
		return readXMPBasicNumber(packet, name, min, max)
	}, "ColorGrade")
	if err != nil {
		return xmpBasicAdjustments{}, err
	}
	curveProperties := []struct {
		primary  string
		fallback string
		channel  int
	}{
		{primary: "ToneCurvePV2012"},
		{primary: "ToneCurvePV2012Red", fallback: "ToneCurveRed", channel: 0},
		{primary: "ToneCurvePV2012Green", fallback: "ToneCurveGreen", channel: 1},
		{primary: "ToneCurvePV2012Blue", fallback: "ToneCurveBlue", channel: 2},
	}
	for _, property := range curveProperties {
		curve, found, err := readXMPToneCurve(packet, property.primary)
		if err != nil {
			return xmpBasicAdjustments{}, err
		}
		if !found && property.fallback != "" {
			curve, found, err = readXMPToneCurve(packet, property.fallback)
			if err != nil {
				return xmpBasicAdjustments{}, err
			}
		}
		if property.channel == 0 && property.primary == "ToneCurvePV2012" {
			adjustments.masterCurve, adjustments.hasMaster = curve, found
		} else {
			adjustments.channelCurve[property.channel] = curve
			adjustments.hasChannel[property.channel] = found
		}
	}
	if !adjustments.hasMaster {
		adjustments.masterCurve, adjustments.hasMaster, err = readXMPToneCurve(packet, "ToneCurve")
		if err != nil {
			return xmpBasicAdjustments{}, err
		}
	}
	adjustments.local, err = readXMPLocalCorrections(packet)
	if err != nil {
		return xmpBasicAdjustments{}, err
	}
	return adjustments, nil
}

// Lightroom writes the result of an Upright solve as one row-major 3x3
// homography per mode. Replaying that stored result is deterministic; solving
// a new Upright transform from pixels is outside this renderer's scope.
func readXMPStoredUpright(packet *XMPPacket) (*xmpHomography, error) {
	modeText, found, err := packet.GetRootScalar("crs:PerspectiveUpright")
	if err != nil || !found {
		return nil, err
	}
	modeValue, valid := scalarNumber(modeText)
	if !valid || modeValue < 0 || modeValue > 5 || math.Trunc(modeValue) != modeValue {
		return nil, fmt.Errorf("crs:PerspectiveUpright must be an integer between 0 and 5")
	}
	mode := int(modeValue)
	if mode == 0 {
		return nil, nil
	}

	countText, hasCount, err := packet.GetRootScalar("crs:UprightTransformCount")
	if err != nil {
		return nil, err
	}
	if hasCount {
		count, parseErr := strconv.Atoi(strings.TrimSpace(countText))
		if parseErr != nil || count < mode+1 || count > 6 {
			return nil, fmt.Errorf("crs:UprightTransformCount must include selected transform %d and be at most 6", mode)
		}
	}
	property := fmt.Sprintf("crs:UprightTransform_%d", mode)
	value, found, err := packet.GetRootScalar(property)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("%s is required when crs:PerspectiveUpright=%d; generating Upright transforms is unsupported", property, mode)
	}
	fields := strings.Split(value, ",")
	if len(fields) != 9 {
		return nil, fmt.Errorf("%s must contain exactly 9 comma-separated numbers", property)
	}
	var transform xmpHomography
	maxAbs := 0.0
	for index, field := range fields {
		number, valid := scalarNumber(field)
		if !valid {
			return nil, fmt.Errorf("%s contains an invalid number at index %d", property, index)
		}
		transform[index] = number
		maxAbs = math.Max(maxAbs, math.Abs(number))
	}
	if maxAbs == 0 {
		return nil, fmt.Errorf("%s must be an invertible homography", property)
	}
	// Homographies are invariant to a shared scalar; normalization keeps
	// determinant and projective-coordinate checks stable for equivalent input.
	for index := range transform {
		transform[index] /= maxAbs
	}
	if _, invertible := transform.inverse(); !invertible {
		return nil, fmt.Errorf("%s must be a finite, invertible homography", property)
	}
	return &transform, nil
}

func readXMPBasicNumber(packet *XMPPacket, name string, min, max float64) (float64, bool, error) {
	value, found, err := packet.GetRootScalar("crs:" + name)
	if err != nil || !found {
		return 0, found, err
	}
	number, valid := scalarNumber(value)
	if !valid || number < min || number > max {
		return 0, false, fmt.Errorf("crs:%s must be a finite number between %g and %g", name, min, max)
	}
	return number, true, nil
}

func readXMPColorGrading(read func(string, float64, float64) (float64, bool, error), prefix string) (xmpColorGrading, error) {
	grading := xmpColorGrading{blending: 50}
	for _, zone := range []struct {
		name   string
		target *xmpColorGradeTone
	}{
		{name: "Shadow", target: &grading.shadows},
		{name: "Midtone", target: &grading.midtones},
		{name: "Highlight", target: &grading.highlights},
		{name: "Global", target: &grading.global},
	} {
		for _, control := range []struct {
			name   string
			target *float64
			min    float64
			max    float64
		}{
			{name: "Hue", target: &zone.target.hue, min: 0, max: 360},
			{name: "Sat", target: &zone.target.saturation, min: 0, max: 100},
			{name: "Lum", target: &zone.target.luminance, min: -100, max: 100},
		} {
			value, found, err := read(prefix+zone.name+control.name, control.min, control.max)
			if err != nil {
				return xmpColorGrading{}, err
			}
			if found {
				*control.target = value
			}
		}
		grading.active = grading.active || zone.target.saturation != 0 || zone.target.luminance != 0
	}
	if value, found, err := read(prefix+"Blending", 0, 100); err != nil {
		return xmpColorGrading{}, err
	} else if found {
		grading.blending = value
	}
	if value, _, err := read(prefix+"Balance", -100, 100); err != nil {
		return xmpColorGrading{}, err
	} else {
		grading.balance = value
	}
	return grading, nil
}

func readXMPLocalColorGrading(element *etree.Element) (xmpColorGrading, error) {
	return readXMPColorGrading(func(name string, min, max float64) (float64, bool, error) {
		return xmpLocalNumber(element, name, 0, min, max)
	}, "LocalColorGrade")
}

// approximateWhiteBalanceGains estimates a Camera Raw white-balance change on
// an already-rendered sRGB image. Camera Raw's RAW pipeline needs sensor and
// camera-profile data unavailable here, so this uses a Planckian chromaticity estimate
// relative to neutral 6500 K and an independent green-magenta tint adjustment.
func approximateWhiteBalanceGains(temperature, tint float64) [3]float64 {
	reference := approximateTemperatureRGB(6500)
	target := approximateTemperatureRGB(temperature)
	magenta := math.Exp2(tint / 150 * 0.15)
	gains := [3]float64{
		reference[0] / target[0] * magenta,
		reference[1] / target[1] / magenta,
		reference[2] / target[2] * magenta,
	}
	luminance := 0.2126*gains[0] + 0.7152*gains[1] + 0.0722*gains[2]
	if luminance > 0 {
		for channel := range gains {
			gains[channel] /= luminance
		}
	}
	return gains
}

func approximateTemperatureRGB(temperature float64) [3]float64 {
	// Convert an approximate CIE 1931 Planckian-locus xy white point to
	// linear sRGB. The locus polynomial is extended above 25,000 K so that
	// common Camera Raw metadata values remain usable, though less accurately.
	var x float64
	if temperature <= 4000 {
		x = -0.2661239e9/math.Pow(temperature, 3) - 0.2343580e6/math.Pow(temperature, 2) + 0.8776956e3/temperature + 0.179910
	} else {
		x = -3.0258469e9/math.Pow(temperature, 3) + 2.1070379e6/math.Pow(temperature, 2) + 0.2226347e3/temperature + 0.240390
	}
	var y float64
	switch {
	case temperature <= 2222:
		y = -1.1063814*math.Pow(x, 3) - 1.34811020*math.Pow(x, 2) + 2.18555832*x - 0.20219683
	case temperature <= 4000:
		y = -0.9549476*math.Pow(x, 3) - 1.37418593*math.Pow(x, 2) + 2.09137015*x - 0.16748867
	default:
		y = 3.0817580*math.Pow(x, 3) - 5.87338670*math.Pow(x, 2) + 3.75112997*x - 0.37001483
	}
	xyzX, xyzZ := x/y, (1-x-y)/y
	return [3]float64{
		3.2404542*xyzX - 1.5371385 - 0.4985314*xyzZ,
		-0.9692660*xyzX + 1.8760108 + 0.0415560*xyzZ,
		0.0556434*xyzX - 0.2040259 + 1.0572252*xyzZ,
	}
}

func readXMPToneCurve(packet *XMPPacket, name string) (toneCurveTable, bool, error) {
	var empty toneCurveTable
	uri, local, err := expandedName("crs:" + name)
	if err != nil {
		return empty, false, err
	}
	property := packet.findRootProperty(uri, local)
	if property.attribute == nil && property.element == nil {
		return empty, false, nil
	}
	return readXMPToneCurveProperty(name, property.element, property.attribute)
}

func readXMPToneCurvePoints(packet *XMPPacket, name string) ([]toneCurvePoint, bool, bool, error) {
	uri, local, err := expandedName("crs:" + name)
	if err != nil {
		return nil, false, false, err
	}
	property := packet.findRootProperty(uri, local)
	if property.attribute == nil && property.element == nil {
		return nil, false, true, nil
	}
	points, err := parseXMPToneCurveProperty(name, property.element, property.attribute)
	if err != nil {
		return nil, false, false, err
	}
	editable := property.attribute != nil
	if property.element != nil {
		_, editable = scalarElementText(property.element)
		if !editable {
			editable = toneCurveSequenceElement(property.element) != nil
		}
	}
	return points, true, editable, nil
}

func setXMPToneCurvePoints(packet *XMPPacket, name string, points []toneCurvePoint) error {
	uri, local, err := expandedName("crs:" + name)
	if err != nil {
		return err
	}
	property := packet.findRootProperty(uri, local)
	if property.attribute == nil && property.element == nil {
		var encoded strings.Builder
		for i, point := range points {
			if i > 0 {
				encoded.WriteByte(' ')
			}
			fmt.Fprintf(&encoded, "%d, %d", int(point.x), int(point.y))
		}
		return packet.SetScalar("crs:"+name, encoded.String())
	}
	return setExistingToneCurvePoints(property, name, points)
}

func setExistingToneCurvePoints(property topLevelXMPProperty, name string, points []toneCurvePoint) error {
	var encoded strings.Builder
	for index, point := range points {
		if index > 0 {
			encoded.WriteByte(' ')
		}
		fmt.Fprintf(&encoded, "%d, %d", int(point.x), int(point.y))
	}
	if property.attribute != nil {
		property.attribute.Value = encoded.String()
		return nil
	}
	if property.element != nil {
		if _, scalar := scalarElementText(property.element); scalar {
			property.element.SetText(encoded.String())
			return nil
		}
		sequence := toneCurveSequenceElement(property.element)
		if sequence == nil {
			return xmpError("cannot replace unsupported structured property crs:%s", name)
		}
		sequence.Child = nil
		prefix := ensurePrefix(sequence, "rdf", rdfNamespace)
		for _, point := range points {
			item := sequence.CreateElement(prefix + ":li")
			item.SetText(fmt.Sprintf("%d, %d", int(point.x), int(point.y)))
		}
		return nil
	}
	return xmpError("missing curve property crs:%s", name)
}

func toneCurveSequenceElement(property *etree.Element) *etree.Element {
	var sequence *etree.Element
	for _, token := range property.Child {
		child, ok := token.(*etree.Element)
		if !ok {
			if text, isText := token.(*etree.CharData); isText {
				if strings.TrimSpace(text.Data) == "" {
					continue
				}
			}
			return nil
		}
		if child.NamespaceURI() != rdfNamespace || child.Tag != "Seq" || sequence != nil {
			return nil
		}
		sequence = child
	}
	if sequence == nil {
		return nil
	}
	for _, token := range sequence.Child {
		child, ok := token.(*etree.Element)
		if !ok {
			if text, isText := token.(*etree.CharData); isText {
				if strings.TrimSpace(text.Data) == "" {
					continue
				}
			}
			return nil
		}
		if child.NamespaceURI() != rdfNamespace || child.Tag != "li" {
			return nil
		}
		if _, scalar := scalarElementText(child); !scalar {
			return nil
		}
	}
	return sequence
}

func readXMPLocalToneCurve(correction *etree.Element, name string) (toneCurveTable, bool, error) {
	var property *etree.Element
	var attribute *etree.Attr
	for index := range correction.Attr {
		if attributeMatches(&correction.Attr[index], crsNamespace, name) {
			attribute = &correction.Attr[index]
			break
		}
	}
	if attribute == nil {
		for _, token := range correction.Child {
			child, ok := token.(*etree.Element)
			if ok && child.NamespaceURI() == crsNamespace && child.Tag == name {
				property = child
				break
			}
		}
	}
	if property == nil && attribute == nil {
		return toneCurveTable{}, false, nil
	}
	return readXMPToneCurveProperty(name, property, attribute)
}

func hasLocalToneCurveProperty(correction *etree.Element, name string) bool {
	for index := range correction.Attr {
		if attributeMatches(&correction.Attr[index], crsNamespace, name) {
			return true
		}
	}
	for _, token := range correction.Child {
		child, ok := token.(*etree.Element)
		if ok && child.NamespaceURI() == crsNamespace && child.Tag == name {
			return true
		}
	}
	return false
}

func readXMPToneCurveProperty(name string, property *etree.Element, attribute *etree.Attr) (toneCurveTable, bool, error) {
	var empty toneCurveTable
	points, err := parseXMPToneCurveProperty(name, property, attribute)
	if err != nil {
		return empty, false, err
	}
	return makeToneCurveTable(points), true, nil
}

func parseXMPToneCurveProperty(name string, property *etree.Element, attribute *etree.Attr) ([]toneCurvePoint, error) {
	values := make([]string, 0, 32)
	if attribute != nil {
		values = append(values, attribute.Value)
	} else {
		var collect func(*etree.Element)
		collect = func(element *etree.Element) {
			if element.NamespaceURI() == rdfNamespace && element.Tag == "li" {
				if text, scalar := scalarElementText(element); scalar {
					values = append(values, text)
					return
				}
			}
			for _, token := range element.Child {
				if child, ok := token.(*etree.Element); ok {
					collect(child)
				}
			}
		}
		if text, scalar := scalarElementText(property); scalar {
			values = append(values, text)
		} else {
			collect(property)
		}
	}
	points, err := parseToneCurveValues(values)
	if err != nil {
		return nil, fmt.Errorf("crs:%s: %w", name, err)
	}
	return points, nil
}

func parseToneCurveValues(values []string) ([]toneCurvePoint, error) {
	coordinates := make([]int, 0, len(values)*2)
	for _, value := range values {
		for _, field := range strings.FieldsFunc(value, func(r rune) bool {
			return r == ',' || r == ';' || r == ' ' || r == '\t' || r == '\r' || r == '\n'
		}) {
			number, err := strconv.Atoi(field)
			if err != nil || number < 0 || number > 255 {
				return nil, fmt.Errorf("expected ordered integer x/y points in the 0..255 range")
			}
			coordinates = append(coordinates, number)
		}
	}
	if len(coordinates) < 4 || len(coordinates)%2 != 0 {
		return nil, fmt.Errorf("expected at least two ordered integer x/y points")
	}
	points := make([]toneCurvePoint, 0, len(coordinates)/2)
	for index := 0; index < len(coordinates); index += 2 {
		point := toneCurvePoint{x: float64(coordinates[index]), y: float64(coordinates[index+1])}
		if len(points) > 0 && point.x <= points[len(points)-1].x {
			return nil, fmt.Errorf("input coordinates must be strictly increasing")
		}
		points = append(points, point)
	}
	return points, nil
}

func toneCurveSlope(leftWidth, rightWidth, leftDelta, rightDelta float64) float64 {
	if leftDelta == 0 || rightDelta == 0 || math.Signbit(leftDelta) != math.Signbit(rightDelta) {
		return 0
	}
	leftWeight := 2*rightWidth + leftWidth
	rightWeight := rightWidth + 2*leftWidth
	return (leftWeight + rightWeight) / (leftWeight/leftDelta + rightWeight/rightDelta)
}

func toneCurveEndpointSlope(firstWidth, secondWidth, firstDelta, secondDelta float64) float64 {
	slope := ((2*firstWidth+secondWidth)*firstDelta - firstWidth*secondDelta) / (firstWidth + secondWidth)
	if math.Signbit(slope) != math.Signbit(firstDelta) {
		return 0
	}
	if math.Signbit(firstDelta) != math.Signbit(secondDelta) && math.Abs(slope) > 3*math.Abs(firstDelta) {
		return 3 * firstDelta
	}
	return slope
}

func makeToneCurveTable(points []toneCurvePoint) toneCurveTable {
	var table toneCurveTable
	widths := make([]float64, len(points)-1)
	deltas := make([]float64, len(points)-1)
	for index := range widths {
		widths[index] = points[index+1].x - points[index].x
		deltas[index] = (points[index+1].y - points[index].y) / widths[index]
	}
	slopes := make([]float64, len(points))
	if len(points) == 2 {
		slopes[0], slopes[1] = deltas[0], deltas[0]
	} else {
		slopes[0] = toneCurveEndpointSlope(widths[0], widths[1], deltas[0], deltas[1])
		last := len(points) - 1
		slopes[last] = toneCurveEndpointSlope(widths[last-1], widths[last-2], deltas[last-1], deltas[last-2])
		for index := 1; index < last; index++ {
			slopes[index] = toneCurveSlope(widths[index-1], widths[index], deltas[index-1], deltas[index])
		}
	}
	table.first, table.last = points[0], points[len(points)-1]
	table.segments = make([]toneCurveSegment, len(widths))
	for i, width := range widths {
		y0, y1 := points[i].y, points[i+1].y
		m0, m1 := width*slopes[i], width*slopes[i+1]
		table.segments[i] = toneCurveSegment{start: points[i].x, width: width, a: 2*y0 - 2*y1 + m0 + m1, b: -3*y0 + 3*y1 - 2*m0 - m1, c: m0, d: y0}
	}

	return table
}

func (adjustments xmpBasicAdjustments) globalActive() bool {
	return adjustments.exposure != 0 || adjustments.hasWhiteBalance || adjustments.contrast != 0 || adjustments.highlights != 0 ||
		adjustments.shadows != 0 || adjustments.whites != 0 || adjustments.blacks != 0 ||
		adjustments.texture != 0 || adjustments.clarity != 0 || adjustments.dehaze != 0 || adjustments.sharpness != 0 || adjustments.parametric.active() || adjustments.vibrance != 0 || adjustments.saturation != 0 || adjustments.hasMaster ||
		adjustments.luminanceNoise != 0 || adjustments.colorNoise != 0 || adjustments.vignetteAmount != 0 ||
		adjustments.grainAmount != 0 ||
		adjustments.hasChannel[0] || adjustments.hasChannel[1] || adjustments.hasChannel[2] || adjustments.hasColorMix || adjustments.colorGrading.active || adjustments.grayscale
}

func (adjustments xmpBasicAdjustments) active() bool {
	return adjustments.globalActive() || len(adjustments.local) > 0 || adjustments.geometry.active()
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
	if adjustments.parametric.active() {
		luminance = applyParametricToneCurve(luminance, adjustments.parametric)
	}
	return math.Max(0, math.Min(1, luminance))
}

func applyParametricToneCurve(luminance float64, curve xmpParametricCurve) float64 {
	shadowSplit := math.Max(0.10, math.Min(0.70, curve.shadowSplit/100))
	midtoneSplit := math.Max(shadowSplit+0.05, math.Min(0.80, curve.midtoneSplit/100))
	highlightSplit := math.Max(midtoneSplit+0.05, math.Min(0.90, curve.highlightSplit/100))
	highlightPeak := (highlightSplit + 1) / 2
	luminance = math.Max(0, math.Min(1, luminance))

	shadowWeight, darkWeight, lightWeight, highlightWeight := 0.0, 0.0, 0.0, 0.0
	switch {
	case luminance < shadowSplit:
		shadowWeight = smoothstep(luminance / shadowSplit)
	case luminance < midtoneSplit:
		t := smoothstep((luminance - shadowSplit) / (midtoneSplit - shadowSplit))
		shadowWeight, darkWeight = 1-t, t
	case luminance < highlightSplit:
		t := smoothstep((luminance - midtoneSplit) / (highlightSplit - midtoneSplit))
		darkWeight, lightWeight = 1-t, t
	case luminance < highlightPeak:
		t := smoothstep((luminance - highlightSplit) / (highlightPeak - highlightSplit))
		lightWeight, highlightWeight = 1-t, t
	default:
		highlightWeight = 1 - smoothstep((luminance-highlightPeak)/(1-highlightPeak))
	}

	delta := 0.15 * (curve.shadows/100*shadowWeight + curve.darks/100*darkWeight +
		curve.lights/100*lightWeight + curve.highlights/100*highlightWeight)
	return math.Max(0, math.Min(1, luminance+delta))
}
func encodeLinearSRGB16(value float64) uint16 {
	value = math.Max(0, math.Min(1, value))
	encoded := 12.92 * value
	if value > 0.0031308 {
		encoded = 1.055*math.Pow(value, 1.0/2.4) - 0.055
	}
	return uint16(math.Round(encoded * 65535))
}

func applyXMPBasic(img image.Image, adjustments xmpBasicAdjustments) *image.NRGBA64 {
	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	result := image.NewNRGBA64(bounds)
	exposure := math.Exp2(adjustments.exposure)
	saturation := 1 + adjustments.saturation/100
	dehazeSaturation := 1 + adjustments.dehaze/100*0.15
	var textureDetail, clarityDetail, dehazeDetail []float32
	if adjustments.dehaze != 0 {
		dehazeDetail = xmpLuminanceDetail(img, xmpDehazeRadius(width, height))
	}
	if adjustments.texture != 0 {
		textureDetail = xmpLuminanceDetail(img, xmpTextureRadius(width, height))
	}
	if adjustments.clarity != 0 {
		clarityDetail = xmpLuminanceDetail(img, xmpClarityRadius(width, height))
	}
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			pixel := color.NRGBA64Model.Convert(img.At(x, y)).(color.NRGBA64)
			redGain, greenGain, blueGain := 1.0, 1.0, 1.0
			if adjustments.hasWhiteBalance {
				redGain, greenGain, blueGain = adjustments.whiteBalance[0], adjustments.whiteBalance[1], adjustments.whiteBalance[2]
			}
			red := srgb16ToLinear[pixel.R] * redGain * exposure
			green := srgb16ToLinear[pixel.G] * greenGain * exposure
			blue := srgb16ToLinear[pixel.B] * blueGain * exposure
			luminance := 0.2126*red + 0.7152*green + 0.0722*blue
			if adjustments.grayscale {
				luminance = xmpGrayscaleLuminance(red, green, blue, adjustments.grayMix)
				red, green, blue = luminance, luminance, luminance
			}
			adjustedLuminance := basicAdjustedLuminance(luminance, adjustments)
			if adjustments.dehaze != 0 {
				index := (y-bounds.Min.Y)*width + (x - bounds.Min.X)
				adjustedLuminance = xmpApplyDehaze(adjustedLuminance, adjustments.dehaze, dehazeDetail[index])
			}
			if adjustments.texture != 0 || adjustments.clarity != 0 {
				index := (y-bounds.Min.Y)*width + (x - bounds.Min.X)
				var fineDetail, broadDetail float32
				if textureDetail != nil {
					fineDetail = textureDetail[index]
				}
				if clarityDetail != nil {
					broadDetail = clarityDetail[index]
				}
				adjustedLuminance = xmpApplyDetail(adjustedLuminance, adjustments.texture, adjustments.clarity, fineDetail, broadDetail)
			}
			if luminance > 1e-12 {
				scale := adjustedLuminance / luminance
				red, green, blue = red*scale, green*scale, blue*scale
			} else {
				red, green, blue = adjustedLuminance, adjustedLuminance, adjustedLuminance
			}
			if !adjustments.grayscale && (adjustments.vibrance != 0 || adjustments.saturation != 0 || adjustments.dehaze != 0) {
				chroma := math.Max(red, math.Max(green, blue)) - math.Min(red, math.Min(green, blue))
				vibrance := 1 + adjustments.vibrance/100*(1-math.Min(1, chroma))
				scale := saturation * vibrance * dehazeSaturation
				red = adjustedLuminance + (red-adjustedLuminance)*scale
				green = adjustedLuminance + (green-adjustedLuminance)*scale
				blue = adjustedLuminance + (blue-adjustedLuminance)*scale
			}
			red16, green16, blue16 := encodeLinearSRGB16(red), encodeLinearSRGB16(green), encodeLinearSRGB16(blue)
			if adjustments.hasMaster {
				red16 = adjustments.masterCurve.sample(red16)
				green16 = adjustments.masterCurve.sample(green16)
				blue16 = adjustments.masterCurve.sample(blue16)
			}
			if adjustments.hasChannel[0] {
				red16 = adjustments.channelCurve[0].sample(red16)
			}
			if adjustments.hasChannel[1] {
				green16 = adjustments.channelCurve[1].sample(green16)
			}
			if adjustments.hasChannel[2] {
				blue16 = adjustments.channelCurve[2].sample(blue16)
			}
			if adjustments.hasColorMix && !adjustments.grayscale {
				red16, green16, blue16 = applyXMPHSLColorMix(red16, green16, blue16, adjustments.colorMix)
			}
			if adjustments.colorGrading.active {
				red16, green16, blue16 = applyXMPColorGrading(red16, green16, blue16, adjustments.colorGrading, 1)
			}
			result.SetNRGBA64(x, y, color.NRGBA64{
				R: red16, G: green16, B: blue16, A: pixel.A,
			})
		}
	}
	if adjustments.luminanceNoise != 0 || adjustments.colorNoise != 0 {
		result = xmpApplyNoiseReduction(result,
			adjustments.luminanceNoise, adjustments.lumaNoiseDetail, adjustments.lumaNoiseContrast,
			adjustments.colorNoise, adjustments.colorNoiseDetail, adjustments.colorNoiseSmoothness)
	}
	if adjustments.sharpness != 0 {
		result = xmpApplyImageSharpness(result, adjustments.sharpness, adjustments.sharpenRadius, adjustments.sharpenDetail, adjustments.sharpenMasking)
	}
	if adjustments.vignetteAmount != 0 {
		result = xmpApplyPostCropVignette(result,
			adjustments.vignetteAmount, adjustments.vignetteMidpoint, adjustments.vignetteRoundness,
			adjustments.vignetteFeather, adjustments.vignetteHighlights)
	}
	if adjustments.grainAmount != 0 {
		result = xmpApplyGrain(result, adjustments.grainAmount, adjustments.grainSize, adjustments.grainFrequency)
	}
	return result
}

func applyXMPColorGrading(red16, green16, blue16 uint16, grading xmpColorGrading, amount float64) (uint16, uint16, uint16) {
	amount = math.Max(0, math.Min(1, amount))
	if !grading.active || amount == 0 {
		return red16, green16, blue16
	}
	red, green, blue := srgb16ToLinear[red16], srgb16ToLinear[green16], srgb16ToLinear[blue16]
	luminance := 0.2126*red + 0.7152*green + 0.0722*blue
	weights := colorGradeWeights(luminance, grading.balance, grading.blending)
	for index, tone := range []xmpColorGradeTone{grading.shadows, grading.midtones, grading.highlights} {
		weight := weights[index] * amount
		red, green, blue = applyLocalToningColor(red, green, blue, tone.hue, tone.saturation, weight)
		red, green, blue = applyColorGradeLuminance(red, green, blue, tone.luminance, weight)
	}
	red, green, blue = applyLocalToningColor(red, green, blue, grading.global.hue, grading.global.saturation, amount)
	red, green, blue = applyColorGradeLuminance(red, green, blue, grading.global.luminance, amount)
	return encodeLinearSRGB16(red), encodeLinearSRGB16(green), encodeLinearSRGB16(blue)
}

func colorGradeWeights(luminance, balance, blending float64) [3]float64 {
	// Adobe documents how blending and balance affect tonal overlap, but not
	// the exact transfer curves; use normalized Gaussian zones as an sRGB approximation.
	effective := math.Max(0, math.Min(1, luminance+balance/100*0.2))
	spread := 0.12 + math.Max(0, math.Min(100, blending))/100*0.22
	weights := [3]float64{}
	total := 0.0
	for index, center := range [3]float64{0.15, 0.5, 0.85} {
		distance := (effective - center) / spread
		weights[index] = math.Exp(-0.5 * distance * distance)
		total += weights[index]
	}
	if total > 0 {
		for index := range weights {
			weights[index] /= total
		}
	}
	return weights
}

func applyColorGradeLuminance(red, green, blue, adjustment, amount float64) (float64, float64, float64) {
	if adjustment == 0 || amount == 0 {
		return red, green, blue
	}
	luminance := 0.2126*red + 0.7152*green + 0.0722*blue
	adjusted := math.Max(0, math.Min(1, luminance+adjustment/100*0.25*amount))
	if luminance <= 1e-12 {
		return adjusted, adjusted, adjusted
	}
	scale := adjusted / luminance
	return red * scale, green * scale, blue * scale
}

var xmpHSLHueCenters = [8]float64{0, 30, 60, 120, 180, 240, 270, 315}

func applyXMPHSLColorMix(red16, green16, blue16 uint16, adjustments [8]xmpHSLColorAdjustment) (uint16, uint16, uint16) {
	hue, saturation, luminance := srgbToHSL(float64(red16)/65535, float64(green16)/65535, float64(blue16)/65535)
	colorAdjustment := interpolateHSLBandAdjustments(adjustments, hue)

	// Camera Raw applies these controls in its RAW color pipeline. Here the
	// slider values are mapped to bounded sRGB-domain approximations.
	hue = math.Mod(hue+colorAdjustment.hue*0.3+360, 360)
	saturation *= math.Max(0, math.Min(2, 1+colorAdjustment.saturation/100))
	luminance += colorAdjustment.luminance / 100 * 0.25
	red, green, blue := hslToSRGB(hue, math.Max(0, math.Min(1, saturation)), math.Max(0, math.Min(1, luminance)))
	return encodeSRGB16(red), encodeSRGB16(green), encodeSRGB16(blue)
}

func encodeSRGB16(value float64) uint16 {
	return uint16(math.Round(math.Max(0, math.Min(1, value)) * 65535))
}

func interpolateHSLBandAdjustments(adjustments [8]xmpHSLColorAdjustment, hue float64) xmpHSLColorAdjustment {
	hue = math.Mod(hue+360, 360)
	for index, start := range xmpHSLHueCenters {
		next := (index + 1) % len(xmpHSLHueCenters)
		end := xmpHSLHueCenters[next]
		wrappedHue := hue
		if next == 0 {
			end += 360
			if wrappedHue < start {
				wrappedHue += 360
			}
		}
		if wrappedHue < start || wrappedHue > end {
			continue
		}
		portion := (wrappedHue - start) / (end - start)
		return xmpHSLColorAdjustment{
			hue:        adjustments[index].hue*(1-portion) + adjustments[next].hue*portion,
			saturation: adjustments[index].saturation*(1-portion) + adjustments[next].saturation*portion,
			luminance:  adjustments[index].luminance*(1-portion) + adjustments[next].luminance*portion,
		}
	}
	return adjustments[0]
}

func srgbToHSL(red, green, blue float64) (hue, saturation, luminance float64) {
	minimum := math.Min(red, math.Min(green, blue))
	maximum := math.Max(red, math.Max(green, blue))
	delta := maximum - minimum
	luminance = (maximum + minimum) / 2
	if delta == 0 {
		return 0, 0, luminance
	}
	if luminance <= 0.5 {
		saturation = delta / (maximum + minimum)
	} else {
		saturation = delta / (2 - maximum - minimum)
	}
	switch maximum {
	case red:
		hue = (green - blue) / delta
		if green < blue {
			hue += 6
		}
	case green:
		hue = (blue-red)/delta + 2
	case blue:
		hue = (red-green)/delta + 4
	}
	hue *= 60
	return hue, saturation, luminance
}

func hslToSRGB(hue, saturation, luminance float64) (float64, float64, float64) {
	chroma := (1 - math.Abs(2*luminance-1)) * saturation
	segment := hue / 60
	secondary := chroma * (1 - math.Abs(math.Mod(segment, 2)-1))
	var red, green, blue float64
	switch {
	case segment < 1:
		red, green = chroma, secondary
	case segment < 2:
		red, green = secondary, chroma
	case segment < 3:
		green, blue = chroma, secondary
	case segment < 4:
		green, blue = secondary, chroma
	case segment < 5:
		red, blue = secondary, chroma
	default:
		red, blue = chroma, secondary
	}
	minimum := luminance - chroma/2
	return red + minimum, green + minimum, blue + minimum
}
