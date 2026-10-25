package lrtl

import (
	"encoding/binary"
	"fmt"
	"image"
	"math"
	"sort"
)

type dngBadPixelList struct {
	bayerPhase uint32
	points     []image.Point
	rectangles []image.Rectangle
}

type dngBadRectNode struct {
	center      int
	byTop       []image.Rectangle
	byBottom    []image.Rectangle
	left, right *dngBadRectNode
}

type dngBadPixelIndex struct {
	points []image.Point
	rects  *dngBadRectNode
}

func parseDNGBadPixelList(data []byte) (dngBadPixelList, error) {
	var result dngBadPixelList
	if len(data) < 16 {
		return result, fmt.Errorf("truncated parameter header")
	}
	size := uint64(binary.BigEndian.Uint32(data))
	result.bayerPhase = binary.BigEndian.Uint32(data[4:])
	pointCount := uint64(binary.BigEndian.Uint32(data[8:]))
	rectangleCount := uint64(binary.BigEndian.Uint32(data[12:]))
	if result.bayerPhase > 3 {
		return dngBadPixelList{}, fmt.Errorf("invalid Bayer phase %d", result.bayerPhase)
	}
	if pointCount > (math.MaxUint32-12)/8 || rectangleCount > (math.MaxUint32-12-pointCount*8)/16 {
		return dngBadPixelList{}, fmt.Errorf("bad-pixel list size overflows")
	}
	expected := uint64(12) + pointCount*8 + rectangleCount*16
	if size != expected || uint64(len(data)) != expected+4 {
		return dngBadPixelList{}, fmt.Errorf("parameter size does not match point and rectangle counts")
	}
	result.points = make([]image.Point, int(pointCount))
	position := 16
	for i := range result.points {
		row := int(int32(binary.BigEndian.Uint32(data[position:])))
		column := int(int32(binary.BigEndian.Uint32(data[position+4:])))
		result.points[i] = image.Pt(column, row)
		position += 8
	}
	result.rectangles = make([]image.Rectangle, int(rectangleCount))
	for i := range result.rectangles {
		top := int(int32(binary.BigEndian.Uint32(data[position:])))
		left := int(int32(binary.BigEndian.Uint32(data[position+4:])))
		bottom := int(int32(binary.BigEndian.Uint32(data[position+8:])))
		right := int(int32(binary.BigEndian.Uint32(data[position+12:])))
		if top >= bottom || left >= right {
			return dngBadPixelList{}, fmt.Errorf("invalid or empty bad rectangle %d", i)
		}
		result.rectangles[i] = image.Rect(left, top, right, bottom)
		position += 16
	}
	sort.Slice(result.points, func(i, j int) bool {
		if result.points[i].Y != result.points[j].Y {
			return result.points[i].Y < result.points[j].Y
		}
		return result.points[i].X < result.points[j].X
	})
	sort.Slice(result.rectangles, func(i, j int) bool {
		if result.rectangles[i].Min.Y != result.rectangles[j].Min.Y {
			return result.rectangles[i].Min.Y < result.rectangles[j].Min.Y
		}
		if result.rectangles[i].Min.X != result.rectangles[j].Min.X {
			return result.rectangles[i].Min.X < result.rectangles[j].Min.X
		}
		if result.rectangles[i].Max.Y != result.rectangles[j].Max.Y {
			return result.rectangles[i].Max.Y < result.rectangles[j].Max.Y
		}
		return result.rectangles[i].Max.X < result.rectangles[j].Max.X
	})
	return result, nil
}

func buildDNGBadRectIndex(rectangles []image.Rectangle) *dngBadRectNode {
	if len(rectangles) == 0 {
		return nil
	}
	centers := make([]int, len(rectangles))
	for i, rectangle := range rectangles {
		centers[i] = rectangle.Min.Y + (rectangle.Max.Y-rectangle.Min.Y)/2
	}
	sort.Ints(centers)
	center := centers[len(centers)/2]
	node := &dngBadRectNode{center: center}
	var left, right []image.Rectangle
	for _, rectangle := range rectangles {
		switch {
		case rectangle.Max.Y <= center:
			left = append(left, rectangle)
		case rectangle.Min.Y > center:
			right = append(right, rectangle)
		default:
			node.byTop = append(node.byTop, rectangle)
			node.byBottom = append(node.byBottom, rectangle)
		}
	}
	// The selected midpoint belongs to at least one input interval, so each
	// recursive call receives fewer rectangles than its parent.
	sort.Slice(node.byTop, func(i, j int) bool { return node.byTop[i].Min.Y < node.byTop[j].Min.Y })
	sort.Slice(node.byBottom, func(i, j int) bool { return node.byBottom[i].Max.Y > node.byBottom[j].Max.Y })
	node.left = buildDNGBadRectIndex(left)
	node.right = buildDNGBadRectIndex(right)
	return node
}

func (node *dngBadRectNode) contains(x, y int) bool {
	for node != nil {
		if y < node.center {
			for _, rectangle := range node.byTop {
				if rectangle.Min.Y > y {
					break
				}
				if x >= rectangle.Min.X && x < rectangle.Max.X && y < rectangle.Max.Y {
					return true
				}
			}
			node = node.left
		} else {
			for _, rectangle := range node.byBottom {
				if rectangle.Max.Y <= y {
					break
				}
				if x >= rectangle.Min.X && x < rectangle.Max.X && y >= rectangle.Min.Y {
					return true
				}
			}
			node = node.right
		}
	}
	return false
}

func (index dngBadPixelIndex) contains(x, y int) bool {
	point := image.Pt(x, y)
	pointIndex := sort.Search(len(index.points), func(i int) bool {
		candidate := index.points[i]
		return candidate.Y > point.Y || candidate.Y == point.Y && candidate.X >= point.X
	})
	if pointIndex < len(index.points) && index.points[pointIndex] == point {
		return true
	}
	return index.rects.contains(x, y)
}

func (opcode dngBadPixelList) index() dngBadPixelIndex {
	points := append([]image.Point(nil), opcode.points...)
	sort.Slice(points, func(i, j int) bool {
		if points[i].Y != points[j].Y {
			return points[i].Y < points[j].Y
		}
		return points[i].X < points[j].X
	})
	return dngBadPixelIndex{points: points, rects: buildDNGBadRectIndex(opcode.rectangles)}
}

func dngSDKIsGreen(x, y int, phase uint32) bool {
	return (x+y+int(phase)+int(phase>>1))&1 == 0
}

func dngSameBayerColor(x1, y1, x2, y2 int, phase uint32) bool {
	if dngSDKIsGreen(x1, y1, phase) {
		return dngSDKIsGreen(x2, y2, phase)
	}
	return x1&1 == x2&1 && y1&1 == y2&1
}

type dngBadPixelEstimate struct {
	left, right uint16
	gradient    uint32
}

func dngSample16(value float32) uint16 {
	value64 := min(1, max(0, float64(value)))
	return uint16(math.Round(value64 * 65535))
}

func repairDNGListedPixel(source []float32, width, height int, bounds image.Rectangle, index dngBadPixelIndex, phase uint32, x, y int) (uint16, bool) {
	const searchRadius = 4
	directions := [4][2]int{{1, 0}, {0, 1}, {1, 1}, {1, -1}}
	var estimates []dngBadPixelEstimate
	minGradient := uint32(math.MaxUint32)
	for _, direction := range directions {
		for distance := 1; distance <= searchRadius; distance++ {
			dx, dy := direction[0]*distance, direction[1]*distance
			leftX, leftY := x-dx, y-dy
			rightX, rightY := x+dx, y+dy
			if !dngSameBayerColor(x, y, leftX, leftY, phase) || !dngSameBayerColor(x, y, rightX, rightY, phase) {
				continue
			}
			if !image.Pt(leftX, leftY).In(bounds) || !image.Pt(rightX, rightY).In(bounds) || index.contains(leftX, leftY) || index.contains(rightX, rightY) {
				continue
			}
			leftLocalX, leftLocalY := leftX-bounds.Min.X, leftY-bounds.Min.Y
			rightLocalX, rightLocalY := rightX-bounds.Min.X, rightY-bounds.Min.Y
			left := dngSample16(source[leftLocalY*width+leftLocalX])
			right := dngSample16(source[rightLocalY*width+rightLocalX])
			difference := int(left) - int(right)
			if difference < 0 {
				difference = -difference
			}
			gradient := uint32(difference)
			estimates = append(estimates, dngBadPixelEstimate{left: left, right: right, gradient: gradient})
			if gradient < minGradient {
				minGradient = gradient
			}
		}
	}
	if len(estimates) == 0 {
		return 0, false
	}
	limit := (minGradient * 3) >> 1
	var total, count uint64
	for _, estimate := range estimates {
		if estimate.gradient > limit {
			continue
		}
		total += uint64(estimate.left) + uint64(estimate.right)
		count += 2
	}
	if count == 0 {
		return 0, false
	}
	return uint16((total + count/2) / count), true
}

func applyDNGBadPixelList(source, destination []float32, width, height int, bounds image.Rectangle, opcode dngBadPixelList) error {
	if width < 1 || height < 1 || uint64(width)*uint64(height) != uint64(len(source)) || len(destination) != len(source) || bounds.Dx() != width || bounds.Dy() != height || opcode.bayerPhase > 3 {
		return fmt.Errorf("invalid FixBadPixelsList image or opcode")
	}
	copy(destination, source)
	index := opcode.index()
	applyPoint := func(point image.Point) {
		if !point.In(bounds) {
			return
		}
		repaired, ok := repairDNGListedPixel(source, width, height, bounds, index, opcode.bayerPhase, point.X, point.Y)
		if ok {
			localX, localY := point.X-bounds.Min.X, point.Y-bounds.Min.Y
			destination[localY*width+localX] = float32(repaired) / 65535
		}
	}
	for _, point := range opcode.points {
		applyPoint(point)
	}
	for _, rectangle := range opcode.rectangles {
		overlap := rectangle.Intersect(bounds)
		for y := overlap.Min.Y; y < overlap.Max.Y; y++ {
			for x := overlap.Min.X; x < overlap.Max.X; x++ {
				applyPoint(image.Pt(x, y))
			}
		}
	}
	return nil
}
