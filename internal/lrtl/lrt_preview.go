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
	for y := 0; y < height; y++ {
		sourceY := (float64(y)+0.5)*float64(sourceHeight)/float64(height) - 0.5
		y0 := int(math.Floor(sourceY))
		dy := sourceY - float64(y0)
		y1 := y0 + 1
		y0, y1 = max(0, y0), min(sourceHeight-1, y1)
		for x := 0; x < width; x++ {
			sourceX := (float64(x)+0.5)*float64(sourceWidth)/float64(width) - 0.5
			x0 := int(math.Floor(sourceX))
			dx := sourceX - float64(x0)
			x1 := x0 + 1
			x0, x1 = max(0, x0), min(sourceWidth-1, x1)
			p00 := img.At(bounds.Min.X+x0, bounds.Min.Y+y0)
			p10 := img.At(bounds.Min.X+x1, bounds.Min.Y+y0)
			p01 := img.At(bounds.Min.X+x0, bounds.Min.Y+y1)
			p11 := img.At(bounds.Min.X+x1, bounds.Min.Y+y1)
			channels := [4]float64{}
			c00 := rgba16(p00)
			c10 := rgba16(p10)
			c01 := rgba16(p01)
			c11 := rgba16(p11)
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
