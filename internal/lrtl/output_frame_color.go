package lrtl

import "encoding/binary"

// Native temporal frames keep interleaved Y/U/V uint16 samples in the same
// six-byte pixel layout as RGB48. This allows bounded queues and cropping to
// retain their existing layout, without converting filtered samples to RGB.
type outputFrameColor bool

func (native outputFrameColor) planes(frame []byte, width, height int) [3][]uint16 {
	if !native {
		return outputYUV10(frame, width, height)
	}
	var planes [3][]uint16
	for p := range planes {
		planes[p] = make([]uint16, width*height)
		for i := range planes[p] {
			planes[p][i] = binary.LittleEndian.Uint16(frame[i*6+p*2:])
		}
	}
	return planes
}

func (native outputFrameColor) apply(frame []byte, original, filtered [3][]uint16) {
	for i := range filtered[0] {
		if native {
			for p := range filtered {
				binary.LittleEndian.PutUint16(frame[i*6+p*2:], filtered[p][i])
			}
			continue
		}
		var delta [3]float64
		for p := range delta {
			delta[p] = float64(int(filtered[p][i])-int(original[p][i])) * (65535.0 / 1023)
		}
		addOutputYUVDelta(frame[i*6:], delta)
	}
}

func nativeSDRWorkingFrame(rgb []byte, width, height int) ([]byte, error) {
	planes, err := nativeSDRPlanes(rgb, width, height)
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(rgb))
	outputFrameColor(true).apply(out, planes, planes)
	return out, nil
}
