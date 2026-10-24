package lrtl

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func rafDimensionsObservations(t *testing.T) rawDimensionsReference {
	t.Helper()
	r := loadRawDimensionsReference(t, "testdata/lrt-raf-dimensions-7.5.5.json", 66, 0)
	if r.ExifToolExit != 1 {
		t.Fatal("missing provenance for rejected RAF header flag cases")
	}
	return r
}

func TestRAFDimensionsNativeCameraCacheAndSave(t *testing.T) {
	for _, row := range rafDimensionsObservations(t).Cases {
		t.Run(row.Name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), row.Name)
			if err := os.WriteFile(path, row.Data, 0600); err != nil {
				t.Fatal(err)
			}
			width, height, present, err := nativeImageSize(path)
			if err != nil || !present {
				t.Fatal(width, height, present, err)
			}
			fields := make([]string, 16)
			for i := range fields {
				fields[i] = "-"
			}
			fields[0], fields[1] = row.Name, row.Size
			expected, err := parseNativeCameraFields(fields)
			if err != nil {
				t.Fatal(err)
			}
			if width != expected.Width || height != expected.Height {
				t.Fatal("native ImageSize cache", width, height, row.Size)
			}
			packet := pasteTestPacket(t, row.Target)
			state, err := readNativeCameraMetadata(Frame{Image: path}, packet)
			if err != nil || state == nil || *state != expected {
				t.Fatal("camera metadata pipeline", state, expected, err)
			}
			if err := applyNativeCameraMetadata(packet, *state); err != nil {
				t.Fatal(err)
			}
			quality, err := nativeSequenceQuality(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := prepareNativeFrameState(packet, quality); err != nil {
				t.Fatal(err)
			}
			assertPastedMetadata(t, packet, row.Saved)
			if width > 0 && height > 0 {
				sizes, err := radialMaskFrameSizes([]*XMPPacket{EmptyXMPPacket()}, []Frame{{Image: path}})
				if err != nil || sizes[0] != (radialMaskSize{float64(width), float64(height)}) {
					t.Fatal("radial interpolation dimensions", sizes, err)
				}
			}
			assertPreviewEVFileBytes(t, path, row.Data)
		})
	}
}

func TestRAFDimensionsInstalledImageSizeAndSave(t *testing.T) {
	assertInstalledRawDimensions(t, rafDimensionsObservations(t))
}

func rafDimensionInput(t *testing.T, name string) []byte {
	t.Helper()
	for _, row := range rafDimensionsObservations(t).Cases {
		if row.Name == name {
			return append([]byte(nil), row.Data...)
		}
	}
	t.Fatal("missing RAF case", name)
	return nil
}

func TestRAFDimensionsMalformedPreflightAndReaderBounds(t *testing.T) {
	good := rafDimensionInput(t, "crop.raf")
	for _, kind := range []string{"short-header", "preview-offset", "preview-length", "directory-offset", "entry-header", "entry-length"} {
		t.Run(kind, func(t *testing.T) {
			data := append([]byte(nil), good...)
			directory := binary.BigEndian.Uint32(data[92:96])
			switch kind {
			case "short-header":
				data = data[:100]
			case "preview-offset":
				binary.BigEndian.PutUint32(data[84:88], 0x7fff0000)
			case "preview-length":
				binary.BigEndian.PutUint32(data[88:92], 0xffffffff)
			case "directory-offset":
				binary.BigEndian.PutUint32(data[92:96], 0xffffffff)
			case "entry-header":
				data = data[:directory+6]
			case "entry-length":
				binary.BigEndian.PutUint16(data[directory+6:directory+8], 0xffff)
			}
			if _, _, _, err := nativeRAFImageSizeAt(bytes.NewReader(data), int64(len(data))); err == nil {
				t.Fatal("accepted truncated/outside RAF dimensions")
			}
			dir := t.TempDir()
			for i, input := range [][]byte{good, data} {
				if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("frame-%d.raf", i)), input, 0600); err != nil {
					t.Fatal(err)
				}
			}
			before := batchFileSnapshot(t, dir)
			var out, stderr bytes.Buffer
			if Main([]string{"init", dir, "--write"}, &out, &stderr) == 0 {
				t.Fatal("accepted malformed later RAF", &out)
			}
			if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
				t.Fatal("dimension preflight failure changed files")
			}
		})
	}
}

func TestRAFDimensionsCLIProfilesBackupsAndAVIPixels(t *testing.T) {
	data := rafDimensionInput(t, "crop.raf")
	for _, profile := range []string{nativeSaveStateProfile, "preserve"} {
		t.Run(profile, func(t *testing.T) {
			dir, control := t.TempDir(), t.TempDir()
			var out, stderr bytes.Buffer
			for i := 0; i < 3; i++ {
				name := fmt.Sprintf("frame-%d", i)
				path := filepath.Join(dir, name+".RAF")
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
				preview, err := extractRawPreview(path, data)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(control, name+".jpg"), preview, 0600); err != nil {
					t.Fatal(err)
				}
				packet := EmptyXMPPacket()
				if err := packet.SetScalar("lrt:Width", "123"); err != nil {
					t.Fatal(err)
				}
				if _, err := WritePacket(filepath.Join(dir, name+".xmp"), packet, false); err != nil {
					t.Fatal(err)
				}
			}
			before := batchFileSnapshot(t, dir)
			args := []string{"init", dir, "--frame-state", profile}
			if Main(args, &out, &stderr) != 0 || !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
				t.Fatal("initialization preview changed files", &stderr)
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
				width, _, _ := packet.GetRootScalar("lrt:Width")
				height, heightPresent, _ := packet.GetRootScalar("lrt:Height")
				if profile == nativeSaveStateProfile {
					if width != "6000" || height != "4000" {
						t.Fatal("RAW crop not saved", width, height)
					}
				} else if width != "123" || heightPresent {
					t.Fatal("preserve changed cached dimensions", width, height)
				}
				assertPreviewEVFileBytes(t, frame.Image, data)
				backups, err := filepath.Glob(frame.Sidecar + "*.bak")
				if err != nil || len(backups) != 1 {
					t.Fatal(backups, err)
				}
				assertPreviewEVFileBytes(t, backups[0], before[frame.Sidecar])
			}
			after := batchFileSnapshot(t, dir)
			if Main(append(args, "--write"), &out, &stderr) != 0 || !reflect.DeepEqual(after, batchFileSnapshot(t, dir)) {
				t.Fatal("repeated initialization changed files", &stderr)
			}
			render := func(directory, name string, raw bool) []byte {
				path := filepath.Join(t.TempDir(), name+".avi")
				args := []string{"render", directory, "--output", path, "--apply-xmp-basic"}
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
			if !bytes.Equal(render(dir, "raw", true), render(control, "jpeg", false)) {
				t.Fatal("RAW crop metadata changed embedded preview render pixels")
			}
		})
	}
}

func FuzzRAFDimensions(f *testing.F) {
	var reference rawDimensionsReference
	input, err := os.ReadFile("testdata/lrt-raf-dimensions-7.5.5.json")
	if err != nil {
		f.Fatal(err)
	}
	if err := json.Unmarshal(input, &reference); err != nil {
		f.Fatal(err)
	}
	for _, row := range reference.Cases {
		f.Add(row.Data)
	}
	f.Add([]byte("FUJIFILMCCD-RAW "))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			return
		}
		original := append([]byte(nil), data...)
		w, h, present, err := nativeRAFImageSizeAt(bytes.NewReader(data), int64(len(data)))
		if err == nil && present {
			if w < 0 || h < 0 || w > 65535 || h > 65535 {
				t.Fatal("invalid RAF dimensions", w, h)
			}
			state, err := parseNativeCameraFields([]string{"frame", fmt.Sprintf("%dx%d", w, h)})
			if err != nil || state.Width != w || state.Height != h {
				t.Fatal("dimension cache changed", state, err)
			}
		}
		if preview, err := extractRAFPreview(data); err == nil {
			config, format, err := image.DecodeConfig(bytes.NewReader(preview))
			if err != nil || format != "jpeg" || config.Width < 1 || config.Height < 1 {
				t.Fatal("invalid extracted JPEG", config, format, err)
			}
		}
		if !bytes.Equal(original, data) {
			t.Fatal("reader changed input bytes")
		}
	})
}
