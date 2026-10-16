package lrtl

import (
	"fmt"
	"unicode"
)

// Root scalar columns observed in the installed 7.5.5 registry. These are
// separate from numeric spline columns and structured XMP equalizers.
var rootTransitionBooleans = []string{"HasSettings", "HasCrop", "UprightPreview", "ConvertToGrayscale"}
var rootTransitionStrings = []string{
	"CompatibleVersion", "CameraProfile", "CameraProfileDigest", "LookName",
	"ToneCurveName2012", "WhiteBalance", "LensProfileDigest", "LensProfileFilename",
	"LensProfileName", "LensProfileSetup", "UprightDependentDigest", "UprightGuidedDependentDigest",
	"UprightTransform_0", "UprightTransform_1", "UprightTransform_2", "UprightTransform_3", "UprightTransform_4", "UprightTransform_5",
	"UprightFourSegments_0", "UprightFourSegments_1", "UprightFourSegments_2", "UprightFourSegments_3",
}

type transitionScalar struct {
	value   string
	present bool
}

// LRT's pre-transition helper carries known values forward, then backfills the
// leading missing run. Empty strings are present values, not missing ones.
func fillMissingTransitionScalars(input []transitionScalar) []transitionScalar {
	values := append([]transitionScalar(nil), input...)
	var previous transitionScalar
	for i, value := range values {
		if value.present {
			previous = value
		} else if previous.present {
			values[i] = previous
		}
	}
	previous = transitionScalar{}
	for i := len(values) - 1; i >= 0; i-- {
		if values[i].present {
			previous = values[i]
		} else if previous.present {
			values[i] = previous
		}
	}
	return values
}

func equalizeTransitionStrings(input []transitionScalar) []transitionScalar {
	values := fillMissingTransitionScalars(input)
	if len(values) == 0 {
		return values
	}
	// The outer Auto-Transition worker skips already equal columns using Java's
	// equalsIgnoreCase. Preserve spelling when that gate skips the string worker.
	equal := true
	for _, value := range values {
		if value.present != values[0].present || !javaScalarEqualIgnoreCase(value.value, values[0].value) {
			equal = false
			break
		}
	}
	if equal {
		return values
	}
	for _, source := range values {
		if source.present && source.value != "" {
			for i := range values {
				values[i] = source
			}
			break
		}
	}
	return values
}

// Java compares simple uppercase mappings, then lowercase of those mappings.
// In particular, the dotted/dotless I variants share its ignore-case group.
func javaScalarEqualIgnoreCase(left, right string) bool {
	a, b := []rune(left), []rune(right)
	if len(a) != len(b) {
		return false
	}
	for i, x := range a {
		y := b[i]
		if x == y {
			continue
		}
		x, y = unicode.ToUpper(x), unicode.ToUpper(y)
		if x != y && unicode.ToLower(x) != unicode.ToLower(y) {
			return false
		}
	}
	return true
}

type rootScalarChanges struct {
	Values   map[string]string
	Removals []string
}

// Model the root-column dispatch observed in the outer automatic worker. The
// direct column helper alone does not account for reference/equal/sparse paths.
func rootAutoTransitionScalars(input []transitionScalar, ratings []int, property string) []transitionScalar {
	values := append([]transitionScalar(nil), input...)
	if len(values) < 2 {
		return values
	}
	reference, keys := -1, 0
	for i, rating := range ratings {
		if rating == 4 {
			keys++
			if reference < 0 {
				reference = i
			}
		}
	}
	if reference < 0 {
		reference = 0
	}
	if property == "WhiteBalance" {
		for i, rating := range ratings {
			if rating == 4 && values[i].present && values[i].value == "Custom" {
				for j := range values {
					values[j] = transitionScalar{"Custom", true}
				}
				break
			}
		}
	}
	if !values[reference].present {
		return make([]transitionScalar, len(values))
	}
	equal := keys <= 1
	anchors := keys
	if ratings[0] != 4 {
		anchors++
	}
	if ratings[len(values)-1] != 4 {
		anchors++
	}
	if keys > 1 {
		equal = true
		first := transitionScalar{}
		for i, value := range values {
			if ratings[i] != 4 && i != 0 && i != len(values)-1 {
				continue
			}
			if !first.present {
				first = value
			}
			if value.present != first.present || !javaScalarEqualIgnoreCase(value.value, first.value) {
				equal = false
				break
			}
		}
	}
	if equal {
		for i := range values {
			values[i] = values[reference]
		}
		return values
	}
	// The sparse/linear worker does not equalize strings. Its missing-value
	// preflight runs when a missing value follows an existing one.
	if anchors <= 2 {
		seen := false
		for _, value := range values {
			if value.present {
				seen = true
			} else if seen {
				return fillMissingTransitionScalars(values)
			}
		}
		return values
	}
	if property == "HasSettings" || property == "HasCrop" || property == "UprightPreview" || property == "ConvertToGrayscale" || property == "CorrectionActive" || property == "Flipped" {
		return fillMissingTransitionScalars(values)
	}
	return equalizeTransitionStrings(values)
}

func planRootScalarTransitions(packets []*XMPPacket) ([]rootScalarChanges, error) {
	changes := make([]rootScalarChanges, len(packets))
	ratings := make([]int, len(packets))
	for i, packet := range packets {
		ratings[i], _ = packet.Rating()
	}
	for group, properties := range [][]string{rootTransitionBooleans, rootTransitionStrings} {
		for _, name := range properties {
			qualified := "crs:" + name
			input := make([]transitionScalar, len(packets))
			for i, packet := range packets {
				value, found, err := packet.GetScalar(qualified)
				if err != nil {
					return nil, fmt.Errorf("frame %d %s: %w", i+1, qualified, err)
				}
				if group == 0 && found {
					if _, err := parseCameraRawBool(value); err != nil {
						return nil, fmt.Errorf("frame %d %s: %w", i+1, qualified, err)
					}
				}
				input[i] = transitionScalar{value, found}
			}
			output := rootAutoTransitionScalars(input, ratings, name)
			clearWhiteBalanceNumbers := false
			if name == "WhiteBalance" && len(packets) > 1 {
				reference := 0
				for i, rating := range ratings {
					if rating == 4 {
						reference = i
						break
					}
				}
				clearWhiteBalanceNumbers = input[reference].present && input[reference].value == "As Shot"
				for i, rating := range ratings {
					if rating == 4 && input[i].present && input[i].value == "Custom" {
						clearWhiteBalanceNumbers = false
						break
					}
				}
			}
			for i, value := range output {
				if !value.present && input[i].present {
					changes[i].Removals = append(changes[i].Removals, qualified)
				}
				if value.present && (!input[i].present || value.value != input[i].value) {
					if changes[i].Values == nil {
						changes[i].Values = map[string]string{}
					}
					changes[i].Values[qualified] = value.value
				}

				if clearWhiteBalanceNumbers {
					for _, temperature := range []string{"Temperature", "Tint", "IncrementalTemperature", "IncrementalTint"} {
						qualified := "crs:" + temperature
						if _, found, err := packets[i].GetScalar(qualified); err != nil {
							return nil, err
						} else if found {
							changes[i].Removals = append(changes[i].Removals, qualified)
						}
					}
				}
			}
		}
	}
	return changes, nil
}
