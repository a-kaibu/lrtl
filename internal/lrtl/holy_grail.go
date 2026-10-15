package lrtl

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/beevik/etree"
	"github.com/evanoberholster/imagemeta"
)

const holyGrailCorrectionProperty = "ol:HolyGrailCorrection2012"
const holyGrailTargetProperty = "ol:HolyGrailXMPTarget"
const holyGrailKeyframeProperty = "ol:HolyGrailKeyframeKind"
const holyGrailPreviousRatingProperty = "ol:HolyGrailPreviousRating"
const legacyLRTInternalMaskName = "LRT: don't alter!"
const maxRenderXMPExposureStops = 32

type holyGrailXMPTarget struct {
	Collection string
	Index      int
}

type holyGrailXMPWrite struct {
	Target holyGrailXMPTarget
	Value  float64
}

func globalHolyGrailTarget() holyGrailXMPTarget {
	return holyGrailXMPTarget{}
}

func (target holyGrailXMPTarget) String() string {
	if target.Collection == "" {
		return "global"
	}
	return fmt.Sprintf("%s:%d", target.Collection, target.Index)
}

func parseHolyGrailXMPTarget(value string) (holyGrailXMPTarget, error) {
	if value == "global" {
		return globalHolyGrailTarget(), nil
	}
	separator := strings.LastIndexByte(value, ':')
	if separator <= 0 {
		return holyGrailXMPTarget{}, fmt.Errorf("invalid Holy Grail XMP target %q", value)
	}
	collection := value[:separator]
	if collection != "MaskGroupBasedCorrections" && collection != "GradientBasedCorrections" {
		return holyGrailXMPTarget{}, fmt.Errorf("unsupported Holy Grail XMP target %q", value)
	}
	index, err := strconv.Atoi(value[separator+1:])
	if err != nil || index < 1 {
		return holyGrailXMPTarget{}, fmt.Errorf("invalid Holy Grail XMP target %q", value)
	}
	return holyGrailXMPTarget{Collection: collection, Index: index}, nil
}

func findHolyGrailXMPTarget(packet *XMPPacket) holyGrailXMPTarget {
	maskGroups := packet.findRootProperty(crsNamespace, "MaskGroupBasedCorrections")
	maskGroupItems := rdfArrayItems(maskGroups.element)
	gradients := packet.findRootProperty(crsNamespace, "GradientBasedCorrections")
	gradientItems := rdfArrayItems(gradients.element)
	for _, candidate := range []struct {
		collection string
		items      []*etree.Element
	}{
		{collection: "MaskGroupBasedCorrections", items: maskGroupItems},
		{collection: "GradientBasedCorrections", items: gradientItems},
	} {
		for index, item := range candidate.items {
			correction := unwrapRDFArrayItem(item)
			name, found, err := crsElementValue(correction, "CorrectionName")
			if err == nil && found && strings.TrimSpace(name) == "#LRT internal use (HG)" {
				return holyGrailXMPTarget{Collection: candidate.collection, Index: index + 1}
			}
		}
	}
	if len(gradientItems) >= 7 &&
		isLegacyLRTInternalGradient(gradientItems[4]) &&
		isLegacyLRTInternalGradient(gradientItems[5]) &&
		isLegacyLRTInternalGradient(gradientItems[6]) {
		return holyGrailXMPTarget{Collection: "GradientBasedCorrections", Index: 5}
	}
	return globalHolyGrailTarget()
}

func isLegacyLRTInternalGradient(item *etree.Element) bool {
	correction := unwrapRDFArrayItem(item)
	masks := findCRSDescendant(correction, "CorrectionMasks")
	maskItems := rdfArrayItems(masks)
	if len(maskItems) != 1 {
		return false
	}
	mask := unwrapRDFArrayItem(maskItems[0])
	name, found, err := crsElementValue(mask, "MaskName")
	return err == nil && found && strings.TrimSpace(name) == legacyLRTInternalMaskName
}

func holyGrailTargetElement(packet *XMPPacket, target holyGrailXMPTarget) (*etree.Element, error) {
	if target.Collection == "" {
		return nil, nil
	}
	if target.Index < 1 {
		return nil, fmt.Errorf("invalid Holy Grail XMP array index %d", target.Index)
	}
	property := packet.findRootProperty(crsNamespace, target.Collection)
	items := rdfArrayItems(property.element)
	if target.Index > len(items) {
		return nil, fmt.Errorf("crs:%s[%d] no longer exists", target.Collection, target.Index)
	}
	return unwrapRDFArrayItem(items[target.Index-1]), nil
}

func getHolyGrailTargetValue(packet *XMPPacket, target holyGrailXMPTarget) (float64, bool, error) {
	if target.Collection == "" {
		text, found, err := packet.GetScalar("crs:Exposure2012")
		if err != nil || !found {
			return 0, found, err
		}
		value, valid := scalarNumber(text)
		if !valid {
			return 0, true, fmt.Errorf("crs:Exposure2012 is not numeric")
		}
		return value, true, nil
	}
	element, err := holyGrailTargetElement(packet, target)
	if err != nil {
		return 0, false, err
	}
	text, found, err := crsElementValue(element, "LocalExposure2012")
	if err != nil || !found {
		return 0, found, err
	}
	value, valid := scalarNumber(text)
	if !valid {
		return 0, true, fmt.Errorf("crs:%s[%d]/crs:LocalExposure2012 is not numeric", target.Collection, target.Index)
	}
	return value, true, nil
}

func readHolyGrailRenderCorrection(packet *XMPPacket) (float64, *etree.Element, error) {
	correctionText, hasCorrection, err := packet.GetScalar(holyGrailCorrectionProperty)
	if err != nil {
		return 0, nil, err
	}
	targetText, hasTarget, err := packet.GetScalar(holyGrailTargetProperty)
	if err != nil {
		return 0, nil, err
	}
	if hasTarget && !hasCorrection {
		return 0, nil, fmt.Errorf("orphaned lrtl property %s", holyGrailTargetProperty)
	}
	if !hasCorrection || !hasTarget {
		return 0, nil, nil
	}
	target, err := parseHolyGrailXMPTarget(targetText)
	if err != nil {
		return 0, nil, err
	}
	if target.Collection == "" {
		return 0, nil, nil
	}
	correction, valid := scalarNumber(correctionText)
	if !valid || math.Abs(correction) > maxRenderXMPExposureStops {
		return 0, nil, fmt.Errorf("%s must be a finite number between -%d and +%d stops", holyGrailCorrectionProperty, maxRenderXMPExposureStops, maxRenderXMPExposureStops)
	}
	element, err := holyGrailTargetElement(packet, target)
	if err != nil {
		return 0, nil, err
	}
	return correction, element, nil
}

func setHolyGrailTargetValue(packet *XMPPacket, target holyGrailXMPTarget, value float64) error {
	if target.Collection == "" {
		return packet.SetScalar("crs:Exposure2012", formatNumber(value))
	}
	element, err := holyGrailTargetElement(packet, target)
	if err != nil {
		return err
	}
	return setCRSElementValue(element, "LocalExposure2012", formatNumber(value))
}

func holyGrailTargetLabel(target holyGrailXMPTarget) string {
	if target.Collection == "" {
		return "Exposure2012"
	}
	return fmt.Sprintf("%s[%d].LocalExposure2012", target.Collection, target.Index)
}

type CameraExposure struct {
	ExposureTime float64
	FNumber      float64
	ISO          float64
	Stops        float64
	Manual       bool
}

type HolyGrailCurveOptions struct {
	Stretch  float64
	Rotate   float64
	Auto     bool
	LRTInput bool
}

type HolyGrailChange struct {
	Frame            Frame
	Packet           *XMPPacket
	Camera           CameraExposure
	Target           holyGrailXMPTarget
	XMPWrites        []holyGrailXMPWrite
	Compensation     float64
	ExposureBefore   float64
	ExposureAfter    float64
	ExposureChanged  bool
	RemoveCorrection bool
	KeyframeRating   int
	RestoreKeyframe  bool
	PreviousRating   string
}

type HolyGrailPlan struct {
	Changes      []HolyGrailChange
	ManualMode   bool
	MarkJumps    bool
	CurveOptions *HolyGrailCurveOptions
	AutoCurve    bool
}

func readCameraExposure(path string, ignoreAperture bool) (CameraExposure, error) {
	file, err := os.Open(path)
	if err != nil {
		return CameraExposure{}, err
	}
	metadata, decodeErr := imagemeta.Decode(file)
	closeErr := file.Close()
	if decodeErr != nil {
		return CameraExposure{}, decodeErr
	}
	if closeErr != nil {
		return CameraExposure{}, closeErr
	}
	exposureTime := float64(metadata.ExposureTime)
	fNumber := float64(metadata.FNumber)
	iso := float64(metadata.ISOSpeed)
	if iso == 0 {
		iso = float64(metadata.ISO)
	}
	if exposureTime <= 0 || math.IsNaN(exposureTime) || math.IsInf(exposureTime, 0) {
		return CameraExposure{}, fmt.Errorf("EXIF ExposureTime is missing or invalid")
	}
	if iso <= 0 || math.IsNaN(iso) || math.IsInf(iso, 0) {
		return CameraExposure{}, fmt.Errorf("EXIF ISO speed is missing or invalid")
	}
	if !ignoreAperture && (fNumber <= 0 || math.IsNaN(fNumber) || math.IsInf(fNumber, 0)) {
		return CameraExposure{}, fmt.Errorf("EXIF FNumber is missing or invalid; use --ignore-aperture if aperture stayed fixed")
	}
	setting := math.Log2(exposureTime * iso)
	if !ignoreAperture {
		setting -= 2 * math.Log2(fNumber)
	}
	if math.IsNaN(setting) || math.IsInf(setting, 0) {
		return CameraExposure{}, fmt.Errorf("EXIF exposure settings produce an invalid exposure value")
	}
	manual := uint16(metadata.ExposureMode) == 1 || uint16(metadata.ExposureProgram) == 1
	return CameraExposure{ExposureTime: exposureTime, FNumber: fNumber, ISO: iso, Stops: setting, Manual: manual}, nil
}

func cameraSettingsChanged(before, after CameraExposure, ignoreAperture bool) bool {
	changed := func(left, right float64) bool {
		return math.Abs(left-right) > 1e-6*math.Max(1, math.Max(math.Abs(left), math.Abs(right)))
	}
	if changed(before.ExposureTime, after.ExposureTime) || changed(before.ISO, after.ISO) {
		return true
	}
	return !ignoreAperture && changed(before.FNumber, after.FNumber)
}

func holyGrailCurveAdjustments(cameras []CameraExposure, options HolyGrailCurveOptions) []float64 {
	adjustments := make([]float64, len(cameras))
	if len(cameras) == 0 {
		return adjustments
	}
	normalized := normalizedCameraExposureCurve(cameras)
	for index := range cameras {
		trend := float64(index)/float64(len(cameras)) - 0.5
		adjustments[index] = normalized[index]*options.Stretch + trend*options.Rotate
	}
	return adjustments
}

func optimizeHolyGrailCurve(cameras []CameraExposure) HolyGrailCurveOptions {
	options := HolyGrailCurveOptions{}
	if len(cameras) < 2 {
		return options
	}
	base := make([]float64, len(cameras))
	minimum, maximum := cameras[0].Stops, cameras[0].Stops
	for index := range cameras {
		base[index] = cameras[0].Stops - cameras[index].Stops
		minimum = math.Min(minimum, cameras[index].Stops)
		maximum = math.Max(maximum, cameras[index].Stops)
	}
	if maximum-minimum <= 1e-12 {
		return options
	}
	normalized := normalizedCameraExposureCurve(cameras)
	trend := make([]float64, len(cameras))
	for index := range cameras {
		trend[index] = float64(index)/float64(len(cameras)) - 0.5
	}
	neighbors := len(cameras) - 1
	baseSmooth := lowessSmooth(base, neighbors)
	normalizedSmooth := lowessSmooth(normalized, neighbors)
	trendSmooth := lowessSmooth(trend, neighbors)

	bestScore := math.Inf(1)
	for step := 1; step <= 150; step++ {
		candidate := float64(step)/50 - 1.5
		score := 0.0
		for index := range base {
			residual := (base[index] - baseSmooth[index]) + candidate*(normalized[index]-normalizedSmooth[index])
			score += math.Abs(residual)
		}
		if score < bestScore {
			bestScore = score
			options.Stretch = candidate
		}
	}

	bestScore = math.Inf(1)
	for step := 1; step <= 200; step++ {
		candidate := float64(step)/25 - 4
		score := 0.0
		for index := range base {
			smoothed := baseSmooth[index] + options.Stretch*normalizedSmooth[index] + candidate*trendSmooth[index]
			score += math.Abs(smoothed)
		}
		if score < bestScore {
			bestScore = score
			options.Rotate = candidate
		}
	}
	return options
}

func normalizedCameraExposureCurve(cameras []CameraExposure) []float64 {
	values := make([]float64, len(cameras))
	if len(cameras) == 0 {
		return values
	}
	minimum, maximum := cameras[0].Stops, cameras[0].Stops
	for _, camera := range cameras[1:] {
		minimum = math.Min(minimum, camera.Stops)
		maximum = math.Max(maximum, camera.Stops)
	}
	span := maximum - minimum
	if span <= 1e-12 {
		return values
	}
	for index, camera := range cameras {
		values[index] = (camera.Stops-minimum)/span - 0.5
	}
	return values
}

func readHolyGrailLRTInputs(frames []Frame) ([]float64, []int, error) {
	inputs := make([]float64, len(frames))
	ratings := make([]int, len(frames))
	for index, frame := range frames {
		packet, err := PacketForSidecar(frame.OutputSidecar())
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", frame.OutputSidecar(), err)
		}
		text, found, err := packet.GetScalar("lrt:ExternalExposureDefault")
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", frame.OutputSidecar(), err)
		}
		if !found {
			return nil, nil, fmt.Errorf("%s: missing scalar lrt:ExternalExposureDefault; export/develop the sequence in LRTimelapse first", frame.OutputSidecar())
		}
		value, valid := scalarNumber(text)
		if !valid {
			return nil, nil, fmt.Errorf("%s: lrt:ExternalExposureDefault is not a finite number", frame.OutputSidecar())
		}
		// LRT's frame model reads ExternalExposureDefault / 4 and negates it
		// before building the Holy Grail curve.
		inputs[index] = value / 4
		if rating, found := packet.Rating(); found {
			ratings[index] = rating
		}
	}
	return inputs, ratings, nil
}

func lrtHolyGrailAnchor(rating int) bool {
	return rating == 2 || rating == 3 || rating == 4
}

func interpolateHolyGrailAnchors(values []float64, ratings []int) ([]float64, error) {
	if len(values) != len(ratings) {
		return nil, fmt.Errorf("Holy Grail input and rating counts differ")
	}
	anchors := make([]int, 0, len(values))
	for index, rating := range ratings {
		if lrtHolyGrailAnchor(rating) {
			anchors = append(anchors, index)
		}
	}
	if len(anchors) == 0 {
		return nil, fmt.Errorf("--lrt-input requires at least one existing 2*, 3*, or 4* XMP rating to anchor the Holy Grail curve")
	}
	result := make([]float64, len(values))
	rightAnchor := 1
	for index := range result {
		if index <= anchors[0] {
			result[index] = values[anchors[0]]
			continue
		}
		if index >= anchors[len(anchors)-1] {
			result[index] = values[anchors[len(anchors)-1]]
			continue
		}
		for anchors[rightAnchor] < index {
			rightAnchor++
		}
		left, right := anchors[rightAnchor-1], anchors[rightAnchor]
		fraction := float64(index-left) / float64(right-left)
		result[index] = values[left] + fraction*(values[right]-values[left])
	}
	return result, nil
}

func centerAndClipHolyGrailCurve(values []float64, limit float64) []float64 {
	result := append([]float64(nil), values...)
	if len(result) == 0 {
		return result
	}
	minimum, maximum := result[0], result[0]
	for _, value := range result[1:] {
		minimum = math.Min(minimum, value)
		maximum = math.Max(maximum, value)
	}
	center := (minimum + maximum) / 2
	for index := range result {
		result[index] -= center
		if result[index] < -limit {
			result[index] = -limit
		}
		if result[index] > limit {
			result[index] = limit
		}
	}
	return result
}

func holyGrailLRTPairDelta(before, after CameraExposure, ignoreAperture bool) float64 {
	if before.ExposureTime <= 0 || after.ExposureTime <= 0 {
		return 0
	}
	delta := math.Log2(2 / (before.ExposureTime / after.ExposureTime))
	if before.ISO > 0 && after.ISO > 0 {
		delta -= math.Log2(after.ISO / before.ISO)
	}
	if !ignoreAperture && before.FNumber > 0 && after.FNumber > 0 {
		delta -= math.Log2(before.FNumber * before.FNumber / (after.FNumber * after.FNumber))
	}
	return delta / 4
}

func normalizedHolyGrailInputCurve(values []float64) []float64 {
	result := make([]float64, len(values))
	if len(values) == 0 {
		return result
	}
	minimum, maximum := values[0], values[0]
	for _, value := range values[1:] {
		minimum = math.Min(minimum, value)
		maximum = math.Max(maximum, value)
	}
	span := maximum - minimum
	if span <= 1e-12 {
		return result
	}
	for index, value := range values {
		result[index] = (value-minimum)/span - 0.5
	}
	return result
}

func buildHolyGrailLRTCompensations(inputs []float64, ratings []int, cameras []CameraExposure, options HolyGrailCurveOptions, ignoreAperture bool) ([]float64, error) {
	if len(inputs) != len(cameras) {
		return nil, fmt.Errorf("Holy Grail input and camera frame counts differ")
	}
	negated := make([]float64, len(inputs))
	for index, value := range inputs {
		negated[index] = -value
	}
	curve, err := interpolateHolyGrailAnchors(negated, ratings)
	if err != nil {
		return nil, err
	}
	curve = centerAndClipHolyGrailCurve(curve, 10)
	for index := range curve {
		curve[index] *= 2
	}
	accumulatedPairDelta := 0.0
	for index := range curve {
		if index > 0 && lrtHolyGrailAnchor(ratings[index-1]) && lrtHolyGrailAnchor(ratings[index]) {
			accumulatedPairDelta += holyGrailLRTPairDelta(cameras[index-1], cameras[index], ignoreAperture)
		}
		curve[index] += accumulatedPairDelta
	}
	curve = centerAndClipHolyGrailCurve(curve, 10)

	normalized := normalizedHolyGrailInputCurve(negated)
	for index := range curve {
		trend := float64(index)/float64(len(curve)) - 0.5
		curve[index] += normalized[index]*options.Stretch + trend*options.Rotate
	}
	if len(curve) > 2 {
		// The LRT helper uses a fixed 0.5 smoothing factor here. The local
		// regression window is an approximation of its irregular-x Pia path.
		neighbors := int(math.Round(0.5 * float64(len(curve)-1)))
		if neighbors < 2 {
			neighbors = 2
		}
		curve = lowessSmooth(curve, neighbors)
	}
	return centerAndClipHolyGrailCurve(curve, 1), nil
}

func optimizeHolyGrailLRTCurve(inputs []float64, ratings []int, cameras []CameraExposure, ignoreAperture bool) HolyGrailCurveOptions {
	options := HolyGrailCurveOptions{LRTInput: true}
	if len(inputs) < 2 {
		return options
	}
	neighbors := int(math.Round(0.5 * float64(len(inputs)-1)))
	if neighbors < 2 {
		neighbors = 2
	}
	bestScore := math.Inf(1)
	for step := 1; step <= 150; step++ {
		candidate := float64(step)/50 - 1.5
		curve, err := buildHolyGrailLRTCompensations(inputs, ratings, cameras, HolyGrailCurveOptions{Stretch: candidate, LRTInput: true}, ignoreAperture)
		if err != nil {
			return options
		}
		smoothed := lowessSmooth(curve, neighbors)
		score := 0.0
		for index := range curve {
			score += math.Abs(curve[index] - smoothed[index])
		}
		if score < bestScore {
			bestScore = score
			options.Stretch = candidate
		}
	}

	bestScore = math.Inf(1)
	for step := 1; step <= 200; step++ {
		candidate := float64(step)/25 - 4
		curve, err := buildHolyGrailLRTCompensations(inputs, ratings, cameras, HolyGrailCurveOptions{Stretch: options.Stretch, Rotate: candidate, LRTInput: true}, ignoreAperture)
		if err != nil {
			return options
		}
		smoothed := lowessSmooth(curve, neighbors)
		score := 0.0
		for _, value := range smoothed {
			score += math.Abs(value)
		}
		if score < bestScore {
			bestScore = score
			options.Rotate = candidate
		}
	}
	return options
}

func holyGrailKeyframePlan(packet *XMPPacket, desiredRating int) (int, bool, string, error) {
	kind, hasKind, err := packet.GetScalar(holyGrailKeyframeProperty)
	if err != nil {
		return 0, false, "", err
	}
	previous, hasPrevious, err := packet.GetScalar(holyGrailPreviousRatingProperty)
	if err != nil {
		return 0, false, "", err
	}
	ratingText, hasRating, err := packet.GetScalar("xmp:Rating")
	if err != nil {
		return 0, false, "", err
	}
	if hasKind {
		oldRating, valid := scalarNumber(kind)
		if !valid || (oldRating != 2 && oldRating != 3) || !hasPrevious {
			return 0, false, "", fmt.Errorf("invalid lrtl Holy Grail keyframe marker")
		}
		if !hasRating || ratingText != kind {
			return 0, false, "", fmt.Errorf("Holy Grail keyframe marker was changed outside lrtl; restore or remove %s first", holyGrailKeyframeProperty)
		}
		if desiredRating == int(oldRating) {
			return 0, false, "", nil
		}
		if desiredRating == 0 {
			return 0, true, previous, nil
		}
		return desiredRating, false, previous, nil
	}
	if hasPrevious {
		return 0, false, "", fmt.Errorf("orphaned lrtl property %s", holyGrailPreviousRatingProperty)
	}
	if desiredRating == 0 {
		return 0, false, "", nil
	}
	if marked, err := packet.HasProperty("ol:KeyframeKind"); err != nil {
		return 0, false, "", err
	} else if marked {
		return 0, false, "", fmt.Errorf("cannot place an HG marker over an existing regular keyframe")
	}
	if hasRating {
		rating, valid := scalarNumber(ratingText)
		if valid && rating == float64(desiredRating) {
			return 0, false, "", nil
		}
		return 0, false, "", fmt.Errorf("cannot mark an HG %d-star keyframe over existing xmp:Rating=%s", desiredRating, ratingText)
	}
	return desiredRating, false, absentRating, nil
}

func buildHolyGrailPlan(frames []Frame, ignoreAperture, reset, forceMarkJumps, noMarkJumps bool) (*HolyGrailPlan, error) {
	return buildHolyGrailPlanWithCurveOptions(frames, ignoreAperture, reset, forceMarkJumps, noMarkJumps, nil)
}

func buildHolyGrailPlanWithCurveOptions(frames []Frame, ignoreAperture, reset, forceMarkJumps, noMarkJumps bool, curveOptions *HolyGrailCurveOptions) (*HolyGrailPlan, error) {
	plan := &HolyGrailPlan{}
	if len(frames) == 0 {
		return nil, fmt.Errorf("sequence contains no images")
	}
	cameras := make([]CameraExposure, len(frames))
	lrtInputMode := curveOptions != nil && curveOptions.LRTInput
	var lrtInputs []float64
	var lrtRatings []int
	var err error
	if !reset {
		for index, frame := range frames {
			camera, err := readCameraExposure(frame.Image, ignoreAperture)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", filepath.Base(frame.Image), err)
			}
			cameras[index] = camera
		}
		if lrtInputMode {
			lrtInputs, lrtRatings, err = readHolyGrailLRTInputs(frames)
			if err != nil {
				return nil, err
			}
		} else {
			plan.ManualMode = len(cameras) > 0
			for _, camera := range cameras {
				if !camera.Manual {
					plan.ManualMode = false
					break
				}
			}
			plan.MarkJumps = !noMarkJumps && (forceMarkJumps || plan.ManualMode)
		}
	}
	if !reset && curveOptions != nil {
		resolvedOptions := *curveOptions
		if resolvedOptions.Auto {
			if lrtInputMode {
				resolvedOptions = optimizeHolyGrailLRTCurve(lrtInputs, lrtRatings, cameras, ignoreAperture)
			} else {
				resolvedOptions = optimizeHolyGrailCurve(cameras)
			}
			plan.AutoCurve = true
		}
		resolvedOptions.LRTInput = lrtInputMode
		resolvedOptions.Auto = false
		plan.CurveOptions = &resolvedOptions
		curveOptions = plan.CurveOptions
	}
	base := 0.0
	compensations := make([]float64, len(frames))
	if !reset {
		if lrtInputMode {
			compensations, err = buildHolyGrailLRTCompensations(lrtInputs, lrtRatings, cameras, *curveOptions, ignoreAperture)
			if err != nil {
				return nil, err
			}
		} else {
			base = cameras[0].Stops
			for index, camera := range cameras {
				compensations[index] = base - camera.Stops
			}
			if curveOptions != nil {
				adjustments := holyGrailCurveAdjustments(cameras, *curveOptions)
				for index := range compensations {
					compensations[index] += adjustments[index]
				}
			}
		}
	}
	jumpRatings := make([]int, len(frames))
	if !reset && plan.MarkJumps {
		for index := 1; index < len(frames); index++ {
			if !cameraSettingsChanged(cameras[index-1], cameras[index], ignoreAperture) {
				continue
			}
			if jumpRatings[index-1] != 0 || jumpRatings[index] != 0 {
				return nil, fmt.Errorf("camera exposure changes are too close to mark adjacent 2*/3* pairs near %s; use --no-mark-jumps for this sequence", filepath.Base(frames[index].Image))
			}
			jumpRatings[index-1], jumpRatings[index] = 2, 3
		}
	}

	changes := make([]HolyGrailChange, 0, len(frames))
	for index, frame := range frames {
		packet, err := PacketForSidecar(frame.OutputSidecar())
		if err != nil {
			return nil, fmt.Errorf("%s: %w", frame.OutputSidecar(), err)
		}
		previousText, hadPrevious, err := packet.GetScalar(holyGrailCorrectionProperty)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", frame.OutputSidecar(), err)
		}
		previous := 0.0
		if hadPrevious {
			previous, _ = scalarNumber(previousText)
			if _, valid := scalarNumber(previousText); !valid {
				return nil, fmt.Errorf("%s: %s is not numeric", frame.OutputSidecar(), holyGrailCorrectionProperty)
			}
		}
		previousTargetText, hadPreviousTarget, err := packet.GetScalar(holyGrailTargetProperty)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", frame.OutputSidecar(), err)
		}
		if hadPreviousTarget && !hadPrevious {
			return nil, fmt.Errorf("%s: orphaned lrtl property %s", frame.OutputSidecar(), holyGrailTargetProperty)
		}
		previousTarget := globalHolyGrailTarget()
		if hadPreviousTarget {
			previousTarget, err = parseHolyGrailXMPTarget(previousTargetText)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", frame.OutputSidecar(), err)
			}
		}
		target := previousTarget
		if !reset {
			target = findHolyGrailXMPTarget(packet)
		}
		previousTargetValue, _, err := getHolyGrailTargetValue(packet, previousTarget)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", frame.OutputSidecar(), err)
		}
		previousBase := previousTargetValue - previous
		compensation := 0.0
		if !reset {
			compensation = compensations[index]
		}
		before := previousBase
		if !hadPrevious || target != previousTarget {
			before, _, err = getHolyGrailTargetValue(packet, target)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", frame.OutputSidecar(), err)
			}
		}
		after := before + compensation
		writes := make([]holyGrailXMPWrite, 0, 2)
		if hadPrevious {
			if reset || target != previousTarget {
				writes = append(writes, holyGrailXMPWrite{Target: previousTarget, Value: previousBase})
			}
		}
		if !reset && ((!hadPrevious && math.Abs(compensation) > 1e-12) ||
			(hadPrevious && (target != previousTarget || math.Abs(compensation-previous) > 1e-12))) {
			writes = append(writes, holyGrailXMPWrite{Target: target, Value: after})
		}
		exposureChanged := len(writes) > 0
		keyframeRating, restoreKeyframe, previousRating := 0, false, ""
		if reset {
			keyframeRating, restoreKeyframe, previousRating, err = holyGrailKeyframePlan(packet, 0)
		} else if plan.MarkJumps {
			keyframeRating, restoreKeyframe, previousRating, err = holyGrailKeyframePlan(packet, jumpRatings[index])
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", frame.OutputSidecar(), err)
		}
		if !exposureChanged && keyframeRating == 0 && !restoreKeyframe {
			continue
		}
		changes = append(changes, HolyGrailChange{
			Frame: frame, Packet: packet, Camera: cameras[index],
			Target: target, XMPWrites: writes,
			Compensation: compensation, ExposureBefore: before, ExposureAfter: after,
			ExposureChanged:  exposureChanged,
			RemoveCorrection: reset || math.Abs(compensation) < 1e-12,
			KeyframeRating:   keyframeRating, RestoreKeyframe: restoreKeyframe, PreviousRating: previousRating,
		})
	}
	plan.Changes = changes
	return plan, nil
}

func applyHolyGrailPlan(changes []HolyGrailChange) error {
	for _, change := range changes {
		if change.ExposureChanged {
			for _, write := range change.XMPWrites {
				if err := setHolyGrailTargetValue(change.Packet, write.Target, write.Value); err != nil {
					return fmt.Errorf("%s: %w", change.Frame.OutputSidecar(), err)
				}
			}
			if change.RemoveCorrection {
				if err := change.Packet.RemoveProperty(holyGrailCorrectionProperty); err != nil {
					return fmt.Errorf("%s: %w", change.Frame.OutputSidecar(), err)
				}
			} else if err := change.Packet.SetScalar(holyGrailCorrectionProperty, formatNumber(change.Compensation)); err != nil {
				return fmt.Errorf("%s: %w", change.Frame.OutputSidecar(), err)
			}
			if change.RemoveCorrection {
				if err := change.Packet.RemoveProperty(holyGrailTargetProperty); err != nil {
					return fmt.Errorf("%s: %w", change.Frame.OutputSidecar(), err)
				}
			} else if err := change.Packet.SetScalar(holyGrailTargetProperty, change.Target.String()); err != nil {
				return fmt.Errorf("%s: %w", change.Frame.OutputSidecar(), err)
			}
		}
		if change.RestoreKeyframe {
			if change.PreviousRating == absentRating {
				if err := change.Packet.RemoveProperty("xmp:Rating"); err != nil {
					return fmt.Errorf("%s: %w", change.Frame.OutputSidecar(), err)
				}
			} else if err := change.Packet.SetScalar("xmp:Rating", change.PreviousRating); err != nil {
				return fmt.Errorf("%s: %w", change.Frame.OutputSidecar(), err)
			}
			if err := change.Packet.RemoveProperty(holyGrailKeyframeProperty); err != nil {
				return fmt.Errorf("%s: %w", change.Frame.OutputSidecar(), err)
			}
			if err := change.Packet.RemoveProperty(holyGrailPreviousRatingProperty); err != nil {
				return fmt.Errorf("%s: %w", change.Frame.OutputSidecar(), err)
			}
		}
		if change.KeyframeRating != 0 {
			if err := change.Packet.SetScalar("xmp:Rating", strconv.Itoa(change.KeyframeRating)); err != nil {
				return fmt.Errorf("%s: %w", change.Frame.OutputSidecar(), err)
			}
			if err := change.Packet.SetScalar(holyGrailKeyframeProperty, strconv.Itoa(change.KeyframeRating)); err != nil {
				return fmt.Errorf("%s: %w", change.Frame.OutputSidecar(), err)
			}
			if err := change.Packet.SetScalar(holyGrailPreviousRatingProperty, change.PreviousRating); err != nil {
				return fmt.Errorf("%s: %w", change.Frame.OutputSidecar(), err)
			}
		}
	}
	return nil
}

func readXMPExposure(frame Frame) (float64, error) {
	packet, err := PacketForSidecar(frame.OutputSidecar())
	if err != nil {
		return 0, err
	}
	value, found, err := packet.GetScalar("crs:Exposure2012")
	if err != nil {
		return 0, err
	}
	exposure := 0.0
	if found {
		var valid bool
		exposure, valid = scalarNumber(value)
		if !valid || math.Abs(exposure) > maxRenderXMPExposureStops {
			return 0, fmt.Errorf("crs:Exposure2012 must be a finite number between -%d and +%d stops", maxRenderXMPExposureStops, maxRenderXMPExposureStops)
		}
	}
	holyGrailCorrection, _, err := readHolyGrailRenderCorrection(packet)
	if err != nil {
		return 0, err
	}
	exposure += holyGrailCorrection
	if math.Abs(exposure) > maxRenderXMPExposureStops {
		return 0, fmt.Errorf("combined XMP exposure must be between -%d and +%d stops", maxRenderXMPExposureStops, maxRenderXMPExposureStops)
	}
	return exposure, nil
}
