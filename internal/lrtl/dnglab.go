package lrtl

import (
	"bytes"
	"fmt"
	"io"
	"os"
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

func convertRawToDNG(executable, source, destination, compression string) (returnErr error) {
	parent := filepath.Dir(destination)
	temporary, err := os.CreateTemp(parent, ".lrtl-dng-*.dng")
	if err != nil {
		return fmt.Errorf("create temporary DNG path: %w", err)
	}
	temporaryPath := temporary.Name()
	if err := temporary.Close(); err != nil {
		_ = os.Remove(temporaryPath)
		return fmt.Errorf("prepare temporary DNG path: %w", err)
	}
	if err := os.Remove(temporaryPath); err != nil {
		return fmt.Errorf("prepare temporary DNG path: %w", err)
	}
	defer func() {
		if returnErr != nil {
			_ = os.Remove(temporaryPath)
		}
	}()

	command := exec.Command(executable,
		"convert", "--loglevel", "error", "--compression", compression, "--keep-mtime", "true",
		source, temporaryPath,
	)
	command.Stdout = io.Discard
	var stderr cappedBuffer
	stderr.limit = ffmpegStderrLimit
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail != "" {
			return fmt.Errorf("DNGLab failed: %w: %s", err, detail)
		}
		return fmt.Errorf("DNGLab failed: %w", err)
	}
	info, err := os.Stat(temporaryPath)
	if err != nil {
		return fmt.Errorf("DNGLab produced no DNG output: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return fmt.Errorf("DNGLab produced an empty or invalid DNG output")
	}
	if err := os.Rename(temporaryPath, destination); err != nil {
		return fmt.Errorf("publish DNG output: %w", err)
	}
	return nil
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
