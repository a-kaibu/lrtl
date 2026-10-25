package lrtl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func nativePreviewFixture(t *testing.T, root string) (Frame, []byte) {
	t.Helper()
	path := filepath.Join(root, "frame.001.NEF")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("not a decodable RAW; the cache must be used"), 0600); err != nil {
		t.Fatal(err)
	}
	sidecar := writeTestXMP(t, path, map[string]string{"crs:Exposure2012": "4", "crs:ConvertToGrayscale": "True", "crs:HasCrop": "True", "crs:CropLeft": ".2", "crs:CropTop": ".2", "crs:CropRight": ".8", "crs:CropBottom": ".8"})
	// Original gradient created by our Java probe and encoded with the installed
	// LRT 7.5.5 tQa JPEG encoder; no vendor images or executable code are included.
	data, err := os.ReadFile("testdata/lrt-native-preview-7.5.5.jpg")
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"camera", "visual"} {
		cache := lrtPreviewPath(path, kind)
		if err := os.MkdirAll(filepath.Dir(cache), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(cache, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return Frame{Image: path, Sidecar: sidecar}, data
}

func TestLRTPreviewPathsAndNativeJPEG(t *testing.T) {
	frame, data := nativePreviewFixture(t, t.TempDir())
	// Both locations were obtained by invoking the installed LRT's h(File) and
	// j(File) methods on frame.001.NEF, including preservation of earlier dots.
	for _, test := range []struct{ kind, folder string }{{"camera", "previews"}, {"visual", "visual"}} {
		want := filepath.Join(filepath.Dir(frame.Image), ".lrt", test.folder, "frame.001.lrtpreview")
		if got := lrtPreviewPath(frame.Image, test.kind); got != want {
			t.Fatalf("%s != %s", got, want)
		}
		got, config, format, err := readRenderInputWithOptions(frame, imageInputOptions{LRTPreview: test.kind})
		if err != nil || format != "jpeg" || config.Width != 32 || config.Height != 16 || !bytes.Equal(got, data) {
			t.Fatalf("native JPEG read: %v %+v %s", err, config, format)
		}
	}
	// The selected cache is authoritative, even if the other cache is valid.
	if err := os.WriteFile(lrtPreviewPath(frame.Image, "camera"), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := readRenderInputWithOptions(frame, imageInputOptions{LRTPreview: "camera"}); err == nil {
		t.Fatal("broken camera cache silently fell back")
	}
	if _, _, _, err := readRenderInputWithOptions(frame, imageInputOptions{LRTPreview: "visual"}); err != nil {
		t.Fatal(err)
	}
}

func TestLRTCameraPreviewResizeMatchesNativeHistogram(t *testing.T) {
	data, err := os.ReadFile("testdata/lrt-preview-resize-7.5.5.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		SourceWidth, SourceHeight   int
		PreviewWidth, PreviewHeight int
		Pattern                     int
		PreviewEV, SourceEV         float64
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) != 3 {
		t.Fatalf("got %d native resize cases, want 3", len(cases))
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%dx%d", tc.SourceWidth, tc.SourceHeight), func(t *testing.T) {
			img := image.NewNRGBA(image.Rect(0, 0, tc.SourceWidth, tc.SourceHeight))
			for y := 0; y < tc.SourceHeight; y++ {
				for x := 0; x < tc.SourceWidth; x++ {
					img.SetNRGBA(x, y, color.NRGBA{
						R: uint8((x*37 + y*11 + tc.Pattern*17) & 255),
						G: uint8((x*13 + y*41 + tc.Pattern*29) & 255),
						B: uint8((x*23 + y*7 + tc.Pattern*43) & 255),
						A: 255,
					})
				}
			}
			preview, err := resizeLRTCameraPreview(img)
			if err != nil {
				t.Fatal(err)
			}
			if got := preview.Bounds().Size(); got != image.Pt(tc.PreviewWidth, tc.PreviewHeight) {
				t.Fatalf("preview size=%v, want %dx%d", got, tc.PreviewWidth, tc.PreviewHeight)
			}
			got, err := framePreviewEV(preview)
			if err != nil || math.Abs(got-tc.PreviewEV) > 1e-5 {
				t.Fatalf("resized Preview EV=%.17g, native=%.17g, err=%v", got, tc.PreviewEV, err)
			}
			sourceEV, err := framePreviewEV(img)
			if err != nil || sourceEV != tc.SourceEV {
				t.Fatalf("source Preview EV=%.17g, native=%.17g, err=%v", sourceEV, tc.SourceEV, err)
			}
		})
	}
}

func TestLRTCameraPreviewResizeOnlyUsesWidthThreshold(t *testing.T) {
	for _, bounds := range []image.Rectangle{
		image.Rect(0, 0, 1024, 1600),
		image.Rect(0, 0, 800, 2000),
	} {
		img := image.NewNRGBA(bounds)
		got, err := resizeLRTCameraPreview(img)
		if err != nil || got != img {
			t.Fatalf("image %v was unexpectedly resized: got=%v err=%v", bounds, got.Bounds(), err)
		}
	}
	if _, err := resizeLRTCameraPreview(nil); err == nil {
		t.Fatal("nil preview accepted")
	}
	if _, err := resizeLRTCameraPreview(image.NewNRGBA(image.Rectangle{})); err == nil {
		t.Fatal("empty preview accepted")
	}
}

func TestLRTPreviewBilinearPixelsMatchNative(t *testing.T) {
	data, err := os.ReadFile("testdata/lrt-preview-resize-pixels-7.5.5.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		SourceWidth  int
		SourceHeight int
		Width        int
		Height       int
		Pattern      int
		RGB          []string
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) != 5 {
		t.Fatalf("got %d native pixel cases, want 5", len(cases))
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%dx%d_to_%dx%d", tc.SourceWidth, tc.SourceHeight, tc.Width, tc.Height), func(t *testing.T) {
			img := image.NewNRGBA(image.Rect(7, 11, 7+tc.SourceWidth, 11+tc.SourceHeight))
			for y := 0; y < tc.SourceHeight; y++ {
				for x := 0; x < tc.SourceWidth; x++ {
					img.SetNRGBA(7+x, 11+y, color.NRGBA{
						R: uint8((x*47 + y*19 + tc.Pattern*17) & 255),
						G: uint8((x*13 + y*71 + tc.Pattern*29) & 255),
						B: uint8((x*83 + y*31 + tc.Pattern*43) & 255),
						A: 255,
					})
				}
			}
			got, err := resizeLRTPreviewTo(img, tc.Width, tc.Height)
			if err != nil {
				t.Fatal(err)
			}
			if len(tc.RGB) != tc.Width*tc.Height {
				t.Fatalf("oracle has %d pixels, want %d", len(tc.RGB), tc.Width*tc.Height)
			}
			for i, want := range tc.RGB {
				x, y := i%tc.Width, i/tc.Width
				pixel := color.NRGBAModel.Convert(got.At(x, y)).(color.NRGBA)
				actual := fmt.Sprintf("%02x%02x%02x", pixel.R, pixel.G, pixel.B)
				if actual != want || pixel.A != 255 {
					t.Errorf("pixel (%d,%d)=%s alpha=%d, native=%s", x, y, actual, pixel.A, want)
				}
			}
		})
	}
}

func TestLRTPreviewBilinearKeepsTransparentColorsPremultiplied(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	img.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 0})
	img.SetNRGBA(1, 0, color.NRGBA{B: 255, A: 255})
	got, err := resizeLRTPreviewTo(img, 3, 1)
	if err != nil {
		t.Fatal(err)
	}
	want := []color.NRGBA{
		{},
		{B: 255, A: 127},
		{B: 255, A: 255},
	}
	for x, expected := range want {
		actual := color.NRGBAModel.Convert(got.At(x, 0)).(color.NRGBA)
		if actual != expected {
			t.Errorf("pixel (%d,0)=%#v, want %#v", x, actual, expected)
		}
	}
}

func TestPreviewEVSequenceMeasuresResizedLRTPreview(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 1300, 900))
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			img.SetNRGBA(x, y, color.NRGBA{
				R: uint8((x*37 + y*11) & 255),
				G: uint8((x*13 + y*41) & 255),
				B: uint8((x*23 + y*7) & 255),
				A: 255,
			})
		}
	}
	path := filepath.Join(t.TempDir(), "frame.png")
	writePreviewEVFixture(t, path, img)
	values, err := analyzePreviewEVSequence([]Frame{{Image: path}}, imageInputOptions{})
	if err != nil {
		t.Fatal(err)
	}
	const want = 0.4970800189788378 // native 7.5.5 after 1024x682 resize
	if len(values) != 1 || math.Abs(values[0]-want) > 1e-5 {
		t.Fatalf("sequence Preview EV=%v, want native resized value %.17g", values, want)
	}
}

func TestLRTPreviewRenderExportAndPreview(t *testing.T) {
	root := t.TempDir()
	frame, data := nativePreviewFixture(t, root)
	before := batchFileSnapshot(t, root)
	reference, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"camera", "visual"} {
		t.Run(kind, func(t *testing.T) {
			output, previews, video := filepath.Join(t.TempDir(), "tiff"), filepath.Join(t.TempDir(), "previews"), filepath.Join(t.TempDir(), "cache.avi")
			var out, stderr bytes.Buffer
			for _, args := range [][]string{
				{"render", root, "--output", video, "--lrt-preview", kind},
				{"export", root, "--output", output, "--lrt-preview", kind},
				{"preview-images", root, "--output", previews, "--lrt-preview", kind},
			} {
				if Main(args, &out, &stderr) != 0 {
					t.Fatal(&stderr)
				}
			}
			tiff := readExportedTIFF(t, filepath.Join(output, "frame000001.tif"))
			for y := 0; y < 16; y++ {
				for x := 0; x < 32; x++ {
					want := color.NRGBA64Model.Convert(reference.At(x, y)).(color.NRGBA64)
					got := color.NRGBA64Model.Convert(tiff.At(x, y)).(color.NRGBA64)
					if got != want {
						t.Fatalf("cache was transformed or quantized: %v != %v", got, want)
					}
				}
			}
			avi, err := os.ReadFile(video)
			if err != nil {
				t.Fatal(err)
			}
			start := bytes.Index(avi, []byte{255, 216})
			if start < 0 || !bytes.Equal(avi[start:start+len(data)], data) {
				t.Fatal("native JPEG was not passed through to AVI")
			}
			manifest := readPreviewManifest(t, previews)
			if manifest.Frames[0].Source != filepath.Base(frame.Image) || manifest.Width != 32 || manifest.Height != 16 {
				t.Fatal("original source mapping lost")
			}
			jpg, err := os.ReadFile(filepath.Join(previews, manifest.Frames[0].File))
			if err != nil {
				t.Fatal(err)
			}
			var want bytes.Buffer
			rgb := image.NewNRGBA64(reference.Bounds())
			for y := 0; y < 16; y++ {
				for x := 0; x < 32; x++ {
					rgb.SetNRGBA64(x, y, color.NRGBA64Model.Convert(reference.At(x, y)).(color.NRGBA64))
				}
			}
			if err := jpeg.Encode(&want, rgb, &jpeg.Options{Quality: 90}); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(jpg, want.Bytes()) {
				t.Fatal("default preview reapplied XMP/crop/orientation")
			}
		})
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, root)) {
		t.Fatal("reading native previews changed sources/cache")
	}
}

func TestLRTPreviewEVWritesOriginalXMP(t *testing.T) {
	root := t.TempDir()
	frame, data := nativePreviewFixture(t, root)
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	want, err := framePreviewEV(img)
	if err != nil {
		t.Fatal(err)
	}
	before := batchFileSnapshot(t, root)
	var out, stderr bytes.Buffer
	if Main([]string{"preview-ev", root, "--lrt-preview", "visual", "--write"}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	packet, err := PacketForFrame(frame)
	if err != nil {
		t.Fatal(err)
	}
	value, found, err := packet.GetScalar("lrt:ExternalExposureDefault")
	if err != nil || !found || value != formatNumber(want*4) {
		t.Fatalf("wrong cache measurement: %s %v", value, err)
	}
	backups, err := filepath.Glob(frame.Sidecar + "*.bak")
	if err != nil || len(backups) != 1 {
		t.Fatal("missing source XMP backup")
	}
	assertPreviewEVFileBytes(t, backups[0], before[frame.Sidecar])
	for path, data := range before {
		if path != frame.Sidecar {
			assertPreviewEVFileBytes(t, path, data)
		}
	}
	if !strings.Contains(out.String(), "frame.001.NEF") {
		t.Fatal("measurement logs lost original source name")
	}
}

func TestLRTPreviewFeedsPreviewAndLongTermCLIs(t *testing.T) {
	root := t.TempDir()
	cacheData, err := os.ReadFile("testdata/lrt-native-preview-7.5.5.jpg")
	if err != nil {
		t.Fatal(err)
	}
	for index := 1; index <= 3; index++ {
		path := filepath.Join(root, fmt.Sprintf("frame%03d.NEF", index))
		if err := os.WriteFile(path, []byte("not a decodable RAW; select the LRT preview cache"), 0600); err != nil {
			t.Fatal(err)
		}
		writeTestXMP(t, path, map[string]string{})
		cache := lrtPreviewPath(path, "visual")
		if err := os.MkdirAll(filepath.Dir(cache), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(cache, cacheData, 0600); err != nil {
			t.Fatal(err)
		}
	}
	before := batchFileSnapshot(t, root)
	previewPath := filepath.Join(t.TempDir(), "preview.svg")
	manifestPath := filepath.Join(t.TempDir(), "selection.json")
	var out, stderr bytes.Buffer
	if code := Main([]string{"preview", root, "--output", previewPath, "--lrt-preview", "visual"}, &out, &stderr); code != 0 {
		t.Fatalf("cache-only preview exit=%d stderr=%s", code, stderr.String())
	}
	if data, err := os.ReadFile(previewPath); err != nil || !bytes.Contains(data, []byte("<svg")) {
		t.Fatalf("cache-only preview output missing or invalid: %v", err)
	}
	out.Reset()
	stderr.Reset()
	if code := Main([]string{"long-term", root, "--output", manifestPath, "--metric-profile", "lrt-7.5.5", "--lrt-preview", "visual"}, &out, &stderr); code != 0 {
		t.Fatalf("cache-only long-term analysis exit=%d stderr=%s", code, stderr.String())
	}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest sequenceSelectionManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Frames) != 3 || manifest.Frames[0].LRTMetrics == nil || manifest.Frames[0].LRTMetrics.LuminanceMean <= 0 {
		t.Fatalf("cache-only long-term metrics missing or invalid: %#v", manifest.Frames)
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, root)) {
		t.Fatal("preview analysis changed RAW inputs, XMP or LRT cache")
	}

	if code := Main([]string{"preview", root, "--output", filepath.Join(t.TempDir(), "conflict.svg"), "--lrt-preview", "visual", "--raw-preview"}, &out, &stderr); code == 0 {
		t.Fatal("preview accepted two mutually exclusive image input modes")
	}
	missingCache := lrtPreviewPath(filepath.Join(root, "frame003.NEF"), "visual")
	if err := os.Remove(missingCache); err != nil {
		t.Fatal(err)
	}
	beforeMissing := batchFileSnapshot(t, root)
	missingManifest := filepath.Join(t.TempDir(), "must-not-exist.json")
	out.Reset()
	stderr.Reset()
	if code := Main([]string{"long-term", root, "--output", missingManifest, "--lrt-preview", "visual"}, &out, &stderr); code == 0 {
		t.Fatal("long-term silently fell back when selected preview cache was missing")
	}
	if _, err := os.Stat(missingManifest); !os.IsNotExist(err) {
		t.Fatalf("failed cache analysis published a manifest: %v", err)
	}
	if !reflect.DeepEqual(beforeMissing, batchFileSnapshot(t, root)) {
		t.Fatal("failed cache analysis changed source files")
	}
}

func TestLRTPreviewInvalidAndMissing(t *testing.T) {
	for _, options := range []imageInputOptions{
		{LRTPreview: "auto"}, {LRTPreview: "visual", RawPreview: true}, {LRTPreview: "camera", RawDevelop: true},
		{LRTPreview: "visual", DNGLabPath: "dnglab"}, {LRTPreview: "visual", DNGConverterPath: "converter"},
	} {
		if err := validateImageInputOptions(options); err == nil {
			t.Fatalf("accepted invalid mode: %+v", options)
		}
	}
	for _, kind := range []string{"camera", "visual"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			frame, _ := nativePreviewFixture(t, root)
			if err := os.Remove(lrtPreviewPath(frame.Image, kind)); err != nil {
				t.Fatal(err)
			}
			before := batchFileSnapshot(t, root)
			output := filepath.Join(t.TempDir(), "previews")
			var out, stderr bytes.Buffer
			if Main([]string{"preview-images", root, "--output", output, "--lrt-preview", kind, "--write-preview-ev"}, &out, &stderr) == 0 || !strings.Contains(stderr.String(), ".lrtpreview") {
				t.Fatal("missing cache did not fail with cache path")
			}
			if _, err := os.Stat(output); !os.IsNotExist(err) {
				t.Fatal("missing cache published output")
			}
			if !reflect.DeepEqual(before, batchFileSnapshot(t, root)) {
				t.Fatal("missing cache modified sources")
			}
		})
	}
}

func TestLRTPreviewBatchGeneration(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a", "b"} {
		nativePreviewFixture(t, filepath.Join(root, name))
	}
	before := batchFileSnapshot(t, root)
	output := filepath.Join(t.TempDir(), "previews")
	var out, stderr bytes.Buffer
	if Main([]string{"batch-preview-images", root, "--output", output, "--workers", "2", "--", "--lrt-preview", "visual"}, &out, &stderr) != 0 {
		t.Fatal(&stderr)
	}
	manifest := readBatchPreviewManifest(t, output)
	if len(manifest.Sequences) != 2 {
		t.Fatal("missing native preview sequences")
	}
	for _, entry := range manifest.Sequences {
		individual := filepath.Join(t.TempDir(), "previews")
		if Main([]string{"preview-images", filepath.Join(root, entry.Source), "--output", individual, "--lrt-preview", "visual"}, &out, &stderr) != 0 {
			t.Fatal(&stderr)
		}
		for relative, data := range relativeBatchPreviewFiles(t, individual) {
			assertPreviewEVFileBytes(t, filepath.Join(output, entry.Directory, relative), data)
		}
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, root)) {
		t.Fatal("batch preview changed source or native caches")
	}
	for _, command := range []string{"deflicker", "batch-deflicker"} {
		dir := root
		if command == "deflicker" {
			dir = filepath.Join(root, "a")
		}
		stderr.Reset()
		if Main([]string{command, dir, "--lrt-preview", "visual", "--reset", "--write"}, &out, &stderr) == 0 || !strings.Contains(stderr.String(), "--reset cannot be combined") {
			t.Fatal("native cache option was ignored with --reset")
		}
	}
}

func TestLRTPreviewDeflickerMatchesDecodedSequence(t *testing.T) {
	for _, batch := range []bool{false, true} {
		t.Run(fmt.Sprint("batch=", batch), func(t *testing.T) {
			root := t.TempDir()
			references := map[string]string{}
			for _, name := range []string{"a", "b"} {
				dir := filepath.Join(root, name)
				first, _ := nativePreviewFixture(t, dir)
				originalXMP, err := os.ReadFile(first.Sidecar)
				if err != nil {
					t.Fatal(err)
				}
				ref := t.TempDir()
				references[dir] = ref
				for i, v := range []uint8{90, 150, 110} {
					stem := fmt.Sprintf("frame.%03d", i+1)
					path := filepath.Join(dir, stem+".NEF")
					if err := os.WriteFile(path, []byte("use cached image"), 0600); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path+".xmp", originalXMP, 0600); err != nil {
						t.Fatal(err)
					}
					img := image.NewNRGBA(image.Rect(0, 0, 32, 16))
					for y := 0; y < 16; y++ {
						for x := 0; x < 32; x++ {
							img.SetNRGBA(x, y, color.NRGBA{v, v, v, 255})
						}
					}
					var jpegData bytes.Buffer
					if err := jpeg.Encode(&jpegData, img, &jpeg.Options{Quality: 90}); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(lrtPreviewPath(path, "visual"), jpegData.Bytes(), 0600); err != nil {
						t.Fatal(err)
					}
					refPath := filepath.Join(ref, stem+".jpg")
					if err := os.WriteFile(refPath, jpegData.Bytes(), 0600); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(refPath+".xmp", originalXMP, 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			before := batchFileSnapshot(t, root)
			var out, stderr bytes.Buffer
			options := []string{"--constant", "--passes", "2", "--smoother", "lowess", "--write"}
			if batch {
				if Main(append([]string{"batch-deflicker", root, "--workers", "2", "--lrt-preview", "visual"}, options...), &out, &stderr) != 0 {
					t.Fatal(&stderr)
				}
			}
			for dir, ref := range references {
				if !batch {
					if Main(append([]string{"deflicker", dir, "--lrt-preview", "visual"}, options...), &out, &stderr) != 0 {
						t.Fatal(&stderr)
					}
				}
				if Main(append([]string{"deflicker", ref}, options...), &out, &stderr) != 0 {
					t.Fatal(&stderr)
				}
				for i := 1; i <= 3; i++ {
					stem := fmt.Sprintf("frame.%03d", i)
					want, err := os.ReadFile(filepath.Join(ref, stem+".jpg.xmp"))
					if err != nil {
						t.Fatal(err)
					}
					assertPreviewEVFileBytes(t, filepath.Join(dir, stem+".NEF.xmp"), want)
				}
			}
			for path, data := range before {
				if !strings.HasSuffix(path, ".xmp") {
					assertPreviewEVFileBytes(t, path, data)
				}
			}
			if !strings.Contains(out.String(), "Updated") && !strings.Contains(out.String(), "Saved:") {
				t.Fatal("no corrections saved")
			}
		})
	}
}
