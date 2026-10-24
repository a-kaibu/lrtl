package lrtl

import (
	"path/filepath"
	"strings"
)

// Independent construction of the measured modern reload defaults. These are
// metadata values, not a copied preset/XML asset. Mask preparation is separate.
func fullCopyDefaultNodes(image, quality string) map[pasteName]*pasteNode {
	nodes := map[pasteName]*pasteNode{}
	put := func(uri, prefix, name, value string) {
		key := pasteName{uri, name}
		nodes[key] = &pasteNode{Name: key, Prefix: prefix, Kind: "scalar", Value: value}
	}
	group := func(value, names string) {
		for _, name := range strings.Fields(names) {
			put(crsNamespace, "crs", name, value)
		}
	}
	group("0", `Highlights2012 Shadows2012 Whites2012 Blacks2012 Contrast2012 Clarity2012 Texture Saturation
CropConstrainToWarp LuminanceSmoothing LuminanceNoiseReductionContrast ChromaticAberrationR ChromaticAberrationB
VignetteAmount VignetteMidpoint ShadowTint RedHue RedSaturation GreenHue GreenSaturation BlueHue BlueSaturation Vibrance Defringe
SplitToningShadowHue SplitToningShadowSaturation ColorGradeShadowLum ColorGradeMidtoneHue ColorGradeMidtoneSat ColorGradeMidtoneLum
SplitToningHighlightHue SplitToningHighlightSaturation ColorGradeHighlightLum ColorGradeGlobalHue ColorGradeGlobalSat ColorGradeGlobalLum
SplitToningBalance ColorGradeBlending ParametricShadows ParametricDarks ParametricLights ParametricHighlights
SharpenEdgeMasking PostCropVignetteAmount PostCropVignetteRoundness PostCropVignetteHighlightContrast GrainAmount LensProfileEnable
LensManualDistortionAmount PerspectiveVertical PerspectiveHorizontal PerspectiveAspect PerspectiveUpright UprightVersion UprightCenterMode
UprightFocalMode UprightFourSegmentsCount AutoLateralCA DefringePurpleAmount DefringeGreenAmount Dehaze ToneMapStrength`)
	for _, color := range []string{"Red", "Orange", "Yellow", "Green", "Aqua", "Blue", "Purple", "Magenta"} {
		for _, control := range []string{"HueAdjustment", "SaturationAdjustment", "LuminanceAdjustment"} {
			group("0", control+color)
		}
	}
	for _, color := range []string{"Aqua", "Blue", "Green", "Magenta", "Orange", "Purple", "Red", "Yellow"} {
		group("0", "GrayMixer"+color)
	}
	group("0.0", "Exposure2012 CropTop CropLeft CropAngle PerspectiveRotate")
	group("1.0", "CropBottom CropRight")
	group("False", "HasCrop AlreadyApplied UprightPreview ConvertToGrayscale")
	group("25", "Sharpness ColorNoiseReduction ParametricShadowSplit SharpenDetail GrainSize")
	group("50", "LuminanceNoiseReductionDetail ColorNoiseReductionDetail ColorNoiseReductionSmoothness ParametricMidtoneSplit PostCropVignetteMidpoint PostCropVignetteFeather GrainFrequency")
	group("100", "LensProfileDistortionScale LensProfileVignettingScale LensProfileChromaticAberrationScale PerspectiveScale CurveRefineSaturation")
	group("", "LensProfileDigest LensProfileFilename LensProfileName")
	group("0.5", "UprightCenterNormX UprightCenterNormY")
	for name, value := range map[string]string{"Version": "14", "ProcessVersion": "11", "HasSettings": "True", "PostCropVignetteStyle": "1", "ParametricHighlightSplit": "75", "SharpenRadius": "+1.0", "UprightFocalLength35mm": "35.000", "UprightTransformCount": "6", "DefringePurpleHueLo": "30", "DefringePurpleHueHi": "70", "DefringeGreenHueLo": "40", "DefringeGreenHueHi": "60", "ToneCurveName2012": "Linear", "LensProfileSetup": "LensDefaults"} {
		put(crsNamespace, "crs", name, value)
	}
	ext := strings.ToLower(filepath.Ext(image))
	flatFile := ext == ".jpg" || ext == ".jpeg" || ext == ".tif" || ext == ".tiff"
	if flatFile || quality == "FLAT" {
		group("0", "Sharpness ColorNoiseReduction")
	}
	if !flatFile && quality != "FLAT" {
		put(crsNamespace, "crs", "RawFileName", filepath.Base(image))
	}
	put(namespacePrefixes["tiff"], "tiff", "Orientation", "1")
	put(xmpNamespace, "xmp", "Rating", "0")
	curveName := pasteName{crsNamespace, "ToneCurvePV2012"}
	curve := &pasteNode{Name: curveName, Prefix: "crs", Kind: "Seq"}
	for _, value := range []string{"0, 0", "255, 255"} {
		curve.Items = append(curve.Items, &pasteNode{Name: pasteName{rdfNamespace, "li"}, Prefix: "rdf", Kind: "scalar", Value: value})
	}
	nodes[curveName] = curve
	return nodes
}

// Native reload appends the input onto fresh defaults with replacement. Empty
// input roots/fields are pruned; nonempty roots replace the corresponding
// default as a whole, including its type, arrays and qualifiers.
func prepareFullCopyDefaults(packet *XMPPacket, image string) (bool, error) {
	before, _, err := saveStateMaps(packet)
	if err != nil {
		return false, err
	}
	quality, _, err := captureTimestampScalar(packet, "lrt:Quality")
	if err != nil {
		return false, err
	}
	after := fullCopyDefaultNodes(image, quality)
	for name, node := range before {
		if copy := clonePasteForAppend(node); copy != nil {
			after[name] = copy
		}
	}
	return writeChangedPasteRoots(packet, before, after)
}
