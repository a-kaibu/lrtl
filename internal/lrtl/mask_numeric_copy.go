package lrtl

// Only registered positional columns take the native automatic worker's
// reference-copy branch. The SyncID fallback has its own interpolation policy.
func maskNumericCopyReferences(keys []int, indexes []map[maskTransitionKey]maskTransitionNode, key maskTransitionKey) map[string]float64 {
	if _, registered := maskTransitionCorrectionIndexes[key.collection][key.correctionIndex]; !registered || key.maskIndex > 1 || len(keys) == 0 {
		return nil
	}
	reference, found := indexes[keys[0]][key]
	if !found || !maskTransitionNodeHasTemplate(reference, key) {
		return nil
	}
	values := make([]map[string]float64, len(indexes))
	for i, index := range indexes {
		values[i] = index[key].values
	}
	copies := make(map[string]float64)
	for name := range reference.values {
		if spec, registered := maskTransitionSpecs[name]; registered {
			if value, copy := numericColumnCopyReference(values, name, keys, spec.integer); copy {
				copies[name] = value
			}
		}
	}
	return copies
}
