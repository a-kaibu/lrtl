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
	exposure        float64
	whiteBalance    [3]float64
	hasWhiteBalance bool
	contrast        float64
	highlights      float64
	shadows         float64
	whites          float64
	blacks          float64
	vibrance        float64
	saturation      float64
	masterCurve     [256]uint8
	hasMaster       bool
	channelCurve    [3][256]uint8
	hasChannel      [3]bool
	colorMix        [8]xmpHSLColorAdjustment
	hasColorMix     bool
	local           []xmpLocalCorrection
}

type xmpHSLColorAdjustment struct {
	hue        float64
	saturation float64
	luminance  float64
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
	temperature, hasTemperature, err := readXMPBasicNumber(packet, "Temperature", 2000, 50000)
	if err != nil {
		return xmpBasicAdjustments{}, err
	}
	tint, hasTint, err := readXMPBasicNumber(packet, "Tint", -150, 150)
	if err != nil {
		return xmpBasicAdjustments{}, err
	}
	if hasTemperature || hasTint {
		if !hasTemperature {
			temperature = 6500
		}
		adjustments.whiteBalance = approximateWhiteBalanceGains(temperature, tint)
		adjustments.hasWhiteBalance = true
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

func readXMPBasicNumber(packet *XMPPacket, name string, min, max float64) (float64, bool, error) {
	value, found, err := packet.GetScalar("crs:" + name)
	if err != nil || !found {
		return 0, found, err
	}
	number, valid := scalarNumber(value)
	if !valid || number < min || number > max {
		return 0, false, fmt.Errorf("crs:%s must be a finite number between %g and %g", name, min, max)
	}
	return number, true, nil
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

func readXMPToneCurve(packet *XMPPacket, name string) ([256]uint8, bool, error) {
	var empty [256]uint8
	uri, local, err := expandedName("crs:" + name)
	if err != nil {
		return empty, false, err
	}
	property := packet.findRootProperty(uri, local)
	if property.attribute == nil && property.element == nil {
		return empty, false, nil
	}
	values := make([]string, 0, 32)
	if property.attribute != nil {
		values = append(values, property.attribute.Value)
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
		if text, scalar := scalarElementText(property.element); scalar {
			values = append(values, text)
		} else {
			collect(property.element)
		}
	}
	points, err := parseToneCurveValues(values)
	if err != nil {
		return empty, false, fmt.Errorf("crs:%s: %w", name, err)
	}
	return makeToneCurveTable(points), true, nil
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

func makeToneCurveTable(points []toneCurvePoint) [256]uint8 {
	var table [256]uint8
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
	segment := 0
	for input := 0; input < 256; input++ {
		x := float64(input)
		if x <= points[0].x {
			table[input] = uint8(math.Round(points[0].y))
			continue
		}
		if x >= points[len(points)-1].x {
			table[input] = uint8(math.Round(points[len(points)-1].y))
			continue
		}
		for segment+1 < len(points)-1 && x > points[segment+1].x {
			segment++
		}
		width := widths[segment]
		t := (x - points[segment].x) / width
		t2, t3 := t*t, t*t*t
		value := (2*t3-3*t2+1)*points[segment].y + (t3-2*t2+t)*width*slopes[segment] +
			(-2*t3+3*t2)*points[segment+1].y + (t3-t2)*width*slopes[segment+1]
		table[input] = uint8(math.Round(math.Max(0, math.Min(255, value))))
	}
	return table
}

func (adjustments xmpBasicAdjustments) globalActive() bool {
	return adjustments.exposure != 0 || adjustments.hasWhiteBalance || adjustments.contrast != 0 || adjustments.highlights != 0 ||
		adjustments.shadows != 0 || adjustments.whites != 0 || adjustments.blacks != 0 ||
		adjustments.vibrance != 0 || adjustments.saturation != 0 || adjustments.hasMaster ||
		adjustments.hasChannel[0] || adjustments.hasChannel[1] || adjustments.hasChannel[2] || adjustments.hasColorMix
}

func (adjustments xmpBasicAdjustments) active() bool {
	return adjustments.globalActive() || len(adjustments.local) > 0
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
			redGain, greenGain, blueGain := 1.0, 1.0, 1.0
			if adjustments.hasWhiteBalance {
				redGain, greenGain, blueGain = adjustments.whiteBalance[0], adjustments.whiteBalance[1], adjustments.whiteBalance[2]
			}
			red := srgb8ToLinear[pixel.R] * redGain * exposure
			green := srgb8ToLinear[pixel.G] * greenGain * exposure
			blue := srgb8ToLinear[pixel.B] * blueGain * exposure
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
			red8, green8, blue8 := encodeLinearSRGB(red), encodeLinearSRGB(green), encodeLinearSRGB(blue)
			if adjustments.hasMaster {
				red8 = adjustments.masterCurve[red8]
				green8 = adjustments.masterCurve[green8]
				blue8 = adjustments.masterCurve[blue8]
			}
			if adjustments.hasChannel[0] {
				red8 = adjustments.channelCurve[0][red8]
			}
			if adjustments.hasChannel[1] {
				green8 = adjustments.channelCurve[1][green8]
			}
			if adjustments.hasChannel[2] {
				blue8 = adjustments.channelCurve[2][blue8]
			}
			if adjustments.hasColorMix {
				red8, green8, blue8 = applyXMPHSLColorMix(red8, green8, blue8, adjustments.colorMix)
			}
			result.SetNRGBA(x, y, color.NRGBA{
				R: red8, G: green8, B: blue8, A: pixel.A,
			})
		}
	}
	return result
}

var xmpHSLHueCenters = [8]float64{0, 30, 60, 120, 180, 240, 270, 315}

func applyXMPHSLColorMix(red8, green8, blue8 uint8, adjustments [8]xmpHSLColorAdjustment) (uint8, uint8, uint8) {
	hue, saturation, luminance := srgbToHSL(float64(red8)/255, float64(green8)/255, float64(blue8)/255)
	colorAdjustment := interpolateHSLBandAdjustments(adjustments, hue)

	// Camera Raw applies these controls in its RAW color pipeline. Here the
	// slider values are mapped to bounded sRGB-domain approximations.
	hue = math.Mod(hue+colorAdjustment.hue*0.3+360, 360)
	saturation *= math.Max(0, math.Min(2, 1+colorAdjustment.saturation/100))
	luminance += colorAdjustment.luminance / 100 * 0.25
	red, green, blue := hslToSRGB(hue, math.Max(0, math.Min(1, saturation)), math.Max(0, math.Min(1, luminance)))
	return encodeSRGBByte(red), encodeSRGBByte(green), encodeSRGBByte(blue)
}

func encodeSRGBByte(value float64) uint8 {
	return uint8(math.Round(math.Max(0, math.Min(1, value)) * 255))
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
