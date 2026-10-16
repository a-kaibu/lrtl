package lrtl

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func pngWithTestOrientation(t *testing.T, order binary.ByteOrder, orientation uint16) ([]byte, *image.NRGBA64) {
	t.Helper()
	img := image.NewNRGBA64(image.Rect(0, 0, 32, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 32; x++ {
			img.SetNRGBA64(x, y, color.NRGBA64{uint16(0x1234 + x), uint16(0x4567 + y), 0x89ab, 65535})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatal(err)
	}
	tiff := make([]byte, 26)
	copy(tiff, "II")
	if order == binary.BigEndian {
		copy(tiff, "MM")
	}
	order.PutUint16(tiff[2:], 42)
	order.PutUint32(tiff[4:], 8)
	order.PutUint16(tiff[8:], 1)
	order.PutUint16(tiff[10:], 274)
	order.PutUint16(tiff[12:], 3)
	order.PutUint32(tiff[14:], 1)
	order.PutUint16(tiff[18:], orientation)
	chunk := binary.BigEndian.AppendUint32(nil, uint32(len(tiff)))
	chunk = append(chunk, []byte("eXIf")...)
	chunk = append(chunk, tiff...)
	chunk = binary.BigEndian.AppendUint32(chunk, crc32.ChecksumIEEE(chunk[4:]))
	data := append([]byte(nil), encoded.Bytes()[:33]...)
	data = append(data, chunk...)
	data = append(data, encoded.Bytes()[33:]...)
	return data, img
}

func TestPNGEXIFOrientationAndTIFFExport(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		t.Run(order.String(), func(t *testing.T) {
			data, source := pngWithTestOrientation(t, order, 6)
			orientation, err := readEXIFOrientation(data)
			if err != nil || orientation != 6 {
				t.Fatalf("PNG orientation=%d err=%v", orientation, err)
			}
			dir := t.TempDir()
			path := filepath.Join(dir, "frame.png")
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			for _, enabled := range []bool{true, false} {
				output := filepath.Join(t.TempDir(), "export")
				args := []string{"export", dir, "--output", output}
				if !enabled {
					args = append(args, "--auto-orient=false")
				}
				var stdout, stderr bytes.Buffer
				if Main(args, &stdout, &stderr) != 0 {
					t.Fatal(&stderr)
				}
				got := readExportedTIFF(t, filepath.Join(output, "frame000001.tif"))
				width, height := 32, 16
				if enabled {
					width, height = 16, 32
				}
				if got.Bounds().Dx() != width || got.Bounds().Dy() != height {
					t.Fatalf("bounds=%v", got.Bounds())
				}
				for y := 0; y < 16; y++ {
					for x := 0; x < 32; x++ {
						dx, dy := x, y
						if enabled {
							dx, dy = 15-y, x
						}
						r, g, b, a := got.At(dx, dy).RGBA()
						er, eg, eb, ea := source.At(x, y).RGBA()
						if r != er || g != eg || b != eb || a != ea {
							t.Fatalf("oriented pixel %d,%d changed", x, y)
						}
					}
				}
			}
			assertPreviewEVFileBytes(t, path, data)
		})
	}
}

func TestPNGOrientationWithoutEXIFAndMalformedChunks(t *testing.T) {
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewNRGBA(image.Rect(0, 0, 16, 16))); err != nil {
		t.Fatal(err)
	}
	if orientation, err := readEXIFOrientation(encoded.Bytes()); err != nil || orientation != 1 {
		t.Fatalf("plain PNG orientation=%d err=%v", orientation, err)
	}
	data, _ := pngWithTestOrientation(t, binary.LittleEndian, 6)
	badCRC := append([]byte(nil), data...)
	badCRC[33+8+26] ^= 1
	badLength := append([]byte(nil), data...)
	binary.BigEndian.PutUint32(badLength[33:], 0xffffffff)
	for _, bad := range [][]byte{data[:12], data[:40], badCRC, badLength} {
		if _, err := readEXIFOrientation(bad); err == nil {
			t.Fatal("malformed PNG metadata accepted")
		}
	}
	invalid, _ := pngWithTestOrientation(t, binary.LittleEndian, 9)
	if _, err := readEXIFOrientation(invalid); err == nil {
		t.Fatal("invalid PNG orientation accepted")
	}
}
