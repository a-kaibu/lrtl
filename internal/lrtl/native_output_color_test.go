package lrtl

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/image/tiff"
)

func TestNativeOutputColorPlan(t *testing.T) {
	var rows []struct {
		Source                          string
		Gamut, DynamicRange, VideoRange int
		Filter, Metadata, Error         string
	}
	if err := json.Unmarshal(denoiseRead(t, "testdata/lrt-color-plan-7.5.5.json"), &rows); err != nil || len(rows) != 162 {
		t.Fatalf("expected 162 native helper observations: %v", err)
	}
	count := 0
	for _, row := range rows {
		if row.Source != "BT709" || row.Gamut != 0 || row.DynamicRange != 0 {
			continue
		}
		value := "limited"
		if row.VideoRange == 1 {
			value = "full"
		}
		got := nativeSDRColorFilter(value, "yuv444p10le")
		if row.Error != "" || !strings.Contains(got, "setparams="+row.Metadata+",") || !strings.Contains(got, row.Filter+",format=yuv444p10le") {
			t.Fatalf("native helper mismatch: %+v vs %s", row, got)
		}
		count++
	}
	if count != 3 {
		t.Fatal("missing SDR helper observations")
	}
	for input, want := range map[string]string{"": "standard", " STANDARD ": "standard", " LRT-7.5.5 ": "lrt-7.5.5"} {
		got, err := normalizeOutputColor(input)
		if err != nil || got != want {
			t.Fatalf("%q: %s %v", input, got, err)
		}
	}
	if _, err := normalizeOutputColor("hdr"); err == nil {
		t.Fatal("accepted unsupported profile")
	}
	if nativeSDRColorFilter("auto", "yuv420p") != nativeSDRColorFilter("limited", "yuv420p") {
		t.Fatal("native auto range must be limited")
	}
	preset, err := decodeLRTRenderPreset(renderPresetObservations(t)[0].Preset)
	if err != nil {
		t.Fatal(err)
	}
	for _, explicit := range []bool{false, true} {
		got, err := applyLRTRenderPreset(RenderOptions{OutputColor: "standard"}, preset, "", map[string]bool{"output-color": explicit})
		want := "lrt-7.5.5"
		if explicit {
			want = "standard"
		}
		if err != nil || got.OutputColor != want {
			t.Fatalf("preset override: %+v %v", got, err)
		}
	}
}

func TestNativeOutputColorBackendArguments(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(t.TempDir(), "capture")
	t.Setenv("LRTL_TEST_FFMPEG_CAPTURE", capture)
	raw := nativeColorSource(64, 32, "lowbits")
	for _, profile := range []string{"standard", "lrt-7.5.5"} {
		for _, codec := range []string{"h264", "h265", "prores-hq", "dnxhr-hqx"} {
			for _, value := range []string{"auto", "limited", "full"} {
				t.Run(profile+"/"+codec+"/"+value, func(t *testing.T) {
					writer, err := newFFmpegFrameWriter(executable, filepath.Join(t.TempDir(), "out.mov"), "mov", codec, 64, 32, frameRate{24, 1}, 1, 0, "ultrafast", "", videoFade{}, value, "", "444", ffmpegFrameEncoding{outputColor: profile})
					if err != nil {
						t.Fatal(err)
					}
					if err := writer.AddFrame(raw); err != nil {
						writer.Abort()
						t.Fatal(err)
					}
					if err := writer.Close(); err != nil {
						t.Fatal(err)
					}
					if got := denoiseRead(t, capture); !bytes.Equal(got, raw) {
						t.Fatal("profile altered RGB48 input")
					}
					var args []string
					if err := json.Unmarshal(denoiseRead(t, capture+".json"), &args); err != nil {
						t.Fatal(err)
					}
					get := func(flag string) string {
						for i := 0; i+1 < len(args); i++ {
							if args[i] == flag {
								return args[i+1]
							}
						}
						return ""
					}
					if profile == "standard" {
						if get("-vf") != videoRangeFilter(value, get("-pix_fmt")) || get("-mov_gamma") != "" || get("-sws_flags") != "" {
							t.Fatal("standard profile changed", args)
						}
						return
					}
					resolved := value
					if resolved == "auto" {
						resolved = "limited"
					}
					metadata := "tv"
					if resolved == "full" {
						metadata = "pc"
					}
					if get("-vf") != nativeSDRColorFilter(resolved, get("-pix_fmt")) || get("-color_range") != metadata || get("-color_primaries") != "bt709" || get("-colorspace") != "bt709" || get("-color_trc") != "unspecified" || get("-mov_gamma") != "2.4" || get("-movflags") != "+write_colr+write_gama+prefer_icc+faststart" {
						t.Fatal("native arguments", args)
					}
					if strings.Count(strings.Join(args, " "), "-sws_flags print_info+accurate_rnd+bitexact+full_chroma_int") != 2 {
						t.Fatal("missing input/output swscale flags")
					}
					if codec == "h265" && get("-x265-params") != "colorprim=bt709:range="+resolved+":transfer=bt709:colormatrix=bt709" {
						t.Fatal("missing x265 color metadata")
					}
				})
			}
		}
	}
}

// Deterministic synthetic RGB16 patterns; no native pixels are redistributed.
func nativeColorSource(w, h int, pattern string) []byte {
	palette := [][3]uint16{{0, 0, 0}, {65535, 65535, 65535}, {65535, 0, 0}, {0, 65535, 0}, {0, 0, 65535}, {0, 65535, 65535}, {65535, 0, 65535}, {65535, 65535, 0}}
	raw := make([]byte, 0, w*h*6)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			for p := 0; p < 3; p++ {
				v := int(palette[(x+y)%8][p])
				switch pattern {
				case "bands":
					v = int(palette[min(7, x*8/w)][p])
				case "gray":
					v = x * 65535 / (w - 1)
				case "cube":
					v = (x*(17+p*3) + y*(5+p*13)) % 65 * 1023
				case "lowbits":
					v = (x+y*3+p*19)%1024*64 + (x*7+y*11+p*17)%64
				case "noise":
					v = (x*13 + y*23 + x*y*7 + p*31) % 101 * 650
				}
				raw = binary.LittleEndian.AppendUint16(raw, uint16(v))
			}
		}
	}
	return raw
}

func nativeColorCommand(t *testing.T, executable string, input []byte, args ...string) []byte {
	t.Helper()
	cmd := exec.Command(executable, args...)
	cmd.Stdin = bytes.NewReader(input)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", executable, args, err, stderr.String())
	}
	return out
}

func TestNativeOutputColorGoldenPlanes(t *testing.T) {
	executable := os.Getenv("LRTL_COLOR_FFMPEG")
	if executable == "" {
		t.Skip("set LRTL_COLOR_FFMPEG to the LRT bundled FFmpeg 8.0.1 for plane reference checks")
	}
	var rows []struct {
		Width, Height                                       int
		CPUFlags, Range, PixelFormat, Pattern, Filter, Hash string
		Bytes                                               int
	}
	if err := json.Unmarshal(denoiseRead(t, "testdata/lrt-sdr-output-color-8.0.1.json"), &rows); err != nil || len(rows) != 144 {
		t.Fatalf("expected 144 plane references: %v", err)
	}
	for _, row := range rows {
		t.Run(fmt.Sprintf("%dx%d/%s/%s/%s/%s", row.Width, row.Height, row.CPUFlags, row.Range, row.PixelFormat, row.Pattern), func(t *testing.T) {
			args := []string{"-v", "error", "-sws_flags", "print_info+accurate_rnd+bitexact+full_chroma_int"}
			if row.CPUFlags == "0" {
				args = append(args, "-cpuflags", "0")
			}
			args = append(args, "-f", "rawvideo", "-pixel_format", "rgb48le", "-video_size", fmt.Sprintf("%dx%d", row.Width, row.Height), "-framerate", "1", "-i", "pipe:0", "-vf", nativeSDRColorFilter(row.Range, row.PixelFormat), "-frames:v", "1", "-f", "rawvideo", "-pix_fmt", row.PixelFormat, "pipe:1")
			out := nativeColorCommand(t, executable, nativeColorSource(row.Width, row.Height, row.Pattern), args...)
			hash := sha256.Sum256(out)
			if len(out) != row.Bytes || hex.EncodeToString(hash[:]) != row.Hash {
				t.Fatalf("native plane mismatch: %d bytes hash%x", len(out), hash)
			}
		})
	}
}

func TestNativeOutputColorActualImages(t *testing.T) {
	verifyNativeColorActualImages(t, 0, "444")
}

func TestNativeSpatialActualImages(t *testing.T) {
	for mode := 1; mode <= 3; mode++ {
		for _, sampling := range []string{"420", "422", "444"} {
			t.Run(fmt.Sprintf("mode%d/%s", mode, sampling), func(t *testing.T) { verifyNativeColorActualImages(t, mode, sampling) })
		}
	}
}

func verifyNativeColorActualImages(t *testing.T, mode int, sampling string) {
	t.Helper()
	executable, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("FFmpeg required")
	}
	for _, extension := range []string{"png", "tif"} {
		for _, codec := range []string{"h264", "h265", "prores-hq"} {
			if codec == "prores-hq" && sampling != "444" {
				continue
			}
			for _, value := range []string{"full", "limited"} {
				t.Run(extension+"/"+codec+"/"+value, func(t *testing.T) {
					dir := t.TempDir()
					for i, pattern := range []string{"palette", "lowbits", "cube"} {
						img, err := imageFromRGB48(nativeColorSource(64, 32, pattern), 64, 32)
						if err != nil {
							t.Fatal(err)
						}
						var data bytes.Buffer
						if extension == "png" {
							err = png.Encode(&data, img)
						} else {
							err = tiff.Encode(&data, img, nil)
						}
						if err != nil {
							t.Fatal(err)
						}
						if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("frame%02d.%s", i, extension)), data.Bytes(), 0600); err != nil {
							t.Fatal(err)
						}
					}
					before := batchFileSnapshot(t, dir)
					suffix := ".mov"
					if codec == "h264" {
						suffix = ".mp4"
					}
					output := filepath.Join(t.TempDir(), "go"+suffix)
					cliArgs := []string{"render", dir, "--output", output, "--codec", codec, "--fps", "24", "--crf", "0", "--preset", "ultrafast", "--video-range", value, "--output-color", "lrt-7.5.5", "--ffmpeg", executable}
					if mode&1 != 0 {
						cliArgs = append(cliArgs, "--sharpen")
					}
					if mode&2 != 0 {
						cliArgs = append(cliArgs, "--cas")
					}
					if codec != "prores-hq" {
						cliArgs = append(cliArgs, "--subsampling", sampling)
					}
					markerCommand(t, cliArgs...)
					reference := filepath.Join(t.TempDir(), "reference"+suffix)
					pix := "yuv444p10le"
					if sampling == "420" {
						pix = "yuv420p"
					}
					if sampling == "422" {
						pix = "yuv422p10le"
					}
					enc := []string{"-c:v", "libx264", "-preset", "ultrafast", "-crf", "0", "-tag:v", "avc1"}
					if codec == "h265" {
						enc = []string{"-c:v", "libx265", "-preset", "ultrafast", "-crf", "0", "-tag:v", "hvc1", "-x265-params", "colorprim=bt709:range=" + value + ":transfer=bt709:colormatrix=bt709"}
					}
					if codec == "prores-hq" {
						pix = "yuv422p10le"
						enc = []string{"-c:v", "prores_ks", "-profile:v", "3"}
					}
					// This reference graph follows the observed native command, independently
					// of the production filter builder and the Go image reader.
					filter := "crop=floor(iw/2)*2:floor(ih/2)*2,scale=w=64:h=32:out_range=full:flags=lanczos,format=yuv444p10le,setparams=color_primaries=bt709:color_trc=bt709:colorspace=bt709,setsar=1/1,zscale=rangein=full:range=" + value + ":matrixin=709:matrix=709:primariesin=709:primaries=709:transferin=709:transfer=709,format=" + pix
					spatial := ""
					if mode&1 != 0 {
						spatial += ",unsharp=luma_msize_x=7:luma_msize_y=7:luma_amount=0.4"
					}
					if mode&2 != 0 {
						spatial += ",cas=strength=0.90"
					}
					filter = strings.Replace(filter, ",zscale=", spatial+",zscale=", 1)
					args := []string{"-v", "error", "-sws_flags", "print_info+accurate_rnd+bitexact+full_chroma_int", "-framerate", "24", "-i", filepath.Join(dir, "frame%02d."+extension), "-sws_flags", "print_info+accurate_rnd+bitexact+full_chroma_int", "-frames:v", "3", "-an", "-vf", filter}
					args = append(args, enc...)
					metadata := "tv"
					if value == "full" {
						metadata = "pc"
					}
					args = append(args, "-pix_fmt", pix, "-color_range", metadata, "-colorspace", "bt709", "-color_primaries", "bt709", "-color_trc", "unspecified", "-mov_gamma", "2.4", "-movflags", "+write_colr+write_gama+prefer_icc+faststart", reference)
					nativeColorCommand(t, executable, nil, args...)
					decode := func(path string) []byte {
						return nativeColorCommand(t, executable, nil, "-v", "error", "-i", path, "-f", "rawvideo", "-pix_fmt", pix, "pipe:1")
					}
					got, want := decode(output), decode(reference)
					if len(got) == 0 || !bytes.Equal(got, want) {
						t.Fatalf("decoded video differs: got %d want %d bytes, hashes %x/%x", len(got), len(want), sha256.Sum256(got), sha256.Sum256(want))
					}
					if probe, err := exec.LookPath("ffprobe"); err == nil {
						metadata := func(path string) []byte {
							return nativeColorCommand(t, probe, nil, "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=width,height,nb_frames,r_frame_rate,pix_fmt,color_range,color_space,color_transfer,color_primaries", "-of", "json", path)
						}
						if !bytes.Equal(metadata(output), metadata(reference)) {
							t.Fatal("reference video metadata differs")
						}
					}
					for path, want := range before {
						if !bytes.Equal(denoiseRead(t, path), want) {
							t.Fatal("source changed", path)
						}
					}
				})
			}
		}
	}
}

func TestNativeOutputColorValidationAndStillRetention(t *testing.T) {
	dir := outputSharpenFixture(t, 3)
	before := batchFileSnapshot(t, dir)
	for _, tc := range []struct{ extension, profile string }{{".mp4", "invalid"}, {".avi", "lrt-7.5.5"}} {
		output := filepath.Join(t.TempDir(), "keep"+tc.extension)
		if err := os.WriteFile(output, []byte("original"), 0600); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr bytes.Buffer
		if Main([]string{"render", dir, "--output", output, "--overwrite", "--output-color", tc.profile}, &stdout, &stderr) == 0 {
			t.Fatal("accepted invalid output profile", tc)
		}
		assertPreviewEVFileBytes(t, output, []byte("original"))
		entries, err := os.ReadDir(filepath.Dir(output))
		if err != nil || len(entries) != 1 {
			t.Fatal("failed render left temporary output", err)
		}
	}
	frames, err := DiscoverSequence(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, preview := range []bool{false, true} {
		var control map[string][]byte
		for _, profile := range []string{"standard", "lrt-7.5.5", "invalid"} {
			output := filepath.Join(t.TempDir(), "still")
			options := RenderOptions{Quality: 90, OutputColor: profile}
			if preview {
				_, err = GeneratePreviewImages(frames, output, options)
			} else {
				err = ExportTIFFSequence(frames, output, options)
			}
			if err != nil {
				t.Fatal(err)
			}
			contents := map[string][]byte{}
			for path, data := range batchFileSnapshot(t, output) {
				rel, err := filepath.Rel(output, path)
				if err != nil {
					t.Fatal(err)
				}
				contents[rel] = data
			}
			if profile == "standard" {
				control = contents
			} else if !reflect.DeepEqual(control, contents) {
				t.Fatal("video output profile changed still export")
			}
		}
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("source modified")
	}
}
