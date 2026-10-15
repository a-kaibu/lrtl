package lrtl

import (
	"fmt"
	"math"
	"os"
	"path/filepath"

	"github.com/evanoberholster/imagemeta"
)

const holyGrailCorrectionProperty = "ol:HolyGrailCorrection2012"

type CameraExposure struct {
	ExposureTime float64
	FNumber      float64
	ISO          float64
	Stops        float64
}

type HolyGrailChange struct {
	Frame            Frame
	Packet           *XMPPacket
	Camera           CameraExposure
	Compensation     float64
	ExposureBefore   float64
	ExposureAfter    float64
	RemoveCorrection bool
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
	if iso <= 0 {
		return CameraExposure{}, fmt.Errorf("EXIF ISO speed is missing or invalid")
	}
	if !ignoreAperture && (fNumber <= 0 || math.IsNaN(fNumber) || math.IsInf(fNumber, 0)) {
		return CameraExposure{}, fmt.Errorf("EXIF FNumber is missing or invalid; use --ignore-aperture if aperture stayed fixed")
	}
	setting := math.Log2(exposureTime * iso)
	if !ignoreAperture {
		setting -= 2 * math.Log2(fNumber)
	}
	return CameraExposure{ExposureTime: exposureTime, FNumber: fNumber, ISO: iso, Stops: setting}, nil
}

func buildHolyGrailPlan(frames []Frame, ignoreAperture, reset bool) ([]HolyGrailChange, error) {
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
	}
	base := 0.0
	if !reset {
		base = cameras[0].Stops
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
			compensation = base - cameras[index].Stops
		}
		after := before + compensation
		changed := math.Abs(compensation-previous) > 1e-12
		if !changed {
			continue
		}
		changes = append(changes, HolyGrailChange{
			Frame: frame, Packet: packet, Camera: cameras[index],
			Compensation: compensation, ExposureBefore: before, ExposureAfter: after,
			RemoveCorrection: reset || math.Abs(compensation) < 1e-12,
		})
	}
	return changes, nil
}

func applyHolyGrailPlan(changes []HolyGrailChange) error {
	for _, change := range changes {
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
	return nil
}
