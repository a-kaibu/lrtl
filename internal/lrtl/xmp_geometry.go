package lrtl

import (
	"image"
	"image/color"
	"image/draw"
	"math"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/math/f64"
)

// Manual transform sliders on the oriented image canvas. The projective
// camera model is an independent approximation of Adobe's manual perspective
// controls; Upright analysis and Adobe's stored transform are not reproduced.
type xmpGeometry struct {
	rotate     float64
	scale      float64
	vertical   float64
	horizontal float64
	aspect     float64
}

func (g xmpGeometry) active() bool {
	return g.rotate != 0 || (g.scale != 0 && g.scale != 100) || g.vertical != 0 || g.horizontal != 0 || g.aspect != 0
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
	aspectScale := math.Exp2(g.aspect / 100)
	scaleX, scaleY := scale*aspectScale, scale
	a, b := scaleX*cosine, -scaleY*sine
	c, d := scaleX*sine, scaleY*cosine
	bounds := source.Bounds()
	cx, cy := float64(bounds.Min.X)+float64(bounds.Dx())/2, float64(bounds.Min.Y)+float64(bounds.Dy())/2
	transform := f64.Aff3{a, b, cx - a*cx - b*cy, c, d, cy - c*cx - d*cy}
	destination := newFrameImage(bounds, source)
	if g.vertical != 0 || g.horizontal != 0 {
		return applyXMPProjectiveGeometry(destination, source, bounds, g, scale, aspectScale, sine, cosine, cx, cy)
	}
	xdraw.ApproxBiLinear.Transform(destination, transform, source, bounds, xdraw.Src, nil)
	return destination
}

// Adobe does not publish the Camera Raw slider-to-matrix calibration. Map the
// manual sliders to a bounded camera tilt, then invert that camera transform
// per destination pixel. A 60-degree field of view keeps the neutral framing
// stable; +/-100 corresponds to +/-20 degrees of pitch or yaw.
func applyXMPProjectiveGeometry(destination draw.Image, source image.Image, bounds image.Rectangle, g xmpGeometry, scale, aspectScale, sine, cosine, cx, cy float64) image.Image {
	if bounds.Empty() {
		return source
	}
	focal := float64(max(bounds.Dx(), bounds.Dy())) / (2 * math.Tan(math.Pi/6))
	vertical := g.vertical * (math.Pi / 9) / 100
	horizontal := g.horizontal * (math.Pi / 9) / 100
	sinV, cosV := math.Sincos(vertical)
	sinH, cosH := math.Sincos(horizontal)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			// Undo uniform scale, aspect and roll before mapping through the
			// inverse camera rotation. Coordinates use pixel edges, matching the
			// affine path's pixel-center convention.
			dx, dy := float64(x)+.5-cx, float64(y)+.5-cy
			u := (cosine*dx + sine*dy) / (scale * aspectScale * focal)
			v := (-sine*dx + cosine*dy) / (scale * focal)
			// R = Ry(horizontal) * Rx(vertical), so the inverse ray is R^T p.
			sx := cosH*u - sinH
			sy := sinH*sinV*u + cosV*v + cosH*sinV
			depth := sinH*cosV*u - sinV*v + cosH*cosV
			if depth <= 1e-12 || math.IsNaN(depth) || math.IsInf(depth, 0) {
				continue
			}
			sourceX := cx + focal*sx/depth
			sourceY := cy + focal*sy/depth
			if math.IsNaN(sourceX) || math.IsInf(sourceX, 0) || math.IsNaN(sourceY) || math.IsInf(sourceY, 0) {
				continue
			}
			if sourceX < float64(bounds.Min.X)-2 || sourceX > float64(bounds.Max.X)+2 || sourceY < float64(bounds.Min.Y)-2 || sourceY > float64(bounds.Max.Y)+2 {
				continue
			}
			destination.Set(x, y, sampleXMPGeometryBilinear(source, bounds, sourceX-.5, sourceY-.5))
		}
	}
	return destination
}

// Interpolate premultiplied samples so transparent borders do not introduce
// colored fringes, then store unassociated color in the NRGBA64 destination.
func sampleXMPGeometryBilinear(source image.Image, bounds image.Rectangle, x, y float64) color.NRGBA64 {
	x0, y0 := int(math.Floor(x)), int(math.Floor(y))
	fx, fy := x-float64(x0), y-float64(y0)
	var red, green, blue, alpha float64
	for row := 0; row < 2; row++ {
		wy := 1 - fy
		if row == 1 {
			wy = fy
		}
		for column := 0; column < 2; column++ {
			wx := 1 - fx
			if column == 1 {
				wx = fx
			}
			weight := wx * wy
			px, py := x0+column, y0+row
			if !image.Pt(px, py).In(bounds) {
				continue
			}
			r, g, b, a := source.At(px, py).RGBA()
			red += float64(r) * weight
			green += float64(g) * weight
			blue += float64(b) * weight
			alpha += float64(a) * weight
		}
	}
	if alpha <= 0 {
		return color.NRGBA64{}
	}
	unassociate := func(value float64) uint16 {
		return uint16(math.Max(0, math.Min(65535, math.Round(value*65535/alpha))))
	}
	return color.NRGBA64{
		R: unassociate(red), G: unassociate(green), B: unassociate(blue),
		A: uint16(math.Max(0, math.Min(65535, math.Round(alpha)))),
	}
}

// Masks are evaluated before geometry, so their corrections move with the
// image. Crop and global post-crop tone processing follow in the caller.
func applyXMPSpatialCorrections(source image.Image, basic xmpBasicAdjustments) image.Image {
	return applyXMPGeometry(applyXMPLocalCorrections(source, basic.local), basic.geometry)
}
