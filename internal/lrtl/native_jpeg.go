package lrtl

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"math"

	"github.com/a-kaibu/lrtl/internal/nativejpeg"
)

func decodeRenderImage(data []byte, native bool) (image.Image, string, error) {
	if native && len(data) >= 2 && data[0] == 0xff && data[1] == 0xd8 {
		img, err := nativejpeg.Decode(bytes.NewReader(data))
		return img, "jpeg", err
	}
	return image.Decode(bytes.NewReader(data))
}

type nativeChromaTap struct {
	position int
	weights  []int64
}

// Quantize normalized Lanczos weights with an error carried between taps.
// Border samples are combined before quantization, as in the measured scaler.
func nativeChromaWeights(source, target, one int) []nativeChromaTap {
	result := make([]nativeChromaTap, target)
	if source == target {
		for i := range result {
			result[i] = nativeChromaTap{i, []int64{int64(one)}}
		}
		return result
	}
	count := min(7, max(1, source-2))
	step := (int64(source)*65536 + int64(target/2)) / int64(target)
	kernels := make([]nativeChromaTap, target)
	for x := range kernels {
		center := step - 65536 + int64(x)*2*step
		start := int((center - int64(count-2)*65536) / 131072)
		raw := make([]int64, count)
		for j := range raw {
			distance := math.Abs(float64(int64(start+j)*131072-center)) / 131072
			weight := 0.0
			if distance == 0 {
				weight = 1
			} else if distance <= 3 {
				weight = math.Sin(distance*math.Pi) * math.Sin(distance*math.Pi/3) / (distance * distance * math.Pi * math.Pi / 3)
			}
			raw[j] = int64(weight * float64(int64(1)<<54))
		}
		kernels[x] = nativeChromaTap{start, raw}
	}
	// Remove tiny leading taps while keeping source positions monotonic.
	minimum := 0
	const cutoff = int64(1<<54) / 500
	for x := len(kernels) - 1; x >= 0; x-- {
		kernel := &kernels[x]
		removed := int64(0)
		for j := 0; j < count; j++ {
			removed += absNativeChroma(kernel.weights[0])
			if removed > cutoff || x+1 < len(kernels) && kernel.position >= kernels[x+1].position {
				break
			}
			copy(kernel.weights, kernel.weights[1:])
			kernel.weights[count-1] = 0
			kernel.position++
		}
		length := count
		removed = 0
		for j := count - 1; j > 0; j-- {
			removed += absNativeChroma(kernel.weights[j])
			if removed > cutoff {
				break
			}
			length--
		}
		minimum = max(minimum, length)
	}
	for x := range result {
		kernel := kernels[x]
		start := kernel.position
		left, right := max(0, min(source-1, start)), max(0, min(source-1, start+minimum-1))
		raw := make([]int64, right-left+1)
		for j := 0; j < minimum; j++ {
			raw[max(0, min(source-1, start+j))-left] += kernel.weights[j]
		}
		sum := int64(0)
		for _, v := range raw {
			sum += v
		}
		divisor := (sum + int64(one)/2) / int64(one)
		if divisor == 0 {
			divisor = 1
		}
		error := int64(0)
		for i := range raw {
			value := raw[i] + error
			quantized := (value + divisor/2) / divisor
			if value < 0 {
				quantized = -((-value + divisor/2) / divisor)
			}
			raw[i] = quantized
			error = value - quantized*divisor
		}
		result[x] = nativeChromaTap{left, raw}
	}
	return result
}

func absNativeChroma(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}

func nativeJPEGWorkingFrame(img *image.YCbCr) ([]byte, error) {
	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width <= 0 || height <= 0 || uint64(width)*uint64(height) > 64*1024*1024 {
		return nil, fmt.Errorf("invalid native JPEG dimensions")
	}
	horizontal, vertical := 1, 1
	switch img.SubsampleRatio {
	case image.YCbCrSubsampleRatio444:
	case image.YCbCrSubsampleRatio422:
		horizontal = 2
	case image.YCbCrSubsampleRatio420:
		horizontal, vertical = 2, 2
	case image.YCbCrSubsampleRatio440:
		vertical = 2
	case image.YCbCrSubsampleRatio411:
		horizontal = 4
	case image.YCbCrSubsampleRatio410:
		horizontal, vertical = 4, 2
	default:
		return nil, fmt.Errorf("unsupported JPEG subsampling")
	}
	chromaWidth, chromaHeight := (width+horizontal-1)/horizontal, (height+vertical-1)/vertical
	if horizontal == 1 && vertical == 1 {
		working := make([]byte, width*height*6)
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				at := img.COffset(bounds.Min.X+x, bounds.Min.Y+y)
				values := [3]byte{img.Y[img.YOffset(bounds.Min.X+x, bounds.Min.Y+y)], img.Cb[at], img.Cr[at]}
				for p, v := range values {
					value := uint16(v) << 2
					if p == 0 {
						value |= uint16(v) >> 6
					}
					binary.LittleEndian.PutUint16(working[(y*width+x)*6+p*2:], value)
				}
			}
		}
		return working, nil
	}
	hx, vy := nativeChromaWeights(chromaWidth, width, 16384), nativeChromaWeights(chromaHeight, height, 4096)
	working := make([]byte, width*height*6)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			binary.LittleEndian.PutUint16(working[(y*width+x)*6:], uint16(img.Y[img.YOffset(bounds.Min.X+x, bounds.Min.Y+y)])*4)
		}
	}
	for plane, source := range [][]byte{img.Cb, img.Cr} {
		rows := make(map[int][]int64)
		var reusable [][]int64
		base := img.COffset(bounds.Min.X, bounds.Min.Y)
		for y, tap := range vy {
			for key, row := range rows {
				if key < tap.position || key >= tap.position+len(tap.weights) {
					reusable = append(reusable, row)
					delete(rows, key)
				}
			}
			for j := range tap.weights {
				rowIndex := tap.position + j
				if _, ok := rows[rowIndex]; ok {
					continue
				}
				var row []int64
				if n := len(reusable); n > 0 {
					row = reusable[n-1]
					reusable = reusable[:n-1]
				} else {
					row = make([]int64, width)
				}
				for x, horizontalTap := range hx {
					sum := int64(0)
					for k, weight := range horizontalTap.weights {
						sum += int64(source[base+rowIndex*img.CStride+horizontalTap.position+k]) * weight
					}
					row[x] = min(32767, sum>>7)
				}
				rows[rowIndex] = row
			}
			for x := 0; x < width; x++ {
				sum := int64(1 << 16)
				for j, weight := range tap.weights {
					sum += rows[tap.position+j][x] * weight
				}
				binary.LittleEndian.PutUint16(working[(y*width+x)*6+(plane+1)*2:], uint16(max(0, min(1023, sum>>17))))
			}
		}
	}
	return working, nil
}

func nativeJPEGGrayWorkingFrame(img *image.Gray) ([]byte, error) {
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w <= 0 || h <= 0 || uint64(w)*uint64(h) > 64*1024*1024 {
		return nil, fmt.Errorf("invalid native JPEG dimensions")
	}
	working := make([]byte, w*h*6)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := uint16(img.GrayAt(bounds.Min.X+x, bounds.Min.Y+y).Y)
			pixel := working[(y*w+x)*6:]
			binary.LittleEndian.PutUint16(pixel, v<<2|v>>6)
			binary.LittleEndian.PutUint16(pixel[2:], 512)
			binary.LittleEndian.PutUint16(pixel[4:], 512)
		}
	}
	return working, nil
}
