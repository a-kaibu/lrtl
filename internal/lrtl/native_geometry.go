package lrtl

import (
	"fmt"
	"image"
	"image/draw"
	"math"
)

type nativeRenderGeometry struct {
	scaleWidth, scaleHeight int
	crop                    image.Rectangle
}

func nativeAspectRatio(ratio float32) float32 {
	for _, standard := range []float32{3, 2.5, 2, 1.7777778, 1.5, 1, .6666667, .5625, .5, .4} {
		if float64(ratio) >= float64(standard)*.99 && float64(ratio) <= float64(standard)*1.01 {
			ratio = standard
		}
	}
	return ratio
}

func planNativeRenderGeometry(width, height, resolution int, aspect float64) (nativeRenderGeometry, error) {
	if width <= 0 || height <= 0 || resolution < 0 || resolution > 32768 || (resolution > 0 && resolution < 16) || math.IsNaN(aspect) || math.IsInf(aspect, 0) || (aspect != 0 && (aspect < 1.0/16 || aspect > 16)) {
		return nativeRenderGeometry{}, fmt.Errorf("invalid native preset dimensions or aspect ratio")
	}
	sourceRatio := nativeAspectRatio(float32(width) / float32(height))
	targetRatio := sourceRatio
	if aspect > 0 {
		targetRatio = nativeAspectRatio(float32(aspect))
	}
	canvasW, canvasH := width/4*4, height/2*2
	if resolution > 0 && resolution != height && !(width <= height && resolution == width) {
		canvasW, canvasH = resolution, resolution
		if targetRatio >= 1 {
			canvasW = int(float32(resolution) * targetRatio)
		} else {
			canvasH = int(float32(resolution) / targetRatio)
		}
	}
	canvasRatio := sourceRatio
	if aspect > 0 {
		canvasRatio = nativeAspectRatio(float32(canvasW) / float32(canvasH))
	}
	// Native comparisons quantize ratios to hundredths; nearby dimensions
	// snap back to the chosen canvas before final alignment.
	near := func(value, reference int) int {
		if absInt(value-reference) <= 4 {
			return reference
		}
		return value
	}
	cropW, cropH := canvasW, canvasH
	if int(targetRatio*100) > int(canvasRatio*100) {
		cropH = int(float32(canvasW) / targetRatio)
	} else if int(targetRatio*100) < int(canvasRatio*100) {
		cropW = int(float32(canvasH) * targetRatio)
	}
	cropW, cropH = near(cropW, canvasW), near(cropH, canvasH)
	scaleW, scaleH := cropW, cropH
	if int(sourceRatio*100) > int(targetRatio*100) {
		scaleW = int(math.Min(float64(float32(cropH)*sourceRatio), float64(maxVideoDimension)))
	} else if int(sourceRatio*100) < int(targetRatio*100) {
		scaleH = int(math.Min(float64(float32(cropW)/sourceRatio), float64(maxVideoDimension)))
	}
	scaleW, scaleH = near(scaleW, cropW), near(scaleH, cropH)
	cropW, cropH, scaleW, scaleH = cropW/4*4, cropH/2*2, scaleW/4*4, scaleH/2*2
	left, top := (scaleW-cropW)/2, (scaleH-cropH)/2
	plan := nativeRenderGeometry{scaleW, scaleH, image.Rect(left, top, left+cropW, top+cropH)}
	if width < 2 || height < 2 || scaleW < 1 || scaleH < 1 || cropW < 1 || cropH < 1 || left < 0 || top < 0 {
		return plan, fmt.Errorf("native preset geometry leaves no valid output; change resolution or aspect ratio")
	}
	if scaleW > 32768 || scaleH > 32768 || uint64(scaleW)*uint64(scaleH) > 64*1024*1024 {
		return plan, fmt.Errorf("native preset scale exceeds 32768 pixels per side or 64 megapixels; reduce resolution or aspect ratio")
	}
	return plan, nil
}

func copyFrameRectangle(img image.Image, bounds image.Rectangle) image.Image {
	if bounds == img.Bounds() {
		return img
	}
	result := newFrameImage(image.Rect(0, 0, bounds.Dx(), bounds.Dy()), img)
	draw.Draw(result, result.Bounds(), img, bounds.Min, draw.Src)
	return result
}

// The native graph crops odd raw dimensions before orientation. Express that
// rectangle in already-oriented coordinates so the same source edge is lost.
func cropNativeEvenFrame(img image.Image, orientation uint16) image.Image {
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	dx, dy := w%2, h%2
	left, top := 0, 0
	switch orientation {
	case 2:
		left = dx
	case 3:
		left, top = dx, dy
	case 4:
		top = dy
	case 6:
		left = dx
	case 7:
		left, top = dx, dy
	case 8:
		top = dy
	}
	return copyFrameRectangle(img, image.Rect(bounds.Min.X+left, bounds.Min.Y+top, bounds.Min.X+left+w-dx, bounds.Min.Y+top+h-dy))
}

func applyNativeRenderGeometry(img image.Image, orientation uint16, plan nativeRenderGeometry) image.Image {
	img = cropNativeEvenFrame(img, orientation)
	img = resizeFrame(img, plan.scaleWidth, plan.scaleHeight)
	return copyFrameRectangle(img, plan.crop)
}
