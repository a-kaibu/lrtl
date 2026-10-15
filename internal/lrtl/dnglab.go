package lrtl

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

const maxDevelopedRAWBytes = 1 << 30

type boundedBuffer struct {
	bytes.Buffer
	limit    int
	exceeded bool
}

func (buffer *boundedBuffer) Write(data []byte) (int, error) {
	remaining := buffer.limit - buffer.Len()
	if len(data) > remaining {
		if remaining > 0 {
			_, _ = buffer.Buffer.Write(data[:remaining])
		}
		buffer.exceeded = true
		return max(remaining, 0), fmt.Errorf("output exceeds %d-byte limit", buffer.limit)
	}
	return buffer.Buffer.Write(data)
}

func resolveDNGLab(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		path = "dnglab"
	}
	resolved, err := exec.LookPath(path)
	if err != nil {
		return "", fmt.Errorf("could not find DNGLab executable %q; install DNGLab or pass --dnglab PATH: %w", path, err)
	}
	return resolved, nil
}

func developRawWithDNGLab(path, executable string) ([]byte, error) {
	resolved, err := resolveDNGLab(executable)
	if err != nil {
		return nil, err
	}
	command := exec.Command(resolved, "analyze", "--loglevel", "error", "--srgb", path)
	output := &boundedBuffer{limit: maxDevelopedRAWBytes}
	var stderr cappedBuffer
	stderr.limit = ffmpegStderrLimit
	command.Stdout = output
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		if output.exceeded {
			return nil, fmt.Errorf("DNGLab output for %s exceeds the 1 GiB in-memory limit; use a lower-resolution source", filepath.Base(path))
		}
		detail := strings.TrimSpace(stderr.String())
		if detail != "" {
			return nil, fmt.Errorf("DNGLab could not develop %s: %w: %s", filepath.Base(path), err, detail)
		}
		return nil, fmt.Errorf("DNGLab could not develop %s: %w", filepath.Base(path), err)
	}
	if output.Len() == 0 {
		return nil, fmt.Errorf("DNGLab produced no developed TIFF for %s", filepath.Base(path))
	}
	return output.Bytes(), nil
}
