package lrtl

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"os"
	"os/exec"
	"testing"

	"github.com/a-kaibu/lrtl/internal/nativejpeg"
)

func FuzzNativeScaleRegion(f *testing.F) {
	f.Add([]byte{0, 127, 255}, uint8(17), uint8(13), uint8(6), uint8(5), uint8(0), uint8(6))
	f.Add([]byte{255}, uint8(2), uint8(2), uint8(48), uint8(32), uint8(1), uint8(2))
	f.Add([]byte{17, 201}, uint8(9), uint8(7), uint8(4), uint8(13), uint8(2), uint8(7))
	f.Add([]byte{255, 0, 63}, uint8(6), uint8(5), uint8(18), uint8(16), uint8(3), uint8(4))
	f.Fuzz(func(t *testing.T, data []byte, wb, hb, twb, thb, kind, orientationByte uint8) {
		if len(data) > 4096 {
			return
		}
		w, h := int(wb%32)+2, int(hb%32)+2
		tw, th := int(twb%48)+2, int(thb%48)+2
		orientation := uint16(orientationByte%8) + 1
		var img image.Image
		format := "jpeg"
		if kind%4 == 0 {
			ratios := []image.YCbCrSubsampleRatio{image.YCbCrSubsampleRatio444, image.YCbCrSubsampleRatio422, image.YCbCrSubsampleRatio420, image.YCbCrSubsampleRatio440}
			source := image.NewYCbCr(image.Rect(0, 0, w, h), ratios[int(kind/4)%4])
			for _, plane := range [][]byte{source.Y, source.Cb, source.Cr} {
				for i := range plane {
					if len(data) > 0 {
						plane[i] = data[i%len(data)]
					}
				}
			}
			img = source
		} else if kind%4 == 3 {
			source := image.NewGray(image.Rect(0, 0, w, h))
			for i := range source.Pix {
				if len(data) > 0 {
					source.Pix[i] = data[i%len(data)]
				}
			}
			img = source
		} else {
			source := image.NewNRGBA(image.Rect(0, 0, w, h))
			for y := 0; y < h; y++ {
				for x := 0; x < w; x++ {
					v := byte(x*13 + y*31)
					if len(data) > 0 {
						v = data[(x+y*w)%len(data)]
					}
					source.SetNRGBA(x, y, color.NRGBA{v, v ^ 91, v ^ 173, 255})
				}
			}
			img, format = source, "png"
			if kind%4 == 2 {
				wide := image.NewNRGBA64(source.Bounds())
				for y := 0; y < h; y++ {
					for x := 0; x < w; x++ {
						c := source.NRGBAAt(x, y)
						wide.SetNRGBA64(x, y, color.NRGBA64{uint16(c.R)*251 + uint16(x), uint16(c.G)*253 + uint16(y), uint16(c.B)*249 + uint16(x+y), 65535})
					}
				}
				img = wide
			}
		}
		before, err := imageRGB48(img)
		if err != nil {
			t.Fatal(err)
		}
		full, err := nativeScaleOrientedWorkingFrame(img, format, orientation, tw, th, image.Rect(0, 0, tw, th))
		if err != nil {
			t.Fatal(err)
		}
		region := image.Rect(tw/3, th/4, tw-tw/4, th-th/3)
		got, err := nativeScaleOrientedWorkingFrame(img, format, orientation, tw, th, region)
		if err != nil {
			t.Fatal(err)
		}
		want := cropOutputRGB48(full, tw, region)
		if !bytes.Equal(got, want) {
			t.Fatal("region changed scale phase or orientation")
		}
		for i := 0; i < len(got); i += 2 {
			if binary.LittleEndian.Uint16(got[i:]) > 1023 {
				t.Fatal("working sample overflow")
			}
		}
		after, err := imageRGB48(img)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("scaling changed source", err)
		}
	})
}

func TestNativeScaleRejectsInvalidDimensions(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for _, tc := range []struct {
		width, height int
		region        image.Rectangle
		orientation   uint16
	}{
		{0, 4, image.Rect(0, 0, 1, 1), 1}, {4, -1, image.Rect(0, 0, 1, 1), 1},
		{4, 4, image.Rectangle{}, 1}, {4, 4, image.Rect(-1, 0, 2, 2), 1},
		{4, 4, image.Rect(0, 0, 5, 4), 1}, {4, 4, image.Rect(0, 0, 4, 4), 0},
		{4, 4, image.Rect(0, 0, 4, 4), 9},
	} {
		if _, err := nativeScaleOrientedWorkingFrame(img, "png", tc.orientation, tc.width, tc.height, tc.region); err == nil {
			t.Fatalf("accepted invalid scale: %+v", tc)
		}
	}
	if _, err := nativeScaleImageWorkingFrame(img, "png", 8193, 8193, image.Rect(0, 0, 8193, 8193)); err == nil {
		t.Fatal("accepted oversized working frame")
	}
	if _, err := nativeScaleRGBWorkingFrame([]byte{1}, 4, 4, 6, 6, image.Rect(0, 0, 6, 6), false, 1, 1); err == nil {
		t.Fatal("accepted malformed RGB frame")
	}
	badJPEG := image.NewYCbCr(image.Rect(0, 0, 4, 4), image.YCbCrSubsampleRatio444)
	badJPEG.SubsampleRatio = image.YCbCrSubsampleRatio(255)
	if _, err := nativeScaleJPEGWorkingFrame(badJPEG, 6, 6, image.Rect(0, 0, 6, 6), 1, 1); err == nil {
		t.Fatal("accepted unsupported JPEG sampling")
	}
}

func TestNativeScaleImageDispatch(t *testing.T) {
	ha, va := nativeScaleAlignment()
	for _, opaque := range []bool{false, true} {
		for _, rgba := range []bool{false, true} {
			a := uint8(127)
			if opaque {
				a = 255
			}
			source := image.NewNRGBA(image.Rect(0, 0, 8, 6))
			for y := 0; y < 6; y++ {
				for x := 0; x < 8; x++ {
					source.SetNRGBA(x, y, color.NRGBA{byte(x * 31), byte(y * 43), byte(x*13 + y*17), a})
				}
			}
			var img image.Image = source
			if rgba {
				out := image.NewRGBA(source.Bounds())
				for y := 0; y < 6; y++ {
					for x := 0; x < 8; x++ {
						out.Set(x, y, source.At(x, y))
					}
				}
				img = out
			}
			for _, format := range []string{"png", "tiff", "other"} {
				t.Run(fmt.Sprintf("%s/opaque%v/rgba%v", format, opaque, rgba), func(t *testing.T) {
					before, err := imageRGB48(img)
					if err != nil {
						t.Fatal(err)
					}
					for _, target := range [][2]int{{8, 6}, {10, 8}} {
						region := image.Rect(1, 1, target[0]-1, target[1]-1)
						got, err := nativeScaleImageWorkingFrame(img, format, target[0], target[1], region)
						if err != nil {
							t.Fatal(err)
						}
						var want []byte
						if target == [2]int{8, 6} {
							full, err := nativeImageWorkingFrame(img, format)
							if err != nil {
								t.Fatal(err)
							}
							want = cropOutputRGB48(full, 8, region)
						} else {
							want, err = nativeScaleRGBWorkingFrame(before, 8, 6, target[0], target[1], region, opaque && (format == "png" || format == "tiff"), ha, va)
							if err != nil {
								t.Fatal(err)
							}
						}
						if !bytes.Equal(got, want) {
							t.Fatal("image format selected the wrong precision")
						}
					}
					after, err := imageRGB48(img)
					if err != nil || !bytes.Equal(before, after) {
						t.Fatal("dispatch changed source", err)
					}
				})
			}
		}
	}
}

func TestNativeScaleJPEGPlanes(t *testing.T) {
	exe := os.Getenv("LRTL_COLOR_FFMPEG")
	if exe == "" {
		var err error
		exe, err = exec.LookPath("ffmpeg")
		if err != nil {
			t.Skip("FFmpeg required")
		}
	}
	for _, name := range []string{"rgb-64x32-q75-s0-p0.jpg", "rgb-64x32-q75-s1-p0.jpg", "rgb-64x32-q75-s2-p1.jpg", "gray-64x32-p0.jpg"} {
		path := "../nativejpeg/testdata/" + name
		img, err := nativejpeg.Decode(bytes.NewReader(denoiseRead(t, path)))
		if err != nil {
			t.Fatal(err)
		}
		for _, shape := range [][2]int{{32, 16}, {40, 22}, {80, 48}, {80, 32}, {64, 48}, {32, 48}, {16, 8}} {
			for _, profile := range []string{"0", "native"} {
				t.Run(fmt.Sprintf("%s/%dx%d/%s", name, shape[0], shape[1], profile), func(t *testing.T) {
					ha, va := 1, 1
					if profile == "native" {
						ha, va = 4, 2
					}
					region := image.Rect(0, 0, shape[0], shape[1])
					var got []byte
					var err error
					if source, ok := img.(*image.YCbCr); ok {
						got, err = nativeScaleJPEGWorkingFrame(source, shape[0], shape[1], region, ha, va)
					} else {
						source := img.(*image.Gray)
						got, err = nativeScaleBytePlanes([3]nativeScaleBytePlane{{source.Pix, source.Stride, 64, 32, 0}, {}, {}}, shape[0], shape[1], region, ha, va)
					}
					if err != nil {
						t.Fatal(err)
					}
					planar := make([]byte, len(got))
					count := len(got) / 6
					for p := 0; p < 3; p++ {
						for i := 0; i < count; i++ {
							copy(planar[(p*count+i)*2:], got[i*6+p*2:i*6+p*2+2])
						}
					}
					args := []string{"-v", "error"}
					if profile == "0" {
						args = append(args, "-cpuflags", "0")
					}
					args = append(args, "-i", path, "-vf", fmt.Sprintf("scale=w=%d:h=%d:out_range=full:flags=lanczos,format=yuv444p10le", shape[0], shape[1]), "-frames:v", "1", "-f", "rawvideo", "pipe:1")
					want := nativeColorCommand(t, exe, nil, args...)
					if !bytes.Equal(planar, want) {
						differences, maxDelta := 0, 0
						for i := 0; i < min(len(planar), len(want)); i += 2 {
							a, b := int(binary.LittleEndian.Uint16(planar[i:])), int(binary.LittleEndian.Uint16(want[i:]))
							d := absInt(a - b)
							if d > 0 {
								if differences == 0 {
									t.Log("first", i/2/count, i/2%count, a, b)
								}
								differences++
							}
							maxDelta = max(maxDelta, d)
						}
						t.Fatalf("scaled JPEG differs: %d samples max%d", differences, maxDelta)
					}
				})
			}
		}
	}
}

func TestNativeScaleRGBPlanes(t *testing.T) {
	exe := os.Getenv("LRTL_COLOR_FFMPEG")
	if exe == "" {
		var err error
		exe, err = exec.LookPath("ffmpeg")
		if err != nil {
			t.Skip("FFmpeg required")
		}
	}
	for _, rgb8 := range []bool{false, true} {
		rgb := nativeColorSource(64, 32, "lowbits")
		input, pixel := rgb, "rgb48le"
		if rgb8 {
			input = nativeRGB8Input(64, 32, "grid")
			rgb = make([]byte, len(input)*2)
			for i, v := range input {
				binary.LittleEndian.PutUint16(rgb[i*2:], uint16(v)*257)
			}
			pixel = "rgb24"
		}
		for _, shape := range [][2]int{{32, 16}, {40, 22}, {80, 48}, {80, 32}, {64, 48}, {32, 48}, {16, 8}} {
			for _, cpu := range []string{"0", "native"} {
				t.Run(fmt.Sprintf("%s/%dx%d/%s", pixel, shape[0], shape[1], cpu), func(t *testing.T) {
					hAlign, vAlign := 1, 1
					if cpu == "native" {
						hAlign, vAlign = 4, 2
					}
					got, err := nativeScaleRGBWorkingFrame(rgb, 64, 32, shape[0], shape[1], image.Rect(0, 0, shape[0], shape[1]), rgb8, hAlign, vAlign)
					if err != nil {
						t.Fatal(err)
					}
					planar := make([]byte, len(got))
					count := len(got) / 6
					for p := 0; p < 3; p++ {
						for i := 0; i < count; i++ {
							copy(planar[(p*count+i)*2:], got[i*6+p*2:i*6+p*2+2])
						}
					}
					args := []string{"-v", "error"}
					if cpu == "0" {
						args = append(args, "-cpuflags", "0")
					}
					args = append(args, "-f", "rawvideo", "-pixel_format", pixel, "-video_size", "64x32", "-i", "pipe:0", "-vf", fmt.Sprintf("scale=w=%d:h=%d:out_range=full:flags=lanczos,format=yuv444p10le", shape[0], shape[1]), "-frames:v", "1", "-f", "rawvideo", "pipe:1")
					want := nativeColorCommand(t, exe, input, args...)
					if !bytes.Equal(planar, want) {
						different, maxDelta := 0, 0
						for i := 0; i < min(len(planar), len(want)); i += 2 {
							a, b := int(binary.LittleEndian.Uint16(planar[i:])), int(binary.LittleEndian.Uint16(want[i:]))
							d := absInt(a - b)
							if d > 0 {
								if different == 0 {
									t.Log("first", i/2/count, i/2%count, a, b)
								}
								different++
							}
							maxDelta = max(maxDelta, d)
						}
						t.Fatalf("scaled output differs: %d samples max%d bytes%d/%d", different, maxDelta, len(planar), len(want))
					}
				})
			}
		}
	}
}
