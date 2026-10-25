package lrtl

import (
	"encoding/binary"
	"fmt"
	"math"
	"strings"
)

var dngCameraProfileTags = map[uint16]struct{}{
	50721: {}, // ColorMatrix1
	50722: {}, // ColorMatrix2
	50725: {}, // ReductionMatrix1
	50726: {}, // ReductionMatrix2
	50778: {}, // CalibrationIlluminant1
	50779: {}, // CalibrationIlluminant2
	50932: {}, // ProfileCalibrationSignature
	50936: {}, // ProfileName
	50937: {}, // ProfileHueSatMapDims
	50938: {}, // ProfileHueSatMapData1
	50939: {}, // ProfileHueSatMapData2
	50940: {}, // ProfileToneCurve
	50964: {}, // ForwardMatrix1
	50965: {}, // ForwardMatrix2
	50981: {}, // ProfileLookTableDims
	50982: {}, // ProfileLookTableData
	51107: {}, // ProfileHueSatMapEncoding
	51108: {}, // ProfileLookTableEncoding
	51109: {}, // BaselineExposureOffset
	52525: {}, // ProfileGainTableMap
	52529: {}, // CalibrationIlluminant3
	52531: {}, // ColorMatrix3
	52532: {}, // ForwardMatrix3
	52533: {}, // IlluminantData1
	52534: {}, // IlluminantData2
	52535: {}, // IlluminantData3
	52537: {}, // ProfileHueSatMapData3
	52538: {}, // ReductionMatrix3
	52544: {}, // ProfileGainTableMap2
}

func readDNGCameraProfile(scanner *tiffPreviewScanner, offset uint64) (string, map[uint16]tiffPreviewEntry, error) {
	if !tiffRange(offset, 8, uint64(len(scanner.data))) {
		return "", nil, fmt.Errorf("DNG camera profile header is outside the file")
	}
	profile := scanner.data[offset:]
	var order binary.ByteOrder = scanner.order
	switch string(profile[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return "", nil, fmt.Errorf("invalid DNG camera profile byte-order mark")
	}
	if order.Uint16(profile[2:]) != 0x4352 {
		return "", nil, fmt.Errorf("invalid DNG camera profile magic")
	}
	ifdOffset := order.Uint32(profile[4:])
	if ifdOffset < 8 || !tiffRange(uint64(ifdOffset), 2, uint64(len(profile))) {
		return "", nil, fmt.Errorf("invalid DNG camera profile IFD offset")
	}
	profileScanner := &tiffPreviewScanner{data: profile, order: order}
	entries, _, err := profileScanner.readDirectory(ifdOffset)
	if err != nil {
		return "", nil, fmt.Errorf("read DNG camera profile IFD: %w", err)
	}
	mapping := make(map[uint16]tiffPreviewEntry, len(entries))
	for _, entry := range entries {
		if _, duplicate := mapping[entry.tag]; duplicate {
			return "", nil, fmt.Errorf("duplicate DNG camera profile tag %d", entry.tag)
		}
		mapping[entry.tag] = entry
	}
	profileFields := &dngFields{scanner: profileScanner, entries: mapping}
	name := profileFields.text(50936)
	if profileFields.err != nil {
		return "", nil, profileFields.err
	}
	selected := make(map[uint16]tiffPreviewEntry)
	for tag := range dngCameraProfileTags {
		if entry, found := mapping[tag]; found {
			selected[tag] = entry
		}
	}
	return name, selected, nil
}

func selectDNGCameraProfile(fields *dngFields, root map[uint16]tiffPreviewEntry, requestedName string) error {
	rootFields := &dngFields{scanner: fields.scanner, entries: root}
	name := strings.TrimSpace(requestedName)
	if name == "" {
		name = rootFields.text(50934) // AsShotProfileName
		if rootFields.err != nil {
			return rootFields.err
		}
	}
	if name == "" {
		return nil
	}
	primaryName := rootFields.text(50936)
	if rootFields.err != nil {
		return rootFields.err
	}
	if name == primaryName {
		return nil
	}
	profiles, found := root[50933] // ExtraCameraProfiles
	if !found {
		return nil
	}
	if profiles.typeID != 4 || profiles.count > maxPreviewIFDs {
		return fmt.Errorf("invalid DNG ExtraCameraProfiles list")
	}
	offsets := rootFields.values(50933)
	if rootFields.err != nil {
		return rootFields.err
	}
	var selected map[uint16]tiffPreviewEntry
	visited := make(map[uint64]bool, len(offsets))
	for _, value := range offsets {
		if value < 8 || value > math.MaxUint32 || value != math.Trunc(value) {
			return fmt.Errorf("invalid DNG camera profile offset")
		}
		offset := uint64(value)
		if visited[offset] {
			continue
		}
		visited[offset] = true
		profileName, entries, err := readDNGCameraProfile(fields.scanner, offset)
		if err != nil {
			return err
		}
		if profileName != name {
			continue
		}
		if selected != nil {
			return fmt.Errorf("DNG camera profile %q is ambiguous", name)
		}
		selected = entries
	}
	for tag, entry := range selected {
		fields.entries[tag] = entry
	}
	return nil
}
