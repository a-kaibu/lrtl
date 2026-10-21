package fdlibm

import (
	"encoding/json"
	"math"
	"os"
	"strconv"
	"testing"
)

func TestAtan2JavaBits(t *testing.T) {
	data, err := os.ReadFile("testdata/java-atan2-25.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows [][3]string
	if err := json.Unmarshal(data, &rows); err != nil || len(rows) != 2304 {
		t.Fatalf("expected 2304 Java atan2 observations: %v", err)
	}
	for i, row := range rows {
		var bits [3]uint64
		for j, s := range row {
			v, err := strconv.ParseUint(s, 16, 64)
			if err != nil {
				t.Fatal(err)
			}
			bits[j] = v
		}
		y, x := math.Float64frombits(bits[0]), math.Float64frombits(bits[1])
		got := Atan2(y, x)
		if math.IsNaN(math.Float64frombits(bits[2])) {
			if !math.IsNaN(got) {
				t.Fatalf("case%d expected NaN", i)
			}
			continue
		}
		if math.Float64bits(got) != bits[2] {
			t.Fatalf("case%d atan2(%x,%x)=%x expected%x", i, bits[0], bits[1], math.Float64bits(got), bits[2])
		}
	}
}

func FuzzAtan2(f *testing.F) {
	f.Add(uint64(0x3fe3609f33ce6bdf), uint64(0x3ff0000000000000))
	f.Add(uint64(0x8000000000000000), uint64(0xbff0000000000000))
	f.Fuzz(func(t *testing.T, yBits, xBits uint64) {
		y, x := math.Float64frombits(yBits), math.Float64frombits(xBits)
		got := Atan2(y, x)
		reference := math.Atan2(y, x)
		if math.IsNaN(reference) {
			if !math.IsNaN(got) {
				t.Fatal("expected NaN")
			}
			return
		}
		if math.IsNaN(got) || math.IsInf(got, 0) || got < -math.Pi || got > math.Pi {
			t.Fatal("invalid atan2 range")
		}
		ulp := math.Abs(math.Nextafter(reference, math.Inf(1)) - reference)
		if math.Abs(got-reference) > 3*ulp {
			t.Fatalf("arctangent diverged: %.17g/%.17g", got, reference)
		}
		if y != 0 && !math.IsNaN(y) && !math.IsNaN(x) {
			opposite := Atan2(-y, x)
			if math.Float64bits(opposite) != math.Float64bits(-got) {
				t.Fatal("lost y sign symmetry")
			}
		}
	})
}
