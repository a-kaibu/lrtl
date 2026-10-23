package lrtl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

type nativeGeometryObservation struct {
	Width, Height, OutputHeight int
	Aspect                      float64
	Crop, Scale                 string
}

func TestNativeGeometryEvenCropAllOrientations(t *testing.T) {
	img := image.NewGray16(image.Rect(0, 0, 33, 25))
	for y := 0; y < 25; y++ {
		for x := 0; x < 33; x++ {
			img.SetGray16(x, y, color.Gray16{uint16(x*1000 + y*100)})
		}
	}
	before := bytes.Clone(img.Pix)
	for orientation := uint16(1); orientation <= 8; orientation++ {
		got := cropNativeEvenFrame(applyEXIFOrientation(img, orientation), orientation)
		want := applyEXIFOrientation(img.SubImage(image.Rect(0, 0, 32, 24)), orientation)
		gotRGB, err := imageRGB48(got)
		if err != nil {
			t.Fatal(err)
		}
		wantRGB, err := imageRGB48(want)
		if err != nil || !bytes.Equal(gotRGB, wantRGB) || got.Bounds().Size() != want.Bounds().Size() {
			t.Fatalf("wrong discarded edge at orientation%d: %v", orientation, err)
		}
	}
	if !bytes.Equal(before, img.Pix) {
		t.Fatal("even crop changed the source")
	}
}

func TestNativeGeometryPresetPixelsAndOverrides(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(t.TempDir(), "capture")
	t.Setenv("LRTL_TEST_FFMPEG_CAPTURE", capture)
	for _, tc := range []struct {
		width, height, resolution int
		aspect                    float64
	}{{32, 24, 16, 0}, {32, 64, 32, 0}, {63, 31, 32, 0}, {33, 25, 0, 0}, {101, 100, 0, 1.7777778}, {64, 32, 16, 1}, {32, 64, 16, 2.5}, {33, 25, 16, .5}} {
		var observation *nativeGeometryObservation
		for _, candidate := range nativeGeometryObservations(t) {
			if candidate.Width == tc.width && candidate.Height == tc.height && candidate.OutputHeight == tc.resolution && float32(candidate.Aspect) == float32(tc.aspect) {
				copy := candidate
				observation = &copy
				break
			}
		}
		if observation == nil {
			t.Fatalf("missing native fixture: %+v", tc)
		}
		var scaleW, scaleH, cropW, cropH, left, top int
		if _, err := fmt.Sscanf(observation.Scale, "scale=w=%d:h=%d", &scaleW, &scaleH); err != nil {
			t.Fatal(err)
		}
		if _, err := fmt.Sscanf(observation.Crop, "%d:%d:%d:%d", &cropW, &cropH, &left, &top); err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		var expected [][]byte
		for i := 0; i < 3; i++ {
			img := image.NewGray16(image.Rect(0, 0, tc.width, tc.height))
			for y := 0; y < tc.height; y++ {
				for x := 0; x < tc.width; x++ {
					img.SetGray16(x, y, color.Gray16{uint16(1000 + (x*149+y*227+i*137)%50000)})
				}
			}
			var data bytes.Buffer
			if err := png.Encode(&data, img); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("frame%d.png", i)), data.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			// Bounds and order come from native observations, not the Go planner.
			even := img.SubImage(image.Rect(0, 0, tc.width/2*2, tc.height/2*2))
			scaled := resizeFrame(even, scaleW, scaleH)
			cropped := image.NewNRGBA64(image.Rect(0, 0, cropW, cropH))
			draw.Draw(cropped, cropped.Bounds(), scaled, image.Pt(left, top), draw.Src)
			rgb, err := imageRGB48(cropped)
			if err != nil {
				t.Fatal(err)
			}
			expected = append(expected, rgb)
		}
		before := batchFileSnapshot(t, dir)
		preset, err := decodeLRTRenderPreset(renderPresetObservations(t)[0].Preset)
		if err != nil {
			t.Fatal(err)
		}
		preset.OutputResV = tc.resolution
		path := savePresetJSON(t, map[string]any{"lastRenderSettings": preset, "renderConstrainCrop": tc.aspect > 0, "renderAR": tc.aspect})
		for _, blur := range []int{0, 2} {
			args := []string{"render", dir, "--output", filepath.Join(t.TempDir(), "video.mp4"), "--ffmpeg", executable, "--lrt-render-preset", path, "--video-range", "auto", "--output-color", "standard", "--motion-blur", strconv.Itoa(blur)}
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
				t.Fatalf("native pixel preparation %+v blur%d: got %d bytes want %d", tc, blur, len(got), len(want))
			}
			argData, err := os.ReadFile(capture + ".json")
			if err != nil || !bytes.Contains(argData, []byte(fmt.Sprintf("%dx%d", cropW, cropH))) {
				t.Fatalf("native output size missing: %s %v", argData, err)
			}
		}
		if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
			t.Fatal("native render modified source files")
		}
		if tc.width == 32 && tc.height == 24 {
			markerCommand(t, "batch-render", dir, "--output", t.TempDir(), "--", "--ffmpeg", executable, "--lrt-render-preset", path, "--video-range", "auto", "--output-color", "standard")
			got, err := os.ReadFile(capture)
			if err != nil || !bytes.Equal(got, bytes.Join(expected, nil)) {
				t.Fatal("batch did not preserve native preset geometry")
			}
			for _, flag := range []string{"--output-height", "--long-edge"} {
				markerCommand(t, "render", dir, "--output", filepath.Join(t.TempDir(), "override.mp4"), "--ffmpeg", executable, "--lrt-render-preset", path, "--video-range", "auto", "--output-color", "standard", flag, "16")
				got, err := os.ReadFile(capture)
				width, height := 21, 16
				if flag == "--long-edge" {
					width, height = 16, 12
				}
				if err != nil || len(got) != width*height*6*3 {
					t.Fatalf("CLI dimension override %s: %d bytes %v", flag, len(got), err)
				}
			}
		}
		options, err := applyLRTRenderPreset(RenderOptions{NativeGeometry: true, OutputHeight: 24}, preset, path, map[string]bool{"output-height": true})
		if err != nil || options.NativeGeometry || options.OutputHeight != 24 {
			t.Fatal("explicit output height failed to override native geometry")
		}
	}
}

func TestNativeGeometryActualMP4Dimensions(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("FFmpeg unavailable")
	}
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe unavailable")
	}
	for _, tc := range []struct{ width, height, resolution, outputW, outputH int }{{33, 25, 0, 32, 24}, {32, 64, 32, 32, 64}, {32, 24, 16, 20, 16}} {
		dir := t.TempDir()
		writeRenderFrame(t, filepath.Join(dir, "frame.png"), "png", tc.width, tc.height)
		preset, err := decodeLRTRenderPreset(renderPresetObservations(t)[0].Preset)
		if err != nil {
			t.Fatal(err)
		}
		preset.OutputResV = tc.resolution
		path := savePresetJSON(t, preset)
		output := filepath.Join(t.TempDir(), "native.mp4")
		markerCommand(t, "render", dir, "--output", output, "--lrt-render-preset", path, "--video-range", "auto", "--output-color", "standard", "--ffmpeg", ffmpeg, "--preset", "ultrafast")
		data, err := exec.Command(ffprobe, "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=width,height,nb_frames", "-of", "json", output).CombinedOutput()
		if err != nil {
			t.Fatalf("ffprobe: %s %v", data, err)
		}
		var probe struct {
			Streams []struct {
				Width, Height int
				Frames        string `json:"nb_frames"`
			}
		}
		if err := json.Unmarshal(data, &probe); err != nil || len(probe.Streams) != 1 || probe.Streams[0].Width != tc.outputW || probe.Streams[0].Height != tc.outputH || probe.Streams[0].Frames != "1" {
			t.Fatalf("actual native geometry: %s %v", data, err)
		}
	}
}

func TestNativeGeometryInvalidAndFailureRetention(t *testing.T) {
	for _, tc := range []struct {
		width, height, resolution int
		aspect                    float64
	}{{0, 32, 16, 0}, {32, 0, 16, 0}, {32, 32, -1, 0}, {32, 32, 1, 0}, {32, 32, 16, math.NaN()}, {32, 32, 16, math.Inf(1)}, {32, 32, 16, 17}, {32, 24, 0, 1.0 / 16}, {6016, 4016, 4320, 1.0 / 16}, {6016, 4016, 9000, 1}} {
		if _, err := planNativeRenderGeometry(tc.width, tc.height, tc.resolution, tc.aspect); err == nil {
			t.Fatalf("accepted invalid or oversized geometry %+v", tc)
		}
	}
	dir := outputSharpenFixture(t, 1) // 64x32, aspect1/16 with Original yields0 width.
	preset, err := decodeLRTRenderPreset(renderPresetObservations(t)[0].Preset)
	if err != nil {
		t.Fatal(err)
	}
	preset.OutputResV = 0
	path := savePresetJSON(t, map[string]any{"lastRenderSettings": preset, "renderConstrainCrop": true, "renderAR": 1.0 / 16})
	output := filepath.Join(t.TempDir(), "keep.mp4")
	if err := os.WriteFile(output, []byte("existing output"), 0600); err != nil {
		t.Fatal(err)
	}
	before := batchFileSnapshot(t, dir)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(t.TempDir(), "unexpected-backend-input")
	t.Setenv("LRTL_TEST_FFMPEG_CAPTURE", capture)
	var out, stderr bytes.Buffer
	if Main([]string{"render", dir, "--output", output, "--lrt-render-preset", path, "--overwrite", "--ffmpeg", executable}, &out, &stderr) == 0 || !strings.Contains(stderr.String(), "geometry") {
		t.Fatalf("invalid native geometry should fail before output: %s", stderr.String())
	}
	if _, err := os.Stat(capture); !os.IsNotExist(err) {
		t.Fatalf("invalid geometry reached the video backend: %v", err)
	}
	assertPreviewEVFileBytes(t, output, []byte("existing output"))
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("invalid geometry changed sources")
	}
}

func nativeGeometryObservations(t *testing.T) []nativeGeometryObservation {
	t.Helper()
	data, err := os.ReadFile("testdata/lrt-geometry-7.5.5.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []nativeGeometryObservation
	if err := json.Unmarshal(data, &cases); err != nil || len(cases) != 480 {
		t.Fatalf("expected 480 native geometry observations: %v", err)
	}
	return cases
}

func TestNativeRenderGeometry480Observations(t *testing.T) {
	for _, tc := range nativeGeometryObservations(t) {
		plan, err := planNativeRenderGeometry(tc.Width, tc.Height, tc.OutputHeight, tc.Aspect)
		var width, height, left, top, scaleW, scaleH int
		if n, e := fmt.Sscanf(tc.Crop, "%d:%d:%d:%d", &width, &height, &left, &top); e != nil || n != 4 {
			t.Fatal("invalid native crop observation")
		}
		if n, e := fmt.Sscanf(tc.Scale, "scale=w=%d:h=%d:out_range=full:flags=lanczos", &scaleW, &scaleH); e != nil || n != 2 {
			t.Fatal("invalid native scale observation")
		}
		want := nativeRenderGeometry{scaleW, scaleH, image.Rect(left, top, left+width, top+height)}
		if plan != want {
			t.Fatalf("source %dx%d res%d aspect%g: %+v want %+v", tc.Width, tc.Height, tc.OutputHeight, tc.Aspect, plan, want)
		}
		invalid := width == 0 || height == 0 || width > 32768 || height > 32768 || uint64(width)*uint64(height) > 64*1024*1024
		if (err != nil) != invalid {
			t.Fatalf("geometry error status source=%dx%d res=%d aspect=%g invalid=%v: %v", tc.Width, tc.Height, tc.OutputHeight, tc.Aspect, invalid, err)
		}
	}
}
