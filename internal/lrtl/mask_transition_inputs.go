package lrtl

import (
	"fmt"
	"strings"
)

type maskTransitionInputs struct {
	Indexes []map[maskTransitionKey]maskTransitionNode
	Missing []map[maskTransitionKey]map[string]float64
}

// Root correction columns can materialize missing array resources when saved,
// including keyframes. Selector/range resources retain their separate policy.
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
				if input[i].present || !value.present {
					continue
				}
				if (!found || !maskTransitionNodeHasTemplate(node, key)) && (key.maskIndex != 0 || strings.HasPrefix(name, "RangeMask")) {
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
