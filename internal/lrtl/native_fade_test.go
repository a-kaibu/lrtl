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

func verifyNativeFadeVideos(t *testing.T, exe, dir string, flags []string, graph string) {
	t.Helper()
	verifyNativeFinalVideos(t, exe, dir, flags, func(pix, videoRange string) string {
		at := strings.LastIndex(graph, ",fade=")
		if at < 0 {
			t.Fatal("missing native fade reference")
		}
		return graph[:at] + "," + nativeSDRPlanarColorFilter(videoRange, pix) + graph[at:]
	})
}

func assertNativeFadeAfterColor(t *testing.T, capture string) {
	t.Helper()
	var flags []string
	if err := json.Unmarshal(denoiseRead(t, capture+".json"), &flags); err != nil {
		t.Fatal(err)
	}
	for i, flag := range flags {
		if flag == "-vf" {
			graph := flags[i+1]
			if strings.Index(graph, "zscale=") < 0 || strings.Index(graph, ",fade=") < strings.Index(graph, "zscale=") {
				t.Fatal("native fade must follow final color conversion", graph)
			}
			return
		}
	}
	t.Fatal("missing encoder color/fade filter")
}

func TestNativeFadeOfflineReferences(t *testing.T) {
	var rows []struct {
		Depth, Bytes    int
		Rate, Hash      string
		Start, Duration float64
	}
	if err := json.Unmarshal(denoiseRead(t, "testdata/lrt-native-fade-8.0.1.json"), &rows); err != nil || len(rows) != 42 {
		t.Fatalf("expected 42 measured fade references: %v", err)
	}
	for _, row := range rows {
		parts := strings.Split(row.Rate, "/")
		n, _ := strconv.Atoi(parts[0])
		d := 1
		if len(parts) > 1 {
			d, _ = strconv.Atoi(parts[1])
		}
		rate := frameRate{uint32(n), uint32(d)}
		data := nativeFadeInput(row.Depth, 41)
		size := len(data) / 41
		for f := 0; f < 41; f++ {
			fadeNativePlanes(data[f*size:(f+1)*size], row.Depth, nativeFadeFactor(videoFade{row.Start, row.Duration}, f, rate))
		}
		if len(data) != row.Bytes || fmt.Sprintf("%x", sha256.Sum256(data)) != row.Hash {
			t.Fatalf("fade reference changed: %+v hash%x", row, sha256.Sum256(data))
		}
	}
}

func TestNativeFadeDisabled(t *testing.T) {
	for _, depth := range []int{8, 10} {
		data := nativeFadeInput(depth, 1)
		before := bytes.Clone(data)
		factor := nativeFadeFactor(videoFade{}, 1234, frameRate{30000, 1001})
		fadeNativePlanes(data, depth, factor)
		if factor != 65535 || !bytes.Equal(data, before) {
			t.Fatal("disabled fade changed samples")
		}
	}
}

func TestNativeSharpenStoragePrecision(t *testing.T) {
	input := make([]uint16, 64*32)
	input[16*64+32] = 1023
	planes := [3][]uint16{input, make([]uint16, len(input)), make([]uint16, len(input))}
	f := nativeSpatialFilter{64, 32, true, false}
	got := f.filterPlanes(planes, len(input)*6)
	if value := binary.LittleEndian.Uint16(got[(16*64+32)*2:]); value != 1024 {
		t.Fatalf("native unsharp must retain storage overshoot, got%d", value)
	}
	if input[16*64+32] != 1023 {
		t.Fatal("source changed")
	}
	if executable := os.Getenv("LRTL_COLOR_FFMPEG"); executable != "" {
		f.sharpen = false
		raw := f.filterPlanes(planes, len(input)*6)
		want := nativeColorCommand(t, executable, raw, "-v", "error", "-f", "rawvideo", "-pixel_format", "yuv444p10le", "-video_size", "64x32", "-i", "pipe:0", "-vf", "unsharp=luma_msize_x=7:luma_msize_y=7:luma_amount=0.4", "-f", "rawvideo", "pipe:1")
		if !bytes.Equal(got, want) {
			t.Fatal("native unsharp storage reference differs")
		}
	}
}

func FuzzNativeFadePlanes(f *testing.F) {
	f.Add([]byte{0, 255, 16, 128}, uint16(32768), true)
	f.Add([]byte{255, 255, 0, 0}, uint16(65535), false)
	f.Fuzz(func(t *testing.T, source []byte, factor uint16, ten bool) {
		fade := videoFade{start: float64(factor%1000) / 1000, duration: float64(factor) / 10000}
		previous := int64(65535)
		for index := 0; index < 80; index++ {
			gain := nativeFadeFactor(fade, index, frameRate{30000, 1001})
			if gain < 0 || gain > previous {
				t.Fatal("native fade gain outside monotone range")
			}
			previous = gain
		}
		depth := 8
		if ten {
			depth = 10
		}
		data := nativeFadeInput(depth, 1)
		if len(source) > 0 {
			for i := range data {
				data[i] = source[i%len(source)]
			}
		}
		if ten {
			for i := 0; i < len(data); i += 2 {
				binary.LittleEndian.PutUint16(data[i:], binary.LittleEndian.Uint16(data[i:])&1023)
			}
		}
		before := bytes.Clone(data)
		fadeNativePlanes(data, depth, int64(factor))
		if factor == 65535 && !bytes.Equal(before, data) {
			t.Fatal("identity changed frame")
		}
		for i := 0; i < len(data); i += (depth + 7) / 8 {
			value := int(data[i])
			if ten {
				value = int(binary.LittleEndian.Uint16(data[i:]))
			}
			if value >= 1<<depth {
				t.Fatal("fade sample outside depth")
			}
			if factor == 0 {
				black := 16 << (depth - 8)
				if i >= len(data)/3 {
					black = 1 << (depth - 1)
				}
				if value != black {
					t.Fatal("fade end is not black")
				}
			}
		}
	})
}

func nativeFadeInput(depth, frames int) []byte {
	stride := (depth + 7) / 8
	data := make([]byte, 32*3*frames*stride)
	for i := 0; i < len(data)/stride; i++ {
		v := (i*137 + i/32*53) % (1 << depth)
		if depth == 8 {
			data[i] = byte(v)
		} else {
			binary.LittleEndian.PutUint16(data[i*2:], uint16(v))
		}
	}
	return data
}

func TestNativeFadePlanesAgainstFFmpeg(t *testing.T) {
	executable := os.Getenv("LRTL_COLOR_FFMPEG")
	if executable == "" {
		t.Skip("set LRTL_COLOR_FFMPEG for native fade comparisons")
	}
	for _, depth := range []int{8, 10} {
		for _, rate := range []frameRate{{24, 1}, {30000, 1001}, {2, 1}} {
			for _, fade := range []videoFade{{0, 2}, {.137, .7}, {.251, .3}, {0, .000001}, {0, .0624996}, {0, .0625004}, {0, .0000001}} {
				t.Run(fmt.Sprintf("%d/%s/%g-%g", depth, rate.String(), fade.start, fade.duration), func(t *testing.T) {
					input := nativeFadeInput(depth, 41)
					format := "yuv444p"
					if depth == 10 {
						format = "yuv444p10le"
					}
					graph := fmt.Sprintf("setparams=range=full,fade=t=out:st=%.3f:d=%s", fade.start, strconv.FormatFloat(fade.duration, 'f', -1, 32))
					want := nativeColorCommand(t, executable, input, "-v", "error", "-f", "rawvideo", "-pixel_format", format, "-video_size", "16x2", "-framerate", rate.String(), "-i", "pipe:0", "-filter_complex", graph, "-pix_fmt", format, "-f", "rawvideo", "pipe:1")
					got := append([]byte(nil), input...)
					size := len(got) / 41
					for f := 0; f < 41; f++ {
						fadeNativePlanes(got[f*size:(f+1)*size], depth, nativeFadeFactor(fade, f, rate))
					}
					t.Logf("FADE depth=%d rate=%s start=%g duration=%g bytes=%d hash=%x", depth, rate.String(), fade.start, fade.duration, len(want), sha256.Sum256(want))
					if !bytes.Equal(got, want) {
						for i := range got {
							if i >= len(want) || got[i] != want[i] {
								t.Fatalf("fade first difference byte%d frame%d got%d want%d", i, i/size, got[i], want[i])
							}
						}
						t.Fatal("fade byte counts differ")
					}
				})
			}
		}
	}
}

func TestNativeFadeActualVideos(t *testing.T) {
	executable, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("FFmpeg required")
	}
	dir := nativeTemporalFixture(t)
	for _, tc := range []nativeTemporalCase{{name: "plain"}, {name: "spatial", spatial: true}, {name: "combined", noise: 4, air: 3, luma: true, chroma: true, spatial: true}} {
		t.Run(tc.name, func(t *testing.T) {
			count := 17
			if tc.air > 0 {
				count = 12 // measured output after strong denoise latency and trim
			}
			fade, err := planVideoFade(count, frameRate{24, 1}, "24", .5)
			if err != nil {
				t.Fatal(err)
			}
			graph := nativeTemporalReferenceGraph(t, tc) + fmt.Sprintf(",fade=t=out:st=%.3f:d=%g", fade.start, float32(fade.duration))
			verifyNativeFadeVideos(t, executable, dir, append(nativeTemporalCLI(tc), "--fade-out", ".5"), graph)
		})
	}
}

func TestNativeFadeTemporalInputPlanes(t *testing.T) {
	executable := os.Getenv("LRTL_COLOR_FFMPEG")
	if executable == "" {
		t.Skip("set LRTL_COLOR_FFMPEG")
	}
	dir := nativeTemporalFixture(t)
	fake, _ := os.Executable()
	capture := filepath.Join(t.TempDir(), "capture")
	t.Setenv("LRTL_TEST_FFMPEG_CAPTURE", capture)
	for _, tc := range []nativeTemporalCase{{name: "plain"}, {name: "spatial", spatial: true}, {name: "combined", noise: 4, air: 3, luma: true, chroma: true, spatial: true}} {
		t.Run(tc.name, func(t *testing.T) {
			graph := nativeTemporalReferenceGraph(t, tc)
			want := nativeFilterReferenceCommand(t, executable, nil, "-v", "error", "-sws_flags", "print_info+accurate_rnd+bitexact+full_chroma_int", "-framerate", "24", "-i", filepath.Join(dir, "frame%02d.png"), "-filter_complex", graph, "-pix_fmt", "yuv444p10le", "-f", "rawvideo", "pipe:1")
			args := []string{"render", dir, "--output", filepath.Join(t.TempDir(), "go.mp4"), "--output-color", "lrt-7.5.5", "--ffmpeg", fake, "--fps", "24", "--fade-out", ".5"}
			markerCommand(t, append(args, nativeTemporalCLI(tc)...)...)
			got := denoiseRead(t, capture)
			assertNativeFadeAfterColor(t, capture)
			if !bytes.Equal(got, want) {
				for i := 0; i < min(len(got), len(want)); i += 2 {
					if !bytes.Equal(got[i:i+2], want[i:i+2]) {
						t.Fatalf("first mismatch frame%d plane%d code%d/%d bytes%d/%d", i/(64*32*6), i/(64*32*2)%3, binary.LittleEndian.Uint16(got[i:]), binary.LittleEndian.Uint16(want[i:]), len(got), len(want))
					}
				}
				t.Fatal("length differs")
			}
		})
	}
}

func TestNativeFadeSlowInputPlanes(t *testing.T) {
	executable := os.Getenv("LRTL_COLOR_FFMPEG")
	if executable == "" {
		t.Skip("set LRTL_COLOR_FFMPEG")
	}
	dir := nativePlaybackFixture(t, 11)
	fake, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(t.TempDir(), "capture")
	t.Setenv("LRTL_TEST_FFMPEG_CAPTURE", capture)
	for _, mode := range []int{1, 2} {
		t.Run(fmt.Sprint(mode), func(t *testing.T) {
			tc := nativeSlowCase{playback: nativePlaybackCase{mode: mode, speed: .5}, spatial: 3}
			graph := nativeSlowReferenceGraph(t, tc)
			want := nativeFilterReferenceCommand(t, executable, nil, "-v", "error", "-sws_flags", "print_info+accurate_rnd+bitexact+full_chroma_int", "-framerate", "24", "-i", filepath.Join(dir, "frame%02d.png"), "-filter_complex", graph, "-pix_fmt", "yuv444p", "-f", "rawvideo", "pipe:1")
			args := []string{"render", dir, "--output", filepath.Join(t.TempDir(), "go.mp4"), "--output-color", "lrt-7.5.5", "--ffmpeg", fake, "--fps", "24", "--fade-out", ".5"}
			markerCommand(t, append(args, nativeSlowCLI(tc)...)...)
			assertNativeFadeAfterColor(t, capture)
			if got := denoiseRead(t, capture); !bytes.Equal(got, want) {
				t.Fatalf("slow fade mismatch bytes%d/%d hashes%x/%x", len(got), len(want), sha256.Sum256(got), sha256.Sum256(want))
			}
		})
	}
}

func TestNativeFadeSlowActualVideos(t *testing.T) {
	executable, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("FFmpeg required")
	}
	dir := nativePlaybackFixture(t, 11)
	for _, mode := range []int{1, 2} {
		t.Run(fmt.Sprint(mode), func(t *testing.T) {
			tc := nativeSlowCase{playback: nativePlaybackCase{mode: mode, speed: .5}, spatial: 3}
			graph := nativeSlowReferenceGraph(t, tc)
			base := nativeFilterReferenceCommand(t, executable, nil, "-v", "error", "-framerate", "24", "-i", filepath.Join(dir, "frame%02d.png"), "-filter_complex", graph, "-pix_fmt", "yuv444p", "-f", "rawvideo", "pipe:1")
			fade, err := planVideoFade(len(base)/(64*32*3), frameRate{24, 1}, "24", .5)
			if err != nil {
				t.Fatal(err)
			}
			graph += fmt.Sprintf(",fade=t=out:st=%.3f:d=%g", fade.start, float32(fade.duration))
			verifyNativeFadeVideos(t, executable, dir, append(nativeSlowCLI(tc), "--fade-out", ".5"), graph)
		})
	}
}

func TestNativeFadePresetBatch(t *testing.T) {
	dir := nativeTemporalFixture(t)
	before := batchFileSnapshot(t, dir)
	fake, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(t.TempDir(), "capture")
	t.Setenv("LRTL_TEST_FFMPEG_CAPTURE", capture)
	markerCommand(t, "render", dir, "--output", filepath.Join(t.TempDir(), "direct.mp4"), "--output-color", "lrt-7.5.5", "--ffmpeg", fake, "--fps", "24", "--fade-out", "1", "--sharpen", "--cas")
	want := denoiseRead(t, capture)
	preset := savePresetJSON(t, lrtRenderPreset{Encoder: "h264", FrameRate: "24", OutputResV: 32, Speed: 1, FadeOut: true, FadeTime: 1, Sharpen: true, CAS: true})
	markerCommand(t, "render", dir, "--output", filepath.Join(t.TempDir(), "preset.mp4"), "--ffmpeg", fake, "--lrt-render-preset", preset)
	if !bytes.Equal(want, denoiseRead(t, capture)) {
		t.Fatal("preset changed native fade")
	}
	markerCommand(t, "batch-render", dir, "--output", t.TempDir(), "--", "--ffmpeg", fake, "--lrt-render-preset", preset)
	if !bytes.Equal(want, denoiseRead(t, capture)) {
		t.Fatal("batch changed native fade")
	}
	for path, data := range before {
		if !bytes.Equal(data, denoiseRead(t, path)) {
			t.Fatal("source changed", path)
		}
	}
}
