package lrtl

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

type nativeTimingObservation struct {
	FPS, AudioFade, AudioTrim string
	Window, SourceFrames      int
	Speed                     float64
	Fade                      string
}

func nativeTimingObservations(t *testing.T) []nativeTimingObservation {
	t.Helper()
	data, err := os.ReadFile("testdata/lrt-native-timing-7.5.5.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []nativeTimingObservation
	if err := json.Unmarshal(data, &cases); err != nil || len(cases) != 96 {
		t.Fatalf("expected96 direct native timing observations: %v", err)
	}
	return cases
}

func TestNativeTiming96FilterObservations(t *testing.T) {
	differences := 0
	for _, tc := range nativeTimingObservations(t) {
		plan, err := planVideoFrames(tc.SourceFrames, tc.Speed, tc.Window)
		if err != nil {
			continue // Native emits invalid durations for empty video; Go refuses it.
		}
		rate, err := parseFrameRate(tc.FPS)
		if err != nil {
			t.Fatal(err)
		}
		options := RenderOptions{NativeTiming: true, PlaybackSpeed: tc.Speed, FadeOutSeconds: 1}
		timing, err := planRenderTiming(options, tc.SourceFrames, plan.count(), tc.Window, rate, tc.FPS)
		var start, duration float64
		if _, err := fmt.Sscanf(tc.Fade, "fade=t=out:st=%f:d=%f", &start, &duration); err != nil {
			t.Fatal(err)
		}
		if err != nil || timing.fade.start != start || float32(timing.fade.duration) != float32(duration) || "atrim=duration="+timing.audioDuration != tc.AudioTrim || strings.TrimPrefix(tc.AudioFade, "a") != tc.Fade {
			t.Fatalf("native %+v vs %+v: %v", tc, timing, err)
		}
		options.NativeTiming = false
		actual, err := planRenderTiming(options, tc.SourceFrames, plan.count(), tc.Window, rate, tc.FPS)
		if err != nil || actual.audioDuration != "" {
			t.Fatal("standalone timing should preserve rational audio duration")
		}
		if timing.fade != actual.fade {
			differences++
		}
		options.NativeTiming, options.FadeOutSeconds = true, 0
		off, err := planRenderTiming(options, tc.SourceFrames, plan.count(), tc.Window, rate, tc.FPS)
		if err != nil || off.fade.duration != 0 || off.audioDuration != timing.audioDuration {
			t.Fatal("disabling fade lost native audio duration")
		}
	}
	if differences == 0 {
		t.Fatal("fixture did not cover actual/native duration disagreements")
	}
	t.Logf("%d native fade plans differ from actual-frame plans", differences)
	for _, options := range []RenderOptions{{NativeTiming: true, PlaybackSpeed: math.NaN()}, {NativeTiming: true, PlaybackSpeed: 16}, {FadeOutSeconds: -1}} {
		if _, err := planRenderTiming(options, 1, 1, 0, frameRate{24, 1}, "24"); err == nil {
			t.Fatal("accepted invalid native timing")
		}
	}
	if _, err := planRenderTiming(RenderOptions{}, 0, 0, 0, frameRate{24, 1}, "24"); err == nil {
		t.Fatal("accepted empty timing input")
	}
}

func TestNativeTimingPresetFramePixelsAudioAndBatch(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(t.TempDir(), "capture")
	t.Setenv("LRTL_TEST_FFMPEG_CAPTURE", capture)
	audio := filepath.Join(t.TempDir(), "audio.wav")
	if err := os.WriteFile(audio, []byte("audio argument fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, count := range []int{3, 11} {
		dir := videoFadeFixture(t, count)
		before := batchFileSnapshot(t, dir)
		for _, speed := range []float64{.1, 1.1} {
			for _, blur := range []int{0, 2} {
				plan, err := planVideoFrames(count, speed, blur)
				if err != nil {
					continue
				}
				var observation *nativeTimingObservation
				for _, tc := range nativeTimingObservations(t) {
					if tc.SourceFrames == count && float32(tc.Speed) == float32(speed) && tc.Window == blur && tc.FPS == "24" {
						copy := tc
						observation = &copy
						break
					}
				}
				if observation == nil {
					t.Fatal("missing native timing observation")
				}
				preset, err := decodeLRTRenderPreset(renderPresetObservations(t)[0].Preset)
				if err != nil {
					t.Fatal(err)
				}
				preset.OutputResV, preset.FrameRate, preset.Speed = 32, "24", speed
				preset.FadeOut, preset.FadeTime, preset.MotionBlur, preset.MotionBlurLevel = true, 1, blur > 0, blur
				preset.Audio, preset.AudioTrack = true, audio
				path := savePresetJSON(t, preset)
				presetBefore, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				run := func(extra ...string) []byte {
					args := []string{"render", dir, "--output", filepath.Join(t.TempDir(), "video.mp4"), "--ffmpeg", executable, "--lrt-render-preset", path, "--video-range", "auto"}
					markerCommand(t, append(args, extra...)...)
					data, err := os.ReadFile(capture)
					if err != nil {
						t.Fatal(err)
					}
					return data
				}
				control := run("--fade-out", "0")
				got := run()
				if len(got) != plan.count()*64*32*6 || len(control) != len(got) {
					t.Fatal("native estimate changed actual frame count")
				}
				var start, duration float64
				if _, err := fmt.Sscanf(observation.Fade, "fade=t=out:st=%f:d=%f", &start, &duration); err != nil {
					t.Fatal(err)
				}
				duration = float64(float32(duration))
				for frame := 0; frame < plan.count(); frame++ {
					gain := math.Max(0, math.Min(1, 1-(float64(frame)/24-start)/duration))
					for p := frame * 64 * 32 * 6; p < (frame+1)*64*32*6; p += 2 {
						want := uint16(math.Round(float64(binary.LittleEndian.Uint16(control[p:])) * gain))
						if value := binary.LittleEndian.Uint16(got[p:]); value != want {
							t.Fatalf("source%d speed%g blur%d frame%d: %d want%d", count, speed, blur, frame, value, want)
						}
					}
				}
				argData, err := os.ReadFile(capture + ".json")
				if err != nil || !bytes.Contains(argData, []byte(observation.AudioTrim)) || !bytes.Contains(argData, []byte(strings.TrimSuffix(observation.AudioFade, ".0"))) {
					t.Fatalf("native audio timing missing: %s %v", argData, err)
				}
				if overridden := run("--output-height", "32"); !bytes.Equal(got, overridden) {
					t.Fatal("explicit dimensions disabled native timing")
				}
				if count == 3 && speed == .1 && blur == 0 {
					markerCommand(t, "batch-render", dir, "--output", t.TempDir(), "--", "--ffmpeg", executable, "--lrt-render-preset", path, "--video-range", "auto")
					assertPreviewEVFileBytes(t, capture, got)
				}
				assertPreviewEVFileBytes(t, path, presetBefore)
			}
		}
		if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
			t.Fatal("native timing changed source images")
		}
	}
}

func TestNativeTimingActualAudioTailAndVideo(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("FFmpeg unavailable")
	}
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe unavailable")
	}
	dir := videoFadeFixture(t, 11)
	audio := filepath.Join(t.TempDir(), "tone.wav")
	if data, err := exec.Command(ffmpeg, "-v", "error", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000", "-t", "1", "-c:a", "pcm_s16le", audio).CombinedOutput(); err != nil {
		t.Fatalf("test tone: %s %v", data, err)
	}
	preset, err := decodeLRTRenderPreset(renderPresetObservations(t)[0].Preset)
	if err != nil {
		t.Fatal(err)
	}
	preset.OutputResV, preset.FrameRate, preset.Speed = 32, "24", 1.1
	preset.FadeOut, preset.FadeTime, preset.Audio, preset.AudioTrack = true, 1, true, audio
	path := savePresetJSON(t, preset)
	output := filepath.Join(t.TempDir(), "native.mp4")
	markerCommand(t, "render", dir, "--output", output, "--lrt-render-preset", path, "--ffmpeg", ffmpeg, "--preset", "ultrafast", "--crf", "0", "--video-range", "auto")
	data, err := exec.Command(ffprobe, "-v", "error", "-show_entries", "stream=codec_type,duration,nb_frames", "-of", "json", output).CombinedOutput()
	if err != nil {
		t.Fatalf("ffprobe: %s %v", data, err)
	}
	var probe struct {
		Streams []struct {
			Type     string `json:"codec_type"`
			Duration string `json:"duration"`
			Frames   string `json:"nb_frames"`
		}
	}
	if err := json.Unmarshal(data, &probe); err != nil || len(probe.Streams) != 2 {
		t.Fatalf("unexpected streams: %s %v", data, err)
	}
	for _, stream := range probe.Streams {
		duration, err := strconv.ParseFloat(stream.Duration, 64)
		want := .417
		if stream.Type == "video" {
			want = .375
			if stream.Frames != "9" {
				t.Fatalf("wrong actual video count: %s", data)
			}
		}
		if err != nil || math.Abs(duration-want) > .001 {
			t.Fatalf("native audio/video duration: %s %v", data, err)
		}
	}
	pcm, err := exec.Command(ffmpeg, "-v", "error", "-i", output, "-vn", "-ac", "1", "-ar", "48000", "-f", "f32le", "pipe:1").Output()
	if err != nil {
		t.Fatal(err)
	}
	rms := func(start, end float64) float64 {
		first, last := int(start*48000), int(end*48000)
		if last*4 > len(pcm) {
			t.Fatal("native audio tail was truncated")
		}
		sum := 0.
		for i := first; i < last; i++ {
			value := float64(math.Float32frombits(binary.LittleEndian.Uint32(pcm[i*4:])))
			sum += value * value
		}
		return math.Sqrt(sum / float64(last-first))
	}
	if ratio := rms(.35, .37) / rms(.10, .12); math.Abs(ratio-.1848) > .04 {
		t.Fatalf("decoded audio did not use native fade duration: %g", ratio)
	}
	if ratio := rms(.39, .40) / rms(.10, .11); ratio < .03 || ratio > .12 {
		t.Fatalf("native audio beyond the video end is missing or unfaded: %g", ratio)
	}
	rgb, err := exec.Command(ffmpeg, "-v", "error", "-i", output, "-an", "-fps_mode", "passthrough", "-f", "rawvideo", "-pix_fmt", "rgb24", "pipe:1").Output()
	if err != nil || len(rgb) != 9*64*32*3 {
		t.Fatalf("decoded video: bytes%d %v", len(rgb), err)
	}
	for channel := 0; channel < 3; channel++ {
		base := rgb[(16*64+32)*3+channel]
		last := rgb[8*64*32*3+(16*64+32)*3+channel]
		if math.Abs(float64(last)-float64(base)*.2) > 3 {
			t.Fatalf("native fade tail channel%d: %d base%d", channel, last, base)
		}
	}
}
