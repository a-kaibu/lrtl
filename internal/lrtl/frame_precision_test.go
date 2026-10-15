package lrtl

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"golang.org/x/image/tiff"
)

type plainPrecisionTestImage struct{ image.Image }

func runFFmpegCaptureHelper() bool {
	args := os.Args[1:]
	if !slices.Contains(args, "-pixel_format") {
		return false
	}
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		panic(err)
	}
	if err := os.WriteFile(os.Getenv("LRTL_TEST_FFMPEG_CAPTURE"), data, 0600); err != nil {
		panic(err)
	}
	arguments, err := json.Marshal(args)
	if err != nil {
		panic(err)
	}
	if err := os.WriteFile(os.Getenv("LRTL_TEST_FFMPEG_CAPTURE")+".json", arguments, 0600); err != nil {
		panic(err)
	}
	if err := os.WriteFile(args[len(args)-1], []byte("fake video container"), 0600); err != nil {
		panic(err)
	}
	return true
}

func TestRGB48ByteOrderBoundsAndAlpha(t *testing.T) {
	img := image.NewNRGBA64(image.Rect(7, 9, 10, 10))
	img.SetNRGBA64(7, 9, color.NRGBA64{0x1234, 0x5678, 0x9abc, 65535})
	img.SetNRGBA64(8, 9, color.NRGBA64{0xffff, 0, 0, 0x8000})
	img.SetNRGBA64(9, 9, color.NRGBA64{65535, 65535, 65535, 0})
	got, err := imageRGB48(img)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{0x34, 0x12, 0x78, 0x56, 0xbc, 0x9a, 0, 0x80, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	if !bytes.Equal(got, want) {
		t.Fatalf("RGB48=%x want %x", got, want)
	}
	// The generic image.Image route must have the same alpha and byte order.
	generic, err := imageRGB48(plainPrecisionTestImage{img})
	if err != nil || !bytes.Equal(generic, want) {
		t.Fatalf("generic RGB48=%x err=%v", generic, err)
	}
	back, err := imageFromRGB48(got, 3, 1)
	if err != nil {
		t.Fatal(err)
	}
	if p := back.NRGBA64At(0, 0); p != (color.NRGBA64{0x1234, 0x5678, 0x9abc, 65535}) {
		t.Fatalf("round trip=%v", p)
	}
	for _, size := range [][2]int{{0, 1}, {3, 0}, {-1, 1}, {1, 1}, {int(^uint(0) >> 1), 2}} {
		if _, err := imageFromRGB48(got, size[0], size[1]); err == nil {
			t.Fatalf("accepted invalid dimensions %v", size)
		}
	}
	if _, err := imageRGB48(image.NewNRGBA64(image.Rectangle{})); err == nil {
		t.Fatal("accepted empty frame")
	}
}

func TestPrecisionPreservedByGeometry(t *testing.T) {
	img := image.NewNRGBA64(image.Rect(0, 0, 32, 24))
	value := color.NRGBA64{12345, 23456, 34567, 65535}
	for y := 0; y < 24; y++ {
		for x := 0; x < 32; x++ {
			img.SetNRGBA64(x, y, value)
		}
	}
	for orientation := uint16(1); orientation <= 8; orientation++ {
		got := applyEXIFOrientation(img, orientation)
		if !highPrecisionImage(got) || got.At(0, 0) != (color.Color(value)) {
			t.Fatalf("orientation %d loses precision", orientation)
		}
	}
	for _, got := range []image.Image{
		resizeFrame(img, 16, 16), cropImageToAspect(img, 1),
		applyXMPCrop(img, cropSpec{Active: true, Bounds: image.Rect(4, 4, 28, 20)}),
		applyXMPCrop(img, cropSpec{Active: true, Bounds: image.Rect(4, 4, 28, 20), Left: 4, Top: 4, Right: 28, Bottom: 20, CenterX: 16, CenterY: 12, Angle: 5}),
	} {
		if !highPrecisionImage(got) {
			t.Fatalf("geometry produced %T", got)
		}
		p := color.NRGBA64Model.Convert(got.At(got.Bounds().Dx()/2, got.Bounds().Dy()/2)).(color.NRGBA64)
		if absInt(int(p.R)-int(value.R)) > 2 || absInt(int(p.G)-int(value.G)) > 2 || absInt(int(p.B)-int(value.B)) > 2 {
			t.Fatalf("geometry quantized pixel: %v", p)
		}
	}
}

func TestExposureGainPreservesFine16BitSteps(t *testing.T) {
	img := image.NewNRGBA64(image.Rect(0, 0, 3, 1))
	for x, v := range []uint16{1000, 1001, 1002} {
		img.SetNRGBA64(x, 0, color.NRGBA64{v, v, v, 65535})
	}
	got := applyExposureGainFrame(img, 2)
	if !highPrecisionImage(got) {
		t.Fatalf("exposure produced %T", got)
	}
	for x, want := range []uint16{2000, 2002, 2004} {
		p := color.NRGBA64Model.Convert(got.At(x, 0)).(color.NRGBA64)
		// All samples lie in sRGB's linear toe, so +1 EV doubles exactly.
		if p.R != want || p.G != want || p.B != want {
			t.Fatalf("pixel %d=%v want %d", x, p, want)
		}
	}
	basic := applyXMPBasicFrame(img, xmpBasicAdjustments{exposure: 1})
	if !highPrecisionImage(basic) {
		t.Fatal("exposure-only basic edit quantized input")
	}
	if img.NRGBA64At(0, 0).R != 1000 {
		t.Fatal("input mutated")
	}
	if applyExposureGainFrame(img, 1) != img {
		t.Fatal("neutral gain should retain original precision")
	}
}

func TestRGB48MotionBlendFineStepsAndInvalidWindows(t *testing.T) {
	a := []byte{0xe8, 3, 0xe9, 3, 0xea, 3}
	b := []byte{0xec, 3, 0xed, 3, 0xee, 3}
	got, err := weightedRGB48Frames([][]byte{a, b}, []float32{1, 3})
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []uint16{1003, 1004, 1005} {
		if v := binary.LittleEndian.Uint16(got[i*2:]); v != want {
			t.Fatalf("channel %d=%d want %d", i, v, want)
		}
	}
	for _, c := range []struct {
		frames  [][]byte
		weights []float32
	}{
		{nil, nil}, {[][]byte{a}, nil}, {[][]byte{a, b[:4]}, []float32{1, 1}},
		{[][]byte{a}, []float32{0}}, {[][]byte{a}, []float32{-1}}, {[][]byte{a}, []float32{float32(math.NaN())}}, {[][]byte{a}, []float32{float32(math.Inf(1))}},
	} {
		if _, err := weightedRGB48Frames(c.frames, c.weights); err == nil {
			t.Fatal("invalid blend accepted")
		}
	}
}

func TestFFmpegReceivesActual16BitSamplesFromTIFFAndPNG(t *testing.T) {
	for _, format := range []string{"tiff", "png"} {
		t.Run(format, func(t *testing.T) {
			dir := t.TempDir()
			img := image.NewNRGBA64(image.Rect(0, 0, 32, 24))
			for y := 0; y < 24; y++ {
				for x := 0; x < 32; x++ {
					v := uint16(1000 + x)
					img.SetNRGBA64(x, y, color.NRGBA64{v, v + 100, v + 200, 65535})
				}
			}
			var encoded bytes.Buffer
			var err error
			if format == "tiff" {
				err = tiff.Encode(&encoded, img, nil)
			} else {
				err = png.Encode(&encoded, img)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "frame."+format), encoded.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			frames, err := DiscoverSequence(dir)
			if err != nil {
				t.Fatal(err)
			}
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			capture := filepath.Join(t.TempDir(), "samples.bin")
			t.Setenv("LRTL_TEST_FFMPEG_CAPTURE", capture)
			if err := RenderSequence(frames, filepath.Join(dir, "out.mov"), RenderOptions{FPS: 24, Quality: 90, Codec: "prores", FFmpegPath: executable}); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(capture)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 32*24*6 {
				t.Fatalf("bytes=%d", len(got))
			}
			for x := 0; x < 32; x++ {
				for channel := 0; channel < 3; channel++ {
					if v := binary.LittleEndian.Uint16(got[x*6+channel*2:]); v != uint16(1000+x+channel*100) {
						t.Fatalf("pixel %d channel %d quantized to %d", x, channel, v)
					}
				}
			}
			argsData, err := os.ReadFile(capture + ".json")
			if err != nil {
				t.Fatal(err)
			}
			var args []string
			if err := json.Unmarshal(argsData, &args); err != nil {
				t.Fatal(err)
			}
			pixelFormat := slices.Index(args, "-pixel_format")
			if pixelFormat < 0 || args[pixelFormat+1] != "rgb48le" {
				t.Fatalf("input pixel format: %v", args)
			}
		})
	}
}

func TestRGB48MotionBlurPipelineAndOverlay(t *testing.T) {
	dir := t.TempDir()
	for i, value := range []uint16{1000, 1004} {
		img := image.NewNRGBA64(image.Rect(0, 0, 64, 48))
		for y := 0; y < 48; y++ {
			for x := 0; x < 64; x++ {
				img.SetNRGBA64(x, y, color.NRGBA64{value, value, value, 65535})
			}
		}
		data, err := encodeDevelopedDNGTIFF(img, 6)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, []string{"a.tiff", "b.tiff"}[i])
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		writeTestXMP(t, path, map[string]string{"crs:Exposure2012": "1", "crs:HasCrop": "True", "crs:CropLeft": "0.1", "crs:CropRight": "0.9", "crs:CropTop": "0.1", "crs:CropBottom": "0.9"})
	}
	frames, err := DiscoverSequence(dir)
	if err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(t.TempDir(), "samples.bin")
	t.Setenv("LRTL_TEST_FFMPEG_CAPTURE", capture)
	opts := RenderOptions{FPS: 24, Quality: 90, Codec: "prores", FFmpegPath: executable, ApplyXMPBasic: true, ApplyXMPCrop: true, AutoOrient: true, LongEdge: 32, MotionBlurFrames: 2, Watermark: "X", WatermarkPosition: "bottom-right"}
	if err := RenderSequence(frames, filepath.Join(dir, "out.mov"), opts); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	// Orientation gives 48x64, then long-edge scaling gives 24x32.
	frameBytes := 24 * 32 * 6
	if len(got) != 2*frameBytes {
		t.Fatalf("captured bytes=%d", len(got))
	}
	// Top-left is outside the overlay. The toe doubles to 2000 and 2008,
	// then weights .354:1 produce round((2000*.354+2008)/1.354)=2006.
	for i, want := range []uint16{2000, 2006} {
		v := binary.LittleEndian.Uint16(got[i*frameBytes:])
		if absInt(int(v)-int(want)) > 2 {
			t.Fatalf("frame %d sample=%d want %d", i, v, want)
		}
	}
}

func TestRealFFmpegProResRetainsMoreThan8BitGradation(t *testing.T) {
	if os.Getenv("LRTL_TEST_REAL_FFMPEG") != "1" {
		t.Skip("set LRTL_TEST_REAL_FFMPEG=1 to exercise installed FFmpeg")
	}
	ffmpeg, err := resolveFFmpeg("")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	img := image.NewNRGBA64(image.Rect(0, 0, 1024, 128))
	for y := 0; y < 128; y++ {
		for x := 0; x < 1024; x++ {
			v := uint16(x * 65535 / 1023)
			img.SetNRGBA64(x, y, color.NRGBA64{v, v, v, 65535})
		}
	}
	var encoded bytes.Buffer
	if err := tiff.Encode(&encoded, img, nil); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "gradient.tiff"), encoded.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	frames, err := DiscoverSequence(dir)
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(dir, "gradient.mov")
	if err := RenderSequence(frames, output, RenderOptions{FPS: 24, Quality: 90, Codec: "prores-hq", FFmpegPath: ffmpeg}); err != nil {
		t.Fatal(err)
	}
	data, err := exec.Command(ffmpeg, "-v", "error", "-i", output, "-frames:v", "1", "-f", "rawvideo", "-pix_fmt", "gray10le", "pipe:1").Output()
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 1024*128*2 {
		t.Fatalf("unexpected decoded size %d", len(data))
	}
	unique := map[uint16]bool{}
	row := data[64*1024*2 : 65*1024*2]
	for x := 0; x < 1024; x++ {
		unique[binary.LittleEndian.Uint16(row[x*2:])] = true
	}
	if len(unique) < 512 {
		t.Fatalf("only %d distinct gray levels survive the actual ProRes encode/decode", len(unique))
	}
	t.Logf("actual ProRes encode/decode: %d distinct levels in the 10-bit gradient", len(unique))
	writeTestXMP(t, frames[0].Image, map[string]string{
		"crs:Contrast2012": "-10", "crs:Saturation": "20", "crs:ToneCurvePV2012": "0, 0 255, 255",
		"crs:Sharpness": "10", "crs:LuminanceSmoothing": "10", "crs:ColorNoiseReduction": "10",
	})
	frames, err = DiscoverSequence(dir)
	if err != nil {
		t.Fatal(err)
	}
	output = filepath.Join(dir, "edited.mov")
	if err := RenderSequence(frames, output, RenderOptions{FPS: 24, Quality: 90, Codec: "prores-hq", FFmpegPath: ffmpeg, ApplyXMPBasic: true}); err != nil {
		t.Fatal(err)
	}
	data, err = exec.Command(ffmpeg, "-v", "error", "-i", output, "-frames:v", "1", "-f", "rawvideo", "-pix_fmt", "gray10le", "pipe:1").Output()
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 1024*128*2 {
		t.Fatalf("edited decoded size=%d", len(data))
	}
	unique = map[uint16]bool{}
	row = data[64*1024*2 : 65*1024*2]
	for x := 0; x < 1024; x++ {
		unique[binary.LittleEndian.Uint16(row[x*2:])] = true
	}
	if len(unique) < 512 {
		t.Fatalf("only %d edited gray levels survive ProRes", len(unique))
	}
	t.Logf("actual ProRes with XMP contrast/saturation/curve/sharpening/denoise: %d distinct levels", len(unique))
}
