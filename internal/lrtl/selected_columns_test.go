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
	"strings"
	"testing"
)

type selectedColumnObservation struct {
	Property, Source string
	Cell, Stored     *string
}

func TestSelectedColumnNativePathCreation(t *testing.T) {
	var rows []struct{ Property, Destination, Source, Expected, Error string }
	if err := json.Unmarshal(denoiseRead(t, "testdata/lrt-selected-column-paths-7.5.5.json"), &rows); err != nil || len(rows) != 9 {
		t.Fatal("native paths reference", err)
	}
	for i, row := range rows {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			columns, err := parseSelectedColumns(row.Property)
			if err != nil {
				t.Fatal(err)
			}
			target := pasteTestPacket(t, row.Destination)
			before, _ := target.Serialize()
			_, err = copySelectedColumnMetadata(target, pasteTestPacket(t, row.Source), columns)
			if !strings.Contains(row.Error, "LRTimelapseMain.m()") {
				if err == nil {
					t.Fatal("accepted path that native storage cannot create", row.Error)
				}
				after, _ := target.Serialize()
				if !bytes.Equal(before, after) {
					t.Fatal("failed preparation changed destination")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			actualNodes, err := pastePacketNodes(target)
			if err != nil {
				t.Fatal(err)
			}
			expectedNodes, err := pastePacketNodes(pasteTestPacket(t, row.Expected))
			if err != nil {
				t.Fatal(err)
			}
			name := columns[0].Steps[0].Name
			actual, expected := selectedField(actualNodes, name), selectedField(expectedNodes, name)
			// Full storage also rewrites cached HG/deflicker values in sibling
			// items. This observation covers preparation of the selected branch;
			// the unrelated save stages are not part of column-cell copying.
			actual = selectedTestBranch(actual, columns[0].Steps)
			expected = selectedTestBranch(expected, columns[0].Steps)
			if actual == nil || expected == nil {
				t.Fatal("missing native selected branch")
			}
			if actual == nil || expected == nil || !reflect.DeepEqual(pasteTestCanonical([]*pasteNode{actual}), pasteTestCanonical([]*pasteNode{expected})) {
				a, _ := json.MarshalIndent(pasteTestCanonical([]*pasteNode{actual}), "", " ")
				b, _ := json.MarshalIndent(pasteTestCanonical([]*pasteNode{expected}), "", " ")
				t.Fatalf("created root differs from native storage\ngot%s\nwant%s", a, b)
			}
		})
	}
}

func TestSelectedColumnsInstalledComponents(t *testing.T) {
	jar := os.Getenv("LRTL_NATIVE_JAR")
	if jar == "" {
		t.Skip("set LRTL_NATIVE_JAR for installed registry/column replay")
	}
	dir := t.TempDir()
	probeRoot, err := filepath.Abs(filepath.Join("..", "..", "scripts", "probes"))
	if err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("javac", "-encoding", "UTF-8", "-d", dir, filepath.Join(probeRoot, "MetadataPasteOracle.java"), filepath.Join(probeRoot, "SelectedColumnOracle.java")).CombinedOutput(); err != nil {
		t.Fatalf("compile original probe: %v\n%s", err, out)
	}
	result := filepath.Join(dir, "observations.json")
	if out, err := exec.Command("java", "--enable-native-access=ALL-UNNAMED", "-cp", dir+string(os.PathListSeparator)+jar, "lrtlprobe.SelectedColumnOracle", result).CombinedOutput(); err != nil {
		t.Fatalf("native observation: %v\n%s", err, out)
	}
	var replay struct {
		Registry []selectedColumn
		Cases    []selectedColumnObservation
	}
	if err := json.Unmarshal(denoiseRead(t, result), &replay); err != nil {
		t.Fatal(err)
	}
	var expectedRegistry []selectedColumn
	if err := json.Unmarshal(selectedColumnsJSON, &expectedRegistry); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(replay.Registry, expectedRegistry) {
		t.Fatal("installed registered column definitions differ")
	}
	if !reflect.DeepEqual(replay.Cases, selectedColumnObservations(t)) {
		t.Fatal("installed cached/stored cells differ")
	}
	// Feed Go-generated scalar results back through the installed XMP parser.
	registry, err := selectedColumnRegistry()
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]selectedColumn{}
	for _, column := range registry {
		byName[column.Property] = column
	}
	var expected []*XMPPacket
	for _, row := range replay.Cases {
		column := byName[row.Property]
		if column.Index < 5 || row.Stored == nil {
			continue
		}
		source := pasteTestPacket(t, row.Source)
		target := pasteTestPacket(t, row.Source)
		if _, err := copySelectedColumnMetadata(target, source, []selectedColumn{column}); err != nil {
			t.Fatal(err)
		}
		data, err := target.Serialize()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("case%04d.input.xmp", len(expected))), data, 0600); err != nil {
			t.Fatal(err)
		}
		expected = append(expected, target)
	}
	if out, err := exec.Command("java", "-cp", dir+string(os.PathListSeparator)+jar, "lrtlprobe.MetadataPasteOracle", "--roundtrip", dir).CombinedOutput(); err != nil {
		t.Fatalf("native parse of Go column packets: %v\n%s", err, out)
	}
	for i, want := range expected {
		actual := pasteTestPacket(t, string(denoiseRead(t, filepath.Join(dir, fmt.Sprintf("case%04d.native.xmp", i)))))
		data, err := want.Serialize()
		if err != nil {
			t.Fatal(err)
		}
		assertPastedMetadata(t, actual, string(data))
	}
}

func TestSelectedColumnRetainsMaskIdentityAndGeometry(t *testing.T) {
	var source *XMPPacket
	for _, row := range metadataPasteObservations(t) {
		if row.Name == "crs-structures" {
			source = pasteTestPacket(t, row.Source)
		}
	}
	if source == nil {
		t.Fatal("missing original structured fixture")
	}
	data, err := source.Serialize()
	if err != nil {
		t.Fatal(err)
	}
	target := pasteTestPacket(t, string(data))
	correction := unwrapRDFArrayItem(rdfArrayItems(target.findRootProperty(crsNamespace, "MaskGroupBasedCorrections").element)[0])
	for name, value := range map[string]string{"CorrectionSyncID": "recipient-id", "LocalExposure2012": "-1", "CorrectionName": "recipient name"} {
		if err := setCRSElementValue(correction, name, value); err != nil {
			t.Fatal(err)
		}
	}
	mask := unwrapRDFArrayItem(rdfArrayItems(directCRSProperty(correction, "CorrectionMasks").element)[0])
	if err := setCRSElementValue(mask, "Top", "0.35"); err != nil {
		t.Fatal(err)
	}
	columns, err := parseSelectedColumns("MaskGroupBasedCorrections[1].LocalExposure2012")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := copySelectedColumnMetadata(target, source, columns); err != nil {
		t.Fatal(err)
	}
	correction = unwrapRDFArrayItem(rdfArrayItems(target.findRootProperty(crsNamespace, "MaskGroupBasedCorrections").element)[0])
	for name, want := range map[string]string{"CorrectionSyncID": "recipient-id", "LocalExposure2012": "0.500000", "CorrectionName": "recipient name"} {
		got, found, err := crsElementValue(correction, name)
		if err != nil || !found || got != want {
			t.Fatal("changed correction identity or scalar", name, got, err)
		}
	}
	mask = unwrapRDFArrayItem(rdfArrayItems(directCRSProperty(correction, "CorrectionMasks").element)[0])
	if got, _, _ := crsElementValue(mask, "Top"); got != "0.35" {
		t.Fatal("copied unselected geometry", got)
	}
}

func TestSelectedColumnNativeOrderCreatesParentBeforeMask(t *testing.T) {
	var rows []struct{ Source string }
	if err := json.Unmarshal(denoiseRead(t, "testdata/lrt-selected-column-paths-7.5.5.json"), &rows); err != nil {
		t.Fatal(err)
	}
	source := pasteTestPacket(t, rows[5].Source)
	correction := unwrapRDFArrayItem(rdfArrayItems(source.findRootProperty(crsNamespace, "MaskGroupBasedCorrections").element)[7])
	if err := setCRSElementValue(correction, "LocalExposure2012", ".5"); err != nil {
		t.Fatal(err)
	}
	// The CLI request deliberately names the child before its parent column.
	columns, err := parseSelectedColumns("MaskGroupBasedCorrections[8].CorrectionMasks[1].Left,MaskGroupBasedCorrections[8].LocalExposure2012")
	if err != nil {
		t.Fatal(err)
	}
	target := EmptyXMPPacket()
	if _, err := copySelectedColumnMetadata(target, source, columns); err != nil {
		t.Fatal(err)
	}
	nodes, err := pastePacketNodes(target)
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"0.500000", "0.250000"} {
		got, err := readSelectedColumn(nodes, columns[i])
		if err != nil || !got.present || got.value != want {
			t.Fatalf("column %d: %q want %q error%v", i, got.value, want, err)
		}
	}
	child := columns[1]
	if changed, err := copySelectedColumnMetadata(target, EmptyXMPPacket(), []selectedColumn{child}); err != nil || !changed {
		t.Fatal("missing nested scalar not removed", changed, err)
	}
	nodes, err = pastePacketNodes(target)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := readSelectedColumn(nodes, columns[0]); err != nil || got.value != "0.500000" {
		t.Fatal("deletion changed parent value", got, err)
	}
	if got, err := readSelectedColumn(nodes, child); err != nil || got.present {
		t.Fatal("nested value survived deletion", got, err)
	}
}

func TestSelectedColumnJavaFormatterLargeValues(t *testing.T) {
	// Additional JVM 25 Formatter observations around large decimal aliases.
	for _, row := range [][2]string{
		{"1e23", "1" + strings.Repeat("0", 23) + ".000000"},
		{"1e22", "1" + strings.Repeat("0", 22) + ".000000"},
		{"1e24", "1" + strings.Repeat("0", 24) + ".000000"},
		{"1000000000000000100", "1000000000000000100.000000"},
		{"1e20", "1" + strings.Repeat("0", 20) + ".000000"},
		{"2.675", "2.675000"},
		{"999999.9999995", "1000000.000000"},
		{"9.999999999999999E22", "1" + strings.Repeat("0", 23) + ".000000"},
	} {
		value, valid := selectedJavaDouble(row[0])
		if !valid || selectedJavaFixed(value) != row[1] {
			t.Fatalf("%s: %s expected%s", row[0], selectedJavaFixed(value), row[1])
		}
	}
}

func selectedTestBranch(root *pasteNode, steps []selectedColumnStep) *pasteNode {
	if root == nil {
		return nil
	}
	copy := clonePasteNode(root)
	current := copy
	for i, step := range steps {
		if step.Index > 0 {
			if step.Index > len(current.Items) {
				return nil
			}
			for j := range current.Items {
				if j != step.Index-1 {
					current.Items[j] = selectedStructItem()
				}
			}
			current = current.Items[step.Index-1]
		}
		if i < len(steps)-1 {
			current = selectedField(current.Fields, steps[i+1].Name)
			if current == nil {
				return nil
			}
		}
	}
	return copy
}

func selectedColumnObservations(t *testing.T) []selectedColumnObservation {
	t.Helper()
	var reference struct {
		SaveCompleted  bool   `json:"save_completed"`
		PostStoreError string `json:"post_store_error"`
		Cases          []selectedColumnObservation
	}
	if err := json.Unmarshal(denoiseRead(t, "testdata/lrt-selected-columns-7.5.5.json"), &reference); err != nil {
		t.Fatal(err)
	}
	if reference.SaveCompleted || !strings.Contains(reference.PostStoreError, "LRTimelapseMain.m()") || len(reference.Cases) != 1001 {
		t.Fatal("unexpected native component provenance")
	}
	return reference.Cases
}

func TestSelectedColumnNativeTypes(t *testing.T) {
	registry, err := selectedColumnRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if len(registry) != 866 {
		t.Fatal("native registry size", len(registry))
	}
	byName := map[string]selectedColumn{}
	for i, column := range registry {
		if column.Index != i {
			t.Fatal("registry index", i)
		}
		byName[column.Property] = column
	}
	tested := map[string]bool{}
	for i, row := range selectedColumnObservations(t) {
		t.Run(row.Property+"/"+strconv.Itoa(i), func(t *testing.T) {
			source := pasteTestPacket(t, row.Source)
			nodes, err := pastePacketNodes(source)
			if err != nil {
				t.Fatal(err)
			}
			column, known := byName[row.Property]
			if !known {
				t.Fatal("unknown observed column")
			}
			actual, err := readSelectedColumn(nodes, column)
			if err != nil {
				t.Fatal(err)
			}
			if row.Stored == nil {
				if actual.present {
					t.Fatalf("expected missing cell, got %q", actual.value)
				}
			} else if !actual.present || actual.value != *row.Stored {
				t.Fatalf("typed cell %q present%v expected%q", actual.value, actual.present, *row.Stored)
			}
			tested[row.Property] = true
		})
	}
	for _, column := range registry[5:] {
		if !tested[column.Property] {
			t.Fatal("unmeasured selectable column", column.Property)
		}
	}
}

func TestSelectedColumnAliasesAndBoundary(t *testing.T) {
	chosen, err := parseSelectedColumns("crs:Exposure2012,Exposure2012, CameraProfile,MaskGroupBasedCorrections[8].CorrectionMasks[1].lrt:CX")
	if err != nil || len(chosen) != 3 {
		t.Fatal(chosen, err)
	}
	if chosen[0].Property != "Exposure2012" || chosen[1].Property != "crs:CameraProfile" {
		t.Fatal("columns not in native order", chosen)
	}
	for _, name := range []string{"", "Exposure2012,", "xmp:Rating", "xmp:CreateDate", "lrt:ExternalExposureDefault", "RawFileName", "Unknown", "MaskGroupBasedCorrections[5].LocalExposure2012", "ToneCurvePV2012", "MaskGroupBasedCorrections[1].CorrectionMasks[2].Left"} {
		if _, err := parseSelectedColumns(name); err == nil {
			t.Fatal("accepted unselectable column", name)
		}
	}
}

func TestSelectedColumnPreservesOtherMetadata(t *testing.T) {
	source := pasteTestPacket(t, metadataPasteObservations(t)[0].Source)
	target := pasteTestPacket(t, metadataPasteObservations(t)[0].Destination)
	columns, err := parseSelectedColumns("Exposure2012")
	if err != nil {
		t.Fatal(err)
	}
	if changed, err := copySelectedColumnMetadata(target, source, columns); err != nil || !changed {
		t.Fatal(changed, err)
	}
	for key, want := range map[string]string{"crs:Exposure2012": "0.750000", "crs:Contrast2012": "25", "xmp:Rating": "4", "exif:DateTimeOriginal": "2026-10-02T02:00:00"} {
		got, present, err := target.GetScalar(key)
		if err != nil || !present || got != want {
			t.Fatalf("%s=%q present%v expected%q error%v", key, got, present, want, err)
		}
	}
	if _, present, _ := target.GetScalar("crs:Temperature"); present {
		t.Fatal("copied an unselected property")
	}
	if changed, err := copySelectedColumnMetadata(target, source, columns); err != nil || changed {
		t.Fatal("non-idempotent scalar paste", changed, err)
	}
	if changed, err := copySelectedColumnMetadata(target, EmptyXMPPacket(), columns); err != nil || !changed {
		t.Fatal("missing source not removed", changed, err)
	}
	if _, present, _ := target.GetScalar("crs:Exposure2012"); present {
		t.Fatal("source null did not clear selection")
	}
}

func TestSelectedColumnAllExistingPaths(t *testing.T) {
	registry, err := selectedColumnRegistry()
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]selectedColumn{}
	for _, column := range registry {
		byName[column.Property] = column
	}
	for i, row := range selectedColumnObservations(t) {
		column := byName[row.Property]
		if column.Index < 5 {
			continue
		}
		t.Run(row.Property+"/"+strconv.Itoa(i), func(t *testing.T) {
			source := pasteTestPacket(t, row.Source)
			// Existing parents use the recorded source shape, with an independent
			// destination sentinel at the selected scalar property.
			nodes, err := pastePacketNodes(source)
			if err != nil {
				t.Fatal(err)
			}
			if row.Stored == nil {
				return
			}
			if err := assignSelectedColumn(&nodes, column.Steps, transitionScalar{"recipient", true}); err != nil {
				t.Fatal(err)
			}
			target := EmptyXMPPacket()
			for _, node := range nodes {
				writePasteNode(target.rootDescriptions()[0], node)
			}
			sourceBefore, _ := source.Serialize()
			if _, err := copySelectedColumnMetadata(target, source, []selectedColumn{column}); err != nil {
				t.Fatal(err)
			}
			actualNodes, err := pastePacketNodes(target)
			if err != nil {
				t.Fatal(err)
			}
			got, err := readSelectedColumn(actualNodes, selectedColumn{Property: column.Property, Kind: "String", Steps: column.Steps})
			if err != nil || !got.present || got.value != *row.Stored {
				t.Fatalf("stored %q expected %q error%v", got.value, *row.Stored, err)
			}
			sourceAfter, _ := source.Serialize()
			if !bytes.Equal(sourceBefore, sourceAfter) {
				t.Fatal("source mutated")
			}
			data, err := target.Serialize()
			if err != nil {
				t.Fatal(err)
			}
			reloaded := pasteTestPacket(t, string(data))
			if changed, err := copySelectedColumnMetadata(reloaded, source, []selectedColumn{column}); err != nil || changed {
				t.Fatal("repeat changed", changed, err)
			}
		})
	}
}
