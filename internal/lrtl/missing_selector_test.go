package lrtl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
)

func TestMissingSelectorCLIAndRepeat(t *testing.T) {
	cases := missingSelectorObservations(t)
	for _, n := range []int{0, 8, 12, 24, 28} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			frames := cropFixture(t, 3)
			packets := missingSelectorPackets(t, cases[n])
			for i, packet := range packets {
				if err := packet.SetScalar("xmp:Rating", "4"); err != nil {
					t.Fatal(err)
				}
				persistExposurePacket(t, frames[i], packet)
			}
			root := filepath.Dir(frames[0].Image)
			actual, err := DiscoverSequence(root)
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
			got, err := PacketForFrame(actual[1])
			if err != nil {
				t.Fatal(err)
			}
			applySelectedSelector(t, packets)
			gotNodes, err := indexMaskTransitionNodes(got)
			if err != nil {
				t.Fatal(err)
			}
			wantNodes, err := indexMaskTransitionNodes(packets[1])
			if err != nil {
				t.Fatal(err)
			}
			key := maskTransitionKey{collection: cases[n].Collection, correctionIndex: 1, maskIndex: 1}
			if !reflect.DeepEqual(gotNodes[key].values, wantNodes[key].values) {
				t.Fatalf("saved selector differs: %v / %v", gotNodes[key].values, wantNodes[key].values)
			}
			if _, found, _ := crsElementValue(unwrapRDFArrayItem(gotNodes[key].maskItem), "MaskSyncID"); found {
				t.Fatal("copied source selector ID")
			}
			backups, err := filepath.Glob(actual[1].Sidecar + "*.bak")
			if err != nil || len(backups) != 1 {
				t.Fatalf("original backup missing: %v %v", backups, err)
			}
			assertPreviewEVFileBytes(t, backups[0], before[actual[1].Sidecar])
			for _, frame := range actual {
				assertPreviewEVFileBytes(t, frame.Image, before[frame.Image])
			}
			after := batchFileSnapshot(t, root)
			if Main([]string{"interpolate", root, "--write"}, &out, &stderr) != 0 {
				t.Fatal(&stderr)
			}
			if !reflect.DeepEqual(after, batchFileSnapshot(t, root)) {
				t.Fatal("repeat changed XMP/backups")
			}
		})
	}
}

func TestMissingSelectorBooleanParentAndEligibility(t *testing.T) {
	for _, tc := range missingSelectorObservations(t) {
		if tc.Mode != "new-parent" {
			continue
		}
		packets := missingSelectorPackets(t, tc)
		for _, i := range []int{0, 2} {
			property := packets[i].findRootProperty(crsNamespace, tc.Collection)
			item := unwrapRDFArrayItem(rdfArrayItems(property.element)[tc.Slot-1])
			field := directCRSProperty(item, "LocalExposure2012")
			if field.element != nil {
				item.RemoveChild(field.element)
			}
			if field.attribute != nil {
				item.RemoveAttr(field.attribute.FullKey())
			}
			if err := setCRSElementValue(item, "CorrectionActive", "False"); err != nil {
				t.Fatal(err)
			}
		}
		applySelectedSelector(t, packets)
		changes, err := planMaskTransitions(packets, []int{0, 1, 2}, 50)
		if err != nil || len(changes[1]) != 0 {
			t.Fatalf("boolean-only parent needs a second pass: %v", err)
		}
	}
	key := maskTransitionKey{collection: "GradientBasedCorrections", correctionIndex: 1, maskIndex: 1}
	root := key
	root.maskIndex = 0
	index := map[maskTransitionKey]maskTransitionNode{root: {values: map[string]float64{"LocalExposure2012": 0}}}
	if !canFillMissingSelector(index, key, "ZeroY") {
		t.Fatal("zero-valued prepared root was not available")
	}
	for _, field := range []string{"Top", "Radius", "RangeMaskLumMin", "LocalExposure2012"} {
		if canFillMissingSelector(index, key, field) {
			t.Fatalf("unregistered linear selector field %s", field)
		}
	}
	for _, bad := range []maskTransitionKey{{collection: key.collection, correctionIndex: 5, maskIndex: 1}, {collection: key.collection, correctionIndex: 1, maskIndex: 2}, {collection: "PaintBasedCorrections", correctionIndex: 1, maskIndex: 1}, {collection: key.collection, correctionID: "custom", maskIndex: 1}} {
		parent := bad
		parent.maskIndex = 0
		available := map[maskTransitionKey]maskTransitionNode{parent: {values: map[string]float64{"LocalExposure2012": 0}}}
		if canFillMissingSelector(available, bad, "ZeroY") {
			t.Fatalf("unregistered selector %+v", bad)
		}
	}
	if canFillMissingSelector(map[maskTransitionKey]maskTransitionNode{}, key, "ZeroY") {
		t.Fatal("selector without any writable parent was allowed")
	}
}

func TestMissingSelectorStructuralFailurePreservesFiles(t *testing.T) {
	for _, bad := range []string{"text", "unexpected-element"} {
		t.Run(bad, func(t *testing.T) {
			frames := cropFixture(t, 3)
			packets := missingSelectorPackets(t, missingSelectorObservations(t)[0])
			property := packets[2].findRootProperty(crsNamespace, "GradientBasedCorrections")
			root := unwrapRDFArrayItem(rdfArrayItems(property.element)[0])
			masks := directCRSProperty(root, "CorrectionMasks").element
			root.RemoveChild(masks)
			masks = root.CreateElement("crs:CorrectionMasks")
			if bad == "text" {
				masks.SetText("invalid array")
			} else {
				masks.CreateElement("crs:Unexpected")
			}
			for i, packet := range packets {
				if err := packet.SetScalar("xmp:Rating", "4"); err != nil {
					t.Fatal(err)
				}
				persistExposurePacket(t, frames[i], packet)
			}
			dir := filepath.Dir(frames[0].Image)
			before := batchFileSnapshot(t, dir)
			var out, stderr bytes.Buffer
			if Main([]string{"interpolate", dir, "--write"}, &out, &stderr) == 0 {
				t.Fatal("malformed later selector array was accepted")
			}
			if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
				t.Fatal("earlier XMP or backups changed before later structural failure")
			}
		})
	}
}

type missingSelectorObservation struct {
	Collection, Property, Mode, Expected, RootContainer, MaskContainer, RootID, RootExposure string
	Slot, ItemCount                                                                          int
	Input                                                                                    []*string
}

func missingSelectorObservations(t *testing.T) []missingSelectorObservation {
	t.Helper()
	data, err := os.ReadFile("testdata/lrt-missing-selector-7.5.5.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []missingSelectorObservation
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) != 40 {
		t.Fatal("expected forty native selector observations")
	}
	return cases
}

func missingSelectorPackets(t *testing.T, tc missingSelectorObservation) []*XMPPacket {
	t.Helper()
	packets := make([]*XMPPacket, len(tc.Input))
	for i, value := range tc.Input {
		packet := EmptyXMPPacket()
		packets[i] = packet
		if err := packet.SetScalar("dc:description", fmt.Sprint("keep-", i)); err != nil {
			t.Fatal(err)
		}
		if i == 1 && tc.Mode == "new-parent" {
			continue
		}
		_, array, err := ensurePacketCRSArray(packet, tc.Collection)
		if err != nil {
			t.Fatal(err)
		}
		item, err := ensureRDFArrayPosition(array, tc.Slot, nil)
		if err != nil {
			t.Fatal(err)
		}
		root := unwrapRDFArrayItem(item)
		for name, text := range map[string]string{"CorrectionSyncID": fmt.Sprint("own-", i), "CorrectionName": "keep", "LocalExposure2012": "0.125"} {
			if err := setCRSElementValue(root, name, text); err != nil {
				t.Fatal(err)
			}
		}
		if i == 1 && tc.Mode == "absent" {
			continue
		}
		property, err := ensureCRSArrayProperty(root, "CorrectionMasks")
		if err != nil {
			t.Fatal(err)
		}
		masks, err := ensureRDFArrayContainer(property)
		if err != nil {
			t.Fatal(err)
		}
		if i == 1 && tc.Mode == "empty-array" {
			continue
		}
		maskItem, err := ensureRDFArrayPosition(masks, 1, nil)
		if err != nil {
			t.Fatal(err)
		}
		if value == nil {
			continue
		}
		for name, text := range map[string]string{tc.Property: *value, "MaskSyncID": fmt.Sprint("source-selector-", i)} {
			if err := setCRSElementValue(unwrapRDFArrayItem(maskItem), name, text); err != nil {
				t.Fatal(err)
			}
		}
	}
	return packets
}

func applySelectedSelector(t *testing.T, packets []*XMPPacket) {
	t.Helper()
	changes, err := planMaskTransitions(packets, []int{0, 1, 2}, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes[1]) == 0 {
		t.Fatal("missing selected selector was not filled")
	}
	for _, change := range changes[1] {
		if change.templateCorrection != nil || change.templateMask != nil {
			t.Fatal("selected selector cloned a template")
		}
		if err := applyMaskTransition(packets[1], change); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMissingSelectorAgainstInstalled755(t *testing.T) {
	for n, tc := range missingSelectorObservations(t) {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			packets := missingSelectorPackets(t, tc)
			applySelectedSelector(t, packets)
			property := packets[1].findRootProperty(crsNamespace, tc.Collection)
			roots := rdfArrayItems(property.element)
			if len(roots) != tc.Slot || rdfArrayContainer(property.element).Tag != tc.RootContainer {
				t.Fatal("native root array mismatch")
			}
			root := unwrapRDFArrayItem(roots[tc.Slot-1])
			id, _, _ := crsElementValue(root, "CorrectionSyncID")
			if id != tc.RootID {
				t.Fatalf("changed own correction ID %s / %s", id, tc.RootID)
			}
			exposure, _, _ := crsElementValue(root, "LocalExposure2012")
			a, errA := strconv.ParseFloat(exposure, 64)
			b, errB := strconv.ParseFloat(tc.RootExposure, 64)
			if errA != nil || errB != nil || a != b {
				t.Fatal("changed parent exposure")
			}
			masks := directCRSProperty(root, "CorrectionMasks").element
			if masks == nil || rdfArrayContainer(masks).Tag != tc.MaskContainer {
				t.Fatal("native selector array mismatch")
			}
			items := rdfArrayItems(masks)
			if len(items) != tc.ItemCount {
				t.Fatal("native selector count mismatch")
			}
			mask := unwrapRDFArrayItem(items[0])
			got, found, err := crsElementValue(mask, tc.Property)
			if err != nil || !found {
				t.Fatal("missing filled selector property")
			}
			if tc.Property == "Flipped" {
				if got != tc.Expected {
					t.Fatalf("got %s native %s", got, tc.Expected)
				}
			} else {
				a, errA := strconv.ParseFloat(got, 64)
				b, errB := strconv.ParseFloat(tc.Expected, 64)
				if errA != nil || errB != nil || a != b {
					t.Fatalf("got %s native %s", got, tc.Expected)
				}
			}
			for _, name := range []string{"MaskSyncID", "What", "MaskName"} {
				if _, found, _ := crsElementValue(mask, name); found {
					t.Fatalf("cloned source selector %s", name)
				}
			}
			text, _, _ := packets[1].GetScalar("dc:description")
			if text != "keep-1" {
				t.Fatal("changed unrelated metadata")
			}
			assertRDFResourceSyntax(t, packets[1].Document.Root())
			changes, err := planMaskTransitions(packets, []int{0, 1, 2}, 50)
			if err != nil || len(changes[1]) != 0 {
				t.Fatalf("repeat generated more changes: %v %v", changes[1], err)
			}
		})
	}
}
