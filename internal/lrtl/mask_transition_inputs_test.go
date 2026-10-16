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
	"strings"
	"testing"
)

func localMissingPacket(t *testing.T, collection string, local, geometry map[string]string) *XMPPacket {
	t.Helper()
	packet := EmptyXMPPacket()
	_, array, err := ensurePacketCRSArray(packet, collection)
	if err != nil {
		t.Fatal(err)
	}
	item, err := ensureRDFArrayPosition(array, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	correction := unwrapRDFArrayItem(item)
	for name, value := range map[string]string{"What": "Correction", "CorrectionActive": "True", "CorrectionAmount": "1", "CorrectionSyncID": "correction"} {
		if err := setCRSElementValue(correction, name, value); err != nil {
			t.Fatal(err)
		}
	}
	for name, value := range local {
		if err := setCRSElementValue(correction, name, value); err != nil {
			t.Fatal(err)
		}
	}
	property, err := ensureCRSArrayProperty(correction, "CorrectionMasks")
	if err != nil {
		t.Fatal(err)
	}
	masks, err := ensureRDFArrayContainer(property)
	if err != nil {
		t.Fatal(err)
	}
	maskItem, err := ensureRDFArrayPosition(masks, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	mask := unwrapRDFArrayItem(maskItem)
	for name, value := range map[string]string{"What": "Mask/Gradient", "MaskSyncID": "selector", "MaskValue": "1"} {
		if err := setCRSElementValue(mask, name, value); err != nil {
			t.Fatal(err)
		}
	}
	if geometry == nil {
		geometry = map[string]string{"ZeroX": ".5", "FullX": ".5", "ZeroY": "-.2", "FullY": "-.1"}
	}
	for name, value := range geometry {
		if err := setCRSElementValue(mask, name, value); err != nil {
			t.Fatal(err)
		}
	}
	return packet
}

func TestLocalNumericMissingAgainstInstalledLRT755(t *testing.T) {
	data, err := os.ReadFile("testdata/lrt-7.5.5-local-numeric-missing.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Property string
		Cases    []struct{ Input, Expected []*float64 }
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Property != "GradientBasedCorrections[1]/crs:LocalExposure2012" || len(fixture.Cases) != 9 {
		t.Fatal("unexpected local fixture")
	}
	for n, c := range fixture.Cases {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			indexes := make([]map[maskTransitionKey]maskTransitionNode, len(c.Input))
			packets := make([]*XMPPacket, len(c.Input))
			originals := make([][]byte, len(c.Input))
			for i, value := range c.Input {
				local := map[string]string{}
				if value != nil {
					local["LocalExposure2012"] = fmt.Sprint(*value)
				}
				packets[i] = localMissingPacket(t, "GradientBasedCorrections", local, nil)
				indexes[i], err = indexMaskTransitionNodes(packets[i])
				if err != nil {
					t.Fatal(err)
				}
				originals[i], err = packets[i].Serialize()
				if err != nil {
					t.Fatal(err)
				}
			}
			prepared := prepareMaskTransitionInputs(indexes)
			key := maskTransitionKey{collection: "GradientBasedCorrections", correctionIndex: 1}
			for i, want := range c.Expected {
				got, found := prepared.Indexes[i][key].values["LocalExposure2012"]
				if (want != nil) != found || (want != nil && got != *want) {
					t.Fatalf("frame %d got %g found=%t want %v", i, got, found, want)
				}
				_, missing := prepared.Missing[i][key]["LocalExposure2012"]
				if missing != (c.Input[i] == nil && want != nil) {
					t.Fatalf("missing flag wrong for frame %d", i)
				}
				_, originallyPresent := indexes[i][key].values["LocalExposure2012"]
				if originallyPresent != (c.Input[i] != nil) {
					t.Fatal("mutated original numeric index")
				}
				after, err := packets[i].Serialize()
				if err != nil || !bytes.Equal(after, originals[i]) {
					t.Fatal("preflight mutated packet")
				}
			}
		})
	}
}

func TestLocalMissingFieldsReachSelectedKeysWithoutReplacingIDs(t *testing.T) {
	for _, collection := range []string{"GradientBasedCorrections", "MaskGroupBasedCorrections"} {
		t.Run(collection, func(t *testing.T) {
			frames := makeFrames(t, t.TempDir(), 5)
			for i, frame := range frames {
				local := map[string]string{"CorrectionSyncID": fmt.Sprintf("own-correction-%d", i)}
				geometry := map[string]string{"ZeroX": ".5", "FullX": ".5", "FullY": "-.1", "MaskSyncID": fmt.Sprintf("own-selector-%d", i)}
				if i == 2 {
					local["LocalExposure2012"] = ".123456789123"
					local["LocalTemperature"] = ".25"
					geometry["ZeroY"] = "-.2"
				}
				packet := localMissingPacket(t, collection, local, geometry)
				if i == 0 || i == 4 {
					packet.SetScalar("xmp:Rating", "4")
				}
				if err := packet.SetScalar("dc:description", fmt.Sprintf("keep-%d", i)); err != nil {
					t.Fatal(err)
				}
				persistExposurePacket(t, frame, packet)
			}
			var err error
			frames, err = DiscoverSequence(filepath.Dir(frames[0].Image))
			if err != nil {
				t.Fatal(err)
			}
			_, packets := interpolationPackets(t, frames, 50)
			for i, p := range packets {
				nodes, err := indexMaskTransitionNodes(p)
				if err != nil {
					t.Fatal(err)
				}
				correction := nodes[maskTransitionKey{collection: collection, correctionIndex: 1}]
				selector := nodes[maskTransitionKey{collection: collection, correctionIndex: 1, maskIndex: 1}]
				if math.Abs(correction.values["LocalExposure2012"]-.123456789123) > 1e-14 || correction.values["LocalTemperature"] != .25 || math.Abs(selector.values["ZeroY"]+.2) > 1e-12 {
					t.Fatalf("frame %d values=%v %v", i, correction.values, selector.values)
				}
				id, _, err := crsElementValue(unwrapRDFArrayItem(correction.correctionItem), "CorrectionSyncID")
				if err != nil || id != fmt.Sprintf("own-correction-%d", i) {
					t.Fatal("correction ID replaced")
				}
				id, _, err = crsElementValue(unwrapRDFArrayItem(selector.maskItem), "MaskSyncID")
				if err != nil || id != fmt.Sprintf("own-selector-%d", i) {
					t.Fatal("mask ID replaced")
				}
				text, _, _ := p.GetScalar("dc:description")
				if text != fmt.Sprintf("keep-%d", i) {
					t.Fatal("unrelated metadata changed")
				}
			}
		})
	}
}

func TestLocalMissingDoesNotCreateSelectedMissingStructure(t *testing.T) {
	packets := []*XMPPacket{localMissingPacket(t, "GradientBasedCorrections", map[string]string{"LocalExposure2012": ".4"}, nil), EmptyXMPPacket(), localMissingPacket(t, "GradientBasedCorrections", map[string]string{"LocalExposure2012": ".8"}, nil)}
	changes, err := planMaskTransitions(packets, []int{0, 1, 2}, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes[1]) != 0 {
		t.Fatalf("created selected-frame structure: %+v", changes[1])
	}
}

func TestLocalMissingDoesNotFillIDFallbackOrReservedPositions(t *testing.T) {
	packet := localMissingPacket(t, "GradientBasedCorrections", map[string]string{"LocalExposure2012": ".3"}, nil)
	indexes := make([]map[maskTransitionKey]maskTransitionNode, 2)
	var err error
	indexes[0], err = indexMaskTransitionNodes(packet)
	if err != nil {
		t.Fatal(err)
	}
	indexes[1], err = indexMaskTransitionNodes(localMissingPacket(t, "GradientBasedCorrections", nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	source := indexes[0][maskTransitionKey{collection: "GradientBasedCorrections", correctionIndex: 1}]
	target := indexes[1][maskTransitionKey{collection: "GradientBasedCorrections", correctionIndex: 1}]
	for _, key := range []maskTransitionKey{{collection: "GradientBasedCorrections", correctionIndex: 5}, {collection: "GradientBasedCorrections", correctionID: "custom"}, {collection: "GradientBasedCorrections", correctionIndex: 1, maskIndex: 2}} {
		indexes[0][key] = source
		indexes[1][key] = target
		prepared := prepareMaskTransitionInputs(indexes)
		if len(prepared.Missing[1][key]) != 0 {
			t.Fatalf("filled unsupported key %+v", key)
		}
		delete(indexes[0], key)
		delete(indexes[1], key)
	}
}

func TestLocalMalformedNumericFailsBeforeWrites(t *testing.T) {
	for _, value := range []string{"", "NaN", "Inf", "bad"} {
		t.Run(value, func(t *testing.T) {
			frames := makeFrames(t, t.TempDir(), 3)
			for i, frame := range frames {
				local := map[string]string{"LocalExposure2012": ".3"}
				if i == 2 {
					local["LocalExposure2012"] = value
				}
				packet := localMissingPacket(t, "GradientBasedCorrections", local, nil)
				packet.SetScalar("xmp:Rating", "4")
				persistExposurePacket(t, frame, packet)
			}
			original, err := os.ReadFile(frames[0].OutputSidecar())
			if err != nil {
				t.Fatal(err)
			}
			var out, stderr bytes.Buffer
			if code := Main([]string{"interpolate", filepath.Dir(frames[0].Image), "--write"}, &out, &stderr); code == 0 {
				t.Fatal("accepted malformed local value")
			}
			after, err := os.ReadFile(frames[0].OutputSidecar())
			if err != nil || !bytes.Equal(after, original) {
				t.Fatal("failed validation wrote earlier frame")
			}
			backups, _ := filepath.Glob(filepath.Join(filepath.Dir(frames[0].Image), "*.bak"))
			if len(backups) != 0 {
				t.Fatal("failed validation created backup")
			}
		})
	}
	packet := localMissingPacket(t, "GradientBasedCorrections", nil, nil)
	nodes, err := indexMaskTransitionNodes(packet)
	if err != nil {
		t.Fatal(err)
	}
	node := unwrapRDFArrayItem(nodes[maskTransitionKey{collection: "GradientBasedCorrections", correctionIndex: 1}].correctionItem)
	property := node.CreateElement("crs:LocalExposure2012")
	property.CreateElement("rdf:Seq").CreateElement("rdf:li").SetText(".3")
	if _, err := indexMaskTransitionNodes(packet); err == nil {
		t.Fatal("accepted structured local number")
	}
}

func TestLocalMissingCLIThroughVideoPixels(t *testing.T) {
	frames := cropFixture(t, 5)
	img := image.NewGray(image.Rect(0, 0, 64, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 64; x++ {
			img.SetGray(x, y, color.Gray{Y: 64})
		}
	}
	var source bytes.Buffer
	if err := png.Encode(&source, img); err != nil {
		t.Fatal(err)
	}
	for i, frame := range frames {
		if err := os.WriteFile(frame.Image, source.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		local := map[string]string{}
		geometry := map[string]string{"ZeroX": ".5", "FullX": ".5", "FullY": "-.1"}
		if i == 2 {
			local["LocalExposure2012"] = ".8"
			geometry["ZeroY"] = "-.2"
		}
		packet := localMissingPacket(t, "MaskGroupBasedCorrections", local, geometry)
		if i == 0 || i == 4 {
			packet.SetScalar("xmp:Rating", "4")
		}
		persistExposurePacket(t, frame, packet)
	}
	original, err := os.ReadFile(frames[0].Sidecar)
	if err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	dir := filepath.Dir(frames[0].Image)
	if code := Main([]string{"interpolate", dir}, &out, &stderr); code != 0 {
		t.Fatalf("preview: %s", &stderr)
	}
	afterPreview, err := os.ReadFile(frames[0].Sidecar)
	if err != nil || !bytes.Equal(afterPreview, original) {
		t.Fatal("preview wrote a keyframe")
	}
	if code := Main([]string{"interpolate", dir, "--write"}, &out, &stderr); code != 0 {
		t.Fatalf("interpolate: %s", &stderr)
	}
	backups, _ := filepath.Glob(frames[0].Sidecar + "*.bak")
	if len(backups) != 1 {
		t.Fatalf("backups=%v", backups)
	}
	backup, err := os.ReadFile(backups[0])
	if err != nil || !bytes.Equal(backup, original) {
		t.Fatal("backup changed original")
	}
	beforeRepeat, _ := filepath.Glob(filepath.Join(dir, "*.bak"))
	if code := Main([]string{"interpolate", dir, "--write"}, &out, &stderr); code != 0 {
		t.Fatalf("repeat: %s", &stderr)
	}
	afterRepeat, _ := filepath.Glob(filepath.Join(dir, "*.bak"))
	if len(afterRepeat) != len(beforeRepeat) {
		t.Fatal("repeat changed packets")
	}
	output := filepath.Join(dir, "local-filled.avi")
	if code := Main([]string{"render", dir, "--output", output, "--apply-xmp-basic", "--quality", "100"}, &out, &stderr); code != 0 {
		t.Fatalf("render: %s", &stderr)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	// Independent exposure calculation for the uniform 64/255 source.
	want := 255 * (1.055*math.Pow(math.Pow((64.0/255+.055)/1.055, 2.4)*math.Exp2(.8), 1/2.4) - .055)
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
		red, _, _, _ := img.At(32, 16).RGBA()
		if math.Abs(float64(red>>8)-want) > 2 {
			t.Fatalf("frame %d red=%d want %g", count, red>>8, want)
		}
		count++
		data = data[end+2:]
	}
	if count != 5 || !strings.Contains(out.String(), "MaskGroupBasedCorrections") {
		t.Fatalf("frames=%d output=%s", count, &out)
	}
	for _, frame := range frames {
		data, err := os.ReadFile(frame.Image)
		if err != nil || !bytes.Equal(data, source.Bytes()) {
			t.Fatal("modified source image")
		}
	}
}

func TestLocalMissingAngleNormalizesOnceAndPreservesActivity(t *testing.T) {
	packets := make([]*XMPPacket, 3)
	for i := range packets {
		geometry := map[string]string{"What": "Mask/CircularGradient", "Top": ".1", "Left": ".1", "Bottom": ".9", "Right": ".9"}
		if i == 1 {
			geometry["Angle"] = "100"
		}
		active := []string{"False", "True", "False"}[i]
		packets[i] = localMissingPacket(t, "CircularGradientBasedCorrections", map[string]string{"CorrectionActive": active}, geometry)
	}
	changes, err := planMaskTransitions(packets, []int{0, 2}, 50)
	if err != nil {
		t.Fatal(err)
	}
	for i := range packets {
		for _, change := range changes[i] {
			if err := applyMaskTransition(packets[i], change); err != nil {
				t.Fatal(err)
			}
		}
		nodes, err := indexMaskTransitionNodes(packets[i])
		if err != nil {
			t.Fatal(err)
		}
		selector := nodes[maskTransitionKey{collection: "CircularGradientBasedCorrections", correctionIndex: 1, maskIndex: 1}]
		if selector.values["Angle"] != 10 {
			t.Fatalf("frame %d angle=%g", i, selector.values["Angle"])
		}
		correction := nodes[maskTransitionKey{collection: "CircularGradientBasedCorrections", correctionIndex: 1}]
		active, _, _ := crsElementValue(unwrapRDFArrayItem(correction.correctionItem), "CorrectionActive")
		if active != []string{"False", "True", "False"}[i] {
			t.Fatal("activity changed")
		}
	}
	again, err := planMaskTransitions(packets, []int{0, 2}, 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, changes := range again {
		if len(changes) != 0 {
			t.Fatal("repeat changed normalized angle")
		}
	}
}

func TestLocalMissingPreflightDoesNotCreateRangeMask(t *testing.T) {
	packets := []*XMPPacket{localMissingPacket(t, "GradientBasedCorrections", nil, nil), localMissingPacket(t, "GradientBasedCorrections", nil, nil)}
	key := maskTransitionKey{collection: "GradientBasedCorrections", correctionIndex: 1}
	source, err := indexMaskTransitionNodes(packets[0])
	if err != nil {
		t.Fatal(err)
	}
	element := unwrapRDFArrayItem(source[key].correctionItem).CreateElement("crs:CorrectionRangeMask")
	if err := setCRSElementValue(element, "Type", "1"); err != nil {
		t.Fatal(err)
	}
	if err := setCRSElementValue(element, "ColorAmount", ".7"); err != nil {
		t.Fatal(err)
	}
	indexes := make([]map[maskTransitionKey]maskTransitionNode, 2)
	for i, p := range packets {
		indexes[i], err = indexMaskTransitionNodes(p)
		if err != nil {
			t.Fatal(err)
		}
	}
	prepared := prepareMaskTransitionInputs(indexes)
	for name := range prepared.Missing[1][key] {
		if strings.HasPrefix(name, "RangeMask") {
			t.Fatal("planned fields for absent range resource")
		}
	}
	changes, err := planMaskTransitions(packets, []int{0, 1}, 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range changes[1] {
		if err := applyMaskTransition(packets[1], change); err != nil {
			t.Fatal(err)
		}
	}
	target, err := indexMaskTransitionNodes(packets[1])
	if err != nil {
		t.Fatal(err)
	}
	if correctionRangeMaskElement(unwrapRDFArrayItem(target[key].correctionItem)) != nil {
		t.Fatal("created missing range resource")
	}
}
