package lrtl

import (
	"fmt"
	"sort"
	"strings"

	"github.com/beevik/etree"
)

const xmlNamespace = "http://www.w3.org/XML/1998/namespace"

func pasteAttributeURI(a *etree.Attr) string {
	if a.Space == "xml" {
		return xmlNamespace
	}
	return a.NamespaceURI()
}

type pasteName struct{ URI, Local string }
type pasteNode struct {
	Name                      pasteName
	Prefix, Kind, Value       string
	Fields, Items, Qualifiers []*pasteNode
}

func addPasteQualifier(n, q *pasteNode) error {
	if q.Name == (pasteName{xmlNamespace, "lang"}) {
		q.Value = pasteLanguage(q.Value)
	}
	for _, existing := range n.Qualifiers {
		if existing.Name == q.Name {
			if pasteNodesEqual(existing, q) {
				return nil
			}
			return xmpError("conflicting XMP qualifier {%s}%s", q.Name.URI, q.Name.Local)
		}
	}
	n.Qualifiers = append(n.Qualifiers, q)
	return nil
}

func pasteAttributeProperty(a *etree.Attr) bool {
	uri := pasteAttributeURI(a)
	return a.Space != "xmlns" && a.Key != "xmlns" && uri != "" && uri != rdfNamespace && uri != xmlNamespace
}

func pasteFields(element *etree.Element, depth int) ([]*pasteNode, error) {
	var out []*pasteNode
	seen := map[pasteName]bool{}
	add := func(n *pasteNode) error {
		if seen[n.Name] {
			return xmpError("duplicate XMP property {%s}%s", n.Name.URI, n.Name.Local)
		}
		seen[n.Name] = true
		out = append(out, n)
		return nil
	}
	for i := range element.Attr {
		a := &element.Attr[i]
		if pasteAttributeProperty(a) {
			if err := add(&pasteNode{Name: pasteName{a.NamespaceURI(), a.Key}, Prefix: a.Space, Kind: "scalar", Value: a.Value}); err != nil {
				return nil, err
			}
		}
	}
	for _, child := range element.ChildElements() {
		if child.NamespaceURI() == rdfNamespace {
			continue
		}
		n, err := parsePasteNode(child, depth+1)
		if err != nil {
			return nil, err
		}
		if err := add(n); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func parsePasteNode(element *etree.Element, depth int) (*pasteNode, error) {
	if depth > 256 {
		return nil, xmpError("XMP paste nesting exceeds 256 levels")
	}
	uri := element.NamespaceURI()
	if element.Space == "xml" {
		uri = xmlNamespace
	}
	n := &pasteNode{Name: pasteName{uri, element.Tag}, Prefix: element.Space, Kind: "scalar", Value: element.Text()}
	if n.Name.URI == "" {
		return nil, xmpError("XMP property %s has no namespace", element.FullTag())
	}
	resource := false
	for i := range element.Attr {
		a := &element.Attr[i]
		if attributeMatches(a, rdfNamespace, "resource") {
			n.Kind = "uri"
			n.Value = a.Value
		}
		if attributeMatches(a, rdfNamespace, "parseType") && a.Value == "Resource" {
			resource = true
		}
		if pasteAttributeURI(a) == xmlNamespace && a.Key == "lang" {
			if err := addPasteQualifier(n, &pasteNode{Name: pasteName{xmlNamespace, "lang"}, Prefix: "xml", Kind: "scalar", Value: pasteLanguage(a.Value)}); err != nil {
				return nil, err
			}
		}
	}
	children := element.ChildElements()
	if len(children) == 1 && children[0].NamespaceURI() == rdfNamespace {
		container := children[0]
		switch container.Tag {
		case "Bag", "Seq", "Alt":
			n.Kind = container.Tag
			n.Value = ""
			for _, item := range container.ChildElements() {
				if item.NamespaceURI() != rdfNamespace || item.Tag != "li" {
					return nil, xmpError("non-item element in RDF array")
				}
				parsed, err := parsePasteNode(item, depth+1)
				if err != nil {
					return nil, err
				}
				n.Items = append(n.Items, parsed)
			}
			if n.Kind == "Alt" {
				for _, item := range n.Items {
					if pasteQualifier(item, xmlNamespace, "lang") != "" {
						n.Kind = "AltText"
						break
					}
				}
			}
			return n, nil
		case "Description":
			n.Kind = "struct"
			n.Value = ""
			var err error
			n.Fields, err = pasteFields(container, depth+1)
			return n, err
		}
	}
	// rdf:value carries the base value of a qualified property. The remaining
	// siblings are qualifiers, not fields to append during struct merging.
	for _, child := range children {
		if child.NamespaceURI() == rdfNamespace && child.Tag == "value" {
			value, err := parsePasteNode(child, depth+1)
			if err != nil {
				return nil, err
			}
			n.Kind, n.Value, n.Fields, n.Items = value.Kind, value.Value, value.Fields, value.Items
			for _, q := range value.Qualifiers {
				if err := addPasteQualifier(n, q); err != nil {
					return nil, err
				}
			}
			for _, q := range children {
				if q == child {
					continue
				}
				qualifier, err := parsePasteNode(q, depth+1)
				if err != nil {
					return nil, err
				}
				if err := addPasteQualifier(n, qualifier); err != nil {
					return nil, err
				}
			}
			return n, nil
		}
	}
	fields, err := pasteFields(element, depth)
	if err != nil {
		return nil, err
	}
	if resource || len(fields) > 0 {
		n.Kind = "struct"
		n.Value = ""
		n.Fields = fields
	}
	return n, nil
}

func pasteLanguage(value string) string {
	if value == "x-default" {
		return value
	}
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == '-' || r == '_' })
	for i := range parts {
		if i == 1 {
			parts[i] = strings.ToUpper(parts[i])
		} else {
			parts[i] = strings.ToLower(parts[i])
		}
	}
	return strings.Join(parts, "-")
}

func pasteQualifier(n *pasteNode, uri, local string) string {
	for _, q := range n.Qualifiers {
		if q.Name == (pasteName{uri, local}) {
			return q.Value
		}
	}
	return ""
}

func normalizePasteDC(n *pasteNode) {
	if n.Name.URI != namespacePrefixes["dc"] {
		return
	}
	kind := ""
	switch n.Name.Local {
	case "contributor", "language", "publisher", "relation", "subject", "type":
		kind = "Bag"
	case "creator", "date":
		kind = "Seq"
	case "description", "rights", "title":
		kind = "AltText"
	}
	if kind == "" {
		return
	}
	if n.Kind == "scalar" || n.Kind == "uri" {
		item := clonePasteNode(n)
		item.Name = pasteName{rdfNamespace, "li"}
		item.Prefix = "rdf"
		n.Value = ""
		n.Qualifiers = nil
		n.Fields = nil
		n.Items = []*pasteNode{item}
		if kind == "AltText" && pasteQualifier(item, xmlNamespace, "lang") == "" {
			item.Qualifiers = append(item.Qualifiers, &pasteNode{Name: pasteName{xmlNamespace, "lang"}, Prefix: "xml", Kind: "scalar", Value: "x-default"})
		}
	}
	if n.Kind != "struct" {
		n.Kind = kind
	}
	if kind == "AltText" {
		var repaired []*pasteNode
		for _, item := range n.Items {
			if item.Kind != "scalar" && item.Kind != "uri" {
				continue
			}
			if pasteQualifier(item, xmlNamespace, "lang") == "" {
				if item.Value == "" {
					continue
				}
				item.Qualifiers = append(item.Qualifiers, &pasteNode{Name: pasteName{xmlNamespace, "lang"}, Prefix: "xml", Kind: "scalar", Value: pasteLanguage("x-repair")})
			}
			repaired = append(repaired, item)
		}
		n.Items = repaired
	}
}

func pastePacketNodes(packet *XMPPacket) ([]*pasteNode, error) {
	if err := validatePasteNamespaceBindings(packet.Document.Root(), 0); err != nil {
		return nil, err
	}
	var result []*pasteNode
	seen := map[pasteName]bool{}
	descriptions := packet.rootDescriptions()
	if len(descriptions) == 0 {
		return nil, xmpError("XMP metadata has no root rdf:Description")
	}
	for _, description := range descriptions {
		fields, err := pasteFields(description, 0)
		if err != nil {
			return nil, err
		}
		for _, n := range fields {
			if seen[n.Name] {
				return nil, xmpError("duplicate top-level XMP property {%s}%s", n.Name.URI, n.Name.Local)
			}
			seen[n.Name] = true
			normalizePasteDC(n)
			result = append(result, n)
		}
	}
	return result, nil
}

// etree can parse an undeclared prefixed attribute as namespace-less XML.
// Adding that prefix while editing would retroactively bind the old attribute,
// potentially creating duplicate properties. Reject it before any mutation.
func validatePasteNamespaceBindings(element *etree.Element, depth int) error {
	if element == nil {
		return nil
	}
	if depth > 512 {
		return xmpError("XMP namespace validation exceeds 512 levels")
	}
	if element.Space != "" && element.Space != "xml" && element.NamespaceURI() == "" {
		return xmpError("undeclared XML namespace prefix %s", element.Space)
	}
	for _, attribute := range element.Attr {
		if attribute.Space != "" && attribute.Space != "xmlns" && attribute.Space != "xml" && attribute.NamespaceURI() == "" {
			return xmpError("undeclared XML namespace prefix %s", attribute.Space)
		}
	}
	for _, child := range element.ChildElements() {
		if err := validatePasteNamespaceBindings(child, depth+1); err != nil {
			return err
		}
	}
	return nil
}

func clonePasteNode(n *pasteNode) *pasteNode {
	copy := *n
	copy.Fields = nil
	copy.Items = nil
	copy.Qualifiers = nil
	for _, f := range n.Fields {
		copy.Fields = append(copy.Fields, clonePasteNode(f))
	}
	for _, f := range n.Items {
		copy.Items = append(copy.Items, clonePasteNode(f))
	}
	for _, f := range n.Qualifiers {
		copy.Qualifiers = append(copy.Qualifiers, clonePasteNode(f))
	}
	return &copy
}

func clonePasteForAppend(n *pasteNode) *pasteNode {
	copy := clonePasteNode(n)
	copy.Fields = nil
	copy.Items = nil
	for _, field := range n.Fields {
		if !pasteNodeEmpty(field) {
			if child := clonePasteForAppend(field); child != nil {
				copy.Fields = append(copy.Fields, child)
			}
		}
	}
	for _, item := range n.Items {
		if !pasteNodeEmpty(item) {
			if child := clonePasteForAppend(item); child != nil {
				copy.Items = append(copy.Items, child)
			}
		}
	}
	if pasteNodeEmpty(copy) {
		return nil
	}
	return copy
}

func pasteNodeEmpty(n *pasteNode) bool {
	switch n.Kind {
	case "scalar", "uri":
		return n.Value == ""
	case "struct":
		return len(n.Fields) == 0
	default:
		return len(n.Items) == 0
	}
}

func pasteOptions(n *pasteNode) string {
	return fmt.Sprintf("%s/%t/%t/%t", n.Kind, len(n.Qualifiers) > 0, pasteQualifier(n, xmlNamespace, "lang") != "", pasteQualifier(n, rdfNamespace, "type") != "")
}

// Native array-item matching is intentionally different from packet equality:
// nested arrays match a source subset, and simple custom qualifier values do
// not distinguish otherwise matching items. Language and URI type still do.
func pasteItemsMatch(source, target *pasteNode) bool {
	if pasteOptions(source) != pasteOptions(target) {
		return false
	}
	switch source.Kind {
	case "scalar", "uri":
		return source.Value == target.Value && pasteQualifier(source, xmlNamespace, "lang") == pasteQualifier(target, xmlNamespace, "lang")
	case "struct":
		if len(source.Fields) != len(target.Fields) {
			return false
		}
		for _, s := range source.Fields {
			found := false
			for _, t := range target.Fields {
				if s.Name == t.Name && pasteItemsMatch(s, t) {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
		return true
	default:
		for _, s := range source.Items {
			found := false
			for _, t := range target.Items {
				if pasteItemsMatch(s, t) {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
		return true
	}
}

func mergePasteNode(target, source *pasteNode) {
	if pasteOptions(target) != pasteOptions(source) {
		return
	}
	switch target.Kind {
	case "struct":
		for _, s := range source.Fields {
			if pasteNodeEmpty(s) {
				continue
			}
			var existing *pasteNode
			for _, t := range target.Fields {
				if t.Name == s.Name {
					existing = t
					break
				}
			}
			if existing == nil {
				if child := clonePasteForAppend(s); child != nil {
					target.Fields = append(target.Fields, child)
				}
			} else {
				mergePasteNode(existing, s)
			}
		}
	case "Bag", "Seq", "Alt":
		for _, s := range source.Items {
			if pasteNodeEmpty(s) {
				continue
			}
			found := false
			for _, t := range target.Items {
				if pasteItemsMatch(s, t) {
					found = true
					break
				}
			}
			if !found {
				if child := clonePasteForAppend(s); child != nil {
					target.Items = append(target.Items, child)
				}
			}
		}
	case "AltText":
		for _, s := range source.Items {
			lang := pasteQualifier(s, xmlNamespace, "lang")
			if lang == "" || pasteNodeEmpty(s) {
				continue
			}
			found := false
			for _, t := range target.Items {
				if pasteQualifier(t, xmlNamespace, "lang") == lang {
					found = true
					break
				}
			}
			if !found {
				copy := clonePasteForAppend(s)
				if copy == nil {
					continue
				}
				if lang == "x-default" {
					target.Items = append([]*pasteNode{copy}, target.Items...)
				} else {
					target.Items = append(target.Items, copy)
				}
			}
		}
	}
}

func pasteNodesEqual(a, b *pasteNode) bool {
	if a.Name != b.Name || a.Kind != b.Kind || a.Value != b.Value || len(a.Fields) != len(b.Fields) || len(a.Items) != len(b.Items) || len(a.Qualifiers) != len(b.Qualifiers) {
		return false
	}
	for i := range a.Items {
		if !pasteNodesEqual(a.Items[i], b.Items[i]) {
			return false
		}
	}
	for _, pair := range [][2][]*pasteNode{{a.Fields, b.Fields}, {a.Qualifiers, b.Qualifiers}} {
		for _, left := range pair[0] {
			found := false
			for _, right := range pair[1] {
				if left.Name == right.Name && pasteNodesEqual(left, right) {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
	}
	return true
}

func pastePrefix(parent *etree.Element, n *pasteNode) string {
	if n.Name.URI == xmlNamespace {
		return "xml"
	}
	prefix := n.Prefix
	if prefix == "" {
		prefix = "ns"
	}
	return ensurePrefix(parent, prefix, n.Name.URI)
}

func writePasteNode(parent *etree.Element, n *pasteNode) {
	element := parent.CreateElement(pastePrefix(parent, n) + ":" + n.Name.Local)
	var custom []*pasteNode
	for _, q := range n.Qualifiers {
		if q.Name == (pasteName{xmlNamespace, "lang"}) {
			element.CreateAttr("xml:lang", q.Value)
		} else {
			custom = append(custom, q)
		}
	}
	if len(custom) > 0 {
		rdf := ensurePrefix(element, "rdf", rdfNamespace)
		element.CreateAttr(rdf+":parseType", "Resource")
		value := clonePasteNode(n)
		value.Name = pasteName{rdfNamespace, "value"}
		value.Prefix = "rdf"
		value.Qualifiers = nil
		writePasteNode(element, value)
		for _, q := range custom {
			writePasteNode(element, q)
		}
		return
	}
	switch n.Kind {
	case "scalar":
		element.SetText(n.Value)
	case "uri":
		rdf := ensurePrefix(element, "rdf", rdfNamespace)
		element.CreateAttr(rdf+":resource", n.Value)
	case "struct":
		rdf := ensurePrefix(element, "rdf", rdfNamespace)
		element.CreateAttr(rdf+":parseType", "Resource")
		for _, field := range n.Fields {
			writePasteNode(element, field)
		}
	default:
		rdf := ensurePrefix(element, "rdf", rdfNamespace)
		kind := n.Kind
		if kind == "AltText" {
			kind = "Alt"
		}
		container := element.CreateElement(rdf + ":" + kind)
		for _, item := range n.Items {
			writePasteNode(container, item)
		}
	}
}

// pasteXMPMetadata models the native XML phase: remove destination Camera Raw
// properties, then append all source schemas without replacing existing simple
// values in other namespaces. It deliberately can fill missing capture data.
func pasteXMPMetadata(destination, source *XMPPacket) (bool, error) {
	src, err := pastePacketNodes(source)
	if err != nil {
		return false, err
	}
	dst, err := pastePacketNodes(destination)
	if err != nil {
		return false, err
	}
	before := map[pasteName]*pasteNode{}
	after := map[pasteName]*pasteNode{}
	for _, n := range dst {
		before[n.Name] = n
		if n.Name.URI != crsNamespace {
			after[n.Name] = clonePasteNode(n)
		}
	}
	for _, n := range src {
		if pasteNodeEmpty(n) {
			continue
		}
		if existing := after[n.Name]; existing != nil {
			mergePasteNode(existing, n)
		} else {
			if copy := clonePasteForAppend(n); copy != nil {
				after[n.Name] = copy
			}
		}
	}
	return writeChangedPasteRoots(destination, before, after)
}

// Only changed root properties are rebuilt; untouched XML remains in place.
func writeChangedPasteRoots(destination *XMPPacket, before, after map[pasteName]*pasteNode) (bool, error) {
	changed := map[pasteName]bool{}
	for name, n := range before {
		other := after[name]
		if other == nil || !pasteNodesEqual(n, other) {
			changed[name] = true
		}
	}
	for name := range after {
		if before[name] == nil {
			changed[name] = true
		}
	}
	if len(changed) == 0 {
		return false, nil
	}
	descriptions := destination.rootDescriptions()
	if len(descriptions) == 0 {
		return false, xmpError("cannot paste into XMP without root rdf:Description")
	}
	for _, description := range descriptions {
		attrs := description.Attr[:0]
		for _, a := range description.Attr {
			if !changed[pasteName{a.NamespaceURI(), a.Key}] {
				attrs = append(attrs, a)
			}
		}
		description.Attr = attrs
		for _, child := range description.ChildElements() {
			if changed[pasteName{child.NamespaceURI(), child.Tag}] {
				child.Remove()
			}
		}
	}
	names := make([]pasteName, 0, len(changed))
	for name := range changed {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		if names[i].URI != names[j].URI {
			return names[i].URI < names[j].URI
		}
		return names[i].Local < names[j].Local
	})
	for _, name := range names {
		if n := after[name]; n != nil {
			writePasteNode(descriptions[0], n)
		}
	}
	return true, nil
}
