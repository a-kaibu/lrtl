package lrtl

import (
	"encoding/binary"
	"fmt"
	"math"
)

type dngWarp struct {
	coefficients     [][6]float64
	centerX, centerY float64
	fisheye          bool
}

type dngImageOpcode struct {
	point    *dngPointOpcode
	warp     *dngWarp
	vignette *dngVignetteRadial
}

func parseDNGWarpRectilinear(data []byte, channels int) (*dngWarp, error) {
	return parseDNGWarp(data, channels, 6, false)
}

func parseDNGWarpFisheye(data []byte, channels int) (*dngWarp, error) {
	return parseDNGWarp(data, channels, 4, true)
}

func parseDNGWarp(data []byte, channels, coefficientCount int, fisheye bool) (*dngWarp, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("truncated warp parameters")
	}
	count := uint64(binary.BigEndian.Uint32(data))
	coefficientBytes := uint64(coefficientCount * 8)
	if (count != 1 && count != uint64(channels)) || uint64(len(data)) != 20+count*coefficientBytes {
		return nil, fmt.Errorf("invalid warp coefficient count")
	}
	warp := &dngWarp{coefficients: make([][6]float64, int(count)), fisheye: fisheye}
	for i := range warp.coefficients {
		for j := 0; j < coefficientCount; j++ {
			value := math.Float64frombits(binary.BigEndian.Uint64(data[4+i*int(coefficientBytes)+j*8:]))
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return nil, fmt.Errorf("non-finite warp coefficient")
			}
			warp.coefficients[i][j] = value
		}
	}
	offset := 4 + int(count*coefficientBytes)
	warp.centerX, warp.centerY = math.Float64frombits(binary.BigEndian.Uint64(data[offset:])), math.Float64frombits(binary.BigEndian.Uint64(data[offset+8:]))
	if math.IsNaN(warp.centerX) || math.IsNaN(warp.centerY) || math.IsInf(warp.centerX, 0) || math.IsInf(warp.centerY, 0) || warp.centerX < 0 || warp.centerX > 1 || warp.centerY < 0 || warp.centerY > 1 {
		return nil, fmt.Errorf("warp center must be within normalized image bounds")
	}
	return warp, nil
}

func (warp *dngWarp) identity() bool {
	if warp.fisheye {
		// A polynomial in atan(r) is not the identity mapping over the full
		// image, so even a zero-order fisheye coefficient needs resampling.
		return false
	}
	for _, c := range warp.coefficients {
		if c != ([6]float64{1, 0, 0, 0, 0, 0}) {
			return false
		}
	}
	return true
}

type dngWarpGeometry struct {
	warp                                    *dngWarp
	centerX, centerY, radius, verticalScale float64
}

func (warp *dngWarp) geometry(width, height int, verticalScale float64) dngWarpGeometry {
	// DefaultScale defines the physical pixel aspect. The SDK rounds the
	// square-pixel height for its normalization radius, but keeps original
	// image coordinates when locating the center and sampling pixels.
	squareHeight := math.Round(float64(height) * verticalScale)
	x := float64(width) * warp.centerX
	y := squareHeight * warp.centerY
	radius := math.Hypot(max(x, float64(width)-x), max(y, squareHeight-y))
	return dngWarpGeometry{warp: warp, centerX: x, centerY: float64(height) * warp.centerY, radius: radius, verticalScale: verticalScale}
}

func (geometry dngWarpGeometry) source(x, y, plane int) (float64, float64) {
	c := geometry.warp.coefficients[plane%len(geometry.warp.coefficients)]
	dx, dy := float64(x)-geometry.centerX, float64(y)-geometry.centerY
	u, v := dx/geometry.radius, dy*geometry.verticalScale/geometry.radius
	r2 := min(1, u*u+v*v)
	if geometry.warp.fisheye {
		r := math.Sqrt(r2)
		factor := c[0]
		if r > 0 {
			theta := math.Atan(r)
			theta2 := theta * theta
			factor = theta * (c[0] + theta2*(c[1]+theta2*(c[2]+theta2*c[3]))) / r
		}
		return geometry.centerX + dx*factor, geometry.centerY + dy*factor
	}
	radial := c[0] + r2*(c[1]+r2*(c[2]+r2*c[3]))
	tangentX := 2*c[4]*u*v + c[5]*(r2+2*u*u)
	tangentY := 2*c[5]*u*v + c[4]*(r2+2*v*v)
	return geometry.centerX + dx*radial + geometry.radius*tangentX, geometry.centerY + dy*radial + geometry.radius*tangentY/geometry.verticalScale
}

// Cubic convolution with A=-0.75, as used by the SDK's bicubic kernel.
// This evaluates weights directly instead of its subsampled lookup table.
func dngWarpCubicWeight(distance float64) float64 {
	x := math.Abs(distance)
	if x >= 2 {
		return 0
	}
	if x < 1 {
		return 1 + x*x*(1.25*x-2.25)
	}
	return 3 + x*(-6+x*(3.75-.75*x))
}

func sampleDNGWarpPlane(data []float32, width, height, channels, plane int, x, y float64) float32 {
	x = min(float64(width-1), max(0, x))
	y = min(float64(height-1), max(0, y))
	left, top := int(math.Floor(x)), int(math.Floor(y))
	value := 0.0
	for row := top - 1; row <= top+2; row++ {
		wy := dngWarpCubicWeight(y - float64(row))
		sy := min(height-1, max(0, row))
		for col := left - 1; col <= left+2; col++ {
			wx := dngWarpCubicWeight(x - float64(col))
			sx := min(width-1, max(0, col))
			value += float64(data[(sy*width+sx)*channels+plane]) * wx * wy
		}
	}
	return float32(min(1, max(0, value)))
}

func applyDNGImageOpcodes(opcodes []dngImageOpcode, data []float32, width, height, channels int, verticalScale float64) ([]float32, error) {
	var scratch []float32
	for i := 0; i < len(opcodes); {
		if vignette := opcodes[i].vignette; vignette != nil {
			i++
			if vignette.identity() {
				continue
			}
			if err := vignette.apply(data, width, height, channels); err != nil {
				return nil, err
			}
			continue
		}
		if warp := opcodes[i].warp; warp != nil {
			i++
			if warp.identity() {
				continue
			}
			if scratch == nil {
				scratch = make([]float32, len(data))
			}
			geometry := warp.geometry(width, height, verticalScale)
			for y := 0; y < height; y++ {
				for x := 0; x < width; x++ {
					for plane := 0; plane < channels; plane++ {
						sx, sy := geometry.source(x, y, plane)
						if math.IsNaN(sx) || math.IsNaN(sy) || math.IsInf(sx, 0) || math.IsInf(sy, 0) {
							return nil, fmt.Errorf("non-finite DNG warp coordinate")
						}
						scratch[(y*width+x)*channels+plane] = sampleDNGWarpPlane(data, width, height, channels, plane, sx, sy)
					}
				}
			}
			data, scratch = scratch, data
			continue
		}
		var points []dngPointOpcode
		if opcodes[i].point == nil {
			return nil, fmt.Errorf("invalid DNG image opcode")
		}
		for i < len(opcodes) && opcodes[i].point != nil {
			points = append(points, *opcodes[i].point)
			i++
		}
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				index := (y*width + x) * channels
				var camera [3]float64
				for channel := 0; channel < channels; channel++ {
					camera[channel] = float64(data[index+channel])
				}
				applyDNGPointOpcodes(points, &camera, x, y, channels, width, height)
				for channel := 0; channel < channels; channel++ {
					data[index+channel] = float32(camera[channel])
				}
			}
		}
	}
	return data, nil
}
