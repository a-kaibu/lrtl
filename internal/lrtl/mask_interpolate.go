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
	element *etree.Element
	values  map[string]float64
}

type maskTransitionChange struct {
	key    maskTransitionKey
	values map[string]string
}

func localCorrectionProperty(name string) bool {
	return name == "CorrectionAmount" || strings.HasPrefix(name, "Local")
}

func maskTransitionCircularProperty(name string) bool {
	if name == "Angle" || name == "LocalToningHue" {
		return true
	}
	return strings.HasPrefix(name, "LocalColorGrade") && strings.HasSuffix(name, "Hue")
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

func maskNodeValues(element *etree.Element, mask bool) map[string]float64 {
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
	return values
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
				indexed[key] = maskTransitionNode{element: element, values: maskNodeValues(element, false)}
			}
			if maskID, found, err := crsElementValue(element, "MaskSyncID"); err != nil {
				return err
			} else if found && strings.TrimSpace(maskID) != "" && correctionID != "" {
				maskID = strings.TrimSpace(maskID)
				key := maskTransitionKey{collection: collection, correctionID: correctionID, maskID: maskID}
				if _, exists := indexed[key]; exists {
					return xmpError("duplicate MaskSyncID %q in crs:%s correction %q", maskID, collection, correctionID)
				}
				indexed[key] = maskTransitionNode{element: element, values: maskNodeValues(element, true)}
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

func cloneFloatMap(values map[string]float64) map[string]float64 {
	cloned := make(map[string]float64, len(values))
	for name, value := range values {
		cloned[name] = value
	}
	return cloned
}

func maskTransitionSource(keyframes []int, indexes []map[maskTransitionKey]maskTransitionNode, target int, key maskTransitionKey) map[string]float64 {
	if len(keyframes) == 0 {
		return nil
	}
	if len(keyframes) == 1 {
		if node, found := indexes[keyframes[0]][key]; found {
			return cloneFloatMap(node.values)
		}
		return nil
	}

	left, right := 0, 0
	if target < keyframes[0] {
		left, right = 0, keyframes[0]
		base, found := indexes[right][key]
		if !found {
			return nil
		}
		values := cloneFloatMap(base.values)
		if edge, exists := indexes[left][key]; exists {
			for name, edgeValue := range edge.values {
				endValue, ok := base.values[name]
				if ok {
					values[name] = interpolateMaskNumber(name, edgeValue, endValue, float64(target-left)/float64(right-left))
				}
			}
		}
		return values
	}
	if target > keyframes[len(keyframes)-1] {
		left, right = keyframes[len(keyframes)-1], len(indexes)-1
		base, found := indexes[left][key]
		if !found {
			return nil
		}
		values := cloneFloatMap(base.values)
		if edge, exists := indexes[right][key]; exists {
			for name, startValue := range base.values {
				endValue, ok := edge.values[name]
				if ok {
					values[name] = interpolateMaskNumber(name, startValue, endValue, float64(target-left)/float64(right-left))
				}
			}
		}
		return values
	}
	for frame := 0; frame < len(keyframes)-1; frame++ {
		left, right = keyframes[frame], keyframes[frame+1]
		if target <= right {
			break
		}
	}
	start, startFound := indexes[left][key]
	end, endFound := indexes[right][key]
	if !startFound || !endFound || right <= left {
		return nil
	}
	fraction := float64(target-left) / float64(right-left)
	values := make(map[string]float64)
	for name, startValue := range start.values {
		if endValue, found := end.values[name]; found {
			values[name] = interpolateMaskNumber(name, startValue, endValue, fraction)
		}
	}
	return values
}

func interpolateMaskNumber(name string, start, end, fraction float64) float64 {
	if !maskTransitionCircularProperty(name) {
		return start + (end-start)*fraction
	}
	span := math.Mod(end-start, 360)
	if span >= 180 {
		span -= 360
	} else if span < -180 {
		span += 360
	}
	value := math.Mod(start+span*fraction, 360)
	if value < 0 {
		value += 360
	}
	return value
}

func isCircularGradientNode(node maskTransitionNode) bool {
	what, found, err := crsElementValue(node.element, "What")
	return err == nil && found && strings.TrimSpace(what) == "Mask/CircularGradient"
}

func radialTransitionForTarget(keyframes []int, indexes []map[maskTransitionKey]maskTransitionNode, target int, key maskTransitionKey, aspect float64) (map[string]float64, bool) {
	if len(keyframes) < 2 || key.maskID == "" || aspect <= 0 || target <= keyframes[0] || target >= keyframes[len(keyframes)-1] {
		return nil, false
	}
	for segment := 0; segment < len(keyframes)-1; segment++ {
		left, right := keyframes[segment], keyframes[segment+1]
		if target <= right {
			start, startFound := indexes[left][key]
			end, endFound := indexes[right][key]
			if !startFound || !endFound || !isCircularGradientNode(start) || !isCircularGradientNode(end) {
				return nil, false
			}
			fraction := float64(target-left) / float64(right-left)
			return interpolateRadialGradient(start.values, end.values, fraction, aspect)
		}
	}
	return nil, false
}

func planMaskTransitions(packets []*XMPPacket, keyframeIndices []int, aspect float64) ([][]maskTransitionChange, error) {
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
	for index := range packets {
		keysByTarget[index] = make(map[maskTransitionKey]bool)
		for key := range indexes[index] {
			keysByTarget[index][key] = true
		}
	}
	for target := range packets {
		if keyframeSet[target] || len(indexes[target]) == 0 {
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
			proposed := maskTransitionSource(keyframeIndices, indexes, target, key)
			if len(proposed) == 0 {
				continue
			}
			if radial, ok := radialTransitionForTarget(keyframeIndices, indexes, target, key, aspect); ok {
				for name, value := range radial {
					proposed[name] = value
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
				value := proposed[property]
				if old, found := current[property]; found && closeEnough(old, value) {
					continue
				}
				values[property] = formatNumber(value)
			}
			if len(values) > 0 {
				changes[target] = append(changes[target], maskTransitionChange{key: key, values: values})
			}
		}
	}
	return changes, nil
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
		if err := setCRSElementValue(target, name, value); err != nil {
			return err
		}
	}
	return nil
}
