package lrtl

import (
	"fmt"
)

const nativeSaveStateProfile = "lrt-7.5.5"

var saveStateNumberNames = []string{"CropTop", "CropLeft", "CropBottom", "CropRight", "CropAngle", "Temperature", "Tint", "IncrementalTemperature", "IncrementalTint"}
var saveStateCropDefaults = map[string]float64{"CropTop": 0, "CropLeft": 0, "CropBottom": 1, "CropRight": 1, "CropAngle": 0}

type saveStateNumber struct {
	Serialized transitionScalar
	Raw        float64
}

// Cache values must survive six-decimal XML serialization. A tiny nonzero
// crop may serialize to zero while its original cached value still enables it.
type nativeSaveStateContext struct {
	Quality string
	Numbers map[string]saveStateNumber
}

func validateSaveStateProfile(profile string) error {
	if profile != nativeSaveStateProfile && profile != "preserve" {
		return fmt.Errorf("--save-state must be lrt-7.5.5 or preserve")
	}
	return nil
}

func saveStateScalar(node *pasteNode, property string) (transitionScalar, error) {
	if node == nil {
		return transitionScalar{}, nil
	}
	if node.Kind != "scalar" && node.Kind != "uri" {
		return transitionScalar{}, fmt.Errorf("%s must be a scalar for native save-state preparation", property)
	}
	return transitionScalar{node.Value, true}, nil
}

func saveStateReadNumber(nodes []*pasteNode, name string) (saveStateNumber, error) {
	value, err := saveStateScalar(selectedField(nodes, pasteName{crsNamespace, name}), "crs:"+name)
	if err != nil {
		return saveStateNumber{}, err
	}
	if !value.present {
		return saveStateNumber{}, nil
	}
	columns, err := parseSelectedColumns(name)
	if err != nil {
		return saveStateNumber{}, err
	}
	raw, _ := selectedJavaDouble(value.value)
	return saveStateNumber{normalizeSelectedColumn(columns[0], value), raw}, nil
}

func readNativeSaveStateContext(packet *XMPPacket) (nativeSaveStateContext, error) {
	nodes, err := pastePacketNodes(packet)
	if err != nil {
		return nativeSaveStateContext{}, err
	}
	quality, err := saveStateScalar(selectedField(nodes, pasteName{namespacePrefixes["lrt"], "Quality"}), "lrt:Quality")
	if err != nil {
		return nativeSaveStateContext{}, err
	}
	context := nativeSaveStateContext{Quality: quality.value, Numbers: map[string]saveStateNumber{}}
	for _, name := range saveStateNumberNames {
		value, err := saveStateReadNumber(nodes, name)
		if err != nil {
			return nativeSaveStateContext{}, err
		}
		context.Numbers[name] = value
	}
	return context, nil
}

func copiedNativeSaveStateContext(target, source *XMPPacket, columns []selectedColumn) (nativeSaveStateContext, error) {
	context, err := readNativeSaveStateContext(target)
	if err != nil {
		return nativeSaveStateContext{}, err
	}
	nodes, err := pastePacketNodes(source)
	if err != nil {
		return nativeSaveStateContext{}, err
	}
	for _, column := range columns {
		if len(column.Steps) != 1 || column.Steps[0].Name.URI != crsNamespace {
			continue
		}
		name := column.Steps[0].Name.Local
		if _, tracked := context.Numbers[name]; !tracked {
			continue
		}
		value, err := saveStateReadNumber(nodes, name)
		if err != nil {
			return nativeSaveStateContext{}, err
		}
		context.Numbers[name] = value
	}
	// Cell assignment updates the quality column but does not reinitialize the
	// frame's separate quality cache. Full paste uses a newly read context instead.
	return context, nil
}

func (context nativeSaveStateContext) cropActive() bool {
	for name, fallback := range saveStateCropDefaults {
		value := context.Numbers[name]
		if value.Serialized.present {
			if value.Raw != fallback {
				return true
			}
		}
	}
	return false
}

func saveStateMaps(packet *XMPPacket) (map[pasteName]*pasteNode, map[pasteName]*pasteNode, error) {
	nodes, err := pastePacketNodes(packet)
	if err != nil {
		return nil, nil, err
	}
	before, after := map[pasteName]*pasteNode{}, map[pasteName]*pasteNode{}
	for _, node := range nodes {
		before[node.Name] = node
		after[node.Name] = clonePasteNode(node)
	}
	return before, after, nil
}

func setSaveStateScalar(nodes map[pasteName]*pasteNode, name, value string) error {
	return setSaveStateProperty(nodes, pasteName{crsNamespace, name}, "crs", value)
}

func setSaveStateProperty(nodes map[pasteName]*pasteNode, key pasteName, prefix, value string) error {
	node := nodes[key]
	if node == nil {
		node = &pasteNode{Name: key, Prefix: prefix, Kind: "scalar"}
		nodes[key] = node
	}
	if _, err := saveStateScalar(node, prefix+":"+key.Local); err != nil {
		return err
	}
	node.Value = value
	return nil
}

func prepareWhiteBalanceNodes(nodes map[pasteName]*pasteNode, quality string) error {
	key := pasteName{crsNamespace, "WhiteBalance"}
	mode, err := saveStateScalar(nodes[key], "crs:WhiteBalance")
	if err != nil {
		return err
	}
	if !mode.present {
		mode = transitionScalar{"As Shot", true}
		if err := setSaveStateScalar(nodes, "WhiteBalance", mode.value); err != nil {
			return err
		}
	}
	remove := []string{"IncrementalTemperature", "IncrementalTint"}
	if mode.value == "As Shot" {
		remove = []string{"Temperature", "Tint", "IncrementalTemperature", "IncrementalTint"}
	} else if quality == "FLAT" {
		remove = []string{"Temperature", "Tint"}
	}
	for _, name := range remove {
		delete(nodes, pasteName{crsNamespace, name})
	}
	return nil
}

func applyNativeWhiteBalanceCleanup(packet *XMPPacket, quality string) (bool, error) {
	before, after, err := saveStateMaps(packet)
	if err != nil {
		return false, err
	}
	if err := prepareWhiteBalanceNodes(after, quality); err != nil {
		return false, err
	}
	return writeChangedPasteRoots(packet, before, after)
}

func applyNativeSaveState(packet *XMPPacket, context nativeSaveStateContext) (bool, error) {
	before, after, err := saveStateMaps(packet)
	if err != nil {
		return false, err
	}
	for _, name := range saveStateNumberNames {
		value := context.Numbers[name].Serialized
		if !value.present {
			delete(after, pasteName{crsNamespace, name})
			continue
		}
		if err := setSaveStateScalar(after, name, value.value); err != nil {
			return false, err
		}
	}
	active := "False"
	if context.cropActive() {
		active = "True"
	}
	if err := setSaveStateScalar(after, "HasCrop", active); err != nil {
		return false, err
	}
	if context.Quality != "" {
		if err := setSaveStateProperty(after, pasteName{namespacePrefixes["lrt"], "Quality"}, "lrt", context.Quality); err != nil {
			return false, err
		}
	}
	if err := prepareWhiteBalanceNodes(after, context.Quality); err != nil {
		return false, err
	}
	return writeChangedPasteRoots(packet, before, after)
}

func copySaveStateProfile(arguments []string) (string, error) {
	// Kept internal so callers planning metadata changes share validation.
	profile := nativeSaveStateProfile
	if len(arguments) > 1 {
		return "", fmt.Errorf("one save-state profile is required")
	}
	if len(arguments) == 1 {
		profile = arguments[0]
	}
	return profile, validateSaveStateProfile(profile)
}

func applyCopySettingsProfile(target, source *XMPPacket, columns []selectedColumn, profile string) (bool, error) {
	if err := validateSaveStateProfile(profile); err != nil {
		return false, err
	}
	before, _, err := saveStateMaps(target)
	if err != nil {
		return false, err
	}
	working := &XMPPacket{Document: target.Document.Copy()}
	var context registeredSaveContext
	if profile == nativeSaveStateProfile && len(columns) > 0 {
		context, err = copiedRegisteredSaveContext(working, source, columns)
		if err != nil {
			return false, err
		}
	}
	if len(columns) > 0 {
		if profile == "preserve" {
			_, err = copySelectedColumnMetadata(working, source, columns)
		}
	} else {
		_, err = pasteXMPMetadata(working, source)
	}
	if err != nil {
		return false, err
	}
	if profile == nativeSaveStateProfile {
		if len(columns) == 0 {
			context, err = readRegisteredSaveContext(working)
			if err != nil {
				return false, err
			}
		}
		if _, err = applyRegisteredSaveState(working, context); err != nil {
			return false, err
		}
	}
	after, _, err := saveStateMaps(working)
	if err != nil {
		return false, err
	}
	// Commit semantic changes once, so cancelled intermediate edits do not
	// reorder unchanged roots or cause spurious backups on the next copy.
	return writeChangedPasteRoots(target, before, after)
}
