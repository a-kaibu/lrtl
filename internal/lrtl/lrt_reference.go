package lrtl

import (
	"encoding/json"
	"fmt"
	"image"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type lrtReferenceBox [4]float32
type lrtReferenceAnchor struct {
	index int
	box   lrtReferenceBox
}

func parseLRTReferenceAnchors(rows []string, count int) ([]lrtReferenceAnchor, error) {
	byIndex := make(map[int]lrtReferenceBox)
	for _, row := range rows {
		parts := strings.Split(row, ";")
		if len(parts) != 5 {
			return nil, fmt.Errorf("invalid referenceBoxes entry %q: expected index;x1;y1;x2;y2", row)
		}
		index, err := strconv.Atoi(strings.TrimSpace(parts[0]))
		if err != nil || index < 0 || index >= count {
			return nil, fmt.Errorf("referenceBoxes index out of sequence range: %q", row)
		}
		var box lrtReferenceBox
		for i := range box {
			v, err := strconv.ParseFloat(strings.TrimSpace(parts[i+1]), 32)
			if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
				return nil, fmt.Errorf("referenceBoxes coordinates must be finite: %q", row)
			}
			box[i] = float32(math.Max(0, math.Min(1, v)))
		}
		if box[2] <= box[0] || box[3] <= box[1] {
			return nil, fmt.Errorf("referenceBoxes entry has empty or reversed bounds: %q", row)
		}
		byIndex[index] = box // LRT applies entries in file order; the last duplicate wins.
	}
	if len(byIndex) == 0 {
		return nil, fmt.Errorf("no referenceBoxes saved in LRT sequence settings")
	}
	anchors := make([]lrtReferenceAnchor, 0, len(byIndex))
	for i, box := range byIndex {
		anchors = append(anchors, lrtReferenceAnchor{i, box})
	}
	sort.Slice(anchors, func(i, j int) bool { return anchors[i].index < anchors[j].index })
	return anchors, nil
}

// The reference-box path uses an equally parameterized natural cubic through
// saved boxes, rather than Auto-Transition's Bezier curve. Evaluate with float32
// arithmetic, including the virtual final anchor at count (not count-1).
func interpolateLRTReferences(anchors []lrtReferenceAnchor, count int) []lrtReferenceBox {
	result := make([]lrtReferenceBox, count)
	if len(anchors) == 1 {
		for i := range result {
			result[i] = anchors[0].box
		}
		return result
	}
	points := append([]lrtReferenceAnchor(nil), anchors...)
	if points[0].index != 0 {
		points = append([]lrtReferenceAnchor{{0, points[0].box}}, points...)
	}
	if points[len(points)-1].index != count-1 {
		points = append(points, lrtReferenceAnchor{count, points[len(points)-1].box})
	}
	n := len(points) - 1
	for axis := 0; axis < 4; axis++ {
		y := make([]float32, len(points))
		for i, p := range points {
			y[i] = p.box[axis]
		}
		// Solve for derivatives with natural endpoint boundary conditions.
		upper, rhs, derivative := make([]float32, n+1), make([]float32, n+1), make([]float32, n+1)
		upper[0] = .5
		for i := 1; i < n; i++ {
			upper[i] = 1 / (4 - upper[i-1])
		}
		upper[n] = 1 / (2 - upper[n-1])
		rhs[0] = 3 * (y[1] - y[0]) * upper[0]
		for i := 1; i < n; i++ {
			rhs[i] = (3*(y[i+1]-y[i-1]) - rhs[i-1]) * upper[i]
		}
		rhs[n] = (3*(y[n]-y[n-1]) - rhs[n-1]) * upper[n]
		derivative[n] = rhs[n]
		for i := n - 1; i >= 0; i-- {
			derivative[i] = rhs[i] - upper[i]*derivative[i+1]
		}
		for segment := 0; segment < n; segment++ {
			a, b := y[segment], derivative[segment]
			c := 3*(y[segment+1]-a) - 2*b - derivative[segment+1]
			d := 2*(a-y[segment+1]) + b + derivative[segment+1]
			left, right := points[segment].index, points[segment+1].index
			for i := left; i < right && i < count; i++ {
				parameter := float32(segment) + float32(i-left)/float32(right-left)
				t := parameter - float32(segment)
				v := ((d*t+c)*t+b)*t + a
				result[i][axis] = max(0, min(1, v))
			}
		}
		if points[n].index == count-1 {
			result[count-1][axis] = points[n].box[axis]
		}
	}
	return result
}

func loadLRTReferences(frames []Frame) ([]lrtReferenceBox, error) {
	if len(frames) == 0 {
		return nil, fmt.Errorf("LRT reference settings require a complete image sequence")
	}
	root := filepath.Dir(frames[0].Image)
	complete, err := DiscoverSequence(root)
	if err != nil {
		return nil, err
	}
	if len(complete) != len(frames) {
		return nil, fmt.Errorf("LRT reference settings require the complete sequence in filename order")
	}
	for i, frame := range frames {
		if !sameFilesystemPath(frame.Image, complete[i].Image) {
			return nil, fmt.Errorf("LRT reference settings require the complete sequence in filename order")
		}
	}
	path := filepath.Join(root, ".lrt", "lrtsequence.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read LRT reference settings: %w", err)
	}
	var settings struct {
		ReferenceBoxes []string `json:"referenceBoxes"`
		ImageCount     int      `json:"imageCount"`
		FilenameSort   *bool    `json:"filenameSort"`
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if settings.ImageCount > 0 && settings.ImageCount != len(frames) {
		return nil, fmt.Errorf("LRT imageCount %d does not match current sequence length %d", settings.ImageCount, len(frames))
	}
	anchors, err := parseLRTReferenceAnchors(settings.ReferenceBoxes, len(frames))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if len(anchors) > 1 && (settings.FilenameSort == nil || !*settings.FilenameSort) {
		return nil, fmt.Errorf("multiple LRT reference boxes require filenameSort=true; capture-time ordering is not supported")
	}
	return interpolateLRTReferences(anchors, len(frames)), nil
}

func (box lrtReferenceBox) rectangle(bounds image.Rectangle) (image.Rectangle, error) {
	// LRT truncates origin and width/height independently in double arithmetic.
	x, y := int(float64(bounds.Dx())*float64(box[0])), int(float64(bounds.Dy())*float64(box[1]))
	w, h := int(float64(bounds.Dx())*(float64(box[2])-float64(box[0]))), int(float64(bounds.Dy())*(float64(box[3])-float64(box[1])))
	if w <= 0 || h <= 0 {
		return image.Rectangle{}, fmt.Errorf("LRT reference box has no pixels at %dx%d: %v", bounds.Dx(), bounds.Dy(), box)
	}
	return image.Rect(x, y, x+w, y+h).Add(bounds.Min), nil
}
