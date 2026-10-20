package lrtl

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func writeSequenceSubtitles(frames []Frame, output string, fps int, timestampFormat string, overwrite bool) error {
	rate, err := parseFrameRate(strconv.Itoa(fps))
	if err != nil {
		return err
	}
	return writeSequenceSubtitlesAtRate(frames, output, rate, timestampFormat, overwrite)
}

func writeSequenceSubtitlesAtRate(frames []Frame, output string, rate frameRate, timestampFormat string, overwrite bool) error {
	return writeSequenceSubtitlesAtSpeed(frames, output, rate, 1, timestampFormat, overwrite)
}

func writeSequenceSubtitlesAtSpeed(frames []Frame, output string, rate frameRate, speed float64, timestampFormat string, overwrite bool) error {
	return writeSequenceSubtitlesWithMotionBlur(frames, output, rate, speed, 0, timestampFormat, overwrite)
}

func writeSequenceSubtitlesWithMotionBlur(frames []Frame, output string, rate frameRate, speed float64, motionBlur int, timestampFormat string, overwrite bool) error {
	if len(frames) == 0 {
		return fmt.Errorf("sequence contains no images")
	}
	if _, err := parseFrameRate(rate.String()); err != nil {
		return err
	}
	speed, err := validatePlaybackSpeed(speed)
	if err != nil {
		return err
	}
	timeline, err := planVideoFrames(len(frames), speed, motionBlur)
	if err != nil {
		return err
	}
	if strings.ToLower(filepath.Ext(output)) != ".srt" {
		return fmt.Errorf("subtitle output must use the .srt extension: %s", output)
	}
	outputDir := filepath.Dir(output)
	directoryInfo, err := os.Stat(outputDir)
	if err != nil {
		return fmt.Errorf("output directory: %w", err)
	}
	if !directoryInfo.IsDir() {
		return fmt.Errorf("output parent is not a directory: %s", outputDir)
	}
	if !overwrite {
		if _, err := os.Lstat(output); err == nil {
			return fmt.Errorf("output already exists (pass --overwrite to replace it): %s", output)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("check output: %w", err)
		}
	}
	timestamps, err := readFrameCaptureTimes(frames)
	if err != nil {
		return fmt.Errorf("subtitle timestamps: %w", err)
	}
	if timestampFormat == "" {
		timestampFormat = defaultTimestampFormat
	}

	temporary, err := os.CreateTemp(outputDir, ".lrtl-subtitles-*.srt")
	if err != nil {
		return fmt.Errorf("create temporary subtitles: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	writer := bufio.NewWriter(temporary)
	index := 0
	for sourceIndex, timestamp := range timestamps {
		label := strings.NewReplacer("\r", " ", "\n", " ").Replace(timestamp.Format(timestampFormat))
		if strings.Contains(label, "-->") {
			_ = temporary.Close()
			return fmt.Errorf("timestamp format produces the reserved SRT delimiter in frame %d", index+1)
		}
		for end := playbackOutputEnd(sourceIndex+1, speed); index < end; index++ {
			if index < timeline.start || index >= timeline.end {
				continue
			}
			outputIndex := index - timeline.start
			startMS, startErr := rate.offsetMilliseconds(int64(outputIndex))
			endMS, endErr := rate.offsetMilliseconds(int64(outputIndex + 1))
			if startErr != nil || endErr != nil {
				_ = temporary.Close()
				return fmt.Errorf("subtitle timing exceeds supported duration")
			}
			start := srtTimecode(startMS)
			end := srtTimecode(endMS)
			if _, err := fmt.Fprintf(writer, "%d\n%s --> %s\n%s\n\n", outputIndex+1, start, end, label); err != nil {
				_ = temporary.Close()
				return fmt.Errorf("write temporary subtitles: %w", err)
			}
		}
	}
	if err := writer.Flush(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("flush temporary subtitles: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync temporary subtitles: %w", err)
	}
	if err := temporary.Chmod(0644); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("set subtitle permissions: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary subtitles: %w", err)
	}
	if !overwrite {
		if _, err := os.Lstat(output); err == nil {
			return fmt.Errorf("output appeared while creating subtitles (pass --overwrite to replace it): %s", output)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("check output: %w", err)
		}
	}
	if err := os.Rename(temporaryPath, output); err != nil {
		return fmt.Errorf("save SRT subtitles to %s: %w", output, err)
	}
	return nil
}

func frameOffsetMilliseconds(frame, fps int64) int64 {
	wholeSeconds := frame / fps
	remainingFrames := frame % fps
	return wholeSeconds*1000 + (remainingFrames*1000+fps/2)/fps
}

func srtTimecode(milliseconds int64) string {
	hours := milliseconds / 3_600_000
	minutes := milliseconds / 60_000 % 60
	seconds := milliseconds / 1_000 % 60
	millisecondsPart := milliseconds % 1_000
	return fmt.Sprintf("%02d:%02d:%02d,%03d", hours, minutes, seconds, millisecondsPart)
}
