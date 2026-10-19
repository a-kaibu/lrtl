package lrtl

import "fmt"

// Native sequence loading checks the first image for MaskGroup, then Gradient.
// An undetected layout uses the application preference. Use our modern default
// unless the caller explicitly selects the corresponding legacy preference.
func transitionMaskFormat(packets []*XMPPacket, requested string) (string, error) {
	switch requested {
	case "legacy", "modern":
		return requested, nil
	case "auto":
		if len(packets) != 0 {
			for _, candidate := range []struct{ property, format string }{
				{"MaskGroupBasedCorrections", "modern"},
				{"GradientBasedCorrections", "legacy"},
			} {
				property := packets[0].findRootProperty(crsNamespace, candidate.property)
				if property.element != nil || property.attribute != nil {
					return candidate.format, nil
				}
			}
		}
		return "modern", nil
	default:
		return "", fmt.Errorf("mask format must be auto, legacy or modern: %s", requested)
	}
}
