package lrtl

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func verifyNativeTimestampVideos(t *testing.T, exe, dir string, flags []string, graph string) {
	t.Helper()
	verifyNativeFinalVideos(t, exe, dir, flags, func(pix, videoRange string) string {
		at := strings.Index(graph, ",subtitles=")
		if at < 0 {
			t.Fatal("missing native timestamp reference")
		}
		return graph[:at] + "," + nativeSDRPlanarColorFilter(videoRange, pix) + graph[at:]
	})
}

func TestNativeTimestampBurnMeasuredGraphs(t *testing.T) {
	var rows []struct {
		Case  int
		Graph string
	}
	if err := json.Unmarshal(denoiseRead(t, "testdata/lrt-timestamp-style-7.5.5.json"), &rows); err != nil || len(rows) != 3 {
		t.Fatalf("expected three native graphs: %v", err)
	}
	for _, row := range rows {
		graph := row.Graph
		color, caption, fade := strings.Index(graph, "zscale="), strings.Index(graph, "subtitles="), strings.Index(graph, ",fade=")
		if color < 0 || caption < color || fade < caption {
			t.Fatal("native output ordering changed", graph)
		}
		if !strings.Contains(graph, "FontName=Arial.ttf,Fontsize=16,PrimaryColour=&He0e0e0") {
			t.Fatal("native subtitle style changed", graph)
		}
		if trim := strings.Index(graph, ",trim="); trim >= 0 && (trim < caption || trim > fade) {
			t.Fatal("native subtitle trim order changed", graph)
		}
	}
}

func TestNativeTimestampBurnRecordedCallerVideos(t *testing.T) {
	exe, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("FFmpeg required")
	}
	var rows []struct {
		Case  int
		Graph string
	}
	if err := json.Unmarshal(denoiseRead(t, "testdata/lrt-timestamp-style-7.5.5.json"), &rows); err != nil {
		t.Fatal(err)
	}
	file, err := os.CreateTemp(".", ".lrtl-native-reference-*.srt")
	if err != nil {
		t.Fatal(err)
	}
	name := file.Name()
	_ = file.Close()
	t.Cleanup(func() { _ = os.Remove(name) })
	dir := nativeTimestampBurnFixture(t, 17)
	nativeTimestampReferenceSRT(t, name, 17, 1, 3)
	for _, row := range rows {
		t.Run(fmt.Sprint(row.Case), func(t *testing.T) {
			preset := savePresetJSON(t, lrtRenderPreset{Encoder: "h264", FrameRate: "24", OutputResV: 32, Speed: 1, FadeOut: true, FadeTime: 1, Sharpen: row.Case > 0, MotionBlur: row.Case == 2, MotionBlurLevel: 3})
			flags := []string{"--lrt-render-preset", preset, "--timestamp-style", "lrt-7.5.5", "--timestamp-every", "3"}
			verifyNativeFinalVideos(t, exe, dir, flags, func(pix, videoRange string) string {
				graph := strings.TrimSuffix(row.Graph, "[v]")
				graph = strings.ReplaceAll(graph, "timestamp-reference.srt", filepath.Base(name))
				return strings.ReplaceAll(graph, "range=limited:", "range="+videoRange+":")
			})
		})
	}
}

func TestNativeTimestampBurnRawPlanes(t *testing.T) {
	exe, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("FFmpeg required")
	}
	dir := nativeTimestampBurnFixture(t, 17)
	fake, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.CreateTemp(".", ".lrtl-native-reference-*.srt")
	if err != nil {
		t.Fatal(err)
	}
	name := file.Name()
	_ = file.Close()
	t.Cleanup(func() { _ = os.Remove(name) })
	nativeTimestampReferenceSRT(t, name, 17, 1, 3)
	capture := filepath.Join(t.TempDir(), "capture")
	t.Setenv("LRTL_TEST_FFMPEG_CAPTURE", capture)
	tc := nativeTemporalCase{noise: 4, air: 3, luma: true, chroma: true, spatial: true}
	args := []string{"render", dir, "--output", filepath.Join(t.TempDir(), "go.mp4"), "--output-color", "lrt-7.5.5", "--ffmpeg", fake, "--fps", "24", "--video-range", "full", "--subsampling", "444", "--timestamp-style", "lrt-7.5.5", "--timestamp-every", "3", "--fade-out", ".5"}
	markerCommand(t, append(args, nativeTemporalCLI(tc)...)...)
	var command []string
	if err := json.Unmarshal(denoiseRead(t, capture+".json"), &command); err != nil {
		t.Fatal(err)
	}
	filter := ""
	for i, arg := range command {
		if arg == "-vf" {
			filter = command[i+1]
		}
	}
	filter = regexp.MustCompile(`subtitles=filename='[^']+'`).ReplaceAllString(filter, "subtitles=filename='"+filepath.Base(name)+"'")
	t.Log("Go final filter", filter)
	input := denoiseRead(t, capture)
	got := nativeFilterReferenceCommand(t, exe, input, "-v", "error", "-f", "rawvideo", "-pixel_format", "yuv444p10le", "-video_size", "64x32", "-framerate", "24", "-i", "pipe:0", "-vf", filter, "-pix_fmt", "yuv444p10le", "-f", "rawvideo", "pipe:1")
	graph := nativeTemporalReferenceGraph(t, tc)
	caption := ",setpts=N/(24)/TB,subtitles=filename='" + filepath.Base(name) + "':force_style='FontName=Arial.ttf,Fontsize=16,PrimaryColour=&He0e0e0'"
	at := strings.Index(graph, ",trim=")
	graph = graph[:at] + "," + nativeSDRPlanarColorFilter("full", "yuv444p10le") + caption + graph[at:] + ",fade=t=out:st=0.000:d=0.5"
	want := nativeFilterReferenceCommand(t, exe, nil, "-v", "error", "-framerate", "24", "-i", filepath.Join(dir, "frame%02d.png"), "-filter_complex", graph, "-pix_fmt", "yuv444p10le", "-f", "rawvideo", "pipe:1")
	if !bytes.Equal(got, want) {
		for i := 0; i < min(len(got), len(want)); i += 2 {
			if !bytes.Equal(got[i:i+2], want[i:i+2]) {
				t.Fatalf("caption differs frame%d plane%d x%d y%d values%d/%d", i/(64*32*6), i/(64*32*2)%3, i/2%64, i/2/64%32, binary.LittleEndian.Uint16(got[i:]), binary.LittleEndian.Uint16(want[i:]))
			}
		}
		t.Fatal("caption lengths differ")
	}
}

func nativeTimestampBurnFixture(t *testing.T, count int) string {
	t.Helper()
	dir := nativePlaybackFixture(t, count)
	for i := 0; i < count; i++ {
		writeTestXMP(t, filepath.Join(dir, fmt.Sprintf("frame%02d.png", i)), map[string]string{"exif:DateTimeOriginal": fmt.Sprintf("2026-01-02T03:04:%02dZ", i)})
	}
	return dir
}

func nativeTimestampReferenceSRT(t *testing.T, path string, count int, speed float64, every int) string {
	t.Helper()
	// Fixed 24-fps inputs use the independently measured native timecodes.
	// Build the reference cues outside the production SRT writer.
	clock, err := newNativeTimestampClock("24", speed)
	if err != nil {
		t.Fatal(err)
	}
	indices, err := nativeTimestampCueFrames(count, "24", speed, every)
	if err != nil {
		t.Fatal(err)
	}
	var data strings.Builder
	for i := 0; i < len(indices)-1; i++ {
		start, err := clock.timecode(indices[i])
		if err != nil {
			t.Fatal(err)
		}
		end, err := clock.timecode(indices[i+1])
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&data, "%d\n%s --> %s\n2026-01-02 03:04:%02d\n\n", i+1, start, end, indices[i])
	}

	if err := os.WriteFile(path, []byte(data.String()), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestNativeTimestampBurnActualVideos(t *testing.T) {
	exe, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("FFmpeg required")
	}
	reference, err := os.CreateTemp(".", ".lrtl-native-reference-*.srt")
	if err != nil {
		t.Fatal(err)
	}
	name := reference.Name()
	if err := reference.Close(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(name) })
	dir := nativeTimestampBurnFixture(t, 17)
	for _, tc := range []nativeTemporalCase{{name: "plain"}, {name: "spatial", spatial: true}, {name: "combined", noise: 4, air: 3, luma: true, chroma: true, spatial: true}} {
		t.Run(tc.name, func(t *testing.T) {
			nativeTimestampReferenceSRT(t, name, 17, 1, 3)
			graph := nativeTemporalReferenceGraph(t, tc)
			caption := ",setpts=N/(24)/TB,subtitles=filename='" + filepath.Base(name) + "':force_style='FontName=Arial.ttf,Fontsize=16,PrimaryColour=&He0e0e0'"
			if at := strings.Index(graph, ",trim="); at >= 0 {
				graph = graph[:at] + caption + graph[at:]
			} else {
				graph += caption
			}
			count := 17
			if tc.air > 0 {
				count = 12
			}
			fade, err := planVideoFade(count, frameRate{24, 1}, "24", .5)
			if err != nil {
				t.Fatal(err)
			}
			graph += fmt.Sprintf(",fade=t=out:st=%.3f:d=%g", fade.start, float32(fade.duration))
			flags := append(nativeTemporalCLI(tc), "--timestamp-style", "lrt-7.5.5", "--timestamp-every", "3", "--fade-out", ".5")
			verifyNativeTimestampVideos(t, exe, dir, flags, graph)
		})
	}
}

func TestNativeTimestampBurnPlaybackActualVideos(t *testing.T) {
	exe, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("FFmpeg required")
	}
	file, err := os.CreateTemp(".", ".lrtl-native-reference-*.srt")
	if err != nil {
		t.Fatal(err)
	}
	name := file.Name()
	_ = file.Close()
	t.Cleanup(func() { _ = os.Remove(name) })
	dir := nativeTimestampBurnFixture(t, 11)
	for _, mode := range []int{1, 2} {
		for _, speed := range []float64{.5, 1.5} {
			t.Run(fmt.Sprintf("mode%d-speed%g", mode, speed), func(t *testing.T) {
				tc := nativeSlowCase{playback: nativePlaybackCase{mode: mode, speed: speed, blur: 2}, spatial: 3}
				graph := nativeSlowReferenceGraph(t, tc)
				if speed >= 1 {
					tc.playback.temporal.spatial = true
					graph = nativePlaybackReferenceGraph(t, tc.playback, 11)
				}
				base := nativeFilterReferenceCommand(t, exe, nil, "-v", "error", "-framerate", "24", "-i", filepath.Join(dir, "frame%02d.png"), "-filter_complex", graph, "-pix_fmt", "yuv444p10le", "-f", "rawvideo", "pipe:1")
				fade, err := planVideoFade(len(base)/(64*32*6), frameRate{24, 1}, "24", .5)
				if err != nil {
					t.Fatal(err)
				}
				nativeTimestampReferenceSRT(t, name, 11, speed, 3)
				caption := ",subtitles=filename='" + filepath.Base(name) + "':force_style='FontName=Arial.ttf,Fontsize=16,PrimaryColour=&He0e0e0'"
				if at := strings.Index(graph, ",trim="); at >= 0 {
					graph = graph[:at] + caption + graph[at:]
				} else {
					graph += caption
				}
				graph += fmt.Sprintf(",fade=t=out:st=%.3f:d=%g", fade.start, float32(fade.duration))
				flags := append(nativeSlowCLI(tc), "--timestamp-style", "lrt-7.5.5", "--timestamp-every", "3", "--fade-out", ".5")
				verifyNativeTimestampVideos(t, exe, dir, flags, graph)
			})
		}
	}
}

func TestNativeTimestampBurnLargerFrames(t *testing.T) {
	exe, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("FFmpeg required")
	}
	dir := t.TempDir()
	for frame := 0; frame < 4; frame++ {
		img, err := imageFromRGB48(nativeColorSource(320, 180, "lowbits"), 320, 180)
		if err != nil {
			t.Fatal(err)
		}
		var data bytes.Buffer
		if err := png.Encode(&data, img); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, fmt.Sprintf("frame%02d.png", frame))
		if err := os.WriteFile(path, data.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		writeTestXMP(t, path, map[string]string{"exif:DateTimeOriginal": fmt.Sprintf("2026-01-02T03:04:%02dZ", frame)})
	}
	file, err := os.CreateTemp(".", ".lrtl-native-reference-*.srt")
	if err != nil {
		t.Fatal(err)
	}
	name := file.Name()
	_ = file.Close()
	t.Cleanup(func() { _ = os.Remove(name) })
	nativeTimestampReferenceSRT(t, name, 4, 1, 1)
	graph := nativeTemporalReferenceGraph(t, nativeTemporalCase{}) + ",subtitles=filename='" + filepath.Base(name) + "':force_style='FontName=Arial.ttf,Fontsize=16,PrimaryColour=&He0e0e0'"
	verifyNativeTimestampVideos(t, exe, dir, []string{"--timestamp-style", "lrt-7.5.5", "--timestamp-every", "1"}, graph)
}

func TestNativeTimestampBurnValidationAndCleanup(t *testing.T) {
	dir := nativeTimestampBurnFixture(t, 8)
	root := t.TempDir()
	out := filepath.Join(root, "video.mp4")
	for _, extra := range [][]string{{"--timestamp-style", "bad"}, {"--timestamp-every", "3"}, {"--timestamp-style", "lrt-7.5.5"}, {"--timestamp-style", "lrt-7.5.5", "--output-color", "lrt-7.5.5", "--watermark", "x"}, {"--timestamp-style", "lrt-7.5.5", "--output-color", "lrt-7.5.5", "--timestamp-every", "0"}} {
		if err := os.WriteFile(out, []byte("keep"), 0600); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr bytes.Buffer
		args := append([]string{"render", dir, "--output", out, "--overwrite"}, extra...)
		if Main(args, &stdout, &stderr) == 0 {
			t.Fatal("invalid native timestamp options accepted", extra)
		}
		if string(denoiseRead(t, out)) != "keep" {
			t.Fatal("failure changed existing video")
		}
		files, _ := filepath.Glob(filepath.Join(root, ".lrtl-*"))
		if len(files) > 0 {
			t.Fatal("native timestamp temporary files leaked", files)
		}
	}
	for _, name := range []string{"bad'name.srt", "bad,name.srt"} {
		if _, err := (nativeTimestampBurn{file: name}).filter(); err == nil {
			t.Fatal("unsafe name accepted")
		}
	}
	if _, err := (nativeTimestampBurn{file: "safe.srt", start: -1}).filter(); err == nil {
		t.Fatal("negative subtitle offset accepted")
	}
}

func TestNativeTimestampBurnPresetBatchPaths(t *testing.T) {
	exe, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("FFmpeg required")
	}
	dir := nativeTimestampBurnFixture(t, 8)
	before := batchFileSnapshot(t, dir)
	root := filepath.Join(t.TempDir(), "space, quote' [日時]")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	preset := savePresetJSON(t, lrtRenderPreset{Encoder: "h264", FrameRate: "24", OutputResV: 32, Speed: 1, Sharpen: true, CAS: true, FadeOut: true, FadeTime: 1})
	output := filepath.Join(root, "native.mp4")
	flags := []string{"--ffmpeg", exe, "--lrt-render-preset", preset, "--timestamp-style", "lrt-7.5.5", "--timestamp-every", "3", "--crf", "0", "--preset", "ultrafast"}
	markerCommand(t, append([]string{"render", dir, "--output", output}, flags...)...)
	decode := func(path string) []byte {
		return nativeColorCommand(t, exe, nil, "-v", "error", "-i", path, "-pix_fmt", "yuv420p", "-f", "rawvideo", "pipe:1")
	}
	want := decode(output)
	batch := filepath.Join(root, "batch")
	markerCommand(t, append([]string{"batch-render", dir, "--output", batch, "--"}, flags...)...)
	entries, err := os.ReadDir(batch)
	var videos []string
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) == ".mp4" {
			videos = append(videos, filepath.Join(batch, entry.Name()))
		}
	}
	if err != nil || len(videos) != 1 {
		t.Fatal("batch native timestamp output", videos, err)
	}
	if !bytes.Equal(want, decode(videos[0])) {
		t.Fatal("batch changed native timestamp pixels")
	}
	// Changing only the encoder's working directory must retain the caller's
	// interpretation of relative video/audio paths.
	audio := filepath.Join(root, "tone.wav")
	nativeColorCommand(t, exe, nil, "-v", "error", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000", "-t", "0.5", "-c:a", "pcm_s16le", audio)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relAudio, err := filepath.Rel(cwd, audio)
	if err != nil {
		t.Fatal(err)
	}
	relOutput, err := filepath.Rel(cwd, filepath.Join(root, "with-audio.mp4"))
	if err != nil {
		t.Fatal(err)
	}
	audioFlags := append(append([]string(nil), flags...), "--audio", relAudio)
	markerCommand(t, append([]string{"render", dir, "--output", relOutput}, audioFlags...)...)
	if !bytes.Equal(want, decode(relOutput)) {
		t.Fatal("relative audio/output paths changed native overlay")
	}
	for _, folder := range []string{root, batch} {
		files, err := os.ReadDir(folder)
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range files {
			if strings.HasPrefix(file.Name(), ".lrtl-") {
				t.Fatal("native timestamp file leaked", file.Name())
			}
		}
	}
	for path, data := range before {
		if !bytes.Equal(data, denoiseRead(t, path)) {
			t.Fatal("source changed", path)
		}
	}
}

func TestNativeTimestampBurnFailureKeepsVideo(t *testing.T) {
	dir := nativeTimestampBurnFixture(t, 8)
	fake, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("LRTL_TEST_FFMPEG_CAPTURE", filepath.Join(t.TempDir(), "capture"))
	if err := os.WriteFile(filepath.Join(dir, "frame07.png"), []byte("corrupt final frame"), 0600); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	output := filepath.Join(root, "keep.mp4")
	if err := os.WriteFile(output, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	args := []string{"render", dir, "--output", output, "--overwrite", "--output-color", "lrt-7.5.5", "--timestamp-style", "lrt-7.5.5", "--ffmpeg", fake}
	if Main(args, &stdout, &stderr) == 0 {
		t.Fatal("accepted corrupt source")
	}
	assertPreviewEVFileBytes(t, output, []byte("original"))
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 {
		t.Fatal("failed native overlay leaked files", entries, err)
	}
	if err := os.Remove(filepath.Join(dir, "frame07.png.xmp")); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if Main(args, &stdout, &stderr) == 0 {
		t.Fatal("accepted missing capture time")
	}
	assertPreviewEVFileBytes(t, output, []byte("original"))
	entries, err = os.ReadDir(root)
	if err != nil || len(entries) != 1 {
		t.Fatal("metadata failure leaked files", entries, err)
	}
}

func FuzzNativeTimestampBurnFilter(f *testing.F) {
	f.Add("safe.srt", uint16(1), true)
	f.Add("unsafe'path.srt", uint16(0), false)
	f.Fuzz(func(t *testing.T, name string, offset uint16, fadeOn bool) {
		if len(name) > 256 {
			return
		}
		fade := videoFade{}
		if fadeOn {
			fade = videoFade{.125, .5}
		}
		graph, err := (nativeTimestampBurn{file: name, start: int(offset)}).filter()
		graph += nativeEncoderFadeFilter(fade)
		if err != nil || name == "" {
			return
		}
		base := filepath.Base(name)
		if !strings.Contains(graph, "subtitles=filename='"+base+"'") {
			t.Fatal("subtitle filename lost")
		}
		if strings.Contains(graph, "setparams=range=full:") {
			t.Fatal("native overlay must retain the final encoder range")
		}
		if strings.Count(graph, "subtitles=") != 1 {
			t.Fatal("unexpected filter injection")
		}
		if offset > 0 && !strings.Contains(graph, ",setpts=PTS-STARTPTS") {
			t.Fatal("subtitle clock not restored")
		}
	})
}
