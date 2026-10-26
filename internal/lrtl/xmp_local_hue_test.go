package lrtl

import (
	"image"
	"image/color"
	"testing"
)

func localHuePacket(t *testing.T, value string) *XMPPacket {
	t.Helper()
	packet, err := parseXMP([]byte(`<x:xmpmeta xmlns:x="adobe:ns:meta/">
		<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
			<rdf:Description xmlns:crs="http://ns.adobe.com/camera-raw-settings/1.0/">
				<crs:MaskGroupBasedCorrections><rdf:Seq><rdf:li>
					<rdf:Description crs:What="Correction" crs:LocalHue="` + value + `">
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

func TestXMPLocalHueParsesAndRendersWeightedPositiveAndNegativeShifts(t *testing.T) {
	tests := []struct {
		name      string
		value     string
		wantLeft  color.NRGBA64
		wantRight color.NRGBA64
	}{
		{
			name:      "positive with hue wrap",
			value:     "0.5",
			wantLeft:  color.NRGBA64{R: 52428, G: 44236, B: 13107, A: 40000},
			wantRight: color.NRGBA64{R: 52428, G: 14745, B: 13107, A: 40000},
		},
		{
			name:      "negative with hue wrap",
			value:     "-0.5",
			wantLeft:  color.NRGBA64{R: 34406, G: 13107, B: 52428, A: 40000},
			wantRight: color.NRGBA64{R: 52428, G: 13107, B: 40959, A: 40000},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			packet := localHuePacket(t, test.value)
			corrections, err := readXMPLocalCorrections(packet)
			if err != nil || len(corrections) != 1 {
				t.Fatalf("corrections=%d err=%v", len(corrections), err)
			}
			wantHue := 50.0
			if test.value[0] == '-' {
				wantHue = -50
			}
			if corrections[0].hue != wantHue || !corrections[0].active() {
				t.Fatalf("hue=%g active=%v, want hue=%g and active", corrections[0].hue, corrections[0].active(), wantHue)
			}

			input := image.NewNRGBA64(image.Rect(0, 0, 2, 1))
			input.SetNRGBA64(0, 0, color.NRGBA64{R: 52428, G: 13107, B: 26214, A: 40000})
			input.SetNRGBA64(1, 0, color.NRGBA64{R: 52428, G: 13107, B: 26214, A: 40000})
			output := applyXMPLocalCorrections(input, corrections)
			for x, want := range []color.NRGBA64{test.wantLeft, test.wantRight} {
				got := color.NRGBA64Model.Convert(output.At(x, 0)).(color.NRGBA64)
				if got != want {
					t.Errorf("pixel %d = %#v, want %#v", x, got, want)
				}
			}
		})
	}
}

func TestXMPLocalHueRejectsOutOfRangeAndKeepsNeutralNoOp(t *testing.T) {
	packet := localHuePacket(t, "1.01")
	if _, err := readXMPLocalCorrections(packet); err == nil {
		t.Fatal("LocalHue outside normalized [-1, 1] range was accepted")
	}

	packet = localHuePacket(t, "0")
	corrections, err := readXMPLocalCorrections(packet)
	if err != nil || len(corrections) != 0 {
		t.Fatalf("neutral correction count=%d err=%v", len(corrections), err)
	}
	input := image.NewNRGBA64(image.Rect(0, 0, 1, 1))
	input.SetNRGBA64(0, 0, color.NRGBA64{R: 40000, G: 20000, B: 10000, A: 30000})
	if output := applyXMPLocalCorrections(input, corrections); output != input {
		t.Fatal("neutral LocalHue should not allocate or alter the image")
	}
}
