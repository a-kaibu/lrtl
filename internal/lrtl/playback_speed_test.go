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

type speedObservation struct {
	FPS     string
	Speed   float64
	Filter  string
	Frames  int
	Indices []int
}

func speedObservations(t *testing.T) []speedObservation {
	t.Helper()
	data, err := os.ReadFile("testdata/lrt-speed-7.5.5.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []speedObservation
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) != 80 {
		t.Fatal("expected eighty native-filter/FFmpeg mapping observations")
	}
	return cases
}

func TestPlaybackSpeedNativeFilterMappings(t *testing.T) {
	for _, tc := range speedObservations(t) {
		speed, err := validatePlaybackSpeed(tc.Speed)
		if err != nil {
			t.Fatal(err)
		}
		count, err := playbackFrameCount(tc.Frames, speed)
		if len(tc.Indices) == 0 {
			if err == nil {
				t.Fatal("accepted speed producing no frames")
			}
			continue
		}
		if err != nil || count != len(tc.Indices) {
			t.Fatalf("%g/%d count %d native-filter %d: %v", tc.Speed, tc.Frames, count, len(tc.Indices), err)
		}
		indices := []int{}
		for i := 0; i < tc.Frames; i++ {
			for end := playbackOutputEnd(i+1, speed); len(indices) < end; {
				indices = append(indices, i)
			}
		}
		if !reflect.DeepEqual(indices, tc.Indices) {
			t.Fatalf("%s/%g/%d: %v vs %v", tc.FPS, tc.Speed, tc.Frames, indices, tc.Indices)
		}
	}
	for _, speed := range []float64{0, -1, .01, 17, math.NaN(), math.Inf(1)} {
		if _, err := validatePlaybackSpeed(speed); err == nil {
			t.Fatalf("accepted %g", speed)
		}
	}
}

func speedImageFixture(t *testing.T, n int) string {
	t.Helper()
	dir := t.TempDir()
	for i := 0; i < n; i++ {
		img := image.NewNRGBA64(image.Rect(0, 0, 64, 32))
		v := uint16(1000 + i*4000)
		for y := 0; y < 32; y++ {
			for x := 0; x < 64; x++ {
				img.SetNRGBA64(x, y, color.NRGBA64{v, v, v, 65535})
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
		writeTestXMP(t, path, map[string]string{"xmp:CreateDate": fmt.Sprintf("2026-10-20T12:00:%02dZ", i), "crs:Exposure2012": strconv.FormatFloat(float64(i)/20, 'f', 2, 64)})
	}
	return dir
}

func TestPlaybackSpeedRGBDataOrderAndEffects(t *testing.T) {
	dir := speedImageFixture(t, 10)
	before := batchFileSnapshot(t, dir)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(t.TempDir(), "capture")
	t.Setenv("LRTL_TEST_FFMPEG_CAPTURE", capture)
	run := func(extra ...string) ([]byte, string) {
		args := []string{"render", dir, "--output", filepath.Join(t.TempDir(), "video.mp4"), "--fps", "4", "--ffmpeg", executable, "--apply-xmp-basic"}
		result := markerCommand(t, append(args, extra...)...)
		data, err := os.ReadFile(capture)
		if err != nil {
			t.Fatal(err)
		}
		return data, result
	}
	control, _ := run()
	frameSize := 64 * 32 * 6
	for _, tc := range speedObservations(t) {
		if tc.FPS != "24" || tc.Frames != 10 || len(tc.Indices) == 0 {
			continue
		}
		data, result := run("--speed", strconv.FormatFloat(tc.Speed, 'g', -1, 64))
		if len(data) != len(tc.Indices)*frameSize || !strings.Contains(result, fmt.Sprintf("Rendered %d frames", len(tc.Indices))) {
			t.Fatalf("speed %g output count: %d %s", tc.Speed, len(data), result)
		}
		for i, source := range tc.Indices {
			if !bytes.Equal(data[i*frameSize:(i+1)*frameSize], control[source*frameSize:(source+1)*frameSize]) {
				t.Fatalf("speed %g output %d should use source %d", tc.Speed, i, source)
			}
		}
	}
	stamped, _ := run("--timestamp-format", "05")
	stampedFast, _ := run("--timestamp-format", "05", "--speed", "2")
	for i, source := range []int{1, 3, 5, 7, 9} {
		if !bytes.Equal(stampedFast[i*frameSize:(i+1)*frameSize], stamped[source*frameSize:(source+1)*frameSize]) {
			t.Fatal("capture timestamp overlay lost source correspondence")
		}
	}
	// Speed must precede Motion Blur; fade must follow the blend, and repeated
	// source buffers must not accumulate gain changes across output frames.
	data, _ := run("--speed", "0.5", "--motion-blur", "2", "--fade-out", "2")
	rate := frameRate{4, 1}
	fade, err := planVideoFade(20, rate, "4", 2)
	if err != nil {
		t.Fatal(err)
	}
	weights := lrtMotionBlurWeights(2)
	for i := 0; i < 20; i++ {
		a, b := (max(0, i-1))/2, i/2
		for c := 0; c < frameSize; c += 2 {
			oldA := binary.LittleEndian.Uint16(control[a*frameSize+c:])
			oldB := binary.LittleEndian.Uint16(control[b*frameSize+c:])
			blended := math.RoundToEven((float64(oldA)*float64(weights[0]) + float64(oldB)*float64(weights[1])) / (float64(weights[0]) + float64(weights[1])))
			want := uint16(math.Round(blended * fade.gain(i, rate)))
			got := binary.LittleEndian.Uint16(data[i*frameSize+c:])
			if got != want {
				t.Fatalf("ordered effects output %d channel %d: %d want %d", i, c, got, want)
			}
		}
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("speed changed source files")
	}
}

func TestPlaybackSpeedAVIAndAudioArguments(t *testing.T) {
	dir := speedImageFixture(t, 10)
	output := filepath.Join(t.TempDir(), "fast.avi")
	markerCommand(t, "render", dir, "--output", output, "--fps", "4", "--speed", "2", "--quality", "100")
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint32(data[48:52]) != 5 || binary.LittleEndian.Uint32(data[140:144]) != 5 {
		t.Fatal("AVI frame counts were not resampled")
	}
	position := bytes.Index(data, []byte("movi")) + 4
	for i, source := range []int{1, 3, 5, 7, 9} {
		if position < 4 || position+8 > len(data) || string(data[position:position+4]) != "00dc" {
			t.Fatal("invalid AVI")
		}
		size := int(binary.LittleEndian.Uint32(data[position+4 : position+8]))
		position += 8
		if position+size > len(data) {
			t.Fatal("truncated JPEG")
		}
		img, err := jpeg.Decode(bytes.NewReader(data[position : position+size]))
		if err != nil {
			t.Fatal(err)
		}
		p := color.GrayModel.Convert(img.At(32, 16)).(color.Gray)
		want := float64(1000+source*4000) / 257
		if math.Abs(float64(p.Y)-want) > 2 {
			t.Fatalf("AVI output %d: %d want source %d (%g)", i, p.Y, source, want)
		}
		position += size + size%2
	}
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
	markerCommand(t, "render", dir, "--output", filepath.Join(t.TempDir(), "audio.mp4"), "--fps", "4", "--speed", "2", "--fade-out", "1", "--audio", audio, "--ffmpeg", executable)
	args, err := os.ReadFile(capture + ".json")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"1.250000000", "-frames:v", "afade=t=out:st=0.250:d=1"} {
		if !bytes.Contains(args, []byte(want)) {
			t.Fatalf("missing %s: %s", want, args)
		}
	}
}

func TestPlaybackSpeedPresetBatchAndRejectedInputs(t *testing.T) {
	dir := speedImageFixture(t, 7)
	preset, err := decodeLRTRenderPreset(renderPresetObservations(t)[0].Preset)
	if err != nil {
		t.Fatal(err)
	}
	preset.Speed, preset.OutputResV = 2, 32
	path := savePresetJSON(t, preset)
	options, err := applyLRTRenderPreset(RenderOptions{FPS: 24}, preset, path, nil)
	if err != nil || options.PlaybackSpeed != 2 {
		t.Fatalf("native speed import: %+v %v", options, err)
	}
	options, err = applyLRTRenderPreset(RenderOptions{FPS: 24, PlaybackSpeed: .5}, preset, path, map[string]bool{"speed": true})
	if err != nil || options.PlaybackSpeed != .5 {
		t.Fatal("speed override was ignored")
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(t.TempDir(), "capture")
	t.Setenv("LRTL_TEST_FFMPEG_CAPTURE", capture)
	result := markerCommand(t, "render", dir, "--output", filepath.Join(t.TempDir(), "native.mp4"), "--lrt-render-preset", path, "--ffmpeg", executable)
	if !strings.Contains(result, "Rendered 3 frames") {
		t.Fatal(result)
	}
	batch := t.TempDir()
	markerCommand(t, "batch-render", dir, "--output", batch, "--format", "avi", "--", "--speed", "2")
	files, err := filepath.Glob(filepath.Join(batch, "*.avi"))
	if err != nil || len(files) != 1 {
		t.Fatalf("batch output: %v %v", files, err)
	}
	before := batchFileSnapshot(t, dir)
	for _, speed := range []string{"0", "-1", "17", "NaN", "Inf", "16"} {
		output := filepath.Join(t.TempDir(), "keep.avi")
		if err := os.WriteFile(output, []byte("keep old output"), 0600); err != nil {
			t.Fatal(err)
		}
		var out, stderr bytes.Buffer
		if Main([]string{"render", dir, "--output", output, "--speed", speed, "--overwrite"}, &out, &stderr) == 0 {
			t.Fatalf("accepted invalid or empty speed %s", speed)
		}
		assertPreviewEVFileBytes(t, output, []byte("keep old output"))
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("invalid speed changed source")
	}
	// Even a dropped source frame must still be decoded/validated, as in the
	// native filter pipeline. Failure must not publish the partially written AVI.
	if err := os.WriteFile(filepath.Join(dir, "frame02.png"), []byte("broken image"), 0600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "keep.avi")
	if err := os.WriteFile(output, []byte("existing output"), 0600); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	if Main([]string{"render", dir, "--output", output, "--speed", "2", "--overwrite"}, &out, &stderr) == 0 {
		t.Fatal("ignored malformed dropped input")
	}
	assertPreviewEVFileBytes(t, output, []byte("existing output"))
	if temporary, _ := filepath.Glob(filepath.Join(filepath.Dir(output), ".lrtl-*")); len(temporary) != 0 {
		t.Fatalf("temporary output survived failure: %v", temporary)
	}
}

func TestPlaybackSpeedSubtitleCorrespondence(t *testing.T) {
	dir := speedImageFixture(t, 10)
	output := filepath.Join(t.TempDir(), "fast.srt")
	markerCommand(t, "subtitles", dir, "--output", output, "--fps", "4", "--speed", "2", "--timestamp-format", "05")
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	entries := strings.Split(strings.TrimSpace(string(data)), "\n\n")
	if len(entries) != 5 {
		t.Fatalf("subtitle count: %s", data)
	}
	for i, entry := range entries {
		lines := strings.Split(entry, "\n")
		if len(lines) != 3 || lines[2] != fmt.Sprintf("%02d", 2*i+1) {
			t.Fatalf("wrong source time: %s", entry)
		}
	}
	if !strings.Contains(string(data), "00:00:01,000 --> 00:00:01,250") {
		t.Fatal("subtitle duration differs from video")
	}
	markerCommand(t, "subtitles", dir, "--output", output, "--fps", "4", "--speed", "0.5", "--timestamp-format", "05", "--overwrite")
	data, err = os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	entries = strings.Split(strings.TrimSpace(string(data)), "\n\n")
	if len(entries) != 20 {
		t.Fatal("slow subtitles did not duplicate source cues")
	}
	for i, entry := range entries {
		if !strings.HasSuffix(entry, fmt.Sprintf("\n%02d", i/2)) {
			t.Fatal("slow subtitle source correspondence")
		}
	}
}

func TestPlaybackSpeedActualFFmpegOutput(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("FFmpeg unavailable")
	}
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe unavailable")
	}
	dir := speedImageFixture(t, 10)
	output := filepath.Join(t.TempDir(), "actual.mp4")
	markerCommand(t, "render", dir, "--output", output, "--fps", "29.97", "--speed", "2", "--ffmpeg", ffmpeg, "--preset", "ultrafast", "--crf", "0")
	raw, err := exec.Command(ffprobe, "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=avg_frame_rate,nb_frames,duration", "-of", "json", output).CombinedOutput()
	if err != nil {
		t.Fatalf("ffprobe: %s %v", raw, err)
	}
	var probe struct {
		Streams []struct {
			Rate     string `json:"avg_frame_rate"`
			Frames   string `json:"nb_frames"`
			Duration string `json:"duration"`
		}
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		t.Fatal(err)
	}
	if len(probe.Streams) != 1 || probe.Streams[0].Rate != "30000/1001" || probe.Streams[0].Frames != "5" {
		t.Fatalf("resampled video metadata: %s", raw)
	}
	duration, err := strconv.ParseFloat(probe.Streams[0].Duration, 64)
	if err != nil || math.Abs(duration-5*1001./30000) > 1e-6 {
		t.Fatalf("duration: %s %v", raw, err)
	}
}
