package lrtl

import (
	"encoding/binary"
	"fmt"
	"math"
)

type dngVignetteRadial struct {
	coefficients     [5]float64
	centerX, centerY float64
}

func parseDNGVignetteRadial(data []byte) (*dngVignetteRadial, error) {
	if len(data) != 7*8 {
		return nil, fmt.Errorf("invalid parameter size")
	}
	result := &dngVignetteRadial{}
	for index := range result.coefficients {
		value := math.Float64frombits(binary.BigEndian.Uint64(data[index*8:]))
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, fmt.Errorf("non-finite radial gain coefficient")
		}
		result.coefficients[index] = value
	}
	result.centerX = math.Float64frombits(binary.BigEndian.Uint64(data[40:]))
	result.centerY = math.Float64frombits(binary.BigEndian.Uint64(data[48:]))
	if !isNormalizedDNGCoordinate(result.centerX) || !isNormalizedDNGCoordinate(result.centerY) {
		return nil, fmt.Errorf("optical center must be within normalized image bounds")
	}
	return result, nil
}

func isNormalizedDNGCoordinate(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && value <= 1
}

func (vignette *dngVignetteRadial) identity() bool {
	return *vignette == (dngVignetteRadial{centerX: vignette.centerX, centerY: vignette.centerY})
}

func (vignette *dngVignetteRadial) gain(x, y, width, height int) float64 {
	cx, cy := float64(width)*vignette.centerX, float64(height)*vignette.centerY
	mx := max(math.Abs(cx), math.Abs(float64(width)-cx))
	my := max(math.Abs(cy), math.Abs(float64(height)-cy))
	radius := math.Hypot(mx, my)
	return vignette.gainAt(float64(x), float64(y), cx, cy, radius)
}

func (vignette *dngVignetteRadial) gainAt(x, y, centerX, centerY, radius float64) float64 {
	r := min(1, math.Hypot(x-centerX, y-centerY)/radius)
	r2 := r * r
	result := vignette.coefficients[4]
	for index := 3; index >= 0; index-- {
		result = result*r2 + vignette.coefficients[index]
	}
	return 1 + r2*result
}

func (vignette *dngVignetteRadial) apply(data []float32, width, height, channels int) error {
	if width < 1 || height < 1 || channels < 1 || channels > 3 || uint64(width) > uint64(len(data))/uint64(height)/uint64(channels) || uint64(width)*uint64(height)*uint64(channels) != uint64(len(data)) {
		return fmt.Errorf("invalid image dimensions for radial vignette")
	}
	cx, cy := float64(width)*vignette.centerX, float64(height)*vignette.centerY
	mx := max(math.Abs(cx), math.Abs(float64(width)-cx))
	my := max(math.Abs(cy), math.Abs(float64(height)-cy))
	radius := math.Hypot(mx, my)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			gain := vignette.gainAt(float64(x), float64(y), cx, cy, radius)
			if math.IsNaN(gain) || math.IsInf(gain, 0) {
				return fmt.Errorf("non-finite DNG radial vignette gain")
			}
			index := (y*width + x) * channels
			for channel := 0; channel < channels; channel++ {
				value := float64(data[index+channel]) * gain
				data[index+channel] = float32(min(1, max(0, value)))
			}
		}
	}
	return nil
}

// applyRaw applies a Stage 1 vignette opcode to stored 16-bit samples. The SDK
// evaluates this opcode through a normalized float buffer, then writes the
// result back to the Stage 1 image. Pixel-center coordinates are used for the
// radial mask, as in the SDK's fixed-point mask setup.
func (vignette *dngVignetteRadial) applyRaw(source, destination []uint16, width, height, channels int) error {
	if width < 1 || height < 1 || channels < 1 || channels > 3 || uint64(width) > uint64(math.MaxInt)/uint64(height) {
		return fmt.Errorf("invalid image dimensions for Stage 1 radial vignette")
	}
	pixelCount := uint64(width) * uint64(height)
	if pixelCount > uint64(math.MaxInt)/uint64(channels) || uint64(len(source)) != pixelCount*uint64(channels) || len(destination) != len(source) {
		return fmt.Errorf("invalid image buffer for Stage 1 radial vignette")
	}
	copy(destination, source)
	cx, cy := float64(width)*vignette.centerX, float64(height)*vignette.centerY
	mx := max(math.Abs(cx), math.Abs(float64(width)-cx))
	my := max(math.Abs(cy), math.Abs(float64(height)-cy))
	radius := math.Hypot(mx, my)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			gain := vignette.gainAt(float64(x)+.5, float64(y)+.5, cx, cy, radius)
			if math.IsNaN(gain) || math.IsInf(gain, 0) {
				return fmt.Errorf("non-finite Stage 1 radial vignette gain")
			}
			gain32 := float32(gain)
			base := (y*width + x) * channels
			for channel := 0; channel < channels; channel++ {
				index := base + channel
				value := float32(source[index]) / 65535 * gain32
				if math.IsNaN(float64(value)) {
					return fmt.Errorf("non-finite Stage 1 radial vignette sample")
				}
				value = min(float32(1), max(float32(0), value))
				destination[index] = uint16(math.Round(float64(value) * 65535))
			}
		}
	}
	return nil
}
