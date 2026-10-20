package lrtl

import (
	"bytes"
	"fmt"
	"image"
	"math"
)

// Render deflicker operates on extracted Y/U/V planes before noise reduction.
// This is independent of the exposure/XMP deflicker analysis workflow.
func outputDeflickerMean(plane []uint16) float32 {
	var sum uint64
	for _, v := range plane {
		sum += uint64(v)
	}
	return float32(sum) / float32(len(plane))
}

func outputDeflickerFactor(means [10]float32) float32 {
	var total float32
	for _, mean := range means {
		total += float32(math.Pow(float64(mean), 10))
	}
	return float32(math.Pow(float64(total/10), float64(float32(.1)))) / means[0]
}

func outputDeflickerPlane(plane []uint16, means [10]float32) []uint16 {
	factor := outputDeflickerFactor(means)
	out := make([]uint16, len(plane))
	for i, v := range plane {
		value := float32(v) * factor
		if math.IsNaN(float64(value)) || value < 0 {
			value = 0
		}
		out[i] = uint16(min(1023, value))
	}
	return out
}

type outputDeflicker struct {
	width, height, count, next, emitted, available int
	luma, chroma                                   bool
	means                                          [3][10]float32
	lastMean                                       [3]float32
	cache                                          map[int][]byte
	load                                           func(int) ([]byte, error)
}

func newOutputDeflicker(width, height, count int, luma, chroma bool, load func(int) ([]byte, error)) *outputDeflicker {
	return &outputDeflicker{width: width, height: height, count: count, luma: luma, chroma: chroma, cache: make(map[int][]byte), load: load}
}

func (d *outputDeflicker) read() ([3]float32, error) {
	var means [3]float32
	rgb, err := d.load(d.next)
	if err != nil {
		return means, fmt.Errorf("output deflicker source frame %d: %w", d.next+1, err)
	}
	if len(rgb) != d.width*d.height*6 {
		return means, fmt.Errorf("output deflicker source frame %d has inconsistent dimensions", d.next+1)
	}
	planes := outputYUV10(rgb, d.width, d.height)
	for p := range planes {
		means[p] = outputDeflickerMean(planes[p])
	}
	d.cache[d.next] = rgb
	d.next++
	d.lastMean = means
	return means, nil
}

func (d *outputDeflicker) frame(index int) ([]byte, error) {
	if index != d.emitted || index >= d.count {
		return nil, fmt.Errorf("output deflicker requires sequential frames")
	}
	if index == 0 {
		d.available = min(10, d.count)
		for i := 0; i < d.available; i++ {
			means, err := d.read()
			if err != nil {
				return nil, err
			}
			for p := range means {
				d.means[p][i] = means[p]
			}
		}
	} else {
		delete(d.cache, index-1)
		means := d.lastMean
		at := d.available - 1
		if d.next < d.count {
			var err error
			means, err = d.read()
			if err != nil {
				return nil, err
			}
		} else {
			d.available--
		}
		for p := range means {
			copy(d.means[p][:9], d.means[p][1:])
			d.means[p][at] = means[p]
		}
	}
	rgb := bytes.Clone(d.cache[index])
	original := outputYUV10(rgb, d.width, d.height)
	filtered := original
	for p := range filtered {
		if (p == 0 && !d.luma) || (p > 0 && !d.chroma) {
			continue
		}
		filtered[p] = outputDeflickerPlane(original[p], d.means[p])
	}
	for i := 0; i < d.width*d.height; i++ {
		var delta [3]float64
		for p := range delta {
			delta[p] = float64(int(filtered[p][i])-int(original[p][i])) * (65535.0 / 1023)
		}
		addOutputYUVDelta(rgb[i*6:], delta)
	}
	d.emitted++
	return rgb, nil
}

func cropOutputRGB48(rgb []byte, width int, bounds image.Rectangle) []byte {
	if bounds.Min.X == 0 && bounds.Min.Y == 0 && bounds.Dx() == width && bounds.Dy()*width*6 == len(rgb) {
		return rgb
	}
	out := make([]byte, bounds.Dx()*bounds.Dy()*6)
	for y := 0; y < bounds.Dy(); y++ {
		start := ((y+bounds.Min.Y)*width + bounds.Min.X) * 6
		copy(out[y*bounds.Dx()*6:], rgb[start:start+bounds.Dx()*6])
	}
	return out
}

func validateOutputDeflickerMemory(width, height, outputWidth, outputHeight, count, noise, airplanes, blur int) error {
	// Ten RGB48 source frames, plane conversion, selected outputs and result.
	bytes := uint64(width) * uint64(height) * uint64(min(count, 10)*6+30)
	perPixel := uint64(max(1, blur)*6 + 24)
	if airplanes > 0 {
		perPixel += uint64(min(count, 2*airplaneRadius(airplanes)+1)*12 + 48)
	}
	if noise > 0 {
		perFrame := 12
		if noise == 4 {
			perFrame = 18
		}
		perPixel += uint64(min(count, outputDenoisePreset(noise).window) * perFrame)
	}
	bytes += uint64(outputWidth) * uint64(outputHeight) * perPixel
	if bytes > 768*1024*1024 {
		return fmt.Errorf("output deflicker frame-buffer estimate exceeds 768 MiB; reduce output dimensions or temporal filters")
	}
	return nil
}
