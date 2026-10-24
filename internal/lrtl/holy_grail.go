package lrtl

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"

	"github.com/evanoberholster/imagemeta"
)

const holyGrailCorrectionProperty = "ol:HolyGrailCorrection2012"
const holyGrailTargetProperty = "ol:HolyGrailXMPTarget"
const holyGrailKeyframeProperty = "ol:HolyGrailKeyframeKind"
const holyGrailPreviousRatingProperty = "ol:HolyGrailPreviousRating"
const legacyLRTInternalMaskName = "LRT: don't alter!"
const maxRenderXMPExposureStops = 32

type CameraExposure struct {
	ExposureTime float64
	FNumber      float64
	ISO          float64
	Stops        float64
	Manual       bool
	lrtISOParsed bool
}

type HolyGrailCurveOptions struct {
	Stretch         float64
	Rotate          float64
	ReduceDeflicker float64
	Auto            bool
	LRTInput        bool
}

type HolyGrailChange struct {
	Frame            Frame
	Packet           *XMPPacket
	Camera           CameraExposure
	Target           xmpExposureTarget
	XMPWrites        []xmpExposureWrite
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

func readCameraExposureMetadata(frame Frame) (CameraExposure, error) {
	file, err := os.Open(frame.Image)
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
	manual := uint16(metadata.ExposureMode) == 1 || uint16(metadata.ExposureProgram) == 1
	return CameraExposure{ExposureTime: exposureTime, FNumber: fNumber, ISO: iso, Manual: manual}, nil
}

func readCameraExposure(frame Frame, ignoreAperture bool) (CameraExposure, error) {
	camera, err := readCameraExposureMetadata(frame)
	if err != nil {
		return CameraExposure{}, err
	}
	exposureTime, fNumber, iso := camera.ExposureTime, camera.FNumber, camera.ISO
	if !ignoreAperture && (fNumber <= 0 || math.IsNaN(fNumber) || math.IsInf(fNumber, 0)) {
		packet, err := PacketForFrame(frame)
		if err != nil {
			return CameraExposure{}, fmt.Errorf("read XMP aperture: %w", err)
		}
		apertureText, found, err := packet.GetScalar("lrt:Aperture")
		if err != nil {
			return CameraExposure{}, fmt.Errorf("read lrt:Aperture: %w", err)
		}
		if found {
			aperture, valid := scalarNumber(apertureText)
			if !valid || aperture <= 0 {
				return CameraExposure{}, fmt.Errorf("lrt:Aperture is present but invalid")
			}
			fNumber = aperture
		}
	}
	if exposureTime <= 0 || math.IsNaN(exposureTime) || math.IsInf(exposureTime, 0) {
		return CameraExposure{}, fmt.Errorf("EXIF ExposureTime is missing or invalid")
	}
	if iso <= 0 || math.IsNaN(iso) || math.IsInf(iso, 0) {
		return CameraExposure{}, fmt.Errorf("EXIF ISO speed is missing or invalid")
	}
	if !ignoreAperture && (fNumber <= 0 || math.IsNaN(fNumber) || math.IsInf(fNumber, 0)) {
		return CameraExposure{}, fmt.Errorf("EXIF FNumber and XMP lrt:Aperture are missing or invalid; use lrtl lens-tag to set a manual aperture or --ignore-aperture if it stayed fixed")
	}
	setting := math.Log2(exposureTime * iso)
	if !ignoreAperture {
		setting -= 2 * math.Log2(fNumber)
	}
	if math.IsNaN(setting) || math.IsInf(setting, 0) {
		return CameraExposure{}, fmt.Errorf("EXIF exposure settings produce an invalid exposure value")
	}
	return CameraExposure{ExposureTime: exposureTime, FNumber: fNumber, ISO: iso, Stops: setting, Manual: camera.Manual}, nil
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
		packet, err := PacketForFrame(frame)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", frame.OutputSidecar(), err)
		}
		text, found, err := packet.GetScalar("lrt:ExternalExposureDefault")
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", frame.OutputSidecar(), err)
		}
		if !found {
			return nil, nil, fmt.Errorf("%s: missing scalar lrt:ExternalExposureDefault; run lrtl preview-ev DIRECTORY --write or supply existing LRT XMP values", frame.OutputSidecar())
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

func lrtHolyGrailPairEligible(rating int) bool {
	return rating == 2 || rating == 3 || rating == 4
}

func weightedHolyGrailRecentAverage(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	weightTotal := float64(len(values) * (len(values) + 1) / 2)
	weightedTotal := 0.0
	for index, value := range values {
		weightedTotal += float64(index+1) * value
	}
	return weightedTotal / weightTotal
}

func lrtHolyGrailLowess(values []float64, smoothing float64) []float64 {
	result := append([]float64(nil), values...)
	if len(values) < 3 || smoothing <= 0 {
		return result
	}
	neighbors := int(smoothing * float64(len(values)))
	if smoothing > 1 {
		// LRT retries an out-of-range bandwidth after dividing it by the
		// rejected value plus a float32 0.001 promoted to double. This is
		// nearly n-1 for short sequences, but differs on long sequences.
		bandwidth := smoothing / (smoothing + float64(float32(0.001)))
		neighbors = int(bandwidth * float64(len(values)))
	}
	if neighbors < 2 {
		return result
	}
	if neighbors > len(values) {
		neighbors = len(values)
	}
	return lowessSmooth(values, neighbors)
}

func centerAndClipHolyGrailCurve(values []float64, limit float64) []float64 {
	return recenterHolyGrailCurve(values, -limit, limit, true)
}

func recenterHolyGrailCurve(values []float64, lower, upper float64, clip bool) []float64 {
	result := append([]float64(nil), values...)
	maximum, minimum := lower-1, upper+1
	for _, value := range result {
		if value > maximum {
			maximum = value
		}
		if value < minimum {
			minimum = value
		}
	}
	// Preserve the installed helper's sentinel extrema and operation order.
	// They differ from a midpoint when every value lies beyond a sentinel.
	span := maximum - minimum
	shift := (2-span)/2 - minimum - 1
	for index := range result {
		result[index] += shift
		if clip {
			if result[index] > upper {
				result[index] = upper
			}
			if result[index] < lower {
				result[index] = lower
			}
		}
	}
	return result
}

func holyGrailLRTPairDelta(before, after CameraExposure, ignoreAperture bool) float64 {
	if before.ExposureTime <= 0 || after.ExposureTime <= 0 {
		return 0
	}
	delta := math.Log2(before.ExposureTime / after.ExposureTime)
	if before.ISO > 0 && after.ISO > 0 {
		delta -= math.Log2(after.ISO / before.ISO)
	}
	if !ignoreAperture && before.FNumber > 0 && after.FNumber > 0 {
		delta -= math.Log2(before.FNumber * before.FNumber / (after.FNumber * after.FNumber))
	}
	return delta / 4
}

// The installed shape helper uses the original Preview EV input, correcting
// only adjacent 2/3-star pairs before normalization. Four-star pairs participate
// in other HG stages, but do not activate or adjust this shape curve.
func lrtHolyGrailShapeCurve(values []float64, ratings []int, stretch, rotate float64) []float64 {
	result := make([]float64, len(values))
	if len(values) == 0 || len(values) != len(ratings) {
		return result
	}
	adjusted := make([]float64, len(values))
	adjusted[0] = values[0]
	minimum, maximum := 100.0, -100.0
	offset, previousDelta := 0.0, 0.0
	hasPair := false
	eligible := func(rating int) bool { return rating == 2 || rating == 3 }
	for i := 1; i < len(values); i++ {
		if eligible(ratings[i-1]) && eligible(ratings[i]) {
			offset += values[i-1] - values[i] - previousDelta
			hasPair = true
		} else {
			previousDelta = values[i-1] - values[i]
		}
		adjusted[i] = offset + values[i]
		minimum = math.Min(minimum, adjusted[i])
		maximum = math.Max(maximum, adjusted[i])
	}
	span := maximum - minimum
	// A degenerate native normalization can produce NaN. Keep lrtl's finite
	// zero-shape policy for that case rather than publishing invalid XMP.
	if !hasPair || span == 0 {
		return result
	}
	for i, value := range adjusted {
		trend := float64(float32(i)/float32(len(values)) - float32(0.5))
		result[i] = ((value-minimum)/span-0.5)*stretch + trend*rotate
	}
	return result
}

func buildHolyGrailLRTCompensations(inputs []float64, ratings []int, cameras []CameraExposure, options HolyGrailCurveOptions, ignoreAperture bool) ([]float64, error) {
	return buildHolyGrailLRTCurve(inputs, ratings, cameras, options, ignoreAperture, true)
}

func buildHolyGrailLRTCurve(inputs []float64, ratings []int, cameras []CameraExposure, options HolyGrailCurveOptions, ignoreAperture, clip bool) ([]float64, error) {
	// The installed controller receives these three controls as Java float.
	// In particular .35f * 20 selects six LOWESS neighbors, not seven.
	options.Stretch = float64(float32(options.Stretch))
	options.Rotate = float64(float32(options.Rotate))
	options.ReduceDeflicker = float64(float32(options.ReduceDeflicker))
	if len(inputs) != len(cameras) || len(inputs) != len(ratings) {
		return nil, fmt.Errorf("Holy Grail input, rating, and camera frame counts differ")
	}
	negated := make([]float64, len(inputs))
	for index, value := range inputs {
		negated[index] = -value
	}
	curve := make([]float64, len(inputs))
	recentDeltas := make([]float64, 0, 10)
	cumulativeOffset := 0.0
	for index, value := range negated {
		if index > 0 && lrtHolyGrailPairEligible(ratings[index-1]) && lrtHolyGrailPairEligible(ratings[index]) {
			delta := value - curve[index-1]
			cumulativeOffset = -delta + weightedHolyGrailRecentAverage(recentDeltas)
		}
		curve[index] = value + cumulativeOffset
		if index > 0 {
			recentDeltas = append(recentDeltas, curve[index]-curve[index-1])
			if len(recentDeltas) > 10 {
				recentDeltas = recentDeltas[1:]
			}
		}
	}
	curve = lrtHolyGrailLowess(curve, options.ReduceDeflicker)
	curve = recenterHolyGrailCurve(curve, -10, 10, clip)
	for index := range curve {
		curve[index] *= 2
	}
	accumulatedPairDelta := 0.0
	for index := range curve {
		if index > 0 && lrtHolyGrailPairEligible(ratings[index-1]) && lrtHolyGrailPairEligible(ratings[index]) {
			accumulatedPairDelta += holyGrailLRTFramePairDelta(cameras[index-1], cameras[index], ignoreAperture)
		}
		curve[index] += accumulatedPairDelta
	}
	curve = recenterHolyGrailCurve(curve, -10, 10, clip)

	shape := lrtHolyGrailShapeCurve(inputs, ratings, options.Stretch, options.Rotate)
	shape = lrtHolyGrailLowess(shape, 0.5)
	for index := range curve {
		curve[index] += shape[index]
	}
	return recenterHolyGrailCurve(curve, -1, 1, clip), nil
}

func optimizeHolyGrailLRTCurve(inputs []float64, ratings []int, cameras []CameraExposure, ignoreAperture bool, reduceDeflicker float64) HolyGrailCurveOptions {
	options := HolyGrailCurveOptions{LRTInput: true, ReduceDeflicker: reduceDeflicker}
	if len(inputs) < 2 {
		return options
	}
	bestScore := math.Inf(1)
	for step := 1; step <= 150; step++ {
		candidate := float64(float32(step)/50 - float32(1.5))
		curve, err := buildHolyGrailLRTCurve(inputs, ratings, cameras, HolyGrailCurveOptions{Stretch: candidate, ReduceDeflicker: reduceDeflicker, LRTInput: true}, ignoreAperture, false)
		if err != nil {
			return options
		}
		smoothed := lrtHolyGrailLowess(curve, 1.5)
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
		candidate := float64(float32(step)/25 - float32(4))
		curve, err := buildHolyGrailLRTCurve(inputs, ratings, cameras, HolyGrailCurveOptions{Stretch: options.Stretch, Rotate: candidate, ReduceDeflicker: reduceDeflicker, LRTInput: true}, ignoreAperture, false)
		if err != nil {
			return options
		}
		smoothed := lrtHolyGrailLowess(curve, 1.5)
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
	kind, hasKind, err := captureTimestampScalar(packet, holyGrailKeyframeProperty)
	if err != nil {
		return 0, false, "", err
	}
	previous, hasPrevious, err := captureTimestampScalar(packet, holyGrailPreviousRatingProperty)
	if err != nil {
		return 0, false, "", err
	}
	ratingText, hasRating, err := readRootRating(packet)
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
	if marked, err := hasRootMetadataProperty(packet, "ol:KeyframeKind"); err != nil {
		return 0, false, "", err
	} else if marked {
		return 0, false, "", fmt.Errorf("cannot place an HG marker over an existing regular keyframe")
	}
	if hasRating {
		rating, valid := scalarNumber(ratingText)
		if valid && rating == float64(desiredRating) {
			return 0, false, "", nil
		}
		if valid && rating == 0 {
			// Native initialization writes an explicit zero for unrated frames.
			// Keep its spelling so removing our marker restores the original.
			return desiredRating, false, ratingText, nil
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
		readExposure := readCameraExposure
		if lrtInputMode {
			readExposure = readHolyGrailLRTCameraExposure
		}
		for index, frame := range frames {
			camera, err := readExposure(frame, ignoreAperture)
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
				resolvedOptions = optimizeHolyGrailLRTCurve(lrtInputs, lrtRatings, cameras, ignoreAperture, resolvedOptions.ReduceDeflicker)
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
		packet, err := PacketForFrame(frame)
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
		previousTarget := globalXMPExposureTarget()
		if hadPreviousTarget {
			previousTarget, err = parseXMPExposureTarget(previousTargetText)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", frame.OutputSidecar(), err)
			}
		}
		target := previousTarget
		if !reset {
			target = findHolyGrailXMPTarget(packet)
		}
		previousTargetValue, _, err := getXMPExposureTargetValue(packet, previousTarget)
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
			before, _, err = getXMPExposureTargetValue(packet, target)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", frame.OutputSidecar(), err)
			}
		}
		after := before + compensation
		writes := make([]xmpExposureWrite, 0, 2)
		if hadPrevious {
			if reset || target != previousTarget {
				writes = append(writes, xmpExposureWrite{Target: previousTarget, Value: previousBase})
			}
		}
		if !reset && ((!hadPrevious && math.Abs(compensation) > 1e-12) ||
			(hadPrevious && (target != previousTarget || math.Abs(compensation-previous) > 1e-12))) {
			writes = append(writes, xmpExposureWrite{Target: target, Value: after})
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
				if err := setXMPExposureTargetValue(change.Packet, write.Target, write.Value); err != nil {
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
				if err := removeRootMetadataProperty(change.Packet, "xmp:Rating"); err != nil {
					return fmt.Errorf("%s: %w", change.Frame.OutputSidecar(), err)
				}
			} else if err := setCaptureTimestampScalar(change.Packet, "xmp:Rating", change.PreviousRating); err != nil {
				return fmt.Errorf("%s: %w", change.Frame.OutputSidecar(), err)
			}
			if err := removeRootMetadataProperty(change.Packet, holyGrailKeyframeProperty); err != nil {
				return fmt.Errorf("%s: %w", change.Frame.OutputSidecar(), err)
			}
			if err := removeRootMetadataProperty(change.Packet, holyGrailPreviousRatingProperty); err != nil {
				return fmt.Errorf("%s: %w", change.Frame.OutputSidecar(), err)
			}
		}
		if change.KeyframeRating != 0 {
			if err := setCaptureTimestampScalar(change.Packet, "xmp:Rating", strconv.Itoa(change.KeyframeRating)); err != nil {
				return fmt.Errorf("%s: %w", change.Frame.OutputSidecar(), err)
			}
			if err := setCaptureTimestampScalar(change.Packet, holyGrailKeyframeProperty, strconv.Itoa(change.KeyframeRating)); err != nil {
				return fmt.Errorf("%s: %w", change.Frame.OutputSidecar(), err)
			}
			if err := setCaptureTimestampScalar(change.Packet, holyGrailPreviousRatingProperty, change.PreviousRating); err != nil {
				return fmt.Errorf("%s: %w", change.Frame.OutputSidecar(), err)
			}
		}
	}
	return nil
}

func readXMPExposure(frame Frame) (float64, error) {
	packet, err := PacketForFrame(frame)
	if err != nil {
		return 0, err
	}
	return readXMPExposurePacket(packet)
}

func readXMPExposurePacket(packet *XMPPacket) (float64, error) {
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
	deflickerCorrection, _, err := readDeflickerRenderCorrection(packet)
	if err != nil {
		return 0, err
	}
	exposure += deflickerCorrection
	if math.Abs(exposure) > maxRenderXMPExposureStops {
		return 0, fmt.Errorf("combined XMP exposure must be between -%d and +%d stops", maxRenderXMPExposureStops, maxRenderXMPExposureStops)
	}
	return exposure, nil
}
