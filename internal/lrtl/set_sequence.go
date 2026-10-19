package lrtl

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type numericXMPSet struct {
	property, value string
	inputValue      float64
	previewEV       bool
}

func parseNumericXMPSet(property, text string) (numericXMPSet, error) {
	property = strings.TrimPrefix(property, "crs:")
	edit := numericXMPSet{property: "crs:" + property, previewEV: property == "PreviewEV" || property == "lrt:PreviewEV"}
	if edit.previewEV {
		edit.property = "lrt:ExternalExposureDefault"
	} else {
		if strings.Contains(property, ":") {
			return edit, fmt.Errorf("property must be a numeric Camera Raw control or PreviewEV: %s", property)
		}
		allowed := false
		for _, candidate := range NumericCRSProperties {
			if candidate == property {
				allowed = true
				break
			}
		}
		if !allowed {
			return edit, fmt.Errorf("unsupported numeric Camera Raw property: %s", property)
		}
	}
	value, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return edit, fmt.Errorf("value must be a finite number: %s", text)
	}
	edit.inputValue = value
	if edit.previewEV {
		value *= 4
		if math.IsInf(value, 0) {
			return edit, fmt.Errorf("PreviewEV is outside the supported numeric range: %s", text)
		}
	}
	edit.value = formatNumber(value)
	return edit, nil
}

func sidecarPlanKey(path string) (string, error) {
	key, err := resolveFuturePath(path)
	if err != nil {
		return "", err
	}
	if samePath("A", "a") {
		key = strings.ToLower(key)
	}
	return key, nil
}

func planSequenceValueEdits(all, selected []Frame, property, text string) ([]packetOutput, error) {
	if len(selected) == 0 {
		return nil, fmt.Errorf("no frames selected")
	}
	var global numericXMPSet
	var local *maskEditPath
	if strings.Contains(property, "[") {
		path, err := parseMaskEditPath(property)
		if err != nil {
			return nil, err
		}
		local = &path
	} else {
		var err error
		global, err = parseNumericXMPSet(property, text)
		if err != nil {
			return nil, err
		}
	}
	return planSelectedPacketEdits(all, selected, func(packet *XMPPacket) error {
		if local != nil {
			_, err := setMaskProperty(packet, *local, text)
			return err
		}
		return packet.SetScalar(global.property, global.value)
	})
}

// Validate every candidate before the caller starts writing any sidecars.
func planSelectedPacketEdits(all, selected []Frame, edit func(*XMPPacket) error) ([]packetOutput, error) {
	if len(selected) == 0 {
		return nil, fmt.Errorf("no frames selected")
	}
	outputs := make([]packetOutput, 0, len(selected))
	selectedImages := map[string]bool{}
	byPath := map[string][]byte{}
	for _, frame := range selected {
		image, err := filepath.Abs(frame.Image)
		if err != nil {
			return nil, err
		}
		selectedImages[image] = true
		path := frame.OutputSidecar()
		if info, err := os.Lstat(path); err == nil {
			if !info.Mode().IsRegular() {
				return nil, fmt.Errorf("sidecar output must be a regular file: %s", path)
			}
		} else if !os.IsNotExist(err) {
			return nil, err
		}
		packet, err := PacketForFrame(frame)
		if err != nil {
			return nil, err
		}
		before, err := packet.Serialize()
		if err != nil {
			return nil, err
		}
		if err := edit(packet); err != nil {
			return nil, fmt.Errorf("%s: %w", frame.Image, err)
		}
		after, err := packet.Serialize()
		if err != nil {
			return nil, err
		}
		if bytes.Equal(before, after) {
			continue
		}
		key, err := sidecarPlanKey(path)
		if err != nil {
			return nil, err
		}
		if earlier, exists := byPath[key]; exists {
			if !bytes.Equal(earlier, after) {
				return nil, fmt.Errorf("selected frames propose conflicting changes to %s", path)
			}
			continue
		}
		byPath[key] = after
		outputs = append(outputs, packetOutput{Path: path, Packet: packet})
	}
	if len(outputs) == 0 {
		return outputs, nil
	}
	for _, frame := range all {
		image, err := filepath.Abs(frame.Image)
		if err != nil {
			return nil, err
		}
		if selectedImages[image] {
			continue
		}
		// Updating a common stem can also make it newer than an unselected
		// image's currently active appended sidecar. Guard both naming variants.
		paths := sidecarCandidates(frame.Image)
		if frame.Sidecar != "" {
			paths = append(paths, frame.Sidecar)
		}
		for _, path := range paths {
			key, err := sidecarPlanKey(path)
			if err != nil {
				return nil, err
			}
			if _, changes := byPath[key]; changes {
				return nil, fmt.Errorf("%s could also apply to an unselected image; select both images or copy the selection first", path)
			}
		}
	}
	return outputs, nil
}

func runSetSequence(arguments []string, stdout, stderr io.Writer) int {
	set := newFlagSet("set-sequence", stderr)
	filters := registerFrameSelectionFlags(set)
	write := set.Bool("write", false, "save all validated XMP changes")
	noBackup := set.Bool("no-backup", false, "save existing sidecars without backups")
	if err := set.Parse(reorderFlags(arguments, map[string]bool{"--selection": true, "-selection": true, "--filename-contains": true, "-filename-contains": true, "--ratings": true, "-ratings": true}, true)); err != nil {
		if done, code := finishFlagParse(set, err); done {
			return code
		}
	}
	if set.NArg() != 3 || *noBackup && !*write {
		return reportError(stderr, fmt.Errorf("Usage: lrtl set-sequence DIRECTORY PROPERTY VALUE [--selection FILTER.json] [--filename-contains TEXT] [--ratings LIST] [--invert] [--write] [--no-backup]"))
	}
	options, err := filters.options(set)
	if err != nil {
		return reportError(stderr, err)
	}
	frames, err := DiscoverSequence(set.Arg(0))
	if err != nil {
		return reportError(stderr, err)
	}
	selected, err := filterSequenceFrames(frames, set.Arg(0), options)
	if err != nil {
		return reportError(stderr, err)
	}
	outputs, err := planSequenceValueEdits(frames, selected, set.Arg(1), set.Arg(2))
	if err != nil {
		return reportError(stderr, err)
	}
	fmt.Fprintf(stdout, "Selected: %d/%d frames; changed sidecars: %d\nSet %s = %s\n", len(selected), len(frames), len(outputs), set.Arg(1), set.Arg(2))
	for _, output := range outputs {
		fmt.Fprintln(stdout, output.Path)
	}
	if !*write {
		fmt.Fprintln(stdout, "Dry run. Pass --write to save sidecars.")
		return 0
	}
	for _, output := range outputs {
		backup, err := WritePacket(output.Path, output.Packet, !*noBackup)
		if err != nil {
			return reportError(stderr, err)
		}
		if backup != "" {
			fmt.Fprintf(stdout, "Backup: %s\n", backup)
		}
	}
	fmt.Fprintf(stdout, "Updated %d sidecar(s).\n", len(outputs))
	return 0
}
