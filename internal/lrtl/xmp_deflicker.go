package lrtl

import (
	"bytes"
	"fmt"
	"image"
	"math"
)

const deflickerCorrectionProperty = "ol:DeflickerCorrection2012"
const deflickerTargetProperty = "ol:DeflickerXMPTarget"

type DeflickerChange struct {
	Frame            Frame
	Packet           *XMPPacket
	Target           xmpExposureTarget
	XMPWrites        []xmpExposureWrite
	Luminance        float64
	Correction       float64
	ExposureBefore   float64
	ExposureAfter    float64
	ExposureChanged  bool
	RemoveCorrection bool
}

func analyzeDeflickerSequence(frames []Frame, reference image.Rectangle, useRawPreview bool) ([]float64, error) {
	return analyzeDeflickerSequenceWithOptions(frames, reference, imageInputOptions{RawPreview: useRawPreview})
}

func analyzeDeflickerSequenceWithOptions(frames []Frame, reference image.Rectangle, options imageInputOptions) ([]float64, error) {
	if len(frames) == 0 {
		return nil, fmt.Errorf("sequence contains no images")
	}
	if err := validateImageInputOptions(options); err != nil {
		return nil, err
	}
	luminances := make([]float64, len(frames))
	width, height := 0, 0
	var adjustments sequenceXMPAdjustments
	for index, frame := range frames {
		data, _, _, err := readRenderInputWithOptions(frame, options)
		if err != nil {
			return nil, fmt.Errorf("deflicker analysis: %w", err)
		}
		img, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("decode %s: %w", frame.Image, err)
		}
		bounds := img.Bounds()
		if bounds.Empty() {
			return nil, fmt.Errorf("invalid image dimensions in %s", frame.Image)
		}
		if index == 0 {
			width, height = bounds.Dx(), bounds.Dy()
			adjustments, err = readSequenceXMPAdjustments(frames, width, height, options)
			if err != nil {
				return nil, err
			}
		} else if bounds.Dx() != width || bounds.Dy() != height {
			return nil, fmt.Errorf("inconsistent dimensions in %s", frame.Image)
		}
		img = adjustments.apply(index, img)
		bounds = img.Bounds()
		area := reference
		if area.Empty() {
			area = bounds
		} else {
			area = area.Add(bounds.Min)
		}
		luminances[index], err = frameLuminance(img, area)
		if err != nil {
			return nil, fmt.Errorf("analyze %s: %w", frame.Image, err)
		}
	}
	return luminances, nil
}

func buildDeflickerChanges(frames []Frame, luminances []float64, smoothingRadius, passes int, constant, reset bool) ([]DeflickerChange, error) {
	return buildDeflickerChangesWithSmoother(frames, luminances, smoothingRadius, passes, constant, reset, "gaussian")
}

func buildDeflickerChangesWithSmoother(frames []Frame, luminances []float64, smoothingRadius, passes int, constant, reset bool, smoother string) ([]DeflickerChange, error) {
	return buildDeflickerChangesForInput(frames, luminances, smoothingRadius, passes, constant, reset, smoother, false)
}

// Developed input already contains our previous correction. Add the measured
// residual in that case; measurements of untouched input replace the old delta.
func buildDeflickerChangesForInput(frames []Frame, luminances []float64, smoothingRadius, passes int, constant, reset bool, smoother string, includesCorrection bool) ([]DeflickerChange, error) {
	return buildDeflickerChangesFromPackets(frames, luminances, smoothingRadius, passes, constant, reset, smoother, includesCorrection, nil)
}

func buildDeflickerChangesFromPackets(frames []Frame, luminances []float64, smoothingRadius, passes int, constant, reset bool, smoother string, includesCorrection bool, packets []*XMPPacket) ([]DeflickerChange, error) {
	if packets != nil && len(packets) != len(frames) {
		return nil, fmt.Errorf("deflicker packet count does not match sequence")
	}
	if len(frames) == 0 {
		return nil, fmt.Errorf("sequence contains no images")
	}
	if !reset && len(luminances) != len(frames) {
		return nil, fmt.Errorf("expected %d luminance samples, got %d", len(frames), len(luminances))
	}
	corrections := make([]float64, len(frames))
	if !reset {
		gains, err := deflickerGainsWithSmoother(luminances, smoothingRadius, passes, constant, smoother)
		if err != nil {
			return nil, err
		}
		for index, gain := range gains {
			corrections[index] = math.Log2(gain)
			if math.IsNaN(corrections[index]) || math.IsInf(corrections[index], 0) || math.Abs(corrections[index]) > maxRenderXMPExposureStops {
				return nil, fmt.Errorf("frame %d deflicker correction exceeds the supported ±%d stops", index+1, maxRenderXMPExposureStops)
			}
		}
	}

	return buildDeflickerCorrectionChanges(frames, luminances, corrections, reset, includesCorrection, packets)
}

func buildDeflickerCorrectionChanges(frames []Frame, luminances, corrections []float64, reset, includesCorrection bool, packets []*XMPPacket) ([]DeflickerChange, error) {
	if len(frames) == 0 || len(corrections) != len(frames) || !reset && len(luminances) != len(frames) || packets != nil && len(packets) != len(frames) {
		return nil, fmt.Errorf("invalid Deflicker correction plan dimensions")
	}
	for i, value := range corrections {
		if math.IsNaN(value) || math.IsInf(value, 0) || math.Abs(value) > maxRenderXMPExposureStops {
			return nil, fmt.Errorf("invalid Deflicker correction at frame %d", i+1)
		}
	}
	changes := make([]DeflickerChange, 0, len(frames))
	for index, frame := range frames {
		var packet *XMPPacket
		var err error
		if packets != nil {
			packet = packets[index]
		} else {
			packet, err = PacketForFrame(frame)
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", frame.OutputSidecar(), err)
		}
		previousText, hasPrevious, err := packet.GetScalar(deflickerCorrectionProperty)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", frame.OutputSidecar(), err)
		}
		previous := 0.0
		if hasPrevious {
			var valid bool
			previous, valid = scalarNumber(previousText)
			if !valid || math.Abs(previous) > maxRenderXMPExposureStops {
				return nil, fmt.Errorf("%s: %s is not numeric", frame.OutputSidecar(), deflickerCorrectionProperty)
			}
		}
		previousTargetText, hasTarget, err := packet.GetScalar(deflickerTargetProperty)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", frame.OutputSidecar(), err)
		}
		if hasTarget && !hasPrevious {
			return nil, fmt.Errorf("%s: orphaned lrtl property %s", frame.OutputSidecar(), deflickerTargetProperty)
		}
		previousTarget := globalXMPExposureTarget()
		if hasTarget {
			previousTarget, err = parseXMPExposureTarget(previousTargetText)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", frame.OutputSidecar(), err)
			}
		}
		if reset && !hasPrevious {
			continue
		}
		target := previousTarget
		if !reset {
			target = findLRTExposureTarget(packet, "Deflicker", 6)
		}
		before := 0.0
		writes := make([]xmpExposureWrite, 0, 2)
		if hasPrevious {
			current, _, err := getXMPExposureTargetValue(packet, previousTarget)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", frame.OutputSidecar(), err)
			}
			before = current - previous
			if reset || target != previousTarget {
				writes = append(writes, xmpExposureWrite{Target: previousTarget, Value: before})
			}
		}
		if !hasPrevious || target != previousTarget {
			before, _, err = getXMPExposureTargetValue(packet, target)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", frame.OutputSidecar(), err)
			}
		}
		correction := corrections[index]
		if !reset && correction != 0 {
			response, err := deflickerTargetResponse(packet, target)
			if err != nil {
				return nil, err
			}
			if response == 0 {
				return nil, fmt.Errorf("%s: Deflicker target is inactive or has zero CorrectionAmount", frame.OutputSidecar())
			}
			correction /= response
		}
		if includesCorrection && !reset {
			correction += previous
		}
		if math.Abs(correction) > maxRenderXMPExposureStops {
			return nil, fmt.Errorf("%s: accumulated deflicker correction exceeds supported range", frame.OutputSidecar())
		}
		after := before + correction
		if !reset && (math.Abs(correction-previous) > 1e-12 || (hasPrevious && target != previousTarget)) {
			writes = append(writes, xmpExposureWrite{Target: target, Value: after})
		}
		if len(writes) == 0 {
			continue
		}
		if math.IsNaN(before) || math.IsInf(before, 0) || math.IsNaN(after) || math.IsInf(after, 0) {
			return nil, fmt.Errorf("%s: deflicker would produce a non-finite Exposure2012 value", frame.OutputSidecar())
		}
		change := DeflickerChange{
			Frame: frame, Packet: packet, Correction: correction, Target: target, XMPWrites: writes,
			ExposureBefore: before, ExposureAfter: after,
			ExposureChanged: true, RemoveCorrection: reset || math.Abs(correction) < 1e-12,
		}
		if !reset {
			change.Luminance = luminances[index]
		}
		changes = append(changes, change)
	}
	return changes, nil
}

func applyDeflickerChanges(changes []DeflickerChange) error {
	for _, change := range changes {
		for _, write := range change.XMPWrites {
			if err := setXMPExposureTargetValue(change.Packet, write.Target, write.Value); err != nil {
				return fmt.Errorf("%s: %w", change.Frame.OutputSidecar(), err)
			}
		}
		if change.RemoveCorrection {
			if err := change.Packet.RemoveProperty(deflickerCorrectionProperty); err != nil {
				return fmt.Errorf("%s: %w", change.Frame.OutputSidecar(), err)
			}
			if err := change.Packet.RemoveProperty(deflickerTargetProperty); err != nil {
				return err
			}
		} else if err := change.Packet.SetScalar(deflickerCorrectionProperty, formatNumber(change.Correction)); err != nil {
			return fmt.Errorf("%s: %w", change.Frame.OutputSidecar(), err)
		} else if err := change.Packet.SetScalar(deflickerTargetProperty, change.Target.String()); err != nil {
			return fmt.Errorf("%s: %w", change.Frame.OutputSidecar(), err)
		}
	}
	return nil
}
