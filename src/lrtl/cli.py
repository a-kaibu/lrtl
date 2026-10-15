"""Command-line interface for XMP sequence inspection and transitions."""

from __future__ import annotations

import argparse
from pathlib import Path
import sys

from . import __version__
from .interpolate import NUMERIC_CRS_PROPERTIES, apply_plan, build_plan
from .operations import mark_keyframe, packet_for_sidecar, unmark_keyframe, write_packet
from .sequence import Frame, discover_sequence, load_packet
from .xmp import XmpError


def _input_paths(image_or_sidecar: Path) -> tuple[Path, Path]:
    if image_or_sidecar.name.casefold().endswith(".xmp"):
        sidecar = image_or_sidecar
        image = Path(str(image_or_sidecar)[:-4])
    else:
        image = image_or_sidecar
        sidecar = Path(f"{image}.xmp")
        alternate = image.with_suffix(".xmp")
        if not sidecar.exists() and alternate.exists():
            sidecar = alternate
    return image, sidecar


def _scan(directory: Path) -> int:
    frames = discover_sequence(directory)
    print(f"Sequence: {directory}")
    print(f"Frames: {len(frames)}")
    if not frames:
        return 0
    print("#    image                                      XMP   mark   numeric controls")
    for frame in frames:
        state = "missing"
        marker = "-"
        controls = 0
        try:
            packet = load_packet(frame)
            if packet is not None:
                state = "yes"
                marker = packet.keyframe_kind() or "-"
                controls = sum(
                    packet.get_scalar(f"crs:{name}") is not None
                    for name in NUMERIC_CRS_PROPERTIES
                )
        except (OSError, XmpError) as exc:
            state = "invalid"
            marker = str(exc)
        print(
            f"{frame.index + 1:<4} {frame.image.name:<42} "
            f"{state:<6} {marker:<6} {controls}"
        )
    return 0


def _mark(image_or_sidecar: Path, kind: str, dry_run: bool, backup: bool) -> int:
    image, sidecar = _input_paths(image_or_sidecar)
    if not image.is_file() and not sidecar.is_file():
        raise ValueError(f"Image and XMP sidecar do not exist: {image}")
    packet = packet_for_sidecar(sidecar)
    rating = 5 if kind == "crop" else 4
    print(f"{image.name}: set {kind} keyframe, xmp:Rating={rating}")
    if dry_run:
        print(f"Dry run. Would write {sidecar}")
        return 0
    mark_keyframe(packet, kind)
    backup_path = write_packet(sidecar, packet, backup=backup)
    if backup_path:
        print(f"Backup: {backup_path}")
    print(f"Saved: {sidecar}")
    return 0


def _unmark(
    image_or_sidecar: Path,
    force_rating: bool,
    dry_run: bool,
    backup: bool,
) -> int:
    image, sidecar = _input_paths(image_or_sidecar)
    if not sidecar.is_file():
        raise ValueError(f"XMP sidecar does not exist: {sidecar}")
    packet = packet_for_sidecar(sidecar)
    if not packet.has_property("ol:KeyframeKind"):
        if not force_rating:
            raise ValueError(
                "This sidecar has no lrtl marker. Use --force-rating to clear "
                "an existing 4- or 5-star rating."
            )
        if packet.rating() not in {4, 5}:
            raise ValueError("This sidecar has no 4- or 5-star keyframe rating")
    print(f"{image.name}: remove keyframe mark")
    if dry_run:
        print(f"Dry run. Would write {sidecar}")
        return 0
    unmark_keyframe(packet, force_rating=force_rating)
    backup_path = write_packet(sidecar, packet, backup=backup)
    if backup_path:
        print(f"Backup: {backup_path}")
    print(f"Saved: {sidecar}")
    return 0


def _interpolate(directory: Path, write: bool, backup: bool) -> int:
    frames = discover_sequence(directory)
    plan = build_plan(frames)
    keyframe_names = [frames[index].image.name for index in plan.keyframe_indices]
    print(f"Sequence: {directory}")
    print(f"Frames: {plan.frame_count}; effective keyframes: {len(keyframe_names)}")
    print("Anchors: " + (", ".join(keyframe_names) if keyframe_names else "none"))
    print(f"Proposed frames: {len(plan.changes)}; numeric values: {plan.property_count}")
    if not write:
        print("Dry run. Pass --write to update sidecar XMP files.")
        for change in plan.changes:
            print(f"  {change.frame.image.name}: {', '.join(sorted(change.values))}")
        return 0

    written = 0
    for sidecar, packet in apply_plan(plan):
        backup_path = write_packet(sidecar, packet, backup=backup)
        if backup_path:
            print(f"Backup: {backup_path}")
        print(f"Saved: {sidecar}")
        written += 1
    print(f"Updated {written} sidecar(s).")
    return 0


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        prog="lrtl",
        description="Inspect and edit XMP metadata for timelapse image sequences.",
    )
    parser.add_argument("--version", action="version", version=f"lrtl {__version__}")
    commands = parser.add_subparsers(dest="command", required=True)

    scan = commands.add_parser("scan", help="list images, sidecars, and keyframes")
    scan.add_argument("directory", type=Path)

    mark = commands.add_parser("mark", help="mark an image as an XMP keyframe")
    mark.add_argument("image", type=Path)
    mark.add_argument("--kind", choices=("default", "crop"), default="default")
    mark.add_argument("--dry-run", action="store_true")
    mark.add_argument("--no-backup", action="store_true")

    unmark = commands.add_parser("unmark", help="remove an XMP keyframe mark")
    unmark.add_argument("image", type=Path)
    unmark.add_argument("--force-rating", action="store_true")
    unmark.add_argument("--dry-run", action="store_true")
    unmark.add_argument("--no-backup", action="store_true")

    interpolate = commands.add_parser(
        "interpolate", help="preview or write transitions between XMP keyframes"
    )
    interpolate.add_argument("directory", type=Path)
    interpolate.add_argument(
        "--write", action="store_true", help="write changes (otherwise preview only)"
    )
    interpolate.add_argument("--no-backup", action="store_true")
    return parser


def main(argv: list[str] | None = None) -> int:
    args = build_parser().parse_args(argv)
    try:
        if args.command == "scan":
            return _scan(args.directory)
        if args.command == "mark":
            return _mark(args.image, args.kind, args.dry_run, not args.no_backup)
        if args.command == "unmark":
            return _unmark(
                args.image,
                args.force_rating,
                args.dry_run,
                not args.no_backup,
            )
        if args.command == "interpolate":
            return _interpolate(args.directory, args.write, not args.no_backup)
    except (OSError, ValueError, XmpError) as exc:
        print(f"lrtl: error: {exc}", file=sys.stderr)
        return 2
    return 2
