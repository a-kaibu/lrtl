package lrtl

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
)

func TestVideoFadeNative755Timing(t *testing.T) {
	data, err := os.ReadFile("testdata/lrt-fade-7.5.5.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Frames                                                int
		FPS                                                   string
		Requested, Start, Duration, AudioStart, AudioDuration float64
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) != 16 {
		t.Fatal("expected sixteen native filter observations")
	}
	for _, tc := range cases {
		rate, err := parseFrameRate(tc.FPS)
		if err != nil {
			t.Fatal(err)
		}
		fade, err := planVideoFade(tc.Frames, rate, tc.FPS, tc.Requested)
		if err != nil || fade.start != tc.Start || float32(fade.duration) != float32(tc.Duration) || tc.Start != tc.AudioStart || tc.Duration != tc.AudioDuration {
			t.Fatalf("native %+v / %+v: %v", tc, fade, err)
		}
	}
	for _, seconds := range []float64{-1, math.NaN(), math.Inf(1), 1e-300} {
		if _, err := planVideoFade(20, frameRate{10, 1}, "10", seconds); err == nil {
			t.Fatalf("accepted %g", seconds)
		}
	}
}

func videoFadeFixture(t *testing.T, n int) string {
	t.Helper()
	dir := t.TempDir()
	img := image.NewNRGBA(image.Rect(0, 0, 64, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 64; x++ {
			img.SetNRGBA(x, y, color.NRGBA{64, 128, 192, 255})
		}
	}
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, img, &jpeg.Options{Quality: 100}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("frame%02d.jpg", i)), encoded.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestVideoFadeRGB48OrderingAndPrecision(t *testing.T) {
	dir := videoFadeFixture(t, 12)
	before := batchFileSnapshot(t, dir)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(t.TempDir(), "capture")
	t.Setenv("LRTL_TEST_FFMPEG_CAPTURE", capture)
	for _, blur := range []int{0, 2} {
		for _, watermark := range []string{"", "TEST"} {
			run := func(fade bool) []byte {
				args := []string{"render", dir, "--output", filepath.Join(t.TempDir(), "video.mp4"), "--fps", "4", "--ffmpeg", executable, "--motion-blur", strconv.Itoa(blur)}
				if watermark != "" {
					args = append(args, "--watermark", watermark)
				}
				if fade {
					args = append(args, "--fade-out", "1")
				}
				markerCommand(t, args...)
				data, err := os.ReadFile(capture)
				if err != nil {
					t.Fatal(err)
				}
				return data
			}
			control, faded := run(false), run(true)
			if len(control) != 12*64*32*6 || len(faded) != len(control) {
				t.Fatal("unexpected raw frame count")
			}
			for i := 0; i < 12; i++ {
				gain := 1.
				if i > 8 {
					gain = float64(12-i) / 4
				}
				for offset := i * 64 * 32 * 6; offset < (i+1)*64*32*6; offset += 2 {
					old := binary.LittleEndian.Uint16(control[offset:])
					got := binary.LittleEndian.Uint16(faded[offset:])
					want := uint16(math.Round(float64(old) * gain))
					if got != want {
						t.Fatalf("blur=%d watermark=%s frame=%d: %d want %d", blur, watermark, i, got, want)
					}
				}
			}
		}
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("fade modified input JPEGs")
	}
	img := image.NewNRGBA64(image.Rect(5, 6, 6, 7))
	img.SetNRGBA64(5, 6, color.NRGBA64{0x1234, 0x5678, 0x9abc, 0xeeee})
	p := color.NRGBA64Model.Convert(fadeVideoImage(img, .5).At(5, 6)).(color.NRGBA64)
	if p != (color.NRGBA64{0x091a, 0x2b3c, 0x4d5e, 0xeeee}) {
		t.Fatalf("16-bit fade/alpha: %+v", p)
	}
}

func TestVideoFadeAVIPixelsAndPassthrough(t *testing.T) {
	dir := videoFadeFixture(t, 12)
	output := filepath.Join(t.TempDir(), "fade.avi")
	markerCommand(t, "render", dir, "--output", output, "--fps", "4", "--quality", "100", "--fade-out", "1")
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(filepath.Join(dir, "frame00.jpg"))
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := jpeg.Decode(bytes.NewReader(original))
	if err != nil {
		t.Fatal(err)
	}
	base := color.NRGBAModel.Convert(decoded.At(32, 16)).(color.NRGBA)
	position := bytes.Index(data, []byte("movi")) + 4
	if position < 4 {
		t.Fatal("missing AVI movi")
	}
	for i := 0; i < 12; i++ {
		if position+8 > len(data) || string(data[position:position+4]) != "00dc" {
			t.Fatal("invalid AVI frame")
		}
		size := int(binary.LittleEndian.Uint32(data[position+4 : position+8]))
		position += 8
		if position+size > len(data) {
			t.Fatal("truncated AVI frame")
		}
		frame := data[position : position+size]
		if i <= 8 && !bytes.Equal(frame, original) {
			t.Fatal("unchanged early JPEG was recompressed")
		}
		img, err := jpeg.Decode(bytes.NewReader(frame))
		if err != nil {
			t.Fatal(err)
		}
		p := color.NRGBAModel.Convert(img.At(32, 16)).(color.NRGBA)
		gain := 1.
		if i > 8 {
			gain = float64(12-i) / 4
		}
		for c, old := range []uint8{base.R, base.G, base.B} {
			got := []uint8{p.R, p.G, p.B}[c]
			want := float64(old) * gain
			if math.Abs(float64(got)-want) > 3 {
				t.Fatalf("frame %d channel %d: %d want %g", i, c, got, want)
			}
		}
		position += size + size%2
	}
}

func TestVideoFadePresetBatchAndInvalidValues(t *testing.T) {
	dir := videoFadeFixture(t, 3)
	preset, err := decodeLRTRenderPreset(renderPresetObservations(t)[0].Preset)
	if err != nil {
		t.Fatal(err)
	}
	preset.FadeOut, preset.FadeTime, preset.OutputResV = true, 3, 32
	path := savePresetJSON(t, preset)
	options, err := applyLRTRenderPreset(RenderOptions{FPS: 24}, preset, path, nil)
	if err != nil || options.FadeOutSeconds != 3 {
		t.Fatalf("preset fade: %+v %v", options, err)
	}
	options, err = applyLRTRenderPreset(RenderOptions{FPS: 24, FadeOutSeconds: 0}, preset, path, map[string]bool{"fade-out": true})
	if err != nil || options.FadeOutSeconds != 0 {
		t.Fatal("explicit fade disable was ignored")
	}
	preset.FadeTime = -1
	if _, err := applyLRTRenderPreset(RenderOptions{FPS: 24}, preset, path, nil); err == nil {
		t.Fatal("accepted negative native fade time")
	}
	before := batchFileSnapshot(t, dir)
	for _, value := range []string{"-1", "NaN", "Inf"} {
		output := filepath.Join(t.TempDir(), "keep.avi")
		if err := os.WriteFile(output, []byte("existing video"), 0600); err != nil {
			t.Fatal(err)
		}
		var out, stderr bytes.Buffer
		if Main([]string{"render", dir, "--output", output, "--fade-out", value, "--overwrite"}, &out, &stderr) == 0 {
			t.Fatal("accepted invalid fade")
		}
		assertPreviewEVFileBytes(t, output, []byte("existing video"))
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("invalid fade changed input")
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(t.TempDir(), "capture")
	t.Setenv("LRTL_TEST_FFMPEG_CAPTURE", capture)
	audio := filepath.Join(t.TempDir(), "audio.wav")
	if err := os.WriteFile(audio, []byte("argument fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	markerCommand(t, "render", dir, "--output", filepath.Join(t.TempDir(), "native.mp4"), "--lrt-render-preset", path, "--audio", audio, "--ffmpeg", executable)
	argsData, err := os.ReadFile(capture + ".json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(argsData, []byte("afade=t=out:st=0.000:d=0.1001001")) {
		t.Fatalf("native audio fade missing: %s", argsData)
	}
	batch := t.TempDir()
	markerCommand(t, "batch-render", dir, "--output", batch, "--format", "avi", "--", "--fps", "4", "--fade-out", "1")
	files, err := filepath.Glob(filepath.Join(batch, "*.avi"))
	if err != nil || len(files) != 1 {
		t.Fatalf("batch AVI: %v %v", files, err)
	}
}

func TestVideoFadeActualAudioAndVideo(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("FFmpeg unavailable")
	}
	dir := videoFadeFixture(t, 20)
	audio := filepath.Join(t.TempDir(), "tone.wav")
	if data, err := exec.Command(ffmpeg, "-v", "error", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000", "-t", "2", "-c:a", "pcm_s16le", audio).CombinedOutput(); err != nil {
		t.Fatalf("test tone: %s %v", data, err)
	}
	output := filepath.Join(t.TempDir(), "fade.mp4")
	markerCommand(t, "render", dir, "--output", output, "--fps", "10", "--fade-out", "1", "--audio", audio, "--ffmpeg", ffmpeg, "--preset", "ultrafast", "--crf", "0")
	pcm, err := exec.Command(ffmpeg, "-v", "error", "-i", output, "-vn", "-ac", "1", "-ar", "48000", "-f", "f32le", "pipe:1").Output()
	if err != nil {
		t.Fatal(err)
	}
	rms := func(start, end float64) float64 {
		first, last := int(start*48000), int(end*48000)
		if last*4 > len(pcm) {
			t.Fatalf("truncated decoded audio: %d bytes", len(pcm))
		}
		var sum float64
		for i := first; i < last; i++ {
			value := float64(math.Float32frombits(binary.LittleEndian.Uint32(pcm[i*4:])))
			sum += value * value
		}
		return math.Sqrt(sum / float64(last-first))
	}
	if ratio := rms(1.7, 1.8) / rms(.7, .8); math.Abs(ratio-.25) > .04 {
		t.Fatalf("incorrect audio fade gain: %g", ratio)
	}
	rgb, err := exec.Command(ffmpeg, "-v", "error", "-i", output, "-an", "-f", "rawvideo", "-pix_fmt", "rgb24", "pipe:1").Output()
	if err != nil {
		t.Fatal(err)
	}
	if len(rgb) != 20*64*32*3 {
		t.Fatalf("decoded frame count: %d", len(rgb))
	}
	for _, i := range []int{0, 10, 15, 19} {
		gain := 1.
		if i > 10 {
			gain = float64(20-i) / 10
		}
		for c := 0; c < 3; c++ {
			base := rgb[(16*64+32)*3+c]
			got := rgb[i*64*32*3+(16*64+32)*3+c]
			if math.Abs(float64(got)-float64(base)*gain) > 3 {
				t.Fatalf("decoded frame %d channel %d: %d base %d", i, c, got, base)
			}
		}
	}
}
