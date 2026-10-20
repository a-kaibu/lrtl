package lrtl

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

type casPlaneObservation struct {
	Width, Height   int
	Pattern         string
	Strength        float32
	Input, Expected []uint16
}

func casObservations(t *testing.T) []casPlaneObservation {
	t.Helper()
	data, err := os.ReadFile("testdata/lrt-cas-ffmpeg-8.0.1.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []casPlaneObservation
	if err := json.Unmarshal(data, &cases); err != nil || len(cases) != 24 {
		t.Fatalf("expected 24 measured CAS planes: %v", err)
	}
	for _, tc := range cases {
		if tc.Width != 32 || tc.Height != 16 || len(tc.Input) != 512 || len(tc.Expected) != 512 {
			t.Fatal("invalid measured CAS plane")
		}
	}
	return cases
}

func TestOutputCASMeasuredPlanes(t *testing.T) {
	for _, tc := range casObservations(t) {
		t.Run(tc.Pattern+"/"+strconv.FormatFloat(float64(tc.Strength), 'f', 2, 32), func(t *testing.T) {
			before := append([]uint16(nil), tc.Input...)
			got := casPlane10(tc.Input, tc.Width, tc.Height, tc.Strength)
			for i, expected := range tc.Expected {
				if got[i] != expected {
					t.Fatalf("sample (%d,%d): got %d want observed %d", i%tc.Width, i/tc.Width, got[i], expected)
				}
			}
			if !reflect.DeepEqual(before, tc.Input) {
				t.Fatal("CAS changed source plane")
			}
		})
	}
	for _, size := range [][2]int{{1, 1}, {1, 7}, {7, 1}, {2, 2}} {
		for _, value := range []uint16{0, 1, 1023} {
			input := make([]uint16, size[0]*size[1])
			for i := range input {
				input[i] = value
			}
			got := casPlane10(input, size[0], size[1], 0.9)
			if !reflect.DeepEqual(got, input) {
				t.Fatalf("thin constant frame %v value %d: %v", size, value, got)
			}
		}
	}
}

func TestOutputCASActualFFmpegPlanes(t *testing.T) {
	ffmpeg := os.Getenv("LRTL_CAS_FFMPEG")
	if ffmpeg == "" {
		var err error
		ffmpeg, err = exec.LookPath("ffmpeg")
		if err != nil {
			t.Skip("FFmpeg unavailable; stored CAS observations remain tested")
		}
	}
	for _, tc := range casObservations(t) {
		var raw bytes.Buffer
		for plane := 0; plane < 3; plane++ {
			for _, value := range tc.Input {
				if err := binary.Write(&raw, binary.LittleEndian, value); err != nil {
					t.Fatal(err)
				}
			}
		}
		filter := "cas=strength=" + strconv.FormatFloat(float64(tc.Strength), 'f', 2, 32)
		command := exec.Command(ffmpeg, "-v", "error", "-f", "rawvideo", "-pixel_format", "yuv444p10le", "-video_size", "32x16", "-framerate", "1", "-i", "pipe:0", "-vf", filter, "-f", "rawvideo", "-pix_fmt", "yuv444p10le", "pipe:1")
		command.Stdin = bytes.NewReader(raw.Bytes())
		var stderr bytes.Buffer
		command.Stderr = &stderr
		data, err := command.Output()
		if err != nil || len(data) != 512*6 {
			t.Fatalf("%s %s: bytes %d %v %s", tc.Pattern, filter, len(data), err, stderr.String())
		}
		for plane := 0; plane < 3; plane++ {
			for i, expected := range tc.Expected {
				if got := binary.LittleEndian.Uint16(data[(plane*512+i)*2:]); got != expected {
					t.Fatalf("%s strength %g plane %d sample %d: got %d want observed %d", tc.Pattern, tc.Strength, plane, i, got, expected)
				}
			}
		}
	}
}

func TestOutputCASNativePresetAndOverrides(t *testing.T) {
	data, err := os.ReadFile("testdata/lrt-cas-filter-7.5.5.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Strength float32
		Filter   string
	}
	if err := json.Unmarshal(data, &cases); err != nil || len(cases) != 5 {
		t.Fatalf("expected five native helper observations: %v", err)
	}
	for _, tc := range cases {
		want := ""
		if tc.Strength > 0 {
			want = ",cas=strength=" + strconv.FormatFloat(float64(tc.Strength), 'f', 2, 32)
		}
		if tc.Filter != want {
			t.Fatalf("native CAS helper %g: %s", tc.Strength, tc.Filter)
		}
	}
	for _, enabled := range []bool{false, true} {
		preset, err := decodeLRTRenderPreset(renderPresetObservations(t)[0].Preset)
		if err != nil {
			t.Fatal(err)
		}
		preset.CAS = enabled
		options, err := applyLRTRenderPreset(RenderOptions{}, preset, "", nil)
		if err != nil || options.OutputCAS != enabled {
			t.Fatalf("imported CAS %v: %+v %v", enabled, options, err)
		}
		options, err = applyLRTRenderPreset(RenderOptions{OutputCAS: !enabled}, preset, "", map[string]bool{"cas": true})
		if err != nil || options.OutputCAS != !enabled {
			t.Fatal("explicit CAS override ignored")
		}
	}
}

func TestOutputCASActualVideoPixels(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("FFmpeg unavailable")
	}
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe unavailable")
	}
	filters, err := exec.Command(ffmpeg, "-hide_banner", "-filters").CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(filters, []byte(" zscale ")) {
		t.Skip("zscale unavailable for explicit BT.709 range comparison")
	}
	dir := outputSharpenFixture(t, 3)
	before := batchFileSnapshot(t, dir)
	frames, err := DiscoverSequence(dir)
	if err != nil {
		t.Fatal(err)
	}
	var expected []int
	for _, frame := range frames {
		data, err := os.ReadFile(frame.Image)
		if err != nil {
			t.Fatal(err)
		}
		img, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		rgb, err := imageRGB48(img)
		if err != nil {
			t.Fatal(err)
		}
		casOutputRGB48(rgb, 64, 32)
		for i := 0; i < len(rgb); i += 6 {
			r := float64(binary.LittleEndian.Uint16(rgb[i:]))
			g := float64(binary.LittleEndian.Uint16(rgb[i+2:]))
			b := float64(binary.LittleEndian.Uint16(rgb[i+4:]))
			expected = append(expected, int(math.Round((0.2126*r+0.7152*g+0.0722*b)*1023/65535)))
		}
	}
	for _, codec := range []string{"h264", "h265"} {
		t.Run(codec, func(t *testing.T) {
			encoder, ext := "libx264", ".mp4"
			if codec == "h265" {
				encoder, ext = "libx265", ".mov"
			}
			if err := requireFFmpegPixelFormat(ffmpeg, encoder, "yuv444p10le"); err != nil {
				t.Skipf("encoder unavailable: %v", err)
			}
			output := filepath.Join(t.TempDir(), "cas"+ext)
			markerCommand(t, "render", dir, "--output", output, "--ffmpeg", ffmpeg, "--codec", codec, "--subsampling", "444", "--video-range", "full", "--fps", "2", "--crf", "0", "--preset", "ultrafast", "--cas")
			metadata, err := exec.Command(ffprobe, "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=pix_fmt,nb_frames,avg_frame_rate,color_range", "-of", "json", output).CombinedOutput()
			if err != nil {
				t.Fatalf("probe CAS video: %s %v", metadata, err)
			}
			var probe struct {
				Streams []struct {
					Pixel  string `json:"pix_fmt"`
					Frames string `json:"nb_frames"`
					FPS    string `json:"avg_frame_rate"`
					Range  string `json:"color_range"`
				}
			}
			if err := json.Unmarshal(metadata, &probe); err != nil || len(probe.Streams) != 1 {
				t.Fatalf("invalid metadata: %s %v", metadata, err)
			}
			stream := probe.Streams[0]
			if stream.Pixel != "yuv444p10le" || stream.Frames != "3" || stream.FPS != "2/1" || stream.Range != "pc" {
				t.Fatalf("CAS video format: %s", metadata)
			}
			luma, err := exec.Command(ffmpeg, "-v", "error", "-i", output, "-vf", "extractplanes=y", "-f", "rawvideo", "pipe:1").Output()
			if err != nil || len(luma) != len(expected)*2 {
				t.Fatalf("decode CAS video: bytes %d %v", len(luma), err)
			}
			maxError, totalError := 0, 0
			for i, value := range expected {
				diff := absInt(int(binary.LittleEndian.Uint16(luma[i*2:])) - value)
				maxError = max(maxError, diff)
				totalError += diff
			}
			meanError := float64(totalError) / float64(len(expected))
			t.Logf("CAS video: 6144 decoded luma samples, maximum error %d, mean %.3f", maxError, meanError)
			// x264 CRF 0 is lossless; x265 CRF 0 still quantizes the image.
			// Bound both isolated errors and the average error for that lossy
			// encode/decode check; the scalar CAS comparisons above are exact.
			limit, meanLimit := 4, 3.0
			if codec == "h265" {
				limit, meanLimit = 32, 4
			}
			if maxError > limit || meanError > meanLimit {
				t.Fatalf("CAS decoded error: maximum %d (limit %d), mean %.3f (limit %.3f)", maxError, limit, meanError, meanLimit)
			}
		})
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("actual video render changed sources")
	}
}

func TestOutputCASRGB48PrecisionBoundsAlphaAndClipping(t *testing.T) {
	if _, err := filterOutputImage(image.NewGray16(image.Rectangle{}), RenderOptions{OutputCAS: true}); err == nil {
		t.Fatal("accepted empty CAS frame")
	}
	for _, value := range []uint16{20000, 20001} {
		rgb := make([]byte, 6)
		for i := 0; i < 3; i++ {
			binary.LittleEndian.PutUint16(rgb[i*2:], value)
		}
		before := bytes.Clone(rgb)
		casOutputRGB48(rgb, 1, 1)
		if !bytes.Equal(before, rgb) {
			t.Fatalf("sub-10-bit precision lost in constant neutral frame: %v -> %v", before, rgb)
		}
	}
	img := image.NewNRGBA64(image.Rect(3, 5, 35, 21))
	for y := 5; y < 21; y++ {
		for x := 3; x < 35; x++ {
			v := uint16((x*13 + y*23 + x*y*7) % 101 * 600)
			img.SetNRGBA64(x, y, color.NRGBA64{v, 65535 - v, v / 3, 32768})
		}
	}
	before, err := imageRGB48(img)
	if err != nil {
		t.Fatal(err)
	}
	want := bytes.Clone(before)
	casOutputRGB48(want, 32, 16)
	if bytes.Equal(want, before) {
		t.Fatal("CAS did not change colored patterned input")
	}
	got, err := filterOutputImage(img, RenderOptions{OutputCAS: true})
	if err != nil {
		t.Fatal(err)
	}
	actual, err := imageRGB48(got)
	if err != nil || !bytes.Equal(actual, want) {
		t.Fatal("nonzero bounds or alpha composition differs from RGB48 path")
	}
	after, _ := imageRGB48(img)
	if !bytes.Equal(before, after) {
		t.Fatal("CAS changed original image")
	}
	_, _, _, a := got.At(0, 0).RGBA()
	if a != 65535 {
		t.Fatal("video frame must be opaque")
	}
	// Colored impulses can overshoot RGB bounds even after each YUV plane clips.
	for _, center := range [][3]uint16{{65535, 40000, 40000}, {0, 20000, 20000}} {
		rgb := make([]byte, 3*3*6)
		for i := 0; i < 9; i++ {
			for c := 0; c < 3; c++ {
				v := uint16(30000)
				if i == 4 {
					v = center[c]
				}
				binary.LittleEndian.PutUint16(rgb[i*6+c*2:], v)
			}
		}
		casOutputRGB48(rgb, 3, 3)
		if got := binary.LittleEndian.Uint16(rgb[24:]); got != center[0] {
			t.Fatalf("CAS did not clip RGB red: %d want %d", got, center[0])
		}
	}
}

func TestOutputCASRenderOrderingAndFrameSchedule(t *testing.T) {
	dir := outputSharpenFixture(t, 6)
	before := batchFileSnapshot(t, dir)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(t.TempDir(), "capture")
	t.Setenv("LRTL_TEST_FFMPEG_CAPTURE", capture)
	for _, tc := range []struct {
		speed string
		blur  string
	}{{"1", "0"}, {"0.5", "0"}, {"0.5", "2"}, {"2", "2"}} {
		for _, overlay := range []bool{false, true} {
			args := []string{"render", dir, "--output", filepath.Join(t.TempDir(), "out.mp4"), "--ffmpeg", executable, "--fps", "2", "--speed", tc.speed, "--motion-blur", tc.blur}
			markerCommand(t, args...)
			control, err := os.ReadFile(capture)
			if err != nil {
				t.Fatal(err)
			}
			on := append(append([]string(nil), args...), "--overwrite", "--sharpen", "--cas", "--fade-out", "1")
			if overlay {
				on = append(on, "--watermark", "TEST", "--watermark-position", "top-left")
			}
			if result := markerCommand(t, on...); !strings.Contains(result, "CAS 0.90") {
				t.Fatal("CAS missing from render summary")
			}
			actual, err := os.ReadFile(capture)
			if err != nil {
				t.Fatal(err)
			}
			want := bytes.Clone(control)
			const size = 64 * 32 * 6
			if len(want) != len(actual) || len(want)%size != 0 {
				t.Fatal("CAS changed output frame schedule")
			}
			fade, err := planVideoFade(len(want)/size, frameRate{2, 1}, "2", 1)
			if err != nil {
				t.Fatal(err)
			}
			face, err := newTimestampOverlayFace(64, 32)
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < len(want)/size; i++ {
				frame := want[i*size : (i+1)*size]
				sharpenOutputRGB48(frame, 64, 32)
				casOutputRGB48(frame, 64, 32)
				if overlay {
					img, err := imageFromRGB48(frame, 64, 32)
					if err != nil {
						t.Fatal(err)
					}
					pixels, err := imageRGB48(applyWatermarkOverlay(img, "TEST", "top-left", face))
					if err != nil {
						t.Fatal(err)
					}
					copy(frame, pixels)
				}
				fadeRGB48(frame, fade.gain(i, frameRate{2, 1}))
			}
			if !bytes.Equal(want, actual) {
				t.Fatalf("wrong filter order or repeated filtering: speed %s blur %s overlay %v", tc.speed, tc.blur, overlay)
			}
		}
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("render changed sources")
	}
}

func TestOutputCASPresetBatchAndDisabledOverride(t *testing.T) {
	dir := outputSharpenFixture(t, 3)
	before := batchFileSnapshot(t, dir)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(t.TempDir(), "capture")
	t.Setenv("LRTL_TEST_FFMPEG_CAPTURE", capture)
	preset, err := decodeLRTRenderPreset(renderPresetObservations(t)[0].Preset)
	if err != nil {
		t.Fatal(err)
	}
	preset.CAS, preset.OutputResV = true, 32
	path := savePresetJSON(t, preset)
	presetBefore, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	readCapture := func() []byte {
		t.Helper()
		data, err := os.ReadFile(capture)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	markerCommand(t, "render", dir, "--output", filepath.Join(t.TempDir(), "control.mp4"), "--ffmpeg", executable)
	control := readCapture()
	markerCommand(t, "render", dir, "--output", filepath.Join(t.TempDir(), "explicit.mp4"), "--ffmpeg", executable, "--cas")
	want := readCapture()
	markerCommand(t, "render", dir, "--output", filepath.Join(t.TempDir(), "preset.mp4"), "--ffmpeg", executable, "--lrt-render-preset", path)
	actual := readCapture()
	if !bytes.Equal(want, actual) {
		t.Fatal("preset CAS differs from explicit CAS")
	}
	markerCommand(t, "batch-render", dir, "--output", t.TempDir(), "--", "--ffmpeg", executable, "--lrt-render-preset", path)
	actual = readCapture()
	if !bytes.Equal(want, actual) {
		t.Fatal("batch did not forward preset CAS")
	}
	markerCommand(t, "render", dir, "--output", filepath.Join(t.TempDir(), "off.mp4"), "--ffmpeg", executable, "--lrt-render-preset", path, "--cas=false")
	off := readCapture()
	if bytes.Equal(want, off) || !bytes.Equal(control, off) {
		t.Fatal("explicit false did not disable preset CAS")
	}
	assertPreviewEVFileBytes(t, path, presetBefore)
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("preset render changed sources")
	}
}

func TestOutputCASJPEGAVIExportsAndInvalidFlag(t *testing.T) {
	dir := t.TempDir()
	img := image.NewRGBA(image.Rect(0, 0, 64, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 64; x++ {
			img.SetRGBA(x, y, color.RGBA{uint8((x*13 + y*23) % 256), uint8(x * 4), uint8(y * 8), 255})
		}
	}
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, img, &jpeg.Options{Quality: 100}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "frame.jpg"), encoded.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	before := batchFileSnapshot(t, dir)
	decoded, err := jpeg.Decode(bytes.NewReader(encoded.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	filtered, err := filterOutputImage(decoded, RenderOptions{OutputCAS: true})
	if err != nil {
		t.Fatal(err)
	}
	var expected bytes.Buffer
	if err := jpeg.Encode(&expected, filtered, &jpeg.Options{Quality: 100}); err != nil {
		t.Fatal(err)
	}
	for _, enabled := range []bool{false, true} {
		output := filepath.Join(t.TempDir(), "out.avi")
		markerCommand(t, "render", dir, "--output", output, "--quality", "100", "--cas="+strconv.FormatBool(enabled))
		data, err := os.ReadFile(output)
		if err != nil {
			t.Fatal(err)
		}
		i := bytes.Index(data, []byte("movi")) + 4
		if i < 4 || i+8 > len(data) {
			t.Fatal("missing AVI frame")
		}
		size := int(binary.LittleEndian.Uint32(data[i+4:]))
		if size < 0 || i+8+size > len(data) {
			t.Fatal("truncated AVI frame")
		}
		want := encoded.Bytes()
		if enabled {
			want = expected.Bytes()
		}
		if !bytes.Equal(data[i+8:i+8+size], want) {
			t.Fatalf("AVI CAS %v: JPEG passthrough/filter mismatch", enabled)
		}
	}
	frames, err := DiscoverSequence(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"tiff", "preview"} {
		var control map[string][]byte
		for _, enabled := range []bool{false, true} {
			output := filepath.Join(t.TempDir(), "export")
			options := RenderOptions{Quality: 100, OutputCAS: enabled}
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
			if !enabled {
				control = contents
			} else if !reflect.DeepEqual(control, contents) {
				t.Fatalf("video CAS changed %s export", kind)
			}
		}
	}
	output := filepath.Join(t.TempDir(), "keep.avi")
	if err := os.WriteFile(output, []byte("existing video"), 0600); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	if Main([]string{"render", dir, "--output", output, "--overwrite", "--cas=invalid"}, &out, &stderr) == 0 {
		t.Fatal("accepted invalid CAS bool")
	}
	assertPreviewEVFileBytes(t, output, []byte("existing video"))
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("CAS changed source JPEG")
	}
}
