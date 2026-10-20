package lrtl

import (
	"fmt"
	"math"
)

func validatePlaybackSpeed(value float64) (float64, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 1.0/16 || value > 16 {
		return 0, fmt.Errorf("speed must be a finite number between 0.0625 and 16")
	}
	return float64(float32(value)), nil
}

func (options RenderOptions) playbackSpeed() (float64, error) {
	value := options.PlaybackSpeed
	if value == 0 {
		value = 1
	} // Existing Go callers omit video speed.
	return validatePlaybackSpeed(value)
}

// The native mode-0 filter uses setpts=(1/Float(speed))*PTS followed by fps.
// With equal input/output rate its integer timestamp interval for source i is
// [floor(i/speed),floor((i+1)/speed)). Empty intervals drop that source frame.
func playbackOutputEnd(sourceCount int, speed float64) int {
	return int(math.Floor(float64(sourceCount) * (1 / speed)))
}

func playbackFrameCount(sourceCount int, speed float64) (int, error) {
	value, err := validatePlaybackSpeed(speed)
	if err != nil {
		return 0, err
	}
	count := math.Floor(float64(sourceCount) * (1 / value))
	if sourceCount <= 0 || count < 1 {
		return 0, fmt.Errorf("speed leaves no output frames")
	}
	if count >= float64(int(^uint(0)>>1)) {
		return 0, fmt.Errorf("output frame count exceeds supported integer range")
	}
	return int(count), nil
}
