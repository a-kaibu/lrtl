package lrtl

import (
	"bytes"
	"fmt"
	"image"
	"math"
)

const deflickerCorrectionProperty = "ol:DeflickerCorrection2012"

type DeflickerChange struct {
	Frame            Frame
	Packet           *XMPPacket
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
		} else if bounds.Dx() != width || bounds.Dy() != height {
			return nil, fmt.Errorf("inconsistent dimensions in %s", frame.Image)
		}
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
	if len(frames) == 0 {
		return nil, fmt.Errorf("sequence contains no images")
	}
	if !reset && len(luminances) != len(frames) {
		return nil, fmt.Errorf("expected %d luminance samples, got %d", len(frames), len(luminances))
	}
	corrections := make([]float64, len(frames))
	if !reset {
		gains, err := deflickerGains(luminances, smoothingRadius, passes, constant)
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

	changes := make([]DeflickerChange, 0, len(frames))
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
			current, found = scalarNumber(currentText)
			if !found {
				return nil, fmt.Errorf("%s: crs:Exposure2012 is not numeric", frame.OutputSidecar())
			}
		}
		previousText, hasPrevious, err := packet.GetScalar(deflickerCorrectionProperty)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", frame.OutputSidecar(), err)
		}
		previous := 0.0
		if hasPrevious {
			previous, found = scalarNumber(previousText)
			if !found {
				return nil, fmt.Errorf("%s: %s is not numeric", frame.OutputSidecar(), deflickerCorrectionProperty)
			}
		}
		if reset && !hasPrevious {
			continue
		}

		correction := corrections[index]
		before := current - previous
		after := before + correction
		changed := math.Abs(correction-previous) > 1e-12
		if !changed {
			continue
		}
		if math.IsNaN(before) || math.IsInf(before, 0) || math.IsNaN(after) || math.IsInf(after, 0) {
			return nil, fmt.Errorf("%s: deflicker would produce a non-finite Exposure2012 value", frame.OutputSidecar())
		}
		change := DeflickerChange{
			Frame: frame, Packet: packet, Correction: correction,
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
		if err := change.Packet.SetScalar("crs:Exposure2012", formatNumber(change.ExposureAfter)); err != nil {
			return fmt.Errorf("%s: %w", change.Frame.OutputSidecar(), err)
		}
		if change.RemoveCorrection {
			if err := change.Packet.RemoveProperty(deflickerCorrectionProperty); err != nil {
				return fmt.Errorf("%s: %w", change.Frame.OutputSidecar(), err)
			}
		} else if err := change.Packet.SetScalar(deflickerCorrectionProperty, formatNumber(change.Correction)); err != nil {
			return fmt.Errorf("%s: %w", change.Frame.OutputSidecar(), err)
		}
	}
	return nil
}
