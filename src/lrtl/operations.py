"""File-level operations with atomic writes and recoverable backups."""

from __future__ import annotations

from datetime import datetime
import os
from pathlib import Path
import shutil
import tempfile

from .xmp import XmpPacket


ABSENT_RATING = "__absent__"


def packet_for_sidecar(path: Path) -> XmpPacket:
    return XmpPacket.parse(path.read_bytes()) if path.exists() else XmpPacket.empty()


def mark_keyframe(packet: XmpPacket, kind: str) -> int:
    if kind not in {"default", "crop"}:
        raise ValueError(f"Unsupported keyframe kind: {kind}")
    if not packet.has_property("ol:KeyframeKind"):
        previous_rating = packet.get_scalar("xmp:Rating")
        packet.set_scalar(
            "ol:PreviousRating",
            previous_rating if previous_rating is not None else ABSENT_RATING,
        )
    rating = "5" if kind == "crop" else "4"
    packet.set_scalar("xmp:Rating", rating)
    packet.set_scalar("ol:KeyframeKind", kind)
    return int(rating)


def unmark_keyframe(packet: XmpPacket, force_rating: bool = False) -> None:
    if packet.has_property("ol:KeyframeKind"):
        previous_rating = packet.get_scalar("ol:PreviousRating")
        if previous_rating == ABSENT_RATING:
            packet.remove_property("xmp:Rating")
        elif previous_rating is not None:
            packet.set_scalar("xmp:Rating", previous_rating)
        packet.remove_property("ol:KeyframeKind")
        packet.remove_property("ol:PreviousRating")
        return
    if force_rating and packet.rating() in {4, 5}:
        packet.remove_property("xmp:Rating")
        return
    raise ValueError(
        "This marker was not created by lrtl; pass --force-rating to clear an "
        "existing 4- or 5-star rating"
    )


def write_packet(path: Path, packet: XmpPacket, backup: bool = True) -> Path | None:
    path.parent.mkdir(parents=False, exist_ok=True)
    backup_path = None
    if path.exists() and backup:
        timestamp = datetime.now().strftime("%Y%m%d-%H%M%S-%f")
        backup_path = path.with_name(f"{path.name}.lrtl-{timestamp}.bak")
        shutil.copy2(path, backup_path)

    temp_path: Path | None = None
    try:
        with tempfile.NamedTemporaryFile(
            mode="wb", dir=path.parent, prefix=f".{path.name}.", suffix=".tmp", delete=False
        ) as stream:
            temp_path = Path(stream.name)
            stream.write(packet.serialize())
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temp_path, path)
    except OSError:
        if temp_path is not None:
            temp_path.unlink(missing_ok=True)
        raise
    return backup_path
