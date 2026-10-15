package lrtl

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"testing"
)

// These numbers were measured by calling the installed LRTimelapse 7.5.5
// smoother on synthetic inputs. The fixture contains observations, not code.
func TestLRTHolyGrailSmoothingAgainstInstalled755(t *testing.T) {
	data, err := os.ReadFile("testdata/lrt-7.5.5-smoothing.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Length    int
		Smoothing float64
		Indices   []int
		Expected  []float64
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) != 53 {
		t.Fatal("incomplete oracle fixture")
	}
	for _, c := range cases {
		t.Run(fmt.Sprintf("n%d/span%g", c.Length, c.Smoothing), func(t *testing.T) {
			values := make([]float64, c.Length)
			for i := range values {
				values[i] = float64(i*37%23) - 11 + float64(i%7)*.125
			}
			got := lrtHolyGrailLowess(values, c.Smoothing)
			if len(got) != c.Length || len(c.Indices) != len(c.Expected) {
				t.Fatal("invalid result or fixture length")
			}
			for j, index := range c.Indices {
				if index < 0 || index >= len(got) {
					t.Fatal("invalid oracle index")
				}
				if math.IsNaN(got[index]) || math.IsInf(got[index], 0) || math.Abs(got[index]-c.Expected[j]) > 2e-8 {
					t.Errorf("frame %d: got %.15g, LRT %.15g", index, got[index], c.Expected[j])
				}
			}
			for i := range values {
				want := float64(i*37%23) - 11 + float64(i%7)*.125
				if values[i] != want {
					t.Fatal("input modified")
				}
			}
		})
	}
}
