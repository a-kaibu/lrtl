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
	"Look", "PointColors", "Preset", "ToneCurve", "ToneCurveName", "ToneCurvePV2012",
	"ToneCurvePV2012Blue", "ToneCurvePV2012Green", "ToneCurvePV2012Red",
}

type ProposedFrame struct {
	Frame                    Frame
	Packet                   *XMPPacket
	Values                   map[string]string
	ScalarValues             map[string]string
	ScalarRemovals           []string
	StructuredProperties     []string
	MaskStructuredProperties []maskStructuredProperty
	MaskTransitions          []maskTransitionChange
	CreatedSidecar           bool
}

type InterpolationPlan struct {
	FrameCount            int
	KeyframeIndices       []int
	NormalKeyframeIndices []int
	CropKeyframeIndices   []int
	Changes               []ProposedFrame
	StructuredSource      *XMPPacket
}

func (plan InterpolationPlan) PropertyCount() int {
	total := 0
	for _, change := range plan.Changes {
		total += len(change.Values) + len(change.ScalarValues) + len(change.ScalarRemovals) + len(change.StructuredProperties) + len(change.MaskStructuredProperties)
		for _, transition := range change.MaskTransitions {
			total += len(transition.values)
			if transition.hasLuminanceRange {
				total++
			}
		}
	}
	return total
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
		created[index] = frame.Sidecar == ""
		packet, err := PacketForFrame(frame)
		if err != nil {
			return nil, err
		}
		packets[index] = packet
	}

	normalKeyframes, cropKeyframes, keyframes := transitionKeyframeGroups(packets)
	proposed := make(map[int]*ProposedFrame)
	scalarTransitions, err := planRootScalarTransitions(packets)
	if err != nil {
		return nil, err
	}
	for index, scalar := range scalarTransitions {
		if len(scalar.Values) > 0 || len(scalar.Removals) > 0 {
			proposed[index] = &ProposedFrame{Frame: frames[index], Packet: packets[index], Values: make(map[string]string), ScalarValues: scalar.Values, ScalarRemovals: scalar.Removals, CreatedSidecar: created[index]}
			for _, name := range scalar.Removals {
				if err := packets[index].RemoveProperty(name); err != nil {
					return nil, err
				}
			}
		}
	}
	if len(packets) > 0 {
		sourceIndex := normalKeyframes[0]
		for targetIndex := 0; targetIndex < len(packets); targetIndex++ {
			if targetIndex == sourceIndex {
				continue
			}
			for _, property := range firstFrameEqualizedProperties {
				differs, err := packets[targetIndex].RootPropertyDiffers(packets[sourceIndex], "crs:"+property)
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
		for targetIndex := 0; targetIndex < len(packets); targetIndex++ {
			if targetIndex == sourceIndex {
				continue
			}
			for _, correctionIndex := range firstFrameEqualizedMaskGroupIndexes {
				for _, property := range firstFrameEqualizedMaskGroupCurves {
					differs, err := packets[targetIndex].maskGroupStructuredPropertyDiffers(packets[sourceIndex], correctionIndex, property)
					if err != nil {
						return nil, fmt.Errorf("%s: %w", frames[targetIndex].Image, err)
					}
					if !differs && maskGroupCorrectionAt(packets[targetIndex], correctionIndex) == nil {
						for _, packet := range packets {
							if packet.hasMaskGroupStructuredProperty(correctionIndex, property) {
								differs = true
								break
							}
						}
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
					change.MaskStructuredProperties = append(change.MaskStructuredProperties, maskStructuredProperty{
						correctionIndex: correctionIndex, property: property,
					})
				}
			}
		}
	}
	assign := func(targetIndex int, sourceValues map[string]float64) error {
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

	numericInputs, err := prepareNumericTransitionInputs(packets)
	if err != nil {
		return nil, err
	}
	if len(normalKeyframes) > 0 {
		for index, removals := range numericInputs.clearMissingWBReferences(normalKeyframes[0]) {
			if len(removals) == 0 {
				continue
			}
			if proposed[index] == nil {
				proposed[index] = &ProposedFrame{Frame: frames[index], Packet: packets[index], Values: make(map[string]string), CreatedSidecar: created[index]}
			}
			proposed[index].ScalarRemovals = append(proposed[index].ScalarRemovals, removals...)
		}
	}
	for index, values := range numericInputs.Missing {
		if err := assign(index, values); err != nil {
			return nil, err
		}
	}
	numericValues := numericInputs.Values
	for _, name := range NumericCRSProperties {
		spec, registered := globalTransitionSpecs[name]
		if !registered {
			continue
		}
		keyframes := normalKeyframes
		if strings.HasPrefix(name, "Crop") {
			keyframes = cropKeyframes
		}
		keyframeSet, keyframeFlags := transitionKeyframeFlags(len(frames), keyframes)
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
		preciseAnchors := make([]transitionAnchor, 0, len(keyframes))
		for _, keyframe := range keyframes {
			if value, found := numericValues[keyframe][name]; found {
				if math.IsInf(float64(float32(value)), 0) {
					return nil, fmt.Errorf("%s: crs:%s exceeds finite float32 transition range", frames[keyframe].Image, name)
				}
				anchors = append(anchors, lrtSplinePoint{x: float32(keyframe), y: float32(value)})
				preciseAnchors = append(preciseAnchors, transitionAnchor{keyframe, value})
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
				if err := assign(targetIndex, map[string]float64{name: adjustTransitionValue(preciseAnchors[0].value, spec)}); err != nil {
					return nil, err
				}
			}
			continue
		}
		values, filled, err := sampleLRTNumericTransition(anchors, preciseAnchors, len(frames), keyframeFlags, smoothness, spec)
		if err != nil {
			return nil, err
		}
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
	maskTransitions, err := planMaskTransitions(packets, normalKeyframes, smoothness)
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
		if len(change.Values) > 0 || len(change.ScalarValues) > 0 || len(change.ScalarRemovals) > 0 || len(change.StructuredProperties) > 0 || len(change.MaskStructuredProperties) > 0 || len(change.MaskTransitions) > 0 {
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
		structuredSource = packets[normalKeyframes[0]]
	}
	return &InterpolationPlan{
		FrameCount: len(frames), KeyframeIndices: keyframes, Changes: changes,
		NormalKeyframeIndices: normalKeyframes, CropKeyframeIndices: cropKeyframes,
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
		for _, name := range change.ScalarRemovals {
			if err := change.Packet.RemoveProperty(name); err != nil {
				return nil, err
			}
		}
		scalarNames := make([]string, 0, len(change.ScalarValues))
		for name := range change.ScalarValues {
			scalarNames = append(scalarNames, name)
		}
		sort.Strings(scalarNames)
		for _, name := range scalarNames {
			if err := change.Packet.SetScalar(name, change.ScalarValues[name]); err != nil {
				return nil, fmt.Errorf("%s: %w", change.Frame.Image, err)
			}
		}
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
		for _, property := range change.MaskStructuredProperties {
			if _, err := change.Packet.copyMaskGroupStructuredPropertyFrom(plan.StructuredSource, property.correctionIndex, property.property); err != nil {
				return nil, fmt.Errorf("%s: %w", change.Frame.Image, err)
			}
		}
		outputs = append(outputs, packetOutput{Path: change.Frame.OutputSidecar(), Packet: change.Packet})
	}
	return outputs, nil
}
