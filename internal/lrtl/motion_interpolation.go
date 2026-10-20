package lrtl

import "fmt"

// The stored mode-2 graph selects bidirectional estimation. In that branch
// each 16x16 block uses a 32x32 overlap support, including its cost window.
// The adaptive/variable-size options belong to the bilateral branch.
type motionVector struct{ x, y int }
type motionBlock [2]motionVector

type motionEstimator struct {
	width, height, columns, rows int
	previous, older              []motionBlock
	fields                       map[int][]motionBlock
	next                         int
	stamp                        uint32
	visited                      [65 * 65]uint32
	costs                        [65 * 65]uint32
}

func newMotionEstimator(width, height int) (*motionEstimator, error) {
	if width < 32 || height < 32 {
		return nil, fmt.Errorf("motion interpolation requires output dimensions of at least 32x32")
	}
	blocks := (width / 16) * (height / 16)
	return &motionEstimator{width: width, height: height, columns: width / 16, rows: height / 16, previous: make([]motionBlock, blocks), older: make([]motionBlock, blocks), fields: make(map[int][]motionBlock)}, nil
}

func medianMotion(a, b, c motionVector) motionVector {
	return motionVector{a.x + b.x + c.x - min(a.x, b.x, c.x) - max(a.x, b.x, c.x), a.y + b.y + c.y - min(a.y, b.y, c.y) - max(a.y, b.y, c.y)}
}

func (m *motionEstimator) search(current, reference []byte, field []motionBlock, bx, by, direction int) motionVector {
	i := by*m.columns + bx
	var spatial []motionVector
	if bx > 0 {
		spatial = append(spatial, field[i-1][direction])
	}
	if by > 0 {
		spatial = append(spatial, field[i-m.columns][direction])
	}
	if by > 0 && bx+1 < m.columns {
		spatial = append(spatial, field[i-m.columns+1][direction])
	}
	prediction := motionVector{}
	switch len(spatial) {
	case 1:
		prediction = spatial[0]
	case 2:
		prediction = medianMotion(motionVector{}, spatial[0], spatial[1])
	case 3:
		prediction = medianMotion(spatial[0], spatial[1], spatial[2])
	}
	prior := m.previous[i][direction]
	old := m.older[i][direction]
	candidates := []motionVector{prediction, {}}
	candidates = append(candidates, spatial...)
	candidates = append(candidates, prior, motionVector{2*prior.x - old.x, 2*prior.y - old.y})
	if bx > 0 {
		candidates = append(candidates, m.previous[i-1][direction])
	}
	if by > 0 {
		candidates = append(candidates, m.previous[i-m.columns][direction])
	}
	if bx+1 < m.columns {
		candidates = append(candidates, m.previous[i+1][direction])
	}
	if by+1 < m.rows {
		candidates = append(candidates, m.previous[i+m.columns][direction])
	}
	x, y := bx*16, by*16
	xmin, xmax := max(0, x-32), min((m.columns-1)*16, x+32)
	ymin, ymax := max(0, y-32), min((m.rows-1)*16, y+32)
	cx, cy := min((m.columns-1)*16-8, max(8, x)), min((m.rows-1)*16-8, max(8, y))
	best, bestCost := motionVector{x, y}, int(^uint(0)>>1)
	m.stamp++
	if m.stamp == 0 {
		clear(m.visited[:])
		m.stamp = 1
	}
	consider := func(px, py int) {
		if px < xmin || px > xmax || py < ymin || py > ymax || bestCost == 0 {
			return
		}
		key := (py-ymin)*65 + px - xmin
		cost := 0
		if m.visited[key] == m.stamp {
			cost = int(m.costs[key])
		} else {
			cost = 64 * (absInt(px-x-prediction.x) + absInt(py-y-prediction.y))
			rx, ry := min((m.columns-1)*16-8, max(8, px)), min((m.rows-1)*16-8, max(8, py))
			for dy := -8; dy < 24 && cost < bestCost; dy++ {
				a, b := (cy+dy)*m.width+cx-8, (ry+dy)*m.width+rx-8
				for dx := 0; dx < 32; dx++ {
					cost += absInt(int(current[a+dx]) - int(reference[b+dx]))
				}
			}
			// An aborted noncompetitive cost remains noncompetitive: the
			// best score only decreases during this block's search.
			m.visited[key], m.costs[key] = m.stamp, uint32(cost)
		}
		if cost < bestCost {
			best, bestCost = motionVector{px, py}, cost
		}
	}
	for _, candidate := range candidates {
		consider(x+candidate.x, y+candidate.y)
	}
	for {
		center := best
		for _, step := range [4]motionVector{{-1, 0}, {0, -1}, {1, 0}, {0, 1}} {
			consider(center.x+step.x, center.y+step.y)
		}
		if best == center {
			break
		}
	}
	return motionVector{best.x - x, best.y - y}
}

func (m *motionEstimator) estimate(index int, previous, current, next []byte) error {
	if index != m.next || len(previous) != m.width*m.height || len(current) != len(previous) || len(next) != len(previous) {
		return fmt.Errorf("motion estimation requires sequential, consistently sized luma planes")
	}
	field := make([]motionBlock, m.columns*m.rows)
	for direction, reference := range [2][]byte{previous, next} {
		for by := 0; by < m.rows; by++ {
			for bx := 0; bx < m.columns; bx++ {
				field[by*m.columns+bx][direction] = m.search(current, reference, field, bx, by, direction)
			}
		}
	}
	m.older, m.previous = m.previous, field
	m.fields[index] = field
	for key := range m.fields {
		if key < index-1 {
			delete(m.fields, key)
		}
	}
	m.next++
	return nil
}

// Quantized separable tent support generating the measured 32x32 integer weights.
func motionOverlapWeight(x, y int) int {
	x = 2*min(x, 31-x) + 1
	y = 2*min(y, 31-y) + 1
	return ((x*y + 8) / 16) * 4
}

func (m *motionEstimator) interpolate(leftIndex, alpha int, left, right [3][]byte, scene bool) ([3][]byte, error) {
	var output [3][]byte
	if alpha < 0 || alpha > 1024 {
		return output, fmt.Errorf("motion interpolation phase must be between 0 and 1024")
	}
	for p := range output {
		if len(left[p]) != m.width*m.height || len(right[p]) != len(left[p]) {
			return output, fmt.Errorf("motion interpolation plane dimensions differ")
		}
		output[p] = blendInterpolationPlane(left[p], right[p], alpha, scene)
	}
	if scene || alpha == 0 || alpha == 1024 {
		return output, nil
	}
	a, hasLeft := m.fields[leftIndex]
	b, hasRight := m.fields[leftIndex+1]
	if !hasLeft || !hasRight {
		return output, fmt.Errorf("motion interpolation requires both neighboring vector fields")
	}
	area := m.width * m.height
	weights := make([]uint32, area)
	references := make([]uint8, area)
	var sums [3][]uint32
	for p := range sums {
		sums[p] = make([]uint32, area)
	}
	for direction, field := range [2][]motionBlock{b, a} {
		fraction := 1024 - alpha
		if direction == 1 {
			fraction = alpha
		}
		for by := 0; by < m.rows; by++ {
			for bx := 0; bx < m.columns; bx++ {
				vector := field[by*m.columns+bx][direction]
				sx, sy := bx*16-8+vector.x*fraction/1024, by*16-8+vector.y*fraction/1024
				if direction == 1 {
					vector.x, vector.y = -vector.x, -vector.y
				}
				xmin, xmax := max(0, min(m.width-1, sx)), max(0, min(m.width-1, sx+32))
				ymin, ymax := max(0, min(m.height-1, sy)), max(0, min(m.height-1, sy+32))
				for y := ymin; y < ymax; y++ {
					for x := xmin; x < xmax; x++ {
						i := y*m.width + x
						weight := motionOverlapWeight(x-sx, y-sy)
						if weight == 0 || references[i] >= 32 {
							continue
						}
						dx0 := min(m.width-x-1, max(-x, vector.x*alpha/1024))
						dy0 := min(m.height-y-1, max(-y, vector.y*alpha/1024))
						dx1 := min(m.width-x-1, max(-x, -vector.x*(1024-alpha)/1024))
						dy1 := min(m.height-y-1, max(-y, -vector.y*(1024-alpha)/1024))
						w0, w1 := uint32(weight*(1024-alpha)), uint32(weight*alpha)
						for p := range sums {
							sums[p][i] += w0*uint32(left[p][i+dy0*m.width+dx0]) + w1*uint32(right[p][i+dy1*m.width+dx1])
						}
						weights[i] += w0 + w1
						references[i] += 2
					}
				}
			}
		}
	}
	for i, weight := range weights {
		if weight > 0 {
			for p := range output {
				output[p][i] = byte((sums[p][i] + weight/2) / weight)
			}
		}
	}
	return output, nil
}
