package lrtl

import (
	"bytes"
	"fmt"
	"image"
	"math"
	"sort"
)

// analyzeLuminanceSequence returns one log2 luminance measurement per frame
// from already-developed JPEG/PNG/TIFF inputs.
func analyzeLuminanceSequence(frames []Frame) ([]float64, error) {
	return analyzeLuminanceSequenceWithRawPreview(frames, false)
}

func analyzeLuminanceSequenceWithRawPreview(frames []Frame, useRawPreview bool) ([]float64, error) {
	return analyzeLuminanceSequenceWithInputOptions(frames, imageInputOptions{RawPreview: useRawPreview})
}

func analyzeLuminanceSequenceWithInputOptions(frames []Frame, options imageInputOptions) ([]float64, error) {
	if len(frames) == 0 {
		return nil, fmt.Errorf("sequence contains no images")
	}
	if err := validateImageInputOptions(options); err != nil {
		return nil, err
	}
	curve := make([]float64, len(frames))
	width, height := 0, 0
	var adjustments sequenceXMPAdjustments
	for index, frame := range frames {
		data, _, _, err := readRenderInputWithOptions(frame, options)
		if err != nil {
			return nil, fmt.Errorf("luminance analysis: %w", err)
		}
		img, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("decode %s: %w", frame.Image, err)
		}
		bounds := img.Bounds()
		if bounds.Empty() {
			return nil, fmt.Errorf("invalid image dimensions in %s", frame.Image)
		}
		if index == 0 {
			width, height = bounds.Dx(), bounds.Dy()
			adjustments, err = readSequenceXMPAdjustments(frames, width, height, options)
			if err != nil {
				return nil, err
			}
		} else if bounds.Dx() != width || bounds.Dy() != height {
			return nil, fmt.Errorf("inconsistent dimensions in %s", frame.Image)
		}
		img = adjustments.apply(index, img)
		bounds = img.Bounds()
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
	return selectKeyframeIndicesWithAnchors(logLuminance, count, smoothRadius, nil)
}

// selectKeyframeIndicesWithAnchors adds count new interior indices while
// preserving existing keyframes as fixed curve anchors.
func selectKeyframeIndicesWithAnchors(logLuminance []float64, count, smoothRadius int, existing []int) ([]int, error) {
	if len(logLuminance) == 0 {
		return nil, fmt.Errorf("sequence contains no luminance samples")
	}
	if len(logLuminance) < 3 {
		return nil, fmt.Errorf("keyframe suggestions require at least one interior frame between the sequence endpoints")
	}
	interiorCount := len(logLuminance) - 2
	anchors := make([]int, 0, len(existing)+2)
	anchors = append(anchors, 0, len(logLuminance)-1)
	blocked := make([]bool, len(logLuminance))
	blocked[0], blocked[len(blocked)-1] = true, true
	for _, index := range existing {
		if index < 0 || index >= len(logLuminance) {
			return nil, fmt.Errorf("existing keyframe index is outside the sequence: %d", index)
		}
		if index == 0 || index == len(logLuminance)-1 || blocked[index] {
			continue
		}
		blocked[index] = true
		anchors = append(anchors, index)
	}
	available := interiorCount
	for _, isBlocked := range blocked[1 : len(blocked)-1] {
		if isBlocked {
			available--
		}
	}
	if count < 1 || count > available {
		return nil, fmt.Errorf("new keyframe count must be between 1 and %d unmarked interior frames: %d", max(0, available), count)
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
		values := make([]float64, 0, available)
		for index := 1; index < len(curve)-1; index++ {
			if !blocked[index] {
				values = append(values, curve[index])
			}
		}
		median := medianFloat64(values)
		best := -1
		for index := 1; index < len(curve)-1; index++ {
			if blocked[index] {
				continue
			}
			if best < 0 {
				best = index
				continue
			}
			if math.Abs(curve[index]-median) < math.Abs(curve[best]-median) {
				best = index
			}
		}
		return []int{best}, nil
	}
	if count == available {
		indices := make([]int, 0, available)
		for index := 1; index < len(curve)-1; index++ {
			if !blocked[index] {
				indices = append(indices, index)
			}
		}
		return indices, nil
	}

	sort.Ints(anchors)
	targetAnchorCount := len(anchors) + count
	for len(anchors) < targetAnchorCount {
		bestIndex, bestSpan := -1, -1
		bestDeviation := -1.0
		for segment := 0; segment < len(anchors)-1; segment++ {
			left, right := anchors[segment], anchors[segment+1]
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
		anchors = append(anchors, bestIndex)
		sort.Ints(anchors)
	}

	selected := make([]int, 0, count)
	for _, index := range anchors {
		if index != 0 && index != len(curve)-1 && !blocked[index] {
			selected = append(selected, index)
		}
	}
	return selected, nil
}

// estimateAutoKeyframeCount follows the quantity heuristic recovered from
// LRTimelapse 7.5.5's keyframe helper. Holy Grail 2*/3* pairs compensate the
// measured curve before its range is converted to a keyframe count. Candidate
// placement is handled separately by the wizard spacing helper or the optional
// luminance-deviation selector.
func estimateAutoKeyframeCount(logLuminance []float64, holyGrailMarkers []bool) int {
	if len(logLuminance) == 0 {
		return 0
	}
	if len(holyGrailMarkers) != len(logLuminance) {
		holyGrailMarkers = make([]bool, len(logLuminance))
	}

	minimum, maximum := math.Inf(1), math.Inf(-1)
	correction := 0.0
	hasHolyGrailMarker := false
	for index, value := range logLuminance {
		hasHolyGrailMarker = hasHolyGrailMarker || holyGrailMarkers[index]
		if index > 0 && holyGrailMarkers[index] && holyGrailMarkers[index-1] {
			correction += logLuminance[index-1] - value
		}
		value += correction
		minimum = math.Min(minimum, value)
		maximum = math.Max(maximum, value)
	}

	frameCount := len(logLuminance)
	span := frameCount - 1
	// LRT calculates in float32 here after dividing the luminance range by 0.1.
	// Keep that rounding step so values near an integer boundary match its UI.
	frameFactor := float32(1) + float32(1000)/float32(frameCount)/float32(10)
	count := int((maximum - minimum) / 0.1 * float64(frameFactor))
	if minimumSpacingCount := span / 250; count < minimumSpacingCount {
		count = minimumSpacingCount
	}
	if count > 1 && span/(count+1) < 50 {
		count = span / 50
	}
	if count == 0 {
		count = 1
	}
	if hasHolyGrailMarker && count < 2 {
		count = 2
	}
	return count
}

// selectLRTWizardKeyframeIndices follows the installed keyframe helper's
// integer spacing and endpoint rules for a sequence with no preselected rows.
func selectLRTWizardKeyframeIndices(frameCount, count int) ([]int, error) {
	if frameCount < 1 {
		return nil, fmt.Errorf("sequence contains no frames")
	}
	if count < 1 {
		return nil, fmt.Errorf("keyframe quantity must be at least 1")
	}
	if count == 1 {
		return []int{0}, nil
	}

	step := frameCount / (count - 1)
	if step > frameCount {
		step = frameCount - 1
	}
	if step == 0 {
		step = 1
	}
	lastRegularIndex := frameCount - int(float64(step)*0.8)
	indices := make([]int, 0, count+1)
	for index := 0; index < frameCount; index++ {
		if (index%step == 0 && index < lastRegularIndex) || index == frameCount-1 {
			indices = append(indices, index)
		}
	}
	return indices, nil
}

// placeLRTWizardKeyframeIndices applies the installed wizard's rating
// replacement and collision-shift behavior to its nominal spacing positions.
func placeLRTWizardKeyframeIndices(indices []int, packets []*XMPPacket, reserved []bool, rating int) []int {
	if len(packets) == 0 || len(reserved) != len(packets) || (rating != 4 && rating != 5) {
		return nil
	}

	workingRatings := make([]int, len(packets))
	for index, packet := range packets {
		if packet == nil {
			continue
		}
		if current, ok := packet.Rating(); ok && current != rating {
			workingRatings[index] = current
		}
	}

	selected := make([]int, 0, len(indices))
	for _, index := range indices {
		if index < 0 || index >= len(packets) || reserved[index] {
			continue
		}
		current := workingRatings[index]
		if index > 0 && index < len(packets)-1 && current >= 2 && current <= 5 && current != rating {
			shifted := false
			for distance := 1; distance < 10; distance++ {
				previous := index - distance
				if previous <= 0 {
					break
				}
				if workingRatings[previous] != 0 || reserved[previous] {
					continue
				}
				workingRatings[previous] = rating
				selected = append(selected, previous)
				shifted = true
				break
			}
			if !shifted {
				continue
			}
			continue
		}
		if current == 2 || current == 3 {
			// MarkKeyframe protects Holy Grail ratings, including endpoint rows
			// that the LRT helper itself would otherwise overwrite.
			continue
		}
		workingRatings[index] = rating
		selected = append(selected, index)
	}
	sort.Ints(selected)
	return selected
}

func selectEvenlySpacedKeyframeIndices(frameCount, count int) ([]int, error) {
	return selectEvenlySpacedKeyframeIndicesWithAnchors(frameCount, count, nil)
}

func selectEvenlySpacedKeyframeIndicesWithAnchors(frameCount, count int, existing []int) ([]int, error) {
	if frameCount < 1 {
		return nil, fmt.Errorf("sequence contains no frames")
	}
	if frameCount < 3 {
		return nil, fmt.Errorf("keyframe suggestions require at least one interior frame between the sequence endpoints")
	}
	interiorCount := frameCount - 2
	blocked := make([]bool, frameCount)
	if frameCount > 0 {
		blocked[0] = true
		blocked[frameCount-1] = true
	}
	for _, index := range existing {
		if index < 0 || index >= frameCount {
			return nil, fmt.Errorf("existing keyframe index is outside the sequence: %d", index)
		}
		blocked[index] = true
	}
	available := interiorCount
	for _, isBlocked := range blocked[1 : frameCount-1] {
		if isBlocked {
			available--
		}
	}
	if count < 1 || count > available {
		return nil, fmt.Errorf("new keyframe count must be between 1 and %d unmarked interior frames: %d", max(0, available), count)
	}

	indices := make([]int, 0, count)
	if count == available {
		for index := 1; index < frameCount-1; index++ {
			if !blocked[index] {
				indices = append(indices, index)
			}
		}
		return indices, nil
	}

	divisions := count + 1
	for position := 1; position <= count; position++ {
		numerator := position * (frameCount - 1)
		target := (numerator + divisions/2) / divisions
		best, bestDistance := -1, math.MaxInt
		for index := 1; index < frameCount-1; index++ {
			if blocked[index] {
				continue
			}
			distance := absInt(index - target)
			if distance < bestDistance || (distance == bestDistance && index < best) {
				best, bestDistance = index, distance
			}
		}
		if best < 0 {
			return nil, fmt.Errorf("not enough unmarked frames for %d new keyframes", count)
		}
		blocked[best] = true
		indices = append(indices, best)
	}
	sort.Ints(indices)
	return indices, nil
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
