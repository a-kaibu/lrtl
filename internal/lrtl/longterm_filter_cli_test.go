package lrtl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLRTLongTermPresetCLIAndVideo(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 12; i++ {
		img := image.NewGray(image.Rect(0, 0, 64, 64))
		level := byte(128)
		if i == 3 || i == 9 {
			level = 255
		}
		for p := range img.Pix {
			img.Pix[p] = level
		}
		path := filepath.Join(dir, fmt.Sprintf("frame%02d.png", i))
		file, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		err = png.Encode(file, img)
		closeErr := file.Close()
		if err != nil || closeErr != nil {
			t.Fatal(err, closeErr)
		}
		date := "2026-10-19T09:00:00"
		if i == 4 {
			date = "2026-10-20T09:00:00"
		}
		writeTestXMP(t, path, map[string]string{"exif:DateTimeOriginal": date, "dc:description": "preserve source"})
	}
	before := batchFileSnapshot(t, dir)
	p := defaultLRTLongTermPreset()
	p.LumConst = true
	p.LumHigh = 50
	p.ContrastConst = true
	p.HueConst = true
	p.DayTue = false
	p.DayWed = false
	p.DayThu = false
	p.DayFri = false
	p.DaySat = false
	p.DaySun = false
	settings := writeLongTermPreset(t, map[string]any{"filterPresets": map[string]any{"default": p, "all": defaultLRTLongTermPreset()}, "filterContrastAndHueActive": true})
	output := filepath.Join(t.TempDir(), "filtered.json")
	markerCommand(t, "long-term", dir, "--lrt-filter-preset", settings, "--output", output)
	var manifest sequenceSelectionManifest
	if err := json.Unmarshal(denoiseRead(t, output), &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.MetricProfile != longTermMetricProfileLRT755 || manifest.LRTFilter == nil || !manifest.LRTFilter.ColorActive || !manifest.LRTFilter.TimeApplied || manifest.LRTFilter.Name != "default" {
		t.Fatal("native preset provenance is missing", manifest.LRTFilter)
	}
	if len(manifest.Frames) != 12 {
		t.Fatal("wrong manifest length")
	}
	for i, frame := range manifest.Frames {
		want := i != 3 && i != 4 && i != 9
		if frame.Included != want {
			t.Fatalf("frame%d selected%v want%v", i, frame.Included, want)
		}
		if frame.LRTFilter == nil {
			t.Fatal("missing decision")
		}
		if i == 4 {
			if frame.LRTFilter.ExcludedBy != "time" || frame.LRTFilter.LuminanceTarget != nil {
				t.Fatal("time filtering did not precede curve construction")
			}
		} else if frame.LRTFilter.LuminanceTarget == nil {
			t.Fatal("missing luminance target")
		}
		if want && (frame.LRTFilter.ContrastTarget == nil || frame.LRTFilter.HueTargetDegrees == nil) {
			t.Fatal("saved contrast/hue stages not applied")
		}
	}
	video := filepath.Join(t.TempDir(), "filtered.avi")
	markerCommand(t, "render", dir, "--selection", output, "--output-height", "32", "--output", video)
	if frames := bytes.Count(denoiseRead(t, video), []byte{255, 216}); frames != 9 {
		t.Fatalf("filtered AVI has%d frames want9", frames)
	}
	allOutput := filepath.Join(t.TempDir(), "all.json")
	markerCommand(t, "long-term", dir, "--lrt-filter-preset", settings, "--lrt-filter-preset-name", "all", "--lrt-filter-color", "off", "--output", allOutput)
	if err := json.Unmarshal(denoiseRead(t, allOutput), &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.LRTFilter.ColorActive || manifest.LRTFilter.TimeApplied {
		t.Fatal("native overrides/default date shortcut ignored")
	}
	for _, frame := range manifest.Frames {
		if !frame.Included {
			t.Fatal("inactive preset excluded a frame")
		}
	}
	standard := filepath.Join(t.TempDir(), "standard.json")
	markerCommand(t, "long-term", dir, "--output", standard)
	manifest = sequenceSelectionManifest{}
	if err := json.Unmarshal(denoiseRead(t, standard), &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.LRTFilter != nil || manifest.MetricProfile != longTermMetricProfileLRTL {
		t.Fatal("ordinary profile changed")
	}
	if !reflect.DeepEqual(before, batchFileSnapshot(t, dir)) {
		t.Fatal("filtering changed source images or XMP")
	}
	// A missing timestamp disables only the time stage, matching the native
	// sequence availability gate; the following metric stages still execute.
	firstImage := filepath.Join(dir, "frame00.png")
	writeTestXMP(t, firstImage, map[string]string{"exif:DateTimeOriginal": "n/a"})
	missingOutput := filepath.Join(t.TempDir(), "missing-time.json")
	markerCommand(t, "long-term", dir, "--lrt-filter-preset", settings, "--output", missingOutput)
	manifest = sequenceSelectionManifest{}
	if err := json.Unmarshal(denoiseRead(t, missingOutput), &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.LRTFilter.TimeApplied || manifest.LRTFilter.TimeSkipReason == "" || !manifest.Frames[4].Included || manifest.Frames[3].Included || manifest.Frames[9].Included {
		t.Fatal("time availability changed later stages")
	}
	if err := os.WriteFile(firstImage+".xmp", before[firstImage+".xmp"], 0600); err != nil {
		t.Fatal(err)
	}
	for _, flags := range [][]string{{"--lrt-filter-preset-name", "missing"}, {"--lrt-filter-color", "on"}, {"--lrt-filter-preset", settings, "--metric-profile", "lrtl"}, {"--lrt-filter-preset", settings, "--lrt-filter-color", "invalid"}, {"--lrt-filter-preset", settings, "--lrt-filter-preset-name", "missing"}} {
		args := append([]string{"long-term", dir, "--output", output, "--overwrite"}, flags...)
		var stdout, stderr bytes.Buffer
		old := denoiseRead(t, output)
		if Main(args, &stdout, &stderr) == 0 {
			t.Fatal("accepted invalid filter options", flags)
		}
		assertPreviewEVFileBytes(t, output, old)
	}
	lateSidecar := filepath.Join(dir, "frame11.png.xmp")
	if err := os.WriteFile(lateSidecar, []byte("<broken XML"), 0600); err != nil {
		t.Fatal(err)
	}
	var failedOut, failedErr bytes.Buffer
	oldOutput := denoiseRead(t, output)
	if Main([]string{"long-term", dir, "--lrt-filter-preset", settings, "--output", output, "--overwrite"}, &failedOut, &failedErr) == 0 {
		t.Fatal("accepted malformed consumed date metadata")
	}
	assertPreviewEVFileBytes(t, output, oldOutput)
	if err := os.WriteFile(lateSidecar, before[lateSidecar], 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "frame99.png"), []byte("corrupt late frame"), 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	old := denoiseRead(t, output)
	if Main([]string{"long-term", dir, "--lrt-filter-preset", settings, "--output", output, "--overwrite"}, &stdout, &stderr) == 0 {
		t.Fatal("published a partial native selection")
	}
	assertPreviewEVFileBytes(t, output, old)
	stdout.Reset()
	stderr.Reset()
	if Main([]string{"long-term", "--help"}, &stdout, &stderr) != 0 || !strings.Contains(stdout.String(), "--lrt-filter-preset") {
		t.Fatal("new filter flags missing from help", stdout.String(), stderr.String())
	}
}

func FuzzLRTLongTermFilter(f *testing.F) {
	f.Add(uint8(12), uint8(5), uint8(75), uint8(50), true, true)
	f.Fuzz(func(t *testing.T, count, smoothing, low, high uint8, constant, color bool) {
		n := int(count%48) + 1
		m, frames := syntheticLongTermFilterManifest(n)
		for i := range m.Frames {
			m.Frames[i].LRTMetrics.LuminanceMean = float64((i*37+int(low))%256) / 256
			m.Frames[i].LRTMetrics.ContrastStdDev = float64((i*17+int(high))%255) / 255
			*m.Frames[i].LRTMetrics.HueMeanDegrees = float64((i*53 + int(smoothing)) % 360)
		}
		p := defaultLRTLongTermPreset()
		p.LumSmoothing = int(smoothing % 51)
		p.LumLow = int(low % 101)
		p.LumHigh = int(high % 101)
		p.LumConst = constant
		p.ContrastSmoothing = int(smoothing % 51)
		p.HueConst = constant
		config := &lrtLongTermSavedFilter{Preset: p, ColorActive: color}
		input, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		if err := applyLRTLongTermFilter(m, frames, config); err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(m)
		if err != nil || bytes.Contains(data, []byte("NaN")) {
			t.Fatal("nonfinite output", err)
		}
		var again sequenceSelectionManifest
		if err := json.Unmarshal(input, &again); err != nil {
			t.Fatal(err)
		}
		if err := applyLRTLongTermFilter(&again, frames, config); err != nil {
			t.Fatal(err)
		}
		repeated, err := json.Marshal(again)
		if err != nil || !bytes.Equal(data, repeated) {
			t.Fatal("nondeterministic saved filter", err)
		}
	})
}
