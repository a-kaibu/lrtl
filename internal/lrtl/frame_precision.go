package lrtl

import (
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
)

func highPrecisionImage(img image.Image) bool {
	switch img.(type) {
	case *image.NRGBA64, *image.RGBA64, *image.Gray16, *image.Alpha16:
		return true
	}
	return false
}

// Geometric operations retain the source's precision and unassociated alpha.
func newFrameImage(bounds image.Rectangle, source image.Image) draw.Image {
	if highPrecisionImage(source) {
		return image.NewNRGBA64(bounds)
	}
	return image.NewNRGBA(bounds)
}

// FFmpeg receives full 16-bit RGB samples, little-endian, composited on black.
func imageRGB48(img image.Image) ([]byte, error) {
	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	maxInt := uint64(^uint(0) >> 1)
	if width <= 0 || height <= 0 || uint64(width) > maxInt/6/uint64(height) {
		return nil, fmt.Errorf("frame dimensions are too large for RGB48 encoding: %dx%d", width, height)
	}
	rgb := make([]byte, width*height*6)
	precise, hasRGBA64 := img.(image.RGBA64Image)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			var r, g, b uint32
			if hasRGBA64 {
				p := precise.RGBA64At(x, y)
				r, g, b = uint32(p.R), uint32(p.G), uint32(p.B)
			} else {
				r, g, b, _ = img.At(x, y).RGBA()
			}
			i := ((y-bounds.Min.Y)*width + x - bounds.Min.X) * 6
			binary.LittleEndian.PutUint16(rgb[i:], uint16(r))
			binary.LittleEndian.PutUint16(rgb[i+2:], uint16(g))
			binary.LittleEndian.PutUint16(rgb[i+4:], uint16(b))
		}
	}
	return rgb, nil
}

func imageFromRGB48(rgb []byte, width, height int) (*image.NRGBA64, error) {
	if width <= 0 || height <= 0 || uint64(width) > uint64(^uint(0)>>1)/8/uint64(height) || uint64(len(rgb)) != uint64(width)*uint64(height)*6 {
		return nil, fmt.Errorf("invalid RGB48 frame size")
	}
	img := image.NewNRGBA64(image.Rect(0, 0, width, height))
	for i, j := 0, 0; i < len(rgb); i, j = i+6, j+8 {
		img.Pix[j], img.Pix[j+1] = rgb[i+1], rgb[i]
		img.Pix[j+2], img.Pix[j+3] = rgb[i+3], rgb[i+2]
		img.Pix[j+4], img.Pix[j+5] = rgb[i+5], rgb[i+4]
		img.Pix[j+6], img.Pix[j+7] = 255, 255
	}
	return img, nil
}

func weightedRGB48Frames(frames [][]byte, weights []float32) ([]byte, error) {
	if len(frames) == 0 || len(frames) != len(weights) || len(frames[0]) == 0 || len(frames[0])%6 != 0 {
		return nil, fmt.Errorf("invalid RGB48 blend window")
	}
	length := len(frames[0])
	sumWeights := 0.0
	for i, w := range weights {
		if len(frames[i]) != length || w < 0 || math.IsNaN(float64(w)) || math.IsInf(float64(w), 0) {
			return nil, fmt.Errorf("invalid RGB48 frame or weight")
		}
		sumWeights += float64(w)
	}
	if sumWeights <= 0 {
		return nil, fmt.Errorf("non-positive RGB48 weight sum")
	}
	result := make([]byte, length)
	for i := 0; i < length; i += 2 {
		sum := 0.0
		for j, frame := range frames {
			sum += float64(binary.LittleEndian.Uint16(frame[i:])) * float64(weights[j])
		}
		value := uint16(math.Min(65535, math.Max(0, math.RoundToEven(sum/sumWeights))))
		binary.LittleEndian.PutUint16(result[i:], value)
	}
	return result, nil
}

func applyExposureGainFrame(img image.Image, gain float64) image.Image {
	if !highPrecisionImage(img) {
		return applyExposureGain(img, gain)
	}
	if gain == 1 {
		return img
	}
	result := image.NewNRGBA64(img.Bounds())
	var lookup [65536]uint16
	for i := range lookup {
		lookup[i] = encodeLinearSRGB16(srgb16ToLinear[i] * gain)
	}
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			p := color.NRGBA64Model.Convert(img.At(x, y)).(color.NRGBA64)
			result.SetNRGBA64(x, y, color.NRGBA64{lookup[p.R], lookup[p.G], lookup[p.B], p.A})
		}
	}
	return result
}

// Exposure-only edits use a LUT; other edits use the shared 16-bit tone path.
func applyXMPBasicFrame(img image.Image, adjustments xmpBasicAdjustments) image.Image {
	other := adjustments
	other.exposure = 0
	if highPrecisionImage(img) && !other.globalActive() {
		return applyExposureGainFrame(img, math.Exp2(adjustments.exposure))
	}
	return applyXMPBasic(img, adjustments)
}
