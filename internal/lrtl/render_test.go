package lrtl

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/image/tiff"
)

func writeRenderFrame(t *testing.T, path, format string, width, height int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 20), G: uint8(y * 30), B: 80, A: 255})
		}
	}
	var encoded bytes.Buffer
	var err error
	switch format {
	case "jpeg":
		err = jpeg.Encode(&encoded, img, &jpeg.Options{Quality: 85})
	case "png":
		err = png.Encode(&encoded, img)
	case "tiff":
		err = tiff.Encode(&encoded, img, nil)
	default:
		t.Fatalf("unsupported fixture format: %s", format)
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestRenderSequenceWritesMJPEGAVIFromJPEGAndPNG(t *testing.T) {
	directory := t.TempDir()
	writeRenderFrame(t, filepath.Join(directory, "frame1.jpg"), "jpeg", 32, 24)
	writeRenderFrame(t, filepath.Join(directory, "frame2.png"), "png", 32, 24)
	writeRenderFrame(t, filepath.Join(directory, "frame3.tif"), "tiff", 32, 24)
	frames, err := DiscoverSequence(directory)
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(directory, "result.avi")
	if err := RenderSequence(frames, output, RenderOptions{FPS: 24, Quality: 90}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 12 || string(data[:4]) != "RIFF" || string(data[8:12]) != "AVI " {
		t.Fatalf("output does not have an AVI RIFF header: %q", data[:min(len(data), 12)])
	}
	if bytes.Contains(data, []byte("MJPG")) == false {
		t.Fatal("AVI stream does not declare the MJPG codec")
	}
	if matches, _ := filepath.Glob(filepath.Join(directory, ".lrtl-*.avi*")); len(matches) != 0 {
		t.Fatalf("temporary render files remain: %v", matches)
	}
	if err := RenderSequence(frames, output, RenderOptions{FPS: 24, Quality: 90}); err == nil {
		t.Fatal("expected existing output to be protected")
	}
	if err := RenderSequence(frames, output, RenderOptions{FPS: 30, Quality: 90, Overwrite: true}); err != nil {
		t.Fatalf("overwrite existing output: %v", err)
	}
}

func TestRenderSequenceRejectsUnsupportedFramesAndMismatchedSizes(t *testing.T) {
	t.Run("raw frame", func(t *testing.T) {
		directory := t.TempDir()
		jpg := filepath.Join(directory, "frame1.jpg")
		raw := filepath.Join(directory, "frame2.cr3")
		writeRenderFrame(t, jpg, "jpeg", 16, 16)
		if err := os.WriteFile(raw, []byte("raw"), 0600); err != nil {
			t.Fatal(err)
		}
		frames, err := DiscoverSequence(directory)
		if err != nil {
			t.Fatal(err)
		}
		output := filepath.Join(directory, "result.avi")
		if err := RenderSequence(frames, output, RenderOptions{FPS: 24, Quality: 90}); err == nil {
			t.Fatal("expected RAW input to be rejected")
		}
		if _, err := os.Stat(output); !os.IsNotExist(err) {
			t.Fatal("unsupported input created an output")
		}
	})

	t.Run("different dimensions", func(t *testing.T) {
		directory := t.TempDir()
		writeRenderFrame(t, filepath.Join(directory, "frame1.jpg"), "jpeg", 16, 16)
		writeRenderFrame(t, filepath.Join(directory, "frame2.png"), "png", 20, 16)
		frames, err := DiscoverSequence(directory)
		if err != nil {
			t.Fatal(err)
		}
		output := filepath.Join(directory, "result.avi")
		if err := RenderSequence(frames, output, RenderOptions{FPS: 24, Quality: 90}); err == nil {
			t.Fatal("expected inconsistent dimensions to be rejected")
		}
		if _, err := os.Stat(output); !os.IsNotExist(err) {
			t.Fatal("invalid sequence created an output")
		}
	})
}

func TestRenderCLIParsesOptionsAfterDirectory(t *testing.T) {
	directory := t.TempDir()
	writeRenderFrame(t, filepath.Join(directory, "frame1.jpg"), "jpeg", 16, 16)
	var stdout, stderr bytes.Buffer
	output := filepath.Join(directory, "cli.avi")
	code := Main([]string{
		"render", directory, "--output", output, "--fps", "30",
		"--reference", "0,0,16,16", "--smooth", "2", "--passes", "2",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("render exit=%d stderr=%s", code, stderr.String())
	}
	if !bytes.Contains(stdout.Bytes(), []byte("Rendered 1 frames at 30 fps")) {
		t.Fatalf("unexpected render output: %s", stdout.String())
	}
}
