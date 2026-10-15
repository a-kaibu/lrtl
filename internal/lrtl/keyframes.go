package lrtl

import (
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"os"
	"path/filepath"
	"sort"

	_ "golang.org/x/image/tiff"
)

// analyzeLuminanceSequence returns one log2 luminance measurement per frame.
// It intentionally supports developed JPEG/PNG/TIFF inputs only.
func analyzeLuminanceSequence(frames []Frame) ([]float64, error) {
	if len(frames) == 0 {
		return nil, fmt.Errorf("sequence contains no images")
	}
	curve := make([]float64, len(frames))
	width, height := 0, 0
	for index, frame := range frames {
		if !supportedRasterExtension(frame.Image) {
			return nil, fmt.Errorf("luminance analysis supports JPEG, PNG, and TIFF only; unsupported frame: %s", filepath.Base(frame.Image))
		}
		data, err := os.ReadFile(frame.Image)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", frame.Image, err)
		}
		img, format, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("decode %s: %w", frame.Image, err)
		}
		if !supportedRasterFormat(format) {
			return nil, fmt.Errorf("luminance analysis supports JPEG, PNG, and TIFF data only: %s", frame.Image)
		}
		bounds := img.Bounds()
		if bounds.Empty() {
			return nil, fmt.Errorf("invalid image dimensions in %s", frame.Image)
		}
		if index == 0 {
			width, height = bounds.Dx(), bounds.Dy()
		} else if bounds.Dx() != width || bounds.Dy() != height {
			return nil, fmt.Errorf("inconsistent dimensions in %s", frame.Image)
		}
		luminance, err := frameLuminance(img, bounds)
		if err != nil {
			return nil, fmt.Errorf("analyze %s: %w", frame.Image, err)
		}
		curve[index] = math.Log2(luminance)
	}
	return curve, nil
}

// selectKeyframeIndices treats the endpoints as curve anchors and returns only
// interior frames, then repeatedly splits the segment with the largest deviation
// from a straight luminance transition.
func selectKeyframeIndices(logLuminance []float64, count, smoothRadius int) ([]int, error) {
	if len(logLuminance) == 0 {
		return nil, fmt.Errorf("sequence contains no luminance samples")
	}
	interiorCount := len(logLuminance) - 2
	if count < 1 || count > interiorCount {
		return nil, fmt.Errorf("keyframe count must be between 1 and %d interior frames: %d", max(0, interiorCount), count)
	}
	if smoothRadius < 0 || smoothRadius > 1000 {
		return nil, fmt.Errorf("smoothing radius must be between 0 and 1000 frames: %d", smoothRadius)
	}
	for index, value := range logLuminance {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, fmt.Errorf("frame %d has invalid luminance", index+1)
		}
	}
	curve := append([]float64(nil), logLuminance...)
	if smoothRadius > 0 {
		curve = smoothLogLuminance(curve, smoothRadius)
	}
	if count == 1 {
		median := medianFloat64(curve[1 : len(curve)-1])
		best := 1
		for index := 2; index < len(curve)-1; index++ {
			if math.Abs(curve[index]-median) < math.Abs(curve[best]-median) {
				best = index
			}
		}
		return []int{best}, nil
	}
	if count == interiorCount {
		indices := make([]int, interiorCount)
		for index := range indices {
			indices[index] = index + 1
		}
		return indices, nil
	}

	indices := []int{0, len(curve) - 1}
	for len(indices) < count+2 {
		bestIndex, bestSpan := -1, -1
		bestDeviation := -1.0
		for segment := 0; segment < len(indices)-1; segment++ {
			left, right := indices[segment], indices[segment+1]
			span := right - left
			if span < 2 {
				continue
			}
			candidate, deviation, candidateDistance := -1, -1.0, math.MaxInt
			midpoint := float64(left+right) / 2
			for index := left + 1; index < right; index++ {
				fraction := float64(index-left) / float64(span)
				expected := curve[left] + (curve[right]-curve[left])*fraction
				currentDeviation := math.Abs(curve[index] - expected)
				distance := int(math.Abs(float64(index)-midpoint) * 2)
				if currentDeviation > deviation+1e-12 || (math.Abs(currentDeviation-deviation) <= 1e-12 && distance < candidateDistance) {
					candidate, deviation, candidateDistance = index, currentDeviation, distance
				}
			}
			if deviation > bestDeviation+1e-12 || (math.Abs(deviation-bestDeviation) <= 1e-12 && span > bestSpan) {
				bestIndex, bestSpan, bestDeviation = candidate, span, deviation
			}
		}
		if bestIndex < 0 {
			break
		}
		indices = append(indices, bestIndex)
		sort.Ints(indices)
	}
	return indices[1 : len(indices)-1], nil
}

func selectEvenlySpacedKeyframeIndices(frameCount, count int) ([]int, error) {
	if frameCount < 1 {
		return nil, fmt.Errorf("sequence contains no frames")
	}
	interiorCount := frameCount - 2
	if count < 1 || count > interiorCount {
		return nil, fmt.Errorf("keyframe count must be between 1 and %d interior frames: %d", max(0, interiorCount), count)
	}

	indices := make([]int, count)
	divisions := count + 1
	for position := 1; position <= count; position++ {
		numerator := position * (frameCount - 1)
		indices[position-1] = (numerator + divisions/2) / divisions
	}
	return indices, nil
}
