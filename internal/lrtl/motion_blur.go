package lrtl

import (
	"fmt"
	"math"
)

// Bounds refer to the resampled timeline before trimming. Native tmix keeps
// its history, then trim discards N output frames (not N-1 input frames).
type videoFramePlan struct{ start, end int }

func (plan videoFramePlan) count() int { return plan.end - plan.start }

func planVideoFrames(sourceCount int, speed float64, window int) (videoFramePlan, error) {
	if window != 0 && !validMotionBlurFrames(window) {
		return videoFramePlan{}, fmt.Errorf("motion blur window must be 0 (disabled) or one of 2, 3, 4, 5, 10, 15, or 20 frames: %d", window)
	}
	count, err := playbackFrameCount(sourceCount, speed)
	if err != nil {
		return videoFramePlan{}, err
	}
	plan := videoFramePlan{end: count}
	if window != 0 {
		plan.start = window
		// The native trim end uses Java float division and an int cast. Actual
		// fps output can be shorter; trim cannot manufacture missing frames.
		nativeEnd := float64(float32(sourceCount) / float32(speed))
		plan.end = min(count, int(math.Min(nativeEnd, math.MaxInt32)))
	}
	if plan.count() <= 0 {
		return videoFramePlan{}, fmt.Errorf("motion blur removes all output frames; reduce the window or playback speed, or add more source images")
	}
	return plan, nil
}
