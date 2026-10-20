package lrtl

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

type renderPresetObservation struct {
	Preset           json.RawMessage
	H264CRF, H265CRF int
}

func renderPresetObservations(t *testing.T) []renderPresetObservation {
	t.Helper()
	data, err := os.ReadFile("testdata/lrt-render-preset-7.5.5.json")
	if err != nil {
		t.Fatal(err)
	}
	var observations []renderPresetObservation
	if err := json.Unmarshal(data, &observations); err != nil {
		t.Fatal(err)
	}
	if len(observations) != 6 {
		t.Fatal("expected six native serialized presets")
	}
	return observations
}

func savePresetJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "preset.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLRTRenderPresetNativeFieldsAndQuality(t *testing.T) {
	for q, tc := range renderPresetObservations(t) {
		path := savePresetJSON(t, tc.Preset)
		preset, err := loadLRTRenderPreset(path, "")
		if err != nil {
			t.Fatal(err)
		}
		for _, encoder := range []string{"h264", "H265", "ProRes", "DNxHR"} {
			preset.Encoder = encoder
			options, err := applyLRTRenderPreset(RenderOptions{FPS: 24, CRF: 18}, preset, path, nil)
			if err != nil {
				t.Fatal(err)
			}
			wantCodec, wantCRF := "h264", tc.H264CRF
			switch encoder {
			case "H265":
				wantCodec, wantCRF = "h265", tc.H265CRF
			case "ProRes":
				wantCodec, wantCRF = []string{"prores-proxy", "prores-lt", "prores", "prores-hq", "prores-4444", "prores-hq"}[q], 18
			case "DNxHR":
				wantCodec, wantCRF = []string{"dnxhr-lb", "dnxhr-sq", "dnxhr-hq", "dnxhr-hqx", "dnxhr-444", "dnxhr-hq"}[q], 18
			}
			if options.Codec != wantCodec || options.CRF != wantCRF || options.OutputHeight != 1080 || options.FrameRate != "29.97" || !options.NativeGeometry {
				t.Fatalf("quality %d %s: %+v", q, encoder, options)
			}
			wantBlur := 0
			if preset.MotionBlur {
				wantBlur = preset.MotionBlurLevel
			}
			if options.MotionBlurFrames != wantBlur {
				t.Fatal("lost saved motion blur")
			}
		}
	}
}

func TestLRTRenderPresetGlobalSelectionAspectAndOverrides(t *testing.T) {
	tc := renderPresetObservations(t)[2]
	settings := map[string]any{"lastRenderSettings": tc.Preset, "renderPresets": []json.RawMessage{tc.Preset}, "renderConstrainCrop": true, "renderAR": 1.5, "otherNativeSettings": "ignored"}
	path := savePresetJSON(t, settings)
	for _, name := range []string{"", "Synthetic quality 2"} {
		preset, err := loadLRTRenderPreset(path, name)
		if err != nil {
			t.Fatal(err)
		}
		options, err := applyLRTRenderPreset(RenderOptions{FPS: 24, Quality: 90}, preset, path, nil)
		if err != nil || options.AspectRatio != 1.5 {
			t.Fatalf("saved aspect: %+v %v", options, err)
		}
		preset.Audio, preset.AudioTrack = true, "music.wav"
		options, err = applyLRTRenderPreset(RenderOptions{FPS: 24}, preset, path, nil)
		if err != nil || options.AudioPath != filepath.Join(filepath.Dir(path), "music.wav") {
			t.Fatalf("relative audio: %s %v", options.AudioPath, err)
		}
		explicit := map[string]bool{"codec": true, "crf": true, "fps": true, "long-edge": true, "aspect": true, "audio": true, "motion-blur": true}
		base := RenderOptions{NativeTiming: true, VideoRange: "limited", PlaybackSpeed: 1, FrameRate: "24", Codec: "h264", CRF: 31, LongEdge: 320, AspectRatio: 2, MotionBlurFrames: 0, AudioPath: ""}
		options, err = applyLRTRenderPreset(base, preset, path, explicit)
		if err != nil || options != base {
			t.Fatalf("overrides changed: %+v %v", options, err)
		}
	}
	for _, name := range []string{"missing", "synthetic quality 2"} {
		if _, err := loadLRTRenderPreset(path, name); err == nil {
			t.Fatalf("accepted unmatched name %s", name)
		}
	}
	settings["renderPresets"] = []json.RawMessage{tc.Preset, tc.Preset}
	if _, err := loadLRTRenderPreset(savePresetJSON(t, settings), "Synthetic quality 2"); err == nil {
		t.Fatal("accepted ambiguous name")
	}
	settings["renderAR"] = 0
	if _, err := loadLRTRenderPreset(savePresetJSON(t, settings), ""); err == nil {
		t.Fatal("accepted invalid saved aspect")
	}
}

func TestLRTRenderPresetCLIEquivalentManualAndBatch(t *testing.T) {
	root := t.TempDir()
	dir := batchInterpolationFixture(t, root, "sequence", false)
	for _, name := range []string{"frame5.png", "frame6.png", "frame7.png"} {
		writeRenderFrame(t, filepath.Join(dir, name), "png", 64, 32)
	}
	preset, err := decodeLRTRenderPreset(renderPresetObservations(t)[3].Preset)
	if err != nil {
		t.Fatal(err)
	}
	preset.OutputResV = 24
	path := savePresetJSON(t, preset)
	before := batchFileSnapshot(t, dir)
	beforePreset, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(t.TempDir(), "capture")
	t.Setenv("LRTL_TEST_FFMPEG_CAPTURE", capture)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) ([]byte, []string) {
		markerCommand(t, args...)
		data, err := os.ReadFile(capture)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(capture + ".json")
		if err != nil {
			t.Fatal(err)
		}
		var flags []string
		if err := json.Unmarshal(raw, &flags); err != nil {
			t.Fatal(err)
		}
		flags = flags[:len(flags)-1] // Temporary output filenames differ.
		return data, flags
	}
	output := filepath.Join(t.TempDir(), "preset.mp4")
	presetPixels, presetFlags := run("render", dir, "--output", output, "--ffmpeg", executable, "--lrt-render-preset", path)
	manualPixels, manualFlags := run("render", dir, "--output", filepath.Join(t.TempDir(), "manual.mp4"), "--ffmpeg", executable, "--fps", "29.97", "--codec", "h264", "--crf", "10", "--video-range", "limited", "--output-height", "24", "--motion-blur", "5")
	if !bytes.Equal(presetPixels, manualPixels) || !reflect.DeepEqual(presetFlags, manualFlags) {
		t.Fatal("saved preset differs from explicit render controls")
	}
	_, overridden := run("render", dir, "--output", filepath.Join(t.TempDir(), "override.mp4"), "--ffmpeg", executable, "--lrt-render-preset", path, "--fps", "25", "--crf", "27", "--motion-blur", "0", "--output-height", "16")
	joined := strings.Join(overridden, " ")
	if !strings.Contains(joined, "-framerate 25") || !strings.Contains(joined, "-crf 27") || !strings.Contains(joined, "-video_size 32x16") {
		t.Fatalf("overrides not applied: %s", joined)
	}
	batchOutput := t.TempDir()
	_, batchFlags := run("batch-render", root, "--output", batchOutput, "--", "--ffmpeg", executable, "--lrt-render-preset", path)
	if !reflect.DeepEqual(presetFlags, batchFlags) {
		t.Fatal("batch did not forward native preset")
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("preset render changed source")
	}
	assertPreviewEVFileBytes(t, path, beforePreset)
}

func TestLRTRenderPresetRejectsUnsupportedBeforeSaving(t *testing.T) {
	frames := cropFixture(t, 2)
	dir := filepath.Dir(frames[0].Image)
	before := batchFileSnapshot(t, dir)
	var original map[string]any
	if err := json.Unmarshal(renderPresetObservations(t)[0].Preset, &original); err != nil {
		t.Fatal(err)
	}
	for field, value := range map[string]any{"speed": 0, "interpolation": 1, "gamut": 1, "dynamicRange": 1, "cas": true, "removeAirplanes": true, "denoise": true, "deflickerLuma": true, "deflickerChroma": true, "subSampling": "444", "encoder": "unknown", "outputQuality": 6, "frameRate": "NaN", "outputResV": -1} {
		t.Run(field, func(t *testing.T) {
			mutated := map[string]any{}
			for key, item := range original {
				mutated[key] = item
			}
			mutated[field] = value
			path := savePresetJSON(t, mutated)
			output := filepath.Join(t.TempDir(), "keep.mp4")
			if err := os.WriteFile(output, []byte("existing output"), 0600); err != nil {
				t.Fatal(err)
			}
			var out, stderr bytes.Buffer
			if Main([]string{"render", dir, "--output", output, "--lrt-render-preset", path, "--overwrite"}, &out, &stderr) == 0 {
				t.Fatalf("accepted %s", field)
			}
			assertPreviewEVFileBytes(t, output, []byte("existing output"))
			if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
				t.Fatal("invalid preset changed source")
			}
		})
	}
	for _, raw := range []string{"null", "{}", "[]", "{broken}", `{"encoder":null}`, `{"lastRenderSettings":null}`} {
		path := filepath.Join(t.TempDir(), "bad.json")
		if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadLRTRenderPreset(path, ""); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	var out, stderr bytes.Buffer
	if Main([]string{"render", dir, "--output", filepath.Join(t.TempDir(), "no.mp4"), "--lrt-preset-name", "missing"}, &out, &stderr) == 0 || !strings.Contains(stderr.String(), "requires --lrt-render-preset") {
		t.Fatalf("accepted name without settings: %s", &stderr)
	}
}

func TestProResPresetProfilesActualOutput(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("FFmpeg unavailable")
	}
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe unavailable")
	}
	frames := cropFixture(t, 2)
	for _, tc := range []struct{ codec, profile, pixel string }{{"prores-proxy", "Proxy", "yuv422p10le"}, {"prores-lt", "LT", "yuv422p10le"}, {"prores-4444", "4444", "yuv444p12le"}} {
		output := filepath.Join(t.TempDir(), "profile.mov")
		if err := RenderSequence(frames, output, RenderOptions{FPS: 24, Quality: 90, Codec: tc.codec, FFmpegPath: ffmpeg}); err != nil {
			t.Fatal(err)
		}
		data, err := exec.Command(ffprobe, "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=profile,pix_fmt,nb_frames", "-of", "json", output).CombinedOutput()
		if err != nil {
			t.Fatalf("ffprobe: %s %v", data, err)
		}
		var result struct {
			Streams []struct {
				Profile string
				Pixel   string `json:"pix_fmt"`
				Frames  string `json:"nb_frames"`
			}
		}
		if err := json.Unmarshal(data, &result); err != nil {
			t.Fatal(err)
		}
		if len(result.Streams) != 1 || result.Streams[0].Profile != tc.profile || result.Streams[0].Pixel != tc.pixel || result.Streams[0].Frames != strconv.Itoa(len(frames)) {
			t.Fatalf("%s: %s", tc.codec, data)
		}
	}
	// Exercise the saved settings path through an actual encoder, including
	// native 1080 height and the separate global aspect constraint.
	path := savePresetJSON(t, map[string]any{"lastRenderSettings": renderPresetObservations(t)[0].Preset, "renderConstrainCrop": true, "renderAR": 1.5})
	output := filepath.Join(t.TempDir(), "native-preset.mp4")
	markerCommand(t, "render", filepath.Dir(frames[0].Image), "--output", output, "--ffmpeg", ffmpeg, "--lrt-render-preset", path, "--preset", "ultrafast")
	data, err := exec.Command(ffprobe, "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=codec_name,width,height,avg_frame_rate,nb_frames", "-of", "json", output).CombinedOutput()
	if err != nil {
		t.Fatalf("probe imported preset: %s %v", data, err)
	}
	var result struct {
		Streams []struct {
			Codec         string `json:"codec_name"`
			Width, Height int
			Rate          string `json:"avg_frame_rate"`
			Frames        string `json:"nb_frames"`
		}
	}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Streams) != 1 || result.Streams[0].Codec != "h264" || result.Streams[0].Width != 1620 || result.Streams[0].Height != 1080 || result.Streams[0].Rate != "30000/1001" || result.Streams[0].Frames != "2" {
		t.Fatalf("imported preset output: %s", data)
	}
}
