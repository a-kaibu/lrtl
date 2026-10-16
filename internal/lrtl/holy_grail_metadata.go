package lrtl

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

func parseHolyGrailCameraNumber(text string) (float64, error) {
	label := text
	reciprocal := strings.HasPrefix(label, "1/")
	if reciprocal {
		label = label[2:]
	}
	value, found := lrtHolyGrailNumberLabels[label]
	if !found {
		var err error
		value, err = strconv.ParseFloat(strings.TrimSpace(label), 64)
		if err != nil {
			return 0, fmt.Errorf("invalid camera number %q", text)
		}
	}
	if reciprocal {
		value = 1 / value
	}
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, fmt.Errorf("camera number %q must be finite", text)
	}
	return value, nil
}

func parseHolyGrailAperture(text string) (float64, error) {
	if value, found := lrtHolyGrailApertureLabels[text]; found {
		return value, nil
	}
	value, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return 0, fmt.Errorf("invalid aperture %q", text)
	}
	return value, nil
}

// The installed frame model reads these three strings from the LRT namespace.
// Complete cached metadata can supply the camera settings without image EXIF.
func readHolyGrailLRTCameraExposure(frame Frame, ignoreAperture bool) (CameraExposure, error) {
	packet, err := PacketForFrame(frame)
	if err != nil {
		return CameraExposure{}, err
	}
	timeText, hasTime, err := packet.GetScalar("lrt:ShutterSpeed")
	if err != nil {
		return CameraExposure{}, err
	}
	isoText, hasISO, err := packet.GetScalar("lrt:Iso")
	if err != nil {
		return CameraExposure{}, err
	}
	apertureText, hasAperture := "", false
	if !ignoreAperture {
		apertureText, hasAperture, err = packet.GetScalar("lrt:Aperture")
		if err != nil {
			return CameraExposure{}, err
		}
	}
	camera := CameraExposure{}
	if !hasTime || !hasISO || (!ignoreAperture && !hasAperture) {
		camera, err = readCameraExposureMetadata(frame)
		if err != nil {
			return CameraExposure{}, err
		}
	}
	if hasTime {
		camera.ExposureTime, err = parseHolyGrailCameraNumber(timeText)
		if err != nil {
			return CameraExposure{}, fmt.Errorf("lrt:ShutterSpeed: %w", err)
		}
	}
	if hasISO {
		value, parseErr := parseHolyGrailCameraNumber(isoText)
		if parseErr != nil {
			return CameraExposure{}, fmt.Errorf("lrt:Iso: %w", parseErr)
		}
		camera.ISO = holyGrailLRTIntISO(value)
		camera.lrtISOParsed = true
	}
	if hasAperture {
		camera.FNumber, err = parseHolyGrailAperture(apertureText)
		if err != nil {
			return CameraExposure{}, fmt.Errorf("lrt:Aperture: %w", err)
		}
	}
	if camera.ExposureTime <= 0 || math.IsNaN(camera.ExposureTime) || math.IsInf(camera.ExposureTime, 0) {
		return CameraExposure{}, fmt.Errorf("positive shutter speed is required in lrt:ShutterSpeed or EXIF")
	}
	if !hasISO && (camera.ISO <= 0 || math.IsNaN(camera.ISO) || math.IsInf(camera.ISO, 0)) {
		return CameraExposure{}, fmt.Errorf("ISO is required in lrt:Iso or EXIF")
	}
	if !ignoreAperture && !hasAperture && (camera.FNumber <= 0 || math.IsNaN(camera.FNumber) || math.IsInf(camera.FNumber, 0)) {
		return CameraExposure{}, fmt.Errorf("aperture is required in lrt:Aperture or EXIF; use --ignore-aperture for fixed aperture")
	}
	if camera.ISO > 0 {
		camera.Stops = math.Log2(camera.ExposureTime) + math.Log2(camera.ISO)
		if !ignoreAperture && camera.FNumber > 0 {
			camera.Stops -= 2 * math.Log2(camera.FNumber)
		}
	}
	return camera, nil
}
