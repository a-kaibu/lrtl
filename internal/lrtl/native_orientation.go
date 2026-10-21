package lrtl

import (
	"fmt"
	"image"
)

func nativeOrientationPoint(x, y, width, height int, orientation uint16) (int, int) {
	switch orientation {
	case 2:
		return width - 1 - x, y
	case 3:
		return width - 1 - x, height - 1 - y
	case 4:
		return x, height - 1 - y
	case 5:
		return y, x
	case 6:
		return height - 1 - y, x
	case 7:
		return height - 1 - y, width - 1 - x
	case 8:
		return y, width - 1 - x
	}
	return x, y
}

// Native geometry scales the cropped source before transposing YUV444. Map
// the output region back first, retaining scale phase without allocating the
// full canvas when only a small crop is needed.
func nativeScaleOrientedWorkingFrame(img image.Image, format string, orientation uint16, targetW, targetH int, region image.Rectangle) ([]byte, error) {
	if orientation < 1 || orientation > 8 || targetW <= 0 || targetH <= 0 || region.Empty() || !region.In(image.Rect(0, 0, targetW, targetH)) {
		return nil, fmt.Errorf("invalid native oriented scaling dimensions or orientation")
	}
	w, h := targetW, targetH
	if orientation >= 5 {
		w, h = h, w
	}
	inverse := orientation
	if orientation == 6 {
		inverse = 8
	} else if orientation == 8 {
		inverse = 6
	}
	sourceRegion := image.Rectangle{}
	for _, corner := range []image.Point{region.Min, {region.Max.X - 1, region.Min.Y}, {region.Min.X, region.Max.Y - 1}, {region.Max.X - 1, region.Max.Y - 1}} {
		x, y := nativeOrientationPoint(corner.X, corner.Y, targetW, targetH, inverse)
		r := image.Rect(x, y, x+1, y+1)
		if sourceRegion.Empty() {
			sourceRegion = r
		} else {
			sourceRegion = sourceRegion.Union(r)
		}
	}
	working, err := nativeScaleImageWorkingFrame(cropNativeEvenFrame(img, 1), format, w, h, sourceRegion)
	if err != nil || orientation == 1 {
		return working, err
	}
	out := make([]byte, len(working))
	for y := 0; y < sourceRegion.Dy(); y++ {
		for x := 0; x < sourceRegion.Dx(); x++ {
			dx, dy := nativeOrientationPoint(x, y, sourceRegion.Dx(), sourceRegion.Dy(), orientation)
			copy(out[(dy*region.Dx()+dx)*6:], working[(y*sourceRegion.Dx()+x)*6:][:6])
		}
	}
	return out, nil
}
