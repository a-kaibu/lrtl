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
	return readSequenceXMPAdjustmentsFromPackets(frames, width, height, options, nil)
}

func readSequenceXMPAdjustmentsFromPackets(frames []Frame, width, height int, options imageInputOptions, packets []*XMPPacket) (sequenceXMPAdjustments, error) {
	if packets != nil && len(packets) != len(frames) {
		return sequenceXMPAdjustments{}, fmt.Errorf("adjustment packet count does not match sequence")
	}
	adjustments := sequenceXMPAdjustments{}
	if options.ApplyXMPExposure {
		adjustments.exposures = make([]float64, len(frames))
		for index, frame := range frames {
			var exposure float64
			var err error
			if packets != nil {
				exposure, err = readXMPExposurePacket(packets[index])
			} else {
				exposure, err = readXMPExposure(frame)
			}
			if err != nil {
				return sequenceXMPAdjustments{}, fmt.Errorf("%s: %w", frame.Image, err)
			}
			adjustments.exposures[index] = exposure
		}
	}
	if options.ApplyXMPBasic {
		adjustments.basics = make([]xmpBasicAdjustments, len(frames))
		for index, frame := range frames {
			var basic xmpBasicAdjustments
			var err error
			if packets != nil {
				basic, err = readXMPBasicPacket(packets[index])
			} else {
				basic, err = readXMPBasicAdjustments(frame)
			}
			if err != nil {
				return sequenceXMPAdjustments{}, fmt.Errorf("%s: %w", frame.Image, err)
			}
			adjustments.basics[index] = basic
		}
	}
	if options.ApplyXMPCrop {
		crops, err := readXMPCropsFromPackets(frames, width, height, packets)
		if err != nil {
			return sequenceXMPAdjustments{}, err
		}
		adjustments.crops = crops
	}
	return adjustments, nil
}

func (adjustments sequenceXMPAdjustments) apply(index int, img image.Image) image.Image {
	if adjustments.basics != nil {
		img = applyXMPSpatialCorrections(img, adjustments.basics[index])
	}
	if adjustments.crops != nil && adjustments.crops[index].Active {
		img = applyXMPCrop(img, adjustments.crops[index])
	}
	if adjustments.basics != nil && adjustments.basics[index].globalActive() {
		img = applyXMPBasicFrame(img, adjustments.basics[index])
	} else if adjustments.exposures != nil && adjustments.exposures[index] != 0 {
		img = applyExposureGainFrame(img, math.Exp2(adjustments.exposures[index]))
	}
	return img
}
