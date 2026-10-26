package lrtl

import (
	"image"
	"image/color"
	"math"
	"testing"
)

func localBrushPacket(t *testing.T, collection string, aggregate bool, masks, rangeMask string) *XMPPacket {
	t.Helper()
	if aggregate {
		masks = `<rdf:li rdf:parseType="Resource" crs:What="Mask/Aggregate"><crs:Masks><rdf:Seq>` + masks + `</rdf:Seq></crs:Masks></rdf:li>`
	}
	packet, err := parseXMP([]byte(`<x:xmpmeta xmlns:x="adobe:ns:meta/">
		<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
			<rdf:Description xmlns:crs="http://ns.adobe.com/camera-raw-settings/1.0/">
				<crs:` + collection + `><rdf:Seq><rdf:li rdf:parseType="Resource" crs:What="Correction" crs:LocalExposure2012="1">
					<crs:CorrectionMasks><rdf:Seq>` + masks + `</rdf:Seq></crs:CorrectionMasks>
					` + rangeMask + `
				</rdf:li></rdf:Seq></crs:` + collection + `>
			</rdf:Description>
		</rdf:RDF>
	</x:xmpmeta>`))
	if err != nil {
		t.Fatal(err)
	}
	return packet
}

func localBrushMaskXML(blend, maskValue, radius, flow, centerWeight, dabs string) string {
	return `<rdf:li rdf:parseType="Resource" crs:What="Mask/Paint" crs:MaskBlendMode="` + blend + `" crs:MaskValue="` + maskValue + `" crs:Radius="` + radius + `" crs:Flow="` + flow + `" crs:CenterWeight="` + centerWeight + `"><crs:Dabs><rdf:Seq>` + dabs + `</rdf:Seq></crs:Dabs></rdf:li>`
}

func TestXMPLocalBrushRendersDabsAndFlowForLegacyAndAggregateMasks(t *testing.T) {
	mask := localBrushMaskXML("0", "0.8", "0.25", "0.5", "1", `<rdf:li>d 0.5 0.5</rdf:li><rdf:li>r 0.1</rdf:li><rdf:li>f 0.25</rdf:li><rdf:li>d 0.7 0.5</rdf:li>`)
	rangeMask := `<crs:CorrectionRangeMask rdf:parseType="Resource"><crs:Type>0</crs:Type></crs:CorrectionRangeMask>`
	for _, test := range []struct {
		name       string
		collection string
		aggregate  bool
	}{
		{name: "legacy paint correction", collection: "PaintBasedCorrections"},
		{name: "modern aggregate mask", collection: "MaskGroupBasedCorrections", aggregate: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			packet := localBrushPacket(t, test.collection, test.aggregate, mask, rangeMask)
			corrections, err := readXMPLocalCorrections(packet)
			if err != nil || len(corrections) != 1 {
				t.Fatalf("corrections=%d err=%v", len(corrections), err)
			}
			if len(corrections[0].masks) != 1 || corrections[0].masks[0].brush == nil {
				t.Fatal("paint mask was not parsed")
			}
			dabs := corrections[0].masks[0].brush.dabs
			if len(dabs) != 2 || dabs[0] != (xmpLocalBrushDab{x: .5, y: .5, radius: .25, flow: .5}) || dabs[1] != (xmpLocalBrushDab{x: .7, y: .5, radius: .1, flow: .25}) {
				t.Fatalf("parsed dabs = %#v", dabs)
			}
			for _, point := range []struct {
				x, y int
				want float64
			}{{2, 2, .4}, {3, 2, .5}, {4, 2, 0}} {
				got := corrections[0].maskWeight(point.x, point.y, 5, 5, .1, [3]float64{.2, .2, .2})
				if math.Abs(got-point.want) > 1e-12 {
					t.Errorf("mask weight at (%d,%d) = %.15f, want %.15f", point.x, point.y, got, point.want)
				}
			}

			input := image.NewNRGBA64(image.Rect(0, 0, 5, 5))
			for y := 0; y < 5; y++ {
				for x := 0; x < 5; x++ {
					input.SetNRGBA64(x, y, color.NRGBA64{R: 16384, G: 16384, B: 16384, A: 45000})
				}
			}
			output := applyXMPLocalCorrections(input, corrections)
			for _, point := range []struct {
				x, y   int
				weight float64
			}{{2, 2, .4}, {3, 2, .5}} {
				got := color.NRGBA64Model.Convert(output.At(point.x, point.y)).(color.NRGBA64)
				want := expectedLocalEVPixel(16384, 45000, point.weight)
				if got != want {
					t.Errorf("rendered pixel (%d,%d) = %#v, want %#v", point.x, point.y, got, want)
				}
			}
			outside := color.NRGBA64Model.Convert(output.At(4, 2)).(color.NRGBA64)
			if outside != (color.NRGBA64{R: 16384, G: 16384, B: 16384, A: 45000}) {
				t.Fatalf("outside brush = %#v", outside)
			}
		})
	}
}

func TestXMPLocalBrushSupportsSubtractBlend(t *testing.T) {
	dab := `<rdf:li>d 0.5 0.5</rdf:li>`
	masks := localBrushMaskXML("0", "1", "0.25", "0.5", "1", dab) + localBrushMaskXML("2", "1", "0.25", "0.5", "1", dab)
	packet := localBrushPacket(t, "MaskGroupBasedCorrections", true, masks, "")
	corrections, err := readXMPLocalCorrections(packet)
	if err != nil || len(corrections) != 1 {
		t.Fatalf("corrections=%d err=%v", len(corrections), err)
	}
	if got := corrections[0].maskWeight(2, 2, 5, 5, .1, [3]float64{.2, .2, .2}); math.Abs(got-.25) > 1e-12 {
		t.Fatalf("add then subtract weight = %.15f, want 0.25", got)
	}
}

func TestXMPLocalBrushRasterizedWeightsMatchDirectEvaluation(t *testing.T) {
	first := &xmpLocalBrush{
		centerWeight: .35,
		dabs: []xmpLocalBrushDab{
			{x: .02, y: .1, radius: .2, flow: .75},
			{x: .55, y: .48, radius: .11, flow: .4},
			{x: .98, y: .9, radius: .18, flow: 1},
		},
	}
	second := &xmpLocalBrush{
		centerWeight: 1,
		dabs:         []xmpLocalBrushDab{{x: .52, y: .5, radius: .3, flow: .6}},
	}
	correction := xmpLocalCorrection{
		exposure: 1,
		masks: []xmpLocalMask{
			{kind: localMaskBrush, blendMode: localMaskBlendAdd, maskValue: .8, brush: first},
			{kind: localMaskBrush, blendMode: localMaskBlendSubtract, maskValue: .5, flipped: true, brush: second},
		},
	}
	img := image.NewNRGBA64(image.Rect(5, 8, 22, 19))
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			img.SetNRGBA64(x, y, color.NRGBA64{R: 32768, G: 22000, B: 11000, A: 65535})
		}
	}
	raster := rasterizeXMPLocalCorrectionMask(img, correction)
	if len(raster) != img.Bounds().Dx()*img.Bounds().Dy() {
		t.Fatalf("raster length = %d, want %d", len(raster), img.Bounds().Dx()*img.Bounds().Dy())
	}
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			pixel := img.At(x+img.Bounds().Min.X, y+img.Bounds().Min.Y).(color.NRGBA64)
			rgb := [3]float64{float64(pixel.R) / 65535, float64(pixel.G) / 65535, float64(pixel.B) / 65535}
			luminance := .2126*srgb16ToLinear[pixel.R] + .7152*srgb16ToLinear[pixel.G] + .0722*srgb16ToLinear[pixel.B]
			want := correction.maskWeight(x, y, img.Bounds().Dx(), img.Bounds().Dy(), luminance, rgb)
			got := float64(raster[y*img.Bounds().Dx()+x])
			if math.Abs(got-want) > 1e-7 {
				t.Errorf("raster weight at (%d,%d) = %.9f, direct = %.12f", x, y, got, want)
			}
		}
	}
}

func TestXMPLocalBrushSkipsUnknownCommandsAndRejectsMalformedDabs(t *testing.T) {
	unknown := localBrushMaskXML("0", "1", "0.25", "1", "1", `<rdf:li>z 0.5 0.5</rdf:li><rdf:li>d 0.5 0.5</rdf:li>`)
	packet := localBrushPacket(t, "PaintBasedCorrections", false, unknown, "")
	corrections, err := readXMPLocalCorrections(packet)
	if err != nil || len(corrections) != 0 {
		t.Fatalf("unknown command partially applied: corrections=%d err=%v", len(corrections), err)
	}

	malformed := localBrushMaskXML("0", "1", "0.25", "1", "1", `<rdf:li>d 0.5</rdf:li>`)
	packet = localBrushPacket(t, "PaintBasedCorrections", false, malformed, "")
	if _, err := readXMPLocalCorrections(packet); err == nil {
		t.Fatal("malformed dab command was accepted")
	}
}

func expectedLocalEVPixel(sample, alpha uint16, ev float64) color.NRGBA64 {
	srgb := float64(sample) / 65535
	linear := srgb / 12.92
	if srgb > .04045 {
		linear = math.Pow((srgb+.055)/1.055, 2.4)
	}
	linear *= math.Exp2(ev)
	srgb = 12.92 * linear
	if linear > .0031308 {
		srgb = 1.055*math.Pow(linear, 1/2.4) - .055
	}
	encoded := uint16(math.Round(math.Max(0, math.Min(1, srgb)) * 65535))
	return color.NRGBA64{R: encoded, G: encoded, B: encoded, A: alpha}
}
