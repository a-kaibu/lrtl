package lrtl

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestNativeSDRConversionOfflineReferences(t *testing.T) {
	var rows []struct {
		Width, Height                     int
		Range, PixelFormat, Pattern, Hash string
		Bytes                             int
	}
	if err := json.Unmarshal(denoiseRead(t, "testdata/lrt-sdr-output-color-8.0.1.json"), &rows); err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, row := range rows {
		if row.Range != "full" || row.PixelFormat != "yuv444p10le" {
			continue
		}
		filter := nativeSpatialFilter{width: row.Width, height: row.Height}
		rgb := nativeColorSource(row.Width, row.Height, row.Pattern)
		before := bytes.Clone(rgb)
		got, err := filter.frame(rgb)
		if err != nil {
			t.Fatal(err)
		}
		hash := sha256.Sum256(got)
		if len(got) != row.Bytes || hex.EncodeToString(hash[:]) != row.Hash {
			t.Fatalf("%dx%d %s differs from full-range native planes: hash%x", row.Width, row.Height, row.Pattern, hash)
		}
		if !bytes.Equal(rgb, before) {
			t.Fatal("converter modified RGB source")
		}
		count++
	}
	if count != 24 {
		t.Fatal("expected 24 full-range native observations", count)
	}
}

func TestNativeSDRConversionExhaustiveRamps(t *testing.T) {
	executable := os.Getenv("LRTL_COLOR_FFMPEG")
	if executable == "" {
		t.Skip("set LRTL_COLOR_FFMPEG for full 16-bit input sweeps")
	}
	for _, cpu := range []string{"native", "0"} {
		for _, pattern := range []string{"gray", "red", "green", "blue", "random"} {
			t.Run(cpu+"/"+pattern, func(t *testing.T) {
				rgb := make([]byte, 65536*6)
				state := uint32(0x31415926)
				for i := 0; i < 65536; i++ {
					for p := 0; p < 3; p++ {
						v := uint16(i)
						if pattern == "random" {
							state ^= state << 13
							state ^= state >> 17
							state ^= state << 5
							v = uint16(state)
						} else if pattern != "gray" && pattern != []string{"red", "green", "blue"}[p] {
							v = 0
						}
						binary.LittleEndian.PutUint16(rgb[i*6+p*2:], v)
					}
				}
				args := []string{"-v", "error", "-sws_flags", "print_info+accurate_rnd+bitexact+full_chroma_int"}
				if cpu == "0" {
					args = append(args, "-cpuflags", "0")
				}
				args = append(args, "-f", "rawvideo", "-pixel_format", "rgb48le", "-video_size", "256x256", "-i", "pipe:0", "-vf", "scale=iw:ih:out_range=full:flags=lanczos,format=yuv444p10le", "-frames:v", "1", "-f", "rawvideo", "pipe:1")
				want := nativeColorCommand(t, executable, rgb, args...)
				filter := nativeSpatialFilter{width: 256, height: 256}
				got, err := filter.frame(rgb)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(got, want) {
					for i := 0; i < min(len(got), len(want)); i += 2 {
						if !bytes.Equal(got[i:i+2], want[i:i+2]) {
							t.Fatalf("plane%d pixel%d: got%d want%d", i/(65536*2), i/2%65536, binary.LittleEndian.Uint16(got[i:]), binary.LittleEndian.Uint16(want[i:]))
						}
					}
					t.Fatal("plane byte count mismatch")
				}
			})
		}
	}
}

func TestNativeSDRConversionValidation(t *testing.T) {
	for _, tc := range []struct{ width, height, size int }{{0, 1, 6}, {1, 0, 6}, {-1, 1, 6}, {1, 1, 5}, {2, 1, 6}, {int(^uint(0) >> 1), 2, 6}, {2, 2, 30}} {
		if _, err := nativeSDRPlanes(make([]byte, tc.size), tc.width, tc.height); err == nil {
			t.Fatal("accepted invalid dimensions", tc)
		}
	}
	filter := nativeSpatialFilter{width: 2, height: 2, sharpen: true, cas: true}
	if _, err := filter.frame([]byte{1}); err == nil {
		t.Fatal("accepted truncated frame")
	}
}

func TestNativeSpatialWriterValidation(t *testing.T) {
	for _, tc := range []struct {
		profile                    string
		width, height, filterWidth int
	}{{"standard", 2, 2, 2}, {"lrt-7.5.5", 3, 2, 3}, {"lrt-7.5.5", 2, 3, 2}, {"lrt-7.5.5", -2, 2, -2}, {"lrt-7.5.5", 2, 2, 4}} {
		encoding := ffmpegFrameEncoding{outputColor: tc.profile, spatial: &nativeSpatialFilter{tc.filterWidth, tc.height, true, true}}
		_, err := newFFmpegFrameWriter("missing-ffmpeg", filepath.Join(t.TempDir(), "output.mov"), "mov", "prores", tc.width, tc.height, frameRate{24, 1}, 1, 0, "ultrafast", "", videoFade{}, "full", "", "", encoding)
		if err == nil || !strings.Contains(err.Error(), "native spatial filtering requires") {
			t.Fatalf("invalid native input reached FFmpeg: %+v %v", tc, err)
		}
	}
}

func TestNativeSpatialRoutingAndFallback(t *testing.T) {
	dir := outputSharpenFixture(t, 8)
	before := batchFileSnapshot(t, dir)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(t.TempDir(), "capture")
	t.Setenv("LRTL_TEST_FFMPEG_CAPTURE", capture)
	for _, extra := range [][]string{nil, {"--speed", "2"}, {"--motion-blur", "2"}, {"--denoise", "1"}, {"--remove-airplanes", "1"}, {"--output-deflicker-luma"}, {"--interpolation", "1", "--speed", "2"}, {"--watermark", "TEST"}, {"--fade-out", "1"}, {"--output-height", "31"}, {"--output-color", "standard"}} {
		args := []string{"render", dir, "--output", filepath.Join(t.TempDir(), "video.mp4"), "--ffmpeg", executable, "--output-color", "lrt-7.5.5", "--sharpen", "--cas"}
		markerCommand(t, append(args, extra...)...)
		var flags []string
		if err := json.Unmarshal(denoiseRead(t, capture+".json"), &flags); err != nil {
			t.Fatal(err)
		}
		joined := strings.Join(flags, " ")
		fallback := false
		for _, flag := range extra {
			if flag == "--watermark" || flag == "--fade-out" || flag == "--output-height" || flag == "standard" {
				fallback = true
			}
		}
		want := "yuv444p10le"
		if fallback {
			want = "rgb48le"
		}
		if !strings.Contains(joined, "-pixel_format "+want) {
			t.Fatalf("routing %v: %s", extra, joined)
		}
		if !fallback && (strings.Contains(joined, "unsharp") || strings.Contains(joined, "cas=strength") || strings.Contains(joined, "scale=iw")) {
			t.Fatal("FFmpeg must not reapply Go conversion/filters", joined)
		}
		if !fallback {
			raw := denoiseRead(t, capture)
			for i := 0; i < len(raw); i += 2 {
				if binary.LittleEndian.Uint16(raw[i:]) > 1023 {
					t.Fatal("planar sample outside 10-bit domain")
				}
			}
			// Capture the same temporal pipeline with spatial filtering disabled,
			// then apply the native conversion and both spatial kernels once.
			baselineArgs := append(append([]string(nil), args...), extra...)
			baselineArgs = append(baselineArgs, "--output", filepath.Join(t.TempDir(), "baseline.mp4"), "--sharpen=false", "--cas=false")
			markerCommand(t, baselineArgs...)
			baseline := denoiseRead(t, capture)
			baselineFlags := string(denoiseRead(t, capture+".json"))
			workingBaseline := strings.Contains(baselineFlags, `"-pixel_format","yuv444p10le"`)
			if len(raw) != len(baseline) || len(raw)%(64*32*6) != 0 {
				t.Fatal("native routing changed frame count")
			}
			filter := nativeSpatialFilter{64, 32, true, true}
			for i := 0; i < len(raw); i += 64 * 32 * 6 {
				var want []byte
				var err error
				if workingBaseline {
					var planes [3][]uint16
					for p := range planes {
						planes[p] = make([]uint16, 64*32)
						for j := range planes[p] {
							planes[p][j] = binary.LittleEndian.Uint16(baseline[i+(p*64*32+j)*2:])
						}
					}
					want = filter.filterPlanes(planes, 64*32*6)
				} else {
					want, err = filter.frame(baseline[i : i+64*32*6])
				}
				if err != nil || !bytes.Equal(raw[i:i+64*32*6], want) {
					t.Fatalf("spatial filter applied in wrong stage for %v frame%d: %v", extra, i/(64*32*6), err)
				}
			}
		}
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("source modified")
	}
}

func TestNativeSpatialFailurePreservesExistingVideo(t *testing.T) {
	dir := outputSharpenFixture(t, 3)
	if err := os.WriteFile(filepath.Join(dir, "frame02.png"), []byte("corrupt final frame"), 0600); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(t.TempDir(), "capture")
	t.Setenv("LRTL_TEST_FFMPEG_CAPTURE", capture)
	output := filepath.Join(t.TempDir(), "keep.mp4")
	if err := os.WriteFile(output, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if Main([]string{"render", dir, "--output", output, "--overwrite", "--output-color", "lrt-7.5.5", "--cas", "--ffmpeg", executable}, &stdout, &stderr) == 0 {
		t.Fatal("accepted corrupt final frame")
	}
	assertPreviewEVFileBytes(t, output, []byte("original"))
	entries, err := os.ReadDir(filepath.Dir(output))
	if err != nil || len(entries) != 1 {
		t.Fatal("failed spatial output left temporary file", err)
	}
}

func FuzzNativeSDRConversion(f *testing.F) {
	f.Add([]byte{0, 0, 255, 255, 1, 0}, uint8(1))
	f.Add([]byte{255, 255, 0, 0, 0, 0, 0, 0, 0, 0, 255, 255}, uint8(2))
	f.Fuzz(func(t *testing.T, raw []byte, width uint8) {
		if len(raw) > 64*64*6 {
			return
		}
		w := int(width)%64 + 1
		if len(raw) == 0 || len(raw)%(w*6) != 0 {
			return
		}
		h := len(raw) / (w * 6)
		filter := nativeSpatialFilter{w, h, true, true}
		got, err := filter.frame(raw)
		if err != nil || len(got) != len(raw) {
			t.Fatal(fmt.Sprint("conversion failed: ", err))
		}
		for i := 0; i < len(got); i += 2 {
			if binary.LittleEndian.Uint16(got[i:]) > 1023 {
				t.Fatal("sample out of range")
			}
		}
	})
}
