package lrtl

import (
	"bytes"
	"encoding/binary"
	"image"
	"math"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func dngTestASCII(value string) dngTestField {
	data := append([]byte(value), 0)
	return dngTestField{typeID: 2, count: uint32(len(data)), data: data}
}

func appendDNGCameraProfile(t testing.TB, data []byte, mainOrder, profileOrder binary.ByteOrder, name string, fields map[uint16]dngTestField) []byte {
	t.Helper()
	rootOffset := uint64(mainOrder.Uint32(data[4:8]))
	entryCount := uint64(mainOrder.Uint16(data[rootOffset : rootOffset+2]))
	profileOffset := uint32(len(data))
	profileListFound := false
	profileOffsetLocation := -1
	for index := uint64(0); index < entryCount; index++ {
		entryOffset := rootOffset + 2 + index*12
		if mainOrder.Uint16(data[entryOffset:]) != 50933 {
			continue
		}
		count := mainOrder.Uint32(data[entryOffset+4:])
		if mainOrder.Uint16(data[entryOffset+2:]) != 4 || count == 0 || count > maxPreviewIFDs {
			t.Fatal("ExtraCameraProfiles fixture must contain a bounded LONG list")
		}
		if count == 1 {
			profileOffsetLocation = int(entryOffset + 8)
		} else {
			listOffset := uint64(mainOrder.Uint32(data[entryOffset+8:]))
			if !tiffRange(listOffset, uint64(count)*4, uint64(len(data))) {
				t.Fatal("ExtraCameraProfiles fixture list is outside the file")
			}
			for item := uint32(0); item < count; item++ {
				location := listOffset + uint64(item)*4
				if mainOrder.Uint32(data[location:]) == 0 {
					profileOffsetLocation = int(location)
					break
				}
			}
		}
		profileListFound = true
		break
	}
	if !profileListFound {
		t.Fatal("fixture has no ExtraCameraProfiles tag")
	}
	if profileOffsetLocation < 0 {
		t.Fatal("ExtraCameraProfiles fixture has no empty offset")
	}
	mainOrder.PutUint32(data[profileOffsetLocation:], profileOffset)
	profileFields := fields
	fields = make(map[uint16]dngTestField, len(profileFields)+1)
	for tag, field := range profileFields {
		fields[tag] = field
	}
	fields[50936] = dngTestASCII(name)
	tags := make([]int, 0, len(fields))
	for tag := range fields {
		tags = append(tags, int(tag))
	}
	sort.Ints(tags)

	profileData := make([]byte, 8+2+len(tags)*12+4)
	if profileOrder == binary.LittleEndian {
		copy(profileData, "II")
	} else {
		copy(profileData, "MM")
	}
	profileOrder.PutUint16(profileData[2:], 0x4352)
	profileOrder.PutUint32(profileData[4:], 8)
	profileOrder.PutUint16(profileData[8:], uint16(len(tags)))
	for index, rawTag := range tags {
		tag := uint16(rawTag)
		field := fields[tag]
		entryOffset := 10 + index*12
		profileOrder.PutUint16(profileData[entryOffset:], tag)
		profileOrder.PutUint16(profileData[entryOffset+2:], field.typeID)
		profileOrder.PutUint32(profileData[entryOffset+4:], field.count)
		if len(field.data) > 4 {
			profileOrder.PutUint32(profileData[entryOffset+8:], uint32(len(profileData)))
			profileData = append(profileData, field.data...)
		} else {
			copy(profileData[entryOffset+8:entryOffset+12], field.data)
		}
	}
	return append(data, profileData...)
}

func testProfileGainMap2(order binary.ByteOrder, gain float32) dngTestField {
	return testDNGProfileGainTableMap2Field(order, 1, 1, 1, 0, 0, 0, 0,
		[5]float32{1, 0, 0, 0, 0}, 0, 1, gain, gain, []byte{0})
}

func TestLinearDNGUsesAsShotAndRequestedCameraProfileIFDs(t *testing.T) {
	mainOrder := binary.LittleEndian
	profileOrder := binary.BigEndian // Profile IFDs have their own byte order mark.
	pixels := []uint16{8192}
	metadata := map[uint16]dngTestField{
		50933: dngTestLong(mainOrder, 0),
		50936: dngTestASCII("Default"),
		52544: testProfileGainMap2(mainOrder, 1),
	}
	profile := map[uint16]dngTestField{52544: testProfileGainMap2(profileOrder, 2)}

	t.Run("as-shot name", func(t *testing.T) {
		fields := make(map[uint16]dngTestField, len(metadata)+1)
		for tag, field := range metadata {
			fields[tag] = field
		}
		fields[50934] = dngTestASCII("Night")
		data := linearDNGFixture(mainOrder, 1, 1, 1, pixels, fields)
		data = appendDNGCameraProfile(t, data, mainOrder, profileOrder, "Night", profile)
		assertDNGCameraProfilePixel(t, data, 2)
	})

	t.Run("XMP-selected name", func(t *testing.T) {
		data := linearDNGFixture(mainOrder, 1, 1, 1, pixels, metadata)
		data = appendDNGCameraProfile(t, data, mainOrder, profileOrder, "Night", profile)
		assertDNGCameraProfilePixel(t, data, 2, "Night")
	})

	t.Run("primary profile remains selected", func(t *testing.T) {
		data := linearDNGFixture(mainOrder, 1, 1, 1, pixels, metadata)
		data = appendDNGCameraProfile(t, data, mainOrder, profileOrder, "Night", profile)
		assertDNGCameraProfilePixel(t, data, 1, "Default")
	})

	t.Run("legacy gain map in selected profile", func(t *testing.T) {
		legacyMetadata := map[uint16]dngTestField{
			50933: dngTestLong(mainOrder, 0), 50936: dngTestASCII("Default"),
			52525: testDNGProfileGainTableMapField(mainOrder, 1, 1, 1, 0, 0, 0, 0,
				[5]float32{1, 0, 0, 0, 0}, 1),
		}
		legacyProfile := map[uint16]dngTestField{
			52525: testDNGProfileGainTableMapField(profileOrder, 1, 1, 1, 0, 0, 0, 0,
				[5]float32{1, 0, 0, 0, 0}, 2),
		}
		data := linearDNGFixture(mainOrder, 1, 1, 1, pixels, legacyMetadata)
		data = appendDNGCameraProfile(t, data, mainOrder, profileOrder, "Night", legacyProfile)
		assertDNGCameraProfilePixel(t, data, 2, "Night")
	})

	t.Run("selects matching profile from multi-entry list", func(t *testing.T) {
		listOrder := binary.LittleEndian
		listMetadata := map[uint16]dngTestField{
			50933: dngTestLong(listOrder, 0, 0), 50934: dngTestASCII("Night"),
			50936: dngTestASCII("Default"), 52544: testProfileGainMap2(listOrder, 1),
		}
		data := linearDNGFixture(listOrder, 1, 1, 1, pixels, listMetadata)
		data = appendDNGCameraProfile(t, data, listOrder, profileOrder, "Day", map[uint16]dngTestField{
			52544: testProfileGainMap2(profileOrder, 3),
		})
		data = appendDNGCameraProfile(t, data, listOrder, profileOrder, "Night", map[uint16]dngTestField{
			52544: testProfileGainMap2(profileOrder, 2),
		})
		assertDNGCameraProfilePixel(t, data, 2)
	})
}

func assertDNGCameraProfilePixel(t *testing.T, data []byte, gain float64, profile ...string) {
	t.Helper()
	name := ""
	if len(profile) > 0 {
		name = profile[0]
	}
	img, err := decodeLinearDNGWithProfile(data, name)
	if err != nil {
		t.Fatal(err)
	}
	linear := gain * 8192.0 / 65535
	encoded := 12.92 * linear
	if linear > .0031308 {
		encoded = 1.055*math.Pow(linear, 1/2.4) - .055
	}
	want := math.Round(encoded * 65535)
	pixel := img.NRGBA64At(0, 0)
	for _, channel := range []uint16{pixel.R, pixel.G, pixel.B} {
		if math.Abs(float64(channel)-want) > 2 {
			t.Fatalf("camera profile pixel=%v want neutral %g", pixel, want)
		}
	}
}

func TestLinearDNGCameraProfileSelectionFallsBackForExternalName(t *testing.T) {
	order := binary.LittleEndian
	data := linearDNGFixture(order, 1, 1, 1, []uint16{8192}, map[uint16]dngTestField{
		50933: dngTestLong(order, 0), 50936: dngTestASCII("Embedded"), 52544: testProfileGainMap2(order, 1),
	})
	data = appendDNGCameraProfile(t, data, order, order, "Night", map[uint16]dngTestField{52544: testProfileGainMap2(order, 2)})
	assertDNGCameraProfilePixel(t, data, 1, "External Profile Not Embedded")
}

func TestLinearDNGRejectsMalformedSelectedCameraProfileIFD(t *testing.T) {
	order := binary.LittleEndian
	for name, corrupt := range map[string]func([]byte){
		"byte order mark": func(profile []byte) { profile[0] = 'X' },
		"magic":           func(profile []byte) { order.PutUint16(profile[2:], 0) },
		"IFD before header": func(profile []byte) {
			order.PutUint32(profile[4:], 4)
		},
		"IFD outside profile": func(profile []byte) {
			order.PutUint32(profile[4:], ^uint32(0))
		},
	} {
		t.Run(name, func(t *testing.T) {
			data := linearDNGFixture(order, 1, 1, 1, []uint16{8192}, map[uint16]dngTestField{
				50933: dngTestLong(order, 0), 50934: dngTestASCII("Selected"),
				50936: dngTestASCII("Default"), 52544: testProfileGainMap2(order, 1),
			})
			profileOffset := len(data)
			data = appendDNGCameraProfile(t, data, order, order, "Selected", map[uint16]dngTestField{
				52544: testProfileGainMap2(order, 2),
			})
			corrupt(data[profileOffset:])
			if _, err := decodeLinearDNG(data); err == nil {
				t.Fatal("malformed selected Camera Profile IFD was accepted")
			}
		})
	}
}

func TestReadFrameCameraProfileSelection(t *testing.T) {
	dir := t.TempDir()
	imagePath := filepath.Join(dir, "frame.nef")
	sidecar := writeTestXMP(t, imagePath, map[string]string{"crs:CameraProfile": "Camera Landscape"})
	profile, err := readFrameCameraProfile(Frame{Image: imagePath, Sidecar: sidecar})
	if err != nil || profile != "Camera Landscape" {
		t.Fatalf("camera profile=%q err=%v", profile, err)
	}
}

func TestLinearDNGCameraProfileSignaturesGateCameraCalibration(t *testing.T) {
	order := binary.LittleEndian
	// CameraCalibration is applied only when its signature matches the
	// calibration signature of the selected profile.
	profileFields := map[uint16]dngTestField{
		50721: dngTestSRationalMatrix(order, [9]int32{1, 0, 0, 0, 1, 0, 0, 0, 1}),
		50932: dngTestASCII("profile-signature"),
	}
	metadata := map[uint16]dngTestField{
		50933: dngTestLong(order, 0), 50934: dngTestASCII("Selected"), 50936: dngTestASCII("Default"),
		50721: dngTestSRationalMatrix(order, [9]int32{1, 0, 0, 0, 1, 0, 0, 0, 1}),
		50723: dngTestSRationalMatrix(order, [9]int32{2, 0, 0, 0, 1, 0, 0, 0, 1}),
		50728: dngTestDouble(order, 1, 1, 1), 50778: dngTestShort(order, 21),
		50931: dngTestASCII("camera-signature"),
	}
	data := linearDNGFixture(order, 1, 1, 3, []uint16{8192, 8192, 8192}, metadata)
	data = appendDNGCameraProfile(t, data, order, order, "Selected", profileFields)
	fields, err := findLinearDNG(data)
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := dngCameraToSpace(fields, dngXYZD50ToSRGB)
	if err != nil {
		t.Fatal(err)
	}
	base := linearDNGFixture(order, 1, 1, 3, []uint16{8192, 8192, 8192}, map[uint16]dngTestField{
		50721: metadata[50721], 50728: metadata[50728], 50778: metadata[50778],
	})
	baseFields, err := findLinearDNG(base)
	if err != nil {
		t.Fatal(err)
	}
	want, _, err := dngCameraToSpace(baseFields, dngXYZD50ToSRGB)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("mismatched camera/profile signatures applied CameraCalibration: got %v want %v", got, want)
	}

	matchingMetadata := make(map[uint16]dngTestField, len(metadata))
	for tag, field := range metadata {
		matchingMetadata[tag] = field
	}
	matchingMetadata[50931] = dngTestASCII("profile-signature")
	matching := linearDNGFixture(order, 1, 1, 3, []uint16{8192, 8192, 8192}, matchingMetadata)
	matching = appendDNGCameraProfile(t, matching, order, order, "Selected", profileFields)
	matchingFields, err := findLinearDNG(matching)
	if err != nil {
		t.Fatal(err)
	}
	matched, _, err := dngCameraToSpace(matchingFields, dngXYZD50ToSRGB)
	if err != nil {
		t.Fatal(err)
	}
	if matched == got {
		t.Fatal("matching camera/profile signatures did not apply CameraCalibration")
	}
}

func dngTestSRationalMatrix(order binary.ByteOrder, values [9]int32) dngTestField {
	data := make([]byte, 9*8)
	for index, value := range values {
		order.PutUint32(data[index*8:], uint32(value))
		order.PutUint32(data[index*8+4:], 1)
	}
	return dngTestField{typeID: 10, count: 9, data: data}
}

func TestRawDevelopUsesSidecarCameraProfile(t *testing.T) {
	converter, dir, _ := fakeDNGConverter(t)
	order := binary.LittleEndian
	metadata := map[uint16]dngTestField{
		50933: dngTestLong(order, 0), 50936: dngTestASCII("Default"),
		52544: testProfileGainMap2(order, 1),
	}
	pixels := make([]uint16, 16*16)
	for index := range pixels {
		pixels[index] = 8192
	}
	fixture := linearDNGFixture(order, 16, 16, 1, pixels, metadata)
	fixture = appendDNGCameraProfile(t, fixture, order, binary.BigEndian, "Camera Landscape", map[uint16]dngTestField{
		52544: testProfileGainMap2(binary.BigEndian, 2),
	})
	fixturePath := filepath.Join(dir, "linear.dng")
	if err := os.WriteFile(fixturePath, fixture, 0600); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(dir, "source.nef")
	if err := os.WriteFile(source, []byte("raw source"), 0600); err != nil {
		t.Fatal(err)
	}
	sidecar := writeTestXMP(t, source, map[string]string{"crs:CameraProfile": "Camera Landscape"})
	data, config, format, err := readRenderInputWithOptions(Frame{Image: source, Sidecar: sidecar}, imageInputOptions{
		RawDevelop: true, DNGConverterPath: converter,
	})
	if err != nil {
		t.Fatal(err)
	}
	decoded, decodedFormat, err := image.Decode(bytes.NewReader(data))
	if err != nil || format != "tiff" || decodedFormat != "tiff" || config.Width != 16 || config.Height != 16 {
		t.Fatalf("developed input config=%v format=%q/%q err=%v", config, format, decodedFormat, err)
	}
	pixel := decoded.At(8, 8)
	red, _, _, _ := pixel.RGBA()
	if red < 30000 {
		t.Fatalf("sidecar CameraProfile was not applied; developed pixel=%v", pixel)
	}
}
