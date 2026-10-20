package lrtl

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
)

type nativeTemporalCase struct {
	name                  string
	noise, air            int
	luma, chroma, spatial bool
}

func nativeTemporalCases() []nativeTemporalCase {
	return []nativeTemporalCase{
		{name: "noise1", noise: 1}, {name: "noise2", noise: 2}, {name: "noise3", noise: 3}, {name: "noise4", noise: 4},
		{name: "air1", air: 1}, {name: "air2", air: 2}, {name: "air3", air: 3},
		{name: "df-y", luma: true}, {name: "df-uv", chroma: true}, {name: "df-all", luma: true, chroma: true},
		{name: "combined", noise: 2, air: 2, luma: true, chroma: true, spatial: true},
		{name: "combined-strong", noise: 4, air: 3, luma: true, chroma: true, spatial: true},
	}
}

func nativeTemporalFixture(t *testing.T) string {
	return nativePlaybackFixture(t, 17)
}

func nativePlaybackFixture(t *testing.T, count int) string {
	t.Helper()
	dir := t.TempDir()
	for f := 0; f < count; f++ {
		rgb := make([]byte, 64*32*6)
		for y := 0; y < 32; y++ {
			for x := 0; x < 64; x++ {
				for p := 0; p < 3; p++ {
					v := (x*(1217+p*137) + y*(311+p*719) + f*(701+p*503) + x*y*f*3) % 65536
					if (x-f*3+64)%64 == 4 && y == 13 {
						v = 65535
					}
					binary.LittleEndian.PutUint16(rgb[(y*64+x)*6+p*2:], uint16(v))
				}
			}
		}
		img, err := imageFromRGB48(rgb, 64, 32)
		if err != nil {
			t.Fatal(err)
		}
		var data bytes.Buffer
		if err := png.Encode(&data, img); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("frame%02d.png", f)), data.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func nativeTemporalCLI(tc nativeTemporalCase) []string {
	return []string{"--denoise", strconv.Itoa(tc.noise), "--remove-airplanes", strconv.Itoa(tc.air), "--output-deflicker-luma=" + strconv.FormatBool(tc.luma), "--output-deflicker-chroma=" + strconv.FormatBool(tc.chroma), "--sharpen=" + strconv.FormatBool(tc.spatial), "--cas=" + strconv.FormatBool(tc.spatial)}
}

// Build the reference from independently recorded installed helper graphs.
func nativeTemporalReferenceGraph(t *testing.T, tc nativeTemporalCase) string {
	graph := nativeTemporalReferenceFilters(t, tc)
	if tc.spatial {
		graph += ",unsharp=luma_msize_x=7:luma_msize_y=7:luma_amount=0.4,cas=strength=0.90"
	}
	if tc.air > 0 {
		graph += ",trim=start_frame=1:end_frame=16,setpts=PTS-STARTPTS"
	}
	return graph
}

func nativeTemporalReferenceFilters(t *testing.T, tc nativeTemporalCase) string {
	t.Helper()
	graph := "scale=iw:ih:out_range=full:flags=lanczos,format=yuv444p10le,setparams=color_primaries=bt709:color_trc=bt709:colorspace=bt709,setsar=1/1"
	var deflicker []struct {
		Luma, Chroma bool
		Filter       string
	}
	if err := json.Unmarshal(denoiseRead(t, "testdata/lrt-output-deflicker-filter-7.5.5.json"), &deflicker); err != nil {
		t.Fatal(err)
	}
	for _, row := range deflicker {
		if row.Luma == tc.luma && row.Chroma == tc.chroma {
			graph += row.Filter
		}
	}
	for _, part := range []struct {
		file  string
		level int
	}{{"lrt-denoise-filter-7.5.5.json", tc.noise}, {"lrt-airplane-filter-7.5.5.json", tc.air}} {
		var rows []struct {
			Level  int
			Filter string
		}
		if err := json.Unmarshal(denoiseRead(t, "testdata/"+part.file), &rows); err != nil {
			t.Fatal(err)
		}
		for _, row := range rows {
			if row.Level == part.level {
				graph += row.Filter
			}
		}
	}
	return graph
}

func TestNativeTemporalOfflineReferences(t *testing.T) {
	var rows []struct {
		Name, CPUFlags, Hash string
		Bytes                int
	}
	if err := json.Unmarshal(denoiseRead(t, "testdata/lrt-native-temporal-color-8.0.1.json"), &rows); err != nil || len(rows) != 24 {
		t.Fatalf("expected 24 temporal reference measurements: %v", err)
	}
	dir := nativeTemporalFixture(t)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(t.TempDir(), "capture")
	t.Setenv("LRTL_TEST_FFMPEG_CAPTURE", capture)
	for _, tc := range nativeTemporalCases() {
		t.Run(tc.name, func(t *testing.T) {
			args := []string{"render", dir, "--output", filepath.Join(t.TempDir(), "go.mp4"), "--ffmpeg", executable, "--output-color", "lrt-7.5.5", "--fps", "24"}
			markerCommand(t, append(args, nativeTemporalCLI(tc)...)...)
			got := denoiseRead(t, capture)
			found := false
			for _, row := range rows {
				if row.Name == tc.name && row.CPUFlags == "0" {
					found = true
					if len(got) != row.Bytes || fmt.Sprintf("%x", sha256.Sum256(got)) != row.Hash {
						t.Fatalf("temporal plane reference mismatch: %s bytes%d hash%x", tc.name, len(got), sha256.Sum256(got))
					}
				}
			}
			if !found {
				t.Fatal("missing scalar reference", tc.name)
			}
		})
	}
}

func TestNativeTemporalPlanesAgainstFFmpeg(t *testing.T) {
	executable := os.Getenv("LRTL_COLOR_FFMPEG")
	if executable == "" {
		t.Skip("set LRTL_COLOR_FFMPEG for native temporal full-plane comparisons")
	}
	dir := nativeTemporalFixture(t)
	before := batchFileSnapshot(t, dir)
	fake, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(t.TempDir(), "capture")
	t.Setenv("LRTL_TEST_FFMPEG_CAPTURE", capture)
	for _, tc := range nativeTemporalCases() {
		t.Run(tc.name, func(t *testing.T) {
			args := []string{"render", dir, "--output", filepath.Join(t.TempDir(), "go.mp4"), "--output-color", "lrt-7.5.5", "--ffmpeg", fake, "--fps", "24"}
			markerCommand(t, append(args, nativeTemporalCLI(tc)...)...)
			got := denoiseRead(t, capture)
			for _, cpu := range []string{"native", "0"} {
				t.Run(cpu, func(t *testing.T) {
					args := []string{"-v", "error", "-sws_flags", "print_info+accurate_rnd+bitexact+full_chroma_int"}
					if cpu == "0" {
						args = append(args, "-cpuflags", "0")
					}
					args = append(args, "-framerate", "24", "-i", filepath.Join(dir, "frame%02d.png"), "-filter_complex", nativeTemporalReferenceGraph(t, tc), "-an", "-pix_fmt", "yuv444p10le", "-f", "rawvideo", "pipe:1")
					want := nativeColorCommand(t, executable, nil, args...)
					t.Logf("REFERENCE name=%s cpu=%s bytes=%d sha256=%x", tc.name, cpu, len(want), sha256.Sum256(want))
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
						// Native Gaussian SIMD changes one rounding boundary in the
						// strongest combined fixture. Keep the exact observed exception
						// visible; scalar planes below must still match byte for byte.
						if cpu == "native" && tc.name == "combined-strong" && len(got) == len(want) && differences == 1 && maxDelta == 2 && fmt.Sprintf("%x", sha256.Sum256(want)) == "8cc207aa3a05cb2de7c495300d4dd12b2faaf354e180b5a190a9279dec282b1d" {
							t.Log("observed native SIMD difference: one component by two codes; scalar parity required")
							return
						}
						t.Fatalf("%s: count%d/%d differing%d max%d hashes%x/%x", tc.name, len(got), len(want), differences, maxDelta, sha256.Sum256(got), sha256.Sum256(want))
					}
				})
			}
		})
	}
	for path, data := range before {
		if !bytes.Equal(data, denoiseRead(t, path)) {
			t.Fatal("source modified", path)
		}
	}
}

func TestNativeTemporalActualVideos(t *testing.T) {
	executable, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("FFmpeg required")
	}
	dir := nativeTemporalFixture(t)
	before := batchFileSnapshot(t, dir)
	for _, tc := range nativeTemporalCases() {
		t.Run(tc.name, func(t *testing.T) {
			verifyNativeFilteredVideos(t, executable, dir, nativeTemporalCLI(tc), nativeTemporalReferenceGraph(t, tc))
		})
	}
	for path, data := range before {
		if !bytes.Equal(data, denoiseRead(t, path)) {
			t.Fatal("source changed", path)
		}
	}
}

func verifyNativeFilteredVideos(t *testing.T, executable, dir string, flags []string, referenceGraph string) {
	t.Helper()
	for _, codec := range []string{"h264", "h265", "prores-hq"} {
		for _, videoRange := range []string{"full", "limited"} {
			t.Run(codec+"/"+videoRange, func(t *testing.T) {
				suffix := ".mov"
				pix := "yuv444p10le"
				enc := []string{"-c:v", "libx265", "-preset", "ultrafast", "-crf", "0", "-tag:v", "hvc1", "-x265-params", "colorprim=bt709:range=" + videoRange + ":transfer=bt709:colormatrix=bt709"}
				if codec == "h264" {
					suffix = ".mp4"
					pix = "yuv420p"
					enc = []string{"-c:v", "libx264", "-preset", "ultrafast", "-crf", "0", "-tag:v", "avc1"}
				}
				if codec == "prores-hq" {
					pix = "yuv422p10le"
					enc = []string{"-c:v", "prores_ks", "-profile:v", "3"}
				}
				output := filepath.Join(t.TempDir(), "go"+suffix)
				args := []string{"render", dir, "--output", output, "--output-color", "lrt-7.5.5", "--ffmpeg", executable, "--fps", "24", "--codec", codec, "--crf", "0", "--preset", "ultrafast", "--video-range", videoRange}
				if codec == "h265" {
					args = append(args, "--subsampling", "444")
				}
				markerCommand(t, append(args, flags...)...)
				reference := filepath.Join(t.TempDir(), "reference"+suffix)
				graph := referenceGraph + ",zscale=rangein=full:range=" + videoRange + ":matrixin=709:matrix=709:primariesin=709:primaries=709:transferin=709:transfer=709,format=" + pix
				args = []string{"-v", "error", "-cpuflags", "0", "-sws_flags", "print_info+accurate_rnd+bitexact+full_chroma_int", "-framerate", "24", "-i", filepath.Join(dir, "frame%02d.png"), "-sws_flags", "print_info+accurate_rnd+bitexact+full_chroma_int", "-filter_complex", graph, "-an"}
				args = append(args, enc...)
				metadata := "tv"
				if videoRange == "full" {
					metadata = "pc"
				}
				args = append(args, "-pix_fmt", pix, "-color_range", metadata, "-colorspace", "bt709", "-color_primaries", "bt709", "-color_trc", "unspecified", "-mov_gamma", "2.4", "-movflags", "+write_colr+write_gama+prefer_icc+faststart", reference)
				nativeColorCommand(t, executable, nil, args...)
				decode := func(path string) []byte {
					return nativeColorCommand(t, executable, nil, "-v", "error", "-i", path, "-pix_fmt", pix, "-f", "rawvideo", "pipe:1")
				}
				got, want := decode(output), decode(reference)
				if len(got) == 0 || !bytes.Equal(got, want) {
					t.Fatalf("decoded temporal video differs: bytes%d/%d hashes%x/%x", len(got), len(want), sha256.Sum256(got), sha256.Sum256(want))
				}
				if probe, err := exec.LookPath("ffprobe"); err == nil {
					metadata := func(path string) []byte {
						return nativeColorCommand(t, probe, nil, "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=width,height,nb_frames,r_frame_rate,pix_fmt,color_range,color_space,color_transfer,color_primaries", "-of", "json", path)
					}
					if !bytes.Equal(metadata(output), metadata(reference)) {
						t.Fatal("temporal video metadata differs")
					}
				}
			})
		}
	}
}

func TestOutputFrameColorRoundTrip(t *testing.T) {
	rgb := nativeColorSource(32, 16, "lowbits")
	working, err := nativeSDRWorkingFrame(rgb, 32, 16)
	if err != nil {
		t.Fatal(err)
	}
	planes, err := nativeSDRPlanes(rgb, 32, 16)
	if err != nil {
		t.Fatal(err)
	}
	got := outputFrameColor(true).planes(working, 32, 16)
	for p := range got {
		for i := range got[p] {
			if got[p][i] != planes[p][i] {
				t.Fatal("working color changed sample")
			}
		}
	}
	f := nativeSpatialFilter{32, 16, true, true}
	a, err := f.frame(rgb)
	if err != nil {
		t.Fatal(err)
	}
	b, err := f.workingFrame(working)
	if err != nil || !bytes.Equal(a, b) {
		t.Fatal("working frame reconverted RGB", err)
	}
	if _, err := f.workingFrame([]byte{1}); err == nil {
		t.Fatal("accepted truncated native working frame")
	}
	if _, err := nativeSDRWorkingFrame(nil, 0, 0); err == nil {
		t.Fatal("accepted invalid source")
	}
	// All default constructors still use the prior RGB residual model.
	legacy := outputFrameColor(false).planes(rgb, 32, 16)
	control := bytes.Clone(rgb)
	outputFrameColor(false).apply(control, legacy, legacy)
	if !bytes.Equal(control, rgb) {
		t.Fatal("zero RGB delta changed original")
	}
}

func TestNativeTemporalDeflickerBeforeCrop(t *testing.T) {
	executable := os.Getenv("LRTL_COLOR_FFMPEG")
	if executable == "" {
		t.Skip("set LRTL_COLOR_FFMPEG to verify native YUV measurement before crop")
	}
	dir := nativeTemporalFixture(t)
	fake, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(t.TempDir(), "capture")
	t.Setenv("LRTL_TEST_FFMPEG_CAPTURE", capture)
	preset, err := decodeLRTRenderPreset(renderPresetObservations(t)[0].Preset)
	if err != nil {
		t.Fatal(err)
	}
	preset.DeflickerLuma, preset.DeflickerChroma, preset.OutputResV = true, true, 32
	path := savePresetJSON(t, preset)
	for _, position := range []int{0, 50, 100} {
		markerCommand(t, "render", dir, "--output", filepath.Join(t.TempDir(), "go.mp4"), "--ffmpeg", fake, "--lrt-render-preset", path, "--fps", "24", "--aspect", "1:1", "--crop-position", strconv.Itoa(position))
		got := denoiseRead(t, capture)
		graph := nativeTemporalReferenceGraph(t, nativeTemporalCase{luma: true, chroma: true}) + fmt.Sprintf(",crop=32:32:%d:0", 32*position/100)
		want := nativeColorCommand(t, executable, nil, "-v", "error", "-framerate", "24", "-i", filepath.Join(dir, "frame%02d.png"), "-filter_complex", graph, "-f", "rawvideo", "-pix_fmt", "yuv444p10le", "pipe:1")
		if !bytes.Equal(got, want) {
			t.Fatalf("native deflicker crop position%d differs: bytes%d/%d hashes%x/%x", position, len(got), len(want), sha256.Sum256(got), sha256.Sum256(want))
		}
	}
}

func TestNativeTemporalCPURoundingStage(t *testing.T) {
	executable := os.Getenv("LRTL_COLOR_FFMPEG")
	if executable == "" {
		t.Skip("set LRTL_COLOR_FFMPEG to isolate the observed CPU rounding difference")
	}
	dir := nativeTemporalFixture(t)
	for _, tc := range []nativeTemporalCase{{name: "before-air", noise: 4, luma: true, chroma: true}, {name: "after-air", noise: 4, air: 3, luma: true, chroma: true}, {name: "after-spatial", noise: 4, air: 3, luma: true, chroma: true, spatial: true}} {
		var outputs [][]byte
		for _, cpu := range []string{"native", "0"} {
			args := []string{"-v", "error"}
			if cpu == "0" {
				args = append(args, "-cpuflags", "0")
			}
			args = append(args, "-framerate", "24", "-i", filepath.Join(dir, "frame%02d.png"), "-filter_complex", nativeTemporalReferenceGraph(t, tc), "-f", "rawvideo", "-pix_fmt", "yuv444p10le", "pipe:1")
			outputs = append(outputs, nativeColorCommand(t, executable, nil, args...))
		}
		differences, maxDelta := 0, 0
		for i := 0; i+1 < min(len(outputs[0]), len(outputs[1])); i += 2 {
			a, b := int(binary.LittleEndian.Uint16(outputs[0][i:])), int(binary.LittleEndian.Uint16(outputs[1][i:]))
			if a != b {
				differences++
				if a < b {
					a, b = b, a
				}
				maxDelta = max(maxDelta, a-b)
			}
		}
		t.Logf("%s CPU difference components=%d max=%d", tc.name, differences, maxDelta)
		expected := map[string][2]int{"before-air": {0, 0}, "after-air": {1, 1}, "after-spatial": {1, 2}}[tc.name]
		if len(outputs[0]) != len(outputs[1]) || differences != expected[0] || maxDelta != expected[1] {
			t.Fatal("CPU rounding changed at stage", tc.name, differences, maxDelta)
		}
	}
}

func FuzzNativeTemporalColor(f *testing.F) {
	f.Add([]byte{0, 255, 16, 128, 64}, uint8(4))
	f.Add([]byte{255, 0, 255, 0, 127, 128}, uint8(1))
	f.Fuzz(func(t *testing.T, data []byte, level uint8) {
		if len(data) == 0 || len(data) > 4096 {
			return
		}
		width, height, count := 8, 8, 7
		load := func(index int) ([]byte, error) {
			rgb := make([]byte, width*height*6)
			for i := range rgb {
				rgb[i] = data[(i+index)%len(data)]
			}
			return nativeSDRWorkingFrame(rgb, width, height)
		}
		df := newOutputDeflicker(width, height, count, true, true, load)
		df.color = true
		noise := newOutputDenoiser(width, height, count, int(level%4)+1, df.frame)
		noise.color = true
		air := newAirplaneRemover(width, height, count, int(level%3)+1, noise.frame)
		air.color = true
		filter := nativeSpatialFilter{width, height, true, true}
		for i := 0; i < airplaneSourceCount(count, air.level); i++ {
			frame, err := air.frame(i)
			if err != nil {
				t.Fatal(err)
			}
			out, err := filter.workingFrame(frame)
			if err != nil || len(out) != width*height*6 {
				t.Fatal("invalid working output", err)
			}
			for j := 0; j < len(out); j += 2 {
				if binary.LittleEndian.Uint16(out[j:]) > 1023 {
					t.Fatal("temporal sample out of range")
				}
			}
		}
	})
}

func TestNativeTemporalPresetBatchAndFailure(t *testing.T) {
	dir := nativeTemporalFixture(t)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(t.TempDir(), "capture")
	t.Setenv("LRTL_TEST_FFMPEG_CAPTURE", capture)
	tc := nativeTemporalCases()[11]
	args := []string{"render", dir, "--output", filepath.Join(t.TempDir(), "direct.mp4"), "--output-color", "lrt-7.5.5", "--fps", "24", "--ffmpeg", executable}
	markerCommand(t, append(args, nativeTemporalCLI(tc)...)...)
	want := denoiseRead(t, capture)
	preset := lrtRenderPreset{Encoder: "h264", FrameRate: "24", OutputResV: 32, Speed: 1, Denoise: true, DenoiseLevel: 4, RemoveAirplanes: true, RemoveAirplanesLevel: 3, DeflickerLuma: true, DeflickerChroma: true, Sharpen: true, CAS: true}
	path := savePresetJSON(t, preset)
	presetBefore := denoiseRead(t, path)
	markerCommand(t, "render", dir, "--output", filepath.Join(t.TempDir(), "preset.mp4"), "--lrt-render-preset", path, "--ffmpeg", executable)
	if !bytes.Equal(want, denoiseRead(t, capture)) {
		t.Fatal("native combined preset changed planes")
	}
	markerCommand(t, "batch-render", dir, "--output", t.TempDir(), "--", "--lrt-render-preset", path, "--ffmpeg", executable)
	if !bytes.Equal(want, denoiseRead(t, capture)) {
		t.Fatal("native combined batch changed planes")
	}
	assertPreviewEVFileBytes(t, path, presetBefore)
	if err := os.WriteFile(filepath.Join(dir, "frame16.png"), []byte("corrupt lookahead"), 0600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "keep.mp4")
	if err := os.WriteFile(output, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	args = []string{"render", dir, "--output", output, "--overwrite", "--lrt-render-preset", path, "--ffmpeg", executable}
	if Main(args, &stdout, &stderr) == 0 {
		t.Fatal("accepted corrupt native temporal tail")
	}
	assertPreviewEVFileBytes(t, output, []byte("original"))
	entries, err := os.ReadDir(filepath.Dir(output))
	if err != nil || len(entries) != 1 {
		t.Fatal("failed native temporal output left files", err)
	}
	_, err = newFFmpegFrameWriter("missing-ffmpeg", filepath.Join(t.TempDir(), "invalid.mov"), "mov", "prores", 2, 2, frameRate{24, 1}, 1, 0, "ultrafast", "", videoFade{}, "full", "", "", ffmpegFrameEncoding{outputColor: "lrt-7.5.5", workingYUV: true})
	if err == nil {
		t.Fatal("accepted working YUV without planar output stage")
	}
}
