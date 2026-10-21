package lrtl

import (
	"encoding/binary"
	"fmt"
	"image"
	"math"
	"math/bits"

	"golang.org/x/sys/cpu"
)

func nativeScaleAlignment() (int, int) {
	if cpu.X86.HasSSE2 {
		return 4, 2
	}
	return 1, 1
}

func nativeScaleImageWorkingFrame(img image.Image, format string, targetW, targetH int, region image.Rectangle) ([]byte, error) {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	if w <= 0 || h <= 0 || targetW <= 0 || targetH <= 0 || region.Empty() || !region.In(image.Rect(0, 0, targetW, targetH)) || uint64(region.Dx())*uint64(region.Dy()) > 64*1024*1024 {
		return nil, fmt.Errorf("invalid native image scaling dimensions")
	}
	if w == targetW && h == targetH {
		working, err := nativeImageWorkingFrame(img, format)
		if err != nil {
			return nil, err
		}
		return cropOutputRGB48(working, w, region), nil
	}
	ha, va := nativeScaleAlignment()
	if format == "jpeg" {
		switch source := img.(type) {
		case *image.YCbCr:
			return nativeScaleJPEGWorkingFrame(source, targetW, targetH, region, ha, va)
		case *image.Gray:
			bounds := source.Bounds()
			plane := nativeScaleBytePlane{source.Pix, source.Stride, w, h, source.PixOffset(bounds.Min.X, bounds.Min.Y)}
			return nativeScaleBytePlanes([3]nativeScaleBytePlane{plane, {}, {}}, targetW, targetH, region, ha, va)
		}
	}
	rgb, err := imageRGB48(img)
	if err != nil {
		return nil, err
	}
	rgb8 := false
	if format == "png" || format == "tiff" {
		switch source := img.(type) {
		case *image.NRGBA:
			rgb8 = source.Opaque()
		case *image.RGBA:
			rgb8 = source.Opaque()
		}
	}
	return nativeScaleRGBWorkingFrame(rgb, w, h, targetW, targetH, region, rgb8, ha, va)
}

type nativeScaleBytePlane struct {
	samples                     []byte
	stride, width, height, base int
}

func nativeScaleJPEGWorkingFrame(img *image.YCbCr, targetW, targetH int, region image.Rectangle, alignH, alignV int) ([]byte, error) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	dx, dy := 1, 1
	switch img.SubsampleRatio {
	case image.YCbCrSubsampleRatio444:
	case image.YCbCrSubsampleRatio422:
		dx = 2
	case image.YCbCrSubsampleRatio420:
		dx, dy = 2, 2
	case image.YCbCrSubsampleRatio440:
		dy = 2
	case image.YCbCrSubsampleRatio411:
		dx = 4
	case image.YCbCrSubsampleRatio410:
		dx, dy = 4, 2
	default:
		return nil, fmt.Errorf("unsupported JPEG subsampling")
	}
	cw, ch := (w+dx-1)/dx, (h+dy-1)/dy
	planes := [3]nativeScaleBytePlane{{img.Y, img.YStride, w, h, img.YOffset(b.Min.X, b.Min.Y)}, {img.Cb, img.CStride, cw, ch, img.COffset(b.Min.X, b.Min.Y)}, {img.Cr, img.CStride, cw, ch, img.COffset(b.Min.X, b.Min.Y)}}
	return nativeScaleBytePlanes(planes, targetW, targetH, region, alignH, alignV)
}

func nativeScaleBytePlanes(planes [3]nativeScaleBytePlane, targetW, targetH int, region image.Rectangle, alignH, alignV int) ([]byte, error) {
	out := make([]byte, region.Dx()*region.Dy()*6)
	for p, plane := range planes {
		if plane.samples == nil {
			for i := 0; i < len(out)/6; i++ {
				binary.LittleEndian.PutUint16(out[i*6+p*2:], 512)
			}
			continue
		}
		hx, vy := nativeScaleWeights(plane.width, targetW, 16384, alignH), nativeScaleWeights(plane.height, targetH, 4096, alignV)
		rows := map[int][]int64{}
		var reusable [][]int64
		for y := region.Min.Y; y < region.Max.Y; y++ {
			tap := vy[y]
			for key, row := range rows {
				if key < tap.position || key >= tap.position+len(tap.weights) {
					reusable = append(reusable, row)
					delete(rows, key)
				}
			}
			for j := range tap.weights {
				index := tap.position + j
				if _, ok := rows[index]; ok {
					continue
				}
				var row []int64
				if n := len(reusable); n > 0 {
					row = reusable[n-1]
					reusable = reusable[:n-1]
				} else {
					row = make([]int64, region.Dx())
				}
				for x := region.Min.X; x < region.Max.X; x++ {
					sum := int64(0)
					for k, weight := range hx[x].weights {
						sum += int64(plane.samples[plane.base+index*plane.stride+hx[x].position+k]) * weight
					}
					row[x-region.Min.X] = min(32767, sum>>7)
				}
				rows[index] = row
			}
			for x := 0; x < region.Dx(); x++ {
				sum := int64(1 << 16)
				for j, weight := range tap.weights {
					sum += rows[tap.position+j][x] * weight
				}
				binary.LittleEndian.PutUint16(out[((y-region.Min.Y)*region.Dx()+x)*6+p*2:], uint16(max(0, min(1023, sum>>17))))
			}
		}
	}
	return out, nil
}

// Convert to limited-range integer samples before either scaling pass, expand
// the horizontal intermediate to full range, and round only after vertical
// filtering. A crop selects final output coordinates without changing phase.
func nativeScaleRGBWorkingFrame(rgb []byte, sourceW, sourceH, targetW, targetH int, region image.Rectangle, rgb8 bool, alignH, alignV int) ([]byte, error) {
	if sourceW <= 0 || sourceH <= 0 || targetW <= 0 || targetH <= 0 || len(rgb)%6 != 0 || len(rgb)/6/sourceW != sourceH || len(rgb)/6%sourceW != 0 || region.Empty() || !region.In(image.Rect(0, 0, targetW, targetH)) {
		return nil, fmt.Errorf("invalid native RGB scaling dimensions")
	}
	hx, vy := nativeScaleWeights(sourceW, targetW, 16384, alignH), nativeScaleWeights(sourceH, targetH, 4096, alignV)
	out := make([]byte, region.Dx()*region.Dy()*6)
	matrix := [3][3]int64{{8414, 16519, 3208}, {-4865, -9528, 14392}, {14392, -12061, -2332}}
	for p := 0; p < 3; p++ {
		half := p > 0 && targetW <= sourceW/2
		planeWidth := sourceW
		planeHX := hx
		if half {
			planeWidth = (sourceW + 1) / 2
			planeHX = nativeScaleWeights(planeWidth, targetW, 16384, alignH)
		}
		upper, bias := int64(940), int64(0x2001)<<14
		if p > 0 {
			upper = 960
			bias = int64(0x10001) << 14
		}
		if rgb8 {
			bias = int64(32<<14) + 256
			if p > 0 {
				bias = int64(256<<14) + 256
			}
			if half {
				bias *= 2
			}
		}
		coefficient := (1023*16384 + upper - 65) / (upper - 64)
		offset := (int64(1023) << 19) - (upper<<5)*coefficient + 8192
		shift := uint(15)
		if rgb8 {
			shift = 13
		}
		rows := map[int][]int64{}
		var reusable [][]int64
		sourceRow := make([]int64, planeWidth)
		for y := region.Min.Y; y < region.Max.Y; y++ {
			tap := vy[y]
			for key, row := range rows {
				if key < tap.position || key >= tap.position+len(tap.weights) {
					reusable = append(reusable, row)
					delete(rows, key)
				}
			}
			for j := range tap.weights {
				index := tap.position + j
				if _, ok := rows[index]; ok {
					continue
				}
				for x := range sourceRow {
					v := bias
					for c, weight := range matrix[p] {
						sourceX := x
						if half {
							sourceX *= 2
						}
						sample := int64(binary.LittleEndian.Uint16(rgb[(index*sourceW+sourceX)*6+c*2:]))
						if rgb8 {
							sample >>= 8
						}
						if half {
							other := int64(binary.LittleEndian.Uint16(rgb[(index*sourceW+min(sourceW-1, sourceX+1))*6+c*2:]))
							if rgb8 {
								sample += other >> 8
							} else {
								sample = (sample + other + 1) >> 1
							}
						}
						v += weight * sample
					}
					if rgb8 {
						if half {
							v >>= 10
						} else {
							v >>= 9
						}
					} else {
						v >>= 15
					}
					sourceRow[x] = v
				}
				var row []int64
				if n := len(reusable); n > 0 {
					row = reusable[n-1]
					reusable = reusable[:n-1]
				} else {
					row = make([]int64, region.Dx())
				}
				for x := region.Min.X; x < region.Max.X; x++ {
					sum := int64(0)
					for k, weight := range planeHX[x].weights {
						sum += sourceRow[planeHX[x].position+k] * weight
					}
					limited := min(32767, sum>>shift)
					row[x-region.Min.X] = min(32767, (limited*coefficient+offset)>>14)
				}
				rows[index] = row
			}
			for x := 0; x < region.Dx(); x++ {
				sum := int64(1 << 16)
				for j, weight := range tap.weights {
					sum += rows[tap.position+j][x] * weight
				}
				binary.LittleEndian.PutUint16(out[((y-region.Min.Y)*region.Dx()+x)*6+p*2:], uint16(max(0, min(1023, sum>>17))))
			}
		}
	}
	return out, nil
}

func nativeScaleWeights(source, target, one, align int) []nativeChromaTap {
	result := make([]nativeChromaTap, target)
	if source == target {
		for i := range result {
			result[i] = nativeChromaTap{i, []int64{int64(one)}}
		}
		return result
	}
	count := 7
	if source > target {
		count = 1 + (6*source+target-1)/target
	}
	count = min(count, max(1, source-2))
	precision := int64(1) << uint(54-min(bits.Len(uint(source/target))-1, 8))
	if source < target {
		precision = int64(1) << 54
	}
	step := (int64(source)*65536 + int64(target/2)) / int64(target)
	kernels := make([]nativeChromaTap, target)
	for x := range kernels {
		center := step - 65536 + int64(x)*2*step
		start := int((center - int64(count-2)*65536) / 131072)
		raw := make([]int64, count)
		for j := range raw {
			distanceFixed := absNativeChroma(int64(start+j)*131072-center) << 13
			if source > target {
				distanceFixed = distanceFixed * int64(target) / int64(source)
			}
			distance := float64(distanceFixed) / (1 << 30)
			weight := 0.0
			if distance == 0 {
				weight = 1
			} else if distance <= 3 {
				weight = math.Sin(distance*math.Pi) * math.Sin(distance*math.Pi/3) / (distance * distance * math.Pi * math.Pi / 3)
			}
			raw[j] = int64(weight * float64(precision))
		}
		kernels[x] = nativeChromaTap{start, raw}
	}
	// Remove tiny leading taps while keeping source positions monotonic.
	minimum := 0
	cutoff := precision / 500
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
	minimum = (minimum + align - 1) / align * align
	minimum = min(minimum, count)
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
