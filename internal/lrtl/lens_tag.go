package lrtl

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// LRTimelapse's manual-lens tagger presents these standard aperture stops.
var lensApertureOptions = []string{
	"1.0", "1.1", "1.2", "1.4", "1.6", "1.8", "2.0", "2.2", "2.5", "2.8",
	"3.2", "3.5", "4.0", "4.5", "5.0", "5.6", "6.3", "7.1", "8.0", "9.0",
	"10.0", "11.0", "13.0", "14.0", "16.0", "18.0", "20.0", "22.0", "25.0", "29.0",
	"32.0", "36.0", "40.0", "45.0", "51.0", "57.0", "64.0", "72.0", "80.0", "90.0",
}

type lensTagChange struct {
	Frame       Frame
	Packet      *XMPPacket
	Previous    string
	HadPrevious bool
}

func normalizeLensAperture(value string) (string, error) {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(strings.ToLower(value), "f/") {
		value = strings.TrimSpace(value[2:])
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || parsed <= 0 || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return "", fmt.Errorf("aperture must be one of the LRT Lens Tagger stops from f/1 to f/90")
	}
	for _, option := range lensApertureOptions {
		candidate, _ := strconv.ParseFloat(option, 64)
		if math.Abs(parsed-candidate) < 1e-6 {
			return option, nil
		}
	}
	return "", fmt.Errorf("unsupported aperture f/%s; choose one of the LRT Lens Tagger stops from f/1 to f/90", value)
}

func buildLensTagPlan(frames []Frame, aperture string) ([]lensTagChange, error) {
	if len(frames) == 0 {
		return nil, fmt.Errorf("sequence contains no images")
	}
	canonical, err := normalizeLensAperture(aperture)
	if err != nil {
		return nil, err
	}
	changes := make([]lensTagChange, 0, len(frames))
	for _, frame := range frames {
		packet, err := PacketForSidecar(frame.OutputSidecar())
		if err != nil {
			return nil, fmt.Errorf("%s: %w", frame.OutputSidecar(), err)
		}
		previous, found, err := packet.GetScalar("lrt:Aperture")
		if err != nil {
			return nil, fmt.Errorf("%s: %w", frame.OutputSidecar(), err)
		}
		if err := packet.SetScalar("lrt:Aperture", canonical); err != nil {
			return nil, fmt.Errorf("%s: %w", frame.OutputSidecar(), err)
		}
		changes = append(changes, lensTagChange{
			Frame: frame, Packet: packet, Previous: previous, HadPrevious: found,
		})
	}
	return changes, nil
}
