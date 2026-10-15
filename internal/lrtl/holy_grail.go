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
const holyGrailKeyframeProperty = "ol:HolyGrailKeyframeKind"
const holyGrailPreviousRatingProperty = "ol:HolyGrailPreviousRating"
const maxRenderXMPExposureStops = 32

type CameraExposure struct {
	ExposureTime float64
	FNumber      float64
	ISO          float64
	Stops        float64
	Manual       bool
}

type HolyGrailCurveOptions struct {
	Stretch float64
	Rotate  float64
	Auto    bool
}

type HolyGrailChange struct {
	Frame            Frame
	Packet           *XMPPacket
	Camera           CameraExposure
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
	baseSmooth := smoothLogLuminance(base, 3)
	normalizedSmooth := smoothLogLuminance(normalized, 3)
	trendSmooth := smoothLogLuminance(trend, 3)

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
	if !reset {
		for index, frame := range frames {
			camera, err := readCameraExposure(frame.Image, ignoreAperture)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", filepath.Base(frame.Image), err)
			}
			cameras[index] = camera
		}
		plan.ManualMode = len(cameras) > 0
		for _, camera := range cameras {
			if !camera.Manual {
				plan.ManualMode = false
				break
			}
		}
		plan.MarkJumps = !noMarkJumps && (forceMarkJumps || plan.ManualMode)
	}
	if !reset && curveOptions != nil {
		resolvedOptions := *curveOptions
		if resolvedOptions.Auto {
			resolvedOptions = optimizeHolyGrailCurve(cameras)
			plan.AutoCurve = true
		}
		resolvedOptions.Auto = false
		plan.CurveOptions = &resolvedOptions
		curveOptions = plan.CurveOptions
	}
	base := 0.0
	compensations := make([]float64, len(frames))
	if !reset {
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
		currentText, found, err := packet.GetScalar("crs:Exposure2012")
		if err != nil {
			return nil, fmt.Errorf("%s: %w", frame.OutputSidecar(), err)
		}
		current := 0.0
		if found {
			current, _ = scalarNumber(currentText)
			if _, valid := scalarNumber(currentText); !valid {
				return nil, fmt.Errorf("%s: crs:Exposure2012 is not numeric", frame.OutputSidecar())
			}
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
		before := current - previous
		compensation := 0.0
		if !reset {
			compensation = compensations[index]
		}
		after := before + compensation
		exposureChanged := math.Abs(compensation-previous) > 1e-12
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
			if err := change.Packet.SetScalar("crs:Exposure2012", formatNumber(change.ExposureAfter)); err != nil {
				return fmt.Errorf("%s: %w", change.Frame.OutputSidecar(), err)
			}
			if change.RemoveCorrection {
				if err := change.Packet.RemoveProperty(holyGrailCorrectionProperty); err != nil {
					return fmt.Errorf("%s: %w", change.Frame.OutputSidecar(), err)
				}
			} else if err := change.Packet.SetScalar(holyGrailCorrectionProperty, formatNumber(change.Compensation)); err != nil {
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
	if !found {
		return 0, nil
	}
	exposure, valid := scalarNumber(value)
	if !valid || math.Abs(exposure) > maxRenderXMPExposureStops {
		return 0, fmt.Errorf("crs:Exposure2012 must be a finite number between -%d and +%d stops", maxRenderXMPExposureStops, maxRenderXMPExposureStops)
	}
	return exposure, nil
}
