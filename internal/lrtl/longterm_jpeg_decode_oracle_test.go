package lrtl

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"
)

type longTermCameraCacheOracle struct {
	SelectedCache string  `json:"selectedCache"`
	Width         int     `json:"width"`
	Height        int     `json:"height"`
	RGBSHA256     string  `json:"rgbSha256"`
	Luminance     float64 `json:"luminance"`
	Contrast      float64 `json:"contrast"`
	Hue           float64 `json:"hue"`
}

type longTermJPEGDecodeCase struct {
	File      string  `json:"file"`
	Width     int     `json:"width"`
	Height    int     `json:"height"`
	Luminance float64 `json:"luminance"`
	Contrast  float64 `json:"contrast"`
	Hue       float64 `json:"hue"`
}

func TestLRTLongTermNativeJPEGDecodedMetrics(t *testing.T) {
	var fixture struct {
		Version int                      `json:"version"`
		Cases   []longTermJPEGDecodeCase `json:"cases"`
	}
	if err := json.Unmarshal(denoiseRead(t, "testdata/lrt-longterm-jpeg-7.5.5.json"), &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Version != 1 || len(fixture.Cases) != 28 {
		t.Fatalf("expected 28 native decoded JPEG cases: version=%d cases=%d", fixture.Version, len(fixture.Cases))
	}
	maxStandardDelta, maxMeteringDelta := [3]float64{}, [3]float64{}
	standardDecodeFailures := 0
	for _, row := range fixture.Cases {
		t.Run(row.File, func(t *testing.T) {
			data := denoiseRead(t, "../nativejpeg/testdata/"+row.File)
			standard, _, err := image.Decode(bytes.NewReader(data))
			if err != nil {
				standardDecodeFailures++
			}
			meteringDecoded, format, err := decodeLRTLongTermImage(data)
			if err != nil || format != "jpeg" {
				t.Fatal("LRT-compatible long-term JPEG decoder", format, err)
			}
			if meteringDecoded.Bounds().Dx() != row.Width || meteringDecoded.Bounds().Dy() != row.Height {
				t.Fatal("decoded JPEG dimensions changed", meteringDecoded.Bounds())
			}
			previewDecoded, previewFormat, previewErr := decodeLRTMeteringImage(data)
			if previewErr != nil || previewFormat != "jpeg" || previewDecoded.Bounds() != meteringDecoded.Bounds() {
				t.Fatal("Preview EV JPEG decoder failed", previewFormat, previewErr)
			}
			want := [3]float64{row.Luminance, row.Contrast, row.Hue}
			meteringMetrics := measureLRTLongTermFrame(meteringDecoded)
			gotMetering := [3]float64{meteringMetrics.LuminanceMean, meteringMetrics.ContrastStdDev, *meteringMetrics.HueMeanDegrees}
			for axis := range want {
				if math.IsNaN(gotMetering[axis]) || math.IsInf(gotMetering[axis], 0) {
					t.Fatal("non-finite metric", gotMetering)
				}
				maxMeteringDelta[axis] = math.Max(maxMeteringDelta[axis], math.Abs(gotMetering[axis]-want[axis]))
			}
			if standard != nil && standard.Bounds() == meteringDecoded.Bounds() {
				standardMetrics := measureLRTLongTermFrame(standard)
				gotStandard := [3]float64{standardMetrics.LuminanceMean, standardMetrics.ContrastStdDev, *standardMetrics.HueMeanDegrees}
				for axis := range want {
					maxStandardDelta[axis] = math.Max(maxStandardDelta[axis], math.Abs(gotStandard[axis]-want[axis]))
				}
			}
			inputDir := t.TempDir()
			inputPath := filepath.Join(inputDir, "frame00.jpg")
			if err := os.WriteFile(inputPath, data, 0600); err != nil {
				t.Fatal(err)
			}
			outputPath := filepath.Join(t.TempDir(), "metrics.json")
			markerCommand(t, "long-term", inputDir, "--metric-profile", "lrt-7.5.5", "--output", outputPath)
			var manifest sequenceSelectionManifest
			if err := json.Unmarshal(denoiseRead(t, outputPath), &manifest); err != nil || len(manifest.Frames) != 1 || manifest.Frames[0].LRTMetrics == nil {
				t.Fatal("long-term CLI failed to measure encoded JPEG", err)
			}
			cli := manifest.Frames[0].LRTMetrics
			if math.Abs(cli.LuminanceMean-row.Luminance) > 0.004 || math.Abs(cli.ContrastStdDev-row.Contrast) > 0.0016 || math.Abs(*cli.HueMeanDegrees-row.Hue) > 0.55 {
				t.Fatalf("long-term CLI metric delta exceeds oracle bounds: file=%s got=(%.12g,%.12g,%.12g) want=(%.12g,%.12g,%.12g)",
					row.File, cli.LuminanceMean, cli.ContrastStdDev, *cli.HueMeanDegrees, row.Luminance, row.Contrast, row.Hue)
			}
		})
	}
	if maxMeteringDelta[0] > 0.004 || maxMeteringDelta[1] > 0.0016 || maxMeteringDelta[2] > 0.55 {
		t.Fatalf("LRT-compatible decoder maximum metric delta %g/%g/%g exceeds 0.004/0.0016/0.55", maxMeteringDelta[0], maxMeteringDelta[1], maxMeteringDelta[2])
	}
	t.Logf("maximum metric delta from LRT: Go image.Decode (failures=%d)=%g/%g/%g, LRT-compatible long-term JPEG decoder=%g/%g/%g",
		standardDecodeFailures,
		maxStandardDelta[0], maxStandardDelta[1], maxStandardDelta[2],
		maxMeteringDelta[0], maxMeteringDelta[1], maxMeteringDelta[2])
}

func TestLRTLongTermCameraCacheMetricsMatchNativeWorker(t *testing.T) {
	var oracle longTermCameraCacheOracle
	if err := json.Unmarshal(denoiseRead(t, "testdata/lrt-longterm-camera-cache-7.5.5.json"), &oracle); err != nil {
		t.Fatal(err)
	}
	if oracle.SelectedCache != "camera" || oracle.Width != 192 || oracle.Height != 96 || oracle.RGBSHA256 == "" {
		t.Fatalf("invalid LRT camera-cache worker oracle: %+v", oracle)
	}
	data := denoiseRead(t, "testdata/lrt-longterm-camera-cache-7.5.5.jpg")
	decoded, _, err := decodeLRTLongTermImage(data)
	if err != nil {
		t.Fatal(err)
	}
	if got := longTermRGBSHA256(decoded); got != oracle.RGBSHA256 {
		t.Fatalf("decoded camera-cache pixels differ from native worker: got %s want %s", got, oracle.RGBSHA256)
	}
	metrics := measureLRTLongTermFrame(decoded)
	gotHue := math.NaN()
	if metrics.HueMeanDegrees != nil {
		gotHue = *metrics.HueMeanDegrees
	}
	if metrics.HueMeanDegrees == nil || math.Abs(metrics.LuminanceMean-oracle.Luminance) > 0.004 || math.Abs(metrics.ContrastStdDev-oracle.Contrast) > 0.0016 || math.Abs(gotHue-oracle.Hue) > 0.55 {
		t.Fatalf("camera-cache metrics exceed native worker bounds: got=(%.12g,%.12g,%.12g) want=(%.12g,%.12g,%.12g)",
			metrics.LuminanceMean, metrics.ContrastStdDev, gotHue, oracle.Luminance, oracle.Contrast, oracle.Hue)
	}
}

func longTermRGBSHA256(img image.Image) string {
	digest := sha256.New()
	bounds := img.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			_, _ = digest.Write([]byte{byte(r >> 8), byte(g >> 8), byte(b >> 8)})
		}
	}
	return hex.EncodeToString(digest.Sum(nil))
}

func TestLRTLongTermAutomaticallyUsesCameraPreviewCache(t *testing.T) {
	var fixture struct {
		Cases []longTermJPEGDecodeCase `json:"cases"`
	}
	if err := json.Unmarshal(denoiseRead(t, "testdata/lrt-longterm-jpeg-7.5.5.json"), &fixture); err != nil || len(fixture.Cases) < 5 {
		t.Fatalf("load native JPEG cases: %v", err)
	}
	inputDir := t.TempDir()
	inputPath := filepath.Join(inputDir, "frame.png")
	source := longTermMetricSource(64, 64, 0, false)
	file, err := os.Create(inputPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(file, source); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	cacheData := make([][]byte, 2)
	cacheMetrics := make([]lrtLongTermMetrics, 2)
	cacheData[0] = denoiseRead(t, "testdata/lrt-longterm-camera-cache-7.5.5.jpg")
	cacheData[1] = denoiseRead(t, "../nativejpeg/testdata/"+fixture.Cases[4].File)
	for index := range cacheData {
		decoded, _, err := decodeLRTLongTermImage(cacheData[index])
		if err != nil {
			t.Fatal(err)
		}
		cacheMetrics[index] = measureLRTLongTermFrame(decoded)
	}
	if math.Abs(cacheMetrics[0].LuminanceMean-cacheMetrics[1].LuminanceMean) < 1e-5 {
		t.Fatal("camera and visual cache fixtures do not distinguish their luminance")
	}
	truncatedCache := cacheData[0][:len(cacheData[0])/2]
	if _, format, err := image.DecodeConfig(bytes.NewReader(truncatedCache)); err != nil || format != "jpeg" {
		t.Fatalf("truncated-cache fixture must pass header decoding as JPEG: format=%q err=%v", format, err)
	}
	sourceMetrics := measureLRTLongTermFrame(source)
	for index, metrics := range cacheMetrics {
		if sameLRTLongTermMetrics(metrics, sourceMetrics) {
			t.Fatalf("source fallback fixture is indistinguishable from cache %d: %+v", index, metrics)
		}
	}

	for index, kind := range []string{"camera", "visual"} {
		cachePath := lrtPreviewPath(inputPath, kind)
		if err := os.MkdirAll(filepath.Dir(cachePath), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(cachePath, cacheData[index], 0600); err != nil {
			t.Fatal(err)
		}
	}

	measure := func(name string, args []string, want lrtLongTermMetrics) {
		t.Helper()
		output := filepath.Join(t.TempDir(), "selection.json")
		command := []string{"long-term", inputDir, "--metric-profile", "lrt-7.5.5", "--output", output}
		command = append(command, args...)
		markerCommand(t, command...)
		var manifest sequenceSelectionManifest
		if err := json.Unmarshal(denoiseRead(t, output), &manifest); err != nil || len(manifest.Frames) != 1 {
			t.Fatalf("%s: invalid manifest: %v", name, err)
		}
		got := manifest.Frames[0].LRTMetrics
		if manifest.MetricRevision != "lrt-7.5.5-camera-cache-preview-histogram-v3" || got == nil || math.Abs(got.LuminanceMean-want.LuminanceMean) > 1e-12 || math.Abs(got.ContrastStdDev-want.ContrastStdDev) > 1e-12 || got.HueMeanDegrees == nil || want.HueMeanDegrees == nil || math.Abs(*got.HueMeanDegrees-*want.HueMeanDegrees) > 1e-12 {
			t.Fatalf("%s: metrics=%+v want %+v revision=%q", name, got, want, manifest.MetricRevision)
		}
	}

	measure("automatic camera cache", nil, cacheMetrics[0])
	measure("explicit visual cache", []string{"--lrt-preview", "visual"}, cacheMetrics[1])
	cachePath := lrtPreviewPath(inputPath, "camera")
	for _, corruption := range []struct {
		name    string
		data    []byte
		missing bool
	}{
		{name: "invalid bytes", data: []byte("broken cache")},
		{name: "truncated JPEG", data: truncatedCache},
		{name: "empty file", data: []byte{}},
		{name: "missing file", missing: true},
	} {
		if corruption.missing {
			if err := os.Remove(cachePath); err != nil {
				t.Fatal(err)
			}
		} else if err := os.WriteFile(cachePath, corruption.data, 0600); err != nil {
			t.Fatal(err)
		}
		t.Logf("checking source fallback after %s camera cache", corruption.name)
		measure("source fallback for "+corruption.name+" automatic camera cache", nil, sourceMetrics)
	}
	if err := os.WriteFile(cachePath, truncatedCache, 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err := readLongTermInputWithOptions(
		Frame{Image: inputPath}, imageInputOptions{LRTPreview: "camera"}, longTermMetricProfileLRT755); err == nil {
		t.Fatal("explicit truncated camera cache silently fell back to the source image")
	}
}

func sameLRTLongTermMetrics(left, right lrtLongTermMetrics) bool {
	if math.Abs(left.LuminanceMean-right.LuminanceMean) > 1e-8 ||
		math.Abs(left.ContrastStdDev-right.ContrastStdDev) > 1e-8 {
		return false
	}
	if left.HueMeanDegrees == nil || right.HueMeanDegrees == nil {
		return left.HueMeanDegrees == nil && right.HueMeanDegrees == nil
	}
	return math.Abs(*left.HueMeanDegrees-*right.HueMeanDegrees) <= 1e-8
}
