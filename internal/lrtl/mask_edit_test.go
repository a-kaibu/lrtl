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

func TestMaskEditPathGrammar(t *testing.T) {
	for _, c := range []struct {
		text, canonical       string
		correction, component int
	}{
		{"MaskGroupBasedCorrections[1].LocalExposure2012", "MaskGroupBasedCorrections[1].LocalExposure2012", 1, 0},
		{"crs:GradientBasedCorrections[4].crs:CorrectionMasks[2].crs:FullY", "GradientBasedCorrections[4].CorrectionMasks[2].FullY", 4, 2},
		{"CircularGradientBasedCorrections[2].CorrectionMasks[1].Angle", "CircularGradientBasedCorrections[2].CorrectionMasks[1].Angle", 2, 1},
	} {
		got, err := parseMaskEditPath(c.text)
		if err != nil || got.String() != c.canonical || got.correction != c.correction || got.component != c.component {
			t.Fatalf("%s: %v %v", c.text, got, err)
		}
	}
	for _, bad := range []string{"Exposure2012", "Unknown[1].LocalExposure2012", "MaskGroupBasedCorrections[0].LocalExposure2012", "MaskGroupBasedCorrections[-1].LocalExposure2012", "MaskGroupBasedCorrections[01].LocalExposure2012", "MaskGroupBasedCorrections[999999999999999999999999999999].LocalExposure2012", "MaskGroupBasedCorrections[1].CorrectionMasks[4097].FullY", "MaskGroupBasedCorrections[1].CorrectionMasks[0].FullY", "MaskGroupBasedCorrections[1]..FullY", "MaskGroupBasedCorrections[1].CorrectionMasks[1].Unknown[1].FullY"} {
		if _, err := parseMaskEditPath(bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}

func TestMaskEditsKeepUnknownDataAndProtectInternalCorrections(t *testing.T) {
	for _, format := range []string{"modern", "legacy"} {
		t.Run(format, func(t *testing.T) {
			frames := makeFrames(t, t.TempDir(), 1)
			plan, err := buildInitializationPlan(frames, format)
			if err != nil {
				t.Fatal(err)
			}
			packet := plan[0].Packet
			collection := "MaskGroupBasedCorrections"
			if format == "legacy" {
				collection = "GradientBasedCorrections"
			}
			item := existingInitializationSlot(packet, format, 1)
			item.CreateAttr("xmlns:vendor", "urn:lrtl:mask-edit")
			item.CreateElement("vendor:Note").SetText("unchanged")
			for _, c := range [][3]string{{"LocalExposure2012", "-0.5", "-0.5"}, {"LocalContrast2012", "0.25", "0.25"}, {"CorrectionActive", "True", "true"}, {"CorrectionActive", "0", "false"}, {"CorrectionActive", "true", "true"}} {
				path := maskEditPath{collection, 1, 0, c[0]}
				got, err := setMaskProperty(packet, path, c[1])
				if err != nil || got != c[2] {
					t.Fatalf("set %s=%s: %s %v", path, c[1], got, err)
				}
			}
			path := maskEditPath{collection, 1, 1, "FullY"}
			if _, err := setMaskProperty(packet, path, "0.35"); err != nil {
				t.Fatal(err)
			}
			mask := unwrapRDFArrayItem(rdfArrayItems(directCRSProperty(item, "CorrectionMasks").element)[0])
			got, _, _ := crsElementValue(mask, "FullY")
			if got != "0.35" {
				t.Fatal("wrong mask edited")
			}
			for slot := 5; slot <= 7; slot++ {
				if _, err := setMaskProperty(packet, maskEditPath{collection, slot, 0, "LocalExposure2012"}, ".5"); err == nil {
					t.Fatalf("internal slot %d edited", slot)
				}
			}
			before, err := packet.Serialize()
			if err != nil || !bytes.Contains(before, []byte("unchanged")) {
				t.Fatal("vendor node lost")
			}
			for _, c := range []struct {
				component   int
				name, value string
			}{
				{0, "LocalExposure2012", "2"}, {0, "LocalContrast2012", "-1.1"}, {0, "LocalColorGradeGlobalHue", "1.5"}, {0, "LocalExposure2012", "NaN"}, {0, "LocalExposure2012", "Inf"}, {0, "What", "Correction"}, {0, "CorrectionName", "rename"}, {0, "MaskActive", "false"}, {1, "Top", "0.2"}, {1, "FullY", "3"}, {1, "MaskActive", "maybe"}, {1, "LocalExposure2012", "0.5"},
			} {
				if _, err := setMaskProperty(packet, maskEditPath{collection, 1, c.component, c.name}, c.value); err == nil {
					t.Fatalf("accepted invalid %s=%s", c.name, c.value)
				}
			}
			after, err := packet.Serialize()
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("invalid edit mutated packet")
			}
		})
	}
}

func TestMaskEditAndInspectionCLI(t *testing.T) {
	dir := t.TempDir()
	frames := makeFrames(t, dir, 1)
	plan, err := buildInitializationPlan(frames, "modern")
	if err != nil {
		t.Fatal(err)
	}
	saveInitializationPlan(t, plan)
	path := frames[0].Image
	original, err := os.ReadFile(path + ".xmp")
	if err != nil {
		t.Fatal(err)
	}
	property := "MaskGroupBasedCorrections[1].LocalExposure2012"
	var out, stderr bytes.Buffer
	if code := Main([]string{"set", path, property, "-0.5", "--dry-run"}, &out, &stderr); code != 0 {
		t.Fatalf("dry run %d: %s", code, stderr.String())
	}
	actual, err := os.ReadFile(path + ".xmp")
	if err != nil || !bytes.Equal(actual, original) {
		t.Fatal("dry run changed XMP")
	}
	if code := Main([]string{"set", path, property, "-0.5"}, &out, &stderr); code != 0 {
		t.Fatalf("edit %d: %s", code, stderr.String())
	}
	backups, err := filepath.Glob(path + ".xmp.*.bak")
	if err != nil || len(backups) != 1 {
		t.Fatal("backup missing")
	}
	back, err := os.ReadFile(backups[0])
	if err != nil || !bytes.Equal(back, original) {
		t.Fatal("backup does not match original")
	}
	out.Reset()
	stderr.Reset()
	if code := Main([]string{"masks", path, "--json"}, &out, &stderr); code != 0 {
		t.Fatalf("inspect %d: %s", code, stderr.String())
	}
	var info []maskCorrectionInfo
	if err := json.Unmarshal(out.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if len(info) != 9 || info[0].Values["LocalExposure2012"] != "-0.5" || !info[0].Editable || !info[0].Transition || info[4].Editable || info[4].Transition {
		t.Fatalf("inspection mismatch: %+v", info)
	}
	if !info[0].Components[0].Transition || info[0].Components[0].Path != "MaskGroupBasedCorrections[1].CorrectionMasks[1]" {
		t.Fatal("wrong component path")
	}
	out.Reset()
	if code := Main([]string{"masks", path + ".xmp"}, &out, &stderr); code != 0 || !strings.Contains(out.String(), "editable=false") {
		t.Fatalf("text listing %d: %s %s", code, out.String(), stderr.String())
	}
	if code := Main([]string{"masks", filepath.Join(dir, "missing.png")}, &out, &stderr); code == 0 {
		t.Fatal("missing PNG accepted")
	}
}

func TestCLILocalMaskAnimationThroughAVIPixels(t *testing.T) {
	for _, format := range []string{"modern", "legacy"} {
		t.Run(format, func(t *testing.T) {
			dir := t.TempDir()
			originals := make(map[string][]byte)
			for i := 0; i < 3; i++ {
				img := image.NewGray(image.Rect(0, 0, 32, 24))
				for j := range img.Pix {
					img.Pix[j] = 100
				}
				var encoded bytes.Buffer
				if err := png.Encode(&encoded, img); err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(dir, fmt.Sprintf("frame%d.png", i))
				if err := os.WriteFile(path, encoded.Bytes(), 0600); err != nil {
					t.Fatal(err)
				}
				originals[path] = encoded.Bytes()
			}
			collection := "MaskGroupBasedCorrections"
			if format == "legacy" {
				collection = "GradientBasedCorrections"
			}
			commands := [][]string{{"init", dir, "--mask-format", format, "--write"}, {"keyframes", dir, "--count", "2", "--evenly-spaced", "--write"}}
			for _, c := range []struct {
				frame           int
				exposure, fullY string
			}{{0, "-0.5", "0.2"}, {2, "0.5", "0.4"}} {
				path := filepath.Join(dir, fmt.Sprintf("frame%d.png", c.frame))
				commands = append(commands, []string{"set", path, collection + "[1].LocalExposure2012", c.exposure}, []string{"set", path, collection + "[1].CorrectionMasks[1].FullY", c.fullY})
			}
			commands = append(commands, []string{"interpolate", dir, "--smoothness", "50", "--write"}, []string{"render", dir, "--apply-xmp-basic", "--quality", "100", "--output", filepath.Join(dir, "animation.avi")})
			for _, args := range commands {
				var out, stderr bytes.Buffer
				if code := Main(args, &out, &stderr); code != 0 {
					t.Fatalf("%v: %d %s", args, code, stderr.String())
				}
			}
			frames, err := DiscoverSequence(dir)
			if err != nil {
				t.Fatal(err)
			}
			middle, err := PacketForFrame(frames[1])
			if err != nil {
				t.Fatal(err)
			}
			geometry, err := maskEditElement(middle, maskEditPath{collection, 1, 1, "FullY"})
			if err != nil {
				t.Fatal(err)
			}
			text, _, _ := crsElementValue(geometry, "FullY")
			value, valid := scalarNumber(text)
			if !valid || math.Abs(value-.3) > 1e-6 {
				t.Fatalf("shape did not animate: %s", text)
			}
			video, err := os.ReadFile(filepath.Join(dir, "animation.avi"))
			if err != nil {
				t.Fatal(err)
			}
			var top []uint8
			for {
				start := bytes.Index(video, []byte{255, 216})
				if start < 0 {
					break
				}
				video = video[start:]
				end := bytes.Index(video, []byte{255, 217})
				if end < 0 {
					t.Fatal("truncated JPEG frame")
				}
				end += 2
				img, err := jpeg.Decode(bytes.NewReader(video[:end]))
				if err != nil {
					t.Fatal(err)
				}
				top = append(top, color.GrayModel.Convert(img.At(16, 1)).(color.Gray).Y)
				bottom := color.GrayModel.Convert(img.At(16, 22)).(color.Gray).Y
				if bottom < 98 || bottom > 102 {
					t.Fatalf("unmasked pixel changed: %d", bottom)
				}
				video = video[end:]
			}
			if len(top) != 3 || !(top[0] < 95 && top[1] >= 98 && top[1] <= 102 && top[2] > 105) {
				t.Fatalf("wrong animated exposure pixels: %v", top)
			}
			for path, original := range originals {
				data, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(data, original) {
					t.Fatal("source image changed")
				}
			}
		})
	}
}

func TestMaskEditWrappedDescriptionsPrefixesAndSecondComponent(t *testing.T) {
	packet, err := parseXMP([]byte(`<x:xmpmeta xmlns:x="adobe:ns:meta/"><r:RDF xmlns:r="http://www.w3.org/1999/02/22-rdf-syntax-ns#"><r:Description r:about="" xmlns:m="http://ns.adobe.com/camera-raw-settings/1.0/" xmlns:crs="urn:lrtl:vendor"><crs:Note>keep foreign prefix</crs:Note><m:MaskGroupBasedCorrections><r:Seq><r:li><r:Description m:What="Correction" m:LocalExposure2012="0.1"><m:CorrectionMasks><r:Seq><r:li><r:Description m:What="Mask/Gradient" m:ZeroX=".5" m:ZeroY=".5" m:FullX=".5"><m:FullY>.2</m:FullY></r:Description></r:li><r:li r:parseType="Resource" m:What="Mask/Gradient" m:ZeroX=".5" m:ZeroY=".5" m:FullX=".5" m:FullY=".8"/></r:Seq></m:CorrectionMasks></r:Description></r:li></r:Seq></m:MaskGroupBasedCorrections></r:Description></r:RDF></x:xmpmeta>`))
	if err != nil {
		t.Fatal(err)
	}
	for _, edit := range []struct{ path, value string }{
		{"crs:MaskGroupBasedCorrections[1].crs:LocalExposure2012", "0.4"},
		{"MaskGroupBasedCorrections[1].CorrectionMasks[1].FullY", "0.3"},
		{"MaskGroupBasedCorrections[1].CorrectionMasks[2].FullY", "0.7"},
		{"MaskGroupBasedCorrections[1].CorrectionMasks[2].MaskInverted", "true"},
	} {
		path, err := parseMaskEditPath(edit.path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := setMaskProperty(packet, path, edit.value); err != nil {
			t.Fatal(err)
		}
	}
	info, err := inspectMaskCorrections(packet)
	if err != nil {
		t.Fatal(err)
	}
	if len(info) != 1 || len(info[0].Components) != 2 || info[0].Values["LocalExposure2012"] != "0.4" || info[0].Components[0].Values["FullY"] != "0.3" || info[0].Components[1].Values["FullY"] != "0.7" || info[0].Components[1].Values["MaskInverted"] != "true" {
		t.Fatalf("wrong wrapped-node edits: %+v", info)
	}
	if !info[0].Components[0].Transition || info[0].Components[1].Transition {
		t.Fatal("wrong component transition eligibility")
	}
	data, err := packet.Serialize()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("keep foreign prefix")) || !bytes.Contains(data, []byte(`xmlns:crs="urn:lrtl:vendor"`)) {
		t.Fatal("foreign namespace was replaced")
	}
	if _, err := parseXMP(data); err != nil {
		t.Fatalf("serialized packet invalid: %v", err)
	}
}

func TestMaskEditsHandleRadialAndMissingOrTrackedSlots(t *testing.T) {
	frames := makeFrames(t, t.TempDir(), 1)
	plan, err := buildInitializationPlan(frames, "modern")
	if err != nil {
		t.Fatal(err)
	}
	packet := plan[0].Packet
	for _, property := range []string{"Angle", "Feather", "Left", "Top", "Right", "Bottom", "Midpoint", "Roundness"} {
		value := "0.3"
		if property == "Angle" {
			value = "-20"
		}
		if property == "Feather" || property == "Midpoint" {
			value = "40"
		}
		if _, err := setMaskProperty(packet, maskEditPath{"MaskGroupBasedCorrections", 8, 1, property}, value); err != nil {
			t.Fatalf("radial %s: %v", property, err)
		}
	}
	for _, path := range []maskEditPath{{"MaskGroupBasedCorrections", 10, 0, "LocalExposure2012"}, {"MaskGroupBasedCorrections", 1, 2, "FullY"}, {"MaskGroupBasedCorrections", 8, 1, "FullY"}} {
		if _, err := setMaskProperty(packet, path, "0.2"); err == nil {
			t.Fatalf("missing or incompatible path accepted: %s", path)
		}
	}
	// Tracking protects a custom slot even when it has no internal product name.
	if err := packet.SetScalar(deflickerTargetProperty, "MaskGroupBasedCorrections:1"); err != nil {
		t.Fatal(err)
	}
	if _, err := setMaskProperty(packet, maskEditPath{"MaskGroupBasedCorrections", 1, 0, "LocalExposure2012"}, ".2"); err == nil {
		t.Fatal("managed custom target edited")
	}
}

func TestMaskSetCLIReadsEmbeddedPacketWithoutChangingImage(t *testing.T) {
	dir := t.TempDir()
	frames := makeFrames(t, dir, 1)
	plan, err := buildInitializationPlan(frames, "modern")
	if err != nil {
		t.Fatal(err)
	}
	xmp, err := plan[0].Packet.Serialize()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "embedded.jpg")
	original := writeEmbeddedTransitionJPEG(t, path, xmp)
	var out, stderr bytes.Buffer
	if code := Main([]string{"set", path, "MaskGroupBasedCorrections[1].LocalExposure2012", ".25", "--no-backup"}, &out, &stderr); code != 0 {
		t.Fatalf("embedded set %d: %s", code, stderr.String())
	}
	packet, err := PacketForSidecar(path + ".xmp")
	if err != nil {
		t.Fatal(err)
	}
	info, err := inspectMaskCorrections(packet)
	if err != nil || info[0].Values["LocalExposure2012"] != "0.25" {
		t.Fatalf("embedded edit not saved: %v", err)
	}
	unchanged, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(unchanged, original) {
		t.Fatal("embedded image overwritten")
	}
}

func TestMaskSetterDoesNotMaterializeEmptyRDFItems(t *testing.T) {
	packet := EmptyXMPPacket()
	_, container, err := ensurePacketCRSArray(packet, "MaskGroupBasedCorrections")
	if err != nil {
		t.Fatal(err)
	}
	item := createRDFResourceArrayItem(container)
	before, err := packet.Serialize()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := setMaskProperty(packet, maskEditPath{"MaskGroupBasedCorrections", 1, 0, "LocalExposure2012"}, ".5"); err == nil {
		t.Fatal("empty item materialized as partial correction")
	}
	after, err := packet.Serialize()
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("empty item mutated")
	}
	info, err := inspectMaskCorrections(packet)
	if err != nil || len(info) != 1 || info[0].Editable {
		t.Fatal("empty item reported editable")
	}
	setCRSElementValue(item, "What", "Correction")
	property, err := ensureCRSArrayProperty(item, "CorrectionMasks")
	if err != nil {
		t.Fatal(err)
	}
	masks, err := ensureRDFArrayContainer(property)
	if err != nil {
		t.Fatal(err)
	}
	createRDFResourceArrayItem(masks)
	if _, err := setMaskProperty(packet, maskEditPath{"MaskGroupBasedCorrections", 1, 1, "MaskActive"}, "true"); err == nil {
		t.Fatal("empty component edited")
	}
}

func FuzzMaskEditPath(f *testing.F) {
	for _, seed := range []string{"MaskGroupBasedCorrections[1].LocalExposure2012", "GradientBasedCorrections[2].CorrectionMasks[1].FullY", "CircularGradientBasedCorrections[2].CorrectionMasks[3].Feather", "Unknown[1].LocalExposure2012"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		if len(text) > 4096 {
			return
		}
		path, err := parseMaskEditPath(text)
		if err != nil {
			return
		}
		canonical, err := parseMaskEditPath(path.String())
		if err != nil || canonical != path {
			t.Fatalf("path did not round trip: %s", text)
		}
		if path.correction < 1 || path.correction > 4096 || path.component < 0 || path.component > 4096 {
			t.Fatal("index range violated")
		}
	})
}
