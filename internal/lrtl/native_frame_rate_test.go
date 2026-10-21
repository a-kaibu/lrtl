package lrtl

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
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

type nativeRateLabelObservation struct{ Input, NativeRate, BlendFilter, BlendError string }
type nativeRateTimingObservation struct {
	Input, NativeRate, Filter, Hash string
	Frames, OutputFrames            int
	FirstY                          []byte
}

func nativeRateLabels(t *testing.T) []nativeRateLabelObservation {
	t.Helper()
	var rows []nativeRateLabelObservation
	if err := json.Unmarshal(denoiseRead(t, "testdata/lrt-rate-label-7.5.5.json"), &rows); err != nil || len(rows) != 156 {
		t.Fatalf("expected 156 native rate labels: %v", err)
	}
	return rows
}
func nativeRateTimings(t *testing.T) []nativeRateTimingObservation {
	t.Helper()
	var rows []nativeRateTimingObservation
	if err := json.Unmarshal(denoiseRead(t, "testdata/lrt-rate-label-timing-8.0.1.json"), &rows); err != nil || len(rows) != 282 {
		t.Fatalf("expected 282 native rate timelines: %v", err)
	}
	return rows
}

func TestNativeFrameRateMeasuredLabels(t *testing.T) {
	for _, tc := range nativeRateLabels(t) {
		want, wantErr := parseFrameRate(tc.NativeRate)
		got, err := parseNativeFrameRate(tc.Input)
		if (err == nil) != (wantErr == nil) || (err == nil && got != want) {
			t.Errorf("native label %q: %v %v want%v %v", tc.Input, got, err, want, wantErr)
		}
		if err == nil && tc.BlendError == "" {
			clock, err := newInterpolationClock(got, nativeFrameRateLabel(tc.Input), .5)
			if err != nil {
				t.Fatal(err)
			}
			fps := new(big.Rat).Mul(clock.ratio, new(big.Rat).SetFrac64(int64(got.numerator), int64(got.denominator)))
			if !strings.Contains(tc.BlendFilter, "fps="+fps.FloatString(3)+"'") {
				t.Fatal("native intermediate rate differs", tc.Input, fps)
			}
		}
	}
	for _, input := range []string{"", strings.Repeat("2", 65), "0x1p", "-1", "-Infinity", "240.5", "241", "NaN", "2_4", "0x1_8p0"} {
		if _, err := parseNativeFrameRate(input); err == nil {
			t.Fatal("accepted invalid native fps", input)
		}
	}
	for _, input := range []string{"0.9992", "0x1.8p4f", "24f", "2.4e1", "24.5"} {
		if _, err := parseNativeFrameRate(input); err != nil {
			t.Fatal(input, err)
		}
	}
	native, err := (RenderOptions{FrameRate: "24.5", NativeFPS: true}).frameRate()
	if err != nil || native.String() != "25000/1001" {
		t.Fatal(native, err)
	}
	standard, err := (RenderOptions{FrameRate: "24.5"}).frameRate()
	if err != nil || standard.String() != "49/2" {
		t.Fatal("standalone decimal changed", standard, err)
	}
	if (RenderOptions{FPS: 24}).frameRateLabel() != "24" {
		t.Fatal("integer label changed")
	}
}

func TestNativeFrameRateMeasuredTimelines(t *testing.T) {
	graphs := map[string]string{}
	for _, row := range nativeRateLabels(t) {
		graphs[row.Input] = strings.TrimPrefix(row.BlendFilter, ",")
	}
	for _, tc := range nativeRateTimings(t) {
		if graphs[tc.Input] != tc.Filter {
			t.Fatal("timing graph differs from installed helper", tc.Input)
		}
		rate, err := parseNativeFrameRate(tc.Input)
		if err != nil {
			t.Fatal(err)
		}
		want, err := parseFrameRate(tc.NativeRate)
		if err != nil || want != rate {
			t.Fatal("wrong translated input/output rate")
		}
		clock, err := newInterpolationClock(rate, nativeFrameRateLabel(tc.Input), .5)
		if err != nil {
			t.Fatal(err)
		}
		if clock.count(tc.Frames) != tc.OutputFrames {
			t.Errorf("native count %q frames%d got%d want%d", tc.Input, tc.Frames, clock.count(tc.Frames), tc.OutputFrames)
			continue
		}
		hash := sha256.New()
		var ids []byte
		j := 0
		for target := 0; target < tc.OutputFrames; target++ {
			for j < clock.last(tc.Frames) && clock.outputPTS(j+1) <= target {
				j++
			}
			left, alpha := clock.sample(j, tc.Frames)
			for p := 0; p < 3; p++ {
				a := 32 + p*48 + left*8
				value := byte((a*(1024-alpha) + (a+8)*alpha + 512) >> 10)
				hash.Write(bytes.Repeat([]byte{value}, 64*32))
				if p == 0 {
					ids = append(ids, value)
				}
			}
		}
		if hex.EncodeToString(hash.Sum(nil)) != tc.Hash || !bytes.Equal(ids, tc.FirstY) {
			t.Errorf("native samples differ: %q frames%d", tc.Input, tc.Frames)
		}
	}
}

func TestNativeFrameRateLiveTimelines(t *testing.T) {
	ffmpeg := os.Getenv("LRTL_RATE_FFMPEG")
	if ffmpeg == "" {
		t.Skip("set LRTL_RATE_FFMPEG for bundled FFmpeg references")
	}
	for _, tc := range nativeRateTimings(t) {
		var input []byte
		for f := 0; f < tc.Frames; f++ {
			for p := 0; p < 3; p++ {
				input = append(input, bytes.Repeat([]byte{byte(32 + p*48 + f*8)}, 64*32)...)
			}
		}
		cmd := exec.Command(ffmpeg, "-v", "error", "-f", "rawvideo", "-pixel_format", "yuv444p", "-video_size", "64x32", "-framerate", tc.NativeRate, "-i", "pipe:0", "-vf", tc.Filter, "-fps_mode", "passthrough", "-f", "rawvideo", "-pix_fmt", "yuv444p", "pipe:1")
		var stderr bytes.Buffer
		cmd.Stdin = bytes.NewReader(input)
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("native rate graph: %v %s", err, stderr.String())
		}
		hash := sha256.Sum256(out)
		if hex.EncodeToString(hash[:]) != tc.Hash {
			t.Fatalf("native rate graph changed: %q frames%d", tc.Input, tc.Frames)
		}
	}
}

func TestNativeFrameRatePresetSubtitlesAndOverride(t *testing.T) {
	dir := outputSharpenFixture(t, 17)
	before := batchFileSnapshot(t, dir)
	frames, err := DiscoverSequence(dir)
	if err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(t.TempDir(), "capture")
	t.Setenv("LRTL_TEST_FFMPEG_CAPTURE", capture)
	for _, label := range []string{"24.5", "23.90625", "24.0009", "0x1.8p4f", "2.4e1"} {
		preset, err := decodeLRTRenderPreset(renderPresetObservations(t)[0].Preset)
		if err != nil {
			t.Fatal(err)
		}
		preset.FrameRate, preset.OutputResV, preset.Speed, preset.Interpolation = label, 32, .7, 1
		preset.MotionBlur, preset.MotionBlurLevel = true, 2
		preset.RemoveAirplanes, preset.RemoveAirplanesLevel = true, 1
		path := savePresetJSON(t, preset)
		saved := denoiseRead(t, path)
		options, err := applyLRTRenderPreset(RenderOptions{FPS: 24}, preset, path, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !options.NativeFPS {
			t.Fatal("preset did not activate native rate mapping")
		}
		rate, err := options.frameRate()
		if err != nil {
			t.Fatal(err)
		}
		speed, _ := options.playbackSpeed()
		plan, err := planInterpolatedVideoFrames(len(frames), speed, 2, 1, 1, rate, options.frameRateLabel())
		if err != nil {
			t.Fatal(err)
		}
		markerCommand(t, "render", dir, "--output", filepath.Join(t.TempDir(), "native.mp4"), "--ffmpeg", executable, "--lrt-render-preset", path)
		pixels := denoiseRead(t, capture)
		if len(pixels) != plan.count()*64*32*3 {
			t.Fatal("native rate lost interpolation timeline")
		}
		var flags []string
		if err := json.Unmarshal(denoiseRead(t, capture+".json"), &flags); err != nil {
			t.Fatal(err)
		}
		args := strings.Join(flags, " ")
		if !strings.Contains(args, "-pixel_format yuv444p ") {
			t.Fatal("native slow rate must retain 8-bit encoder input")
		}
		if !strings.Contains(args, "-framerate "+rate.String()) {
			t.Fatalf("encoder rate mismatch: %s", args)
		}
		markerCommand(t, "batch-render", dir, "--output", t.TempDir(), "--", "--ffmpeg", executable, "--lrt-render-preset", path)
		if !bytes.Equal(pixels, denoiseRead(t, capture)) {
			t.Fatal("batch changed native rate pixels")
		}
		want := filepath.Join(t.TempDir(), "want.srt")
		if err := writeSequenceSubtitlesWithInterpolation(frames, want, rate, options.frameRateLabel(), speed, 2, 1, 1, "05", false); err != nil {
			t.Fatal(err)
		}
		for _, selector := range []bool{false, true} {
			presetPath := path
			var extra []string
			if selector {
				presetPath = savePresetJSON(t, map[string]any{"renderPresets": []lrtRenderPreset{preset}})
				extra = []string{"--lrt-preset-name", preset.Name}
			}
			output := filepath.Join(t.TempDir(), "preset.srt")
			cmd := []string{"subtitles", dir, "--output", output, "--lrt-render-preset", presetPath, "--timestamp-format", "05"}
			markerCommand(t, append(cmd, extra...)...)
			if !bytes.Equal(denoiseRead(t, want), denoiseRead(t, output)) {
				t.Fatal("subtitle preset differs from video scheduler")
			}
		}
		overridden, err := applyLRTRenderPreset(RenderOptions{FrameRate: "24.5"}, preset, path, map[string]bool{"fps": true})
		if err != nil {
			t.Fatal(err)
		}
		if overridden.NativeFPS {
			t.Fatal("explicit --fps inherited native remapping")
		}
		literal, _ := overridden.frameRate()
		if literal.String() != "49/2" {
			t.Fatal("literal fps override changed")
		}
		markerCommand(t, "render", dir, "--output", filepath.Join(t.TempDir(), "literal.mp4"), "--ffmpeg", executable, "--lrt-render-preset", path, "--fps", "24.5")
		if err := json.Unmarshal(denoiseRead(t, capture+".json"), &flags); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(strings.Join(flags, " "), "-framerate 49/2") {
			t.Fatal("render CLI remapped explicit fractional fps")
		}
		srt := filepath.Join(t.TempDir(), "override.srt")
		markerCommand(t, "subtitles", dir, "--output", srt, "--lrt-render-preset", path, "--fps", "24.5", "--speed", "1", "--motion-blur", "0", "--remove-airplanes", "0", "--interpolation", "0", "--timestamp-format", "05")
		want = filepath.Join(t.TempDir(), "literal.srt")
		if err := writeSequenceSubtitlesAtRate(frames, want, frameRate{49, 2}, "05", false); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(denoiseRead(t, want), denoiseRead(t, srt)) {
			t.Fatal("explicit subtitle controls lost precedence")
		}
		assertPreviewEVFileBytes(t, path, saved)
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("native rate changed source files")
	}
}

func FuzzNativeFrameRate(f *testing.F) {
	for _, label := range []string{"24", "24.5", "24.000999", "0x1.8p4f", "1e2", "30000/1001", "NaN", "Infinity", "not-a-rate"} {
		f.Add(label)
	}
	f.Fuzz(func(t *testing.T, label string) {
		rate, err := parseNativeFrameRate(label)
		if err != nil {
			return
		}
		value := float64(rate.numerator) / float64(rate.denominator)
		if math.IsNaN(value) || value < 1 || value > 240 {
			t.Fatal("accepted invalid effective rate", rate)
		}
		again, err := parseFrameRate(rate.String())
		if err != nil || again != rate {
			t.Fatal("effective rate cannot round-trip", rate, err)
		}
	})
}

func TestNativeFrameRateSubtitleFailureRetention(t *testing.T) {
	dir := outputSharpenFixture(t, 8)
	before := batchFileSnapshot(t, dir)
	preset, err := decodeLRTRenderPreset(renderPresetObservations(t)[0].Preset)
	if err != nil {
		t.Fatal(err)
	}
	preset.FrameRate = "24.5"
	good := savePresetJSON(t, preset)
	bad := filepath.Join(t.TempDir(), "broken.json")
	if err := os.WriteFile(bad, []byte("{broken"), 0600); err != nil {
		t.Fatal(err)
	}
	duplicate := savePresetJSON(t, map[string]any{"renderPresets": []lrtRenderPreset{preset, preset}})
	invalid := preset
	invalid.FrameRate = "NaN"
	invalidPath := savePresetJSON(t, invalid)
	unsupported := preset
	unsupported.Interpolation, unsupported.Speed = 3, .5
	unsupportedPath := savePresetJSON(t, unsupported)
	for _, flags := range [][]string{
		{"--lrt-preset-name", "missing"},
		{"--lrt-render-preset", bad},
		{"--lrt-render-preset", good, "--lrt-preset-name", "missing"},
		{"--lrt-render-preset", duplicate, "--lrt-preset-name", preset.Name},
		{"--lrt-render-preset", invalidPath},
		{"--lrt-render-preset", unsupportedPath},
		{"--lrt-render-preset", good, "--fps", "1e2"},
	} {
		output := filepath.Join(t.TempDir(), "keep.srt")
		if err := os.WriteFile(output, []byte("original"), 0600); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr bytes.Buffer
		args := []string{"subtitles", dir, "--output", output, "--overwrite"}
		if Main(append(args, flags...), &stdout, &stderr) == 0 {
			t.Fatal("accepted invalid subtitle preset", flags)
		}
		assertPreviewEVFileBytes(t, output, []byte("original"))
		files, err := os.ReadDir(filepath.Dir(output))
		if err != nil || len(files) != 1 {
			t.Fatal("failed subtitle left temporary files", err)
		}
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("failed preset changed source sequence")
	}
}

func nativeRateAudioObservations(t *testing.T) []nativeTimingObservation {
	t.Helper()
	var rows []nativeTimingObservation
	if err := json.Unmarshal(denoiseRead(t, "testdata/lrt-rate-label-audio-7.5.5.json"), &rows); err != nil || len(rows) != 5 {
		t.Fatalf("expected five native rate/audio graphs: %v", err)
	}
	return rows
}

func TestNativeFrameRateAudioFilterObservations(t *testing.T) {
	for _, tc := range nativeRateAudioObservations(t) {
		rate, err := parseNativeFrameRate(tc.FPS)
		if err != nil {
			t.Fatal(err)
		}
		timing, err := planRenderTiming(RenderOptions{NativeTiming: true, PlaybackSpeed: 1, FadeOutSeconds: 1}, tc.SourceFrames, tc.SourceFrames, 0, rate, tc.FPS)
		if err != nil {
			t.Fatal(err)
		}
		var start, duration float64
		if _, err := fmt.Sscanf(tc.Fade, "fade=t=out:st=%f:d=%f", &start, &duration); err != nil {
			t.Fatal(err)
		}
		if timing.fade.start != start || float32(timing.fade.duration) != float32(duration) || "atrim=duration="+timing.audioDuration != tc.AudioTrim {
			t.Fatalf("native raw label timing differs: %+v vs %+v", tc, timing)
		}
	}
}

func TestNativeFrameRateActualAudioAndFade(t *testing.T) {
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
	dir := videoFadeFixture(t, 15)
	before := batchFileSnapshot(t, dir)
	audio := filepath.Join(t.TempDir(), "tone.wav")
	if data, err := exec.Command(ffmpeg, "-v", "error", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000", "-t", "8", "-c:a", "pcm_s16le", audio).CombinedOutput(); err != nil {
		t.Fatalf("tone: %v %s", err, data)
	}
	for _, tc := range nativeRateAudioObservations(t) {
		t.Run(tc.FPS, func(t *testing.T) {
			preset, err := decodeLRTRenderPreset(renderPresetObservations(t)[0].Preset)
			if err != nil {
				t.Fatal(err)
			}
			preset.FrameRate, preset.OutputResV, preset.FadeOut, preset.FadeTime, preset.Audio, preset.AudioTrack = tc.FPS, 32, true, 1, true, audio
			path := savePresetJSON(t, preset)
			saved := denoiseRead(t, path)
			output := filepath.Join(t.TempDir(), "native.mp4")
			markerCommand(t, "render", dir, "--output", output, "--lrt-render-preset", path, "--ffmpeg", ffmpeg, "--video-range", "auto", "--crf", "0", "--preset", "ultrafast")
			metadata, err := exec.Command(ffprobe, "-v", "error", "-show_entries", "stream=codec_type,avg_frame_rate,duration,nb_frames", "-of", "json", output).CombinedOutput()
			if err != nil {
				t.Fatalf("probe %v %s", err, metadata)
			}
			var probe struct {
				Streams []struct {
					Type     string `json:"codec_type"`
					FPS      string `json:"avg_frame_rate"`
					Duration string `json:"duration"`
					Frames   string `json:"nb_frames"`
				}
			}
			if err := json.Unmarshal(metadata, &probe); err != nil || len(probe.Streams) != 2 {
				t.Fatalf("metadata %s %v", metadata, err)
			}
			rate, err := parseNativeFrameRate(tc.FPS)
			if err != nil {
				t.Fatal(err)
			}
			audioDuration, err := strconv.ParseFloat(strings.TrimPrefix(tc.AudioTrim, "atrim=duration="), 64)
			if err != nil {
				t.Fatal(err)
			}
			for _, stream := range probe.Streams {
				want := audioDuration
				if stream.Type == "video" {
					want = rate.seconds(15)
					fps := rate.String()
					if rate.denominator == 1 {
						fps += "/1"
					}
					if stream.FPS != fps || stream.Frames != "15" {
						t.Fatalf("wrong native output rate: %s", metadata)
					}
				}
				duration, err := strconv.ParseFloat(stream.Duration, 64)
				if err != nil || math.Abs(duration-want) > .002 {
					t.Fatalf("wrong native duration %s expected%.6f", metadata, want)
				}
			}
			timing, err := planRenderTiming(RenderOptions{NativeTiming: true, PlaybackSpeed: 1, FadeOutSeconds: 1}, 15, 15, 0, rate, tc.FPS)
			if err != nil {
				t.Fatal(err)
			}
			y, err := exec.Command(ffmpeg, "-v", "error", "-i", output, "-vf", "extractplanes=y", "-fps_mode", "passthrough", "-f", "rawvideo", "pipe:1").Output()
			if err != nil || len(y) != 15*64*32 {
				t.Fatalf("decoded video: %d %v", len(y), err)
			}
			// Use the recorded native fade command as the oracle. Native YUV
			// fades have format black levels and an integer frame clock, unlike
			// the previous continuous RGB gain assertion. Decode the JPEG with
			// Go on both paths to isolate the known JPEG decoder difference.
			img, _, err := image.Decode(bytes.NewReader(denoiseRead(t, filepath.Join(dir, "frame00.jpg"))))
			if err != nil {
				t.Fatal(err)
			}
			rgb, err := imageRGB48(img)
			if err != nil {
				t.Fatal(err)
			}
			graph := "scale=iw:ih:out_range=full:flags=lanczos,format=yuv444p10le,setparams=color_primaries=bt709:color_trc=bt709:colorspace=bt709," + nativeSDRPlanarColorFilter("limited", "yuv420p") + "," + tc.Fade + ",extractplanes=y"
			wantY := nativeColorCommand(t, ffmpeg, bytes.Repeat(rgb, 15), "-v", "error", "-sws_flags", "print_info+accurate_rnd+bitexact+full_chroma_int", "-f", "rawvideo", "-pixel_format", "rgb48le", "-video_size", "64x32", "-framerate", rate.String(), "-i", "pipe:0", "-vf", graph, "-f", "rawvideo", "pipe:1")
			if !bytes.Equal(y, wantY) {
				t.Fatalf("video fade differs from recorded native fps clock: hashes%x/%x", sha256.Sum256(y), sha256.Sum256(wantY))
			}
			pcm, err := exec.Command(ffmpeg, "-v", "error", "-i", output, "-vn", "-ac", "1", "-ar", "48000", "-f", "f32le", "pipe:1").Output()
			if err != nil {
				t.Fatal(err)
			}
			rms := func(first, last float64) float64 {
				a, b := int(first*48000), int(last*48000)
				if b*4 > len(pcm) || a < 0 || a >= b {
					t.Fatal("audio tail missing")
				}
				sum := 0.0
				for i := a; i < b; i++ {
					v := float64(math.Float32frombits(binary.LittleEndian.Uint32(pcm[i*4:])))
					sum += v * v
				}
				return math.Sqrt(sum / float64(b-a))
			}
			gain := func(time float64) float64 { return max(0, min(1, 1-(time-timing.fade.start)/timing.fade.duration)) }
			want := gain(audioDuration-.15) / gain(.06)
			got := rms(audioDuration-.2, audioDuration-.1) / rms(.04, .08)
			if math.Abs(got-want) > .04 {
				t.Fatalf("audio fade follows wrong label clock: ratio%.4f expected%.4f", got, want)
			}
			assertPreviewEVFileBytes(t, path, saved)
		})
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("native rate render changed source images")
	}
}
