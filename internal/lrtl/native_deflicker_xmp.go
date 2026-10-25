package lrtl

import (
	"fmt"
	"math"
)

func readDeflickerTracking(packet *XMPPacket) (float64, xmpExposureTarget, error) {
	text, found, err := packet.GetRootScalar(deflickerCorrectionProperty)
	if err != nil {
		return 0, xmpExposureTarget{}, err
	}
	targetText, hasTarget, err := packet.GetRootScalar(deflickerTargetProperty)
	if err != nil {
		return 0, xmpExposureTarget{}, err
	}
	if hasTarget && !found {
		return 0, xmpExposureTarget{}, fmt.Errorf("orphaned Deflicker tracking target")
	}
	target := globalXMPExposureTarget()
	if hasTarget {
		target, err = parseXMPExposureTarget(targetText)
		if err != nil {
			return 0, target, err
		}
	}
	if !found {
		return 0, target, nil
	}
	value, valid := scalarNumber(text)
	if !valid || math.Abs(value) > maxRenderXMPExposureStops {
		return 0, target, fmt.Errorf("invalid Deflicker tracking correction")
	}
	return value, target, nil
}

func trackedDeflickerExposure(packet *XMPPacket) (float64, error) {
	value, target, err := readDeflickerTracking(packet)
	if err != nil {
		return 0, err
	}
	if value == 0 {
		return 0, nil
	}
	response, err := deflickerTargetResponse(packet, target)
	return value * response, err
}

func buildNativeDeflickerChanges(frames []Frame, measured []float64, packets []*XMPPacket, options nativeDeflickerOptions, constant bool, pass int) ([]DeflickerChange, error) {
	if len(frames) != len(measured) || len(frames) != len(packets) {
		return nil, fmt.Errorf("native Deflicker frame/measurement/packet count mismatch")
	}
	current := make([]float64, len(frames))
	responses := make([]float64, len(frames))
	for i, packet := range packets {
		previous, oldTarget, err := readDeflickerTracking(packet)
		if err != nil {
			return nil, err
		}
		target := findLRTExposureTarget(packet, "Deflicker", 6)
		if target.Collection == "" {
			// Global fallback keeps user Exposure2012 outside the native model.
			current[i] = previous
		} else {
			current[i], _, err = getXMPExposureTargetValue(packet, target)
			if err != nil {
				return nil, err
			}
			if oldTarget != target {
				current[i] += previous
			}
		}
		responses[i], err = deflickerTargetResponse(packet, target)
		if err != nil {
			return nil, err
		}
	}
	result, err := calculateNativeDeflickerPass(measured, current, options.Smoothing, constant, options.Accuracy, pass)
	if err != nil {
		return nil, err
	}
	deltas := make([]float64, len(frames))
	for i, value := range result.Correction {
		delta := value - current[i]
		if delta != 0 && responses[i] == 0 {
			return nil, fmt.Errorf("%s: native Deflicker target is inactive or has zero CorrectionAmount", frames[i].Image)
		}
		// The shared planner converts effective exposure by CorrectionAmount.
		// Native correction units are the stored slot value, so cancel that scale.
		deltas[i] = delta * responses[i]
	}
	return buildDeflickerCorrectionChanges(frames, measured, deltas, false, true, packets)
}
