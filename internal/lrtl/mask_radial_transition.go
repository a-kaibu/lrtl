package lrtl

import (
	"fmt"
	"image"
	"math"
	"os"
	"sort"
	"strconv"

	"github.com/a-kaibu/lrtl/internal/fdlibm"
	"github.com/evanoberholster/imagemeta"
)

type radialMaskShape struct {
	top, bottom, left, right, angle float64
	width, height                   float64
}

type radialMaskCoordinates struct {
	x, y, rx, ry, angle float64
}

func radialMaskQuarterAngle(angle float64) float64 {
	a := math.Mod(math.Mod(angle+45, 90)+90, 90) - 45
	if a <= -45 {
		a += 90
	}
	return a
}

func restoreRadialMask(c radialMaskCoordinates, offset, width, height float64) radialMaskShape {
	radius := math.Sqrt(c.rx*c.rx + c.ry*c.ry)
	opening := fdlibm.Atan2(c.ry, c.rx) * (180 / math.Pi)
	return radialShapeFromPolar(c.x, c.y, radius, opening, c.angle+offset, width, height, false)
}

func radialShapeFromPolar(cx, cy, radius, opening, angle, width, height float64, precise bool) radialMaskShape {
	normalized := radialMaskQuarterAngle(angle)
	if math.Mod(math.Floor((angle+45)/90), 2) != 0 {
		opening = 90 - opening
	}
	diagonal := math.Mod(math.Mod(normalized+opening+180, 360)+360, 360) - 180
	if diagonal <= -90 {
		diagonal += 180
	}
	radians := diagonal * (math.Pi / 180)
	sine, cosine := math.Sin(radians), math.Cos(radians)
	if precise {
		sine, cosine = radialExactSinCos(radians)
	}
	x, y := radius*cosine/width, radius*sine/height
	return radialMaskShape{cy - y, cy + y, cx - x, cx + x, normalized, width, height}
}

// Interpolate a physical diagonal vector and an unwrapped ellipse rotation.
// Nearly equal opening angles share their median; this avoids introducing a
// shape wobble from independently interpolated normalized rectangle edges.
func prepareRadialMasks(shapes []radialMaskShape) ([]radialMaskCoordinates, float64, error) {
	prepared := make([]radialMaskCoordinates, len(shapes))
	if len(shapes) == 0 {
		return prepared, 0, nil
	}
	opening, radii, rotation := make([]float64, len(shapes)), make([]float64, len(shapes)), make([]float64, len(shapes))
	for i, s := range shapes {
		for _, v := range []float64{s.top, s.bottom, s.left, s.right, s.angle, s.width, s.height} {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return nil, 0, fmt.Errorf("radial mask geometry must be finite")
			}
		}
		if s.width <= 0 || s.height <= 0 {
			return nil, 0, fmt.Errorf("radial mask image dimensions must be positive")
		}
		dx, dy := (s.right-s.left)*s.width/2, (s.bottom-s.top)*s.height/2
		radii[i] = math.Sqrt(dx*dx + dy*dy)
		if math.IsInf(radii[i], 0) {
			return nil, 0, fmt.Errorf("radial mask radius exceeds finite range")
		}
		bearing := fdlibm.Atan2(dy, dx) * (180 / math.Pi)
		a := math.Mod(math.Mod(bearing-s.angle, 90)+90, 90)
		if a > 45 {
			a = 90 - a
		}
		opening[i] = a
		prepared[i].x, prepared[i].y = (s.left+s.right)/2, (s.top+s.bottom)/2
	}
	ordered := append([]float64(nil), opening...)
	sort.Float64s(ordered)
	median := ordered[len(ordered)/2]
	if len(ordered)%2 == 0 {
		median = (ordered[len(ordered)/2-1] + median) / 2
	}
	for i, s := range shapes {
		if math.Abs(opening[i]-median) < 3 {
			opening[i] = median
		}
		radians := opening[i] * (math.Pi / 180)
		prepared[i].rx, prepared[i].ry = radii[i]*math.Cos(radians), radii[i]*math.Sin(radians)
		best, chosen := math.Inf(1), s.angle
		for turn := -2; turn <= 2; turn++ {
			candidate := s.angle + 90*float64(turn)
			c := prepared[i]
			r := radialShapeFromPolar(c.x, c.y, radii[i], opening[i], candidate, s.width, s.height, true)
			distance := math.Abs(r.top-s.top) + math.Abs(r.bottom-s.bottom) + math.Abs(r.left-s.left) + math.Abs(r.right-s.right)
			if distance < best {
				best, chosen = distance, candidate
			}
		}
		chosen = math.Mod(chosen, 180)
		if chosen > 90 {
			chosen -= 180
		}
		if chosen <= -90 {
			chosen += 180
		}
		rotation[i] = chosen
	}
	deltas := make([]float64, len(rotation))
	for i := 1; i < len(rotation); i++ {
		rotation[i] += 180 * math.Floor((rotation[i-1]-rotation[i])/180+.5)
		deltas[i] = rotation[i] - rotation[i-1]
	}
	if len(rotation) >= 3 {
		trend := 0.0
		for _, d := range deltas[1:] {
			if d != 0 {
				trend += math.Copysign(1, d) / math.Max(math.Abs(d), 1)
			}
		}
		if trend != 0 {
			direction := math.Copysign(1, trend)
			for i := 1; i < len(rotation); i++ {
				d := deltas[i]
				if math.Abs(d) > 80 && math.Copysign(1, d) != direction {
					d += direction * 180
				}
				rotation[i] = rotation[i-1] + d
			}
		}
	}
	minimum, maximum := rotation[0], rotation[0]
	for _, a := range rotation {
		minimum = math.Min(minimum, a)
		maximum = math.Max(maximum, a)
	}
	offset := (minimum + maximum) / 2
	for i, a := range rotation {
		prepared[i].angle = a - offset
	}
	return prepared, offset, nil
}

type radialMaskSize struct{ width, height float64 }

func radialTransitionKey(key maskTransitionKey) bool {
	return key.collection == "MaskGroupBasedCorrections" && (key.correctionIndex == 8 || key.correctionIndex == 9) && key.maskIndex == 1
}

func completeRadialShape(node maskTransitionNode) bool {
	if !node.materializable && node.maskItem == nil {
		return false
	}
	for _, name := range []string{"Top", "Bottom", "Left", "Right", "Angle"} {
		if _, found := node.values[name]; !found {
			return false
		}
	}
	return true
}

// Metadata dimensions take precedence. Image headers are an optional fallback
// for normal CLI sequences; XMP-only workflows need not have decodable images.
func radialMaskFrameSizes(packets []*XMPPacket, frames []Frame) ([]radialMaskSize, error) {
	sizes := make([]radialMaskSize, len(packets))
	for i, packet := range packets {
		values := [2]float64{}
		for axis, names := range [][3]string{{"exif:PixelXDimension", "tiff:ImageWidth", "lrt:Width"}, {"exif:PixelYDimension", "tiff:ImageLength", "lrt:Height"}} {
			for _, name := range names {
				text, found, err := packet.GetRootScalar(name)
				if err != nil {
					return nil, fmt.Errorf("frame %d %s: %w", i+1, name, err)
				}
				if !found {
					continue
				}
				v, err := strconv.ParseInt(text, 10, 32)
				if err != nil || v <= 0 {
					return nil, fmt.Errorf("frame %d %s must be a positive 32-bit integer", i+1, name)
				}
				values[axis] = float64(v)
				break
			}
		}
		if (values[0] == 0 || values[1] == 0) && i < len(frames) && frames[i].Image != "" {
			if file, err := os.Open(frames[i].Image); err == nil {
				w, h := 0, 0
				rawWidth, rawHeight, found, err := nativeImageSize(frames[i].Image)
				if err != nil {
					_ = file.Close()
					return nil, fmt.Errorf("frame %d dimensions: %w", i+1, err)
				}
				if found {
					w, h = int(rawWidth), int(rawHeight)
				} else if config, _, err := image.DecodeConfig(file); err == nil {
					w, h = config.Width, config.Height
				} else if _, err = file.Seek(0, 0); err == nil {
					if metadata, err := imagemeta.Decode(file); err == nil {
						w, h = int(metadata.ImageWidth), int(metadata.ImageHeight)
					}
				}
				_ = file.Close()
				if values[0] == 0 && w > 0 && int64(w) <= math.MaxInt32 {
					values[0] = float64(w)
				}
				if values[1] == 0 && h > 0 && int64(h) <= math.MaxInt32 {
					values[1] = float64(h)
				}
			}
		}
		sizes[i] = radialMaskSize{values[0], values[1]}
	}
	return sizes, nil
}

func radialTransitionSeries(indexes []map[maskTransitionKey]maskTransitionNode, keys []int, key maskTransitionKey, sizes []radialMaskSize, smoothness int, preparationMarks ...[]int) ([]map[string]float64, bool, error) {
	if !radialTransitionKey(key) || len(keys) == 0 || len(sizes) != len(indexes) {
		return nil, false, nil
	}
	marked := keys
	if len(preparationMarks) > 0 {
		marked = preparationMarks[0]
	}
	var shapes []radialMaskShape
	var sourceIndices []int
	allMarked := make(map[int]bool, len(marked))
	for _, k := range marked {
		if k < 0 || k >= len(indexes) {
			continue
		}
		allMarked[k] = true
		if !completeRadialShape(indexes[k][key]) {
			continue
		}
		v, size := indexes[k][key].values, sizes[k]
		if size.width <= 0 || size.height <= 0 {
			return nil, false, nil
		}
		shapes = append(shapes, radialMaskShape{v["Top"], v["Bottom"], v["Left"], v["Right"], v["Angle"], size.width, size.height})
		sourceIndices = append(sourceIndices, k)
	}
	// Every output requires its own physical dimensions for the inverse map.
	for _, s := range sizes {
		if s.width <= 0 || s.height <= 0 {
			return nil, false, nil
		}
	}
	prepared, offset, err := prepareRadialMasks(shapes)
	if err != nil {
		return nil, false, err
	}
	byFrame := make(map[int]radialMaskCoordinates, len(sourceIndices))
	for i, k := range sourceIndices {
		byFrame[k] = prepared[i]
	}
	coordinates := make([]radialMaskCoordinates, len(keys))
	for i, k := range keys {
		c, found := byFrame[k]
		if !found {
			return nil, false, nil
		}
		coordinates[i] = c
	}
	flags := make([]bool, len(indexes))
	for _, k := range keys {
		flags[k] = true
	}
	columns := make([]map[string]float64, len(indexes))
	cursor := 0
	vector := func(c radialMaskCoordinates) []float64 { return []float64{c.x, c.y, c.rx, c.ry, c.angle} }
	names := []string{"lrt:CX", "lrt:CY", "lrt:RX", "lrt:RY", "lrt:AN"}
	for i := range columns {
		for cursor+1 < len(sourceIndices) && sourceIndices[cursor+1] <= i {
			cursor++
		}
		columns[i] = map[string]float64{}
		for j, v := range vector(prepared[cursor]) {
			columns[i][names[j]] = v
		}
	}
	out := make([]map[string]float64, len(indexes))
	sampled := make([][5]float64, len(indexes))
	for field, name := range names {
		if value, copy := numericColumnCopyReference(columns, name, keys, false); copy {
			for i := range sampled {
				sampled[i][field] = value
			}
			continue
		}
		anchors := make([]lrtSplinePoint, len(keys))
		precise := make([]transitionAnchor, len(keys))
		for a, k := range keys {
			value := vector(coordinates[a])[field]
			if math.IsInf(float64(float32(value)), 0) {
				return nil, false, fmt.Errorf("radial mask %s exceeds finite float32 transition range", name)
			}
			anchors[a] = lrtSplinePoint{float32(k), float32(value)}
			precise[a] = transitionAnchor{k, value}
		}
		values, filled, err := sampleLRTNumericTransition(anchors, precise, len(indexes), flags, smoothness, maskTransitionSpecs[name])
		if err != nil {
			return nil, false, err
		}
		for i := range sampled {
			if !flags[i] && !filled[i] {
				return nil, false, fmt.Errorf("radial helper column %s is incomplete at frame %d", name, i+1)
			}
			sampled[i][field] = values[i]
		}
	}
	for i, values := range sampled {
		if allMarked[i] {
			continue
		}
		s := restoreRadialMask(radialMaskCoordinates{values[0], values[1], values[2], values[3], values[4]}, offset, sizes[i].width, sizes[i].height)
		out[i] = map[string]float64{"Top": s.top, "Bottom": s.bottom, "Left": s.left, "Right": s.right, "Angle": s.angle}
		for _, v := range out[i] {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return nil, false, fmt.Errorf("restored radial mask must be finite")
			}
		}
	}
	return out, true, nil
}
