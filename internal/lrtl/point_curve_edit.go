package lrtl

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/beevik/etree"
)

type pointCurveTarget struct {
	name, label string
	parent      *etree.Element
	property    topLevelXMPProperty
	equalized   bool
}

func uniqueCurveProperty(parents []*etree.Element, name string) error {
	count := 0
	for _, parent := range parents {
		for i := range parent.Attr {
			if attributeMatches(&parent.Attr[i], crsNamespace, name) {
				count++
			}
		}
		for _, token := range parent.Child {
			if child, ok := token.(*etree.Element); ok && child.NamespaceURI() == crsNamespace && child.Tag == name {
				count++
			}
		}
	}
	if count > 1 {
		return fmt.Errorf("ambiguous duplicate curve property crs:%s", name)
	}
	return nil
}

func resolvePointCurveTarget(packet *XMPPacket, channel, mask string) (pointCurveTarget, error) {
	primary, fallback, err := toneCurveChannelProperties(channel)
	if err != nil {
		return pointCurveTarget{}, err
	}
	target := pointCurveTarget{name: primary}
	if mask == "" {
		target.property = packet.findRootProperty(crsNamespace, primary)
		if target.property.attribute == nil && target.property.element == nil {
			other := packet.findRootProperty(crsNamespace, fallback)
			if other.attribute != nil || other.element != nil {
				target.name, target.property = fallback, other
			}
		}
		target.parent = packet.rootDescriptions()[0]
		if err := uniqueCurveProperty(packet.rootDescriptions(), target.name); err != nil {
			return target, err
		}
		for _, name := range firstFrameEqualizedProperties {
			if name == target.name {
				target.equalized = true
			}
		}
		target.label = "crs:" + target.name
		return target, nil
	}
	local := map[string]string{"master": "MainCurve", "red": "RedCurve", "green": "GreenCurve", "blue": "BlueCurve"}[channel]
	path, err := parseMaskEditPath(mask + "." + local)
	if err != nil {
		return target, err
	}
	if path.component != 0 {
		return target, fmt.Errorf("--mask must select a correction record, not a selector component")
	}
	parent, err := maskEditElement(packet, path)
	if err != nil {
		return target, err
	}
	target.name, target.label, target.parent = local, path.String(), parent
	target.property = directCRSProperty(parent, local)
	if err := uniqueCurveProperty([]*etree.Element{parent}, local); err != nil {
		return target, err
	}
	if path.collection == "MaskGroupBasedCorrections" {
		_, target.equalized = maskTransitionCorrectionIndexes[path.collection][path.correction]
	}
	return target, nil
}

func readPointCurveTarget(target pointCurveTarget) ([]toneCurvePoint, bool, bool, error) {
	property := target.property
	if property.attribute == nil && property.element == nil {
		return []toneCurvePoint{{0, 0}, {255, 255}}, false, true, nil
	}
	points, err := parseXMPToneCurveProperty(target.name, property.element, property.attribute)
	if err != nil {
		return nil, true, false, err
	}
	editable := property.attribute != nil
	if property.element != nil {
		_, editable = scalarElementText(property.element)
		if !editable {
			editable = toneCurveSequenceElement(property.element) != nil
		}
	}
	editable = editable && len(points) <= 64 && points[0].x == 0 && points[len(points)-1].x == 255
	return points, true, editable, nil
}

func parsePointCurveEdit(text string) ([]toneCurvePoint, error) {
	if len(text) > 4096 {
		return nil, fmt.Errorf("curve point list is too long (maximum 4096 bytes)")
	}
	points, err := parseToneCurveValues([]string{text})
	if err != nil {
		return nil, err
	}
	if len(points) > 64 || points[0].x != 0 || points[len(points)-1].x != 255 {
		return nil, fmt.Errorf("curve requires 2 to 64 integer points with x endpoints 0 and 255")
	}
	return points, nil
}

func writePointCurveTarget(target pointCurveTarget, points []toneCurvePoint) error {
	property := target.property
	if property.attribute == nil && property.element == nil {
		prefix := ensurePrefix(target.parent, "crs", crsNamespace)
		element := target.parent.CreateElement(prefix + ":" + target.name)
		rdfPrefix := ensurePrefix(element, "rdf", rdfNamespace)
		element.CreateElement(rdfPrefix + ":Seq")
		property = topLevelXMPProperty{description: target.parent, element: element}
	}
	return setExistingToneCurvePoints(property, target.name, points)
}

func planPointCurveEdits(frames []Frame, channel, mask string, points []toneCurvePoint, mark bool) ([]packetOutput, error) {
	outputs := make([]packetOutput, 0, len(frames))
	for _, frame := range frames {
		packet, err := PacketForFrame(frame)
		if err != nil {
			return nil, err
		}
		before, err := packet.Serialize()
		if err != nil {
			return nil, err
		}
		target, err := resolvePointCurveTarget(packet, channel, mask)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", frame.Image, err)
		}
		current, found, editable, err := readPointCurveTarget(target)
		if err != nil {
			return nil, err
		}
		if !editable {
			return nil, fmt.Errorf("%s %s: unsupported curve representation or point domain", frame.Image, target.label)
		}
		same := found && len(current) == len(points)
		if same {
			for i := range points {
				if current[i] != points[i] {
					same = false
					break
				}
			}
		}
		if !same {
			if err := writePointCurveTarget(target, points); err != nil {
				return nil, err
			}
		}
		if mark {
			if _, err := MarkKeyframe(packet, "default"); err != nil {
				return nil, err
			}
		}
		after, err := packet.Serialize()
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(before, after) {
			outputs = append(outputs, packetOutput{frame.OutputSidecar(), packet})
		}
	}
	return outputs, nil
}

type pointCurveJSONPoint struct {
	X int `json:"x"`
	Y int `json:"y"`
}
type pointCurveInfo struct {
	Image     string                `json:"image"`
	Channel   string                `json:"channel"`
	Property  string                `json:"property"`
	Found     bool                  `json:"found"`
	Editable  bool                  `json:"editable"`
	Equalized bool                  `json:"equalized"`
	Points    []pointCurveJSONPoint `json:"points"`
}

func inspectPointCurves(frames []Frame, channel, mask string) ([]pointCurveInfo, error) {
	rows := make([]pointCurveInfo, 0, len(frames))
	for _, frame := range frames {
		packet, err := PacketForFrame(frame)
		if err != nil {
			return nil, err
		}
		target, err := resolvePointCurveTarget(packet, channel, mask)
		if err != nil {
			return nil, err
		}
		points, found, editable, err := readPointCurveTarget(target)
		if err != nil {
			return nil, err
		}
		row := pointCurveInfo{Image: frame.Image, Channel: channel, Property: target.label, Found: found, Editable: editable, Equalized: target.equalized, Points: make([]pointCurveJSONPoint, len(points))}
		for i, point := range points {
			row.Points[i] = pointCurveJSONPoint{int(point.x), int(point.y)}
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func runPointCurve(arguments []string, stdout, stderr io.Writer) int {
	set := newFlagSet("curve", stderr)
	channel := set.String("channel", "master", "curve channel: master, red, green, or blue")
	mask := set.String("mask", "", "existing local correction, e.g. MaskGroupBasedCorrections[1]")
	text := set.String("points", "", "ordered 0..255 integer points, e.g. '0,0;128,96;255,255'")
	linear := set.Bool("linear", false, "write the identity curve 0,0;255,255")
	mark := set.Bool("mark", false, "also mark one image as a 4-star develop keyframe")
	dryRun := set.Bool("dry-run", false, "preview edits without writing")
	noBackup := set.Bool("no-backup", false, "do not create backups")
	jsonOutput := set.Bool("json", false, "inspect current points as JSON")
	path, err := parseOnePath(set, flagFirst(arguments, map[string]bool{"--channel": true, "-channel": true, "--mask": true, "-mask": true, "--points": true, "-points": true}))
	if err != nil {
		if done, code := finishFlagParse(set, err); done {
			return code
		}
		return reportError(stderr, err)
	}
	provided := false
	set.Visit(func(f *flag.Flag) {
		if f.Name == "points" {
			provided = true
		}
	})
	editing := provided || *linear
	if _, _, err := toneCurveChannelProperties(*channel); err != nil {
		return reportError(stderr, err)
	}
	if provided && *linear {
		return reportError(stderr, fmt.Errorf("--points cannot be combined with --linear"))
	}
	if editing && *jsonOutput {
		return reportError(stderr, fmt.Errorf("--json is for inspection only"))
	}
	if !editing && (*mark || *dryRun || *noBackup) {
		return reportError(stderr, fmt.Errorf("--mark, --dry-run and --no-backup require --points or --linear"))
	}
	frames, directory, err := metadataInputFrames(path)
	if err != nil {
		return reportError(stderr, err)
	}
	if directory && *mark {
		return reportError(stderr, fmt.Errorf("--mark requires a single image or XMP file"))
	}
	if !editing {
		rows, err := inspectPointCurves(frames, *channel, *mask)
		if err != nil {
			return reportError(stderr, err)
		}
		if *jsonOutput {
			encoder := json.NewEncoder(stdout)
			encoder.SetIndent("", "  ")
			if err := encoder.Encode(rows); err != nil {
				return reportError(stderr, err)
			}
		} else {
			for _, row := range rows {
				pairs := make([]string, len(row.Points))
				for i, p := range row.Points {
					pairs[i] = fmt.Sprintf("%d,%d", p.X, p.Y)
				}
				fmt.Fprintf(stdout, "%s %s: found=%t editable=%t equalized=%t points=%s\n", row.Image, row.Property, row.Found, row.Editable, row.Equalized, strings.Join(pairs, ";"))
			}
		}
		return 0
	}
	points := []toneCurvePoint{{0, 0}, {255, 255}}
	if provided {
		points, err = parsePointCurveEdit(*text)
		if err != nil {
			return reportError(stderr, err)
		}
	}
	outputs, err := planPointCurveEdits(frames, *channel, *mask, points, *mark)
	if err != nil {
		return reportError(stderr, err)
	}
	fmt.Fprintf(stdout, "Frames: %d; curve changes: %d\n", len(frames), len(outputs))
	pairs := make([]string, len(points))
	for i, point := range points {
		pairs[i] = fmt.Sprintf("%d,%d", int(point.x), int(point.y))
	}
	for _, output := range outputs {
		target, err := resolvePointCurveTarget(output.Packet, *channel, *mask)
		if err != nil {
			return reportError(stderr, err)
		}
		fmt.Fprintf(stdout, "  %s: %s points=%s\n", output.Path, target.label, strings.Join(pairs, ";"))
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
