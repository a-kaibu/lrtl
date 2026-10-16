package lrtl

import (
	"encoding/json"
	"math"
	"os"
	"reflect"
	"testing"
)

func TestHolyGrailRangeAgainstInstalledLRT755(t *testing.T) {
	data, err := os.ReadFile("testdata/lrt-7.5.5-hg-range.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Input, Expected []float64
			Lower, Upper    float64
			Clip            bool
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 84 {
		t.Fatal("incomplete range component fixture")
	}
	for n, c := range fixture.Cases {
		original := append([]float64(nil), c.Input...)
		got := recenterHolyGrailCurve(c.Input, c.Lower, c.Upper, c.Clip)
		if len(got) != len(c.Expected) {
			t.Fatal("wrong result length")
		}
		for i, want := range c.Expected {
			if math.Float64bits(got[i]) != math.Float64bits(want) {
				t.Fatalf("case %d frame %d got %.17g want %.17g", n, i, got[i], want)
			}
		}
		if len(c.Input) > 0 && !reflect.DeepEqual(original, c.Input) {
			t.Fatal("range helper modified caller input")
		}
	}
}

func TestHolyGrailExtremeConstantClippingPipeline(t *testing.T) {
	// Analytical composition of the directly measured range helper. Its
	// sentinel extrema keep these entirely out-of-range columns from centering
	// to zero. This is not a successful manual-mode controller oracle.
	first := centerAndClipHolyGrailCurve([]float64{-40}, 10)
	second := centerAndClipHolyGrailCurve([]float64{2 * first[0]}, 10)
	third := centerAndClipHolyGrailCurve(second, 1)
	if first[0] != -10 || second[0] != -4.5 || third[0] != -1 {
		t.Fatalf("clip stages: %v %v %v", first, second, third)
	}
	c := measuredHolyGrailControllerCase{Input: []float64{40, 40, 40, 40, 40}, ISO: []int{100, 100, 100, 100, 100}, Ratings: []int{2, 0, 0, 0, 0}, Expected: []float64{-1, -1, -1, -1, -1}}
	runHolyGrailCLIAndVideo(t, c, false)
}
