package lrtl

import (
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type VideoImportOptions struct {
	FFmpegPath  string
	Format      string
	FPS         float64
	JPEGQuality int
}

// ImportVideo extracts a video to an ordered image sequence in a new directory.
func ImportVideo(source, destination string, options VideoImportOptions, stdout io.Writer) error {
	format := strings.ToLower(strings.TrimSpace(options.Format))
	if format == "" {
		format = "jpg"
	}
	if format != "jpg" && format != "png" {
		return fmt.Errorf("video import format must be jpg or png: %q", options.Format)
	}
	if math.IsNaN(options.FPS) || math.IsInf(options.FPS, 0) || options.FPS < 0 || options.FPS > 1000 {
		return fmt.Errorf("video import fps must be 0 (all frames) or between 0 and 1000: %v", options.FPS)
	}
	if options.JPEGQuality < 1 || options.JPEGQuality > 31 {
		return fmt.Errorf("JPEG quality must be between 1 and 31: %d", options.JPEGQuality)
	}

	sourcePath, err := filepath.Abs(source)
	if err != nil {
		return fmt.Errorf("resolve video path: %w", err)
	}
	sourceInfo, err := os.Stat(sourcePath)
	if err != nil {
		return fmt.Errorf("open video: %w", err)
	}
	if !sourceInfo.Mode().IsRegular() {
		return fmt.Errorf("video source must be a regular file: %s", sourcePath)
	}

	destinationPath, err := filepath.Abs(destination)
	if err != nil {
		return fmt.Errorf("resolve output path: %w", err)
	}
	if _, err := os.Lstat(destinationPath); err == nil {
		return fmt.Errorf("output already exists: %s", destinationPath)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("check output path: %w", err)
	}
	parent := filepath.Dir(destinationPath)
	if err := os.MkdirAll(parent, 0755); err != nil {
		return fmt.Errorf("create output parent directory: %w", err)
	}

	executable, err := resolveFFmpeg(options.FFmpegPath)
	if err != nil {
		return err
	}
	temporaryDirectory, err := os.MkdirTemp(parent, ".lrtl-import-video-*")
	if err != nil {
		return fmt.Errorf("create temporary output directory: %w", err)
	}
	keepTemporary := false
	defer func() {
		if !keepTemporary {
			_ = os.RemoveAll(temporaryDirectory)
		}
	}()

	extension := "." + format
	pattern := filepath.ToSlash(filepath.Join(temporaryDirectory, "frame_%06d"+extension))
	arguments := []string{
		"-hide_banner", "-loglevel", "error", "-nostats", "-nostdin", "-n",
		"-i", sourcePath, "-map", "0:v:0", "-an",
	}
	if options.FPS > 0 {
		arguments = append(arguments, "-vf", "fps="+strconv.FormatFloat(options.FPS, 'f', -1, 64))
	}
	arguments = append(arguments, "-fps_mode", "passthrough")
	if format == "jpg" {
		arguments = append(arguments, "-q:v", strconv.Itoa(options.JPEGQuality))
	}
	arguments = append(arguments, "-start_number", "1", "-f", "image2", pattern)
	command := exec.Command(executable, arguments...)
	command.Stdout = io.Discard
	var stderr cappedBuffer
	stderr.limit = ffmpegStderrLimit
	command.Stderr = &stderr
	fmt.Fprintf(stdout, "Extracting %s to %s\n", filepath.Base(sourcePath), destinationPath)
	if err := command.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail != "" {
			return fmt.Errorf("FFmpeg video import failed: %w: %s", err, detail)
		}
		return fmt.Errorf("FFmpeg video import failed: %w", err)
	}

	entries, err := os.ReadDir(temporaryDirectory)
	if err != nil {
		return fmt.Errorf("read extracted frames: %w", err)
	}
	frameCount := 0
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, "frame_") || strings.ToLower(filepath.Ext(name)) != extension {
			continue
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("FFmpeg created a non-regular frame output: %s", name)
		}
		frameCount++
	}
	if frameCount == 0 {
		return fmt.Errorf("video contains no decodable video frames")
	}
	if err := os.Rename(temporaryDirectory, destinationPath); err != nil {
		return fmt.Errorf("publish extracted sequence: %w", err)
	}
	keepTemporary = true
	fmt.Fprintf(stdout, "Imported %d frame(s) to %s.\n", frameCount, destinationPath)
	return nil
}
