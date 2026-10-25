package lrtl

import (
	"fmt"
	"path/filepath"
	"sort"
)

// The native date comparator uses xmp:CreateDate text without parsing dates or
// normalizing offsets. Equal dates fall back to case-insensitive filename text,
// not numeric filename order. Case-insensitive ties preserve the input order.
func lrtReferenceDateOrder(names, dates []string) ([]int, error) {
	if len(names) != len(dates) {
		return nil, fmt.Errorf("LRT reference date/name count mismatch")
	}
	missing := 0
	for _, date := range dates {
		if date == "" {
			missing++
		}
	}
	if missing > 0 && missing < len(dates) {
		// Native pairwise filename fallback can violate transitivity when only
		// some dates are missing. Refuse an unprovable image/box mapping.
		return nil, fmt.Errorf("LRT reference date order requires xmp:CreateDate on all frames, or no dates on any frame")
	}
	order := make([]int, len(names))
	for i := range order {
		order[i] = i
	}
	nameKeys, dateKeys := make([]string, len(names)), make([]string, len(dates))
	for i := range names {
		nameKeys[i], dateKeys[i] = javaOrderKey(names[i]), javaOrderKey(dates[i])
	}
	sort.SliceStable(order, func(a, b int) bool {
		i, j := order[a], order[b]
		if dates[i] == dates[j] || dates[i] == "" || dates[j] == "" {
			return nameKeys[i] < nameKeys[j]
		}
		return dateKeys[i] < dateKeys[j]
	})
	return order, nil
}

func readLRTReferenceDateOrder(frames []Frame) ([]int, error) {
	names, dates := make([]string, len(frames)), make([]string, len(frames))
	for i, frame := range frames {
		names[i] = filepath.Base(frame.Image)
		packet, err := PacketForFrame(frame)
		if err != nil {
			return nil, err
		}
		value, _, err := captureTimestampScalar(packet, "xmp:CreateDate")
		if err != nil {
			return nil, fmt.Errorf("%s: %w", frame.Image, err)
		}
		dates[i] = value
	}
	return lrtReferenceDateOrder(names, dates)
}
