package lrtl

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"path/filepath"
	"strings"
)

const lrtDefaultCameraPreviewWidth = 1024
const lrtPreviewFixedOne = int64(1) << 32

// resizeLRTCameraPreview follows the final resize in LRT 7.5.5's camera
// preview loader: only images wider than previewWidthCamera are resized, to
// width x int(width/1.5), with one bilinear pass.
func resizeLRTCameraPreview(img image.Image) (image.Image, error) {
	if img == nil || img.Bounds().Empty() {
		return nil, fmt.Errorf("cannot resize an empty LRT preview")
	}
	bounds := img.Bounds()
	if bounds.Dx() <= lrtDefaultCameraPreviewWidth {
		return img, nil
	}
	width := lrtDefaultCameraPreviewWidth
	height := int(float32(width) / 1.5)
	if height <= 0 {
		return nil, fmt.Errorf("invalid LRT preview dimensions %dx%d", width, height)
	}
	return resizeLRTPreviewTo(img, width, height)
}

func resizeLRTPreviewTo(img image.Image, width, height int) (image.Image, error) {
	if img == nil || img.Bounds().Empty() || width <= 0 || height <= 0 {
		return nil, fmt.Errorf("invalid image resize dimensions")
	}
	if uint64(width)*uint64(height) > 64*1024*1024 {
		return nil, fmt.Errorf("image resize exceeds 64 megapixels")
	}
	bounds := img.Bounds()
	preview := image.NewNRGBA(image.Rect(0, 0, width, height))
	sourceWidth, sourceHeight := bounds.Dx(), bounds.Dy()
	// Java2D's inverse transform advances in signed 32.32 fixed point. Keep
	// the start coordinate and per-pixel step separate: accumulating the
	// truncated step is observable at exact integer boundaries (for example,
	// the second sample of a 5-to-3 resize lands just below source pixel 2).
	xStart := lrtPreviewFixedCoordinate(float64(sourceWidth) / (2 * float64(width)))
	yStart := lrtPreviewFixedCoordinate(float64(sourceHeight) / (2 * float64(height)))
	xStep := lrtPreviewFixedCoordinate(float64(sourceWidth) / float64(width))
	yStep := lrtPreviewFixedCoordinate(float64(sourceHeight) / float64(height))
	for y := 0; y < height; y++ {
		y0, y1, fy, dy := lrtPreviewSample(yStart+int64(y)*yStep, sourceHeight)
		for x := 0; x < width; x++ {
			x0, x1, fx, dx := lrtPreviewSample(xStart+int64(x)*xStep, sourceWidth)
			p00 := color.NRGBAModel.Convert(img.At(bounds.Min.X+x0, bounds.Min.Y+y0)).(color.NRGBA)
			p10 := color.NRGBAModel.Convert(img.At(bounds.Min.X+x1, bounds.Min.Y+y0)).(color.NRGBA)
			p01 := color.NRGBAModel.Convert(img.At(bounds.Min.X+x0, bounds.Min.Y+y1)).(color.NRGBA)
			p11 := color.NRGBAModel.Convert(img.At(bounds.Min.X+x1, bounds.Min.Y+y1)).(color.NRGBA)
			if p00.A == 255 && p10.A == 255 && p01.A == 255 && p11.A == 255 {
				preview.SetNRGBA(x, y, color.NRGBA{
					R: lrtJavaBilinearOpaque(p00.R, p10.R, p01.R, p11.R, fx, fy),
					G: lrtJavaBilinearOpaque(p00.G, p10.G, p01.G, p11.G, fx, fy),
					B: lrtJavaBilinearOpaque(p00.B, p10.B, p01.B, p11.B, fx, fy),
					A: 255,
				})
				continue
			}
			channels := [4]float64{}
			c00 := rgba16(img.At(bounds.Min.X+x0, bounds.Min.Y+y0))
			c10 := rgba16(img.At(bounds.Min.X+x1, bounds.Min.Y+y0))
			c01 := rgba16(img.At(bounds.Min.X+x0, bounds.Min.Y+y1))
			c11 := rgba16(img.At(bounds.Min.X+x1, bounds.Min.Y+y1))
			for channel := range channels {
				top := float64(c00[channel])*(1-dx) + float64(c10[channel])*dx
				bottom := float64(c01[channel])*(1-dx) + float64(c11[channel])*dx
				channels[channel] = top*(1-dy) + bottom*dy
			}
			alpha := clampByte(math.Round(channels[3] / 257))
			pixel := color.NRGBA{A: alpha}
			if channels[3] > 0 {
				toUnassociatedByte := func(channel int) uint8 {
					return clampByte(math.Round(channels[channel] * 255 / channels[3]))
				}
				pixel.R = toUnassociatedByte(0)
				pixel.G = toUnassociatedByte(1)
				pixel.B = toUnassociatedByte(2)
			}
			preview.SetNRGBA(x, y, pixel)
		}
	}
	return preview, nil
}

func lrtPreviewFixedCoordinate(value float64) int64 {
	return int64(math.Trunc(math.Ldexp(value, 32)))
}

// lrtPreviewSample converts Java2D's source-edge coordinate into the pixel
// centered sample coordinate used by bilinear interpolation. Java2D keeps
// eight fractional bits for the interpolation weights.
func lrtPreviewSample(edgeCoordinate int64, limit int) (first, second int, fraction uint8, preciseFraction float64) {
	sample := edgeCoordinate - lrtPreviewFixedOne/2
	first = int(sample >> 32)
	preciseFraction = float64(uint64(sample)&uint64(lrtPreviewFixedOne-1)) / float64(lrtPreviewFixedOne)
	fraction = uint8(uint64(sample) >> 24)
	second = first + 1
	first = max(0, min(limit-1, first))
	second = max(0, min(limit-1, second))
	return first, second, fraction, preciseFraction
}

// lrtJavaBilinearOpaque follows the 8-bit fractional, 16-bit accumulator
// used by Java2D's integer bilinear raster path for opaque RGB images.
func lrtJavaBilinearOpaque(p00, p10, p01, p11, fx, fy uint8) uint8 {
	top := (int(p00) << 8) + (int(p10)-int(p00))*int(fx)
	bottom := (int(p01) << 8) + (int(p11)-int(p01))*int(fx)
	value := (top << 8) + (bottom-top)*int(fy)
	return uint8((value + (1 << 15)) >> 16)
}

func rgba16(pixel color.Color) [4]uint32 {
	r, g, b, a := pixel.RGBA()
	return [4]uint32{r, g, b, a}
}

func clampByte(value float64) uint8 {
	return uint8(max(0, min(255, int(value))))
}

// LRT 7.5.5 stores ordinary JPEG data in both .lrtpreview cache locations.
// Select the cache explicitly: neither timestamps nor XMP edits prove freshness.
func lrtPreviewPath(source, kind string) string {
	folder := "previews"
	if kind == "visual" {
		folder = "visual"
	}
	stem := strings.TrimSuffix(filepath.Base(source), filepath.Ext(source))
	return filepath.Join(filepath.Dir(source), ".lrt", folder, stem+".lrtpreview")
}
