package lrtl

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type settingsReference struct {
	Packet      *XMPPacket
	Path, Image string
}

func readSettingsReference(path string) (*settingsReference, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("settings reference must be a regular image or XMP file")
	}
	ref := &settingsReference{Path: path}
	if strings.EqualFold(filepath.Ext(path), ".xmp") {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		ref.Packet, err = parseXMP(data)
		if err != nil {
			return nil, err
		}
	} else {
		if !imageExtensions[strings.ToLower(filepath.Ext(path))] {
			return nil, fmt.Errorf("unsupported settings reference image: %s", path)
		}
		ref.Image = path
		sidecar, err := findSidecar(path)
		if err != nil {
			return nil, err
		}
		packet, found, err := loadFramePacket(Frame{Image: path, Sidecar: sidecar})
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, fmt.Errorf("reference image contains no embedded or sidecar XMP: %s", path)
		}
		ref.Packet = packet
		if sidecar != "" {
			ref.Path = sidecar
		}
	}
	if _, err := pastePacketNodes(ref.Packet); err != nil {
		return nil, fmt.Errorf("reference metadata: %w", err)
	}
	return ref, nil
}

func planCopySettings(all, selected []Frame, ref *settingsReference) ([]packetOutput, int, error) {
	return planCopySettingsColumns(all, selected, ref, nil)
}

func planCopySettingsColumns(all, selected []Frame, ref *settingsReference, columns []selectedColumn, saveStateProfiles ...string) ([]packetOutput, int, error) {
	profile, err := copySaveStateProfile(saveStateProfiles)
	if err != nil {
		return nil, 0, err
	}
	if len(selected) == 0 {
		return nil, 0, fmt.Errorf("no frames selected")
	}
	if ref == nil || ref.Packet == nil {
		return nil, 0, fmt.Errorf("missing settings reference")
	}
	refKey, err := sidecarPlanKey(ref.Path)
	if err != nil {
		return nil, 0, err
	}
	imageKey := ""
	if ref.Image != "" {
		imageKey, err = sidecarPlanKey(ref.Image)
		if err != nil {
			return nil, 0, err
		}
	}
	refInfo, err := os.Stat(ref.Path)
	if err != nil {
		return nil, 0, err
	}
	var targets []Frame
	skipped := 0
	for _, frame := range selected {
		key, err := sidecarPlanKey(frame.OutputSidecar())
		if err != nil {
			return nil, 0, err
		}
		self := key == refKey
		if !self && imageKey != "" {
			key, err := sidecarPlanKey(frame.Image)
			if err != nil {
				return nil, 0, err
			}
			self = key == imageKey
		}
		if !self {
			if info, err := os.Stat(frame.OutputSidecar()); err == nil {
				self = os.SameFile(info, refInfo)
			} else if !os.IsNotExist(err) {
				return nil, 0, err
			}
		}
		if self {
			skipped++
			continue
		}
		targets = append(targets, frame)
	}
	if len(targets) == 0 {
		return nil, skipped, nil
	}
	outputs, err := planSelectedPacketEdits(all, targets, func(target *XMPPacket) error {
		_, err := applyCopySettingsProfile(target, ref.Packet, columns, profile)
		return err
	})
	return outputs, skipped, err
}

func runCopySettings(arguments []string, stdout, stderr io.Writer) int {
	set := newFlagSet("copy-settings", stderr)
	filters := registerFrameSelectionFlags(set)
	write := set.Bool("write", false, "save all validated metadata changes to sidecars")
	noBackup := set.Bool("no-backup", false, "save existing sidecars without backups")
	properties := set.String("properties", "", "copy these registered columns, separated by commas; a missing source value clears the column")
	listProperties := set.Bool("list-properties", false, "list the registered columns available for selected-column copying")
	saveState := set.String("save-state", nativeSaveStateProfile, "registered-column serialization and native save preparation: lrt-7.5.5 or preserve")
	const usage = "Usage: lrtl copy-settings SOURCE_IMAGE_OR_XMP DIRECTORY [--properties NAMES] [--save-state lrt-7.5.5|preserve] [--selection FILTER.json] [--filename-contains TEXT] [--ratings LIST] [--invert] [--write] [--no-backup]\n       lrtl copy-settings --list-properties"
	if err := set.Parse(reorderFlags(arguments, map[string]bool{"--selection": true, "-selection": true, "--filename-contains": true, "-filename-contains": true, "--ratings": true, "-ratings": true, "--properties": true, "-properties": true, "--save-state": true, "-save-state": true}, true)); err != nil {
		if done, code := finishFlagParse(set, err); done {
			fmt.Fprintln(stdout, usage)
			return code
		}
	}
	propertyFlag := false
	flagCount := 0
	set.Visit(func(f *flag.Flag) {
		flagCount++
		if f.Name == "properties" {
			propertyFlag = true
		}
	})
	if *listProperties {
		if set.NArg() != 0 || flagCount != 1 {
			return reportError(stderr, fmt.Errorf("--list-properties must be used alone"))
		}
		columns, err := selectedColumnRegistry()
		if err != nil {
			return reportError(stderr, err)
		}
		for _, column := range columns {
			if column.Index >= 5 {
				fmt.Fprintf(stdout, "%s\t%s\n", column.Property, column.Kind)
			}
		}
		return 0
	}
	if set.NArg() != 2 || (*noBackup && !*write) {
		return reportError(stderr, fmt.Errorf("%s", usage))
	}
	if err := validateSaveStateProfile(*saveState); err != nil {
		return reportError(stderr, err)
	}
	options, err := filters.options(set)
	if err != nil {
		return reportError(stderr, err)
	}
	var columns []selectedColumn
	if propertyFlag {
		columns, err = parseSelectedColumns(*properties)
		if err != nil {
			return reportError(stderr, err)
		}
	}
	ref, err := readSettingsReference(set.Arg(0))
	if err != nil {
		return reportError(stderr, err)
	}
	frames, err := DiscoverSequence(set.Arg(1))
	if err != nil {
		return reportError(stderr, err)
	}
	selected, err := filterSequenceFrames(frames, set.Arg(1), options)
	if err != nil {
		return reportError(stderr, err)
	}
	outputs, skipped, err := planCopySettingsColumns(frames, selected, ref, columns, *saveState)
	if err != nil {
		return reportError(stderr, err)
	}
	fmt.Fprintf(stdout, "Reference: %s\nSelected: %d/%d frames; reference files skipped: %d; changed sidecars: %d\n", set.Arg(0), len(selected), len(frames), skipped, len(outputs))
	if len(columns) == 0 {
		fmt.Fprintln(stdout, "Replace Camera Raw settings; merge other metadata, including missing capture information.")
	} else {
		fmt.Fprintf(stdout, "Copy %d registered column cache(s); missing source values clear cached cells.\nSource cache:\n", len(columns))
		nodes, err := pastePacketNodes(ref.Packet)
		if err != nil {
			return reportError(stderr, err)
		}
		for _, column := range columns {
			value, err := readSelectedColumn(nodes, column)
			if err != nil {
				return reportError(stderr, err)
			}
			if value.present {
				fmt.Fprintf(stdout, "  %s: %q\n", column.Property, value.value)
			} else {
				fmt.Fprintf(stdout, "  %s: remove\n", column.Property)
			}
		}
	}
	fmt.Fprintf(stdout, "Save state: %s\n", *saveState)
	for _, output := range outputs {
		fmt.Fprintln(stdout, output.Path)
		if *saveState == nativeSaveStateProfile {
			nodes, err := pastePacketNodes(output.Packet)
			if err != nil {
				return reportError(stderr, err)
			}
			wb := selectedField(nodes, pasteName{crsNamespace, "WhiteBalance"})
			crop := selectedField(nodes, pasteName{crsNamespace, "HasCrop"})
			quality := selectedField(nodes, pasteName{namespacePrefixes["lrt"], "Quality"})
			qualityText := ""
			if quality != nil {
				qualityText = quality.Value
			}
			fmt.Fprintf(stdout, "  Quality=%q; WhiteBalance=%q; HasCrop=%s\n", qualityText, wb.Value, crop.Value)
			rating, found, _ := output.Packet.GetRootScalar("xmp:Rating")
			if found {
				fmt.Fprintf(stdout, "  Rating=%q\n", rating)
			} else {
				fmt.Fprintln(stdout, "  Rating=(absent)")
			}
			for _, column := range columns {
				column.Kind = ""
				value, err := readSelectedColumn(nodes, column)
				if err != nil {
					return reportError(stderr, err)
				}
				if value.present {
					fmt.Fprintf(stdout, "  final %s: %q\n", column.Property, value.value)
				} else {
					fmt.Fprintf(stdout, "  final %s: absent\n", column.Property)
				}
			}
		}
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
