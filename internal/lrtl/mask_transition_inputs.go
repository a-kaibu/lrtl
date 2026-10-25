package lrtl

import (
	"fmt"
	"sort"
)

type maskTransitionInputs struct {
	Indexes []map[maskTransitionKey]maskTransitionNode
	Missing []map[maskTransitionKey]map[string]float64
}

// Registered root columns and first selectors with an existing parent can
// materialize missing resources on keyframes. Registered numeric range fields
// can create their struct; selector LumRange strings are not registered.
func prepareMaskTransitionInputs(indexes []map[maskTransitionKey]maskTransitionNode) maskTransitionInputs {
	plan := maskTransitionInputs{Indexes: make([]map[maskTransitionKey]maskTransitionNode, len(indexes)), Missing: make([]map[maskTransitionKey]map[string]float64, len(indexes))}
	properties := map[maskTransitionKey]map[string]bool{}
	scalarParents := map[maskTransitionKey]bool{}
	for i, index := range indexes {
		plan.Indexes[i] = make(map[maskTransitionKey]maskTransitionNode, len(index))
		for key, node := range index {
			copyNode := node
			copyNode.values = make(map[string]float64, len(node.values))
			for name, value := range node.values {
				copyNode.values[name] = value
			}
			plan.Indexes[i][key] = copyNode
			if _, registered := maskTransitionCorrectionIndexes[key.collection][key.correctionIndex]; !registered || key.maskIndex > 1 || !maskTransitionNodeHasTemplate(node, key) {
				continue
			}
			if properties[key] == nil {
				properties[key] = map[string]bool{}
			}
			if key.maskIndex == 0 {
				if _, found, _ := crsElementValue(unwrapRDFArrayItem(node.correctionItem), "CorrectionActive"); found {
					scalarParents[key] = true
				}
				if node.hasLuminanceRange {
					scalarParents[key] = true
				}
			}
			for name := range node.values {
				if _, registered := maskTransitionSpecs[name]; registered {
					properties[key][name] = true
				}
			}
		}
	}
	// Boolean and root CorrectionRangeMask.LumRange columns can create a
	// parent without numbers. Their values use separate scalar workers.
	for key := range scalarParents {
		for i := range plan.Indexes {
			node := plan.Indexes[i][key]
			node.materializable = true
			plan.Indexes[i][key] = node
		}
	}
	keys := make([]maskTransitionKey, 0, len(properties))
	for key := range properties {
		keys = append(keys, key)
	}
	// A native save writes root columns before nested selector columns. Make
	// parent availability deterministic, independent of Go map iteration.
	sort.Slice(keys, func(i, j int) bool { return keys[i].maskIndex < keys[j].maskIndex })
	for _, key := range keys {
		names := properties[key]
		for name := range names {
			input := make([]transitionScalar, len(indexes))
			for i, index := range indexes {
				if node, found := index[key]; found && maskTransitionNodeHasTemplate(node, key) {
					if value, present := node.values[name]; present {
						input[i] = transitionScalar{fmt.Sprint(value), true}
					}
				}
			}
			filled := fillMissingTransitionScalars(input)
			for i, value := range filled {
				node, found := plan.Indexes[i][key]
				if input[i].present || !value.present {
					continue
				}
				if !found || !maskTransitionNodeHasTemplate(node, key) {
					if key.maskIndex > 0 && !canFillMissingSelector(plan.Indexes[i], key, name) {
						continue
					}
				}
				number, _ := scalarNumber(value.value)
				if node.values == nil {
					node.values = map[string]float64{}
					plan.Indexes[i][key] = node
				}
				node.values[name] = number
				if plan.Missing[i] == nil {
					plan.Missing[i] = map[maskTransitionKey]map[string]float64{}
				}
				if plan.Missing[i][key] == nil {
					plan.Missing[i][key] = map[string]float64{}
				}
				plan.Missing[i][key][name] = number
			}
		}
	}
	return plan
}

// Native selector columns are positional, not SyncID based. Creating their
// first resource requires an existing parent or a prepared root scalar column.
func canFillMissingSelector(index map[maskTransitionKey]maskTransitionNode, key maskTransitionKey, property string) bool {
	if key.maskIndex != 1 || key.correctionIndex < 1 {
		return false
	}
	if _, ok := maskTransitionCorrectionIndexes[key.collection][key.correctionIndex]; !ok {
		return false
	}
	parent := key
	parent.maskIndex = 0
	if index[parent].correctionItem == nil && len(index[parent].values) == 0 && !index[parent].materializable {
		return false
	}
	linear := property == "MaskValue" || property == "ZeroX" || property == "ZeroY" || property == "FullX" || property == "FullY"
	radial := property == "MaskValue" || property == "Top" || property == "Left" || property == "Bottom" || property == "Right" || property == "Angle" || property == "Midpoint" || property == "Roundness" || property == "Feather" || property == "Flipped"
	switch key.collection {
	case "GradientBasedCorrections":
		return linear
	case "CircularGradientBasedCorrections":
		return radial
	case "MaskGroupBasedCorrections":
		return linear || radial
	}
	return false
}
