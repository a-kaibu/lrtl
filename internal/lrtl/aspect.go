package lrtl

import (
	"fmt"
	"image"
	"math"
	"strconv"
	"strings"

	xdraw "golang.org/x/image/draw"
)

func parseAspectRatio(value string) (float64, error) {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) != 2 {
		return 0, fmt.Errorf("aspect ratio must use WIDTH:HEIGHT format, for example 16:9: %q", value)
	}
	width, widthErr := strconv.Atoi(strings.TrimSpace(parts[0]))
	height, heightErr := strconv.Atoi(strings.TrimSpace(parts[1]))
	if widthErr != nil || heightErr != nil || width <= 0 || height <= 0 || width > 32768 || height > 32768 {
		return 0, fmt.Errorf("aspect ratio width and height must be integers between 1 and 32768: %q", value)
	}
	ratio := float64(width) / float64(height)
	if ratio < 1.0/16 || ratio > 16 {
		return 0, fmt.Errorf("aspect ratio must be between 1:16 and 16:1: %q", value)
	}
	return ratio, nil
}

func aspectCropBounds(bounds image.Rectangle, aspectRatio float64) image.Rectangle {
	if aspectRatio <= 0 || bounds.Empty() {
		return bounds
	}
	width, height := bounds.Dx(), bounds.Dy()
	if float64(width)/float64(height) > aspectRatio {
		cropWidth := max(1, min(width, int(math.Round(float64(height)*aspectRatio))))
		left := bounds.Min.X + (width-cropWidth)/2
		return image.Rect(left, bounds.Min.Y, left+cropWidth, bounds.Max.Y)
	}
	cropHeight := max(1, min(height, int(math.Round(float64(width)/aspectRatio))))
	top := bounds.Min.Y + (height-cropHeight)/2
	return image.Rect(bounds.Min.X, top, bounds.Max.X, top+cropHeight)
}

func cropImageToAspect(img image.Image, aspectRatio float64) image.Image {
	bounds := img.Bounds()
	cropBounds := aspectCropBounds(bounds, aspectRatio)
	if cropBounds == bounds {
		return img
	}
	cropped := newFrameImage(image.Rect(0, 0, cropBounds.Dx(), cropBounds.Dy()), img)
	xdraw.Draw(cropped, cropped.Bounds(), img, cropBounds.Min, xdraw.Src)
	return cropped
}
