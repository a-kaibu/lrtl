import os
import tempfile
import unittest
from pathlib import Path

from lrtl.interpolate import NUMERIC_CRS_PROPERTIES, apply_plan, build_plan
from lrtl.sequence import discover_sequence, find_sidecar
from lrtl.xmp import XmpPacket


def write_xmp(image: Path, properties: dict[str, str]) -> Path:
    sidecar = Path(f"{image}.xmp")
    packet = XmpPacket.empty()
    for name, value in properties.items():
        packet.set_scalar(name, value)
    sidecar.write_bytes(packet.serialize())
    return sidecar


class SequenceTests(unittest.TestCase):
    def test_discovers_common_images_in_natural_order(self):
        with tempfile.TemporaryDirectory() as directory:
            folder = Path(directory)
            for name in ("frame10.CR3", "frame2.cr3", "ignore.txt", "frame1.JPG"):
                (folder / name).write_bytes(b"")

            frames = discover_sequence(folder)

        self.assertEqual(
            [frame.image.name for frame in frames],
            ["frame1.JPG", "frame2.cr3", "frame10.CR3"],
        )
        self.assertEqual([frame.index for frame in frames], [0, 1, 2])

    def test_prefers_the_newer_of_two_sidecar_conventions(self):
        with tempfile.TemporaryDirectory() as directory:
            image = Path(directory) / "frame1.CR3"
            image.write_bytes(b"")
            appended = Path(f"{image}.xmp")
            replaced = image.with_suffix(".xmp")
            appended.write_text("appended", encoding="utf-8")
            replaced.write_text("replaced", encoding="utf-8")
            now = 1_800_000_000_000_000_000
            os.utime(appended, ns=(now, now))
            os.utime(replaced, ns=(now + 1_000_000, now + 1_000_000))

            self.assertEqual(find_sidecar(image), replaced)

    def test_appended_sidecar_wins_when_timestamps_match(self):
        with tempfile.TemporaryDirectory() as directory:
            image = Path(directory) / "frame1.CR3"
            image.write_bytes(b"")
            appended = Path(f"{image}.xmp")
            replaced = image.with_suffix(".xmp")
            appended.write_text("appended", encoding="utf-8")
            replaced.write_text("replaced", encoding="utf-8")
            same_time = 1_800_000_000_000_000_000
            os.utime(appended, ns=(same_time, same_time))
            os.utime(replaced, ns=(same_time, same_time))

            self.assertEqual(find_sidecar(image), appended)


class InterpolationTests(unittest.TestCase):
    def make_frames(self, folder: Path, count: int) -> list:
        for index in range(count):
            (folder / f"frame{index + 1:02}.dng").write_bytes(b"")
        return discover_sequence(folder)

    def test_whitelist_includes_old_and_new_numeric_sliders_but_not_enums(self):
        self.assertIn("Exposure", NUMERIC_CRS_PROPERTIES)
        self.assertIn("RedHue", NUMERIC_CRS_PROPERTIES)
        self.assertIn("Exposure2012", NUMERIC_CRS_PROPERTIES)
        self.assertIn("Highlights2012", NUMERIC_CRS_PROPERTIES)
        self.assertNotIn("CropUnits", NUMERIC_CRS_PROPERTIES)
        self.assertNotIn("ToneCurve", NUMERIC_CRS_PROPERTIES)
        self.assertNotIn("HasCrop", NUMERIC_CRS_PROPERTIES)

    def test_interpolates_only_shared_scalar_values(self):
        with tempfile.TemporaryDirectory() as directory:
            folder = Path(directory)
            frames = self.make_frames(folder, 3)
            write_xmp(
                frames[0].image,
                {
                    "xmp:Rating": "4",
                    "crs:Exposure2012": "0",
                    "crs:Brightness": "10",
                    "crs:CropUnits": "0",
                },
            )
            write_xmp(
                frames[2].image,
                {
                    "xmp:Rating": "4",
                    "crs:Exposure2012": "2",
                    "crs:Brightness": "30",
                    "crs:CropUnits": "1",
                },
            )
            plan = build_plan(discover_sequence(folder))

        self.assertEqual(plan.keyframe_indices, [0, 2])
        self.assertEqual(len(plan.changes), 1)
        self.assertEqual(
            plan.changes[0].values,
            {"Exposure2012": "1", "Brightness": "20"},
        )

    def test_one_keyframe_is_used_as_reference_for_all_other_frames(self):
        with tempfile.TemporaryDirectory() as directory:
            folder = Path(directory)
            frames = self.make_frames(folder, 3)
            write_xmp(
                frames[1].image,
                {"xmp:Rating": "4", "crs:Exposure": "-1.5"},
            )
            plan = build_plan(discover_sequence(folder))
            outputs = apply_plan(plan)

        self.assertEqual(plan.keyframe_indices, [1])
        self.assertEqual([path.name for path, _ in outputs], [
            "frame01.dng.xmp", "frame03.dng.xmp"
        ])
        self.assertEqual(
            [packet.get_scalar("crs:Exposure") for _, packet in outputs],
            ["-1.5", "-1.5"],
        )

    def test_sequence_without_marks_uses_first_image_as_reference(self):
        with tempfile.TemporaryDirectory() as directory:
            folder = Path(directory)
            frames = self.make_frames(folder, 3)
            write_xmp(frames[0].image, {"crs:RedHue": "-20"})
            plan = build_plan(discover_sequence(folder))

        self.assertEqual(plan.keyframe_indices, [0])
        self.assertEqual(len(plan.changes), 2)
        self.assertTrue(
            all(change.values == {"RedHue": "-20"} for change in plan.changes)
        )

    def test_interpolates_interior_and_virtual_edges_without_writing_endpoints(self):
        with tempfile.TemporaryDirectory() as directory:
            folder = Path(directory)
            frames = self.make_frames(folder, 7)
            write_xmp(frames[0].image, {"crs:Exposure2012": "-4"})
            write_xmp(
                frames[2].image,
                {"xmp:Rating": "4", "crs:Exposure2012": "0"},
            )
            write_xmp(
                frames[4].image,
                {"xmp:Rating": "4", "crs:Exposure2012": "4"},
            )
            write_xmp(frames[6].image, {"crs:Exposure2012": "8"})
            plan = build_plan(discover_sequence(folder))

        self.assertEqual(
            [change.frame.index for change in plan.changes], [1, 3, 5]
        )
        self.assertEqual(
            [change.values["Exposure2012"] for change in plan.changes],
            ["-2", "2", "6"],
        )

    def test_bad_sidecar_reports_the_path(self):
        with tempfile.TemporaryDirectory() as directory:
            folder = Path(directory)
            frames = self.make_frames(folder, 1)
            Path(f"{frames[0].image}.xmp").write_text("<broken", encoding="utf-8")

            with self.assertRaisesRegex(ValueError, "frame01.dng.xmp"):
                build_plan(discover_sequence(folder))


if __name__ == "__main__":
    unittest.main()
