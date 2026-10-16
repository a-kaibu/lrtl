package lrtl

import "fmt"

// LRT 7.5.5 registers CorrectionActive on every supported correction and
// Flipped on the first circular/MaskGroup selector. MaskInverted, linear
// Flipped and brush selector flags are not registered transition columns.
func maskBooleanTransitionProperties(key maskTransitionKey) []string {
	if _, registered := maskTransitionCorrectionIndexes[key.collection][key.correctionIndex]; !registered {
		return nil
	}
	if key.maskIndex == 0 {
		return []string{"CorrectionActive"}
	}
	if key.maskIndex == 1 && (key.collection == "CircularGradientBasedCorrections" || key.collection == "MaskGroupBasedCorrections") {
		return []string{"Flipped"}
	}
	return nil
}

func planMaskBooleanTransitions(indexes []map[maskTransitionKey]maskTransitionNode, keys []int) ([]map[maskTransitionKey]map[string]string, error) {
	changes := make([]map[maskTransitionKey]map[string]string, len(indexes))
	ratings := make([]int, len(indexes))
	for _, key := range keys {
		ratings[key] = 4
	}
	allKeys := map[maskTransitionKey]bool{}
	for _, index := range indexes {
		for key := range index {
			allKeys[key] = true
		}
	}
	for key := range allKeys {
		for _, name := range maskBooleanTransitionProperties(key) {
			input := make([]transitionScalar, len(indexes))
			for i, index := range indexes {
				node, found := index[key]
				if !found || !maskTransitionNodeHasTemplate(node, key) {
					continue
				}
				item := node.correctionItem
				if key.maskIndex > 0 {
					item = node.maskItem
				}
				text, present, err := crsElementValue(unwrapRDFArrayItem(item), name)
				if err != nil {
					return nil, err
				}
				if !present {
					// Structured content is not a missing boolean.
					property := directCRSProperty(unwrapRDFArrayItem(item), name)
					if property.element != nil || property.attribute != nil {
						return nil, fmt.Errorf("frame %d crs:%s[%d] crs:%s must be a scalar boolean", i+1, key.collection, key.correctionIndex, name)
					}
					continue
				}
				value, err := parseCameraRawBool(text)
				if err != nil {
					return nil, fmt.Errorf("frame %d crs:%s[%d] crs:%s: %w", i+1, key.collection, key.correctionIndex, name, err)
				}
				canonical := "False"
				if value {
					canonical = "True"
				}
				input[i] = transitionScalar{canonical, true}
			}
			// Local booleans participate in ordinary metadata missing-value
			// preflight before the same reference/equality dispatch as root flags.
			output := rootAutoTransitionScalars(fillMissingTransitionScalars(input), ratings, name)
			for i, value := range output {
				if !value.present || value == input[i] {
					continue
				}
				node, found := indexes[i][key]
				if ratings[i] == 4 && key.maskIndex > 0 && (!found || !maskTransitionNodeHasTemplate(node, key)) {
					continue
				}
				if changes[i] == nil {
					changes[i] = map[maskTransitionKey]map[string]string{}
				}
				if changes[i][key] == nil {
					changes[i][key] = map[string]string{}
				}
				changes[i][key][name] = value.value
			}
		}
	}
	return changes, nil
}
