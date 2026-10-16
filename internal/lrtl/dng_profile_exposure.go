package lrtl

import (
	"fmt"
	"math"
)

// Profile offsets are additive EV, not a second pixel gain after the curve.
// Their sum goes through the same exposure stage as BaselineExposure alone.
func readDNGBaselineExposure(fields *dngFields) (float64, error) {
	baseline := fields.scalar(50730, 0)
	offset := 0.0
	if entry, found := fields.entries[51109]; found {
		if entry.typeID != 10 || entry.count != 1 {
			return 0, fmt.Errorf("DNG BaselineExposureOffset requires one SRATIONAL value")
		}
		offset = fields.scalar(51109, 0)
	}
	if fields.err != nil {
		return 0, fields.err
	}
	total := baseline + offset
	if math.Abs(baseline) > 16 || math.Abs(offset) > 16 || math.Abs(total) > 16 {
		return 0, fmt.Errorf("Linear DNG baseline exposure/offset is outside supported range (-16..16 EV)")
	}
	return total, nil
}
