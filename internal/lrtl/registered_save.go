package lrtl

import (
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
)

type registeredInternalExposure struct {
	Steps  []selectedColumnStep
	Value  float64
	Mapped bool
}

type registeredSaveContext struct {
	Values   []transitionScalar
	WB       nativeSaveStateContext
	Rating   int32
	Internal []registeredInternalExposure
}

func readRegisteredSaveContext(packet *XMPPacket) (registeredSaveContext, error) {
	columns, err := selectedColumnRegistry()
	if err != nil {
		return registeredSaveContext{}, err
	}
	nodes, err := pastePacketNodes(packet)
	if err != nil {
		return registeredSaveContext{}, err
	}
	wb, err := readNativeSaveStateContext(packet)
	if err != nil {
		return registeredSaveContext{}, err
	}
	context := registeredSaveContext{Values: make([]transitionScalar, len(columns)), WB: wb}
	for _, column := range columns {
		if column.Index < 5 {
			continue
		}
		value, err := readSelectedColumn(nodes, column)
		if err != nil {
			return registeredSaveContext{}, err
		}
		context.Values[column.Index] = value
	}
	rating, _, err := packet.GetRootScalar("xmp:Rating")
	if err != nil {
		return registeredSaveContext{}, err
	}
	context.Rating, _ = selectedJavaInteger(rating)
	if context.Rating == 1 {
		context.Rating = 4
	}
	collection := "MaskGroupBasedCorrections"
	modernPresent := selectedField(nodes, pasteName{crsNamespace, "MaskGroupBasedCorrections"}) != nil
	for _, name := range []string{"GradientBasedCorrections", "CircularGradientBasedCorrections", "PaintBasedCorrections"} {
		if selectedField(nodes, pasteName{crsNamespace, name}) != nil {
			collection = "GradientBasedCorrections"
			break
		}
	}
	indices := []int{5, 6, 7}
	if modern := selectedField(nodes, pasteName{crsNamespace, "MaskGroupBasedCorrections"}); modern != nil {
		collection = "MaskGroupBasedCorrections"
		if modern.Kind != "Bag" && modern.Kind != "Seq" && modern.Kind != "Alt" {
			return registeredSaveContext{}, fmt.Errorf("%s is not an array", collection)
		}
		for index, item := range modern.Items {
			visitRegisteredRoleNames(item, index+1, indices)
		}
	}
	for _, index := range indices {
		path := fmt.Sprintf("%s[%d]/crs:LocalExposure2012", collection, index)
		steps, _, err := parseSelectedColumnPath(path)
		if err != nil {
			return registeredSaveContext{}, err
		}
		value, err := readSelectedColumn(nodes, selectedColumn{Property: path, Steps: steps})
		if err != nil {
			return registeredSaveContext{}, err
		}
		number := 0.0
		if value.present {
			var valid bool
			switch value.value {
			case "∞":
				number, valid = math.Inf(1), true
			case "-∞":
				number, valid = math.Inf(-1), true
			default:
				number, valid = selectedJavaDouble(value.value)
			}
			if !valid {
				return registeredSaveContext{}, fmt.Errorf("%s must be numeric for the internal exposure cache", path)
			}
		}
		context.Internal = append(context.Internal, registeredInternalExposure{steps, number, modernPresent || collection == "GradientBasedCorrections"})
	}
	return context, nil
}

func visitRegisteredRoleNames(node *pasteNode, index int, indices []int) {
	if node.Name.URI == crsNamespace && strings.HasPrefix(node.Name.Local, "CorrectionName") && strings.HasPrefix(node.Value, "#LRT internal use") {
		name := strings.ToLower(node.Value)
		switch {
		case strings.Contains(name, "hg"):
			indices[0] = index
		case strings.Contains(name, "deflicker"):
			indices[1] = index
		case strings.Contains(name, "global"):
			indices[2] = index
		}
	}
	for _, field := range node.Fields {
		visitRegisteredRoleNames(field, index, indices)
	}
	for _, item := range node.Items {
		visitRegisteredRoleNames(item, index, indices)
	}
}

func copiedRegisteredSaveContext(target, source *XMPPacket, columns []selectedColumn) (registeredSaveContext, error) {
	context, err := readRegisteredSaveContext(target)
	if err != nil {
		return registeredSaveContext{}, err
	}
	context.WB, err = copiedNativeSaveStateContext(target, source, columns)
	if err != nil {
		return registeredSaveContext{}, err
	}
	nodes, err := pastePacketNodes(source)
	if err != nil {
		return registeredSaveContext{}, err
	}
	for _, column := range columns {
		value, err := readSelectedColumn(nodes, column)
		if err != nil {
			return registeredSaveContext{}, err
		}
		context.Values[column.Index] = value
	}
	return context, nil
}

// DecimalFormat 0.########, US symbols. Its fixed-point underflow branch
// treats the decimal 5.0E-9 as a tie before removing the trailing zero.
// Decimal digits that already fit are retained; otherwise the binary value is
// rounded to eight places. The large integral path uses the legacy converter.
func registeredInternalExposureText(value float64) string {
	if math.IsNaN(value) {
		return "NaN"
	}
	if math.IsInf(value, 1) {
		return "∞"
	}
	if math.IsInf(value, -1) {
		return "-∞"
	}
	if math.Abs(value) <= 5e-9 {
		if math.Signbit(value) {
			return "-0"
		}
		return "0"
	}
	if math.Abs(value) >= 1e16 {
		return registeredLargeExposureText(value)
	}
	shortest := strconv.FormatFloat(value, 'f', -1, 64)
	if point := strings.IndexByte(shortest, '.'); point < 0 || len(shortest)-point-1 <= 8 {
		return shortest
	}
	return strings.TrimSuffix(strings.TrimRight(strconv.FormatFloat(value, 'f', 8, 64), "0"), ".")
}

// The legacy Java decimal converter keeps decimal digits rather than exposing
// every binary integer digit. Find those digits using exact rational intervals.
func registeredLargeExposureText(value float64) string {
	abs := math.Abs(value)
	_, exponent := math.Frexp(abs)
	power10 := func(n int) *big.Int { return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(n)), nil) }
	integer := new(big.Int)
	if exponent <= 63 {
		integer.SetUint64(uint64(abs))
		// The legacy integral fast path discards decimal digits below its
		// binary precision, rounding the remaining integer half up.
		discard := 0
		if exponent > 54 {
			discard = int(float64(exponent-55) * math.Log10(2))
		}
		if discard > 0 {
			unit := power10(discard)
			remainder := new(big.Int)
			integer.QuoRem(integer, unit, remainder)
			if remainder.Lsh(remainder, 1).Cmp(unit) >= 0 {
				integer.Add(integer, big.NewInt(1))
			}
			integer.Mul(integer, unit)
		}
	} else {
		exact := new(big.Rat).SetFloat64(abs)
		margin := new(big.Rat).SetFloat64(math.Ldexp(1, exponent-54))
		if math.Float64bits(abs)&((uint64(1)<<52)-1) == 0 {
			margin.Quo(margin, big.NewRat(2, 1))
		}
		decimalExponent, _ := strconv.Atoi(strings.Split(strconv.FormatFloat(abs, 'e', -1, 64), "e")[1])
		for digits := 2; digits <= 18; digits++ {
			unit := power10(decimalExponent - digits + 1)
			scaled := new(big.Rat).Quo(exact, new(big.Rat).SetInt(unit))
			floor := new(big.Int).Quo(scaled.Num(), scaled.Denom())
			lower := new(big.Int).Mul(floor, unit)
			upper := new(big.Int).Add(lower, unit)
			lowDistance := new(big.Rat).Sub(exact, new(big.Rat).SetInt(lower))
			highDistance := new(big.Rat).Sub(new(big.Rat).SetInt(upper), exact)
			low, high := lowDistance.Cmp(margin) < 0, highDistance.Cmp(margin) < 0
			if !low && !high {
				continue
			}
			integer.Set(lower)
			if high && (!low || lowDistance.Cmp(highDistance) > 0 || lowDistance.Cmp(highDistance) == 0 && floor.Bit(0) != 0) {
				integer.Set(upper)
			}
			break
		}
	}
	if math.Signbit(value) {
		integer.Neg(integer)
	}
	return integer.String()
}

func registeredRootHasExplicitPrefix(column selectedColumn) bool {
	root := strings.SplitN(column.Property, "/", 2)[0]
	return strings.Contains(root, ":")
}

func applyRegisteredSaveState(packet *XMPPacket, context registeredSaveContext) (bool, error) {
	columns, err := selectedColumnRegistry()
	if err != nil {
		return false, err
	}
	if len(context.Values) != len(columns) {
		return false, fmt.Errorf("invalid registered save cache length")
	}
	nodes, err := pastePacketNodes(packet)
	if err != nil {
		return false, err
	}
	before := map[pasteName]*pasteNode{}
	fields := make([]*pasteNode, 0, len(nodes))
	for _, node := range nodes {
		before[node.Name] = node
		fields = append(fields, clonePasteNode(node))
	}
	namespace := crsNamespace
	for _, column := range columns {
		if column.Index < 5 {
			continue
		}
		value := context.Values[column.Index]
		steps := column.Steps
		if !value.present {
			// The native null-cell branch retains the preceding schema URI.
			// Explicitly prefixed roots that disagree are silently not deleted.
			if registeredRootHasExplicitPrefix(column) && steps[0].Name.URI != namespace {
				continue
			}
			steps = append([]selectedColumnStep(nil), steps...)
			steps[0].Name.URI = namespace
			_ = assignSelectedColumn(&fields, steps, value)
			continue
		}
		namespace = steps[0].Name.URI
		lastArray := -1
		for i, step := range steps {
			if step.Index > 0 {
				lastArray = i
			}
		}
		if lastArray >= 0 {
			if err := prepareSelectedArray(&fields, steps[:lastArray+1]); err != nil {
				return false, fmt.Errorf("%s: %w", column.Property, err)
			}
		}
		if err := assignSelectedColumn(&fields, steps, value); err != nil {
			return false, fmt.Errorf("%s: %w", column.Property, err)
		}
	}
	after := map[pasteName]*pasteNode{}
	for _, node := range fields {
		after[node.Name] = node
	}
	crop := "False"
	if context.WB.cropActive() {
		crop = "True"
	}
	if err := setSaveStateScalar(after, "HasCrop", crop); err != nil {
		return false, err
	}
	if context.WB.Quality != "" {
		if err := setSaveStateProperty(after, pasteName{namespacePrefixes["lrt"], "Quality"}, "lrt", context.WB.Quality); err != nil {
			return false, err
		}
	}
	if err := prepareWhiteBalanceNodes(after, context.WB.Quality); err != nil {
		return false, err
	}
	hasSettings := context.Values[6]
	if context.Rating > 0 || hasSettings.present && strings.EqualFold(hasSettings.value, "true") {
		rating := context.Rating
		if rating < 0 {
			rating = 0
		}
		if err := setSaveStateProperty(after, pasteName{namespacePrefixes["xmp"], "Rating"}, "xmp", strconv.FormatInt(int64(rating), 10)); err != nil {
			return false, err
		}
	}
	fields = fields[:0]
	for _, node := range after {
		fields = append(fields, node)
	}
	internal := append([]registeredInternalExposure(nil), context.Internal...)
	if len(internal) > 0 && !internal[0].Mapped {
		indices := []int{5, 6, 7}
		if root := selectedField(fields, pasteName{crsNamespace, "MaskGroupBasedCorrections"}); root != nil {
			for i, item := range root.Items {
				visitRegisteredRoleNames(item, i+1, indices)
			}
		}
		for i, index := range indices {
			steps, _, err := parseSelectedColumnPath(fmt.Sprintf("MaskGroupBasedCorrections[%d]/crs:LocalExposure2012", index))
			if err != nil {
				return false, err
			}
			internal[i].Steps = steps
			internal[i].Mapped = true
		}
	}
	for _, exposure := range internal {
		root := selectedField(fields, exposure.Steps[0].Name)
		if root == nil || exposure.Steps[0].Index > len(root.Items) {
			continue
		}
		value := transitionScalar{registeredInternalExposureText(exposure.Value), true}
		if err := assignSelectedColumn(&fields, exposure.Steps, value); err != nil {
			return false, err
		}
	}
	after = map[pasteName]*pasteNode{}
	for _, node := range fields {
		after[node.Name] = node
	}
	for _, name := range []string{"ReferenceKeyFrame", "ReferenceBoxX1", "ReferenceBoxY1", "ReferenceBoxX2", "ReferenceBoxY2"} {
		delete(after, pasteName{namespacePrefixes["lrt"], name})
	}
	for _, name := range []string{"SidecarForExtension", "EmbeddedXMPDigest"} {
		delete(after, pasteName{"http://ns.adobe.com/photoshop/1.0/", name})
	}
	changed, err := writeChangedPasteRoots(packet, before, after)
	if err == nil {
		context.Values[14] = transitionScalar{crop, true}
		copy(context.Internal, internal)
	}
	return changed, err
}
