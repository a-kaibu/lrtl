package lrtl

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func readExportedTIFF(t *testing.T, path string) image.Image {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil || format != "tiff" {
		t.Fatalf("TIFF decode: %s %v", format, err)
	}
	// Inspect the actual TIFF tags independently of the image decoder.
	if string(data[:2]) != "MM" {
		t.Fatal("unexpected TIFF byte order")
	}
	order := binary.BigEndian
	offset := int(order.Uint32(data[4:8]))
	count := int(order.Uint16(data[offset:]))
	found := map[uint16]bool{}
	for i := 0; i < count; i++ {
		entry := data[offset+2+i*12:]
		tag := order.Uint16(entry)
		switch tag {
		case 258:
			bits := int(order.Uint32(entry[8:]))
			if order.Uint32(entry[4:]) != 3 || !bytes.Equal(data[bits:bits+6], []byte{0, 16, 0, 16, 0, 16}) {
				t.Fatal("TIFF is not RGB16")
			}
			found[tag] = true
		case 259, 274:
			if order.Uint16(entry[8:]) != 1 {
				t.Fatalf("tag %d must be one", tag)
			}
			found[tag] = true
		}
	}
	if len(found) != 3 {
		t.Fatal("missing TIFF precision/compression/orientation tags")
	}
	return img
}

func TestExportTIFFRetainsRGB16AndAlpha(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "original.png")
	img := image.NewNRGBA64(image.Rect(0, 0, 32, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 32; x++ {
			alpha := []uint16{65535, 32768, 0}[x%3]
			img.SetNRGBA64(x, y, color.NRGBA64{uint16(0x1234 + x), uint16(0x5678 + y), 0x9abc, alpha})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	frames, err := DiscoverSequence(dir)
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "TIFF frames")
	if err := ExportTIFFSequence(frames, output, RenderOptions{}); err != nil {
		t.Fatal(err)
	}
	got := readExportedTIFF(t, filepath.Join(output, "frame000001.tif"))
	for y := 0; y < 16; y++ {
		for x := 0; x < 32; x++ {
			p := img.NRGBA64At(x, y)
			r, g, b, a := got.At(x, y).RGBA()
			if r != uint32(p.R)*uint32(p.A)/65535 || g != uint32(p.G)*uint32(p.A)/65535 || b != uint32(p.B)*uint32(p.A)/65535 || a != 65535 {
				t.Fatalf("pixel %d,%d quantized or incorrect alpha: %x,%x,%x,%x", x, y, r, g, b, a)
			}
		}
	}
	var manifest struct {
		Version, BitDepth int
		ColorSpace        string
		Frames            []exportedFrame
	}
	data, err := os.ReadFile(filepath.Join(output, "frames.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Version != 1 || manifest.BitDepth != 16 || manifest.ColorSpace != "sRGB" || !reflect.DeepEqual(manifest.Frames, []exportedFrame{{Source: "original.png", File: "frame000001.tif"}}) {
		t.Fatalf("manifest=%+v", manifest)
	}
	assertPreviewEVFileBytes(t, path, encoded.Bytes())
	before := batchFileSnapshot(t, output)
	if err := ExportTIFFSequence(frames, output, RenderOptions{}); err == nil {
		t.Fatal("existing output replaced")
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, output)) {
		t.Fatal("existing export changed")
	}
}

func TestExportTIFFMatchesVideoRGB48Pipeline(t *testing.T) {
	for _, kind := range []string{"plain", "basic-crop-resize", "motion-overlay", "deflicker"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			for i := 0; i < 5; i++ {
				img := image.NewNRGBA64(image.Rect(0, 0, 64, 32))
				for y := 0; y < 32; y++ {
					for x := 0; x < 64; x++ {
						v := uint16(3000 + i*400 + x*13 + y*23)
						img.SetNRGBA64(x, y, color.NRGBA64{v, v + 1000, v + 2000, 65535})
					}
				}
				var data bytes.Buffer
				if err := png.Encode(&data, img); err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(dir, fmt.Sprintf("frame%d.png", i))
				if err := os.WriteFile(path, data.Bytes(), 0600); err != nil {
					t.Fatal(err)
				}
				writeTestXMP(t, path, map[string]string{"crs:Exposure2012": ".35", "crs:Contrast2012": "15", "crs:Temperature": "6000", "crs:Tint": "10", "crs:PerspectiveRotate": "1.2", "crs:PerspectiveScale": "105", "crs:HasCrop": "True", "crs:CropLeft": ".1", "crs:CropRight": ".9", "crs:CropTop": ".1", "crs:CropBottom": ".9", "crs:CropAngle": "2", "exif:DateTimeOriginal": fmt.Sprintf("2025-01-02T03:04:%02d", i)})
			}
			before := batchFileSnapshot(t, dir)
			frames, err := DiscoverSequence(dir)
			if err != nil {
				t.Fatal(err)
			}
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			capture := filepath.Join(t.TempDir(), "rgb.bin")
			t.Setenv("LRTL_TEST_FFMPEG_CAPTURE", capture)
			options := RenderOptions{FPS: 24, Quality: 90, FFmpegPath: executable, AutoOrient: true}
			switch kind {
			case "basic-crop-resize":
				options.ApplyXMPBasic = true
				options.ApplyXMPCrop = true
				options.OutputHeight = 16
				options.AspectRatio = 1.5
			case "motion-overlay":
				options.MotionBlurFrames = 3
				options.ApplyXMPExposure = true
				options.Watermark = "Test"
				options.TimestampOverlay = true
			case "deflicker":
				options.Deflicker = true
				options.SmoothRadius = 2
				options.DeflickerPasses = 2
				options.DeflickerSmoother = "lowess"
				options.Constant = true
			}
			if err := RenderSequence(frames, filepath.Join(t.TempDir(), "video.mp4"), options); err != nil {
				t.Fatal(err)
			}
			expected, err := os.ReadFile(capture)
			if err != nil {
				t.Fatal(err)
			}
			output := filepath.Join(t.TempDir(), "export")
			if err := ExportTIFFSequence(frames, output, options); err != nil {
				t.Fatal(err)
			}
			var got []byte
			for i := range frames {
				img := readExportedTIFF(t, filepath.Join(output, fmt.Sprintf("frame%06d.tif", i+1)))
				for y := 0; y < img.Bounds().Dy(); y++ {
					for x := 0; x < img.Bounds().Dx(); x++ {
						r, g, b, _ := img.At(x, y).RGBA()
						for _, v := range []uint32{r, g, b} {
							got = binary.LittleEndian.AppendUint16(got, uint16(v))
						}
					}
				}
			}
			// TIFF keeps every source correspondence; video removes its warm-up.
			videoPixels := got[options.MotionBlurFrames*(len(got)/len(frames)):]
			if !bytes.Equal(videoPixels, expected) {
				t.Fatalf("export pixels differ from video pipeline: got %d bytes want %d", len(got), len(expected))
			}
			if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
				t.Fatal("export modified source images or XMP")
			}
		})
	}
}

func TestExportTIFFFailureLeavesNoPartialDirectory(t *testing.T) {
	for _, kind := range []string{"late-corrupt-image", "late-size-mismatch", "invalid-xmp", "bad-options"} {
		t.Run(kind, func(t *testing.T) {
			dir, parent := t.TempDir(), t.TempDir()
			writeRenderFrame(t, filepath.Join(dir, "frame1.png"), "png", 32, 16)
			path := filepath.Join(dir, "frame2.png")
			writeRenderFrame(t, path, "png", 32, 16)
			options := RenderOptions{ApplyXMPExposure: true}
			switch kind {
			case "late-corrupt-image":
				if err := os.WriteFile(path, []byte("broken PNG"), 0600); err != nil {
					t.Fatal(err)
				}
			case "late-size-mismatch":
				writeRenderFrame(t, path, "png", 16, 16)
			case "invalid-xmp":
				writeTestXMP(t, path, map[string]string{"crs:Exposure2012": "NaN"})
			case "bad-options":
				options.ApplyXMPBasic = true
			}
			frames, err := DiscoverSequence(dir)
			if err != nil {
				t.Fatal(err)
			}
			before := batchFileSnapshot(t, dir)
			if err := ExportTIFFSequence(frames, filepath.Join(parent, "output"), options); err == nil {
				t.Fatal("invalid export accepted")
			}
			entries, err := os.ReadDir(parent)
			if err != nil || len(entries) != 0 {
				t.Fatalf("partial/staged export retained: %v %v", entries, err)
			}
			if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
				t.Fatal("failed export modified source")
			}
		})
	}
}

func TestExportTIFFCLIAndRAWDevelopment(t *testing.T) {
	executable, dir, sourceData := fakeDNGConverter(t)
	source := filepath.Join(dir, "source.nef")
	if err := os.WriteFile(source, sourceData, 0600); err != nil {
		t.Fatal(err)
	}
	writeTestXMP(t, source, map[string]string{"crs:Exposure2012": "1"})
	output := filepath.Join(t.TempDir(), "developed")
	var out, stderr bytes.Buffer
	if Main([]string{"export", dir, "--output", output, "--filename-contains", "SOURCE.NEF", "--raw-develop", "--dng-converter", executable, "--apply-xmp-exposure"}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	img := readExportedTIFF(t, filepath.Join(output, "frame000001.tif"))
	r, g, b, _ := img.At(0, 0).RGBA()
	linear := 2 * 16384.0 / 65535
	expected := 65535 * (1.055*math.Pow(linear, 1/2.4) - .055)
	if math.Abs(float64(r)-expected) > 3 || r != g || g != b {
		t.Fatalf("developed/exposure pixels=%d,%d,%d want %g", r, g, b, expected)
	}
	assertPreviewEVFileBytes(t, source, sourceData)
	var args struct{ Frames []exportedFrame }
	data, err := os.ReadFile(filepath.Join(output, "frames.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &args); err != nil {
		t.Fatal(err)
	}
	if len(args.Frames) != 1 || args.Frames[0].Source != "source.nef" {
		t.Fatal("filename selection not applied")
	}
	if Main([]string{"render", output, "--output", filepath.Join(t.TempDir(), "roundtrip.avi")}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	for _, arguments := range [][]string{{}, {dir}, {dir, "--output", output}, {dir, "--output", filepath.Join(t.TempDir(), "bad"), "--long-edge", "1"}, {dir, "--output", filepath.Join(t.TempDir(), "bad"), "--aspect", "bad"}, {dir, "--output", filepath.Join(t.TempDir(), "bad"), "--unknown"}, {dir, "--output", filepath.Join(t.TempDir(), "bad"), "--filename-contains", "absent"}} {
		out.Reset()
		stderr.Reset()
		if Main(append([]string{"export"}, arguments...), &out, &stderr) == 0 {
			t.Fatalf("invalid CLI accepted: %v", arguments)
		}
	}
	out.Reset()
	stderr.Reset()
	if Main([]string{"export", "--help"}, &out, &stderr) != 0 || !strings.Contains(stderr.String(), "16-bit") {
		t.Fatal("export help missing")
	}
}

func TestExportTIFFRealRAW(t *testing.T) {
	source := os.Getenv("LRTL_RAW_SAMPLE")
	if source == "" {
		t.Skip("set LRTL_RAW_SAMPLE to test installed Adobe DNG Converter on a real RAW")
	}
	original, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	before := sha256.Sum256(original)
	output := filepath.Join(t.TempDir(), "export")
	if err := ExportTIFFSequence([]Frame{{Image: source}}, output, RenderOptions{RawDevelop: true, DNGConverterPath: os.Getenv("LRTL_DNG_CONVERTER"), AutoOrient: true, LongEdge: 256}); err != nil {
		t.Fatal(err)
	}
	img := readExportedTIFF(t, filepath.Join(output, "frame000001.tif"))
	if max(img.Bounds().Dx(), img.Bounds().Dy()) > 256 {
		t.Fatal("RAW export ignored downscale")
	}
	var stdout, stderr bytes.Buffer
	if Main([]string{"render", output, "--output", filepath.Join(t.TempDir(), "raw-export.avi")}, &stdout, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	after, err := os.ReadFile(source)
	if err != nil || sha256.Sum256(after) != before {
		t.Fatal("real RAW changed")
	}
}
