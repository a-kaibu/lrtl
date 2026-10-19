package lrtl

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/beevik/etree"
)

var maskTransitionCollections = []string{
	"CircularGradientBasedCorrections",
	"GradientBasedCorrections",
	"MaskGroupBasedCorrections",
	"PaintBasedCorrections",
}

// LRT 7.5.5's post-transition XMP equalizer copies these curve trees from its
// reference frame (first rating 4, falling back to frame zero).
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
	materializable    bool
	values            map[string]float64
	luminanceRange    string
	hasLuminanceRange bool
	correctionItem    *etree.Element
	maskItem          *etree.Element
}

type maskTransitionChange struct {
	key                maskTransitionKey
	values             map[string]string
	luminanceRange     string
	hasLuminanceRange  bool
	templateCorrection *etree.Element
	templateMask       *etree.Element
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
	// RDF parseType="Resource" permits property elements, not property
	// attributes. Adobe's XMP parser rejects their combination.
	for _, attribute := range element.Attr {
		if attributeMatches(&attribute, rdfNamespace, "parseType") && attribute.Value == "Resource" {
			element.CreateElement(prefix + ":" + name).SetText(value)
			return nil
		}
	}
	element.CreateAttr(prefix+":"+name, value)
	return nil
}

func maskNodeValues(element *etree.Element, mask bool) (map[string]float64, error) {
	values := make(map[string]float64)
	var numericError error
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
		} else {
			numericError = xmpError("crs:%s must be a finite number", name)
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
		} else {
			allowed := localCorrectionProperty(child.Tag)
			if mask {
				_, allowed = maskGeometryProperties[child.Tag]
				_, registered := maskTransitionSpecs[child.Tag]
				allowed = allowed && registered
			}
			if allowed {
				numericError = xmpError("crs:%s must be a scalar number", child.Tag)
			}
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
	return values, numericError
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
				hasLuminanceRange: hasCorrectionRange, correctionItem: item,
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
					hasLuminanceRange: hasMaskRange, correctionItem: item, maskItem: maskItem,
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
	return rdfArrayItemsInContainer(rdfArrayContainer(property))
}

func rdfArrayContainer(property *etree.Element) *etree.Element {
	if property == nil {
		return nil
	}
	for _, token := range property.Child {
		container, ok := token.(*etree.Element)
		if !ok || container.NamespaceURI() != rdfNamespace || (container.Tag != "Seq" && container.Tag != "Bag" && container.Tag != "Alt") {
			continue
		}
		return container
	}
	return nil
}

func rdfArrayItemsInContainer(container *etree.Element) []*etree.Element {
	var items []*etree.Element
	if container == nil {
		return items
	}
	for _, itemToken := range container.Child {
		item, ok := itemToken.(*etree.Element)
		if ok && item.NamespaceURI() == rdfNamespace && item.Tag == "li" {
			items = append(items, item)
		}
	}
	return items
}

func ensureRDFArrayContainer(property *etree.Element) (*etree.Element, error) {
	if container := rdfArrayContainer(property); container != nil {
		return container, nil
	}
	for _, token := range property.Child {
		if child, ok := token.(*etree.Element); ok {
			return nil, xmpError("crs:%s contains %s instead of an RDF array", property.Tag, child.Tag)
		}
		if text, ok := token.(*etree.CharData); ok && strings.TrimSpace(text.Data) != "" {
			return nil, xmpError("crs:%s contains text instead of an RDF array", property.Tag)
		}
	}
	prefix := ensurePrefix(property, "rdf", rdfNamespace)
	return property.CreateElement(prefix + ":Seq"), nil
}

func ensureCRSArrayProperty(scope *etree.Element, local string) (*etree.Element, error) {
	property := directCRSProperty(scope, local)
	if property.attribute != nil {
		return nil, xmpError("crs:%s is stored as an attribute, not an RDF array", local)
	}
	if property.element == nil {
		prefix := ensurePrefix(scope, "crs", crsNamespace)
		property.element = scope.CreateElement(prefix + ":" + local)
	}
	return property.element, nil
}

func ensurePacketCRSArray(packet *XMPPacket, local string) (*etree.Element, *etree.Element, error) {
	property := packet.findRootProperty(crsNamespace, local)
	if property.element == nil && property.attribute != nil {
		return nil, nil, xmpError("crs:%s is stored as an attribute, not an RDF array", local)
	}
	if property.element == nil {
		descriptions := packet.rootDescriptions()
		if len(descriptions) == 0 {
			return nil, nil, xmpError("cannot create crs:%s without rdf:Description", local)
		}
		prefix := ensurePrefix(descriptions[0], "crs", crsNamespace)
		property.element = descriptions[0].CreateElement(prefix + ":" + local)
	}
	container, err := ensureRDFArrayContainer(property.element)
	return property.element, container, err
}

func ensureRDFArrayPosition(container *etree.Element, position int, template *etree.Element) (*etree.Element, error) {
	if position < 1 {
		return nil, xmpError("RDF array position must be positive: %d", position)
	}
	if template != nil && (template.NamespaceURI() != rdfNamespace || template.Tag != "li") {
		return nil, xmpError("cannot materialize RDF array position %d from non-rdf:li template", position)
	}
	items := rdfArrayItemsInContainer(container)
	for index := 0; index < len(items) && index < position; index++ {
		if !rdfArrayItemIsEmpty(items[index]) {
			continue
		}
		itemTemplate := rdfArrayTemplateItem(template, index+1)
		if itemTemplate == nil && index+1 == position {
			itemTemplate = template
		}
		if itemTemplate != nil && !rdfArrayItemIsEmpty(itemTemplate) {
			item, err := replaceRDFArrayItem(container, items[index], itemTemplate, index+1)
			if err != nil {
				return nil, err
			}
			items[index] = item
		}
	}
	if len(items) >= position {
		return items[position-1], nil
	}
	for len(items) < position {
		currentPosition := len(items) + 1
		itemTemplate := rdfArrayTemplateItem(template, currentPosition)
		if itemTemplate == nil && currentPosition == position {
			itemTemplate = template
		}
		var item *etree.Element
		if itemTemplate != nil && !rdfArrayItemIsEmpty(itemTemplate) {
			item = copyElementWithNamespaces(itemTemplate)
		} else {
			item = createRDFResourceArrayItem(container)
		}
		container.AddChild(item)
		items = append(items, item)
	}
	return items[position-1], nil
}

func rdfArrayTemplateItem(template *etree.Element, position int) *etree.Element {
	if template == nil || position < 1 {
		return nil
	}
	sourceContainer := template.Parent()
	if sourceContainer == nil || sourceContainer == template {
		return nil
	}
	items := rdfArrayItemsInContainer(sourceContainer)
	if position > len(items) {
		return nil
	}
	return items[position-1]
}

func replaceRDFArrayItem(container, oldItem, template *etree.Element, position int) (*etree.Element, error) {
	copy := copyElementWithNamespaces(template)
	for index, token := range container.Child {
		if token == oldItem {
			container.RemoveChild(oldItem)
			container.InsertChildAt(index, copy)
			return copy, nil
		}
	}
	return nil, xmpError("RDF array position %d is detached from its container", position)
}

func rdfArrayItemIsEmpty(item *etree.Element) bool {
	if item == nil {
		return true
	}
	for _, attribute := range item.Attr {
		if attributeMatches(&attribute, rdfNamespace, "parseType") && attribute.Value == "Resource" {
			continue
		}
		if attribute.Space != "xmlns" && attribute.Key != "xmlns" {
			return false
		}
	}
	for _, token := range item.Child {
		switch child := token.(type) {
		case *etree.Element:
			return false
		case *etree.CharData:
			if strings.TrimSpace(child.Data) != "" {
				return false
			}
		}
	}
	return true
}

func createRDFResourceArrayItem(container *etree.Element) *etree.Element {
	rdfPrefix := ensurePrefix(container, "rdf", rdfNamespace)
	item := container.CreateElement(rdfPrefix + ":li")
	item.CreateAttr(rdfPrefix+":parseType", "Resource")
	return item
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
	item := maskGroupCorrectionItemAt(packet, correctionIndex)
	if item == nil {
		return nil
	}
	return unwrapRDFArrayItem(item)
}

func maskGroupCorrectionItemAt(packet *XMPPacket, correctionIndex int) *etree.Element {
	if packet == nil || correctionIndex < 1 {
		return nil
	}
	property := packet.findRootProperty(crsNamespace, "MaskGroupBasedCorrections")
	items := rdfArrayItems(property.element)
	if correctionIndex > len(items) {
		return nil
	}
	return items[correctionIndex-1]
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
	sourceProperty := directCRSProperty(maskGroupCorrectionAt(source, correctionIndex), local)
	if targetCorrection == nil {
		return sourceProperty.element != nil || sourceProperty.attribute != nil, nil
	}
	targetProperty := directCRSProperty(targetCorrection, local)
	same, err := sameRootProperty(targetProperty, sourceProperty)
	return !same, err
}

func (packet *XMPPacket) hasMaskGroupStructuredProperty(correctionIndex int, local string) bool {
	property := directCRSProperty(maskGroupCorrectionAt(packet, correctionIndex), local)
	return property.element != nil || property.attribute != nil
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
	targetItem := maskGroupCorrectionItemAt(packet, correctionIndex)
	if targetCorrection == nil || rdfArrayItemIsEmpty(targetItem) {
		sourceItem := maskGroupCorrectionItemAt(source, correctionIndex)
		if sourceItem == nil || rdfArrayItemIsEmpty(sourceItem) {
			return false, nil
		}
		_, container, err := ensurePacketCRSArray(packet, "MaskGroupBasedCorrections")
		if err != nil {
			return false, err
		}
		item, err := ensureRDFArrayPosition(container, correctionIndex, sourceItem)
		if err != nil {
			return false, err
		}
		targetCorrection = unwrapRDFArrayItem(item)
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
	return maskTransitionSeriesExcept(keyframes, indexes, key, frameCount, smoothness, nil)
}

func maskTransitionSeriesExcept(keyframes []int, indexes []map[maskTransitionKey]maskTransitionNode, key maskTransitionKey, frameCount, smoothness int, copied map[string]float64) []map[string]float64 {
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
		if _, copy := copied[name]; copy {
			continue
		}
		spec, registered := maskTransitionSpecs[name]
		if !registered {
			continue
		}
		anchors := make([]lrtSplinePoint, 0, len(keyframes))
		preciseAnchors := make([]transitionAnchor, 0, len(keyframes))
		for _, keyframe := range keyframes {
			if node, found := indexes[keyframe][key]; found {
				if value, present := node.values[name]; present {
					anchors = append(anchors, lrtSplinePoint{x: float32(keyframe), y: float32(value)})
					preciseAnchors = append(preciseAnchors, transitionAnchor{keyframe, value})
				}
			}
		}
		if len(anchors) == 0 {
			continue
		}
		values, filled, err := sampleLRTNumericTransition(anchors, preciseAnchors, frameCount, keyframeFlags, smoothness, spec)
		if err != nil {
			continue
		}
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
	prepared := prepareMaskTransitionInputs(indexes)
	booleanChanges, err := planMaskBooleanTransitions(prepared.Indexes, keyframeIndices)
	if err != nil {
		return nil, err
	}
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
		for key := range booleanChanges[index] {
			keysByTarget[index][key] = true
		}
		for key := range prepared.Missing[index] {
			keysByTarget[index][key] = true
		}
	}
	seriesByKey := make(map[maskTransitionKey][]map[string]float64, len(allKeys))
	copiesByKey := make(map[maskTransitionKey]map[string]float64, len(allKeys))
	luminanceRangeByKey := make(map[maskTransitionKey]string, len(allKeys))
	hasLuminanceRangeByKey := make(map[maskTransitionKey]bool, len(allKeys))
	for key := range allKeys {
		copies := maskNumericCopyReferences(keyframeIndices, prepared.Indexes, key)
		copiesByKey[key] = copies
		for i, index := range prepared.Indexes {
			for name, value := range index[key].values {
				if _, copy := copies[name]; !copy && math.IsInf(float64(float32(value)), 0) {
					return nil, fmt.Errorf("frame %d crs:%s[%d] crs:%s exceeds finite float32 transition range", i+1, key.collection, key.correctionIndex, name)
				}
			}
		}
		seriesByKey[key] = maskTransitionSeriesExcept(keyframeIndices, prepared.Indexes, key, len(packets), transitionSmoothness, copies)
		luminanceRangeByKey[key], hasLuminanceRangeByKey[key] = firstMaskLuminanceRange(indexes, key)
	}
	for target := range packets {
		if !keyframeSet[target] {
			for key := range allKeys {
				if key.correctionIndex < 1 {
					continue
				}
				interpolated := target < len(seriesByKey[key]) && seriesByKey[key][target] != nil
				if interpolated || len(copiesByKey[key]) > 0 || hasLuminanceRangeByKey[key] {
					keysByTarget[target][key] = true
				}
			}
		}
		if len(keysByTarget[target]) == 0 {
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
			for name, value := range prepared.Missing[target][key] {
				if name == "Angle" && lrtNormalizesMaskAngle(key) {
					value = normalizeLRTMaskAngle(value)
				}
				proposed[name] = value
			}
			if !keyframeSet[target] {
				if target < len(seriesByKey[key]) && seriesByKey[key][target] != nil {
					for name, value := range seriesByKey[key][target] {
						proposed[name] = value
					}
				}
			}
			currentNode, present := indexes[target][key]
			current := currentNode.values
			copies := copiesByKey[key]
			if !keyframeSet[target] || (present && maskTransitionNodeHasTemplate(currentNode, key)) {
				for name, value := range copies {
					if name == "Angle" && lrtNormalizesMaskAngle(key) {
						value = normalizeLRTMaskAngle(value)
					}
					proposed[name] = value
				}
			}
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
				_, copy := copies[property]
				if copy {
					value = proposed[property]
				}
				old, found := current[property]
				same := found && closeEnough(old, value)
				if copy {
					same = found && math.Float64bits(old) == math.Float64bits(value)
				}
				if same {
					continue
				}
				values[property] = formatNumber(value)
				if copy {
					values[property] = strconv.FormatFloat(value, 'g', -1, 64)
				}
			}
			for name, value := range booleanChanges[target][key] {
				values[name] = value
			}
			luminanceRange, hasLuminanceRange := luminanceRangeByKey[key], hasLuminanceRangeByKey[key]
			hasLuminanceRangeChange := hasLuminanceRange && (!currentNode.hasLuminanceRange || currentNode.luminanceRange != luminanceRange)
			if keyframeSet[target] && (!present || !maskTransitionNodeHasTemplate(currentNode, key)) {
				hasLuminanceRangeChange = false
			}
			if len(values) > 0 || hasLuminanceRangeChange {
				templateCorrection, templateMask := (*etree.Element)(nil), (*etree.Element)(nil)
				if !keyframeSet[target] && (!present || !maskTransitionNodeHasTemplate(currentNode, key)) {
					template := maskTransitionTemplate(indexes, keyframeIndices, key, target)
					templateCorrection, templateMask = template.correctionItem, template.maskItem
				}
				changes[target] = append(changes[target], maskTransitionChange{
					key: key, values: values, luminanceRange: luminanceRange,
					hasLuminanceRange: hasLuminanceRangeChange, templateCorrection: templateCorrection,
					templateMask: templateMask,
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

func maskTransitionTemplate(indexes []map[maskTransitionKey]maskTransitionNode, keyframes []int, key maskTransitionKey, target int) maskTransitionNode {
	bestIndex, bestDistance := -1, math.MaxInt
	for _, candidate := range keyframes {
		if candidate < 0 || candidate >= len(indexes) {
			continue
		}
		node, found := indexes[candidate][key]
		if !found || !maskTransitionNodeHasTemplate(node, key) {
			continue
		}
		distance := absInt(candidate - target)
		if bestIndex < 0 || distance < bestDistance || (distance == bestDistance && candidate < bestIndex) {
			bestIndex, bestDistance = candidate, distance
		}
	}
	if bestIndex < 0 {
		for candidate, frame := range indexes {
			node, found := frame[key]
			if !found || !maskTransitionNodeHasTemplate(node, key) {
				continue
			}
			distance := absInt(candidate - target)
			if bestIndex < 0 || distance < bestDistance || (distance == bestDistance && candidate < bestIndex) {
				bestIndex, bestDistance = candidate, distance
			}
		}
	}
	if bestIndex < 0 {
		return maskTransitionNode{}
	}
	return indexes[bestIndex][key]
}

func maskTransitionNodeHasTemplate(node maskTransitionNode, key maskTransitionKey) bool {
	if key.maskIndex > 0 {
		return !rdfArrayItemIsEmpty(node.maskItem)
	}
	return !rdfArrayItemIsEmpty(node.correctionItem)
}

func ensureMaskTransitionStructure(packet *XMPPacket, change maskTransitionChange) error {
	if change.key.correctionIndex < 1 {
		return nil
	}
	property := packet.findRootProperty(crsNamespace, change.key.collection)
	nativeEmptyRoot := property.element == nil && property.attribute == nil && change.templateCorrection == nil && change.key.maskIndex == 0
	_, corrections, err := ensurePacketCRSArray(packet, change.key.collection)
	if err != nil {
		return err
	}
	if nativeEmptyRoot {
		// The observed native scalar writer creates an unordered correction bag.
		corrections.Tag = "Bag"
	}
	correctionItem, err := ensureRDFArrayPosition(corrections, change.key.correctionIndex, change.templateCorrection)
	if err != nil {
		return err
	}
	if change.key.maskIndex < 1 {
		return nil
	}
	correction := unwrapRDFArrayItem(correctionItem)
	existingMasks := directCRSProperty(correction, "CorrectionMasks")
	nativeEmptyMask := existingMasks.element == nil && existingMasks.attribute == nil && change.templateMask == nil
	maskProperty, err := ensureCRSArrayProperty(correction, "CorrectionMasks")
	if err != nil {
		return err
	}
	masks, err := ensureRDFArrayContainer(maskProperty)
	if err != nil {
		return err
	}
	if nativeEmptyMask {
		masks.Tag = "Bag"
	}
	_, err = ensureRDFArrayPosition(masks, change.key.maskIndex, change.templateMask)
	return err
}

func applyMaskTransition(packet *XMPPacket, change maskTransitionChange) error {
	if err := ensureMaskTransitionStructure(packet, change); err != nil {
		return fmt.Errorf("crs:%s[%d]: %w", change.key.collection, change.key.correctionIndex, err)
	}
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
