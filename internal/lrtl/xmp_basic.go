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
	exposure     float64
	contrast     float64
	highlights   float64
	shadows      float64
	whites       float64
	blacks       float64
	vibrance     float64
	saturation   float64
	masterCurve  [256]uint8
	hasMaster    bool
	channelCurve [3][256]uint8
	hasChannel   [3]bool
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
	return adjustments, nil
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

func (adjustments xmpBasicAdjustments) active() bool {
	return adjustments.exposure != 0 || adjustments.contrast != 0 || adjustments.highlights != 0 ||
		adjustments.shadows != 0 || adjustments.whites != 0 || adjustments.blacks != 0 ||
		adjustments.vibrance != 0 || adjustments.saturation != 0 || adjustments.hasMaster ||
		adjustments.hasChannel[0] || adjustments.hasChannel[1] || adjustments.hasChannel[2]
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
			result.SetNRGBA(x, y, color.NRGBA{
				R: red8, G: green8, B: blue8, A: pixel.A,
			})
		}
	}
	return result
}
