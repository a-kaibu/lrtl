package lrtl

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRawDimensionsRejectMalformedDirectories(t *testing.T) {
	var original []byte
	for _, row := range rawDimensionsObservations(t).Cases {
		if row.Name == "le-root.nef" {
			original = row.Data
		}
	}
	for _, kind := range []string{"outside-directory", "too-many-entries", "signed-width", "empty-width"} {
		t.Run(kind, func(t *testing.T) {
			data := append([]byte(nil), original...)
			switch kind {
			case "outside-directory":
				binary.LittleEndian.PutUint32(data[4:8], uint32(len(data)+1))
			case "too-many-entries":
				binary.LittleEndian.PutUint16(data[8:10], 65535)
			case "signed-width":
				binary.LittleEndian.PutUint16(data[12:14], 9)
			case "empty-width":
				binary.LittleEndian.PutUint32(data[14:18], 0)
			}
			path := filepath.Join(t.TempDir(), "frame.nef")
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			if _, _, _, err := nativeTIFFImageSize(path); err == nil {
				t.Fatal("accepted malformed TIFF dimensions", kind)
			}
			assertPreviewEVFileBytes(t, path, data)
		})
	}
}

type rawDimensionsReference struct {
	StoreCompleted bool `json:"store_completed"`
	Stop           string
	Cases          []struct {
		Name, Size, Target, Saved string
		Data                      []byte
		Invalid                   bool
	}
}

func rawDimensionsObservations(t *testing.T) rawDimensionsReference {
	t.Helper()
	return loadRawDimensionsReference(t, "testdata/lrt-raw-dimensions-7.5.5.json", 80, 2)
}

func loadRawDimensionsReference(t *testing.T, path string, count, invalidCount int) rawDimensionsReference {
	t.Helper()
	var reference rawDimensionsReference
	if err := json.Unmarshal(denoiseRead(t, path), &reference); err != nil {
		t.Fatal(err)
	}
	if reference.StoreCompleted || reference.Stop != "history: null LRTimelapseMain.m()" || len(reference.Cases) != count {
		t.Fatal("invalid raw dimension provenance")
	}
	seen := map[string]bool{}
	invalid := 0
	for _, row := range reference.Cases {
		if seen[row.Name] || filepath.Base(row.Name) != row.Name || len(row.Data) == 0 {
			t.Fatal("invalid synthetic image", row.Name)
		}
		seen[row.Name] = true
		if row.Invalid {
			invalid++
		}
	}
	if invalid != invalidCount {
		t.Fatal("native invalid-dimension count", invalid)
	}
	return reference
}

func TestRawDimensionsNativeImageSizeAndSave(t *testing.T) {
	assertRawDimensionsReference(t, rawDimensionsObservations(t))
}

func assertRawDimensionsReference(t *testing.T, reference rawDimensionsReference) {
	t.Helper()
	for _, row := range reference.Cases {
		t.Run(row.Name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), row.Name)
			if err := os.WriteFile(path, row.Data, 0600); err != nil {
				t.Fatal(err)
			}
			width, height, present, err := nativeTIFFImageSize(path)
			if (err != nil) != row.Invalid {
				t.Fatal(width, height, present, err, row.Invalid)
			}
			if err != nil {
				return
			}
			size := "-"
			if present {
				size = fmt.Sprintf("%dx%d", width, height)
			}
			if size != row.Size {
				t.Fatal("ImageSize", size, "want", row.Size)
			}
			if present && width > 0 && height > 0 {
				sizes, err := radialMaskFrameSizes([]*XMPPacket{EmptyXMPPacket()}, []Frame{{Image: path}})
				if err != nil || sizes[0] != (radialMaskSize{float64(width), float64(height)}) {
					t.Fatal("radial interpolation used different image dimensions", sizes, err)
				}
			}
			fields := make([]string, 16)
			for i := range fields {
				fields[i] = "-"
			}
			fields[0], fields[1] = row.Name, size
			state, err := parseNativeCameraFields(fields)
			if err != nil {
				t.Fatal(err)
			}
			packet := pasteTestPacket(t, row.Target)
			if err := applyNativeCameraMetadata(packet, state); err != nil {
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
			assertPreviewEVFileBytes(t, path, row.Data)
		})
	}
}

func TestRawDimensionsRadialCachedRootValues(t *testing.T) {
	packet := EmptyXMPPacket()
	for key, value := range map[string]string{"lrt:Width": "70000", "lrt:Height": "80000"} {
		if err := packet.SetScalar(key, value); err != nil {
			t.Fatal(err)
		}
	}
	sizes, err := radialMaskFrameSizes([]*XMPPacket{packet}, nil)
	if err != nil || sizes[0] != (radialMaskSize{70000, 80000}) {
		t.Fatal(sizes, err)
	}
	if err := packet.SetScalar("lrt:Width", "2147483648"); err != nil {
		t.Fatal(err)
	}
	if _, err := radialMaskFrameSizes([]*XMPPacket{packet}, nil); err == nil {
		t.Fatal("accepted overflowing cached width")
	}
	packet = pasteTestPacket(t, `<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"><rdf:Description xmlns:f="urn:lrtl:raw-dimensions" xmlns:exif="http://ns.adobe.com/exif/1.0/" xmlns:lrt="http://lrtimelapse.com/" lrt:Width="6000" lrt:Height="4000"><f:Nested rdf:parseType="Resource"><exif:PixelXDimension>12</exif:PixelXDimension><exif:PixelYDimension>34</exif:PixelYDimension></f:Nested></rdf:Description></rdf:RDF>`)
	sizes, err = radialMaskFrameSizes([]*XMPPacket{packet}, nil)
	if err != nil || sizes[0] != (radialMaskSize{6000, 4000}) {
		t.Fatal("nested dimensions shadowed global cache", sizes, err)
	}
}

func TestRawDimensionsInstalledImageSizeAndSave(t *testing.T) {
	assertInstalledRawDimensions(t, rawDimensionsObservations(t))
}

func assertInstalledRawDimensions(t *testing.T, reference rawDimensionsReference) {
	t.Helper()
	jar, exiftool := os.Getenv("LRTL_NATIVE_JAR"), os.Getenv("LRTL_ICC_EXIFTOOL")
	if jar == "" || exiftool == "" {
		t.Skip("set LRTL_NATIVE_JAR and LRTL_ICC_EXIFTOOL for installed ImageSize/save replay")
	}
	dir := t.TempDir()
	images := filepath.Join(dir, "images")
	if err := os.Mkdir(images, 0700); err != nil {
		t.Fatal(err)
	}
	for _, row := range reference.Cases {
		if err := os.WriteFile(filepath.Join(images, row.Name), row.Data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	root, err := filepath.Abs(filepath.Join("..", "..", "scripts", "probes"))
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"-encoding", "UTF-8", "-d", dir}
	for _, name := range []string{"MetadataPasteOracle.java", "SelectedColumnOracle.java", "RegisteredSaveOracle.java", "RawDimensionsOracle.java"} {
		args = append(args, filepath.Join(root, name))
	}
	if out, err := exec.Command("javac", args...).CombinedOutput(); err != nil {
		t.Fatalf("compile observer: %v\n%s", err, out)
	}
	path := filepath.Join(dir, "replay.json")
	if out, err := exec.Command("java", "--enable-native-access=ALL-UNNAMED", "-cp", dir+string(os.PathListSeparator)+jar, "lrtlprobe.RawDimensionsOracle", path, exiftool, images).CombinedOutput(); err != nil {
		t.Fatalf("native replay: %v\n%s", err, out)
	}
	var replay rawDimensionsReference
	if err := json.Unmarshal(denoiseRead(t, path), &replay); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(replay, reference) {
		t.Fatal("installed ImageSize/save results changed")
	}
}

func TestRawDimensionsInitializationProfilesAndPreflight(t *testing.T) {
	rows := rawDimensionsObservations(t).Cases
	var wide, overflow []byte
	for _, row := range rows {
		if row.Name == "le-wide.nef" {
			wide = row.Data
		}
		if row.Name == "le-overflow.nef" {
			overflow = row.Data
		}
	}
	for _, profile := range []string{nativeSaveStateProfile, "preserve"} {
		t.Run(profile, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "frame.nef")
			if err := os.WriteFile(path, wide, 0600); err != nil {
				t.Fatal(err)
			}
			packet := EmptyXMPPacket()
			if err := packet.SetScalar("lrt:Width", "123"); err != nil {
				t.Fatal(err)
			}
			if _, err := WritePacket(filepath.Join(dir, "frame.xmp"), packet, false); err != nil {
				t.Fatal(err)
			}
			before := batchFileSnapshot(t, dir)
			var out, stderr bytes.Buffer
			args := []string{"init", dir, "--frame-state", profile}
			if Main(args, &out, &stderr) != 0 {
				t.Fatal(&stderr)
			}
			if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
				t.Fatal("preview changed files")
			}
			if Main(append(args, "--write"), &out, &stderr) != 0 {
				t.Fatal(&stderr)
			}
			frames, err := DiscoverSequence(dir)
			if err != nil {
				t.Fatal(err)
			}
			packet, err = PacketForFrame(frames[0])
			if err != nil {
				t.Fatal(err)
			}
			want := "70000"
			if profile == "preserve" {
				want = "123"
			}
			if value, _, _ := packet.GetRootScalar("lrt:Width"); value != want {
				t.Fatal("width", value, want)
			}
			if profile == nativeSaveStateProfile {
				if value, _, _ := packet.GetRootScalar("lrt:Height"); value != "80000" {
					t.Fatal("height", value)
				}
			}
			assertPreviewEVFileBytes(t, path, wide)
			backups, err := filepath.Glob(filepath.Join(dir, "frame.xmp*.bak"))
			if err != nil || len(backups) != 1 {
				t.Fatal(backups, err)
			}
			assertPreviewEVFileBytes(t, backups[0], before[filepath.Join(dir, "frame.xmp")])
			after := batchFileSnapshot(t, dir)
			if Main(append(args, "--write"), &out, &stderr) != 0 {
				t.Fatal(&stderr)
			}
			if !reflect.DeepEqual(after, batchFileSnapshot(t, dir)) {
				t.Fatal("repeat wrote files")
			}
		})
	}
	dir := t.TempDir()
	for i, data := range [][]byte{wide, overflow} {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("frame%d.nef", i)), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	before := batchFileSnapshot(t, dir)
	var out, stderr bytes.Buffer
	if Main([]string{"init", dir, "--write"}, &out, &stderr) == 0 {
		t.Fatal("accepted out-of-range RAW dimensions")
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("late range error wrote files")
	}
}

func FuzzRawDimensions(f *testing.F) {
	var reference rawDimensionsReference
	data, err := os.ReadFile("testdata/lrt-raw-dimensions-7.5.5.json")
	if err != nil {
		f.Fatal(err)
	}
	if err := json.Unmarshal(data, &reference); err != nil {
		f.Fatal(err)
	}
	for i, row := range reference.Cases {
		if i%5 == 0 {
			f.Add(row.Data, filepath.Ext(row.Name) == ".tif")
		}
	}
	f.Fuzz(func(t *testing.T, data []byte, bigTIFF bool) {
		if len(data) > 32768 {
			return
		}
		suffix := ".nef"
		if bigTIFF {
			suffix = ".tif"
		}
		path := filepath.Join(t.TempDir(), "frame"+suffix)
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		width, height, present, err := nativeTIFFImageSize(path)
		if err == nil && present {
			state, parseErr := parseNativeCameraFields([]string{"frame", fmt.Sprintf("%dx%d", width, height)})
			if parseErr != nil || state.Width != width || state.Height != height {
				t.Fatal("dimension cache round trip", width, height, parseErr)
			}
		}
		if !bytes.Equal(data, denoiseRead(t, path)) {
			t.Fatal("dimension reader changed the image")
		}
	})
}
