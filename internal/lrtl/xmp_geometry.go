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
// controls. When XMP contains Adobe's saved Upright homography, it is replayed
// before the manual sliders; generating a new Upright solution is unsupported.
type xmpGeometry struct {
	rotate     float64
	scale      float64
	vertical   float64
	horizontal float64
	aspect     float64
	xOffset    float64
	yOffset    float64
	upright    *xmpHomography
}

func (g xmpGeometry) active() bool {
	return g.rotate != 0 || (g.scale != 0 && g.scale != 100) || g.vertical != 0 || g.horizontal != 0 || g.aspect != 0 ||
		g.xOffset != 0 || g.yOffset != 0 || (g.upright != nil && !g.upright.isIdentity())
}

// Independent Lightroom export measurements reported X+20 = +0.1621 frame
// widths and Y+20 = -0.1625 frame heights. Keep the midpoint of those measured
// coefficients and preserve the observed, opposite Y-axis sign.
const xmpGeometryOffsetPerSlider = 0.8121

func applyXMPGeometry(source image.Image, g xmpGeometry) image.Image {
	if g.upright != nil && !g.upright.isIdentity() {
		source = applyXMPStoredUpright(source, *g.upright)
		g.upright = nil
	}
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
	offsetX := g.xOffset / 100 * xmpGeometryOffsetPerSlider * float64(bounds.Dx())
	offsetY := -g.yOffset / 100 * xmpGeometryOffsetPerSlider * float64(bounds.Dy())
	transform := f64.Aff3{a, b, cx - a*cx - b*cy + offsetX, c, d, cy - c*cx - d*cy + offsetY}
	destination := newFrameImage(bounds, source)
	if g.vertical != 0 || g.horizontal != 0 {
		return applyXMPProjectiveGeometry(destination, source, bounds, g, scale, aspectScale, sine, cosine, cx, cy)
	}
	xdraw.ApproxBiLinear.Transform(destination, transform, source, bounds, xdraw.Src, nil)
	return destination
}

// XMP stores Adobe's Upright result as a row-major homography in per-axis
// normalized frame coordinates. It maps source points to destination points;
// rendering therefore inverse-maps each destination sample. Pixel coordinates
// are normalized over [0,width-1] and [0,height-1], retaining non-zero image
// origins while making the frame corners correspond to 0 and 1.
type xmpHomography [9]float64

func (h xmpHomography) isIdentity() bool {
	return h[0] == h[4] && h[4] == h[8] && h[0] != 0 &&
		h[1] == 0 && h[2] == 0 && h[3] == 0 && h[5] == 0 && h[6] == 0 && h[7] == 0
}

func (h xmpHomography) inverse() (xmpHomography, bool) {
	a, b, c, d, e, f, g, i, j := h[0], h[1], h[2], h[3], h[4], h[5], h[6], h[7], h[8]
	cofactors := [9]float64{
		e*j - f*i, c*i - b*j, b*f - c*e,
		f*g - d*j, a*j - c*g, c*d - a*f,
		d*i - e*g, b*g - a*i, a*e - b*d,
	}
	determinant := a*cofactors[0] + b*cofactors[3] + c*cofactors[6]
	if math.IsNaN(determinant) || math.IsInf(determinant, 0) || math.Abs(determinant) < 1e-12 {
		return xmpHomography{}, false
	}
	var inverse xmpHomography
	maxAbs := 0.0
	for index, value := range cofactors {
		inverse[index] = value / determinant
		if math.IsNaN(inverse[index]) || math.IsInf(inverse[index], 0) {
			return xmpHomography{}, false
		}
		maxAbs = math.Max(maxAbs, math.Abs(inverse[index]))
	}
	if maxAbs == 0 || math.IsInf(maxAbs, 0) {
		return xmpHomography{}, false
	}
	// Homographies are unchanged by a common scale factor. Normalize the
	// inverse so mapping cannot overflow for otherwise equivalent matrices.
	for index := range inverse {
		inverse[index] /= maxAbs
	}
	return inverse, true
}

func (h xmpHomography) mapPoint(x, y float64) (float64, float64, bool) {
	denominator := h[6]*x + h[7]*y + h[8]
	if math.IsNaN(denominator) || math.IsInf(denominator, 0) || math.Abs(denominator) < 1e-12 {
		return 0, 0, false
	}
	u := (h[0]*x + h[1]*y + h[2]) / denominator
	v := (h[3]*x + h[4]*y + h[5]) / denominator
	if math.IsNaN(u) || math.IsInf(u, 0) || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, 0, false
	}
	return u, v, true
}

func applyXMPStoredUpright(source image.Image, transform xmpHomography) image.Image {
	if transform.isIdentity() {
		return source
	}
	bounds := source.Bounds()
	if bounds.Empty() {
		return source
	}
	inverse, ok := transform.inverse()
	if !ok {
		// XMP parsing rejects singular matrices. Keep this renderer defensive for
		// callers that construct xmpGeometry directly in package-level tests.
		return source
	}
	destination := newFrameImage(bounds, source)
	width, height := float64(max(bounds.Dx()-1, 1)), float64(max(bounds.Dy()-1, 1))
	sourceWidth, sourceHeight := float64(max(bounds.Dx()-1, 0)), float64(max(bounds.Dy()-1, 0))
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		v := float64(y-bounds.Min.Y) / height
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			u := float64(x-bounds.Min.X) / width
			sourceU, sourceV, valid := inverse.mapPoint(u, v)
			if !valid || sourceU < 0 || sourceU > 1 || sourceV < 0 || sourceV > 1 {
				continue
			}
			sourceX := float64(bounds.Min.X) + sourceU*sourceWidth
			sourceY := float64(bounds.Min.Y) + sourceV*sourceHeight
			destination.Set(x, y, sampleXMPGeometryBilinear(source, bounds, sourceX, sourceY))
		}
	}
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
	offsetX := g.xOffset / 100 * xmpGeometryOffsetPerSlider * float64(bounds.Dx())
	offsetY := -g.yOffset / 100 * xmpGeometryOffsetPerSlider * float64(bounds.Dy())
	sinV, cosV := math.Sincos(vertical)
	sinH, cosH := math.Sincos(horizontal)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			// Undo uniform scale, aspect and roll before mapping through the
			// inverse camera rotation. Coordinates use pixel edges, matching the
			// affine path's pixel-center convention.
			// X/Y offsets are the final forward-map step, so undo them before
			// reversing scale, aspect, roll and the projective camera transform.
			dx, dy := float64(x)+.5-cx-offsetX, float64(y)+.5-cy-offsetY
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
