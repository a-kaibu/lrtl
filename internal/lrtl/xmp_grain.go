package lrtl

import (
	"image"
	"math"
)

// xmpApplyGrain adds a coordinate-stable, monochrome grain approximation.
// Keeping the pattern tied to image coordinates avoids frame-to-frame random
// changes when a timelapse uses the same framing. Adobe's exact grain kernel
// and GrainFrequency transfer curve are not public, so this is not pixel
// equivalent to Camera Raw.
func xmpApplyGrain(img *image.NRGBA64, amount, size, frequency float64) *image.NRGBA64 {
	if img == nil || amount <= 0 || math.IsNaN(amount) {
		return img
	}
	amount = math.Min(100, amount) / 100
	size = math.Max(0, math.Min(100, size))
	frequency = math.Max(0, math.Min(100, frequency))
	coarseScale := 0.7 + size/100*1.8
	fineScale := math.Max(0.4, coarseScale*0.38)
	roughness := frequency / 100
	amplitude := amount * 0.045

	for y := img.Rect.Min.Y; y < img.Rect.Max.Y; y++ {
		for x := img.Rect.Min.X; x < img.Rect.Max.X; x++ {
			index := img.PixOffset(x, y)
			alpha := uint16(img.Pix[index+6])<<8 | uint16(img.Pix[index+7])
			if alpha == 0 {
				continue
			}
			coarse := xmpGrainValueNoise(x, y, coarseScale, 0x9e3779b97f4a7c15)
			fine := xmpGrainValueNoise(x, y, fineScale, 0xd1b54a32d192ed03)
			noise := coarse*(1-roughness) + fine*roughness
			delta := noise * amplitude

			red := uint16(img.Pix[index])<<8 | uint16(img.Pix[index+1])
			green := uint16(img.Pix[index+2])<<8 | uint16(img.Pix[index+3])
			blue := uint16(img.Pix[index+4])<<8 | uint16(img.Pix[index+5])
			red = encodeLinearSRGB16(srgb16ToLinear[red] + delta)
			green = encodeLinearSRGB16(srgb16ToLinear[green] + delta)
			blue = encodeLinearSRGB16(srgb16ToLinear[blue] + delta)
			img.Pix[index], img.Pix[index+1] = byte(red>>8), byte(red)
			img.Pix[index+2], img.Pix[index+3] = byte(green>>8), byte(green)
			img.Pix[index+4], img.Pix[index+5] = byte(blue>>8), byte(blue)
		}
	}
	return img
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
