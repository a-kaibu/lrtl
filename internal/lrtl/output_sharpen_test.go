package lrtl

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

type unsharpPlaneObservation struct {
	Bits, Width, Height int
	Pattern             string
	Input, Expected     []uint16
}

func unsharpPlaneObservations(t *testing.T) []unsharpPlaneObservation {
	t.Helper()
	data, err := os.ReadFile("testdata/lrt-unsharp-ffmpeg-8.0.1.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []unsharpPlaneObservation
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) != 6 {
		t.Fatal("expected six measured 10-bit luma planes")
	}
	for _, tc := range cases {
		if tc.Bits != 10 || tc.Width != 32 || tc.Height != 16 || len(tc.Input) != 512 || len(tc.Expected) != 512 {
			t.Fatal("invalid measured plane dimensions")
		}
	}
	return cases
}

func TestOutputSharpenNative755ToggleAndPreset(t *testing.T) {
	data, err := os.ReadFile("testdata/lrt-output-sharpen-7.5.5.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		VideoRange int
		Enabled    bool
		Filter     string
	}
	if err := json.Unmarshal(data, &cases); err != nil || len(cases) != 4 {
		t.Fatalf("native filter observations: %s %v", data, err)
	}
	for _, tc := range cases {
		want := ""
		if tc.Enabled {
			want = "unsharp=luma_msize_x=7:luma_msize_y=7:luma_amount=0.4"
		}
		if tc.Filter != want {
			t.Fatalf("native range %d sharpen %v: %s", tc.VideoRange, tc.Enabled, tc.Filter)
		}
		preset, err := decodeLRTRenderPreset(renderPresetObservations(t)[0].Preset)
		if err != nil {
			t.Fatal(err)
		}
		preset.Sharpen, preset.VideoRange = tc.Enabled, tc.VideoRange
		options, err := applyLRTRenderPreset(RenderOptions{}, preset, "", nil)
		if err != nil || options.OutputSharpen != tc.Enabled {
			t.Fatalf("preset sharpen: %+v %v", options, err)
		}
		options, err = applyLRTRenderPreset(RenderOptions{OutputSharpen: !tc.Enabled}, preset, "", map[string]bool{"sharpen": true})
		if err != nil || options.OutputSharpen != !tc.Enabled {
			t.Fatal("explicit sharpen override was ignored")
		}
	}
}

func TestOutputSharpenMeasuredLumaPlanes(t *testing.T) {
	for _, tc := range unsharpPlaneObservations(t) {
		t.Run(tc.Pattern, func(t *testing.T) {
			before := append([]uint16(nil), tc.Input...)
			got := sharpenLuma10(tc.Input, tc.Width, tc.Height)
			for i, expected := range tc.Expected {
				if got[i] != expected {
					t.Fatalf("pixel (%d,%d): %d want measured %d", i%tc.Width, i/tc.Width, got[i], expected)
				}
			}
			if !reflect.DeepEqual(before, tc.Input) {
				t.Fatal("filter changed its source plane")
			}
		})
	}
	for _, level := range []uint16{0, 1, 511, 1023} {
		if got := sharpenLuma10([]uint16{level}, 1, 1); !reflect.DeepEqual(got, []uint16{level}) {
			t.Fatalf("one-pixel edge replication: %v", got)
		}
	}
}

func TestOutputSharpenActualFFmpegPlaneReference(t *testing.T) {
	executable := os.Getenv("LRTL_SHARPEN_FFMPEG")
	if executable == "" {
		var err error
		executable, err = exec.LookPath("ffmpeg")
		if err != nil {
			t.Skip("FFmpeg unavailable; stored observations still checked by pure Go tests")
		}
	}
	for _, tc := range unsharpPlaneObservations(t) {
		var raw bytes.Buffer
		for plane := 0; plane < 3; plane++ {
			for _, value := range tc.Input {
				if plane > 0 {
					value = 512
				}
				if err := binary.Write(&raw, binary.LittleEndian, value); err != nil {
					t.Fatal(err)
				}
			}
		}
		cmd := exec.Command(executable, "-v", "error", "-f", "rawvideo", "-pixel_format", "yuv444p10le", "-video_size", "32x16", "-framerate", "1", "-i", "pipe:0", "-vf", "unsharp=luma_msize_x=7:luma_msize_y=7:luma_amount=0.4", "-f", "rawvideo", "-pix_fmt", "yuv444p10le", "pipe:1")
		cmd.Stdin = bytes.NewReader(raw.Bytes())
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		data, err := cmd.Output()
		if err != nil || len(data) != 512*2*3 {
			t.Fatalf("%s: %v bytes=%d %s", tc.Pattern, err, len(data), stderr.String())
		}
		for i, expected := range tc.Expected {
			if got := binary.LittleEndian.Uint16(data[i*2:]); got != expected {
				t.Fatalf("FFmpeg %s sample %d: %d want stored %d; filter version may differ", tc.Pattern, i, got, expected)
			}
		}
		if !bytes.Equal(data[1024:], raw.Bytes()[1024:]) {
			t.Fatal("native unsharp unexpectedly changed chroma")
		}
	}
}

func outputSharpenFixture(t *testing.T, count int) string {
	t.Helper()
	dir := t.TempDir()
	for i := 0; i < count; i++ {
		img := image.NewNRGBA64(image.Rect(0, 0, 64, 32))
		for y := 0; y < 32; y++ {
			for x := 0; x < 64; x++ {
				v := uint16((x*13 + y*23 + x*y*7 + i*19) % 101 * 600)
				img.SetNRGBA64(x, y, color.NRGBA64{v, v / 2, v / 3, 65535})
			}
		}
		var data bytes.Buffer
		if err := png.Encode(&data, img); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, fmt.Sprintf("frame%02d.png", i))
		if err := os.WriteFile(path, data.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		writeTestXMP(t, path, map[string]string{"xmp:CreateDate": fmt.Sprintf("2026-10-20T12:00:%02dZ", i)})
	}
	return dir
}

func TestOutputSharpenRGB48PrecisionChromaAndAlpha(t *testing.T) {
	if _, err := sharpenOutputImage(image.NewGray16(image.Rectangle{})); err == nil {
		t.Fatal("accepted an empty output frame")
	}
	// With no clipping, a common additive luma delta leaves channel differences
	// unchanged. Sub-8-bit sample detail must survive this video operation.
	img := image.NewNRGBA64(image.Rect(5, 6, 37, 22))
	for y := 6; y < 22; y++ {
		for x := 5; x < 37; x++ {
			v := uint16(15000 + (x*13+y*23+x*y*7)%101*300)
			img.SetNRGBA64(x, y, color.NRGBA64{v, v + 1000, v + 2000, 65535})
		}
	}
	before, err := imageRGB48(img)
	if err != nil {
		t.Fatal(err)
	}
	rgb := bytes.Clone(before)
	sharpenOutputRGB48(rgb, 32, 16)
	if bytes.Equal(before, rgb) {
		t.Fatal("sharpen produced no pixel changes on patterned input")
	}
	for i := 0; i < len(rgb); i += 6 {
		r := int(binary.LittleEndian.Uint16(rgb[i:]))
		g := int(binary.LittleEndian.Uint16(rgb[i+2:]))
		b := int(binary.LittleEndian.Uint16(rgb[i+4:]))
		if g-r != 1000 || b-g != 1000 {
			t.Fatal("sharpen changed unclipped chroma")
		}
	}
	got, err := sharpenOutputImage(img)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := imageRGB48(got)
	if err != nil || !bytes.Equal(actual, rgb) {
		t.Fatal("nonzero image bounds lost RGB48 precision")
	}
	after, _ := imageRGB48(img)
	if !bytes.Equal(before, after) {
		t.Fatal("image source changed")
	}
	// Output is opaque, composited on black just like the video input writer.
	alpha := image.NewNRGBA64(image.Rect(5, 6, 6, 7))
	alpha.SetNRGBA64(5, 6, color.NRGBA64{0xffff, 0x8000, 0x4000, 0x8000})
	got, err = sharpenOutputImage(alpha)
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, a := got.At(0, 0).RGBA()
	if r != 32768 || g != 16384 || b != 8192 || a != 65535 {
		t.Fatalf("alpha composition: %d %d %d %d", r, g, b, a)
	}
	// A dark green impulse on white has Y10=11 and rounded blur=924.
	// The fixed-point delta is -2 luma codes, or -128 in RGB16.
	clipped := bytes.Repeat([]byte{255, 255, 255, 255, 255, 255}, 32*16)
	center := (8*32 + 16) * 6
	copy(clipped[center:], []byte{0, 0, 232, 3, 0, 0})
	sharpenOutputRGB48(clipped, 32, 16)
	if got := clipped[center : center+6]; !bytes.Equal(got, []byte{0, 0, 104, 3, 0, 0}) {
		t.Fatalf("negative channel clipping: %v", got)
	}
}

func TestOutputSharpenLeavesImageExportsUnchanged(t *testing.T) {
	dir := outputSharpenFixture(t, 2)
	frames, err := DiscoverSequence(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"tiff", "preview"} {
		var control map[string][]byte
		for _, sharpen := range []bool{false, true} {
			output := filepath.Join(t.TempDir(), "export")
			options := RenderOptions{Quality: 100, OutputSharpen: sharpen}
			if kind == "tiff" {
				err = ExportTIFFSequence(frames, output, options)
			} else {
				_, err = GeneratePreviewImages(frames, output, options)
			}
			if err != nil {
				t.Fatal(err)
			}
			files, err := os.ReadDir(output)
			if err != nil {
				t.Fatal(err)
			}
			contents := map[string][]byte{}
			for _, file := range files {
				contents[file.Name()], err = os.ReadFile(filepath.Join(output, file.Name()))
				if err != nil {
					t.Fatal(err)
				}
			}
			if !sharpen {
				control = contents
			} else if !reflect.DeepEqual(control, contents) {
				t.Fatalf("video-only sharpen changed %s export", kind)
			}
		}
	}
}

func TestOutputSharpenJPEGAVIAndInvalidFlagRetention(t *testing.T) {
	source := outputSharpenFixture(t, 1)
	data, err := os.ReadFile(filepath.Join(source, "frame00.png"))
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, img, &jpeg.Options{Quality: 100}); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "frame.jpg"), encoded.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	before := batchFileSnapshot(t, dir)
	decoded, err := jpeg.Decode(bytes.NewReader(encoded.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	filtered, err := sharpenOutputImage(decoded)
	if err != nil {
		t.Fatal(err)
	}
	var expected bytes.Buffer
	if err := jpeg.Encode(&expected, filtered, &jpeg.Options{Quality: 100}); err != nil {
		t.Fatal(err)
	}
	for _, enabled := range []bool{false, true} {
		output := filepath.Join(t.TempDir(), "video.avi")
		markerCommand(t, "render", dir, "--output", output, "--quality", "100", "--sharpen="+strconv.FormatBool(enabled))
		video, err := os.ReadFile(output)
		if err != nil {
			t.Fatal(err)
		}
		pos := bytes.Index(video, []byte("movi")) + 4
		if pos < 4 || pos+8 > len(video) {
			t.Fatal("missing AVI frame")
		}
		size := int(binary.LittleEndian.Uint32(video[pos+4:]))
		pos += 8
		if size < 0 || pos+size > len(video) {
			t.Fatal("truncated AVI frame")
		}
		want := encoded.Bytes()
		if enabled {
			want = expected.Bytes()
		}
		if !bytes.Equal(video[pos:pos+size], want) {
			t.Fatalf("JPEG path enabled=%v", enabled)
		}
	}
	output := filepath.Join(t.TempDir(), "keep.avi")
	if err := os.WriteFile(output, []byte("existing video"), 0600); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	if Main([]string{"render", dir, "--output", output, "--overwrite", "--sharpen=invalid"}, &out, &stderr) == 0 {
		t.Fatal("accepted invalid sharpening bool")
	}
	assertPreviewEVFileBytes(t, output, []byte("existing video"))
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("JPEG input changed")
	}
}

func TestOutputSharpenRenderOrderingAndDuplication(t *testing.T) {
	dir := outputSharpenFixture(t, 4)
	before := batchFileSnapshot(t, dir)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(t.TempDir(), "capture")
	t.Setenv("LRTL_TEST_FFMPEG_CAPTURE", capture)
	for _, tc := range []struct {
		speed string
		blur  int
	}{{"1", 0}, {"0.5", 0}, {"0.5", 2}, {"2", 2}} {
		for _, overlay := range []bool{false, true} {
			args := []string{"render", dir, "--output", filepath.Join(t.TempDir(), "video.mp4"), "--fps", "2", "--ffmpeg", executable, "--speed", tc.speed, "--motion-blur", strconv.Itoa(tc.blur)}
			markerCommand(t, args...)
			control, err := os.ReadFile(capture)
			if err != nil {
				t.Fatal(err)
			}
			on := append(append([]string(nil), args...), "--sharpen", "--fade-out", "1", "--overwrite")
			if overlay {
				on = append(on, "--watermark", "TEST", "--watermark-position", "top-left")
			}
			if result := markerCommand(t, on...); !strings.Contains(result, "output sharpening") {
				t.Fatal("CLI summary omitted enabled sharpening")
			}
			got, err := os.ReadFile(capture)
			if err != nil {
				t.Fatal(err)
			}
			want := bytes.Clone(control)
			frameSize := 64 * 32 * 6
			if len(got) != len(want) || len(got)%frameSize != 0 {
				t.Fatal("output frame count changed")
			}
			fade, err := planVideoFade(len(want)/frameSize, frameRate{2, 1}, "2", 1)
			if err != nil {
				t.Fatal(err)
			}
			face, err := newTimestampOverlayFace(64, 32)
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < len(want)/frameSize; i++ {
				frame := want[i*frameSize : (i+1)*frameSize]
				sharpenOutputRGB48(frame, 64, 32)
				if overlay {
					img, err := imageFromRGB48(frame, 64, 32)
					if err != nil {
						t.Fatal(err)
					}
					imgData, err := imageRGB48(applyWatermarkOverlay(img, "TEST", "top-left", face))
					if err != nil {
						t.Fatal(err)
					}
					copy(frame, imgData)
				}
				fadeRGB48(frame, fade.gain(i, frameRate{2, 1}))
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("effect order speed=%s blur=%d overlay=%v", tc.speed, tc.blur, overlay)
			}
		}
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("render modified source images/XMP")
	}
}

func TestOutputSharpenAVIPresetBatchAndFalseOverride(t *testing.T) {
	dir := outputSharpenFixture(t, 3)
	preset, err := decodeLRTRenderPreset(renderPresetObservations(t)[0].Preset)
	if err != nil {
		t.Fatal(err)
	}
	preset.Sharpen, preset.OutputResV = true, 32
	presetPath := savePresetJSON(t, preset)
	presetBefore, err := os.ReadFile(presetPath)
	if err != nil {
		t.Fatal(err)
	}
	run := func(extra ...string) []byte {
		output := filepath.Join(t.TempDir(), "video.avi")
		markerCommand(t, append([]string{"render", dir, "--output", output, "--fps", "2", "--quality", "100", "--video-range", "auto", "--codec", ""}, extra...)...)
		data, err := os.ReadFile(output)
		if err != nil {
			t.Fatal(err)
		}
		movi := bytes.Index(data, []byte("movi"))
		if movi < 0 {
			t.Fatal("missing AVI payload")
		}
		return data[movi:]
	}
	control := run()
	sharpened := run("--sharpen")
	if bytes.Equal(sharpened, control) {
		t.Fatal("AVI pixels did not change")
	}
	if !bytes.Equal(run("--lrt-render-preset", presetPath), sharpened) {
		t.Fatal("preset sharpen differs from explicit CLI")
	}
	if !bytes.Equal(run("--lrt-render-preset", presetPath, "--sharpen=false"), control) {
		t.Fatal("--sharpen=false failed to disable saved sharpen")
	}
	position := 4
	for i := 0; i < 3; i++ {
		if position+8 > len(sharpened) || string(sharpened[position:position+4]) != "00dc" {
			t.Fatal("invalid AVI chunk")
		}
		size := int(binary.LittleEndian.Uint32(sharpened[position+4:]))
		position += 8
		if size < 0 || position+size > len(sharpened) {
			t.Fatal("truncated JPEG")
		}
		data, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("frame%02d.png", i)))
		if err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		filtered, err := sharpenOutputImage(img)
		if err != nil {
			t.Fatal(err)
		}
		var want bytes.Buffer
		if err := jpeg.Encode(&want, filtered, &jpeg.Options{Quality: 100}); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(sharpened[position:position+size], want.Bytes()) {
			t.Fatal("AVI did not encode filtered pixels")
		}
		position += size + size%2
	}
	batch := t.TempDir()
	markerCommand(t, "batch-render", dir, "--output", batch, "--format", "avi", "--", "--fps", "2", "--quality", "100", "--sharpen")
	files, err := filepath.Glob(filepath.Join(batch, "*.avi"))
	if err != nil || len(files) != 1 {
		t.Fatalf("batch output: %v %v", files, err)
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	movi := bytes.Index(data, []byte("movi"))
	if movi < 0 || !bytes.Equal(data[movi:], sharpened) {
		t.Fatal("batch sharpen differs from single render")
	}
	assertPreviewEVFileBytes(t, presetPath, presetBefore)
}
