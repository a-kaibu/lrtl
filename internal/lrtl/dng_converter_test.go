package lrtl

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMain(main *testing.M) {
	if mode := os.Getenv("LRTL_TEST_CONVERTER_HELPER"); mode != "" {
		// A child copy of the test executable implements the external command
		// contract. The parent suite needs no Adobe software or native library.
		arguments := os.Args[1:]
		if len(arguments) > 0 && filepath.Base(arguments[len(arguments)-1]) == os.Getenv("LRTL_TEST_CONVERTER_FAIL_NAME") {
			mode = "fail"
		}
		if log := os.Getenv("LRTL_TEST_CONVERTER_LOG"); log != "" {
			data, _ := json.Marshal(arguments)
			if err := os.WriteFile(log, data, 0600); err != nil {
				panic(err)
			}
		}
		if mode == "fail" {
			fmt.Fprintln(os.Stderr, "synthetic converter failure")
			os.Exit(7)
		}
		if mode == "missing" {
			os.Exit(0)
		}
		directory, name := "", ""
		for index := 0; index+1 < len(arguments); index++ {
			if arguments[index] == "-d" {
				directory = arguments[index+1]
			}
			if arguments[index] == "-o" {
				name = arguments[index+1]
			}
		}
		data, err := os.ReadFile(os.Getenv("LRTL_TEST_CONVERTER_FIXTURE"))
		if mode == "empty" {
			data, err = nil, nil
		}
		if mode == "invalid" {
			data, err = []byte("invalid output"), nil
		}
		if err != nil {
			panic(err)
		}
		if err := os.WriteFile(filepath.Join(directory, name), data, 0600); err != nil {
			panic(err)
		}
		os.Exit(0)
	}
	os.Exit(main.Run())
}

func fakeDNGConverter(t *testing.T) (string, string, []byte) {
	t.Helper()
	dir := t.TempDir()
	pixels := make([]uint16, 16*16)
	for index := range pixels {
		pixels[index] = 16384
	}
	data := linearDNGFixture(binary.LittleEndian, 16, 16, 1, pixels, nil)
	fixture := filepath.Join(dir, "linear.dng")
	if err := os.WriteFile(fixture, data, 0600); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("LRTL_TEST_CONVERTER_HELPER", "valid")
	t.Setenv("LRTL_TEST_CONVERTER_FIXTURE", fixture)
	t.Setenv("LRTL_TEST_CONVERTER_LOG", filepath.Join(dir, "arguments.json"))
	return executable, dir, data
}

func TestDNGConverterCommandAndDevelopedPixels(t *testing.T) {
	executable, dir, sourceData := fakeDNGConverter(t)
	source := filepath.Join(dir, "source with spaces.nef")
	if err := os.WriteFile(source, sourceData, 0600); err != nil {
		t.Fatal(err)
	}
	data, err := developRawWithDNGConverter(source, executable)
	if err != nil {
		t.Fatal(err)
	}
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil || format != "tiff" || img.Bounds().Dx() != 16 || img.Bounds().Dy() != 16 {
		t.Fatalf("developed image format=%s err=%v", format, err)
	}
	r, g, b, _ := img.At(0, 0).RGBA()
	if r < 35000 || r > 35400 || r != g || g != b {
		t.Fatalf("incorrect linear normalization/sRGB: %d %d %d", r, g, b)
	}
	log, err := os.ReadFile(os.Getenv("LRTL_TEST_CONVERTER_LOG"))
	if err != nil {
		t.Fatal(err)
	}
	var arguments []string
	if err := json.Unmarshal(log, &arguments); err != nil {
		t.Fatal(err)
	}
	if len(arguments) != 9 || strings.Join(arguments[:4], " ") != "-u -l -p0 -dng1.4" || arguments[8] != source {
		t.Fatalf("unsafe or incorrect command arguments: %q", arguments)
	}
	if _, err := os.Stat(arguments[5]); !os.IsNotExist(err) {
		t.Fatalf("temporary conversion directory was retained: %v", err)
	}
	assertPreviewEVFileBytes(t, source, sourceData)
}

func TestDNGConverterFailureAndInvalidOutput(t *testing.T) {
	executable, dir, data := fakeDNGConverter(t)
	source := filepath.Join(dir, "source.nef")
	if err := os.WriteFile(source, data, 0600); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"fail", "missing", "empty", "invalid"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("LRTL_TEST_CONVERTER_HELPER", mode)
			if _, err := developRawWithDNGConverter(source, executable); err == nil {
				t.Fatal("converter failure accepted")
			}
			log, err := os.ReadFile(os.Getenv("LRTL_TEST_CONVERTER_LOG"))
			if err != nil {
				t.Fatal(err)
			}
			var arguments []string
			if err := json.Unmarshal(log, &arguments); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(arguments[5]); !os.IsNotExist(err) {
				t.Fatalf("failure leaked conversion directory: %v", err)
			}
			assertPreviewEVFileBytes(t, source, data)
		})
	}
}

func TestDNGConverterPreservesOrientationForRendering(t *testing.T) {
	executable, dir, _ := fakeDNGConverter(t)
	pixels := make([]uint16, 16*32)
	for index := range pixels {
		pixels[index] = 16384
	}
	data := linearDNGFixture(binary.LittleEndian, 16, 32, 1, pixels, map[uint16]dngTestField{274: dngTestShort(binary.LittleEndian, 6)})
	if err := os.WriteFile(os.Getenv("LRTL_TEST_CONVERTER_FIXTURE"), data, 0600); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(dir, "portrait.nef")
	if err := os.WriteFile(source, data, 0600); err != nil {
		t.Fatal(err)
	}
	developed, err := developRawWithDNGConverter(source, executable)
	if err != nil {
		t.Fatal(err)
	}
	orientation, err := readEXIFOrientation(developed)
	if err != nil || orientation != 6 {
		t.Fatalf("orientation=%d err=%v", orientation, err)
	}
	output := filepath.Join(dir, "rotated.avi")
	if err := RenderSequence([]Frame{{Image: source}}, output, RenderOptions{FPS: 24, Quality: 90, RawDevelop: true, DNGConverterPath: executable, AutoOrient: true}); err != nil {
		t.Fatal(err)
	}
	video, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	header := bytes.Index(video, []byte("avih"))
	if header < 0 || header+48 > len(video) {
		t.Fatal("missing AVI main header")
	}
	width, height := binary.LittleEndian.Uint32(video[header+40:]), binary.LittleEndian.Uint32(video[header+44:])
	if width != 32 || height != 16 {
		t.Fatalf("portrait RAW was not rotated: %dx%d", width, height)
	}
}

func TestDevelopedTIFFLargeImageKeepsReadableMetadata(t *testing.T) {
	img := image.NewNRGBA64(image.Rect(0, 0, 1024, 1024))
	img.SetNRGBA64(0, 0, color.NRGBA64{R: 12345, G: 23456, B: 34567, A: 65535})
	data, err := encodeDevelopedDNGTIFF(img, 6)
	if err != nil {
		t.Fatal(err)
	}
	if binary.BigEndian.Uint32(data[4:8]) != 8 {
		t.Fatal("TIFF IFD was moved beyond the metadata read window")
	}
	orientation, err := readEXIFOrientation(data)
	if err != nil || orientation != 6 {
		t.Fatalf("large TIFF orientation=%d err=%v", orientation, err)
	}
	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, _ := decoded.At(0, 0).RGBA()
	if r != 12345 || g != 23456 || b != 34567 {
		t.Fatalf("16-bit pixel precision lost: %d %d %d", r, g, b)
	}
}

func TestAdobeRAWBackendIsAvailableToAllAnalysisCommands(t *testing.T) {
	executable, root, data := fakeDNGConverter(t)
	dir := filepath.Join(root, "sequence")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(dir, "frame.nef")
	if err := os.WriteFile(source, data, 0600); err != nil {
		t.Fatal(err)
	}
	for _, command := range []struct{ name, output string }{
		{"render", "result.avi"}, {"preview", "result.svg"}, {"preview-ev", ""}, {"keyframes", ""}, {"deflicker", ""}, {"long-term", "filter.json"},
	} {
		t.Run(command.name, func(t *testing.T) {
			arguments := []string{command.name, dir, "--raw-develop", "--dng-converter", executable}
			if command.output != "" {
				arguments = append(arguments, "--output", filepath.Join(root, command.output))
			}
			var stdout, stderr bytes.Buffer
			if code := Main(arguments, &stdout, &stderr); code != 0 {
				t.Fatalf("%s exit=%d stdout=%s stderr=%s", command.name, code, &stdout, &stderr)
			}
			if command.output != "" {
				if info, err := os.Stat(filepath.Join(root, command.output)); err != nil || info.Size() == 0 {
					t.Fatalf("missing command output: %v", err)
				}
			}
		})
	}
	assertPreviewEVFileBytes(t, source, data)
}

func TestDNGConverterDevelopmentAppliesXMPExposureOnce(t *testing.T) {
	executable, dir, data := fakeDNGConverter(t)
	source := filepath.Join(dir, "frame.nef")
	if err := os.WriteFile(source, data, 0600); err != nil {
		t.Fatal(err)
	}
	sidecar := writeTestXMP(t, source, map[string]string{"crs:Exposure2012": "1"})
	original, err := os.ReadFile(sidecar)
	if err != nil {
		t.Fatal(err)
	}
	frames := []Frame{{Image: source, Sidecar: sidecar}}
	options := imageInputOptions{RawDevelop: true, DNGConverterPath: executable}
	baseline, err := analyzeLuminanceSequenceWithInputOptions(frames, options)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(baseline[0]-(-2)) > .02 {
		t.Fatalf("XMP was baked into the unadjusted RAW path: %v", baseline)
	}
	for _, basic := range []bool{false, true} {
		options.ApplyXMPBasic, options.ApplyXMPExposure = basic, !basic
		adjusted, err := analyzeLuminanceSequenceWithInputOptions(frames, options)
		if err != nil {
			t.Fatal(err)
		}
		if math.Abs((adjusted[0]-baseline[0])-1) > .025 {
			t.Fatalf("XMP +1 EV was not applied exactly once: baseline=%v adjusted=%v", baseline, adjusted)
		}
	}
	assertPreviewEVFileBytes(t, sidecar, original)
}

func TestAdobeRAWBatchRender(t *testing.T) {
	executable, root, data := fakeDNGConverter(t)
	batchRoot := filepath.Join(root, "sequences")
	sequence := filepath.Join(batchRoot, "sequence1")
	if err := os.MkdirAll(sequence, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sequence, "frame.nef"), data, 0600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(root, "videos")
	var stdout, stderr bytes.Buffer
	if code := Main([]string{"batch-render", batchRoot, "--output", output, "--format", "avi", "--", "--raw-develop", "--dng-converter", executable}, &stdout, &stderr); code != 0 {
		t.Fatalf("batch-render exit=%d stdout=%s stderr=%s", code, &stdout, &stderr)
	}
	if info, err := os.Stat(filepath.Join(output, "sequence1.avi")); err != nil || info.Size() == 0 {
		t.Fatalf("RAW batch did not produce video: %v", err)
	}
}

func TestAdobeAdditionalRAWExtensions(t *testing.T) {
	executable, root, data := fakeDNGConverter(t)
	for _, extension := range []string{".RAW", ".RWL", ".GPR", ".X3F"} {
		t.Run(extension, func(t *testing.T) {
			dir := filepath.Join(root, extension[1:])
			if err := os.Mkdir(dir, 0700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "frame"+extension)
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			frames, err := DiscoverSequence(dir)
			if err != nil || len(frames) != 1 {
				t.Fatalf("RAW discovery failed: %v err=%v", frames, err)
			}
			_, config, format, err := readRenderInputWithOptions(frames[0], imageInputOptions{RawDevelop: true, DNGConverterPath: executable})
			if err != nil || format != "tiff" || config.Width != 16 {
				t.Fatalf("RAW converter dispatch failed: format=%s config=%v err=%v", format, config, err)
			}
			plan, err := planImport(dir, filepath.Join(root, "import-"+extension[1:]), ImportOptions{ConvertToDNG: true, DNGConverterPath: executable})
			if err != nil || len(plan) != 1 || !plan[0].convertDNG || plan[0].destination != "frame.dng" {
				t.Fatalf("DNG import dispatch failed: %v err=%v", plan, err)
			}
		})
	}
}

func TestAdobeDNGImportPreservesSidecar(t *testing.T) {
	executable, root, data := fakeDNGConverter(t)
	source := filepath.Join(root, "input")
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(source, "frame.nef")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	sidecar := writeTestXMP(t, path, map[string]string{"crs:Exposure2012": "1.5"})
	original, err := os.ReadFile(sidecar)
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(root, "output")
	var stdout, stderr bytes.Buffer
	if code := Main([]string{"import", source, "--output", output, "--convert-dng", "--dng-converter", executable}, &stdout, &stderr); code != 0 {
		t.Fatalf("import exit=%d stderr=%s", code, &stderr)
	}
	assertPreviewEVFileBytes(t, filepath.Join(output, "frame.dng"), data)
	assertPreviewEVFileBytes(t, filepath.Join(output, "frame.dng.xmp"), original)
	assertPreviewEVFileBytes(t, path, data)
	if code := Main([]string{"import", source, "--output", output, "--convert-dng", "--dng-converter", executable}, &stdout, &stderr); code == 0 {
		t.Fatal("import overwrote existing DNG")
	}
	if code := Main([]string{"import", source, "--output", filepath.Join(root, "uncompressed"), "--convert-dng", "--dng-compression", "UNCOMPRESSED", "--dng-converter", executable}, &stdout, &stderr); code != 0 {
		t.Fatalf("uncompressed import exit=%d stderr=%s", code, &stderr)
	}
	log, err := os.ReadFile(os.Getenv("LRTL_TEST_CONVERTER_LOG"))
	if err != nil {
		t.Fatal(err)
	}
	var arguments []string
	if err := json.Unmarshal(log, &arguments); err != nil {
		t.Fatal(err)
	}
	if len(arguments) == 0 || arguments[0] != "-u" {
		t.Fatalf("compression selection was ignored: %q", arguments)
	}
}

func TestAdobeImportRollsBackPartialConversion(t *testing.T) {
	executable, root, data := fakeDNGConverter(t)
	source, output := filepath.Join(root, "input"), filepath.Join(root, "output")
	for _, dir := range []string{source, output} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"frame01.nef", "frame02.nef"} {
		if err := os.WriteFile(filepath.Join(source, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	writeTestXMP(t, filepath.Join(source, "frame01.nef"), map[string]string{"crs:Exposure2012": "1.5"})
	notes := filepath.Join(output, "notes.txt")
	if err := os.WriteFile(notes, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LRTL_TEST_CONVERTER_FAIL_NAME", "frame02.nef")
	var stdout, stderr bytes.Buffer
	if code := Main([]string{"import", source, "--output", output, "--convert-dng", "--dng-converter", executable}, &stdout, &stderr); code == 0 {
		t.Fatal("partial conversion failure was ignored")
	}
	entries, err := os.ReadDir(output)
	if err != nil || len(entries) != 1 || entries[0].Name() != "notes.txt" {
		t.Fatalf("partial import was not rolled back: %v err=%v", entries, err)
	}
	assertPreviewEVFileBytes(t, notes, []byte("keep"))
	for _, name := range []string{"frame01.nef", "frame02.nef"} {
		assertPreviewEVFileBytes(t, filepath.Join(source, name), data)
	}
}

func TestDNGConverterOptionsValidation(t *testing.T) {
	for _, options := range []imageInputOptions{
		{DNGConverterPath: "converter"}, {RawDevelop: true, DNGConverterPath: "converter", DNGLabPath: "dnglab"},
		{RawPreview: true, RawDevelop: true},
	} {
		if err := validateImageInputOptions(options); err == nil {
			t.Fatalf("invalid backend options accepted: %+v", options)
		}
	}
	if _, err := resolveDNGConverter(filepath.Join(t.TempDir(), "missing-converter")); err == nil {
		t.Fatal("missing explicit converter accepted")
	}
}

func TestGUIAdobeRAWPreviewAndAnalysis(t *testing.T) {
	executable, root, data := fakeDNGConverter(t)
	dir := filepath.Join(root, "sequence")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "frame.nef")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	handler := newGUIHandler()
	for _, endpoint := range []string{"/api/preview", "/api/luminance"} {
		query := url.Values{"directory": {dir}, "name": {"frame.nef"}, "raw": {"develop"}, "dngconverter": {executable}}
		request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8787"+endpoint+"?"+query.Encode(), nil)
		request.RemoteAddr = "127.0.0.1:1234"
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", endpoint, response.Code, response.Body)
		}
		if endpoint == "/api/preview" {
			if _, _, err := image.Decode(bytes.NewReader(response.Body.Bytes())); err != nil {
				t.Fatal(err)
			}
		}
	}
	payload, _ := json.Marshal(guiKeyframesRequest{Directory: dir, Count: 1, Rating: 4, DNGMode: "develop", DNGConverter: executable})
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8787/api/keyframes", bytes.NewReader(payload))
	request.RemoteAddr = "127.0.0.1:1234"
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://127.0.0.1:8787")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("keyframe API status=%d body=%s", response.Code, response.Body)
	}
	payload, _ = json.Marshal(guiRenderRequest{Directory: dir, Output: filepath.Join(root, "gui.avi"), FPS: 24, DNGMode: "develop", DNGConverter: executable})
	request = httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8787/api/render", bytes.NewReader(payload))
	request.RemoteAddr = "127.0.0.1:1234"
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://127.0.0.1:8787")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("render API status=%d body=%s", response.Code, response.Body)
	}
	video, err := os.ReadFile(filepath.Join(root, "gui.avi"))
	if err != nil || len(video) < 12 || string(video[:4]) != "RIFF" || string(video[8:12]) != "AVI " {
		t.Fatalf("GUI did not create an AVI: %v", err)
	}
}

func TestAdobeDNGConverterRealRAW(t *testing.T) {
	source := os.Getenv("LRTL_RAW_SAMPLE")
	if source == "" {
		t.Skip("set LRTL_RAW_SAMPLE to test installed Adobe DNG Converter on a real RAW")
	}
	original, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	before := sha256.Sum256(original)
	data, err := developRawWithDNGConverter(source, os.Getenv("LRTL_DNG_CONVERTER"))
	if err != nil {
		t.Fatal(err)
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || format != "tiff" || config.Width < 1 || config.Height < 1 {
		t.Fatalf("real RAW output config=%v format=%s err=%v", config, format, err)
	}
	query := url.Values{"directory": {filepath.Dir(source)}, "name": {filepath.Base(source)}, "raw": {"develop"}}
	if converter := os.Getenv("LRTL_DNG_CONVERTER"); converter != "" {
		query.Set("dngconverter", converter)
	}
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8787/api/preview?"+query.Encode(), nil)
	request.RemoteAddr = "127.0.0.1:1234"
	response := httptest.NewRecorder()
	newGUIHandler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("real RAW GUI preview status=%d: %s", response.Code, response.Body)
	}
	if _, _, err := image.DecodeConfig(bytes.NewReader(response.Body.Bytes())); err != nil {
		t.Fatalf("real RAW GUI preview is not decodable: %v", err)
	}
	after, err := os.ReadFile(source)
	if err != nil || sha256.Sum256(after) != before {
		t.Fatal("RAW source changed during development")
	}
	t.Logf("real RAW -> %dx%d %s", config.Width, config.Height, format)
}
