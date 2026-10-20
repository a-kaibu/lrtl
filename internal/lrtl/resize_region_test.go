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
	"testing"
)

func TestResizeRegionExactlyMatchesFullResizeAndCrop(t *testing.T) {
	bounds := image.Rect(5, 7, 37, 31)
	images := []draw.Image{image.NewNRGBA(bounds), image.NewRGBA(bounds), image.NewGray(bounds), image.NewNRGBA64(bounds), image.NewRGBA64(bounds), image.NewGray16(bounds), image.NewPaletted(bounds, color.Palette{color.Black, color.White, color.NRGBA{255, 0, 0, 128}})}
	var sources []image.Image
	for _, img := range images {
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				alpha := []uint16{0, 16384, 32768, 65535}[(x+y)%4]
				img.Set(x, y, color.NRGBA64{uint16(x * 1501), uint16(y * 1733), uint16((x*917 + y*113) % 65536), alpha})
			}
		}
		sources = append(sources, img)
	}
	sources = append(sources, plainPrecisionTestImage{images[0]})
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, images[0], &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	decoded, err := jpeg.Decode(bytes.NewReader(encoded.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	sources = append(sources, decoded)
	if view, ok := decoded.(interface {
		SubImage(image.Rectangle) image.Image
	}); ok {
		sources = append(sources, view.SubImage(image.Rect(5, 7, 20, 20)))
	}
	for _, img := range sources {
		before, err := imageRGB48(img)
		if err != nil {
			t.Fatal(err)
		}
		for _, size := range [][2]int{{16, 12}, {19, 17}, {48, 36}, {20, 24}, {img.Bounds().Dx(), img.Bounds().Dy()}, {1, 1}, {1, 17}, {17, 1}} {
			w, h := size[0], size[1]
			full := resizeFrame(img, w, h)
			rw, rh := min(5, w), min(4, h)
			regions := []image.Rectangle{image.Rect(0, 0, w, h), image.Rect(0, 0, rw, rh), image.Rect(w-rw, h-rh, w, h), image.Rect(w/3, h/3, min(w, w/3+rw), min(h, h/3+rh)), image.Rect(w-1, h-1, w, h)}
			for _, region := range regions {
				got := resizeFrameRegion(img, w, h, region)
				want := copyFrameRectangle(full, region.Add(full.Bounds().Min))
				if got.Bounds().Size() != want.Bounds().Size() {
					t.Fatalf("%T size%v region%v: bounds%v want%v", img, size, region, got.Bounds(), want.Bounds())
				}
				for y := 0; y < region.Dy(); y++ {
					for x := 0; x < region.Dx(); x++ {
						actual := color.NRGBA64Model.Convert(got.At(got.Bounds().Min.X+x, got.Bounds().Min.Y+y)).(color.NRGBA64)
						expected := color.NRGBA64Model.Convert(want.At(want.Bounds().Min.X+x, want.Bounds().Min.Y+y)).(color.NRGBA64)
						if actual != expected {
							t.Fatalf("%T size%v region%v pixel%d,%d: %+v want%+v", img, size, region, x, y, actual, expected)
						}
					}
				}
			}
		}
		after, err := imageRGB48(img)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("regional resize changed its source")
		}
	}
	for _, region := range []image.Rectangle{image.Rectangle{}, image.Rect(-1, 0, 5, 5), image.Rect(0, 0, 100, 100)} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatal("invalid internal resize region was accepted")
				}
			}()
			resizeFrameRegion(images[0], 16, 12, region)
		}()
	}
}

type boundedRegionSource struct {
	bounds image.Rectangle
	reads  int
}

func (img *boundedRegionSource) ColorModel() color.Model { return color.RGBA64Model }
func (img *boundedRegionSource) Bounds() image.Rectangle { return img.bounds }
func (img *boundedRegionSource) At(x, y int) color.Color { return img.RGBA64At(x, y) }
func (img *boundedRegionSource) RGBA64At(x, y int) color.RGBA64 {
	if !image.Pt(x, y).In(img.bounds) {
		panic("regional resampler read beyond source bounds")
	}
	img.reads++
	return color.RGBA64{20 * 257, 60 * 257, 100 * 257, 65535}
}

func largeRegionObservations(t *testing.T) []nativeGeometryObservation {
	t.Helper()
	data, err := os.ReadFile("testdata/lrt-region-geometry-7.5.5.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []nativeGeometryObservation
	if err := json.Unmarshal(data, &cases); err != nil || len(cases) != 4 {
		t.Fatalf("expected4 native large-canvas cases: %v", err)
	}
	return cases
}

func TestResizeRegionLargeNativeCanvases(t *testing.T) {
	for _, tc := range largeRegionObservations(t) {
		plan, err := planNativeRenderGeometry(tc.Width, tc.Height, tc.OutputHeight, tc.Aspect)
		if err != nil {
			t.Fatal(err)
		}
		var sw, sh, w, h, x, y int
		if _, err := fmt.Sscanf(tc.Scale, "scale=w=%d:h=%d", &sw, &sh); err != nil {
			t.Fatal(err)
		}
		if _, err := fmt.Sscanf(tc.Crop, "%d:%d:%d:%d", &w, &h, &x, &y); err != nil {
			t.Fatal(err)
		}
		if plan != (nativeRenderGeometry{sw, sh, image.Rect(x, y, x+w, y+h)}) || uint64(sw)*uint64(sh) <= 64*1024*1024 {
			t.Fatalf("lost native previously rejected geometry: %+v", plan)
		}
		source := &boundedRegionSource{bounds: image.Rect(0, 0, tc.Width, tc.Height)}
		region := image.Rect(x+10, y+20, x+74, y+52)
		got := resizeFrameRegion(source, sw, sh, region)
		if source.reads > 20000 {
			t.Fatalf("small region read a full source/canvas: %d samples", source.reads)
		}
		for y := 0; y < 32; y++ {
			for x := 0; x < 64; x++ {
				if got.At(x, y) != (color.NRGBA{20, 60, 100, 255}) {
					t.Fatal("large-canvas regional resampling changed constant color")
				}
			}
		}
	}
	// Generic source types retain the existing odd-edge copy behavior.
	plain := plainPrecisionTestImage{image.NewNRGBA(image.Rect(0, 0, 33, 25))}
	if got := cropNativeEvenFrame(plain, 1); got.Bounds().Size() != image.Pt(32, 24) {
		t.Fatal("generic even-crop fallback changed dimensions")
	}
}

func TestResizeRegionLargePresetPipeline(t *testing.T) {
	dir := t.TempDir()
	img := image.NewGray16(image.Rect(0, 0, 32, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 32; x++ {
			img.SetGray16(x, y, color.Gray16{12345})
		}
	}
	var input bytes.Buffer
	if err := png.Encode(&input, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "frame.png"), input.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	before := batchFileSnapshot(t, dir)
	preset, err := decodeLRTRenderPreset(renderPresetObservations(t)[0].Preset)
	if err != nil {
		t.Fatal(err)
	}
	preset.OutputResV = 384
	path := savePresetJSON(t, map[string]any{"lastRenderSettings": preset, "renderConstrainCrop": true, "renderAR": 16})
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(t.TempDir(), "capture")
	t.Setenv("LRTL_TEST_FFMPEG_CAPTURE", capture)
	markerCommand(t, "render", dir, "--output", filepath.Join(t.TempDir(), "video.mp4"), "--lrt-render-preset", path, "--video-range", "auto", "--ffmpeg", executable)
	data, err := os.ReadFile(capture)
	if err != nil || len(data) != 6144*384*6 {
		t.Fatalf("large-canvas small-output capture: bytes%d %v", len(data), err)
	}
	for i := 0; i < len(data); i += 2 {
		if value := binary.LittleEndian.Uint16(data[i:]); value != 12345 {
			t.Fatalf("large native region lost16-bit constant: %d", value)
		}
	}
	t.Run("actual-mp4", func(t *testing.T) {
		ffmpeg, err := exec.LookPath("ffmpeg")
		if err != nil {
			t.Skip("FFmpeg unavailable")
		}
		ffprobe, err := exec.LookPath("ffprobe")
		if err != nil {
			t.Skip("ffprobe unavailable")
		}
		output := filepath.Join(t.TempDir(), "large-region.mp4")
		markerCommand(t, "render", dir, "--output", output, "--lrt-render-preset", path, "--video-range", "auto", "--ffmpeg", ffmpeg, "--preset", "ultrafast", "--crf", "0")
		probeData, err := exec.Command(ffprobe, "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=width,height,nb_frames", "-of", "json", output).CombinedOutput()
		if err != nil {
			t.Fatalf("ffprobe: %s %v", probeData, err)
		}
		var probe struct {
			Streams []struct {
				Width, Height int
				Frames        string `json:"nb_frames"`
			}
		}
		if err := json.Unmarshal(probeData, &probe); err != nil || len(probe.Streams) != 1 || probe.Streams[0].Width != 6144 || probe.Streams[0].Height != 384 || probe.Streams[0].Frames != "1" {
			t.Fatalf("actual large preset metadata: %s %v", probeData, err)
		}
		rgb, err := exec.Command(ffmpeg, "-v", "error", "-i", output, "-an", "-f", "rawvideo", "-pix_fmt", "rgb24", "pipe:1").Output()
		if err != nil || len(rgb) != 6144*384*3 {
			t.Fatalf("decoded large MP4: bytes%d %v", len(rgb), err)
		}
		for i := range rgb {
			if absInt(int(rgb[i])-12345/257) > 3 {
				t.Fatalf("decoded large constant changed: sample%d=%d", i, rgb[i])
			}
		}
	})
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("regional preset render changed source files")
	}
}

func TestResizeRegionActualFFmpegLargeCanvas(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("FFmpeg unavailable")
	}
	// Replay the observed first large scale/crop chain on a constant plane.
	raw := make([]byte, 32*64*2)
	for i := 0; i < len(raw); i += 2 {
		binary.LittleEndian.PutUint16(raw[i:], 12345)
	}
	cmd := exec.Command(ffmpeg, "-v", "error", "-f", "rawvideo", "-pixel_format", "gray16le", "-video_size", "32x64", "-i", "pipe:0", "-vf", "scale=w=6144:h=12288:out_range=full:flags=lanczos,crop=6144:384:0:5952", "-frames:v", "1", "-f", "rawvideo", "-pix_fmt", "gray16le", "pipe:1")
	cmd.Stdin = bytes.NewReader(raw)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	data, err := cmd.Output()
	if err != nil || len(data) != 6144*384*2 {
		t.Fatalf("large native component replay: bytes%d %v %s", len(data), err, stderr.String())
	}
	for i := 0; i < len(data); i += 2 {
		if value := binary.LittleEndian.Uint16(data[i:]); absInt(int(value)-12345) > 1 {
			t.Fatalf("native large scale/crop changed constant plane: %d", value)
		}
	}
}

func BenchmarkResizeRegionLargeCanvasWindow(b *testing.B) {
	img := &boundedRegionSource{bounds: image.Rect(0, 0, 6016, 4016)}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		resizeFrameRegion(img, 10800, 7200, image.Rect(10, 1460, 74, 1492))
	}
}

func BenchmarkResizeRegionNative6144x384(b *testing.B) {
	img := image.NewGray16(image.Rect(0, 0, 32, 64))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resizeFrameRegion(img, 6144, 12288, image.Rect(0, 5952, 6144, 6336))
	}
}
