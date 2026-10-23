package lrtl

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/beevik/etree"
)

type initializationChange struct {
	Frame        Frame
	Packet       *XMPPacket
	Format       string
	CreatedSlots int
}

var initializationRoles = []string{"HG", "Deflicker", "Global"}

func packetMaskFormat(packet *XMPPacket) string {
	for _, name := range []string{"MaskGroupBasedCorrections", "GradientBasedCorrections", "CircularGradientBasedCorrections", "PaintBasedCorrections"} {
		p := packet.findRootProperty(crsNamespace, name)
		if p.element != nil || p.attribute != nil {
			if name == "MaskGroupBasedCorrections" {
				return "modern"
			}
			return "legacy"
		}
	}
	return ""
}

func newMaskSyncID() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	return strings.ToUpper(hex.EncodeToString(id[:])), nil
}

func initializeMaskPacket(packet *XMPPacket, format string, ids [18]string) (int, error) {
	collection := "MaskGroupBasedCorrections"
	if format == "legacy" {
		collection = "GradientBasedCorrections"
	}
	for _, role := range initializationRoles {
		target := findLRTExposureTarget(packet, role, 5)
		if target.Collection != "" && target.Collection != collection {
			return 0, fmt.Errorf("existing %s internal correction uses %s; select its format or auto", role, target.Collection)
		}
	}
	_, container, err := ensurePacketCRSArray(packet, collection)
	if err != nil {
		return 0, err
	}
	items := rdfArrayItemsInContainer(container)
	// Existing named internal corrections may be referenced by other software.
	// Do not move them or create duplicate roles at a new position.
	for index, item := range items {
		name, found, err := crsElementValue(unwrapRDFArrayItem(item), "CorrectionName")
		if err != nil {
			return 0, err
		}
		if !found {
			continue
		}
		for roleIndex, role := range initializationRoles {
			if strings.TrimSpace(name) == "#LRT internal use ("+role+")" && index != roleIndex+4 {
				return 0, fmt.Errorf("%s internal %s is at position %d, expected %d; existing corrections were not moved", collection, role, index+1, roleIndex+5)
			}
		}
	}
	for index := 4; index < 7 && index < len(items); index++ {
		if rdfArrayItemIsEmpty(items[index]) {
			continue
		}
		name, _, err := crsElementValue(unwrapRDFArrayItem(items[index]), "CorrectionName")
		if err != nil {
			return 0, err
		}
		recognized := strings.TrimSpace(name) == "#LRT internal use ("+initializationRoles[index-4]+")"
		if format == "legacy" {
			recognized = recognized || isLegacyLRTInternalGradient(items[index])
		}
		if !recognized {
			return 0, fmt.Errorf("crs:%s[%d] contains an unrelated correction; cannot initialize reserved slot", collection, index+1)
		}
	}
	created := 0
	for slot := 1; slot <= 9; slot++ {
		scope, position := container, slot
		if format == "legacy" && slot >= 8 {
			_, scope, err = ensurePacketCRSArray(packet, "CircularGradientBasedCorrections")
			if err != nil {
				return 0, err
			}
			position = slot - 7
		}
		item, err := ensureRDFArrayPosition(scope, position, nil)
		if err != nil {
			return 0, err
		}
		internal := slot >= 5 && slot <= 7
		empty := rdfArrayItemIsEmpty(item)
		if !empty && !internal {
			continue
		}
		if empty {
			created++
		}
		correction := unwrapRDFArrayItem(item)
		defaults := map[string]string{"What": "Correction", "CorrectionAmount": "1", "CorrectionActive": "true", "LocalExposure2012": "0"}
		if format == "modern" {
			defaults["CorrectionSyncID"] = ids[slot-1]
		}
		if internal {
			defaults["CorrectionName"] = "#LRT internal use (" + initializationRoles[slot-5] + ")"
		} else {
			defaults["CorrectionName"] = fmt.Sprintf("LumaFrame Mask %d", slot)
			for _, name := range []string{"LocalTemperature", "LocalTint", "LocalHue", "LocalHighlights2012", "LocalShadows2012", "LocalWhites2012", "LocalBlacks2012", "LocalSaturation", "LocalContrast2012", "LocalClarity2012", "LocalTexture", "LocalDehaze", "LocalSharpness", "LocalBrightness", "LocalLuminanceNoise", "LocalMoire", "LocalDefringe", "LocalToningSaturation", "LocalGrain"} {
				defaults[name] = "0"
			}
			defaults["LocalToningHue"] = "230"
			defaults["LocalCurveRefineSaturation"] = "100"
		}
		if err := setMissingInitializationValues(correction, defaults); err != nil {
			return 0, err
		}
		property, err := ensureCRSArrayProperty(correction, "CorrectionMasks")
		if err != nil {
			return 0, err
		}
		masks, err := ensureRDFArrayContainer(property)
		if err != nil {
			return 0, err
		}
		if len(rdfArrayItemsInContainer(masks)) > 0 {
			continue
		}
		mask := createRDFResourceArrayItem(masks)
		values := map[string]string{"MaskActive": "true", "MaskValue": "1", "MaskSyncID": ids[slot+8], "What": "Mask/Gradient"}
		if internal {
			values["MaskName"] = legacyLRTInternalMaskName
			values["ZeroX"], values["FullX"] = "0.5", "0.5"
			// Fully applied side lies below the short gradient above the image.
			values["ZeroY"] = formatNumber(-.41 + float64(slot-5)*.1)
			values["FullY"] = formatNumber(-.4 + float64(slot-5)*.1)
		} else if slot <= 4 {
			values["MaskName"] = "LumaFrame: edit me"
			values["ZeroX"], values["FullX"] = "0.5", "0.5"
			values["ZeroY"] = []string{"0.5", "0.5", "0.8", "0.2"}[slot-1]
			values["FullY"] = []string{"0.2", "0.8", "0.9", "0.1"}[slot-1]
		} else {
			values["What"] = "Mask/CircularGradient"
			values["MaskName"] = "LumaFrame: edit me"
			values["Left"], values["Right"] = formatNumber(float64(slot-8)*.5), formatNumber(float64(slot-7)*.5)
			values["Top"], values["Bottom"], values["Angle"] = "0.2", "0.8", "0"
			values["Midpoint"], values["Roundness"], values["Feather"], values["Flipped"], values["Version"] = "50", "0", "50", "true", "2"
		}
		if err := setMissingInitializationValues(mask, values); err != nil {
			return 0, err
		}
	}
	return created, nil
}

func existingInitializationSlot(packet *XMPPacket, format string, slot int) *etree.Element {
	name, position := "MaskGroupBasedCorrections", slot
	if format == "legacy" {
		name = "GradientBasedCorrections"
		if slot >= 8 {
			name, position = "CircularGradientBasedCorrections", slot-7
		}
	}
	items := rdfArrayItems(packet.findRootProperty(crsNamespace, name).element)
	if position > len(items) {
		return nil
	}
	return unwrapRDFArrayItem(items[position-1])
}

func setMissingInitializationValues(element *etree.Element, values map[string]string) error {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		_, found, err := crsElementValue(element, key)
		if err != nil {
			return err
		}
		if !found {
			if err := setCRSElementValue(element, key, values[key]); err != nil {
				return err
			}
		}
	}
	return nil
}

func buildInitializationPlan(frames []Frame, format string, frameStateProfiles ...string) ([]initializationChange, error) {
	profile, err := copySaveStateProfile(frameStateProfiles)
	if err != nil {
		return nil, fmt.Errorf("--frame-state must be lrt-7.5.5 or preserve")
	}
	format = strings.ToLower(strings.TrimSpace(format))
	if format != "auto" && format != "modern" && format != "legacy" {
		return nil, fmt.Errorf("mask format must be auto, modern, or legacy")
	}
	if len(frames) == 0 {
		return nil, fmt.Errorf("sequence contains no images")
	}
	quality := ""
	packets := make([]*XMPPacket, len(frames))
	fallback := "modern"
	for i, frame := range frames {
		packet, err := PacketForFrame(frame)
		if err != nil {
			return nil, err
		}
		packets[i] = packet
	}
	if profile == nativeSaveStateProfile {
		quality, err = nativeSequenceQuality(frames[nativeFrameStateReference(packets)].Image)
		if err != nil {
			return nil, err
		}
	}
	for _, packet := range packets {
		if existing := packetMaskFormat(packet); existing != "" {
			fallback = existing
			break
		}
	}
	var ids [18]string
	for i := range ids {
		id, err := newMaskSyncID()
		if err != nil {
			return nil, err
		}
		ids[i] = id
	}
	formats := make([]string, len(frames))
	var adopted [18]bool
	for i, packet := range packets {
		selected := format
		if selected == "auto" {
			selected = packetMaskFormat(packet)
			if selected == "" {
				selected = fallback
			}
		}
		formats[i] = selected
		for slot := 1; slot <= 9; slot++ {
			correction := existingInitializationSlot(packet, selected, slot)
			if correction == nil {
				continue
			}
			for _, candidate := range []struct {
				element *etree.Element
				name    string
				index   int
			}{{correction, "CorrectionSyncID", slot - 1}, {nil, "MaskSyncID", slot + 8}} {
				if candidate.element == nil {
					masks := rdfArrayItems(directCRSProperty(correction, "CorrectionMasks").element)
					if len(masks) == 0 {
						continue
					}
					candidate.element = unwrapRDFArrayItem(masks[0])
				}
				value, found, err := crsElementValue(candidate.element, candidate.name)
				if err != nil {
					return nil, err
				}
				if found && strings.TrimSpace(value) != "" && !adopted[candidate.index] {
					ids[candidate.index], adopted[candidate.index] = value, true
				}
			}
		}
	}
	changes := make([]initializationChange, 0, len(frames))
	for i, frame := range frames {
		selected := formats[i]
		before, err := packets[i].Serialize()
		if err != nil {
			return nil, err
		}
		if err := prepareInitializationHeaders(packets[i]); err != nil {
			return nil, err
		}
		count, err := initializeMaskPacket(packets[i], selected, ids)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", frame.OutputSidecar(), err)
		}
		if profile == nativeSaveStateProfile {
			if err := prepareNativeFrameState(packets[i], quality); err != nil {
				return nil, fmt.Errorf("%s: %w", frame.OutputSidecar(), err)
			}
		}
		after, err := packets[i].Serialize()
		if err != nil {
			return nil, err
		}
		if string(before) != string(after) {
			changes = append(changes, initializationChange{frame, packets[i], selected, count})
		}
	}
	return changes, nil
}

func runInitialize(arguments []string, stdout, stderr io.Writer) int {
	set := newFlagSet("init", stderr)
	write := set.Bool("write", false, "write initialized XMP sidecars")
	noBackup := set.Bool("no-backup", false, "do not back up existing sidecars")
	format := set.String("mask-format", "auto", "mask structure: auto, modern, or legacy")
	frameState := set.String("frame-state", nativeSaveStateProfile, "frame quality, registered-column serialization and native save preparation: lrt-7.5.5 or preserve")
	if err := set.Parse(flagFirst(arguments, map[string]bool{"--mask-format": true, "-mask-format": true, "--frame-state": true, "-frame-state": true})); err != nil {
		if done, code := finishFlagParse(set, err); done {
			return code
		}
	}
	if set.NArg() != 1 {
		return reportError(stderr, fmt.Errorf("expected one sequence directory"))
	}
	if *noBackup && !*write {
		return reportError(stderr, fmt.Errorf("--no-backup requires --write"))
	}
	frames, err := DiscoverSequence(set.Arg(0))
	if err != nil {
		return reportError(stderr, err)
	}
	changes, err := buildInitializationPlan(frames, *format, *frameState)
	if err != nil {
		return reportError(stderr, err)
	}
	fmt.Fprintf(stdout, "Frames: %d; sidecars to initialize: %d\n", len(frames), len(changes))
	fmt.Fprintf(stdout, "Frame state: %s\n", *frameState)
	for _, change := range changes {
		fmt.Fprintf(stdout, "  %s: %s, new correction slots: %d\n", filepath.Base(change.Frame.Image), change.Format, change.CreatedSlots)
		if *frameState == nativeSaveStateProfile {
			quality, _, _ := change.Packet.GetRootScalar("lrt:Quality")
			rating, _, _ := change.Packet.GetRootScalar("xmp:Rating")
			fmt.Fprintf(stdout, "    Quality=%q; Rating=%q\n", quality, rating)
			writeWBEditPreview(stdout, change.Packet)
		}
	}
	if !*write {
		fmt.Fprintln(stdout, "Preview only. Pass --write to save XMP sidecars.")
		return 0
	}
	for _, change := range changes {
		backup, err := WritePacket(change.Frame.OutputSidecar(), change.Packet, !*noBackup)
		if err != nil {
			return reportError(stderr, err)
		}
		if backup != "" {
			fmt.Fprintf(stdout, "Backup: %s\n", backup)
		}
		fmt.Fprintf(stdout, "Saved: %s\n", change.Frame.OutputSidecar())
	}
	return 0
}

func prepareInitializationHeaders(packet *XMPPacket) error {
	before, after, err := saveStateMaps(packet)
	if err != nil {
		return err
	}
	for _, header := range [][2]string{{"HasSettings", "True"}, {"ProcessVersion", "11"}, {"Version", "14"}} {
		existing, err := saveStateScalar(after[pasteName{crsNamespace, header[0]}], "crs:"+header[0])
		if err != nil {
			return err
		}
		if !existing.present {
			if err := setSaveStateScalar(after, header[0], header[1]); err != nil {
				return err
			}
		}
	}
	_, err = writeChangedPasteRoots(packet, before, after)
	return err
}
