package lrtl

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
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
