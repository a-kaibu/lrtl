package lrtl

import (
	"image"
	"image/color"
	"math"
	"testing"
)

func localNoisePacket(t *testing.T, value string) *XMPPacket {
	return localAdjustmentPacket(t, "LocalLuminanceNoise", value)
}

func localAdjustmentPacket(t *testing.T, property, value string) *XMPPacket {
	t.Helper()
	packet, err := parseXMP([]byte(`<x:xmpmeta xmlns:x="adobe:ns:meta/">
		<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
			<rdf:Description xmlns:crs="http://ns.adobe.com/camera-raw-settings/1.0/">
				<crs:MaskGroupBasedCorrections><rdf:Seq><rdf:li>
					<rdf:Description crs:What="Correction" crs:` + property + `="` + value + `">
						<crs:CorrectionMasks><rdf:Seq><rdf:li>
							<rdf:Description crs:What="Mask/Gradient" crs:ZeroX="1" crs:ZeroY="0.5" crs:FullX="0" crs:FullY="0.5"/>
						</rdf:li></rdf:Seq></crs:CorrectionMasks>
					</rdf:Description>
				</rdf:li></rdf:Seq></crs:MaskGroupBasedCorrections>
			</rdf:Description>
		</rdf:RDF>
	</x:xmpmeta>`))
	if err != nil {
		t.Fatal(err)
	}
	return packet
}

func TestXMPLocalLuminanceNoiseParsesSignedSliderAndRejectsOutOfRange(t *testing.T) {
	for _, test := range []struct {
		value string
		want  float64
	}{
		{value: "0.5", want: 50},
		{value: "-0.25", want: -25},
	} {
		packet := localNoisePacket(t, test.value)
		corrections, err := readXMPLocalCorrections(packet)
		if err != nil || len(corrections) != 1 {
			t.Fatalf("value %s: corrections=%d err=%v", test.value, len(corrections), err)
		}
		if corrections[0].luminanceNoise != test.want || !corrections[0].active() {
			t.Errorf("value %s: luminanceNoise=%g active=%v, want %g and active", test.value, corrections[0].luminanceNoise, corrections[0].active(), test.want)
		}
	}

	if _, err := readXMPLocalCorrections(localNoisePacket(t, "1.01")); err == nil {
		t.Fatal("LocalLuminanceNoise outside normalized [-1, 1] range was accepted")
	}
	if corrections, err := readXMPLocalCorrections(localNoisePacket(t, "0")); err != nil || len(corrections) != 0 {
		t.Fatalf("neutral correction count=%d err=%v, want no correction", len(corrections), err)
	}
}

func TestXMPLocalLuminanceNoiseBlendsOnlyInsideMask(t *testing.T) {
	input := image.NewNRGBA64(image.Rect(7, 9, 12, 12))
	for y := input.Bounds().Min.Y; y < input.Bounds().Max.Y; y++ {
		for x := input.Bounds().Min.X; x < input.Bounds().Max.X; x++ {
			value := uint16(30000)
			if (x+y)%2 == 0 {
				value += 1200
			} else {
				value -= 1200
			}
			input.SetNRGBA64(x, y, color.NRGBA64{R: value, G: value, B: value, A: uint16(40000 + (x-input.Bounds().Min.X)*1000)})
		}
	}

	correction := xmpLocalCorrection{
		amount:         1,
		luminanceNoise: 50,
		masks: []xmpLocalMask{{
			kind: localMaskGradient, maskValue: 1, zeroX: 0.5, zeroY: 0.5, fullX: 0, fullY: 0.5,
		}},
	}
	actual := applyXMPLocalCorrections(input, []xmpLocalCorrection{correction})
	filtered := xmpApplyNoiseReduction(input, 100, 50, 0, 0, 50, 50)
	bounds := input.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			original := input.NRGBA64At(x, y)
			denoised := filtered.NRGBA64At(x, y)
			normalizedX := (float64(x-bounds.Min.X) + 0.5) / float64(bounds.Dx())
			maskWeight := math.Max(0, math.Min(1, 1-normalizedX/0.5))
			blend := 0.5 * maskWeight
			want := blendLocalNoisePixel(original, denoised, blend)
			got := color.NRGBA64Model.Convert(actual.At(x, y)).(color.NRGBA64)
			if got != want {
				t.Errorf("pixel (%d,%d) = %#v, want mask-weighted noise reduction %#v", x, y, got, want)
			}
			originalLuma, originalU, originalV := xmpLinearLumaChroma(original)
			filteredLuma, _, _ := xmpLinearLumaChroma(denoised)
			gotLuma, gotU, gotV := xmpLinearLumaChroma(got)
			wantLuma := originalLuma + (filteredLuma-originalLuma)*blend
			if math.Abs(gotLuma-wantLuma) > 2e-5 || math.Abs(gotU-originalU) > 2e-5 || math.Abs(gotV-originalV) > 2e-5 {
				t.Errorf("pixel (%d,%d) luma/chroma = (%g,%g,%g), want local luma blend with preserved chroma (%g,%g,%g)", x, y, gotLuma, gotU, gotV, wantLuma, originalU, originalV)
			}
			if x == bounds.Min.X && y == bounds.Min.Y {
				if denoised.R == original.R {
					t.Fatal("noise-reduction fixture did not contain a reducible luminance variation")
				}
				if got.R == original.R {
					t.Fatal("positive LocalLuminanceNoise had no effect inside the mask")
				}
			}
			if maskWeight == 0 && got != original {
				t.Errorf("pixel (%d,%d) outside the mask = %#v, want unchanged %#v", x, y, got, original)
			}
		}
	}
}

func TestXMPLocalLuminanceNoiseNegativeAmplifiesLocalDetail(t *testing.T) {
	input := image.NewNRGBA64(image.Rect(0, 0, 5, 3))
	for y := 0; y < 3; y++ {
		for x := 0; x < 5; x++ {
			value := uint16(30000)
			if (x+y)%2 == 0 {
				value += 1200
			} else {
				value -= 1200
			}
			input.SetNRGBA64(x, y, color.NRGBA64{R: value, G: value, B: value, A: 65535})
		}
	}
	correction := xmpLocalCorrection{
		amount:         1,
		luminanceNoise: -50,
		masks: []xmpLocalMask{{
			kind: localMaskLuminanceRange, maskValue: 1,
			luminanceRange: &xmpLocalLuminanceRange{
				lowOuter: 0, lowInner: 0, highInner: 1, highOuter: 1,
			},
		}},
	}
	filtered := xmpApplyNoiseReduction(input, 100, 50, 0, 0, 50, 50)
	actual := applyXMPLocalCorrections(input, []xmpLocalCorrection{correction})
	original := input.NRGBA64At(2, 1)
	denoised := filtered.NRGBA64At(2, 1)
	got := color.NRGBA64Model.Convert(actual.At(2, 1)).(color.NRGBA64)
	want := blendLocalNoisePixel(original, denoised, -0.5)
	if got != want {
		t.Fatalf("negative local noise pixel = %#v, want inverse-filter blend %#v", got, want)
	}
	if got.R == original.R {
		t.Fatal("negative LocalLuminanceNoise had no effect")
	}
}
