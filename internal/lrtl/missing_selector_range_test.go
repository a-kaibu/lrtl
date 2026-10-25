package lrtl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"

	"github.com/beevik/etree"
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
	rangeMask := selectorRangeElementAt(t, packet, "MaskGroupBasedCorrections", 1)
	if rangeMask == nil {
		t.Fatal("selector range struct is missing")
	}
	typeValue, _, _ := crsElementValue(rangeMask, "Type")
	amount, _, _ := crsElementValue(rangeMask, "ColorAmount")
	samples, _, _ := crsElementValue(rangeMask, "PointModels")
	return typeValue, amount, samples
}

func selectorRangeElementAt(t *testing.T, packet *XMPPacket, collection string, slot int) *etree.Element {
	t.Helper()
	property := packet.findRootProperty(crsNamespace, collection)
	roots := rdfArrayItems(property.element)
	if slot < 1 || slot > len(roots) {
		t.Fatalf("expected correction slot %d in crs:%s", slot, collection)
	}
	correction := unwrapRDFArrayItem(roots[slot-1])
	maskProperty := directCRSProperty(correction, "CorrectionMasks").element
	masks := rdfArrayItems(maskProperty)
	if len(masks) != 1 {
		t.Fatal("expected one selector")
	}
	return correctionRangeMaskElement(unwrapRDFArrayItem(masks[0]))
}

type selectorRangeObservation struct {
	Collection     string    `json:"collection"`
	Slot           int       `json:"slot"`
	Property       string    `json:"property"`
	Path           string    `json:"path"`
	Changed        bool      `json:"changed"`
	PostStoreError bool      `json:"post_store_error"`
	TargetPresent  bool      `json:"target_range_present"`
	Input          []*string `json:"input"`
	Filled         []string  `json:"filled"`
	Saved          []string  `json:"saved"`
	PointModels    []*string `json:"point_models"`
}

func selectorRangeObservations(t *testing.T) []selectorRangeObservation {
	t.Helper()
	data, err := os.ReadFile("testdata/lrt-missing-selector-range-7.5.5.json")
	if err != nil {
		t.Fatal(err)
	}
	var observations struct {
		Cases []selectorRangeObservation
	}
	if err := json.Unmarshal(data, &observations); err != nil {
		t.Fatal(err)
	}
	if len(observations.Cases) != 80 {
		t.Fatalf("expected eighty installed LRT selector range observations, got %d", len(observations.Cases))
	}
	return observations.Cases
}

func selectorRangeColumnPacket(t *testing.T, observation selectorRangeObservation, value *string, frame int) *XMPPacket {
	t.Helper()
	packet := EmptyXMPPacket()
	_, corrections, err := ensurePacketCRSArray(packet, observation.Collection)
	if err != nil {
		t.Fatal(err)
	}
	correctionItem, err := ensureRDFArrayPosition(corrections, observation.Slot, nil)
	if err != nil {
		t.Fatal(err)
	}
	correction := unwrapRDFArrayItem(correctionItem)
	for name, text := range map[string]string{"What": "Correction", "CorrectionSyncID": fmt.Sprintf("own-correction-%d-%d", observation.Slot, frame), "CorrectionActive": "True", "CorrectionAmount": "1"} {
		if err := setCRSElementValue(correction, name, text); err != nil {
			t.Fatal(err)
		}
	}
	maskProperty, err := ensureCRSArrayProperty(correction, "CorrectionMasks")
	if err != nil {
		t.Fatal(err)
	}
	masks, err := ensureRDFArrayContainer(maskProperty)
	if err != nil {
		t.Fatal(err)
	}
	maskItem, err := ensureRDFArrayPosition(masks, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	mask := unwrapRDFArrayItem(maskItem)
	if err := setCRSElementValue(mask, "What", "Mask/Aggregate"); err != nil {
		t.Fatal(err)
	}
	if err := setCRSElementValue(mask, "MaskSyncID", fmt.Sprintf("own-mask-%d", frame)); err != nil {
		t.Fatal(err)
	}
	if !observation.TargetPresent && frame == 1 {
		return packet
	}
	rangeMask := mask.CreateElement("crs:CorrectionRangeMask")
	rangeMask.CreateAttr("rdf:parseType", "Resource")
	defaultType := "1"
	if observation.Property == "LumMin" || observation.Property == "LumMax" || observation.Property == "LumFeather" {
		defaultType = "2"
	}
	defaults := map[string]string{
		"Type": defaultType, "ColorAmount": ".5", "LumMin": ".1", "LumMax": ".9", "LumFeather": ".2",
	}
	for _, name := range []string{"Type", "ColorAmount", "LumMin", "LumMax", "LumFeather"} {
		fieldValue := defaults[name]
		if name == observation.Property {
			if value == nil {
				continue
			}
			fieldValue = *value
		}
		if err := setCRSElementValue(rangeMask, name, fieldValue); err != nil {
			t.Fatal(err)
		}
	}
	if err := setCRSElementValue(rangeMask, "PointModels", fmt.Sprintf("target-samples-%d", frame)); err != nil {
		t.Fatal(err)
	}
	return packet
}

func TestMissingSelectorRangeFieldsAgainstInstalled755(t *testing.T) {
	for _, observation := range selectorRangeObservations(t) {
		name := fmt.Sprintf("%s/%d/%s/range-present-%t", observation.Collection, observation.Slot, observation.Property, observation.TargetPresent)
		t.Run(name, func(t *testing.T) {
			packets := make([]*XMPPacket, len(observation.Input))
			indexes := make([]map[maskTransitionKey]maskTransitionNode, len(packets))
			for i, input := range observation.Input {
				packets[i] = selectorRangeColumnPacket(t, observation, input, i)
				var err error
				indexes[i], err = indexMaskTransitionNodes(packets[i])
				if err != nil {
					t.Fatal(err)
				}
			}
			prepared := prepareMaskTransitionInputs(indexes)
			key := maskTransitionKey{collection: observation.Collection, correctionIndex: observation.Slot, maskIndex: 1}
			for i, expected := range observation.Filled {
				value, found := prepared.Indexes[i][key].values["RangeMask"+observation.Property]
				want, err := strconv.ParseFloat(expected, 64)
				if err != nil || !found || value != want {
					t.Fatalf("prefilled frame %d = %g, found %v; installed LRT expected %s", i, value, found, expected)
				}
			}
			changes, err := planMaskTransitions(packets, []int{0, 1, 2}, 50)
			if err != nil {
				t.Fatal(err)
			}
			for frame := range packets {
				for _, change := range changes[frame] {
					if err := applyMaskTransition(packets[frame], change); err != nil {
						t.Fatal(err)
					}
				}
				rangeMask := selectorRangeElementAt(t, packets[frame], observation.Collection, observation.Slot)
				if rangeMask == nil {
					t.Fatalf("frame %d selector range struct was not created", frame)
				}
				value, found, err := crsElementValue(rangeMask, observation.Property)
				want, parseErr := strconv.ParseFloat(observation.Saved[frame], 64)
				if err != nil || parseErr != nil || !found {
					t.Fatalf("frame %d saved field missing: %v", frame, err)
				}
				got, parseGotErr := strconv.ParseFloat(value, 64)
				if parseGotErr != nil || got != want {
					t.Fatalf("frame %d saved %s = %q, installed LRT expected %s", frame, observation.Property, value, observation.Saved[frame])
				}
				samples, found, err := crsElementValue(rangeMask, "PointModels")
				wantSamples := observation.PointModels[frame]
				if err != nil || (wantSamples == nil && found) || (wantSamples != nil && (!found || samples != *wantSamples)) {
					if wantSamples == nil {
						t.Fatalf("frame %d PointModels = %q, unexpected value", frame, samples)
					}
					t.Fatalf("frame %d PointModels = %q, want %q", frame, samples, *wantSamples)
				}
			}
		})
	}
}

func TestMissingSelectorRangeInstalledReplay(t *testing.T) {
	jar := os.Getenv("LRTL_NATIVE_JAR")
	if jar == "" {
		t.Skip("set LRTL_NATIVE_JAR for installed selector range replay")
	}
	dir := t.TempDir()
	probeRoot, err := filepath.Abs(filepath.Join("..", "..", "scripts", "probes"))
	if err != nil {
		t.Fatal(err)
	}
	classes := []string{"MetadataPasteOracle.java", "SelectedColumnOracle.java", "RegisteredSaveOracle.java", "MissingSelectorRangeOracle.java"}
	args := []string{"-encoding", "UTF-8", "-d", dir}
	for _, class := range classes {
		args = append(args, filepath.Join(probeRoot, class))
	}
	if output, err := exec.Command("javac", args...).CombinedOutput(); err != nil {
		t.Fatalf("compile selector range observer: %v\n%s", err, output)
	}
	result := filepath.Join(dir, "observations.json")
	classpath := dir + string(os.PathListSeparator) + jar
	output, err := exec.Command("java", "--enable-native-access=ALL-UNNAMED", "-cp", classpath, "lrtlprobe.MissingSelectorRangeOracle", result).CombinedOutput()
	if err != nil {
		t.Fatalf("installed selector range observer: %v\n%s", err, output)
	}
	data, err := os.ReadFile(result)
	if err != nil {
		t.Fatal(err)
	}
	var replay struct {
		Cases []selectorRangeObservation
	}
	if err := json.Unmarshal(data, &replay); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(replay.Cases, selectorRangeObservations(t)) {
		t.Fatal("installed selector range observations changed")
	}
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
	for _, collection := range []string{"GradientBasedCorrections", "CircularGradientBasedCorrections", "PaintBasedCorrections", "MaskGroupBasedCorrections"} {
		for _, slot := range []int{1, 2} {
			t.Run(fmt.Sprintf("%s/%d", collection, slot), func(t *testing.T) {
				frames := cropFixture(t, 3)
				left, right := ".2", ".8"
				observation := selectorRangeObservation{Collection: collection, Slot: slot, Property: "ColorAmount", TargetPresent: false}
				inputs := []*string{&left, nil, &right}
				packets := make([]*XMPPacket, len(inputs))
				for i, input := range inputs {
					packet := selectorRangeColumnPacket(t, observation, input, i)
					packets[i] = packet
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
				rangeMask := selectorRangeElementAt(t, actual, collection, slot)
				if rangeMask == nil {
					t.Fatal("CLI did not create selector range resource")
				}
				typeValue, _, _ := crsElementValue(rangeMask, "Type")
				amount, _, _ := crsElementValue(rangeMask, "ColorAmount")
				if typeValue != "1" || amount != "0.2" {
					t.Fatalf("saved selector range = Type %q, ColorAmount %q", typeValue, amount)
				}
				if _, found, _ := crsElementValue(rangeMask, "PointModels"); found {
					t.Fatal("copied color samples from another frame")
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
			})
		}
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
