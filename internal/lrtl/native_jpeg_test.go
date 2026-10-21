package lrtl

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/a-kaibu/lrtl/internal/nativejpeg"
)

func TestNativeJPEGOfflineWorkingPlanes(t *testing.T) {
	var rows []struct {
		File, WorkingHash string
		WorkingBytes      int
	}
	if err := json.Unmarshal(denoiseRead(t, "../nativejpeg/testdata/ffmpeg-8.0.1.json"), &rows); err != nil || len(rows) != 32 {
		t.Fatalf("expected 32 working-plane observations: %v", err)
	}
	for _, row := range rows {
		t.Run(row.File, func(t *testing.T) {
			data := denoiseRead(t, filepath.Join("../nativejpeg/testdata", row.File))
			img, err := nativejpeg.Decode(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			got, err := nativeImageWorkingFrame(img, "jpeg")
			if err != nil {
				t.Fatal(err)
			}
			size := len(got) / 6
			planar := make([]byte, len(got))
			for p := 0; p < 3; p++ {
				for i := 0; i < size; i++ {
					copy(planar[(p*size+i)*2:], got[i*6+p*2:i*6+p*2+2])
				}
			}
			if len(planar) != row.WorkingBytes || fmt.Sprintf("%x", sha256.Sum256(planar)) != row.WorkingHash {
				t.Fatal("working-plane observation changed", row.File)
			}
		})
	}
}

func TestNativeJPEGWorkingPlanesAgainstFFmpeg(t *testing.T) {
	exe := os.Getenv("LRTL_COLOR_FFMPEG")
	if exe == "" {
		var err error
		exe, err = exec.LookPath("ffmpeg")
		if err != nil {
			t.Skip("FFmpeg required")
		}
	}
	paths, err := filepath.Glob("../nativejpeg/testdata/*.jpg")
	if err != nil || len(paths) != 32 {
		t.Fatalf("expected 32 JPEG fixtures: %v", err)
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			img, err := nativejpeg.Decode(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			got, err := nativeImageWorkingFrame(img, "jpeg")
			if err != nil {
				t.Fatal(err)
			}
			w, h := img.Bounds().Dx(), img.Bounds().Dy()
			planar := make([]byte, len(got))
			for p := 0; p < 3; p++ {
				for i := 0; i < w*h; i++ {
					copy(planar[(p*w*h+i)*2:], got[i*6+p*2:i*6+p*2+2])
				}
			}
			for _, cpu := range []string{"0", "native"} {
				args := []string{"-v", "error"}
				if cpu == "0" {
					args = append(args, "-cpuflags", "0")
				}
				args = append(args, "-i", path, "-vf", "scale=iw:ih:out_range=full:flags=lanczos,format=yuv444p10le", "-frames:v", "1", "-f", "rawvideo", "pipe:1")
				want := nativeColorCommand(t, exe, nil, args...)
				if !bytes.Equal(planar, want) {
					for i := 0; i < min(len(planar), len(want)); i += 2 {
						if !bytes.Equal(planar[i:i+2], want[i:i+2]) {
							t.Fatalf("%s plane%d pixel%d got%d want%d", cpu, i/(w*h*2), i/2%(w*h), binary.LittleEndian.Uint16(planar[i:]), binary.LittleEndian.Uint16(want[i:]))
						}
					}
					t.Fatal("JPEG working size differs")
				}
			}
		})
	}
}

func nativeJPEGFixture(t *testing.T, count int, subsampling int, progressive, gray bool) string {
	t.Helper()
	dir := t.TempDir()
	for frame := 0; frame < count; frame++ {
		quality := []int{30, 75, 95}[frame%3]
		name := fmt.Sprintf("rgb-64x32-q%d-s%d-p%d.jpg", quality, subsampling, map[bool]int{false: 0, true: 1}[progressive])
		if gray {
			name = fmt.Sprintf("gray-64x32-p%d.jpg", map[bool]int{false: 0, true: 1}[progressive])
		}
		data, err := os.ReadFile(filepath.Join("../nativejpeg/testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, fmt.Sprintf("frame%02d.jpg", frame))
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		writeTestXMP(t, path, map[string]string{"exif:DateTimeOriginal": fmt.Sprintf("2026-01-02T03:04:%02dZ", frame)})
	}
	return dir
}

func TestNativeJPEGActualVideos(t *testing.T) {
	exe, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("FFmpeg required")
	}
	for _, subsampling := range []int{0, 1, 2, 3} {
		for _, progressive := range []bool{false, true} {
			dir := nativeJPEGFixture(t, 17, subsampling, progressive, subsampling == 3)
			before := batchFileSnapshot(t, dir)
			for i, tc := range []nativeTemporalCase{{}, {spatial: true}, {noise: 4, air: 3, luma: true, chroma: true, spatial: true}} {
				t.Run(fmt.Sprintf("s%d/p%v/%d", subsampling, progressive, i), func(t *testing.T) {
					verifyNativeFilteredVideos(t, exe, dir, nativeTemporalCLI(tc), nativeTemporalReferenceGraph(t, tc))
				})
			}
			for path, data := range before {
				assertPreviewEVFileBytes(t, path, data)
			}
		}
	}
}

func TestNativeJPEGRecordedOverlayVideos(t *testing.T) {
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
	if err := json.Unmarshal(denoiseRead(t, "testdata/lrt-jpeg-caller-7.5.5.json"), &rows); err != nil || len(rows) != 3 {
		t.Fatalf("expected three native caller graphs: %v", err)
	}
	file, err := os.CreateTemp(".", ".lrtl-jpeg-reference-*.srt")
	if err != nil {
		t.Fatal(err)
	}
	name := file.Name()
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(name) })
	nativeTimestampReferenceSRT(t, name, 17, 1, 3)
	for _, progressive := range []bool{false, true} {
		dir := nativeJPEGFixture(t, 17, 2, progressive, false)
		for _, row := range rows {
			t.Run(fmt.Sprintf("p%v/case%d", progressive, row.Case), func(t *testing.T) {
				preset := savePresetJSON(t, lrtRenderPreset{Encoder: "h264", FrameRate: "24", OutputResV: 32, Speed: 1, FadeOut: true, FadeTime: 1, Sharpen: row.Case > 0, MotionBlur: row.Case == 2, MotionBlurLevel: 3})
				flags := []string{"--lrt-render-preset", preset, "--watermark", "LRTL 2026", "--watermark-style", "lrt-7.5.5", "--timestamp-style", "lrt-7.5.5", "--timestamp-every", "3"}
				graph := strings.ReplaceAll(strings.TrimSuffix(row.Graph, "[v]"), "timestamp-reference.srt", filepath.Base(name))
				verifyNativeFinalVideos(t, exe, dir, flags, func(pix, videoRange string) string {
					return strings.ReplaceAll(graph, "range=limited:", "range="+videoRange+":")
				})
			})
		}
	}
}

func TestNativeJPEGDecodeSelectionAndErrors(t *testing.T) {
	data := denoiseRead(t, "../nativejpeg/testdata/rgb-64x32-q75-s2-p0.jpg")
	standard, format, err := decodeRenderImage(data, false)
	if err != nil || format != "jpeg" {
		t.Fatal(err, format)
	}
	native, format, err := decodeRenderImage(data, true)
	if err != nil || format != "jpeg" {
		t.Fatal(err, format)
	}
	if bytes.Equal(standard.(*image.YCbCr).Y, native.(*image.YCbCr).Y) {
		t.Fatal("native decoder was not selected")
	}
	if _, _, err := decodeRenderImage(data[:len(data)/2], true); err == nil {
		t.Fatal("truncated JPEG accepted")
	}
	for _, img := range []*image.YCbCr{image.NewYCbCr(image.Rect(0, 0, 0, 0), image.YCbCrSubsampleRatio420), {Rect: image.Rect(0, 0, 1<<20, 1<<20)}} {
		if _, err := nativeJPEGWorkingFrame(img); err == nil {
			t.Fatal("invalid JPEG dimensions accepted")
		}
	}
}

func TestNativeJPEGFailureRetention(t *testing.T) {
	dir := nativeJPEGFixture(t, 17, 2, true, false)
	last := filepath.Join(dir, "frame16.jpg")
	data := denoiseRead(t, last)
	sos := bytes.Index(data, []byte{0xff, 0xda})
	if sos < 0 {
		t.Fatal("missing JPEG scan")
	}
	if err := os.WriteFile(last, data[:sos+16], 0600); err != nil {
		t.Fatal(err)
	}
	before := batchFileSnapshot(t, dir)
	fake, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("LRTL_TEST_FFMPEG_CAPTURE", filepath.Join(t.TempDir(), "capture"))
	out := filepath.Join(t.TempDir(), "keep.mp4")
	if err := os.WriteFile(out, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if Main([]string{"render", dir, "--output", out, "--overwrite", "--ffmpeg", fake, "--output-color", "lrt-7.5.5", "--timestamp-style", "lrt-7.5.5", "--denoise", "4"}, &stdout, &stderr) == 0 {
		t.Fatal("truncated native JPEG accepted")
	}
	assertPreviewEVFileBytes(t, out, []byte("original"))
	entries, err := os.ReadDir(filepath.Dir(out))
	if err != nil || len(entries) != 1 {
		t.Fatal("native JPEG failure leaked staging files", entries, err)
	}
	for path, b := range before {
		assertPreviewEVFileBytes(t, path, b)
	}
}

func FuzzNativeJPEGWorkingFrame(f *testing.F) {
	f.Add([]byte{0, 128, 255}, uint8(17), uint8(13), uint8(0))
	f.Fuzz(func(t *testing.T, data []byte, wByte, hByte, kind uint8) {
		if len(data) > 4096 {
			return
		}
		ratios := []image.YCbCrSubsampleRatio{image.YCbCrSubsampleRatio444, image.YCbCrSubsampleRatio422, image.YCbCrSubsampleRatio420, image.YCbCrSubsampleRatio440, image.YCbCrSubsampleRatio411, image.YCbCrSubsampleRatio410}
		w, h := int(wByte%64)+1, int(hByte%64)+1
		img := image.NewYCbCr(image.Rect(0, 0, w, h), ratios[int(kind)%len(ratios)])
		if len(data) > 0 {
			for _, plane := range [][]byte{img.Y, img.Cb, img.Cr} {
				for i := range plane {
					plane[i] = data[i%len(data)]
				}
			}
		}
		before := append(append(bytes.Clone(img.Y), img.Cb...), img.Cr...)
		got, err := nativeJPEGWorkingFrame(img)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != w*h*6 {
			t.Fatal("invalid working frame length")
		}
		for i := 0; i < len(got); i += 2 {
			if binary.LittleEndian.Uint16(got[i:]) > 1023 {
				t.Fatal("working sample overflow")
			}
		}
		if !bytes.Equal(before, append(append(bytes.Clone(img.Y), img.Cb...), img.Cr...)) {
			t.Fatal("JPEG samples changed")
		}
	})
}

func TestNativeJPEGChromaShapes(t *testing.T) {
	exe := os.Getenv("LRTL_COLOR_FFMPEG")
	if exe == "" {
		var err error
		exe, err = exec.LookPath("ffmpeg")
		if err != nil {
			t.Skip("FFmpeg required")
		}
	}
	for _, shape := range [][2]int{{1, 1}, {2, 2}, {3, 7}, {6, 18}, {17, 13}, {25, 27}, {32, 18}, {66, 34}} {
		for _, sub := range []struct {
			ratio  image.YCbCrSubsampleRatio
			pixel  string
			dx, dy int
		}{{image.YCbCrSubsampleRatio444, "yuvj444p", 1, 1}, {image.YCbCrSubsampleRatio422, "yuvj422p", 2, 1}, {image.YCbCrSubsampleRatio420, "yuvj420p", 2, 2}, {image.YCbCrSubsampleRatio440, "yuvj440p", 1, 2}} {
			t.Run(fmt.Sprintf("%dx%d/%s", shape[0], shape[1], sub.pixel), func(t *testing.T) {
				w, h := shape[0], shape[1]
				img := image.NewYCbCr(image.Rect(0, 0, w, h), sub.ratio)
				for p, plane := range [][]byte{img.Y, img.Cb, img.Cr} {
					for i := range plane {
						plane[i] = byte(i*(31+p*13) + i*i*7 + p*41)
					}
				}
				got, err := nativeJPEGWorkingFrame(img)
				if err != nil {
					t.Fatal(err)
				}
				planar := make([]byte, len(got))
				for p := 0; p < 3; p++ {
					for i := 0; i < w*h; i++ {
						copy(planar[(p*w*h+i)*2:], got[i*6+p*2:i*6+p*2+2])
					}
				}
				input := append(append(bytes.Clone(img.Y), img.Cb...), img.Cr...)
				for _, cpu := range []string{"0", "native"} {
					args := []string{"-v", "error"}
					if cpu == "0" {
						args = append(args, "-cpuflags", "0")
					}
					args = append(args, "-f", "rawvideo", "-pixel_format", sub.pixel, "-video_size", fmt.Sprintf("%dx%d", w, h), "-i", "pipe:0", "-vf", "scale=iw:ih:in_range=full:out_range=full:flags=lanczos,format=yuv444p10le", "-frames:v", "1", "-f", "rawvideo", "pipe:1")
					want := nativeColorCommand(t, exe, input, args...)
					if !bytes.Equal(planar, want) {
						for i := 0; i < min(len(planar), len(want)); i += 2 {
							if !bytes.Equal(planar[i:i+2], want[i:i+2]) {
								t.Fatalf("%s plane%d pixel%d got%d want%d", cpu, i/(w*h*2), i/2%(w*h), binary.LittleEndian.Uint16(planar[i:]), binary.LittleEndian.Uint16(want[i:]))
							}
						}
						t.Fatal("plane lengths differ")
					}
				}
			})
		}
	}
}
