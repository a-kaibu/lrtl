package lrtl

import (
	"fmt"
	"io"
)

func unmarkSelectedKeyframe(packet *XMPPacket, force bool) error {
	owned, err := hasRootMetadataProperty(packet, "ol:KeyframeKind")
	if err != nil {
		return err
	}
	_, internal, err := captureTimestampScalar(packet, holyGrailKeyframeProperty)
	if err != nil {
		return err
	}
	rating, valid := packet.Rating()
	if internal || valid && (rating == 2 || rating == 3) {
		if owned {
			return fmt.Errorf("owned marker now has a Holy Grail mark; reset Holy Grail marks before removing the regular keyframe")
		}
		return nil
	}
	if !owned && !(force && valid && (rating == 4 || rating == 5)) {
		return nil
	}
	return UnmarkKeyframe(packet, force)
}

func runMarkSequence(arguments []string, stdout, stderr io.Writer, unmark bool) int {
	command := "mark-sequence"
	if unmark {
		command = "unmark-sequence"
	}
	set := newFlagSet(command, stderr)
	filters := registerFrameSelectionFlags(set)
	write := set.Bool("write", false, "save all validated XMP changes")
	noBackup := set.Bool("no-backup", false, "save existing sidecars without backups")
	kind := "default"
	force := false
	if unmark {
		set.BoolVar(&force, "force-rating", false, "also clear external 4- or 5-star ratings")
	} else {
		set.StringVar(&kind, "kind", "default", "default (4 stars) or crop (5 stars)")
	}
	if err := set.Parse(flagFirst(arguments, map[string]bool{"--kind": true, "-kind": true, "--selection": true, "-selection": true, "--filename-contains": true, "-filename-contains": true, "--ratings": true, "-ratings": true})); err != nil {
		if done, code := finishFlagParse(set, err); done {
			return code
		}
	}
	if set.NArg() != 1 || *noBackup && !*write || kind != "default" && kind != "crop" {
		return reportError(stderr, fmt.Errorf("Usage: lrtl %s DIRECTORY [selection flags] [--write] [--no-backup]; mark kind must be default or crop", command))
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
	outputs, err := planSelectedPacketEdits(frames, selected, func(packet *XMPPacket) error {
		if unmark {
			return unmarkSelectedKeyframe(packet, force)
		}
		_, err := MarkKeyframe(packet, kind)
		return err
	})
	if err != nil {
		return reportError(stderr, err)
	}
	fmt.Fprintf(stdout, "Selected: %d/%d frames; changed sidecars: %d\n", len(selected), len(frames), len(outputs))
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
