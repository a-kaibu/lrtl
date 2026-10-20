package lrtl

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSubsamplingNative36Formats(t *testing.T) {
	data, err := os.ReadFile("testdata/lrt-h26x-format-7.5.5.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Codec, SubSampling, PixelFormat, Extension, Options string
		Quality, VideoRange                                 int
	}
	if err := json.Unmarshal(data, &cases); err != nil || len(cases) != 36 {
		t.Fatalf("expected36 native format observations: %v", err)
	}
	for _, tc := range cases {
		preset, err := decodeLRTRenderPreset(renderPresetObservations(t)[0].Preset)
		if err != nil {
			t.Fatal(err)
		}
		preset.Encoder, preset.SubSampling, preset.OutputQuality, preset.VideoRange = tc.Codec, tc.SubSampling, tc.Quality, tc.VideoRange
		options, err := applyLRTRenderPreset(RenderOptions{}, preset, "", nil)
		if err != nil || options.Subsampling != tc.SubSampling {
			t.Fatalf("preset format %+v: %+v %v", tc, options, err)
		}
		format, err := h26xPixelFormat(options.Subsampling)
		if err != nil || format != tc.PixelFormat {
			t.Fatalf("native format%s vs%s: %v", tc.PixelFormat, format, err)
		}
		encoder, extension := "libx264", ".mp4"
		if options.Codec == "h265" {
			encoder, extension = "libx265", ".mov"
		}
		if encoder != tc.Options || extension != tc.Extension {
			t.Fatalf("native encoder/container %+v", tc)
		}
		options, err = applyLRTRenderPreset(RenderOptions{Subsampling: "420"}, preset, "", map[string]bool{"subsampling": true})
		if err != nil || options.Subsampling != "420" {
			t.Fatal("explicit sampling override was ignored")
		}
	}
	for _, value := range []string{"411", "4::2:2", "10", "NaN"} {
		if _, err := h26xPixelFormat(value); err == nil {
			t.Fatal("accepted invalid subsampling")
		}
	}
	for value, want := range map[string]string{"": "420", "4:2:0": "420", "4:2:2": "422", " 444 ": "444", "4:4:4": "444"} {
		if got, err := normalizeSubsampling(value); err != nil || got != want {
			t.Fatal("subsampling alias mismatch")
		}
	}
}

func TestSubsamplingPresetCLIAndBatch(t *testing.T) {
	dir := videoFadeFixture(t, 3)
	before := batchFileSnapshot(t, dir)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(t.TempDir(), "capture")
	t.Setenv("LRTL_TEST_FFMPEG_CAPTURE", capture)
	var baseline []byte
	for _, codec := range []string{"h264", "h265"} {
		for _, sampling := range []string{"420", "422", "444"} {
			preset, err := decodeLRTRenderPreset(renderPresetObservations(t)[0].Preset)
			if err != nil {
				t.Fatal(err)
			}
			preset.Encoder, preset.SubSampling, preset.OutputResV = codec, sampling, 32
			path := savePresetJSON(t, preset)
			presetBefore, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			ext := ".mp4"
			if codec == "h265" {
				ext = ".mov"
			}
			for _, videoRange := range []string{"auto", "limited", "full"} {
				result := markerCommand(t, "render", dir, "--output", filepath.Join(t.TempDir(), "video"+ext), "--lrt-render-preset", path, "--ffmpeg", executable, "--video-range", videoRange)
				if sampling != "420" && !strings.Contains(result, "10-bit") {
					t.Fatal("summary omitted high-depth selection")
				}
				data, err := os.ReadFile(capture)
				if err != nil {
					t.Fatal(err)
				}
				if baseline == nil {
					baseline = data
				} else if !bytes.Equal(baseline, data) {
					t.Fatal("output subsampling changed RGB48 source preparation")
				}
				argsData, err := os.ReadFile(capture + ".json")
				if err != nil {
					t.Fatal(err)
				}
				var args []string
				if err := json.Unmarshal(argsData, &args); err != nil {
					t.Fatal(err)
				}
				format, _ := h26xPixelFormat(sampling)
				joined := strings.Join(args, " ")
				if !strings.Contains(joined, "-pix_fmt "+format) || !strings.Contains(joined, "-vf "+videoRangeFilter(videoRange, format)) {
					t.Fatalf("wrong output format/range: %s", joined)
				}
				probe, err := os.ReadFile(capture + ".probe.json")
				encoder := "libx264"
				if codec == "h265" {
					encoder = "libx265"
				}
				if err != nil || !bytes.Contains(probe, []byte("encoder="+encoder)) {
					t.Fatalf("missing capability probe: %s %v", probe, err)
				}
			}
			assertPreviewEVFileBytes(t, path, presetBefore)
		}
	}
	markerCommand(t, "batch-render", dir, "--output", t.TempDir(), "--format", "mov", "--", "--codec", "h265", "--subsampling", "4:4:4", "--ffmpeg", executable)
	args, err := os.ReadFile(capture + ".json")
	if err != nil || !bytes.Contains(args, []byte("yuv444p10le")) || !bytes.Contains(args, []byte("hvc1")) {
		t.Fatalf("batch lost MOV/high-depth format: %s %v", args, err)
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("subsampling changed source files")
	}
}

func TestSubsamplingCapabilityAndFailureRetention(t *testing.T) {
	dir := videoFadeFixture(t, 1)
	before := batchFileSnapshot(t, dir)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(t.TempDir(), "capture")
	t.Setenv("LRTL_TEST_FFMPEG_CAPTURE", capture)
	for _, tc := range []struct{ formats, sampling string }{{"yuv420p", "444"}, {"yuv420p yuv422p10leX", "422"}, {"yuv420p10le", "420"}, {"missing", "444"}, {"failure", "422"}} {
		t.Setenv("LRTL_TEST_FFMPEG_FORMATS", tc.formats)
		output := filepath.Join(t.TempDir(), "keep.mp4")
		if err := os.WriteFile(output, []byte("original video"), 0600); err != nil {
			t.Fatal(err)
		}
		var out, stderr bytes.Buffer
		if Main([]string{"render", dir, "--output", output, "--ffmpeg", executable, "--subsampling", tc.sampling, "--overwrite"}, &out, &stderr) == 0 {
			t.Fatalf("silently accepted unsupported format: %+v", tc)
		}
		assertPreviewEVFileBytes(t, output, []byte("original video"))
	}
	t.Setenv("LRTL_TEST_FFMPEG_FORMATS", "")
	for _, tc := range []struct{ ext, codec, sampling string }{{".avi", "", "420"}, {".mov", "prores", "444"}, {".mp4", "h264", "411"}} {
		output := filepath.Join(t.TempDir(), "keep"+tc.ext)
		if err := os.WriteFile(output, []byte("original video"), 0600); err != nil {
			t.Fatal(err)
		}
		var out, stderr bytes.Buffer
		if Main([]string{"render", dir, "--output", output, "--ffmpeg", executable, "--codec", tc.codec, "--subsampling", tc.sampling, "--overwrite"}, &out, &stderr) == 0 {
			t.Fatal("accepted invalid format/container combination")
		}
		assertPreviewEVFileBytes(t, output, []byte("original video"))
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("failed format selection changed sources")
	}
	frames, err := DiscoverSequence(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := ExportTIFFSequence(frames, filepath.Join(t.TempDir(), "export"), RenderOptions{Subsampling: "444"}); err != nil {
		t.Fatal("TIFF should clear video-only sampling:", err)
	}
	if _, err := GeneratePreviewImages(frames, filepath.Join(t.TempDir(), "preview"), RenderOptions{Quality: 90, Subsampling: "444"}); err != nil {
		t.Fatal("preview should clear video-only sampling:", err)
	}
}

func TestSubsamplingShortH265TimestampGuard(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(t.TempDir(), "capture")
	t.Setenv("LRTL_TEST_FFMPEG_CAPTURE", capture)
	for _, codec := range []string{"h264", "h265"} {
		for _, count := range []int{1, 2, 3} {
			dir := videoFadeFixture(t, count)
			markerCommand(t, "render", dir, "--output", filepath.Join(t.TempDir(), "video.mp4"), "--codec", codec, "--subsampling", "422", "--ffmpeg", executable)
			data, err := os.ReadFile(capture + ".json")
			if err != nil {
				t.Fatal(err)
			}
			var args []string
			if err := json.Unmarshal(data, &args); err != nil {
				t.Fatal(err)
			}
			found := false
			for i, value := range args {
				if value == "-bf" {
					if i+1 >= len(args) || args[i+1] != "0" {
						t.Fatalf("invalid B-frame guard: %v", args)
					}
					found = true
				}
			}
			if found != (codec == "h265" && count < 3) {
				t.Fatalf("B-frame guard codec %s count %d: %v", codec, count, args)
			}
		}
	}
}

func TestSubsamplingActualCodecFormatsAndGradation(t *testing.T) {
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
		t.Skip("zscale unavailable for explicit range comparison")
	}
	dir := t.TempDir()
	img := image.NewGray16(image.Rect(0, 0, 1024, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 1024; x++ {
			img.SetGray16(x, y, color.Gray16{uint16(x * 65535 / 1023)})
		}
	}
	var input bytes.Buffer
	if err := png.Encode(&input, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "gradient.png"), input.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	for _, codec := range []string{"h264", "h265"} {
		extensions := []string{".mp4"}
		if codec == "h265" {
			extensions = []string{".mov", ".mp4"}
		}
		for _, sampling := range []string{"420", "422", "444"} {
			for _, videoRange := range []string{"limited", "full"} {
				for _, extension := range extensions {
					t.Run(codec+"/"+sampling+"/"+videoRange+"/"+extension, func(t *testing.T) {
						format, _ := h26xPixelFormat(sampling)
						encoder, ext := "libx264", extension
						if codec == "h265" {
							encoder = "libx265"
						}
						if err := requireFFmpegPixelFormat(ffmpeg, encoder, format); err != nil {
							t.Skipf("installed encoder format unavailable: %v", err)
						}
						output := filepath.Join(t.TempDir(), "gradient"+ext)
						markerCommand(t, "render", dir, "--output", output, "--codec", codec, "--subsampling", sampling, "--video-range", videoRange, "--ffmpeg", ffmpeg, "--crf", "0", "--preset", "ultrafast", "--fps", "1")
						data, err := exec.Command(ffprobe, "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=pix_fmt,color_range,color_space,nb_frames,codec_tag_string", "-of", "json", output).CombinedOutput()
						if err != nil {
							t.Fatalf("ffprobe: %s %v", data, err)
						}
						var probe struct {
							Streams []struct {
								Pixel  string `json:"pix_fmt"`
								Range  string `json:"color_range"`
								Matrix string `json:"color_space"`
								Frames string `json:"nb_frames"`
								Tag    string `json:"codec_tag_string"`
							}
						}
						if err := json.Unmarshal(data, &probe); err != nil || len(probe.Streams) != 1 {
							t.Fatalf("probe data: %s %v", data, err)
						}
						stream := probe.Streams[0]
						wantRange, tag := "tv", "avc1"
						if videoRange == "full" {
							wantRange = "pc"
						}
						if codec == "h265" {
							tag = "hvc1"
						}
						validPixel := stream.Pixel == format || (format == "yuv420p" && stream.Pixel == "yuvj420p")
						if !validPixel || stream.Range != wantRange || stream.Matrix != "bt709" || stream.Frames != "1" || stream.Tag != tag {
							t.Fatalf("wrong encoded format/range: %s", data)
						}
						y, err := exec.Command(ffmpeg, "-v", "error", "-i", output, "-vf", "extractplanes=y", "-f", "rawvideo", "pipe:1").Output()
						depth, black, white := 1, 16, 235
						if sampling != "420" {
							depth, black, white = 2, 64, 940
						}
						if videoRange == "full" {
							black, white = 0, 255
							if depth == 2 {
								white = 1023
							}
						}
						if err != nil || len(y) != 1024*64*depth {
							t.Fatalf("decoded luma plane: bytes%d %v", len(y), err)
						}
						values := map[int]bool{}
						first, last := 0, 0
						for x := 0; x < 1024; x++ {
							v := int(y[x])
							if depth == 2 {
								v = int(binary.LittleEndian.Uint16(y[x*2:]))
							}
							values[v] = true
							if x == 0 {
								first = v
							}
							last = v
						}
						if absInt(first-black) > 4 || absInt(last-white) > 4 || (depth == 2 && len(values) <= 700) || (depth == 1 && len(values) > 256) {
							t.Fatalf("gradation/range: first%d last%d levels%d", first, last, len(values))
						}
						t.Logf("decoded %s with%d distinct luma levels", stream.Pixel, len(values))
					})
				}
			}
		}
	}
	if err := requireFFmpegPixelFormat(ffmpeg, "libx265", "yuv422p10le"); err != nil {
		t.Logf("two-frame encoder check unavailable: %v", err)
		return
	}
	if err := os.WriteFile(filepath.Join(dir, "gradient2.png"), input.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	for _, ext := range []string{".mov", ".mp4"} {
		t.Run("two-frame-timestamps/"+ext, func(t *testing.T) {
			output := filepath.Join(t.TempDir(), "two"+ext)
			markerCommand(t, "render", dir, "--output", output, "--codec", "h265", "--subsampling", "422", "--ffmpeg", ffmpeg, "--preset", "ultrafast", "--fps", "1")
			data, err := exec.Command(ffprobe, "-v", "error", "-select_streams", "v:0", "-show_entries", "packet=pts_time,dts_time", "-of", "json", output).CombinedOutput()
			if err != nil {
				t.Fatalf("probe timestamps: %s %v", data, err)
			}
			var probe struct {
				Packets []struct {
					PTS string `json:"pts_time"`
					DTS string `json:"dts_time"`
				}
			}
			if err := json.Unmarshal(data, &probe); err != nil || len(probe.Packets) != 2 {
				t.Fatalf("two-frame packets: %s %v", data, err)
			}
			for i, want := range []string{"0.000000", "1.000000"} {
				if probe.Packets[i].PTS != want || probe.Packets[i].DTS != want {
					t.Fatalf("invalid short-output timestamps: %s", data)
				}
			}
		})
	}
}
