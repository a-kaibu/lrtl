package lrtl

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

func normalizeSubsampling(value string) (string, error) {
	switch strings.TrimSpace(value) {
	case "", "420", "4:2:0":
		return "420", nil
	case "422", "4:2:2":
		return "422", nil
	case "444", "4:4:4":
		return "444", nil
	default:
		return "", fmt.Errorf("subsampling must be 420, 422 or 444")
	}
}

func h26xPixelFormat(subsampling string) (string, error) {
	value, err := normalizeSubsampling(subsampling)
	if err != nil {
		return "", err
	}
	if value == "420" {
		return "yuv420p", nil
	}
	return "yuv" + value + "p10le", nil
}

// Encoder help is checked before starting an output: FFmpeg may otherwise
// substitute a supported format, losing the requested chroma or bit depth.
func requireFFmpegPixelFormat(executable, encoder, format string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "-hide_banner", "-h", "encoder="+encoder)
	output := &cappedBuffer{limit: ffmpegStderrLimit}
	command.Stdout, command.Stderr = output, output
	if err := command.Run(); err != nil {
		return fmt.Errorf("inspect FFmpeg %s pixel formats: %w: %s", encoder, err, strings.TrimSpace(output.String()))
	}
	for _, line := range strings.Split(output.String(), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "Supported pixel formats:") {
			continue
		}
		for _, supported := range strings.Fields(strings.TrimPrefix(line, "Supported pixel formats:")) {
			if supported == format {
				return nil
			}
		}
		return fmt.Errorf("FFmpeg encoder %s does not support requested pixel format %s", encoder, format)
	}
	return fmt.Errorf("FFmpeg encoder %s did not report supported pixel formats; cannot confirm %s", encoder, format)
}
