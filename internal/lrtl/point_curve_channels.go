package lrtl

import "fmt"

func toneCurveChannelProperties(channel string) (string, string, error) {
	switch channel {
	case "master":
		return "ToneCurvePV2012", "ToneCurve", nil
	case "red":
		return "ToneCurvePV2012Red", "ToneCurveRed", nil
	case "green":
		return "ToneCurvePV2012Green", "ToneCurveGreen", nil
	case "blue":
		return "ToneCurvePV2012Blue", "ToneCurveBlue", nil
	default:
		return "", "", fmt.Errorf("channel must be master, red, green, or blue")
	}
}

func readEditableGlobalCurve(packet *XMPPacket, channel string) (string, []toneCurvePoint, bool, bool, error) {
	primary, fallback, err := toneCurveChannelProperties(channel)
	if err != nil {
		return "", nil, false, false, err
	}
	points, found, editable, err := readXMPToneCurvePoints(packet, primary)
	if err != nil {
		return "", nil, false, false, err
	}
	property := primary
	if !found && fallback != "" {
		points, found, editable, err = readXMPToneCurvePoints(packet, fallback)
		if err != nil {
			return "", nil, false, false, err
		}
		if found {
			property = fallback
		}
	}
	if !found {
		points = []toneCurvePoint{{x: 0, y: 0}, {x: 255, y: 255}}
		editable = true
	}
	if len(points) > 64 || points[0].x != 0 || points[len(points)-1].x != 255 {
		editable = false
	}
	return property, points, found, editable, nil
}
