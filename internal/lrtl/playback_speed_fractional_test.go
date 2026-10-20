package lrtl

import (
	"bytes"
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

type fractionalSpeedObservation struct {
	FPS, Filter string
	Speed       float64
	Frames      int
	Ends        []int
}

func fractionalSpeedObservations(t *testing.T) []fractionalSpeedObservation {
	t.Helper()
	data, err := os.ReadFile("testdata/lrt-speed-fractional-7.5.5.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []fractionalSpeedObservation
	if err := json.Unmarshal(data, &cases); err != nil || len(cases) != 252 {
		t.Fatalf("expected252 native-filter fractional maps: %v", err)
	}
	return cases
}

func TestFractionalSpeed252NativeFilterMappings(t *testing.T) {
	total := 0
	for _, tc := range fractionalSpeedObservations(t) {
		if len(tc.Ends) != tc.Frames || tc.Frames < 1 {
			t.Fatal("invalid lossless source-interval encoding")
		}
		speed, err := validatePlaybackSpeed(tc.Speed)
		if err != nil {
			t.Fatal(err)
		}
		for source, observedEnd := range tc.Ends {
			if got := playbackOutputEnd(source+1, speed); got != observedEnd {
				t.Fatalf("fps%s speed%g frames%d source%d: end%d want native-filter%d", tc.FPS, tc.Speed, tc.Frames, source, got, observedEnd)
			}
		}
		count, err := playbackFrameCount(tc.Frames, speed)
		observed := tc.Ends[len(tc.Ends)-1]
		if observed == 0 {
			if err == nil {
				t.Fatal("accepted empty filtered output")
			}
		} else if err != nil || count != observed {
			t.Fatalf("count%d want%d: %v", count, observed, err)
		}
		total += observed
	}
	if total != 166242 {
		t.Fatalf("fixture lost actual observed output frames: %d", total)
	}
}

func TestFractionalSpeedActualFFmpegReplay(t *testing.T) {
	executable := os.Getenv("LRTL_SPEED_FFMPEG")
	if executable == "" {
		var err error
		executable, err = exec.LookPath("ffmpeg")
		if err != nil {
			t.Skip("FFmpeg unavailable; stored mappings remain checked")
		}
	}
	for _, tc := range fractionalSpeedObservations(t) {
		if tc.Frames != 11 {
			continue // Replay all36 filter/rate combinations on eleven source IDs.
		}
		raw := make([]byte, tc.Frames*512)
		for source := 0; source < tc.Frames; source++ {
			for pixel := 0; pixel < 256; pixel++ {
				binary.LittleEndian.PutUint16(raw[source*512+pixel*2:], uint16(source))
			}
		}
		rate := strings.TrimSpace(strings.Split(tc.Filter, "fps=")[1])
		cmd := exec.Command(executable, "-v", "error", "-f", "rawvideo", "-pixel_format", "gray16le", "-video_size", "16x16", "-framerate", rate, "-i", "pipe:0", "-vf", strings.TrimPrefix(tc.Filter, ","), "-fps_mode", "passthrough", "-f", "rawvideo", "-pix_fmt", "gray16le", "pipe:1")
		cmd.Stdin = bytes.NewReader(raw)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		data, err := cmd.Output()
		if err != nil || len(data) != tc.Ends[len(tc.Ends)-1]*512 {
			t.Fatalf("fps%s speed%g replay: bytes%d %v %s", tc.FPS, tc.Speed, len(data), err, stderr.String())
		}
		output := 0
		for source, end := range tc.Ends {
			for ; output < end; output++ {
				for pixel := 0; pixel < 256; pixel++ {
					if value := binary.LittleEndian.Uint16(data[output*512+pixel*2:]); int(value) != source {
						t.Fatalf("fps%s speed%g frame%d: source%d want%d", tc.FPS, tc.Speed, output, value, source)
					}
				}
			}
		}
	}
}

func TestFractionalSpeedRenderAndSubtitleSourceIDs(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(t.TempDir(), "capture")
	t.Setenv("LRTL_TEST_FFMPEG_CAPTURE", capture)
	for _, count := range []int{3, 11} {
		dir := speedImageFixture(t, count)
		markerCommand(t, "render", dir, "--output", filepath.Join(t.TempDir(), "control.mp4"), "--ffmpeg", executable)
		control, err := os.ReadFile(capture)
		if err != nil {
			t.Fatal(err)
		}
		for _, tc := range fractionalSpeedObservations(t) {
			if tc.FPS != "24" || tc.Frames != count || (tc.Speed != .1 && tc.Speed != .3 && tc.Speed != .7 && tc.Speed != 1.1 && tc.Speed != 1.3) {
				continue
			}
			speed := strconv.FormatFloat(tc.Speed, 'g', -1, 64)
			markerCommand(t, "render", dir, "--output", filepath.Join(t.TempDir(), "speed.mp4"), "--ffmpeg", executable, "--speed", speed)
			data, err := os.ReadFile(capture)
			if err != nil || len(data) != tc.Ends[len(tc.Ends)-1]*64*32*6 {
				t.Fatalf("render frame count: bytes%d %v", len(data), err)
			}
			srt := filepath.Join(t.TempDir(), "speed.srt")
			markerCommand(t, "subtitles", dir, "--output", srt, "--speed", speed, "--timestamp-format", "05")
			text, err := os.ReadFile(srt)
			if err != nil {
				t.Fatal(err)
			}
			blocks := strings.Split(strings.TrimSpace(string(text)), "\n\n")
			if len(blocks) != tc.Ends[len(tc.Ends)-1] {
				t.Fatal("SRT frame count differs from native map")
			}
			output, size := 0, 64*32*6
			for source, end := range tc.Ends {
				for ; output < end; output++ {
					if !bytes.Equal(data[output*size:(output+1)*size], control[source*size:(source+1)*size]) || !strings.HasSuffix(blocks[output], fmt.Sprintf("\n%02d", source)) {
						t.Fatalf("speed%g output%d lost source%d correspondence", tc.Speed, output, source)
					}
				}
			}
		}
	}
}
