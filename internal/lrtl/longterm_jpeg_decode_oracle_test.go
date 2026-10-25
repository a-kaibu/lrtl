package lrtl

import (
	"bytes"
	"encoding/json"
	"image"
	"math"
	"os"
	"path/filepath"
	"testing"
)

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
