package lrtl

import (
	"bytes"
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func saveInitializationPlan(t *testing.T, changes []initializationChange) {
	t.Helper()
	for _, change := range changes {
		if _, err := WritePacket(change.Frame.OutputSidecar(), change.Packet, false); err != nil {
			t.Fatal(err)
		}
	}
}

func TestInitializeModernAndLegacyLayout(t *testing.T) {
	for _, format := range []string{"modern", "legacy"} {
		t.Run(format, func(t *testing.T) {
			dir := t.TempDir()
			frames := makeFrames(t, dir, 2)
			changes, err := buildInitializationPlan(frames, format)
			if err != nil || len(changes) != 2 {
				t.Fatalf("changes=%d err=%v", len(changes), err)
			}
			ids := map[string]bool{}
			for i, change := range changes {
				if change.CreatedSlots != 9 {
					t.Fatalf("created=%d", change.CreatedSlots)
				}
				packet := change.Packet
				for _, header := range [][2]string{{"Version", "14"}, {"ProcessVersion", "11"}, {"HasSettings", "True"}} {
					value, found, err := packet.GetScalar("crs:" + header[0])
					if err != nil || !found || value != header[1] {
						t.Fatalf("header %s=%s %v", header[0], value, err)
					}
				}
				for slot := 1; slot <= 9; slot++ {
					item := existingInitializationSlot(packet, format, slot)
					if item == nil {
						t.Fatalf("slot %d missing", slot)
					}
					value, found, err := crsElementValue(item, "LocalExposure2012")
					if err != nil || !found || value != "0" {
						t.Fatalf("initial exposure %s %v", value, err)
					}
					masks := rdfArrayItems(directCRSProperty(item, "CorrectionMasks").element)
					if len(masks) != 1 {
						t.Fatalf("mask count=%d", len(masks))
					}
					mask := unwrapRDFArrayItem(masks[0])
					if _, supported, err := parseXMPLocalMask(mask); err != nil || !supported {
						t.Fatalf("generated mask unsupported: slot %d, %v", slot, err)
					}
					kind, _, _ := crsElementValue(mask, "What")
					want := "Mask/Gradient"
					if slot >= 8 {
						want = "Mask/CircularGradient"
					}
					if kind != want {
						t.Fatalf("slot %d type=%s", slot, kind)
					}
					id, found, _ := crsElementValue(mask, "MaskSyncID")
					if !found || len(id) != 32 {
						t.Fatal("missing mask ID")
					}
					if i == 0 {
						if ids[id] {
							t.Fatal("duplicate mask ID")
						}
						ids[id] = true
					} else {
						other := existingInitializationSlot(changes[0].Packet, format, slot)
						first := unwrapRDFArrayItem(rdfArrayItems(directCRSProperty(other, "CorrectionMasks").element)[0])
						wantID, _, _ := crsElementValue(first, "MaskSyncID")
						if id != wantID {
							t.Fatal("sequence mask IDs differ")
						}
					}
					if format == "modern" {
						correctionID, found, _ := crsElementValue(item, "CorrectionSyncID")
						if !found || len(correctionID) != 32 || correctionID == id {
							t.Fatal("invalid correction ID")
						}
					}
				}
				collection := "MaskGroupBasedCorrections"
				if format == "legacy" {
					collection = "GradientBasedCorrections"
				}
				for j, role := range initializationRoles {
					target := findLRTExposureTarget(packet, role, j+5)
					if target.Collection != collection || target.Index != j+5 {
						t.Fatalf("%s target=%v", role, target)
					}
				}
				local, err := readXMPLocalCorrections(packet)
				if err != nil || len(local) != 0 {
					t.Fatalf("local=%d err=%v", len(local), err)
				}
				img := precisionToneImage()
				actual := applyXMPLocalCorrections(img, local)
				if actual != img {
					t.Fatal("neutral initialized masks should not allocate or alter pixels")
				}
			}
			saveInitializationPlan(t, changes)
			frames, err = DiscoverSequence(dir)
			if err != nil {
				t.Fatal(err)
			}
			again, err := buildInitializationPlan(frames, "auto")
			if err != nil || len(again) != 0 {
				t.Fatalf("not idempotent: %d %v", len(again), err)
			}
		})
	}
}

func TestInitializeRetainsEditsAndAdoptsExistingIDs(t *testing.T) {
	dir := t.TempDir()
	frames := makeFrames(t, dir, 2)
	first, err := buildInitializationPlan(frames[:1], "modern")
	if err != nil {
		t.Fatal(err)
	}
	packet := first[0].Packet
	packet.SetScalar("crs:Exposure2012", "1.25")
	packet.SetScalar("crs:ProcessVersion", "15.4")
	packet.SetScalar("xmp:Rating", "5")
	item := existingInitializationSlot(packet, "modern", 1)
	setCRSElementValue(item, "LocalExposure2012", "0.5")
	setXMPExposureTargetValue(packet, xmpExposureTarget{"MaskGroupBasedCorrections", 5}, .3)
	packet.rootDescriptions()[0].CreateAttr("xmlns:vendor", "urn:lrtl:init:test")
	packet.rootDescriptions()[0].CreateElement("vendor:Note").SetText("preserve me")
	saveInitializationPlan(t, first)
	frames, err = DiscoverSequence(dir)
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(frames[0].Sidecar)
	if err != nil {
		t.Fatal(err)
	}
	changes, err := buildInitializationPlan(frames, "auto")
	if err != nil || len(changes) != 1 {
		t.Fatalf("changes=%d err=%v", len(changes), err)
	}
	if changes[0].Frame.Index != 1 {
		t.Fatal("existing edited packet was changed")
	}
	newSlot := existingInitializationSlot(changes[0].Packet, "modern", 1)
	newID, _, _ := crsElementValue(newSlot, "CorrectionSyncID")
	oldID, _, _ := crsElementValue(item, "CorrectionSyncID")
	if newID != oldID {
		t.Fatal("new frame did not adopt existing slot identity")
	}
	unchanged, err := os.ReadFile(frames[0].Sidecar)
	if err != nil || !bytes.Equal(original, unchanged) {
		t.Fatal("existing XMP changed")
	}
}

func TestInitializePreflightsConflictsBeforeAnyWrite(t *testing.T) {
	dir := t.TempDir()
	frames := makeFrames(t, dir, 2)
	packet := exposureTestPacket(t, "unsigned")
	frame := persistExposurePacket(t, frames[1], packet)
	original, err := os.ReadFile(frame.Sidecar)
	if err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	if code := Main([]string{"init", dir, "--write"}, &out, &stderr); code == 0 || !strings.Contains(stderr.String(), "unrelated correction") {
		t.Fatalf("expected conflict, code=%d error=%s", code, stderr.String())
	}
	if _, err := os.Stat(frames[0].OutputSidecar()); !os.IsNotExist(err) {
		t.Fatal("earlier frame written before conflict preflight finished")
	}
	unchanged, err := os.ReadFile(frame.Sidecar)
	if err != nil || !bytes.Equal(unchanged, original) {
		t.Fatal("conflicting XMP overwritten")
	}
}

func TestInitializeCLIPreviewWriteBackupAndEmbeddedMetadata(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "frame.jpg")
	xmp := []byte(`<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"><rdf:Description rdf:about="" xmlns:v="urn:lrtl:init" xmlns:crs="http://ns.adobe.com/camera-raw-settings/1.0/" crs:Exposure2012="0.5"><v:Note>embedded stays</v:Note></rdf:Description></rdf:RDF></x:xmpmeta>`)
	original := writeEmbeddedTransitionJPEG(t, path, xmp)
	var out, stderr bytes.Buffer
	if code := Main([]string{"init", dir, "--mask-format", "modern"}, &out, &stderr); code != 0 {
		t.Fatalf("preview %d %s", code, stderr.String())
	}
	if _, err := os.Stat(path + ".xmp"); !os.IsNotExist(err) {
		t.Fatal("preview wrote XMP")
	}
	if code := Main([]string{"init", dir, "--write"}, &out, &stderr); code != 0 {
		t.Fatalf("write %d %s", code, stderr.String())
	}
	packet, err := PacketForSidecar(path + ".xmp")
	if err != nil {
		t.Fatal(err)
	}
	assertExposureValue(t, packet, globalXMPExposureTarget(), .5)
	data, err := os.ReadFile(path + ".xmp")
	if err != nil || !bytes.Contains(data, []byte("embedded stays")) {
		t.Fatal("embedded metadata lost")
	}
	unchanged, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(unchanged, original) {
		t.Fatal("JPEG was modified")
	}
	// An existing sidecar receives a backup when missing slots are added.
	packet.findRootProperty(crsNamespace, "MaskGroupBasedCorrections").element.Parent().RemoveChild(packet.findRootProperty(crsNamespace, "MaskGroupBasedCorrections").element)
	if _, err := WritePacket(path+".xmp", packet, false); err != nil {
		t.Fatal(err)
	}
	if code := Main([]string{"init", dir, "--write"}, &out, &stderr); code != 0 {
		t.Fatalf("backup write %d %s", code, stderr.String())
	}
	backups, err := filepath.Glob(path + ".xmp.*.bak")
	if err != nil || len(backups) != 1 {
		t.Fatalf("backups=%d %v", len(backups), err)
	}
}

func TestInitializeThenDeflickerKeepsGlobalExposureSeparate(t *testing.T) {
	dir := t.TempDir()
	frames := makeFrames(t, dir, 3)
	changes, err := buildInitializationPlan(frames, "modern")
	if err != nil {
		t.Fatal(err)
	}
	saveInitializationPlan(t, changes)
	frames, err = DiscoverSequence(dir)
	if err != nil {
		t.Fatal(err)
	}
	corrections, err := buildDeflickerChanges(frames, []float64{.25, .5, .25}, 1, 1, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := applyDeflickerChanges(corrections); err != nil {
		t.Fatal(err)
	}
	if len(corrections) != 1 || corrections[0].Target != (xmpExposureTarget{"MaskGroupBasedCorrections", 6}) {
		t.Fatalf("wrong correction target: %v", corrections)
	}
	packet := corrections[0].Packet
	if _, found, _ := packet.GetScalar("crs:Exposure2012"); found {
		t.Fatal("global exposure was created")
	}
	local, err := readXMPLocalCorrections(packet)
	if err != nil {
		t.Fatal(err)
	}
	// Managed slot is excluded from local rendering; other eight neutral slots are omitted.
	if len(local) != 0 {
		t.Fatalf("local count=%d", len(local))
	}
	value, _, err := readDeflickerRenderCorrection(packet)
	if err != nil || value > -.69999 || value < -.70001 {
		t.Fatalf("correction=%g %v", value, err)
	}
	// A non-managed internal slot must still cover the entire image.
	setXMPExposureTargetValue(packet, xmpExposureTarget{"MaskGroupBasedCorrections", 7}, 1)
	local, err = readXMPLocalCorrections(packet)
	if err != nil {
		t.Fatal(err)
	}
	img := precisionToneImage()
	rendered := applyXMPLocalCorrections(img, local)
	for _, p := range [][2]int{{7, 11}, {38, 26}} {
		before := color.NRGBA64Model.Convert(img.At(p[0], p[1])).(color.NRGBA64)
		after := color.NRGBA64Model.Convert(rendered.At(p[0], p[1])).(color.NRGBA64)
		if after.G <= before.G {
			t.Fatalf("internal full-frame gradient misses %v", p)
		}
	}
}

func TestInitializeRejectsWrongFormatsAndMovedInternalSlots(t *testing.T) {
	frames := makeFrames(t, t.TempDir(), 1)
	if _, err := buildInitializationPlan(frames, "other"); err == nil {
		t.Fatal("invalid format accepted")
	}
	if _, err := buildInitializationPlan(nil, "auto"); err == nil {
		t.Fatal("empty sequence accepted")
	}
	packet := exposureTestPacket(t, "modern")
	persistExposurePacket(t, frames[0], packet)
	if _, err := buildInitializationPlan(frames, "modern"); err == nil {
		t.Fatal("moved internal slots accepted")
	}
	for _, format := range []string{"modern", "legacy"} {
		t.Run(format, func(t *testing.T) {
			dir := t.TempDir()
			frames := makeFrames(t, dir, 1)
			changes, err := buildInitializationPlan(frames, format)
			if err != nil {
				t.Fatal(err)
			}
			saveInitializationPlan(t, changes)
			other := "modern"
			if format == "modern" {
				other = "legacy"
			}
			if _, err := buildInitializationPlan(frames, other); err == nil {
				t.Fatalf("duplicate opposite-format internal roles created: %s -> %s", format, other)
			}
		})
	}
}

func TestInitializeKeyframesTransitionDeflickerRenderWorkflow(t *testing.T) {
	dir := t.TempDir()
	originals := map[string][]byte{}
	for i := 0; i < 3; i++ {
		path := filepath.Join(dir, fmt.Sprintf("frame%d.png", i))
		writeRenderFrame(t, path, "png", 32, 24)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		originals[path] = data
	}
	commands := [][]string{
		{"init", dir, "--write"},
		{"keyframes", dir, "--count", "2", "--evenly-spaced", "--write"},
		{"set", filepath.Join(dir, "frame0.png"), "Exposure2012", "0"},
		{"set", filepath.Join(dir, "frame2.png"), "Exposure2012", "1"},
		{"interpolate", dir, "--smoothness", "50", "--write"},
		{"deflicker", dir, "--apply-xmp-basic", "--constant", "--write"},
		{"render", dir, "--apply-xmp-basic", "--output", filepath.Join(dir, "result.avi")},
	}
	for _, args := range commands {
		var out, stderr bytes.Buffer
		if code := Main(args, &out, &stderr); code != 0 {
			t.Fatalf("%s failed (%d): %s", args[0], code, stderr.String())
		}
	}
	frames, err := DiscoverSequence(dir)
	if err != nil {
		t.Fatal(err)
	}
	for i, frame := range frames {
		packet, err := PacketForFrame(frame)
		if err != nil {
			t.Fatal(err)
		}
		assertExposureValue(t, packet, globalXMPExposureTarget(), float64(i)*.5)
		assertExposureValue(t, packet, xmpExposureTarget{"MaskGroupBasedCorrections", 5}, 0)
	}
	last, err := PacketForFrame(frames[2])
	if err != nil {
		t.Fatal(err)
	}
	target, found, err := last.GetScalar(deflickerTargetProperty)
	if err != nil || !found || target != "MaskGroupBasedCorrections:6" {
		t.Fatalf("Deflicker target=%s %v", target, err)
	}
	video, err := os.ReadFile(filepath.Join(dir, "result.avi"))
	if err != nil || len(video) < 12 || string(video[:4]) != "RIFF" {
		t.Fatal("workflow did not produce AVI")
	}
	for path, original := range originals {
		data, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(data, original) {
			t.Fatal("workflow modified source image")
		}
	}
}

func TestInitializeRejectsMalformedArraysBeforeWriting(t *testing.T) {
	for _, body := range []string{
		`<crs:MaskGroupBasedCorrections>text</crs:MaskGroupBasedCorrections>`,
		`<crs:MaskGroupBasedCorrections><crs:Unknown/></crs:MaskGroupBasedCorrections>`,
	} {
		t.Run(body, func(t *testing.T) {
			dir := t.TempDir()
			frames := makeFrames(t, dir, 2)
			packet, err := parseXMP([]byte(`<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"><rdf:Description rdf:about="" xmlns:crs="http://ns.adobe.com/camera-raw-settings/1.0/">` + body + `</rdf:Description></rdf:RDF></x:xmpmeta>`))
			if err != nil {
				t.Fatal(err)
			}
			persistExposurePacket(t, frames[1], packet)
			var out, stderr bytes.Buffer
			if code := Main([]string{"init", dir, "--write"}, &out, &stderr); code == 0 {
				t.Fatal("malformed array accepted")
			}
			if _, err := os.Stat(frames[0].OutputSidecar()); !os.IsNotExist(err) {
				t.Fatal("sidecar created before preflight finished")
			}
		})
	}
}
