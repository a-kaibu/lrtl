package lrtl

import "testing"

func TestLRTJPEGYCbCrToRGBReferenceVectors(t *testing.T) {
	tests := []struct {
		name                string
		y, cb, cr           byte
		wantR, wantG, wantB byte
	}{
		{name: "black", y: 0, cb: 128, cr: 128, wantR: 0, wantG: 0, wantB: 0},
		{name: "neutral gray", y: 127, cb: 128, cr: 128, wantR: 127, wantG: 127, wantB: 127},
		{name: "white", y: 255, cb: 128, cr: 128, wantR: 255, wantG: 255, wantB: 255},
		{name: "blue clipping", y: 128, cb: 255, cr: 128, wantR: 128, wantG: 84, wantB: 255},
		{name: "red clipping", y: 128, cb: 128, cr: 255, wantR: 255, wantG: 37, wantB: 128},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, g, b := lrtJPEGYCbCrToRGB(tt.y, tt.cb, tt.cr)
			if r != tt.wantR || g != tt.wantG || b != tt.wantB {
				t.Fatalf("YCbCr(%d,%d,%d) = RGB(%d,%d,%d), want (%d,%d,%d)", tt.y, tt.cb, tt.cr, r, g, b, tt.wantR, tt.wantG, tt.wantB)
			}
		})
	}
}
