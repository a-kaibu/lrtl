package lrtl

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// NumericCRSProperties is the explicit allowlist of scalar Camera Raw controls.
var NumericCRSProperties = []string{
	"BlueHue", "BlueSaturation", "Brightness", "ChromaticAberrationB",
	"ChromaticAberrationR", "Blacks2012", "Clarity2012", "ColorNoiseReduction",
	"ColorNoiseReductionDetail", "ColorNoiseReductionSmoothness", "Contrast",
	"Contrast2012", "CropAngle", "CropBottom", "CropLeft", "CropRight", "CropTop",
	"Dehaze", "Exposure", "Exposure2012", "GreenHue", "GreenSaturation",
	"Highlights2012", "LuminanceNoiseReductionContrast", "LuminanceNoiseReductionDetail",
	"LuminanceSmoothing", "RedHue", "RedSaturation", "Saturation", "ShadowTint",
	"Shadows", "Shadows2012", "SharpenDetail", "SharpenEdgeMasking", "SharpenRadius",
	"Sharpness", "Temperature", "Texture", "Tint", "Vibrance", "VignetteAmount",
	"VignetteMidpoint", "Whites2012",
}

type ProposedFrame struct {
	Frame          Frame
	Packet         *XMPPacket
	Values         map[string]string
	CreatedSidecar bool
}

type InterpolationPlan struct {
	FrameCount      int
	KeyframeIndices []int
	Changes         []ProposedFrame
}

func (plan InterpolationPlan) PropertyCount() int {
	total := 0
	for _, change := range plan.Changes {
		total += len(change.Values)
	}
	return total
}

func numericCRSValues(packet *XMPPacket) map[string]float64 {
	values := make(map[string]float64)
	for _, property := range NumericCRSProperties {
		value, found, _ := packet.GetScalar("crs:" + property)
		if !found {
			continue
		}
		if number, ok := scalarNumber(value); ok {
			values[property] = number
		}
	}
	return values
}

func scalarNumber(value string) (float64, bool) {
	number, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
		return 0, false
	}
	return number, true
}

func formatNumber(value float64) string {
	if math.Abs(value) < 1e-12 {
		value = 0
	}
	return strconv.FormatFloat(value, 'g', 12, 64)
}

func linearValues(targetIndex, startIndex, endIndex int, startValues, endValues map[string]float64) map[string]float64 {
	span := endIndex - startIndex
	if span <= 0 {
		return nil
	}
	fraction := float64(targetIndex-startIndex) / float64(span)
	values := make(map[string]float64)
	for _, name := range NumericCRSProperties {
		start, startOK := startValues[name]
		end, endOK := endValues[name]
		if startOK && endOK {
			values[name] = start + (end-start)*fraction
		}
	}
	return values
}

func buildPlan(frames []Frame) (*InterpolationPlan, error) {
	packets := make([]*XMPPacket, len(frames))
	created := make([]bool, len(frames))
	for index, frame := range frames {
		if frame.Sidecar == "" {
			packets[index] = EmptyXMPPacket()
			created[index] = true
			continue
		}
		packet, err := loadPacket(frame)
		if err != nil {
			return nil, err
		}
		packets[index] = packet
	}

	keyframes := make([]int, 0)
	for index, packet := range packets {
		if packet.IsKeyframe() {
			keyframes = append(keyframes, index)
		}
	}
	if len(keyframes) == 0 && len(packets) > 0 {
		keyframes = append(keyframes, 0)
	}
	proposed := make(map[int]*ProposedFrame)
	assign := func(targetIndex int, sourceValues map[string]float64) error {
		for _, keyframeIndex := range keyframes {
			if targetIndex == keyframeIndex {
				return nil
			}
		}
		if len(sourceValues) == 0 {
			return nil
		}
		target := proposed[targetIndex]
		if target == nil {
			target = &ProposedFrame{
				Frame: frames[targetIndex], Packet: packets[targetIndex],
				Values: make(map[string]string), CreatedSidecar: created[targetIndex],
			}
			proposed[targetIndex] = target
		}
		for _, name := range NumericCRSProperties {
			value, ok := sourceValues[name]
			if !ok {
				continue
			}
			rendered := formatNumber(value)
			currentText, found, err := target.Packet.GetScalar("crs:" + name)
			if err != nil {
				return err
			}
			current, valid := scalarNumber(currentText)
			if !found || !valid || !closeEnough(current, value) {
				target.Values[name] = rendered
			}
		}
		return nil
	}

	if len(keyframes) == 1 {
		anchor := numericCRSValues(packets[keyframes[0]])
		for targetIndex := range frames {
			if err := assign(targetIndex, anchor); err != nil {
				return nil, err
			}
		}
	} else if len(keyframes) > 1 {
		first, last := keyframes[0], keyframes[len(keyframes)-1]
		firstValues := numericCRSValues(packets[first])
		lastValues := numericCRSValues(packets[last])
		if first > 0 {
			edgeValues := numericCRSValues(packets[0])
			for targetIndex := 1; targetIndex < first; targetIndex++ {
				values := copyNumericValues(firstValues)
				for name, value := range linearValues(targetIndex, 0, first, edgeValues, firstValues) {
					values[name] = value
				}
				if err := assign(targetIndex, values); err != nil {
					return nil, err
				}
			}
		}
		if last < len(frames)-1 {
			edgeIndex := len(frames) - 1
			edgeValues := numericCRSValues(packets[edgeIndex])
			for targetIndex := last + 1; targetIndex < edgeIndex; targetIndex++ {
				values := copyNumericValues(lastValues)
				for name, value := range linearValues(targetIndex, last, edgeIndex, lastValues, edgeValues) {
					values[name] = value
				}
				if err := assign(targetIndex, values); err != nil {
					return nil, err
				}
			}
		}
		for keyframeIndex := 0; keyframeIndex < len(keyframes)-1; keyframeIndex++ {
			left, right := keyframes[keyframeIndex], keyframes[keyframeIndex+1]
			leftValues := numericCRSValues(packets[left])
			rightValues := numericCRSValues(packets[right])
			for targetIndex := left + 1; targetIndex < right; targetIndex++ {
				if err := assign(targetIndex, linearValues(targetIndex, left, right, leftValues, rightValues)); err != nil {
					return nil, err
				}
			}
		}
	}

	indices := make([]int, 0, len(proposed))
	for index, change := range proposed {
		if len(change.Values) > 0 {
			indices = append(indices, index)
		}
	}
	sort.Ints(indices)
	changes := make([]ProposedFrame, 0, len(indices))
	for _, index := range indices {
		changes = append(changes, *proposed[index])
	}
	return &InterpolationPlan{FrameCount: len(frames), KeyframeIndices: keyframes, Changes: changes}, nil
}

func copyNumericValues(values map[string]float64) map[string]float64 {
	copy := make(map[string]float64, len(values))
	for name, value := range values {
		copy[name] = value
	}
	return copy
}

func closeEnough(left, right float64) bool {
	return math.Abs(left-right) <= 1e-12+1e-12*math.Abs(right)
}

type packetOutput struct {
	Path   string
	Packet *XMPPacket
}

func applyPlan(plan *InterpolationPlan) ([]packetOutput, error) {
	outputs := make([]packetOutput, 0, len(plan.Changes))
	for _, change := range plan.Changes {
		for _, name := range NumericCRSProperties {
			if value, ok := change.Values[name]; ok {
				if err := change.Packet.SetScalar("crs:"+name, value); err != nil {
					return nil, fmt.Errorf("%s: %w", change.Frame.Image, err)
				}
			}
		}
		outputs = append(outputs, packetOutput{Path: change.Frame.OutputSidecar(), Packet: change.Packet})
	}
	return outputs, nil
}
