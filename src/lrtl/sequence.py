"""Image sequence discovery and XMP sidecar lookup."""

from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path
import re

from .xmp import XmpPacket


IMAGE_EXTENSIONS = {
    ".arw",
    ".cr2",
    ".cr3",
    ".dng",
    ".fff",
    ".iiq",
    ".jpeg",
    ".jpg",
    ".nef",
    ".nrw",
    ".orf",
    ".pef",
    ".png",
    ".raf",
    ".rw2",
    ".srw",
    ".tif",
    ".tiff",
}


@dataclass(frozen=True)
class Frame:
    index: int
    image: Path
    sidecar: Path | None

    @property
    def output_sidecar(self) -> Path:
        return self.sidecar or Path(f"{self.image}.xmp")


def natural_sort_key(path: Path) -> tuple[tuple[int, int | str], ...]:
    parts = re.split(r"(\d+)", path.name.casefold())
    return tuple(
        (0, int(part)) if part.isdigit() else (1, part)
        for part in parts
    )


def sidecar_candidates(image: Path) -> list[Path]:
    return [Path(f"{image}.xmp"), image.with_suffix(".xmp")]


def find_sidecar(image: Path) -> Path | None:
    existing = [candidate for candidate in sidecar_candidates(image) if candidate.is_file()]
    if not existing:
        return None
    return max(
        existing,
        key=lambda candidate: (
            candidate.stat().st_mtime_ns,
            candidate.name.casefold().endswith(image.suffix.casefold() + ".xmp"),
        ),
    )


def discover_sequence(directory: Path) -> list[Frame]:
    if not directory.is_dir():
        raise ValueError(f"Not a directory: {directory}")
    images = sorted(
        (
            path
            for path in directory.iterdir()
            if path.is_file() and path.suffix.casefold() in IMAGE_EXTENSIONS
        ),
        key=natural_sort_key,
    )
    return [
        Frame(index=index, image=image, sidecar=find_sidecar(image))
        for index, image in enumerate(images)
    ]


def load_packet(frame: Frame) -> XmpPacket | None:
    if frame.sidecar is None:
        return None
    return XmpPacket.parse(frame.sidecar.read_bytes())
