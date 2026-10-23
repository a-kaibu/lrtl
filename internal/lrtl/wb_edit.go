package lrtl

import (
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
)

type wbEditControl struct {
	name               string
	min, max, fallback int
}

// Observed 7.5.5 control ranges and initial slider values. These describe
// numeric editing, distinct from unclamped metadata copying and interpolation.
var wbEditControls = []wbEditControl{
	{"Temperature", 2000, 50000, 5850},
	{"Tint", -150, 150, 8},
	{"IncrementalTemperature", -100, 100, 0},
	{"IncrementalTint", -100, 100, 0},
}

func wbEditControlIndex(property string) int {
	name := strings.TrimPrefix(property, "crs:")
	for i, control := range wbEditControls {
		if name == control.name {
			return i
		}
	}
	return -1
}

func wbEditInteger(index int, value float64) string {
	control := wbEditControls[index]
	value = math.Max(float64(control.min), math.Min(float64(control.max), math.Trunc(value)))
	return strconv.Itoa(int(value))
}

func wbEditCompanion(nodes map[pasteName]*pasteNode, index int) (string, error) {
	control := wbEditControls[index]
	value, err := saveStateScalar(nodes[pasteName{crsNamespace, control.name}], "crs:"+control.name)
	if err != nil {
		return "", err
	}
	if !value.present {
		return strconv.Itoa(control.fallback), nil
	}
	columns, err := parseSelectedColumns(control.name)
	if err != nil {
		return "", err
	}
	normalized := normalizeSelectedColumn(columns[0], value)
	number, err := strconv.ParseFloat(normalized.value, 64)
	if err != nil {
		return "", err
	}
	return wbEditInteger(index, number), nil
}

func writeWBEditPreview(writer io.Writer, packet *XMPPacket) {
	fmt.Fprint(writer, "  WB:")
	for _, name := range []string{"WhiteBalance", "Temperature", "Tint", "IncrementalTemperature", "IncrementalTint"} {
		value, present, _ := packet.GetRootScalar("crs:" + name)
		if present {
			fmt.Fprintf(writer, " %s=%q", name, value)
		} else {
			fmt.Fprintf(writer, " %s=(absent)", name)
		}
	}
	fmt.Fprintln(writer)
}

// Global numeric edits must not find or replace a same-name field in a mask,
// preset or other nested Description. Compose the WB pair before committing,
// so validation failures leave the original packet untouched.
func applyNumericXMPSet(packet *XMPPacket, edit numericXMPSet) error {
	before, after, err := saveStateMaps(packet)
	if err != nil {
		return err
	}
	index := wbEditControlIndex(edit.property)
	if index >= 0 {
		quality, err := saveStateScalar(after[pasteName{namespacePrefixes["lrt"], "Quality"}], "lrt:Quality")
		if err != nil {
			return err
		}
		flat := quality.value == "FLAT"
		if (index >= 2) == flat {
			companion := index ^ 1
			value, err := wbEditCompanion(after, companion)
			if err != nil {
				return err
			}
			if err := setSaveStateScalar(after, wbEditControls[companion].name, value); err != nil {
				return err
			}
			if err := setSaveStateScalar(after, "WhiteBalance", "Custom"); err != nil {
				return err
			}
			for other, control := range wbEditControls {
				if other/2 != index/2 {
					delete(after, pasteName{crsNamespace, control.name})
				}
			}
		}
	}
	uri, name, err := expandedName(edit.property)
	if err != nil {
		return err
	}
	prefix := "crs"
	if edit.previewEV {
		prefix = "lrt"
	}
	if err := setSaveStateProperty(after, pasteName{uri, name}, prefix, edit.value); err != nil {
		return err
	}
	_, err = writeChangedPasteRoots(packet, before, after)
	return err
}
