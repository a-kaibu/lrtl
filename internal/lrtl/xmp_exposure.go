package lrtl

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/beevik/etree"
)

type xmpExposureTarget struct {
	Collection string
	Index      int
}

type xmpExposureWrite struct {
	Target xmpExposureTarget
	Value  float64
}

func globalXMPExposureTarget() xmpExposureTarget {
	return xmpExposureTarget{}
}

func (target xmpExposureTarget) String() string {
	if target.Collection == "" {
		return "global"
	}
	return fmt.Sprintf("%s:%d", target.Collection, target.Index)
}

func parseXMPExposureTarget(value string) (xmpExposureTarget, error) {
	if value == "global" {
		return globalXMPExposureTarget(), nil
	}
	separator := strings.LastIndexByte(value, ':')
	if separator <= 0 {
		return xmpExposureTarget{}, fmt.Errorf("invalid exposure XMP target %q", value)
	}
	collection := value[:separator]
	if collection != "MaskGroupBasedCorrections" && collection != "GradientBasedCorrections" {
		return xmpExposureTarget{}, fmt.Errorf("unsupported exposure XMP target %q", value)
	}
	index, err := strconv.Atoi(value[separator+1:])
	if err != nil || index < 1 {
		return xmpExposureTarget{}, fmt.Errorf("invalid exposure XMP target %q", value)
	}
	return xmpExposureTarget{Collection: collection, Index: index}, nil
}

func findHolyGrailXMPTarget(packet *XMPPacket) xmpExposureTarget {
	return findLRTExposureTarget(packet, "HG", 5)
}

func findLRTExposureTarget(packet *XMPPacket, role string, legacyIndex int) xmpExposureTarget {
	maskGroups := packet.findRootProperty(crsNamespace, "MaskGroupBasedCorrections")
	maskGroupItems := rdfArrayItems(maskGroups.element)
	gradients := packet.findRootProperty(crsNamespace, "GradientBasedCorrections")
	gradientItems := rdfArrayItems(gradients.element)
	for _, candidate := range []struct {
		collection string
		items      []*etree.Element
	}{
		{collection: "MaskGroupBasedCorrections", items: maskGroupItems},
		{collection: "GradientBasedCorrections", items: gradientItems},
	} {
		for index, item := range candidate.items {
			correction := unwrapRDFArrayItem(item)
			name, found, err := crsElementValue(correction, "CorrectionName")
			if err == nil && found && strings.TrimSpace(name) == "#LRT internal use ("+role+")" {
				return xmpExposureTarget{Collection: candidate.collection, Index: index + 1}
			}
		}
	}
	if len(gradientItems) >= 7 &&
		isLegacyLRTInternalGradient(gradientItems[4]) &&
		isLegacyLRTInternalGradient(gradientItems[5]) &&
		isLegacyLRTInternalGradient(gradientItems[6]) {
		return xmpExposureTarget{Collection: "GradientBasedCorrections", Index: legacyIndex}
	}
	return globalXMPExposureTarget()
}

func isLegacyLRTInternalGradient(item *etree.Element) bool {
	correction := unwrapRDFArrayItem(item)
	masks := findCRSDescendant(correction, "CorrectionMasks")
	maskItems := rdfArrayItems(masks)
	if len(maskItems) != 1 {
		return false
	}
	mask := unwrapRDFArrayItem(maskItems[0])
	name, found, err := crsElementValue(mask, "MaskName")
	return err == nil && found && strings.TrimSpace(name) == legacyLRTInternalMaskName
}

func xmpExposureTargetElement(packet *XMPPacket, target xmpExposureTarget) (*etree.Element, error) {
	if target.Collection == "" {
		return nil, nil
	}
	if target.Index < 1 {
		return nil, fmt.Errorf("invalid exposure XMP array index %d", target.Index)
	}
	property := packet.findRootProperty(crsNamespace, target.Collection)
	items := rdfArrayItems(property.element)
	if target.Index > len(items) {
		return nil, fmt.Errorf("crs:%s[%d] no longer exists", target.Collection, target.Index)
	}
	return unwrapRDFArrayItem(items[target.Index-1]), nil
}

func getXMPExposureTargetValue(packet *XMPPacket, target xmpExposureTarget) (float64, bool, error) {
	if target.Collection == "" {
		text, found, err := packet.GetScalar("crs:Exposure2012")
		if err != nil || !found {
			return 0, found, err
		}
		value, valid := scalarNumber(text)
		if !valid {
			return 0, true, fmt.Errorf("crs:Exposure2012 is not numeric")
		}
		return value, true, nil
	}
	element, err := xmpExposureTargetElement(packet, target)
	if err != nil {
		return 0, false, err
	}
	text, found, err := crsElementValue(element, "LocalExposure2012")
	if err != nil || !found {
		return 0, found, err
	}
	value, valid := scalarNumber(text)
	if !valid {
		return 0, true, fmt.Errorf("crs:%s[%d]/crs:LocalExposure2012 is not numeric", target.Collection, target.Index)
	}
	return value, true, nil
}

func readHolyGrailRenderCorrection(packet *XMPPacket) (float64, *etree.Element, error) {
	return readManagedExposureRenderCorrection(packet, holyGrailCorrectionProperty, holyGrailTargetProperty)
}

func readDeflickerRenderCorrection(packet *XMPPacket) (float64, *etree.Element, error) {
	return readManagedExposureRenderCorrection(packet, deflickerCorrectionProperty, deflickerTargetProperty)
}

func readManagedExposureRenderCorrection(packet *XMPPacket, correctionProperty, targetProperty string) (float64, *etree.Element, error) {
	correctionText, hasCorrection, err := packet.GetScalar(correctionProperty)
	if err != nil {
		return 0, nil, err
	}
	targetText, hasTarget, err := packet.GetScalar(targetProperty)
	if err != nil {
		return 0, nil, err
	}
	if hasTarget && !hasCorrection {
		return 0, nil, fmt.Errorf("orphaned lrtl property %s", targetProperty)
	}
	if !hasCorrection || !hasTarget {
		return 0, nil, nil
	}
	target, err := parseXMPExposureTarget(targetText)
	if err != nil {
		return 0, nil, err
	}
	if target.Collection == "" {
		return 0, nil, nil
	}
	correction, valid := scalarNumber(correctionText)
	if !valid || math.Abs(correction) > maxRenderXMPExposureStops {
		return 0, nil, fmt.Errorf("%s must be a finite number between -%d and +%d stops", correctionProperty, maxRenderXMPExposureStops, maxRenderXMPExposureStops)
	}
	element, err := xmpExposureTargetElement(packet, target)
	if err != nil {
		return 0, nil, err
	}
	// The target can contain an existing user offset in addition to our delta.
	// Render its actual value, while excluding this reserved slot from the
	// ordinary local-mask pass so the exposure is applied exactly once.
	value, _, err := getXMPExposureTargetValue(packet, target)
	if err != nil {
		return 0, nil, err
	}
	if math.Abs(value) > maxRenderXMPExposureStops {
		return 0, nil, fmt.Errorf("managed local exposure exceeds supported range")
	}
	active, _, err := xmpLocalBool(element, "CorrectionActive", true)
	if err != nil {
		return 0, nil, err
	}
	if !active {
		return 0, element, nil
	}
	amount, _, err := xmpLocalNumber(element, "CorrectionAmount", 1, -10, 10)
	if err != nil {
		return 0, nil, err
	}
	return value * amount, element, nil
}

func setXMPExposureTargetValue(packet *XMPPacket, target xmpExposureTarget, value float64) error {
	if target.Collection == "" {
		return packet.SetScalar("crs:Exposure2012", formatNumber(value))
	}
	element, err := xmpExposureTargetElement(packet, target)
	if err != nil {
		return err
	}
	return setCRSElementValue(element, "LocalExposure2012", formatNumber(value))
}

func xmpExposureTargetLabel(target xmpExposureTarget) string {
	if target.Collection == "" {
		return "Exposure2012"
	}
	return fmt.Sprintf("%s[%d].LocalExposure2012", target.Collection, target.Index)
}
