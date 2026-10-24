package lrtl

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSaveStateRendererIgnoresNestedGlobalNames(t *testing.T) {
	frames := cropFixture(t, 1)
	dir := filepath.Dir(frames[0].Image)
	packet := pasteTestPacket(t, `<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"><rdf:Description xmlns:crs="http://ns.adobe.com/camera-raw-settings/1.0/" xmlns:f="urn:lrtl:save-state/" crs:WhiteBalance="Custom" crs:Temperature="4500" crs:CropLeft=".25" crs:HasCrop="False"><f:Nested><rdf:Description crs:Temperature="777" crs:CropRight=".5"/></f:Nested></rdf:Description></rdf:RDF></x:xmpmeta>`)
	if _, err := WritePacket(frames[0].Sidecar, packet, false); err != nil {
		t.Fatal(err)
	}
	source := EmptyXMPPacket()
	if err := source.SetScalar("crs:WhiteBalance", "As Shot"); err != nil {
		t.Fatal(err)
	}
	ref := filepath.Join(t.TempDir(), "source.xmp")
	if _, err := WritePacket(ref, source, false); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	if Main([]string{"copy-settings", ref, dir, "--properties", "WhiteBalance", "--write"}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	render := func(name string) []byte {
		path := filepath.Join(t.TempDir(), name+".avi")
		if Main([]string{"render", dir, "--output", path, "--apply-xmp-basic", "--apply-xmp-crop"}, &out, &stderr) != 0 {
			t.Fatal(&stderr)
		}
		data := denoiseRead(t, path)
		index := bytes.Index(data, []byte("movi"))
		if index < 0 {
			t.Fatal("no video frames")
		}
		return data[index:]
	}
	after := render("retained")
	packet, err := PacketForFrame(frames[0])
	if err != nil {
		t.Fatal(err)
	}
	nested := packet.findRootProperty("urn:lrtl:save-state/", "Nested").element
	if nested == nil {
		t.Fatal("normalization deleted unrelated nested metadata")
	}
	if !strings.Contains(string(denoiseRead(t, frames[0].Sidecar)), "777") {
		t.Fatal("nested WB value lost")
	}
	nested.Remove()
	if _, err := WritePacket(frames[0].Sidecar, packet, false); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, render("control")) {
		t.Fatal("nested metadata affected global WB or crop pixels")
	}
}

func TestSaveStateCLICropAndWBThroughAVIPixels(t *testing.T) {
	for _, kind := range []string{"crop", "wb"} {
		t.Run(kind, func(t *testing.T) {
			frames := cropFixture(t, 3)
			dir := filepath.Dir(frames[0].Image)
			reference := EmptyXMPPacket()
			property := "CropLeft"
			if kind == "crop" {
				if err := reference.SetScalar("crs:CropLeft", ".25"); err != nil {
					t.Fatal(err)
				}
			} else {
				property = "WhiteBalance"
				if err := reference.SetScalar("crs:WhiteBalance", "As Shot"); err != nil {
					t.Fatal(err)
				}
				for _, frame := range frames {
					p, err := PacketForFrame(frame)
					if err != nil {
						t.Fatal(err)
					}
					for key, value := range map[string]string{"crs:WhiteBalance": "Custom", "crs:Temperature": "4500", "crs:Tint": "20"} {
						if err := p.SetScalar(key, value); err != nil {
							t.Fatal(err)
						}
					}
					if _, err := WritePacket(frame.Sidecar, p, false); err != nil {
						t.Fatal(err)
					}
				}
			}
			ref := filepath.Join(t.TempDir(), "reference.xmp")
			if _, err := WritePacket(ref, reference, false); err != nil {
				t.Fatal(err)
			}
			originalRef := denoiseRead(t, ref)
			original := batchFileSnapshot(t, dir)
			var out, stderr bytes.Buffer
			render := func(name string) []byte {
				path := filepath.Join(t.TempDir(), name+".avi")
				if Main([]string{"render", dir, "--output", path, "--apply-xmp-crop", "--apply-xmp-basic"}, &out, &stderr) != 0 {
					t.Fatal(&stderr)
				}
				data := denoiseRead(t, path)
				index := bytes.Index(data, []byte("movi"))
				if index < 0 {
					t.Fatal("no AVI frames")
				}
				return data[index:]
			}
			before := render("before")
			args := []string{"copy-settings", ref, dir, "--properties", property}
			if Main(args, &out, &stderr) != 0 {
				t.Fatal(&stderr)
			}
			if !reflect.DeepEqual(original, batchFileSnapshot(t, dir)) {
				t.Fatal("dry run wrote metadata")
			}
			if Main(append(args, "--write"), &out, &stderr) != 0 {
				t.Fatal(&stderr)
			}
			if !strings.Contains(out.String(), "Save state: lrt-7.5.5") {
				t.Fatal("default profile not reported")
			}
			after := render("after")
			if bytes.Equal(before, after) {
				t.Fatal("native save state did not change video pixels")
			}
			for _, frame := range frames {
				p, err := PacketForFrame(frame)
				if err != nil {
					t.Fatal(err)
				}
				flag, _, _ := p.GetScalar("crs:HasCrop")
				wantFlag := "False"
				if kind == "crop" {
					wantFlag = "True"
				}
				if flag != wantFlag {
					t.Fatal("crop activation", flag, wantFlag)
				}
				wb, _, _ := p.GetScalar("crs:WhiteBalance")
				if wb != "As Shot" {
					t.Fatal("WB state", wb)
				}
				if kind == "wb" {
					for _, key := range []string{"Temperature", "Tint", "IncrementalTemperature", "IncrementalTint"} {
						if _, present, _ := p.GetScalar("crs:" + key); present {
							t.Fatal("inactive WB number retained", key)
						}
					}
				}
				backups, err := filepath.Glob(frame.Sidecar + "*.bak")
				if err != nil || len(backups) != 1 {
					t.Fatal("backup", backups, err)
				}
				assertPreviewEVFileBytes(t, backups[0], original[frame.Sidecar])
				assertPreviewEVFileBytes(t, frame.Image, original[frame.Image])
			}
			saved := batchFileSnapshot(t, dir)
			if Main(append(args, "--write"), &out, &stderr) != 0 {
				t.Fatal(&stderr)
			}
			if !reflect.DeepEqual(saved, batchFileSnapshot(t, dir)) {
				t.Fatal("repeat introduced intermediate-state or ordering writes")
			}
			// Independent explicit metadata control for the renderer.
			for _, frame := range frames {
				p, err := PacketForFrame(frame)
				if err != nil {
					t.Fatal(err)
				}
				if kind == "crop" {
					for key, value := range map[string]string{"crs:CropLeft": "0.25", "crs:CropTop": "0", "crs:CropRight": "1", "crs:CropBottom": "1", "crs:CropAngle": "0", "crs:HasCrop": "True"} {
						if err := p.SetScalar(key, value); err != nil {
							t.Fatal(err)
						}
					}
				} else {
					for _, key := range []string{"Temperature", "Tint", "IncrementalTemperature", "IncrementalTint"} {
						if err := p.RemoveProperty("crs:" + key); err != nil {
							t.Fatal(err)
						}
					}
					if err := p.SetScalar("crs:WhiteBalance", "As Shot"); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := WritePacket(frame.Sidecar, p, false); err != nil {
					t.Fatal(err)
				}
			}
			if !bytes.Equal(after, render("explicit")) {
				t.Fatal("save-state AVI differs from explicit WB/crop control")
			}
			assertPreviewEVFileBytes(t, ref, originalRef)
		})
	}
}

func TestSaveStateCLIPreserveAndQualityCache(t *testing.T) {
	for _, profile := range []string{nativeSaveStateProfile, "preserve"} {
		t.Run(profile, func(t *testing.T) {
			frames := cropFixture(t, 1)
			dir := filepath.Dir(frames[0].Image)
			p := EmptyXMPPacket()
			for key, value := range map[string]string{"lrt:Quality": "RAW", "crs:WhiteBalance": "Custom", "crs:Temperature": "5500", "crs:Tint": "20", "crs:IncrementalTemperature": "10", "crs:IncrementalTint": "-5"} {
				if err := p.SetScalar(key, value); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := WritePacket(frames[0].Sidecar, p, false); err != nil {
				t.Fatal(err)
			}
			source := EmptyXMPPacket()
			if err := source.SetScalar("lrt:Quality", "FLAT"); err != nil {
				t.Fatal(err)
			}
			ref := filepath.Join(t.TempDir(), "source.xmp")
			if _, err := WritePacket(ref, source, false); err != nil {
				t.Fatal(err)
			}
			var out, stderr bytes.Buffer
			if Main([]string{"copy-settings", ref, dir, "--properties", "lrt:Quality", "--save-state", profile, "--write"}, &out, &stderr) != 0 {
				t.Fatal(&stderr)
			}
			p, err := PacketForFrame(frames[0])
			if err != nil {
				t.Fatal(err)
			}
			quality, _, _ := p.GetScalar("lrt:Quality")
			want := "RAW"
			if profile == "preserve" {
				want = "FLAT"
			}
			if quality != want {
				t.Fatal("cached quality was not respected", quality, want)
			}
			_, relative, _ := p.GetScalar("crs:IncrementalTemperature")
			if relative != (profile == "preserve") {
				t.Fatal("wrong cached quality WB cleanup")
			}
		})
	}
}

func TestSaveStateCLIProfilesAndLateValidation(t *testing.T) {
	frames := cropFixture(t, 2)
	dir := filepath.Dir(frames[0].Image)
	ref := pasteReferenceFile(t)
	for _, flags := range [][]string{{"--save-state", "unknown"}, {"--save-state", ""}, {"--list-properties", "--save-state", "preserve"}} {
		var out, stderr bytes.Buffer
		before := batchFileSnapshot(t, dir)
		args := append([]string{"copy-settings", ref, dir, "--write"}, flags...)
		if Main(args, &out, &stderr) == 0 {
			t.Fatal("invalid save profile accepted", flags)
		}
		if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
			t.Fatal("invalid profile wrote files")
		}
	}
	bad := pasteTestPacket(t, `<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"><rdf:Description xmlns:crs="http://ns.adobe.com/camera-raw-settings/1.0/"><crs:WhiteBalance rdf:parseType="Resource"><crs:Name>Custom</crs:Name></crs:WhiteBalance></rdf:Description></rdf:RDF></x:xmpmeta>`)
	if _, err := WritePacket(frames[1].Sidecar, bad, false); err != nil {
		t.Fatal(err)
	}
	before := batchFileSnapshot(t, dir)
	var out, stderr bytes.Buffer
	if Main([]string{"copy-settings", ref, dir, "--properties", "Exposure2012", "--write"}, &out, &stderr) == 0 {
		t.Fatal("invalid late WB state accepted")
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("late save-state error wrote earlier frames")
	}
}

func FuzzNativeSaveState(f *testing.F) {
	var reference saveStateReference
	data, err := os.ReadFile("testdata/lrt-save-state-7.5.5.json")
	if err != nil {
		f.Fatal(err)
	}
	if err := json.Unmarshal(data, &reference); err != nil {
		f.Fatal(err)
	}
	empty, _ := EmptyXMPPacket().Serialize()
	for i, row := range reference.WB {
		if i%11 == 0 {
			f.Add(row.Source, row.Source, "WhiteBalance", false)
			f.Add(row.Source, string(empty), "", true)
		}
	}
	for i, row := range reference.Crop {
		if i%12 == 0 {
			f.Add(row.Source, row.Source, "CropLeft", false)
		}
	}
	for _, row := range reference.CopyCrop {
		f.Add(row.Source, row.Destination, row.Properties, false)
	}
	var fullDates fullCopyDateReference
	dateData, err := os.ReadFile("testdata/lrt-full-paste-date-7.5.5.json")
	if err != nil {
		f.Fatal(err)
	}
	if err := json.Unmarshal(dateData, &fullDates); err != nil {
		f.Fatal(err)
	}
	for _, row := range fullDates.Cases {
		f.Add(row.Source, row.Target, "", false)
	}
	f.Fuzz(func(t *testing.T, sourceText, targetText, properties string, preserve bool) {
		if len(sourceText) > 32768 || len(targetText) > 32768 || len(properties) > 4096 {
			t.Skip()
		}
		source, err := parseXMP([]byte(sourceText))
		if err != nil {
			return
		}
		target, err := parseXMP([]byte(targetText))
		if err != nil {
			return
		}
		var columns []selectedColumn
		if properties != "" {
			columns, err = parseSelectedColumns(properties)
			if err != nil {
				return
			}
		}
		originalSource, err := source.Serialize()
		if err != nil {
			return
		}
		originalTarget, err := target.Serialize()
		if err != nil {
			return
		}
		profile := nativeSaveStateProfile
		if preserve {
			profile = "preserve"
		}
		image := []string{"frame.nef", "frame.jpg", "frame.png"}[len(sourceText)%3]
		_, copyErr := applyCopySettingsProfile(target, source, columns, profile, image)
		afterSource, err := source.Serialize()
		if err != nil || !bytes.Equal(originalSource, afterSource) {
			t.Fatal("source changed", err)
		}
		afterTarget, err := target.Serialize()
		if err != nil {
			t.Fatal(err)
		}
		if copyErr != nil {
			if !bytes.Equal(originalTarget, afterTarget) {
				t.Fatal("failed composition mutated target")
			}
			return
		}
		reloaded, err := parseXMP(afterTarget)
		if err != nil {
			t.Fatal("generated invalid XML", err)
		}
		actualNodes, err := pastePacketNodes(target)
		if err != nil {
			t.Fatal(err)
		}
		roundtripNodes, err := pastePacketNodes(reloaded)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(pasteTestCanonical(actualNodes), pasteTestCanonical(roundtripNodes)) {
			t.Fatal("serialization changed metadata")
		}
		if profile == nativeSaveStateProfile {
			context, err := readNativeSaveStateContext(reloaded)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := applyNativeSaveState(reloaded, context); err != nil {
				t.Fatal(err)
			}
			frozen, err := reloaded.Serialize()
			if err != nil {
				t.Fatal(err)
			}
			second := pasteTestPacket(t, string(frozen))
			if changed, err := applyNativeSaveState(second, context); err != nil || changed {
				t.Fatal("frozen native cache changed on repeat", changed, err)
			}
		}
	})
}
