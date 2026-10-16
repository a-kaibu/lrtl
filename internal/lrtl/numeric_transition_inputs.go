package lrtl

import "fmt"

// The installed metadata preflight excludes the four WB number columns. Its
// other exclusions (WB name, dates and image dimensions) are not numeric CRS
// transition columns in lrtl.
var numericPreflightExclusions = map[string]bool{
	"Temperature": true, "Tint": true, "IncrementalTemperature": true, "IncrementalTint": true,
}

type numericTransitionInputs struct {
	Values  []map[string]float64
	Missing []map[string]float64
}

func prepareNumericTransitionInputs(packets []*XMPPacket) (numericTransitionInputs, error) {
	plan := numericTransitionInputs{Values: make([]map[string]float64, len(packets)), Missing: make([]map[string]float64, len(packets))}
	for _, name := range NumericCRSProperties {
		if _, registered := globalTransitionSpecs[name]; !registered {
			continue
		}
		input := make([]transitionScalar, len(packets))
		for i, packet := range packets {
			text, found, err := packet.GetScalar("crs:" + name)
			if err != nil {
				return plan, fmt.Errorf("frame %d crs:%s: %w", i+1, name, err)
			}
			if found {
				if _, valid := scalarNumber(text); !valid {
					return plan, fmt.Errorf("frame %d crs:%s must be a finite number", i+1, name)
				}
			}
			input[i] = transitionScalar{text, found}
		}
		filled := input
		if !numericPreflightExclusions[name] {
			filled = fillMissingTransitionScalars(input)
		}
		for i, value := range filled {
			if !value.present {
				continue
			}
			number, _ := scalarNumber(value.value)
			if plan.Values[i] == nil {
				plan.Values[i] = make(map[string]float64)
			}
			plan.Values[i][name] = number
			if !input[i].present {
				if plan.Missing[i] == nil {
					plan.Missing[i] = make(map[string]float64)
				}
				plan.Missing[i][name] = number
			}
		}
	}
	return plan, nil
}
