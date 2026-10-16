package lrtl

import (
	"image"
	"math"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/math/f64"
)

// Manual rotation and uniform scale on the oriented image canvas. This does
// not implement Adobe's Upright/projective transform or automatic crop fitting.
type xmpGeometry struct {
	rotate float64
	scale  float64
}

func (g xmpGeometry) active() bool {
	return g.rotate != 0 || (g.scale != 0 && g.scale != 100)
}

func applyXMPGeometry(source image.Image, g xmpGeometry) image.Image {
	if !g.active() {
		return source
	}
	scale := g.scale / 100
	if g.scale == 0 { // Zero-value adjustments mean no requested scale change.
		scale = 1
	}
	sine, cosine := math.Sincos(g.rotate * math.Pi / 180)
	a, b := scale*cosine, -scale*sine
	c, d := scale*sine, scale*cosine
	bounds := source.Bounds()
	cx, cy := float64(bounds.Min.X)+float64(bounds.Dx())/2, float64(bounds.Min.Y)+float64(bounds.Dy())/2
	transform := f64.Aff3{a, b, cx - a*cx - b*cy, c, d, cy - c*cx - d*cy}
	destination := newFrameImage(bounds, source)
	xdraw.ApproxBiLinear.Transform(destination, transform, source, bounds, xdraw.Src, nil)
	return destination
}

// Masks are evaluated before geometry, so their corrections move with the
// image. Crop and global post-crop tone processing follow in the caller.
func applyXMPSpatialCorrections(source image.Image, basic xmpBasicAdjustments) image.Image {
	return applyXMPGeometry(applyXMPLocalCorrections(source, basic.local), basic.geometry)
}
