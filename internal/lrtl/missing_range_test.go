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

func TestMissingRangeCLIAndRepeat(t *testing.T) {
	cases := missingRangeObservations(t)
	for _, base := range []int{0, 36, 72, 108} {
		for _, offset := range []int{3, 17} {
			n := base + offset
			t.Run(fmt.Sprint(n), func(t *testing.T) {
				frames := cropFixture(t, 3)
				packets := missingRangePackets(t, cases[n])
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
				property := got.findRootProperty(crsNamespace, cases[n].Collection)
				correction := unwrapRDFArrayItem(rdfArrayItems(property.element)[0])
				value, found, err := crsElementValue(correctionRangeMaskElement(correction), cases[n].Property)
				if err != nil || !found {
					t.Fatal("range field missing from saved XMP")
				}
				if cases[n].Property == "LumRange" {
					if value != cases[n].Expected {
						t.Fatalf("saved %s native %s", value, cases[n].Expected)
					}
				} else {
					a, errA := strconv.ParseFloat(value, 64)
					b, errB := strconv.ParseFloat(cases[n].Expected, 64)
					if errA != nil || errB != nil || a != b {
						t.Fatalf("saved %s native %s", value, cases[n].Expected)
					}
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
}

func TestMissingRangeUnknownSamplesAndFailure(t *testing.T) {
	cases := missingRangeObservations(t)
	for _, base := range []int{0, 36, 72, 108} {
		tc := cases[base+3]
		packets := missingRangePackets(t, tc)
		property := packets[1].findRootProperty(crsNamespace, tc.Collection)
		root := unwrapRDFArrayItem(rdfArrayItems(property.element)[0])
		rangeElement := root.CreateElement("crs:CorrectionRangeMask")
		rangeElement.CreateAttr("rdf:parseType", "Resource")
		if err := setCRSElementValue(rangeElement, "PointModels", "own samples"); err != nil {
			t.Fatal(err)
		}
		applySelectedRange(t, packets)
		samples, _, _ := crsElementValue(rangeElement, "PointModels")
		if samples != "own samples" {
			t.Fatal("overwrote own color samples")
		}
	}
	for _, bad := range []string{"text", "array", "attribute"} {
		t.Run(bad, func(t *testing.T) {
			frames := cropFixture(t, 3)
			packets := missingRangePackets(t, cases[3])
			property := packets[2].findRootProperty(crsNamespace, cases[3].Collection)
			root := unwrapRDFArrayItem(rdfArrayItems(property.element)[0])
			rangeElement := correctionRangeMaskElement(root)
			root.RemoveChild(rangeElement)
			switch bad {
			case "attribute":
				root.CreateAttr("crs:CorrectionRangeMask", "invalid")
			case "text":
				root.CreateElement("crs:CorrectionRangeMask").SetText("invalid")
			case "array":
				root.CreateElement("crs:CorrectionRangeMask").CreateElement("rdf:Seq")
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
				t.Fatal("malformed later range accepted")
			}
			if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
				t.Fatal("failure changed XMP/images/backups")
			}
		})
	}
}

func TestMissingRangeTextParentAndSelectorInOnePass(t *testing.T) {
	packets := missingRangePackets(t, missingRangeObservations(t)[125])
	for _, i := range []int{0, 2} {
		property := packets[i].findRootProperty(crsNamespace, "MaskGroupBasedCorrections")
		root := unwrapRDFArrayItem(rdfArrayItems(property.element)[0])
		masksProperty, err := ensureCRSArrayProperty(root, "CorrectionMasks")
		if err != nil {
			t.Fatal(err)
		}
		masks, err := ensureRDFArrayContainer(masksProperty)
		if err != nil {
			t.Fatal(err)
		}
		item, err := ensureRDFArrayPosition(masks, 1, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := setCRSElementValue(item, "ZeroY", []string{".4", "", ".8"}[i]); err != nil {
			t.Fatal(err)
		}
	}
	applySelectedRange(t, packets)
	nodes, err := indexMaskTransitionNodes(packets[1])
	if err != nil {
		t.Fatal(err)
	}
	key := maskTransitionKey{collection: "MaskGroupBasedCorrections", correctionIndex: 1, maskIndex: 1}
	if nodes[key].values["ZeroY"] != .4 {
		t.Fatal("LumRange-only parent did not allow same-pass selector creation")
	}
	changes, err := planMaskTransitions(packets, []int{0, 1, 2}, 50)
	if err != nil || len(changes[1]) != 0 {
		t.Fatal("text parent/selector required another pass")
	}
	assertRDFResourceSyntax(t, packets[1].Document.Root())
}

type missingRangeObservation struct {
	Collection, Property, Mode, Expected, RootContainer, RootID, RootExposure string
	Slot, ItemCount                                                           int
	Input                                                                     []*string
}

func missingRangeObservations(t *testing.T) []missingRangeObservation {
	t.Helper()
	data, err := os.ReadFile("testdata/lrt-missing-range-7.5.5.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []missingRangeObservation
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) != 144 {
		t.Fatal("expected 144 native range observations")
	}
	return cases
}

func missingRangePackets(t *testing.T, tc missingRangeObservation) []*XMPPacket {
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
		for name, text := range map[string]string{"CorrectionSyncID": fmt.Sprint("own-", i), "CorrectionName": "keep"} {
			if err := setCRSElementValue(root, name, text); err != nil {
				t.Fatal(err)
			}
		}
		if tc.Mode != "new-parent" {
			if err := setCRSElementValue(root, "LocalExposure2012", "0.125"); err != nil {
				t.Fatal(err)
			}
		}
		if i == 1 && tc.Mode == "absent" {
			continue
		}
		rangeElement := root.CreateElement("crs:CorrectionRangeMask")
		rangeElement.CreateAttr("rdf:parseType", "Resource")
		if value == nil {
			continue
		}
		for name, text := range map[string]string{tc.Property: *value, "PointModels": "source-only"} {
			if err := setCRSElementValue(rangeElement, name, text); err != nil {
				t.Fatal(err)
			}
		}
	}
	return packets
}

func applySelectedRange(t *testing.T, packets []*XMPPacket) {
	t.Helper()
	changes, err := planMaskTransitions(packets, []int{0, 1, 2}, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes[1]) == 0 {
		t.Fatal("missing selected range was not filled")
	}
	for _, change := range changes[1] {
		if change.templateCorrection != nil || change.templateMask != nil {
			t.Fatal("selected range cloned a template")
		}
		if err := applyMaskTransition(packets[1], change); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMissingRangeAgainstInstalled755(t *testing.T) {
	for n, tc := range missingRangeObservations(t) {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			packets := missingRangePackets(t, tc)
			applySelectedRange(t, packets)
			property := packets[1].findRootProperty(crsNamespace, tc.Collection)
			roots := rdfArrayItems(property.element)
			if len(roots) != tc.ItemCount || rdfArrayContainer(property.element).Tag != tc.RootContainer {
				t.Fatal("native root array mismatch")
			}
			root := unwrapRDFArrayItem(roots[tc.Slot-1])
			id, _, _ := crsElementValue(root, "CorrectionSyncID")
			if id != tc.RootID {
				t.Fatalf("changed parent ID: %s / %s", id, tc.RootID)
			}
			exposure, found, _ := crsElementValue(root, "LocalExposure2012")
			if tc.RootExposure == "" {
				if found {
					t.Fatal("copied parent exposure from another frame")
				}
			} else if exposure != tc.RootExposure {
				t.Fatalf("changed parent exposure: %s / %s", exposure, tc.RootExposure)
			}
			rangeElement := correctionRangeMaskElement(root)
			if rangeElement == nil {
				t.Fatal("missing materialized range struct")
			}
			got, found, err := crsElementValue(rangeElement, tc.Property)
			if err != nil || !found {
				t.Fatal("missing saved range field")
			}
			if tc.Property == "LumRange" {
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
			if _, found, _ := crsElementValue(rangeElement, "PointModels"); found {
				t.Fatal("cloned source color samples")
			}
			text, _, _ := packets[1].GetScalar("dc:description")
			if text != "keep-1" {
				t.Fatal("changed unrelated metadata")
			}
			assertRDFResourceSyntax(t, packets[1].Document.Root())
			changes, err := planMaskTransitions(packets, []int{0, 1, 2}, 50)
			if err != nil || len(changes[1]) != 0 {
				t.Fatalf("repeat changed range: %v %v", changes[1], err)
			}
		})
	}
}
