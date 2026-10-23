package lrtl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type saveStateWBObservation struct {
	Quality                 *string
	Source, Expected, Saved string
}
type saveStateCropObservation struct {
	Source, Saved  string
	ExpectedActive bool `json:"expected_active"`
}
type saveStateCopyObservation struct {
	Source, Destination, Properties, Saved string
	ExpectedActive                         bool `json:"expected_active"`
}
type saveStateReference struct {
	StoreCompleted bool   `json:"store_completed"`
	StoreStop      string `json:"store_stop"`
	WB             []saveStateWBObservation
	Crop           []saveStateCropObservation
	CopyCrop       []saveStateCopyObservation `json:"copy_crop"`
}

func saveStateObservations(t *testing.T) saveStateReference {
	t.Helper()
	var reference saveStateReference
	if err := json.Unmarshal(denoiseRead(t, "testdata/lrt-save-state-7.5.5.json"), &reference); err != nil {
		t.Fatal(err)
	}
	if reference.StoreCompleted || !strings.Contains(reference.StoreStop, "history") || len(reference.WB) != 267 || len(reference.Crop) != 124 || len(reference.CopyCrop) != 6 {
		t.Fatal("invalid native save-state reference provenance")
	}
	return reference
}

func saveStateBareRDF(t *testing.T, packet *XMPPacket) *XMPPacket {
	t.Helper()
	root := packet.Document.Root()
	if root.Tag == "RDF" && root.NamespaceURI() == rdfNamespace {
		return packet
	}
	for _, child := range root.ChildElements() {
		if child.Tag == "RDF" && child.NamespaceURI() == rdfNamespace {
			packet.Document.SetRoot(child.Copy())
			return packet
		}
	}
	t.Fatal("no RDF container")
	return nil
}

func TestSaveStateNativeRootScope(t *testing.T) {
	var row struct {
		Source, Expected   string
		Temperature, Right *string
		NestedTemperature  string `json:"nested_temperature"`
		NestedRight        string `json:"nested_right"`
	}
	if err := json.Unmarshal(denoiseRead(t, "testdata/lrt-save-state-root-scope-7.5.5.json"), &row); err != nil {
		t.Fatal(err)
	}
	if row.Temperature != nil || row.Right != nil || row.NestedTemperature != "777" || row.NestedRight != ".5" {
		t.Fatal("invalid native scope reference")
	}
	for _, bare := range []bool{false, true} {
		packet := pasteTestPacket(t, row.Source)
		if bare {
			packet = saveStateBareRDF(t, packet)
		}
		if _, err := applyNativeWhiteBalanceCleanup(packet, ""); err != nil {
			t.Fatal(err)
		}
		assertPastedMetadata(t, packet, row.Expected)
		for _, name := range []string{"Temperature", "CropRight"} {
			if value, found, err := packet.GetRootScalar("crs:" + name); err != nil || found {
				t.Fatal("nested value leaked into root lookup", name, value, err)
			}
		}
	}
}

func TestNativeSaveWhiteBalance(t *testing.T) {
	for i, row := range saveStateObservations(t).WB {
		for _, bare := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/bare%v", i, bare), func(t *testing.T) {
				target := pasteTestPacket(t, row.Source)
				if bare {
					target = saveStateBareRDF(t, target)
				}
				quality := ""
				if row.Quality != nil {
					quality = *row.Quality
				}
				if _, err := applyNativeWhiteBalanceCleanup(target, quality); err != nil {
					t.Fatal(err)
				}
				assertPastedMetadata(t, target, row.Expected)
				if changed, err := applyNativeWhiteBalanceCleanup(target, quality); err != nil || changed {
					t.Fatal("repeat WB cleanup changed metadata", changed, err)
				}
			})
		}
	}
}

func TestNativeSaveWBPrefix(t *testing.T) {
	for i, row := range saveStateObservations(t).WB {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			target := pasteTestPacket(t, row.Source)
			context, err := readNativeSaveStateContext(target)
			if err != nil {
				t.Fatal(err)
			}
			context.Quality = ""
			if row.Quality != nil {
				context.Quality = *row.Quality
			}
			if _, err := applyNativeSaveState(target, context); err != nil {
				t.Fatal(err)
			}
			assertPastedMetadata(t, target, row.Saved)
			if changed, err := applyNativeSaveState(target, context); err != nil || changed {
				t.Fatal("frozen native cache changed on repeat", changed, err)
			}
		})
	}
}

func TestNativeSaveCropPrefix(t *testing.T) {
	for i, row := range saveStateObservations(t).Crop {
		for _, bare := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/bare%v", i, bare), func(t *testing.T) {
				target := pasteTestPacket(t, row.Source)
				if bare {
					target = saveStateBareRDF(t, target)
				}
				context, err := readNativeSaveStateContext(target)
				if err != nil {
					t.Fatal(err)
				}
				if context.cropActive() != row.ExpectedActive {
					t.Fatal("crop state differs from native cache")
				}
				if _, err := applyNativeSaveState(target, context); err != nil {
					t.Fatal(err)
				}
				assertPastedMetadata(t, target, row.Saved)
				if changed, err := applyNativeSaveState(target, context); err != nil || changed {
					t.Fatal("frozen crop cache changed on repeat", changed, err)
				}
			})
		}
	}
}

func TestNativeSaveCopiedCropCache(t *testing.T) {
	for i, row := range saveStateObservations(t).CopyCrop {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			columns, err := parseSelectedColumns(row.Properties)
			if err != nil {
				t.Fatal(err)
			}
			source, target := pasteTestPacket(t, row.Source), pasteTestPacket(t, row.Destination)
			original, _ := source.Serialize()
			context, err := copiedNativeSaveStateContext(target, source, columns)
			if err != nil {
				t.Fatal(err)
			}
			if context.cropActive() != row.ExpectedActive {
				t.Fatal("copied raw cache differs")
			}
			if _, err := applyCopySettingsProfile(target, source, columns, nativeSaveStateProfile); err != nil {
				t.Fatal(err)
			}
			assertPastedMetadata(t, target, row.Saved)
			after, _ := source.Serialize()
			if !bytes.Equal(original, after) {
				t.Fatal("source changed")
			}
			before, _ := target.Serialize()
			if changed, err := applyCopySettingsProfile(target, source, columns, nativeSaveStateProfile); err != nil || changed {
				t.Fatal("repeat copied state changed", changed, err)
			}
			repeated, _ := target.Serialize()
			if !bytes.Equal(before, repeated) {
				t.Fatal("intermediate edit reordered unchanged roots")
			}
		})
	}
}

func TestSaveStateDirectRDFMetadataPaste(t *testing.T) {
	for _, row := range metadataPasteObservations(t) {
		t.Run(row.Name, func(t *testing.T) {
			source, target := saveStateBareRDF(t, pasteTestPacket(t, row.Source)), saveStateBareRDF(t, pasteTestPacket(t, row.Destination))
			if _, err := pasteXMPMetadata(target, source); err != nil {
				t.Fatal(err)
			}
			assertPastedMetadata(t, target, row.Expected)
		})
	}
	bad := pasteTestPacket(t, `<root xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"><rdf:Description xmlns:crs="http://ns.adobe.com/camera-raw-settings/1.0/" crs:Exposure2012="1"/></root>`)
	target := pasteTestPacket(t, metadataPasteObservations(t)[0].Destination)
	before, _ := target.Serialize()
	if _, err := pasteXMPMetadata(target, bad); err == nil {
		t.Fatal("unrecognized metadata wrapper silently accepted")
	}
	after, _ := target.Serialize()
	if !bytes.Equal(before, after) {
		t.Fatal("unrecognized metadata erased target settings")
	}
}

func TestSaveStateInstalledComponents(t *testing.T) {
	jar := os.Getenv("LRTL_NATIVE_JAR")
	if jar == "" {
		t.Skip("set LRTL_NATIVE_JAR for installed WB/crop/save-prefix comparison")
	}
	dir := t.TempDir()
	probeRoot, err := filepath.Abs(filepath.Join("..", "..", "scripts", "probes"))
	if err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("javac", "-encoding", "UTF-8", "-d", dir, filepath.Join(probeRoot, "MetadataPasteOracle.java"), filepath.Join(probeRoot, "SaveStateOracle.java")).CombinedOutput(); err != nil {
		t.Fatalf("compile original save-state probe: %v\n%s", err, out)
	}
	result := filepath.Join(dir, "replay.json")
	if out, err := exec.Command("java", "--enable-native-access=ALL-UNNAMED", "-cp", dir+string(os.PathListSeparator)+jar, "lrtlprobe.SaveStateOracle", result).CombinedOutput(); err != nil {
		t.Fatalf("native save-state replay: %v\n%s", err, out)
	}
	var replay saveStateReference
	if err := json.Unmarshal(denoiseRead(t, result), &replay); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(replay, saveStateObservations(t)) {
		t.Fatal("native save-state observations changed")
	}
	var expected []*XMPPacket
	for _, row := range replay.WB {
		target := saveStateBareRDF(t, pasteTestPacket(t, row.Source))
		context, err := readNativeSaveStateContext(target)
		if err != nil {
			t.Fatal(err)
		}
		context.Quality = ""
		if row.Quality != nil {
			context.Quality = *row.Quality
		}
		if _, err := applyNativeSaveState(target, context); err != nil {
			t.Fatal(err)
		}
		data, err := target.Serialize()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("save%04d.input.xmp", len(expected))), data, 0600); err != nil {
			t.Fatal(err)
		}
		expected = append(expected, target)
	}
	if out, err := exec.Command("java", "-cp", dir+string(os.PathListSeparator)+jar, "lrtlprobe.MetadataPasteOracle", "--roundtrip", dir).CombinedOutput(); err != nil {
		t.Fatalf("native parse of bare RDF results: %v\n%s", err, out)
	}
	for i, want := range expected {
		actual := pasteTestPacket(t, string(denoiseRead(t, filepath.Join(dir, fmt.Sprintf("save%04d.native.xmp", i)))))
		data, err := want.Serialize()
		if err != nil {
			t.Fatal(err)
		}
		assertPastedMetadata(t, actual, string(data))
	}
}
