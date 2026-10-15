"""Small, dependency-free reader and writer for XMP sidecar packets."""

from __future__ import annotations

from dataclasses import dataclass
import re
import xml.etree.ElementTree as ET


NAMESPACES = {
    "x": "adobe:ns:meta/",
    "rdf": "http://www.w3.org/1999/02/22-rdf-syntax-ns#",
    "xmp": "http://ns.adobe.com/xap/1.0/",
    "crs": "http://ns.adobe.com/camera-raw-settings/1.0/",
    "exif": "http://ns.adobe.com/exif/1.0/",
    "tiff": "http://ns.adobe.com/tiff/1.0/",
    "dc": "http://purl.org/dc/elements/1.1/",
    "photoshop": "http://ns.adobe.com/photoshop/1.0/",
    "ol": "urn:open-lrtl:metadata:1.0",
}

RDF_DESCRIPTION = f"{{{NAMESPACES['rdf']}}}Description"
RDF_RDF = f"{{{NAMESPACES['rdf']}}}RDF"
XMP_META = f"{{{NAMESPACES['x']}}}xmpmeta"
XMLNS_PATTERN = re.compile(
    rb"xmlns(?::([A-Za-z_][\w.-]*))?\s*=\s*(['\"])(.*?)\2",
    re.DOTALL,
)
INVALID_PREFIX = re.compile(r"ns\d+$")


class XmpError(ValueError):
    """An XMP packet is malformed or contains an unsupported value shape."""


def _expanded_name(name: str) -> str:
    if name.startswith("{"):
        return name
    if ":" not in name:
        raise XmpError(f"Expected a prefixed XMP property, got {name!r}")
    prefix, local_name = name.split(":", 1)
    try:
        uri = NAMESPACES[prefix]
    except KeyError as exc:
        raise XmpError(f"Unknown XMP namespace prefix {prefix!r}") from exc
    return f"{{{uri}}}{local_name}"


def _register_packet_namespaces(data: bytes) -> None:
    for match in XMLNS_PATTERN.finditer(data):
        prefix_bytes, _, uri_bytes = match.groups()
        prefix = prefix_bytes.decode("utf-8") if prefix_bytes else ""
        if prefix == "xml" or INVALID_PREFIX.fullmatch(prefix):
            continue
        try:
            uri = uri_bytes.decode("utf-8")
        except UnicodeDecodeError:
            continue
        ET.register_namespace(prefix, uri)
    for prefix, uri in NAMESPACES.items():
        ET.register_namespace(prefix, uri)


@dataclass
class XmpPacket:
    """Parsed RDF content from an XMP packet."""

    root: ET.Element

    @classmethod
    def parse(cls, data: bytes) -> "XmpPacket":
        _register_packet_namespaces(data)
        parser = ET.XMLParser(
            target=ET.TreeBuilder(insert_comments=True, insert_pis=True)
        )
        try:
            root = ET.fromstring(data, parser=parser)
        except ET.ParseError as exc:
            raise XmpError(f"Invalid XML in XMP packet: {exc}") from exc
        packet = cls(root)
        if not packet.descriptions:
            raise XmpError("XMP packet has no rdf:Description element")
        return packet

    @classmethod
    def empty(cls) -> "XmpPacket":
        root = ET.Element(XMP_META)
        rdf = ET.SubElement(root, RDF_RDF)
        ET.SubElement(rdf, RDF_DESCRIPTION, {f"{{{NAMESPACES['rdf']}}}about": ""})
        return cls(root)

    @property
    def descriptions(self) -> list[ET.Element]:
        return list(self.root.iter(RDF_DESCRIPTION))

    def get_scalar(self, property_name: str) -> str | None:
        tag = _expanded_name(property_name)
        for description in self.descriptions:
            if tag in description.attrib:
                return description.attrib[tag]
            for child in description:
                if child.tag == tag:
                    if len(child) != 0 or child.attrib:
                        return None
                    return child.text or ""
        return None

    def set_scalar(self, property_name: str, value: str) -> None:
        tag = _expanded_name(property_name)
        descriptions = self.descriptions
        if not descriptions:
            raise XmpError("Cannot add a property without rdf:Description")
        for description in descriptions:
            if tag in description.attrib:
                description.set(tag, value)
                return
            for child in description:
                if child.tag == tag:
                    if len(child) != 0 or child.attrib:
                        raise XmpError(
                            f"Cannot replace structured property {property_name}"
                        )
                    child.text = value
                    return
        descriptions[0].set(tag, value)

    def remove_property(self, property_name: str) -> None:
        tag = _expanded_name(property_name)
        for description in self.descriptions:
            description.attrib.pop(tag, None)
            for child in list(description):
                if child.tag == tag:
                    description.remove(child)

    def has_property(self, property_name: str) -> bool:
        tag = _expanded_name(property_name)
        return any(
            tag in description.attrib
            or any(child.tag == tag for child in description)
            for description in self.descriptions
        )

    def rating(self) -> int | None:
        value = self.get_scalar("xmp:Rating")
        if value is None:
            return None
        try:
            numeric = float(value)
        except ValueError:
            return None
        if not numeric.is_integer():
            return None
        return int(numeric)

    def keyframe_kind(self) -> str | None:
        kind = self.get_scalar("ol:KeyframeKind")
        if kind in {"default", "crop"}:
            return kind
        rating = self.rating()
        if rating == 5:
            return "crop"
        if rating == 4:
            return "default"
        return None

    def is_keyframe(self) -> bool:
        return self.keyframe_kind() is not None

    def serialize(self) -> bytes:
        return ET.tostring(
            self.root,
            encoding="utf-8",
            xml_declaration=True,
            short_empty_elements=True,
        )
