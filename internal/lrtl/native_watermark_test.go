package lrtl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeWatermarkMeasuredFilters(t *testing.T) {
	var rows []struct {
		Width        int
		Text, Filter string
	}
	if err := json.Unmarshal(denoiseRead(t, "testdata/lrt-watermark-filter-7.5.5.json"), &rows); err != nil || len(rows) != 105 {
		t.Fatalf("expected 105 native filters: %v", err)
	}
	for _, row := range rows {
		if got := nativeWatermarkFilter(row.Text, row.Width, "/Windows/Fonts/arial.ttf"); got != row.Filter {
			t.Fatalf("width%d text%q:\n%s\n%s", row.Width, row.Text, got, row.Filter)
		}
	}
}

func TestNativeWatermarkRecordedCallerVideos(t *testing.T) {
	if _, err := nativeWatermarkFont(); err != nil {
		t.Skip(err)
	}
	exe, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("FFmpeg required")
	}
	var rows []struct {
		Case  int
		Graph string
	}
	if err := json.Unmarshal(denoiseRead(t, "testdata/lrt-watermark-caller-7.5.5.json"), &rows); err != nil || len(rows) != 3 {
		t.Fatalf("expected three measured caller graphs: %v", err)
	}
	file, err := os.CreateTemp(".", ".lrtl-watermark-reference-*.srt")
	if err != nil {
		t.Fatal(err)
	}
	name := file.Name()
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(name) })
	dir := nativeWatermarkFixture(t, 17, 640, 320)
	nativeTimestampReferenceSRT(t, name, 17, 1, 3)
	for _, row := range rows {
		t.Run(fmt.Sprint(row.Case), func(t *testing.T) {
			color, watermark, caption, fade := strings.Index(row.Graph, "zscale="), strings.Index(row.Graph, ",drawtext="), strings.Index(row.Graph, ",subtitles="), strings.Index(row.Graph, ",fade=")
			if color < 0 || watermark < color || caption < watermark || fade < caption {
				t.Fatal("native overlay order changed", row.Graph)
			}
			preset := savePresetJSON(t, lrtRenderPreset{Encoder: "h264", FrameRate: "24", OutputResV: 320, Speed: 1, FadeOut: true, FadeTime: 1, Sharpen: row.Case > 0, MotionBlur: row.Case == 2, MotionBlurLevel: 3})
			flags := []string{"--lrt-render-preset", preset, "--watermark", "LRTL 2026", "--watermark-style", "lrt-7.5.5", "--timestamp-style", "lrt-7.5.5", "--timestamp-every", "3"}
			graph := strings.ReplaceAll(strings.TrimSuffix(row.Graph, "[v]"), "timestamp-reference.srt", filepath.Base(name))
			verifyNativeFinalVideos(t, exe, dir, flags, func(pix, videoRange string) string {
				return strings.ReplaceAll(graph, "range=limited:", "range="+videoRange+":")
			})
		})
	}
}

func nativeWatermarkFixture(t *testing.T, count, width, height int) string {
	t.Helper()
	dir := t.TempDir()
	for i := 0; i < count; i++ {
		path := filepath.Join(dir, fmt.Sprintf("frame%02d.png", i))
		rgb := nativeColorSource(width, height, "lowbits")
		for j := range rgb {
			rgb[j] += byte(i * 7)
		}
		img, err := imageFromRGB48(rgb, width, height)
		if err != nil {
			t.Fatal(err)
		}
		var encoded bytes.Buffer
		if err := png.Encode(&encoded, img); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, encoded.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		writeTestXMP(t, path, map[string]string{"exif:DateTimeOriginal": fmt.Sprintf("2026-01-02T03:04:%02dZ", i)})
	}
	return dir
}

func TestNativeWatermarkCLIAndValidation(t *testing.T) {
	if _, err := nativeWatermarkFont(); err != nil {
		t.Skip(err)
	}
	dir := nativeTimestampBurnFixture(t, 8)
	before := batchFileSnapshot(t, dir)
	fake, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(t.TempDir(), "capture")
	t.Setenv("LRTL_TEST_FFMPEG_CAPTURE", capture)
	args := []string{"--output-color", "lrt-7.5.5", "--ffmpeg", fake, "--watermark", "A;B:C\"D'E", "--watermark-style", "lrt-7.5.5", "--timestamp-style", "lrt-7.5.5", "--fade-out", ".2"}
	markerCommand(t, append([]string{"render", dir, "--output", filepath.Join(t.TempDir(), "out.mp4")}, args...)...)
	var command []string
	if err := json.Unmarshal(denoiseRead(t, capture+".json"), &command); err != nil {
		t.Fatal(err)
	}
	for i, arg := range command {
		if arg == "-vf" {
			graph := command[i+1]
			at := strings.Index(graph, "drawtext=")
			if at < strings.Index(graph, "zscale=") || at > strings.Index(graph, "subtitles=") || at > strings.Index(graph, ",fade=") || !strings.Contains(graph, "text='A\u00a0B\u00a0C\u00a0D\u00a0E'") {
				t.Fatal("invalid watermark graph", graph)
			}
		}
	}
	markerCommand(t, append([]string{"batch-render", dir, "--output", t.TempDir(), "--"}, args...)...)
	for _, extra := range [][]string{{"--watermark-style", "bad"}, {"--watermark-style", "lrt-7.5.5"}, {"--watermark-style", "lrt-7.5.5", "--output-color", "lrt-7.5.5", "--watermark-position", "center"}, {"--watermark-style", "lrt-7.5.5", "--output-color", "lrt-7.5.5", "--timestamp-overlay"}} {
		out := filepath.Join(t.TempDir(), "keep.mp4")
		if err := os.WriteFile(out, []byte("original"), 0600); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr bytes.Buffer
		if Main(append([]string{"render", dir, "--output", out, "--overwrite", "--watermark", "LRTL", "--ffmpeg", fake}, extra...), &stdout, &stderr) == 0 {
			t.Fatal("invalid watermark options accepted", extra)
		}
		assertPreviewEVFileBytes(t, out, []byte("original"))
		entries, err := os.ReadDir(filepath.Dir(out))
		if err != nil || len(entries) != 1 {
			t.Fatal("invalid watermark left temporary files", entries, err)
		}
	}
	for path, data := range before {
		assertPreviewEVFileBytes(t, path, data)
	}
	// A source failure after native subtitle staging must remove both temporary
	// video and subtitle files and retain the previous destination.
	if err := os.WriteFile(filepath.Join(dir, "frame07.png"), []byte("corrupt future source"), 0600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "keep.mp4")
	if err := os.WriteFile(out, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if Main(append([]string{"render", dir, "--output", out, "--overwrite"}, args...), &stdout, &stderr) == 0 {
		t.Fatal("ignored corrupt native overlay source")
	}
	assertPreviewEVFileBytes(t, out, []byte("original"))
	entries, err := os.ReadDir(filepath.Dir(out))
	if err != nil || len(entries) != 1 {
		t.Fatal("native overlays leaked temporary files", entries, err)
	}
}

func FuzzNativeWatermarkFilter(f *testing.F) {
	f.Add("LRTL 2026", uint16(1920))
	f.Add("A;B:C\"D'E", uint16(7681))
	f.Fuzz(func(t *testing.T, text string, width uint16) {
		if len(text) > 4096 {
			return
		}
		got := nativeWatermarkFilter(text, int(width), "font")
		if !strings.HasPrefix(got, ",drawtext=font:fontsize=") || !strings.HasSuffix(got, "'") {
			t.Fatal("invalid native watermark envelope")
		}
	})
}

func TestNativeWatermarkSavedSettings(t *testing.T) {
	preset := lrtRenderPreset{Encoder: "h264", FrameRate: "24", OutputResV: 32, Speed: 1, Name: "watermark"}
	settings := map[string]any{"lastRenderSettings": preset, "renderPresets": []lrtRenderPreset{preset}, "renderCopyrightEnabled": true, "renderCopyrightText": "LRTL 2026"}
	for _, name := range []string{"", "watermark"} {
		path := savePresetJSON(t, settings)
		saved, err := loadLRTRenderPreset(path, name)
		if err != nil {
			t.Fatal(err)
		}
		options, err := applyLRTRenderPreset(RenderOptions{}, saved, path, nil)
		if err != nil || options.Watermark != "LRTL 2026" || options.WatermarkStyle != "lrt-7.5.5" {
			t.Fatalf("lost saved copyright: %+v/%v", options, err)
		}
		options, err = applyLRTRenderPreset(RenderOptions{Watermark: "custom", WatermarkStyle: "standard"}, saved, path, map[string]bool{"watermark": true, "watermark-style": true})
		if err != nil || options.Watermark != "custom" || options.WatermarkStyle != "standard" {
			t.Fatalf("lost override: %+v/%v", options, err)
		}
	}
	settings["renderCopyrightEnabled"] = false
	path := savePresetJSON(t, settings)
	saved, err := loadLRTRenderPreset(path, "")
	if err != nil {
		t.Fatal(err)
	}
	options, err := applyLRTRenderPreset(RenderOptions{Watermark: "default"}, saved, path, nil)
	if err != nil || options.Watermark != "" {
		t.Fatalf("ignored saved disable: %+v/%v", options, err)
	}
	for _, bad := range []map[string]any{{"renderCopyrightEnabled": "yes"}, {"renderCopyrightEnabled": true}, {"renderCopyrightEnabled": true, "renderCopyrightText": 42}} {
		bad["lastRenderSettings"] = preset
		if _, err := loadLRTRenderPreset(savePresetJSON(t, bad), ""); err == nil {
			t.Fatal("invalid copyright settings accepted", bad)
		}
	}
	if _, err := nativeWatermarkFont(); err != nil {
		t.Skip(err)
	}
	settings["renderCopyrightEnabled"] = true
	path = savePresetJSON(t, settings)
	fake, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(t.TempDir(), "capture")
	t.Setenv("LRTL_TEST_FFMPEG_CAPTURE", capture)
	markerCommand(t, "render", nativePlaybackFixture(t, 3), "--output", filepath.Join(t.TempDir(), "video.mp4"), "--ffmpeg", fake, "--lrt-render-preset", path)
	if !bytes.Contains(denoiseRead(t, capture+".json"), []byte("drawtext=")) {
		t.Fatal("CLI ignored saved copyright")
	}
}

func TestNativeWatermarkPlaybackVideos(t *testing.T) {
	if _, err := nativeWatermarkFont(); err != nil {
		t.Skip(err)
	}
	exe, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("FFmpeg required")
	}
	dir := nativePlaybackFixture(t, 11)
	watermark := nativeWatermarkReferenceFilter(t, "LRTL")
	for i, tc := range []nativeSlowCase{
		{playback: nativePlaybackCase{speed: 1, blur: 2, temporal: nativeTemporalCase{noise: 4, air: 3, luma: true, chroma: true, spatial: true}}},
		{playback: nativePlaybackCase{speed: .5, mode: 1, blur: 2}, spatial: 3},
		{playback: nativePlaybackCase{speed: 1.5, mode: 2, blur: 2}, spatial: 3},
	} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			graph := nativeSlowReferenceGraph(t, tc)
			if i == 0 {
				graph = nativePlaybackReferenceGraph(t, tc.playback, 11)
			}
			flags := append(nativeSlowCLI(tc), "--watermark", "LRTL", "--watermark-style", "lrt-7.5.5")
			if i == 0 {
				flags = append(nativePlaybackCLI(tc.playback), "--watermark", "LRTL", "--watermark-style", "lrt-7.5.5")
			}
			verifyNativeFinalVideos(t, exe, dir, flags, func(pix, videoRange string) string {
				insert := "," + nativeSDRPlanarColorFilter(videoRange, pix) + watermark
				if at := strings.Index(graph, ",trim="); at >= 0 {
					return graph[:at] + insert + graph[at:]
				}
				return graph + insert
			})
		})
	}
}

func nativeWatermarkReferenceFilter(t *testing.T, text string) string {
	t.Helper()
	var rows []struct {
		Width        int
		Text, Filter string
	}
	if err := json.Unmarshal(denoiseRead(t, "testdata/lrt-watermark-filter-7.5.5.json"), &rows); err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.Width == 640 && row.Text == text {
			return row.Filter
		}
	}
	t.Fatal("missing measured watermark", text)
	return ""
}
