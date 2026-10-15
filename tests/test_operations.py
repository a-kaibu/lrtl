import tempfile
import unittest
from pathlib import Path

from lrtl.operations import mark_keyframe, unmark_keyframe, write_packet
from lrtl.xmp import XmpPacket


class KeyframeOperationTests(unittest.TestCase):
    def test_mark_then_unmark_restores_existing_rating(self):
        packet = XmpPacket.empty()
        packet.set_scalar("xmp:Rating", "2")

        self.assertEqual(mark_keyframe(packet, "default"), 4)
        self.assertEqual(packet.rating(), 4)
        self.assertEqual(packet.keyframe_kind(), "default")
        unmark_keyframe(packet)

        self.assertEqual(packet.rating(), 2)
        self.assertFalse(packet.is_keyframe())
        self.assertFalse(packet.has_property("ol:PreviousRating"))

    def test_unmark_after_first_mark_removes_rating(self):
        packet = XmpPacket.empty()
        mark_keyframe(packet, "crop")
        unmark_keyframe(packet)

        self.assertIsNone(packet.rating())
        self.assertFalse(packet.is_keyframe())

    def test_external_rating_requires_explicit_force(self):
        packet = XmpPacket.empty()
        packet.set_scalar("xmp:Rating", "5")
        with self.assertRaisesRegex(ValueError, "not created by lrtl"):
            unmark_keyframe(packet)
        unmark_keyframe(packet, force_rating=True)
        self.assertIsNone(packet.rating())

    def test_write_creates_backup_and_replaces_sidecar(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "photo.xmp"
            path.write_bytes(b"old sidecar")
            packet = XmpPacket.empty()
            packet.set_scalar("xmp:Rating", "4")

            backup = write_packet(path, packet)

            self.assertIsNotNone(backup)
            self.assertEqual(backup.read_bytes(), b"old sidecar")
            self.assertEqual(XmpPacket.parse(path.read_bytes()).rating(), 4)


if __name__ == "__main__":
    unittest.main()
