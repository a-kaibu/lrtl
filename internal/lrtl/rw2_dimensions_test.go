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

func TestRW2DimensionsUnusedJPEGAndCycle(t *testing.T) {
	var original []byte
	for _, row := range rw2DimensionsObservations(t).Cases {
		if row.Name == "le-jpeg-borders.rw2" {
			original = row.Data
		}
	}
	first := int(binary.LittleEndian.Uint32(original[4:8]))
	count := int(binary.LittleEndian.Uint16(original[first : first+2]))
	for _, kind := range []string{"unused-jpeg", "required-jpeg", "next-cycle"} {
		t.Run(kind, func(t *testing.T) {
			data := append([]byte(nil), original...)
			for i := 0; i < count; i++ {
				entry := first + 2 + i*12
				tag := binary.LittleEndian.Uint16(data[entry : entry+2])
				if tag == 46 && kind != "next-cycle" {
					binary.LittleEndian.PutUint32(data[entry+8:entry+12], uint32(len(data)+100))
				}
				if tag == 5 && kind == "required-jpeg" {
					binary.LittleEndian.PutUint16(data[entry:entry+2], 0x9999)
				}
			}
			if kind == "next-cycle" {
				binary.LittleEndian.PutUint32(data[first+2+count*12:], uint32(first))
			}
			path := filepath.Join(t.TempDir(), "frame.rw2")
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			width, height, present, err := nativeTIFFImageSize(path)
			if kind == "required-jpeg" {
				if err == nil {
					t.Fatal("accepted missing required JPEG bytes")
				}
			} else if err != nil || !present || width != 4000 || height != 3000 {
				t.Fatal(width, height, present, err)
			}
			assertPreviewEVFileBytes(t, path, data)
		})
	}
}

func TestRW2PreviewRenderAndLegacyExtensions(t *testing.T) {
	var data []byte
	for _, row := range rw2DimensionsObservations(t).Cases {
		if row.Name == "le-jpeg-borders.rw2" {
			data = row.Data
		}
	}
	for _, extension := range []string{".rw2", ".rwl", ".raw"} {
		t.Run(extension, func(t *testing.T) {
			dir, control := t.TempDir(), t.TempDir()
			var out, stderr bytes.Buffer
			for i := 0; i < 3; i++ {
				name := fmt.Sprintf("frame%d", i)
				path := filepath.Join(dir, name+extension)
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
			}
			before := batchFileSnapshot(t, dir)
			if Main([]string{"init", dir, "--write"}, &out, &stderr) != 0 {
				t.Fatal(&stderr)
			}
			for path, bytes := range before {
				assertPreviewEVFileBytes(t, path, bytes)
			}
			video := filepath.Join(t.TempDir(), "raw.avi")
			reference := filepath.Join(t.TempDir(), "jpeg.avi")
			if Main([]string{"render", dir, "--output", video, "--raw-preview", "--apply-xmp-basic"}, &out, &stderr) != 0 {
				t.Fatal(&stderr)
			}
			if Main([]string{"render", control, "--output", reference, "--apply-xmp-basic"}, &out, &stderr) != 0 {
				t.Fatal(&stderr)
			}
			raw, jpeg := denoiseRead(t, video), denoiseRead(t, reference)
			rawStart, jpegStart := bytes.Index(raw, []byte("movi")), bytes.Index(jpeg, []byte("movi"))
			if rawStart < 0 || jpegStart < 0 || !bytes.Equal(raw[rawStart:], jpeg[jpegStart:]) {
				t.Fatal("RAW preview AVI differs from extracted JPEG control")
			}
		})
	}
}

func rw2DimensionsObservations(t *testing.T) rawDimensionsReference {
	t.Helper()
	return loadRawDimensionsReference(t, "testdata/lrt-rw2-dimensions-7.5.5.json", 84, 4)
}

func TestRW2DimensionsNativeImageSizeAndSave(t *testing.T) {
	assertRawDimensionsReference(t, rw2DimensionsObservations(t))
}

func TestRW2DimensionsInstalledImageSizeAndSave(t *testing.T) {
	assertInstalledRawDimensions(t, rw2DimensionsObservations(t))
}

func TestRW2DimensionsInitializationAndPreflight(t *testing.T) {
	rows := rw2DimensionsObservations(t).Cases
	for _, name := range []string{"le-normal.rw2", "be-wide.rw2", "le-jpeg-only.rw2", "be-jpeg-exif.rw2", "le-family.raw", "be-family.rwl"} {
		t.Run(name, func(t *testing.T) {
			var data []byte
			size := ""
			for _, row := range rows {
				if row.Name == name {
					data, size = row.Data, row.Size
				}
			}
			if data == nil {
				t.Fatal("missing synthetic input", name)
			}
			dir := t.TempDir()
			path := filepath.Join(dir, name)
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			packet := EmptyXMPPacket()
			if err := packet.SetScalar("lrt:Width", "123"); err != nil {
				t.Fatal(err)
			}
			sidecar := path[:len(path)-len(filepath.Ext(path))] + ".xmp"
			if _, err := WritePacket(sidecar, packet, false); err != nil {
				t.Fatal(err)
			}
			before := batchFileSnapshot(t, dir)
			var out, stderr bytes.Buffer
			if Main([]string{"init", dir}, &out, &stderr) != 0 {
				t.Fatal(&stderr)
			}
			if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
				t.Fatal("preview wrote files")
			}
			if Main([]string{"init", dir, "--write"}, &out, &stderr) != 0 {
				t.Fatal(&stderr)
			}
			frames, err := DiscoverSequence(dir)
			if err != nil || len(frames) != 1 {
				t.Fatal(frames, err)
			}
			packet, err = PacketForFrame(frames[0])
			if err != nil {
				t.Fatal(err)
			}
			width, _, _ := packet.GetRootScalar("lrt:Width")
			height, _, _ := packet.GetRootScalar("lrt:Height")
			if width+"x"+height != size {
				t.Fatal("saved dimensions", width, height, size)
			}
			assertPreviewEVFileBytes(t, path, data)
			backups, err := filepath.Glob(sidecar + "*.bak")
			if err != nil || len(backups) != 1 {
				t.Fatal(backups, err)
			}
			assertPreviewEVFileBytes(t, backups[0], before[sidecar])
			after := batchFileSnapshot(t, dir)
			if Main([]string{"init", dir, "--write"}, &out, &stderr) != 0 {
				t.Fatal(&stderr)
			}
			if !reflect.DeepEqual(after, batchFileSnapshot(t, dir)) {
				t.Fatal("repeated initialization changed files")
			}
		})
	}
	dir := t.TempDir()
	for i, name := range []string{"le-normal.rw2", "le-positive-overflow.rw2"} {
		for _, row := range rows {
			if row.Name == name {
				if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("frame%d.rw2", i)), row.Data, 0600); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	before := batchFileSnapshot(t, dir)
	var out, stderr bytes.Buffer
	if Main([]string{"init", dir, "--write"}, &out, &stderr) == 0 {
		t.Fatal("accepted overflowing RW2 dimensions")
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("late dimension error wrote files")
	}
}

func FuzzRW2Dimensions(f *testing.F) {
	data, err := os.ReadFile("testdata/lrt-rw2-dimensions-7.5.5.json")
	if err != nil {
		f.Fatal(err)
	}
	var reference rawDimensionsReference
	if err := json.Unmarshal(data, &reference); err != nil {
		f.Fatal(err)
	}
	for i, row := range reference.Cases {
		if i%5 == 0 {
			f.Add(row.Data)
		}
	}
	// Each fuzz worker runs setup in its own process. Reuse its private file
	// instead of creating/deleting a directory for every generated input.
	path := filepath.Join(f.TempDir(), "frame.rw2")
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 32768 {
			return
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		width, height, present, err := nativeTIFFImageSize(path)
		if err == nil && present {
			state, err := parseNativeCameraFields([]string{"frame", fmt.Sprintf("%dx%d", width, height)})
			if err != nil {
				t.Fatal(err)
			}
			if state.Width != width || state.Height != height {
				t.Fatal("signed dimensions changed in cache")
			}
			packet := EmptyXMPPacket()
			if err := applyNativeCameraMetadata(packet, state); err != nil {
				t.Fatal(err)
			}
			serialized, err := packet.Serialize()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := parseXMP(serialized); err != nil {
				t.Fatal(err)
			}
		}
		if preview, err := extractRawPreview(path, data); err == nil {
			config, format, err := image.DecodeConfig(bytes.NewReader(preview))
			if err != nil || !supportedRasterFormat(format) || config.Width <= 0 || config.Height <= 0 {
				t.Fatal("invalid successful preview extraction", config, format, err)
			}
		}
		if !bytes.Equal(data, denoiseRead(t, path)) {
			t.Fatal("RW2 image was modified")
		}
	})
}
