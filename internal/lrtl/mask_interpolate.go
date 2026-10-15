package lrtl

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/beevik/etree"
)

var maskTransitionCollections = []string{
	"CircularGradientBasedCorrections",
	"DepthBasedCorrections",
	"GradientBasedCorrections",
	"MaskGroupBasedCorrections",
	"PaintBasedCorrections",
}

var maskGeometryProperties = map[string]struct{}{
	"Alpha": {}, "Angle": {}, "Bottom": {}, "CenterValue": {}, "CenterWeight": {},
	"Feather": {}, "Flow": {}, "FullX": {}, "FullY": {}, "Left": {},
	"MaskValue": {}, "Midpoint": {}, "PerimeterValue": {}, "Radius": {},
	"Right": {}, "Roundness": {}, "SizeX": {}, "SizeY": {}, "Top": {},
	"X": {}, "Y": {}, "ZeroX": {}, "ZeroY": {},
}

type maskTransitionKey struct {
	collection   string
	correctionID string
	maskID       string
}

type maskTransitionNode struct {
	values            map[string]float64
	luminanceRange    string
	hasLuminanceRange bool
}

type maskTransitionChange struct {
	key               maskTransitionKey
	values            map[string]string
	luminanceRange    string
	hasLuminanceRange bool
}

func localCorrectionProperty(name string) bool {
	return name == "CorrectionAmount" || strings.HasPrefix(name, "Local")
}

func crsElementValue(element *etree.Element, name string) (string, bool, error) {
	for index := range element.Attr {
		if attributeMatches(&element.Attr[index], crsNamespace, name) {
			return element.Attr[index].Value, true, nil
		}
	}
	for _, token := range element.Child {
		child, ok := token.(*etree.Element)
		if !ok || child.NamespaceURI() != crsNamespace || child.Tag != name {
			continue
		}
		value, scalar := scalarElementText(child)
		if !scalar {
			return "", false, nil
		}
		return value, true, nil
	}
	return "", false, nil
}

func setCRSElementValue(element *etree.Element, name, value string) error {
	for index := range element.Attr {
		if attributeMatches(&element.Attr[index], crsNamespace, name) {
			element.Attr[index].Value = value
			return nil
		}
	}
	for _, token := range element.Child {
		child, ok := token.(*etree.Element)
		if !ok || child.NamespaceURI() != crsNamespace || child.Tag != name {
			continue
		}
		if _, scalar := scalarElementText(child); !scalar {
			return xmpError("cannot replace structured mask property crs:%s", name)
		}
		child.SetText(value)
		return nil
	}
	prefix := ensurePrefix(element, "crs", crsNamespace)
	element.CreateAttr(prefix+":"+name, value)
	return nil
}

func maskNodeValues(element *etree.Element, mask bool) (map[string]float64, error) {
	values := make(map[string]float64)
	add := func(name, value string) {
		allowed := localCorrectionProperty(name)
		if mask {
			_, allowed = maskGeometryProperties[name]
		}
		if !allowed {
			return
		}
		if number, ok := scalarNumber(value); ok {
			values[name] = number
		}
	}
	for index := range element.Attr {
		attribute := &element.Attr[index]
		if attribute.NamespaceURI() == crsNamespace {
			add(attribute.Key, attribute.Value)
		}
	}
	for _, token := range element.Child {
		child, ok := token.(*etree.Element)
		if !ok || child.NamespaceURI() != crsNamespace {
			continue
		}
		if value, scalar := scalarElementText(child); scalar {
			add(child.Tag, value)
		}
	}
	if rangeElement := correctionRangeMaskElement(element); rangeElement != nil {
		for _, property := range []string{"Type", "ColorAmount", "LumMin", "LumMax", "LumFeather"} {
			text, found, err := crsElementValue(rangeElement, property)
			if err != nil {
				return nil, err
			}
			if !found {
				continue
			}
			number, valid := scalarNumber(text)
			if !valid {
				return nil, xmpError("crs:CorrectionRangeMask %s must be numeric", property)
			}
			if property == "ColorAmount" || property == "LumMin" || property == "LumMax" || property == "LumFeather" {
				if number < 0 || number > 1 {
					return nil, xmpError("crs:CorrectionRangeMask %s must be between 0 and 1", property)
				}
			}
			values["RangeMask"+property] = number
		}
	}
	return values, nil
}

func indexMaskTransitionNodes(packet *XMPPacket) (map[maskTransitionKey]maskTransitionNode, error) {
	indexed := make(map[maskTransitionKey]maskTransitionNode)
	for _, collection := range maskTransitionCollections {
		property := packet.findRootProperty(crsNamespace, collection)
		if property.element == nil {
			continue
		}
		var walk func(*etree.Element, string) error
		walk = func(element *etree.Element, correctionID string) error {
			if value, found, err := crsElementValue(element, "CorrectionSyncID"); err != nil {
				return err
			} else if found && strings.TrimSpace(value) != "" {
				correctionID = strings.TrimSpace(value)
				key := maskTransitionKey{collection: collection, correctionID: correctionID}
				if _, exists := indexed[key]; exists {
					return xmpError("duplicate CorrectionSyncID %q in crs:%s", correctionID, collection)
				}
				values, err := maskNodeValues(element, false)
				if err != nil {
					return fmt.Errorf("correction %q: %w", correctionID, err)
				}
				luminanceRange, hasLuminanceRange, err := parseXMPRangeMaskLumRange(element)
				if err != nil {
					return fmt.Errorf("correction %q: %w", correctionID, err)
				}
				indexed[key] = maskTransitionNode{values: values, luminanceRange: luminanceRange, hasLuminanceRange: hasLuminanceRange}
			}
			if maskID, found, err := crsElementValue(element, "MaskSyncID"); err != nil {
				return err
			} else if found && strings.TrimSpace(maskID) != "" && correctionID != "" {
				maskID = strings.TrimSpace(maskID)
				key := maskTransitionKey{collection: collection, correctionID: correctionID, maskID: maskID}
				if _, exists := indexed[key]; exists {
					return xmpError("duplicate MaskSyncID %q in crs:%s correction %q", maskID, collection, correctionID)
				}
				values, err := maskNodeValues(element, true)
				if err != nil {
					return fmt.Errorf("mask %q: %w", maskID, err)
				}
				luminanceRange, hasLuminanceRange, err := parseXMPRangeMaskLumRange(element)
				if err != nil {
					return fmt.Errorf("mask %q: %w", maskID, err)
				}
				indexed[key] = maskTransitionNode{values: values, luminanceRange: luminanceRange, hasLuminanceRange: hasLuminanceRange}
			}
			for _, token := range element.Child {
				child, ok := token.(*etree.Element)
				if ok {
					if err := walk(child, correctionID); err != nil {
						return err
					}
				}
			}
			return nil
		}
		if err := walk(property.element, ""); err != nil {
			return nil, err
		}
	}
	return indexed, nil
}

func correctionRangeMaskElement(element *etree.Element) *etree.Element {
	for _, token := range element.Child {
		child, ok := token.(*etree.Element)
		if ok && child.NamespaceURI() == crsNamespace && child.Tag == "CorrectionRangeMask" {
			return child
		}
	}
	return nil
}

func parseXMPRangeMaskLumRange(element *etree.Element) (string, bool, error) {
	rangeElement := correctionRangeMaskElement(element)
	if rangeElement == nil {
		return "", false, nil
	}
	text, found, err := crsElementValue(rangeElement, "LumRange")
	if err != nil || !found || strings.TrimSpace(text) == "" {
		return "", false, err
	}
	return text, true, nil
}

func maskTransitionSource(keyframes []int, indexes []map[maskTransitionKey]maskTransitionNode, target int, key maskTransitionKey) map[string]float64 {
	return maskTransitionSourceWithSmoothness(keyframes, indexes, target, key, 50)
}

func maskTransitionSourceWithSmoothness(keyframes []int, indexes []map[maskTransitionKey]maskTransitionNode, target int, key maskTransitionKey, smoothness int) map[string]float64 {
	if target < 0 || target >= len(indexes) {
		return nil
	}
	return maskTransitionSeries(keyframes, indexes, key, len(indexes), smoothness)[target]
}

func maskTransitionSeries(keyframes []int, indexes []map[maskTransitionKey]maskTransitionNode, key maskTransitionKey, frameCount, smoothness int) []map[string]float64 {
	series := make([]map[string]float64, frameCount)
	properties := make(map[string]bool)
	for _, keyframe := range keyframes {
		if node, found := indexes[keyframe][key]; found {
			for name := range node.values {
				properties[name] = true
			}
		}
	}
	for name := range properties {
		anchors := make([]lrtSplinePoint, 0, len(keyframes))
		for _, keyframe := range keyframes {
			if node, found := indexes[keyframe][key]; found {
				if value, present := node.values[name]; present {
					anchors = append(anchors, lrtSplinePoint{x: float32(keyframe), y: float32(value)})
				}
			}
		}
		if len(anchors) == 0 {
			continue
		}
		spline, err := newLRTTransitionSpline(transitionSplinePoints(anchors, frameCount), smoothness)
		if err != nil {
			continue
		}
		for target := 0; target < frameCount; target++ {
			value, found := spline.valueAt(float32(target))
			if !found {
				continue
			}
			if series[target] == nil {
				series[target] = make(map[string]float64)
			}
			series[target][name] = value
		}
	}
	return series
}

func planMaskTransitions(packets []*XMPPacket, keyframeIndices []int, smoothness ...int) ([][]maskTransitionChange, error) {
	transitionSmoothness := 50
	if len(smoothness) > 0 {
		transitionSmoothness = smoothness[0]
	}
	changes := make([][]maskTransitionChange, len(packets))
	indexes := make([]map[maskTransitionKey]maskTransitionNode, len(packets))
	for index, packet := range packets {
		indexed, err := indexMaskTransitionNodes(packet)
		if err != nil {
			return nil, fmt.Errorf("frame %d local corrections: %w", index+1, err)
		}
		indexes[index] = indexed
	}
	keyframeSet := make(map[int]bool, len(keyframeIndices))
	for _, index := range keyframeIndices {
		keyframeSet[index] = true
	}
	keysByTarget := make([]map[maskTransitionKey]bool, len(packets))
	allKeys := make(map[maskTransitionKey]bool)
	for index := range packets {
		keysByTarget[index] = make(map[maskTransitionKey]bool)
		for key := range indexes[index] {
			keysByTarget[index][key] = true
			allKeys[key] = true
		}
	}
	seriesByKey := make(map[maskTransitionKey][]map[string]float64, len(allKeys))
	luminanceRangeByKey := make(map[maskTransitionKey]string, len(allKeys))
	hasLuminanceRangeByKey := make(map[maskTransitionKey]bool, len(allKeys))
	for key := range allKeys {
		seriesByKey[key] = maskTransitionSeries(keyframeIndices, indexes, key, len(packets), transitionSmoothness)
		luminanceRangeByKey[key], hasLuminanceRangeByKey[key] = firstMaskLuminanceRange(indexes, key)
	}
	for target := range packets {
		if len(indexes[target]) == 0 {
			continue
		}
		keys := make([]maskTransitionKey, 0, len(keysByTarget[target]))
		for key := range keysByTarget[target] {
			keys = append(keys, key)
		}
		sort.Slice(keys, func(i, j int) bool {
			if keys[i].collection != keys[j].collection {
				return keys[i].collection < keys[j].collection
			}
			if keys[i].correctionID != keys[j].correctionID {
				return keys[i].correctionID < keys[j].correctionID
			}
			return keys[i].maskID < keys[j].maskID
		})
		for _, key := range keys {
			proposed := make(map[string]float64)
			if !keyframeSet[target] {
				if target < len(seriesByKey[key]) && seriesByKey[key][target] != nil {
					proposed = seriesByKey[key][target]
				}
			}
			current := indexes[target][key].values
			values := make(map[string]string)
			propertyNames := make([]string, 0, len(proposed))
			for property := range proposed {
				propertyNames = append(propertyNames, property)
			}
			sort.Strings(propertyNames)
			for _, property := range propertyNames {
				value := clampMaskTransitionNumber(property, proposed[property])
				if old, found := current[property]; found && closeEnough(old, value) {
					continue
				}
				values[property] = formatNumber(value)
			}
			luminanceRange, hasLuminanceRange := luminanceRangeByKey[key], hasLuminanceRangeByKey[key]
			hasLuminanceRangeChange := hasLuminanceRange && (!indexes[target][key].hasLuminanceRange || indexes[target][key].luminanceRange != luminanceRange)
			if len(values) > 0 || hasLuminanceRangeChange {
				changes[target] = append(changes[target], maskTransitionChange{
					key: key, values: values, luminanceRange: luminanceRange,
					hasLuminanceRange: hasLuminanceRangeChange,
				})
			}
		}
	}
	return changes, nil
}

func clampMaskTransitionNumber(name string, value float64) float64 {
	if name == "RangeMaskType" {
		return math.Max(0, math.Min(5, value))
	}
	if name == "RangeMaskColorAmount" || name == "RangeMaskLumMin" || name == "RangeMaskLumMax" || name == "RangeMaskLumFeather" {
		return math.Max(0, math.Min(1, value))
	}
	return value
}

func firstMaskLuminanceRange(indexes []map[maskTransitionKey]maskTransitionNode, key maskTransitionKey) (string, bool) {
	for _, frame := range indexes {
		if node, found := frame[key]; found && node.hasLuminanceRange {
			return node.luminanceRange, true
		}
	}
	return "", false
}

func applyMaskTransition(packet *XMPPacket, change maskTransitionChange) error {
	property := packet.findRootProperty(crsNamespace, change.key.collection)
	if property.element == nil {
		return xmpError("missing crs:%s for correction %q", change.key.collection, change.key.correctionID)
	}
	var correction *etree.Element
	var mask *etree.Element
	var walk func(*etree.Element, string) error
	walk = func(element *etree.Element, correctionID string) error {
		if value, found, err := crsElementValue(element, "CorrectionSyncID"); err != nil {
			return err
		} else if found && strings.TrimSpace(value) != "" {
			correctionID = strings.TrimSpace(value)
			if correctionID == change.key.correctionID {
				if correction != nil {
					return xmpError("duplicate CorrectionSyncID %q in crs:%s", change.key.correctionID, change.key.collection)
				}
				correction = element
			}
		}
		if change.key.maskID != "" && correctionID == change.key.correctionID {
			if value, found, err := crsElementValue(element, "MaskSyncID"); err != nil {
				return err
			} else if found && strings.TrimSpace(value) == change.key.maskID {
				if mask != nil {
					return xmpError("duplicate MaskSyncID %q in correction %q", change.key.maskID, change.key.correctionID)
				}
				mask = element
			}
		}
		for _, token := range element.Child {
			child, ok := token.(*etree.Element)
			if ok {
				if err := walk(child, correctionID); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(property.element, ""); err != nil {
		return err
	}
	target := correction
	if change.key.maskID != "" {
		target = mask
	}
	if target == nil {
		return xmpError("missing synchronized correction/mask %q in crs:%s", change.key.correctionID, change.key.collection)
	}
	for name, value := range change.values {
		if strings.HasPrefix(name, "RangeMask") {
			rangeElement := correctionRangeMaskElement(target)
			if rangeElement == nil {
				return xmpError("missing crs:CorrectionRangeMask for range-mask transition")
			}
			if err := setCRSElementValue(rangeElement, strings.TrimPrefix(name, "RangeMask"), value); err != nil {
				return err
			}
			continue
		}
		if err := setCRSElementValue(target, name, value); err != nil {
			return err
		}
	}
	if change.hasLuminanceRange {
		rangeElement := correctionRangeMaskElement(target)
		if rangeElement == nil {
			return xmpError("missing crs:CorrectionRangeMask for luminance-range transition")
		}
		if err := setCRSElementValue(rangeElement, "LumRange", change.luminanceRange); err != nil {
			return err
		}
	}
	return nil
}
