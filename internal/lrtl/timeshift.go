package lrtl

import (
	"fmt"
	"io"
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var captureTimestampProperties = []string{
	"exif:DateTimeOriginal",
	"exif:DateTimeDigitized",
	"xmp:CreateDate",
}

type timeShiftChange struct {
	frame      Frame
	path       string
	packet     *XMPPacket
	created    bool
	properties []string
	before     map[string]string
	after      map[string]string
}

func parseTimeShiftOffset(value string) (time.Duration, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0, fmt.Errorf("time shift offset is required")
	}
	if strings.HasSuffix(trimmed, "d") || strings.HasSuffix(trimmed, "w") {
		unit := trimmed[len(trimmed)-1]
		number := strings.TrimSpace(trimmed[:len(trimmed)-1])
		amount, err := strconv.ParseFloat(number, 64)
		if err != nil || math.IsNaN(amount) || math.IsInf(amount, 0) {
			return 0, fmt.Errorf("invalid time shift offset %q; use a duration such as 2h30m or 1d", value)
		}
		hours := amount * 24
		if unit == 'w' {
			hours *= 7
		}
		hourText := strconv.FormatFloat(hours, 'f', -1, 64)
		trimmed = hourText + "h"
	}
	offset, err := time.ParseDuration(trimmed)
	if err != nil {
		return 0, fmt.Errorf("invalid time shift offset %q; use a duration such as 2h30m or 1d: %w", value, err)
	}
	if offset == 0 {
		return 0, fmt.Errorf("time shift offset must not be zero")
	}
	return offset, nil
}

func shiftTimestampValue(value string, offset time.Duration) (string, error) {
	value = strings.TrimSpace(value)
	layouts := []string{
		time.RFC3339Nano,
		"2006-01-02T15:04:05.999999999",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
		"2006:01:02 15:04:05.999999999",
		"2006:01:02 15:04:05",
	}
	for _, layout := range layouts {
		parsed, err := time.Parse(layout, value)
		if err != nil {
			continue
		}
		shifted := parsed.Add(offset)
		return shifted.Format(layout), nil
	}
	return "", fmt.Errorf("unsupported capture timestamp %q", value)
}

func planTimeShift(frames []Frame, offset time.Duration) ([]timeShiftChange, error) {
	if len(frames) == 0 {
		return nil, fmt.Errorf("sequence contains no images")
	}
	changes := make([]timeShiftChange, 0, len(frames))
	for _, frame := range frames {
		packet := EmptyXMPPacket()
		created := frame.Sidecar == ""
		if !created {
			var err error
			packet, err = loadPacket(frame)
			if err != nil {
				return nil, err
			}
		}
		change := timeShiftChange{
			frame: frame, path: frame.OutputSidecar(), packet: packet, created: created,
			before: make(map[string]string), after: make(map[string]string),
		}
		for _, property := range captureTimestampProperties {
			hasProperty, err := packet.HasProperty(property)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", filepath.Base(frame.Image), err)
			}
			value, found, err := packet.GetScalar(property)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", filepath.Base(frame.Image), err)
			}
			if hasProperty && !found {
				return nil, fmt.Errorf("%s: %s is not a scalar timestamp", filepath.Base(frame.Image), property)
			}
			if !found {
				continue
			}
			shifted, err := shiftTimestampValue(value, offset)
			if err != nil {
				return nil, fmt.Errorf("%s %s: %w", filepath.Base(frame.Image), property, err)
			}
			if err := packet.SetScalar(property, shifted); err != nil {
				return nil, fmt.Errorf("%s %s: %w", filepath.Base(frame.Image), property, err)
			}
			change.properties = append(change.properties, property)
			change.before[property] = value
			change.after[property] = shifted
		}
		if len(change.properties) == 0 {
			captureTime, found, err := captureTimeFromImage(frame)
			if err != nil {
				return nil, fmt.Errorf("%s: read capture time: %w", filepath.Base(frame.Image), err)
			}
			if !found {
				continue
			}
			property := "exif:DateTimeOriginal"
			shifted := captureTime.Add(offset).Format("2006:01:02 15:04:05")
			if err := packet.SetScalar(property, shifted); err != nil {
				return nil, fmt.Errorf("%s: %w", filepath.Base(frame.Image), err)
			}
			change.properties = append(change.properties, property)
			change.before[property] = captureTime.Format("2006:01:02 15:04:05")
			change.after[property] = shifted
		}
		changes = append(changes, change)
	}
	return changes, nil
}

func TimeShiftSequence(directory string, offset time.Duration, write, backup bool, stdout io.Writer) error {
	if offset == 0 {
		return fmt.Errorf("time shift offset must not be zero")
	}
	frames, err := DiscoverSequence(directory)
	if err != nil {
		return err
	}
	changes, err := planTimeShift(frames, offset)
	if err != nil {
		return err
	}
	for _, change := range changes {
		fmt.Fprintf(stdout, "%s\n", filepath.Base(change.frame.Image))
		for _, property := range change.properties {
			fmt.Fprintf(stdout, "  %-28s %s -> %s\n", property, change.before[property], change.after[property])
		}
		if change.created {
			fmt.Fprintf(stdout, "  create XMP: %s\n", change.path)
		}
	}
	if len(changes) == 0 {
		fmt.Fprintln(stdout, "No capture timestamps found to shift.")
		return nil
	}
	if !write {
		fmt.Fprintf(stdout, "Preview only. %d image(s) would shift by %s; pass --write to save XMP sidecars.\n", len(changes), offset)
		return nil
	}
	for _, change := range changes {
		backupPath, err := WritePacket(change.path, change.packet, backup)
		if err != nil {
			return fmt.Errorf("write XMP for %s: %w", filepath.Base(change.frame.Image), err)
		}
		if backupPath != "" {
			fmt.Fprintf(stdout, "  Backup: %s\n", backupPath)
		}
		fmt.Fprintf(stdout, "  Saved: %s\n", change.path)
	}
	fmt.Fprintf(stdout, "Shifted capture timestamps for %d image(s) by %s.\n", len(changes), offset)
	return nil
}
