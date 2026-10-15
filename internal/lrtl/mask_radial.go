package lrtl

import (
	"image"
	"math"
	"os"

	"github.com/evanoberholster/imagemeta"
)

type radialGradientGeometry struct {
	centerX, centerY float64
	major, minor     float64
	orientation      float64
}

func sequenceImageAspect(frame Frame) float64 {
	file, err := os.Open(frame.Image)
	if err != nil {
		return 0
	}
	defer file.Close()

	config, _, configErr := image.DecodeConfig(file)
	width, height := 0, 0
	if configErr == nil {
		width, height = config.Width, config.Height
	}
	if _, err := file.Seek(0, 0); err == nil {
		metadata, metadataErr := imagemeta.Decode(file)
		if metadataErr == nil {
			if metadata.ImageWidth > 0 && metadata.ImageHeight > 0 {
				width, height = int(metadata.ImageWidth), int(metadata.ImageHeight)
			}
			orientation := uint16(metadata.Orientation)
			if orientation >= 5 && orientation <= 8 {
				width, height = height, width
			}
		}
	}
	if width <= 0 || height <= 0 {
		return 0
	}
	return float64(width) / float64(height)
}

func radialGradientFromValues(values map[string]float64, aspect float64) (radialGradientGeometry, bool) {
	left, leftOK := values["Left"]
	top, topOK := values["Top"]
	right, rightOK := values["Right"]
	bottom, bottomOK := values["Bottom"]
	angle, angleOK := values["Angle"]
	if !leftOK || !topOK || !rightOK || !bottomOK || !angleOK || aspect <= 0 || angle < -45.000001 || angle > 45.000001 {
		return radialGradientGeometry{}, false
	}
	deltaX, deltaY := right-left, bottom-top
	halfWidth, halfHeight := math.Abs(deltaX)*aspect/2, math.Abs(deltaY)/2
	if halfWidth <= 1e-12 || halfHeight <= 1e-12 {
		return radialGradientGeometry{}, false
	}
	root := math.Hypot(halfWidth, halfHeight)
	signX, signY := 1.0, 1.0
	if deltaX < 0 {
		signX = -1
	}
	if deltaY < 0 {
		signY = -1
	}
	signedBoxDiagonal := math.Atan2(signY*halfHeight, signX*halfWidth) * 180 / math.Pi
	beta := foldedRadialAspect(signedBoxDiagonal - angle)
	centerX, centerY := (left+right)/2, (top+bottom)/2

	bestOrientation, bestError := 0.0, math.Inf(1)
	for turn := -4; turn <= 4; turn++ {
		orientation := angle + float64(turn)*90
		encoded := encodeRadialGradient(centerX, centerY, root, beta, orientation, aspect)
		error := math.Abs(encoded["Left"]-left) + math.Abs(encoded["Right"]-right) +
			math.Abs(encoded["Top"]-top) + math.Abs(encoded["Bottom"]-bottom)
		if error < bestError-1e-12 || (math.Abs(error-bestError) <= 1e-12 && math.Abs(orientation) < math.Abs(bestOrientation)) {
			bestOrientation, bestError = orientation, error
		}
	}
	if bestError > 0.01 {
		return radialGradientGeometry{}, false
	}
	return radialGradientGeometry{
		centerX:     centerX,
		centerY:     centerY,
		major:       root * math.Cos(beta*math.Pi/180),
		minor:       root * math.Sin(beta*math.Pi/180),
		orientation: bestOrientation,
	}, true
}

func foldedRadialAspect(angle float64) float64 {
	folded := math.Mod(angle, 90)
	if folded < 0 {
		folded += 90
	}
	if folded > 45 {
		folded = 90 - folded
	}
	return folded
}

func foldRadialAngle(angle float64) float64 {
	folded := math.Mod(angle+45, 90)
	if folded < 0 {
		folded += 90
	}
	folded -= 45
	if folded <= -45+1e-12 {
		return 45
	}
	return folded
}

func encodeRadialGradient(centerX, centerY, root, beta, orientation, aspect float64) map[string]float64 {
	angle := foldRadialAngle(orientation)
	band := int(math.Floor((orientation + 45) / 90))
	phi := angle + beta
	if band%2 != 0 {
		phi = angle + 90 - beta
	}
	for phi <= -90 {
		phi += 180
	}
	for phi > 135 {
		phi -= 180
	}
	radians := phi * math.Pi / 180
	deltaX := 2 * root * math.Cos(radians) / aspect
	deltaY := 2 * root * math.Sin(radians)
	return map[string]float64{
		"Left": centerX - deltaX/2, "Right": centerX + deltaX/2,
		"Top": centerY - deltaY/2, "Bottom": centerY + deltaY/2,
		"Angle": angle,
	}
}

func interpolateRadialGradient(startValues, endValues map[string]float64, fraction, aspect float64) (map[string]float64, bool) {
	start, startOK := radialGradientFromValues(startValues, aspect)
	end, endOK := radialGradientFromValues(endValues, aspect)
	if !startOK || !endOK || fraction < 0 || fraction > 1 {
		return nil, false
	}
	orientationSpan := math.Mod(end.orientation-start.orientation, 180)
	if orientationSpan > 90 {
		orientationSpan -= 180
	} else if orientationSpan <= -90 {
		orientationSpan += 180
	}
	geometry := radialGradientGeometry{
		centerX:     start.centerX + (end.centerX-start.centerX)*fraction,
		centerY:     start.centerY + (end.centerY-start.centerY)*fraction,
		major:       start.major + (end.major-start.major)*fraction,
		minor:       start.minor + (end.minor-start.minor)*fraction,
		orientation: start.orientation + orientationSpan*fraction,
	}
	if geometry.major <= 1e-12 || geometry.minor <= 1e-12 {
		return nil, false
	}
	root := math.Hypot(geometry.major, geometry.minor)
	beta := math.Atan2(geometry.minor, geometry.major) * 180 / math.Pi
	return encodeRadialGradient(geometry.centerX, geometry.centerY, root, beta, geometry.orientation, aspect), true
}

func radialMaskWeight(left, top, right, bottom, angle float64, x, y, width, height int) (float64, bool) {
	if width <= 0 || height <= 0 {
		return 0, false
	}
	values := map[string]float64{"Left": left, "Top": top, "Right": right, "Bottom": bottom, "Angle": angle}
	aspect := float64(width) / float64(height)
	geometry, ok := radialGradientFromValues(values, aspect)
	if !ok {
		return 0, false
	}
	dx := (float64(x)+0.5)/float64(width) - geometry.centerX
	dy := (float64(y)+0.5)/float64(height) - geometry.centerY
	dx *= aspect
	rotation := geometry.orientation * math.Pi / 180
	rotatedX := dx*math.Cos(rotation) + dy*math.Sin(rotation)
	rotatedY := -dx*math.Sin(rotation) + dy*math.Cos(rotation)
	radius := math.Sqrt((rotatedX/geometry.major)*(rotatedX/geometry.major) + (rotatedY/geometry.minor)*(rotatedY/geometry.minor))
	return radius, true
}
