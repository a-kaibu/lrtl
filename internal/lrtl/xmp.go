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
	"crs":       crsNamespace,
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
		return nil, xmpError("XMP packet has no rdf:Description element")
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
	uri, local, err := expandedName(property)
	if err != nil {
		return "", false, err
	}
	for _, description := range p.Descriptions() {
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
