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
	return planFilteredVideoFrames(sourceCount, speed, window, 0)
}

func planFilteredVideoFrames(sourceCount int, speed float64, window, airplanes int) (videoFramePlan, error) {
	if err := validateRemoveAirplanes(airplanes); err != nil {
		return videoFramePlan{}, err
	}
	if window != 0 && !validMotionBlurFrames(window) {
		return videoFramePlan{}, fmt.Errorf("motion blur window must be 0 (disabled) or one of 2, 3, 4, 5, 10, 15, or 20 frames: %d", window)
	}
	validatedSpeed, err := validatePlaybackSpeed(speed)
	if err != nil {
		return videoFramePlan{}, err
	}
	speed = validatedSpeed
	filteredCount := airplaneSourceCount(sourceCount, airplanes)
	if filteredCount <= 0 && airplanes > 0 {
		return videoFramePlan{}, fmt.Errorf("airplane removal leaves no frames; add more source images or reduce --remove-airplanes")
	}
	count, err := playbackFrameCount(airplaneTimelineSourceCount(sourceCount, speed, airplanes), speed)
	if err != nil {
		return videoFramePlan{}, err
	}
	plan := videoFramePlan{end: count}
	if window != 0 || airplanes != 0 {
		head, tail := nativeFilterTrim(window, airplanes)
		plan.start = head
		// The native trim end uses Java float division and an int cast. Actual
		// fps output can be shorter; trim cannot manufacture missing frames.
		nativeEnd := float64(float32(sourceCount) / float32(speed))
		plan.end = min(count, int(math.Min(nativeEnd, math.MaxInt32))-tail)
	}
	if plan.count() <= 0 {
		if airplanes == 0 {
			return videoFramePlan{}, fmt.Errorf("motion blur removes all output frames; reduce the window or playback speed, or add more source images")
		}
		return videoFramePlan{}, fmt.Errorf("temporal filters remove all output frames; reduce the filter level, window or playback speed, or add more source images")
	}
	return plan, nil
}

func nativeFilterTrim(blur, airplanes int) (head, tail int) {
	head = blur
	if airplanes > 0 {
		head = max(1, head)
		tail = 1
	}
	return
}
