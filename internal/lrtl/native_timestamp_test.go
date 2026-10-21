package lrtl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestNativeTimestampMeasuredClocks(t *testing.T) {
	var rows []struct {
		Label, Time string
		FPS         float32
		Speed       float64
		Frame       int
	}
	if err := json.Unmarshal(denoiseRead(t, "testdata/lrt-timestamp-clock-7.5.5.json"), &rows); err != nil || len(rows) != 1344 {
		t.Fatalf("expected 1344 native clocks: %v", err)
	}
	for _, row := range rows {
		if got := nativeTimestampFPS(row.Label); got != row.FPS {
			t.Fatalf("native timestamp fps %s=%g want%g", row.Label, got, row.FPS)
		}
		clock, err := newNativeTimestampClock(row.Label, row.Speed)
		if err != nil {
			t.Fatal(err)
		}
		got, err := clock.timecode(row.Frame)
		if err != nil || got != row.Time {
			t.Fatalf("clock %+v got%s error%v", row, got, err)
		}
	}
}

func TestNativeTimestampIntervals(t *testing.T) {
	for _, tc := range []struct {
		count, every int
		speed        float64
		want         []int
	}{
		{8, 3, 1, []int{0, 3, 6, 8}},
		{8, 3, .5, []int{0, 1, 3, 4, 6, 7, 8}},
		{8, 3, 2.3, []int{0, 3, 6, 8}},
		{8, -1, 1, []int{0, 8}},
		{25, -1, 1, []int{0, 24, 25}},
	} {
		got, err := nativeTimestampCueFrames(tc.count, "24", tc.speed, tc.every)
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("interval %+v got%v error%v", tc, got, err)
		}
	}
	for _, every := range []int{0, -2} {
		if _, err := nativeTimestampCueFrames(8, "24", 1, every); err == nil {
			t.Fatal("invalid interval accepted")
		}
	}
	if _, err := nativeTimestampCueFrames(0, "24", 1, 1); err == nil {
		t.Fatal("empty sequence accepted")
	}
	for _, fps := range []string{"0", "NaN", "Inf"} {
		if _, err := newNativeTimestampClock(fps, 1); err == nil {
			t.Fatal("invalid timestamp fps", fps)
		}
	}
	if _, err := newNativeTimestampClock("24", math.NaN()); err == nil {
		t.Fatal("invalid speed accepted")
	}
	clock, _ := newNativeTimestampClock("24", 1)
	if _, err := clock.timecode(-1); err == nil {
		t.Fatal("negative frame accepted")
	}
	if _, err := clock.timecode(math.MaxInt32 + 1); err == nil {
		t.Fatal("oversized frame accepted")
	}
}

func TestNativeTimestampCLI(t *testing.T) {
	dir := videoFadeFixture(t, 8)
	for i := 0; i < 8; i++ {
		writeTestXMP(t, filepath.Join(dir, fmt.Sprintf("frame%02d.jpg", i)), map[string]string{"exif:DateTimeOriginal": fmt.Sprintf("2026-01-02T03:04:%02dZ", i)})
	}
	before := batchFileSnapshot(t, dir)
	output := filepath.Join(t.TempDir(), "native.srt")
	markerCommand(t, "subtitles", dir, "--output", output, "--profile", "lrt-7.5.5", "--every", "3", "--fps", "24")
	want := "1\n00:00:00,000 --> 00:00:00,124\n2026-01-02 03:04:00\n\n2\n00:00:00,124 --> 00:00:00,249\n2026-01-02 03:04:03\n\n3\n00:00:00,249 --> 00:00:00,333\n2026-01-02 03:04:06\n\n"
	if got := string(denoiseRead(t, output)); got != want {
		t.Fatalf("native SRT mismatch\n%s", got)
	}
	// Native SRT is prepared against the original sequence before temporal
	// filtering and trim. The standard profile retains its final-video clock.
	markerCommand(t, "subtitles", dir, "--output", output, "--profile", "lrt-7.5.5", "--every", "3", "--fps", "24", "--motion-blur", "20", "--overwrite")
	if string(denoiseRead(t, output)) != want {
		t.Fatal("native subtitles incorrectly followed video trim")
	}
	for _, args := range [][]string{{"--profile", "unknown"}, {"--profile", "lrt-7.5.5", "--every", "0"}, {"--every", "3"}, {"--profile", "lrt-7.5.5", "--timestamp-format", "-->"}} {
		var stdout, stderr bytes.Buffer
		command := append([]string{"subtitles", dir, "--output", output, "--overwrite"}, args...)
		if Main(command, &stdout, &stderr) == 0 {
			t.Fatal("invalid native options accepted", args)
		}
		if string(denoiseRead(t, output)) != want {
			t.Fatal("failed command changed existing subtitle")
		}
	}
	preset := savePresetJSON(t, lrtRenderPreset{FrameRate: "29.97", Speed: 2, Encoder: "h264", OutputResV: 32})
	markerCommand(t, "subtitles", dir, "--output", output, "--profile", "lrt-7.5.5", "--lrt-render-preset", preset, "--overwrite")
	if data := string(denoiseRead(t, output)); !strings.Contains(data, "00:00:00,133") {
		t.Fatal("native preset subtitle clock", data)
	}
	for path, data := range before {
		if !bytes.Equal(data, denoiseRead(t, path)) {
			t.Fatal("source modified", path)
		}
	}
	files, err := filepath.Glob(filepath.Join(filepath.Dir(output), ".lrtl-subtitles-*"))
	if err != nil || len(files) != 0 {
		t.Fatal("temporary subtitle leaked", files, err)
	}
	if err := os.Remove(output); err != nil {
		t.Fatal(err)
	}
}

func FuzzNativeTimestampClock(f *testing.F) {
	f.Add(uint32(86399), uint16(2997), uint16(16), uint8(3))
	f.Add(uint32(0), uint16(24), uint16(1), uint8(1))
	f.Fuzz(func(t *testing.T, frame uint32, fpsRaw, speedRaw uint16, everyRaw uint8) {
		label := fmt.Sprint(1 + fpsRaw%240)
		speed := .0625 + float64(speedRaw%255)/16
		clock, err := newNativeTimestampClock(label, speed)
		if err != nil {
			t.Fatal(err)
		}
		if value, err := clock.timecode(int(frame & math.MaxInt32)); err != nil || value == "" {
			t.Fatal("valid native clock failed", err)
		}
		indices, err := nativeTimestampCueFrames(1+int(frame%128), label, speed, 1+int(everyRaw))
		if err != nil {
			t.Fatal(err)
		}
		for i := 1; i < len(indices); i++ {
			if indices[i] <= indices[i-1] {
				t.Fatal("unordered cues")
			}
		}
	})
}
