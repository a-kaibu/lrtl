package lrtl

import (
	"bytes"
	"fmt"
	"math"
)

func validateOutputDenoise(level int) error {
	if level < 0 || level > 4 {
		return fmt.Errorf("denoise must be 0 (disabled) or a level from 1 to 4")
	}
	return nil
}

func validateOutputDenoiseMemory(width, height, count, level, blur int) error {
	if level == 0 {
		return nil
	}
	settings := outputDenoisePreset(level)
	perFrame := uint64(12)
	if level == 4 {
		perFrame = 18
	}
	perPixel := uint64(min(count, settings.window))*perFrame + 40 + uint64(max(1, blur))*6
	if uint64(width)*uint64(height)*perPixel > 768*1024*1024 {
		return fmt.Errorf("denoise frame-buffer estimate exceeds 768 MiB; reduce --denoise, --motion-blur or output dimensions")
	}
	return nil
}

type denoiseInputFrame struct {
	rgb             []byte
	original, clean [3][]uint16
}

type outputDenoiser struct {
	width, height, count, level, next, emitted int
	cache                                      map[int]denoiseInputFrame
	load                                       func(int) ([]byte, error)
}

func newOutputDenoiser(width, height, count, level int, load func(int) ([]byte, error)) *outputDenoiser {
	return &outputDenoiser{width: width, height: height, count: count, level: level, cache: make(map[int]denoiseInputFrame), load: load}
}

func (d *outputDenoiser) frame(index int) ([]byte, error) {
	if index != d.emitted || index >= d.count {
		return nil, fmt.Errorf("denoise requires sequential source frames")
	}
	settings := outputDenoisePreset(d.level)
	radius := settings.window / 2
	for key := range d.cache {
		if key < max(0, index-radius) {
			delete(d.cache, key)
		}
	}
	for d.next <= min(d.count-1, index+radius) {
		rgb, err := d.load(d.next)
		if err != nil {
			return nil, fmt.Errorf("denoise source frame %d: %w", d.next+1, err)
		}
		if len(rgb) != d.width*d.height*6 {
			return nil, fmt.Errorf("denoise source frame %d has inconsistent dimensions", d.next+1)
		}
		planes := outputYUV10(rgb, d.width, d.height)
		frame := denoiseInputFrame{rgb: rgb, original: planes, clean: planes}
		if d.level == 4 {
			for p := range frame.clean {
				frame.clean[p] = denoisePreclean10(planes[p], d.width, d.height)
			}
		}
		d.cache[d.next] = frame
		d.next++
	}
	center := d.cache[index]
	var filtered [3][]uint16
	for p := range filtered {
		window := make([][]uint16, settings.window)
		for i := range window {
			position := max(0, index+i-radius)
			if position >= d.count {
				// The native EOF drain repeats frame count-radius, rather than
				// always repeating the last frame. Short sequences use frame 0.
				position = max(0, d.count-radius)
			}
			window[i] = d.cache[position].clean[p]
		}
		temporal := temporalDenoise10(window, settings.a[p], settings.b[p])
		filtered[p] = waveletDenoise10(temporal, d.width, d.height, settings)
	}
	rgb := bytes.Clone(center.rgb)
	for i := 0; i < d.width*d.height; i++ {
		var delta [3]float64
		for p := range delta {
			delta[p] = float64(int(filtered[p][i])-int(center.original[p][i])) * (65535.0 / 1023)
		}
		addOutputYUVDelta(rgb[i*6:], delta)
	}
	d.emitted++
	return rgb, nil
}

type outputDenoiseSettings struct {
	window, steps int
	a, b          [3]float32
	threshold     float32
	soft          bool
}

func outputDenoisePreset(level int) outputDenoiseSettings {
	switch level {
	case 1:
		return outputDenoiseSettings{5, 2, [3]float32{.03, .10, .02}, [3]float32{.04, .10, .04}, 2, true}
	case 2:
		return outputDenoiseSettings{5, 6, [3]float32{.05, .20, .20}, [3]float32{.15, .40, .40}, 4, false}
	case 3:
		return outputDenoiseSettings{7, 6, [3]float32{.08, .20, .20}, [3]float32{.17, .60, .60}, 4, false}
	case 4:
		return outputDenoiseSettings{13, 10, [3]float32{.10, .30, .30}, [3]float32{.20, .80, .80}, 6, false}
	}
	return outputDenoiseSettings{}
}

// temporalDenoise10 walks outward in alternating past/future order. A failed
// threshold ends the joint walk, including when only one side has failed.
// Missing endpoints are supplied by the caller's sequence padding policy.
func temporalDenoise10(window [][]uint16, a, b float32) []uint16 {
	middle := len(window) / 2
	output := make([]uint16, len(window[middle]))
	perSample, cumulative := int(a*1024-1), int(b*1024-1)
	for pixel, center := range window[middle] {
		sum, count := int(center), 1
		var distances [2]int
		for distance := 1; distance <= middle; distance++ {
			stop := false
			for side, offset := range [2]int{-distance, distance} {
				v := int(window[middle+offset][pixel])
				diff := absInt(v - int(center))
				distances[side] += diff
				if diff > perSample || distances[side] > cumulative {
					stop = true
					break
				}
				sum += v
				count++
			}
			if stop {
				break
			}
		}
		output[pixel] = uint16((sum + count/2) / count)
	}
	return output
}

// CDF 9/7 analysis and synthesis coefficients. The implementation uses a pull
// convolution with explicit symmetric boundaries, independently of the native
// filter's padded buffers. Each arithmetic step is rounded to float32.
var waveletAnalysisLow = [...]float32{.037828455506995, -.023849465019380, -.110624404418423, .377402855612654, .852698679009403, .377402855612654, -.110624404418423, -.023849465019380, .037828455506995}
var waveletAnalysisHigh = [...]float32{-.064538882628938, .040689417609558, .418092273222212, -.788485616405664, .418092273222212, .040689417609558, -.064538882628938}
var waveletSynthesisLow = [...]float32{-.064538882628938, -.040689417609558, .418092273222212, .788485616405664, .418092273222212, -.040689417609558, -.064538882628938}
var waveletSynthesisHigh = [...]float32{-.037828455506995, -.023849465019380, .110624404418423, .377402855612654, -.852698679009403, .377402855612654, .110624404418423, -.023849465019380, -.037828455506995}

func reflectedWaveletIndex(index, count int, repeatLeft, repeatRight bool) int {
	if count <= 1 {
		return 0
	}
	for index < 0 || index >= count {
		if index < 0 {
			index = -index
			if repeatLeft {
				index--
			}
		} else {
			index = 2*count - 2 - index
			if repeatRight {
				index++
			}
		}
	}
	return index
}

func analyzeWaveletLine(input, output []float32) {
	low := (len(input) + 1) / 2
	for i := range output {
		center, kernel := 2*i, waveletAnalysisLow[:]
		if i >= low {
			center, kernel = 2*(i-low)+1, waveletAnalysisHigh[:]
		}
		sum := float32(0)
		for k, coefficient := range kernel {
			position := reflectedWaveletIndex(center+k-len(kernel)/2, len(input), false, false)
			term := float32(input[position] * coefficient)
			sum = float32(sum + term)
		}
		output[i] = sum
	}
}

func synthesizeWaveletLine(input, output []float32) {
	low := (len(input) + 1) / 2
	for x := range output {
		sum := float32(0)
		for band := 0; band < 2; band++ {
			coefficients, kernel, phase := input[:low], waveletSynthesisLow[:], 0
			left, right := false, len(input)%2 == 0
			if band == 1 {
				coefficients, kernel, phase = input[low:], waveletSynthesisHigh[:], 1
				left, right = true, len(input)%2 != 0
			}
			radius := len(kernel) / 2
			// Include all possible contributing centers, in ascending order.
			for i := (x-phase-radius)/2 - 1; i <= (x-phase+radius)/2+1; i++ {
				tap := x - (2*i + phase) + radius
				if tap < 0 || tap >= len(kernel) {
					continue
				}
				position := reflectedWaveletIndex(i, len(coefficients), left, right)
				term := float32(coefficients[position] * kernel[tap])
				sum = float32(sum + term)
			}
		}
		output[x] = sum
	}
}

func waveletDenoise10(input []uint16, width, height int, settings outputDenoiseSettings) []uint16 {
	steps, bound := 0, 1
	for bound < min(width, height) && steps < 15 {
		bound *= 2
		steps++
	}
	steps = max(0, min(settings.steps, steps-2))
	block := make([]float32, len(input))
	for i, v := range input {
		block[i] = float32(v)
	}
	line := make([]float32, max(width, height))
	result := make([]float32, len(line))
	sizes := make([][2]int, 0, steps)
	w, h := width, height
	transform := func(w, h int, inverse bool) {
		rows := func() {
			for y := 0; y < h; y++ {
				copy(line[:w], block[y*width:y*width+w])
				if inverse {
					synthesizeWaveletLine(line[:w], result[:w])
				} else {
					analyzeWaveletLine(line[:w], result[:w])
				}
				copy(block[y*width:y*width+w], result[:w])
			}
		}
		columns := func() {
			for x := 0; x < w; x++ {
				for y := 0; y < h; y++ {
					line[y] = block[y*width+x]
				}
				if inverse {
					synthesizeWaveletLine(line[:h], result[:h])
				} else {
					analyzeWaveletLine(line[:h], result[:h])
				}
				for y := 0; y < h; y++ {
					block[y*width+x] = result[y]
				}
			}
		}
		if inverse {
			columns()
			rows()
		} else {
			rows()
			columns()
		}
	}
	for range steps {
		sizes = append(sizes, [2]int{w, h})
		transform(w, h, false)
		w, h = (w+1)/2, (h+1)/2
	}
	threshold := settings.threshold * 4 // Native threshold is specified in 8-bit units.
	percent := float32(80)
	fraction := float32(percent * float32(.01))
	remainder := float32(1 - fraction)
	shift := float32(float32(threshold*float32(.01)) * percent)
	squared := float32(float32(threshold*threshold) * fraction)
	for i, value := range block {
		magnitude := float32(math.Abs(float64(value)))
		switch {
		case magnitude <= threshold:
			block[i] = float32(value * remainder)
		case settings.soft:
			block[i] = float32(math.Copysign(float64(float32(magnitude-shift)), float64(value)))
		default:
			power := float32(magnitude * magnitude)
			gain := float32(float32(power-squared) / power)
			block[i] = float32(value * gain)
		}
	}
	for i := len(sizes) - 1; i >= 0; i-- {
		transform(sizes[i][0], sizes[i][1], true)
	}
	output := make([]uint16, len(input))
	for i, value := range block {
		output[i] = uint16(min(1023, max(0, int(float32(value+float32(.5))))))
	}
	return output
}

// The strongest native preset first fills dark point defects with the lighter
// of the original and a blurred, twice-median-filtered frame.
func denoisePreclean10(input []uint16, width, height int) []uint16 {
	median := func(input []uint16) []uint16 {
		output := append([]uint16(nil), input...)
		for y := 1; y < height-1; y++ {
			for x := 1; x < width-1; x++ {
				var values [9]uint16
				count := 0
				for yy := y - 1; yy <= y+1; yy++ {
					for xx := x - 1; xx <= x+1; xx++ {
						v := input[yy*width+xx]
						position := count
						for position > 0 && values[position-1] > v {
							values[position] = values[position-1]
							position--
						}
						values[position] = v
						count++
					}
				}
				output[y*width+x] = values[4]
			}
		}
		return output
	}
	// The native removegrain branch negotiates 8-bit YUV. Its swscale
	// conversion uses the measured 2x2 quantization phases below; the other
	// blend input retains 10-bit samples.
	quantized := make([]uint16, len(input))
	phases := [2][2]int{{1, 2}, {3, 0}}
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			i := y*width + x
			quantized[i] = uint16(min(255, (int(input[i])+phases[y%2][x%2])/4))
		}
	}
	filtered := median(median(quantized))
	buffer := make([]float32, len(input))
	for i, v := range filtered {
		buffer[i] = float32(v)
	}
	const lambda = .125 // sigma^2 / 2, sigma=0.5 and one recursive step.
	decay := (1 + 2*lambda - math.Sqrt(1+4*lambda)) / (2 * lambda)
	feedback := float32(decay)
	boundary := float32(1 / (1 - decay))
	scale := float32(decay / lambda)
	scale = float32(scale * scale)
	pass := func(start, stride, count int) {
		buffer[start] = float32(buffer[start] * boundary)
		for i := 1; i < count; i++ {
			index := start + i*stride
			term := float32(feedback * buffer[index-stride])
			buffer[index] = float32(buffer[index] + term)
		}
		end := start + (count-1)*stride
		buffer[end] = float32(buffer[end] * boundary)
		for i := count - 2; i >= 0; i-- {
			index := start + i*stride
			term := float32(feedback * buffer[index+stride])
			buffer[index] = float32(buffer[index] + term)
		}
	}
	for y := 0; y < height; y++ {
		pass(y*width, 1, width)
	}
	for x := 0; x < width; x++ {
		pass(x, width, height)
	}
	output := make([]uint16, len(input))
	for i, value := range buffer {
		blur := int(math.RoundToEven(float64(float32(value * scale))))
		output[i] = max(input[i], uint16(min(255, max(0, blur))*4))
	}
	return output
}
