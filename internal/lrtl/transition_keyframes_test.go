package lrtl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestTransitionKeyframeGroups(t *testing.T) {
	for _, c := range []struct {
		name                   string
		ratings                []int
		normal, crop, combined []int
	}{
		{"mixed", []int{4, 2, 5, 3, 4, 5, 1}, []int{0, 4}, []int{2, 5}, []int{0, 2, 4, 5}},
		{"no-crop", []int{1, 4, 0, 4}, []int{1, 3}, []int{1, 3}, []int{1, 3}},
		{"crop-only", []int{0, 5, 0, 5}, []int{0}, []int{1, 3}, []int{0, 1, 3}},
		{"no-marks", []int{2, 0, 3}, []int{0}, []int{0}, []int{0}},
		{"empty", nil, nil, nil, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			packets := make([]*XMPPacket, len(c.ratings))
			for i, rating := range c.ratings {
				packets[i] = EmptyXMPPacket()
				packets[i].SetScalar("xmp:Rating", fmt.Sprint(rating))
			}
			normal, crop, combined := transitionKeyframeGroups(packets)
			if !reflect.DeepEqual(normal, c.normal) || !reflect.DeepEqual(crop, c.crop) || !reflect.DeepEqual(combined, c.combined) {
				t.Fatalf("groups=%v %v %v", normal, crop, combined)
			}
		})
	}
	// Stale lrtl marker text cannot promote a 2-star HG frame to an LRT anchor.
	packet := EmptyXMPPacket()
	packet.SetScalar("ol:KeyframeKind", "default")
	packet.SetScalar("xmp:Rating", "2")
	normal, _, _ := transitionKeyframeGroups([]*XMPPacket{EmptyXMPPacket(), packet})
	if !reflect.DeepEqual(normal, []int{0}) {
		t.Fatalf("stale marker selected: %v", normal)
	}
}

func interpolationPackets(t *testing.T, frames []Frame, smoothness int) (*InterpolationPlan, []*XMPPacket) {
	t.Helper()
	plan, err := buildPlanWithSmoothness(frames, smoothness)
	if err != nil {
		t.Fatal(err)
	}
	outputs, err := applyPlan(plan)
	if err != nil {
		t.Fatal(err)
	}
	packets := make([]*XMPPacket, len(frames))
	byPath := map[string]*XMPPacket{}
	for _, output := range outputs {
		byPath[output.Path] = output.Packet
	}
	for i, frame := range frames {
		packets[i] = byPath[frame.OutputSidecar()]
		if packets[i] == nil {
			packets[i], err = PacketForFrame(frame)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	return plan, packets
}

func assertTransitionScalar(t *testing.T, packet *XMPPacket, name string, want float64) {
	t.Helper()
	text, found, err := packet.GetScalar("crs:" + name)
	value, valid := scalarNumber(text)
	if err != nil || !found || !valid || math.Abs(value-want) > 1e-6 {
		t.Fatalf("%s=%s want %g found=%t err=%v", name, text, want, found, err)
	}
}

func TestMixedRatingsSeparateDevelopCropAndLocalTransitions(t *testing.T) {
	dir := t.TempDir()
	frames := makeFrames(t, dir, 9)
	originals := map[string][]byte{}
	for i, frame := range frames {
		rating := []int{4, 2, 5, 0, 0, 0, 5, 3, 4}[i]
		exposure, local, left := 4.8, -.8, .8
		if i == 0 {
			exposure, local = 0, 0
		}
		if i == 8 {
			exposure, local = 4, .8
		}
		if i == 2 {
			left = .1
		}
		if i == 6 {
			left = .3
		}
		xml := fmt.Sprintf(`<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"><rdf:Description xmlns:crs="http://ns.adobe.com/camera-raw-settings/1.0/" xmlns:xmp="http://ns.adobe.com/xap/1.0/" xmp:Rating="%d" crs:Exposure2012="%g" crs:CropLeft="%g" crs:CropTop="0" crs:CropRight="1" crs:CropBottom="1" crs:CropAngle="0"><crs:GradientBasedCorrections><rdf:Seq><rdf:li rdf:parseType="Resource" crs:What="Correction" crs:LocalExposure2012="%g"/></rdf:Seq></crs:GradientBasedCorrections></rdf:Description></rdf:RDF></x:xmpmeta>`, rating, exposure, left, local)
		path := frame.OutputSidecar()
		if err := os.WriteFile(path, []byte(xml), 0600); err != nil {
			t.Fatal(err)
		}
		originals[path] = []byte(xml)
	}
	var err error
	frames, err = DiscoverSequence(dir)
	if err != nil {
		t.Fatal(err)
	}
	plan, packets := interpolationPackets(t, frames, 0)
	// A separate all-four-star control retains the same two interior crop
	// anchors. Its endpoint-extension spline must match the mixed sequence;
	// this tests anchor isolation without assuming that dense sampling is linear.
	control := makeFrames(t, t.TempDir(), 9)
	for i, frame := range control {
		properties := map[string]string{"crs:CropLeft": ".8"}
		if i == 2 || i == 6 {
			properties["xmp:Rating"] = "4"
			properties["crs:CropLeft"] = []string{".1", ".3"}[(i-2)/4]
		}
		writeTestXMP(t, frame.Image, properties)
	}
	control, err = DiscoverSequence(filepath.Dir(control[0].Image))
	if err != nil {
		t.Fatal(err)
	}
	_, controlPackets := interpolationPackets(t, control, 0)
	if !reflect.DeepEqual(plan.NormalKeyframeIndices, []int{0, 8}) || !reflect.DeepEqual(plan.CropKeyframeIndices, []int{2, 6}) {
		t.Fatalf("wrong anchors: %+v", plan)
	}
	for i, packet := range packets {
		assertTransitionScalar(t, packet, "Exposure2012", float64(i)/2)
		leftText, _, _ := controlPackets[i].GetScalar("crs:CropLeft")
		wantLeft, _ := scalarNumber(leftText)
		assertTransitionScalar(t, packet, "CropLeft", wantLeft)
		value, found, err := getXMPExposureTargetValue(packet, xmpExposureTarget{Collection: "GradientBasedCorrections", Index: 1})
		if err != nil || !found || math.Abs(value-float64(i)/10) > 1e-6 {
			t.Fatalf("frame %d local=%g err=%v", i, value, err)
		}
		if rating, _ := packet.Rating(); rating != []int{4, 2, 5, 0, 0, 0, 5, 3, 4}[i] {
			t.Fatal("rating changed")
		}
	}
	for path, want := range originals {
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatal("plan wrote source sidecar")
		}
	}
	var out, stderr bytes.Buffer
	if code := Main([]string{"interpolate", dir}, &out, &stderr); code != 0 {
		t.Fatalf("CLI: %s", &stderr)
	}
	if !strings.Contains(out.String(), "Develop anchors: frame01.dng, frame09.dng") || !strings.Contains(out.String(), "Crop anchors: frame03.dng, frame07.dng") {
		t.Fatalf("missing groups: %s", &out)
	}
}

func TestCropTransitionsFallBackToFourStarAndIgnoreFiveStarDevelopValues(t *testing.T) {
	for _, cropOnly := range []bool{false, true} {
		t.Run(fmt.Sprint(cropOnly), func(t *testing.T) {
			frames := makeFrames(t, t.TempDir(), 3)
			rating := "4"
			if cropOnly {
				rating = "5"
			}
			for i, frame := range frames {
				properties := map[string]string{"crs:Exposure2012": fmt.Sprint(i), "crs:CropLeft": fmt.Sprint(float64(i) / 5)}
				if i != 1 {
					properties["xmp:Rating"] = rating
				}
				writeTestXMP(t, frame.Image, properties)
			}
			var err error
			frames, err = DiscoverSequence(filepath.Dir(frames[0].Image))
			if err != nil {
				t.Fatal(err)
			}
			_, packets := interpolationPackets(t, frames, 0)
			for i, p := range packets {
				assertTransitionScalar(t, p, "CropLeft", float64(i)/5)
				wantExposure := float64(i)
				if cropOnly {
					wantExposure = 0
				}
				assertTransitionScalar(t, p, "Exposure2012", wantExposure)
			}
		})
	}
}

func TestSingleCropAnchorDoesNotBlockDevelopOnItsFrame(t *testing.T) {
	frames := makeFrames(t, t.TempDir(), 5)
	for i, frame := range frames {
		properties := map[string]string{"crs:Exposure2012": "4.5", "crs:CropLeft": ".9"}
		if i == 0 || i == 4 {
			properties["xmp:Rating"] = "4"
			properties["crs:Exposure2012"] = fmt.Sprint(i)
		}
		if i == 2 {
			properties["xmp:Rating"] = "5"
			properties["crs:CropLeft"] = ".2"
		}
		writeTestXMP(t, frame.Image, properties)
	}
	var err error
	frames, err = DiscoverSequence(filepath.Dir(frames[0].Image))
	if err != nil {
		t.Fatal(err)
	}
	_, packets := interpolationPackets(t, frames, 0)
	for i, p := range packets {
		assertTransitionScalar(t, p, "Exposure2012", float64(i))
		assertTransitionScalar(t, p, "CropLeft", .2)
	}
}

func TestStructuredTransitionsUseFirstFourStarReference(t *testing.T) {
	frames := makeFrames(t, t.TempDir(), 4)
	for i, frame := range frames {
		rating := []int{0, 4, 5, 4}[i]
		midpoint := []int{10, 80, 20, 120}[i]
		xml := fmt.Sprintf(`<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"><rdf:Description xmlns:crs="http://ns.adobe.com/camera-raw-settings/1.0/" xmlns:xmp="http://ns.adobe.com/xap/1.0/" xmlns:vendor="urn:lrtl:reference-test" xmp:Rating="%d"><vendor:Keep>frame-%d</vendor:Keep><crs:ToneCurvePV2012><rdf:Seq><rdf:li>0, 0</rdf:li><rdf:li>128, %d</rdf:li><rdf:li>255, 255</rdf:li></rdf:Seq></crs:ToneCurvePV2012><crs:MaskGroupBasedCorrections><rdf:Seq><rdf:li rdf:parseType="Resource" crs:What="Correction"><crs:MainCurve><rdf:Seq><rdf:li>0, 0</rdf:li><rdf:li>128, %d</rdf:li><rdf:li>255, 255</rdf:li></rdf:Seq></crs:MainCurve></rdf:li></rdf:Seq></crs:MaskGroupBasedCorrections></rdf:Description></rdf:RDF></x:xmpmeta>`, rating, i, midpoint, midpoint)
		if err := os.WriteFile(frame.OutputSidecar(), []byte(xml), 0600); err != nil {
			t.Fatal(err)
		}
	}
	var err error
	frames, err = DiscoverSequence(filepath.Dir(frames[0].Image))
	if err != nil {
		t.Fatal(err)
	}
	_, packets := interpolationPackets(t, frames, 50)
	for i, p := range packets {
		data, err := p.Serialize()
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Count(data, []byte("128, 80")) != 2 || !bytes.Contains(data, []byte(fmt.Sprintf("frame-%d", i))) {
			t.Fatalf("reference curves or vendor value wrong: frame %d %s", i, data)
		}
	}
}

func TestRootPropertyEqualityUsesExpandedNamespaceNames(t *testing.T) {
	packet := func(prefix, namespace string) *XMPPacket {
		t.Helper()
		xml := fmt.Sprintf(`<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="%s"><rdf:Description xmlns:%s="%s"><%s:Look %s:Fixture="same"/></rdf:Description></rdf:RDF></x:xmpmeta>`, rdfNamespace, prefix, namespace, prefix, prefix)
		parsed, err := parseXMP([]byte(xml))
		if err != nil {
			t.Fatal(err)
		}
		return parsed
	}

	canonical := packet("crs", crsNamespace)
	alias := packet("camera", crsNamespace)
	differs, err := canonical.RootPropertyDiffers(alias, "crs:Look")
	if err != nil || differs {
		t.Fatalf("equivalent prefix alias differs=%t err=%v", differs, err)
	}

	differentNamespace := packet("crs", "urn:another-camera-namespace")
	differs, err = canonical.RootPropertyDiffers(differentNamespace, "crs:Look")
	if err != nil || !differs {
		t.Fatalf("different namespace was treated as equal: differs=%t err=%v", differs, err)
	}
}

func TestStructuredEqualizationCopiesAllRootPropertiesFromFirstFourStar(t *testing.T) {
	rows := []struct {
		name   string
		source string
		other  string
	}{
		{"Look", `<crs:Look crs:FixtureName="ref-look" crs:FixtureAmount="0.75"/>`, `<crs:Look crs:FixtureName="other-look" crs:FixtureAmount="0.25"/>`},
		{"PointColors", `<crs:PointColors><rdf:Description crs:FixtureName="Red" crs:FixtureHue="12" crs:FixtureChroma="34"/></crs:PointColors>`, `<crs:PointColors><rdf:Description crs:FixtureName="Blue" crs:FixtureHue="90"/></crs:PointColors>`},
		{"Preset", `<crs:Preset crs:FixtureName="ref-preset"><rdf:Description crs:FixtureID="ref-id" crs:FixtureAmount="0.5"/></crs:Preset>`, `<crs:Preset crs:FixtureName="other-preset"><rdf:Description crs:FixtureID="other-id"/></crs:Preset>`},
		{"ToneCurve", `<crs:ToneCurve><rdf:Description crs:FixtureName="ref-master" crs:FixturePoints="0,0 128,64 255,255"/></crs:ToneCurve>`, `<crs:ToneCurve><rdf:Description crs:FixtureName="other-master" crs:FixturePoints="0,0 128,180"/></crs:ToneCurve>`},
		{"ToneCurveName", `<crs:ToneCurveName>ref-custom</crs:ToneCurveName>`, `<crs:ToneCurveName>other-custom</crs:ToneCurveName>`},
		{"ToneCurvePV2012", `<crs:ToneCurvePV2012><rdf:Description crs:FixturePoints="0,0 128,64 255,255"/></crs:ToneCurvePV2012>`, `<crs:ToneCurvePV2012><rdf:Description crs:FixturePoints="0,0 128,180 255,255"/></crs:ToneCurvePV2012>`},
		{"ToneCurvePV2012Blue", `<crs:ToneCurvePV2012Blue><rdf:Description crs:FixturePoints="0,0 128,72 255,255"/></crs:ToneCurvePV2012Blue>`, `<crs:ToneCurvePV2012Blue><rdf:Description crs:FixturePoints="0,0 128,190 255,255"/></crs:ToneCurvePV2012Blue>`},
		{"ToneCurvePV2012Green", `<crs:ToneCurvePV2012Green><rdf:Description crs:FixturePoints="0,0 128,80 255,255"/></crs:ToneCurvePV2012Green>`, `<crs:ToneCurvePV2012Green><rdf:Description crs:FixturePoints="0,0 128,200 255,255"/></crs:ToneCurvePV2012Green>`},
		{"ToneCurvePV2012Red", `<crs:ToneCurvePV2012Red><rdf:Description crs:FixturePoints="0,0 128,88 255,255"/></crs:ToneCurvePV2012Red>`, `<crs:ToneCurvePV2012Red><rdf:Description crs:FixturePoints="0,0 128,210 255,255"/></crs:ToneCurvePV2012Red>`},
	}

	propertyXML := func(useOther bool, includeBlue bool) string {
		var content strings.Builder
		for _, row := range rows {
			if !includeBlue && row.name == "ToneCurvePV2012Blue" {
				continue
			}
			fragment := row.source
			if useOther {
				fragment = row.other
			}
			content.WriteString(fragment)
		}
		return content.String()
	}
	writePacket := func(t *testing.T, frame Frame, rating, index int, useOther, includeBlue bool) {
		t.Helper()
		xml := fmt.Sprintf(`<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"><rdf:Description rdf:about="" xmlns:crs="http://ns.adobe.com/camera-raw-settings/1.0/" xmlns:xmp="http://ns.adobe.com/xap/1.0/" xmlns:vendor="urn:lrtl:equalization-test" xmp:Rating="%d"><vendor:Keep>frame-%d</vendor:Keep>%s</rdf:Description></rdf:RDF></x:xmpmeta>`, rating, index, propertyXML(useOther, includeBlue))
		if _, err := parseXMP([]byte(xml)); err != nil {
			t.Fatalf("generated invalid fixture XMP: %v\n%s", err, xml)
		}
		if err := os.WriteFile(frame.OutputSidecar(), []byte(xml), 0600); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("copy reference properties and preserve unrelated fields", func(t *testing.T) {
		frames := makeFrames(t, t.TempDir(), 4)
		// A leading 5-star crop frame must not replace the first 4-star
		// reference used for structured develop properties.
		writePacket(t, frames[0], 5, 0, true, true)
		writePacket(t, frames[1], 4, 1, false, true)
		writePacket(t, frames[2], 0, 2, true, true)
		writePacket(t, frames[3], 4, 3, true, true)
		frames, err := DiscoverSequence(filepath.Dir(frames[0].Image))
		if err != nil {
			t.Fatal(err)
		}
		plan, packets := interpolationPackets(t, frames, 50)
		for _, row := range rows {
			differs, err := plan.StructuredSource.RootPropertyDiffers(packets[1], "crs:"+row.name)
			if err != nil || differs {
				t.Fatalf("structured source did not come from the first 4-star frame: property=%s differs=%t err=%v", row.name, differs, err)
			}
		}
		for _, targetIndex := range []int{0, 2, 3} {
			for _, row := range rows {
				differs, err := packets[targetIndex].RootPropertyDiffers(packets[1], "crs:"+row.name)
				if err != nil || differs {
					got, _ := packets[targetIndex].Serialize()
					t.Fatalf("frame %d property %s differs from reference: differs=%t err=%v\n%s", targetIndex, row.name, differs, err, got)
				}
			}
			rating, found, err := packets[targetIndex].GetScalar("xmp:Rating")
			if err != nil || !found || rating != []string{"5", "4", "0", "4"}[targetIndex] {
				t.Fatalf("frame %d rating changed: %q found=%t err=%v", targetIndex, rating, found, err)
			}
			serialized, err := packets[targetIndex].Serialize()
			if err != nil || !bytes.Contains(serialized, []byte(fmt.Sprintf("<vendor:Keep>frame-%d</vendor:Keep>", targetIndex))) {
				t.Fatalf("frame %d vendor metadata changed: err=%v\n%s", targetIndex, err, serialized)
			}
		}
		for _, targetIndex := range []int{0, 2, 3} {
			serialized, err := packets[targetIndex].Serialize()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(frames[targetIndex].OutputSidecar(), serialized, 0600); err != nil {
				t.Fatal(err)
			}
		}
		frames, err = DiscoverSequence(filepath.Dir(frames[0].Image))
		if err != nil {
			t.Fatal(err)
		}
		secondPlan, err := buildPlanWithSmoothness(frames, 50)
		if err != nil {
			t.Fatal(err)
		}
		for _, change := range secondPlan.Changes {
			if len(change.StructuredProperties) != 0 {
				t.Fatalf("second interpolation still schedules structured copies for %s: %v", change.Frame.Image, change.StructuredProperties)
			}
		}
	})

	t.Run("remove values absent from reference", func(t *testing.T) {
		frames := makeFrames(t, t.TempDir(), 3)
		writePacket(t, frames[0], 5, 0, true, true)
		writePacket(t, frames[1], 4, 1, false, false)
		writePacket(t, frames[2], 0, 2, true, true)
		frames, err := DiscoverSequence(filepath.Dir(frames[0].Image))
		if err != nil {
			t.Fatal(err)
		}
		_, packets := interpolationPackets(t, frames, 50)
		for _, targetIndex := range []int{0, 2} {
			differs, err := packets[targetIndex].RootPropertyDiffers(packets[1], "crs:ToneCurvePV2012Blue")
			if err != nil || differs {
				t.Fatalf("frame %d retained a property absent from the reference: differs=%t err=%v", targetIndex, differs, err)
			}
		}
	})
}

func TestInterpolationDoesNotPromoteNestedPointColorSaturation(t *testing.T) {
	frames := makeFrames(t, t.TempDir(), 3)
	for index, nestedSaturation := range []int{18, 54, 91} {
		rating := 0
		if index == 0 || index == 2 {
			rating = 4
		}
		xml := fmt.Sprintf(`<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="%s"><rdf:Description xmlns:crs="%s" xmlns:xmp="%s" xmp:Rating="%d"><crs:PointColors><rdf:Description crs:Name="Red" crs:Saturation="%d"/></crs:PointColors></rdf:Description></rdf:RDF></x:xmpmeta>`, rdfNamespace, crsNamespace, xmpNamespace, rating, nestedSaturation)
		if _, err := parseXMP([]byte(xml)); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(frames[index].OutputSidecar(), []byte(xml), 0600); err != nil {
			t.Fatal(err)
		}
	}
	frames, err := DiscoverSequence(filepath.Dir(frames[0].Image))
	if err != nil {
		t.Fatal(err)
	}
	_, packets := interpolationPackets(t, frames, 50)
	for index, packet := range packets {
		if _, found, err := packet.GetRootScalar("crs:Saturation"); err != nil || found {
			t.Fatalf("frame %d acquired a global saturation from nested PointColors: found=%t err=%v", index, found, err)
		}
		differs, err := packet.RootPropertyDiffers(packets[0], "crs:PointColors")
		if err != nil || differs {
			t.Fatalf("frame %d PointColors did not follow the selected reference: differs=%t err=%v", index, differs, err)
		}
	}
}

func TestSparseTransitionsAgainstInstalledLRT755(t *testing.T) {
	data, err := os.ReadFile("testdata/lrt-7.5.5-sparse-transition.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Count      int
			Integer    bool
			Start, End float64
			Expected   []float64
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 20 {
		t.Fatalf("cases=%d", len(fixture.Cases))
	}
	for n, c := range fixture.Cases {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			precise := []transitionAnchor{{0, c.Start}, {c.Count - 1, c.End}}
			anchors := []lrtSplinePoint{{0, float32(c.Start)}, {float32(c.Count - 1), float32(c.End)}}
			_, flags := transitionKeyframeFlags(c.Count, []int{0, c.Count - 1})
			spec := numericTransitionSpec{min: -100, max: 100, integer: c.Integer}
			for _, smoothness := range []int{0, 50, 100} {
				values, filled, err := sampleLRTNumericTransition(anchors, precise, c.Count, flags, smoothness, spec)
				if err != nil {
					t.Fatal(err)
				}
				for i, want := range c.Expected {
					if flags[i] {
						if filled[i] {
							t.Fatal("sparse sampler overwrote anchor")
						}
						continue
					}
					if !filled[i] || math.Abs(values[i]-want) > 2e-15 {
						t.Fatalf("frame %d got %.17g want %.17g", i, values[i], want)
					}
				}
			}
		})
	}
}

func TestMixedRatingCLIWorkflowThroughRenderedAVIPixels(t *testing.T) {
	dir := t.TempDir()
	originals := map[string][]byte{}
	for i := 0; i < 9; i++ {
		path := filepath.Join(dir, fmt.Sprintf("frame%02d.png", i))
		img := image.NewGray(image.Rect(0, 0, 64, 32))
		for y := 0; y < 32; y++ {
			for x := 0; x < 64; x++ {
				img.SetGray(x, y, color.Gray{Y: 64})
			}
		}
		var data bytes.Buffer
		if err := png.Encode(&data, img); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		originals[path] = append([]byte(nil), data.Bytes()...)
		properties := map[string]string{"crs:Exposure2012": "4.5", "crs:HasCrop": "True", "crs:CropLeft": ".8", "crs:CropTop": "0", "crs:CropRight": "1", "crs:CropBottom": "1", "crs:CropAngle": "0"}
		if i == 0 || i == 8 {
			properties["xmp:Rating"] = "4"
			properties["crs:Exposure2012"] = fmt.Sprint(float64(i) / 4)
		}
		if i == 2 || i == 6 {
			properties["xmp:Rating"] = "5"
			properties["crs:CropLeft"] = []string{".1", ".3"}[(i-2)/4]
		}
		writeTestXMP(t, path, properties)
	}
	var out, stderr bytes.Buffer
	if code := Main([]string{"interpolate", dir, "--write"}, &out, &stderr); code != 0 {
		t.Fatalf("interpolate: %s", &stderr)
	}
	output := filepath.Join(dir, "mixed.avi")
	if code := Main([]string{"render", dir, "--output", output, "--apply-xmp-exposure", "--apply-xmp-crop", "--quality", "100"}, &out, &stderr); code != 0 {
		t.Fatalf("render: %s", &stderr)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for {
		start := bytes.Index(data, []byte{255, 216})
		if start < 0 {
			break
		}
		data = data[start:]
		end := bytes.Index(data, []byte{255, 217})
		if end < 0 {
			t.Fatal("truncated JPEG")
		}
		img, err := jpeg.Decode(bytes.NewReader(data[:end+2]))
		if err != nil {
			t.Fatal(err)
		}
		// Independent sRGB exposure calculation for the uniform 64/255 source.
		linear := math.Pow((64.0/255+.055)/1.055, 2.4) * math.Exp2(float64(count)/4)
		want := 255 * (1.055*math.Pow(linear, 1/2.4) - .055)
		red, _, _, _ := img.At(32, 16).RGBA()
		if math.Abs(float64(red>>8)-want) > 2 {
			t.Fatalf("frame %d red=%d want about %g", count, red>>8, want)
		}
		count++
		data = data[end+2:]
	}
	if count != 9 {
		t.Fatalf("frames=%d", count)
	}
	for path, want := range originals {
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatal("image source changed")
		}
	}
}

func TestSingleReferenceKeepsDoublePrecisionForGlobalAndLocalValues(t *testing.T) {
	frames := makeFrames(t, t.TempDir(), 5)
	const want = .123456789123
	for i, frame := range frames {
		rating := 0
		value := ".8"
		if i == 2 {
			rating = 4
			value = ".123456789123"
		}
		packet := fmt.Sprintf(`<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"><rdf:Description xmlns:crs="http://ns.adobe.com/camera-raw-settings/1.0/" xmlns:xmp="http://ns.adobe.com/xap/1.0/" xmp:Rating="%d" crs:Exposure2012="%s"><crs:GradientBasedCorrections><rdf:Seq><rdf:li rdf:parseType="Resource" crs:What="Correction" crs:LocalExposure2012="%s"/></rdf:Seq></crs:GradientBasedCorrections></rdf:Description></rdf:RDF></x:xmpmeta>`, rating, value, value)
		if err := os.WriteFile(frame.OutputSidecar(), []byte(packet), 0600); err != nil {
			t.Fatal(err)
		}
	}
	var err error
	frames, err = DiscoverSequence(filepath.Dir(frames[0].Image))
	if err != nil {
		t.Fatal(err)
	}
	_, packets := interpolationPackets(t, frames, 50)
	for i, p := range packets {
		text, _, err := p.GetScalar("crs:Exposure2012")
		value, valid := scalarNumber(text)
		if err != nil || !valid || math.Abs(value-want) > 1e-14 {
			t.Fatalf("frame %d exposure=%s err=%v", i, text, err)
		}
		local, found, err := getXMPExposureTargetValue(p, xmpExposureTarget{Collection: "GradientBasedCorrections", Index: 1})
		if err != nil || !found || math.Abs(local-want) > 1e-14 {
			t.Fatalf("frame %d local=%.15g err=%v", i, local, err)
		}
	}
}
