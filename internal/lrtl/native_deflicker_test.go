package lrtl

import (
	"encoding/json"
	"math"
	"os"
	"reflect"
	"testing"
)

func TestNativeDeflickerPassAgainstInstalled755(t *testing.T) {
	data, err := os.ReadFile("testdata/lrt-native-deflicker-7.5.5.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Input, Previous, Target, Result []float64
		Constant                        bool
		Accuracy, Pass                  int
		Smoothing                       float64
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) != 108 {
		t.Fatal("native Deflicker fixture incomplete")
	}
	for c, tc := range cases {
		before, previous := append([]float64(nil), tc.Input...), append([]float64(nil), tc.Previous...)
		got, err := calculateNativeDeflickerPass(tc.Input, tc.Previous, tc.Smoothing, tc.Constant, tc.Accuracy, tc.Pass)
		if err != nil {
			t.Fatal(err)
		}
		for i := range tc.Input {
			if math.Abs(got.Target[i]-tc.Target[i]) > 2e-8 || math.Abs(got.Correction[i]-tc.Result[i]) > 2e-8 {
				t.Fatalf("case %d frame %d target %.15g / native %.15g, correction %.15g / native %.15g", c, i, got.Target[i], tc.Target[i], got.Correction[i], tc.Result[i])
			}
		}
		if !reflect.DeepEqual(before, tc.Input) || !reflect.DeepEqual(previous, tc.Previous) {
			t.Fatal("native pass mutated input")
		}
	}
}

func TestNativeDeflickerPassValidation(t *testing.T) {
	for _, sample := range []float64{math.NaN(), math.Inf(1), -.1, 1.1} {
		if _, err := calculateNativeDeflickerPass([]float64{sample, .5}, []float64{0, 0}, .5, false, 0, 0); err == nil {
			t.Fatal("invalid native measurement accepted")
		}
	}
	for _, smoothing := range []float64{math.NaN(), math.Inf(1), -.1, 2.1} {
		if _, err := calculateNativeDeflickerPass([]float64{.2, .5}, []float64{0, 0}, smoothing, false, 0, 0); err == nil {
			t.Fatal("invalid smoothing accepted")
		}
	}
	for _, pair := range [][2]int{{-2, 0}, {2, 0}, {0, -1}} {
		if _, err := calculateNativeDeflickerPass([]float64{.2, .5}, []float64{0, 0}, .5, false, pair[0], pair[1]); err == nil {
			t.Fatal("invalid accuracy/pass accepted")
		}
	}
	if _, err := calculateNativeDeflickerPass([]float64{.5}, []float64{0}, .5, false, 0, 0); err == nil {
		t.Fatal("short native input accepted")
	}
	if _, err := calculateNativeDeflickerPass([]float64{.5, .5}, []float64{0}, .5, false, 0, 0); err == nil {
		t.Fatal("mismatched current correction count accepted")
	}
}

func FuzzNativeDeflickerPass(f *testing.F) {
	f.Add([]byte{0, 128, 255, 64, 200, 32, 100, 180}, uint8(1), true)
	f.Add([]byte{128, 128, 128}, uint8(0), false)
	f.Fuzz(func(t *testing.T, data []byte, pass uint8, constant bool) {
		if len(data) < 2 || len(data) > 64 {
			return
		}
		values, current := make([]float64, len(data)), make([]float64, len(data))
		for i, value := range data {
			values[i] = float64(value) / 255
			current[i] = .1
		}
		result, err := calculateNativeDeflickerPass(values, current, .5, constant, 1, int(pass))
		if err != nil {
			t.Fatal(err)
		}
		for i, value := range result.Correction {
			if math.IsNaN(value) || math.IsInf(value, 0) || value < -1 || value > 1 || math.IsNaN(result.Target[i]) || math.IsInf(result.Target[i], 0) {
				t.Fatal("nonfinite or unbounded native result")
			}
		}
	})
}
