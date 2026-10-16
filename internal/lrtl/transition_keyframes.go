package lrtl

// The automatic worker uses rating 4 for ordinary develop columns. Crop*
// columns switch to rating 5 whenever at least one crop keyframe exists.
// Without a selected rating, the first frame remains the reference value.
func transitionKeyframeGroups(packets []*XMPPacket) (normal, crop, combined []int) {
	for index, packet := range packets {
		rating, valid := packet.Rating()
		if !valid {
			continue
		}
		if rating == 4 {
			normal = append(normal, index)
		}
		if rating == 5 {
			crop = append(crop, index)
		}
	}
	if len(normal) == 0 && len(packets) > 0 {
		normal = []int{0}
	}
	if len(crop) == 0 {
		crop = append([]int(nil), normal...)
	}
	selected := make([]bool, len(packets))
	for _, index := range normal {
		selected[index] = true
	}
	for _, index := range crop {
		selected[index] = true
	}
	for index, found := range selected {
		if found {
			combined = append(combined, index)
		}
	}
	return normal, crop, combined
}

func transitionKeyframeFlags(count int, indices []int) (map[int]bool, []bool) {
	selected := make(map[int]bool, len(indices))
	flags := make([]bool, count)
	for _, index := range indices {
		selected[index] = true
		flags[index] = true
	}
	return selected, flags
}

// With only two endpoint anchors, the outer worker chooses bqa/Ura's linear
// double-precision path. Sampling a two-point Bezier is not equivalent: the
// first sample in each frame bin can differ substantially from a straight line.
func sampleLRTNumericTransition(anchors []lrtSplinePoint, precise []transitionAnchor, count int, flags []bool, smoothness int, spec numericTransitionSpec) ([]float64, []bool, error) {
	if len(precise) == 1 {
		values, filled := make([]float64, count), make([]bool, count)
		for i := range values {
			if !flags[i] {
				values[i] = adjustTransitionValue(precise[0].value, spec)
				filled[i] = true
			}
		}
		return values, filled, nil
	}
	selectedCount := 0
	for _, selected := range flags {
		if selected {
			selectedCount++
		}
	}
	if len(precise) == 2 && selectedCount == 2 && precise[0].index == 0 && precise[1].index == count-1 && count > 1 {
		values, filled := make([]float64, count), make([]bool, count)
		step := 1 / float64(count-1)
		for i := range values {
			if !flags[i] {
				values[i] = adjustTransitionValue(precise[0].value+(precise[1].value-precise[0].value)*(float64(i)*step), spec)
				filled[i] = true
			}
		}
		return values, filled, nil
	}
	spline, err := newLRTTransitionSpline(transitionSplinePoints(anchors, count), smoothness)
	if err != nil {
		return nil, nil, err
	}
	values, filled := spline.sampleFrames(count, flags)
	postprocessLRTTransition(values, filled, flags, precise, spec)
	return values, filled, nil
}
