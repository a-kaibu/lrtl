package lrtl

import (
	"fmt"
	"image"
	"math"
	"strconv"
	"strings"

	xdraw "golang.org/x/image/draw"
)

type cropSpec struct {
	Bounds image.Rectangle
	Active bool
}

func readXMPCrops(frames []Frame, width, height int) ([]cropSpec, error) {
	crops := make([]cropSpec, len(frames))
	full := image.Rect(0, 0, width, height)
	for index, frame := range frames {
		packet, err := PacketForSidecar(frame.OutputSidecar())
		if err != nil {
			return nil, fmt.Errorf("read crop settings for %s: %w", frame.Image, err)
		}
		active, found, err := packet.GetScalar("crs:HasCrop")
		if err != nil {
			return nil, fmt.Errorf("read crop flag for %s: %w", frame.Image, err)
		}
		cropEnabled := false
		if found {
			cropEnabled, err = parseCameraRawBool(active)
			if err != nil {
				return nil, fmt.Errorf("%s has invalid crs:HasCrop value %q", frame.Image, active)
			}
		} else {
			// Some older sidecars omit HasCrop. Treat non-default bounds as an
			// active crop, while keeping packets with only template defaults intact.
			for _, property := range []struct {
				name         string
				defaultValue float64
			}{
				{name: "CropTop", defaultValue: 0},
				{name: "CropLeft", defaultValue: 0},
				{name: "CropBottom", defaultValue: 1},
				{name: "CropRight", defaultValue: 1},
			} {
				value, exists, getErr := packet.GetScalar("crs:" + property.name)
				if getErr != nil {
					return nil, fmt.Errorf("read %s for %s: %w", property.name, frame.Image, getErr)
				}
				if exists {
					number, valid := scalarNumber(value)
					if !valid {
						return nil, fmt.Errorf("%s has invalid crs:%s value %q", frame.Image, property.name, value)
					}
					if math.Abs(number-property.defaultValue) > 1e-12 {
						cropEnabled = true
					}
				}
			}
		}
		if !cropEnabled {
			crops[index] = cropSpec{Bounds: full}
			continue
		}

		angleText, angleFound, err := packet.GetScalar("crs:CropAngle")
		if err != nil {
			return nil, fmt.Errorf("read crop angle for %s: %w", frame.Image, err)
		}
		if angleFound {
			angle, valid := scalarNumber(angleText)
			if !valid {
				return nil, fmt.Errorf("%s has invalid crs:CropAngle value %q", frame.Image, angleText)
			}
			if math.Abs(angle) > 1e-9 {
				return nil, fmt.Errorf("%s uses crs:CropAngle=%s; render currently supports axis-aligned XMP crops only", frame.Image, angleText)
			}
		}

		values := []struct {
			name         string
			defaultValue float64
			pixels       int
		}{
			{name: "CropTop", defaultValue: 0, pixels: height},
			{name: "CropLeft", defaultValue: 0, pixels: width},
			{name: "CropBottom", defaultValue: 1, pixels: height},
			{name: "CropRight", defaultValue: 1, pixels: width},
		}
		coordinates := make(map[string]int, len(values))
		for _, property := range values {
			text, exists, getErr := packet.GetScalar("crs:" + property.name)
			if getErr != nil {
				return nil, fmt.Errorf("read %s for %s: %w", property.name, frame.Image, getErr)
			}
			value := property.defaultValue
			if exists {
				var valid bool
				value, valid = scalarNumber(text)
				if !valid {
					return nil, fmt.Errorf("%s has invalid crs:%s value %q", frame.Image, property.name, text)
				}
			}
			if value < 0 || value > 1 {
				return nil, fmt.Errorf("%s crs:%s must be between 0 and 1, got %s", frame.Image, property.name, strconv.FormatFloat(value, 'g', -1, 64))
			}
			coordinates[property.name] = int(math.Round(value * float64(property.pixels)))
		}
		bounds := image.Rect(coordinates["CropLeft"], coordinates["CropTop"], coordinates["CropRight"], coordinates["CropBottom"])
		if bounds.Empty() || bounds.Dx() < 1 || bounds.Dy() < 1 {
			return nil, fmt.Errorf("%s has an empty Camera Raw crop rectangle", frame.Image)
		}
		crops[index] = cropSpec{Bounds: bounds, Active: true}
	}
	return crops, nil
}

func parseCameraRawBool(value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "1":
		return true, nil
	case "false", "0":
		return false, nil
	default:
		return false, fmt.Errorf("invalid boolean %q", value)
	}
}

func applyXMPCrop(source image.Image, crop cropSpec) image.Image {
	if !crop.Active || crop.Bounds == source.Bounds() {
		return source
	}
	result := image.NewRGBA(image.Rect(0, 0, source.Bounds().Dx(), source.Bounds().Dy()))
	xdraw.CatmullRom.Scale(result, result.Bounds(), source, crop.Bounds, xdraw.Src, nil)
	return result
}
