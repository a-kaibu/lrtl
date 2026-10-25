package lrtl

import (
	"bytes"
	"path/filepath"
	"reflect"
	"testing"
)

func selectorRangeFixture(t *testing.T, amount string, ownSamples string) *XMPPacket {
	t.Helper()
	packet := localMissingPacket(t, "MaskGroupBasedCorrections", nil, nil)
	correction := maskGroupCorrectionAt(packet, 1)
	maskProperty := directCRSProperty(correction, "CorrectionMasks").element
	masks := rdfArrayItems(maskProperty)
	if len(masks) != 1 {
		t.Fatal("expected one selector")
	}
	mask := unwrapRDFArrayItem(masks[0])
	rangeMask := mask.CreateElement("crs:CorrectionRangeMask")
	rangeMask.CreateAttr("rdf:parseType", "Resource")
	if amount != "" {
		if err := setCRSElementValue(rangeMask, "Type", "1"); err != nil {
			t.Fatal(err)
		}
		if err := setCRSElementValue(rangeMask, "ColorAmount", amount); err != nil {
			t.Fatal(err)
		}
	}
	if ownSamples != "" {
		if err := setCRSElementValue(rangeMask, "PointModels", ownSamples); err != nil {
			t.Fatal(err)
		}
	}
	return packet
}

func selectorRangeValues(t *testing.T, packet *XMPPacket) (string, string, string) {
	t.Helper()
	correction := maskGroupCorrectionAt(packet, 1)
	maskProperty := directCRSProperty(correction, "CorrectionMasks").element
	masks := rdfArrayItems(maskProperty)
	if len(masks) != 1 {
		t.Fatal("expected one selector")
	}
	rangeMask := correctionRangeMaskElement(unwrapRDFArrayItem(masks[0]))
	if rangeMask == nil {
		t.Fatal("selector range struct is missing")
	}
	typeValue, _, _ := crsElementValue(rangeMask, "Type")
	amount, _, _ := crsElementValue(rangeMask, "ColorAmount")
	samples, _, _ := crsElementValue(rangeMask, "PointModels")
	return typeValue, amount, samples
}

func TestMissingSelectorRangeFieldsArePrefilledOnExistingResource(t *testing.T) {
	packets := []*XMPPacket{
		selectorRangeFixture(t, ".2", "left samples"),
		selectorRangeFixture(t, "", "target samples"),
		selectorRangeFixture(t, ".8", "right samples"),
	}
	changes, err := planMaskTransitions(packets, []int{0, 1, 2}, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes[1]) == 0 {
		t.Fatal("missing selector range values were not planned")
	}
	for _, change := range changes[1] {
		if err := applyMaskTransition(packets[1], change); err != nil {
			t.Fatal(err)
		}
	}
	typeValue, amount, samples := selectorRangeValues(t, packets[1])
	if typeValue != "1" || amount != "0.2" {
		t.Fatalf("selected missing values = Type %q, ColorAmount %q; want 1 and 0.2", typeValue, amount)
	}
	if samples != "target samples" {
		t.Fatalf("overwrote target PointModels: %q", samples)
	}
	again, err := planMaskTransitions(packets, []int{0, 1, 2}, 50)
	if err != nil || len(again[1]) != 0 {
		t.Fatalf("repeat produced changes: %v %v", again[1], err)
	}
}

func TestMissingSelectorRangeCLIAndRepeat(t *testing.T) {
	frames := cropFixture(t, 3)
	packets := []*XMPPacket{
		selectorRangeFixture(t, ".2", "left samples"),
		selectorRangeFixture(t, "", "target samples"),
		selectorRangeFixture(t, ".8", "right samples"),
	}
	for i, packet := range packets {
		if err := packet.SetScalar("xmp:Rating", "4"); err != nil {
			t.Fatal(err)
		}
		persistExposurePacket(t, frames[i], packet)
	}
	root := filepath.Dir(frames[0].Image)
	sequence, err := DiscoverSequence(root)
	if err != nil {
		t.Fatal(err)
	}
	before := batchFileSnapshot(t, root)
	var out, stderr bytes.Buffer
	if Main([]string{"interpolate", root}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, root)) {
		t.Fatal("dry run wrote files")
	}
	if Main([]string{"interpolate", root, "--write"}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	actual, err := PacketForFrame(sequence[1])
	if err != nil {
		t.Fatal(err)
	}
	typeValue, amount, samples := selectorRangeValues(t, actual)
	if typeValue != "1" || amount != "0.2" || samples != "target samples" {
		t.Fatalf("saved selector range = (%q, %q, %q)", typeValue, amount, samples)
	}
	backups, err := filepath.Glob(sequence[1].Sidecar + "*.bak")
	if err != nil || len(backups) != 1 {
		t.Fatalf("original backup missing: %v %v", backups, err)
	}
	assertPreviewEVFileBytes(t, backups[0], before[sequence[1].Sidecar])
	for _, frame := range sequence {
		assertPreviewEVFileBytes(t, frame.Image, before[frame.Image])
	}
	after := batchFileSnapshot(t, root)
	if Main([]string{"interpolate", root, "--write"}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	if !reflect.DeepEqual(after, batchFileSnapshot(t, root)) {
		t.Fatal("repeat changed XMP/backups")
	}
}

func TestMalformedSelectorRangeFieldDoesNotPartiallyWrite(t *testing.T) {
	frames := cropFixture(t, 3)
	packets := []*XMPPacket{
		selectorRangeFixture(t, ".2", "left samples"),
		selectorRangeFixture(t, "", "target samples"),
		selectorRangeFixture(t, ".8", "right samples"),
	}
	mask := unwrapRDFArrayItem(rdfArrayItems(directCRSProperty(maskGroupCorrectionAt(packets[1], 1), "CorrectionMasks").element)[0])
	rangeMask := correctionRangeMaskElement(mask)
	bad := rangeMask.CreateElement("crs:ColorAmount")
	bad.CreateElement("rdf:Seq").CreateElement("rdf:li").SetText(".4")
	for i, packet := range packets {
		if err := packet.SetScalar("xmp:Rating", "4"); err != nil {
			t.Fatal(err)
		}
		persistExposurePacket(t, frames[i], packet)
	}
	root := filepath.Dir(frames[0].Image)
	before := batchFileSnapshot(t, root)
	var out, stderr bytes.Buffer
	if Main([]string{"interpolate", root, "--write"}, &out, &stderr) == 0 {
		t.Fatal("structured selector ColorAmount was accepted")
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, root)) {
		t.Fatal("invalid selector range changed XMP/images/backups")
	}
}
