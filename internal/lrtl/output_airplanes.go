package lrtl

import (
	"bytes"
	"fmt"
)

func validateRemoveAirplanes(level int) error {
	if level < 0 || level > 3 {
		return fmt.Errorf("remove-airplanes must be 0 (disabled) or a level from 1 to 3")
	}
	return nil
}

func airplaneRadius(level int) int {
	if level == 0 {
		return 0
	}
	if level == 3 {
		return 2
	}
	return 1
}

func airplaneSourceCount(count, level int) int { return count - 2*airplaneRadius(level) }

// Native speed!=1 adds fps after tmedian. The original EOF time propagates,
// making fps hold the last median image through the original sequence end.
func airplaneTimelineSourceCount(count int, speed float64, level int) int {
	if level > 0 && speed == 1 {
		return airplaneSourceCount(count, level)
	}
	return count
}

func validateAirplaneMemory(width, height, count, level, noise, blur int) error {
	if level == 0 {
		return nil
	}
	perPixel := uint64(min(count, 2*airplaneRadius(level)+1))*12 + 48 + uint64(max(1, blur))*6
	if noise > 0 {
		perFrame := uint64(12)
		if noise == 4 {
			perFrame = 18
		}
		perPixel += uint64(min(count, outputDenoisePreset(noise).window)) * perFrame
	}
	if uint64(width)*uint64(height)*perPixel > 768*1024*1024 {
		return fmt.Errorf("temporal filter frame-buffer estimate exceeds 768 MiB; reduce --remove-airplanes, --denoise, --motion-blur or output dimensions")
	}
	return nil
}

func temporalMedian10(window [][]uint16) []uint16 {
	output := make([]uint16, len(window[0]))
	for pixel := range output {
		var sorted [5]uint16
		for n, frame := range window {
			v := frame[pixel]
			at := n
			for at > 0 && sorted[at-1] > v {
				sorted[at] = sorted[at-1]
				at--
			}
			sorted[at] = v
		}
		output[pixel] = sorted[len(window)/2]
	}
	return output
}

func airplaneLuma10(input []uint16, width, height, level int) []uint16 {
	return airplaneLuma10Arithmetic(input, width, height, level, false)
}

func airplaneLuma10Arithmetic(input []uint16, width, height, level int, fused bool) []uint16 {
	if level == 1 {
		return append([]uint16(nil), input...)
	}
	blurred := gaussianOutputPlaneArithmetic(input, width, height, 15, 1023, fused)
	amount, knee, gain := .20, 918.0, 2.0
	if level == 3 {
		amount, knee, gain = .35, 867, 3
	}
	output := make([]uint16, len(input))
	for i, v := range input {
		// Native blend expressions truncate to an integer before the LUT.
		value := float64(min(1023, max(0, int(float64(v)+(float64(v)-float64(blurred[i]))*amount))))
		if value >= knee {
			value = knee + (value-knee)*gain
		}
		output[i] = uint16(min(1020, int(value)))
	}
	return output
}

type airplaneInput struct {
	rgb    []byte
	planes [3][]uint16
}
type airplaneRemover struct {
	width, height, count, level, next, emitted int
	cache                                      map[int]airplaneInput
	load                                       func(int) ([]byte, error)
	color                                      outputFrameColor
}

func newAirplaneRemover(width, height, count, level int, load func(int) ([]byte, error)) *airplaneRemover {
	return &airplaneRemover{width: width, height: height, count: count, level: level, cache: make(map[int]airplaneInput), load: load}
}
func (a *airplaneRemover) frame(index int) ([]byte, error) {
	if index != a.emitted || index >= airplaneSourceCount(a.count, a.level) {
		return nil, fmt.Errorf("airplane removal requires sequential filtered frames")
	}
	for key := range a.cache {
		if key < index {
			delete(a.cache, key)
		}
	}
	radius := airplaneRadius(a.level)
	for a.next <= index+2*radius {
		rgb, err := a.load(a.next)
		if err != nil {
			return nil, fmt.Errorf("airplane removal source frame %d: %w", a.next+1, err)
		}
		if len(rgb) != a.width*a.height*6 {
			return nil, fmt.Errorf("airplane removal source frame %d has inconsistent dimensions", a.next+1)
		}
		a.cache[a.next] = airplaneInput{rgb, a.color.planes(rgb, a.width, a.height)}
		a.next++
	}
	anchor := a.cache[index]
	var filtered [3][]uint16
	for p := range filtered {
		window := make([][]uint16, 2*radius+1)
		for j := range window {
			window[j] = a.cache[index+j].planes[p]
		}
		filtered[p] = temporalMedian10(window)
		if p == 0 {
			filtered[p] = airplaneLuma10Arithmetic(filtered[p], a.width, a.height, a.level, bool(a.color) && nativeGaussianFused())
		} else if a.level > 1 {
			// The native lutyuv overrides only Y; its default clipval
			// expressions constrain 10-bit YUV chroma to 64..960.
			for i, v := range filtered[p] {
				filtered[p][i] = min(960, max(64, v))
			}
		}
	}
	// tmedian timestamps each window from its first frame, not its center.
	// Preserve the RGB16 residual of that same anchor when applying YUV deltas.
	rgb := bytes.Clone(anchor.rgb)
	a.color.apply(rgb, anchor.planes, filtered)
	a.emitted++
	return rgb, nil
}
