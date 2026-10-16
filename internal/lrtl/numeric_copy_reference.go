package lrtl

import "math"

// The automatic worker tests selected marks and both sequence endpoints before
// entering either numeric worker. Its copy branch does not clamp, round or
// convert to float32. Metadata preflight has already filled ordinary columns;
// WB columns must still contain their original missing cells at this point.
func numericColumnCopyReference(values []map[string]float64, name string, keys []int, integer bool) (float64, bool) {
	if len(values) < 2 || len(keys) == 0 {
		return 0, false
	}
	reference, present := values[keys[0]][name]
	if !present {
		return 0, false
	}
	// A single key (including the frame-zero fallback) bypasses the equality
	// scan. Other frame values cannot prevent reference copying.
	if len(keys) == 1 {
		return reference, true
	}
	selected, _ := transitionKeyframeFlags(len(values), keys)
	first, present := values[0][name]
	if !present {
		return 0, false
	}
	for i, frame := range values {
		if !selected[i] && i != 0 && i != len(values)-1 {
			continue
		}
		value, present := frame[name]
		// Java's numeric toString distinguishes signed Double zero. Integer
		// cells have no signed zero.
		if !present || value != first || (!integer && value == 0 && math.Signbit(value) != math.Signbit(first)) {
			return 0, false
		}
	}
	return reference, true
}
