package lrtl

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestFrameStateInitializationProfilesAndSequenceQuality(t *testing.T) {
	for _, first := range []string{"jpg", "jpeg", "tiff", "nef", "dng"} {
		t.Run(first, func(t *testing.T) {
			dir := t.TempDir()
			for i, extension := range []string{first, "png"} {
				data := []byte("synthetic metadata-only image")
				if extension == "dng" {
					data = frameStateNeutralTIFF(binary.LittleEndian, 0, 0, nil, false)
				}
				path := filepath.Join(dir, "frame00"+string(rune('1'+i))+"."+extension)
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
				p := EmptyXMPPacket()
				for name, value := range map[string]string{"lrt:Quality": "old", "crs:WhiteBalance": "Custom", "crs:CropLeft": ".25", "crs:HasCrop": "False", "xmp:Rating": "5"} {
					if err := p.SetScalar(name, value); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := WritePacket(path+".xmp", p, false); err != nil {
					t.Fatal(err)
				}
			}
			original := batchFileSnapshot(t, dir)
			var out, stderr bytes.Buffer
			if Main([]string{"init", dir}, &out, &stderr) != 0 {
				t.Fatal(&stderr)
			}
			if !reflect.DeepEqual(original, batchFileSnapshot(t, dir)) {
				t.Fatal("dry run wrote metadata")
			}
			if !strings.Contains(out.String(), "Frame state: lrt-7.5.5") {
				t.Fatal(&out)
			}
			if Main([]string{"init", dir, "--write"}, &out, &stderr) != 0 {
				t.Fatal(&stderr)
			}
			frames, err := DiscoverSequence(dir)
			if err != nil {
				t.Fatal(err)
			}
			quality := "FLAT"
			if first == "nef" {
				quality = "RAW"
			}
			for _, frame := range frames {
				p, err := PacketForFrame(frame)
				if err != nil {
					t.Fatal(err)
				}
				want := map[string]string{"lrt:Quality": quality, "crs:WhiteBalance": "Custom", "crs:HasCrop": "True", "xmp:Rating": "5"}
				if quality == "RAW" {
					want["crs:Temperature"] = "5850"
					want["crs:Tint"] = "8"
				} else {
					want["crs:IncrementalTemperature"] = "0"
					want["crs:IncrementalTint"] = "0"
				}
				for name, expected := range want {
					if value, present, _ := p.GetRootScalar(name); !present || value != expected {
						t.Fatal(name, value, expected)
					}
				}
				assertPreviewEVFileBytes(t, frame.Image, original[frame.Image])
				backups, err := filepath.Glob(frame.Sidecar + "*.bak")
				if err != nil || len(backups) != 1 {
					t.Fatal(backups, err)
				}
				assertPreviewEVFileBytes(t, backups[0], original[frame.Sidecar])
			}
			saved := batchFileSnapshot(t, dir)
			if Main([]string{"init", dir, "--write"}, &out, &stderr) != 0 {
				t.Fatal(&stderr)
			}
			if !reflect.DeepEqual(saved, batchFileSnapshot(t, dir)) {
				t.Fatal("repeat init wrote files")
			}
		})
	}
	frames := cropFixture(t, 1)
	dir := filepath.Dir(frames[0].Image)
	var out, stderr bytes.Buffer
	if Main([]string{"init", dir, "--frame-state", "preserve", "--write"}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	p, err := PacketForFrame(frames[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, present, _ := p.GetRootScalar("lrt:Quality"); present {
		t.Fatal("preserve classified quality")
	}
	before := batchFileSnapshot(t, dir)
	for _, args := range [][]string{{"init", dir, "--frame-state", "bad", "--write"}, {"init", dir, "--no-backup"}} {
		if Main(args, &out, &stderr) == 0 {
			t.Fatal("accepted invalid options", args)
		}
		if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
			t.Fatal("invalid options wrote files")
		}
	}
}

func TestFrameStateInitializationLateErrorAndRootScope(t *testing.T) {
	frames := cropFixture(t, 3)
	dir := filepath.Dir(frames[0].Image)
	p := pasteTestPacket(t, `<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"><rdf:Description xmlns:crs="http://ns.adobe.com/camera-raw-settings/1.0/" xmlns:f="urn:lrtl:frame-state/"><f:Nested><rdf:Description crs:Version="99" crs:WhiteBalance="Custom" crs:Temperature="777"/></f:Nested><crs:Tint rdf:parseType="Resource"><crs:Keep>20</crs:Keep></crs:Tint></rdf:Description></rdf:RDF>`)
	if _, err := WritePacket(frames[2].Sidecar, p, false); err != nil {
		t.Fatal(err)
	}
	before := batchFileSnapshot(t, dir)
	var out, stderr bytes.Buffer
	if Main([]string{"init", dir, "--write"}, &out, &stderr) == 0 {
		t.Fatal("accepted invalid late WB numeric structure")
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("preflight wrote earlier images")
	}
	if err := p.RemoveProperty("crs:Tint"); err != nil {
		t.Fatal(err)
	}
	if _, err := WritePacket(frames[2].Sidecar, p, false); err != nil {
		t.Fatal(err)
	}
	if Main([]string{"init", dir, "--write"}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	p, err := PacketForFrame(frames[2])
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"Version": "14", "WhiteBalance": "As Shot"} {
		if value, found, _ := p.GetRootScalar("crs:" + name); !found || value != want {
			t.Fatal(name, value)
		}
	}
	if !strings.Contains(string(denoiseRead(t, frames[2].Sidecar)), "777") {
		t.Fatal("nested metadata removed")
	}
}

func TestFrameStateRelativeWBThroughAVI(t *testing.T) {
	frames := cropFixture(t, 3)
	dir := filepath.Dir(frames[0].Image)
	var out, stderr bytes.Buffer
	if Main([]string{"init", dir, "--write"}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	render := func(name string) []byte {
		path := filepath.Join(t.TempDir(), name+".avi")
		if Main([]string{"render", dir, "--output", path, "--apply-xmp-basic"}, &out, &stderr) != 0 {
			t.Fatal(&stderr)
		}
		data := denoiseRead(t, path)
		index := bytes.Index(data, []byte("movi"))
		if index < 0 {
			t.Fatal("no video frames")
		}
		return data[index:]
	}
	before := render("before")
	if Main([]string{"set-sequence", dir, "IncrementalTemperature", "25", "--write"}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	if Main([]string{"set-sequence", dir, "IncrementalTint", "20", "--write"}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	after := render("relative")
	if bytes.Equal(before, after) {
		t.Fatal("relative WB had no video effect")
	}
	for _, frame := range frames {
		p, err := PacketForFrame(frame)
		if err != nil {
			t.Fatal(err)
		}
		for name, value := range map[string]string{"lrt:Quality": "RAW", "crs:Temperature": "7500", "crs:Tint": "20"} {
			if err := p.SetScalar(name, value); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := WritePacket(frame.Sidecar, p, false); err != nil {
			t.Fatal(err)
		}
	}
	if !bytes.Equal(after, render("explicit-srgb-control")) {
		t.Fatal("relative approximation differs from explicit mapping")
	}
}

func TestFrameStateKeyframeChoosesQuality(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		rawIndex, firstRating int
		quality               string
	}{
		{"second-flat-keyframe", 0, 5, "FLAT"},
		{"second-raw-keyframe", 1, 5, "RAW"},
		{"first-raw-keyframe", 0, 4, "RAW"},
		{"first-flat-keyframe", 1, 4, "FLAT"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			frames := cropFixture(t, 2)
			dir := filepath.Dir(frames[0].Image)
			frame := frames[tc.rawIndex]
			rawPath := strings.TrimSuffix(frame.Image, filepath.Ext(frame.Image)) + ".nef"
			if err := os.Rename(frame.Image, rawPath); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(frame.Sidecar, rawPath+".xmp"); err != nil {
				t.Fatal(err)
			}
			frames, err := DiscoverSequence(dir)
			if err != nil {
				t.Fatal(err)
			}
			for i, frame := range frames {
				packet, err := PacketForFrame(frame)
				if err != nil {
					t.Fatal(err)
				}
				rating := "4"
				if i == 0 && tc.firstRating == 5 {
					rating = "5"
				}
				if err := packet.SetScalar("xmp:Rating", rating); err != nil {
					t.Fatal(err)
				}
				if _, err := WritePacket(frame.Sidecar, packet, false); err != nil {
					t.Fatal(err)
				}
			}
			var out, stderr bytes.Buffer
			if Main([]string{"init", dir, "--write"}, &out, &stderr) != 0 {
				t.Fatal(&stderr)
			}
			for _, frame := range frames {
				packet, err := PacketForFrame(frame)
				if err != nil {
					t.Fatal(err)
				}
				if quality, found, _ := packet.GetRootScalar("lrt:Quality"); !found || quality != tc.quality {
					t.Fatal("wrong reference quality", quality, tc.quality)
				}
			}
		})
	}
}
