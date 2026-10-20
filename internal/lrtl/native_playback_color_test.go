package lrtl

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

type nativePlaybackCase struct {
	name       string
	blur, mode int
	speed      float64
	temporal   nativeTemporalCase
}

func nativePlaybackCases() []nativePlaybackCase {
	var cases []nativePlaybackCase
	for _, window := range []int{2, 3, 4, 5, 10, 15, 20} {
		cases = append(cases, nativePlaybackCase{name: fmt.Sprintf("blur%d", window), blur: window, speed: 1, temporal: nativeTemporalCase{spatial: window > 5}})
	}
	for _, mode := range []int{1, 2} {
		for _, speed := range []float64{1.1, 1.5, 2, 2.3, 4, 16} {
			cases = append(cases, nativePlaybackCase{name: fmt.Sprintf("mode%d-speed%g", mode, speed), mode: mode, speed: speed})
		}
	}
	cases = append(cases, nativePlaybackCase{"combined", 3, 1, 1.5, nativeTemporalCase{noise: 2, air: 2, luma: true, chroma: true, spatial: true}}, nativePlaybackCase{"combined-strong", 2, 2, 2.3, nativeTemporalCase{noise: 4, air: 3, luma: true, chroma: true, spatial: true}}, nativePlaybackCase{"slow-duplicate", 5, 0, .5, nativeTemporalCase{spatial: true}})
	return cases
}

func nativePlaybackCLI(tc nativePlaybackCase) []string {
	return append(nativeTemporalCLI(tc.temporal), "--motion-blur", strconv.Itoa(tc.blur), "--interpolation", strconv.Itoa(tc.mode), "--speed", strconv.FormatFloat(tc.speed, 'f', -1, 64))
}

func nativePlaybackReferenceGraph(t *testing.T, tc nativePlaybackCase) string {
	t.Helper()
	graph := nativeTemporalReferenceFilters(t, tc.temporal)
	var rows []struct {
		FPS    string
		Speed  float64
		Mode   int
		Filter string
	}
	if err := json.Unmarshal(denoiseRead(t, "testdata/lrt-interpolation-filter-7.5.5.json"), &rows); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range rows {
		if row.FPS == "24" && row.Speed == tc.speed && row.Mode == tc.mode {
			graph += row.Filter
			found = true
			break
		}
	}
	if !found {
		t.Fatal("missing native playback helper", tc.name)
	}
	if tc.blur > 0 {
		var weights []string
		for i := 1; i <= tc.blur; i++ {
			weights = append(weights, fmt.Sprintf("%.3f", math.Pow(float64(i)/float64(tc.blur), 1.5)))
		}
		graph += fmt.Sprintf(",tmix=frames=%d:weights='%s'", tc.blur, strings.Join(weights, " "))
	}
	if tc.temporal.spatial {
		graph += ",unsharp=luma_msize_x=7:luma_msize_y=7:luma_amount=0.4,cas=strength=0.90"
	}
	head, tail := tc.blur, 0
	if tc.temporal.air > 0 {
		head = max(head, 1)
		tail = 1
	}
	if head > 0 || tail > 0 {
		graph += fmt.Sprintf(",trim=start_frame=%d:end_frame=%d,setpts=PTS-STARTPTS", head, int(float32(41)/float32(tc.speed))-tail)
	}
	return graph
}

func TestNativePlaybackPlanesAgainstFFmpeg(t *testing.T) {
	executable := os.Getenv("LRTL_COLOR_FFMPEG")
	if executable == "" {
		t.Skip("set LRTL_COLOR_FFMPEG for native playback YUV comparisons")
	}
	dir := nativePlaybackFixture(t, 41)
	fake, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(t.TempDir(), "capture")
	t.Setenv("LRTL_TEST_FFMPEG_CAPTURE", capture)
	for _, tc := range nativePlaybackCases() {
		t.Run(tc.name, func(t *testing.T) {
			args := []string{"render", dir, "--output", filepath.Join(t.TempDir(), "go.mp4"), "--output-color", "lrt-7.5.5", "--ffmpeg", fake, "--fps", "24"}
			markerCommand(t, append(args, nativePlaybackCLI(tc)...)...)
			got := denoiseRead(t, capture)
			want := nativeColorCommand(t, executable, nil, "-v", "error", "-cpuflags", "0", "-framerate", "24", "-i", filepath.Join(dir, "frame%02d.png"), "-filter_complex", nativePlaybackReferenceGraph(t, tc), "-pix_fmt", "yuv444p10le", "-f", "rawvideo", "pipe:1")
			t.Logf("REFERENCE name=%s bytes=%d sha256=%x", tc.name, len(want), sha256.Sum256(want))
			if !bytes.Equal(got, want) {
				differences, maxDelta := 0, 0
				for i := 0; i+1 < min(len(got), len(want)); i += 2 {
					d := int(binary.LittleEndian.Uint16(got[i:])) - int(binary.LittleEndian.Uint16(want[i:]))
					if d != 0 {
						differences++
						if d < 0 {
							d = -d
						}
						maxDelta = max(maxDelta, d)
					}
				}
				t.Fatalf("playback %s count%d/%d differing%d max%d hashes%x/%x", tc.name, len(got), len(want), differences, maxDelta, sha256.Sum256(got), sha256.Sum256(want))
			}
		})
	}
}

func TestWeightedWorkingFramesValidation(t *testing.T) {
	pixel := func(v uint16) []byte {
		raw := make([]byte, 6)
		for i := 0; i < 6; i += 2 {
			binary.LittleEndian.PutUint16(raw[i:], v)
		}
		return raw
	}
	for _, tc := range []struct {
		frames  [][]byte
		weights []float32
	}{{nil, nil}, {[][]byte{nil}, []float32{1}}, {[][]byte{pixel(0)}, nil}, {[][]byte{{1}}, []float32{1}}, {[][]byte{pixel(0), {1}}, []float32{1, 1}}, {[][]byte{pixel(0)}, []float32{-1}}, {[][]byte{pixel(0)}, []float32{float32(math.NaN())}}, {[][]byte{pixel(0)}, []float32{float32(math.Inf(1))}}, {[][]byte{pixel(0)}, []float32{0}}, {[][]byte{pixel(0), pixel(0)}, []float32{math.MaxFloat32, math.MaxFloat32}}} {
		if _, err := weightedWorkingFrames(tc.frames, tc.weights); err == nil {
			t.Fatal("accepted invalid native blend")
		}
	}
	for _, tc := range []struct {
		weights []float32
		want    uint16
	}{{[]float32{1, 1}, 1}, {[]float32{1, 3}, 1}, {[]float32{3, 1}, 0}} {
		got, err := weightedWorkingFrames([][]byte{pixel(0), pixel(1)}, tc.weights)
		if err != nil || binary.LittleEndian.Uint16(got) != tc.want {
			t.Fatalf("rounding %v: %x %v", tc.weights, got, err)
		}
	}
	got, err := weightedWorkingFrames([][]byte{pixel(65535)}, []float32{1})
	if err != nil || binary.LittleEndian.Uint16(got) != 1023 {
		t.Fatal("native range clipping failed")
	}
}

func TestNativePlaybackOfflineReferences(t *testing.T) {
	var rows []struct {
		Name, Hash string
		Bytes      int
	}
	if err := json.Unmarshal(denoiseRead(t, "testdata/lrt-native-playback-color-8.0.1.json"), &rows); err != nil || len(rows) != 22 {
		t.Fatalf("expected 22 native playback references: %v", err)
	}
	dir := nativePlaybackFixture(t, 41)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(t.TempDir(), "capture")
	t.Setenv("LRTL_TEST_FFMPEG_CAPTURE", capture)
	for _, tc := range nativePlaybackCases() {
		t.Run(tc.name, func(t *testing.T) {
			args := []string{"render", dir, "--output", filepath.Join(t.TempDir(), "go.mp4"), "--output-color", "lrt-7.5.5", "--ffmpeg", executable, "--fps", "24"}
			markerCommand(t, append(args, nativePlaybackCLI(tc)...)...)
			got := denoiseRead(t, capture)
			found := false
			for _, row := range rows {
				if row.Name == tc.name {
					found = true
					if len(got) != row.Bytes || fmt.Sprintf("%x", sha256.Sum256(got)) != row.Hash {
						t.Fatalf("native playback hash changed: %s bytes%d hash%x", tc.name, len(got), sha256.Sum256(got))
					}
				}
			}
			if !found {
				t.Fatal("missing playback reference", tc.name)
			}
		})
	}
}

func TestNativePlaybackActualVideos(t *testing.T) {
	executable, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("FFmpeg required")
	}
	dir := nativePlaybackFixture(t, 41)
	before := batchFileSnapshot(t, dir)
	for _, tc := range nativePlaybackCases() {
		t.Run(tc.name, func(t *testing.T) {
			verifyNativeFilteredVideos(t, executable, dir, nativePlaybackCLI(tc), nativePlaybackReferenceGraph(t, tc))
		})
	}
	for path, data := range before {
		if !bytes.Equal(data, denoiseRead(t, path)) {
			t.Fatal("source changed", path)
		}
	}
}

func TestNativePlaybackPresetBatchFallbackAndFailure(t *testing.T) {
	dir := nativePlaybackFixture(t, 41)
	before := batchFileSnapshot(t, dir)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(t.TempDir(), "capture")
	t.Setenv("LRTL_TEST_FFMPEG_CAPTURE", capture)
	tc := nativePlaybackCases()[19]
	args := []string{"render", dir, "--output", filepath.Join(t.TempDir(), "direct.mp4"), "--output-color", "lrt-7.5.5", "--ffmpeg", executable, "--fps", "24"}
	markerCommand(t, append(args, nativePlaybackCLI(tc)...)...)
	want := denoiseRead(t, capture)
	preset := lrtRenderPreset{Encoder: "h264", FrameRate: "24", OutputResV: 32, Speed: 1.5, Interpolation: 1, MotionBlur: true, MotionBlurLevel: 3, Denoise: true, DenoiseLevel: 2, RemoveAirplanes: true, RemoveAirplanesLevel: 2, DeflickerLuma: true, DeflickerChroma: true, Sharpen: true, CAS: true}
	path := savePresetJSON(t, preset)
	markerCommand(t, "render", dir, "--output", filepath.Join(t.TempDir(), "preset.mp4"), "--lrt-render-preset", path, "--ffmpeg", executable)
	if !bytes.Equal(want, denoiseRead(t, capture)) {
		t.Fatal("native playback preset differs from direct CLI")
	}
	markerCommand(t, "batch-render", dir, "--output", t.TempDir(), "--", "--lrt-render-preset", path, "--ffmpeg", executable)
	if !bytes.Equal(want, denoiseRead(t, capture)) {
		t.Fatal("native playback batch changed planes")
	}
	for _, extra := range [][]string{{"--interpolation", "1", "--speed", ".5"}, {"--interpolation", "2", "--speed", ".5"}, {"--watermark", "TEST"}, {"--fade-out", "1"}, {"--output-height", "31"}, {"--output-color", "standard"}} {
		args := []string{"render", dir, "--output", filepath.Join(t.TempDir(), "fallback.mp4"), "--output-color", "lrt-7.5.5", "--ffmpeg", executable, "--motion-blur", "3"}
		markerCommand(t, append(args, extra...)...)
		var flags []string
		if err := json.Unmarshal(denoiseRead(t, capture+".json"), &flags); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(strings.Join(flags, " "), "-pixel_format rgb48le") {
			t.Fatal("unsupported combination used native packed YUV", extra, flags)
		}
	}
	for path, data := range before {
		if !bytes.Equal(data, denoiseRead(t, path)) {
			t.Fatal("source modified", path)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "frame40.png"), []byte("corrupt dropped tail"), 0600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "keep.mp4")
	if err := os.WriteFile(output, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if Main([]string{"render", dir, "--output", output, "--overwrite", "--output-color", "lrt-7.5.5", "--interpolation", "1", "--speed", "16", "--ffmpeg", executable}, &stdout, &stderr) == 0 {
		t.Fatal("ignored corrupt dropped tail")
	}
	assertPreviewEVFileBytes(t, output, []byte("original"))
	entries, err := os.ReadDir(filepath.Dir(output))
	if err != nil || len(entries) != 1 {
		t.Fatal("failed playback left output files", err)
	}
	if _, err := weightedWorkingFrames([][]byte{{0, 0, 0, 0, 0, 0}}, []float32{math.SmallestNonzeroFloat32}); err == nil {
		t.Fatal("accepted overflowing weight reciprocal")
	}
}

func FuzzWeightedWorkingFrames(f *testing.F) {
	f.Add([]byte{0, 0, 255, 3, 0, 2}, uint8(2))
	f.Add([]byte{1, 0, 2, 0, 3, 0, 4, 0, 5, 0, 6, 0}, uint8(20))
	f.Fuzz(func(t *testing.T, raw []byte, window uint8) {
		if len(raw) == 0 || len(raw)%6 != 0 || len(raw) > 64*6 {
			return
		}
		n := int(window)%20 + 1
		frames := make([][]byte, n)
		for j := range frames {
			frames[j] = make([]byte, len(raw))
			for i := 0; i < len(raw); i += 2 {
				v := binary.LittleEndian.Uint16(raw[i:])
				binary.LittleEndian.PutUint16(frames[j][i:], uint16((int(v)+j*29)%1024))
			}
		}
		control := bytes.Clone(frames[0])
		got, err := weightedWorkingFrames(frames, lrtMotionBlurWeights(n))
		if err != nil || len(got) != len(raw) {
			t.Fatal("invalid working blend", err)
		}
		if !bytes.Equal(control, frames[0]) {
			t.Fatal("blend modified source")
		}
		for i := 0; i < len(got); i += 2 {
			if binary.LittleEndian.Uint16(got[i:]) > 1023 {
				t.Fatal("blend sample out of range")
			}
		}
	})
}

func TestNativePlaybackCropBeforeBlur(t *testing.T) {
	executable := os.Getenv("LRTL_COLOR_FFMPEG")
	if executable == "" {
		t.Skip("set LRTL_COLOR_FFMPEG to verify crop before native blur/playback")
	}
	dir := nativePlaybackFixture(t, 41)
	fake, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(t.TempDir(), "capture")
	t.Setenv("LRTL_TEST_FFMPEG_CAPTURE", capture)
	tc := nativePlaybackCase{blur: 3, mode: 1, speed: 1.5, temporal: nativeTemporalCase{spatial: true}}
	preset := lrtRenderPreset{Encoder: "h264", FrameRate: "24", OutputResV: 32, Speed: 1.5, Interpolation: 1, MotionBlur: true, MotionBlurLevel: 3, Sharpen: true, CAS: true}
	path := savePresetJSON(t, preset)
	for _, position := range []int{0, 50, 100} {
		markerCommand(t, "render", dir, "--output", filepath.Join(t.TempDir(), "go.mp4"), "--ffmpeg", fake, "--lrt-render-preset", path, "--aspect", "1:1", "--crop-position", strconv.Itoa(position))
		got := denoiseRead(t, capture)
		graph := strings.Replace(nativePlaybackReferenceGraph(t, tc), "setsar=1/1", fmt.Sprintf("setsar=1/1,crop=32:32:%d:0", 32*position/100), 1)
		want := nativeColorCommand(t, executable, nil, "-v", "error", "-cpuflags", "0", "-framerate", "24", "-i", filepath.Join(dir, "frame%02d.png"), "-filter_complex", graph, "-f", "rawvideo", "-pix_fmt", "yuv444p10le", "pipe:1")
		if !bytes.Equal(got, want) {
			t.Fatalf("native crop/blur at position%d differs: bytes%d/%d hashes%x/%x", position, len(got), len(want), sha256.Sum256(got), sha256.Sum256(want))
		}
	}
}
