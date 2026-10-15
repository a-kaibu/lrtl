import unittest
import xml.etree.ElementTree as ET

from lrtl.xmp import XmpError, XmpPacket


class XmpPacketTests(unittest.TestCase):
    def test_round_trip_preserves_unknown_properties_and_changes_scalar(self):
        source = b'''<?xml version="1.0"?>
        <x:xmpmeta xmlns:x="adobe:ns:meta/">
          <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
            <rdf:Description rdf:about=""
              xmlns:crs="http://ns.adobe.com/camera-raw-settings/1.0/"
              xmlns:vendor="urn:example:vendor">
              <crs:Exposure2012>+0.50</crs:Exposure2012>
              <vendor:Note>keep me</vendor:Note>
              <xmp:Rating xmlns:xmp="http://ns.adobe.com/xap/1.0/">2</xmp:Rating>
            </rdf:Description>
          </rdf:RDF>
        </x:xmpmeta>'''
        packet = XmpPacket.parse(source)
        self.assertEqual(packet.get_scalar("crs:Exposure2012"), "+0.50")
        packet.set_scalar("crs:Exposure2012", "1.25")

        serialized = packet.serialize()
        reparsed = XmpPacket.parse(serialized)
        self.assertEqual(reparsed.get_scalar("crs:Exposure2012"), "1.25")
        self.assertEqual(reparsed.get_scalar("xmp:Rating"), "2")
        vendor_note = next(reparsed.root.iter("{urn:example:vendor}Note"))
        self.assertEqual(vendor_note.text, "keep me")

    def test_accepts_packet_processing_instructions(self):
        source = b'''<?xpacket begin="\xef\xbb\xbf"?>
        <x:xmpmeta xmlns:x="adobe:ns:meta/">
          <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
            <rdf:Description rdf:about="" />
          </rdf:RDF>
        </x:xmpmeta>
        <?xpacket end="w"?>'''
        packet = XmpPacket.parse(source)
        self.assertEqual(len(packet.descriptions), 1)

    def test_rejects_malformed_xml_and_packets_without_description(self):
        with self.assertRaises(XmpError):
            XmpPacket.parse(b"<xmp>")
        with self.assertRaises(XmpError):
            XmpPacket.parse(
                b'<x:xmpmeta xmlns:x="adobe:ns:meta/" />'
            )

    def test_structured_property_is_not_treated_as_scalar(self):
        source = b'''<x:xmpmeta xmlns:x="adobe:ns:meta/">
          <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
            <rdf:Description rdf:about=""
              xmlns:crs="http://ns.adobe.com/camera-raw-settings/1.0/">
              <crs:ToneCurve><rdf:Seq><rdf:li>0,0</rdf:li></rdf:Seq></crs:ToneCurve>
            </rdf:Description>
          </rdf:RDF>
        </x:xmpmeta>'''
        packet = XmpPacket.parse(source)
        self.assertIsNone(packet.get_scalar("crs:ToneCurve"))
        with self.assertRaises(XmpError):
            packet.set_scalar("crs:ToneCurve", "0,0")


if __name__ == "__main__":
    unittest.main()
