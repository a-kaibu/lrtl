package lrtl

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"

	"github.com/beevik/etree"
)

const (
	rdfNamespace      = "http://www.w3.org/1999/02/22-rdf-syntax-ns#"
	xmpNamespace      = "http://ns.adobe.com/xap/1.0/"
	crsNamespace      = "http://ns.adobe.com/camera-raw-settings/1.0/"
	openLRTLNamespace = "urn:open-lrtl:metadata:1.0"
)

var namespacePrefixes = map[string]string{
	"x":         "adobe:ns:meta/",
	"rdf":       rdfNamespace,
	"xmp":       xmpNamespace,
	"xmpNote":   "http://ns.adobe.com/xmp/note/",
	"crs":       crsNamespace,
	"lrt":       "http://lrtimelapse.com/",
	"ol":        openLRTLNamespace,
	"exif":      "http://ns.adobe.com/exif/1.0/",
	"tiff":      "http://ns.adobe.com/tiff/1.0/",
	"dc":        "http://purl.org/dc/elements/1.1/",
	"photoshop": "http://ns.adobe.com/photoshop/1.0/",
}

type XMPError struct{ Message string }

func (e *XMPError) Error() string { return e.Message }

func xmpError(format string, args ...any) error {
	return &XMPError{Message: fmt.Sprintf(format, args...)}
}

type XMPPacket struct{ Document *etree.Document }

func parseXMP(data []byte) (*XMPPacket, error) {
	if err := validateXMPXML(data); err != nil {
		return nil, xmpError("invalid XML in XMP packet: %v", err)
	}
	document := etree.NewDocument()
	document.ReadSettings.PreserveDuplicateAttrs = true
	if err := document.ReadFromBytes(data); err != nil {
		return nil, xmpError("invalid XML in XMP packet: %v", err)
	}
	if document.Root() == nil {
		return nil, xmpError("XMP packet has no XML root element")
	}
	packet := &XMPPacket{Document: document}
	if len(packet.Descriptions()) == 0 {
		var rdf *etree.Element
		root := document.Root()
		if root.NamespaceURI() == rdfNamespace && root.Tag == "RDF" {
			rdf = root
		} else {
			for _, token := range root.Child {
				child, ok := token.(*etree.Element)
				if ok && child.NamespaceURI() == rdfNamespace && child.Tag == "RDF" {
					rdf = child
					break
				}
			}
		}
		if rdf == nil {
			return nil, xmpError("XMP packet has no rdf:Description element")
		}
		// Some camera RAW files contain a valid but empty rdf:RDF packet.
		// Give subsequent edits a standard property container without discarding it.
		description := rdf.CreateElement("rdf:Description")
		description.CreateAttr("xmlns:rdf", rdfNamespace)
		description.CreateAttr("rdf:about", "")
	}
	return packet, nil
}

func validateXMPXML(data []byte) error {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	depth, roots := 0, 0
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		switch token := token.(type) {
		case xml.StartElement:
			if depth == 0 {
				roots++
				if roots > 1 {
					return fmt.Errorf("multiple XML root elements")
				}
			}
			depth++
		case xml.EndElement:
			depth--
			if depth < 0 {
				return fmt.Errorf("unexpected closing XML element")
			}
		case xml.CharData:
			if depth == 0 && strings.TrimSpace(string(token)) != "" {
				return fmt.Errorf("text outside XML root element")
			}
		case xml.Directive:
			if depth == 0 && roots > 0 {
				return fmt.Errorf("directive after XML root element")
			}
		case xml.ProcInst:
			if strings.EqualFold(token.Target, "xml") && roots > 0 {
				return fmt.Errorf("XML declaration after root element")
			}
		}
	}
	if depth != 0 {
		return fmt.Errorf("unclosed XML element")
	}
	if roots != 1 {
		return fmt.Errorf("expected one XML root element, got %d", roots)
	}
	return nil
}

func EmptyXMPPacket() *XMPPacket {
	document := etree.NewDocument()
	document.CreateProcInst("xml", `version="1.0" encoding="UTF-8"`)
	root := document.CreateElement("x:xmpmeta")
	root.CreateAttr("xmlns:x", namespacePrefixes["x"])
	rdf := root.CreateElement("rdf:RDF")
	rdf.CreateAttr("xmlns:rdf", rdfNamespace)
	description := rdf.CreateElement("rdf:Description")
	description.CreateAttr("rdf:about", "")
	return &XMPPacket{Document: document}
}

func (p *XMPPacket) Descriptions() []*etree.Element {
	var result []*etree.Element
	var visit func(*etree.Element)
	visit = func(element *etree.Element) {
		if element.NamespaceURI() == rdfNamespace && element.Tag == "Description" {
			result = append(result, element)
		}
		for _, token := range element.Child {
			if child, ok := token.(*etree.Element); ok {
				visit(child)
			}
		}
	}
	if root := p.Document.Root(); root != nil {
		visit(root)
	}
	return result
}

func expandedName(name string) (string, string, error) {
	separator := strings.IndexByte(name, ':')
	if separator <= 0 || separator == len(name)-1 {
		return "", "", xmpError("expected a prefixed XMP property, got %q", name)
	}
	prefix, local := name[:separator], name[separator+1:]
	uri, ok := namespacePrefixes[prefix]
	if !ok {
		return "", "", xmpError("unknown XMP namespace prefix %q", prefix)
	}
	return uri, local, nil
}

func attributeMatches(attribute *etree.Attr, uri, local string) bool {
	return attribute.Key == local && attribute.NamespaceURI() == uri
}

func scalarElementText(element *etree.Element) (string, bool) {
	for _, attribute := range element.Attr {
		if attribute.FullKey() != "xmlns" && !strings.HasPrefix(attribute.FullKey(), "xmlns:") {
			return "", false
		}
	}
	for _, token := range element.Child {
		if _, ok := token.(*etree.CharData); !ok {
			return "", false
		}
	}
	return element.Text(), true
}

func (p *XMPPacket) GetScalar(property string) (string, bool, error) {
	return scalarFromDescriptions(p.Descriptions(), property)
}

// GetRootScalar excludes identically named properties inside masks, looks,
// presets and other RDF structs when reading global image controls.
func (p *XMPPacket) GetRootScalar(property string) (string, bool, error) {
	return scalarFromDescriptions(p.rootDescriptions(), property)
}

func scalarFromDescriptions(descriptions []*etree.Element, property string) (string, bool, error) {
	uri, local, err := expandedName(property)
	if err != nil {
		return "", false, err
	}
	for _, description := range descriptions {
		for index := range description.Attr {
			if attributeMatches(&description.Attr[index], uri, local) {
				return description.Attr[index].Value, true, nil
			}
		}
		for _, token := range description.Child {
			child, ok := token.(*etree.Element)
			if !ok || child.NamespaceURI() != uri || child.Tag != local {
				continue
			}
			value, scalar := scalarElementText(child)
			if !scalar {
				return "", false, nil
			}
			return value, true, nil
		}
	}
	return "", false, nil
}

func prefixBinding(element *etree.Element, prefix string) (string, bool) {
	key := "xmlns:" + prefix
	for current := element; current != nil; current = current.Parent() {
		for _, attribute := range current.Attr {
			if attribute.FullKey() == key {
				return attribute.Value, true
			}
		}
	}
	return "", false
}

func ensurePrefix(element *etree.Element, prefix, uri string) string {
	if boundURI, declared := prefixBinding(element, prefix); !declared {
		element.CreateAttr("xmlns:"+prefix, uri)
		return prefix
	} else if boundURI == uri {
		return prefix
	}
	for suffix := 1; ; suffix++ {
		candidate := prefix + strconv.Itoa(suffix)
		if _, declared := prefixBinding(element, candidate); declared {
			continue
		}
		element.CreateAttr("xmlns:"+candidate, uri)
		return candidate
	}
}

func (p *XMPPacket) SetScalar(property, value string) error {
	uri, local, err := expandedName(property)
	if err != nil {
		return err
	}
	descriptions := p.Descriptions()
	if len(descriptions) == 0 {
		return xmpError("cannot add a property without rdf:Description")
	}
	for _, description := range descriptions {
		for index := range description.Attr {
			if attributeMatches(&description.Attr[index], uri, local) {
				description.Attr[index].Value = value
				return nil
			}
		}
		for _, token := range description.Child {
			child, ok := token.(*etree.Element)
			if !ok || child.NamespaceURI() != uri || child.Tag != local {
				continue
			}
			if _, scalar := scalarElementText(child); !scalar {
				return xmpError("cannot replace structured property %s", property)
			}
			child.SetText(value)
			return nil
		}
	}
	prefix := ""
	for candidate, candidateURI := range namespacePrefixes {
		if candidateURI == uri && (prefix == "" || len(candidate) < len(prefix)) {
			prefix = candidate
		}
	}
	if prefix == "" {
		return xmpError("no preferred prefix is configured for namespace %q", uri)
	}
	description := descriptions[0]
	prefix = ensurePrefix(description, prefix, uri)
	description.CreateAttr(prefix+":"+local, value)
	return nil
}

func (p *XMPPacket) RemoveProperty(property string) error {
	uri, local, err := expandedName(property)
	if err != nil {
		return err
	}
	for _, description := range p.Descriptions() {
		attrs := description.Attr[:0]
		for _, attribute := range description.Attr {
			if !attributeMatches(&attribute, uri, local) {
				attrs = append(attrs, attribute)
			}
		}
		description.Attr = attrs
		for _, token := range append([]etree.Token(nil), description.Child...) {
			child, ok := token.(*etree.Element)
			if ok && child.NamespaceURI() == uri && child.Tag == local {
				child.Remove()
			}
		}
	}
	return nil
}

func (p *XMPPacket) HasProperty(property string) (bool, error) {
	uri, local, err := expandedName(property)
	if err != nil {
		return false, err
	}
	for _, description := range p.Descriptions() {
		for index := range description.Attr {
			if attributeMatches(&description.Attr[index], uri, local) {
				return true, nil
			}
		}
		for _, token := range description.Child {
			if child, ok := token.(*etree.Element); ok && child.NamespaceURI() == uri && child.Tag == local {
				return true, nil
			}
		}
	}
	return false, nil
}

type topLevelXMPProperty struct {
	description *etree.Element
	element     *etree.Element
	attribute   *etree.Attr
}

func (p *XMPPacket) rootDescriptions() []*etree.Element {
	var descriptions []*etree.Element
	root := p.Document.Root()
	if root == nil {
		return descriptions
	}
	if root.NamespaceURI() == rdfNamespace && root.Tag == "RDF" {
		for _, child := range root.ChildElements() {
			if child.NamespaceURI() == rdfNamespace && child.Tag == "Description" {
				descriptions = append(descriptions, child)
			}
		}
		return descriptions
	}
	for _, token := range root.Child {
		rdf, ok := token.(*etree.Element)
		if !ok || rdf.NamespaceURI() != rdfNamespace || rdf.Tag != "RDF" {
			continue
		}
		for _, childToken := range rdf.Child {
			description, ok := childToken.(*etree.Element)
			if ok && description.NamespaceURI() == rdfNamespace && description.Tag == "Description" {
				descriptions = append(descriptions, description)
			}
		}
	}
	return descriptions
}

func (p *XMPPacket) findRootProperty(uri, local string) topLevelXMPProperty {
	for _, description := range p.rootDescriptions() {
		for index := range description.Attr {
			if attributeMatches(&description.Attr[index], uri, local) {
				return topLevelXMPProperty{description: description, attribute: &description.Attr[index]}
			}
		}
		for _, token := range description.Child {
			element, ok := token.(*etree.Element)
			if ok && element.NamespaceURI() == uri && element.Tag == local {
				return topLevelXMPProperty{description: description, element: element}
			}
		}
	}
	return topLevelXMPProperty{}
}

func rootDescriptionAbout(description *etree.Element) (string, bool) {
	for index := range description.Attr {
		attribute := &description.Attr[index]
		if attributeMatches(attribute, rdfNamespace, "about") {
			return attribute.Value, true
		}
	}
	return "", false
}

func sortElementAttributes(element *etree.Element) {
	element.SortAttrs()
	for _, token := range element.Child {
		if child, ok := token.(*etree.Element); ok {
			sortElementAttributes(child)
		}
	}
}

func xmpElementSignature(element *etree.Element) (string, error) {
	copy := element.Copy()
	sortElementAttributes(copy)
	document := etree.NewDocumentWithRoot(copy)
	data, err := document.WriteToBytes()
	return string(data), err
}

func sameRootProperty(left, right topLevelXMPProperty) (bool, error) {
	if left.element != nil || right.element != nil {
		if left.element == nil || right.element == nil {
			return false, nil
		}
		leftSignature, err := xmpElementSignature(left.element)
		if err != nil {
			return false, err
		}
		rightSignature, err := xmpElementSignature(right.element)
		if err != nil {
			return false, err
		}
		return leftSignature == rightSignature, nil
	}
	if left.attribute == nil || right.attribute == nil {
		return left.attribute == nil && right.attribute == nil, nil
	}
	return left.attribute.Value == right.attribute.Value, nil
}

// RootPropertyDiffers reports whether a top-level XMP property differs from
// the corresponding property in source. Structured values are compared as
// XML trees so RDF arrays and resource attributes are included.
func (p *XMPPacket) RootPropertyDiffers(source *XMPPacket, property string) (bool, error) {
	uri, local, err := expandedName(property)
	if err != nil {
		return false, err
	}
	left := p.findRootProperty(uri, local)
	right := source.findRootProperty(uri, local)
	same, err := sameRootProperty(left, right)
	return !same, err
}

// CopyRootPropertyFrom replaces a top-level XMP property with the source
// representation. A missing source property removes the destination value.
func (p *XMPPacket) CopyRootPropertyFrom(source *XMPPacket, property string) (bool, error) {
	uri, local, err := expandedName(property)
	if err != nil {
		return false, err
	}
	destination := p.findRootProperty(uri, local)
	sourceValue := source.findRootProperty(uri, local)
	same, err := sameRootProperty(destination, sourceValue)
	if err != nil || same {
		return false, err
	}
	for _, description := range p.rootDescriptions() {
		attributes := description.Attr[:0]
		for _, attribute := range description.Attr {
			if !attributeMatches(&attribute, uri, local) {
				attributes = append(attributes, attribute)
			}
		}
		description.Attr = attributes
		for _, token := range append([]etree.Token(nil), description.Child...) {
			child, ok := token.(*etree.Element)
			if ok && child.NamespaceURI() == uri && child.Tag == local {
				child.Remove()
			}
		}
	}
	if sourceValue.element == nil && sourceValue.attribute == nil {
		return true, nil
	}
	descriptions := p.rootDescriptions()
	if len(descriptions) == 0 {
		return false, xmpError("cannot copy a property without rdf:Description")
	}
	target := descriptions[0]
	if sourceAbout, found := rootDescriptionAbout(sourceValue.description); found {
		for _, description := range descriptions {
			if about, exists := rootDescriptionAbout(description); exists && about == sourceAbout {
				target = description
				break
			}
		}
	}
	if sourceValue.element != nil {
		copy := sourceValue.element.Copy()
		prefixes := make(map[string]struct{})
		var collect func(*etree.Element)
		collect = func(element *etree.Element) {
			if element.Space != "" {
				prefixes[element.Space] = struct{}{}
			}
			for _, attribute := range element.Attr {
				if attribute.Space != "" && attribute.Space != "xmlns" {
					prefixes[attribute.Space] = struct{}{}
				}
			}
			for _, token := range element.Child {
				if child, ok := token.(*etree.Element); ok {
					collect(child)
				}
			}
		}
		collect(copy)
		for prefix := range prefixes {
			if _, exists := prefixBinding(copy, prefix); exists {
				continue
			}
			if namespace, found := prefixBinding(sourceValue.element, prefix); found {
				copy.CreateAttr("xmlns:"+prefix, namespace)
			}
		}
		target.AddChild(copy)
		return true, nil
	}
	attribute := sourceValue.attribute
	prefix := attribute.Space
	if prefix == "" {
		return false, xmpError("cannot copy unprefixed namespaced attribute %s", property)
	}
	prefix = ensurePrefix(target, prefix, uri)
	target.CreateAttr(prefix+":"+local, attribute.Value)
	return true, nil
}

func (p *XMPPacket) Rating() (int, bool) {
	value, found, _ := p.GetScalar("xmp:Rating")
	if !found {
		return 0, false
	}
	number, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil || math.Trunc(number) != number || math.IsInf(number, 0) || math.IsNaN(number) {
		return 0, false
	}
	return int(number), true
}

func (p *XMPPacket) KeyframeKind() string {
	if value, found, _ := p.GetScalar("ol:KeyframeKind"); found {
		if value == "default" || value == "crop" {
			return value
		}
	}
	if rating, found := p.Rating(); found {
		if rating == 5 {
			return "crop"
		}
		if rating == 4 {
			return "default"
		}
	}
	return ""
}

func (p *XMPPacket) IsKeyframe() bool { return p.KeyframeKind() != "" }

func (p *XMPPacket) Serialize() ([]byte, error) {
	data, err := p.Document.WriteToBytes()
	if err != nil {
		return nil, xmpError("could not serialize XMP packet: %v", err)
	}
	return data, nil
}
