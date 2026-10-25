package lrtl

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/beevik/etree"
)

var maskEditPattern = regexp.MustCompile(`^(?:crs:)?(MaskGroupBasedCorrections|GradientBasedCorrections|CircularGradientBasedCorrections|PaintBasedCorrections)\[([1-9][0-9]*)\]\.(?:(?:crs:)?CorrectionMasks\[([1-9][0-9]*)\]\.)?(?:crs:)?([A-Za-z][A-Za-z0-9]*)$`)

type maskEditPath struct {
	collection            string
	correction, component int
	property              string
}

func parseMaskEditPath(value string) (maskEditPath, error) {
	matches := maskEditPattern.FindStringSubmatch(value)
	if matches == nil {
		return maskEditPath{}, fmt.Errorf("expected COLLECTION[N].PROPERTY or COLLECTION[N].CorrectionMasks[M].PROPERTY")
	}
	index, err := strconv.Atoi(matches[2])
	if err != nil || index > 4096 {
		return maskEditPath{}, fmt.Errorf("correction index must be between 1 and 4096")
	}
	component := 0
	if matches[3] != "" {
		component, err = strconv.Atoi(matches[3])
		if err != nil || component > 4096 {
			return maskEditPath{}, fmt.Errorf("mask component index must be between 1 and 4096")
		}
	}
	return maskEditPath{matches[1], index, component, matches[4]}, nil
}

func (path maskEditPath) String() string {
	base := fmt.Sprintf("%s[%d]", path.collection, path.correction)
	if path.component > 0 {
		base += fmt.Sprintf(".CorrectionMasks[%d]", path.component)
	}
	return base + "." + path.property
}

func protectedMaskCorrection(packet *XMPPacket, collection string, index int, correction *etree.Element) (bool, error) {
	name, _, err := crsElementValue(correction, "CorrectionName")
	if err != nil {
		return false, err
	}
	if strings.HasPrefix(strings.TrimSpace(name), "#LRT internal use") {
		return true, nil
	}
	if collection == "GradientBasedCorrections" {
		items := rdfArrayItems(packet.findRootProperty(crsNamespace, collection).element)
		if index > 0 && index <= len(items) && isLegacyLRTInternalGradient(items[index-1]) {
			return true, nil
		}
	}
	for _, property := range []string{holyGrailTargetProperty, deflickerTargetProperty} {
		value, found, err := packet.GetRootScalar(property)
		if err != nil {
			return false, err
		}
		if !found {
			continue
		}
		target, err := parseXMPExposureTarget(value)
		if err != nil {
			return false, err
		}
		if target.Collection == collection && target.Index == index {
			return true, nil
		}
	}
	return false, nil
}

func maskEditElement(packet *XMPPacket, path maskEditPath) (*etree.Element, error) {
	property := packet.findRootProperty(crsNamespace, path.collection)
	items := rdfArrayItems(property.element)
	if path.correction < 1 || path.correction > len(items) {
		return nil, fmt.Errorf("crs:%s[%d] does not exist; initialize missing slots with init", path.collection, path.correction)
	}
	correction := unwrapRDFArrayItem(items[path.correction-1])
	kind, found, err := crsElementValue(correction, "What")
	if err != nil {
		return nil, err
	}
	if !found || kind != "Correction" {
		return nil, fmt.Errorf("%s[%d] is not a Correction record", path.collection, path.correction)
	}
	internal, err := protectedMaskCorrection(packet, path.collection, path.correction, correction)
	if err != nil {
		return nil, err
	}
	if internal {
		return nil, fmt.Errorf("%s[%d] is an internal correction; use holy-grail/deflicker for managed corrections", path.collection, path.correction)
	}
	if path.component == 0 {
		return correction, nil
	}
	masks := rdfArrayItems(directCRSProperty(correction, "CorrectionMasks").element)
	if path.component < 1 || path.component > len(masks) {
		return nil, fmt.Errorf("%s[%d].CorrectionMasks[%d] does not exist", path.collection, path.correction, path.component)
	}
	mask := unwrapRDFArrayItem(masks[path.component-1])
	kind, found, err = crsElementValue(mask, "What")
	if err != nil {
		return nil, err
	}
	if !found || !strings.HasPrefix(kind, "Mask/") {
		return nil, fmt.Errorf("component %d is not a mask record", path.component)
	}
	return mask, nil
}

func maskEditBoolean(name string, component bool) bool {
	if !component {
		return name == "CorrectionActive"
	}
	return name == "MaskActive" || name == "MaskInverted" || name == "Flipped"
}

func maskEditSpec(element *etree.Element, path maskEditPath) (numericTransitionSpec, bool) {
	spec, registered := maskTransitionSpecs[path.property]
	if !registered {
		return spec, false
	}
	if path.component == 0 {
		return spec, localCorrectionProperty(path.property)
	}
	if path.property == "MaskValue" {
		return spec, true
	}
	kind, _, err := crsElementValue(element, "What")
	if err != nil {
		return spec, false
	}
	allowed := ""
	switch kind {
	case "Mask/Gradient":
		allowed = " ZeroX ZeroY FullX FullY "
	case "Mask/CircularGradient":
		allowed = " Left Top Right Bottom Angle Feather Midpoint Roundness "
	}
	return spec, strings.Contains(allowed, " "+path.property+" ")
}

func setMaskProperty(packet *XMPPacket, path maskEditPath, text string) (string, error) {
	element, err := maskEditElement(packet, path)
	if err != nil {
		return "", err
	}
	canonical := ""
	if maskEditBoolean(path.property, path.component > 0) {
		value, err := parseCameraRawBool(text)
		if err != nil {
			return "", err
		}
		canonical = strconv.FormatBool(value)
	} else {
		spec, allowed := maskEditSpec(element, path)
		if !allowed {
			return "", fmt.Errorf("unsupported numeric or boolean property for %s", path.String())
		}
		value, valid := scalarNumber(text)
		if !valid {
			return "", fmt.Errorf("mask value must be a finite number")
		}
		if value < spec.min || value > spec.max {
			return "", fmt.Errorf("%s must be between %g and %g (stored XMP units)", path.property, spec.min, spec.max)
		}
		if spec.integer && math.Trunc(value) != value {
			return "", fmt.Errorf("%s requires an integer", path.property)
		}
		canonical = formatNumber(value)
	}
	if err := setCRSElementValue(element, path.property, canonical); err != nil {
		return "", err
	}
	return canonical, nil
}

func runSetMaskProperty(image, sidecar, property, value string, dryRun, noBackup bool, stdout, stderr io.Writer) int {
	path, err := parseMaskEditPath(property)
	if err != nil {
		return reportError(stderr, err)
	}
	packet, err := PacketForImage(image, sidecar)
	if err != nil {
		return reportError(stderr, err)
	}
	canonical, err := setMaskProperty(packet, path, value)
	if err != nil {
		return reportError(stderr, err)
	}
	fmt.Fprintf(stdout, "%s: set %s=%s\n", filepath.Base(image), path.String(), canonical)
	if dryRun {
		fmt.Fprintf(stdout, "Dry run. Would write %s\n", sidecar)
		return 0
	}
	backup, err := WritePacket(sidecar, packet, !noBackup)
	if err != nil {
		return reportError(stderr, err)
	}
	if backup != "" {
		fmt.Fprintf(stdout, "Backup: %s\n", backup)
	}
	fmt.Fprintf(stdout, "Saved: %s\n", sidecar)
	return 0
}

type maskComponentInfo struct {
	Path       string            `json:"path"`
	Transition bool              `json:"transition"`
	Values     map[string]string `json:"values"`
}
type maskCorrectionInfo struct {
	Path       string              `json:"path"`
	Editable   bool                `json:"editable"`
	Transition bool                `json:"transition"`
	Values     map[string]string   `json:"values"`
	Components []maskComponentInfo `json:"components"`
}

func maskInfoValues(element *etree.Element, component bool) map[string]string {
	values := make(map[string]string)
	add := func(name, value string) {
		_, numeric := maskTransitionSpecs[name]
		allowed := numeric && (localCorrectionProperty(name) || component)
		if allowed || maskEditBoolean(name, component) || name == "What" || name == "CorrectionName" || name == "CorrectionSyncID" || name == "MaskName" || name == "MaskSyncID" {
			values[name] = value
		}
	}
	for _, attr := range element.Attr {
		if attr.NamespaceURI() == crsNamespace {
			add(attr.Key, attr.Value)
		}
	}
	for _, token := range element.Child {
		if child, ok := token.(*etree.Element); ok && child.NamespaceURI() == crsNamespace {
			if text, scalar := scalarElementText(child); scalar {
				add(child.Tag, text)
			}
		}
	}
	return values
}

func inspectMaskCorrections(packet *XMPPacket) ([]maskCorrectionInfo, error) {
	result := make([]maskCorrectionInfo, 0)
	for _, collection := range maskTransitionCollections {
		items := rdfArrayItems(packet.findRootProperty(crsNamespace, collection).element)
		for i, item := range items {
			correction := unwrapRDFArrayItem(item)
			internal, err := protectedMaskCorrection(packet, collection, i+1, correction)
			if err != nil {
				return nil, err
			}
			_, transition := maskTransitionCorrectionIndexes[collection][i+1]
			kind, _, err := crsElementValue(correction, "What")
			if err != nil {
				return nil, err
			}
			info := maskCorrectionInfo{Path: fmt.Sprintf("%s[%d]", collection, i+1), Editable: !internal && kind == "Correction", Transition: transition && !internal, Values: maskInfoValues(correction, false), Components: make([]maskComponentInfo, 0)}
			masks := rdfArrayItems(directCRSProperty(correction, "CorrectionMasks").element)
			for j, item := range masks {
				info.Components = append(info.Components, maskComponentInfo{Path: fmt.Sprintf("%s.CorrectionMasks[%d]", info.Path, j+1), Transition: info.Transition && j == 0, Values: maskInfoValues(unwrapRDFArrayItem(item), true)})
			}
			result = append(result, info)
		}
	}
	return result, nil
}

func runMasks(arguments []string, stdout, stderr io.Writer) int {
	set := newFlagSet("masks", stderr)
	jsonOutput := set.Bool("json", false, "print structured correction/component values")
	path, err := parseOnePath(set, flagFirst(arguments, nil))
	if err != nil {
		if done, code := finishFlagParse(set, err); done {
			return code
		}
		return reportError(stderr, err)
	}
	image, sidecar := inputPaths(path)
	imageExists, _ := osStat(image)
	sidecarExists, _ := osStat(sidecar)
	if !imageExists && !sidecarExists {
		return reportError(stderr, fmt.Errorf("image and XMP sidecar do not exist: %s", image))
	}
	packet, err := PacketForImage(image, sidecar)
	if err != nil {
		return reportError(stderr, err)
	}
	info, err := inspectMaskCorrections(packet)
	if err != nil {
		return reportError(stderr, err)
	}
	if *jsonOutput {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(info); err != nil {
			return reportError(stderr, err)
		}
		return 0
	}
	for _, correction := range info {
		fmt.Fprintf(stdout, "%s editable=%t transition=%t name=%q exposure=%s active=%q amount=%q\n", correction.Path, correction.Editable, correction.Transition, correction.Values["CorrectionName"], correction.Values["LocalExposure2012"], correction.Values["CorrectionActive"], correction.Values["CorrectionAmount"])
		for _, component := range correction.Components {
			fmt.Fprintf(stdout, "  %s transition=%t", component.Path, component.Transition)
			keys := make([]string, 0, len(component.Values))
			for key := range component.Values {
				if key != "MaskSyncID" {
					keys = append(keys, key)
				}
			}
			sort.Strings(keys)
			for _, key := range keys {
				fmt.Fprintf(stdout, " %s=%s", key, component.Values[key])
			}
			fmt.Fprintln(stdout)
		}
	}
	if len(info) == 0 {
		fmt.Fprintln(stdout, "No RDF-array correction slots found. Use init to create editable slots.")
	}
	return 0
}
