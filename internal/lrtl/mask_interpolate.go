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
	"GradientBasedCorrections",
	"MaskGroupBasedCorrections",
	"PaintBasedCorrections",
}

// LRT 7.5.5's post-transition XMP equalizer copies these curve trees from frame zero.
var firstFrameEqualizedMaskGroupIndexes = []int{1, 2, 3, 4, 8, 9, 10, 11}
var firstFrameEqualizedMaskGroupCurves = []string{"MainCurve", "RedCurve", "GreenCurve", "BlueCurve"}

var maskTransitionCorrectionIndexes = map[string]map[int]struct{}{
	"CircularGradientBasedCorrections": transitionIndexes(1, 2),
	"GradientBasedCorrections":         transitionIndexes(1, 2, 3, 4),
	"MaskGroupBasedCorrections":        transitionIndexes(1, 2, 3, 4, 8, 9, 10, 11),
	"PaintBasedCorrections":            transitionIndexes(1, 2),
}

var maskGeometryProperties = map[string]struct{}{
	"Alpha": {}, "Angle": {}, "Bottom": {}, "CenterValue": {}, "CenterWeight": {},
	"Feather": {}, "Flow": {}, "FullX": {}, "FullY": {}, "Left": {},
	"MaskValue": {}, "Midpoint": {}, "PerimeterValue": {}, "Radius": {},
	"Right": {}, "Roundness": {}, "SizeX": {}, "SizeY": {}, "Top": {},
	"X": {}, "Y": {}, "ZeroX": {}, "ZeroY": {},
}

type maskTransitionKey struct {
	collection      string
	correctionID    string
	maskID          string
	correctionIndex int
	maskIndex       int
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

type maskStructuredProperty struct {
	correctionIndex int
	property        string
}

func (property maskStructuredProperty) String() string {
	return fmt.Sprintf("MaskGroupBasedCorrections[%d].%s", property.correctionIndex, property.property)
}

func localCorrectionProperty(name string) bool {
	if name == "CorrectionAmount" {
		return true
	}
	_, registered := maskTransitionSpecs[name]
	return registered && strings.HasPrefix(name, "Local")
}

func transitionIndexes(indexes ...int) map[int]struct{} {
	result := make(map[int]struct{}, len(indexes))
	for _, index := range indexes {
		result[index] = struct{}{}
	}
	return result
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
			_, registered := maskTransitionSpecs[name]
			allowed = allowed && registered
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
		corrections := rdfArrayItems(property.element)
		if len(corrections) == 0 {
			if err := indexMaskTransitionNodesByID(property.element, collection, indexed); err != nil {
				return nil, err
			}
			continue
		}
		registeredIndexes := maskTransitionCorrectionIndexes[collection]
		for correctionIndex, item := range corrections {
			position := correctionIndex + 1
			if _, registered := registeredIndexes[position]; !registered {
				continue
			}
			correction := unwrapRDFArrayItem(item)
			correctionKey := maskTransitionKey{collection: collection, correctionIndex: position}
			correctionValues, err := maskNodeValues(correction, false)
			if err != nil {
				return nil, fmt.Errorf("crs:%s[%d]: %w", collection, position, err)
			}
			correctionRange, hasCorrectionRange, err := parseXMPRangeMaskLumRange(correction)
			if err != nil {
				return nil, fmt.Errorf("crs:%s[%d]: %w", collection, position, err)
			}
			indexed[correctionKey] = maskTransitionNode{
				values: correctionValues, luminanceRange: correctionRange,
				hasLuminanceRange: hasCorrectionRange,
			}

			maskProperty := findCRSDescendant(correction, "CorrectionMasks")
			if maskProperty == nil {
				continue
			}
			masks := rdfArrayItems(maskProperty)
			for maskIndex, maskItem := range masks {
				maskPosition := maskIndex + 1
				if maskPosition != 1 {
					continue
				}
				mask := unwrapRDFArrayItem(maskItem)
				maskKey := maskTransitionKey{
					collection: collection, correctionIndex: position, maskIndex: maskPosition,
				}
				maskValues, err := maskNodeValues(mask, true)
				if err != nil {
					return nil, fmt.Errorf("crs:%s[%d]/crs:CorrectionMasks[%d]: %w", collection, position, maskPosition, err)
				}
				maskRange, hasMaskRange, err := parseXMPRangeMaskLumRange(mask)
				if err != nil {
					return nil, fmt.Errorf("crs:%s[%d]/crs:CorrectionMasks[%d]: %w", collection, position, maskPosition, err)
				}
				indexed[maskKey] = maskTransitionNode{
					values: maskValues, luminanceRange: maskRange,
					hasLuminanceRange: hasMaskRange,
				}
			}
		}
	}
	return indexed, nil
}

func indexMaskTransitionNodesByID(root *etree.Element, collection string, indexed map[maskTransitionKey]maskTransitionNode) error {
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
	return walk(root, "")
}

func rdfArrayItems(property *etree.Element) []*etree.Element {
	if property == nil {
		return nil
	}
	for _, token := range property.Child {
		container, ok := token.(*etree.Element)
		if !ok || container.NamespaceURI() != rdfNamespace || (container.Tag != "Seq" && container.Tag != "Bag" && container.Tag != "Alt") {
			continue
		}
		var items []*etree.Element
		for _, itemToken := range container.Child {
			item, ok := itemToken.(*etree.Element)
			if ok && item.NamespaceURI() == rdfNamespace && item.Tag == "li" {
				items = append(items, item)
			}
		}
		return items
	}
	return nil
}

func unwrapRDFArrayItem(item *etree.Element) *etree.Element {
	for _, token := range item.Child {
		child, ok := token.(*etree.Element)
		if ok && child.NamespaceURI() == rdfNamespace && child.Tag == "Description" {
			return child
		}
	}
	return item
}

func findCRSDescendant(element *etree.Element, local string) *etree.Element {
	if element.NamespaceURI() == crsNamespace && element.Tag == local {
		return element
	}
	for _, token := range element.Child {
		child, ok := token.(*etree.Element)
		if ok {
			if found := findCRSDescendant(child, local); found != nil {
				return found
			}
		}
	}
	return nil
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

func maskGroupCorrectionAt(packet *XMPPacket, correctionIndex int) *etree.Element {
	if packet == nil || correctionIndex < 1 {
		return nil
	}
	property := packet.findRootProperty(crsNamespace, "MaskGroupBasedCorrections")
	items := rdfArrayItems(property.element)
	if correctionIndex > len(items) {
		return nil
	}
	return unwrapRDFArrayItem(items[correctionIndex-1])
}

func directCRSProperty(element *etree.Element, local string) topLevelXMPProperty {
	if element == nil {
		return topLevelXMPProperty{}
	}
	for index := range element.Attr {
		if attributeMatches(&element.Attr[index], crsNamespace, local) {
			return topLevelXMPProperty{description: element, attribute: &element.Attr[index]}
		}
	}
	for _, token := range element.Child {
		child, ok := token.(*etree.Element)
		if ok && child.NamespaceURI() == crsNamespace && child.Tag == local {
			return topLevelXMPProperty{description: element, element: child}
		}
	}
	return topLevelXMPProperty{}
}

func (packet *XMPPacket) maskGroupStructuredPropertyDiffers(source *XMPPacket, correctionIndex int, local string) (bool, error) {
	targetCorrection := maskGroupCorrectionAt(packet, correctionIndex)
	if targetCorrection == nil {
		return false, nil
	}
	sourceProperty := directCRSProperty(maskGroupCorrectionAt(source, correctionIndex), local)
	targetProperty := directCRSProperty(targetCorrection, local)
	same, err := sameRootProperty(targetProperty, sourceProperty)
	return !same, err
}

func copyElementWithNamespaces(source *etree.Element) *etree.Element {
	copy := source.Copy()
	prefixes := make(map[string]struct{})
	var collect func(*etree.Element)
	collect = func(element *etree.Element) {
		if element.Space != "" {
			prefixes[element.Space] = struct{}{}
		}
		for _, attribute := range element.Attr {
			if attribute.Space != "" && attribute.Space != "xmlns" {
				prefixes[attribute.Space] = struct{}{}
			}
		}
		for _, token := range element.Child {
			if child, ok := token.(*etree.Element); ok {
				collect(child)
			}
		}
	}
	collect(copy)
	for prefix := range prefixes {
		if _, exists := prefixBinding(copy, prefix); exists {
			continue
		}
		if namespace, found := prefixBinding(source, prefix); found {
			copy.CreateAttr("xmlns:"+prefix, namespace)
		}
	}
	return copy
}

func copyScopedCRSPropertyFrom(source, target *etree.Element, local string) (bool, error) {
	sourceProperty := directCRSProperty(source, local)
	targetProperty := directCRSProperty(target, local)
	same, err := sameRootProperty(targetProperty, sourceProperty)
	if err != nil || same {
		return false, err
	}
	attributes := target.Attr[:0]
	for _, attribute := range target.Attr {
		if !attributeMatches(&attribute, crsNamespace, local) {
			attributes = append(attributes, attribute)
		}
	}
	target.Attr = attributes
	for _, token := range append([]etree.Token(nil), target.Child...) {
		child, ok := token.(*etree.Element)
		if ok && child.NamespaceURI() == crsNamespace && child.Tag == local {
			child.Remove()
		}
	}
	if sourceProperty.element != nil {
		target.AddChild(copyElementWithNamespaces(sourceProperty.element))
	} else if sourceProperty.attribute != nil {
		prefix := sourceProperty.attribute.Space
		if prefix == "" {
			return false, xmpError("cannot copy unprefixed namespaced attribute crs:%s", local)
		}
		prefix = ensurePrefix(target, prefix, crsNamespace)
		target.CreateAttr(prefix+":"+local, sourceProperty.attribute.Value)
	}
	return true, nil
}

func (packet *XMPPacket) copyMaskGroupStructuredPropertyFrom(source *XMPPacket, correctionIndex int, local string) (bool, error) {
	targetCorrection := maskGroupCorrectionAt(packet, correctionIndex)
	if targetCorrection == nil {
		return false, nil
	}
	return copyScopedCRSPropertyFrom(maskGroupCorrectionAt(source, correctionIndex), targetCorrection, local)
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
	keyframeFlags := make([]bool, frameCount)
	for _, keyframe := range keyframes {
		if keyframe >= 0 && keyframe < frameCount {
			keyframeFlags[keyframe] = true
		}
	}
	properties := make(map[string]bool)
	for _, keyframe := range keyframes {
		if node, found := indexes[keyframe][key]; found {
			for name := range node.values {
				properties[name] = true
			}
		}
	}
	for name := range properties {
		spec, registered := maskTransitionSpecs[name]
		if !registered {
			continue
		}
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
		values, filled := spline.sampleFrames(frameCount, keyframeFlags)
		for target := 0; target < frameCount; target++ {
			if keyframeFlags[target] || !filled[target] {
				continue
			}
			if series[target] == nil {
				series[target] = make(map[string]float64)
			}
			value := adjustTransitionValue(values[target], spec)
			if name == "Angle" && lrtNormalizesMaskAngle(key) {
				value = normalizeLRTMaskAngle(value)
			}
			series[target][name] = value
		}
	}
	return series
}

func lrtNormalizesMaskAngle(key maskTransitionKey) bool {
	switch key.collection {
	case "CircularGradientBasedCorrections":
		return key.correctionIndex == 0 || key.correctionIndex == 1 || key.correctionIndex == 2
	case "MaskGroupBasedCorrections":
		return key.correctionIndex == 0 || key.correctionIndex == 8 || key.correctionIndex == 9
	default:
		return false
	}
}

// normalizeLRTMaskAngle mirrors the installed LRTimelapse post-transition
// adjustment: add 90 below -45, then subtract 90 above 45.
func normalizeLRTMaskAngle(angle float64) float64 {
	if angle < -45 {
		angle += 90
	}
	if angle > 45 {
		angle -= 90
	}
	return angle
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
			if keys[i].correctionIndex != keys[j].correctionIndex {
				return keys[i].correctionIndex < keys[j].correctionIndex
			}
			if keys[i].maskIndex != keys[j].maskIndex {
				return keys[i].maskIndex < keys[j].maskIndex
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
			if lrtNormalizesMaskAngle(key) {
				if angle, found := current["Angle"]; found {
					_, hasInterpolatedAngle := proposed["Angle"]
					if keyframeSet[target] || !hasInterpolatedAngle {
						normalized := normalizeLRTMaskAngle(angle)
						if !closeEnough(angle, normalized) {
							proposed["Angle"] = normalized
						}
					}
				}
			}
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
		return xmpError("missing crs:%s for local-correction transition", change.key.collection)
	}
	var correction *etree.Element
	var mask *etree.Element
	if change.key.correctionIndex > 0 {
		corrections := rdfArrayItems(property.element)
		if change.key.correctionIndex > len(corrections) {
			return xmpError("crs:%s has no correction at registered position %d", change.key.collection, change.key.correctionIndex)
		}
		correction = unwrapRDFArrayItem(corrections[change.key.correctionIndex-1])
		if change.key.maskIndex > 0 {
			maskProperty := findCRSDescendant(correction, "CorrectionMasks")
			masks := rdfArrayItems(maskProperty)
			if change.key.maskIndex > len(masks) {
				return xmpError("crs:%s[%d] has no correction mask at position %d", change.key.collection, change.key.correctionIndex, change.key.maskIndex)
			}
			mask = unwrapRDFArrayItem(masks[change.key.maskIndex-1])
		}
	} else {
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
	}
	target := correction
	if change.key.maskIndex > 0 || change.key.maskID != "" {
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

func maskTransitionKeyLabel(key maskTransitionKey) string {
	label := "crs:" + key.collection
	if key.correctionIndex > 0 {
		label += fmt.Sprintf("[%d]", key.correctionIndex)
	} else if key.correctionID != "" {
		label += "/" + key.correctionID
	}
	if key.maskIndex > 0 {
		label += fmt.Sprintf("/crs:CorrectionMasks[%d]", key.maskIndex)
	} else if key.maskID != "" {
		label += "/" + key.maskID
	}
	return label
}
