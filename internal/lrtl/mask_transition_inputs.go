package lrtl

import (
	"fmt"
	"strings"
)

type maskTransitionInputs struct {
	Indexes []map[maskTransitionKey]maskTransitionNode
	Missing []map[maskTransitionKey]map[string]float64
}

// Fill registered numeric paths only where their correction/selector resource
// already exists. Missing-resource cloning keeps its separate transition policy.
func prepareMaskTransitionInputs(indexes []map[maskTransitionKey]maskTransitionNode) maskTransitionInputs {
	plan := maskTransitionInputs{Indexes: make([]map[maskTransitionKey]maskTransitionNode, len(indexes)), Missing: make([]map[maskTransitionKey]map[string]float64, len(indexes))}
	properties := map[maskTransitionKey]map[string]bool{}
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
			for name := range node.values {
				if _, registered := maskTransitionSpecs[name]; registered {
					properties[key][name] = true
				}
			}
		}
	}
	for key, names := range properties {
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
				if !found || !maskTransitionNodeHasTemplate(node, key) || input[i].present || !value.present {
					continue
				}
				if strings.HasPrefix(name, "RangeMask") {
					item := node.correctionItem
					if key.maskIndex > 0 {
						item = node.maskItem
					}
					if correctionRangeMaskElement(unwrapRDFArrayItem(item)) == nil {
						continue
					}
				}
				number, _ := scalarNumber(value.value)
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
