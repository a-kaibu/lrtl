package lrtl

import (
	"fmt"
	"image"
	"math"
)

// trimDNGStage1Pixels compacts the requested rows into the source buffer and
// returns a shorter view, keeping the Stage 1 working set within its limit.
func trimDNGStage1Pixels(source []uint16, width, height, channels int, bounds image.Rectangle) ([]uint16, error) {
	if width < 1 || height < 1 || channels < 1 || uint64(width)*uint64(height)*uint64(channels) != uint64(len(source)) {
		return nil, fmt.Errorf("invalid DNG Stage 1 TrimBounds source dimensions")
	}
	if bounds.Empty() || !bounds.In(image.Rect(0, 0, width, height)) {
		return nil, fmt.Errorf("DNG Stage 1 TrimBounds is outside the current image")
	}
	count := uint64(bounds.Dx()) * uint64(bounds.Dy()) * uint64(channels)
	if count > uint64(math.MaxInt) || count > uint64(len(source)) || count > uint64(maxDevelopedRAWBytes)/2 {
		return nil, fmt.Errorf("DNG Stage 1 TrimBounds image exceeds development limits")
	}
	for y := 0; y < bounds.Dy(); y++ {
		sourceStart := ((bounds.Min.Y+y)*width + bounds.Min.X) * channels
		destinationStart := y * bounds.Dx() * channels
		copy(source[destinationStart:destinationStart+bounds.Dx()*channels], source[sourceStart:sourceStart+bounds.Dx()*channels])
	}
	return source[:int(count)], nil
}

// applyDNGStage1Opcodes returns the Stage 1 sample buffer and its dimensions.
// TrimBounds rebases the image origin, so later Stage 1 opcodes use coordinates
// in the newly trimmed image, matching the SDK's image buffer behavior.
func applyDNGStage1Opcodes(opcodes []dngImageOpcode, pixels []uint16, width, height, channels int, activeArea image.Rectangle) ([]uint16, int, int, image.Rectangle, int, int, error) {
	if width < 1 || height < 1 || channels < 1 || uint64(width)*uint64(height)*uint64(channels) != uint64(len(pixels)) {
		return nil, 0, 0, image.Rectangle{}, 0, 0, fmt.Errorf("invalid DNG Stage 1 opcode image dimensions")
	}
	if activeArea.Empty() || !activeArea.In(image.Rect(0, 0, width, height)) {
		return nil, 0, 0, image.Rectangle{}, 0, 0, fmt.Errorf("invalid DNG Stage 1 ActiveArea")
	}
	currentWidth, currentHeight := width, height
	deltaOffsetX, deltaOffsetY := 0, 0
	var destination []uint16
	for _, opcode := range opcodes {
		if opcode.trim != nil {
			bounds := *opcode.trim
			intersection := activeArea.Intersect(bounds)
			if intersection.Empty() {
				return nil, 0, 0, image.Rectangle{}, 0, 0, fmt.Errorf("DNG Stage 1 TrimBounds removes the ActiveArea")
			}
			trimmed, err := trimDNGStage1Pixels(pixels, currentWidth, currentHeight, channels, bounds)
			if err != nil {
				return nil, 0, 0, image.Rectangle{}, 0, 0, err
			}
			deltaOffsetX += intersection.Min.X - activeArea.Min.X
			deltaOffsetY += intersection.Min.Y - activeArea.Min.Y
			activeArea = intersection.Sub(bounds.Min)
			pixels = trimmed
			currentWidth, currentHeight = bounds.Dx(), bounds.Dy()
			destination = nil
			continue
		}
		if destination == nil || len(destination) != len(pixels) {
			destination = make([]uint16, len(pixels))
		}
		var err error
		switch {
		case opcode.badPixelConstant != nil:
			err = applyDNGBadPixelConstantRaw(pixels, destination, currentWidth, currentHeight, *opcode.badPixelConstant)
		case opcode.badPixelList != nil:
			err = applyDNGBadPixelListRaw(pixels, destination, currentWidth, currentHeight, image.Rect(0, 0, currentWidth, currentHeight), *opcode.badPixelList)
		case opcode.vignette != nil:
			err = opcode.vignette.applyRaw(pixels, destination, currentWidth, currentHeight, channels)
		case opcode.point != nil:
			err = applyDNGPointOpcodeRaw(pixels, destination, currentWidth, currentHeight, channels, *opcode.point)
		default:
			err = fmt.Errorf("unsupported Linear DNG Stage 1 opcode implementation")
		}
		if err != nil {
			return nil, 0, 0, image.Rectangle{}, 0, 0, err
		}
		pixels, destination = destination, pixels
	}
	return pixels, currentWidth, currentHeight, activeArea, deltaOffsetX, deltaOffsetY, nil
}
