package lrtl

import (
	"bytes"
	"fmt"
	"image"
	"math"
	"sort"
)

type deflickerSequencePlan struct {
	Changes      []DeflickerChange
	Measurements [][]float64
}

func pixelFeedbackGains(count, radius, passes int, constant bool, smoother string, measure func([]float64) ([]float64, error)) ([]float64, error) {
	if count < 1 {
		return nil, fmt.Errorf("cannot Deflicker an empty sequence")
	}
	if _, err := deflickerGainsWithSmoother([]float64{1}, radius, passes, constant, smoother); err != nil {
		return nil, err
	}
	gains := make([]float64, count)
	for i := range gains {
		gains[i] = 1
	}
	for pass := 0; pass < passes; pass++ {
		luminances, err := measure(gains)
		if err != nil {
			return nil, err
		}
		if len(luminances) != count {
			return nil, fmt.Errorf("expected %d feedback measurements, got %d", count, len(luminances))
		}
		deltas, err := deflickerGainsWithSmoother(luminances, radius, 1, constant, smoother)
		if err != nil {
			return nil, err
		}
		for i := range gains {
			gains[i] *= deltas[i]
			if gains[i] <= 0 || math.IsNaN(gains[i]) || math.IsInf(gains[i], 0) {
				return nil, fmt.Errorf("invalid cumulative Deflicker gain for frame %d", i+1)
			}
		}
	}
	return gains, nil
}

func deflickerTargetResponse(packet *XMPPacket, target xmpExposureTarget) (float64, error) {
	if target.Collection == "" {
		return 1, nil
	}
	element, err := xmpExposureTargetElement(packet, target)
	if err != nil {
		return 0, err
	}
	active, _, err := xmpLocalBool(element, "CorrectionActive", true)
	if err != nil {
		return 0, err
	}
	if !active {
		return 0, nil
	}
	amount, _, err := xmpLocalNumber(element, "CorrectionAmount", 1, -10, 10)
	return amount, err
}

func measureDeflickerPackets(frames []Frame, reference image.Rectangle, options imageInputOptions, packets []*XMPPacket, source *frameInputCache, feedback []float64) ([]float64, error) {
	values := make([]float64, len(frames))
	width, height := 0, 0
	var adjustments sequenceXMPAdjustments
	for i, frame := range frames {
		data, _, _, err := source.read(i, frame)
		if err != nil {
			return nil, err
		}
		img, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("decode %s: %w", frame.Image, err)
		}
		if i == 0 {
			width, height = img.Bounds().Dx(), img.Bounds().Dy()
			adjustments, err = readSequenceXMPAdjustmentsFromPackets(frames, width, height, options, packets)
			if err != nil {
				return nil, err
			}
		} else if img.Bounds().Dx() != width || img.Bounds().Dy() != height {
			return nil, fmt.Errorf("inconsistent dimensions in %s", frame.Image)
		}
		img = adjustments.apply(i, img)
		if feedback != nil && feedback[i] != 1 {
			img = applyExposureGainFrame(img, feedback[i])
		}
		area := reference
		if area.Empty() {
			area = img.Bounds()
		} else {
			area = area.Add(img.Bounds().Min)
		}
		values[i], err = frameLuminance(img, area)
		if err != nil {
			return nil, fmt.Errorf("analyze %s: %w", frame.Image, err)
		}
	}
	return values, nil
}

func planDeflickerSequence(frames []Frame, reference image.Rectangle, options imageInputOptions, radius, passes int, constant bool, smoother string) (*deflickerSequencePlan, error) {
	if len(frames) == 0 {
		return nil, fmt.Errorf("sequence contains no images")
	}
	if err := validateImageInputOptions(options); err != nil {
		return nil, err
	}
	if _, err := deflickerGainsWithSmoother([]float64{1}, radius, passes, constant, smoother); err != nil {
		return nil, err
	}
	packets := make([]*XMPPacket, len(frames))
	indicesByPacket := make(map[*XMPPacket]int, len(frames))
	original := make([][]byte, len(frames))
	for i, frame := range frames {
		packet, err := PacketForFrame(frame)
		if err != nil {
			return nil, err
		}
		original[i], err = packet.Serialize()
		if err != nil {
			return nil, err
		}
		packets[i], err = parseXMP(original[i])
		if err != nil {
			return nil, err
		}
		indicesByPacket[packets[i]] = i
	}
	source := newFrameInputCache(options, passes > 1)
	defer source.close()
	plan := &deflickerSequencePlan{}
	summary := make(map[int]DeflickerChange)
	writes := make(map[int]map[xmpExposureTarget]float64)
	includesXMP := options.ApplyXMPExposure || options.ApplyXMPBasic
	for pass := 0; pass < passes; pass++ {
		var feedback []float64
		if !includesXMP && pass > 0 {
			feedback = make([]float64, len(frames))
			for i, packet := range packets {
				feedback[i] = 1
				text, found, err := packet.GetScalar(deflickerCorrectionProperty)
				if err != nil {
					return nil, err
				}
				if !found {
					continue
				}
				delta, valid := scalarNumber(text)
				if !valid {
					return nil, fmt.Errorf("invalid deflicker tracking")
				}
				targetText, found, err := packet.GetScalar(deflickerTargetProperty)
				if err != nil {
					return nil, err
				}
				if !found {
					targetText = "global"
				}
				target, err := parseXMPExposureTarget(targetText)
				if err != nil {
					return nil, err
				}
				response, err := deflickerTargetResponse(packet, target)
				if err != nil {
					return nil, err
				}
				feedback[i] = math.Exp2(delta * response)
			}
		}
		measurements, err := measureDeflickerPackets(frames, reference, options, packets, source, feedback)
		if err != nil {
			return nil, err
		}
		plan.Measurements = append(plan.Measurements, measurements)
		changes, err := buildDeflickerChangesFromPackets(frames, measurements, radius, 1, constant, false, smoother, includesXMP || pass > 0, packets)
		if err != nil {
			return nil, err
		}
		if len(changes) == 0 {
			break
		}
		for _, change := range changes {
			index := indicesByPacket[change.Packet]
			if old, found := summary[index]; found {
				change.ExposureBefore = old.ExposureBefore
			}
			change.Luminance = plan.Measurements[0][index]
			summary[index] = change
			if writes[index] == nil {
				writes[index] = make(map[xmpExposureTarget]float64)
			}
			for _, write := range change.XMPWrites {
				writes[index][write.Target] = write.Value
			}
		}
		if err := applyDeflickerChanges(changes); err != nil {
			return nil, err
		}
	}
	indices := make([]int, 0, len(summary))
	for i := range summary {
		indices = append(indices, i)
	}
	sort.Ints(indices)
	for _, i := range indices {
		final, err := packets[i].Serialize()
		if err != nil {
			return nil, err
		}
		if bytes.Equal(original[i], final) {
			continue
		}
		change := summary[i]
		change.Packet = packets[i]
		change.XMPWrites = nil
		targets := make([]xmpExposureTarget, 0, len(writes[i]))
		for target := range writes[i] {
			targets = append(targets, target)
		}
		sort.Slice(targets, func(a, b int) bool { return targets[a].String() < targets[b].String() })
		for _, target := range targets {
			change.XMPWrites = append(change.XMPWrites, xmpExposureWrite{target, writes[i][target]})
		}
		plan.Changes = append(plan.Changes, change)
	}
	return plan, nil
}
