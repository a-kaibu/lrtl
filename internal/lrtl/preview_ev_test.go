package lrtl

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/image/tiff"
)

func TestPreviewEVKnownHistogramMeans(t *testing.T) {
	// Expectations are measured from installed LRT 7.5.5, including its
	// exclusive white bound, weighted smoothing and population normalization.
	for _, test := range []struct {
		name  string
		pixel color.NRGBA
		count int
		want  float64
	}{
		{"middle_gray", color.NRGBA{128, 128, 128, 255}, 5, 0.499603271484375},
		{"black_edge", color.NRGBA{0, 0, 0, 255}, 5, 0.0023345947265625},
		{"white_edge", color.NRGBA{255, 255, 255, 255}, 5, 255.0 / 256},
		{"rgb_mean", color.NRGBA{64, 128, 192, 255}, 5, 0.499603271484375},
		{"red_edges", color.NRGBA{255, 0, 0, 255}, 5, 255.0 / 256},
		{"integer_rounding", color.NRGBA{0, 0, 0, 255}, 4, 0.002918243408203125},
		{"all_bins_truncate_to_zero", color.NRGBA{128, 128, 128, 255}, 1, 255.0 / 256},
		{"unassociated_alpha", color.NRGBA{64, 128, 192, 128}, 5, 0.499603271484375},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Non-zero image origins must not skip pixels or sample outside bounds.
			img := image.NewNRGBA(image.Rect(7, 11, 7+test.count, 12))
			for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
				img.SetNRGBA(x, 11, test.pixel)
			}
			got, err := framePreviewEV(img)
			if err != nil || math.Abs(got-test.want) > 1e-12 {
				t.Fatalf("PreviewEV=%g, want %g, err=%v", got, test.want, err)
			}
		})
	}
}

func TestPreviewEVMixedHistogramAnd16BitInput(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 12, 1))
	for x := 6; x < 12; x++ {
		img.SetGray(x, 0, color.Gray{Y: 255})
	}
	// Native case 56 excludes white pixels and measures the remaining black bins.
	got, err := framePreviewEV(img)
	if err != nil || math.Abs(got-0.002593994140625) > 1e-12 {
		t.Fatalf("mixed PreviewEV=%g err=%v", got, err)
	}
	wide := image.NewNRGBA64(image.Rect(0, 0, 5, 1))
	for x := 0; x < 5; x++ {
		wide.SetNRGBA64(x, 0, color.NRGBA64{R: 0x4040, G: 0x8080, B: 0xc0c0, A: 0xffff})
	}
	got, err = framePreviewEV(wide)
	if err != nil || math.Abs(got-0.499603271484375) > 1e-12 {
		t.Fatalf("16-bit PreviewEV=%g err=%v", got, err)
	}
	if _, err := framePreviewEV(image.NewNRGBA(image.Rectangle{})); err == nil {
		t.Fatal("empty image accepted")
	}
}

func writePreviewEVFixture(t *testing.T, path string, img image.Image) {
	t.Helper()
	var data bytes.Buffer
	var err error
	switch filepath.Ext(path) {
	case ".jpg":
		err = jpeg.Encode(&data, img, &jpeg.Options{Quality: 100})
	case ".tif":
		err = tiff.Encode(&data, img, nil)
	default:
		err = png.Encode(&data, img)
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
}

func previewEVGray(value uint8, width, height int) *image.Gray {
	img := image.NewGray(image.Rect(0, 0, width, height))
	for index := range img.Pix {
		img.Pix[index] = value
	}
	return img
}

func TestPreviewEVSequenceDecodesRasterFormats(t *testing.T) {
	for _, ext := range []string{".png", ".jpg", ".tif"} {
		t.Run(ext, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "frame"+ext)
			writePreviewEVFixture(t, path, previewEVGray(128, 20, 20))
			values, err := analyzePreviewEVSequence([]Frame{{Image: path}}, imageInputOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if len(values) != 1 || math.Abs(values[0]-0.4990196228027344) > 1e-12 {
				t.Fatalf("decoded values=%v, want native case 54", values)
			}
		})
	}
}

func TestPreviewEVCLIWritePreservesMetadataAndBacksUp(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "frame01.png")
	second := filepath.Join(dir, "frame02.png")
	writePreviewEVFixture(t, first, previewEVGray(128, 20, 20))
	writePreviewEVFixture(t, second, previewEVGray(64, 20, 20))
	sidecar := writeTestXMP(t, first, map[string]string{
		"lrt:ExternalExposureDefault": "99", "xmp:Rating": "1", "crs:Exposure2012": "1.25",
	})
	originalXMP, err := os.ReadFile(sidecar)
	if err != nil {
		t.Fatal(err)
	}
	// Include an unknown namespace to ensure this command preserves data
	// outside the edited LRT scalar, not just its known Camera Raw fields.
	originalPacket, err := parseXMP(originalXMP)
	if err != nil {
		t.Fatal(err)
	}
	note := originalPacket.Descriptions()[0].CreateElement("vendor:Note")
	note.CreateAttr("xmlns:vendor", "urn:preview-ev-test:vendor")
	note.SetText("keep & preserve")
	originalXMP, err = originalPacket.Serialize()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sidecar, originalXMP, 0600); err != nil {
		t.Fatal(err)
	}
	originalImage, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := Main([]string{"preview-ev", dir}, &stdout, &stderr); code != 0 {
		t.Fatalf("preview exit=%d stderr=%s", code, &stderr)
	}
	if !strings.Contains(stdout.String(), "PreviewEV="+formatNumber(0.4990196228027344)) || !strings.Contains(stdout.String(), "Preview only") {
		t.Fatalf("unexpected preview output: %s", &stdout)
	}
	assertPreviewEVFileBytes(t, sidecar, originalXMP)
	if _, err := os.Stat(second + ".xmp"); !os.IsNotExist(err) {
		t.Fatalf("preview created a sidecar: %v", err)
	}
	stdout.Reset()
	if code := Main([]string{"preview-ev", dir, "--write"}, &stdout, &stderr); code != 0 {
		t.Fatalf("write exit=%d stderr=%s", code, &stderr)
	}
	packet, err := PacketForSidecar(sidecar)
	if err != nil {
		t.Fatal(err)
	}
	for property, want := range map[string]string{
		"lrt:ExternalExposureDefault": formatNumber(4 * 0.4990196228027344), "xmp:Rating": "1", "crs:Exposure2012": "1.25",
	} {
		got, found, err := packet.GetScalar(property)
		if err != nil || !found || got != want {
			t.Errorf("%s=%q found=%v err=%v, want %q", property, got, found, err, want)
		}
	}
	updatedXMP, err := packet.Serialize()
	if err != nil || !bytes.Contains(updatedXMP, []byte("urn:preview-ev-test:vendor")) || !bytes.Contains(updatedXMP, []byte("keep &amp; preserve")) {
		t.Fatalf("unknown vendor property was lost: %s err=%v", updatedXMP, err)
	}
	packet, err = PacketForSidecar(second + ".xmp")
	if err != nil {
		t.Fatal(err)
	}
	if value, found, err := packet.GetScalar("lrt:ExternalExposureDefault"); err != nil || !found || value != formatNumber(4*0.24999618530273438) {
		t.Fatalf("new sidecar value=%q found=%v err=%v", value, found, err)
	}
	backups, err := filepath.Glob(sidecar + ".lrtl-*.bak")
	if err != nil || len(backups) != 1 {
		t.Fatalf("backups=%v err=%v", backups, err)
	}
	assertPreviewEVFileBytes(t, backups[0], originalXMP)
	assertPreviewEVFileBytes(t, first, originalImage)
	stdout.Reset()
	if code := Main([]string{"keyframes", dir}, &stdout, &stderr); code != 0 {
		t.Fatalf("keyframes exit=%d stderr=%s", code, &stderr)
	}
	if !strings.Contains(stdout.String(), "source: LRT XMP") {
		t.Fatalf("generated inputs were not consumed by keyframes: %s", &stdout)
	}
}

func assertPreviewEVFileBytes(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("file %s was changed or unreadable: %v", path, err)
	}
}

func TestPreviewEVCLIRejectsBadSequenceBeforeWriting(t *testing.T) {
	for _, failure := range []string{"bad_xmp", "structured_xmp", "bad_raster", "dimensions"} {
		t.Run(failure, func(t *testing.T) {
			dir := t.TempDir()
			first, second := filepath.Join(dir, "frame01.png"), filepath.Join(dir, "frame02.png")
			writePreviewEVFixture(t, first, previewEVGray(128, 20, 20))
			writePreviewEVFixture(t, second, previewEVGray(128, 20, 20))
			sidecar := writeTestXMP(t, first, map[string]string{"lrt:ExternalExposureDefault": "99"})
			original, err := os.ReadFile(sidecar)
			if err != nil {
				t.Fatal(err)
			}
			path := second + ".xmp"
			if failure == "bad_raster" {
				path = second
			} else if failure == "dimensions" {
				writePreviewEVFixture(t, second, previewEVGray(128, 21, 20))
			}
			if failure != "dimensions" {
				invalid := []byte("invalid")
				if failure == "structured_xmp" {
					invalid = []byte(`<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"><rdf:Description xmlns:lrt="http://lrtimelapse.com/"><lrt:ExternalExposureDefault><rdf:Seq><rdf:li>1</rdf:li></rdf:Seq></lrt:ExternalExposureDefault></rdf:Description></rdf:RDF>`)
				}
				if err := os.WriteFile(path, invalid, 0600); err != nil {
					t.Fatal(err)
				}
			}
			var stdout, stderr bytes.Buffer
			if code := Main([]string{"preview-ev", dir, "--write"}, &stdout, &stderr); code == 0 {
				t.Fatal("invalid sequence accepted")
			}
			assertPreviewEVFileBytes(t, sidecar, original)
			backups, err := filepath.Glob(sidecar + ".lrtl-*.bak")
			if err != nil || len(backups) != 0 {
				t.Fatalf("preflight produced backups: %v err=%v", backups, err)
			}
		})
	}
}

func TestPreviewEVCLIValidatesOptions(t *testing.T) {
	dir := t.TempDir()
	writePreviewEVFixture(t, filepath.Join(dir, "frame.png"), previewEVGray(128, 20, 20))
	for _, test := range []struct {
		arguments []string
		message   string
	}{
		{nil, "Usage:"},
		{[]string{dir, "--no-backup"}, "--no-backup requires --write"},
		{[]string{dir, "--raw-preview", "--raw-develop"}, "cannot be used together"},
		{[]string{dir, "--dnglab", "unused"}, "--dnglab requires --raw-develop"},
		{[]string{dir, "--apply-xmp-basic", "--apply-xmp-exposure"}, "cannot be combined"},
		{[]string{dir, "--unknown-option"}, "flag provided but not defined"},
		{[]string{t.TempDir()}, "sequence contains no images"},
	} {
		t.Run(test.message, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := Main(append([]string{"preview-ev"}, test.arguments...), &stdout, &stderr); code == 0 {
				t.Fatalf("invalid options accepted: %v", test.arguments)
			}
			if !strings.Contains(stderr.String(), test.message) {
				t.Fatalf("expected %q error, got %s", test.message, &stderr)
			}
		})
	}
}

func TestPreviewEVXMPAdjustmentsAreOptIn(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "frame.png")
	img := previewEVGray(0, 20, 10)
	for y := 0; y < 10; y++ {
		for x := 10; x < 20; x++ {
			img.SetGray(x, y, color.Gray{Y: 128})
		}
	}
	writePreviewEVFixture(t, path, img)
	writeTestXMP(t, path, map[string]string{
		"crs:Exposure2012": "1", "crs:HasCrop": "True", "crs:CropLeft": "0.5",
		"crs:CropRight": "1", "crs:CropTop": "0", "crs:CropBottom": "1",
	})
	frames, err := DiscoverSequence(dir)
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := analyzePreviewEVSequence(frames, imageInputOptions{})
	if err != nil {
		t.Fatal(err)
	}
	cropped, err := analyzePreviewEVSequence(frames, imageInputOptions{ApplyXMPCrop: true})
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(cropped[0]-0.4990196228027344) > 1e-12 || baseline[0] >= cropped[0] {
		t.Fatalf("crop should measure only gray half: baseline=%v crop=%v", baseline, cropped)
	}
	for _, options := range []imageInputOptions{{ApplyXMPExposure: true}, {ApplyXMPBasic: true}} {
		adjusted, err := analyzePreviewEVSequence(frames, options)
		if err != nil {
			t.Fatal(err)
		}
		if adjusted[0] <= baseline[0] {
			t.Fatalf("+1 EV did not raise measured value: baseline=%v adjusted=%v", baseline, adjusted)
		}
	}
}

func TestPreviewEVEmbeddedRAWPreview(t *testing.T) {
	var jpegData bytes.Buffer
	if err := jpeg.Encode(&jpegData, previewEVGray(128, 20, 20), &jpeg.Options{Quality: 100}); err != nil {
		t.Fatal(err)
	}
	// Minimal TIFF envelope with JPEGInterchangeFormat/Length IFD entries.
	// This exercises the preview container reader without proprietary RAW data.
	data := make([]byte, 38)
	copy(data, "II")
	binary.LittleEndian.PutUint16(data[2:], 42)
	binary.LittleEndian.PutUint32(data[4:], 8)
	binary.LittleEndian.PutUint16(data[8:], 2)
	for index, entry := range []struct{ tag, value uint32 }{{513, 38}, {514, uint32(jpegData.Len())}} {
		offset := 10 + index*12
		binary.LittleEndian.PutUint16(data[offset:], uint16(entry.tag))
		binary.LittleEndian.PutUint16(data[offset+2:], 4)
		binary.LittleEndian.PutUint32(data[offset+4:], 1)
		binary.LittleEndian.PutUint32(data[offset+8:], entry.value)
	}
	path := filepath.Join(t.TempDir(), "frame.dng")
	if err := os.WriteFile(path, append(data, jpegData.Bytes()...), 0600); err != nil {
		t.Fatal(err)
	}
	frames := []Frame{{Image: path}}
	if _, err := analyzePreviewEVSequence(frames, imageInputOptions{}); err == nil {
		t.Fatal("RAW accepted without an explicit input mode")
	}
	values, err := analyzePreviewEVSequence(frames, imageInputOptions{RawPreview: true})
	if err != nil || len(values) != 1 || math.Abs(values[0]-0.4990196228027344) > 1e-12 {
		t.Fatalf("embedded preview values=%v err=%v", values, err)
	}
}

func TestPreviewEVCLINoBackupAndHelp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "frame.png")
	writePreviewEVFixture(t, path, previewEVGray(128, 20, 20))
	sidecar := writeTestXMP(t, path, map[string]string{"lrt:ExternalExposureDefault": "99"})
	var stdout, stderr bytes.Buffer
	if code := Main([]string{"preview-ev", "--write", "--no-backup", dir}, &stdout, &stderr); code != 0 {
		t.Fatalf("write without backup exit=%d stderr=%s", code, &stderr)
	}
	packet, err := PacketForSidecar(sidecar)
	if err != nil {
		t.Fatal(err)
	}
	if value, found, err := packet.GetScalar("lrt:ExternalExposureDefault"); err != nil || !found || value != formatNumber(4*0.4990196228027344) {
		t.Fatalf("no-backup did not save: value=%q found=%v err=%v", value, found, err)
	}
	backups, err := filepath.Glob(sidecar + ".lrtl-*.bak")
	if err != nil || len(backups) != 0 {
		t.Fatalf("unexpected backups=%v err=%v", backups, err)
	}
	stdout.Reset()
	if code := Main([]string{"preview-ev", "--help"}, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "Usage: lrtl preview-ev") {
		t.Fatalf("help exit=%d stdout=%s stderr=%s", code, &stdout, &stderr)
	}
}

func TestMarkKeyframeProtectsHolyGrailRatings(t *testing.T) {
	for _, rating := range []string{"2", "3"} {
		t.Run(rating, func(t *testing.T) {
			packet := EmptyXMPPacket()
			if err := packet.SetScalar("xmp:Rating", rating); err != nil {
				t.Fatal(err)
			}
			before, err := packet.Serialize()
			if err != nil {
				t.Fatal(err)
			}
			for _, kind := range []string{"default", "crop"} {
				if _, err := MarkKeyframe(packet, kind); err == nil {
					t.Fatalf("Holy Grail %s-star rating overwritten by %s", rating, kind)
				}
			}
			after, err := packet.Serialize()
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("rejected edit changed packet: %v", err)
			}
		})
	}
}
