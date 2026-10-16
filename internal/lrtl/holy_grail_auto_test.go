package lrtl

import (
	"encoding/json"
	"math"
	"os"
	"testing"
)

func TestHolyGrailAutoGridAgainstInstalledController(t *testing.T) {
	data, err := os.ReadFile("testdata/lrt-7.5.5-hg-auto.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Input, Expected, WideExpected                                    []float64
			WideRotate                                                       float64
			Ratings, ISO                                                     []int
			Reduce, Stretch, Rotate, StretchScore, RotateScore, MaxMagnitude float64
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 6 {
		t.Fatal("incomplete grid fixture")
	}
	for n, c := range fixture.Cases {
		if c.MaxMagnitude <= 1 {
			t.Fatal("fixture does not exercise clipping-sensitive candidates")
		}
		cameras := make([]CameraExposure, len(c.Input))
		for i := range cameras {
			cameras[i] = CameraExposure{ExposureTime: 1, ISO: float64(c.ISO[i]), FNumber: 8}
		}
		options := optimizeHolyGrailLRTCurve(c.Input, c.Ratings, cameras, false, c.Reduce)
		wideOptions := HolyGrailCurveOptions{LRTInput: true, Stretch: options.Stretch, Rotate: c.WideRotate, ReduceDeflicker: c.Reduce}
		wide, err := buildHolyGrailLRTCurve(c.Input, c.Ratings, cameras, wideOptions, false, false)
		if err != nil {
			t.Fatal(err)
		}
		manual, err := buildHolyGrailLRTCompensations(c.Input, c.Ratings, cameras, wideOptions, false)
		if err != nil {
			t.Fatal(err)
		}
		if len(c.WideExpected) != len(wide) {
			t.Fatal("missing wide-candidate curve")
		}
		hasOutside := false
		for i, want := range c.WideExpected {
			hasOutside = hasOutside || math.Abs(want) > 1
			if math.IsNaN(wide[i]) || math.IsInf(wide[i], 0) || math.Abs(wide[i]-want) > 2e-8 {
				t.Fatalf("case %d wide candidate frame %d got %g want %g", n, i, wide[i], want)
			}
			if math.Abs(manual[i]) > 1 {
				t.Fatal("manual curve was not clipped")
			}
		}
		if !hasOutside {
			t.Fatal("wide candidate does not distinguish unclipped scoring")
		}
		if options.Stretch != c.Stretch || options.Rotate != c.Rotate {
			t.Fatalf("case %d chosen stretch/rotate=%g/%g native=%g/%g", n, options.Stretch, options.Rotate, c.Stretch, c.Rotate)
		}
		curve, err := buildHolyGrailLRTCurve(c.Input, c.Ratings, cameras, options, false, false)
		if err != nil {
			t.Fatal(err)
		}
		for i, want := range c.Expected {
			if math.IsNaN(curve[i]) || math.IsInf(curve[i], 0) || math.Abs(curve[i]-want) > 2e-8 {
				t.Fatalf("case %d frame %d got %g want %g", n, i, curve[i], want)
			}
		}
		stretchCurve, err := buildHolyGrailLRTCurve(c.Input, c.Ratings, cameras, HolyGrailCurveOptions{LRTInput: true, Stretch: options.Stretch, ReduceDeflicker: c.Reduce}, false, false)
		if err != nil {
			t.Fatal(err)
		}
		stretchSmooth := lrtHolyGrailLowess(stretchCurve, 1.5)
		rotateSmooth := lrtHolyGrailLowess(curve, 1.5)
		stretchScore, rotateScore := 0.0, 0.0
		for i := range curve {
			stretchScore += math.Abs(stretchCurve[i] - stretchSmooth[i])
			rotateScore += math.Abs(rotateSmooth[i])
		}
		if math.Abs(stretchScore-c.StretchScore) > 2e-8 || math.Abs(rotateScore-c.RotateScore) > 2e-8 {
			t.Fatalf("case %d score mismatch", n)
		}
	}
}

func TestHolyGrailAutoGridCLIThroughVideo(t *testing.T) {
	data, err := os.ReadFile("testdata/lrt-7.5.5-hg-auto.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []measuredHolyGrailControllerCase
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 6 {
		t.Fatal("incomplete grid fixture")
	}
	c := fixture.Cases[0]
	for _, value := range c.Expected {
		if math.Abs(value) >= 1 {
			t.Fatal("chosen-grid result requires manual clipping oracle")
		}
	}
	runHolyGrailCLIAndVideo(t, c, true)
}
