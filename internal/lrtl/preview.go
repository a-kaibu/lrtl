package lrtl

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	previewSVGWidth  = 1120
	previewSVGHeight = 680
)

type previewFrameMark struct {
	label string
	color string
}

func writeSequencePreview(frames []Frame, output string, useRawPreview, overwrite bool) error {
	return writeSequencePreviewWithOptions(frames, output, imageInputOptions{RawPreview: useRawPreview}, overwrite)
}

func writeSequencePreviewWithOptions(frames []Frame, output string, options imageInputOptions, overwrite bool) error {
	if len(frames) == 0 {
		return fmt.Errorf("sequence contains no images")
	}
	if err := validateImageInputOptions(options); err != nil {
		return err
	}
	if strings.ToLower(filepath.Ext(output)) != ".svg" {
		return fmt.Errorf("preview output must use the .svg extension: %s", output)
	}
	outputDir := filepath.Dir(output)
	info, err := os.Stat(outputDir)
	if err != nil {
		return fmt.Errorf("output directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("output parent is not a directory: %s", outputDir)
	}
	if !overwrite {
		if _, err := os.Lstat(output); err == nil {
			return fmt.Errorf("output already exists (pass --overwrite to replace it): %s", output)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("check output: %w", err)
		}
	}

	luminance, err := analyzeLuminanceSequenceWithInputOptions(frames, options)
	if err != nil {
		return err
	}
	marks := make([]previewFrameMark, len(frames))
	exposure := make([]float64, len(frames))
	hasExposure := false
	for index, frame := range frames {
		packet, err := PacketForFrame(frame)
		if err != nil {
			return fmt.Errorf("read preview metadata for %s: %w", frame.Image, err)
		}
		if value, found, err := packet.GetScalar("crs:Exposure2012"); err != nil {
			return fmt.Errorf("read Exposure2012 for %s: %w", frame.Image, err)
		} else if found {
			number, valid := scalarNumber(value)
			if !valid {
				return fmt.Errorf("%s has invalid crs:Exposure2012 value %q", frame.Image, value)
			}
			exposure[index] = number
			hasExposure = true
		}
		if packet.KeyframeKind() == "default" {
			marks[index] = previewFrameMark{label: "KF", color: "#2563eb"}
		} else if packet.KeyframeKind() == "crop" {
			marks[index] = previewFrameMark{label: "Crop", color: "#7c3aed"}
		} else {
			kind, found, err := packet.GetScalar(holyGrailKeyframeProperty)
			if err != nil {
				return fmt.Errorf("read Holy Grail keyframe marker for %s: %w", frame.Image, err)
			}
			if found && (kind == "2" || kind == "3") {
				marks[index] = previewFrameMark{label: kind + "*", color: "#d97706"}
			} else if rating, valid := packet.Rating(); valid && (rating == 2 || rating == 3) {
				marks[index] = previewFrameMark{label: strconv.Itoa(rating) + "*", color: "#d97706"}
			} else if rating, valid := packet.Rating(); valid && (rating == 4 || rating == 5) {
				marks[index] = previewFrameMark{label: strconv.Itoa(rating) + "*", color: "#2563eb"}
			}
		}
	}
	baseExposure := exposure[0]
	for index := range exposure {
		exposure[index] -= baseExposure
	}
	baseLuminance := luminance[0]
	for index := range luminance {
		luminance[index] -= baseLuminance
	}

	svg := renderPreviewSVG(frames, luminance, exposure, hasExposure, marks)
	temp, err := os.CreateTemp(outputDir, ".lrtl-preview-*.svg")
	if err != nil {
		return fmt.Errorf("create temporary preview: %w", err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if _, err := temp.WriteString(svg); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write temporary preview: %w", err)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return fmt.Errorf("sync temporary preview: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close temporary preview: %w", err)
	}
	if !overwrite {
		if _, err := os.Lstat(output); err == nil {
			return fmt.Errorf("output appeared while creating preview (pass --overwrite to replace it): %s", output)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("check output: %w", err)
		}
	}
	if err := os.Rename(tempPath, output); err != nil {
		return fmt.Errorf("save SVG preview to %s: %w", output, err)
	}
	return nil
}

func renderPreviewSVG(frames []Frame, luminance, exposure []float64, hasExposure bool, marks []previewFrameMark) string {
	const (
		left   = 92.0
		top    = 82.0
		width  = 980.0
		height = 470.0
	)
	minimum, maximum := luminance[0], luminance[0]
	include := append([]float64(nil), luminance...)
	if hasExposure {
		include = append(include, exposure...)
	}
	for _, value := range include {
		if value < minimum {
			minimum = value
		}
		if value > maximum {
			maximum = value
		}
	}
	if maximum-minimum < 0.25 {
		minimum -= 0.25
		maximum += 0.25
	} else {
		padding := (maximum - minimum) * 0.08
		minimum -= padding
		maximum += padding
	}
	xFor := func(index int) float64 {
		if len(frames) < 2 {
			return left + width/2
		}
		return left + width*float64(index)/float64(len(frames)-1)
	}
	yFor := func(value float64) float64 {
		return top + height*(maximum-value)/(maximum-minimum)
	}
	points := func(series []float64) string {
		var result strings.Builder
		for index, value := range series {
			if index > 0 {
				result.WriteByte(' ')
			}
			result.WriteString(svgNumber(xFor(index)))
			result.WriteByte(',')
			result.WriteString(svgNumber(yFor(value)))
		}
		return result.String()
	}

	var svg strings.Builder
	fmt.Fprintf(&svg, "<svg xmlns=\"http://www.w3.org/2000/svg\" width=\"%d\" height=\"%d\" viewBox=\"0 0 %d %d\" role=\"img\">\n", previewSVGWidth, previewSVGHeight, previewSVGWidth, previewSVGHeight)
	svg.WriteString("<title>Sequence brightness and exposure preview</title>\n")
	svg.WriteString("<rect width=\"100%\" height=\"100%\" fill=\"#fff\"/>\n")
	svg.WriteString("<style>text{font-family:system-ui,Segoe UI,sans-serif;fill:#1f2937}.grid{stroke:#e5e7eb;stroke-width:1}.axis{stroke:#64748b;stroke-width:1}.luma{fill:none;stroke:#0f766e;stroke-width:2.5}.exposure{fill:none;stroke:#9333ea;stroke-width:2;stroke-dasharray:8 5}</style>\n")
	svg.WriteString("<text x=\"92\" y=\"34\" font-size=\"22\" font-weight=\"600\">Sequence preview</text>\n")
	if hasExposure {
		fmt.Fprintf(&svg, "<text x=\"92\" y=\"57\" font-size=\"13\">%d frames · brightness and XMP exposure relative to frame 1</text>\n", len(frames))
	} else {
		fmt.Fprintf(&svg, "<text x=\"92\" y=\"57\" font-size=\"13\">%d frames · brightness relative to frame 1</text>\n", len(frames))
	}
	for tick := 0; tick <= 5; tick++ {
		value := maximum - (maximum-minimum)*float64(tick)/5
		y := yFor(value)
		fmt.Fprintf(&svg, "<line class=\"grid\" x1=\"%.1f\" y1=\"%.1f\" x2=\"%.1f\" y2=\"%.1f\"/>\n", left, y, left+width, y)
		fmt.Fprintf(&svg, "<text x=\"%.1f\" y=\"%.1f\" text-anchor=\"end\" font-size=\"12\">%+.2f EV</text>\n", left-12, y+4, value)
	}
	fmt.Fprintf(&svg, "<line class=\"axis\" x1=\"%.1f\" y1=\"%.1f\" x2=\"%.1f\" y2=\"%.1f\"/>\n", left, top, left, top+height)
	fmt.Fprintf(&svg, "<line class=\"axis\" x1=\"%.1f\" y1=\"%.1f\" x2=\"%.1f\" y2=\"%.1f\"/>\n", left, top+height, left+width, top+height)
	fmt.Fprintf(&svg, "<text x=\"%.1f\" y=\"%.1f\" font-size=\"12\">Frame number</text>\n", left+width/2, top+height+46)
	fmt.Fprintf(&svg, "<polyline class=\"luma\" points=\"%s\"/>\n", points(luminance))
	if hasExposure {
		fmt.Fprintf(&svg, "<polyline class=\"exposure\" points=\"%s\"/>\n", points(exposure))
	}
	for index, mark := range marks {
		if mark.label == "" {
			continue
		}
		x := xFor(index)
		fmt.Fprintf(&svg, "<line x1=\"%.1f\" y1=\"%.1f\" x2=\"%.1f\" y2=\"%.1f\" stroke=\"%s\" stroke-opacity=\"0.25\"/>\n", x, top, x, top+height, mark.color)
		fmt.Fprintf(&svg, "<text x=\"%.1f\" y=\"%.1f\" text-anchor=\"middle\" font-size=\"11\" style=\"fill:%s;font-weight:600\">%s</text>\n", x, top-12, mark.color, mark.label)
	}
	if len(frames) == 1 {
		fmt.Fprintf(&svg, "<text x=\"%.1f\" y=\"%.1f\" text-anchor=\"middle\" font-size=\"12\">1</text>\n", xFor(0), top+height+20)
	} else {
		seen := make(map[int]bool)
		for tick := 0; tick <= 4; tick++ {
			index := int(math.Round(float64(len(frames)-1) * float64(tick) / 4))
			if seen[index] {
				continue
			}
			seen[index] = true
			fmt.Fprintf(&svg, "<text x=\"%.1f\" y=\"%.1f\" text-anchor=\"middle\" font-size=\"12\">%d</text>\n", xFor(index), top+height+20, index+1)
		}
	}
	svg.WriteString("<line x1=\"92\" y1=\"624\" x2=\"116\" y2=\"624\" stroke=\"#0f766e\" stroke-width=\"3\"/><text x=\"122\" y=\"628\" font-size=\"13\">Measured brightness</text>\n")
	if hasExposure {
		svg.WriteString("<line x1=\"290\" y1=\"624\" x2=\"314\" y2=\"624\" stroke=\"#9333ea\" stroke-width=\"2\" stroke-dasharray=\"8 5\"/><text x=\"320\" y=\"628\" font-size=\"13\">XMP Exposure2012</text>\n")
	}
	svg.WriteString("<line x1=\"510\" y1=\"624\" x2=\"510\" y2=\"642\" stroke=\"#2563eb\" stroke-opacity=\"0.4\"/><text x=\"518\" y=\"638\" font-size=\"13\" style=\"fill:#2563eb;font-weight:600\">KF / 4* / 5*</text>\n")
	svg.WriteString("<line x1=\"650\" y1=\"624\" x2=\"650\" y2=\"642\" stroke=\"#7c3aed\" stroke-opacity=\"0.4\"/><text x=\"658\" y=\"638\" font-size=\"13\" style=\"fill:#7c3aed;font-weight:600\">Crop</text>\n")
	svg.WriteString("<line x1=\"730\" y1=\"624\" x2=\"730\" y2=\"642\" stroke=\"#d97706\" stroke-opacity=\"0.4\"/><text x=\"738\" y=\"638\" font-size=\"13\" style=\"fill:#d97706;font-weight:600\">2* / 3* Holy Grail</text>\n")
	svg.WriteString("</svg>\n")
	return svg.String()
}

func svgNumber(value float64) string {
	return strconv.FormatFloat(value, 'f', 1, 64)
}
