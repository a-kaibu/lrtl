package lrtl

import (
	"fmt"
	"image"
	"math"
)

// DNG black subtraction is spatial, but each sample plane has one common
// normalization denominator. Pattern and delta origins are relative to ActiveArea.
type dngBlackModel struct {
	active                      image.Rectangle
	rows, cols, channels        int
	black, horizontal, vertical []float64
	denominators                [3]float64
}

func newDNGBlackModel(fields *dngFields, active image.Rectangle, channels, depth int) (*dngBlackModel, error) {
	return newDNGBlackModelForTrimmedArea(fields, active, active, 0, 0, channels, depth)
}

func newDNGBlackModelForTrimmedArea(fields *dngFields, active, originalActive image.Rectangle, deltaOffsetX, deltaOffsetY, channels, depth int) (*dngBlackModel, error) {
	if active.Empty() || originalActive.Empty() || deltaOffsetX < 0 || deltaOffsetY < 0 || deltaOffsetX+active.Dx() > originalActive.Dx() || deltaOffsetY+active.Dy() > originalActive.Dy() {
		return nil, fmt.Errorf("invalid Linear DNG ActiveArea after Stage 1 trimming")
	}
	model := &dngBlackModel{active: active, rows: 1, cols: 1, channels: channels}
	if repeat := fields.values(50713); len(repeat) != 0 {
		if len(repeat) != 2 || repeat[0] < 1 || repeat[1] < 1 || repeat[0] > 32768 || repeat[1] > 32768 || repeat[0] != math.Trunc(repeat[0]) || repeat[1] != math.Trunc(repeat[1]) {
			return nil, fmt.Errorf("invalid Linear DNG BlackLevelRepeatDim")
		}
		model.rows, model.cols = int(repeat[0]), int(repeat[1])
	}
	count := uint64(model.rows) * uint64(model.cols) * uint64(channels)
	if count > 1<<20 {
		return nil, fmt.Errorf("Linear DNG black pattern exceeds development limits")
	}
	model.black = fields.values(50714)
	if len(model.black) == 0 {
		model.black = []float64{0}
	} else if uint64(len(model.black)) != count && !(model.rows == 1 && model.cols == 1 && len(model.black) == 1) {
		return nil, fmt.Errorf("invalid Linear DNG black level layout")
	}
	white := fields.values(50717)
	if len(white) == 0 {
		white = []float64{float64((uint64(1) << uint(depth)) - 1)}
	}
	if len(white) != 1 && len(white) != channels {
		return nil, fmt.Errorf("invalid Linear DNG white level layout")
	}
	model.horizontal, model.vertical = fields.values(50715), fields.values(50716)
	if fields.err != nil {
		return nil, fields.err
	}
	if (len(model.horizontal) != 0 && len(model.horizontal) != originalActive.Dx()) || (len(model.vertical) != 0 && len(model.vertical) != originalActive.Dy()) {
		return nil, fmt.Errorf("invalid Linear DNG black delta dimensions for ActiveArea")
	}
	if len(model.horizontal) != 0 {
		model.horizontal = model.horizontal[deltaOffsetX : deltaOffsetX+active.Dx()]
	}
	if len(model.vertical) != 0 {
		model.vertical = model.vertical[deltaOffsetY : deltaOffsetY+active.Dy()]
	}
	// Maxima are grouped by pattern phase, so unrelated phases' maxima are
	// never combined. As in Adobe's SDK, unused pattern phases retain a zero
	// delta when the repeat pattern extends beyond the active dimensions.
	horizontalMax, verticalMax := make([]float64, model.cols), make([]float64, model.rows)
	for x := 0; x < active.Dx(); x++ {
		delta := 0.0
		if len(model.horizontal) > 0 {
			delta = model.horizontal[x]
		}
		if x < model.cols {
			horizontalMax[x] = delta
		} else {
			horizontalMax[x%model.cols] = max(horizontalMax[x%model.cols], delta)
		}
	}
	for y := 0; y < active.Dy(); y++ {
		delta := 0.0
		if len(model.vertical) > 0 {
			delta = model.vertical[y]
		}
		if y < model.rows {
			verticalMax[y] = delta
		} else {
			verticalMax[y%model.rows] = max(verticalMax[y%model.rows], delta)
		}
	}
	for channel := 0; channel < channels; channel++ {
		maximum := math.Inf(-1)
		for row, v := range verticalMax {
			for col, h := range horizontalMax {
				maximum = max(maximum, model.pattern(row, col, channel)+h+v)
			}
		}
		model.denominators[channel] = white[channel%len(white)] - maximum
		if model.denominators[channel] <= 0 || math.IsInf(model.denominators[channel], 0) || math.IsNaN(model.denominators[channel]) {
			return nil, fmt.Errorf("Linear DNG white level must exceed maximum black level for plane %d", channel)
		}
	}
	return model, nil
}

func (model *dngBlackModel) pattern(row, col, channel int) float64 {
	if len(model.black) == 1 {
		return model.black[0]
	}
	return model.black[(row*model.cols+col)*model.channels+channel]
}

func (model *dngBlackModel) normalize(raw float64, x, y, channel int) float64 {
	x, y = x-model.active.Min.X, y-model.active.Min.Y
	black := model.pattern(y%model.rows, x%model.cols, channel)
	if len(model.horizontal) > 0 {
		black += model.horizontal[x]
	}
	if len(model.vertical) > 0 {
		black += model.vertical[y]
	}
	return min(1, max(0, (raw-black)/model.denominators[channel]))
}
