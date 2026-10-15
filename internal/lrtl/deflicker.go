package lrtl

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"sort"
	"strconv"
	"strings"
)

var srgb8ToLinear [256]float64

func init() {
	for value := range srgb8ToLinear {
		channel := float64(value) / 255
		if channel <= 0.04045 {
			srgb8ToLinear[value] = channel / 12.92
		} else {
			srgb8ToLinear[value] = math.Pow((channel+0.055)/1.055, 2.4)
		}
	}
}

func parseReferenceArea(value string) (image.Rectangle, error) {
	parts := strings.Split(value, ",")
	if len(parts) != 4 {
		return image.Rectangle{}, fmt.Errorf("reference area must be x,y,width,height")
	}
	values := [4]int{}
	for index, part := range parts {
		parsed, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			return image.Rectangle{}, fmt.Errorf("invalid reference area %q: %w", value, err)
		}
		values[index] = parsed
	}
	x, y, width, height := values[0], values[1], values[2], values[3]
	maxInt := int(^uint(0) >> 1)
	if x < 0 || y < 0 || width < 1 || height < 1 || x > maxInt-width || y > maxInt-height {
		return image.Rectangle{}, fmt.Errorf("reference area coordinates and dimensions are out of range")
	}
	return image.Rect(x, y, x+width, y+height), nil
}

// frameLuminance samples a bounded grid and takes its median in linear-light
// sRGB. The median reduces the influence of small moving subjects.
func frameLuminance(img image.Image, reference image.Rectangle) (float64, error) {
	bounds := img.Bounds()
	region := bounds.Intersect(reference)
	if region.Empty() || region != reference {
		return 0, fmt.Errorf("reference area %v is outside image bounds %v", reference, bounds)
	}
	stepX := max(1, (region.Dx()+63)/64)
	stepY := max(1, (region.Dy()+63)/64)
	samples := make([]float64, 0, 4096)
	for y := region.Min.Y; y < region.Max.Y; y += stepY {
		for x := region.Min.X; x < region.Max.X; x += stepX {
			pixel := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			alpha := float64(pixel.A) / 255
			luminance := 0.2126*srgb8ToLinear[pixel.R] +
				0.7152*srgb8ToLinear[pixel.G] +
				0.0722*srgb8ToLinear[pixel.B]
			// The AVI output is opaque; treat transparent PNG pixels as black.
			samples = append(samples, luminance*alpha)
		}
	}
	sort.Float64s(samples)
	median := samples[len(samples)/2]
	if median <= 1e-8 || math.IsNaN(median) || math.IsInf(median, 0) {
		return 0, fmt.Errorf("reference area has no measurable luminance")
	}
	return median, nil
}

func deflickerGains(luminances []float64, smoothingRadius, passes int, constant bool) ([]float64, error) {
	return deflickerGainsWithSmoother(luminances, smoothingRadius, passes, constant, "gaussian")
}

func deflickerGainsWithSmoother(luminances []float64, smoothingRadius, passes int, constant bool, smoother string) ([]float64, error) {
	smoother = strings.ToLower(strings.TrimSpace(smoother))
	if len(luminances) == 0 {
		return nil, fmt.Errorf("cannot deflicker an empty sequence")
	}
	if smoothingRadius < 1 || smoothingRadius > 1000 {
		return nil, fmt.Errorf("smoothing radius must be between 1 and 1000 frames: %d", smoothingRadius)
	}
	if passes < 1 || passes > 8 {
		return nil, fmt.Errorf("deflicker passes must be between 1 and 8: %d", passes)
	}
	if smoother != "gaussian" && smoother != "lowess" {
		return nil, fmt.Errorf("deflicker smoother must be gaussian or lowess: %q", smoother)
	}
	logs := make([]float64, len(luminances))
	for index, luminance := range luminances {
		if luminance <= 0 || math.IsNaN(luminance) || math.IsInf(luminance, 0) {
			return nil, fmt.Errorf("frame %d has invalid luminance", index+1)
		}
		logs[index] = math.Log2(luminance)
	}
	if len(logs) == 1 {
		return []float64{1}, nil
	}

	correction := make([]float64, len(logs))
	const passResponse = 0.7
	for pass := 0; pass < passes; pass++ {
		var target []float64
		if constant {
			median := medianFloat64(logs)
			target = make([]float64, len(logs))
			for index := range target {
				target[index] = median
			}
		} else {
			target = smoothDeflickerLuminance(logs, smoothingRadius, smoother)
		}
		for index := range logs {
			// Leave a residual so additional passes refine the result gradually.
			delta := (target[index] - logs[index]) * passResponse
			logs[index] += delta
			correction[index] += delta
		}
	}
	gains := make([]float64, len(correction))
	for index, stops := range correction {
		gains[index] = math.Exp2(stops)
	}
	return gains, nil
}

func smoothDeflickerLuminance(values []float64, radius int, smoother string) []float64 {
	if smoother == "lowess" {
		neighbors := min(len(values), 2*radius+1)
		return lowessSmooth(values, neighbors)
	}
	return smoothLogLuminance(values, radius)
}

func medianFloat64(values []float64) float64 {
	ordered := append([]float64(nil), values...)
	sort.Float64s(ordered)
	return ordered[len(ordered)/2]
}

func smoothLogLuminance(values []float64, radius int) []float64 {
	result := make([]float64, len(values))
	sigma := math.Max(float64(radius)/2, 0.5)
	for index := range values {
		start := max(0, index-radius)
		end := min(len(values)-1, index+radius)
		weighted, weightSum := 0.0, 0.0
		for sample := start; sample <= end; sample++ {
			distance := float64(sample - index)
			weight := math.Exp(-0.5 * distance * distance / (sigma * sigma))
			weighted += values[sample] * weight
			weightSum += weight
		}
		result[index] = weighted / weightSum
	}
	return result
}

func applyExposureGain(img image.Image, gain float64) *image.NRGBA {
	bounds := img.Bounds()
	result := image.NewNRGBA(bounds)
	draw.Draw(result, bounds, img, bounds.Min, draw.Src)
	var lookup [256]uint8
	for value, linear := range srgb8ToLinear {
		adjusted := math.Min(linear*gain, 1)
		encoded := 12.92 * adjusted
		if adjusted > 0.0031308 {
			encoded = 1.055*math.Pow(adjusted, 1.0/2.4) - 0.055
		}
		lookup[value] = uint8(math.Round(math.Max(0, math.Min(encoded, 1)) * 255))
	}
	for offset := 0; offset < len(result.Pix); offset += 4 {
		result.Pix[offset] = lookup[result.Pix[offset]]
		result.Pix[offset+1] = lookup[result.Pix[offset+1]]
		result.Pix[offset+2] = lookup[result.Pix[offset+2]]
	}
	return result
}
