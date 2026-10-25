package lrtl

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"

	"github.com/a-kaibu/lrtl/internal/nativejpeg"
)

// decodeLRTMeteringImage reproduces the centered 4:2:0 chroma upsampling
// observed in LRTimelapse's AWT JPEG decoder before native Preview EV
// metering. Go's JPEG decoder exposes subsampled YCbCr and its At method
// selects the nearest chroma sample; LRT interpolates between centered
// chroma samples instead. Other sampling ratios use the same model but need
// separate native measurements.
func decodeLRTMeteringImage(data []byte) (image.Image, string, error) {
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil || format != "jpeg" {
		return img, format, err
	}
	ycbcr, ok := img.(*image.YCbCr)
	if !ok {
		return img, format, nil
	}
	img, err = upsampleLRTJPEGChroma(ycbcr)
	return img, format, err
}

func upsampleLRTJPEGChroma(src *image.YCbCr) (image.Image, error) {
	bounds := src.Bounds()
	if bounds.Empty() {
		return nil, fmt.Errorf("invalid JPEG dimensions")
	}
	horizontal, vertical := 1, 1
	switch src.SubsampleRatio {
	case image.YCbCrSubsampleRatio444:
		return src, nil
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
		return nil, fmt.Errorf("unsupported JPEG chroma subsampling %v", src.SubsampleRatio)
	}
	width, height := bounds.Dx(), bounds.Dy()
	chromaWidth := (bounds.Max.X-1)/horizontal - bounds.Min.X/horizontal + 1
	chromaHeight := (bounds.Max.Y-1)/vertical - bounds.Min.Y/vertical + 1
	base := src.COffset(bounds.Min.X, bounds.Min.Y)
	xTaps := make([]lrtChromaAxis, width)
	for x := range xTaps {
		xTaps[x] = centeredLRTChromaAxis(x, horizontal, chromaWidth)
	}
	yTaps := make([]lrtChromaAxis, height)
	for y := range yTaps {
		yTaps[y] = centeredLRTChromaAxis(y, vertical, chromaHeight)
	}
	return &lrtJPEGChromaImage{
		source: src, base: base,
		denominatorX: 2 * horizontal, denominatorY: 2 * vertical,
		xTaps: xTaps, yTaps: yTaps,
	}, nil
}

type lrtJPEGChromaImage struct {
	source                     *image.YCbCr
	base                       int
	denominatorX, denominatorY int
	xTaps, yTaps               []lrtChromaAxis
}

func (img *lrtJPEGChromaImage) ColorModel() color.Model { return color.RGBAModel }

func (img *lrtJPEGChromaImage) Bounds() image.Rectangle { return img.source.Bounds() }

func (img *lrtJPEGChromaImage) At(x, y int) color.Color {
	bounds := img.source.Bounds()
	if !image.Pt(x, y).In(bounds) {
		return color.RGBA{}
	}
	xTap := img.xTaps[x-bounds.Min.X]
	yTap := img.yTaps[y-bounds.Min.Y]
	w00 := (img.denominatorX - xTap.fraction) * (img.denominatorY - yTap.fraction)
	w10 := xTap.fraction * (img.denominatorY - yTap.fraction)
	w01 := (img.denominatorX - xTap.fraction) * yTap.fraction
	w11 := xTap.fraction * yTap.fraction
	cb := interpolateLRTChroma(img.source.Cb, img.source.CStride, img.base, xTap, yTap, w00, w10, w01, w11, img.denominatorX*img.denominatorY)
	cr := interpolateLRTChroma(img.source.Cr, img.source.CStride, img.base, xTap, yTap, w00, w10, w01, w11, img.denominatorX*img.denominatorY)
	yValue := img.source.Y[img.source.YOffset(x, y)]
	r, g, b := color.YCbCrToRGB(yValue, cb, cr)
	return color.RGBA{R: r, G: g, B: b, A: 255}
}

type lrtChromaAxis struct {
	first, second int
	fraction      int
}

func centeredLRTChromaAxis(pixel, factor, samples int) lrtChromaAxis {
	denominator := 2 * factor
	numerator := 2*pixel - (factor - 1)
	first := numerator / denominator
	if numerator < 0 && numerator%denominator != 0 {
		first--
	}
	fraction := numerator - first*denominator
	if first < 0 {
		return lrtChromaAxis{first: 0, second: 0}
	}
	if first >= samples-1 {
		last := samples - 1
		return lrtChromaAxis{first: last, second: last}
	}
	return lrtChromaAxis{first: first, second: first + 1, fraction: fraction}
}

func interpolateLRTChroma(plane []byte, stride, base int, x, y lrtChromaAxis, w00, w10, w01, w11, denominator int) byte {
	top := int(plane[base+y.first*stride+x.first])*w00 + int(plane[base+y.first*stride+x.second])*w10
	bottom := int(plane[base+y.second*stride+x.first])*w01 + int(plane[base+y.second*stride+x.second])*w11
	return byte((top + bottom + denominator/2) / denominator)
}

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
	return nativeScaleWeights(source, target, one, 1)
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
