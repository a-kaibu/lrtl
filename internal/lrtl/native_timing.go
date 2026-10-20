package lrtl

import (
	"fmt"
	"math"
	"strconv"
)

type renderTiming struct {
	fade          videoFade
	audioDuration string // Empty selects actual rational output duration.
}

func planRenderTiming(options RenderOptions, sourceCount, actualCount, trimWindow int, rate frameRate, label string) (renderTiming, error) {
	if sourceCount <= 0 || actualCount <= 0 {
		return renderTiming{}, fmt.Errorf("render timing requires nonempty input and output")
	}
	frames := actualCount
	audioDuration := ""
	if options.NativeTiming {
		speed, err := options.playbackSpeed()
		if err != nil {
			return renderTiming{}, err
		}
		// Native graph duration uses int(Float(sourceCount)/Float(speed)),
		// independently of the actual fps filter's PTS truncation schedule.
		frames = int(math.Min(float64(float32(sourceCount)/float32(speed)), math.MaxInt32)) - trimWindow
		if frames <= 0 {
			return renderTiming{}, fmt.Errorf("native estimated duration leaves no output frames")
		}
		audioDuration = strconv.FormatFloat(float64(displayedVideoDuration(frames, rate, label)), 'f', 3, 64)
	}
	fade, err := planVideoFade(frames, rate, label, options.FadeOutSeconds)
	if err != nil {
		return renderTiming{}, err
	}
	return renderTiming{fade: fade, audioDuration: audioDuration}, nil
}
