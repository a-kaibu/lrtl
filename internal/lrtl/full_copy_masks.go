package lrtl

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// LumaFrame owns its default correction identities. They are stable across
// copies and frames, and deliberately differ from the product's preset UUIDs.
func fullCopyMaskID(slot int) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("org.lumaframe/correction/v1/%d", slot)))
	return strings.ToUpper(hex.EncodeToString(digest[:16]))
}

func fullMaskField(node *pasteNode, name string) *pasteNode {
	if node == nil {
		return nil
	}
	return selectedField(node.Fields, pasteName{crsNamespace, name})
}

func fullMaskSet(node *pasteNode, name, value string) error {
	if node.Kind != "struct" {
		return fmt.Errorf("mask correction must be an RDF struct")
	}
	if field := fullMaskField(node, name); field != nil {
		if field.Kind != "scalar" && field.Kind != "uri" {
			return fmt.Errorf("crs:%s must be a scalar in mask preparation", name)
		}
		field.Value = value
	} else {
		node.Fields = append(node.Fields, &pasteNode{Name: pasteName{crsNamespace, name}, Prefix: "crs", Kind: "scalar", Value: value})
	}
	return nil
}

func fullMaskSetValues(node *pasteNode, values map[string]string) {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		_ = fullMaskSet(node, name, values[name])
	}
}

func fullMaskArray(nodes map[pasteName]*pasteNode, name string) (*pasteNode, error) {
	key := pasteName{crsNamespace, name}
	node := nodes[key]
	if node == nil {
		node = &pasteNode{Name: key, Prefix: "crs", Kind: "Seq"}
		nodes[key] = node
	}
	if node.Kind != "Seq" && node.Kind != "Bag" && node.Kind != "Alt" {
		return nil, fmt.Errorf("crs:%s must be an RDF array", name)
	}
	return node, nil
}

func newFullMaskCorrection(localDefaults bool) *pasteNode {
	node := &pasteNode{Name: pasteName{rdfNamespace, "li"}, Prefix: "rdf", Kind: "struct"}
	fullMaskSetValues(node, map[string]string{"What": "Correction", "CorrectionAmount": "1.0", "CorrectionActive": "True", "LocalExposure2012": "0.0"})
	if localDefaults {
		for _, name := range []string{"LocalTemperature", "LocalTint", "LocalHue", "LocalGrain", "LocalContrast2012", "LocalHighlights2012", "LocalShadows2012", "LocalWhites2012", "LocalBlacks2012", "LocalTexture", "LocalClarity2012", "LocalClarity", "LocalDehaze", "LocalSaturation", "LocalSharpness", "LocalLuminanceNoise", "LocalMoire", "LocalDefringe", "LocalToningSaturation"} {
			_ = fullMaskSet(node, name, "0.0")
		}
		_ = fullMaskSet(node, "LocalCurveRefineSaturation", "100.0")
		_ = fullMaskSet(node, "LocalToningHue", "230.0")
	}
	return node
}

func fullMaskRole(node *pasteNode, role int) bool {
	masks := fullMaskField(node, "CorrectionMasks")
	if masks == nil || len(masks.Items) == 0 {
		return false
	}
	y := fullMaskField(masks.Items[0], "ZeroY")
	return y != nil && (y.Kind == "scalar" || y.Kind == "uri") && strings.HasPrefix(y.Value, []string{"-0.41", "-0.31", "-0.21"}[role])
}

func ensureFullMask(node *pasteNode, slot int, radial bool) error {
	if node.Kind != "struct" {
		return fmt.Errorf("mask correction %d must be an RDF struct", slot)
	}
	masks := fullMaskField(node, "CorrectionMasks")
	if masks == nil {
		masks = &pasteNode{Name: pasteName{crsNamespace, "CorrectionMasks"}, Prefix: "crs", Kind: "Seq"}
		node.Fields = append(node.Fields, masks)
	}
	if masks.Kind != "Seq" && masks.Kind != "Bag" && masks.Kind != "Alt" {
		return fmt.Errorf("CorrectionMasks at %d must be an RDF array", slot)
	}
	if len(masks.Items) > 0 {
		return nil
	}
	mask := &pasteNode{Name: pasteName{rdfNamespace, "li"}, Prefix: "rdf", Kind: "struct"}
	values := map[string]string{"What": "Mask/Gradient", "MaskName": "LumaFrame: edit me", "MaskValue": "1.0", "ZeroX": "0.5", "FullX": "0.5"}
	if radial {
		values = map[string]string{"What": "Mask/CircularGradient", "MaskName": "LumaFrame: edit me", "MaskValue": "1.0", "Top": "0.2", "Bottom": "0.8", "Left": "0.0", "Right": "0.5", "Angle": "0.0", "Midpoint": "50", "Roundness": "0.0", "Feather": "50", "Flipped": "True", "Version": "2"}
		if slot == 9 {
			values["Left"], values["Right"] = "0.5", "1.0"
		}
	} else {
		values["ZeroY"] = []string{"0.5", "0.5", "0.8", "0.2", "-0.41", "-0.31", "-0.21"}[slot-1]
		values["FullY"] = []string{"0.2", "0.8", "0.9", "0.1", "-0.4", "-0.3", "-0.2"}[slot-1]
		if slot >= 5 {
			values["MaskName"] = legacyLRTInternalMaskName
		}
	}
	fullMaskSetValues(mask, values)
	masks.Items = append(masks.Items, mask)
	return nil
}

func insertFullCorrection(array *pasteNode, index int, node *pasteNode) {
	array.Items = append(array.Items, nil)
	copy(array.Items[index+1:], array.Items[index:])
	array.Items[index] = node
}

// The modern helper recognizes internal roles from the first mask's ZeroY
// prefix. Its repair can discard intervening entries to bring a later role
// forward, but excludes the last array item from that search. Preserve those
// measured rules, including their effect on user entries.
func prepareModernFullMasks(nodes map[pasteName]*pasteNode) error {
	array, err := fullMaskArray(nodes, "MaskGroupBasedCorrections")
	if err != nil {
		return err
	}
	for i := 0; i < 9; i++ {
		if i >= 4 && i <= 6 {
			role := i - 4
			matches := func() bool { return i < len(array.Items) && fullMaskRole(array.Items[i], role) }
			if !matches() {
				for j := i + 1; j < len(array.Items)-1; j++ {
					if fullMaskRole(array.Items[j], role) {
						array.Items = append(array.Items[:i], array.Items[j:]...)
						break
					}
				}
				if !matches() {
					insertFullCorrection(array, i, newFullMaskCorrection(false))
				}
			}
		}
		if i < 4 && i < len(array.Items) && fullMaskRole(array.Items[i], 0) {
			insertFullCorrection(array, i, newFullMaskCorrection(false))
		}
		if i >= len(array.Items) {
			array.Items = append(array.Items, newFullMaskCorrection(i < 4 || i >= 7))
		}
		if err := ensureFullMask(array.Items[i], i+1, i >= 7); err != nil {
			return err
		}
	}
	delete(nodes, pasteName{crsNamespace, "GradientBasedCorrections"})
	delete(nodes, pasteName{crsNamespace, "CircularGradientBasedCorrections"})
	for i := 0; i < 9; i++ {
		name := fmt.Sprintf("LumaFrame Mask %d", i+1)
		if i >= 4 && i <= 6 {
			name = "#LRT internal use (" + initializationRoles[i-4] + ")"
		} else if i >= 7 {
			name = fmt.Sprintf("LumaFrame Mask %d", i-2)
		}
		if err := fullMaskSet(array.Items[i], "CorrectionName", name); err != nil {
			return err
		}
		if err := fullMaskSet(array.Items[i], "CorrectionSyncID", fullCopyMaskID(i+1)); err != nil {
			return err
		}
		maskName := "LumaFrame: edit me"
		if i >= 4 && i <= 6 {
			maskName = legacyLRTInternalMaskName
		}
		masks := fullMaskField(array.Items[i], "CorrectionMasks")
		if err := fullMaskSet(masks.Items[0], "MaskName", maskName); err != nil {
			return err
		}
	}
	return nil
}

func prepareLegacyFullMasks(nodes map[pasteName]*pasteNode) error {
	array, err := fullMaskArray(nodes, "GradientBasedCorrections")
	if err != nil {
		return err
	}
	for i := 0; i < 7; i++ {
		if i >= len(array.Items) {
			array.Items = append(array.Items, newFullMaskCorrection(i < 4))
		}
		if err := ensureFullMask(array.Items[i], i+1, false); err != nil {
			return err
		}
	}
	circular, err := fullMaskArray(nodes, "CircularGradientBasedCorrections")
	if err != nil {
		return err
	}
	for len(circular.Items) < 2 {
		slot := len(circular.Items) + 8
		node := newFullMaskCorrection(true)
		if err := ensureFullMask(node, slot, true); err != nil {
			return err
		}
		rangeMask := &pasteNode{Name: pasteName{crsNamespace, "CorrectionRangeMask"}, Prefix: "crs", Kind: "struct"}
		fullMaskSetValues(rangeMask, map[string]string{"Version": "2", "Type": "2", "ColorAmount": "0.0", "LumMin": "0.0", "LumMax": "1.0", "LumFeather": "0.5", "DepthMin": "0.0", "DepthMax": "1.0", "DepthFeather": "0.5"})
		node.Fields = append(node.Fields, rangeMask)
		circular.Items = append(circular.Items, node)
	}
	delete(nodes, pasteName{crsNamespace, "MaskGroupBasedCorrections"})
	return nil
}

func prepareFullCopyMasks(packet *XMPPacket, format string) (bool, error) {
	before, after, err := saveStateMaps(packet)
	if err != nil {
		return false, err
	}
	switch format {
	case "modern":
		err = prepareModernFullMasks(after)
	case "legacy":
		err = prepareLegacyFullMasks(after)
	case "reload":
		err = prepareReloadFullMasks(after)
	default:
		return false, fmt.Errorf("unknown mask preparation format: %s", format)
	}
	if err != nil {
		return false, err
	}
	return writeChangedPasteRoots(packet, before, after)
}

// Default modern reload first completes populated legacy collections, joins
// their gradient/radial entries, and then applies modern role preparation.
func prepareReloadFullMasks(nodes map[pasteName]*pasteNode) error {
	gradient := nodes[pasteName{crsNamespace, "GradientBasedCorrections"}]
	circular := nodes[pasteName{crsNamespace, "CircularGradientBasedCorrections"}]
	for _, node := range []*pasteNode{gradient, circular} {
		if node != nil && node.Kind != "Seq" && node.Kind != "Bag" && node.Kind != "Alt" {
			return fmt.Errorf("legacy mask collection must be an RDF array")
		}
	}
	populated := gradient != nil && len(gradient.Items) > 0 || circular != nil && len(circular.Items) > 0
	if populated {
		// Reject the measured unsupported collection types and missing-gradient
		// inputs before mutation. The native text converter fails on these
		// inputs; do not return its partially repaired packet.
		if gradient == nil || gradient.Kind != "Seq" || circular != nil && circular.Kind != "Seq" {
			return fmt.Errorf("native legacy mask conversion requires a gradient Seq and ordered radial Seq")
		}
		if err := prepareLegacyFullMasks(nodes); err != nil {
			return err
		}
		gradient = nodes[pasteName{crsNamespace, "GradientBasedCorrections"}]
		circular = nodes[pasteName{crsNamespace, "CircularGradientBasedCorrections"}]
		modern := &pasteNode{Name: pasteName{crsNamespace, "MaskGroupBasedCorrections"}, Prefix: "crs", Kind: "Seq"}
		modern.Items = append(modern.Items, gradient.Items...)
		modern.Items = append(modern.Items, circular.Items...)
		nodes[modern.Name] = modern
	}
	return prepareModernFullMasks(nodes)
}
