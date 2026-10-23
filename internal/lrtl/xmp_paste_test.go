package lrtl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

type metadataPasteObservation struct{ Name, Source, Destination, Expected, Repeated string }

func metadataPasteObservations(t *testing.T) []metadataPasteObservation {
	t.Helper()
	var rows []metadataPasteObservation
	if err := json.Unmarshal(denoiseRead(t, "testdata/lrt-metadata-paste-7.5.5.json"), &rows); err != nil || len(rows) != 42 {
		t.Fatalf("expected 42 native XML paste observations: %v", err)
	}
	return rows
}

func TestMetadataPasteInstalledParser(t *testing.T) {
	jar := os.Getenv("LRTL_NATIVE_JAR")
	if jar == "" {
		t.Skip("set LRTL_NATIVE_JAR for optional installed XMP parser round trips")
	}
	dir := t.TempDir()
	rows := metadataPasteObservations(t)
	for i, row := range rows {
		source, target := pasteTestPacket(t, row.Source), pasteTestPacket(t, row.Destination)
		if _, err := pasteXMPMetadata(target, source); err != nil {
			t.Fatal(err)
		}
		data, err := target.Serialize()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("case%02d.input.xmp", i)), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	probe, err := filepath.Abs(filepath.Join("..", "..", "scripts", "probes", "MetadataPasteOracle.java"))
	if err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("javac", "-encoding", "UTF-8", "-d", dir, probe).CombinedOutput(); err != nil {
		t.Fatalf("compile native paste parser harness: %v\n%s", err, out)
	}
	out, err := exec.Command("java", "-cp", dir+string(os.PathListSeparator)+jar, "lrtlprobe.MetadataPasteOracle", "--roundtrip", dir).CombinedOutput()
	if err != nil || !strings.Contains(string(out), fmt.Sprintf("ROUNDTRIPPED %d XMP PACKETS", len(rows))) {
		t.Fatalf("native XMP parse/serialize: %v\n%s", err, out)
	}
	for i, row := range rows {
		t.Run(row.Name, func(t *testing.T) {
			assertPastedMetadata(t, pasteTestPacket(t, string(denoiseRead(t, filepath.Join(dir, fmt.Sprintf("case%02d.native.xmp", i))))), row.Expected)
		})
	}
}

func pasteTestPacket(t *testing.T, text string) *XMPPacket {
	t.Helper()
	p, err := parseXMP([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func pasteTestCanonical(nodes []*pasteNode) []*pasteNode {
	copy := make([]*pasteNode, 0, len(nodes))
	for _, n := range nodes {
		c := clonePasteNode(n)
		c.Prefix = ""
		c.Fields = pasteTestCanonical(c.Fields)
		c.Qualifiers = pasteTestCanonical(c.Qualifiers)
		for i, item := range c.Items {
			c.Items[i] = pasteTestCanonical([]*pasteNode{item})[0]
		}
		copy = append(copy, c)
	}
	sort.Slice(copy, func(i, j int) bool {
		if copy[i].Name.URI != copy[j].Name.URI {
			return copy[i].Name.URI < copy[j].Name.URI
		}
		return copy[i].Name.Local < copy[j].Name.Local
	})
	return copy
}

func assertPastedMetadata(t *testing.T, packet *XMPPacket, expected string) {
	t.Helper()
	want := pasteTestPacket(t, expected)
	actual, err := pastePacketNodes(packet)
	if err != nil {
		t.Fatal(err)
	}
	reference, err := pastePacketNodes(want)
	if err != nil {
		t.Fatal(err)
	}
	a, b := pasteTestCanonical(actual), pasteTestCanonical(reference)
	if !reflect.DeepEqual(a, b) {
		actualJSON, _ := json.MarshalIndent(a, "", " ")
		expectedJSON, _ := json.MarshalIndent(b, "", " ")
		t.Fatalf("native metadata differs\ngot%s\nwant%s", actualJSON, expectedJSON)
	}
}

func TestMetadataPasteNativeXML(t *testing.T) {
	for _, row := range metadataPasteObservations(t) {
		t.Run(row.Name, func(t *testing.T) {
			source, destination := pasteTestPacket(t, row.Source), pasteTestPacket(t, row.Destination)
			before, err := source.Serialize()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := pasteXMPMetadata(destination, source); err != nil {
				t.Fatal(err)
			}
			assertPastedMetadata(t, destination, row.Expected)
			after, err := source.Serialize()
			if err != nil || string(after) != string(before) {
				t.Fatal("paste mutated source metadata", err)
			}
			serialized, err := destination.Serialize()
			if err != nil {
				t.Fatal(err)
			}
			reloaded := pasteTestPacket(t, string(serialized))
			assertPastedMetadata(t, reloaded, row.Expected)
			if _, err := pasteXMPMetadata(reloaded, source); err != nil {
				t.Fatal(err)
			}
			assertPastedMetadata(t, reloaded, row.Repeated)
		})
	}
}

func TestMetadataPastePreservesCaptureAndUnknown(t *testing.T) {
	row := metadataPasteObservations(t)[0]
	p := pasteTestPacket(t, row.Destination)
	if _, err := pasteXMPMetadata(p, pasteTestPacket(t, row.Source)); err != nil {
		t.Fatal(err)
	}
	for property, want := range map[string]string{"crs:Exposure2012": "0.75", "crs:Temperature": "5500", "xmp:Rating": "4", "exif:DateTimeOriginal": "2026-10-02T02:00:00"} {
		if got, found, err := p.GetScalar(property); err != nil || !found || got != want {
			t.Fatalf("%s=%q want%q found%v error%v", property, got, want, found, err)
		}
	}
	if _, found, err := p.GetScalar("crs:Contrast2012"); err != nil || found {
		t.Fatal("destination-only Camera Raw setting survived", err)
	}
}

func TestMetadataPasteRejectsDuplicateProperties(t *testing.T) {
	for i, body := range []string{`<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"><rdf:Description xmlns:crs="http://ns.adobe.com/camera-raw-settings/1.0/" crs:Exposure2012="1"><crs:Exposure2012>2</crs:Exposure2012></rdf:Description></rdf:RDF></x:xmpmeta>`} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			target := EmptyXMPPacket()
			before, _ := target.Serialize()
			if _, err := pasteXMPMetadata(target, pasteTestPacket(t, body)); err == nil {
				t.Fatal("accepted ambiguous duplicate metadata")
			}
			after, _ := target.Serialize()
			if string(before) != string(after) {
				t.Fatal("failed paste changed target")
			}
		})
	}
}

func FuzzMetadataPaste(f *testing.F) {
	var rows []metadataPasteObservation
	data, err := os.ReadFile("testdata/lrt-metadata-paste-7.5.5.json")
	if err != nil {
		f.Fatal(err)
	}
	if err := json.Unmarshal(data, &rows); err != nil {
		f.Fatal(err)
	}
	for _, row := range rows {
		f.Add(row.Source, row.Destination)
	}
	f.Fuzz(func(t *testing.T, sourceText, destinationText string) {
		if len(sourceText) > 32768 || len(destinationText) > 32768 {
			t.Skip()
		}
		source, err := parseXMP([]byte(sourceText))
		if err != nil {
			return
		}
		destination, err := parseXMP([]byte(destinationText))
		if err != nil {
			return
		}
		beforeSource, err := source.Serialize()
		if err != nil {
			return
		}
		beforeDestination, err := destination.Serialize()
		if err != nil {
			return
		}
		_, pasteErr := pasteXMPMetadata(destination, source)
		afterSource, err := source.Serialize()
		if err != nil || string(beforeSource) != string(afterSource) {
			t.Fatal("source changed", err)
		}
		afterDestination, err := destination.Serialize()
		if err != nil {
			t.Fatal(err)
		}
		if pasteErr != nil {
			if string(beforeDestination) != string(afterDestination) {
				t.Fatal("failed paste changed destination")
			}
			return
		}
		reloaded, err := parseXMP(afterDestination)
		if err != nil {
			t.Fatal("generated invalid XMP", err)
		}
		actual, err := pastePacketNodes(destination)
		if err != nil {
			t.Fatal(err)
		}
		roundtrip, err := pastePacketNodes(reloaded)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(pasteTestCanonical(actual), pasteTestCanonical(roundtrip)) {
			t.Fatal("serialization changed metadata semantics")
		}
	})
}

func TestMetadataPasteErrorPreservesDestination(t *testing.T) {
	wrap := func(body string) string {
		return `<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"><rdf:Description xmlns:f="urn:lrtl:errors/">` + body + `</rdf:Description></rdf:RDF></x:xmpmeta>`
	}
	for _, body := range []string{
		`<f:Value rdf:parseType="Resource"><f:Duplicate>one</f:Duplicate><f:Duplicate>two</f:Duplicate></f:Value>`,
		`<f:Value><rdf:Bag><f:NotAnItem>one</f:NotAnItem></rdf:Bag></f:Value>`,
		`<f:Value rdf:parseType="Resource" xml:lang="en-US"><rdf:value xml:lang="ja-JP">text</rdf:value></f:Value>`,
		strings.Repeat(`<f:Value rdf:parseType="Resource">`, 260) + `<f:Leaf>text</f:Leaf>` + strings.Repeat(`</f:Value>`, 260),
	} {
		for _, badSource := range []bool{false, true} {
			source, target := EmptyXMPPacket(), pasteTestPacket(t, wrap(body))
			if badSource {
				source, target = target, source
			}
			before, _ := target.Serialize()
			if _, err := pasteXMPMetadata(target, source); err == nil {
				t.Fatal("accepted ambiguous or unsupported input")
			}
			after, _ := target.Serialize()
			if !bytes.Equal(before, after) {
				t.Fatal("failed paste mutated target")
			}
		}
	}
}

func TestMetadataPastePreservesUnchangedRepresentation(t *testing.T) {
	row := metadataPasteObservations(t)[0]
	targetText := row.Destination
	targetText = strings.Replace(targetText, `</rdf:Description>`, `<!-- retained root comment --><f:Untouched><![CDATA[raw <text>]]></f:Untouched></rdf:Description>`, 1)
	target := pasteTestPacket(t, targetText)
	before, err := target.Serialize()
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(string(before), "<f:Untouched>")
	end := strings.Index(string(before), "</f:Untouched>")
	if start < 0 || end < start {
		t.Fatal("missing test property")
	}
	untouched := string(before)[start : end+len("</f:Untouched>")]
	if _, err := pasteXMPMetadata(target, pasteTestPacket(t, row.Source)); err != nil {
		t.Fatal(err)
	}
	data, err := target.Serialize()
	if err != nil {
		t.Fatal(err)
	}
	for _, literal := range []string{`f:Kept="target"`, `<!-- retained root comment -->`, untouched} {
		if !strings.Contains(string(data), literal) {
			t.Fatalf("lost unchanged representation %q", literal)
		}
	}
}

func TestMetadataPasteDefaultPropertyNamespace(t *testing.T) {
	for _, row := range metadataPasteObservations(t) {
		if row.Name != "new-qualified-struct" {
			continue
		}
		sourceText := strings.NewReplacer("xmlns:f=", "xmlns=", "<f:", "<", "</f:", "</").Replace(row.Source)
		target := pasteTestPacket(t, row.Destination)
		if _, err := pasteXMPMetadata(target, pasteTestPacket(t, sourceText)); err != nil {
			t.Fatal(err)
		}
		data, err := target.Serialize()
		if err != nil {
			t.Fatal(err)
		}
		assertPastedMetadata(t, pasteTestPacket(t, string(data)), row.Expected)
		return
	}
	t.Fatal("missing qualified-struct observation")
}
