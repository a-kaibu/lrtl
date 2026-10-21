package lrtl

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"image"
	"image/draw"
	"image/jpeg"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

type nativeScaleObservation struct {
	Orientation                     uint16
	Mode, Width, Height, Resolution int
	Aspect                          float64
	Graph                           string
}

func nativeScaleObservations(t *testing.T) []nativeScaleObservation {
	t.Helper()
	var rows []nativeScaleObservation
	if err := json.Unmarshal(denoiseRead(t, "testdata/lrt-scale-caller-7.5.5.json"), &rows); err != nil || len(rows) != 48 {
		t.Fatalf("expected 48 native scaling graphs: %v", err)
	}
	return rows
}

// Synthetic odd-sized sources exercise even-edge removal and EXIF metadata.
func nativeScaleFixture(t *testing.T, count, width, height int, format string, orientation uint16) string {
	t.Helper()
	input := nativeWatermarkFixture(t, count, width, height)
	dir := t.TempDir()
	for f := 0; f < count; f++ {
		source, _, err := image.Decode(bytes.NewReader(denoiseRead(t, filepath.Join(input, fmt.Sprintf("frame%02d.png", f)))))
		if err != nil {
			t.Fatal(err)
		}
		if format == "rgb8" {
			out := image.NewNRGBA(source.Bounds())
			draw.Draw(out, out.Bounds(), source, source.Bounds().Min, draw.Src)
			source = out
		}
		var b bytes.Buffer
		extension := ".png"
		if format == "jpeg" {
			err = jpeg.Encode(&b, source, &jpeg.Options{Quality: 75})
			extension = ".jpg"
		} else {
			err = png.Encode(&b, source)
		}
		if err != nil {
			t.Fatal(err)
		}
		tiff := make([]byte, 26)
		copy(tiff, "II")
		binary.LittleEndian.PutUint16(tiff[2:], 42)
		binary.LittleEndian.PutUint32(tiff[4:], 8)
		binary.LittleEndian.PutUint16(tiff[8:], 1)
		binary.LittleEndian.PutUint16(tiff[10:], 274)
		binary.LittleEndian.PutUint16(tiff[12:], 3)
		binary.LittleEndian.PutUint32(tiff[14:], 1)
		binary.LittleEndian.PutUint16(tiff[18:], orientation)
		data := b.Bytes()
		if extension == ".jpg" {
			payload := append([]byte("Exif\x00\x00"), tiff...)
			chunk := binary.BigEndian.AppendUint16([]byte{0xff, 0xe1}, uint16(len(payload)+2))
			chunk = append(chunk, payload...)
			data = append(append(append([]byte{}, data[:2]...), chunk...), data[2:]...)
		} else {
			chunk := binary.BigEndian.AppendUint32(nil, uint32(len(tiff)))
			chunk = append(chunk, []byte("eXIf")...)
			chunk = append(chunk, tiff...)
			chunk = binary.BigEndian.AppendUint32(chunk, crc32.ChecksumIEEE(chunk[4:]))
			data = append(append(append([]byte{}, data[:33]...), chunk...), data[33:]...)
		}
		if got, err := readEXIFOrientation(data); err != nil || got != orientation {
			t.Fatalf("orientation fixture: %d %v", got, err)
		}
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("frame%02d%s", f, extension)), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestNativeScaleRecordedGeometryPlanes(t *testing.T) {
	exe := os.Getenv("LRTL_COLOR_FFMPEG")
	record := os.Getenv("LRTL_RECORD_SCALE")
	type observation struct {
		Bytes          int
		Native, Scalar string
	}
	references := map[string]observation{}
	if record == "" {
		if err := json.Unmarshal(denoiseRead(t, "testdata/lrt-scale-working-8.0.1.json"), &references); err != nil || len(references) != 144 {
			t.Fatalf("expected 144 independent scale observations: %v", err)
		}
	} else if exe == "" {
		t.Fatal("LRTL_RECORD_SCALE requires LRTL_COLOR_FFMPEG")
	}
	scaleRE := regexp.MustCompile(`scale=w=(\d+):h=(\d+)`)
	cropRE := regexp.MustCompile(`,crop=(\d+):(\d+):(\d+):(\d+)`)
	for _, format := range []string{"rgb8", "rgb16", "jpeg"} {
		for _, row := range nativeScaleObservations(t) {
			name := fmt.Sprintf("%s/%dx%d/o%d/m%d", format, row.Width, row.Height, row.Orientation, row.Mode)
			t.Run(name, func(t *testing.T) {
				dir := nativeScaleFixture(t, 1, row.Width, row.Height, format, row.Orientation)
				sources, _ := DiscoverSequence(dir)
				path := sources[0].Image
				img, decoded, err := decodeRenderImage(denoiseRead(t, path), true)
				if err != nil {
					t.Fatal(err)
				}
				s, c := scaleRE.FindString(row.Graph), cropRE.FindString(row.Graph)
				var sw, sh, cw, ch, x, y int
				if _, err := fmt.Sscanf(s, "scale=w=%d:h=%d", &sw, &sh); err != nil {
					t.Fatal(err)
				}
				if _, err := fmt.Sscanf(c, ",crop=%d:%d:%d:%d", &cw, &ch, &x, &y); err != nil {
					t.Fatal(err)
				}
				tw, th := orientedDimensions(sw, sh, row.Orientation)
				got, err := nativeScaleOrientedWorkingFrame(img, decoded, row.Orientation, tw, th, image.Rect(x, y, x+cw, y+ch))
				if err != nil {
					t.Fatal(err)
				}
				planar := make([]byte, len(got))
				count := len(got) / 6
				for p := 0; p < 3; p++ {
					for i := 0; i < count; i++ {
						copy(planar[(p*count+i)*2:], got[i*6+p*2:][:2])
					}
				}
				graph := strings.Split(row.Graph, ",setsar=")[0]
				if exe == "" {
					reference, ok := references[name]
					hash := reference.Native
					if ha, _ := nativeScaleAlignment(); ha == 1 {
						hash = reference.Scalar
					}
					if !ok || len(planar) != reference.Bytes || fmt.Sprintf("%x", sha256.Sum256(planar)) != hash {
						t.Fatal("working planes differ from saved FFmpeg observation")
					}
					return
				}
				args := []string{"-v", "error", "-noautorotate", "-i", path, "-vf", graph, "-frames:v", "1", "-f", "rawvideo", "pipe:1"}
				want := nativeColorCommand(t, exe, nil, args...)
				if record != "" {
					scalar := nativeColorCommand(t, exe, nil, append([]string{"-cpuflags", "0"}, args...)...)
					references[name] = observation{len(want), fmt.Sprintf("%x", sha256.Sum256(want)), fmt.Sprintf("%x", sha256.Sum256(scalar))}
				}
				if !bytes.Equal(planar, want) {
					for i := 0; i < min(len(planar), len(want)); i += 2 {
						if !bytes.Equal(planar[i:i+2], want[i:i+2]) {
							t.Fatalf("scaled orientation differs plane%d sample%d: %d/%d", i/2/count, i/2%count, binary.LittleEndian.Uint16(planar[i:]), binary.LittleEndian.Uint16(want[i:]))
						}
					}
					t.Fatalf("scaled lengths %d/%d", len(planar), len(want))
				}
			})
		}
	}
	if record != "" && !t.Failed() {
		data, err := json.MarshalIndent(references, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(record, append(data, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestNativeScaleRecordedVideos(t *testing.T) {
	exe := os.Getenv("LRTL_COLOR_FFMPEG")
	if exe == "" {
		t.Skip("set LRTL_COLOR_FFMPEG to compare native scaling videos")
	}
	if _, err := exec.LookPath(exe); err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"rgb8", "rgb16", "jpeg"} {
		for _, orientation := range []uint16{1, 2, 6, 7} {
			dir := nativeScaleFixture(t, 17, 128, 96, format, orientation)
			before := batchFileSnapshot(t, dir)
			for _, row := range nativeScaleObservations(t) {
				if row.Orientation != orientation || row.Width != 128 {
					continue
				}
				t.Run(fmt.Sprintf("%s/o%d/m%d", format, orientation, row.Mode), func(t *testing.T) {
					preset := savePresetJSON(t, map[string]any{"lastRenderSettings": lrtRenderPreset{Encoder: "h264", FrameRate: "24", OutputResV: row.Resolution, Speed: 1, Sharpen: row.Mode > 0, MotionBlur: row.Mode == 2, MotionBlurLevel: 3}, "renderConstrainCrop": row.Aspect > 0, "renderAR": row.Aspect})
					flags := []string{"--lrt-render-preset", preset, "--auto-orient"}
					verifyNativeFinalVideos(t, exe, dir, flags, func(pix, videoRange string) string {
						return strings.ReplaceAll(strings.TrimSuffix(row.Graph, "[v]"), "range=limited:", "range="+videoRange+":")
					})
				})
			}
			for path, data := range before {
				assertPreviewEVFileBytes(t, path, data)
			}
		}
	}
}

// Use separately observed native temporal helpers around the recorded scale
// and transpose prefix, measuring deflicker on the full canvas before crop.
func TestNativeScaleTemporalCrops(t *testing.T) {
	exe := os.Getenv("LRTL_COLOR_FFMPEG")
	if exe == "" {
		t.Skip("set LRTL_COLOR_FFMPEG to compare resized temporal output")
	}
	base := "scale=iw:ih:out_range=full:flags=lanczos,format=yuv444p10le,setparams=color_primaries=bt709:color_trc=bt709:colorspace=bt709,setsar=1/1"
	df := strings.TrimPrefix(nativeTemporalReferenceFilters(t, nativeTemporalCase{luma: true, chroma: true}), base)
	temporal := strings.TrimPrefix(nativeTemporalReferenceFilters(t, nativeTemporalCase{noise: 4, air: 3}), base)
	tc := nativeTemporalCase{noise: 4, air: 3, luma: true, chroma: true, spatial: true}
	for _, format := range []string{"rgb8", "rgb16", "jpeg"} {
		for _, orientation := range []uint16{1, 6} {
			dir := nativeScaleFixture(t, 17, 128, 96, format, orientation)
			var row nativeScaleObservation
			for _, candidate := range nativeScaleObservations(t) {
				if candidate.Width == 128 && candidate.Orientation == orientation && candidate.Mode == 2 {
					row = candidate
					break
				}
			}
			if row.Graph == "" {
				t.Fatal("missing native crop observation")
			}
			preset := savePresetJSON(t, map[string]any{"lastRenderSettings": lrtRenderPreset{Encoder: "h264", FrameRate: "24", OutputResV: 64, Speed: 1}, "renderConstrainCrop": true, "renderAR": 1})
			for _, position := range []int{0, 50, 100} {
				t.Run(fmt.Sprintf("%s/o%d/p%d", format, orientation, position), func(t *testing.T) {
					x, y := 20*position/100, 0
					if orientation >= 5 {
						x, y = 0, x
					}
					graph := strings.Split(row.Graph, ",crop=")[0] + df + fmt.Sprintf(",crop=64:64:%d:%d,setsar=1/1,setpts=N/(24)/TB", x, y) + temporal + ",unsharp=luma_msize_x=7:luma_msize_y=7:luma_amount=0.4,cas=strength=0.90,trim=start_frame=1:end_frame=16,setpts=PTS-STARTPTS"
					flags := append(nativeTemporalCLI(tc), "--lrt-render-preset", preset, "--auto-orient", "--crop-position", strconv.Itoa(position))
					verifyNativeFilteredVideos(t, exe, dir, flags, graph)
				})
			}
		}
	}
}
