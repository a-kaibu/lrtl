package lrtl

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
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

type denoisePlaneObservation struct {
	Component                    string
	CPUFlags                     string
	Level, Width, Height, Frames int
	Pattern, Filter              string
	Input, Expected              []uint16
}

func denoiseObservations(t *testing.T) []denoisePlaneObservation {
	t.Helper()
	data, err := os.ReadFile("testdata/lrt-denoise-planes-8.0.1.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []denoisePlaneObservation
	if err := json.Unmarshal(data, &cases); err != nil || len(cases) != 354 {
		t.Fatalf("expected 354 denoise observations: %v", err)
	}
	return cases
}

func TestOutputDenoiseMeasuredPlanes(t *testing.T) {
	for _, tc := range denoiseObservations(t) {
		settings := outputDenoisePreset(tc.Level)
		area := tc.Width * tc.Height
		var actual []uint16
		switch tc.Component {
		case "preclean":
			for p := 0; p < 3; p++ {
				actual = append(actual, denoisePreclean10(tc.Input[p*area:(p+1)*area], tc.Width, tc.Height)...)
			}
		case "wavelet":
			for p := 0; p < 3; p++ {
				actual = append(actual, waveletDenoise10(tc.Input[p*area:(p+1)*area], tc.Width, tc.Height, settings)...)
			}
		case "temporal", "full":
			input := append([]uint16(nil), tc.Input...)
			if tc.Component == "full" && tc.Level == 4 {
				for i := 0; i < tc.Frames*3; i++ {
					copy(input[i*area:(i+1)*area], denoisePreclean10(input[i*area:(i+1)*area], tc.Width, tc.Height))
				}
			}
			for f := 0; f < tc.Frames; f++ {
				for p := 0; p < 3; p++ {
					window := make([][]uint16, settings.window)
					radius := settings.window / 2
					for i := range window {
						index := f + i - radius
						if index < 0 {
							index = 0
						} else if index >= tc.Frames {
							index = max(0, tc.Frames-radius)
						}
						start := (index*3 + p) * area
						window[i] = input[start : start+area]
					}
					plane := temporalDenoise10(window, settings.a[p], settings.b[p])
					if tc.Component == "full" {
						plane = waveletDenoise10(plane, tc.Width, tc.Height, settings)
					}
					actual = append(actual, plane...)
				}
			}
		default:
			t.Fatal("unknown reference component")
		}
		if len(actual) != len(tc.Expected) {
			t.Fatal("different plane size")
		}
		mismatches, maxError := 0, 0
		for i, expected := range tc.Expected {
			if actual[i] != expected {
				mismatches++
				maxError = max(maxError, absInt(int(actual[i])-int(expected)))
			}
		}
		if mismatches > 0 {
			t.Errorf("%s level %d pattern %s %dx%d frames %d: %d mismatches, max error %d", tc.Component, tc.Level, tc.Pattern, tc.Width, tc.Height, tc.Frames, mismatches, maxError)
		}
	}
}

func TestOutputDenoiseDeepWaveletLevels(t *testing.T) {
	var cases []struct {
		Level, Width, Height, Steps int
		SHA256                      string
	}
	if err := json.Unmarshal(denoiseRead(t, "testdata/lrt-denoise-deep-8.0.1.json"), &cases); err != nil || len(cases) != 3 {
		t.Fatalf("deep wavelet observations: %v", err)
	}
	for _, tc := range cases {
		input := make([]uint16, tc.Width*tc.Height)
		for y := 0; y < tc.Height; y++ {
			for x := 0; x < tc.Width; x++ {
				input[y*tc.Width+x] = uint16((x*13 + y*23 + x*y*7) % 101 * 1023 / 100)
			}
		}
		settings := outputDenoisePreset(tc.Level)
		if settings.steps != tc.Steps {
			t.Fatal("wrong deep transform step count")
		}
		output := waveletDenoise10(input, tc.Width, tc.Height, settings)
		raw := make([]byte, len(output)*2)
		for i, v := range output {
			binary.LittleEndian.PutUint16(raw[i*2:], v)
		}
		sum := sha256.Sum256(raw)
		if fmt.Sprintf("%x", sum) != tc.SHA256 {
			t.Fatalf("wavelet level %d %dx%d digest %x want %s", tc.Level, tc.Width, tc.Height, sum, tc.SHA256)
		}
	}
}

func TestOutputDenoiseLivePlaneReferences(t *testing.T) {
	executable := os.Getenv("LRTL_DENOISE_FFMPEG")
	if executable == "" {
		t.Skip("set LRTL_DENOISE_FFMPEG to repeat the 354 recorded filter observations")
	}
	for _, tc := range denoiseObservations(t) {
		args := []string{"-v", "error"}
		if tc.CPUFlags == "0" {
			args = append(args, "-cpuflags", "0")
		}
		args = append(args, "-f", "rawvideo", "-pixel_format", "yuv444p10le", "-video_size", fmt.Sprintf("%dx%d", tc.Width, tc.Height), "-framerate", "1", "-i", "pipe:0", "-vf", tc.Filter, "-f", "rawvideo", "-pix_fmt", "yuv444p10le", "pipe:1")
		var raw, stderr bytes.Buffer
		for _, v := range tc.Input {
			if err := binary.Write(&raw, binary.LittleEndian, v); err != nil {
				t.Fatal(err)
			}
		}
		cmd := exec.Command(executable, args...)
		cmd.Stdin, cmd.Stderr = &raw, &stderr
		data, err := cmd.Output()
		if err != nil || len(data) != len(tc.Expected)*2 {
			t.Fatalf("%s level %d CPU %s: bytes %d %v %s", tc.Component, tc.Level, tc.CPUFlags, len(data), err, stderr.String())
		}
		for i, want := range tc.Expected {
			if got := binary.LittleEndian.Uint16(data[i*2:]); got != want {
				t.Fatalf("%s level %d CPU %s sample %d: %d want recorded %d", tc.Component, tc.Level, tc.CPUFlags, i, got, want)
			}
		}
	}
}

// Offline reference joins separately measured plane operations without the
// production streaming cache. The tests below check cache, RGB deltas and order.
func denoiseReferenceRGB(input [][]byte, width, height, level int) [][]byte {
	settings := outputDenoisePreset(level)
	original := make([][3][]uint16, len(input))
	clean := make([][3][]uint16, len(input))
	for i, rgb := range input {
		original[i] = outputYUV10(rgb, width, height)
		clean[i] = original[i]
		if level == 4 {
			for p := range clean[i] {
				clean[i][p] = denoisePreclean10(original[i][p], width, height)
			}
		}
	}
	result := make([][]byte, len(input))
	for f := range input {
		var filtered [3][]uint16
		for p := range filtered {
			window := make([][]uint16, settings.window)
			for i := range window {
				index := max(0, f+i-settings.window/2)
				if index >= len(input) {
					index = max(0, len(input)-settings.window/2)
				}
				window[i] = clean[index][p]
			}
			filtered[p] = waveletDenoise10(temporalDenoise10(window, settings.a[p], settings.b[p]), width, height, settings)
		}
		result[f] = bytes.Clone(input[f])
		for i := 0; i < width*height; i++ {
			dy := float64(int(filtered[0][i])-int(original[f][0][i])) * 65535 / 1023
			cb := float64(int(filtered[1][i])-int(original[f][1][i])) * 65535 / 1023
			cr := float64(int(filtered[2][i])-int(original[f][2][i])) * 65535 / 1023
			for c, change := range []float64{dy + 1.5748*cr, dy - (0.0722*1.8556/0.7152)*cb - (0.2126*1.5748/0.7152)*cr, dy + 1.8556*cb} {
				at := i*6 + c*2
				v := int(binary.LittleEndian.Uint16(input[f][at:])) + int(math.Round(change))
				binary.LittleEndian.PutUint16(result[f][at:], uint16(min(65535, max(0, v))))
			}
		}
	}
	return result
}

func denoiseRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestOutputDenoiseStreamingOwnershipAndBoundaries(t *testing.T) {
	for _, count := range []int{1, 2, 3, 7, 15} {
		input := make([][]byte, count)
		for f := range input {
			input[f] = make([]byte, 16*8*6)
			for i := 0; i < len(input[f])/2; i++ {
				binary.LittleEndian.PutUint16(input[f][i*2:], uint16(20000+(f*97+i*13)%5000))
			}
		}
		for level := 1; level <= 4; level++ {
			want := denoiseReferenceRGB(input, 16, 8, level)
			calls := make([]int, count)
			d := newOutputDenoiser(16, 8, count, level, func(index int) ([]byte, error) {
				calls[index]++
				return input[index], nil
			})
			for f := range input {
				got, err := d.frame(f)
				if err != nil || !bytes.Equal(got, want[f]) {
					t.Fatalf("stream count %d level %d frame %d: %v", count, level, f, err)
				}
				if len(d.cache) > outputDenoisePreset(level).window {
					t.Fatal("unbounded denoise cache")
				}
				clear(got) // Caller ownership: later outputs must remain unaffected.
			}
			for _, n := range calls {
				if n != 1 {
					t.Fatal("denoiser reread or skipped a source frame")
				}
			}
			if _, err := d.frame(count); err == nil {
				t.Fatal("denoiser accepted an exhausted timeline")
			}
		}
	}
	d := newOutputDenoiser(1, 1, 1, 1, func(int) ([]byte, error) { return nil, fmt.Errorf("synthetic read error") })
	if _, err := d.frame(1); err == nil {
		t.Fatal("accepted out-of-order request")
	}
	if _, err := d.frame(0); err == nil || !strings.Contains(err.Error(), "synthetic read error") {
		t.Fatal("discarded future-frame load error")
	}
	d = newOutputDenoiser(1, 1, 1, 1, func(int) ([]byte, error) { return nil, nil })
	if _, err := d.frame(0); err == nil {
		t.Fatal("accepted inconsistent cached dimensions")
	}
}

func TestOutputDenoiseRenderScheduleAndLayering(t *testing.T) {
	dir := outputSharpenFixture(t, 8)
	before := batchFileSnapshot(t, dir)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(t.TempDir(), "capture")
	t.Setenv("LRTL_TEST_FFMPEG_CAPTURE", capture)
	markerCommand(t, "render", dir, "--output", filepath.Join(t.TempDir(), "control.mp4"), "--ffmpeg", executable)
	control := denoiseRead(t, capture)
	const size = 64 * 32 * 6
	var input [][]byte
	for i := 0; i < len(control); i += size {
		input = append(input, control[i:i+size])
	}
	for level := 1; level <= 4; level++ {
		filtered := denoiseReferenceRGB(input, 64, 32, level)
		for _, tc := range []struct {
			speed   float64
			blur    int
			overlay bool
		}{{1, 0, false}, {.5, 0, false}, {2, 2, true}} {
			args := []string{"render", dir, "--output", filepath.Join(t.TempDir(), "noise.mp4"), "--ffmpeg", executable, "--denoise", strconv.Itoa(level), "--fps", "2", "--speed", strconv.FormatFloat(tc.speed, 'f', -1, 64), "--motion-blur", strconv.Itoa(tc.blur), "--sharpen", "--cas", "--fade-out", "1"}
			if tc.overlay {
				args = append(args, "--watermark", "TEST", "--watermark-position", "top-left")
			}
			if result := markerCommand(t, args...); !strings.Contains(result, fmt.Sprintf("denoise level %d", level)) {
				t.Fatal("missing denoise summary")
			}
			var resampled [][]byte
			for f := range filtered {
				for len(resampled) < playbackOutputEnd(f+1, tc.speed) {
					resampled = append(resampled, filtered[f])
				}
			}
			timeline, err := planVideoFrames(len(input), tc.speed, tc.blur)
			if err != nil {
				t.Fatal(err)
			}
			fade, err := planVideoFade(timeline.count(), frameRate{2, 1}, "2", 1)
			if err != nil {
				t.Fatal(err)
			}
			face, err := newTimestampOverlayFace(64, 32)
			if err != nil {
				t.Fatal(err)
			}
			var expected []byte
			for i := timeline.start; i < timeline.end; i++ {
				rgb := bytes.Clone(resampled[i])
				if tc.blur > 0 {
					window := make([][]byte, tc.blur)
					for j := range window {
						window[j] = resampled[max(0, i-tc.blur+1+j)]
					}
					rgb, err = weightedRGB48Frames(window, lrtMotionBlurWeights(tc.blur))
					if err != nil {
						t.Fatal(err)
					}
				}
				sharpenOutputRGB48(rgb, 64, 32)
				casOutputRGB48(rgb, 64, 32)
				if tc.overlay {
					img, err := imageFromRGB48(rgb, 64, 32)
					if err != nil {
						t.Fatal(err)
					}
					rgb, err = imageRGB48(applyWatermarkOverlay(img, "TEST", "top-left", face))
					if err != nil {
						t.Fatal(err)
					}
				}
				fadeRGB48(rgb, fade.gain(i-timeline.start, frameRate{2, 1}))
				expected = append(expected, rgb...)
			}
			if !bytes.Equal(expected, denoiseRead(t, capture)) {
				t.Fatalf("denoise order/schedule level %d speed %g blur %d", level, tc.speed, tc.blur)
			}
		}
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("denoise changed source images/XMP")
	}
}

func TestOutputDenoisePresetBatchAndFailureRetention(t *testing.T) {
	dir := outputSharpenFixture(t, 8)
	before := batchFileSnapshot(t, dir)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(t.TempDir(), "capture")
	t.Setenv("LRTL_TEST_FFMPEG_CAPTURE", capture)
	for level := 1; level <= 4; level++ {
		preset, err := decodeLRTRenderPreset(renderPresetObservations(t)[0].Preset)
		if err != nil {
			t.Fatal(err)
		}
		preset.Denoise, preset.DenoiseLevel, preset.OutputResV = true, level, 32
		path := savePresetJSON(t, preset)
		presetBefore := denoiseRead(t, path)
		markerCommand(t, "render", dir, "--output", filepath.Join(t.TempDir(), "direct.mp4"), "--ffmpeg", executable, "--denoise", strconv.Itoa(level), "--output-color", "lrt-7.5.5")
		want := denoiseRead(t, capture)
		markerCommand(t, "render", dir, "--output", filepath.Join(t.TempDir(), "preset.mp4"), "--ffmpeg", executable, "--lrt-render-preset", path)
		if !bytes.Equal(want, denoiseRead(t, capture)) {
			t.Fatal("preset denoise differs from explicit level")
		}
		markerCommand(t, "batch-render", dir, "--output", t.TempDir(), "--", "--ffmpeg", executable, "--lrt-render-preset", path)
		if !bytes.Equal(want, denoiseRead(t, capture)) {
			t.Fatal("batch lost denoise preset")
		}
		markerCommand(t, "render", dir, "--output", filepath.Join(t.TempDir(), "off.mp4"), "--ffmpeg", executable, "--lrt-render-preset", path, "--denoise", "0")
		if bytes.Equal(want, denoiseRead(t, capture)) {
			t.Fatal("explicit zero did not disable saved denoise")
		}
		assertPreviewEVFileBytes(t, path, presetBefore)
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("preset denoise changed sources")
	}
	for _, value := range []string{"-1", "5", "NaN"} {
		output := filepath.Join(t.TempDir(), "keep.avi")
		if err := os.WriteFile(output, []byte("existing video"), 0600); err != nil {
			t.Fatal(err)
		}
		var out, stderr bytes.Buffer
		if Main([]string{"render", dir, "--output", output, "--overwrite", "--denoise", value}, &out, &stderr) == 0 {
			t.Fatal("accepted invalid denoise level")
		}
		assertPreviewEVFileBytes(t, output, []byte("existing video"))
	}
	if err := os.WriteFile(filepath.Join(dir, "frame07.png"), []byte("broken future source"), 0600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "keep.avi")
	if err := os.WriteFile(output, []byte("existing video"), 0600); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	if Main([]string{"render", dir, "--output", output, "--overwrite", "--denoise", "4", "--speed", "2"}, &out, &stderr) == 0 {
		t.Fatal("ignored corrupt future source")
	}
	assertPreviewEVFileBytes(t, output, []byte("existing video"))
	if err := validateOutputDenoiseMemory(1920, 1080, 15, 4, 2); err != nil {
		t.Fatal(err)
	}
	if err := validateOutputDenoiseMemory(7680, 4320, 15, 4, 2); err == nil {
		t.Fatal("unbounded temporal denoise allocation")
	}
	if err := validateOutputDenoiseMemory(7680, 4320, 15, 0, 0); err != nil {
		t.Fatal("disabled denoise changed dimension support")
	}
}

func TestOutputDenoiseJPEGAVIAndStillExports(t *testing.T) {
	fixture := outputSharpenFixture(t, 1)
	img, _, err := image.Decode(bytes.NewReader(denoiseRead(t, filepath.Join(fixture, "frame00.png"))))
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
	rgb, err := imageRGB48(decoded)
	if err != nil {
		t.Fatal(err)
	}
	for level := 0; level <= 4; level++ {
		output := filepath.Join(t.TempDir(), "noise.avi")
		markerCommand(t, "render", dir, "--output", output, "--quality", "100", "--denoise", strconv.Itoa(level))
		data := denoiseRead(t, output)
		i := bytes.Index(data, []byte("movi")) + 4
		if i < 4 || i+8 > len(data) {
			t.Fatal("missing AVI frame")
		}
		size := int(binary.LittleEndian.Uint32(data[i+4:]))
		if size < 0 || i+8+size > len(data) {
			t.Fatal("truncated AVI frame")
		}
		want := encoded.Bytes()
		if level > 0 {
			filtered := denoiseReferenceRGB([][]byte{rgb}, 64, 32, level)[0]
			img, err := imageFromRGB48(filtered, 64, 32)
			if err != nil {
				t.Fatal(err)
			}
			var expected bytes.Buffer
			if err := jpeg.Encode(&expected, img, &jpeg.Options{Quality: 100}); err != nil {
				t.Fatal(err)
			}
			want = expected.Bytes()
		}
		if !bytes.Equal(data[i+8:i+8+size], want) {
			t.Fatalf("AVI noise level %d", level)
		}
	}
	frames, err := DiscoverSequence(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"tiff", "preview"} {
		var control map[string][]byte
		for _, level := range []int{0, 4} {
			output := filepath.Join(t.TempDir(), "export")
			options := RenderOptions{Quality: 100, OutputDenoise: level}
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
				contents[file.Name()] = denoiseRead(t, filepath.Join(output, file.Name()))
			}
			if level == 0 {
				control = contents
			} else if !reflect.DeepEqual(control, contents) {
				t.Fatalf("video denoise changed %s export", kind)
			}
		}
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("denoise changed source JPEG")
	}
}

func TestOutputDenoiseNativeConfigurationAndValidation(t *testing.T) {
	var native []struct {
		Level  int
		Filter string
	}
	if err := json.Unmarshal(denoiseRead(t, "testdata/lrt-denoise-filter-7.5.5.json"), &native); err != nil || len(native) != 4 {
		t.Fatalf("native noise presets: %v", err)
	}
	for _, tc := range native {
		settings := outputDenoisePreset(tc.Level)
		for _, part := range []string{"atadenoise=0a=" + strconv.FormatFloat(float64(settings.a[0]), 'f', 2, 32), "s=" + strconv.Itoa(settings.window), "threshold=" + strconv.Itoa(int(settings.threshold)), "nsteps=" + strconv.Itoa(settings.steps), "percent=80"} {
			if !strings.Contains(tc.Filter, part) {
				t.Fatalf("native configuration mismatch: %s missing %s", tc.Filter, part)
			}
		}
		if strings.Contains(tc.Filter, "method=soft") != settings.soft {
			t.Fatal("wrong wavelet threshold method")
		}
	}
	preset, err := decodeLRTRenderPreset([]byte(`{"encoder":"h264","frameRate":"24","outputResV":1080,"outputQuality":1,"denoise":true}`))
	if err != nil || preset.DenoiseLevel != 1 {
		t.Fatalf("native denoise default: %+v %v", preset, err)
	}
	for _, level := range []int{-1, 0, 5} {
		preset.DenoiseLevel = level
		if _, err := applyLRTRenderPreset(RenderOptions{}, preset, "", nil); err == nil {
			t.Fatal("accepted invalid active saved denoise level")
		}
		options, err := applyLRTRenderPreset(RenderOptions{}, preset, "", map[string]bool{"denoise": true})
		if err != nil || options.OutputDenoise != 0 {
			t.Fatal("explicit disable did not override invalid saved level")
		}
		preset.Denoise = false
		if _, err := applyLRTRenderPreset(RenderOptions{}, preset, "", nil); err != nil {
			t.Fatal("inactive denoise level was validated")
		}
		preset.Denoise = true
	}
}

func TestOutputDenoiseActualVideoPixels(t *testing.T) {
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
		t.Skip("zscale unavailable")
	}
	dir := outputSharpenFixture(t, 8)
	before := batchFileSnapshot(t, dir)
	frames, err := DiscoverSequence(dir)
	if err != nil {
		t.Fatal(err)
	}
	var input [][]byte
	for _, frame := range frames {
		img, _, err := image.Decode(bytes.NewReader(denoiseRead(t, frame.Image)))
		if err != nil {
			t.Fatal(err)
		}
		rgb, err := imageRGB48(img)
		if err != nil {
			t.Fatal(err)
		}
		input = append(input, rgb)
	}
	for level := 1; level <= 4; level++ {
		reference := denoiseReferenceRGB(input, 64, 32, level)
		var expected []int
		for _, rgb := range reference {
			for i := 0; i < len(rgb); i += 6 {
				r, g, b := float64(binary.LittleEndian.Uint16(rgb[i:])), float64(binary.LittleEndian.Uint16(rgb[i+2:])), float64(binary.LittleEndian.Uint16(rgb[i+4:]))
				expected = append(expected, int(math.Round((.2126*r+.7152*g+.0722*b)*1023/65535)))
			}
		}
		for _, codec := range []string{"h264", "h265"} {
			t.Run(fmt.Sprintf("level%d/%s", level, codec), func(t *testing.T) {
				encoder, ext := "libx264", ".mp4"
				if codec == "h265" {
					encoder, ext = "libx265", ".mov"
				}
				if err := requireFFmpegPixelFormat(ffmpeg, encoder, "yuv444p10le"); err != nil {
					t.Skipf("encoder unavailable: %v", err)
				}
				output := filepath.Join(t.TempDir(), "denoise"+ext)
				markerCommand(t, "render", dir, "--output", output, "--ffmpeg", ffmpeg, "--codec", codec, "--subsampling", "444", "--video-range", "full", "--fps", "2", "--crf", "0", "--preset", "ultrafast", "--denoise", strconv.Itoa(level))
				metadata, err := exec.Command(ffprobe, "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=pix_fmt,nb_frames,avg_frame_rate,color_range", "-of", "json", output).CombinedOutput()
				if err != nil {
					t.Fatalf("probe: %s %v", metadata, err)
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
					t.Fatalf("probe data: %s %v", metadata, err)
				}
				stream := probe.Streams[0]
				if stream.Pixel != "yuv444p10le" || stream.Frames != "8" || stream.FPS != "2/1" || stream.Range != "pc" {
					t.Fatalf("encoded noise video: %s", metadata)
				}
				luma, err := exec.Command(ffmpeg, "-v", "error", "-i", output, "-vf", "extractplanes=y", "-f", "rawvideo", "pipe:1").Output()
				if err != nil || len(luma) != len(expected)*2 {
					t.Fatalf("decode bytes %d %v", len(luma), err)
				}
				maximum, total := 0, 0
				for i, want := range expected {
					diff := absInt(int(binary.LittleEndian.Uint16(luma[i*2:])) - want)
					maximum = max(maximum, diff)
					total += diff
				}
				mean := float64(total) / float64(len(expected))
				limit, meanLimit := 4, 3.0
				if codec == "h265" {
					limit, meanLimit = 32, 4
				}
				t.Logf("16384 luma samples, max %d mean %.3f", maximum, mean)
				if maximum > limit || mean > meanLimit {
					t.Fatalf("encoded noise luma: maximum %d mean %.3f", maximum, mean)
				}
			})
		}
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("actual video denoise changed sources")
	}
}
