package lrtl

import (
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
)

const ffmpegStderrLimit = 16 * 1024

type cappedBuffer struct {
	bytes.Buffer
	limit int
}

func (buffer *cappedBuffer) Write(data []byte) (int, error) {
	length := len(data)
	remaining := buffer.limit - buffer.Len()
	if remaining > 0 {
		if len(data) > remaining {
			data = data[:remaining]
		}
		_, _ = buffer.Buffer.Write(data)
	}
	return length, nil
}

type ffmpegFrameWriter struct {
	command *exec.Cmd
	input   io.WriteCloser
	stderr  cappedBuffer
	closed  bool
}

func resolveFFmpeg(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		path = "ffmpeg"
	}
	resolved, err := exec.LookPath(path)
	if err != nil {
		return "", fmt.Errorf("could not find FFmpeg executable %q; install FFmpeg or pass --ffmpeg PATH: %w", path, err)
	}
	return resolved, nil
}

func validH264Preset(preset string) bool {
	switch preset {
	case "ultrafast", "superfast", "veryfast", "faster", "fast", "medium", "slow", "slower", "veryslow", "placebo":
		return true
	default:
		return false
	}
}

func dnxhrProfile(codec string) (string, string, bool) {
	switch codec {
	case "dnxhr-lb":
		return "dnxhr_lb", "yuv422p", true
	case "dnxhr-sq":
		return "dnxhr_sq", "yuv422p", true
	case "dnxhr-hq":
		return "dnxhr_hq", "yuv422p", true
	case "dnxhr-hqx":
		return "dnxhr_hqx", "yuv422p10le", true
	case "dnxhr-444":
		return "dnxhr_444", "yuv444p10le", true
	default:
		return "", "", false
	}
}

func proresProfile(codec string) (string, string, bool) {
	switch codec {
	case "prores-proxy":
		return "0", "yuv422p10le", true
	case "prores-lt":
		return "1", "yuv422p10le", true
	case "prores":
		return "2", "yuv422p10le", true
	case "prores-hq":
		return "3", "yuv422p10le", true
	case "prores-4444":
		return "4", "yuv444p10le", true
	}
	return "", "", false
}

func newFFmpegFrameWriter(executable, output, container, codec string, width, height int, rate frameRate, frameCount, crf int, preset, audioPath string) (*ffmpegFrameWriter, error) {
	arguments := []string{
		"-hide_banner", "-loglevel", "error", "-nostats", "-n",
		"-f", "rawvideo", "-pixel_format", "rgb48le",
		"-video_size", fmt.Sprintf("%dx%d", width, height),
		"-framerate", rate.String(), "-i", "pipe:0",
	}
	if audioPath != "" {
		arguments = append(arguments, "-i", audioPath, "-map", "0:v:0", "-map", "1:a:0")
	}
	arguments = append(arguments, "-frames:v", strconv.Itoa(frameCount))
	if audioPath == "" {
		arguments = append(arguments, "-an")
	}
	switch codec {
	case "h264":
		arguments = append(arguments,
			"-c:v", "libx264", "-preset", preset, "-crf", strconv.Itoa(crf),
			"-pix_fmt", "yuv420p", "-vf", "pad=ceil(iw/2)*2:ceil(ih/2)*2", "-tag:v", "avc1")
	case "h265":
		arguments = append(arguments,
			"-c:v", "libx265", "-preset", preset, "-crf", strconv.Itoa(crf),
			"-pix_fmt", "yuv420p", "-vf", "pad=ceil(iw/2)*2:ceil(ih/2)*2", "-tag:v", "hvc1")
	default:
		if profile, pixelFormat, supported := proresProfile(codec); supported {
			arguments = append(arguments, "-c:v", "prores_ks", "-profile:v", profile, "-pix_fmt", pixelFormat, "-vf", "pad=ceil(iw/2)*2:ceil(ih/2)*2")
		} else if profile, pixelFormat, supported := dnxhrProfile(codec); supported {
			arguments = append(arguments,
				"-c:v", "dnxhd", "-profile:v", profile,
				"-pix_fmt", pixelFormat, "-vf", "pad=ceil(iw/2)*2:ceil(ih/2)*2")
		} else {
			return nil, fmt.Errorf("unsupported FFmpeg video codec %q", codec)
		}
	}
	if audioPath != "" {
		duration := strconv.FormatFloat(rate.seconds(frameCount), 'f', 9, 64)
		arguments = append(arguments,
			"-c:a", "aac", "-b:a", "192k",
			"-af", "asetpts=PTS-STARTPTS,apad,atrim=duration="+duration,
			"-t", duration, "-shortest",
		)
	}
	arguments = append(arguments, "-movflags", "+faststart", "-f", container, output)
	command := exec.Command(executable, arguments...)
	input, err := command.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("open FFmpeg input: %w", err)
	}
	writer := &ffmpegFrameWriter{command: command, input: input, stderr: cappedBuffer{limit: ffmpegStderrLimit}}
	command.Stderr = &writer.stderr
	if err := command.Start(); err != nil {
		_ = input.Close()
		return nil, fmt.Errorf("start FFmpeg: %w", err)
	}
	return writer, nil
}

func (writer *ffmpegFrameWriter) AddFrame(frame []byte) error {
	if writer.closed {
		return fmt.Errorf("FFmpeg input is already closed")
	}
	for len(frame) > 0 {
		written, err := writer.input.Write(frame)
		if err != nil {
			return fmt.Errorf("write frame to FFmpeg: %w", err)
		}
		if written == 0 {
			return io.ErrShortWrite
		}
		frame = frame[written:]
	}
	return nil
}

func (writer *ffmpegFrameWriter) Close() error {
	if writer.closed {
		return nil
	}
	writer.closed = true
	closeErr := writer.input.Close()
	waitErr := writer.command.Wait()
	if waitErr != nil {
		detail := strings.TrimSpace(writer.stderr.String())
		if detail != "" {
			return fmt.Errorf("FFmpeg encoding failed: %w: %s", waitErr, detail)
		}
		return fmt.Errorf("FFmpeg encoding failed: %w", waitErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close FFmpeg input: %w", closeErr)
	}
	return nil
}

func (writer *ffmpegFrameWriter) Abort() {
	if writer.closed {
		return
	}
	writer.closed = true
	_ = writer.input.Close()
	if writer.command.Process != nil {
		_ = writer.command.Process.Kill()
	}
	_ = writer.command.Wait()
}
