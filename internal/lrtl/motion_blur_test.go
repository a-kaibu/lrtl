package lrtl

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

type motionTrimObservation struct {
	Window       int
	Speed        float64
	SourceFrames int
	Trim, Fade   string
	Temporal     string
	Luma         []uint16
}

func motionTrimObservations(t *testing.T) []motionTrimObservation {
	t.Helper()
	data, err := os.ReadFile("testdata/lrt-motion-trim-7.5.5.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []motionTrimObservation
	if err := json.Unmarshal(data, &cases); err != nil || len(cases) != 48 {
		t.Fatalf("expected 48 native temporal observations: %v", err)
	}
	return cases
}

func TestMotionBlurNative755TrimCountAndFade(t *testing.T) {
	for _, tc := range motionTrimObservations(t) {
		plan, err := planVideoFrames(tc.SourceFrames, tc.Speed, tc.Window)
		if len(tc.Luma) == 0 {
			if err == nil {
				t.Fatalf("accepted native empty output: %+v", tc)
			}
			continue
		}
		if err != nil || plan.count() != len(tc.Luma) || plan.start != tc.Window {
			t.Fatalf("native %+v vs %+v: %v", tc, plan, err)
		}
		if tc.Window == 0 {
			if tc.Trim != "" {
				t.Fatal("native disabled blur unexpectedly trims")
			}
		} else if tc.Trim != fmt.Sprintf("trim=start_frame=%d:end_frame=%d", plan.start, plan.end) {
			t.Fatalf("native %s vs %+v", tc.Trim, plan)
		}
		fade, err := planVideoFade(plan.count(), frameRate{24, 1}, "24", 1)
		var start, duration float64
		if n, parseErr := fmt.Sscanf(tc.Fade, "fade=t=out:st=%f:d=%f", &start, &duration); parseErr != nil || n != 2 {
			t.Fatalf("native fade fragment %s: %v", tc.Fade, parseErr)
		}
		if err != nil || fade.start != start || float32(fade.duration) != float32(duration) {
			t.Fatalf("trimmed fade %+v vs %s: %v", fade, tc.Fade, err)
		}
	}
	for _, window := range []int{-1, 1, 6, 21} {
		if _, err := planVideoFrames(31, 1, window); err == nil {
			t.Fatalf("accepted invalid window %d", window)
		}
	}
	if _, err := planVideoFrames(31, math.NaN(), 2); err == nil {
		t.Fatal("accepted invalid speed")
	}
}

func TestMotionBlurActualFFmpegTemporalReference(t *testing.T) {
	executable := os.Getenv("LRTL_MOTION_FFMPEG")
	if executable == "" {
		var err error
		executable, err = exec.LookPath("ffmpeg")
		if err != nil {
			t.Skip("FFmpeg unavailable; stored native observations remain covered")
		}
	}
	for _, tc := range motionTrimObservations(t) {
		raw := make([]byte, tc.SourceFrames*3072)
		for i := 0; i < tc.SourceFrames; i++ {
			for plane := 0; plane < 3; plane++ {
				value := uint16(512)
				if plane == 0 {
					value = uint16(100 + i*20)
				}
				for pixel := 0; pixel < 512; pixel++ {
					binary.LittleEndian.PutUint16(raw[i*3072+plane*1024+pixel*2:], value)
				}
			}
		}
		cmd := exec.Command(executable, "-v", "error", "-f", "rawvideo", "-pixel_format", "yuv444p10le", "-video_size", "32x16", "-framerate", "24", "-i", "pipe:0", "-vf", tc.Temporal, "-fps_mode", "passthrough", "-f", "rawvideo", "-pix_fmt", "yuv444p10le", "pipe:1")
		cmd.Stdin = bytes.NewReader(raw)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		output, err := cmd.Output()
		if err != nil || len(output) != len(tc.Luma)*3072 {
			t.Fatalf("window=%d speed=%g source=%d: bytes=%d %v %s", tc.Window, tc.Speed, tc.SourceFrames, len(output), err, stderr.String())
		}
		for i, expected := range tc.Luma {
			for plane := 0; plane < 3; plane++ {
				want := expected
				if plane > 0 {
					want = 512
				}
				for pixel := 0; pixel < 512; pixel++ {
					if got := binary.LittleEndian.Uint16(output[i*3072+plane*1024+pixel*2:]); got != want {
						t.Fatalf("native temporal replay frame=%d plane=%d pixel=%d: %d want %d", i, plane, pixel, got, want)
					}
				}
			}
		}
	}
}

func motionTrimFixture(t *testing.T, count int) string {
	t.Helper()
	dir := t.TempDir()
	for i := 0; i < count; i++ {
		img := image.NewGray16(image.Rect(0, 0, 32, 16))
		for y := 0; y < 16; y++ {
			for x := 0; x < 32; x++ {
				img.SetGray16(x, y, color.Gray16{uint16((100 + i*20) * 64)})
			}
		}
		var data bytes.Buffer
		if err := png.Encode(&data, img); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, fmt.Sprintf("frame%02d.png", i))
		if err := os.WriteFile(path, data.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		writeTestXMP(t, path, map[string]string{"xmp:CreateDate": fmt.Sprintf("2026-10-20T12:00:%02dZ", i)})
	}
	return dir
}

func TestMotionBlurTrimmedRenderMatchesNativeTimeline(t *testing.T) {
	dirs := map[int]string{6: motionTrimFixture(t, 6), 31: motionTrimFixture(t, 31)}
	before := map[int]map[string][]byte{}
	for count, dir := range dirs {
		before[count] = batchFileSnapshot(t, dir)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(t.TempDir(), "capture")
	t.Setenv("LRTL_TEST_FFMPEG_CAPTURE", capture)
	for _, tc := range motionTrimObservations(t) {
		if len(tc.Luma) == 0 {
			continue
		}
		result := markerCommand(t, "render", dirs[tc.SourceFrames], "--output", filepath.Join(t.TempDir(), "video.mp4"), "--ffmpeg", executable, "--fps", "24", "--motion-blur", strconv.Itoa(tc.Window), "--speed", strconv.FormatFloat(tc.Speed, 'g', -1, 64))
		if !strings.Contains(result, fmt.Sprintf("Rendered %d frames", len(tc.Luma))) {
			t.Fatal(result)
		}
		data, err := os.ReadFile(capture)
		if err != nil || len(data) != len(tc.Luma)*32*16*6 {
			t.Fatalf("capture length %d: %v", len(data), err)
		}
		for i, expected := range tc.Luma {
			for pixel := 0; pixel < 32*16*3; pixel++ {
				got := binary.LittleEndian.Uint16(data[i*3072+pixel*2:])
				// Native plane arithmetic rounds to 10 bits; independent RGB16
				// blending retains its lower bits. Compare within half a Y10 code.
				if math.Abs(float64(got)/64-float64(expected)) > .51 {
					t.Fatalf("window=%d speed=%g output=%d: RGB16=%d native Y10=%d", tc.Window, tc.Speed, i, got, expected)
				}
			}
		}
	}
	for count, dir := range dirs {
		if !reflect.DeepEqual(before[count], batchFileSnapshot(t, dir)) {
			t.Fatal("trimmed render changed sources/XMP")
		}
	}
}

func TestMotionBlurAudioSubtitlesAVIAndFailureRetention(t *testing.T) {
	dir := motionTrimFixture(t, 12)
	before := batchFileSnapshot(t, dir)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(t.TempDir(), "capture")
	t.Setenv("LRTL_TEST_FFMPEG_CAPTURE", capture)
	audio := filepath.Join(t.TempDir(), "audio.wav")
	if err := os.WriteFile(audio, []byte("audio argument fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	markerCommand(t, "render", dir, "--output", filepath.Join(t.TempDir(), "video.mp4"), "--ffmpeg", executable, "--fps", "4", "--motion-blur", "5", "--audio", audio, "--fade-out", "1")
	argsData, err := os.ReadFile(capture + ".json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(argsData, []byte("atrim=duration=1.750000000,afade=t=out:st=0.750:d=1")) {
		t.Fatalf("audio duration/fade failed to follow trim: %s", argsData)
	}
	for _, speed := range []float64{.5, 2} {
		output := filepath.Join(t.TempDir(), "capture.srt")
		markerCommand(t, "subtitles", dir, "--output", output, "--fps", "4", "--speed", strconv.FormatFloat(speed, 'g', -1, 64), "--motion-blur", "2", "--timestamp-format", "05")
		data, err := os.ReadFile(output)
		if err != nil {
			t.Fatal(err)
		}
		blocks := strings.Split(strings.TrimSpace(string(data)), "\n\n")
		wantCount := int(12/speed) - 2
		if len(blocks) != wantCount {
			t.Fatalf("SRT blocks %d want %d", len(blocks), wantCount)
		}
		for i, block := range blocks {
			source := int(math.Ceil(float64(i+3)*speed)) - 1
			want := fmt.Sprintf("%d\n%s --> %s\n%02d", i+1, srtTimecode(int64(i)*250), srtTimecode(int64(i+1)*250), source)
			if block != want {
				t.Fatalf("trimmed SRT block %q want %q", block, want)
			}
		}
	}
	avi := filepath.Join(t.TempDir(), "video.avi")
	markerCommand(t, "render", dir, "--output", avi, "--motion-blur", "2", "--speed", "2", "--quality", "100", "--fps", "29.97")
	data, err := os.ReadFile(avi)
	if err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint32(data[48:]) != 4 || binary.LittleEndian.Uint32(data[140:]) != 4 {
		t.Fatal("AVI headers did not use trimmed count")
	}
	position := bytes.Index(data, []byte("movi")) + 4
	for i := 0; i < 4; i++ {
		if position < 4 || position+8 > len(data) || string(data[position:position+4]) != "00dc" {
			t.Fatal("invalid AVI frame")
		}
		size := int(binary.LittleEndian.Uint32(data[position+4:]))
		position += 8
		if size < 0 || position+size > len(data) {
			t.Fatal("truncated AVI JPEG")
		}
		img, err := jpeg.Decode(bytes.NewReader(data[position : position+size]))
		if err != nil {
			t.Fatal(err)
		}
		// Target 2 after 2x scheduling mixes source IDs 3 and 5.
		want := ((100+float64(3+i*2)*20)*.354 + 100 + float64(5+i*2)*20) / 1.354 * 64 / 257
		got := color.GrayModel.Convert(img.At(16, 8)).(color.Gray).Y
		if math.Abs(float64(got)-want) > 2 {
			t.Fatalf("AVI frame %d source alignment: %d want %g", i, got, want)
		}
		position += size + size%2
	}
	for _, command := range []string{"render", "subtitles"} {
		ext := ".avi"
		if command == "subtitles" {
			ext = ".srt"
		}
		output := filepath.Join(t.TempDir(), "keep"+ext)
		if err := os.WriteFile(output, []byte("keep original"), 0600); err != nil {
			t.Fatal(err)
		}
		var out, stderr bytes.Buffer
		if Main([]string{command, dir, "--output", output, "--motion-blur", "20", "--overwrite"}, &out, &stderr) == 0 {
			t.Fatal("published empty trimmed output")
		}
		assertPreviewEVFileBytes(t, output, []byte("keep original"))
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("video/subtitles changed input files")
	}
	// The discarded prefix is still decoded and validated before publication.
	if err := os.WriteFile(filepath.Join(dir, "frame01.png"), []byte("broken warm-up frame"), 0600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "keep.avi")
	if err := os.WriteFile(output, []byte("keep original"), 0600); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	if Main([]string{"render", dir, "--output", output, "--motion-blur", "2", "--overwrite"}, &out, &stderr) == 0 {
		t.Fatal("ignored corrupt warm-up source")
	}
	assertPreviewEVFileBytes(t, output, []byte("keep original"))
}

func TestMotionBlurActualVideoAndAudioDuration(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("FFmpeg unavailable")
	}
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe unavailable")
	}
	dir := videoFadeFixture(t, 20)
	audio := filepath.Join(t.TempDir(), "tone.wav")
	if data, err := exec.Command(ffmpeg, "-v", "error", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000", "-t", "3", "-c:a", "pcm_s16le", audio).CombinedOutput(); err != nil {
		t.Fatalf("test tone: %s %v", data, err)
	}
	output := filepath.Join(t.TempDir(), "motion.mp4")
	markerCommand(t, "render", dir, "--output", output, "--ffmpeg", ffmpeg, "--fps", "4", "--speed", "2", "--motion-blur", "2", "--fade-out", "1", "--audio", audio, "--crf", "0", "--preset", "ultrafast")
	data, err := exec.Command(ffprobe, "-v", "error", "-show_entries", "stream=codec_type,duration,nb_frames", "-of", "json", output).CombinedOutput()
	if err != nil {
		t.Fatalf("ffprobe: %s %v", data, err)
	}
	var probe struct {
		Streams []struct {
			Type     string `json:"codec_type"`
			Duration string `json:"duration"`
			Frames   string `json:"nb_frames"`
		}
	}
	if err := json.Unmarshal(data, &probe); err != nil || len(probe.Streams) != 2 {
		t.Fatalf("actual streams: %s %v", data, err)
	}
	for _, stream := range probe.Streams {
		duration, err := strconv.ParseFloat(stream.Duration, 64)
		if err != nil || math.Abs(duration-2) > .001 || (stream.Type == "video" && stream.Frames != "8") {
			t.Fatalf("trimmed audio/video duration or frame count: %s", data)
		}
	}
	pcm, err := exec.Command(ffmpeg, "-v", "error", "-i", output, "-vn", "-ac", "1", "-ar", "48000", "-f", "f32le", "pipe:1").Output()
	if err != nil {
		t.Fatal(err)
	}
	rms := func(start, end int) float64 {
		if end*4 > len(pcm) {
			t.Fatal("truncated decoded audio")
		}
		sum := 0.
		for i := start; i < end; i++ {
			v := float64(math.Float32frombits(binary.LittleEndian.Uint32(pcm[i*4:])))
			sum += v * v
		}
		return math.Sqrt(sum / float64(end-start))
	}
	if ratio := rms(81600, 86400) / rms(33600, 38400); math.Abs(ratio-.25) > .04 {
		t.Fatalf("audio fade did not follow trimmed duration: %g", ratio)
	}
	rgb, err := exec.Command(ffmpeg, "-v", "error", "-i", output, "-an", "-f", "rawvideo", "-pix_fmt", "rgb24", "pipe:1").Output()
	if err != nil || len(rgb) != 8*64*32*3 {
		t.Fatalf("decoded trimmed video count: bytes=%d %v", len(rgb), err)
	}
	for _, i := range []int{0, 4, 6, 7} {
		gain := math.Min(1, float64(8-i)/4)
		for channel := 0; channel < 3; channel++ {
			base := rgb[(16*64+32)*3+channel]
			value := rgb[i*64*32*3+(16*64+32)*3+channel]
			if math.Abs(float64(value)-float64(base)*gain) > 3 {
				t.Fatalf("video fade did not follow trimmed duration: frame=%d got=%d", i, value)
			}
		}
	}
}
