package lrtl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

type wbEditObservation struct {
	Source, Property string
	Requested        float64
	Expected         map[string]*string
}

type wbEditReference struct {
	CallbackCompleted        bool `json:"callback_completed"`
	EditorConstructorSkipped bool `json:"editor_constructor_skipped"`
	Stop                     string
	Bounds                   []struct {
		Property           string
		Min, Max, Fallback float64
	}
	Cases []wbEditObservation
}

func wbEditObservations(t *testing.T) wbEditReference {
	t.Helper()
	var reference wbEditReference
	if err := json.Unmarshal(denoiseRead(t, "testdata/lrt-wb-edit-7.5.5.json"), &reference); err != nil {
		t.Fatal(err)
	}
	if reference.CallbackCompleted || !reference.EditorConstructorSkipped || reference.Stop != "null editor controller g" || len(reference.Cases) != 960 || len(reference.Bounds) != 4 {
		t.Fatal("invalid WB edit reference provenance")
	}
	return reference
}

func TestWBEditNativeCallbackPrefix(t *testing.T) {
	reference := wbEditObservations(t)
	for i, bound := range reference.Bounds {
		control := wbEditControls[i]
		if bound.Property != control.name || bound.Min != float64(control.min) || bound.Max != float64(control.max) || bound.Fallback != float64(control.fallback) {
			t.Fatal("WB slider metadata differs", bound, control)
		}
	}
	for i, row := range reference.Cases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			for _, bare := range []bool{false, true} {
				packet := pasteTestPacket(t, row.Source)
				if bare {
					packet = saveStateBareRDF(t, packet)
				}
				beforeNodes, err := pastePacketNodes(packet)
				if err != nil {
					t.Fatal(err)
				}
				nested := clonePasteNode(selectedField(beforeNodes, pasteName{"urn:lrtl:wb-edit/", "Nested"}))
				edit, err := parseNumericXMPSet(row.Property, formatNumber(row.Requested))
				if err != nil {
					t.Fatal(err)
				}
				if err := applyNumericXMPSet(packet, edit); err != nil {
					t.Fatal(err)
				}
				for name, expected := range row.Expected {
					value, present, err := packet.GetRootScalar("crs:" + name)
					if err != nil {
						t.Fatal(err)
					}
					if expected == nil {
						if present {
							t.Fatal(name, "unexpected value", value)
						}
						continue
					}
					// The callback edits cache cells. Unselected XML integers are
					// retained lexically, and are compared through native loading.
					if name != "WhiteBalance" {
						columns, err := parseSelectedColumns(name)
						if err != nil {
							t.Fatal(err)
						}
						value = normalizeSelectedColumn(columns[0], transitionScalar{value, present}).value
					}
					if !present || value != *expected {
						t.Fatalf("%s: got %q/%t want %q (bare=%t)", name, value, present, *expected, bare)
					}
				}
				afterNodes, err := pastePacketNodes(packet)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(nested, selectedField(afterNodes, nested.Name)) {
					t.Fatal("nested same-name metadata changed")
				}
				saved, _ := packet.Serialize()
				if err := applyNumericXMPSet(packet, edit); err != nil {
					t.Fatal(err)
				}
				repeated, _ := packet.Serialize()
				if !bytes.Equal(saved, repeated) {
					t.Fatal("repeat edit changed serialized packet")
				}
			}
		})
	}
}

func TestWBEditInstalledCallbackPrefix(t *testing.T) {
	jar := os.Getenv("LRTL_NATIVE_JAR")
	if jar == "" {
		t.Skip("set LRTL_NATIVE_JAR for the isolated installed edit-callback prefix")
	}
	dir := t.TempDir()
	probeRoot, err := filepath.Abs(filepath.Join("..", "..", "scripts", "probes"))
	if err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("javac", "-encoding", "UTF-8", "-d", dir, filepath.Join(probeRoot, "MetadataPasteOracle.java"), filepath.Join(probeRoot, "WBEditOracle.java")).CombinedOutput(); err != nil {
		t.Fatalf("compile original WB edit observer: %v\n%s", err, out)
	}
	path := filepath.Join(dir, "replay.json")
	if out, err := exec.Command("java", "--enable-native-access=ALL-UNNAMED", "--sun-misc-unsafe-memory-access=allow", "-cp", dir+string(os.PathListSeparator)+jar, "lrtlprobe.WBEditOracle", path).CombinedOutput(); err != nil {
		t.Fatalf("native callback prefix: %v\n%s", err, out)
	}
	var replay wbEditReference
	if err := json.Unmarshal(denoiseRead(t, path), &replay); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(replay, wbEditObservations(t)) {
		t.Fatal("installed WB edit observations changed")
	}
}

func TestWBEditRootScopeAndAtomicErrors(t *testing.T) {
	for _, name := range []string{"Exposure2012", "Temperature", "Tint", "IncrementalTemperature", "IncrementalTint", "PreviewEV"} {
		t.Run(name, func(t *testing.T) {
			packet := pasteTestPacket(t, `<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"><rdf:Description xmlns:f="urn:lrtl:wb-edit/" xmlns:crs="http://ns.adobe.com/camera-raw-settings/1.0/" xmlns:lrt="http://lrtimelapse.com/"><f:Nested><rdf:Description crs:Temperature="777" crs:Tint="99" crs:Exposure2012="6" lrt:ExternalExposureDefault="8"/></f:Nested></rdf:Description></rdf:RDF>`)
			before, _ := pastePacketNodes(packet)
			edit, err := parseNumericXMPSet(name, "21.9")
			if err != nil {
				t.Fatal(err)
			}
			if err := applyNumericXMPSet(packet, edit); err != nil {
				t.Fatal(err)
			}
			after, _ := pastePacketNodes(packet)
			if !reflect.DeepEqual(before[0], selectedField(after, before[0].Name)) {
				t.Fatal("global edit changed nested data")
			}
			if value, present, _ := packet.GetRootScalar(edit.property); !present || value != edit.value {
				t.Fatal("missing root edit", value)
			}
		})
	}
	for _, body := range []string{
		`<crs:Tint rdf:parseType="Resource"><crs:Keep>20</crs:Keep></crs:Tint>`,
		`<crs:WhiteBalance rdf:parseType="Resource"><crs:Keep>Custom</crs:Keep></crs:WhiteBalance>`,
		`<lrt:Quality rdf:parseType="Resource"><crs:Keep>RAW</crs:Keep></lrt:Quality>`,
		`<crs:Temperature rdf:parseType="Resource"><crs:Keep>5000</crs:Keep></crs:Temperature>`,
	} {
		packet := pasteTestPacket(t, `<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"><rdf:Description xmlns:crs="http://ns.adobe.com/camera-raw-settings/1.0/" xmlns:lrt="http://lrtimelapse.com/">`+body+`</rdf:Description></rdf:RDF>`)
		original, _ := packet.Serialize()
		edit, _ := parseNumericXMPSet("Temperature", "6000")
		if err := applyNumericXMPSet(packet, edit); err == nil {
			t.Fatal("accepted structured WB state")
		}
		after, _ := packet.Serialize()
		if !bytes.Equal(original, after) {
			t.Fatal("failed edit mutated packet")
		}
	}
}

func FuzzWBNumericEdit(f *testing.F) {
	f.Add("Temperature", 5500.9, "As Shot", "RAW", "20.5")
	f.Add("IncrementalTint", -1000.0, "Custom", "FLAT", "NaN")
	f.Add("Tint", 2.0, "", "flat", "4294967297")
	f.Add("Tint", 182.0, "0", "\r", "0")
	f.Fuzz(func(t *testing.T, name string, number float64, wb, quality, companion string) {
		if len(name)+len(wb)+len(quality)+len(companion) > 4096 || math.IsNaN(number) || math.IsInf(number, 0) {
			return
		}
		if wbEditControlIndex(name) < 0 {
			return
		}
		packet := EmptyXMPPacket()
		for key, value := range map[string]string{"crs:WhiteBalance": wb, "lrt:Quality": quality, "crs:Temperature": companion, "crs:Tint": companion, "crs:IncrementalTemperature": companion, "crs:IncrementalTint": companion} {
			if err := packet.SetScalar(key, value); err != nil {
				return
			}
		}
		// Inputs enter the CLI through XML parsing. Normalize XML attribute
		// line endings before editing; raw in-memory CR attributes are not
		// equivalent to a packet read from a sidecar.
		input, err := packet.Serialize()
		if err != nil {
			return
		}
		packet, err = parseXMP(input)
		if err != nil {
			return
		}
		edit, err := parseNumericXMPSet(name, strconv.FormatFloat(number, 'g', -1, 64))
		if err != nil {
			t.Fatal(err)
		}
		if err := applyNumericXMPSet(packet, edit); err != nil {
			t.Fatal(err)
		}
		serialized, err := packet.Serialize()
		if err != nil {
			t.Fatal(err)
		}
		reloaded, err := parseXMP(serialized)
		if err != nil {
			t.Fatal(err)
		}
		if err := applyNumericXMPSet(reloaded, edit); err != nil {
			t.Fatal(err)
		}
		repeated, _ := reloaded.Serialize()
		if !bytes.Equal(serialized, repeated) {
			t.Fatal("WB edit did not stabilize", fmt.Sprintf("%q %g %q %q %q", name, number, wb, quality, companion))
		}
		index := wbEditControlIndex(name)
		value, found, err := reloaded.GetRootScalar("crs:" + strings.TrimPrefix(name, "crs:"))
		if err != nil || !found || value != wbEditInteger(index, number) {
			t.Fatal("invalid stored control", value, err)
		}
	})
}
