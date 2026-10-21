package lrtl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func nativeWideReferenceColor(t *testing.T, gamut, dynamic int, value string) (string, string) {
	t.Helper()
	var rows []struct {
		Source                          string
		Gamut, DynamicRange, VideoRange int
		Filter, Metadata                string
	}
	if err := json.Unmarshal(denoiseRead(t, "testdata/lrt-color-plan-7.5.5.json"), &rows); err != nil {
		t.Fatal(err)
	}
	rangeID := 0
	if value == "full" {
		rangeID = 1
	}
	for _, row := range rows {
		if row.Source == "BT2020" && row.Gamut == gamut && row.DynamicRange == dynamic && row.VideoRange == rangeID {
			return row.Filter, row.Metadata
		}
	}
	t.Fatal("missing independently measured color plan")
	return "", ""
}

func nativeWideReferenceEncoder(codec, value string, gamut, dynamic int) []string {
	primary, matrix, trc := "bt709", "bt709", "bt709"
	if gamut == 1 {
		primary, matrix, trc = "bt2020", "bt2020nc", "bt2020-10"
	}
	switch codec {
	case "h264":
		full := "off"
		if value == "full" {
			full = "on"
		}
		return []string{"-c:v", "libx264", "-preset", "ultrafast", "-crf", "0", "-tag:v", "avc1", "-x264-params", "colorprim=" + primary + ":fullrange=" + full + ":transfer=" + trc + ":colormatrix=" + matrix}
	case "h265":
		if gamut == 0 {
			matrix = "bt2020nc"
		}
		if dynamic == 1 {
			trc = "smpte2084"
		}
		params := "colorprim=" + primary + ":range=" + value + ":transfer=" + trc + ":colormatrix=" + matrix
		if dynamic == 1 {
			params += ":master-display=G(13250,34500)B(7500,3000)R(34000,16000)WP(15635,16450)L(10000000,1):max-cll=1000,400"
		}
		return []string{"-c:v", "libx265", "-preset", "ultrafast", "-crf", "0", "-tag:v", "hvc1", "-x265-params", params}
	default:
		if gamut == 1 {
			trc = "auto"
			if dynamic == 1 {
				trc = "smpte2084"
			}
		}
		return []string{"-c:v", "prores_ks", "-profile:v", "3", "-bsf:v", "prores_metadata=color_primaries=" + primary + ":color_trc=" + trc + ":colorspace=" + matrix}
	}
}

func TestNativeWideColorActualVideos(t *testing.T) {
	exe, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("FFmpeg required")
	}
	probe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe required")
	}
	for _, combined := range []bool{false, true} {
		count := 3
		tc := nativeTemporalCase{}
		if combined {
			count = 17
			tc = nativeTemporalCase{noise: 4, air: 3, luma: true, chroma: true, spatial: true}
		}
		dir := nativePlaybackFixture(t, count)
		before := batchFileSnapshot(t, dir)
		for _, mode := range []struct {
			name string
			g, d int
		}{{"2020-to-709", 0, 0}, {"2020-sdr", 1, 0}, {"2020-pq", 1, 1}} {
			for _, codec := range []string{"h264", "h265", "prores-hq"} {
				samplings := []string{"420", "422", "444"}
				if codec == "prores-hq" {
					samplings = []string{""}
				}
				for _, sampling := range samplings {
					for _, value := range []string{"full", "limited"} {
						t.Run(fmt.Sprintf("%t/%s/%s/%s/%s", combined, mode.name, codec, sampling, value), func(t *testing.T) {
							gamut, dynamic, primary, matrix, transfer := "bt709", "sdr", "bt709", "bt709", "unspecified"
							if mode.g == 1 {
								gamut, primary, matrix = "bt2020", "bt2020", "bt2020nc"
							}
							if mode.d == 1 {
								dynamic, transfer = "pq", "smpte2084"
							}
							pix := "yuv" + sampling + "p10le"
							if sampling == "420" && mode.g == 0 {
								pix = "yuv420p"
							}
							if codec == "prores-hq" {
								pix = "yuv422p10le"
							}
							suffix := ".mp4"
							if codec == "prores-hq" {
								suffix = ".mov"
							}
							output, reference := filepath.Join(t.TempDir(), "go"+suffix), filepath.Join(t.TempDir(), "ref"+suffix)
							args := []string{"render", dir, "--output", output, "--ffmpeg", exe, "--output-color", "lrt-7.5.5", "--input-color", "bt2020", "--output-gamut", gamut, "--dynamic-range", dynamic, "--fps", "24", "--codec", codec, "--crf", "0", "--preset", "ultrafast", "--video-range", value}
							if sampling != "" {
								args = append(args, "--subsampling", sampling)
							}
							markerCommand(t, append(args, nativeTemporalCLI(tc)...)...)
							filter, metadata := nativeWideReferenceColor(t, mode.g, mode.d, value)
							graph := strings.Replace(nativeTemporalReferenceGraph(t, tc), "setparams=color_primaries=bt709:color_trc=bt709:colorspace=bt709", "setparams="+metadata, 1) + filter + ",format=" + pix
							refArgs := []string{"-v", "error", "-sws_flags", "print_info+accurate_rnd+bitexact+full_chroma_int", "-framerate", "24", "-i", filepath.Join(dir, "frame%02d.png"), "-filter_complex", graph, "-an"}
							refArgs = append(refArgs, nativeWideReferenceEncoder(codec, value, mode.g, mode.d)...)
							rangeTag := "tv"
							if value == "full" {
								rangeTag = "pc"
							}
							refArgs = append(refArgs, "-pix_fmt", pix, "-color_range", rangeTag, "-color_primaries", primary, "-colorspace", matrix, "-color_trc", transfer)
							if mode.d == 0 {
								refArgs = append(refArgs, "-mov_gamma", "2.4", "-movflags", "+write_colr+write_gama+prefer_icc+faststart")
							} else {
								refArgs = append(refArgs, "-movflags", "+write_colr+faststart", "-metadata:s:v:0", "master_display=\"G(13250,34500)B(7500,3000)R(34000,16000)WP(15635,16450)L(10000000,0.005)\"", "-metadata:s:v:0", "max_cll=\"1000,400\"")
							}
							refArgs = append(refArgs, reference)
							nativeFilterReferenceCommand(t, exe, nil, refArgs...)
							decode := func(path string) []byte {
								return nativeColorCommand(t, exe, nil, "-v", "error", "-i", path, "-pix_fmt", pix, "-f", "rawvideo", "pipe:1")
							}
							got, want := decode(output), decode(reference)
							if len(got) == 0 || !bytes.Equal(got, want) {
								t.Fatalf("native wide pixels differ bytes%d/%d", len(got), len(want))
							}
							getMetadata := func(path string) []byte {
								return nativeColorCommand(t, probe, nil, "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=width,height,nb_frames,r_frame_rate,pix_fmt,color_range,color_space,color_transfer,color_primaries", "-of", "json", path)
							}
							if !bytes.Equal(getMetadata(output), getMetadata(reference)) {
								t.Fatalf("native wide metadata differs %s/%s", getMetadata(output), getMetadata(reference))
							}
							if !combined && codec == "h265" && mode.d == 1 && sampling == "420" && value == "limited" {
								data := nativeColorCommand(t, probe, nil, "-v", "error", "-select_streams", "v:0", "-read_intervals", "%+#1", "-show_frames", "-of", "json", output)
								if !bytes.Contains(data, []byte("Mastering display metadata")) || !bytes.Contains(data, []byte("Content light level metadata")) {
									t.Fatal("HDR mastering/CLL metadata absent", string(data))
								}
							}
						})
					}
				}
			}
		}
		for path, data := range before {
			assertPreviewEVFileBytes(t, path, data)
		}
	}
}

func TestNativeWideColorMeasuredPlans(t *testing.T) {
	var rows []struct {
		Source                          string
		Gamut, DynamicRange, VideoRange int
		Filter, Metadata, Error         string
	}
	if err := json.Unmarshal(denoiseRead(t, "testdata/lrt-color-plan-7.5.5.json"), &rows); err != nil || len(rows) != 162 {
		t.Fatalf("expected 162 native color plans: %v", err)
	}
	for _, row := range rows {
		filter, metadata, err := nativeColorPlan(row.Source, row.Gamut, row.DynamicRange, row.VideoRange)
		message := ""
		if err != nil {
			message = err.Error()
		}
		if filter != row.Filter || metadata != row.Metadata || message != row.Error {
			t.Fatalf("native plan changed: %+v got%q %q %q", row, filter, metadata, message)
		}
	}
	for _, tc := range []RenderOptions{{InputColor: "bad"}, {OutputGamut: "bad"}, {DynamicRange: "bad"}, {DynamicRange: "pq"}, {OutputGamut: "bt2020"}} {
		if _, err := renderNativeColorSpec(tc); err == nil {
			t.Fatal("invalid native color plan accepted", tc)
		}
	}
	for _, dynamic := range []string{"sdr", "pq"} {
		spec, err := renderNativeColorSpec(RenderOptions{InputColor: "bt2020", OutputGamut: "bt2020", DynamicRange: dynamic})
		if err != nil {
			t.Fatal(err)
		}
		if pix, err := spec.h26xPixelFormat("420"); err != nil || pix != "yuv420p10le" {
			t.Fatal("wide output must retain 10-bit", pix, err)
		}
		if !strings.Contains(spec.filter("full", "yuv420p10le", true), "primaries=2020:") {
			t.Fatal("wide primaries lost")
		}
	}
}

func TestNativeWideColorMeasuredPixelFormats(t *testing.T) {
	var rows []struct {
		Codec, Sampling, PixelFormat    string
		Gamut, DynamicRange, VideoRange int
	}
	if err := json.Unmarshal(denoiseRead(t, "testdata/lrt-wide-pixel-format-7.5.5.json"), &rows); err != nil || len(rows) != 48 {
		t.Fatalf("expected 48 native format measurements: %v", err)
	}
	for _, row := range rows {
		spec := nativeColorSpec{gamut: row.Gamut, dynamic: row.DynamicRange}
		got, err := spec.h26xPixelFormat(row.Sampling)
		if err != nil || got != row.PixelFormat {
			t.Fatalf("native format %+v got%s error%v", row, got, err)
		}
	}
}

func TestNativeWideColorCallerTimestampFade(t *testing.T) {
	nativeWideCallerTimestampFade(t, false)
}

func TestNativeWatermarkWideCallerTimestampFade(t *testing.T) {
	if _, err := nativeWatermarkFont(); err != nil {
		t.Skip(err)
	}
	nativeWideCallerTimestampFade(t, true)
}

func nativeWideCallerTimestampFade(t *testing.T, watermark bool) {
	exe, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("FFmpeg required")
	}
	var rows []struct {
		Gamut, DynamicRange int
		Graph               string
	}
	if err := json.Unmarshal(denoiseRead(t, "testdata/lrt-wide-caller-7.5.5.json"), &rows); err != nil || len(rows) != 3 {
		t.Fatalf("expected three native wide caller graphs: %v", err)
	}
	dir := nativeTimestampBurnFixture(t, 3)
	file, err := os.CreateTemp(".", ".lrtl-wide-reference-*.srt")
	if err != nil {
		t.Fatal(err)
	}
	name := file.Name()
	_ = file.Close()
	t.Cleanup(func() { _ = os.Remove(name) })
	nativeTimestampReferenceSRT(t, name, 3, 1, 3)
	for _, row := range rows {
		for _, value := range []string{"limited", "full"} {
			t.Run(fmt.Sprintf("%d-%d/%s", row.Gamut, row.DynamicRange, value), func(t *testing.T) {
				preset := savePresetJSON(t, lrtRenderPreset{Encoder: "h265", FrameRate: "24", OutputResV: 32, Speed: 1, Gamut: row.Gamut, DynamicRange: row.DynamicRange, FadeOut: true, FadeTime: 1, Sharpen: true})
				out, ref := filepath.Join(t.TempDir(), "go.mp4"), filepath.Join(t.TempDir(), "ref.mp4")
				flags := []string{"render", dir, "--output", out, "--ffmpeg", exe, "--lrt-render-preset", preset, "--input-color", "bt2020", "--timestamp-style", "lrt-7.5.5", "--timestamp-every", "3", "--video-range", value, "--crf", "0", "--preset", "ultrafast"}
				if watermark {
					flags = append(flags, "--watermark", "LRTL", "--watermark-style", "lrt-7.5.5")
				}
				markerCommand(t, flags...)
				graph := strings.TrimSuffix(row.Graph, "[v]")
				graph = strings.ReplaceAll(graph, "wide-timestamp-reference.srt", filepath.Base(name))
				graph = strings.ReplaceAll(graph, "range=limited:", "range="+value+":")
				if watermark {
					graph = strings.Replace(graph, ",subtitles=", nativeWatermarkReferenceFilter(t, "LRTL")+",subtitles=", 1)
				}
				primary, matrix, trc, pix := "bt709", "bt709", "unspecified", "yuv420p"
				if row.Gamut == 1 {
					primary, matrix, pix = "bt2020", "bt2020nc", "yuv420p10le"
				}
				if row.DynamicRange == 1 {
					trc = "smpte2084"
				}
				args := []string{"-v", "error", "-framerate", "24", "-i", filepath.Join(dir, "frame%02d.png"), "-filter_complex", graph, "-an"}
				args = append(args, nativeWideReferenceEncoder("h265", value, row.Gamut, row.DynamicRange)...)
				rangeTag := "tv"
				if value == "full" {
					rangeTag = "pc"
				}
				args = append(args, "-pix_fmt", pix, "-color_range", rangeTag, "-color_primaries", primary, "-colorspace", matrix, "-color_trc", trc, ref)
				nativeFilterReferenceCommand(t, exe, nil, args...)
				decode := func(path string) []byte {
					return nativeColorCommand(t, exe, nil, "-v", "error", "-i", path, "-pix_fmt", pix, "-f", "rawvideo", "pipe:1")
				}
				if !bytes.Equal(decode(out), decode(ref)) {
					t.Fatal("wide caller timestamp/fade pixels differ")
				}
				assertNativeWideVideoMetadata(t, out, ref)
			})
		}
	}
}

func TestNativeWideColorDNxHR(t *testing.T) {
	exe, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("FFmpeg required")
	}
	dir := t.TempDir()
	for f := 0; f < 3; f++ {
		img, err := imageFromRGB48(nativeColorSource(256, 128, "lowbits"), 256, 128)
		if err != nil {
			t.Fatal(err)
		}
		var data bytes.Buffer
		if err := png.Encode(&data, img); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("frame%02d.png", f)), data.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, dynamic := range []string{"sdr", "pq"} {
		for _, value := range []string{"full", "limited"} {
			t.Run(dynamic+"/"+value, func(t *testing.T) {
				d := 0
				trc := "unspecified"
				if dynamic == "pq" {
					d = 1
					trc = "smpte2084"
				}
				out, ref := filepath.Join(t.TempDir(), "go.mov"), filepath.Join(t.TempDir(), "ref.mov")
				markerCommand(t, "render", dir, "--output", out, "--ffmpeg", exe, "--output-color", "lrt-7.5.5", "--input-color", "bt2020", "--output-gamut", "bt2020", "--dynamic-range", dynamic, "--codec", "dnxhr-hqx", "--video-range", value, "--fps", "24")
				filter, metadata := nativeWideReferenceColor(t, 1, d, value)
				graph := "scale=iw:ih:out_range=full:flags=lanczos,format=yuv444p10le,setparams=" + metadata + filter + ",format=yuv422p10le"
				rangeTag := "tv"
				if value == "full" {
					rangeTag = "pc"
				}
				nativeColorCommand(t, exe, nil, "-v", "error", "-framerate", "24", "-i", filepath.Join(dir, "frame%02d.png"), "-vf", graph, "-an", "-c:v", "dnxhd", "-profile:v", "dnxhr_hqx", "-pix_fmt", "yuv422p10le", "-color_range", rangeTag, "-color_primaries", "bt2020", "-colorspace", "bt2020nc", "-color_trc", trc, ref)
				decode := func(path string) []byte {
					return nativeColorCommand(t, exe, nil, "-v", "error", "-i", path, "-pix_fmt", "yuv422p10le", "-f", "rawvideo", "pipe:1")
				}
				if !bytes.Equal(decode(out), decode(ref)) {
					t.Fatal("native wide DNxHR pixels differ")
				}
				assertNativeWideVideoMetadata(t, out, ref)
			})
		}
	}
}

func assertNativeWideVideoMetadata(t *testing.T, out, reference string) {
	t.Helper()
	probe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe required for metadata comparison")
	}
	read := func(path string) []byte {
		return nativeColorCommand(t, probe, nil, "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=width,height,nb_frames,r_frame_rate,pix_fmt,color_range,color_space,color_transfer,color_primaries", "-of", "json", path)
	}
	if got, want := read(out), read(reference); !bytes.Equal(got, want) {
		t.Fatalf("wide metadata differs %s/%s", got, want)
	}
}

func TestNativeWideColorPresetBatchAndValidation(t *testing.T) {
	dir := nativePlaybackFixture(t, 3)
	before := batchFileSnapshot(t, dir)
	fake, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(t.TempDir(), "capture")
	t.Setenv("LRTL_TEST_FFMPEG_CAPTURE", capture)
	t.Setenv("LRTL_TEST_FFMPEG_FORMATS", "yuv420p yuv420p10le yuv422p10le yuv444p10le")
	preset := savePresetJSON(t, lrtRenderPreset{Encoder: "h265", FrameRate: "24", OutputResV: 32, Speed: 1, Gamut: 1, DynamicRange: 1})
	loaded, err := loadLRTRenderPreset(preset, "")
	if err != nil {
		t.Fatal(err)
	}
	overridden, err := applyLRTRenderPreset(RenderOptions{InputColor: "bt2020", OutputGamut: "bt709", DynamicRange: "sdr"}, loaded, preset, map[string]bool{"output-gamut": true, "dynamic-range": true})
	if err != nil || overridden.OutputGamut != "bt709" || overridden.DynamicRange != "sdr" {
		t.Fatal("explicit gamut/range override lost", overridden, err)
	}
	flags := []string{"--ffmpeg", fake, "--input-color", "bt2020", "--lrt-render-preset", preset}
	markerCommand(t, append([]string{"render", dir, "--output", filepath.Join(t.TempDir(), "go.mp4")}, flags...)...)
	want := denoiseRead(t, capture)
	var args []string
	if err := json.Unmarshal(denoiseRead(t, capture+".json"), &args); err != nil {
		t.Fatal(err)
	}
	get := func(flag string) string {
		for i, arg := range args {
			if arg == flag {
				return args[i+1]
			}
		}
		return ""
	}
	if get("-pix_fmt") != "yuv420p10le" || get("-color_primaries") != "bt2020" || get("-color_trc") != "smpte2084" || get("-mov_gamma") != "" || get("-movflags") != "+write_colr+faststart" {
		t.Fatal("native PQ preset flags", args)
	}
	if !strings.Contains(get("-x265-params"), "master-display=") || !strings.Contains(get("-x265-params"), "max-cll=1000,400") {
		t.Fatal("missing native mastering metadata", args)
	}
	markerCommand(t, append([]string{"batch-render", dir, "--output", t.TempDir(), "--"}, flags...)...)
	if !bytes.Equal(want, denoiseRead(t, capture)) {
		t.Fatal("batch changed prepared wide source")
	}
	for _, extra := range [][]string{{"--output-gamut", "bt2020"}, {"--dynamic-range", "pq"}, {"--input-color", "bt2020"}, {"--output-color", "lrt-7.5.5", "--input-color", "bt2020", "--raw-develop"}, {"--output-color", "lrt-7.5.5", "--input-color", "bt2020", "--apply-xmp-basic"}, {"--output-color", "lrt-7.5.5", "--output-gamut", "bad"}} {
		output := filepath.Join(t.TempDir(), "keep.mp4")
		if err := os.WriteFile(output, []byte("original"), 0600); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr bytes.Buffer
		if Main(append([]string{"render", dir, "--output", output, "--overwrite", "--ffmpeg", fake}, extra...), &stdout, &stderr) == 0 {
			t.Fatal("invalid color setup accepted", extra)
		}
		assertPreviewEVFileBytes(t, output, []byte("original"))
		entries, err := os.ReadDir(filepath.Dir(output))
		if err != nil || len(entries) != 1 {
			t.Fatal("invalid color left temporary files", entries, err)
		}
	}
	for path, data := range before {
		assertPreviewEVFileBytes(t, path, data)
	}
}

func FuzzNativeWideColorPlan(f *testing.F) {
	f.Add("bt2020", true, true, uint8(0))
	f.Add("bt709", false, false, uint8(2))
	f.Fuzz(func(t *testing.T, input string, wide, pq bool, sample uint8) {
		if len(input) > 128 {
			return
		}
		gamut, dynamic := "bt709", "sdr"
		if wide {
			gamut = "bt2020"
		}
		if pq {
			dynamic = "pq"
		}
		spec, err := renderNativeColorSpec(RenderOptions{InputColor: input, OutputGamut: gamut, DynamicRange: dynamic})
		if err != nil {
			return
		}
		if err := spec.validate(); err != nil {
			t.Fatal("accepted plan fails backend validation", err)
		}
		pix, err := spec.h26xPixelFormat([]string{"420", "422", "444"}[sample%3])
		if err != nil {
			t.Fatal(err)
		}
		graph := spec.filter("limited", pix, true)
		if !strings.Contains(graph, "zscale=") || !strings.HasSuffix(graph, "format="+pix) {
			t.Fatal("invalid native color graph", graph)
		}
		if wide && !strings.Contains(pix, "10") {
			t.Fatal("wide plan lost precision")
		}
	})
}

// Independent SDR reference options transcribed from the installed encoder
// caller. These were previously omitted by the constructed video references.
func nativeSDRReferenceEncoderMetadata(codec, value string) []string {
	switch codec {
	case "h264":
		full := "off"
		if value == "full" {
			full = "on"
		}
		return []string{"-x264-params", "colorprim=bt709:fullrange=" + full + ":transfer=bt709:colormatrix=bt709"}
	case "prores-hq":
		return []string{"-bsf:v", "prores_metadata=color_primaries=bt709:color_trc=bt709:colorspace=bt709"}
	}
	return nil
}
