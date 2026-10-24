package lrtl

import (
	"fmt"
	"strings"
	"time"
)

// Native uses YearOfEra, seconds (without fractions), and a compact offset
// with optional offset seconds. UTC is +0000 rather than Z.
func formatNativeMetadataDate(stamp time.Time) string {
	year := stamp.Year()
	if year <= 0 {
		year = 1 - year
	}
	yearText := fmt.Sprintf("%04d", year)
	if year > 9999 {
		yearText = "+" + yearText
	}
	_, offset := stamp.Zone()
	sign := '+'
	if offset < 0 {
		sign = '-'
		offset = -offset
	}
	zone := fmt.Sprintf("%c%02d%02d", sign, offset/3600, offset/60%60)
	if offset%60 != 0 {
		zone += fmt.Sprintf("%02d", offset%60)
	}
	return fmt.Sprintf("%s-%02d-%02dT%02d:%02d:%02d%s", yearText, stamp.Month(), stamp.Day(), stamp.Hour(), stamp.Minute(), stamp.Second(), zone)
}

func nativeMetadataDateEligible(packet *XMPPacket, captureDate string) (bool, error) {
	if captureDate == "" {
		return false, nil
	}
	value, found, err := captureTimestampScalar(packet, "crs:HasSettings")
	return found && strings.EqualFold(value, "true"), err
}

func applyNativeMetadataDate(packet *XMPPacket, captureDate, value string) (bool, error) {
	eligible, err := nativeMetadataDateEligible(packet, captureDate)
	if err != nil || !eligible {
		return false, err
	}
	if err := setCaptureTimestampScalar(packet, "xmp:MetadataDate", value); err != nil {
		return false, err
	}
	return true, nil
}

func validateNativeMetadataDateTarget(packet *XMPPacket, captureDate string) error {
	eligible, err := nativeMetadataDateEligible(packet, captureDate)
	if err != nil || !eligible {
		return err
	}
	root := packet.findRootProperty(xmpNamespace, "MetadataDate")
	if root.attribute != nil || root.element == nil {
		return nil
	}
	node, err := parsePasteNode(root.element, 0)
	if err != nil {
		return err
	}
	if node.Kind != "scalar" && node.Kind != "uri" {
		return fmt.Errorf("xmp:MetadataDate must be a scalar for native save preparation")
	}
	return nil
}

// Resolve every new clock value and validate every final packet before the
// first filesystem write. Planning and previews never call the wall clock.
func prepareInitializationWrites(changes []initializationChange, clock func() time.Time) ([]initializationChange, error) {
	prepared := append([]initializationChange(nil), changes...)
	for i, change := range changes {
		packet := &XMPPacket{Document: change.Packet.Document.Copy()}
		eligible, err := nativeMetadataDateEligible(packet, change.CaptureDate)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", change.Frame.OutputSidecar(), err)
		}
		if eligible {
			if _, err := applyNativeMetadataDate(packet, change.CaptureDate, formatNativeMetadataDate(clock())); err != nil {
				return nil, fmt.Errorf("%s: %w", change.Frame.OutputSidecar(), err)
			}
		}
		if _, err := packet.Serialize(); err != nil {
			return nil, err
		}
		prepared[i].Packet = packet
	}
	return prepared, nil
}
