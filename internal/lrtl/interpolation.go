package lrtl

import (
	"bytes"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
)

// Interpolation is performed before setpts/fps. Preserve the intermediate
// rational timebase, including Java float formatting to three decimal places.
type interpolationClock struct {
	speed float64
	ratio *big.Rat // intermediate fps / source fps
}

func newInterpolationClock(rate frameRate, label string, speed float64) (interpolationClock, error) {
	speed, err := validatePlaybackSpeed(speed)
	if err != nil {
		return interpolationClock{}, err
	}
	value, err := strconv.ParseFloat(label, 32)
	if err != nil && strings.Contains(label, "/") {
		value = float64(rate.numerator) / float64(rate.denominator)
		err = nil
	}
	if err != nil || rate.numerator == 0 || rate.denominator == 0 {
		return interpolationClock{}, fmt.Errorf("invalid interpolation frame rate")
	}
	intermediate := float32(float32(value) / float32(speed))
	if intermediate <= 0 || math.IsNaN(float64(intermediate)) || math.IsInf(float64(intermediate), 0) {
		return interpolationClock{}, fmt.Errorf("invalid interpolation frame rate")
	}
	// Java Formatter uses half-up decimal rounding here. Go's formatted
	// decimal output uses ties-to-even, which differs at e.g. 47.8125 fps.
	rounded := big.NewRat(int64(math.Floor(float64(intermediate)*1000+.5)), 1000)
	ratio := new(big.Rat).Mul(rounded, new(big.Rat).SetFrac64(int64(rate.denominator), int64(rate.numerator)))
	return interpolationClock{speed, ratio}, nil
}

func rescaleInterpolation(value int, ratio *big.Rat, nearest bool) int {
	n := new(big.Int).Mul(big.NewInt(int64(value)), ratio.Num())
	if nearest {
		n.Lsh(n, 1)
		n.Add(n, ratio.Denom())
		return int(n.Quo(n, new(big.Int).Lsh(new(big.Int).Set(ratio.Denom()), 1)).Int64())
	}
	return int(n.Quo(n, ratio.Denom()).Int64())
}

func (clock interpolationClock) outputPTS(intermediate int) int {
	pts := int(float64(intermediate) * (1 / clock.speed))
	return rescaleInterpolation(pts, new(big.Rat).Inv(clock.ratio), true)
}

func (clock interpolationClock) count(sourceCount int) int {
	if sourceCount < 3 {
		return 0
	}
	// EOF is rescaled into the intermediate timebase, then setpts truncates,
	// and fps rounds to its output timebase. This can differ from N/speed.
	return clock.outputPTS(rescaleInterpolation(sourceCount, clock.ratio, true))
}

func (clock interpolationClock) last(sourceCount int) int {
	return rescaleInterpolation(sourceCount-2, clock.ratio, false)
}

func (clock interpolationClock) sample(intermediate, sourceCount int) (left, alpha int) {
	inverse := new(big.Rat).Inv(clock.ratio)
	left = min(sourceCount-3, rescaleInterpolation(intermediate, inverse, false))
	alpha = rescaleInterpolation(intermediate, new(big.Rat).Mul(inverse, big.NewRat(1024, 1)), true) - left*1024
	return left, min(1024, max(0, alpha))
}

func interpolationSceneChange(left, right []byte, previous float64) (bool, float64) {
	var sum uint64
	for i, v := range left {
		sum += uint64(absInt(int(v) - int(right[i])))
	}
	mafd := float64(sum) * 100 / float64(len(left)) / 256
	return float64(float32(min(mafd, math.Abs(mafd-previous)))) >= 10, mafd
}

func blendInterpolationPlane(left, right []byte, alpha int, scene bool) []byte {
	output := make([]byte, len(left))
	if scene {
		if alpha > 512 {
			copy(output, right)
		} else {
			copy(output, left)
		}
		return output
	}
	for i, v := range left {
		output[i] = byte((int(v)*(1024-alpha) + int(right[i])*alpha + 512) >> 10)
	}
	return output
}

func averageInterpolationPlane(window [][]byte) []byte {
	output := make([]byte, len(window[0]))
	for i := range output {
		sum := len(window) / 2
		for _, plane := range window {
			sum += int(plane[i])
		}
		output[i] = byte(sum / len(window))
	}
	return output
}

func validateInterpolation(mode int, speed float64) error {
	if mode < 0 || mode > 2 {
		return fmt.Errorf("interpolation must be 0 (off), 1 (blend), or 2 (motion compensated)")
	}
	if mode == 2 && speed < 1 {
		return fmt.Errorf("motion-compensated slow interpolation is not implemented; use --interpolation 1 for blend")
	}
	return nil
}

func planInterpolatedVideoFrames(count int, speed float64, blur, airplanes, mode int, rate frameRate, label string) (videoFramePlan, error) {
	speed, err := validatePlaybackSpeed(speed)
	if err != nil {
		return videoFramePlan{}, err
	}
	if err := validateInterpolation(mode, speed); err != nil {
		return videoFramePlan{}, err
	}
	plan, err := planFilteredVideoFrames(count, speed, blur, airplanes)
	if err != nil || mode == 0 || speed >= 1 {
		return plan, err
	}
	if airplaneSourceCount(count, airplanes) < 3 {
		return videoFramePlan{}, fmt.Errorf("blend interpolation needs at least three frames after light-trail filtering")
	}
	clock, err := newInterpolationClock(rate, label, speed)
	if err != nil {
		return videoFramePlan{}, err
	}
	plan.end = clock.count(count)
	if blur > 0 || airplanes > 0 {
		_, tail := nativeFilterTrim(blur, airplanes)
		plan.end = min(plan.end, int(min(float64(float32(count)/float32(speed)), float64(math.MaxInt32)))-tail)
	}
	if plan.count() <= 0 {
		return videoFramePlan{}, fmt.Errorf("interpolation and temporal filters leave no output frames")
	}
	return plan, nil
}

type interpolationInput struct {
	rgb    []byte
	planes [3][]uint16
	eight  [3][]byte
}

type playbackFrameStream struct {
	width, height, actual, total, mode, window, count, next, emitted, source, j int
	speed                                                                       float64
	clock                                                                       interpolationClock
	cache                                                                       map[int]interpolationInput
	scenes                                                                      map[int]bool
	previousMAFD                                                                float64
	load                                                                        func(int) ([]byte, error)
	lastRGB                                                                     []byte
	lastSource                                                                  int
}

func newPlaybackFrameStream(width, height, count, airplanes, mode int, speed float64, rate frameRate, label string, load func(int) ([]byte, error)) (*playbackFrameStream, error) {
	if err := validateInterpolation(mode, speed); err != nil {
		return nil, err
	}
	p := &playbackFrameStream{width: width, height: height, actual: airplaneSourceCount(count, airplanes), total: airplaneTimelineSourceCount(count, speed, airplanes), mode: mode, speed: speed, cache: make(map[int]interpolationInput), scenes: make(map[int]bool), load: load, lastSource: -1, window: 1}
	p.count = playbackOutputEnd(p.total, speed)
	if mode > 0 && speed < 1 {
		clock, err := newInterpolationClock(rate, label, speed)
		if err != nil {
			return nil, err
		}
		p.clock = clock
		p.count = 0
		if p.actual >= 3 {
			p.count = clock.count(count)
		}
	} else if mode > 0 {
		p.window = max(1, int(speed+.5))
	}
	return p, nil
}

func (p *playbackFrameStream) sample(target int) (source, alpha int) {
	if p.mode > 0 && p.speed < 1 {
		for p.j < p.clock.last(p.actual) && p.clock.outputPTS(p.j+1) <= target {
			p.j++
		}
		return p.clock.sample(p.j, p.actual)
	}
	for p.source+1 < p.total && playbackOutputEnd(p.source+1, p.speed) <= target {
		p.source++
	}
	return p.source, 0
}

func quantizeInterpolationPlane(plane []uint16, width int) []byte {
	output := make([]byte, len(plane))
	phases := [2][2]int{{1, 2}, {3, 0}}
	for i, v := range plane {
		output[i] = byte(min(255, (int(v)+phases[(i/width)%2][(i%width)%2])/4))
	}
	return output
}

func (p *playbackFrameStream) readThrough(last int) error {
	for p.next <= last {
		rgb, err := p.load(p.next)
		if err != nil {
			return fmt.Errorf("interpolation source frame %d: %w", p.next+1, err)
		}
		if len(rgb) != p.width*p.height*6 {
			return fmt.Errorf("interpolation source frame %d has inconsistent dimensions", p.next+1)
		}
		input := interpolationInput{rgb: rgb}
		if p.mode > 0 {
			input.planes = outputYUV10(rgb, p.width, p.height)
			if p.speed < 1 {
				for c := range input.eight {
					input.eight[c] = quantizeInterpolationPlane(input.planes[c], p.width)
				}
				if p.next > 0 {
					p.scenes[p.next-1], p.previousMAFD = interpolationSceneChange(p.cache[p.next-1].eight[0], input.eight[0], p.previousMAFD)
				}
			}
		}
		p.cache[p.next] = input
		keep := 1
		if p.mode > 0 {
			keep = max(3, p.window)
		}
		for key := range p.cache {
			if key < p.next-keep+1 {
				delete(p.cache, key)
				delete(p.scenes, key)
			}
		}
		p.next++
	}
	return nil
}

func (p *playbackFrameStream) frame(target int) ([]byte, int, error) {
	if target != p.emitted || target >= p.count {
		return nil, 0, fmt.Errorf("playback requires sequential output frames")
	}
	source, alpha := p.sample(target)
	anchor := min(source, p.actual-1)
	lookahead := anchor
	if p.mode > 0 && p.speed < 1 {
		lookahead = anchor + 2
	}
	if err := p.readThrough(lookahead); err != nil {
		return nil, 0, err
	}
	if p.lastSource == anchor && !(p.mode > 0 && p.speed < 1) {
		p.emitted++
		return bytes.Clone(p.lastRGB), source, nil
	}
	input := p.cache[anchor]
	rgb := bytes.Clone(input.rgb)
	if p.mode > 0 {
		filtered := input.planes
		for c := range filtered {
			filtered[c] = make([]uint16, p.width*p.height)
			if p.speed < 1 {
				blended := blendInterpolationPlane(input.eight[c], p.cache[anchor+1].eight[c], alpha, p.scenes[anchor])
				for i, v := range blended {
					filtered[c][i] = uint16(v) * 4
				}
			} else {
				for i := range filtered[c] {
					sum := p.window / 2
					for j := 0; j < p.window; j++ {
						sum += int(p.cache[max(0, anchor-p.window+1+j)].planes[c][i])
					}
					filtered[c][i] = uint16(sum / p.window)
				}
			}
		}
		for i := 0; i < p.width*p.height; i++ {
			var delta [3]float64
			for c := range delta {
				delta[c] = float64(int(filtered[c][i])-int(input.planes[c][i])) * (65535.0 / 1023)
			}
			addOutputYUVDelta(rgb[i*6:], delta)
		}
	}
	p.lastSource, p.lastRGB = anchor, rgb
	p.emitted++
	return bytes.Clone(rgb), source, nil
}

func (p *playbackFrameStream) finish() error { return p.readThrough(p.actual - 1) }

func validateInterpolationMemory(width, height, deflickerWidth, deflickerHeight, count, mode int, speed float64, options RenderOptions) error {
	if mode == 0 || speed == 1 {
		return nil
	}
	window := max(3, int(speed+.5))
	perPixel := uint64(min(count, window)*15 + 48 + max(1, options.MotionBlurFrames)*6)
	if options.OutputDenoise > 0 {
		perFrame := 12
		if options.OutputDenoise == 4 {
			perFrame = 18
		}
		perPixel += uint64(min(count, outputDenoisePreset(options.OutputDenoise).window) * perFrame)
	}
	if options.RemoveAirplanes > 0 {
		perPixel += uint64(min(count, 2*airplaneRadius(options.RemoveAirplanes)+1)*12 + 48)
	}
	estimate := uint64(width) * uint64(height) * perPixel
	if options.OutputDeflickerLuma || options.OutputDeflickerChroma {
		estimate += uint64(deflickerWidth) * uint64(deflickerHeight) * uint64(min(count, 10)*6+30)
	}
	if estimate > 768*1024*1024 {
		return fmt.Errorf("interpolation frame-buffer estimate exceeds 768 MiB; reduce output dimensions, speed or temporal filters")
	}
	return nil
}
