package lrtl

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"os"
	"strings"
	"time"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

const defaultTimestampFormat = "2006-01-02 15:04:05"

// Capture dates are global properties. Qualified simple values (including
// xml:lang) remain dates, while nested preset metadata is not a capture clock.
func captureTimestampScalar(packet *XMPPacket, property string) (string, bool, error) {
	uri, local, err := expandedName(property)
	if err != nil {
		return "", false, err
	}
	root := packet.findRootProperty(uri, local)
	if root.attribute != nil {
		return root.attribute.Value, true, nil
	}
	if root.element == nil {
		return "", false, nil
	}
	node, err := parsePasteNode(root.element, 0)
	if err != nil {
		return "", false, err
	}
	if node.Kind != "scalar" && node.Kind != "uri" {
		return "", false, nil
	}
	return node.Value, true, nil
}

func setCaptureTimestampScalar(packet *XMPPacket, property, value string) error {
	uri, local, err := expandedName(property)
	if err != nil {
		return err
	}
	before, after, err := saveStateMaps(packet)
	if err != nil {
		return err
	}
	if err := setSaveStateProperty(after, pasteName{uri, local}, strings.SplitN(property, ":", 2)[0], value); err != nil {
		return err
	}
	_, err = writeChangedPasteRoots(packet, before, after)
	return err
}

func parseCaptureTimestamp(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{
		time.RFC3339Nano,
		"2006-01-02T15:04:05.999999999",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006:01:02 15:04:05",
	} {
		parsed, err := time.Parse(layout, value)
		if err == nil && !parsed.IsZero() {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func captureTimeFromXMP(frame Frame) (time.Time, bool, error) {
	packet, err := loadPacket(frame)
	if err != nil {
		return time.Time{}, false, err
	}
	if packet == nil {
		return time.Time{}, false, nil
	}
	for _, property := range []string{"exif:DateTimeOriginal", "xmp:CreateDate", "xmp:ModifyDate"} {
		value, found, err := captureTimestampScalar(packet, property)
		if err != nil {
			return time.Time{}, false, err
		}
		if !found {
			continue
		}
		parsed, ok := parseCaptureTimestamp(value)
		if !ok {
			return time.Time{}, false, fmt.Errorf("invalid %s value %q", property, value)
		}
		return parsed, true, nil
	}
	return time.Time{}, false, nil
}

func captureTimeFromImage(frame Frame) (time.Time, bool, error) {
	file, err := os.Open(frame.Image)
	if err != nil {
		return time.Time{}, false, err
	}
	metadata, decodeErr := decodeImageMetadata(file)
	closeErr := file.Close()
	if decodeErr != nil {
		return time.Time{}, false, decodeErr
	}
	if closeErr != nil {
		return time.Time{}, false, closeErr
	}
	for _, timestamp := range []time.Time{metadata.DateTimeOriginal(), metadata.CreateDate(), metadata.ModifyDate()} {
		if !timestamp.IsZero() {
			return timestamp, true, nil
		}
	}
	return time.Time{}, false, nil
}

func readFrameCaptureTime(frame Frame) (time.Time, error) {
	if timestamp, found, err := captureTimeFromXMP(frame); err != nil {
		return time.Time{}, err
	} else if found {
		return timestamp, nil
	}
	timestamp, found, err := captureTimeFromImage(frame)
	if err != nil {
		return time.Time{}, err
	}
	if !found {
		return time.Time{}, fmt.Errorf("capture time not found in XMP or EXIF metadata")
	}
	return timestamp, nil
}

func readFrameCaptureTimes(frames []Frame) ([]time.Time, error) {
	timestamps := make([]time.Time, len(frames))
	for index, frame := range frames {
		timestamp, err := readFrameCaptureTime(frame)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", frame.Image, err)
		}
		timestamps[index] = timestamp
	}
	return timestamps, nil
}

func newTimestampOverlayFace(width, height int) (font.Face, error) {
	parsed, err := opentype.Parse(goregular.TTF)
	if err != nil {
		return nil, fmt.Errorf("parse embedded timestamp font: %w", err)
	}
	size := float64(max(12, min(width, height)/40))
	if size > 72 {
		size = 72
	}
	face, err := opentype.NewFace(parsed, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		return nil, fmt.Errorf("initialize timestamp font: %w", err)
	}
	return face, nil
}

func applyTimestampOverlay(source image.Image, label string, face font.Face) image.Image {
	return applyTextOverlay(source, label, face, "bottom-right")
}

func applyWatermarkOverlay(source image.Image, label, position string, face font.Face) image.Image {
	return applyTextOverlay(source, label, face, position)
}

func applyTextOverlay(source image.Image, label string, face font.Face, position string) image.Image {
	bounds := source.Bounds()
	result := newFrameImage(bounds, source)
	draw.Draw(result, bounds, source, bounds.Min, draw.Src)
	if label == "" || face == nil {
		return result
	}
	metrics := face.Metrics()
	margin := max(8, metrics.Height.Ceil()/2)
	outline := max(1, metrics.Height.Ceil()/12)
	drawer := font.Drawer{Dst: result, Face: face}
	stringWidth := drawer.MeasureString(label).Ceil()
	textHeight := metrics.Ascent.Ceil() + metrics.Descent.Ceil()
	x := bounds.Min.X + margin
	y := bounds.Min.Y + margin + metrics.Ascent.Ceil()
	switch position {
	case "top-right":
		x = bounds.Max.X - margin - stringWidth
	case "bottom-left":
		y = bounds.Max.Y - margin - metrics.Descent.Ceil()
	case "bottom-right", "":
		x = bounds.Max.X - margin - stringWidth
		y = bounds.Max.Y - margin - metrics.Descent.Ceil()
	case "center":
		x = bounds.Min.X + (bounds.Dx()-stringWidth)/2
		y = bounds.Min.Y + (bounds.Dy()-textHeight)/2 + metrics.Ascent.Ceil()
	}
	minX, maxX := bounds.Min.X+margin, bounds.Max.X-margin-stringWidth
	if maxX < minX {
		x = minX
	} else {
		x = max(minX, min(maxX, x))
	}
	minY, maxY := bounds.Min.Y+metrics.Ascent.Ceil()+margin, bounds.Max.Y-margin-metrics.Descent.Ceil()
	if maxY < minY {
		y = bounds.Min.Y + (bounds.Dy()-textHeight)/2 + metrics.Ascent.Ceil()
	} else {
		y = max(minY, min(maxY, y))
	}
	drawer.Src = image.NewUniform(color.NRGBA{R: 0, G: 0, B: 0, A: 210})
	for dy := -outline; dy <= outline; dy++ {
		for dx := -outline; dx <= outline; dx++ {
			if dx*dx+dy*dy > outline*outline {
				continue
			}
			drawer.Dot = fixed.P(x+dx, y+dy)
			drawer.DrawString(label)
		}
	}
	drawer.Src = image.NewUniform(color.NRGBA{R: 255, G: 255, B: 255, A: 255})
	drawer.Dot = fixed.P(x, y)
	drawer.DrawString(label)
	return result
}
