package nativejpeg

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestOfflineDecodedJPEGPlanes(t *testing.T) {
	var rows []struct {
		File, PixelFormat, Hash string
		Bytes                   int
	}
	b, err := os.ReadFile("testdata/ffmpeg-8.0.1.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &rows); err != nil || len(rows) != 32 {
		t.Fatalf("expected 32 installed-decoder observations: %v", err)
	}
	for _, row := range rows {
		t.Run(row.File, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", row.File))
			if err != nil {
				t.Fatal(err)
			}
			img, err := Decode(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			got, pixel := packedJPEGPlanes(t, img)
			if pixel != row.PixelFormat || len(got) != row.Bytes || fmt.Sprintf("%x", sha256.Sum256(got)) != row.Hash {
				t.Fatal("decoded JPEG observation changed", row.File)
			}
		})
	}
}

func packedJPEGPlanes(t *testing.T, img image.Image) ([]byte, string) {
	t.Helper()
	bounds := img.Bounds()
	result := []byte{}
	switch m := img.(type) {
	case *image.Gray:
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				result = append(result, m.GrayAt(x, y).Y)
			}
		}
		return result, "gray"
	case *image.YCbCr:
		h, v, pixel := 1, 1, "yuvj444p"
		switch m.SubsampleRatio {
		case image.YCbCrSubsampleRatio422:
			h, pixel = 2, "yuvj422p"
		case image.YCbCrSubsampleRatio420:
			h, v, pixel = 2, 2, "yuvj420p"
		case image.YCbCrSubsampleRatio444:
		default:
			t.Fatal("unexpected subsampling", m.SubsampleRatio)
		}
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				result = append(result, m.Y[m.YOffset(x, y)])
			}
		}
		for _, plane := range [][]byte{m.Cb, m.Cr} {
			for y := bounds.Min.Y; y < bounds.Max.Y; y += v {
				for x := bounds.Min.X; x < bounds.Max.X; x += h {
					result = append(result, plane[m.COffset(x, y)])
				}
			}
		}
		return result, pixel
	default:
		t.Fatalf("unexpected JPEG image type %T", img)
	}
	return nil, ""
}

func TestDecodedJPEGPlanesAgainstFFmpeg(t *testing.T) {
	exe := os.Getenv("LRTL_COLOR_FFMPEG")
	if exe == "" {
		var err error
		exe, err = exec.LookPath("ffmpeg")
		if err != nil {
			t.Skip("FFmpeg required")
		}
	}
	paths, err := filepath.Glob("testdata/*.jpg")
	if err != nil || len(paths) != 32 {
		t.Fatalf("expected 32 synthetic JPEG fixtures: %v", err)
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			img, err := Decode(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			got, pixel := packedJPEGPlanes(t, img)
			for _, cpu := range []string{"0", "native"} {
				args := []string{"-v", "error"}
				if cpu == "0" {
					args = append(args, "-cpuflags", "0")
				}
				args = append(args, "-i", path, "-frames:v", "1", "-pix_fmt", pixel, "-f", "rawvideo", "pipe:1")
				want, err := exec.Command(exe, args...).Output()
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(got, want) {
					different, maxDelta := 0, 0
					for i := 0; i < min(len(got), len(want)); i++ {
						d := int(got[i]) - int(want[i])
						if d < 0 {
							d = -d
						}
						if d > 0 {
							different++
						}
						maxDelta = max(maxDelta, d)
					}
					t.Fatalf("%s decode differs: bytes%d/%d samples%d max%d", cpu, len(got), len(want), different, maxDelta)
				}
			}
		})
	}
}

func FuzzDecode(f *testing.F) {
	for _, name := range []string{"rgb-64x32-q75-s2-p0.jpg", "rgb-17x13-q75-s0-p1.jpg", "gray-17x13-p0.jpg", "rgb-64x32-q75-s2-p1-restart-blocks.jpg", "rgb-64x32-q75-s2-p1-restart-rows.jpg"} {
		b, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			f.Fatal(err)
		}
		f.Add(b)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<16 {
			return
		}
		config, err := DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return
		}
		if config.Width > 256 || config.Height > 256 {
			return
		}
		_, _ = Decode(bytes.NewReader(data))
	})
}

func TestDecodeRejectsOversizedSOF(t *testing.T) {
	data, err := os.ReadFile("testdata/rgb-64x32-q75-s2-p0.jpg")
	if err != nil {
		t.Fatal(err)
	}
	marker := bytes.Index(data, []byte{0xff, 0xc0})
	if marker < 0 {
		t.Fatal("missing baseline SOF")
	}
	binary.BigEndian.PutUint16(data[marker+5:], 65535)
	binary.BigEndian.PutUint16(data[marker+7:], 65535)
	if _, err := DecodeConfig(bytes.NewReader(data)); err == nil {
		t.Fatal("oversized config accepted")
	}
	if _, err := Decode(bytes.NewReader(data)); err == nil {
		t.Fatal("oversized image accepted")
	}
}
