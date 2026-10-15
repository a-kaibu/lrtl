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
	"Blacks2012", "BlueHue", "BlueSaturation", "Brightness", "ChromaticAberrationB",
	"ChromaticAberrationR", "Clarity", "Clarity2012", "ColorGradeBlending",
	"ColorGradeGlobalHue", "ColorGradeGlobalLum", "ColorGradeGlobalSat", "ColorGradeHighlightLum",
	"ColorGradeMidtoneHue", "ColorGradeMidtoneLum", "ColorGradeMidtoneSat", "ColorGradeShadowLum",
	"ColorNoiseReduction", "ColorNoiseReductionDetail", "ColorNoiseReductionSmoothness",
	"Contrast", "Contrast2012", "CropAngle", "CropBottom", "CropLeft", "CropRight",
	"CropTop", "CurveRefineSaturation", "Defringe", "DefringeGreenAmount",
	"DefringeGreenHueHi", "DefringeGreenHueLo", "DefringePurpleAmount", "DefringePurpleHueHi",
	"DefringePurpleHueLo", "Dehaze", "Exposure", "Exposure2012", "FillLight",
	"GrainAmount", "GrainFrequency", "GrainSize", "GrayMixerAqua", "GrayMixerBlue",
	"GrayMixerGreen", "GrayMixerMagenta", "GrayMixerOrange", "GrayMixerPurple",
	"GrayMixerRed", "GrayMixerYellow", "GreenHue", "GreenSaturation", "HDRMaxValue", "HighlightRecovery",
	"Highlights2012", "HueAdjustmentAqua",
	"HueAdjustmentBlue", "HueAdjustmentGreen", "HueAdjustmentMagenta", "HueAdjustmentOrange",
	"HueAdjustmentPurple", "HueAdjustmentRed", "HueAdjustmentYellow", "IncrementalTemperature",
	"IncrementalTint", "LensManualDistortionAmount", "LensProfileChromaticAberrationScale", "LensProfileDistortionScale",
	"LensProfileVignettingScale", "LuminanceAdjustmentAqua", "LuminanceAdjustmentBlue",
	"LuminanceAdjustmentGreen", "LuminanceAdjustmentMagenta", "LuminanceAdjustmentOrange",
	"LuminanceAdjustmentPurple", "LuminanceAdjustmentRed", "LuminanceAdjustmentYellow",
	"LuminanceNoiseReductionContrast", "LuminanceNoiseReductionDetail", "LuminanceSmoothing",
	"ParametricDarks", "ParametricHighlights", "ParametricHighlightSplit", "ParametricLights",
	"ParametricMidtoneSplit", "ParametricShadowSplit", "ParametricShadows", "PerspectiveAspect",
	"PerspectiveHorizontal", "PerspectiveRotate", "PerspectiveScale", "PerspectiveVertical",
	"PostCropVignetteAmount", "PostCropVignetteFeather", "PostCropVignetteHighlightContrast",
	"PostCropVignetteMidpoint", "PostCropVignetteRoundness", "RedHue", "RedSaturation",
	"Saturation", "SaturationAdjustmentAqua", "SaturationAdjustmentBlue",
	"SaturationAdjustmentGreen", "SaturationAdjustmentMagenta", "SaturationAdjustmentOrange",
	"SaturationAdjustmentPurple", "SaturationAdjustmentRed", "SaturationAdjustmentYellow",
	"ShadowTint", "Shadows", "Shadows2012", "SharpenDetail", "SharpenEdgeMasking",
	"SharpenRadius", "Sharpness", "SplitToningBalance", "SplitToningHighlightHue",
	"SplitToningHighlightSaturation", "SplitToningShadowHue", "SplitToningShadowSaturation",
	"Temperature", "Texture", "Tint", "ToneMapStrength",
	"UprightCenterNormX", "UprightCenterNormY", "UprightFocalLength35mm", "Vibrance",
	"VignetteAmount", "VignetteMidpoint", "Whites2012",
}

var firstFrameEqualizedProperties = []string{
	"Look", "PointColors", "Preset", "ToneCurve", "ToneCurveName", "ToneCurveName2012", "ToneCurvePV2012",
	"ToneCurvePV2012Blue", "ToneCurvePV2012Green", "ToneCurvePV2012Red",
}

type ProposedFrame struct {
	Frame                Frame
	Packet               *XMPPacket
	Values               map[string]string
	StructuredProperties []string
	MaskTransitions      []maskTransitionChange
	CreatedSidecar       bool
}

type InterpolationPlan struct {
	FrameCount       int
	KeyframeIndices  []int
	Changes          []ProposedFrame
	StructuredSource *XMPPacket
}

func (plan InterpolationPlan) PropertyCount() int {
	total := 0
	for _, change := range plan.Changes {
		total += len(change.Values) + len(change.StructuredProperties)
		for _, transition := range change.MaskTransitions {
			total += len(transition.values)
			if transition.hasLuminanceRange {
				total++
			}
		}
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

func buildPlan(frames []Frame) (*InterpolationPlan, error) {
	return buildPlanWithSmoothness(frames, 50)
}

func buildPlanWithSmoothness(frames []Frame, smoothness int) (*InterpolationPlan, error) {
	if smoothness < 0 || smoothness > 100 {
		return nil, fmt.Errorf("smoothness must be between 0 and 100")
	}
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
	if len(packets) > 0 {
		for targetIndex := 1; targetIndex < len(packets); targetIndex++ {
			for _, property := range firstFrameEqualizedProperties {
				differs, err := packets[targetIndex].RootPropertyDiffers(packets[0], "crs:"+property)
				if err != nil {
					return nil, fmt.Errorf("%s: %w", frames[targetIndex].Image, err)
				}
				if !differs {
					continue
				}
				change := proposed[targetIndex]
				if change == nil {
					change = &ProposedFrame{
						Frame: frames[targetIndex], Packet: packets[targetIndex],
						Values: make(map[string]string), CreatedSidecar: created[targetIndex],
					}
					proposed[targetIndex] = change
				}
				change.StructuredProperties = append(change.StructuredProperties, property)
			}
		}
	}
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

	numericValues := make([]map[string]float64, len(packets))
	for index, packet := range packets {
		numericValues[index] = numericCRSValues(packet)
	}
	keyframeSet := make(map[int]bool, len(keyframes))
	keyframeFlags := make([]bool, len(frames))
	for _, keyframe := range keyframes {
		keyframeSet[keyframe] = true
		keyframeFlags[keyframe] = true
	}
	for _, name := range NumericCRSProperties {
		spec, registered := globalTransitionSpecs[name]
		if !registered {
			continue
		}
		if name == "Exposure2012" && len(packets) > 0 {
			mergedHDR, found, err := packets[0].GetScalar("crs:IsMergedHDR")
			if err != nil {
				return nil, fmt.Errorf("%s: %w", frames[0].Image, err)
			}
			if found && mergedHDR == "true" {
				spec.min, spec.max = -10, 10
			}
		}
		anchors := make([]lrtSplinePoint, 0, len(keyframes))
		for _, keyframe := range keyframes {
			if value, found := numericValues[keyframe][name]; found {
				anchors = append(anchors, lrtSplinePoint{x: float32(keyframe), y: float32(value)})
			}
		}
		if len(anchors) == 0 {
			continue
		}
		if len(anchors) == 1 {
			for targetIndex := range frames {
				if keyframeSet[targetIndex] {
					continue
				}
				if err := assign(targetIndex, map[string]float64{name: float64(anchors[0].y)}); err != nil {
					return nil, err
				}
			}
			continue
		}
		splinePoints := transitionSplinePoints(anchors, len(frames))
		spline, err := newLRTTransitionSpline(splinePoints, smoothness)
		if err != nil {
			return nil, err
		}
		values, filled := spline.sampleFrames(len(frames), keyframeFlags)
		for targetIndex := range frames {
			if keyframeSet[targetIndex] {
				continue
			}
			if !filled[targetIndex] {
				continue
			}
			value := adjustTransitionValue(values[targetIndex], spec)
			if err := assign(targetIndex, map[string]float64{name: value}); err != nil {
				return nil, err
			}
		}
	}
	maskTransitions, err := planMaskTransitions(packets, keyframes, smoothness)
	if err != nil {
		return nil, err
	}
	for targetIndex, transitions := range maskTransitions {
		if len(transitions) == 0 {
			continue
		}
		target := proposed[targetIndex]
		if target == nil {
			target = &ProposedFrame{
				Frame: frames[targetIndex], Packet: packets[targetIndex],
				Values: make(map[string]string), CreatedSidecar: created[targetIndex],
			}
			proposed[targetIndex] = target
		}
		target.MaskTransitions = append(target.MaskTransitions, transitions...)
	}

	indices := make([]int, 0, len(proposed))
	for index, change := range proposed {
		if len(change.Values) > 0 || len(change.StructuredProperties) > 0 || len(change.MaskTransitions) > 0 {
			indices = append(indices, index)
		}
	}
	sort.Ints(indices)
	changes := make([]ProposedFrame, 0, len(indices))
	for _, index := range indices {
		changes = append(changes, *proposed[index])
	}
	var structuredSource *XMPPacket
	if len(packets) > 0 {
		structuredSource = packets[0]
	}
	return &InterpolationPlan{
		FrameCount: len(frames), KeyframeIndices: keyframes, Changes: changes,
		StructuredSource: structuredSource,
	}, nil
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
		for _, property := range change.StructuredProperties {
			if _, err := change.Packet.CopyRootPropertyFrom(plan.StructuredSource, "crs:"+property); err != nil {
				return nil, fmt.Errorf("%s: %w", change.Frame.Image, err)
			}
		}
		for _, transition := range change.MaskTransitions {
			if err := applyMaskTransition(change.Packet, transition); err != nil {
				return nil, fmt.Errorf("%s: %w", change.Frame.Image, err)
			}
		}
		outputs = append(outputs, packetOutput{Path: change.Frame.OutputSidecar(), Packet: change.Packet})
	}
	return outputs, nil
}
