package lrtl

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
)

type cropEditOptions struct {
	rectangle           *[4]float64
	angle               *float64
	enable, clear, mark bool
}

// Only the crop fields and an explicitly requested keyframe mark are changed.
// Packet instances belong to this plan; no files are written during preflight.
func planCropEdits(frames []Frame, options cropEditOptions) ([]packetOutput, error) {
	if options.clear && (options.rectangle != nil || options.angle != nil || options.enable || options.mark) {
		return nil, fmt.Errorf("--clear cannot be combined with crop edits or --mark")
	}
	if options.rectangle != nil {
		r := *options.rectangle
		for _, value := range r {
			if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 1 {
				return nil, fmt.Errorf("crop coordinates must be finite numbers between 0 and 1")
			}
		}
		if r[0] >= r[2] || r[1] >= r[3] {
			return nil, fmt.Errorf("crop rectangle requires left < right and top < bottom")
		}
	}
	if options.angle != nil && (math.IsNaN(*options.angle) || math.IsInf(*options.angle, 0) || *options.angle < -45 || *options.angle > 45) {
		return nil, fmt.Errorf("crop angle must be between -45 and 45 degrees")
	}
	outputs := make([]packetOutput, 0, len(frames))
	for _, frame := range frames {
		packet, err := PacketForFrame(frame)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", frame.Image, err)
		}
		before, err := packet.Serialize()
		if err != nil {
			return nil, err
		}
		if options.clear || options.rectangle != nil {
			rectangle := [4]float64{0, 0, 1, 1}
			if options.rectangle != nil {
				rectangle = *options.rectangle
			}
			for i, name := range []string{"CropLeft", "CropTop", "CropRight", "CropBottom"} {
				if err := packet.SetScalar("crs:"+name, formatNumber(rectangle[i])); err != nil {
					return nil, err
				}
			}
		}
		if options.clear || options.angle != nil {
			angle := 0.0
			if options.angle != nil {
				angle = *options.angle
			}
			if err := packet.SetScalar("crs:CropAngle", formatNumber(angle)); err != nil {
				return nil, err
			}
		}
		if options.clear || options.enable || options.rectangle != nil || options.angle != nil {
			active := "True"
			if options.clear {
				active = "False"
			}
			if err := packet.SetScalar("crs:HasCrop", active); err != nil {
				return nil, err
			}
		}
		if options.mark {
			if _, err := MarkKeyframe(packet, "crop"); err != nil {
				return nil, fmt.Errorf("%s: %w", frame.Image, err)
			}
		}
		// Validate the complete candidate, including coordinates retained from XMP.
		if _, err := readCropMetadata(packet); err != nil {
			return nil, fmt.Errorf("%s: %w", frame.Image, err)
		}
		after, err := packet.Serialize()
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(before, after) {
			outputs = append(outputs, packetOutput{Path: frame.OutputSidecar(), Packet: packet})
		}
	}
	return outputs, nil
}

type cropMetadata struct {
	Left   float64 `json:"left"`
	Top    float64 `json:"top"`
	Right  float64 `json:"right"`
	Bottom float64 `json:"bottom"`
	Angle  float64 `json:"angle"`
	Active bool    `json:"active"`
}

func readCropMetadata(packet *XMPPacket) (cropMetadata, error) {
	crop := cropMetadata{Right: 1, Bottom: 1}
	for _, field := range []struct {
		name string
		dest *float64
	}{{"CropLeft", &crop.Left}, {"CropTop", &crop.Top}, {"CropRight", &crop.Right}, {"CropBottom", &crop.Bottom}, {"CropAngle", &crop.Angle}} {
		text, found, err := packet.GetScalar("crs:" + field.name)
		if err != nil {
			return crop, err
		}
		if found {
			value, valid := scalarNumber(text)
			if !valid {
				return crop, fmt.Errorf("crs:%s must be a finite number", field.name)
			}
			*field.dest = value
		}
	}
	for _, v := range []float64{crop.Left, crop.Top, crop.Right, crop.Bottom} {
		if v < 0 || v > 1 {
			return crop, fmt.Errorf("XMP crop bounds must be between 0 and 1")
		}
	}
	if crop.Left >= crop.Right || crop.Top >= crop.Bottom {
		return crop, fmt.Errorf("XMP crop rectangle is empty or inverted")
	}
	text, found, err := packet.GetScalar("crs:HasCrop")
	if err != nil {
		return crop, err
	}
	if found {
		crop.Active, err = parseCameraRawBool(text)
		if err != nil {
			return crop, err
		}
	} else {
		crop.Active = crop.Left != 0 || crop.Top != 0 || crop.Right != 1 || crop.Bottom != 1 || crop.Angle != 0
	}
	return crop, nil
}

func runCrop(arguments []string, stdout, stderr io.Writer) int {
	set := newFlagSet("crop", stderr)
	rectangleText := set.String("rect", "", "normalized left,top,right,bottom (0..1)")
	angle := set.Float64("angle", 0, "clockwise XMP crop angle (-45..45 degrees)")
	enable := set.Bool("enable", false, "enable existing crop bounds without changing them")
	clear := set.Bool("clear", false, "restore full-frame bounds and disable crop; keep ratings")
	mark := set.Bool("mark", false, "also mark this image as a 5-star crop keyframe")
	dryRun := set.Bool("dry-run", false, "preview edits without writing")
	noBackup := set.Bool("no-backup", false, "do not create sidecar backups")
	jsonOutput := set.Bool("json", false, "inspect current crop values as JSON")
	path, err := parseOnePath(set, flagFirst(arguments, map[string]bool{"--rect": true, "-rect": true, "--angle": true, "-angle": true}))
	if err != nil {
		if done, code := finishFlagParse(set, err); done {
			return code
		}
		return reportError(stderr, err)
	}
	options := cropEditOptions{enable: *enable, clear: *clear, mark: *mark}
	rectangleProvided := false
	set.Visit(func(f *flag.Flag) {
		if f.Name == "angle" {
			options.angle = angle
		}
		if f.Name == "rect" {
			rectangleProvided = true
		}
	})
	if rectangleProvided {
		pieces := strings.Split(*rectangleText, ",")
		if len(pieces) != 4 {
			return reportError(stderr, fmt.Errorf("--rect requires left,top,right,bottom"))
		}
		r := [4]float64{}
		for i, piece := range pieces {
			r[i], err = strconv.ParseFloat(strings.TrimSpace(piece), 64)
			if err != nil {
				return reportError(stderr, fmt.Errorf("invalid crop coordinate: %s", piece))
			}
		}
		options.rectangle = &r
	}
	editing := options.rectangle != nil || options.angle != nil || *enable || *clear || *mark
	if *jsonOutput && editing {
		return reportError(stderr, fmt.Errorf("--json is for inspection and cannot be combined with edits"))
	}
	if !editing && (*dryRun || *noBackup) {
		return reportError(stderr, fmt.Errorf("--dry-run and --no-backup require a crop edit"))
	}
	frames, directory, err := metadataInputFrames(path)
	if err != nil {
		return reportError(stderr, err)
	}
	if directory && *mark {
		return reportError(stderr, fmt.Errorf("--mark requires a single image or XMP file"))
	}
	if !editing {
		type row struct {
			Image string       `json:"image"`
			Crop  cropMetadata `json:"crop"`
		}
		rows := make([]row, 0, len(frames))
		for _, frame := range frames {
			packet, err := PacketForFrame(frame)
			if err != nil {
				return reportError(stderr, err)
			}
			crop, err := readCropMetadata(packet)
			if err != nil {
				return reportError(stderr, fmt.Errorf("%s: %w", frame.Image, err))
			}
			rows = append(rows, row{frame.Image, crop})
		}
		if *jsonOutput {
			encoder := json.NewEncoder(stdout)
			encoder.SetIndent("", "  ")
			if err := encoder.Encode(rows); err != nil {
				return reportError(stderr, err)
			}
		} else {
			for _, row := range rows {
				fmt.Fprintf(stdout, "%s: active=%t rect=%g,%g,%g,%g angle=%g\n", row.Image, row.Crop.Active, row.Crop.Left, row.Crop.Top, row.Crop.Right, row.Crop.Bottom, row.Crop.Angle)
			}
		}
		return 0
	}
	outputs, err := planCropEdits(frames, options)
	if err != nil {
		return reportError(stderr, err)
	}
	fmt.Fprintf(stdout, "Frames: %d; crop changes: %d\n", len(frames), len(outputs))
	for _, output := range outputs {
		crop, _ := readCropMetadata(output.Packet)
		fmt.Fprintf(stdout, "  %s: active=%t rect=%g,%g,%g,%g angle=%g\n", output.Path, crop.Active, crop.Left, crop.Top, crop.Right, crop.Bottom, crop.Angle)
	}
	if *dryRun {
		fmt.Fprintln(stdout, "Dry run. No sidecars written.")
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
		fmt.Fprintf(stdout, "Saved: %s\n", output.Path)
	}
	return 0
}
