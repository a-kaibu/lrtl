package lrtl

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

type nativeSlowCase struct {
	name     string
	playback nativePlaybackCase
	spatial  int
	pan      bool
}

func nativeSlowCases() []nativeSlowCase {
	var cases []nativeSlowCase
	for _, mode := range []int{1, 2} {
		for _, speed := range []float64{.0625, .3, .5, .7, .9} {
			cases = append(cases, nativeSlowCase{name: fmt.Sprintf("mode%d-speed%g", mode, speed), playback: nativePlaybackCase{mode: mode, speed: speed}})
		}
		for _, spatial := range []int{1, 2, 3} {
			cases = append(cases, nativeSlowCase{name: fmt.Sprintf("mode%d-spatial%d", mode, spatial), playback: nativePlaybackCase{mode: mode, speed: .5}, spatial: spatial})
		}
		for _, speed := range []float64{.3, .5} {
			cases = append(cases, nativeSlowCase{name: fmt.Sprintf("pan-mode%d-speed%g", mode, speed), playback: nativePlaybackCase{mode: mode, speed: speed, blur: 2}, spatial: 3, pan: true})
		}
	}
	cases = append(cases, nativeSlowCase{name: "combined", playback: nativePlaybackCase{mode: 1, speed: .7, blur: 3, temporal: nativeTemporalCase{noise: 2, air: 2, luma: true, chroma: true}}, spatial: 3}, nativeSlowCase{name: "combined-strong", playback: nativePlaybackCase{mode: 2, speed: .5, blur: 2, temporal: nativeTemporalCase{noise: 4, air: 3, luma: true, chroma: true}}, spatial: 3})
	return cases
}

func nativeSlowCLI(tc nativeSlowCase) []string {
	return append(nativePlaybackCLI(tc.playback), "--sharpen="+strconv.FormatBool(tc.spatial&1 != 0), "--cas="+strconv.FormatBool(tc.spatial&2 != 0))
}

func nativeSlowReferenceGraph(t *testing.T, tc nativeSlowCase) string {
	graph := nativePlaybackReferenceGraph(t, tc.playback, 11)
	spatial := ""
	if tc.spatial&1 != 0 {
		spatial += ",unsharp=luma_msize_x=7:luma_msize_y=7:luma_amount=0.4"
	}
	if tc.spatial&2 != 0 {
		spatial += ",cas=strength=0.90"
	}
	if at := strings.Index(graph, ",trim="); at >= 0 {
		return graph[:at] + spatial + graph[at:]
	}
	return graph + spatial
}

func TestNativeSlowPlanesAgainstFFmpeg(t *testing.T) {
	executable := os.Getenv("LRTL_COLOR_FFMPEG")
	if executable == "" {
		t.Skip("set LRTL_COLOR_FFMPEG for native 8-bit slow interpolation comparisons")
	}
	fixtures := map[bool]string{false: nativePlaybackFixture(t, 11), true: motionImageFixture(t, 11, "pan-x")}
	fake, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(t.TempDir(), "capture")
	t.Setenv("LRTL_TEST_FFMPEG_CAPTURE", capture)
	hashes := map[string][32]byte{}
	for _, tc := range nativeSlowCases() {
		t.Run(tc.name, func(t *testing.T) {
			dir := fixtures[tc.pan]
			args := []string{"render", dir, "--output", filepath.Join(t.TempDir(), "go.mp4"), "--output-color", "lrt-7.5.5", "--ffmpeg", fake, "--fps", "24"}
			markerCommand(t, append(args, nativeSlowCLI(tc)...)...)
			got := denoiseRead(t, capture)
			want := nativeColorCommand(t, executable, nil, "-v", "error", "-cpuflags", "0", "-framerate", "24", "-i", filepath.Join(dir, "frame%02d.png"), "-filter_complex", nativeSlowReferenceGraph(t, tc), "-pix_fmt", "yuv444p", "-f", "rawvideo", "pipe:1")
			t.Logf("REFERENCE name=%s bytes=%d sha256=%x", tc.name, len(want), sha256.Sum256(want))
			if !bytes.Equal(got, want) {
				differences, maxDelta := 0, 0
				for i := 0; i < min(len(got), len(want)); i++ {
					d := int(got[i]) - int(want[i])
					if d != 0 {
						differences++
						if d < 0 {
							d = -d
						}
						maxDelta = max(maxDelta, d)
					}
				}
				t.Fatalf("native slow %s: bytes%d/%d differing%d max%d hashes%x/%x", tc.name, len(got), len(want), differences, maxDelta, sha256.Sum256(got), sha256.Sum256(want))
			}
			var flags []string
			if err := json.Unmarshal(denoiseRead(t, capture+".json"), &flags); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(strings.Join(flags, " "), "-pixel_format yuv444p ") {
				t.Fatal("slow native output must remain 8-bit", flags)
			}
			hashes[tc.name] = sha256.Sum256(got)
		})
	}
	if hashes["pan-mode1-speed0.5"] == hashes["pan-mode2-speed0.5"] {
		t.Fatal("motion compensation did not differ from blending")
	}
}

func TestNativeSlowActualVideos(t *testing.T) {
	executable, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("FFmpeg required")
	}
	fixtures := map[bool]string{false: nativePlaybackFixture(t, 11), true: motionImageFixture(t, 11, "pan-x")}
	for _, tc := range nativeSlowCases() {
		t.Run(tc.name, func(t *testing.T) {
			verifyNativeFilteredVideos(t, executable, fixtures[tc.pan], nativeSlowCLI(tc), nativeSlowReferenceGraph(t, tc))
		})
	}
}

func TestNativeSlowOfflineReferences(t *testing.T) {
	var rows []struct {
		Name, Hash string
		Bytes      int
	}
	if err := json.Unmarshal(denoiseRead(t, "testdata/lrt-native-slow-color-8.0.1.json"), &rows); err != nil || len(rows) != 22 {
		t.Fatalf("expected 22 slow color references: %v", err)
	}
	fixtures := map[bool]string{false: nativePlaybackFixture(t, 11), true: motionImageFixture(t, 11, "pan-x")}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(t.TempDir(), "capture")
	t.Setenv("LRTL_TEST_FFMPEG_CAPTURE", capture)
	for _, tc := range nativeSlowCases() {
		t.Run(tc.name, func(t *testing.T) {
			args := []string{"render", fixtures[tc.pan], "--output", filepath.Join(t.TempDir(), "go.mp4"), "--output-color", "lrt-7.5.5", "--ffmpeg", executable, "--fps", "24"}
			markerCommand(t, append(args, nativeSlowCLI(tc)...)...)
			got := denoiseRead(t, capture)
			found := false
			for _, row := range rows {
				if row.Name == tc.name {
					found = true
					if len(got) != row.Bytes || fmt.Sprintf("%x", sha256.Sum256(got)) != row.Hash {
						t.Fatalf("native slow reference mismatch: %s bytes%d hash%x", tc.name, len(got), sha256.Sum256(got))
					}
				}
			}
			if !found {
				t.Fatal("missing native slow reference", tc.name)
			}
		})
	}
}

func TestNativeWorkingEightBitDepthAndValidation(t *testing.T) {
	plane := make([]uint16, 64)
	plane[4*8+4] = 128
	eight := sharpenLumaDepth(plane, 8, 8, 8)
	ten := sharpenLumaDepth(plane, 8, 8, 10)
	if eight[36] != 173 || ten[36] != 128 {
		t.Fatalf("native unsharp depth convention changed: %d/%d", eight[36], ten[36])
	}
	frame := make([]byte, 64*6)
	for i := 0; i < 64; i++ {
		binary.LittleEndian.PutUint16(frame[i*6:], plane[i])
	}
	filter := nativeSpatialFilter{8, 8, true, false}
	got, err := filter.workingFrameDepth(frame, 8)
	if err != nil || len(got) != 64*3 || got[36] != 173 {
		t.Fatal("8-bit encoder packing or sharpening failed", err)
	}
	if _, err := filter.workingFrameDepth(frame, 12); err == nil {
		t.Fatal("accepted invalid spatial depth")
	}
	if _, err := weightedWorkingFramesDepth([][]byte{frame}, []float32{1}, 12); err == nil {
		t.Fatal("accepted invalid blend depth")
	}
	for i := 0; i < len(frame); i += 2 {
		binary.LittleEndian.PutUint16(frame[i:], 1023)
	}
	clipped, err := weightedWorkingFramesDepth([][]byte{frame}, []float32{1}, 8)
	if err != nil || binary.LittleEndian.Uint16(clipped) != 255 {
		t.Fatal("8-bit blur clipping failed", err)
	}
	for _, encoding := range []ffmpegFrameEncoding{{outputColor: "lrt-7.5.5", spatial: &filter, workingYUV: true, workingDepth: 12}, {outputColor: "lrt-7.5.5", spatial: &filter, workingDepth: 8}} {
		_, err := newFFmpegFrameWriter("missing-ffmpeg", filepath.Join(t.TempDir(), "invalid.mov"), "mov", "prores", 8, 8, frameRate{24, 1}, 1, 0, "ultrafast", "", videoFade{}, "full", "", "", encoding)
		if err == nil || !strings.Contains(err.Error(), "working depth") {
			t.Fatal("invalid depth reached encoder", err)
		}
	}
}

func TestNativeSlowPresetBatchFallbackAndFailure(t *testing.T) {
	dir := nativePlaybackFixture(t, 11)
	before := batchFileSnapshot(t, dir)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(t.TempDir(), "capture")
	t.Setenv("LRTL_TEST_FFMPEG_CAPTURE", capture)
	tc := nativeSlowCases()[21]
	args := []string{"render", dir, "--output", filepath.Join(t.TempDir(), "direct.mp4"), "--output-color", "lrt-7.5.5", "--ffmpeg", executable, "--fps", "24"}
	markerCommand(t, append(args, nativeSlowCLI(tc)...)...)
	want := denoiseRead(t, capture)
	preset := lrtRenderPreset{Encoder: "h264", FrameRate: "24", OutputResV: 32, Speed: .5, Interpolation: 2, MotionBlur: true, MotionBlurLevel: 2, Denoise: true, DenoiseLevel: 4, RemoveAirplanes: true, RemoveAirplanesLevel: 3, DeflickerLuma: true, DeflickerChroma: true, Sharpen: true, CAS: true}
	path := savePresetJSON(t, preset)
	presetBefore := denoiseRead(t, path)
	markerCommand(t, "render", dir, "--output", filepath.Join(t.TempDir(), "preset.mp4"), "--lrt-render-preset", path, "--ffmpeg", executable)
	if !bytes.Equal(want, denoiseRead(t, capture)) {
		t.Fatal("native slow preset changed 8-bit planes")
	}
	markerCommand(t, "batch-render", dir, "--output", t.TempDir(), "--", "--lrt-render-preset", path, "--ffmpeg", executable)
	if !bytes.Equal(want, denoiseRead(t, capture)) {
		t.Fatal("native slow batch changed 8-bit planes")
	}
	assertPreviewEVFileBytes(t, path, presetBefore)
	for _, extra := range [][]string{{"--watermark", "TEST"}, {"--fade-out", "1"}, {"--output-height", "31"}, {"--output-color", "standard"}} {
		args := []string{"render", dir, "--output", filepath.Join(t.TempDir(), "fallback.mp4"), "--output-color", "lrt-7.5.5", "--ffmpeg", executable, "--interpolation", "1", "--speed", ".5", "--motion-blur", "2"}
		markerCommand(t, append(args, extra...)...)
		var flags []string
		if err := json.Unmarshal(denoiseRead(t, capture+".json"), &flags); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(strings.Join(flags, " "), "-pixel_format rgb48le") {
			t.Fatal("unsupported native slow combination used planar input", extra, flags)
		}
	}
	for path, data := range before {
		if !bytes.Equal(data, denoiseRead(t, path)) {
			t.Fatal("source changed", path)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "frame10.png"), []byte("corrupt future source"), 0600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "keep.mp4")
	if err := os.WriteFile(output, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if Main([]string{"render", dir, "--output", output, "--overwrite", "--lrt-render-preset", path, "--ffmpeg", executable}, &stdout, &stderr) == 0 {
		t.Fatal("ignored corrupt native slow lookahead")
	}
	assertPreviewEVFileBytes(t, output, []byte("original"))
	entries, err := os.ReadDir(filepath.Dir(output))
	if err != nil || len(entries) != 1 {
		t.Fatal("failed native slow render left temporary files", err)
	}
}

func FuzzNativeSlowWorkingColor(f *testing.F) {
	f.Add([]byte{0, 255, 17, 128, 63}, uint8(1))
	f.Add([]byte{255, 255, 0, 0, 127, 128}, uint8(2))
	f.Fuzz(func(t *testing.T, data []byte, selector uint8) {
		if len(data) == 0 || len(data) > 4096 {
			return
		}
		mode := int(selector%2) + 1
		const width, height, count = 32, 32, 5
		frames := make([][]byte, count)
		for n := range frames {
			rgb := make([]byte, width*height*6)
			for i := range rgb {
				rgb[i] = data[(i+n*7)%len(data)]
			}
			var err error
			frames[n], err = nativeSDRWorkingFrame(rgb, width, height)
			if err != nil {
				t.Fatal(err)
			}
		}
		before := bytes.Clone(frames[0])
		calls := make([]int, count)
		stream, err := newPlaybackFrameStream(width, height, count, 0, mode, .5, frameRate{24, 1}, "24", func(i int) ([]byte, error) { calls[i]++; return frames[i], nil })
		if err != nil {
			t.Fatal(err)
		}
		stream.color = true
		filter := nativeSpatialFilter{width, height, true, true}
		var previous []byte
		for target := 0; target < stream.count; target++ {
			frame, _, err := stream.frame(target)
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < len(frame); i += 2 {
				if binary.LittleEndian.Uint16(frame[i:]) > 255 {
					t.Fatal("interpolation returned samples beyond 8-bit")
				}
			}
			if previous == nil {
				previous = frame
			}
			blended, err := weightedWorkingFramesDepth([][]byte{previous, frame}, []float32{.354, 1}, 8)
			if err != nil {
				t.Fatal(err)
			}
			out, err := filter.workingFrameDepth(blended, 8)
			if err != nil || len(out) != width*height*3 {
				t.Fatal("invalid native 8-bit encoder frame", err)
			}
			previous = frame
			if len(stream.cache) > 3 || stream.motion != nil && len(stream.motion.fields) > 2 {
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
		if !bytes.Equal(frames[0], before) {
			t.Fatal("native interpolation changed source")
		}
	})
}
