package lrtl

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/image/tiff"
)

func nativeGrayOrientation(t *testing.T, data []byte, format string, orientation uint16) []byte {
	t.Helper()
	if format == "png" {
		payload := make([]byte, 26)
		copy(payload, "II")
		binary.LittleEndian.PutUint16(payload[2:], 42)
		binary.LittleEndian.PutUint32(payload[4:], 8)
		binary.LittleEndian.PutUint16(payload[8:], 1)
		binary.LittleEndian.PutUint16(payload[10:], 274)
		binary.LittleEndian.PutUint16(payload[12:], 3)
		binary.LittleEndian.PutUint32(payload[14:], 1)
		binary.LittleEndian.PutUint16(payload[18:], orientation)
		chunk := binary.BigEndian.AppendUint32(nil, uint32(len(payload)))
		chunk = append(chunk, []byte("eXIf")...)
		chunk = append(chunk, payload...)
		chunk = binary.BigEndian.AppendUint32(chunk, crc32.ChecksumIEEE(chunk[4:]))
		return append(append(append([]byte{}, data[:33]...), chunk...), data[33:]...)
	}
	if string(data[:2]) != "II" {
		t.Fatal("expected little-endian synthetic TIFF")
	}
	offset := int(binary.LittleEndian.Uint32(data[4:]))
	count := int(binary.LittleEndian.Uint16(data[offset:]))
	entries := make([][]byte, 0, count+1)
	for i := 0; i < count; i++ {
		entry := data[offset+2+i*12:][:12]
		if binary.LittleEndian.Uint16(entry) != 274 {
			entries = append(entries, entry)
		}
	}
	entry := make([]byte, 12)
	binary.LittleEndian.PutUint16(entry, 274)
	binary.LittleEndian.PutUint16(entry[2:], 3)
	binary.LittleEndian.PutUint32(entry[4:], 1)
	binary.LittleEndian.PutUint16(entry[8:], orientation)
	entries = append(entries, entry)
	sort.Slice(entries, func(i, j int) bool {
		return binary.LittleEndian.Uint16(entries[i]) < binary.LittleEndian.Uint16(entries[j])
	})
	out := bytes.Clone(data)
	binary.LittleEndian.PutUint32(out[4:], uint32(len(out)))
	out = binary.LittleEndian.AppendUint16(out, uint16(len(entries)))
	for _, entry := range entries {
		out = append(out, entry...)
	}
	return append(out, 0, 0, 0, 0)
}

func TestNativeGrayActualVideos(t *testing.T) {
	exe := os.Getenv("LRTL_COLOR_FFMPEG")
	if exe == "" {
		t.Skip("set LRTL_COLOR_FFMPEG to compare grayscale videos")
	}
	base := "scale=iw:ih:out_range=full:flags=lanczos,format=yuv444p10le,setparams=color_primaries=bt709:color_trc=bt709:colorspace=bt709,setsar=1/1"
	df := strings.TrimPrefix(nativeTemporalReferenceFilters(t, nativeTemporalCase{luma: true, chroma: true}), base)
	temporal := strings.TrimPrefix(nativeTemporalReferenceFilters(t, nativeTemporalCase{noise: 4, air: 3}), base)
	for _, depth := range []int{8, 16} {
		for _, format := range []string{"png", "tiff"} {
			for _, orientation := range []uint16{1, 6} {
				dir := t.TempDir()
				for frame := 0; frame < 17; frame++ {
					data := nativeGrayOrientation(t, encodeNativeGray(t, nativeGraySource(128, 96, depth, frame, false), format), format, orientation)
					if got, err := readEXIFOrientation(data); err != nil || got != orientation {
						t.Fatalf("gray orientation fixture %d %v", got, err)
					}
					if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("frame%02d.%s", frame, format)), data, 0600); err != nil {
						t.Fatal(err)
					}
				}
				before := batchFileSnapshot(t, dir)
				for _, position := range []int{-1, 0, 50, 100} {
					t.Run(fmt.Sprintf("%s/%d/o%d/p%d", format, depth, orientation, position), func(t *testing.T) {
						var graph string
						var flags []string
						if position < 0 {
							preset := savePresetJSON(t, lrtRenderPreset{Encoder: "h264", FrameRate: "24", OutputResV: 0, Speed: 1})
							flags = []string{"--lrt-render-preset", preset, "--auto-orient"}
							graph = "crop=floor(iw/2)*2:floor(ih/2)*2,scale=w=128:h=96:out_range=full:flags=lanczos,format=yuv444p10le,setparams=color_primaries=bt709:color_trc=bt709:colorspace=bt709"
							cw, ch := 128, 96
							if orientation == 6 {
								graph += ",transpose=clock"
								cw, ch = 96, 128
							}
							graph += fmt.Sprintf(",crop=%d:%d:0:0,setsar=sar=1/1,setpts=N/(24)/TB", cw, ch)
						} else {
							preset := savePresetJSON(t, map[string]any{"lastRenderSettings": lrtRenderPreset{Encoder: "h264", FrameRate: "24", OutputResV: 64, Speed: 1}, "renderConstrainCrop": true, "renderAR": 1})
							flags = append(nativeTemporalCLI(nativeTemporalCase{noise: 4, air: 3, luma: true, chroma: true, spatial: true}), "--lrt-render-preset", preset, "--auto-orient", "--crop-position", strconv.Itoa(position))
							for _, row := range nativeScaleObservations(t) {
								if row.Width == 128 && row.Orientation == orientation && row.Mode == 2 {
									graph = strings.Split(row.Graph, ",crop=")[0]
									break
								}
							}
							if graph == "" {
								t.Fatal("missing recorded geometry prefix")
							}
							x, y := 20*position/100, 0
							if orientation == 6 {
								x, y = 0, x
							}
							graph += df + fmt.Sprintf(",crop=64:64:%d:%d,setsar=1/1,setpts=N/(24)/TB", x, y) + temporal + ",unsharp=luma_msize_x=7:luma_msize_y=7:luma_amount=0.4,cas=strength=0.90,trim=start_frame=1:end_frame=16,setpts=PTS-STARTPTS"
						}
						verifyNativeFilteredVideos(t, exe, dir, flags, graph)
					})
				}
				for path, data := range before {
					assertPreviewEVFileBytes(t, path, data)
				}
			}
		}
	}
}

func TestNativeGrayConstantAndBounds(t *testing.T) {
	source := image.NewGray16(image.Rect(3, 5, 35, 69))
	for y := 5; y < 69; y++ {
		for x := 3; x < 35; x++ {
			source.SetGray16(x, y, color.Gray16{12345})
		}
	}
	before := bytes.Clone(source.Pix)
	region := image.Rect(4, 6, 44, 58)
	got, err := nativeScaleImageWorkingFrame(source, "png", 64, 128, region)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < len(got); i += 2 {
		want := uint16(512)
		if i%6 == 0 {
			want = 193
		}
		if v := binary.LittleEndian.Uint16(got[i:]); v != want {
			t.Fatalf("constant gray %d want%d", v, want)
		}
	}
	if exe := os.Getenv("LRTL_COLOR_FFMPEG"); exe != "" {
		data := encodeNativeGray(t, source, "png")
		path := filepath.Join(t.TempDir(), "constant.png")
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		want := nativeColorCommand(t, exe, nil, "-v", "error", "-i", path, "-vf", "scale=64:128:out_range=full:flags=lanczos,format=yuv444p10le,crop=40:52:4:6", "-frames:v", "1", "-f", "rawvideo", "pipe:1")
		if !bytes.Equal(nativeGrayPlanar(got), want) {
			t.Fatal("constant gray differs from installed scaler")
		}
	}
	for _, depth := range []int{8, 16} {
		var empty, huge image.Image
		if depth == 8 {
			empty = image.NewGray(image.Rectangle{})
			huge = &image.Gray{Rect: image.Rect(0, 0, 8193, 8193)}
		} else {
			empty = image.NewGray16(image.Rectangle{})
			huge = &image.Gray16{Rect: image.Rect(0, 0, 8193, 8193)}
		}
		for _, img := range []image.Image{empty, huge} {
			if _, err := nativeImageWorkingFrame(img, "png"); err == nil {
				t.Fatal("accepted invalid grayscale dimensions")
			}
		}
	}
	for _, rect := range []image.Rectangle{{}, image.Rect(-1, 0, 2, 2), image.Rect(0, 0, 9, 8)} {
		if _, err := nativeScaleGray16WorkingFrame(source, 8, 8, rect, 1, 1); err == nil {
			t.Fatal("accepted invalid grayscale region")
		}
	}
	if !bytes.Equal(before, source.Pix) {
		t.Fatal("scaler changed the grayscale source")
	}
}

func FuzzNativeGrayRegion(f *testing.F) {
	f.Add([]byte{0, 255, 63, 31}, uint8(17), uint8(13), uint8(5), uint8(9), uint8(6))
	f.Fuzz(func(t *testing.T, data []byte, wb, hb, twb, thb, orientationByte uint8) {
		if len(data) > 4096 {
			return
		}
		w, h := int(wb%32)+2, int(hb%32)+2
		tw, th := int(twb%48)+2, int(thb%48)+2
		for _, wide := range []bool{false, true} {
			var img image.Image
			if wide {
				g := image.NewGray16(image.Rect(3, 5, w+3, h+5))
				for i := range g.Pix {
					if len(data) > 0 {
						g.Pix[i] = data[i%len(data)]
					}
				}
				img = g
			} else {
				g := image.NewGray(image.Rect(3, 5, w+3, h+5))
				for i := range g.Pix {
					if len(data) > 0 {
						g.Pix[i] = data[i%len(data)]
					}
				}
				img = g
			}
			before, err := imageRGB48(img)
			if err != nil {
				t.Fatal(err)
			}
			orientation := uint16(orientationByte%8) + 1
			full, err := nativeScaleOrientedWorkingFrame(img, "png", orientation, tw, th, image.Rect(0, 0, tw, th))
			if err != nil {
				t.Fatal(err)
			}
			region := image.Rect(tw/3, th/4, tw-tw/4, th-th/3)
			got, err := nativeScaleOrientedWorkingFrame(img, "png", orientation, tw, th, region)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, cropOutputRGB48(full, tw, region)) {
				t.Fatal("grayscale crop changed phase")
			}
			for i := 0; i < len(got); i += 2 {
				v := binary.LittleEndian.Uint16(got[i:])
				if v > 1023 || i%6 != 0 && v != 512 {
					t.Fatal("invalid grayscale working sample")
				}
			}
			after, err := imageRGB48(img)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("grayscale source changed", err)
			}
		}
	})
}

func nativeGraySource(width, height, depth, frame int, exhaustive bool) image.Image {
	var gray *image.Gray
	var wide *image.Gray16
	if depth == 8 {
		gray = image.NewGray(image.Rect(0, 0, width, height))
	} else {
		wide = image.NewGray16(image.Rect(0, 0, width, height))
	}
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			v := (x*937 + y*431 + x*y*31 + frame*701 + frame*x*83) & 65535
			if exhaustive {
				v = x/8 + (y/8)*(width/8)
			}
			if depth == 8 {
				if !exhaustive {
					v >>= 8
				}
				gray.Pix[y*gray.Stride+x] = byte(v)
			} else {
				binary.BigEndian.PutUint16(wide.Pix[y*wide.Stride+x*2:], uint16(v))
			}
		}
	}
	if depth == 8 {
		return gray
	}
	return wide
}

func encodeNativeGray(t *testing.T, source image.Image, format string) []byte {
	t.Helper()
	var b bytes.Buffer
	var err error
	if format == "png" {
		err = png.Encode(&b, source)
	} else {
		err = tiff.Encode(&b, source, nil)
	}
	if err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestNativeGrayFailureRetention(t *testing.T) {
	for _, format := range []string{"png", "tiff"} {
		t.Run(format, func(t *testing.T) {
			dir := t.TempDir()
			for frame := 0; frame < 17; frame++ {
				path := filepath.Join(dir, fmt.Sprintf("frame%02d.%s", frame, format))
				data := encodeNativeGray(t, nativeGraySource(64, 48, 16, frame, false), format)
				if frame == 16 {
					if format == "png" {
						idat := bytes.Index(data, []byte("IDAT"))
						if idat < 0 {
							t.Fatal("missing synthetic IDAT")
						}
						data = data[:idat+12]
					} else {
						offset := int(binary.LittleEndian.Uint32(data[4:]))
						count := int(binary.LittleEndian.Uint16(data[offset:]))
						found := false
						for i := 0; i < count; i++ {
							entry := data[offset+2+i*12:][:12]
							if binary.LittleEndian.Uint16(entry) == 273 {
								if binary.LittleEndian.Uint32(entry[4:]) != 1 {
									t.Fatal("expected one synthetic TIFF strip")
								}
								binary.LittleEndian.PutUint32(entry[8:], uint32(len(data)-1))
								found = true
								break
							}
						}
						if !found {
							t.Fatal("missing TIFF strip offsets")
						}
					}
					if _, _, err := image.DecodeConfig(bytes.NewReader(data)); err != nil {
						t.Fatalf("corruption must retain valid dimensions: %v", err)
					}
				}
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
				writeTestXMP(t, path, map[string]string{"exif:DateTimeOriginal": fmt.Sprintf("2026-01-02T03:04:%02dZ", frame)})
			}
			before := batchFileSnapshot(t, dir)
			fake, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("LRTL_TEST_FFMPEG_CAPTURE", filepath.Join(t.TempDir(), "capture"))
			out := filepath.Join(t.TempDir(), "keep.mp4")
			if err := os.WriteFile(out, []byte("original"), 0600); err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer
			if Main([]string{"render", dir, "--output", out, "--overwrite", "--ffmpeg", fake, "--output-color", "lrt-7.5.5", "--timestamp-style", "lrt-7.5.5", "--denoise", "4"}, &stdout, &stderr) == 0 {
				t.Fatal("accepted truncated grayscale pixels")
			}
			if !strings.Contains(stderr.String(), "decode") || !strings.Contains(stderr.String(), "frame16."+format) {
				t.Fatal("did not reach the intended future grayscale decode failure", stderr.String())
			}
			assertPreviewEVFileBytes(t, out, []byte("original"))
			entries, err := os.ReadDir(filepath.Dir(out))
			if err != nil || len(entries) != 1 {
				t.Fatal("grayscale failure leaked staging files", entries, err)
			}
			for path, data := range before {
				assertPreviewEVFileBytes(t, path, data)
			}
		})
	}
}

func nativeGrayPlanar(working []byte) []byte {
	count := len(working) / 6
	out := make([]byte, len(working))
	for p := 0; p < 3; p++ {
		for i := 0; i < count; i++ {
			copy(out[(p*count+i)*2:], working[i*6+p*2:][:2])
		}
	}
	return out
}

func TestNativeGrayInputPlanes(t *testing.T) {
	exe := os.Getenv("LRTL_COLOR_FFMPEG")
	record := os.Getenv("LRTL_RECORD_GRAY")
	type observation struct {
		Bytes int
		Hash  string
	}
	refs := map[string]observation{}
	if record == "" {
		if err := json.Unmarshal(denoiseRead(t, "testdata/lrt-gray-input-8.0.1.json"), &refs); err != nil || len(refs) != 72 {
			t.Fatalf("expected 72 grayscale references: %v", err)
		}
	} else if exe == "" {
		t.Fatal("recording requires FFmpeg")
	}
	for _, depth := range []int{8, 16} {
		for _, format := range []string{"png", "tiff"} {
			for _, exhaustive := range []bool{true, false} {
				w, h := 64, 48
				if exhaustive {
					w, h = 128, 128
					if depth == 16 {
						w, h = 2048, 2048
					}
				}
				source := nativeGraySource(w, h, depth, 0, exhaustive)
				data := encodeNativeGray(t, source, format)
				path := filepath.Join(t.TempDir(), "input."+format)
				if exe != "" {
					if err := os.WriteFile(path, data, 0600); err != nil {
						t.Fatal(err)
					}
				}
				img, decoded, err := decodeRenderImage(data, true)
				if err != nil || decoded != format {
					t.Fatal(err, decoded)
				}
				if depth == 8 {
					if _, ok := img.(*image.Gray); !ok {
						t.Fatalf("expected Gray, got %T", img)
					}
				} else {
					if _, ok := img.(*image.Gray16); !ok {
						t.Fatalf("expected Gray16, got %T", img)
					}
				}
				targets := [][2]int{{32, 24}, {40, 30}, {80, 60}, {80, 48}, {64, 60}, {32, 60}, {16, 12}, {64, 48}}
				if exhaustive {
					targets = [][2]int{{w, h}}
				}
				for _, shape := range targets {
					for _, profile := range []string{"0", "native"} {
						name := fmt.Sprintf("scale/%s/%d/%dx%d/%s", format, depth, shape[0], shape[1], profile)
						if exhaustive {
							name = fmt.Sprintf("all-values/%s/%d/%s", format, depth, profile)
						}
						t.Run(name, func(t *testing.T) {
							var got []byte
							var err error
							region := image.Rect(0, 0, shape[0], shape[1])
							ha, va := 1, 1
							if profile == "native" {
								ha, va = nativeScaleAlignment()
							}
							if shape == [2]int{w, h} {
								got, err = nativeImageWorkingFrame(img, format)
							} else if depth == 16 {
								got, err = nativeScaleGray16WorkingFrame(img.(*image.Gray16), shape[0], shape[1], region, ha, va)
							} else {
								g := img.(*image.Gray)
								got, err = nativeScaleBytePlanes([3]nativeScaleBytePlane{{g.Pix, g.Stride, w, h, 0}, {}, {}}, shape[0], shape[1], region, ha, va)
							}
							if err != nil {
								t.Fatal(err)
							}
							planar := nativeGrayPlanar(got)
							if exe == "" {
								key := name
								if profile == "native" && ha == 1 {
									key = strings.TrimSuffix(name, "native") + "0"
								}
								reference, ok := refs[key]
								if !ok || reference.Bytes != len(planar) || reference.Hash != fmt.Sprintf("%x", sha256.Sum256(planar)) {
									t.Fatal("grayscale differs from saved FFmpeg observation")
								}
								return
							}
							args := []string{"-v", "error"}
							if profile == "0" {
								args = append(args, "-cpuflags", "0")
							}
							args = append(args, "-i", path, "-vf", fmt.Sprintf("scale=w=%d:h=%d:out_range=full:flags=lanczos,format=yuv444p10le", shape[0], shape[1]), "-frames:v", "1", "-f", "rawvideo", "pipe:1")
							want := nativeColorCommand(t, exe, nil, args...)
							if !bytes.Equal(planar, want) {
								for i := 0; i < min(len(planar), len(want)); i += 2 {
									if !bytes.Equal(planar[i:i+2], want[i:i+2]) {
										t.Fatalf("plane%d pixel%d: %d/%d", i/2/(shape[0]*shape[1]), i/2%(shape[0]*shape[1]), binary.LittleEndian.Uint16(planar[i:]), binary.LittleEndian.Uint16(want[i:]))
									}
								}
								t.Fatalf("lengths %d/%d", len(planar), len(want))
							}
							if record != "" {
								refs[name] = observation{len(want), fmt.Sprintf("%x", sha256.Sum256(want))}
							}
						})
					}
				}
			}
		}
	}
	if record != "" && !t.Failed() {
		data, err := json.MarshalIndent(refs, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(record, append(data, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
}
