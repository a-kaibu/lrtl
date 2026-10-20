package lrtl

import (
	"bytes"
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

type outputDeflickerObservation struct {
	Luma, Chroma                        bool
	Width, Height, Frames, OutputFrames int
	Pattern, Filter                     string
	Input, Expected                     []uint16
}

func TestOutputDeflickerLiveReferences(t *testing.T) {
	ffmpeg := os.Getenv("LRTL_OUTPUT_DEFLICKER_FFMPEG")
	if ffmpeg == "" {
		t.Skip("set LRTL_OUTPUT_DEFLICKER_FFMPEG for the bundled FFmpeg")
	}
	for _, tc := range outputDeflickerObservations(t) {
		var input, stderr bytes.Buffer
		for _, v := range tc.Input {
			_ = binary.Write(&input, binary.LittleEndian, v)
		}
		cmd := exec.Command(ffmpeg, "-v", "error", "-f", "rawvideo", "-pixel_format", "yuv444p10le", "-video_size", fmt.Sprintf("%dx%d", tc.Width, tc.Height), "-framerate", "24", "-i", "pipe:0", "-vf", tc.Filter, "-fps_mode", "passthrough", "-f", "rawvideo", "-pix_fmt", "yuv444p10le", "pipe:1")
		cmd.Stdin, cmd.Stderr = &input, &stderr
		actual, err := cmd.Output()
		if err != nil {
			t.Fatalf("native filter: %v %s", err, stderr.String())
		}
		var want bytes.Buffer
		for _, v := range tc.Expected {
			_ = binary.Write(&want, binary.LittleEndian, v)
		}
		if !bytes.Equal(actual, want.Bytes()) {
			t.Fatalf("native output changed: %v/%v %dx%d frames %d %s", tc.Luma, tc.Chroma, tc.Width, tc.Height, tc.Frames, tc.Pattern)
		}
	}
}

// Batch reference uses full planes and the measured power-mean primitive;
// it does not use the production streaming cache or RGB delta helper.
func outputDeflickerReferenceRGB(input [][]byte, width, height int, luma, chroma bool) [][]byte {
	planes := make([][3][]uint16, len(input))
	var means [3][10]float32
	for f, rgb := range input {
		planes[f] = outputYUV10(rgb, width, height)
	}
	for p := range means {
		for f := 0; f < min(10, len(input)); f++ {
			means[p][f] = outputDeflickerMean(planes[f][p])
		}
	}
	out := make([][]byte, len(input))
	for f := range input {
		filtered := planes[f]
		for p := range means {
			if (p == 0 && luma) || (p > 0 && chroma) {
				filtered[p] = outputDeflickerPlane(planes[f][p], means[p])
			}
			copy(means[p][:9], means[p][1:])
			means[p][min(9, len(input)-f-1)] = outputDeflickerMean(planes[min(f+10, len(input)-1)][p])
		}
		out[f] = bytes.Clone(input[f])
		for i := 0; i < width*height; i++ {
			dy := float64(int(filtered[0][i])-int(planes[f][0][i])) * (65535.0 / 1023)
			cb := float64(int(filtered[1][i])-int(planes[f][1][i])) * (65535.0 / 1023)
			cr := float64(int(filtered[2][i])-int(planes[f][2][i])) * (65535.0 / 1023)
			for c, change := range []float64{dy + 1.5748*cr, dy - (.0722*1.8556/.7152)*cb - (.2126*1.5748/.7152)*cr, dy + 1.8556*cb} {
				at := i*6 + c*2
				v := int(binary.LittleEndian.Uint16(input[f][at:])) + int(math.Round(change))
				binary.LittleEndian.PutUint16(out[f][at:], uint16(min(65535, max(0, v))))
			}
		}
	}
	return out
}

func outputDeflickerSynthetic(count, width, height int) [][]byte {
	input := make([][]byte, count)
	for f := range input {
		input[f] = make([]byte, width*height*6)
		for i := 0; i < len(input[f])/2; i++ {
			binary.LittleEndian.PutUint16(input[f][i*2:], uint16((f*7001+i*1117)%65536))
		}
	}
	return input
}

func TestOutputDeflickerStreamingAndOwnership(t *testing.T) {
	for _, n := range []int{1, 2, 5, 9, 10, 11, 17, 23} {
		for _, mode := range [][2]bool{{true, false}, {false, true}, {true, true}, {false, false}} {
			input := outputDeflickerSynthetic(n, 7, 5)
			before := make([][]byte, n)
			for f := range input {
				before[f] = bytes.Clone(input[f])
			}
			want := outputDeflickerReferenceRGB(input, 7, 5, mode[0], mode[1])
			calls := make([]int, n)
			d := newOutputDeflicker(7, 5, n, mode[0], mode[1], func(i int) ([]byte, error) { calls[i]++; return input[i], nil })
			for f := range input {
				got, err := d.frame(f)
				if err != nil || !bytes.Equal(got, want[f]) {
					t.Fatalf("frames %d mode %v index %d: %v", n, mode, f, err)
				}
				if len(d.cache) > 10 {
					t.Fatal("unbounded deflicker cache")
				}
				clear(got)
			}
			for _, n := range calls {
				if n != 1 {
					t.Fatal("reread or skipped input")
				}
			}
			if !reflect.DeepEqual(input, before) {
				t.Fatal("source RGB changed")
			}
			if _, err := d.frame(n); err == nil {
				t.Fatal("accepted exhausted timeline")
			}
		}
	}
	d := newOutputDeflicker(1, 1, 2, true, true, func(int) ([]byte, error) { return nil, fmt.Errorf("future failed") })
	if _, err := d.frame(1); err == nil {
		t.Fatal("accepted nonsequential input")
	}
	if _, err := d.frame(0); err == nil || !strings.Contains(err.Error(), "future failed") {
		t.Fatal("lost lookahead error")
	}
	d = newOutputDeflicker(1, 1, 2, true, true, func(int) ([]byte, error) { return nil, nil })
	if _, err := d.frame(0); err == nil {
		t.Fatal("accepted wrong dimensions")
	}
	for _, noise := range []int{0, 1, 4} {
		if err := validateOutputDeflickerMemory(64, 32, 64, 32, 23, noise, 3, 2); err != nil {
			t.Fatal(err)
		}
		if err := validateOutputDeflickerMemory(32768, 32768, 32768, 32768, 23, noise, 3, 2); err == nil {
			t.Fatal("accepted excessive buffers")
		}
	}
}

func TestOutputDeflickerRenderPresetBatchAndLayering(t *testing.T) {
	dir := outputSharpenFixture(t, 17)
	before := batchFileSnapshot(t, dir)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(t.TempDir(), "capture")
	t.Setenv("LRTL_TEST_FFMPEG_CAPTURE", capture)
	markerCommand(t, "render", dir, "--output", filepath.Join(t.TempDir(), "base.mp4"), "--ffmpeg", executable)
	raw := denoiseRead(t, capture)
	var input [][]byte
	for i := 0; i < len(raw); i += 64 * 32 * 6 {
		input = append(input, raw[i:i+64*32*6])
	}
	for _, mode := range [][2]bool{{true, false}, {false, true}, {true, true}} {
		flags := []string{"--output-deflicker-luma=" + strconv.FormatBool(mode[0]), "--output-deflicker-chroma=" + strconv.FormatBool(mode[1])}
		run := func(extra ...string) []byte {
			args := []string{"render", dir, "--output", filepath.Join(t.TempDir(), "out.mp4"), "--ffmpeg", executable}
			markerCommand(t, append(args, extra...)...)
			return denoiseRead(t, capture)
		}
		filtered := outputDeflickerReferenceRGB(input, 64, 32, mode[0], mode[1])
		want := bytes.Join(filtered, nil)
		if !bytes.Equal(run(flags...), want) {
			t.Fatal("CLI deflicker differs from batch reference")
		}
		preset, err := decodeLRTRenderPreset(renderPresetObservations(t)[0].Preset)
		if err != nil {
			t.Fatal(err)
		}
		preset.DeflickerLuma, preset.DeflickerChroma, preset.OutputResV = mode[0], mode[1], 32
		path := savePresetJSON(t, preset)
		presetBefore := denoiseRead(t, path)
		if !bytes.Equal(run("--lrt-render-preset", path), want) {
			t.Fatal("preset lost deflicker")
		}
		markerCommand(t, "batch-render", dir, "--output", t.TempDir(), "--", "--ffmpeg", executable, "--lrt-render-preset", path)
		if !bytes.Equal(denoiseRead(t, capture), want) {
			t.Fatal("batch lost deflicker")
		}
		if !bytes.Equal(run("--lrt-render-preset", path, "--output-deflicker-luma=false", "--output-deflicker-chroma=false"), raw) {
			t.Fatal("explicit disable lost precedence")
		}
		assertPreviewEVFileBytes(t, path, presetBefore)
		// Native graph: render deflicker, noise, light trails, speed, blur,
		// output sharpen/CAS, overlays, trim and fade.
		for _, speed := range []float64{1, .5, 2} {
			clean := denoiseReferenceRGB(filtered, 64, 32, 2)
			clean = airplaneReferenceRGB(clean, 64, 32, 1)
			var resampled [][]byte
			for f := 0; f < airplaneTimelineSourceCount(len(input), speed, 1); f++ {
				for len(resampled) < playbackOutputEnd(f+1, speed) {
					resampled = append(resampled, clean[min(f, len(clean)-1)])
				}
			}
			plan, err := planFilteredVideoFrames(len(input), speed, 2, 1)
			if err != nil {
				t.Fatal(err)
			}
			fade, err := planVideoFade(plan.count(), frameRate{2, 1}, "2", 1)
			if err != nil {
				t.Fatal(err)
			}
			face, err := newTimestampOverlayFace(64, 32)
			if err != nil {
				t.Fatal(err)
			}
			var expected []byte
			for f := plan.start; f < plan.end; f++ {
				rgb, err := weightedRGB48Frames([][]byte{resampled[f-1], resampled[f]}, lrtMotionBlurWeights(2))
				if err != nil {
					t.Fatal(err)
				}
				sharpenOutputRGB48(rgb, 64, 32)
				casOutputRGB48(rgb, 64, 32)
				img, err := imageFromRGB48(rgb, 64, 32)
				if err != nil {
					t.Fatal(err)
				}
				rgb, err = imageRGB48(applyWatermarkOverlay(img, "TEST", "top-left", face))
				if err != nil {
					t.Fatal(err)
				}
				fadeRGB48(rgb, fade.gain(f-plan.start, frameRate{2, 1}))
				expected = append(expected, rgb...)
			}
			args := append(append([]string{}, flags...), "--denoise", "2", "--remove-airplanes", "1", "--speed", strconv.FormatFloat(speed, 'f', -1, 64), "--motion-blur", "2", "--sharpen", "--cas", "--watermark", "TEST", "--watermark-position", "top-left", "--fade-out", "1", "--fps", "2")
			if !bytes.Equal(run(args...), expected) {
				t.Fatalf("wrong filter order mode %v speed %g", mode, speed)
			}
		}
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("source sequence changed")
	}
}

func TestOutputDeflickerNativeCropOrder(t *testing.T) {
	dir := outputSharpenFixture(t, 11)
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
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(t.TempDir(), "capture")
	t.Setenv("LRTL_TEST_FFMPEG_CAPTURE", capture)
	for _, resolution := range []int{16, 32} {
		for _, position := range []int{0, 50, 100} {
			plan, err := planNativeRenderGeometryAtPosition(64, 32, resolution, 1, position)
			if err != nil {
				t.Fatal(err)
			}
			var scaled [][]byte
			for _, rgb := range input {
				img, err := imageFromRGB48(rgb, 64, 32)
				if err != nil {
					t.Fatal(err)
				}
				frame, err := imageRGB48(resizeFrame(img, plan.scaleWidth, plan.scaleHeight))
				if err != nil {
					t.Fatal(err)
				}
				scaled = append(scaled, frame)
			}
			filtered := outputDeflickerReferenceRGB(scaled, plan.scaleWidth, plan.scaleHeight, true, true)
			var expected []byte
			for _, rgb := range filtered {
				img, err := imageFromRGB48(rgb, plan.scaleWidth, plan.scaleHeight)
				if err != nil {
					t.Fatal(err)
				}
				cropped, err := imageRGB48(copyFrameRectangle(img, plan.crop))
				if err != nil {
					t.Fatal(err)
				}
				expected = append(expected, cropped...)
			}
			preset, err := decodeLRTRenderPreset(renderPresetObservations(t)[0].Preset)
			if err != nil {
				t.Fatal(err)
			}
			preset.DeflickerLuma, preset.DeflickerChroma, preset.OutputResV = true, true, resolution
			path := savePresetJSON(t, preset)
			markerCommand(t, "render", dir, "--output", filepath.Join(t.TempDir(), "crop.mp4"), "--ffmpeg", executable, "--lrt-render-preset", path, "--aspect", "1:1", "--crop-position", strconv.Itoa(position))
			if !bytes.Equal(expected, denoiseRead(t, capture)) {
				t.Fatalf("native crop %d resolution %d occurred before measurement", position, resolution)
			}
		}
	}
}

func TestOutputDeflickerFailureRetentionAndStillExports(t *testing.T) {
	dir := outputSharpenFixture(t, 11)
	frames, err := DiscoverSequence(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, preview := range []bool{false, true} {
		var control map[string][]byte
		for _, enabled := range []bool{false, true} {
			output := filepath.Join(t.TempDir(), "still")
			opts := RenderOptions{Quality: 100, OutputDeflickerLuma: enabled, OutputDeflickerChroma: enabled}
			if preview {
				_, err = GeneratePreviewImages(frames, output, opts)
			} else {
				err = ExportTIFFSequence(frames, output, opts)
			}
			if err != nil {
				t.Fatal(err)
			}
			contents := map[string][]byte{}
			for path, data := range batchFileSnapshot(t, output) {
				relative, err := filepath.Rel(output, path)
				if err != nil {
					t.Fatal(err)
				}
				contents[relative] = data
			}
			if !enabled {
				control = contents
			} else if !reflect.DeepEqual(control, contents) {
				t.Fatal("video deflicker changed still export")
			}
		}
	}
	// A decode failure beyond the first complete ten-frame window must not
	// replace an existing video, even after filtered frames have been written.
	if err := os.WriteFile(filepath.Join(dir, "frame10.png"), []byte("broken future"), 0600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "keep.avi")
	if err := os.WriteFile(output, []byte("keep original"), 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if Main([]string{"render", dir, "--output", output, "--overwrite", "--output-deflicker-luma", "--output-deflicker-chroma"}, &stdout, &stderr) == 0 {
		t.Fatal("ignored future decode error")
	}
	assertPreviewEVFileBytes(t, output, []byte("keep original"))
	files, err := os.ReadDir(filepath.Dir(output))
	if err != nil || len(files) != 1 {
		t.Fatal("failed deflicker left temporary output", err)
	}
	d := newOutputDeflicker(1, 1, 11, true, true, func(i int) ([]byte, error) {
		if i == 10 {
			return nil, fmt.Errorf("later future failed")
		}
		return make([]byte, 6), nil
	})
	if _, err := d.frame(0); err != nil {
		t.Fatal(err)
	}
	if _, err := d.frame(1); err == nil || !strings.Contains(err.Error(), "later future failed") {
		t.Fatal("lost later lookahead error")
	}
}

func TestOutputDeflickerJPEGAVI(t *testing.T) {
	dir := t.TempDir()
	for f, rgb := range outputDeflickerSynthetic(11, 16, 8) {
		img, err := imageFromRGB48(rgb, 16, 8)
		if err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 95}); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%03d.jpg", f)), buf.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
	}
	before := batchFileSnapshot(t, dir)
	frames, err := DiscoverSequence(dir)
	if err != nil {
		t.Fatal(err)
	}
	var input [][]byte
	for _, f := range frames {
		img, _, err := image.Decode(bytes.NewReader(denoiseRead(t, f.Image)))
		if err != nil {
			t.Fatal(err)
		}
		rgb, err := imageRGB48(img)
		if err != nil {
			t.Fatal(err)
		}
		input = append(input, rgb)
	}
	want := outputDeflickerReferenceRGB(input, 16, 8, true, true)
	output := filepath.Join(t.TempDir(), "output.avi")
	markerCommand(t, "render", dir, "--output", output, "--quality", "100", "--output-deflicker-luma", "--output-deflicker-chroma")
	data := denoiseRead(t, output)
	pos := bytes.Index(data, []byte("movi")) + 4
	if pos < 4 {
		t.Fatal("AVI has no movie list")
	}
	for f, rgb := range want {
		img, err := imageFromRGB48(rgb, 16, 8)
		if err != nil {
			t.Fatal(err)
		}
		var encoded bytes.Buffer
		if err := jpeg.Encode(&encoded, img, &jpeg.Options{Quality: 100}); err != nil {
			t.Fatal(err)
		}
		if pos+8 > len(data) || string(data[pos:pos+4]) != "00dc" {
			t.Fatal("missing AVI frame")
		}
		size := int(binary.LittleEndian.Uint32(data[pos+4:]))
		pos += 8
		if pos+size > len(data) || !bytes.Equal(encoded.Bytes(), data[pos:pos+size]) {
			t.Fatalf("AVI skipped deflicker on JPEG %d", f)
		}
		pos += size + size%2
	}
	if pos+4 <= len(data) && string(data[pos:pos+4]) == "00dc" {
		t.Fatal("extra AVI frame")
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("source JPEG changed")
	}
}

func TestOutputDeflickerActualVideo(t *testing.T) {
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
	dir := outputSharpenFixture(t, 11)
	before := batchFileSnapshot(t, dir)
	frames, err := DiscoverSequence(dir)
	if err != nil {
		t.Fatal(err)
	}
	var input [][]byte
	for _, f := range frames {
		img, _, err := image.Decode(bytes.NewReader(denoiseRead(t, f.Image)))
		if err != nil {
			t.Fatal(err)
		}
		rgb, err := imageRGB48(img)
		if err != nil {
			t.Fatal(err)
		}
		input = append(input, rgb)
	}
	for _, mode := range [][2]bool{{true, false}, {false, true}, {true, true}} {
		filtered := outputDeflickerReferenceRGB(input, 64, 32, mode[0], mode[1])
		for _, speed := range []float64{1, .5, 2} {
			var expected []int
			for f := range filtered {
				for frame := playbackOutputEnd(f, speed); frame < playbackOutputEnd(f+1, speed); frame++ {
					for i := 0; i < len(filtered[f]); i += 6 {
						rgb := filtered[f][i:]
						r, g, b := float64(binary.LittleEndian.Uint16(rgb)), float64(binary.LittleEndian.Uint16(rgb[2:])), float64(binary.LittleEndian.Uint16(rgb[4:]))
						expected = append(expected, int(math.Round((.2126*r+.7152*g+.0722*b)*1023/65535)))
					}
				}
			}
			for _, codec := range []string{"h264", "h265"} {
				t.Run(fmt.Sprintf("luma%v/chroma%v/speed%g/%s", mode[0], mode[1], speed, codec), func(t *testing.T) {
					encoder, ext := "libx264", ".mp4"
					if codec == "h265" {
						encoder, ext = "libx265", ".mov"
					}
					if err := requireFFmpegPixelFormat(ffmpeg, encoder, "yuv444p10le"); err != nil {
						t.Skipf("encoder unavailable: %v", err)
					}
					output := filepath.Join(t.TempDir(), "deflicker"+ext)
					markerCommand(t, "render", dir, "--output", output, "--ffmpeg", ffmpeg, "--codec", codec, "--subsampling", "444", "--video-range", "full", "--fps", "2", "--crf", "0", "--preset", "ultrafast", "--output-deflicker-luma="+strconv.FormatBool(mode[0]), "--output-deflicker-chroma="+strconv.FormatBool(mode[1]), "--speed", strconv.FormatFloat(speed, 'f', -1, 64))
					metadata, err := exec.Command(ffprobe, "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=pix_fmt,nb_frames,duration,avg_frame_rate", "-of", "json", output).CombinedOutput()
					if err != nil {
						t.Fatalf("metadata %v %s", err, metadata)
					}
					var probe struct {
						Streams []struct {
							Pixel    string `json:"pix_fmt"`
							Frames   string `json:"nb_frames"`
							Duration string `json:"duration"`
							FPS      string `json:"avg_frame_rate"`
						}
					}
					if err := json.Unmarshal(metadata, &probe); err != nil || len(probe.Streams) != 1 {
						t.Fatalf("metadata %s %v", metadata, err)
					}
					count := len(expected) / (64 * 32)
					s := probe.Streams[0]
					duration, err := strconv.ParseFloat(s.Duration, 64)
					if err != nil || s.Frames != strconv.Itoa(count) || s.FPS != "2/1" || s.Pixel != "yuv444p10le" || math.Abs(duration-float64(count)/2) > .001 {
						t.Fatalf("wrong video timing %s", metadata)
					}
					luma, err := exec.Command(ffmpeg, "-v", "error", "-i", output, "-vf", "extractplanes=y", "-fps_mode", "passthrough", "-f", "rawvideo", "pipe:1").Output()
					if err != nil || len(luma) != len(expected)*2 {
						t.Fatalf("decoded output %d %v", len(luma), err)
					}
					maximum, total := 0, 0
					for i, want := range expected {
						difference := absInt(int(binary.LittleEndian.Uint16(luma[i*2:])) - want)
						maximum = max(maximum, difference)
						total += difference
					}
					limit, meanLimit := 4, 3.0
					if codec == "h265" {
						limit, meanLimit = 32, 4
					}
					mean := float64(total) / float64(len(expected))
					t.Logf("%d frames luma max %d mean %.3f", count, maximum, mean)
					if maximum > limit || mean > meanLimit {
						t.Fatal("encoded deflicker differs from filtered RGB")
					}
				})
			}
		}
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("actual rendering changed sources")
	}
}

func outputDeflickerObservations(t *testing.T) []outputDeflickerObservation {
	t.Helper()
	var rows []outputDeflickerObservation
	if err := json.Unmarshal(denoiseRead(t, "testdata/lrt-output-deflicker-planes-8.0.1.json"), &rows); err != nil || len(rows) != 288 {
		t.Fatalf("expected 288 output deflicker references: %v", err)
	}
	return rows
}

func TestOutputDeflickerHelperGraphs(t *testing.T) {
	var presets []struct {
		Luma, Chroma bool
		Filter       string
	}
	if err := json.Unmarshal(denoiseRead(t, "testdata/lrt-output-deflicker-filter-7.5.5.json"), &presets); err != nil || len(presets) != 4 {
		t.Fatalf("expected four installed helper graphs: %v", err)
	}
	graphs := map[[2]bool]string{}
	for _, preset := range presets {
		key := [2]bool{preset.Luma, preset.Chroma}
		if _, exists := graphs[key]; exists {
			t.Fatal("duplicate helper combination")
		}
		graphs[key] = strings.TrimPrefix(preset.Filter, ",")
	}
	if graphs[[2]bool{false, false}] != "" {
		t.Fatal("disabled native helper has a filter")
	}
	for _, row := range outputDeflickerObservations(t) {
		graph, exists := graphs[[2]bool{row.Luma, row.Chroma}]
		if !exists || graph != row.Filter {
			t.Fatal("measured graph differs from installed helper")
		}
	}
}

func TestOutputDeflickerMeasuredPlanes(t *testing.T) {
	total := 0
	for _, tc := range outputDeflickerObservations(t) {
		t.Run(fmt.Sprintf("luma%v/chroma%v/%dx%d/%d/%s", tc.Luma, tc.Chroma, tc.Width, tc.Height, tc.Frames, tc.Pattern), func(t *testing.T) {
			if tc.OutputFrames != tc.Frames {
				t.Fatalf("native output count %d != %d", tc.OutputFrames, tc.Frames)
			}
			area := tc.Width * tc.Height
			var means [3][10]float32
			for p := range means {
				for f := 0; f < min(10, tc.Frames); f++ {
					at := (f*3 + p) * area
					means[p][f] = outputDeflickerMean(tc.Input[at : at+area])
				}
			}
			mismatches, maximum := 0, 0
			for f := 0; f < tc.Frames; f++ {
				for p := range means {
					at := (f*3 + p) * area
					plane := tc.Input[at : at+area]
					if (p == 0 && tc.Luma) || (p > 0 && tc.Chroma) {
						plane = outputDeflickerPlane(plane, means[p])
					}
					for i, v := range plane {
						difference := absInt(int(v) - int(tc.Expected[at+i]))
						if difference != 0 {
							mismatches++
						}
						maximum = max(maximum, difference)
					}
					// EOF keeps a shrinking queue but shifts all ten mean slots.
					// Short sequences begin with zero-filled unused slots.
					future := min(f+10, tc.Frames-1)
					insert := min(9, tc.Frames-f-1)
					copy(means[p][:9], means[p][1:])
					nextAt := (future*3 + p) * area
					means[p][insert] = outputDeflickerMean(tc.Input[nextAt : nextAt+area])
				}
			}
			total += len(tc.Expected)
			if mismatches > 0 {
				t.Errorf("%d mismatches max %d", mismatches, maximum)
			}
		})
	}
	t.Logf("compared %d plane samples", total)
}
