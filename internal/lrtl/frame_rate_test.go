package lrtl

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestFrameRateParsingAndNative755(t *testing.T) {
	data, err := os.ReadFile("testdata/lrt-frame-rate-7.5.5.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Input  string
		Native float32
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) != 9 {
		t.Fatal("expected nine native frame-rate observations")
	}
	for _, tc := range cases {
		rate, err := parseFrameRate(tc.Input)
		if err != nil || float32(float64(rate.numerator)/float64(rate.denominator)) != tc.Native {
			t.Fatalf("native %s: %v %v vs %v", tc.Input, rate, err, tc.Native)
		}
	}
	for input, want := range map[string]string{"23.976": "24000/1001", "29.9700": "30000/1001", "029.970": "30000/1001", "47.952": "48000/1001", "59.94": "60000/1001", "24000/1001": "24000/1001", "2997/100": "2997/100", "24.5": "49/2", "0024": "24", "048/002": "24", "240": "240", " 25 ": "25"} {
		rate, err := parseFrameRate(input)
		if err != nil || rate.String() != want {
			t.Fatalf("%q: %s %v, want %s", input, rate, err, want)
		}
	}
	for _, input := range []string{"", "0", "-1", "0.99", "241", "240.001", "NaN", "Inf", "1e2", "24/0", "1/2", "1/0.5", "24//1", "24/1/1", "24.", ".5", "+24", "4294967296/17895699", "1.00000000001", strings.Repeat("1", 65)} {
		if _, err := parseFrameRate(input); err == nil {
			t.Fatalf("accepted invalid rate %q", input)
		}
	}
}

func TestFractionalFrameOffsets(t *testing.T) {
	rate, err := parseFrameRate("29.97")
	if err != nil {
		t.Fatal(err)
	}
	for frames, want := range map[int64]int64{0: 0, 1: 33, 2: 67, 3: 100, 30: 1001, 30000: 1001000, 3000000: 100100000, 3000000000000: 100100000000000} {
		if got, err := rate.offsetMilliseconds(frames); err != nil || got != want {
			t.Fatalf("frame %d: %d %v, want %d", frames, got, err, want)
		}
	}
	if _, err := rate.offsetMilliseconds(math.MaxInt64); err == nil {
		t.Fatal("accepted overflow duration")
	}
	if _, err := rate.offsetMilliseconds(-1); err == nil {
		t.Fatal("accepted negative offset")
	}
	for _, fps := range []int64{1, 24, 25, 30, 60, 240} {
		for n := int64(0); n < 1000; n++ {
			got, err := (frameRate{uint32(fps), 1}).offsetMilliseconds(n)
			if err != nil || got != frameOffsetMilliseconds(n, fps) {
				t.Fatal("integer subtitle rounding changed")
			}
		}
	}
}

func TestFractionalAVIHeaderPayloadAndCLI(t *testing.T) {
	frames := cropFixture(t, 3)
	dir := filepath.Dir(frames[0].Image)
	before := batchFileSnapshot(t, dir)
	control := filepath.Join(t.TempDir(), "control.avi")
	markerCommand(t, "render", dir, "--output", control, "--fps", "24")
	controlBytes, err := os.ReadFile(control)
	if err != nil {
		t.Fatal(err)
	}
	// The dependency includes wall-clock creation time in strn. Compare every
	// other byte without requiring all renders to finish in the same second.
	normalizeCreationTime := func(data []byte) {
		movi := bytes.Index(data, []byte("movi"))
		if movi < 0 {
			t.Fatal("missing AVI movie list")
		}
		position := bytes.Index(data[:movi], []byte("strn"))
		if position < 0 || position+8 > movi {
			t.Fatal("missing AVI stream description")
		}
		size := int(binary.LittleEndian.Uint32(data[position+4 : position+8]))
		end := position + 8 + size
		prefix := []byte("Created with https://github.com/icza/mjpeg at ")
		if end > movi || !bytes.HasPrefix(data[position+8:end], prefix) {
			t.Fatal("unexpected AVI creation description")
		}
		clear(data[position+8+len(prefix) : end])
	}
	normalizeCreationTime(controlBytes)
	for _, input := range []string{"23.976", "29.97", "47.952", "59.94", "49/2", "2997/100", "24000/1001"} {
		t.Run(input, func(t *testing.T) {
			rate, err := parseFrameRate(input)
			if err != nil {
				t.Fatal(err)
			}
			output := filepath.Join(t.TempDir(), "video.avi")
			markerCommand(t, "render", dir, "--output", output, "--fps", input)
			data, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			if binary.LittleEndian.Uint32(data[128:132]) != rate.denominator || binary.LittleEndian.Uint32(data[132:136]) != rate.numerator {
				t.Fatalf("incorrect AVI rate for %s", input)
			}
			wantDelay := uint32(math.Round(1e6 * float64(rate.denominator) / float64(rate.numerator)))
			if binary.LittleEndian.Uint32(data[32:36]) != wantDelay {
				t.Fatal("incorrect main AVI frame interval")
			}
			copy(data[32:36], controlBytes[32:36])
			copy(data[128:136], controlBytes[128:136])
			normalizeCreationTime(data)
			if !bytes.Equal(data, controlBytes) {
				t.Fatal("rate change altered JPEG payload, index or other headers")
			}
			if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
				t.Fatal("render changed source files")
			}
			if leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(output), ".lrtl-*")); len(leftovers) != 0 {
				t.Fatalf("temporary files remain: %v", leftovers)
			}
		})
	}
	bad := filepath.Join(t.TempDir(), "bad.avi")
	badBytes := bytes.Repeat([]byte{'x'}, 200)
	if err := os.WriteFile(bad, badBytes, 0600); err != nil {
		t.Fatal(err)
	}
	if err := setGeneratedAVIRate(bad, frameRate{30000, 1001}); err == nil {
		t.Fatal("patched unexpected AVI layout")
	}
	assertPreviewEVFileBytes(t, bad, badBytes)
	for _, command := range []string{"render", "subtitles"} {
		output := filepath.Join(t.TempDir(), "existing.avi")
		if command == "subtitles" {
			output = filepath.Join(t.TempDir(), "existing.srt")
		}
		if err := os.WriteFile(output, []byte("keep output"), 0600); err != nil {
			t.Fatal(err)
		}
		var out, stderr bytes.Buffer
		if Main([]string{command, dir, "--output", output, "--fps", "1/0", "--overwrite"}, &out, &stderr) == 0 {
			t.Fatal("accepted invalid fps")
		}
		assertPreviewEVFileBytes(t, output, []byte("keep output"))
	}
}

func TestFractionalSubtitlesAndBatchAVI(t *testing.T) {
	root := t.TempDir()
	dir := batchInterpolationFixture(t, root, "sequence", false)
	frames, err := DiscoverSequence(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, frame := range frames {
		packet, err := PacketForFrame(frame)
		if err != nil {
			t.Fatal(err)
		}
		if err := packet.SetScalar("xmp:CreateDate", "2026-10-19T12:00:00Z"); err != nil {
			t.Fatal(err)
		}
		persistExposurePacket(t, frame, packet)
	}
	var first []byte
	for _, fps := range []string{"29.97", "30000/1001"} {
		output := filepath.Join(t.TempDir(), "time.srt")
		markerCommand(t, "subtitles", dir, "--output", output, "--fps", fps)
		data, err := os.ReadFile(output)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(data, []byte("00:00:00,133 --> 00:00:00,167")) {
			t.Fatalf("incorrect fifth-frame timing: %s", data)
		}
		if first == nil {
			first = data
		} else if !bytes.Equal(first, data) {
			t.Fatal("decimal alias and rational timing differ")
		}
	}
	output := t.TempDir()
	markerCommand(t, "batch-render", root, "--output", output, "--format", "avi", "--", "--fps", "29.97")
	files, err := filepath.Glob(filepath.Join(output, "*.avi"))
	if err != nil || len(files) != 1 {
		t.Fatalf("batch output: %v %v", files, err)
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint32(data[128:132]) != 1001 || binary.LittleEndian.Uint32(data[132:136]) != 30000 {
		t.Fatal("batch lost rational frame rate")
	}
}

func TestFractionalFFmpegArgumentsAndAudioDuration(t *testing.T) {
	frames := cropFixture(t, 3)
	capture := filepath.Join(t.TempDir(), "capture")
	t.Setenv("LRTL_TEST_FFMPEG_CAPTURE", capture)
	audio := filepath.Join(t.TempDir(), "audio.wav")
	if err := os.WriteFile(audio, []byte("fake audio for argument capture"), 0600); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, codec := range []string{"h264", "h265", "prores", "dnxhr-hqx"} {
		ext := "mp4"
		if codec == "prores" || codec == "dnxhr-hqx" {
			ext = "mov"
		}
		output := filepath.Join(t.TempDir(), "out."+ext)
		if err := RenderSequence(frames, output, RenderOptions{FrameRate: "29.97", Quality: 90, Codec: codec, CRF: 18, Preset: "medium", FFmpegPath: executable, AudioPath: audio}); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(capture + ".json")
		if err != nil {
			t.Fatal(err)
		}
		var args []string
		if err := json.Unmarshal(data, &args); err != nil {
			t.Fatal(err)
		}
		joined := strings.Join(args, " ")
		if !strings.Contains(joined, "-framerate 30000/1001") || !strings.Contains(joined, "atrim=duration=0.100100000") || !strings.Contains(joined, "-t 0.100100000") {
			t.Fatalf("incorrect timing: %s", joined)
		}
	}
}

func TestFractionalVideoProbe(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("FFmpeg unavailable")
	}
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe unavailable")
	}
	frames := cropFixture(t, 3)
	for _, format := range []string{"avi", "mp4", "mov"} {
		output := filepath.Join(t.TempDir(), "actual."+format)
		options := RenderOptions{FrameRate: "30000/1001", Quality: 90, FFmpegPath: ffmpeg, CRF: 18, Preset: "ultrafast"}
		if format == "mov" {
			options.Codec = "prores"
		}
		if err := RenderSequence(frames, output, options); err != nil {
			t.Fatal(err)
		}
		data, err := exec.Command(ffprobe, "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=avg_frame_rate,nb_frames,duration", "-of", "json", output).CombinedOutput()
		if err != nil {
			t.Fatalf("ffprobe: %s %v", data, err)
		}
		var result struct {
			Streams []struct {
				Rate     string `json:"avg_frame_rate"`
				Frames   string `json:"nb_frames"`
				Duration string `json:"duration"`
			}
		}
		if err := json.Unmarshal(data, &result); err != nil {
			t.Fatal(err)
		}
		if len(result.Streams) != 1 || result.Streams[0].Rate != "30000/1001" || result.Streams[0].Frames != "3" {
			t.Fatalf("incorrect encoded timing: %s", data)
		}
		duration, err := strconv.ParseFloat(result.Streams[0].Duration, 64)
		if err != nil || math.Abs(duration-.1001) > 1e-6 {
			t.Fatalf("duration: %s %v", data, err)
		}
	}
	// A short audio input must be padded to the rational video duration.
	audio := filepath.Join(t.TempDir(), "short.wav")
	if data, err := exec.Command(ffmpeg, "-v", "error", "-f", "lavfi", "-i", "anullsrc=r=48000:cl=mono", "-t", "0.25", "-c:a", "pcm_s16le", audio).CombinedOutput(); err != nil {
		t.Fatalf("create test audio: %s %v", data, err)
	}
	longer := make([]Frame, 30)
	for i := range longer {
		longer[i] = frames[0]
	}
	output := filepath.Join(t.TempDir(), "audio.mp4")
	if err := RenderSequence(longer, output, RenderOptions{FrameRate: "29.97", Quality: 90, FFmpegPath: ffmpeg, CRF: 18, Preset: "ultrafast", AudioPath: audio}); err != nil {
		t.Fatal(err)
	}
	data, err := exec.Command(ffprobe, "-v", "error", "-show_entries", "stream=codec_type,avg_frame_rate,nb_frames,duration", "-of", "json", output).CombinedOutput()
	if err != nil {
		t.Fatalf("probe audio video: %s %v", data, err)
	}
	var result struct {
		Streams []struct {
			Type     string `json:"codec_type"`
			Rate     string `json:"avg_frame_rate"`
			Frames   string `json:"nb_frames"`
			Duration string `json:"duration"`
		}
	}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Streams) != 2 {
		t.Fatalf("expected video and audio: %s", data)
	}
	for _, stream := range result.Streams {
		duration, err := strconv.ParseFloat(stream.Duration, 64)
		if err != nil || math.Abs(duration-1.001) > .002 {
			t.Fatalf("audio/video timing: %s %v", data, err)
		}
		if stream.Type == "video" && (stream.Rate != "30000/1001" || stream.Frames != "30") {
			t.Fatalf("video timing: %s", data)
		}
	}
}
