package lrtl

import (
	"fmt"
	"path/filepath"
)

type intervalImportGroup struct {
	indices []int
	singles bool
}

// Group outgoing frame intervals in milliseconds. At a changed interval, a
// run of at least two frames retains the boundary frame; shorter runs end
// before it. Short runs are collected into one final group without deletion.
func groupLRTImportIntervals(intervals []int64, tolerance int64, minimum int) []intervalImportGroup {
	var runs [][]int
	var current, unknown []int
	flush := func() {
		if len(current) > 0 {
			runs = append(runs, current)
		}
		current = nil
	}
	previous := int64(-1)
	for i, interval := range intervals {
		if interval <= 0 && previous <= 0 {
			unknown = append(unknown, i)
			continue
		}
		delta := interval - previous
		if delta < 0 {
			delta = -delta
		}
		if previous >= 0 && delta > tolerance {
			if len(current) > 1 {
				current = append(current, i)
				flush()
			} else {
				flush()
				current = append(current, i)
			}
		} else {
			current = append(current, i)
		}
		previous = interval
	}
	flush()
	if len(unknown) > 0 {
		runs = append(runs, unknown)
	}
	var groups []intervalImportGroup
	var singles []int
	for _, run := range runs {
		if len(run) < minimum {
			singles = append(singles, run...)
		} else {
			groups = append(groups, intervalImportGroup{indices: run})
		}
	}
	if len(singles) > 0 {
		groups = append(groups, intervalImportGroup{indices: singles, singles: true})
	}
	return groups
}

func splitImportByIntervals(items []importItem, options ImportOptions) {
	intervals := make([]int64, len(items))
	for i := range items {
		intervals[i] = -1
		if i+1 < len(items) {
			intervals[i] = items[i+1].captureTime.UnixMilli() - items[i].captureTime.UnixMilli()
		}
	}
	minimum := options.MinimumSequenceLength
	if minimum == 0 {
		minimum = 5
	}
	groups := groupLRTImportIntervals(intervals, options.IntervalTolerance.Milliseconds(), minimum)
	for i, group := range groups {
		folder := "single_images"
		if !group.singles {
			folder = fmt.Sprintf("sequence_%03d_%s", i+1, items[group.indices[0]].captureTime.Format("20060102_150405"))
		}
		for _, index := range group.indices {
			items[index].destination = filepath.Join(folder, items[index].destination)
		}
	}
}
