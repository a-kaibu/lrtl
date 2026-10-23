package lrtl

import (
	"fmt"
	"image"
	"math"
	"strconv"
	"strings"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/math/f64"
)

type cropSpec struct {
	Bounds                   image.Rectangle
	Left, Top, Right, Bottom float64
	CenterX, CenterY         float64
	Angle                    float64
	Active                   bool
}

func readXMPCrops(frames []Frame, width, height int) ([]cropSpec, error) {
	return readXMPCropsFromPackets(frames, width, height, nil)
}

func readXMPCropsFromPackets(frames []Frame, width, height int, packets []*XMPPacket) ([]cropSpec, error) {
	if packets != nil && len(packets) != len(frames) {
		return nil, fmt.Errorf("crop packet count does not match sequence")
	}
	crops := make([]cropSpec, len(frames))
	full := image.Rect(0, 0, width, height)
	for index, frame := range frames {
		var packet *XMPPacket
		var err error
		if packets != nil {
			packet = packets[index]
		} else {
			packet, err = PacketForFrame(frame)
		}
		if err != nil {
			return nil, fmt.Errorf("read crop settings for %s: %w", frame.Image, err)
		}
		active, found, err := packet.GetRootScalar("crs:HasCrop")
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
				{name: "CropAngle", defaultValue: 0},
			} {
				value, exists, getErr := packet.GetRootScalar("crs:" + property.name)
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
			crops[index] = cropSpec{Bounds: full, Right: float64(width), Bottom: float64(height), CenterX: float64(width) / 2, CenterY: float64(height) / 2}
			continue
		}

		angleText, angleFound, err := packet.GetRootScalar("crs:CropAngle")
		if err != nil {
			return nil, fmt.Errorf("read crop angle for %s: %w", frame.Image, err)
		}
		angle := 0.0
		if angleFound {
			var valid bool
			angle, valid = scalarNumber(angleText)
			if !valid {
				return nil, fmt.Errorf("%s has invalid crs:CropAngle value %q", frame.Image, angleText)
			}
			// Camera Raw stores positive crop angles clockwise. Normalize so that
			// trigonometric calculations remain stable for unusual XMP values.
			angle = math.Remainder(angle, 360)
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
		coordinates := make(map[string]float64, len(values))
		for _, property := range values {
			text, exists, getErr := packet.GetRootScalar("crs:" + property.name)
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
			coordinates[property.name] = value * float64(property.pixels)
		}
		left, top := coordinates["CropLeft"], coordinates["CropTop"]
		right, bottom := coordinates["CropRight"], coordinates["CropBottom"]
		if right-left < 1 || bottom-top < 1 {
			return nil, fmt.Errorf("%s has an empty Camera Raw crop rectangle", frame.Image)
		}
		centerX, centerY := (left+right)/2, (top+bottom)/2
		cropLeft, cropTop, cropRight, cropBottom := left, top, right, bottom
		if math.Abs(angle) > 1e-9 {
			radians := angle * math.Pi / 180
			cosine, sine := math.Cos(radians), math.Sin(radians)
			rotateToLevel := func(x, y float64) (float64, float64) {
				dx, dy := x-centerX, y-centerY
				return centerX + cosine*dx + sine*dy, centerY - sine*dx + cosine*dy
			}
			x1, y1 := rotateToLevel(left, top)
			x2, y2 := rotateToLevel(right, bottom)
			cropLeft, cropRight = math.Min(x1, x2), math.Max(x1, x2)
			cropTop, cropBottom = math.Min(y1, y2), math.Max(y1, y2)
			if cropRight-cropLeft < 1 || cropBottom-cropTop < 1 {
				return nil, fmt.Errorf("%s has an empty rotated Camera Raw crop rectangle", frame.Image)
			}
		}
		bounds := image.Rect(int(math.Round(left)), int(math.Round(top)), int(math.Round(right)), int(math.Round(bottom)))
		if bounds.Empty() {
			return nil, fmt.Errorf("%s has an empty Camera Raw crop rectangle", frame.Image)
		}
		crops[index] = cropSpec{
			Bounds: bounds, Left: cropLeft, Top: cropTop, Right: cropRight, Bottom: cropBottom,
			CenterX: centerX, CenterY: centerY, Angle: angle, Active: true,
		}
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
	if !crop.Active {
		return source
	}
	outputBounds := image.Rect(0, 0, source.Bounds().Dx(), source.Bounds().Dy())
	if math.Abs(crop.Angle) < 1e-9 {
		if crop.Bounds == source.Bounds() || crop.Bounds == outputBounds {
			return source
		}
		result := newFrameImage(outputBounds, source)
		xdraw.CatmullRom.Scale(result, outputBounds, source, crop.Bounds, xdraw.Src, nil)
		return result
	}

	result := newFrameImage(outputBounds, source)
	width, height := float64(outputBounds.Dx()), float64(outputBounds.Dy())
	cropWidth, cropHeight := crop.Right-crop.Left, crop.Bottom-crop.Top
	scaleX, scaleY := width/cropWidth, height/cropHeight
	radians := crop.Angle * math.Pi / 180
	cosine, sine := math.Cos(radians), math.Sin(radians)
	// Map the source image through the inverse rotation of the crop rectangle,
	// then scale its unrotated bounds into the fixed output dimensions.
	transform := f64.Aff3{
		cosine * scaleX,
		sine * scaleX,
		(crop.CenterX*(1-cosine) - crop.CenterY*sine - crop.Left) * scaleX,
		-sine * scaleY,
		cosine * scaleY,
		(crop.CenterY*(1-cosine) + crop.CenterX*sine - crop.Top) * scaleY,
	}
	xdraw.CatmullRom.Transform(result, transform, source, source.Bounds(), xdraw.Src, nil)
	return result
}
