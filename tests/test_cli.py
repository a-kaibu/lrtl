import contextlib
import io
import tempfile
import unittest
from pathlib import Path

from lrtl.cli import main
from lrtl.xmp import XmpPacket


class CliTests(unittest.TestCase):
    def test_mark_and_unmark_commands_restore_existing_rating(self):
        with tempfile.TemporaryDirectory() as directory:
            image = Path(directory) / "frame1.CR3"
            image.write_bytes(b"")
            sidecar = Path(f"{image}.xmp")
            packet = XmpPacket.empty()
            packet.set_scalar("xmp:Rating", "2")
            sidecar.write_bytes(packet.serialize())

            output = io.StringIO()
            with contextlib.redirect_stdout(output):
                mark_code = main(["mark", str(image), "--kind", "crop"])
            marked = XmpPacket.parse(sidecar.read_bytes())
            self.assertEqual(mark_code, 0)
            self.assertEqual(marked.rating(), 5)
            self.assertEqual(marked.keyframe_kind(), "crop")
            self.assertTrue(any(sidecar.parent.glob(f"{sidecar.name}.lrtl-*.bak")))

            with contextlib.redirect_stdout(output):
                unmark_code = main(["unmark", str(image)])
            restored = XmpPacket.parse(sidecar.read_bytes())
            self.assertEqual(unmark_code, 0)
            self.assertEqual(restored.rating(), 2)

    def test_interpolation_is_preview_only_unless_write_is_given(self):
        with tempfile.TemporaryDirectory() as directory:
            folder = Path(directory)
            images = [folder / f"frame{index}.dng" for index in range(3)]
            for image in images:
                image.write_bytes(b"")
            for image, exposure in ((images[0], "0"), (images[2], "2")):
                packet = XmpPacket.empty()
                packet.set_scalar("xmp:Rating", "4")
                packet.set_scalar("crs:Exposure2012", exposure)
                Path(f"{image}.xmp").write_bytes(packet.serialize())

            output = io.StringIO()
            with contextlib.redirect_stdout(output):
                preview_code = main(["interpolate", str(folder)])
            middle_sidecar = Path(f"{images[1]}.xmp")
            self.assertEqual(preview_code, 0)
            self.assertFalse(middle_sidecar.exists())
            self.assertIn("Dry run", output.getvalue())

            with contextlib.redirect_stdout(output):
                write_code = main(["interpolate", str(folder), "--write"])
            self.assertEqual(write_code, 0)
            self.assertEqual(
                XmpPacket.parse(middle_sidecar.read_bytes()).get_scalar(
                    "crs:Exposure2012"
                ),
                "1",
            )


if __name__ == "__main__":
    unittest.main()
