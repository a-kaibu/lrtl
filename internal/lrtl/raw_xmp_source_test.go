package lrtl

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func rawXMPSourceObservations(t testing.TB) xmpSourceReference {
	t.Helper()
	file, err := os.Open("testdata/lrt-raw-xmp-source-7.5.5.json.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	var reference xmpSourceReference
	if err := json.NewDecoder(reader).Decode(&reference); err != nil {
		t.Fatal(err)
	}
	if !reference.Completed || len(reference.Cases) != 420 {
		t.Fatal("invalid native RAW file-reader observations")
	}
	seen := map[string]bool{}
	for _, row := range reference.Cases {
		if seen[row.Name] || filepath.Base(row.Name) != row.Name || filepath.Base(row.Filename) != row.Filename || len(row.Data) == 0 {
			t.Fatal("invalid native input", row.Name)
		}
		seen[row.Name] = true
		if (row.Sidecar == nil) != (row.SidecarTimeNS == nil) || (row.Sidecar == nil) != (row.SidecarTimeMS == nil) {
			t.Fatal("missing timestamp", row.Name)
		}
	}
	return reference
}

func TestRawXMPSourceNativeFileReader(t *testing.T) {
	strict := 0
	for _, row := range rawXMPSourceObservations(t).Cases {
		t.Run(row.Name, func(t *testing.T) {
			frame := writeXMPSourceObservation(t, t.TempDir(), row)
			before := batchFileSnapshot(t, filepath.Dir(frame.Image))
			packet, found, _, err := loadFramePacketSource(frame, true)
			if !reflect.DeepEqual(before, batchFileSnapshot(t, filepath.Dir(frame.Image))) {
				t.Fatal("reader changed files")
			}
			if err != nil {
				if row.Sidecar != nil && *row.Sidecar == "<invalid" && row.Result == nil {
					strict++
					return
				}
				t.Fatal(err)
			}
			if row.Result == nil {
				if found {
					t.Fatal("native loader returned no packet")
				}
				return
			}
			if !found || packet == nil {
				t.Fatal("missing native packet")
			}
			assertPastedMetadata(t, packet, *row.Result)
		})
	}
	if strict != 168 {
		t.Fatal("selected-sidecar strictness count", strict)
	}
}

func TestRawXMPSourceInstalledFileReader(t *testing.T) {
	assertInstalledXMPSourceReference(t, rawXMPSourceObservations(t))
}

func rawXMPSourceInput(t *testing.T, name string) xmpSourceObservation {
	t.Helper()
	for _, row := range rawXMPSourceObservations(t).Cases {
		if row.Name == name {
			return row
		}
	}
	t.Fatal("missing RAW source input", name)
	return xmpSourceObservation{}
}

func TestRawXMPSourceInitializationProfilesAndAVIPixels(t *testing.T) {
	for _, name := range []string{"le-nef-none", "le-cr2-none", "le-arw-none", "le-orf-none", "raf-none", "cr3-none"} {
		row := rawXMPSourceInput(t, name)
		var profileVideos [][]byte
		for _, profile := range []string{nativeSaveStateProfile, "preserve"} {
			t.Run(name+"/"+profile, func(t *testing.T) {
				dir, control := t.TempDir(), t.TempDir()
				var out, stderr bytes.Buffer
				for i := 0; i < 3; i++ {
					base := fmt.Sprintf("frame-%02d", i)
					path := filepath.Join(dir, base+strings.ToUpper(filepath.Ext(row.Filename)))
					if err := os.WriteFile(path, row.Data, 0600); err != nil {
						t.Fatal(err)
					}
					preview, err := extractRawPreview(path, row.Data)
					if err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(control, base+".jpg"), preview, 0600); err != nil {
						t.Fatal(err)
					}
					packet := EmptyXMPPacket()
					exposure := "0"
					if profile == "preserve" {
						exposure = "1"
					}
					if err := packet.SetScalar("crs:Exposure2012", exposure); err != nil {
						t.Fatal(err)
					}
					if _, err := WritePacket(filepath.Join(control, base+".xmp"), packet, false); err != nil {
						t.Fatal(err)
					}
				}
				before := batchFileSnapshot(t, dir)
				args := []string{"init", dir, "--frame-state", profile}
				if Main(args, &out, &stderr) != 0 || !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
					t.Fatal("preview changed files", &stderr)
				}
				if Main(append(args, "--write"), &out, &stderr) != 0 {
					t.Fatal(&stderr)
				}
				frames, err := DiscoverSequence(dir)
				if err != nil || len(frames) != 3 {
					t.Fatal(frames, err)
				}
				for _, frame := range frames {
					packet, err := PacketForFrame(frame)
					if err != nil {
						t.Fatal(err)
					}
					nodes, err := pastePacketNodes(packet)
					if err != nil {
						t.Fatal(err)
					}
					origin := selectedField(nodes, pasteName{"urn:lrtl:xmp-source", "Origin"})
					if profile == "preserve" {
						if origin == nil || origin.Value != "embedded" {
							t.Fatal("preserve lost image metadata", origin)
						}
					} else if origin != nil {
						t.Fatal("native init adopted RAW image packet", origin)
					}
					value, found, err := packet.GetRootScalar("crs:Exposure2012")
					if err != nil {
						t.Fatal(err)
					}
					if profile == "preserve" {
						if !found || value != "1" {
							t.Fatal("preserve exposure", value, found)
						}
					} else if found {
						number, valid := scalarNumber(value)
						if !valid || number != 0 {
							t.Fatal("native init adopted image exposure", value)
						}
					}
					assertPreviewEVFileBytes(t, frame.Image, row.Data)
				}
				after := batchFileSnapshot(t, dir)
				if Main(append(args, "--write"), &out, &stderr) != 0 || !reflect.DeepEqual(after, batchFileSnapshot(t, dir)) {
					t.Fatal("repeat changed files", &stderr)
				}
				render := func(d string, raw bool) []byte {
					path := filepath.Join(t.TempDir(), "video.avi")
					args := []string{"render", d, "--output", path, "--apply-xmp-basic"}
					if raw {
						args = append(args, "--raw-preview")
					}
					if Main(args, &out, &stderr) != 0 {
						t.Fatal(&stderr)
					}
					avi := denoiseRead(t, path)
					offset := bytes.Index(avi, []byte("movi"))
					if offset < 0 {
						t.Fatal("missing AVI payload")
					}
					return avi[offset:]
				}
				video := render(dir, true)
				if !bytes.Equal(video, render(control, false)) {
					t.Fatal("RAW initialization pixels differ from explicit exposure control")
				}
				profileVideos = append(profileVideos, video)
			})
		}
		if len(profileVideos) == 2 && bytes.Equal(profileVideos[0], profileVideos[1]) {
			t.Fatal("profile source change had no visible effect", name)
		}
	}
}

func TestRawXMPSourceSelectedSidecarPreflight(t *testing.T) {
	for _, name := range []string{"le-nef-none", "raf-none", "cr3-none"} {
		t.Run(name, func(t *testing.T) {
			row := rawXMPSourceInput(t, name)
			dir := t.TempDir()
			for i := 0; i < 2; i++ {
				path := filepath.Join(dir, fmt.Sprintf("frame-%d%s", i, filepath.Ext(row.Filename)))
				if err := os.WriteFile(path, row.Data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(dir, "frame-1.xmp"), []byte("<invalid"), 0600); err != nil {
				t.Fatal(err)
			}
			before := batchFileSnapshot(t, dir)
			var out, stderr bytes.Buffer
			if Main([]string{"init", dir, "--write"}, &out, &stderr) == 0 {
				t.Fatal("accepted selected invalid RAW sidecar")
			}
			if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
				t.Fatal("preflight failure changed files")
			}
		})
	}
}

func FuzzRawXMPSourcePolicy(f *testing.F) {
	for _, extension := range []string{"nef", "cr2", "arw", "orf", "rw2", "raf", "cr3", "iiq", "dng", "jpg", "jpeg", "tif", "tiff", "png", "NEF", "TIF"} {
		f.Add(extension, []byte("<invalid"), true)
	}
	dir := f.TempDir()
	f.Fuzz(func(t *testing.T, extension string, data []byte, native bool) {
		if len(data) > 16384 {
			return
		}
		if extension == "" || len(extension) > 12 || strings.EqualFold(extension, "xmp") {
			return
		}
		for _, r := range extension {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
				return
			}
		}
		path := filepath.Join(dir, "frame."+extension)
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		defer os.Remove(path)
		packet, found, _, err := loadFramePacketSource(Frame{Image: path}, native)
		if native && !nativeEmbeddedFrameXMP(path) {
			if err != nil || found || packet != nil {
				t.Fatal("native RAW fallback parsed image metadata", found, err)
			}
		}
		if err == nil && found {
			serialized, err := packet.Serialize()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := parseXMP(serialized); err != nil {
				t.Fatal(err)
			}
		}
		assertPreviewEVFileBytes(t, path, data)
	})
}
