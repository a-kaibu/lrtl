package lrtl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

const selectionManifestVersion = 2
const maxSelectionManifestSize = 128 << 20

type longTermMetricProfile string

const (
	longTermMetricProfileLRTL   longTermMetricProfile = "lrtl"
	longTermMetricProfileLRT755 longTermMetricProfile = "lrt-7.5.5"
)

type longTermFrameMetrics struct {
	Image            string                    `json:"image"`
	Included         bool                      `json:"included"`
	SizeBytes        int64                     `json:"size_bytes"`
	ModifiedUnixNano int64                     `json:"modified_unix_nano"`
	Luminance        float64                   `json:"median_luminance_linear_srgb,omitempty"`
	ContrastEV       float64                   `json:"contrast_p90_p10_ev,omitempty"`
	HueDegrees       *float64                  `json:"mean_hue_degrees,omitempty"`
	LRTMetrics       *lrtLongTermMetrics       `json:"lrt_metrics,omitempty"`
	LRTFilter        *lrtLongTermFrameDecision `json:"lrt_filter,omitempty"`
}

type lrtLongTermMetrics struct {
	LuminanceMean float64 `json:"luminance_mean_0_to_1"`
	// Historical JSON field name; the native display value is twice the deviation.
	ContrastStdDev float64  `json:"contrast_stddev_0_to_1"`
	HueMeanDegrees *float64 `json:"hue_mean_degrees,omitempty"`
}

type sequenceSelectionManifest struct {
	Version        int                     `json:"version"`
	Directory      string                  `json:"directory"`
	MetricProfile  longTermMetricProfile   `json:"metric_profile,omitempty"`
	MetricRevision string                  `json:"metric_revision,omitempty"`
	LRTFilter      *lrtLongTermSavedFilter `json:"lrt_filter,omitempty"`
	Filters        longTermRanges          `json:"filters"`
	Frames         []longTermFrameMetrics  `json:"frames"`
}

type metricRange struct {
	Minimum float64 `json:"minimum"`
	Maximum float64 `json:"maximum"`
}

type longTermRanges struct {
	Luminance *metricRange `json:"luminance_range,omitempty"`
	Contrast  *metricRange `json:"contrast_range,omitempty"`
	Hue       *metricRange `json:"hue_range,omitempty"`
}

func parseMetricRange(value, label string, minimumAllowed, maximumAllowed float64, allowWrap bool) (*metricRange, error) {
	parts := strings.Split(value, ",")
	if len(parts) != 2 {
		return nil, fmt.Errorf("%s must be MIN,MAX", label)
	}
	minimum, minimumValid := scalarNumber(parts[0])
	maximum, maximumValid := scalarNumber(parts[1])
	if !minimumValid || !maximumValid {
		return nil, fmt.Errorf("%s must contain two finite numbers", label)
	}
	if minimum < minimumAllowed || maximum < minimumAllowed || minimum > maximumAllowed || maximum > maximumAllowed {
		return nil, fmt.Errorf("%s values must be between %g and %g", label, minimumAllowed, maximumAllowed)
	}
	if !allowWrap && minimum > maximum {
		return nil, fmt.Errorf("%s minimum must not exceed its maximum", label)
	}
	return &metricRange{Minimum: minimum, Maximum: maximum}, nil
}

func analyzeLongTermSequence(frames []Frame, options imageInputOptions, ranges longTermRanges, profile longTermMetricProfile) (*sequenceSelectionManifest, error) {
	if len(frames) == 0 {
		return nil, fmt.Errorf("sequence contains no images")
	}
	if profile != longTermMetricProfileLRTL && profile != longTermMetricProfileLRT755 {
		return nil, fmt.Errorf("unsupported long-term metric profile %q", profile)
	}
	if err := validateImageInputOptions(options); err != nil {
		return nil, err
	}
	manifest := &sequenceSelectionManifest{
		Version:       selectionManifestVersion,
		MetricProfile: profile,
		Filters:       ranges,
		Frames:        make([]longTermFrameMetrics, len(frames)),
	}
	if profile == longTermMetricProfileLRT755 {
		manifest.MetricRevision = "lrt-7.5.5-preview-histogram-v2"
	}
	width, height := 0, 0
	var adjustments sequenceXMPAdjustments
	for index, frame := range frames {
		data, config, _, err := readRenderInputWithOptions(frame, options)
		if err != nil {
			return nil, fmt.Errorf("long-term analysis: %w", err)
		}
		var img image.Image
		if profile == longTermMetricProfileLRT755 {
			img, _, err = decodeLRTLongTermImage(data)
		} else {
			img, _, err = image.Decode(bytes.NewReader(data))
		}
		if err != nil {
			return nil, fmt.Errorf("decode %s: %w", frame.Image, err)
		}
		bounds := img.Bounds()
		if bounds.Empty() || config.Width <= 0 || config.Height <= 0 || config.Width != bounds.Dx() || config.Height != bounds.Dy() {
			return nil, fmt.Errorf("invalid image dimensions in %s", frame.Image)
		}
		if index == 0 {
			width, height = bounds.Dx(), bounds.Dy()
			adjustments, err = readSequenceXMPAdjustments(frames, width, height, options)
			if err != nil {
				return nil, err
			}
		} else if bounds.Dx() != width || bounds.Dy() != height {
			return nil, fmt.Errorf("inconsistent dimensions in %s", frame.Image)
		}
		img = adjustments.apply(index, img)
		metrics := longTermFrameMetrics{}
		luminance, contrast := 0.0, 0.0
		var hue *float64
		if profile == longTermMetricProfileLRT755 {
			previewWidth := options.PreviewWidthCamera
			if previewWidth == 0 {
				previewWidth = lrtDefaultCameraPreviewWidth
			}
			img, err = resizeLRTCameraPreviewWidth(img, previewWidth)
			if err != nil {
				return nil, fmt.Errorf("prepare LRT long-term image %s: %w", frame.Image, err)
			}
			measured := measureLRTLongTermFrame(img)
			metrics.LRTMetrics = &measured
			luminance, contrast, hue = measured.LuminanceMean, measured.ContrastStdDev, measured.HueMeanDegrees
		} else {
			metrics = measureLongTermFrame(img)
			luminance, contrast, hue = metrics.Luminance, metrics.ContrastEV, metrics.HueDegrees
		}
		included := rangeContains(ranges.Luminance, &luminance, false) &&
			rangeContains(ranges.Contrast, &contrast, false) &&
			rangeContains(ranges.Hue, hue, true)
		metrics.Image = filepath.Base(frame.Image)
		metrics.Included = included
		fileInfo, err := os.Stat(frame.Image)
		if err != nil {
			return nil, fmt.Errorf("inspect %s: %w", frame.Image, err)
		}
		metrics.SizeBytes = fileInfo.Size()
		metrics.ModifiedUnixNano = fileInfo.ModTime().UnixNano()
		manifest.Frames[index] = metrics
	}
	return manifest, nil
}

func measureLongTermFrame(img image.Image) longTermFrameMetrics {
	bounds := img.Bounds()
	stepX := max(1, (bounds.Dx()+63)/64)
	stepY := max(1, (bounds.Dy()+63)/64)
	luminances := make([]float64, 0, 4096)
	logLuminances := make([]float64, 0, 4096)
	var hueX, hueY, hueWeight float64
	for y := bounds.Min.Y; y < bounds.Max.Y; y += stepY {
		for x := bounds.Min.X; x < bounds.Max.X; x += stepX {
			pixel := colorNRGBA(img.At(x, y))
			alpha := float64(pixel.A) / 255
			red := srgb8ToLinear[pixel.R]
			green := srgb8ToLinear[pixel.G]
			blue := srgb8ToLinear[pixel.B]
			luminance := (0.2126*red + 0.7152*green + 0.0722*blue) * alpha
			luminances = append(luminances, luminance)
			logLuminances = append(logLuminances, math.Log2(max(luminance, 1e-8)))

			encodedRed := float64(pixel.R) / 255
			encodedGreen := float64(pixel.G) / 255
			encodedBlue := float64(pixel.B) / 255
			maximum := max(encodedRed, encodedGreen, encodedBlue)
			minimum := min(encodedRed, encodedGreen, encodedBlue)
			chroma := maximum - minimum
			if chroma <= 1e-6 || maximum <= 1e-6 || alpha == 0 {
				continue
			}
			hue := 0.0
			switch maximum {
			case encodedRed:
				hue = 60 * math.Mod((encodedGreen-encodedBlue)/chroma, 6)
			case encodedGreen:
				hue = 60 * ((encodedBlue-encodedRed)/chroma + 2)
			default:
				hue = 60 * ((encodedRed-encodedGreen)/chroma + 4)
			}
			if hue < 0 {
				hue += 360
			}
			weight := (chroma / maximum) * alpha
			radians := hue * math.Pi / 180
			hueX += math.Cos(radians) * weight
			hueY += math.Sin(radians) * weight
			hueWeight += weight
		}

	}
	sort.Float64s(luminances)
	sort.Float64s(logLuminances)
	result := longTermFrameMetrics{
		Luminance:  percentileSorted(luminances, 0.5),
		ContrastEV: percentileSorted(logLuminances, 0.9) - percentileSorted(logLuminances, 0.1),
	}
	if hueWeight > 0 && math.Hypot(hueX, hueY)/hueWeight >= 0.05 {
		hue := math.Atan2(hueY, hueX) * 180 / math.Pi
		if hue < 0 {
			hue += 360
		}
		result.HueDegrees = &hue
	}
	return result
}

func colorNRGBA(value color.Color) color.NRGBA {
	return color.NRGBAModel.Convert(value).(color.NRGBA)
}

func percentileSorted(values []float64, fraction float64) float64 {
	if len(values) == 1 {
		return values[0]
	}
	position := fraction * float64(len(values)-1)
	left := int(math.Floor(position))
	right := int(math.Ceil(position))
	if left == right {
		return values[left]
	}
	part := position - float64(left)
	return values[left] + (values[right]-values[left])*part
}

func rangeContains(bounds *metricRange, value *float64, circular bool) bool {
	if bounds == nil {
		return true
	}
	if value == nil {
		return false
	}
	if circular && bounds.Minimum > bounds.Maximum {
		return *value >= bounds.Minimum || *value <= bounds.Maximum
	}
	return *value >= bounds.Minimum && *value <= bounds.Maximum
}

func writeSelectionManifest(output string, manifest *sequenceSelectionManifest, overwrite bool) error {
	if strings.ToLower(filepath.Ext(output)) != ".json" {
		return fmt.Errorf("selection output must use the .json extension: %s", output)
	}
	outputDir := filepath.Dir(output)
	directoryInfo, err := os.Stat(outputDir)
	if err != nil {
		return fmt.Errorf("output directory: %w", err)
	}
	if !directoryInfo.IsDir() {
		return fmt.Errorf("output parent is not a directory: %s", outputDir)
	}
	if !overwrite {
		if _, err := os.Lstat(output); err == nil {
			return fmt.Errorf("output already exists (pass --overwrite to replace it): %s", output)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("check output: %w", err)
		}
	}
	absoluteDirectory, err := filepath.Abs(manifest.Directory)
	if err != nil {
		return fmt.Errorf("resolve sequence directory: %w", err)
	}
	manifest.Directory = filepath.Clean(absoluteDirectory)

	temporary, err := os.CreateTemp(outputDir, ".lrtl-selection-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary selection: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	encoder := json.NewEncoder(temporary)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(manifest); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("encode selection manifest: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync selection manifest: %w", err)
	}
	if err := temporary.Chmod(0644); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("set selection permissions: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close selection manifest: %w", err)
	}
	if !overwrite {
		if _, err := os.Lstat(output); err == nil {
			return fmt.Errorf("output appeared while creating selection (pass --overwrite to replace it): %s", output)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("check output: %w", err)
		}
	}
	if err := os.Rename(temporaryPath, output); err != nil {
		return fmt.Errorf("save selection manifest to %s: %w", output, err)
	}
	return nil
}

func loadSelectedFrames(frames []Frame, manifestPath, directory string) ([]Frame, error) {
	return loadSelectionFrames(frames, manifestPath, directory, false)
}

func loadSelectionFrames(frames []Frame, manifestPath, directory string, allowEmpty bool) ([]Frame, error) {
	info, err := os.Stat(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("selection manifest: %w", err)
	}
	if info.Size() > maxSelectionManifestSize {
		return nil, fmt.Errorf("selection manifest exceeds the %d MiB limit", maxSelectionManifestSize>>20)
	}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("read selection manifest: %w", err)
	}
	var manifest sequenceSelectionManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parse selection manifest: %w", err)
	}
	if manifest.Version != 1 && manifest.Version != selectionManifestVersion {
		return nil, fmt.Errorf("unsupported selection manifest version %d", manifest.Version)
	}
	absManifestDir, err := filepath.Abs(manifest.Directory)
	if err != nil {
		return nil, fmt.Errorf("resolve manifest sequence directory: %w", err)
	}
	absDirectory, err := filepath.Abs(directory)
	if err != nil {
		return nil, fmt.Errorf("resolve sequence directory: %w", err)
	}
	if !samePath(filepath.Clean(absManifestDir), filepath.Clean(absDirectory)) {
		return nil, fmt.Errorf("selection manifest belongs to %s, not %s", manifest.Directory, absDirectory)
	}
	if len(manifest.Frames) != len(frames) {
		return nil, fmt.Errorf("selection manifest contains %d frames, but the sequence has %d", len(manifest.Frames), len(frames))
	}
	byName := make(map[string]Frame, len(frames))
	for _, frame := range frames {
		byName[filepath.Base(frame.Image)] = frame
	}
	seen := make(map[string]bool, len(frames))
	selected := make([]Frame, 0, len(frames))
	for _, entry := range manifest.Frames {
		if entry.Image == "" || filepath.Base(entry.Image) != entry.Image || strings.ContainsAny(entry.Image, "/\\") {
			return nil, fmt.Errorf("selection manifest contains an invalid image name %q", entry.Image)
		}
		frame, found := byName[entry.Image]
		if !found || seen[entry.Image] {
			return nil, fmt.Errorf("selection manifest has an unknown or duplicate image %q", entry.Image)
		}
		fileInfo, err := os.Stat(frame.Image)
		if err != nil {
			return nil, fmt.Errorf("inspect %s: %w", frame.Image, err)
		}
		if fileInfo.Size() != entry.SizeBytes || fileInfo.ModTime().UnixNano() != entry.ModifiedUnixNano {
			return nil, fmt.Errorf("image %q changed after the selection manifest was created", entry.Image)
		}
		seen[entry.Image] = true
		if entry.Included {
			selected = append(selected, frame)
		}
	}
	if len(seen) != len(frames) {
		return nil, fmt.Errorf("selection manifest does not describe every image in the sequence")
	}
	if len(selected) == 0 && !allowEmpty {
		return nil, fmt.Errorf("selection manifest excludes every image")
	}
	sort.Slice(selected, func(i, j int) bool { return selected[i].Index < selected[j].Index })
	return selected, nil
}

func samePath(left, right string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}
