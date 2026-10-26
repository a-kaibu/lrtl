package lrtl

import (
	"image"
	"image/color"
	"math"
)

// xmpApplyGrain adds coordinate-stable luminance grain and, at size 25 or
// above, a smaller independent blue component.
// Keeping the pattern tied to image coordinates avoids frame-to-frame random
// changes when a timelapse uses the same framing. Adobe's exact grain kernel
// and GrainFrequency transfer curve are not public, so this is not pixel
// equivalent to Camera Raw.
func xmpApplyGrain(img *image.NRGBA64, amount, size, frequency float64) *image.NRGBA64 {
	if img == nil || amount <= 0 || math.IsNaN(amount) {
		return img
	}
	amount = math.Min(100, amount)

	for y := img.Rect.Min.Y; y < img.Rect.Max.Y; y++ {
		for x := img.Rect.Min.X; x < img.Rect.Max.X; x++ {
			pixel := img.NRGBA64At(x, y)
			img.SetNRGBA64(x, y, xmpApplyGrainPixel(pixel, x, y, amount, size, frequency))
		}
	}
	return img
}

func xmpApplyGrainPixel(pixel color.NRGBA64, x, y int, amount, size, frequency float64) color.NRGBA64 {
	if pixel.A == 0 || amount == 0 || math.IsNaN(amount) {
		return pixel
	}
	amount = math.Max(-100, math.Min(100, amount)) / 100
	size = math.Max(0, math.Min(100, size))
	frequency = math.Max(0, math.Min(100, frequency))
	coarseScale := 0.7 + size/100*1.8
	fineScale := math.Max(0.4, coarseScale*0.38)
	roughness := frequency / 100
	coarse := xmpGrainValueNoise(x, y, coarseScale, 0x9e3779b97f4a7c15)
	fine := xmpGrainValueNoise(x, y, fineScale, 0xd1b54a32d192ed03)
	delta := (coarse*(1-roughness) + fine*roughness) * 0.045 * amount
	blueDelta := 0.0
	if size >= 25 {
		blueCoarse := xmpGrainValueNoise(x, y, coarseScale, 0x94d049bb133111eb)
		blueFine := xmpGrainValueNoise(x, y, fineScale, 0x8538ecb5bd456ea3)
		blueNoise := blueCoarse*(1-roughness) + blueFine*roughness
		blueWeight := 0.10 + (size-25)/75*0.25
		blueDelta = blueNoise * 0.045 * amount * blueWeight
	}
	pixel.R = encodeLinearSRGB16(srgb16ToLinear[pixel.R] + delta)
	pixel.G = encodeLinearSRGB16(srgb16ToLinear[pixel.G] + delta)
	pixel.B = encodeLinearSRGB16(srgb16ToLinear[pixel.B] + delta + blueDelta)
	return pixel
}

func xmpGrainValueNoise(x, y int, scale float64, seed uint64) float64 {
	if scale <= 0 {
		return 0
	}
	gridX, gridY := float64(x)/scale, float64(y)/scale
	cellX, cellY := math.Floor(gridX), math.Floor(gridY)
	ix, iy := int64(cellX), int64(cellY)
	tx := smoothstep(gridX - cellX)
	ty := smoothstep(gridY - cellY)
	a := xmpGrainLattice(ix, iy, seed)
	b := xmpGrainLattice(ix+1, iy, seed)
	c := xmpGrainLattice(ix, iy+1, seed)
	d := xmpGrainLattice(ix+1, iy+1, seed)
	top := a*(1-tx) + b*tx
	bottom := c*(1-tx) + d*tx
	return top*(1-ty) + bottom*ty
}

func xmpGrainLattice(x, y int64, seed uint64) float64 {
	value := uint64(x)*0x9e3779b185ebca87 ^ uint64(y)*0xc2b2ae3d27d4eb4f ^ seed
	value ^= value >> 30
	value *= 0xbf58476d1ce4e5b9
	value ^= value >> 27
	value *= 0x94d049bb133111eb
	value ^= value >> 31
	return float64(value>>11)*(1.0/(1<<53))*2 - 1
}
