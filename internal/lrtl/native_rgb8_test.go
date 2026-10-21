package lrtl

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"golang.org/x/image/tiff"
)

func TestNativeRGB8OfflineObservations(t *testing.T) {
	var rows []struct {
		Pattern, CPUFlags, Hash string
		Width, Height, Bytes    int
	}
	if err := json.Unmarshal(denoiseRead(t, "testdata/lrt-rgb8-conversion-8.0.1.json"), &rows); err != nil || len(rows) != 12 {
		t.Fatalf("expected 12 installed-FFmpeg observations: %v", err)
	}
	for _, row := range rows {
		planes, err := nativeRGB8Planes(nativeRGB8Input(row.Width, row.Height, row.Pattern), row.Width, row.Height)
		if err != nil {
			t.Fatal(err)
		}
		data := make([]byte, row.Width*row.Height*6)
		for p := range planes {
			for i, v := range planes[p] {
				binary.LittleEndian.PutUint16(data[(p*row.Width*row.Height+i)*2:], v)
			}
		}
		if len(data) != row.Bytes || fmt.Sprintf("%x", sha256.Sum256(data)) != row.Hash {
			t.Fatalf("observed conversion changed: %+v", row)
		}
	}
}

func TestNativeRGB8Exhaustive(t *testing.T) {
	exe := os.Getenv("LRTL_COLOR_FFMPEG")
	if exe == "" {
		t.Skip("set LRTL_COLOR_FFMPEG for all 16,777,216 RGB8 combinations")
	}
	const width, height = 4096, 4096
	rgb := make([]byte, width*height*3)
	for i := 0; i < width*height; i++ {
		rgb[i*3], rgb[i*3+1], rgb[i*3+2] = byte(i>>16), byte(i>>8), byte(i)
	}
	planes, err := nativeRGB8Planes(rgb, width, height)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len(rgb)*2)
	for p := range planes {
		for i, v := range planes[p] {
			binary.LittleEndian.PutUint16(got[(p*width*height+i)*2:], v)
		}
	}
	for _, cpu := range []string{"0", "native"} {
		args := []string{"-v", "error"}
		if cpu == "0" {
			args = append(args, "-cpuflags", "0")
		}
		args = append(args, "-f", "rawvideo", "-pixel_format", "rgb24", "-video_size", "4096x4096", "-i", "pipe:0", "-vf", "scale=iw:ih:out_range=full:flags=lanczos,format=yuv444p10le", "-frames:v", "1", "-f", "rawvideo", "pipe:1")
		want := nativeColorCommand(t, exe, rgb, args...)
		if !bytes.Equal(got, want) {
			for i := 0; i < min(len(got), len(want)); i += 2 {
				if !bytes.Equal(got[i:i+2], want[i:i+2]) {
					t.Fatalf("%s RGB %06x plane%d differs", cpu, i/2%(width*height), i/(width*height*2))
				}
			}
			t.Fatal("exhaustive frame sizes differ")
		}
	}
}

func TestNativeRGB8PlanesAgainstFFmpeg(t *testing.T) {
	exe := os.Getenv("LRTL_COLOR_FFMPEG")
	if exe == "" {
		var err error
		exe, err = exec.LookPath("ffmpeg")
		if err != nil {
			t.Skip("FFmpeg required")
		}
	}
	for _, pattern := range []string{"grid", "random", "red", "green", "blue", "gray"} {
		t.Run(pattern, func(t *testing.T) {
			rgb := nativeRGB8Input(256, 256, pattern)
			planes, err := nativeRGB8Planes(rgb, 256, 256)
			if err != nil {
				t.Fatal(err)
			}
			got := make([]byte, len(rgb)*2)
			for p := range planes {
				for i, v := range planes[p] {
					binary.LittleEndian.PutUint16(got[(p*256*256+i)*2:], v)
				}
			}
			for _, cpu := range []string{"0", "native"} {
				args := []string{"-v", "error"}
				if cpu == "0" {
					args = append(args, "-cpuflags", "0")
				}
				args = append(args, "-f", "rawvideo", "-pixel_format", "rgb24", "-video_size", "256x256", "-i", "pipe:0", "-vf", "scale=iw:ih:out_range=full:flags=lanczos,format=yuv444p10le", "-frames:v", "1", "-f", "rawvideo", "pipe:1")
				want := nativeColorCommand(t, exe, rgb, args...)
				if !bytes.Equal(got, want) {
					for i := 0; i < min(len(got), len(want)); i += 2 {
						if !bytes.Equal(got[i:i+2], want[i:i+2]) {
							t.Fatalf("%s plane%d pixel%d got%d want%d", cpu, i/(256*256*2), i/2%(256*256), binary.LittleEndian.Uint16(got[i:]), binary.LittleEndian.Uint16(want[i:]))
						}
					}
					t.Fatal("plane lengths differ")
				}
			}
		})
	}
}

func TestNativeRGB8ShapesAndRGBA(t *testing.T) {
	exe := os.Getenv("LRTL_COLOR_FFMPEG")
	if exe == "" {
		var err error
		exe, err = exec.LookPath("ffmpeg")
		if err != nil {
			t.Skip("FFmpeg required")
		}
	}
	for _, shape := range [][2]int{{1, 1}, {2, 2}, {3, 5}, {7, 4}, {16, 9}, {31, 17}, {128, 4}} {
		w, h := shape[0], shape[1]
		rgb := nativeRGB8Input(w, h, "random")
		planes, err := nativeRGB8Planes(rgb, w, h)
		if err != nil {
			t.Fatal(err)
		}
		got := make([]byte, w*h*6)
		for p := range planes {
			for i, v := range planes[p] {
				binary.LittleEndian.PutUint16(got[(p*w*h+i)*2:], v)
			}
		}
		for _, pixel := range []string{"rgb24", "rgba"} {
			input := rgb
			if pixel == "rgba" {
				input = make([]byte, w*h*4)
				for i := 0; i < w*h; i++ {
					copy(input[i*4:i*4+3], rgb[i*3:i*3+3])
					input[i*4+3] = 255
				}
			}
			want := nativeColorCommand(t, exe, input, "-v", "error", "-f", "rawvideo", "-pixel_format", pixel, "-video_size", fmt.Sprintf("%dx%d", w, h), "-i", "pipe:0", "-vf", "scale=iw:ih:out_range=full:flags=lanczos,format=yuv444p10le", "-frames:v", "1", "-f", "rawvideo", "pipe:1")
			if !bytes.Equal(got, want) {
				t.Fatalf("%v %s conversion differs", shape, pixel)
			}
		}
	}
}

func TestNativeRGB8ValidationAndPrecision(t *testing.T) {
	for _, shape := range [][2]int{{0, 1}, {1, 0}, {-1, 1}, {1, 2}} {
		if _, err := nativeRGB8Planes([]byte{1, 2, 3}, shape[0], shape[1]); err == nil {
			t.Fatal("invalid dimensions accepted", shape)
		}
	}
	if _, err := nativeRGB8Planes([]byte{1, 2, 3, 4}, 1, 1); err == nil {
		t.Fatal("partial pixel accepted")
	}
	// Eligibility follows the actual image representation, never the numeric
	// coincidence that RGB16 channels happen to be multiples of 257.
	low := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	for i := 3; i < len(low.Pix); i += 4 {
		low.Pix[i] = 255
	}
	low.Pix[0] = 128
	rgb, err := imageRGB48(low)
	if err != nil {
		t.Fatal(err)
	}
	high, err := imageFromRGB48(rgb, 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	gotLow, err := nativeImageWorkingFrame(low, "png")
	if err != nil {
		t.Fatal(err)
	}
	gotHigh, err := nativeImageWorkingFrame(high, "png")
	if err != nil {
		t.Fatal(err)
	}
	wantHigh, err := nativeSDRWorkingFrame(rgb, 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotHigh, wantHigh) || bytes.Equal(gotLow, gotHigh) {
		t.Fatal("input bit depth was inferred from values")
	}
	// Non-opaque and non-RGB input paths retain their prior RGB48 conversion.
	low.Pix[3] = 128
	for _, format := range []string{"png", "jpeg"} {
		rgb, err := imageRGB48(low)
		if err != nil {
			t.Fatal(err)
		}
		want, err := nativeSDRWorkingFrame(rgb, 2, 2)
		if err != nil {
			t.Fatal(err)
		}
		got, err := nativeImageWorkingFrame(low, format)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatal("fallback changed", format, err)
		}
	}
}

func TestNativeRGB8MixedDepthSequence(t *testing.T) {
	dir := t.TempDir()
	low := image.NewNRGBA(image.Rect(0, 0, 16, 16))
	input := nativeRGB8Input(16, 16, "grid")
	for i := 0; i < 16*16; i++ {
		copy(low.Pix[i*4:i*4+3], input[i*3:i*3+3])
		low.Pix[i*4+3] = 255
	}
	rgb, err := imageRGB48(low)
	if err != nil {
		t.Fatal(err)
	}
	high, err := imageFromRGB48(rgb, 16, 16)
	if err != nil {
		t.Fatal(err)
	}
	for i, img := range []image.Image{low, high} {
		var encoded bytes.Buffer
		if err := png.Encode(&encoded, img); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("frame%02d.png", i)), encoded.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
	}
	before := batchFileSnapshot(t, dir)
	fake, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(t.TempDir(), "capture")
	t.Setenv("LRTL_TEST_FFMPEG_CAPTURE", capture)
	markerCommand(t, "render", dir, "--output", filepath.Join(t.TempDir(), "native.mp4"), "--ffmpeg", fake, "--output-color", "lrt-7.5.5")
	first, err := nativeRGB8Planes(input, 16, 16)
	if err != nil {
		t.Fatal(err)
	}
	second, err := nativeSDRPlanes(rgb, 16, 16)
	if err != nil {
		t.Fatal(err)
	}
	var want []byte
	for _, planes := range [][3][]uint16{first, second} {
		for _, plane := range planes {
			for _, v := range plane {
				want = binary.LittleEndian.AppendUint16(want, v)
			}
		}
	}
	if !bytes.Equal(denoiseRead(t, capture), want) {
		t.Fatal("mixed source depth was lost")
	}
	markerCommand(t, "render", dir, "--output", filepath.Join(t.TempDir(), "standard.mp4"), "--ffmpeg", fake, "--output-color", "standard")
	if !bytes.Equal(denoiseRead(t, capture), append(bytes.Clone(rgb), rgb...)) {
		t.Fatal("standard output changed")
	}
	for path, data := range before {
		assertPreviewEVFileBytes(t, path, data)
	}
}

func FuzzNativeRGB8Planes(f *testing.F) {
	f.Add([]byte{0, 0, 0, 255, 255, 255}, uint8(2))
	f.Fuzz(func(t *testing.T, rgb []byte, widthByte uint8) {
		if len(rgb) > 1<<16 {
			return
		}
		width := int(widthByte) + 1
		height := len(rgb) / 3 / width
		before := bytes.Clone(rgb)
		planes, err := nativeRGB8Planes(rgb, width, height)
		if !bytes.Equal(before, rgb) {
			t.Fatal("source changed")
		}
		if err != nil {
			return
		}
		for _, plane := range planes {
			if len(plane) != width*height {
				t.Fatal("invalid plane size")
			}
			for _, v := range plane {
				if v > 1023 {
					t.Fatal("sample overflow")
				}
			}
		}
	})
}

func nativeRGB8Fixture(t *testing.T, count, width, height int, format string) string {
	t.Helper()
	dir := t.TempDir()
	for frame := 0; frame < count; frame++ {
		img := image.NewNRGBA(image.Rect(0, 0, width, height))
		rgb := nativeRGB8Input(width, height, "grid")
		for i := 0; i < width*height; i++ {
			for c := 0; c < 3; c++ {
				img.Pix[i*4+c] = rgb[i*3+c] + byte(frame*(17+c*23))
			}
			img.Pix[i*4+3] = 255
		}
		var out bytes.Buffer
		var err error
		if format == "tiff" {
			err = tiff.Encode(&out, img, nil)
		} else {
			err = png.Encode(&out, img)
		}
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, fmt.Sprintf("frame%02d.%s", frame, format))
		if err := os.WriteFile(path, out.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		writeTestXMP(t, path, map[string]string{"exif:DateTimeOriginal": fmt.Sprintf("2026-01-02T03:04:%02dZ", frame)})
	}
	return dir
}

func TestNativeRGB8ActualVideos(t *testing.T) {
	exe, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("FFmpeg required")
	}
	for _, format := range []string{"png", "tiff"} {
		dir := nativeRGB8Fixture(t, 17, 64, 32, format)
		before := batchFileSnapshot(t, dir)
		for i, tc := range []nativeTemporalCase{{}, {spatial: true}, {noise: 4, air: 3, luma: true, chroma: true, spatial: true}} {
			t.Run(fmt.Sprintf("%s/%d", format, i), func(t *testing.T) {
				verifyNativeFilteredVideos(t, exe, dir, nativeTemporalCLI(tc), nativeTemporalReferenceGraph(t, tc))
			})
		}
		for path, data := range before {
			assertPreviewEVFileBytes(t, path, data)
		}
	}
}

func TestNativeRGB8PlaybackActualVideos(t *testing.T) {
	exe, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("FFmpeg required")
	}
	dir := nativeRGB8Fixture(t, 11, 64, 32, "png")
	for i, tc := range []nativeSlowCase{{playback: nativePlaybackCase{speed: .5, mode: 1, blur: 2}, spatial: 3}, {playback: nativePlaybackCase{speed: 1.5, mode: 2, blur: 2}, spatial: 3}, {playback: nativePlaybackCase{speed: .5}}} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			verifyNativeFilteredVideos(t, exe, dir, nativeSlowCLI(tc), nativeSlowReferenceGraph(t, tc))
		})
	}
}

func nativeRGB8Input(width, height int, pattern string) []byte {
	rgb := make([]byte, width*height*3)
	state := uint32(17)
	for i := 0; i < width*height; i++ {
		for c := 0; c < 3; c++ {
			v := byte(0)
			switch pattern {
			case "grid":
				v = byte((i%width)*(13+c*7) + (i/width)*(31+c*17))
			case "random":
				state ^= state << 13
				state ^= state >> 17
				state ^= state << 5
				v = byte(state)
			case "gray":
				v = byte(i)
			case "red":
				if c == 0 {
					v = byte(i)
				}
			case "green":
				if c == 1 {
					v = byte(i)
				}
			case "blue":
				if c == 2 {
					v = byte(i)
				}
			}
			rgb[i*3+c] = v
		}
	}
	return rgb
}
