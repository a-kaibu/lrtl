package lrtl

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

type maskAngleObservation struct {
	Mode   int
	Input  *float64
	Values map[string]*float64
}

func maskAngleObservations(t *testing.T) []maskAngleObservation {
	t.Helper()
	data, err := os.ReadFile("testdata/lrt-mask-angle-7.5.5.json")
	if err != nil {
		t.Fatal(err)
	}
	var observations []maskAngleObservation
	if err := json.Unmarshal(data, &observations); err != nil {
		t.Fatal(err)
	}
	if len(observations) != 24 {
		t.Fatalf("expected 24 native cases, got %d", len(observations))
	}
	for _, observation := range observations {
		if len(observation.Values) != 6 {
			t.Fatal("incomplete native column observations")
		}
	}
	return observations
}

// Each packet uses one native layout. Native K(Rsa) selects one layout from the
// sequence's mask version; mixed-layout dispatch is outside this comparison.
func maskAnglePacket(t *testing.T, tc maskAngleObservation, form int) *XMPPacket {
	t.Helper()
	collection := "CircularGradientBasedCorrections"
	slots := 2
	if tc.Mode == 2 {
		collection, slots = "MaskGroupBasedCorrections", 10
	}
	var tree strings.Builder
	fmt.Fprintf(&tree, `<crs:%s><rdf:Seq>`, collection)
	for slot := 1; slot <= slots; slot++ {
		angle := ""
		if tc.Input != nil {
			value := strconv.FormatFloat(*tc.Input, 'g', -1, 64)
			if form == 0 {
				angle = ` crs:Angle="` + value + `"`
			} else {
				angle = `<crs:Angle>` + value + `</crs:Angle>`
			}
		}
		fmt.Fprintf(&tree, `<rdf:li><rdf:Description crs:CorrectionSyncID="own-root-%d"><crs:CorrectionMasks><rdf:Seq>`, slot)
		switch form {
		case 0:
			fmt.Fprintf(&tree, `<rdf:li><rdf:Description crs:MaskSyncID="own-mask-%d" crs:MaskValue="1"%s/></rdf:li>`, slot, angle)
		case 1:
			fmt.Fprintf(&tree, `<rdf:li rdf:parseType="Resource"><crs:MaskSyncID>own-mask-%d</crs:MaskSyncID><crs:MaskValue>1</crs:MaskValue>%s</rdf:li>`, slot, angle)
		case 2:
			fmt.Fprintf(&tree, `<rdf:li><rdf:Description crs:MaskSyncID="own-mask-%d" crs:MaskValue="1">%s</rdf:Description></rdf:li>`, slot, angle)
		}
		tree.WriteString(`</rdf:Seq></crs:CorrectionMasks></rdf:Description></rdf:li>`)
	}
	fmt.Fprintf(&tree, `</rdf:Seq></crs:%s>`, collection)
	packet, err := parseXMP([]byte(`<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"><rdf:Description rdf:about="" xmlns:crs="http://ns.adobe.com/camera-raw-settings/1.0/">` + tree.String() + `</rdf:Description></rdf:RDF></x:xmpmeta>`))
	if err != nil {
		t.Fatal(err)
	}
	if err := packet.SetScalar("dc:description", "preserve unrelated metadata"); err != nil {
		t.Fatal(err)
	}
	return packet
}

func assertMaskAngleObservation(t *testing.T, packet *XMPPacket, tc maskAngleObservation) {
	t.Helper()
	nodes, err := indexMaskTransitionNodes(packet)
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for path, expected := range tc.Values {
		collection := "CircularGradientBasedCorrections"
		if tc.Mode == 2 {
			collection = "MaskGroupBasedCorrections"
		}
		if !strings.HasPrefix(path, collection+"[") {
			continue
		}
		var slot int
		if _, err := fmt.Sscanf(strings.TrimPrefix(path, collection), "[%d]", &slot); err != nil {
			t.Fatal(err)
		}
		key := maskTransitionKey{collection: collection, correctionIndex: slot, maskIndex: 1}
		node, found := nodes[key]
		if !found {
			t.Fatalf("missing own selector %s", path)
		}
		angle, found := node.values["Angle"]
		if expected == nil {
			if found {
				t.Fatalf("created absent angle %s", path)
			}
		} else if !found || angle != *expected {
			t.Fatalf("%s: saved %g, native %g", path, angle, *expected)
		}
		id, found, err := crsElementValue(unwrapRDFArrayItem(node.maskItem), "MaskSyncID")
		if err != nil || !found || id != fmt.Sprint("own-mask-", slot) {
			t.Fatalf("lost selector ID: %q %v", id, err)
		}
		checked++
	}
	if checked != 2 && checked != 4 {
		t.Fatalf("incomplete comparisons: %d", checked)
	}
	if text, found, err := packet.GetScalar("dc:description"); err != nil || !found || text != "preserve unrelated metadata" {
		t.Fatal("changed unrelated metadata")
	}
	assertRDFResourceSyntax(t, packet.Document.Root())
}

func TestMaskAngleAgainstInstalled755(t *testing.T) {
	for n, tc := range maskAngleObservations(t) {
		for form := 0; form < 3; form++ {
			t.Run(fmt.Sprintf("%d/form%d", n, form), func(t *testing.T) {
				packet := maskAnglePacket(t, tc, form)
				format := "legacy"
				if tc.Mode == 2 {
					format = "modern"
				}
				changes, err := planMaskTransitionsWithFormat([]*XMPPacket{packet}, []int{0}, 50, format)
				if err != nil {
					t.Fatal(err)
				}
				for _, change := range changes[0] {
					if err := applyMaskTransition(packet, change); err != nil {
						t.Fatal(err)
					}
				}
				data, err := packet.Serialize()
				if err != nil {
					t.Fatal(err)
				}
				saved, err := parseXMP(data)
				if err != nil {
					t.Fatal(err)
				}
				assertMaskAngleObservation(t, saved, tc)
			})
		}
	}
}

func TestMaskAngleCLIOriginalBackupsAndImages(t *testing.T) {
	for _, tc := range maskAngleObservations(t) {
		// Values outside +-135 intentionally need more than one native pass.
		if tc.Input == nil || *tc.Input != 45.001 && *tc.Input != -45.001 {
			continue
		}
		t.Run(fmt.Sprintf("%d/%g", tc.Mode, *tc.Input), func(t *testing.T) {
			format := "legacy"
			if tc.Mode == 2 {
				format = "modern"
			}
			frames := cropFixture(t, 3)
			dir := filepath.Dir(frames[0].Image)
			for i, frame := range frames {
				packet := maskAnglePacket(t, tc, i)
				if err := packet.SetScalar("xmp:Rating", "4"); err != nil {
					t.Fatal(err)
				}
				persistExposurePacket(t, frame, packet)
			}
			before := batchFileSnapshot(t, dir)
			markerCommand(t, "interpolate", dir, "--mask-format", format)
			if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
				t.Fatal("dry run wrote source")
			}
			markerCommand(t, "interpolate", dir, "--mask-format", format, "--write")
			for _, frame := range frames {
				packet, err := PacketForFrame(frame)
				if err != nil {
					t.Fatal(err)
				}
				assertMaskAngleObservation(t, packet, tc)
				assertPreviewEVFileBytes(t, frame.Image, before[frame.Image])
				backups, err := filepath.Glob(frame.Sidecar + "*.bak")
				if err != nil || len(backups) != 1 {
					t.Fatalf("expected original backup: %v %v", backups, err)
				}
				assertPreviewEVFileBytes(t, backups[0], before[frame.Sidecar])
			}
			after := batchFileSnapshot(t, dir)
			markerCommand(t, "interpolate", dir, "--mask-format", format, "--write")
			if !reflect.DeepEqual(after, batchFileSnapshot(t, dir)) {
				t.Fatal("stable angles created more writes/backups")
			}
		})
	}
}
