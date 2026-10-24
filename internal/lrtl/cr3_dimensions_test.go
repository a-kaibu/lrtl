package lrtl

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func cr3DimensionsObservations(t *testing.T) rawDimensionsReference {
	t.Helper()
	return loadRawDimensionsReference(t, "testdata/lrt-cr3-dimensions-7.5.5.json", 76, 2)
}

func cr3DimensionInput(t *testing.T, name string) []byte {
	t.Helper()
	for _, row := range cr3DimensionsObservations(t).Cases {
		if row.Name == name {
			return append([]byte(nil), row.Data...)
		}
	}
	t.Fatal("missing CR3 fixture", name)
	return nil
}

func TestCR3DimensionsNativeCacheAndSave(t *testing.T) {
	for _, row := range cr3DimensionsObservations(t).Cases {
		t.Run(row.Name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), row.Name)
			if err := os.WriteFile(path, row.Data, 0600); err != nil {
				t.Fatal(err)
			}
			w, h, present, err := nativeImageSize(path)
			if row.Invalid {
				if err == nil {
					t.Fatal("accepted native integer overflow", w, h)
				}
				return
			}
			if err != nil || !present {
				t.Fatal(w, h, present, err)
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
			if w != expected.Width || h != expected.Height {
				t.Fatal("ImageSize cache", w, h, row.Size)
			}
			packet := pasteTestPacket(t, row.Target)
			state, err := readNativeCameraMetadata(Frame{Image: path}, packet)
			if err != nil || state == nil || *state != expected {
				t.Fatal("camera metadata", state, expected, err)
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
			if w > 0 && h > 0 {
				sizes, err := radialMaskFrameSizes([]*XMPPacket{EmptyXMPPacket()}, []Frame{{Image: path}})
				if err != nil || sizes[0] != (radialMaskSize{float64(w), float64(h)}) {
					t.Fatal(sizes, err)
				}
			}
			assertPreviewEVFileBytes(t, path, row.Data)
		})
	}
}

func TestCR3DimensionsInstalledImageSizeAndSave(t *testing.T) {
	assertInstalledRawDimensions(t, cr3DimensionsObservations(t))
}

func TestCR3DimensionsUnsupportedMetadataDoesNotPanic(t *testing.T) {
	data := cr3DimensionInput(t, "be-big.cr3")
	if _, err := decodeImageMetadata(bytes.NewReader(data)); err == nil {
		t.Fatal("unsupported CR3 metadata should report a decode error")
	}
	if _, err := readEXIFOrientation(data); err == nil {
		t.Fatal("unsupported orientation metadata should report a decode error")
	}
	path := filepath.Join(t.TempDir(), "frame.cr3")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	frame := Frame{Image: path}
	if _, _, err := captureTimeFromImage(frame); err == nil {
		t.Fatal("missing metadata time should report a decode error")
	}
	if _, err := readCameraExposureMetadata(frame); err == nil {
		t.Fatal("unsupported camera metadata should report a decode error")
	}
	if _, err := readHolyGrailLRTExposureMetadata(frame); err == nil {
		t.Fatal("unsupported precise camera metadata should report a decode error")
	}
	assertPreviewEVFileBytes(t, path, data)
}

func TestCR3DimensionsMalformedPreflight(t *testing.T) {
	good := cr3DimensionInput(t, "le-root.cr3")
	for _, kind := range []string{"short-box", "outside-parent", "extended-header", "extended-outside", "short-uuid", "bad-tiff", "entry-data", "overflow"} {
		t.Run(kind, func(t *testing.T) {
			data := append([]byte(nil), good...)
			moov := bytes.Index(data, []byte("moov")) - 4
			uuid := bytes.Index(data, []byte("uuid")) - 4
			cmt := bytes.Index(data, []byte("CMT1")) - 4
			switch kind {
			case "short-box":
				data = append(data, 0, 0, 0)
			case "outside-parent":
				binary.BigEndian.PutUint32(data[cmt:cmt+4], uint32(len(data)))
			case "extended-header":
				data = data[:moov+12]
				binary.BigEndian.PutUint32(data[moov:moov+4], 1)
			case "extended-outside":
				binary.BigEndian.PutUint32(data[moov:moov+4], 1)
				binary.BigEndian.PutUint64(data[moov+8:moov+16], ^uint64(0))
			case "short-uuid":
				binary.BigEndian.PutUint32(data[uuid:uuid+4], 12)
			case "bad-tiff":
				binary.LittleEndian.PutUint32(data[cmt+12:cmt+16], 0x7fffffff)
			case "entry-data":
				binary.LittleEndian.PutUint32(data[cmt+22:cmt+26], 2)
			case "overflow":
				data = cr3DimensionInput(t, "le-overflow.cr3")
			}
			if _, _, _, err := nativeCR3ImageSizeAt(bytes.NewReader(data), int64(len(data))); err == nil {
				t.Fatal("accepted malformed CR3 dimensions")
			}
			dir := t.TempDir()
			for i, input := range [][]byte{good, data} {
				if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("frame-%d.cr3", i)), input, 0600); err != nil {
					t.Fatal(err)
				}
			}
			before := batchFileSnapshot(t, dir)
			var out, stderr bytes.Buffer
			if Main([]string{"init", dir, "--write"}, &out, &stderr) == 0 {
				t.Fatal("accepted malformed later CR3", &out)
			}
			if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
				t.Fatal("preflight failure changed files")
			}
		})
	}
}

func TestCR3DimensionsSkipPayloadAndBoxLimits(t *testing.T) {
	good := cr3DimensionInput(t, "le-root.cr3")
	moov := bytes.Index(good, []byte("moov")) - 4
	data := append([]byte(nil), good...)
	copy(data[moov+4:moov+8], "mdat")
	if w, h, found, err := nativeCR3ImageSizeAt(bytes.NewReader(data), int64(len(data))); err != nil || !found || w != 0 || h != 0 {
		t.Fatal("scanned opaque payload", w, h, found, err)
	}
	data = append([]byte(nil), good[:moov]...)
	for i := 0; i < 4096; i++ {
		data = append(data, 0, 0, 0, 8, 'f', 'r', 'e', 'e')
	}
	if _, _, _, err := nativeCR3ImageSizeAt(bytes.NewReader(data), int64(len(data))); err == nil {
		t.Fatal("accepted excessive boxes")
	}
}

func TestCR3DimensionsCLIBackupsProfilesAndAVIPixels(t *testing.T) {
	data := cr3DimensionInput(t, "preview-cmt1.cr3")
	for _, profile := range []string{nativeSaveStateProfile, "preserve"} {
		t.Run(profile, func(t *testing.T) {
			dir, control := t.TempDir(), t.TempDir()
			var out, stderr bytes.Buffer
			for i := 0; i < 3; i++ {
				name := fmt.Sprintf("frame-%d", i)
				path := filepath.Join(dir, name+".CR3")
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
				w, _, _ := packet.GetRootScalar("lrt:Width")
				h, hasHeight, _ := packet.GetRootScalar("lrt:Height")
				if profile == nativeSaveStateProfile {
					if w != "6000" || h != "4000" {
						t.Fatal("metadata dimensions", w, h)
					}
				} else if w != "123" || hasHeight {
					t.Fatal("preserve changed dimensions", w, h)
				}
				backups, err := filepath.Glob(frame.Sidecar + "*.bak")
				if err != nil || len(backups) != 1 {
					t.Fatal(backups, err)
				}
				assertPreviewEVFileBytes(t, backups[0], before[frame.Sidecar])
				assertPreviewEVFileBytes(t, frame.Image, data)
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
					t.Fatal("no AVI payload")
				}
				return avi[offset:]
			}
			if !bytes.Equal(render(dir, true), render(control, false)) {
				t.Fatal("CR3 preview differs from extracted JPEG control")
			}
		})
	}
}

func FuzzCR3Dimensions(f *testing.F) {
	data, err := os.ReadFile("testdata/lrt-cr3-dimensions-7.5.5.json")
	if err != nil {
		f.Fatal(err)
	}
	var reference rawDimensionsReference
	if err := json.Unmarshal(data, &reference); err != nil {
		f.Fatal(err)
	}
	for _, row := range reference.Cases {
		f.Add(row.Data)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			return
		}
		original := append([]byte(nil), data...)
		w, h, found, err := nativeCR3ImageSizeAt(bytes.NewReader(data), int64(len(data)))
		if err == nil && found {
			if w < 0 || h < 0 {
				t.Fatal("negative dimensions", w, h)
			}
			state, err := parseNativeCameraFields([]string{"frame", fmt.Sprintf("%dx%d", w, h)})
			if err != nil || state.Width != w || state.Height != h {
				t.Fatal("dimension cache changed", state, err)
			}
		}
		if !bytes.Equal(original, data) {
			t.Fatal("reader changed source bytes")
		}
	})
}
