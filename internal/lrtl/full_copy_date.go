package lrtl

import "strings"

// Full paste reloads its merged XML. This differs from selected-column copying,
// which keeps the target's already loaded date cache verbatim. Reload reads
// CreateDate first (even if empty), falls back only when absent, and normalizes
// the chosen string without calendar validation or a new image EXIF query.
func fullCopyCaptureDate(packet *XMPPacket) (string, error) {
	value, found, err := captureTimestampScalar(packet, "xmp:CreateDate")
	if err != nil {
		return "", err
	}
	if !found {
		value, _, err = captureTimestampScalar(packet, "exif:DateTimeOriginal")
		if err != nil {
			return "", err
		}
	}
	if value == "" {
		return "n/a", nil
	}
	return nativeCaptureDateText(value), nil
}

// The reload's default merge supplies True for a missing/empty flag. The SDK
// accepts boolean words and signed nonzero int32s at this stage, while later
// save eligibility checks the literal word true. Keep that distinction.
func prepareFullCopyHasSettings(packet *XMPPacket) error {
	value, found, err := captureTimestampScalar(packet, "crs:HasSettings")
	if err != nil {
		return err
	}
	if !found || value == "" {
		return setCaptureTimestampScalar(packet, "crs:HasSettings", "True")
	}
	if fullCopySDKBoolean(value) {
		return nil
	}
	return setCaptureTimestampScalar(packet, "crs:HasSettings", "true")
}

func fullCopySDKBoolean(value string) bool {
	switch strings.ToLower(value) {
	case "true", "t", "on", "yes":
		return true
	}
	// Integer.parseInt does not trim whitespace or allow a second sign. The
	// shared registered-column converter is more permissive about both, so
	// reject those forms first. Its BMP decimal-digit handling is reusable.
	unsigned := value
	if strings.HasPrefix(unsigned, "+") || strings.HasPrefix(unsigned, "-") {
		unsigned = unsigned[1:]
	}
	if unsigned == "" || unsigned != selectedJavaTrim(unsigned) || strings.HasPrefix(unsigned, "+") || strings.HasPrefix(unsigned, "-") {
		return false
	}
	number, valid := selectedJavaInteger(value)
	return valid && number != 0
}
