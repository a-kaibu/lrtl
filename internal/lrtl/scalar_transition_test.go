package lrtl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func scalarOracleValue(value any) transitionScalar {
	if value == nil {
		return transitionScalar{}
	}
	return transitionScalar{fmt.Sprint(value), true}
}

func TestScalarTransitionsAgainstInstalledLRT755(t *testing.T) {
	data, err := os.ReadFile("testdata/lrt-7.5.5-scalar-transition.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Property                string
			Input, Filled, Expected []any
			Changed                 bool
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 12 {
		t.Fatalf("oracle cases=%d", len(fixture.Cases))
	}
	for n, c := range fixture.Cases {
		t.Run(fmt.Sprintf("%d/%s", n, c.Property), func(t *testing.T) {
			input := make([]transitionScalar, len(c.Input))
			for i, v := range c.Input {
				input[i] = scalarOracleValue(v)
			}
			filled := fillMissingTransitionScalars(input)
			for i, want := range c.Filled {
				if got := filled[i]; got != scalarOracleValue(want) {
					t.Fatalf("fill frame %d=%+v want %v", i, got, want)
				}
			}
			if changed := !reflect.DeepEqual(input, filled); changed != c.Changed {
				t.Fatalf("fill changed=%t want %t", changed, c.Changed)
			}
			output := filled
			if c.Property != "HasCrop" {
				output = equalizeTransitionStrings(input)
			}
			for i, want := range c.Expected {
				if output[i] != scalarOracleValue(want) {
					t.Fatalf("component frame %d=%+v want %v", i, output[i], want)
				}
			}

		})
	}
}

func TestRootBooleanDenseTransitionsPreserveExplicitFlagsAndFillMissingKeys(t *testing.T) {
	for _, property := range rootTransitionBooleans {
		t.Run(property, func(t *testing.T) {
			packets := make([]*XMPPacket, 5)
			for i := range packets {
				packets[i] = EmptyXMPPacket()
				packets[i].SetScalar("xmp:Rating", "4")
			}
			packets[0].SetScalar("crs:"+property, "True")
			packets[1].SetScalar("crs:"+property, "True")
			packets[3].SetScalar("crs:"+property, "False")
			changes, err := planRootScalarTransitions(packets)
			if err != nil {
				t.Fatal(err)
			}
			for i, want := range []string{"", "", "True", "", "False"} {
				if got := changes[i].Values["crs:"+property]; got != want {
					t.Fatalf("frame %d change=%s want %s", i, got, want)
				}
			}
			packets[2].SetScalar("crs:"+property, "broken")
			if _, err := planRootScalarTransitions(packets); err == nil {
				t.Fatal("accepted invalid flag")
			}
		})
	}
}

func TestRootStringTransitionsUseFirstNonemptyAcrossKeyframes(t *testing.T) {
	for _, property := range rootTransitionStrings {
		t.Run(property, func(t *testing.T) {
			packets := []*XMPPacket{EmptyXMPPacket(), EmptyXMPPacket(), EmptyXMPPacket()}
			for _, p := range packets {
				p.SetScalar("xmp:Rating", "4")
			}
			packets[0].SetScalar("crs:"+property, "")
			packets[1].SetScalar("crs:"+property, "first")
			packets[2].SetScalar("crs:"+property, "last")
			changes, err := planRootScalarTransitions(packets)
			if err != nil {
				t.Fatal(err)
			}
			for i, want := range []string{"first", "", "first"} {
				if got := changes[i].Values["crs:"+property]; got != want {
					t.Fatalf("frame %d change=%q want %q", i, got, want)
				}
			}
		})
	}
}

func TestScalarTransitionCLIBackupsDryRunAndCropFlags(t *testing.T) {
	frames := cropFixture(t, 3)
	for i, frame := range frames {
		properties := map[string]string{"xmp:Rating": "4", "crs:WhiteBalance": []string{"", "Custom", "As Shot"}[i], "crs:CropLeft": "0.1", "crs:CropTop": "0", "crs:CropRight": "0.9", "crs:CropBottom": "1", "crs:CropAngle": "0"}
		if i == 0 {
			properties["crs:HasCrop"] = "True"
		}
		if i == 2 {
			properties["crs:HasCrop"] = "False"
		}
		writeTestXMP(t, frame.Image, properties)
	}
	original, err := os.ReadFile(frames[1].Sidecar)
	if err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	args := []string{"interpolate", filepath.Dir(frames[0].Image)}
	if code := Main(args, &out, &stderr); code != 0 {
		t.Fatalf("dry run: %s", &stderr)
	}
	if !strings.Contains(out.String(), "crs:HasCrop") || !strings.Contains(out.String(), "crs:WhiteBalance") {
		t.Fatalf("missing scalar changes in preview: %s", &out)
	}
	after, err := os.ReadFile(frames[1].Sidecar)
	if err != nil || !bytes.Equal(original, after) {
		t.Fatal("preview changed XMP")
	}
	if code := Main(append(args, "--write"), &out, &stderr); code != 0 {
		t.Fatalf("write: %s", &stderr)
	}
	for i, frame := range frames {
		p, err := PacketForFrame(frame)
		if err != nil {
			t.Fatal(err)
		}
		text, _, _ := p.GetScalar("crs:HasCrop")
		if text != []string{"True", "True", "False"}[i] {
			t.Fatalf("frame %d crop=%s", i, text)
		}
		wb, _, _ := p.GetScalar("crs:WhiteBalance")
		if wb != "Custom" {
			t.Fatalf("frame %d WB=%s", i, wb)
		}
		if rating, _ := p.Rating(); rating != 4 {
			t.Fatal("rating changed")
		}
	}
	backups, err := filepath.Glob(frames[1].Sidecar + "*.bak")
	if err != nil || len(backups) != 1 {
		t.Fatalf("backups=%v %v", backups, err)
	}
	backup, err := os.ReadFile(backups[0])
	if err != nil || !bytes.Equal(backup, original) {
		t.Fatal("backup is not original")
	}
	crops, err := readXMPCrops(frames, 64, 32)
	if err != nil || !crops[1].Active || crops[2].Active {
		t.Fatalf("render flags=%v %v", crops, err)
	}
	backupsBefore, _ := filepath.Glob(filepath.Join(filepath.Dir(frames[0].Image), "*.bak"))
	if code := Main(append(args, "--write"), &out, &stderr); code != 0 {
		t.Fatalf("repeat: %s", &stderr)
	}
	backupsAfter, _ := filepath.Glob(filepath.Join(filepath.Dir(frames[0].Image), "*.bak"))
	if len(backupsAfter) != len(backupsBefore) {
		t.Fatal("repeat created backups")
	}
}

func TestScalarTransitionInvalidLaterFlagDoesNotWrite(t *testing.T) {
	frames := cropFixture(t, 3)
	original, err := os.ReadFile(frames[0].Sidecar)
	if err != nil {
		t.Fatal(err)
	}
	writeTestXMP(t, frames[2].Image, map[string]string{"crs:HasCrop": "invalid"})
	var out, stderr bytes.Buffer
	if code := Main([]string{"interpolate", filepath.Dir(frames[0].Image), "--write"}, &out, &stderr); code == 0 {
		t.Fatal("accepted malformed flag")
	}
	after, err := os.ReadFile(frames[0].Sidecar)
	if err != nil || !bytes.Equal(original, after) {
		t.Fatal("failed planning wrote XMP")
	}
}

func TestScalarTransitionEmbeddedPacketsKeepUnknownRDFAndImages(t *testing.T) {
	dir := t.TempDir()
	originals := map[string][]byte{}
	for i, wb := range []string{"", "Custom", "As Shot"} {
		// The familiar crs prefix deliberately belongs to an unrelated namespace.
		// Root scalar writes must resolve the Camera Raw URI, not the prefix text.
		packet := fmt.Sprintf(`<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"><rdf:Description xmlns:raw="http://ns.adobe.com/camera-raw-settings/1.0/" xmlns:crs="urn:lrtl:vendor-crop" xmlns:xmp="http://ns.adobe.com/xap/1.0/" xmp:Rating="4" raw:WhiteBalance="%s" crs:Keep="vendor"><crs:Note><rdf:Seq><rdf:li>preserve nested value</rdf:li></rdf:Seq></crs:Note></rdf:Description></rdf:RDF></x:xmpmeta>`, wb)
		path := filepath.Join(dir, fmt.Sprintf("frame%d.jpg", i))
		originals[path] = writeEmbeddedTransitionJPEG(t, path, []byte(packet))
	}
	var out, stderr bytes.Buffer
	if code := Main([]string{"interpolate", dir, "--write"}, &out, &stderr); code != 0 {
		t.Fatalf("CLI: %s", &stderr)
	}
	// Both keyframe packets with differing WB names receive the uniform value.
	for _, i := range []int{0, 2} {
		path := filepath.Join(dir, fmt.Sprintf("frame%d.jpg.xmp", i))
		data, err := os.ReadFile(path)
		if err != nil || !bytes.Contains(data, []byte("preserve nested value")) || !bytes.Contains(data, []byte("vendor")) {
			t.Fatalf("unknown RDF discarded: %s %v", path, err)
		}
		packet, err := PacketForSidecar(path)
		if err != nil {
			t.Fatal(err)
		}
		got, found, err := packet.GetScalar("crs:WhiteBalance")
		if err != nil || !found || got != "Custom" {
			t.Fatalf("WB=%s found=%t err=%v", got, found, err)
		}
	}
	for path, want := range originals {
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatal("embedded image modified")
		}
	}
}

func TestRootScalarAutoTransitionDispatch(t *testing.T) {
	// These branch expectations follow the outer worker's bytecode; the six
	// WhiteBalance/HasCrop arrays were also observed before its headless UI error.
	for _, c := range []struct {
		name, property string
		input          []any
		ratings        []int
		want           []any
	}{
		{"two-WB-anchors", "WhiteBalance", []any{"As Shot", "Custom", "Auto"}, []int{4, 0, 4}, []any{"As Shot", "Custom", "Auto"}},
		{"custom-WB-anchor", "WhiteBalance", []any{"As Shot", "Custom", "Auto"}, []int{4, 4, 4}, []any{"Custom", "Custom", "Custom"}},
		{"one-WB-anchor", "WhiteBalance", []any{"As Shot", "Custom", "Auto"}, []int{0, 4, 0}, []any{"Custom", "Custom", "Custom"}},
		{"missing-reference-custom-WB", "WhiteBalance", []any{nil, "Custom", "Auto"}, []int{4, 4, 4}, []any{"Custom", "Custom", "Custom"}},
		{"mixed-boolean", "HasCrop", []any{true, nil, false}, []int{4, 4, 4}, []any{true, true, false}},
		{"crop-ratings-are-not-root-anchors", "HasCrop", []any{true, nil, false}, []int{5, 5, 5}, []any{true, true, true}},
		{"two-string-anchors", "LensProfileName", []any{"a", "b", "c"}, []int{4, 0, 4}, []any{"a", "b", "c"}},
		{"three-string-anchors", "LensProfileName", []any{"a", "b", "c"}, []int{4, 4, 4}, []any{"a", "a", "a"}},
		{"missing-reference", "LensProfileName", []any{nil, "b", "c"}, []int{4, 4, 4}, []any{nil, nil, nil}},
		{"equal-anchors", "LensProfileName", []any{"a", "b", "A"}, []int{4, 0, 4}, []any{"a", "a", "a"}},
		{"one-interior-anchor", "LensProfileName", []any{"a", "b", "c"}, []int{0, 4, 0}, []any{"b", "b", "b"}},
		{"empty-input", "LensProfileName", nil, nil, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			input := make([]transitionScalar, len(c.input))
			for i, v := range c.input {
				input[i] = scalarOracleValue(v)
			}
			got := rootAutoTransitionScalars(input, c.ratings, c.property)
			for i, want := range c.want {
				if got[i] != scalarOracleValue(want) {
					t.Fatalf("frame %d=%+v want %v", i, got[i], want)
				}
			}
		})
	}
}

func TestScalarTransitionRemovalAndAsShotWhiteBalance(t *testing.T) {
	frames := cropFixture(t, 3)
	for i, frame := range frames {
		properties := map[string]string{"xmp:Rating": "4", "crs:WhiteBalance": []string{"As Shot", "Auto", "Auto"}[i], "crs:Temperature": "4500", "crs:Tint": "5", "crs:IncrementalTemperature": "2", "crs:IncrementalTint": "3"}
		if i != 0 {
			properties["crs:LensProfileName"] = "later"
		}
		writeTestXMP(t, frame.Image, properties)
	}
	plan, err := buildPlan(frames)
	if err != nil {
		t.Fatal(err)
	}
	outputs, err := applyPlan(plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(outputs) != 3 {
		t.Fatalf("outputs=%d", len(outputs))
	}
	for _, output := range outputs {
		for _, property := range []string{"LensProfileName", "Temperature", "Tint", "IncrementalTemperature", "IncrementalTint"} {
			if _, found, err := output.Packet.GetScalar("crs:" + property); err != nil || found {
				t.Fatalf("retained %s in %s: %v", property, output.Path, err)
			}
		}
	}
	// A dry plan leaves sidecars intact even though the in-memory numeric inputs
	// must omit removed WB sliders before numerical spline planning.
	packet, err := PacketForFrame(frames[1])
	if err != nil {
		t.Fatal(err)
	}
	if _, found, _ := packet.GetScalar("crs:Temperature"); !found {
		t.Fatal("planning removed an on-disk slider")
	}
}
