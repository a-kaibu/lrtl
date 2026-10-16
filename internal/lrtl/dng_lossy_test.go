package lrtl

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestAdobeLossyDNGImportCommandAndPreservation(t *testing.T) {
	for _, extension := range []string{".nef", ".dng"} {
		t.Run(extension, func(t *testing.T) {
			executable, root, data := fakeDNGConverter(t)
			source := filepath.Join(root, "input with spaces")
			if err := os.Mkdir(source, 0700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(source, "-frame name"+extension)
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			writeTestXMP(t, path, map[string]string{"crs:Exposure2012": ".5", "crs:CameraProfile": "keep"})
			before := batchFileSnapshot(t, source)
			output := filepath.Join(root, "output")
			args := []string{"import", source, "--output", output, "--convert-dng", "--dng-compression", "LoSsY", "--dng-long-edge", "16", "--dng-converter", executable}
			var stdout, stderr bytes.Buffer
			if Main(append(append([]string{}, args...), "--dry-run"), &stdout, &stderr) != 0 {
				t.Fatal(&stderr)
			}
			if _, err := os.Stat(output); !os.IsNotExist(err) {
				t.Fatal("dry-run created output")
			}
			if _, err := os.Stat(os.Getenv("LRTL_TEST_CONVERTER_LOG")); !os.IsNotExist(err) {
				t.Fatal("dry-run launched converter")
			}
			if Main(args, &stdout, &stderr) != 0 {
				t.Fatal(&stderr)
			}
			log, err := os.ReadFile(os.Getenv("LRTL_TEST_CONVERTER_LOG"))
			if err != nil {
				t.Fatal(err)
			}
			var argv []string
			if err := json.Unmarshal(log, &argv); err != nil {
				t.Fatal(err)
			}
			if len(argv) != 10 || !reflect.DeepEqual(argv[:5], []string{"-lossy", "-p1", "-dng1.4", "-side", "16"}) || argv[len(argv)-1] != path {
				t.Fatalf("invalid lossy/resize command: %q", argv)
			}
			assertPreviewEVFileBytes(t, filepath.Join(output, "-frame name.dng"), data)
			assertPreviewEVFileBytes(t, filepath.Join(output, "-frame name.dng.xmp"), before[path+".xmp"])
			if !reflect.DeepEqual(before, batchFileSnapshot(t, source)) {
				t.Fatal("lossy import changed sources")
			}
		})
	}
}

func TestAdobeLossyDNGValidationAndRollback(t *testing.T) {
	executable, root, data := fakeDNGConverter(t)
	source := filepath.Join(root, "input")
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"frame01.nef", "frame02.nef"} {
		if err := os.WriteFile(filepath.Join(source, name), data, 0600); err != nil {
			t.Fatal(err)
		}
		writeTestXMP(t, filepath.Join(source, name), map[string]string{"crs:Exposure2012": ".5"})
	}
	before := batchFileSnapshot(t, source)
	for _, flags := range [][]string{
		{"--dng-long-edge", "16"}, {"--convert-dng", "--dng-long-edge", "16"},
		{"--convert-dng", "--dng-compression", "lossy", "--dng-long-edge", "-1"},
		{"--convert-dng", "--dng-compression", "lossy", "--dng-long-edge", "32769"},
		{"--convert-dng", "--dng-compression", "lossy", "--dnglab", executable},
	} {
		output := filepath.Join(t.TempDir(), "out")
		var stdout, stderr bytes.Buffer
		if Main(append([]string{"import", source, "--output", output}, flags...), &stdout, &stderr) == 0 {
			t.Fatalf("invalid options accepted: %v", flags)
		}
		if _, err := os.Stat(output); !os.IsNotExist(err) {
			t.Fatal("invalid options created destination")
		}
	}
	for _, mode := range []string{"ignored-size", "failed-second", "invalid-output", "missing-output"} {
		t.Run(mode, func(t *testing.T) {
			output := t.TempDir()
			sentinel := filepath.Join(output, "keep.txt")
			if err := os.WriteFile(sentinel, []byte("keep"), 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("LRTL_TEST_CONVERTER_HELPER", "valid")
			t.Setenv("LRTL_TEST_CONVERTER_FAIL_NAME", "")
			side := "16"
			switch mode {
			case "ignored-size":
				side = "8" // Synthetic converter always writes 16x16.
			case "failed-second":
				t.Setenv("LRTL_TEST_CONVERTER_FAIL_NAME", "frame02.nef")
			case "invalid-output":
				t.Setenv("LRTL_TEST_CONVERTER_HELPER", "invalid")
			case "missing-output":
				t.Setenv("LRTL_TEST_CONVERTER_HELPER", "missing")
			}
			var stdout, stderr bytes.Buffer
			if Main([]string{"import", source, "--output", output, "--convert-dng", "--dng-compression", "lossy", "--dng-long-edge", side, "--dng-converter", executable}, &stdout, &stderr) == 0 {
				t.Fatal("converter failure was ignored")
			}
			entries, err := os.ReadDir(output)
			if err != nil || len(entries) != 1 || entries[0].Name() != "keep.txt" {
				t.Fatalf("failed conversion left files: %v %v", entries, err)
			}
			assertPreviewEVFileBytes(t, sentinel, []byte("keep"))
		})
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, source)) {
		t.Fatal("failed lossy import changed originals")
	}
}

func TestAdobeLossyDNGRealRAW(t *testing.T) {
	source := os.Getenv("LRTL_RAW_SAMPLE")
	if source == "" {
		t.Skip("set LRTL_RAW_SAMPLE to exercise installed Adobe lossy conversion and resize")
	}
	original, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	input := filepath.Join(root, "input")
	if err := os.Mkdir(input, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(input, "frame"+strings.ToLower(filepath.Ext(source)))
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	writeTestXMP(t, path, map[string]string{"crs:Exposure2012": ".5"})
	before := batchFileSnapshot(t, input)
	fullProxy := ""
	for _, side := range []int{0, 256} {
		output := filepath.Join(t.TempDir(), "out")
		var stdout bytes.Buffer
		if err := ImportSequence(input, output, ImportOptions{ConvertToDNG: true, DNGCompression: "lossy", DNGLongEdge: side, DNGConverterPath: os.Getenv("LRTL_DNG_CONVERTER")}, &stdout); err != nil {
			t.Fatal(err)
		}
		converted := filepath.Join(output, "frame.dng")
		data, err := os.ReadFile(converted)
		if err != nil {
			t.Fatal(err)
		}
		fields, err := findLinearDNG(data)
		if err != nil {
			t.Fatal(err)
		}
		width, height, compression := fields.integer(256, 0), fields.integer(257, 0), fields.integer(259, 0)
		if fields.err != nil || width < 1 || height < 1 || compression != 34892 || (side > 0 && max(width, height) > side) {
			t.Fatalf("real lossy DNG dimensions=%dx%d compression=%d error=%v", width, height, compression, fields.err)
		}
		t.Logf("lossy DNG limit=%d actual=%dx%d compression=%d bytes=%d", side, width, height, compression, len(data))
		if side == 0 {
			fullProxy = output
		}
		assertPreviewEVFileBytes(t, converted+".xmp", before[path+".xmp"])
		if side > 0 {
			exported := filepath.Join(t.TempDir(), "tiff")
			if err := ExportTIFFSequence([]Frame{{Image: converted, Sidecar: converted + ".xmp"}}, exported, RenderOptions{RawDevelop: true, ApplyXMPExposure: true, DNGConverterPath: os.Getenv("LRTL_DNG_CONVERTER")}); err != nil {
				t.Fatal(err)
			}
			img := readExportedTIFF(t, filepath.Join(exported, "frame000001.tif"))
			if max(img.Bounds().Dx(), img.Bounds().Dy()) > side {
				t.Fatal("resized proxy grew during development")
			}
			var stderr bytes.Buffer
			if Main([]string{"render", exported, "--output", filepath.Join(t.TempDir(), "proxy.avi")}, &stdout, &stderr) != 0 {
				t.Fatal(&stderr)
			}
		}
	}
	// Existing DNG input must be reconverted when lossy conversion is explicit.
	// Ordinary default/lossless imports continue to copy existing DNG files.
	proxyBefore := batchFileSnapshot(t, fullProxy)
	resizedProxy := filepath.Join(t.TempDir(), "existing-dng-resized")
	var proxyOut bytes.Buffer
	if err := ImportSequence(fullProxy, resizedProxy, ImportOptions{ConvertToDNG: true, DNGCompression: "lossy", DNGLongEdge: 256, DNGConverterPath: os.Getenv("LRTL_DNG_CONVERTER")}, &proxyOut); err != nil {
		t.Fatal(err)
	}
	proxyData, err := os.ReadFile(filepath.Join(resizedProxy, "frame.dng"))
	if err != nil {
		t.Fatal(err)
	}
	proxyFields, err := findLinearDNG(proxyData)
	if err != nil {
		t.Fatal(err)
	}
	if max(proxyFields.integer(256, 0), proxyFields.integer(257, 0)) > 256 || proxyFields.err != nil {
		t.Fatal("existing DNG was not resized")
	}
	if !reflect.DeepEqual(proxyBefore, batchFileSnapshot(t, fullProxy)) {
		t.Fatal("existing DNG input was changed")
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, input)) {
		t.Fatal("real lossy conversion changed source RAW/XMP")
	}
	after, err := os.ReadFile(source)
	if err != nil || !bytes.Equal(original, after) {
		t.Fatal("real RAW sample changed")
	}
}
