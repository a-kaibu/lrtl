package lrtl

import (
	"bytes"
	"image"
	"image/color"
	"math"
	"testing"
)

func TestFrameStateRelativeWBPrecisionAndValidation(t *testing.T) {
	for _, values := range [][2]float64{{-100, -100}, {-25, 30}, {0, 0}, {25, 20}, {100, 100}} {
		p := EmptyXMPPacket()
		for name, value := range map[string]string{"lrt:Quality": "FLAT", "crs:Temperature": "777", "crs:Tint": "NaN", "crs:IncrementalTemperature": formatNumber(values[0]), "crs:IncrementalTint": formatNumber(values[1])} {
			if err := p.SetScalar(name, value); err != nil {
				t.Fatal(err)
			}
		}
		adjustments, err := readXMPBasicPacket(p)
		if err != nil {
			t.Fatal(err)
		}
		want := approximateWhiteBalanceGains(6500+40*values[0], values[1])
		if values != [2]float64{} && adjustments.whiteBalance != want {
			t.Fatal("incorrect relative gain mapping", values)
		}
		if values == [2]float64{} && adjustments.hasWhiteBalance {
			t.Fatal("neutral relative WB activated transform")
		}
		img := image.NewNRGBA64(image.Rect(0, 0, 2, 1))
		img.SetNRGBA64(0, 0, color.NRGBA64{R: 20001, G: 30001, B: 40001, A: 32001})
		img.SetNRGBA64(1, 0, color.NRGBA64{R: 20002, G: 30002, B: 40002, A: 65535})
		got := applyXMPBasic(img, adjustments)
		if got.NRGBA64At(0, 0).A != 32001 || got.NRGBA64At(1, 0).A != 65535 {
			t.Fatal("alpha changed")
		}
		if values == [2]float64{} && !bytes.Equal(img.Pix, got.Pix) {
			t.Fatal("neutral WB lost 16-bit precision")
		}
		if values != [2]float64{} && got.NRGBA64At(0, 0) == got.NRGBA64At(1, 0) {
			t.Fatal("WB lost fine input steps")
		}
	}
	for _, name := range []string{"IncrementalTemperature", "IncrementalTint"} {
		for _, value := range []string{"101", "-101", "NaN", "Infinity"} {
			p := EmptyXMPPacket()
			if err := p.SetScalar("lrt:Quality", "FLAT"); err != nil {
				t.Fatal(err)
			}
			if err := p.SetScalar("crs:"+name, value); err != nil {
				t.Fatal(err)
			}
			if _, err := readXMPBasicPacket(p); err == nil {
				t.Fatal("accepted invalid active relative WB", name, value)
			}
		}
	}
}

func FuzzRelativeWBGains(f *testing.F) {
	f.Add(25.0, 20.0)
	f.Add(-100.0, -100.0)
	f.Add(0.0, 0.0)
	f.Fuzz(func(t *testing.T, temperature, tint float64) {
		if math.IsNaN(temperature) || math.IsNaN(tint) || temperature < -100 || temperature > 100 || tint < -100 || tint > 100 {
			return
		}
		gains := approximateWhiteBalanceGains(6500+40*temperature, tint)
		for _, gain := range gains {
			if math.IsNaN(gain) || math.IsInf(gain, 0) || gain <= 0 {
				t.Fatal("invalid relative WB gain", gains)
			}
		}
		luma := .2126*gains[0] + .7152*gains[1] + .0722*gains[2]
		if math.Abs(luma-1) > 1e-12 {
			t.Fatal("relative WB changed neutral luminance", luma)
		}
	})
}
