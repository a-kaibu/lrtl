package lrtl

import (
	"image"
	"image/color"
	"math"
)

type resizeTap struct {
	index  int
	weight float64
}

func lanczos3Weight(distance float64) float64 {
	if distance == 0 {
		return 1
	}
	if math.Abs(distance) >= 3 {
		return 0
	}
	x := math.Pi * distance
	return math.Sin(x) / x * math.Sin(x/3) / (x / 3)
}

// Pixel centers use half-pixel alignment. Downsampling widens the support
// before sampling to suppress aliasing. Out-of-image weights fold onto the
// closest edge sample, instead of truncating and renormalizing the kernel.
func lanczos3Contributions(input, output int) [][]resizeTap {
	result := make([][]resizeTap, output)
	if input == output {
		for i := range result {
			result[i] = []resizeTap{{index: i, weight: 1}}
		}
		return result
	}
	scale := math.Min(1, float64(output)/float64(input))
	for i := range result {
		center := (float64(i)+.5)*float64(input)/float64(output) - .5
		first := int(math.Ceil(center - 3/scale))
		last := int(math.Floor(center + 3/scale))
		sum := 0.
		for source := first; source <= last; source++ {
			weight := lanczos3Weight((float64(source) - center) * scale)
			if weight == 0 {
				continue
			}
			index := min(input-1, max(0, source))
			length := len(result[i])
			if length > 0 && result[i][length-1].index == index {
				result[i][length-1].weight += weight
			} else {
				result[i] = append(result[i], resizeTap{index: index, weight: weight})
			}
			sum += weight
		}
		for j := range result[i] {
			result[i][j].weight /= sum
		}
	}
	return result
}

// resizeFrame uses the Lanczos3 family selected by the native video graph.
// The independent floating-point RGB/alpha implementation does not reproduce
// every fixed-point/color-conversion step of FFmpeg's native scale operation.
func resizeFrame(img image.Image, width, height int) image.Image {
	bounds := img.Bounds()
	if bounds.Dx() == width && bounds.Dy() == height {
		return img
	}
	horizontal := lanczos3Contributions(bounds.Dx(), width)
	vertical := lanczos3Contributions(bounds.Dy(), height)
	result := newFrameImage(image.Rect(0, 0, width, height), img)
	output64, _ := result.(*image.NRGBA64)
	output8, _ := result.(*image.NRGBA)
	precise, hasRGBA64 := img.(image.RGBA64Image)
	// Retain only rows used by the current vertical window, rather than a
	// full-image floating-point intermediate (four float64 channels per pixel).
	rows := make(map[int][]float64)
	var available [][]float64
	for y, taps := range vertical {
		for index := range rows {
			if index < taps[0].index {
				available = append(available, rows[index])
				delete(rows, index)
			}
		}
		for _, tap := range taps {
			if _, found := rows[tap.index]; found {
				continue
			}
			var row []float64
			if len(available) > 0 {
				row = available[len(available)-1]
				available = available[:len(available)-1]
				clear(row)
			} else {
				row = make([]float64, width*4)
			}
			for x, contributions := range horizontal {
				for _, contribution := range contributions {
					var r, g, b, a uint32
					px, py := bounds.Min.X+contribution.index, bounds.Min.Y+tap.index
					if hasRGBA64 {
						pixel := precise.RGBA64At(px, py)
						r, g, b, a = uint32(pixel.R), uint32(pixel.G), uint32(pixel.B), uint32(pixel.A)
					} else {
						r, g, b, a = img.At(px, py).RGBA()
					}
					row[x*4] += float64(r) * contribution.weight
					row[x*4+1] += float64(g) * contribution.weight
					row[x*4+2] += float64(b) * contribution.weight
					row[x*4+3] += float64(a) * contribution.weight
				}
			}
			rows[tap.index] = row
		}
		for x := 0; x < width; x++ {
			var channels [4]float64
			for _, tap := range taps {
				row := rows[tap.index][x*4:]
				for channel := range channels {
					channels[channel] += row[channel] * tap.weight
				}
			}
			alpha := math.Max(0, math.Min(65535, channels[3]))
			pixel := color.NRGBA64{A: uint16(math.Round(alpha))}
			if alpha > 0 {
				convert := func(value float64) uint16 {
					return uint16(math.Round(math.Max(0, math.Min(65535, value*65535/alpha))))
				}
				pixel.R, pixel.G, pixel.B = convert(channels[0]), convert(channels[1]), convert(channels[2])
			}
			if output64 != nil {
				output64.SetNRGBA64(x, y, pixel)
			} else {
				output8.SetNRGBA(x, y, color.NRGBA{R: uint8(pixel.R >> 8), G: uint8(pixel.G >> 8), B: uint8(pixel.B >> 8), A: uint8(pixel.A >> 8)})
			}
		}
	}
	return result
}
