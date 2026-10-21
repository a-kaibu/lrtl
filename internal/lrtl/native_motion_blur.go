package lrtl

import (
	"encoding/binary"
	"fmt"
	"math"
)

// Native tmix accumulates unnormalized products in float32, then multiplies
// once by the float32 reciprocal weight sum and rounds to nearest even.
// Packed working YUV samples have the same uint16 layout as RGB48 queues.
func weightedWorkingFrames(frames [][]byte, weights []float32) ([]byte, error) {
	return weightedWorkingFramesDepth(frames, weights, 10)
}

func weightedWorkingFramesDepth(frames [][]byte, weights []float32, depth int) ([]byte, error) {
	if depth != 8 && depth != 10 {
		return nil, fmt.Errorf("native YUV blend depth must be 8 or 10")
	}
	maximum := (1 << depth) - 1
	if len(frames) == 0 || len(frames) != len(weights) || len(frames[0]) == 0 || len(frames[0])%6 != 0 {
		return nil, fmt.Errorf("invalid native YUV blend window")
	}
	length := len(frames[0])
	var total float32
	uniform := true
	for i, w := range weights {
		if len(frames[i]) != length || w < 0 || math.IsNaN(float64(w)) || math.IsInf(float64(w), 0) {
			return nil, fmt.Errorf("invalid native YUV frame or weight")
		}
		total = float32(total + w)
		uniform = uniform && w == weights[0]
	}
	if total <= 0 || math.IsInf(float64(total), 0) {
		return nil, fmt.Errorf("invalid native YUV weight sum")
	}
	factor := float32(1 / total)
	if math.IsInf(float64(factor), 0) {
		return nil, fmt.Errorf("native YUV weights are too small")
	}
	out := make([]byte, length)
	for i := 0; i < length; i += 2 {
		var value int
		if uniform {
			sum := uint64(len(frames) / 2)
			for _, frame := range frames {
				sum += uint64(binary.LittleEndian.Uint16(frame[i:]))
			}
			value = int(sum / uint64(len(frames)))
		} else {
			var sum float32
			for j, frame := range frames {
				term := float32(float32(binary.LittleEndian.Uint16(frame[i:])) * weights[j])
				sum = float32(sum + term)
			}
			value = int(math.RoundToEven(float64(float32(sum * factor))))
		}
		binary.LittleEndian.PutUint16(out[i:], uint16(min(maximum, max(0, value))))
	}
	return out, nil
}
