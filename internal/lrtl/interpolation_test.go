package lrtl

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"math"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

type interpolationObservation struct {
	Depth                                     int
	ScaleFlags                                string
	FPS                                       string
	Speed                                     float64
	Mode, Width, Height, Frames, OutputFrames int
	CPUFlags, Pattern, Filter, Hash           string
	FirstY                                    []byte
}

func interpolation10Observations(t *testing.T) []interpolationObservation {
	t.Helper()
	var rows []interpolationObservation
	if err := json.Unmarshal(denoiseRead(t, "testdata/lrt-interpolation-10bit-8.0.1.json"), &rows); err != nil || len(rows) != 576 {
		t.Fatalf("expected 576 10-bit references: %v", err)
	}
	return rows
}

func interpolationNativeInput(tc interpolationObservation) ([]byte, [][3][]uint16) {
	input := interpolationFixture(tc.Width, tc.Height, tc.Frames, tc.Pattern)
	var raw []byte
	planes := make([][3][]uint16, tc.Frames)
	for f := range input {
		for p := range input[f] {
			planes[f][p] = make([]uint16, tc.Width*tc.Height)
			for i, v := range input[f][p] {
				if tc.Depth == 10 {
					value := uint16(v)*4 + uint16((i%tc.Width+i/tc.Width+p+f)%4)
					planes[f][p][i] = value
					raw = binary.LittleEndian.AppendUint16(raw, value)
				} else {
					raw = append(raw, v)
				}
			}
		}
	}
	return raw, planes
}

func TestInterpolationMeasured10Bit(t *testing.T) {
	for _, tc := range interpolation10Observations(t) {
		_, input := interpolationNativeInput(tc)
		speed := float64(float32(tc.Speed))
		rate, _ := parseFrameRate(tc.FPS)
		var out []byte
		count := 0
		if speed < 1 {
			clock, err := newInterpolationClock(rate, tc.FPS, speed)
			if err != nil {
				t.Fatal(err)
			}
			eight := make([][3][]byte, len(input))
			for f := range input {
				for p := range input[f] {
					eight[f][p] = quantizeInterpolationPlane(input[f][p], tc.Width)
				}
			}
			scenes := make([]bool, len(input))
			var previous float64
			for f := 0; f+1 < len(input); f++ {
				scenes[f], previous = interpolationSceneChange(eight[f][0], eight[f+1][0], previous)
			}
			j := 0
			count = clock.count(len(input))
			for target := 0; target < count; target++ {
				for j < clock.last(len(input)) && clock.outputPTS(j+1) <= target {
					j++
				}
				left, alpha := clock.sample(j, len(input))
				for p := 0; p < 3; p++ {
					for _, v := range blendInterpolationPlane(eight[left][p], eight[left+1][p], alpha, scenes[left]) {
						out = binary.LittleEndian.AppendUint16(out, uint16(v)*4)
					}
				}
			}
		} else {
			window := max(1, int(speed+.5))
			for f := range input {
				for target := playbackOutputEnd(f, speed); target < playbackOutputEnd(f+1, speed); target++ {
					count++
					for p := 0; p < 3; p++ {
						for i := range input[f][p] {
							sum := window / 2
							for j := 0; j < window; j++ {
								sum += int(input[max(0, f-window+1+j)][p][i])
							}
							out = binary.LittleEndian.AppendUint16(out, uint16(sum/window))
						}
					}
				}
			}
		}
		hash := sha256.Sum256(out)
		if count != tc.OutputFrames || hex.EncodeToString(hash[:]) != tc.Hash {
			t.Errorf("10bit %s speed%g %dx%d frames%d %s count%d want%d hash differs", tc.FPS, tc.Speed, tc.Width, tc.Height, tc.Frames, tc.Pattern, count, tc.OutputFrames)
		}
	}
}

func TestInterpolationLiveReferences(t *testing.T) {
	ffmpeg := os.Getenv("LRTL_INTERPOLATION_FFMPEG")
	if ffmpeg == "" {
		t.Skip("set LRTL_INTERPOLATION_FFMPEG for native graph measurements")
	}
	rows := append(interpolationObservations(t), interpolation10Observations(t)...)
	for _, tc := range rows {
		raw, _ := interpolationNativeInput(tc)
		format := "yuv444p"
		if tc.Depth == 10 {
			format = "yuv444p10le"
		}
		rate, err := parseFrameRate(tc.FPS)
		if err != nil {
			t.Fatal(err)
		}
		args := []string{"-v", "error"}
		if tc.CPUFlags == "0" {
			args = append(args, "-cpuflags", "0")
		}
		if tc.ScaleFlags != "" {
			args = append(args, "-sws_flags", tc.ScaleFlags)
		}
		args = append(args, "-f", "rawvideo", "-pixel_format", format, "-video_size", fmt.Sprintf("%dx%d", tc.Width, tc.Height), "-framerate", rate.String(), "-i", "pipe:0", "-vf", tc.Filter, "-fps_mode", "passthrough", "-f", "rawvideo", "-pix_fmt", format, "pipe:1")
		cmd := exec.Command(ffmpeg, args...)
		var stderr bytes.Buffer
		cmd.Stdin = bytes.NewReader(raw)
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("native interpolation %v %s", err, stderr.String())
		}
		hash := sha256.Sum256(out)
		if hex.EncodeToString(hash[:]) != tc.Hash {
			t.Fatalf("native interpolation changed: depth%d %s speed%g %dx%d n%d %s", tc.Depth, tc.FPS, tc.Speed, tc.Width, tc.Height, tc.Frames, tc.Pattern)
		}
	}
}

func interpolationReferenceRGB(input [][]byte, width, height, eof, mode int, speed float64, label string) ([][]byte, []int) {
	rate, _ := parseFrameRate(label)
	planes := make([][3][]uint16, len(input))
	eight := make([][3][]byte, len(input))
	for f, rgb := range input {
		planes[f] = outputYUV10(rgb, width, height)
		for p := range planes[f] {
			eight[f][p] = quantizeInterpolationPlane(planes[f][p], width)
		}
	}
	var sources, alphas []int
	if speed < 1 && mode > 0 {
		clock, _ := newInterpolationClock(rate, label, speed)
		j := 0
		for target := 0; target < clock.count(eof); target++ {
			for j < clock.last(len(input)) && clock.outputPTS(j+1) <= target {
				j++
			}
			left, alpha := clock.sample(j, len(input))
			sources = append(sources, left)
			alphas = append(alphas, alpha)
		}
	} else {
		for f := 0; f < eof; f++ {
			for target := playbackOutputEnd(f, speed); target < playbackOutputEnd(f+1, speed); target++ {
				sources = append(sources, f)
				alphas = append(alphas, 0)
			}
		}
	}
	scenes := make([]bool, len(input))
	var previous float64
	for f := 0; f+1 < len(input); f++ {
		scenes[f], previous = interpolationSceneChange(eight[f][0], eight[f+1][0], previous)
	}
	var output [][]byte
	for target, source := range sources {
		anchor := min(source, len(input)-1)
		filtered := planes[anchor]
		if mode > 0 {
			for p := range filtered {
				filtered[p] = make([]uint16, width*height)
				if speed < 1 {
					blend := blendInterpolationPlane(eight[anchor][p], eight[anchor+1][p], alphas[target], scenes[anchor])
					for i, v := range blend {
						filtered[p][i] = uint16(v) * 4
					}
				} else {
					window := max(1, int(speed+.5))
					for i := range filtered[p] {
						sum := window / 2
						for j := 0; j < window; j++ {
							sum += int(planes[max(0, anchor-window+1+j)][p][i])
						}
						filtered[p][i] = uint16(sum / window)
					}
				}
			}
		}
		rgb := bytes.Clone(input[anchor])
		for i := 0; i < width*height; i++ {
			dy := float64(int(filtered[0][i])-int(planes[anchor][0][i])) * (65535.0 / 1023)
			cb := float64(int(filtered[1][i])-int(planes[anchor][1][i])) * (65535.0 / 1023)
			cr := float64(int(filtered[2][i])-int(planes[anchor][2][i])) * (65535.0 / 1023)
			for c, change := range []float64{dy + 1.5748*cr, dy - (.0722*1.8556/.7152)*cb - (.2126*1.5748/.7152)*cr, dy + 1.8556*cb} {
				at := i*6 + c*2
				v := int(binary.LittleEndian.Uint16(input[anchor][at:])) + int(math.Round(change))
				binary.LittleEndian.PutUint16(rgb[at:], uint16(min(65535, max(0, v))))
			}
		}
		output = append(output, rgb)
	}
	return output, sources
}

func TestInterpolationStreamOwnership(t *testing.T) {
	for _, count := range []int{3, 4, 8, 17} {
		for _, speed := range []float64{.0625, .3, .5, .7, .9, 1, 1.1, 1.5, 2, 2.3, 4, 16} {
			speed = float64(float32(speed))
			if playbackOutputEnd(count, speed) < 1 {
				continue
			}
			input := outputDeflickerSynthetic(count, 7, 5)
			before := make([][]byte, count)
			for f := range input {
				before[f] = bytes.Clone(input[f])
			}
			want, ids := interpolationReferenceRGB(input, 7, 5, count, 1, speed, "29.97")
			calls := make([]int, count)
			stream, err := newPlaybackFrameStream(7, 5, count, 0, 1, speed, frameRate{30000, 1001}, "29.97", func(i int) ([]byte, error) { calls[i]++; return input[i], nil })
			if err != nil {
				t.Fatal(err)
			}
			if stream.count != len(want) {
				t.Fatal("wrong stream count")
			}
			for f := range want {
				got, id, err := stream.frame(f)
				if err != nil || id != ids[f] || !bytes.Equal(got, want[f]) {
					t.Fatalf("count%d speed%g frame%d source%d want%d: %v", count, speed, f, id, ids[f], err)
				}
				clear(got)
				if len(stream.cache) > max(3, stream.window) {
					t.Fatal("unbounded interpolation cache")
				}
			}
			if err := stream.finish(); err != nil {
				t.Fatal(err)
			}
			for _, n := range calls {
				if n != 1 {
					t.Fatal("source reread or skipped")
				}
			}
			if !reflect.DeepEqual(input, before) {
				t.Fatal("source RGB changed")
			}
			if _, _, err := stream.frame(len(want)); err == nil {
				t.Fatal("accepted exhausted stream")
			}
		}
	}
	for _, mode := range []int{-1, 3} {
		if err := validateInterpolation(mode, 1); err == nil {
			t.Fatal("accepted invalid mode")
		}
	}
	if err := validateInterpolation(2, .5); err == nil {
		t.Fatal("accepted unimplemented slow MCI")
	}
	if err := validateInterpolation(2, 2); err != nil {
		t.Fatal(err)
	}
	for _, later := range []bool{false, true} {
		stream, err := newPlaybackFrameStream(1, 1, 3, 0, 1, .5, frameRate{24, 1}, "24", func(i int) ([]byte, error) {
			if !later && i == 2 {
				return nil, fmt.Errorf("future failed")
			}
			return nil, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := stream.frame(1); err == nil {
			t.Fatal("accepted nonsequential frame")
		}
		if _, _, err := stream.frame(0); err == nil {
			t.Fatal("accepted bad dimensions or future error")
		}
	}
	for _, speed := range []float64{.5, 16} {
		if err := validateInterpolationMemory(64, 32, 64, 32, 20, 1, speed, RenderOptions{OutputDenoise: 4, RemoveAirplanes: 3, OutputDeflickerLuma: true, MotionBlurFrames: 2}); err != nil {
			t.Fatal(err)
		}
		if err := validateInterpolationMemory(32768, 32768, 32768, 32768, 20, 1, speed, RenderOptions{}); err == nil {
			t.Fatal("accepted excessive interpolation buffers")
		}
	}
}

func TestInterpolationCLIPresetBatchSubtitlesAndFilters(t *testing.T) {
	dir := outputSharpenFixture(t, 17)
	before := batchFileSnapshot(t, dir)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(t.TempDir(), "capture")
	t.Setenv("LRTL_TEST_FFMPEG_CAPTURE", capture)
	run := func(extra ...string) []byte {
		args := []string{"render", dir, "--output", filepath.Join(t.TempDir(), "out.mp4"), "--ffmpeg", executable}
		markerCommand(t, append(args, extra...)...)
		return denoiseRead(t, capture)
	}
	raw := run()
	var input [][]byte
	for i := 0; i < len(raw); i += 64 * 32 * 6 {
		input = append(input, raw[i:i+64*32*6])
	}
	for _, tc := range []struct {
		speed            float64
		air, noise, blur int
	}{{.5, 0, 0, 0}, {.7, 0, 0, 0}, {.3, 1, 1, 2}, {1, 0, 0, 0}, {1.1, 0, 0, 0}, {1.5, 0, 0, 0}, {2.3, 1, 2, 2}, {4, 0, 0, 0}} {
		speed := float64(float32(tc.speed))
		clean := input
		if tc.noise > 0 {
			clean = denoiseReferenceRGB(clean, 64, 32, tc.noise)
		}
		if tc.air > 0 {
			clean = airplaneReferenceRGB(clean, 64, 32, tc.air)
		}
		filtered, ids := interpolationReferenceRGB(clean, 64, 32, len(input), 1, speed, "24")
		plan, err := planInterpolatedVideoFrames(len(input), speed, tc.blur, tc.air, 1, frameRate{24, 1}, "24")
		if err != nil {
			t.Fatal(err)
		}
		var want []byte
		for f := plan.start; f < plan.end; f++ {
			rgb := filtered[f]
			if tc.blur > 0 {
				rgb, err = weightedRGB48Frames([][]byte{filtered[f-1], filtered[f]}, lrtMotionBlurWeights(2))
				if err != nil {
					t.Fatal(err)
				}
			}
			want = append(want, rgb...)
		}
		flags := []string{"--interpolation", "1", "--speed", strconv.FormatFloat(tc.speed, 'f', -1, 64), "--denoise", strconv.Itoa(tc.noise), "--remove-airplanes", strconv.Itoa(tc.air), "--motion-blur", strconv.Itoa(tc.blur)}
		if got := run(flags...); !bytes.Equal(got, want) {
			t.Fatalf("interpolation layer order speed%g air%d noise%d blur%d", speed, tc.air, tc.noise, tc.blur)
		}
		preset, err := decodeLRTRenderPreset(renderPresetObservations(t)[0].Preset)
		if err != nil {
			t.Fatal(err)
		}
		preset.Interpolation, preset.Speed, preset.OutputResV = 1, tc.speed, 32
		preset.FrameRate = "24"
		preset.Denoise, preset.DenoiseLevel = tc.noise > 0, tc.noise
		preset.RemoveAirplanes, preset.RemoveAirplanesLevel = tc.air > 0, tc.air
		preset.MotionBlur, preset.MotionBlurLevel = tc.blur > 0, tc.blur
		path := savePresetJSON(t, preset)
		presetBefore := denoiseRead(t, path)
		if !bytes.Equal(run("--lrt-render-preset", path), want) {
			t.Fatal("preset interpolation differs from CLI")
		}
		markerCommand(t, "batch-render", dir, "--output", t.TempDir(), "--", "--ffmpeg", executable, "--lrt-render-preset", path)
		if !bytes.Equal(denoiseRead(t, capture), want) {
			t.Fatal("batch lost interpolation")
		}
		assertPreviewEVFileBytes(t, path, presetBefore)
		srt := filepath.Join(t.TempDir(), "match.srt")
		markerCommand(t, "subtitles", dir, "--output", srt, "--interpolation", "1", "--speed", strconv.FormatFloat(tc.speed, 'f', -1, 64), "--remove-airplanes", strconv.Itoa(tc.air), "--motion-blur", strconv.Itoa(tc.blur), "--timestamp-format", "05")
		blocks := strings.Split(strings.TrimSpace(strings.ReplaceAll(string(denoiseRead(t, srt)), "\r\n", "\n")), "\n\n")
		if len(blocks) != plan.count() {
			t.Fatal("subtitle and video counts differ")
		}
		for f, block := range blocks {
			lines := strings.Split(block, "\n")
			if len(lines) != 3 || lines[2] != fmt.Sprintf("%02d", ids[f+plan.start]) {
				t.Fatal("wrong interpolated subtitle source")
			}
		}
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("interpolation changed sources")
	}
}

func interpolationObservations(t *testing.T) []interpolationObservation {
	t.Helper()
	var rows []interpolationObservation
	if err := json.Unmarshal(denoiseRead(t, "testdata/lrt-interpolation-8.0.1.json"), &rows); err != nil || len(rows) != 1728 {
		t.Fatalf("expected 1728 interpolation references: %v", err)
	}
	return rows
}

func interpolationFixture(width, height, count int, pattern string) [][3][]byte {
	frames := make([][3][]byte, count)
	for f := range frames {
		for p := range frames[f] {
			frames[f][p] = make([]byte, width*height)
			for y := 0; y < height; y++ {
				for x := 0; x < width; x++ {
					v := 32 + p*70 + (f*7)%60
					switch pattern {
					case "scene":
						if f == count/2 {
							v = 220
						}
					case "constant":
						v = 32 + p*70
					case "moving":
						if x == (f*3)%width && y == height/2 {
							v = 255
						}
					}
					frames[f][p][y*width+x] = byte(v)
				}
			}
		}
	}
	return frames
}

func TestInterpolationMeasuredPlanesAndTiming(t *testing.T) {
	for _, tc := range interpolationObservations(t) {
		input := interpolationFixture(tc.Width, tc.Height, tc.Frames, tc.Pattern)
		rate, err := parseFrameRate(tc.FPS)
		if err != nil {
			t.Fatal(err)
		}
		speed := float64(float32(tc.Speed))
		var output [][3][]byte
		if speed < 1 {
			clock, err := newInterpolationClock(rate, tc.FPS, speed)
			if err != nil {
				t.Fatal(err)
			}
			count := clock.count(len(input))
			var previous float64
			scenes := make([]bool, max(0, len(input)-2))
			for left := range scenes {
				scenes[left], previous = interpolationSceneChange(input[left][0], input[left+1][0], previous)
			}
			j := 0
			for target := 0; target < count; target++ {
				for j < clock.last(len(input)) && clock.outputPTS(j+1) <= target {
					j++
				}
				left, alpha := clock.sample(j, len(input))
				var planes [3][]byte
				for p := range planes {
					planes[p] = blendInterpolationPlane(input[left][p], input[left+1][p], alpha, scenes[left])
				}
				output = append(output, planes)
			}
		} else {
			window := max(1, int(speed+.5))
			for f := range input {
				var planes [3][]byte
				for p := range planes {
					var past [][]byte
					for j := 0; j < window; j++ {
						past = append(past, input[max(0, f-window+1+j)][p])
					}
					planes[p] = averageInterpolationPlane(past)
				}
				for target := playbackOutputEnd(f, speed); target < playbackOutputEnd(f+1, speed); target++ {
					output = append(output, planes)
				}
			}
		}
		hash := sha256.New()
		var ids []byte
		for _, planes := range output {
			ids = append(ids, planes[0][0])
			for _, p := range planes {
				hash.Write(p)
			}
		}
		if len(output) != tc.OutputFrames || hex.EncodeToString(hash.Sum(nil)) != tc.Hash {
			t.Errorf("%s speed%g %dx%d cpu%s frames%d %s: count%d want%d ids%v want%v", tc.FPS, tc.Speed, tc.Width, tc.Height, tc.CPUFlags, tc.Frames, tc.Pattern, len(output), tc.OutputFrames, ids, tc.FirstY)
		}
	}
}

func TestInterpolationClockValidation(t *testing.T) {
	for _, label := range []string{"NaN", "invalid", "0"} {
		if _, err := newInterpolationClock(frameRate{24, 1}, label, .5); err == nil {
			t.Fatal("accepted invalid intermediate fps", label)
		}
	}
	if _, err := newInterpolationClock(frameRate{}, "24", .5); err == nil {
		t.Fatal("accepted invalid source rate")
	}
	if _, err := newInterpolationClock(frameRate{24, 1}, "24", 0); err == nil {
		t.Fatal("accepted invalid speed")
	}
	clock, err := newInterpolationClock(frameRate{30000, 1001}, "30000/1001", .5)
	if err != nil || clock.ratio.Sign() <= 0 {
		t.Fatal(fmt.Sprint("fractional rate: ", err))
	}
}

func TestInterpolationHelperGraphs(t *testing.T) {
	var helpers []struct {
		FPS    string
		Speed  float64
		Mode   int
		Filter string
	}
	if err := json.Unmarshal(denoiseRead(t, "testdata/lrt-interpolation-filter-7.5.5.json"), &helpers); err != nil || len(helpers) != 108 {
		t.Fatalf("expected 108 installed helper graphs: %v", err)
	}
	graphs := map[string]string{}
	key := func(fps string, speed float64, mode int) string { return fmt.Sprintf("%s/%g/%d", fps, speed, mode) }
	for _, helper := range helpers {
		graphs[key(helper.FPS, helper.Speed, helper.Mode)] = strings.TrimPrefix(helper.Filter, ",")
	}
	for _, row := range append(interpolationObservations(t), interpolation10Observations(t)...) {
		if graphs[key(row.FPS, row.Speed, row.Mode)] != row.Filter {
			t.Fatal("measured graph differs from installed helper")
		}
	}
	for _, helper := range helpers {
		if helper.Mode == 2 && helper.Speed >= 1 && graphs[key(helper.FPS, helper.Speed, 1)] != strings.TrimPrefix(helper.Filter, ",") {
			t.Fatal("fast interpolation modes differ")
		}
	}
}

func TestInterpolationNativeDecimalRounding(t *testing.T) {
	var helpers []struct {
		FPS    string
		Speed  float64
		Filter string
	}
	if err := json.Unmarshal(denoiseRead(t, "testdata/lrt-interpolation-rounding-7.5.5.json"), &helpers); err != nil || len(helpers) != 3 {
		t.Fatalf("expected three native decimal-rounding graphs: %v", err)
	}
	for _, helper := range helpers {
		parts := strings.Split(helper.Filter, "fps=")
		rate, err := parseFrameRate(parts[len(parts)-1])
		if err != nil {
			t.Fatal(err)
		}
		clock, err := newInterpolationClock(rate, helper.FPS, helper.Speed)
		if err != nil {
			t.Fatal(err)
		}
		intermediate := new(big.Rat).Mul(clock.ratio, new(big.Rat).SetFrac64(int64(rate.numerator), int64(rate.denominator)))
		if !strings.Contains(helper.Filter, "fps="+intermediate.FloatString(3)+"'") {
			t.Fatalf("Java half-up fps rounding differs: %s got%s", helper.Filter, intermediate.FloatString(3))
		}
	}
}

func TestInterpolationPreservationAndOverrides(t *testing.T) {
	dir := outputSharpenFixture(t, 17)
	frames, err := DiscoverSequence(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, preview := range []bool{false, true} {
		var control map[string][]byte
		for _, mode := range []int{0, 1, 2} {
			output := filepath.Join(t.TempDir(), "still")
			options := RenderOptions{Quality: 90, Interpolation: mode}
			if preview {
				_, err = GeneratePreviewImages(frames, output, options)
			} else {
				err = ExportTIFFSequence(frames, output, options)
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
			if mode == 0 {
				control = contents
			} else if !reflect.DeepEqual(control, contents) {
				t.Fatal("video interpolation changed still exports")
			}
		}
	}
	for _, command := range []string{"render", "subtitles"} {
		for _, flags := range [][]string{{"--interpolation", "-1"}, {"--interpolation", "3"}, {"--interpolation", "2", "--speed", "0.5"}} {
			ext := ".avi"
			if command == "subtitles" {
				ext = ".srt"
			}
			output := filepath.Join(t.TempDir(), "keep"+ext)
			if err := os.WriteFile(output, []byte("original"), 0600); err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer
			args := []string{command, dir, "--output", output, "--overwrite"}
			if Main(append(args, flags...), &stdout, &stderr) == 0 {
				t.Fatal("accepted unsupported interpolation", flags)
			}
			assertPreviewEVFileBytes(t, output, []byte("original"))
		}
	}
	short := outputSharpenFixture(t, 2)
	var stdout, stderr bytes.Buffer
	if Main([]string{"render", short, "--output", filepath.Join(t.TempDir(), "empty.avi"), "--interpolation", "1", "--speed", ".5"}, &stdout, &stderr) == 0 {
		t.Fatal("accepted empty minterpolate timeline")
	}
	if err := os.WriteFile(filepath.Join(dir, "frame16.png"), []byte("corrupt dropped tail"), 0600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "keep.avi")
	if err := os.WriteFile(output, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if Main([]string{"render", dir, "--output", output, "--overwrite", "--speed", "4", "--interpolation", "1"}, &stdout, &stderr) == 0 {
		t.Fatal("ignored dropped source tail error")
	}
	assertPreviewEVFileBytes(t, output, []byte("original"))
	files, err := os.ReadDir(filepath.Dir(output))
	if err != nil || len(files) != 1 {
		t.Fatal("failed interpolation left temporary video", err)
	}
	preset, err := decodeLRTRenderPreset(renderPresetObservations(t)[0].Preset)
	if err != nil {
		t.Fatal(err)
	}
	preset.Interpolation, preset.Speed = 2, .5
	options, err := applyLRTRenderPreset(RenderOptions{Interpolation: 0}, preset, "", map[string]bool{"interpolation": true})
	if err != nil {
		t.Fatal(err)
	}
	if options.Interpolation != 0 {
		t.Fatal("explicit interpolation zero lost priority")
	}
	if _, err := planInterpolatedVideoFrames(17, .5, 0, 0, options.Interpolation, frameRate{24, 1}, "24"); err != nil {
		t.Fatal(err)
	}
	if _, err := newPlaybackFrameStream(1, 1, 3, 0, 1, .5, frameRate{24, 1}, "invalid", nil); err == nil {
		t.Fatal("accepted bad intermediate rate")
	}
	stream, err := newPlaybackFrameStream(1, 1, 3, 0, 1, .5, frameRate{24, 1}, "24", func(i int) ([]byte, error) {
		if i == 2 {
			return nil, fmt.Errorf("future failed")
		}
		return make([]byte, 6), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := stream.frame(0); err == nil || !strings.Contains(err.Error(), "future failed") {
		t.Fatal("lost interpolation lookahead error")
	}
	plan, err := planInterpolatedVideoFrames(3, .7, 0, 0, 1, frameRate{24, 1}, "24")
	if err != nil || plan.count() != 3 {
		t.Fatal("wrong short fractional timeline", err)
	}
	timing, err := planRenderTiming(RenderOptions{NativeTiming: true, PlaybackSpeed: .7}, 3, plan.count(), 0, frameRate{24, 1}, "24")
	if err != nil || timing.audioDuration != "0.167" {
		t.Fatal("native audio estimate changed", timing, err)
	}
}

func TestInterpolationJPEGAVI(t *testing.T) {
	dir := t.TempDir()
	var input [][]byte
	for f, rgb := range outputDeflickerSynthetic(8, 16, 8) {
		img, err := imageFromRGB48(rgb, 16, 8)
		if err != nil {
			t.Fatal(err)
		}
		var encoded bytes.Buffer
		if err := jpeg.Encode(&encoded, img, &jpeg.Options{Quality: 95}); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%03d.jpg", f)), encoded.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		decoded, err := jpeg.Decode(bytes.NewReader(encoded.Bytes()))
		if err != nil {
			t.Fatal(err)
		}
		pixels, err := imageRGB48(decoded)
		if err != nil {
			t.Fatal(err)
		}
		input = append(input, pixels)
	}
	before := batchFileSnapshot(t, dir)
	for _, speed := range []float64{.5, 2} {
		want, _ := interpolationReferenceRGB(input, 16, 8, len(input), 1, speed, "24")
		output := filepath.Join(t.TempDir(), "blend.avi")
		markerCommand(t, "render", dir, "--output", output, "--quality", "100", "--speed", strconv.FormatFloat(speed, 'f', -1, 64), "--interpolation", "1")
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
				t.Fatal("missing interpolated AVI frame")
			}
			size := int(binary.LittleEndian.Uint32(data[pos+4:]))
			pos += 8
			if pos+size > len(data) || !bytes.Equal(encoded.Bytes(), data[pos:pos+size]) {
				t.Fatalf("AVI speed%g frame%d", speed, f)
			}
			pos += size + size%2
		}
		if pos+4 <= len(data) && string(data[pos:pos+4]) == "00dc" {
			t.Fatal("extra AVI frame")
		}
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("interpolation changed JPEG inputs")
	}
}

func TestInterpolationActualVideo(t *testing.T) {
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
	dir := outputSharpenFixture(t, 17)
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
	for _, label := range []string{"24", "29.97", "23.976"} {
		for _, value := range []float64{.5, .7, 2.3} {
			speed := float64(float32(value))
			rate, _ := parseFrameRate(label)
			filtered, _ := interpolationReferenceRGB(input, 64, 32, len(input), 1, speed, label)
			var expected []int
			for _, rgb := range filtered {
				for i := 0; i < len(rgb); i += 6 {
					r, g, b := float64(binary.LittleEndian.Uint16(rgb[i:])), float64(binary.LittleEndian.Uint16(rgb[i+2:])), float64(binary.LittleEndian.Uint16(rgb[i+4:]))
					expected = append(expected, int(math.Round((.2126*r+.7152*g+.0722*b)*1023/65535)))
				}
			}
			for _, codec := range []string{"h264", "h265"} {
				t.Run(fmt.Sprintf("fps%s/speed%g/%s", label, speed, codec), func(t *testing.T) {
					encoder, ext := "libx264", ".mp4"
					if codec == "h265" {
						encoder, ext = "libx265", ".mov"
					}
					if err := requireFFmpegPixelFormat(ffmpeg, encoder, "yuv444p10le"); err != nil {
						t.Skipf("encoder unavailable: %v", err)
					}
					output := filepath.Join(t.TempDir(), "interpolate"+ext)
					markerCommand(t, "render", dir, "--output", output, "--ffmpeg", ffmpeg, "--codec", codec, "--subsampling", "444", "--video-range", "full", "--fps", label, "--crf", "0", "--preset", "ultrafast", "--interpolation", "1", "--speed", strconv.FormatFloat(value, 'f', -1, 64))
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
						t.Fatalf("probe %s %v", metadata, err)
					}
					s := probe.Streams[0]
					duration, err := strconv.ParseFloat(s.Duration, 64)
					fps := rate.String()
					if rate.denominator == 1 {
						fps += "/1"
					}
					if err != nil || s.Pixel != "yuv444p10le" || s.Frames != strconv.Itoa(len(filtered)) || s.FPS != fps || math.Abs(duration-rate.seconds(len(filtered))) > .001 {
						t.Fatalf("wrong interpolation timeline %s", metadata)
					}
					luma, err := exec.Command(ffmpeg, "-v", "error", "-i", output, "-vf", "extractplanes=y", "-fps_mode", "passthrough", "-f", "rawvideo", "pipe:1").Output()
					if err != nil || len(luma) != len(expected)*2 {
						t.Fatalf("decoded bytes%d %v", len(luma), err)
					}
					maximum, total := 0, 0
					for i, want := range expected {
						difference := absInt(int(binary.LittleEndian.Uint16(luma[i*2:])) - want)
						maximum = max(maximum, difference)
						total += difference
					}
					mean := float64(total) / float64(len(expected))
					t.Logf("%d frames luma max%d mean%.3f", len(filtered), maximum, mean)
					// Compare all decoded Y/U/V samples against a separate encode
					// of the offline RGB reference. CRF 0 round-trip errors are not
					// used as a substitute for the interpolation pixel comparison.
					reference := filepath.Join(t.TempDir(), "reference"+ext)
					tag, container := "avc1", "mp4"
					if codec == "h265" {
						tag, container = "hvc1", "mov"
					}
					command := exec.Command(ffmpeg, "-v", "error", "-f", "rawvideo", "-pixel_format", "rgb48le", "-video_size", "64x32", "-framerate", rate.String(), "-i", "pipe:0", "-an", "-c:v", encoder, "-preset", "ultrafast", "-crf", "0", "-pix_fmt", "yuv444p10le", "-vf", videoRangeFilter("full", "yuv444p10le"), "-tag:v", tag, "-color_range", "pc", "-colorspace", "bt709", "-movflags", "+faststart+write_colr", "-f", container, reference)
					command.Stdin = bytes.NewReader(bytes.Join(filtered, nil))
					if result, err := command.CombinedOutput(); err != nil {
						t.Fatalf("reference encode: %v %s", err, result)
					}
					decode := func(path string) []byte {
						t.Helper()
						data, err := exec.Command(ffmpeg, "-v", "error", "-i", path, "-fps_mode", "passthrough", "-pix_fmt", "yuv444p10le", "-f", "rawvideo", "pipe:1").Output()
						if err != nil {
							t.Fatal(err)
						}
						return data
					}
					if !bytes.Equal(decode(output), decode(reference)) {
						t.Fatal("decoded interpolation pixels differ from independent reference encode")
					}
				})
			}
		}
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("real interpolation changed sources")
	}
}
