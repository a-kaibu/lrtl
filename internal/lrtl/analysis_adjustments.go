package lrtl

import (
	"fmt"
	"image"
	"math"
)

type sequenceXMPAdjustments struct {
	exposures []float64
	basics    []xmpBasicAdjustments
	crops     []cropSpec
}

func readSequenceXMPAdjustments(frames []Frame, width, height int, options imageInputOptions) (sequenceXMPAdjustments, error) {
	adjustments := sequenceXMPAdjustments{}
	if options.ApplyXMPExposure {
		adjustments.exposures = make([]float64, len(frames))
		for index, frame := range frames {
			exposure, err := readXMPExposure(frame)
			if err != nil {
				return sequenceXMPAdjustments{}, fmt.Errorf("%s: %w", frame.Image, err)
			}
			adjustments.exposures[index] = exposure
		}
	}
	if options.ApplyXMPBasic {
		adjustments.basics = make([]xmpBasicAdjustments, len(frames))
		for index, frame := range frames {
			basic, err := readXMPBasicAdjustments(frame)
			if err != nil {
				return sequenceXMPAdjustments{}, fmt.Errorf("%s: %w", frame.Image, err)
			}
			adjustments.basics[index] = basic
		}
	}
	if options.ApplyXMPCrop {
		crops, err := readXMPCrops(frames, width, height)
		if err != nil {
			return sequenceXMPAdjustments{}, err
		}
		adjustments.crops = crops
	}
	return adjustments, nil
}

func (adjustments sequenceXMPAdjustments) apply(index int, img image.Image) image.Image {
	if adjustments.basics != nil {
		img = applyXMPLocalCorrections(img, adjustments.basics[index].local)
	}
	if adjustments.crops != nil && adjustments.crops[index].Active {
		img = applyXMPCrop(img, adjustments.crops[index])
	}
	if adjustments.basics != nil && adjustments.basics[index].globalActive() {
		img = applyXMPBasic(img, adjustments.basics[index])
	} else if adjustments.exposures != nil && adjustments.exposures[index] != 0 {
		img = applyExposureGain(img, math.Exp2(adjustments.exposures[index]))
	}
	return img
}
