package lrtl

import (
	"fmt"
	"image"
	"image/color"
	"runtime"
	"strings"

	"github.com/mandykoh/prism/adobergb"
	"github.com/mandykoh/prism/ciexyz"
	"github.com/mandykoh/prism/displayp3"
	"github.com/mandykoh/prism/linear"
	"github.com/mandykoh/prism/prophotorgb"
	"github.com/mandykoh/prism/srgb"
)

var proPhotoToSRGB = ciexyz.AdaptBetweenXYYWhitePoints(
	prophotorgb.StandardWhitePoint,
	srgb.StandardWhitePoint,
)

func namedRenderInputProfile(input string) string {
	switch strings.ToLower(strings.TrimSpace(input)) {
	case "adobe-rgb", "display-p3", "prophoto-rgb":
		return strings.ToLower(strings.TrimSpace(input))
	default:
		return ""
	}
}

// convertRenderInputProfile converts a common encoded RGB space to sRGB while
// retaining the source bounds, alpha, and 16-bit channel precision.
func convertRenderInputProfile(img image.Image, profile string) (image.Image, error) {
	if img == nil {
		return nil, fmt.Errorf("input image is nil")
	}
	profile = namedRenderInputProfile(profile)
	if profile == "" {
		return nil, fmt.Errorf("input-color profile must be adobe-rgb, display-p3, or prophoto-rgb")
	}
	transform := func(pixel color.Color) color.RGBA64 {
		var xyz ciexyz.Color
		var alpha float32
		switch profile {
		case "adobe-rgb":
			converted, a := adobergb.ColorFromEncodedColor(pixel)
			xyz, alpha = converted.ToXYZ(), a
		case "display-p3":
			converted, a := displayp3.ColorFromEncodedColor(pixel)
			xyz, alpha = converted.ToXYZ(), a
		case "prophoto-rgb":
			converted, a := prophotorgb.ColorFromEncodedColor(pixel)
			xyz, alpha = proPhotoToSRGB.Apply(converted.ToXYZ()), a
		}
		return srgb.ColorFromXYZ(xyz).ToRGBA64(alpha)
	}
	bounds := img.Bounds()
	if bounds.Dx() <= 0 || bounds.Dy() <= 0 {
		return nil, fmt.Errorf("input image has empty bounds: %v", bounds)
	}
	maxInt := uint64(^uint(0) >> 1)
	if uint64(bounds.Dx()) > maxInt/8/uint64(bounds.Dy()) {
		return nil, fmt.Errorf("input image is too large to color-convert: %v", bounds)
	}
	dst := image.NewRGBA64(bounds)
	linear.TransformImageColor(dst, img, max(1, runtime.GOMAXPROCS(0)), transform)
	return dst, nil
}
