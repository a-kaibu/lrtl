package lrtl

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

func nativeFrameSuffix(path string) string {
	name := filepath.Base(path)
	index := strings.LastIndexByte(name, '.')
	if index <= 0 {
		return ""
	}
	return strings.ToLower(name[index+1:])
}

func nativeFlatFrameFile(path string) bool {
	switch nativeFrameSuffix(path) {
	case "jpg", "jpeg", "png", "tif", "tiff":
		return true
	}
	return false
}

// The CSV reader's initial classification is subsequently reconciled against
// the reference image. JPEG/TIFF/PNG become FLAT; DNG uses AsShotNeutral (50728).
func nativeSequenceQuality(firstImage string) (string, error) {
	if nativeFlatFrameFile(firstImage) {
		return "FLAT", nil
	}
	if nativeFrameSuffix(firstImage) != "dng" {
		return "RAW", nil
	}
	neutral, present, err := readTIFFMetadata(firstImage, func(reader *tiffXMPReader, first uint64) ([]byte, bool, error) {
		return reader.scanMetadata(first, func(entry tiffXMPEntry) ([]byte, bool, error) {
			if entry.tag != 50728 {
				return nil, false, nil
			}
			if entry.count == 0 {
				return []byte{}, true, nil
			}
			width := uint64(tiffTypeSize(entry.typeID))
			if width == 0 {
				return nil, false, fmt.Errorf("unsupported AsShotNeutral field type %d", entry.typeID)
			}
			data, err := reader.readEntryData(entry, width)
			if err != nil {
				return nil, false, err
			}
			if entry.typeID == 2 {
				data = bytes.TrimSpace(bytes.TrimRight(data, "\x00"))
			}
			return data, true, nil
		})
	})
	if err != nil {
		return "", fmt.Errorf("read DNG AsShotNeutral for %s: %w", firstImage, err)
	}
	if present && len(neutral) > 0 {
		return "RAW", nil
	}
	return "FLAT", nil
}

func nativeFrameStateReference(packets []*XMPPacket) int {
	for i, packet := range packets {
		value, present, _ := packet.GetRootScalar("xmp:Rating")
		if !present {
			continue
		}
		rating, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err == nil && rating == 4 {
			return i
		}
	}
	return 0
}

// This cache helper supplies only missing numeric cells for exact Custom WB.
// Unlike the edit callback, it treats any non-null quality other than RAW as
// relative WB. Keep that distinction when observing the helper in isolation.
func applyNativeWBDefaults(packet *XMPPacket, quality transitionScalar) (bool, error) {
	before, after, err := saveStateMaps(packet)
	if err != nil {
		return false, err
	}
	mode, err := saveStateScalar(after[pasteName{crsNamespace, "WhiteBalance"}], "crs:WhiteBalance")
	if err != nil {
		return false, err
	}
	if !quality.present || mode.value != "Custom" {
		return false, nil
	}
	defaults := map[string]string{"IncrementalTemperature": "0", "IncrementalTint": "0"}
	if quality.value == "RAW" {
		defaults = map[string]string{"Temperature": "5850", "Tint": "8"}
	}
	for name, value := range defaults {
		if after[pasteName{crsNamespace, name}] == nil {
			if err := setSaveStateScalar(after, name, value); err != nil {
				return false, err
			}
		}
	}
	return writeChangedPasteRoots(packet, before, after)
}

func prepareNativeFrameState(packet *XMPPacket, quality string) error {
	working := &XMPPacket{Document: packet.Document.Copy()}
	before, after, err := saveStateMaps(working)
	if err != nil {
		return err
	}
	if err := setSaveStateProperty(after, pasteName{namespacePrefixes["lrt"], "Quality"}, "lrt", quality); err != nil {
		return err
	}
	if _, err := writeChangedPasteRoots(working, before, after); err != nil {
		return err
	}
	if _, err := applyNativeWBDefaults(working, transitionScalar{quality, true}); err != nil {
		return err
	}
	context, err := readNativeSaveStateContext(working)
	if err != nil {
		return err
	}
	if _, err := applyNativeSaveState(working, context); err != nil {
		return err
	}
	before, _, err = saveStateMaps(packet)
	if err != nil {
		return err
	}
	after, _, err = saveStateMaps(working)
	if err != nil {
		return err
	}
	_, err = writeChangedPasteRoots(packet, before, after)
	return err
}
