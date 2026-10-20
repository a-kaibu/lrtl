package lrtl

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestVideoRangeNative755Mapping(t *testing.T) {
	data, err := os.ReadFile("testdata/lrt-video-range-7.5.5.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		VideoRange     int
		Source, Filter string
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) != 3 {
		t.Fatal("expected three native range-filter observations")
	}
	for _, tc := range cases {
		preset, err := decodeLRTRenderPreset(renderPresetObservations(t)[0].Preset)
		if err != nil {
			t.Fatal(err)
		}
		preset.VideoRange = tc.VideoRange
		options, err := applyLRTRenderPreset(RenderOptions{FPS: 24}, preset, "", nil)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(tc.Filter, ":range="+options.VideoRange+":") {
			t.Fatalf("native filter %s vs %s", tc.Filter, options.VideoRange)
		}
		options, err = applyLRTRenderPreset(RenderOptions{FPS: 24, VideoRange: "auto"}, preset, "", map[string]bool{"video-range": true})
		if err != nil || options.VideoRange != "auto" {
			t.Fatal("explicit range override ignored")
		}
	}
	for _, value := range []string{"rgb", "tv", "pc", "-1"} {
		if _, err := normalizeVideoRange(value); err == nil {
			t.Fatalf("accepted %s", value)
		}
	}
}

func rangeImageFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for i, level := range []uint16{0, 65535, 32768} {
		img := image.NewNRGBA64(image.Rect(0, 0, 256, 128))
		for y := 0; y < 128; y++ {
			for x := 0; x < 256; x++ {
				img.SetNRGBA64(x, y, color.NRGBA64{level, level, level, 65535})
			}
		}
		var data bytes.Buffer
		if err := png.Encode(&data, img); err != nil {
			t.Fatal(err)
		}
		name := []string{"frame0.png", "frame1.png", "frame2.png"}[i]
		if err := os.WriteFile(filepath.Join(dir, name), data.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestVideoRangeCLIBackendArgumentsAndSourceRetention(t *testing.T) {
	dir := rangeImageFixture(t)
	before := batchFileSnapshot(t, dir)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(t.TempDir(), "capture")
	t.Setenv("LRTL_TEST_FFMPEG_CAPTURE", capture)
	for _, codec := range []string{"h264", "h265", "prores", "prores-4444", "dnxhr-hqx", "dnxhr-444"} {
		for _, value := range []string{"auto", "limited", "full"} {
			ext := ".mov"
			if codec == "h264" || codec == "h265" {
				ext = ".mp4"
			}
			markerCommand(t, "render", dir, "--output", filepath.Join(t.TempDir(), "out"+ext), "--codec", codec, "--video-range", value, "--ffmpeg", executable)
			data, err := os.ReadFile(capture)
			if err != nil {
				t.Fatal(err)
			}
			if len(data) != 3*256*128*6 {
				t.Fatal("RGB48 frame count changed")
			}
			for i, want := range []uint16{0, 65535, 32768} {
				if got := binary.LittleEndian.Uint16(data[i*256*128*6:]); got != want {
					t.Fatalf("input was range-scaled twice: %d want %d", got, want)
				}
			}
			flags, err := os.ReadFile(capture + ".json")
			if err != nil {
				t.Fatal(err)
			}
			var args []string
			if err := json.Unmarshal(flags, &args); err != nil {
				t.Fatal(err)
			}
			joined := strings.Join(args, " ")
			if value == "auto" {
				if strings.Contains(joined, "zscale") || strings.Contains(joined, "-color_range") {
					t.Fatal("auto changed legacy pipeline")
				}
				continue
			}
			meta := "tv"
			if value == "full" {
				meta = "pc"
			}
			if !strings.Contains(joined, "zscale=matrixin=gbr:matrix=709:rangein=full:range="+value) || !strings.Contains(joined, "-color_range "+meta) || !strings.Contains(joined, "-colorspace bt709") {
				t.Fatalf("missing conversion/signaling: %s", joined)
			}
		}
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("range rendering changed source")
	}
	output := filepath.Join(t.TempDir(), "keep.avi")
	if err := os.WriteFile(output, []byte("old AVI"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"limited", "invalid"} {
		var out, stderr bytes.Buffer
		if Main([]string{"render", dir, "--output", output, "--video-range", value, "--overwrite"}, &out, &stderr) == 0 {
			t.Fatalf("accepted %s AVI range", value)
		}
		assertPreviewEVFileBytes(t, output, []byte("old AVI"))
	}
	a, b := filepath.Join(t.TempDir(), "auto.avi"), filepath.Join(t.TempDir(), "full.avi")
	markerCommand(t, "render", dir, "--output", a)
	markerCommand(t, "render", dir, "--output", b, "--video-range", "full")
	first, err := os.ReadFile(a)
	if err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(b)
	if err != nil {
		t.Fatal(err)
	}
	i, j := bytes.Index(first, []byte("movi")), bytes.Index(second, []byte("movi"))
	if i < 0 || j < 0 || !bytes.Equal(first[i:], second[j:]) {
		t.Fatal("full changed AVI pixels/index")
	}
}

func TestVideoRangePresetAndBatchForwarding(t *testing.T) {
	dir := rangeImageFixture(t)
	preset, err := decodeLRTRenderPreset(renderPresetObservations(t)[0].Preset)
	if err != nil {
		t.Fatal(err)
	}
	preset.VideoRange, preset.OutputResV = 1, 128
	path := savePresetJSON(t, preset)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(t.TempDir(), "capture")
	t.Setenv("LRTL_TEST_FFMPEG_CAPTURE", capture)
	for _, value := range []string{"", "limited", "auto"} {
		args := []string{"render", dir, "--output", filepath.Join(t.TempDir(), "native.mp4"), "--lrt-render-preset", path, "--ffmpeg", executable}
		if value != "" {
			args = append(args, "--video-range", value)
		}
		markerCommand(t, args...)
		data, err := os.ReadFile(capture + ".json")
		if err != nil {
			t.Fatal(err)
		}
		if value == "auto" {
			if bytes.Contains(data, []byte("zscale")) {
				t.Fatal("auto override ignored")
			}
		} else {
			want := value
			if want == "" {
				want = "full"
			}
			if !bytes.Contains(data, []byte("range="+want)) {
				t.Fatalf("preset range/override: %s", data)
			}
		}
	}
	markerCommand(t, "batch-render", dir, "--output", t.TempDir(), "--", "--lrt-render-preset", path, "--ffmpeg", executable)
	data, err := os.ReadFile(capture + ".json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("range=full")) {
		t.Fatal("batch lost saved range")
	}
	assertPreviewEVFileBytes(t, path, before)
}

func TestVideoRangeActualLevelsAndMetadata(t *testing.T) {
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
		t.Skip("FFmpeg zscale unavailable")
	}
	dir := rangeImageFixture(t)
	for _, codec := range []string{"h264", "h265", "prores", "dnxhr-hqx"} {
		for _, value := range []string{"limited", "full"} {
			t.Run(codec+"/"+value, func(t *testing.T) {
				ext := ".mov"
				if codec == "h264" || codec == "h265" {
					ext = ".mp4"
				}
				output := filepath.Join(t.TempDir(), "range"+ext)
				markerCommand(t, "render", dir, "--output", output, "--codec", codec, "--video-range", value, "--ffmpeg", ffmpeg, "--fps", "1", "--crf", "0", "--preset", "ultrafast")
				data, err := exec.Command(ffprobe, "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=color_range,color_space,pix_fmt,nb_frames", "-of", "json", output).CombinedOutput()
				if err != nil {
					t.Fatalf("ffprobe: %s %v", data, err)
				}
				var probe struct {
					Streams []struct {
						Range  string `json:"color_range"`
						Matrix string `json:"color_space"`
						Pixel  string `json:"pix_fmt"`
						Frames string `json:"nb_frames"`
					}
				}
				if err := json.Unmarshal(data, &probe); err != nil {
					t.Fatal(err)
				}
				if len(probe.Streams) != 1 || probe.Streams[0].Frames != "3" {
					t.Fatalf("video count: %s", data)
				}
				if probe.Streams[0].Matrix != "bt709" {
					t.Fatalf("missing/wrong YUV matrix signaling: %s", data)
				}
				meta := "tv"
				if value == "full" {
					meta = "pc"
				}
				if codec != "prores" && probe.Streams[0].Range != meta {
					t.Fatalf("range signaling: %s", data)
				}
				if codec == "prores" && probe.Streams[0].Range != "" && probe.Streams[0].Range != "unknown" && probe.Streams[0].Range != meta {
					t.Fatalf("ProRes range contradicts requested range: %s", data)
				}
				y, err := exec.Command(ffmpeg, "-v", "error", "-i", output, "-vf", "extractplanes=y", "-f", "rawvideo", "pipe:1").Output()
				if err != nil {
					t.Fatal(err)
				}
				depth := 1
				want := []int{16, 235, 126}
				if codec == "prores" || codec == "dnxhr-hqx" {
					depth = 2
					want = []int{64, 940, 502}
				}
				if value == "full" {
					want = []int{0, 255, 128}
					if depth == 2 {
						want = []int{0, 1023, 512}
					}
				}
				if codec == "prores" && value == "full" {
					want = []int{4, 1019, 512}
				} // Encoder clamps legal ProRes endpoints.
				if len(y) != 3*256*128*depth {
					t.Fatalf("unexpected luma size %d", len(y))
				}
				for i, expected := range want {
					offset := (i*256*128 + 64*256 + 128) * depth
					got := int(y[offset])
					if depth == 2 {
						got = int(binary.LittleEndian.Uint16(y[offset:]))
					}
					if math.Abs(float64(got-expected)) > 1 {
						t.Fatalf("luma frame %d: %d want %d; %s", i, got, expected, data)
					}
				}
			})
		}
	}
}
