package lrtl

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode"
)

// Observed property names and types, not application code or user metadata.
//
//go:embed data/lrt-columns-7.5.5.json
var selectedColumnsJSON []byte

type selectedColumnStep struct {
	Name   pasteName
	Prefix string
	Index  int
}

type selectedColumn struct {
	Index    int                  `json:"index"`
	Property string               `json:"property"`
	Kind     string               `json:"kind"`
	Steps    []selectedColumnStep `json:"-"`
}

var selectedColumnStepPattern = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)(?:\[([1-9][0-9]*)\])?$`)
var selectedDoublePattern = regexp.MustCompile(`^[+-]?(?:NaN|Infinity|(?:(?:[0-9]+\.?[0-9]*|\.[0-9]+)(?:[eE][+-]?[0-9]+)?|0[xX](?:[0-9a-fA-F]+\.?[0-9a-fA-F]*|\.[0-9a-fA-F]+)[pP][+-]?[0-9]+)[fFdD]?)$`)

func selectedColumnNamespace(prefix string) (string, bool) {
	if prefix == "crd" {
		return "http://ns.adobe.com/camera-raw-defaults/1.0/", true
	}
	value, ok := namespacePrefixes[prefix]
	return value, ok
}

func parseSelectedColumnPath(text string) ([]selectedColumnStep, string, error) {
	text = strings.ReplaceAll(text, ".", "/")
	var steps []selectedColumnStep
	var key strings.Builder
	for _, part := range strings.Split(text, "/") {
		prefix, local := "crs", part
		if strings.Contains(part, ":") {
			prefix, local, _ = strings.Cut(part, ":")
		}
		uri, known := selectedColumnNamespace(prefix)
		match := selectedColumnStepPattern.FindStringSubmatch(local)
		if !known || match == nil {
			return nil, "", fmt.Errorf("invalid registered property path %q", text)
		}
		index := 0
		if match[2] != "" {
			var err error
			index, err = strconv.Atoi(match[2])
			if err != nil || index > 4096 {
				return nil, "", fmt.Errorf("invalid property array index")
			}
		}
		step := selectedColumnStep{pasteName{uri, match[1]}, prefix, index}
		steps = append(steps, step)
		fmt.Fprintf(&key, "{%s}%s[%d]/", uri, match[1], index)
	}
	return steps, key.String(), nil
}

// Definitions and their Steps are immutable after this one-time load.
var selectedColumnRegistry = sync.OnceValues(func() ([]selectedColumn, error) {
	var columns []selectedColumn
	if err := json.Unmarshal(selectedColumnsJSON, &columns); err != nil {
		return nil, err
	}
	for i := range columns {
		steps, _, err := parseSelectedColumnPath(columns[i].Property)
		if err != nil {
			return nil, err
		}
		columns[i].Steps = steps
	}
	return columns, nil
})

var selectedColumnsByPath = sync.OnceValues(func() (map[string]selectedColumn, error) {
	registry, err := selectedColumnRegistry()
	if err != nil {
		return nil, err
	}
	byPath := map[string]selectedColumn{}
	for _, column := range registry {
		_, key, err := parseSelectedColumnPath(column.Property)
		if err != nil {
			return nil, err
		}
		if _, duplicate := byPath[key]; duplicate {
			return nil, fmt.Errorf("duplicate registered property path %s", column.Property)
		}
		byPath[key] = column
	}
	return byPath, nil
})

func parseSelectedColumns(text string) ([]selectedColumn, error) {
	byPath, err := selectedColumnsByPath()
	if err != nil {
		return nil, err
	}
	seen := map[int]bool{}
	var chosen []selectedColumn
	for _, name := range strings.Split(text, ",") {
		name = strings.TrimSpace(name)
		_, key, err := parseSelectedColumnPath(name)
		if err != nil {
			return nil, err
		}
		column, found := byPath[key]
		if !found {
			return nil, fmt.Errorf("property %q is not a registered LRT 7.5.5 column; use --list-properties", name)
		}
		if column.Index < 5 {
			return nil, fmt.Errorf("property %q is outside the native selected-column paste range", name)
		}
		if !seen[column.Index] {
			chosen = append(chosen, column)
			seen[column.Index] = true
		}
	}
	sort.Slice(chosen, func(i, j int) bool { return chosen[i].Index < chosen[j].Index })
	return chosen, nil
}

func selectedJavaTrim(text string) string {
	return strings.TrimFunc(text, func(r rune) bool { return r <= 32 })
}

func selectedJavaDouble(text string) (float64, bool) {
	if strings.HasPrefix(text, "+") {
		text = text[1:]
	}
	text = selectedJavaTrim(text)
	if !selectedDoublePattern.MatchString(text) {
		return 0, false
	}
	switch strings.TrimLeft(text, "+-") {
	case "NaN":
		return math.NaN(), true
	case "Infinity":
		if strings.HasPrefix(text, "-") {
			return math.Inf(-1), true
		}
		return math.Inf(1), true
	}
	if strings.ContainsAny(text[len(text)-1:], "fFdD") {
		text = text[:len(text)-1]
	}
	value, err := strconv.ParseFloat(text, 64)
	if err != nil {
		if numberError, ok := err.(*strconv.NumError); !ok || numberError.Err != strconv.ErrRange {
			return 0, false
		}
	}
	return value, true
}

// Integer.parseInt recognizes BMP decimal digits, including full-width digits.
func selectedJavaInteger(text string) (int32, bool) {
	if strings.HasPrefix(text, "+") {
		text = text[1:]
	}
	text = selectedJavaTrim(text)
	negative := false
	if strings.HasPrefix(text, "-") || strings.HasPrefix(text, "+") {
		negative = text[0] == '-'
		text = text[1:]
	}
	if text == "" {
		return 0, false
	}
	var value int64
	for _, r := range text {
		digit := -1
		for _, entry := range unicode.Digit.R16 {
			if r >= rune(entry.Lo) && r <= rune(entry.Hi) && (r-rune(entry.Lo))%rune(entry.Stride) == 0 {
				digit = int((r-rune(entry.Lo))/rune(entry.Stride)) % 10
				break
			}
		}
		if digit < 0 {
			return 0, false
		}
		value = value*10 + int64(digit)
		limit := int64(math.MaxInt32)
		if negative {
			limit++
		}
		if value > limit {
			return 0, false
		}
	}
	if negative {
		value = -value
	}
	return int32(value), true
}

func selectedJavaRoundInt(value float64) int32 {
	if math.IsNaN(value) {
		return 0
	}
	if value >= 0x1p63 {
		return -1
	}
	if value <= -0x1p63 {
		return 0
	}
	rounded := math.Floor(value)
	if value-rounded >= .5 {
		rounded++
	}
	return int32(int64(rounded))
}

// Formatter's %f rounds the shortest decimal form half up, preserving -0.
func selectedJavaFixed(value float64) string {
	if math.IsNaN(value) {
		return "NaN"
	}
	if math.IsInf(value, 1) {
		return "Infinity"
	}
	if math.IsInf(value, -1) {
		return "-Infinity"
	}
	decimal, _ := new(big.Rat).SetString(strconv.FormatFloat(math.Abs(value), 'g', -1, 64))
	decimal.Mul(decimal, big.NewRat(1000000, 1))
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(decimal.Num(), decimal.Denom(), remainder)
	if new(big.Int).Lsh(remainder, 1).Cmp(decimal.Denom()) >= 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	text := quotient.String()
	if len(text) < 7 {
		text = strings.Repeat("0", 7-len(text)) + text
	}
	text = text[:len(text)-6] + "." + text[len(text)-6:]
	if math.Signbit(value) {
		text = "-" + text
	}
	return text
}

func normalizeSelectedColumn(column selectedColumn, raw transitionScalar) transitionScalar {
	if !raw.present {
		return raw
	}
	switch column.Kind {
	case "Double":
		value, _ := selectedJavaDouble(raw.value)
		raw.value = selectedJavaFixed(value)
	case "Integer":
		value, valid := selectedJavaInteger(raw.value)
		if !valid {
			number, _ := selectedJavaDouble(raw.value)
			value = selectedJavaRoundInt(number)
		}
		raw.value = strconv.FormatInt(int64(value), 10)
	}
	return raw
}

func selectedField(fields []*pasteNode, name pasteName) *pasteNode {
	for _, field := range fields {
		if field.Name == name {
			return field
		}
	}
	return nil
}

func readSelectedColumn(nodes []*pasteNode, column selectedColumn) (transitionScalar, error) {
	fields := nodes
	for i, step := range column.Steps {
		node := selectedField(fields, step.Name)
		if node == nil {
			return transitionScalar{}, nil
		}
		if step.Index > 0 {
			if node.Kind != "Bag" && node.Kind != "Seq" && node.Kind != "Alt" && node.Kind != "AltText" {
				return transitionScalar{}, fmt.Errorf("%s indexes a non-array property", column.Property)
			}
			if step.Index > len(node.Items) {
				return transitionScalar{}, nil
			}
			node = node.Items[step.Index-1]
		}
		if i == len(column.Steps)-1 {
			if node.Kind != "scalar" && node.Kind != "uri" {
				return transitionScalar{}, fmt.Errorf("%s requires a scalar source value", column.Property)
			}
			return normalizeSelectedColumn(column, transitionScalar{node.Value, true}), nil
		}
		if node.Kind != "struct" {
			return transitionScalar{}, fmt.Errorf("%s traverses a non-struct property", column.Property)
		}
		fields = node.Fields
	}
	return transitionScalar{}, nil
}

func selectedStructItem() *pasteNode {
	return &pasteNode{Name: pasteName{rdfNamespace, "li"}, Prefix: "rdf", Kind: "struct"}
}

func prepareSelectedArray(fields *[]*pasteNode, steps []selectedColumnStep) error {
	for i, step := range steps {
		node := selectedField(*fields, step.Name)
		final := i == len(steps)-1
		if node == nil {
			if step.Index > 0 && !final {
				return fmt.Errorf("parent array %s must exist before creating its nested mask array", step.Name.Local)
			}
			node = &pasteNode{Name: step.Name, Prefix: step.Prefix, Kind: "struct"}
			if final {
				node.Kind = "Bag"
			}
			*fields = append(*fields, node)
		}
		if step.Index > 0 {
			if node.Kind != "Bag" && node.Kind != "Seq" && node.Kind != "Alt" {
				return fmt.Errorf("%s is not an array", step.Name.Local)
			}
			if final {
				for appended := 0; len(node.Items) < step.Index && appended < 10; appended++ {
					node.Items = append(node.Items, selectedStructItem())
				}
				return nil
			}
			if step.Index > len(node.Items) {
				return fmt.Errorf("parent array item %s[%d] does not exist", step.Name.Local, step.Index)
			}
			node = node.Items[step.Index-1]
		}
		if node.Kind != "struct" {
			return fmt.Errorf("%s is not a structure", step.Name.Local)
		}
		fields = &node.Fields
	}
	return nil
}

func assignSelectedColumn(fields *[]*pasteNode, steps []selectedColumnStep, value transitionScalar) error {
	step := steps[0]
	node := selectedField(*fields, step.Name)
	if len(steps) == 1 {
		if step.Index != 0 {
			return fmt.Errorf("an array item cannot be selected as a scalar column")
		}
		if !value.present {
			for i, field := range *fields {
				if field == node {
					*fields = append((*fields)[:i], (*fields)[i+1:]...)
					break
				}
			}
			return nil
		}
		if node == nil {
			node = &pasteNode{Name: step.Name, Prefix: step.Prefix, Kind: "scalar"}
			*fields = append(*fields, node)
		}
		if node.Kind != "scalar" && node.Kind != "uri" {
			return fmt.Errorf("%s is not a scalar target", step.Name.Local)
		}
		node.Value = value.value
		return nil
	}
	if node == nil {
		if !value.present {
			return nil
		}
		if step.Index > 0 {
			return fmt.Errorf("missing array %s", step.Name.Local)
		}
		node = &pasteNode{Name: step.Name, Prefix: step.Prefix, Kind: "struct"}
		*fields = append(*fields, node)
	}
	if step.Index > 0 {
		if node.Kind != "Bag" && node.Kind != "Seq" && node.Kind != "Alt" {
			return fmt.Errorf("%s is not an array", step.Name.Local)
		}
		if step.Index > len(node.Items) {
			if !value.present {
				return nil
			}
			return fmt.Errorf("missing structured array item %d after native preparation", step.Index)
		}
		node = node.Items[step.Index-1]
	}
	if node.Kind != "struct" {
		return fmt.Errorf("%s is not a structure", step.Name.Local)
	}
	return assignSelectedColumn(&node.Fields, steps[1:], value)
}

func copySelectedColumnMetadata(destination, source *XMPPacket, columns []selectedColumn) (bool, error) {
	src, err := pastePacketNodes(source)
	if err != nil {
		return false, err
	}
	dst, err := pastePacketNodes(destination)
	if err != nil {
		return false, err
	}
	before, after := map[pasteName]*pasteNode{}, map[pasteName]*pasteNode{}
	fields := make([]*pasteNode, 0, len(dst))
	for _, node := range dst {
		before[node.Name] = node
		fields = append(fields, clonePasteNode(node))
	}
	for _, column := range columns {
		value, err := readSelectedColumn(src, column)
		if err != nil {
			return false, err
		}
		lastArray := -1
		for i, step := range column.Steps {
			if step.Index > 0 {
				lastArray = i
			}
		}
		if value.present && lastArray >= 0 {
			if err := prepareSelectedArray(&fields, column.Steps[:lastArray+1]); err != nil {
				return false, fmt.Errorf("%s: %w", column.Property, err)
			}
		}
		if err := assignSelectedColumn(&fields, column.Steps, value); err != nil {
			return false, fmt.Errorf("%s: %w", column.Property, err)
		}
	}
	for _, node := range fields {
		after[node.Name] = node
	}
	return writeChangedPasteRoots(destination, before, after)
}
