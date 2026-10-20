package lrtl

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
)

func TestCropPositionNative288Observations(t *testing.T) {
	data, err := os.ReadFile("testdata/lrt-crop-position-7.5.5.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		nativeGeometryObservation
		Position int
	}
	if err := json.Unmarshal(data, &cases); err != nil || len(cases) != 288 {
		t.Fatalf("expected 288 native positions: %v", err)
	}
	for _, tc := range cases {
		plan, err := planNativeRenderGeometryAtPosition(tc.Width, tc.Height, tc.OutputHeight, tc.Aspect, tc.Position)
		var w, h, x, y, sw, sh int
		if _, err := fmt.Sscanf(tc.Crop, "%d:%d:%d:%d", &w, &h, &x, &y); err != nil {
			t.Fatal(err)
		}
		if _, err := fmt.Sscanf(tc.Scale, "scale=w=%d:h=%d", &sw, &sh); err != nil {
			t.Fatal(err)
		}
		want := nativeRenderGeometry{sw, sh, image.Rect(x, y, x+w, y+h)}
		if err != nil || plan != want {
			t.Fatalf("source%dx%d resolution%d aspect%g position%d: %+v want %+v: %v", tc.Width, tc.Height, tc.OutputHeight, tc.Aspect, tc.Position, plan, want, err)
		}
	}
	if position, err := (RenderOptions{}).cropPosition(); position != 50 || err != nil {
		t.Fatal("omitted Go option must retain center")
	}
	position := 0
	if value, err := (RenderOptions{CropPosition: &position}).cropPosition(); value != 0 || err != nil {
		t.Fatal("explicit zero must retain left/top")
	}
	for _, position := range []int{-1, 101} {
		if _, err := planNativeRenderGeometryAtPosition(64, 32, 32, 1, position); err == nil {
			t.Fatal("accepted out-of-range position")
		}
	}
	if _, err := planNativeRenderGeometryAtPosition(1, 1, 0, 1, 50); err == nil {
		t.Fatal("ignored degenerate native geometry")
	}
}

func TestCropPositionActualMP4Pixels(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("FFmpeg unavailable")
	}
	dir := t.TempDir()
	var input bytes.Buffer
	if err := png.Encode(&input, scaleTestImage("ramp")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "frame.png"), input.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	preset, err := decodeLRTRenderPreset(renderPresetObservations(t)[0].Preset)
	if err != nil {
		t.Fatal(err)
	}
	preset.OutputResV = 24
	path := savePresetJSON(t, map[string]any{"lastRenderSettings": preset, "renderConstrainCrop": true, "renderAR": 1})
	for _, position := range []int{0, 100} {
		output := filepath.Join(t.TempDir(), "video.mp4")
		markerCommand(t, "render", dir, "--output", output, "--lrt-render-preset", path, "--crop-position", strconv.Itoa(position), "--ffmpeg", ffmpeg, "--video-range", "auto", "--output-color", "standard", "--preset", "ultrafast", "--crf", "0")
		data, err := exec.Command(ffmpeg, "-v", "error", "-i", output, "-an", "-f", "rawvideo", "-pix_fmt", "rgb24", "pipe:1").Output()
		if err != nil || len(data) != 24*24*3 {
			t.Fatalf("decoded MP4 frame bytes%d: %v", len(data), err)
		}
		x := 12 + 8*position/100
		want := (x*1600 + 12*200) / 257
		for channel := 0; channel < 3; channel++ {
			if got := int(data[(12*24+12)*3+channel]); absInt(got-want) > 3 {
				t.Fatalf("position%d decoded channel%d=%d want%d", position, channel, got, want)
			}
		}
	}
}

func TestCropPositionBoundsPrecisionAndDefault(t *testing.T) {
	if got := cropPositionOffset(1<<24+3, 50); got != (1<<24+3)/2 {
		t.Fatal("large literal-CLI center changed")
	}
	for _, size := range [][2]int{{64, 32}, {32, 64}, {63, 31}} {
		img := image.NewNRGBA64(image.Rect(5, 7, 5+size[0], 7+size[1]))
		for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
			for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
				img.SetNRGBA64(x, y, color.NRGBA64{uint16(x * 100), uint16(y * 100), 12345, 32768})
			}
		}
		before := bytes.Clone(img.Pix)
		for _, position := range []int{0, 33, 50, 100} {
			got := cropImageToAspectAtPosition(img, 1, position)
			edge := min(size[0], size[1])
			left, top := 5+(size[0]-edge)*position/100, 7+(size[1]-edge)*position/100
			if !highPrecisionImage(got) || got.Bounds() != image.Rect(0, 0, edge, edge) {
				t.Fatal("lost crop bounds or precision")
			}
			for y := 0; y < edge; y++ {
				for x := 0; x < edge; x++ {
					if got.At(x, y) != img.At(left+x, top+y) {
						t.Fatalf("position%d pixel%d,%d changed RGB16/alpha", position, x, y)
					}
				}
			}
		}
		if !bytes.Equal(before, img.Pix) {
			t.Fatal("crop modified its source")
		}
		if got := cropImageToAspectAtPosition(img, 0, 100); got != img {
			t.Fatal("position should do nothing without an aspect crop")
		}
	}
}

func TestCropPositionCLIFramesBlurAVIAndBatch(t *testing.T) {
	dir := t.TempDir()
	var sources []*image.Gray16
	for i := 0; i < 3; i++ {
		img := image.NewGray16(image.Rect(0, 0, 63, 31))
		for y := 0; y < 31; y++ {
			for x := 0; x < 63; x++ {
				img.SetGray16(x, y, color.Gray16{uint16(1000 + x*400 + y*137 + i*800)})
			}
		}
		var data bytes.Buffer
		if err := png.Encode(&data, img); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("frame%d.png", i)), data.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, img)
	}
	before := batchFileSnapshot(t, dir)
	preset, err := decodeLRTRenderPreset(renderPresetObservations(t)[0].Preset)
	if err != nil {
		t.Fatal(err)
	}
	preset.OutputResV = 32
	path := savePresetJSON(t, map[string]any{"lastRenderSettings": preset, "renderConstrainCrop": true, "renderAR": 1})
	presetBefore, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(t.TempDir(), "capture")
	t.Setenv("LRTL_TEST_FFMPEG_CAPTURE", capture)
	for _, native := range []bool{false, true} {
		var centered []byte
		for _, position := range []int{50, 0, 33, 100} {
			var expected [][]byte
			for _, img := range sources {
				var prepared image.Image
				if native {
					// Native source63x31/res32/aspect1: even62x30 ->64x32,
					// then32x32 crop with excess width32. Positions0/33/100
					// are observed at x=0/10/32, not a crop before resizing.
					scaled := resizeFrame(img.SubImage(image.Rect(0, 0, 62, 30)), 64, 32)
					cropped := image.NewNRGBA64(image.Rect(0, 0, 32, 32))
					draw.Draw(cropped, cropped.Bounds(), scaled, image.Pt(32*position/100, 0), draw.Src)
					prepared = cropped
				} else {
					left := 32 * position / 100
					prepared = resizeFrame(img.SubImage(image.Rect(left, 0, left+31, 31)), 32, 32)
				}
				rgb, err := imageRGB48(prepared)
				if err != nil {
					t.Fatal(err)
				}
				expected = append(expected, rgb)
			}
			flags := []string{"--aspect", "1:1", "--output-height", "32"}
			if native {
				flags = []string{"--lrt-render-preset", path, "--video-range", "auto", "--output-color", "standard"}
			}
			for _, blur := range []int{0, 2} {
				args := append([]string{"render", "--crop-position", strconv.Itoa(position), dir, "--output", filepath.Join(t.TempDir(), "video.mp4"), "--ffmpeg", executable, "--motion-blur", strconv.Itoa(blur)}, flags...)
				markerCommand(t, args...)
				got, err := os.ReadFile(capture)
				if err != nil {
					t.Fatal(err)
				}
				want := bytes.Join(expected, nil)
				if blur == 2 {
					want, err = weightedRGB48Frames(expected[1:], []float32{.354, 1})
					if err != nil {
						t.Fatal(err)
					}
				}
				if !bytes.Equal(got, want) {
					t.Fatalf("native=%v position%d blur%d: mismatched frame pixels", native, position, blur)
				}
				if position == 50 && blur == 0 {
					centered = bytes.Clone(got)
				}
			}
			if !native && position == 100 {
				output := filepath.Join(t.TempDir(), "video.avi")
				markerCommand(t, "render", dir, "--output", output, "--aspect", "1:1", "--output-height", "32", "--crop-position", "100", "--quality", "100")
				data, err := os.ReadFile(output)
				if err != nil {
					t.Fatal(err)
				}
				pos := bytes.Index(data, []byte("movi")) + 4
				if pos < 4 || pos+8 > len(data) {
					t.Fatal("missing AVI frame")
				}
				size := int(binary.LittleEndian.Uint32(data[pos+4:]))
				pos += 8
				if size < 0 || pos+size > len(data) {
					t.Fatal("truncated AVI frame")
				}
				img, err := imageFromRGB48(expected[0], 32, 32)
				if err != nil {
					t.Fatal(err)
				}
				var want bytes.Buffer
				if err := jpeg.Encode(&want, img, &jpeg.Options{Quality: 100}); err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(data[pos:pos+size], want.Bytes()) {
					t.Fatal("AVI did not encode the positioned crop")
				}
			}
		}
		flags := []string{"--aspect", "1:1", "--output-height", "32"}
		if native {
			flags = []string{"--lrt-render-preset", path, "--video-range", "auto", "--output-color", "standard"}
		}
		markerCommand(t, append([]string{"render", dir, "--output", filepath.Join(t.TempDir(), "default.mp4"), "--ffmpeg", executable}, flags...)...)
		assertPreviewEVFileBytes(t, capture, centered)
		markerCommand(t, append([]string{"batch-render", dir, "--output", t.TempDir(), "--", "--crop-position", "50", "--ffmpeg", executable}, flags...)...)
		assertPreviewEVFileBytes(t, capture, centered)
	}
	for _, position := range []string{"-1", "101", "1.5", "NaN"} {
		output := filepath.Join(t.TempDir(), "keep.avi")
		if err := os.WriteFile(output, []byte("existing output"), 0600); err != nil {
			t.Fatal(err)
		}
		var out, stderr bytes.Buffer
		if Main([]string{"render", dir, "--output", output, "--crop-position", position, "--overwrite"}, &out, &stderr) == 0 {
			t.Fatal("accepted invalid crop position")
		}
		assertPreviewEVFileBytes(t, output, []byte("existing output"))
	}
	assertPreviewEVFileBytes(t, path, presetBefore)
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("positioned crop changed source files")
	}
}
