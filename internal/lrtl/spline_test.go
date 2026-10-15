package lrtl

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"testing"
)

func TestTransitionAgainstInstalledLRT755(t *testing.T) {
	data, err := os.ReadFile("testdata/lrt-7.5.5-transition.json")
	if err != nil {
		t.Fatal(err)
	}
	var oracle struct {
		Rounding        []struct{ Input, Expected float64 }
		DecimalRounding []struct{ Input, Expected float64 }
		Cases           []struct {
			Length, Smoothness int
			Positions          []int
			Values             []float32
			Expected           []*float64
		}
	}
	if err := json.Unmarshal(data, &oracle); err != nil {
		t.Fatal(err)
	}
	if len(oracle.Cases) != 27 || len(oracle.Rounding) != 11 {
		t.Fatal("incomplete oracle")
	}
	for _, r := range oracle.Rounding {
		got := adjustTransitionValue(r.Input, numericTransitionSpec{min: -1000, max: 1000, integer: true})
		if got != r.Expected {
			t.Errorf("round(%g)=%g, LRT=%g", r.Input, got, r.Expected)
		}
	}
	if len(oracle.DecimalRounding) != 13 {
		t.Fatal("incomplete decimal rounding oracle")
	}
	for _, r := range oracle.DecimalRounding {
		if got := roundLRTFourDecimals(r.Input); got != r.Expected {
			t.Errorf("round4(%.17g) = %.17g, LRT %.17g", r.Input, got, r.Expected)
		}
	}
	for i, c := range oracle.Cases {
		t.Run(fmt.Sprintf("case%d/n%d/smooth%d", i, c.Length, c.Smoothness), func(t *testing.T) {
			points := make([]lrtSplinePoint, len(c.Positions))
			flags := make([]bool, c.Length)
			for j, pos := range c.Positions {
				points[j] = lrtSplinePoint{float32(pos), c.Values[j]}
				flags[pos] = true
			}
			spline, err := newLRTTransitionSpline(points, c.Smoothness)
			if err != nil {
				t.Fatal(err)
			}
			values, filled := spline.sampleFrames(c.Length, flags)
			for frame, want := range c.Expected {
				if flags[frame] {
					continue
				}
				if want == nil {
					if filled[frame] {
						t.Fatalf("frame %d unexpectedly assigned", frame)
					}
					continue
				}
				if !filled[frame] || math.Abs(values[frame]-*want) > 1e-6 {
					t.Errorf("frame %d = %.15g (filled=%t), LRT %.15g", frame, values[frame], filled[frame], *want)
				}
			}
		})
	}
}
