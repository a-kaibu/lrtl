package lrtl

import (
	"fmt"
	"sort"
)

// lrtSplinePoint and lrtTransitionSpline reproduce the cubic Bezier spline
// used by LRTimelapse's Auto-Transition numeric property path. The installed
// 7.5.5 UI maps its 0..100 smoothness slider to f=(100-slider)/500.
type lrtSplinePoint struct {
	x float32
	y float32
}

type lrtTransitionSpline struct {
	points   []lrtSplinePoint
	tangents []lrtSplinePoint
}

func newLRTTransitionSpline(points []lrtSplinePoint, smoothness int) (*lrtTransitionSpline, error) {
	if smoothness < 0 || smoothness > 100 {
		return nil, fmt.Errorf("smoothness must be between 0 and 100")
	}
	if len(points) < 2 {
		return nil, fmt.Errorf("a transition spline needs at least two anchor values")
	}
	points = append([]lrtSplinePoint(nil), points...)
	sort.Slice(points, func(i, j int) bool { return points[i].x < points[j].x })
	for index := 1; index < len(points); index++ {
		if points[index].x <= points[index-1].x {
			return nil, fmt.Errorf("transition spline anchors must have unique frame positions")
		}
	}

	spline := &lrtTransitionSpline{
		points:   points,
		tangents: make([]lrtSplinePoint, len(points)),
	}
	f := float32(100-smoothness) / 500
	if f == 0 || len(points) < 3 {
		return spline, nil
	}

	d := 1 / f
	j := make([]float32, len(points))
	c := make([]lrtSplinePoint, len(points))
	j[1] = -f
	c[1] = splineScale(splineSubtract(splineSubtract(points[2], points[0]), spline.tangents[0]), f)
	for index := 2; index < len(points)-1; index++ {
		j[index] = -1 / (d + j[index-1])
		difference := splineSubtract(splineSubtract(points[index+1], points[index-1]), c[index-1])
		c[index] = splineScale(difference, -j[index])
	}
	for index := len(points) - 2; index > 0; index-- {
		spline.tangents[index] = splineAdd(c[index], splineScale(spline.tangents[index+1], j[index]))
	}
	return spline, nil
}

// sampleFrames mirrors LRT's Auto-Transition discretization: each spline
// segment is sampled at 2*frameCount uniformly spaced parameters, each sample
// is assigned to int(x), and the first sample for each non-keyframe wins.
func (spline *lrtTransitionSpline) sampleFrames(frameCount int, keyframe []bool) ([]float64, []bool) {
	values := make([]float64, frameCount)
	filled := make([]bool, frameCount)
	if frameCount == 0 || spline == nil || len(spline.points) < 2 {
		return values, filled
	}
	for index := range filled {
		if index < len(keyframe) {
			filled[index] = keyframe[index]
		}
	}
	steps := frameCount * 2
	if steps < 1 {
		steps = 1
	}
	parameterStep := float32(1) / float32(steps)
	for segment := 0; segment < len(spline.points)-1; segment++ {
		start, end := spline.points[segment], spline.points[segment+1]
		control1 := splineAdd(start, spline.tangents[segment])
		control2 := splineSubtract(end, spline.tangents[segment+1])
		parameter := float32(0)
		for sample := 0; sample < steps; sample++ {
			point := cubicBezier(start, control1, control2, end, parameter)
			frame := int(point.x)
			if frame < 0 {
				frame = 0
			} else if frame >= frameCount {
				frame = frameCount - 1
			}
			if !filled[frame] {
				values[frame] = float64(point.y)
				filled[frame] = true
			}
			parameter += parameterStep
		}
	}
	last := spline.points[len(spline.points)-1]
	frame := int(last.x)
	if frame < 0 {
		frame = 0
	} else if frame >= frameCount {
		frame = frameCount - 1
	}
	if !filled[frame] {
		values[frame] = float64(last.y)
		filled[frame] = true
	}
	return values, filled
}

func transitionSplinePoints(samples []lrtSplinePoint, frameCount int) []lrtSplinePoint {
	if len(samples) == 0 || frameCount <= 0 {
		return nil
	}
	samples = append([]lrtSplinePoint(nil), samples...)
	sort.Slice(samples, func(i, j int) bool { return samples[i].x < samples[j].x })
	if samples[0].x > 0 {
		samples = append([]lrtSplinePoint{{x: 0, y: samples[0].y}}, samples...)
	}
	lastFrame := float32(frameCount - 1)
	if samples[len(samples)-1].x < lastFrame {
		samples = append(samples, lrtSplinePoint{x: lastFrame, y: samples[len(samples)-1].y})
	}
	return samples
}

func splineAdd(left, right lrtSplinePoint) lrtSplinePoint {
	return lrtSplinePoint{x: left.x + right.x, y: left.y + right.y}
}

func splineSubtract(left, right lrtSplinePoint) lrtSplinePoint {
	return lrtSplinePoint{x: left.x - right.x, y: left.y - right.y}
}

func splineScale(point lrtSplinePoint, scale float32) lrtSplinePoint {
	return lrtSplinePoint{x: point.x * scale, y: point.y * scale}
}

func cubicBezier(p0, p1, p2, p3 lrtSplinePoint, t float32) lrtSplinePoint {
	oneMinusT := 1 - t
	b0 := oneMinusT * oneMinusT * oneMinusT
	b1 := 3 * oneMinusT * oneMinusT * t
	b2 := 3 * oneMinusT * t * t
	b3 := t * t * t
	return lrtSplinePoint{
		x: p0.x*b0 + p1.x*b1 + p2.x*b2 + p3.x*b3,
		y: p0.y*b0 + p1.y*b1 + p2.y*b2 + p3.y*b3,
	}
}
