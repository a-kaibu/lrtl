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

type airplanePlaneObservation struct {
	Component, CPUFlags                        string
	Level, Width, Height, Frames, OutputFrames int
	Pattern, Filter                            string
	Input, Expected                            []uint16
	PTS                                        []int
}

func airplaneObservations(t *testing.T) []airplanePlaneObservation {
	t.Helper()
	data, err := os.ReadFile("testdata/lrt-airplane-planes-8.0.1.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []airplanePlaneObservation
	if err := json.Unmarshal(data, &rows); err != nil || len(rows) != 102 {
		t.Fatalf("expected 102 airplane plane observations: %v", err)
	}
	return rows
}
func TestAirplaneRemovalMeasuredPlanesAndPTS(t *testing.T) {
	for _, tc := range airplaneObservations(t) {
		area := tc.Width * tc.Height
		var actual []uint16
		if tc.Component == "spatial" {
			actual = append(actual, airplaneLuma10(tc.Input[:area], tc.Width, tc.Height, tc.Level)...)
			for _, v := range tc.Input[area:] {
				actual = append(actual, min(960, max(64, v)))
			}
		} else {
			radius := airplaneRadius(tc.Level)
			if tc.OutputFrames != tc.Frames-2*radius {
				t.Fatal("native tmedian output count differs")
			}
			for f := 0; f < tc.OutputFrames; f++ {
				for p := 0; p < 3; p++ {
					window := make([][]uint16, 2*radius+1)
					for j := range window {
						start := ((f+j)*3 + p) * area
						window[j] = tc.Input[start : start+area]
					}
					plane := temporalMedian10(window)
					if p == 0 && tc.Component == "full" {
						plane = airplaneLuma10(plane, tc.Width, tc.Height, tc.Level)
					} else if p > 0 && tc.Component == "full" && tc.Level > 1 {
						for i, v := range plane {
							plane[i] = min(960, max(64, v))
						}
					}
					actual = append(actual, plane...)
				}
			}
		}
		if len(actual) != len(tc.Expected) || len(tc.PTS) != tc.OutputFrames {
			t.Fatalf("invalid %s output dimensions/PTS", tc.Component)
		}
		for f, pts := range tc.PTS {
			if pts != f {
				t.Fatalf("median timestamp anchor %d: %d", f, pts)
			}
		}
		mismatches, maximum := 0, 0
		for i, want := range tc.Expected {
			if actual[i] != want {
				mismatches++
				maximum = max(maximum, absInt(int(actual[i])-int(want)))
			}
		}
		if mismatches > 0 {
			t.Errorf("%s level %d pattern %s %dx%d CPU %s: %d mismatches max %d", tc.Component, tc.Level, tc.Pattern, tc.Width, tc.Height, tc.CPUFlags, mismatches, maximum)
		}
	}
}

type airplaneTimingObservation struct {
	Frames, Blur, Level, NativeEnd, Head, Tail, OutputCount, Width, Height int
	Speed                                                                  float64
	Filter, Failure                                                        string
	FirstY                                                                 []uint16
}

func airplaneTimingObservations(t *testing.T) []airplaneTimingObservation {
	t.Helper()
	var cases []airplaneTimingObservation
	if err := json.Unmarshal(denoiseRead(t, "testdata/lrt-airplane-timing-8.0.1.json"), &cases); err != nil || len(cases) != 100 {
		t.Fatalf("expected 100 helper/caller timing observations: %v", err)
	}
	return cases
}
func TestAirplaneRemovalMeasuredTimelineAndHold(t *testing.T) {
	for _, tc := range airplaneTimingObservations(t) {
		speed, err := validatePlaybackSpeed(tc.Speed)
		if err != nil {
			t.Fatal(err)
		}
		plan, err := planFilteredVideoFrames(tc.Frames, speed, tc.Blur, tc.Level)
		if tc.OutputCount == 0 {
			if err == nil {
				t.Fatalf("accepted empty native timeline: %+v", tc)
			}
			continue
		}
		if err != nil || plan.count() != tc.OutputCount || plan.start != tc.Head {
			t.Fatalf("timeline frames %d speed %g blur %d level %d: %+v %v want %d", tc.Frames, tc.Speed, tc.Blur, tc.Level, plan, err, tc.OutputCount)
		}
		head, tail := nativeFilterTrim(tc.Blur, tc.Level)
		if head != tc.Head || tail != tc.Tail {
			t.Fatal("native trim bounds differ")
		}
		options := RenderOptions{FPS: 24, PlaybackSpeed: speed, NativeTiming: true, RemoveAirplanes: tc.Level}
		timing, err := planRenderTiming(options, tc.Frames, tc.OutputCount, head+tail, frameRate{24, 1}, "24")
		if err != nil {
			t.Fatal(err)
		}
		want := strconv.FormatFloat(float64(float32(tc.NativeEnd-head-tail)/24), 'f', 3, 64)
		if timing.audioDuration != want {
			t.Fatalf("native audio duration %s want %s", timing.audioDuration, want)
		}
		if tc.Blur == 0 {
			filteredCount := airplaneSourceCount(tc.Frames, tc.Level)
			var resampled []uint16
			for f := 0; f < airplaneTimelineSourceCount(tc.Frames, speed, tc.Level); f++ {
				value := uint16(32 + (min(f, filteredCount-1)+airplaneRadius(tc.Level))*32)
				for len(resampled) < playbackOutputEnd(f+1, speed) {
					resampled = append(resampled, value)
				}
			}
			if !reflect.DeepEqual(resampled[plan.start:plan.end], tc.FirstY) {
				t.Fatalf("held median frame IDs frames %d speed %g level %d: %v want %v", tc.Frames, speed, tc.Level, resampled[plan.start:plan.end], tc.FirstY)
			}
		}
	}
	for _, speed := range []float64{1, 1.000000001} {
		plan, err := planFilteredVideoFrames(8, speed, 0, 3)
		if err != nil || plan.count() != 3 {
			t.Fatal("native float32 speed-one branch changed", plan, err)
		}
	}
}

func TestAirplaneRemovalLivePlaneReferences(t *testing.T) {
	executable := os.Getenv("LRTL_AIRPLANE_FFMPEG")
	if executable == "" {
		t.Skip("set LRTL_AIRPLANE_FFMPEG to repeat native filter observations")
	}
	for _, tc := range airplaneObservations(t) {
		args := []string{"-v", "error"}
		if tc.CPUFlags == "0" {
			args = append(args, "-cpuflags", "0")
		}
		args = append(args, "-f", "rawvideo", "-pixel_format", "yuv444p10le", "-video_size", fmt.Sprintf("%dx%d", tc.Width, tc.Height), "-framerate", "1", "-i", "pipe:0", "-vf", tc.Filter, "-fps_mode", "passthrough", "-f", "rawvideo", "-pix_fmt", "yuv444p10le", "pipe:1")
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
			t.Fatalf("native %s level %d: bytes %d %v %s", tc.Component, tc.Level, len(data), err, stderr.String())
		}
		for i, want := range tc.Expected {
			if got := binary.LittleEndian.Uint16(data[i*2:]); got != want {
				t.Fatalf("native %s sample %d: %d want %d", tc.Component, i, got, want)
			}
		}
	}
}

func TestAirplaneRemovalLiveTimingReferences(t *testing.T) {
	executable := os.Getenv("LRTL_AIRPLANE_FFMPEG")
	if executable == "" {
		t.Skip("set LRTL_AIRPLANE_FFMPEG to repeat the helper/caller timing graphs")
	}
	for _, tc := range airplaneTimingObservations(t) {
		var raw, stderr bytes.Buffer
		for f := 0; f < tc.Frames; f++ {
			for p := 0; p < 3; p++ {
				for i := 0; i < tc.Width*tc.Height; i++ {
					value := uint16(32 + f*32)
					if p > 0 {
						value = 512
					}
					if err := binary.Write(&raw, binary.LittleEndian, value); err != nil {
						t.Fatal(err)
					}
				}
			}
		}
		cmd := exec.Command(executable, "-v", "error", "-f", "rawvideo", "-pixel_format", "yuv444p10le", "-video_size", fmt.Sprintf("%dx%d", tc.Width, tc.Height), "-framerate", "24", "-i", "pipe:0", "-vf", tc.Filter, "-r", "24", "-f", "rawvideo", "-pix_fmt", "yuv444p10le", "pipe:1")
		cmd.Stdin, cmd.Stderr = &raw, &stderr
		out, err := cmd.Output()
		if (err != nil) != (tc.Failure != "") || len(out) != tc.OutputCount*tc.Width*tc.Height*6 {
			t.Fatalf("helper/caller graph frames %d speed %g level %d blur %d: bytes %d %v %s", tc.Frames, tc.Speed, tc.Level, tc.Blur, len(out), err, stderr.String())
		}
		for i, want := range tc.FirstY {
			if got := binary.LittleEndian.Uint16(out[i*tc.Width*tc.Height*6:]); got != want {
				t.Fatalf("held frame trace %d: %d want %d", i, got, want)
			}
		}
	}
}

func airplaneReferenceRGB(input [][]byte, width, height, level int) [][]byte {
	planes := make([][3][]uint16, len(input))
	for i, rgb := range input {
		planes[i] = outputYUV10(rgb, width, height)
	}
	output := make([][]byte, airplaneSourceCount(len(input), level))
	for f := range output {
		var filtered [3][]uint16
		for p := range filtered {
			window := make([][]uint16, 2*airplaneRadius(level)+1)
			for j := range window {
				window[j] = planes[f+j][p]
			}
			filtered[p] = temporalMedian10(window)
			if p == 0 {
				filtered[p] = airplaneLuma10(filtered[p], width, height, level)
			} else if level > 1 {
				for i, v := range filtered[p] {
					filtered[p][i] = min(960, max(64, v))
				}
			}
		}
		output[f] = bytes.Clone(input[f])
		for i := 0; i < width*height; i++ {
			dy := float64(int(filtered[0][i])-int(planes[f][0][i])) * 65535 / 1023
			cb := float64(int(filtered[1][i])-int(planes[f][1][i])) * 65535 / 1023
			cr := float64(int(filtered[2][i])-int(planes[f][2][i])) * 65535 / 1023
			for c, change := range []float64{dy + 1.5748*cr, dy - (.0722*1.8556/.7152)*cb - (.2126*1.5748/.7152)*cr, dy + 1.8556*cb} {
				at := i*6 + c*2
				v := int(binary.LittleEndian.Uint16(input[f][at:])) + int(math.Round(change))
				binary.LittleEndian.PutUint16(output[f][at:], uint16(min(65535, max(0, v))))
			}
		}
	}
	return output
}

func TestAirplaneRemovalStreamingOwnership(t *testing.T) {
	for _, count := range []int{5, 6, 9, 15} {
		input := make([][]byte, count)
		for f := range input {
			input[f] = make([]byte, 32*16*6)
			for i := 0; i < len(input[f])/2; i++ {
				binary.LittleEndian.PutUint16(input[f][i*2:], uint16(10000+(f*131+i*17)%40000))
			}
		}
		for level := 1; level <= 3; level++ {
			want := airplaneReferenceRGB(input, 32, 16, level)
			calls := make([]int, count)
			a := newAirplaneRemover(32, 16, count, level, func(i int) ([]byte, error) { calls[i]++; return input[i], nil })
			for f := range want {
				got, err := a.frame(f)
				if err != nil || !bytes.Equal(got, want[f]) {
					t.Fatalf("stream count %d level %d frame %d: %v", count, level, f, err)
				}
				if len(a.cache) > 2*airplaneRadius(level)+1 {
					t.Fatal("unbounded airplane cache")
				}
				clear(got)
			}
			for _, n := range calls {
				if n != 1 {
					t.Fatal("source reread or skipped")
				}
			}
			if _, err := a.frame(len(want)); err == nil {
				t.Fatal("accepted exhausted median timeline")
			}
		}
	}
	a := newAirplaneRemover(1, 1, 3, 1, func(int) ([]byte, error) { return nil, fmt.Errorf("synthetic future error") })
	if _, err := a.frame(1); err == nil {
		t.Fatal("accepted out-of-order median frame")
	}
	if _, err := a.frame(0); err == nil || !strings.Contains(err.Error(), "synthetic future error") {
		t.Fatal("discarded lookahead error")
	}
	a = newAirplaneRemover(1, 1, 3, 1, func(int) ([]byte, error) { return nil, nil })
	if _, err := a.frame(0); err == nil {
		t.Fatal("accepted inconsistent median dimensions")
	}
}

func TestAirplaneRemovalRenderLayeringAndSubtitles(t *testing.T) {
	dir := outputSharpenFixture(t, 12)
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
	for level := 1; level <= 3; level++ {
		for _, tc := range []struct {
			speed       float64
			blur, noise int
		}{{1, 0, 0}, {.5, 0, 0}, {1.1, 2, 2}, {2, 2, 0}} {
			speed, err := validatePlaybackSpeed(tc.speed)
			if err != nil {
				t.Fatal(err)
			}
			args := []string{"render", dir, "--output", filepath.Join(t.TempDir(), "air.mp4"), "--ffmpeg", executable, "--fps", "2", "--remove-airplanes", strconv.Itoa(level), "--speed", strconv.FormatFloat(tc.speed, 'f', -1, 64), "--motion-blur", strconv.Itoa(tc.blur), "--denoise", strconv.Itoa(tc.noise), "--sharpen", "--cas", "--watermark", "TEST", "--watermark-position", "top-left", "--fade-out", "1"}
			if result := markerCommand(t, args...); !strings.Contains(result, fmt.Sprintf("airplane removal level %d", level)) {
				t.Fatal("missing light-trail summary")
			}
			clean := input
			if tc.noise > 0 {
				clean = denoiseReferenceRGB(input, 64, 32, tc.noise)
			}
			filtered := airplaneReferenceRGB(clean, 64, 32, level)
			var resampled [][]byte
			var sourceIDs []int
			for f := 0; f < airplaneTimelineSourceCount(len(input), speed, level); f++ {
				for len(resampled) < playbackOutputEnd(f+1, speed) {
					resampled = append(resampled, filtered[min(f, len(filtered)-1)])
					sourceIDs = append(sourceIDs, f)
				}
			}
			plan, err := planFilteredVideoFrames(len(input), speed, tc.blur, level)
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
			var want []byte
			for i := plan.start; i < plan.end; i++ {
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
				img, err := imageFromRGB48(rgb, 64, 32)
				if err != nil {
					t.Fatal(err)
				}
				rgb, err = imageRGB48(applyWatermarkOverlay(img, "TEST", "top-left", face))
				if err != nil {
					t.Fatal(err)
				}
				fadeRGB48(rgb, fade.gain(i-plan.start, frameRate{2, 1}))
				want = append(want, rgb...)
			}
			if !bytes.Equal(want, denoiseRead(t, capture)) {
				t.Fatalf("filter order/held tail level %d speed %g blur %d", level, speed, tc.blur)
			}
			srt := filepath.Join(t.TempDir(), "air.srt")
			markerCommand(t, "subtitles", dir, "--output", srt, "--fps", "2", "--speed", strconv.FormatFloat(tc.speed, 'f', -1, 64), "--motion-blur", strconv.Itoa(tc.blur), "--remove-airplanes", strconv.Itoa(level), "--timestamp-format", "05")
			text := string(denoiseRead(t, srt))
			blocks := strings.Split(strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n")), "\n\n")
			if len(blocks) != plan.count() {
				t.Fatal("subtitle/video frame count differs")
			}
			for i, block := range blocks {
				lines := strings.Split(block, "\n")
				if len(lines) != 3 || lines[2] != fmt.Sprintf("%02d", sourceIDs[plan.start+i]) {
					t.Fatalf("subtitle PTS/source anchor mismatch: %s", block)
				}
			}
		}
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("light-trail rendering changed sources")
	}
}

func TestAirplaneRemovalPresetBatchAndValidation(t *testing.T) {
	dir := outputSharpenFixture(t, 8)
	before := batchFileSnapshot(t, dir)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(t.TempDir(), "capture")
	t.Setenv("LRTL_TEST_FFMPEG_CAPTURE", capture)
	for level := 1; level <= 3; level++ {
		preset, err := decodeLRTRenderPreset(renderPresetObservations(t)[0].Preset)
		if err != nil {
			t.Fatal(err)
		}
		preset.RemoveAirplanes, preset.RemoveAirplanesLevel, preset.OutputResV = true, level, 32
		path := savePresetJSON(t, preset)
		presetBefore := denoiseRead(t, path)
		markerCommand(t, "render", dir, "--output", filepath.Join(t.TempDir(), "direct.mp4"), "--ffmpeg", executable, "--remove-airplanes", strconv.Itoa(level))
		want := denoiseRead(t, capture)
		markerCommand(t, "render", dir, "--output", filepath.Join(t.TempDir(), "preset.mp4"), "--ffmpeg", executable, "--lrt-render-preset", path)
		if !bytes.Equal(want, denoiseRead(t, capture)) {
			t.Fatal("saved light-trail level differs from CLI")
		}
		markerCommand(t, "batch-render", dir, "--output", t.TempDir(), "--", "--ffmpeg", executable, "--lrt-render-preset", path)
		if !bytes.Equal(want, denoiseRead(t, capture)) {
			t.Fatal("batch lost saved light-trail filter")
		}
		markerCommand(t, "render", dir, "--output", filepath.Join(t.TempDir(), "off.mp4"), "--ffmpeg", executable, "--lrt-render-preset", path, "--remove-airplanes", "0")
		if len(denoiseRead(t, capture)) != 8*64*32*6 {
			t.Fatal("explicit zero did not restore the original frame count")
		}
		assertPreviewEVFileBytes(t, path, presetBefore)
	}
	preset, err := decodeLRTRenderPreset([]byte(`{"encoder":"h264","frameRate":"24","outputResV":1080,"outputQuality":1,"removeAirplanes":true}`))
	if err != nil || preset.RemoveAirplanesLevel != 1 {
		t.Fatal("native default removeAirplanesLevel differs", err)
	}
	for _, level := range []int{-1, 0, 4} {
		preset.RemoveAirplanesLevel = level
		if _, err := applyLRTRenderPreset(RenderOptions{}, preset, "", nil); err == nil {
			t.Fatal("accepted invalid active native light-trail level")
		}
		options, err := applyLRTRenderPreset(RenderOptions{}, preset, "", map[string]bool{"remove-airplanes": true})
		if err != nil || options.RemoveAirplanes != 0 {
			t.Fatal("explicit disable lost priority")
		}
		preset.RemoveAirplanes = false
		if _, err := applyLRTRenderPreset(RenderOptions{}, preset, "", nil); err != nil {
			t.Fatal("validated an inactive level")
		}
		preset.RemoveAirplanes = true
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("preset light-trail render changed sources")
	}
	for _, command := range []string{"render", "subtitles"} {
		for _, level := range []string{"-1", "4", "NaN"} {
			ext := ".avi"
			if command == "subtitles" {
				ext = ".srt"
			}
			output := filepath.Join(t.TempDir(), "keep"+ext)
			if err := os.WriteFile(output, []byte("original output"), 0600); err != nil {
				t.Fatal(err)
			}
			var out, stderr bytes.Buffer
			if Main([]string{command, dir, "--output", output, "--overwrite", "--remove-airplanes", level}, &out, &stderr) == 0 {
				t.Fatal("accepted invalid light-trail level")
			}
			assertPreviewEVFileBytes(t, output, []byte("original output"))
		}
	}
	short := outputSharpenFixture(t, 3)
	for _, command := range []string{"render", "subtitles"} {
		ext := ".avi"
		if command == "subtitles" {
			ext = ".srt"
		}
		output := filepath.Join(t.TempDir(), "keep"+ext)
		if err := os.WriteFile(output, []byte("original output"), 0600); err != nil {
			t.Fatal(err)
		}
		var out, stderr bytes.Buffer
		if Main([]string{command, short, "--output", output, "--overwrite", "--remove-airplanes", "3"}, &out, &stderr) == 0 {
			t.Fatal("accepted an empty filtered timeline")
		}
		assertPreviewEVFileBytes(t, output, []byte("original output"))
	}
	if err := os.WriteFile(filepath.Join(dir, "frame07.png"), []byte("broken future image"), 0600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "keep.avi")
	if err := os.WriteFile(output, []byte("original output"), 0600); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	if Main([]string{"render", dir, "--output", output, "--overwrite", "--remove-airplanes", "1"}, &out, &stderr) == 0 {
		t.Fatal("ignored corrupt window tail")
	}
	assertPreviewEVFileBytes(t, output, []byte("original output"))
	files, err := os.ReadDir(filepath.Dir(output))
	if err != nil || len(files) != 1 {
		t.Fatal("failed light-trail render left temporary output", err)
	}
	if err := validateAirplaneMemory(1920, 1080, 20, 3, 4, 2); err != nil {
		t.Fatal(err)
	}
	if err := validateAirplaneMemory(7680, 4320, 20, 3, 4, 2); err == nil {
		t.Fatal("unbounded combined temporal cache")
	}
	if err := validateAirplaneMemory(7680, 4320, 20, 0, 0, 0); err != nil {
		t.Fatal("disabled filter changed dimensions")
	}
}

func TestAirplaneRemovalJPEGAVIAndStillExports(t *testing.T) {
	fixture := outputSharpenFixture(t, 8)
	dir := t.TempDir()
	var input [][]byte
	var original [][]byte
	for i := 0; i < 8; i++ {
		img, _, err := image.Decode(bytes.NewReader(denoiseRead(t, filepath.Join(fixture, fmt.Sprintf("frame%02d.png", i)))))
		if err != nil {
			t.Fatal(err)
		}
		var encoded bytes.Buffer
		if err := jpeg.Encode(&encoded, img, &jpeg.Options{Quality: 100}); err != nil {
			t.Fatal(err)
		}
		original = append(original, bytes.Clone(encoded.Bytes()))
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("frame%02d.jpg", i)), encoded.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		decoded, err := jpeg.Decode(bytes.NewReader(encoded.Bytes()))
		if err != nil {
			t.Fatal(err)
		}
		rgb, err := imageRGB48(decoded)
		if err != nil {
			t.Fatal(err)
		}
		input = append(input, rgb)
	}
	before := batchFileSnapshot(t, dir)
	for level := 0; level <= 3; level++ {
		output := filepath.Join(t.TempDir(), "air.avi")
		markerCommand(t, "render", dir, "--output", output, "--quality", "100", "--remove-airplanes", strconv.Itoa(level))
		var want [][]byte
		if level == 0 {
			want = original
		} else {
			filtered := airplaneReferenceRGB(input, 64, 32, level)
			plan, err := planFilteredVideoFrames(len(input), 1, 0, level)
			if err != nil {
				t.Fatal(err)
			}
			for _, rgb := range filtered[plan.start:plan.end] {
				img, err := imageFromRGB48(rgb, 64, 32)
				if err != nil {
					t.Fatal(err)
				}
				var encoded bytes.Buffer
				if err := jpeg.Encode(&encoded, img, &jpeg.Options{Quality: 100}); err != nil {
					t.Fatal(err)
				}
				want = append(want, bytes.Clone(encoded.Bytes()))
			}
		}
		data := denoiseRead(t, output)
		position := bytes.Index(data, []byte("movi")) + 4
		if position < 4 {
			t.Fatal("AVI has no movie list")
		}
		for i, expected := range want {
			if position+8 > len(data) || string(data[position:position+4]) != "00dc" {
				t.Fatal("missing AVI JPEG frame")
			}
			size := int(binary.LittleEndian.Uint32(data[position+4:]))
			position += 8
			if size < 0 || position+size > len(data) || !bytes.Equal(data[position:position+size], expected) {
				t.Fatalf("AVI level %d frame %d", level, i)
			}
			position += size + size%2
		}
		if position+4 <= len(data) && string(data[position:position+4]) == "00dc" {
			t.Fatal("extra AVI frame after light-trail trim")
		}
	}
	frames, err := DiscoverSequence(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"tiff", "preview"} {
		var control map[string][]byte
		for _, level := range []int{0, 3} {
			output := filepath.Join(t.TempDir(), "export")
			options := RenderOptions{Quality: 100, RemoveAirplanes: level}
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
				t.Fatal("video-only filter changed still exports")
			}
		}
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("light-trail filter changed source JPEGs")
	}
}

func TestAirplaneRemovalActualVideoPixels(t *testing.T) {
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
	dir := outputSharpenFixture(t, 12)
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
	for level := 1; level <= 3; level++ {
		filtered := airplaneReferenceRGB(input, 64, 32, level)
		for _, speed := range []float64{1, .5, 2} {
			var resampled [][]byte
			for f := 0; f < airplaneTimelineSourceCount(len(input), speed, level); f++ {
				for len(resampled) < playbackOutputEnd(f+1, speed) {
					resampled = append(resampled, filtered[min(f, len(filtered)-1)])
				}
			}
			plan, err := planFilteredVideoFrames(len(input), speed, 0, level)
			if err != nil {
				t.Fatal(err)
			}
			var expected []int
			for _, rgb := range resampled[plan.start:plan.end] {
				for i := 0; i < len(rgb); i += 6 {
					r, g, b := float64(binary.LittleEndian.Uint16(rgb[i:])), float64(binary.LittleEndian.Uint16(rgb[i+2:])), float64(binary.LittleEndian.Uint16(rgb[i+4:]))
					expected = append(expected, int(math.Round((.2126*r+.7152*g+.0722*b)*1023/65535)))
				}
			}
			for _, codec := range []string{"h264", "h265"} {
				t.Run(fmt.Sprintf("level%d/speed%g/%s", level, speed, codec), func(t *testing.T) {
					encoder, ext := "libx264", ".mp4"
					if codec == "h265" {
						encoder, ext = "libx265", ".mov"
					}
					if err := requireFFmpegPixelFormat(ffmpeg, encoder, "yuv444p10le"); err != nil {
						t.Skipf("encoder unavailable: %v", err)
					}
					output := filepath.Join(t.TempDir(), "air"+ext)
					markerCommand(t, "render", dir, "--output", output, "--ffmpeg", ffmpeg, "--codec", codec, "--subsampling", "444", "--video-range", "full", "--fps", "2", "--crf", "0", "--preset", "ultrafast", "--remove-airplanes", strconv.Itoa(level), "--speed", strconv.FormatFloat(speed, 'f', -1, 64))
					metadata, err := exec.Command(ffprobe, "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=pix_fmt,nb_frames,duration,avg_frame_rate", "-of", "json", output).CombinedOutput()
					if err != nil {
						t.Fatalf("probe %s %v", metadata, err)
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
					stream := probe.Streams[0]
					duration, err := strconv.ParseFloat(stream.Duration, 64)
					if err != nil || stream.Pixel != "yuv444p10le" || stream.Frames != strconv.Itoa(plan.count()) || stream.FPS != "2/1" || math.Abs(duration-float64(plan.count())/2) > .001 {
						t.Fatalf("wrong filtered video timeline: %s", metadata)
					}
					luma, err := exec.Command(ffmpeg, "-v", "error", "-i", output, "-vf", "extractplanes=y", "-fps_mode", "passthrough", "-f", "rawvideo", "pipe:1").Output()
					if err != nil || len(luma) != len(expected)*2 {
						t.Fatalf("decoded count bytes %d %v", len(luma), err)
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
					t.Logf("%d frames, luma max %d mean %.3f", plan.count(), maximum, mean)
					if maximum > limit || mean > meanLimit {
						t.Fatal("encoded light-trail pixels differ from filtered RGB reference")
					}
				})
			}
		}
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("actual light-trail rendering changed sources")
	}
}

func TestAirplaneRemovalActualPresetAudioTail(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("FFmpeg unavailable")
	}
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe unavailable")
	}
	if err := requireFFmpegPixelFormat(ffmpeg, "libx264", "yuv420p"); err != nil {
		t.Skipf("encoder unavailable: %v", err)
	}
	dir := videoFadeFixture(t, 12)
	audio := filepath.Join(t.TempDir(), "tone.wav")
	if data, err := exec.Command(ffmpeg, "-v", "error", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000", "-t", "6", "-c:a", "pcm_s16le", audio).CombinedOutput(); err != nil {
		t.Fatalf("test tone %s %v", data, err)
	}
	preset, err := decodeLRTRenderPreset(renderPresetObservations(t)[0].Preset)
	if err != nil {
		t.Fatal(err)
	}
	preset.OutputResV, preset.FrameRate, preset.RemoveAirplanes, preset.RemoveAirplanesLevel = 32, "2", true, 3
	preset.FadeOut, preset.FadeTime, preset.Audio, preset.AudioTrack = true, 1, true, audio
	path := savePresetJSON(t, preset)
	output := filepath.Join(t.TempDir(), "native.mp4")
	markerCommand(t, "render", dir, "--output", output, "--lrt-render-preset", path, "--ffmpeg", ffmpeg, "--video-range", "auto", "--crf", "0", "--preset", "ultrafast")
	metadata, err := exec.Command(ffprobe, "-v", "error", "-show_entries", "stream=codec_type,duration,nb_frames", "-of", "json", output).CombinedOutput()
	if err != nil {
		t.Fatalf("probe %s %v", metadata, err)
	}
	var probe struct {
		Streams []struct {
			Type     string `json:"codec_type"`
			Duration string `json:"duration"`
			Frames   string `json:"nb_frames"`
		}
	}
	if err := json.Unmarshal(metadata, &probe); err != nil || len(probe.Streams) != 2 {
		t.Fatalf("streams %s %v", metadata, err)
	}
	for _, stream := range probe.Streams {
		duration, err := strconv.ParseFloat(stream.Duration, 64)
		want := 5.0
		if stream.Type == "video" {
			want = 3.5
			if stream.Frames != "7" {
				t.Fatalf("filtered video frames %s", metadata)
			}
		}
		if err != nil || math.Abs(duration-want) > .002 {
			t.Fatalf("native estimated audio time %s", metadata)
		}
	}
	pcm, err := exec.Command(ffmpeg, "-v", "error", "-i", output, "-vn", "-ac", "1", "-ar", "48000", "-f", "f32le", "pipe:1").Output()
	if err != nil {
		t.Fatal(err)
	}
	rms := func(start, end float64) float64 {
		first, last := int(start*48000), int(end*48000)
		if last*4 > len(pcm) {
			t.Fatal("audio tail was cut at the video end")
		}
		sum := 0.0
		for i := first; i < last; i++ {
			v := float64(math.Float32frombits(binary.LittleEndian.Uint32(pcm[i*4:])))
			sum += v * v
		}
		return math.Sqrt(sum / float64(last-first))
	}
	base := rms(3, 3.1)
	if ratio := rms(3.8, 3.9) / base; math.Abs(ratio-1) > .05 {
		t.Fatalf("audio beyond video end %g", ratio)
	}
	if ratio := rms(4.8, 4.9) / base; math.Abs(ratio-.15) > .04 {
		t.Fatalf("native estimated fade timing %g", ratio)
	}
	rgb, err := exec.Command(ffmpeg, "-v", "error", "-i", output, "-an", "-fps_mode", "passthrough", "-pix_fmt", "rgb24", "-f", "rawvideo", "pipe:1").Output()
	if err != nil || len(rgb) != 7*64*32*3 {
		t.Fatalf("decoded preset video bytes %d %v", len(rgb), err)
	}
	if !bytes.Equal(rgb[:64*32*3], rgb[6*64*32*3:]) {
		t.Fatal("video faded before the native estimated fade start")
	}
}
