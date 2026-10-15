"""A conservative linear interpolator for common numeric Camera Raw controls."""

from __future__ import annotations

from dataclasses import dataclass, field
import math
from pathlib import Path

from .sequence import Frame, load_packet
from .xmp import XmpError, XmpPacket


# Categorical fields, masks, tone curves, profiles, and unknown properties are
# intentionally excluded. These are numeric slider values whose interpolation
# has a clear meaning across two edited keyframes.
NUMERIC_CRS_PROPERTIES = (
    "Blacks2012",
    "Clarity2012",
    "ColorNoiseReduction",
    "ColorNoiseReductionDetail",
    "ColorNoiseReductionSmoothness",
    "Contrast2012",
    "CropAngle",
    "CropBottom",
    "CropLeft",
    "CropRight",
    "CropTop",
    "Dehaze",
    "Exposure2012",
    "Highlights2012",
    "LuminanceSmoothing",
    "Saturation",
    "Shadows2012",
    "SharpenDetail",
    "SharpenEdgeMasking",
    "SharpenRadius",
    "Sharpness",
    "Temperature",
    "Texture",
    "Tint",
    "Vibrance",
    "Whites2012",
)


@dataclass
class ProposedFrame:
    frame: Frame
    packet: XmpPacket
    values: dict[str, str] = field(default_factory=dict)
    created_sidecar: bool = False


@dataclass
class InterpolationPlan:
    frame_count: int
    keyframe_indices: list[int]
    changes: list[ProposedFrame]

    @property
    def property_count(self) -> int:
        return sum(len(change.values) for change in self.changes)


def _numeric_value(value: str | None) -> float | None:
    if value is None:
        return None
    try:
        parsed = float(value)
    except ValueError:
        return None
    return parsed if math.isfinite(parsed) else None


def _numeric_crs_values(packet: XmpPacket) -> dict[str, float]:
    result: dict[str, float] = {}
    for property_name in NUMERIC_CRS_PROPERTIES:
        value = _numeric_value(packet.get_scalar(f"crs:{property_name}"))
        if value is not None:
            result[property_name] = value
    return result


def _format_number(value: float) -> str:
    if abs(value) < 1e-12:
        value = 0.0
    return format(value, ".12g")


def _linear_values(
    target_index: int,
    start_index: int,
    end_index: int,
    start_values: dict[str, float],
    end_values: dict[str, float],
) -> dict[str, float]:
    span = end_index - start_index
    if span <= 0:
        return {}
    fraction = (target_index - start_index) / span
    shared = start_values.keys() & end_values.keys()
    return {
        name: start_values[name] + (end_values[name] - start_values[name]) * fraction
        for name in shared
    }


def build_plan(frames: list[Frame]) -> InterpolationPlan:
    packets: list[XmpPacket] = []
    created: list[bool] = []
    for frame in frames:
        if frame.sidecar is None:
            packets.append(XmpPacket.empty())
            created.append(True)
        else:
            try:
                packets.append(load_packet(frame))
            except (OSError, XmpError) as exc:
                raise XmpError(f"{frame.sidecar}: {exc}") from exc
            created.append(False)

    explicit = [
        index for index, packet in enumerate(packets) if packet.is_keyframe()
    ]
    keyframes = explicit if explicit else ([0] if packets else [])
    proposed: dict[int, ProposedFrame] = {}

    def assign(target_index: int, source_values: dict[str, float]) -> None:
        if target_index in keyframes or not source_values:
            return
        target = proposed.get(target_index)
        if target is None:
            target = ProposedFrame(
                frame=frames[target_index],
                packet=packets[target_index],
                created_sidecar=created[target_index],
            )
            proposed[target_index] = target
        for name, value in source_values.items():
            rendered = _format_number(value)
            current = _numeric_value(target.packet.get_scalar(f"crs:{name}"))
            if current is None or not math.isclose(
                current, value, rel_tol=1e-12, abs_tol=1e-12
            ):
                target.values[name] = rendered

    if len(keyframes) == 1:
        anchor = _numeric_crs_values(packets[keyframes[0]])
        for target_index in range(len(frames)):
            assign(target_index, anchor)
    elif len(keyframes) > 1:
        first, last = keyframes[0], keyframes[-1]
        first_values = _numeric_crs_values(packets[first])
        last_values = _numeric_crs_values(packets[last])

        # An unmarked sequence endpoint can contain an existing develop value.
        # Use it as a virtual endpoint and connect it to the nearest keyframe.
        if first > 0:
            edge_values = _numeric_crs_values(packets[0])
            for target_index in range(1, first):
                values = dict(first_values)
                values.update(
                    _linear_values(
                        target_index, 0, first, edge_values, first_values
                    )
                )
                assign(target_index, values)
        if last < len(frames) - 1:
            edge_index = len(frames) - 1
            edge_values = _numeric_crs_values(packets[edge_index])
            for target_index in range(last + 1, edge_index):
                values = dict(last_values)
                values.update(
                    _linear_values(
                        target_index, last, edge_index, last_values, edge_values
                    )
                )
                assign(target_index, values)

        for left, right in zip(keyframes, keyframes[1:]):
            left_values = _numeric_crs_values(packets[left])
            right_values = _numeric_crs_values(packets[right])
            for target_index in range(left + 1, right):
                values = _linear_values(
                    target_index, left, right, left_values, right_values
                )
                assign(target_index, values)

    changes = [
        change
        for change in (proposed[index] for index in sorted(proposed))
        if change.values
    ]
    return InterpolationPlan(
        frame_count=len(frames), keyframe_indices=keyframes, changes=changes
    )


def apply_plan(plan: InterpolationPlan) -> list[tuple[Path, XmpPacket]]:
    output: list[tuple[Path, XmpPacket]] = []
    for change in plan.changes:
        for name, value in change.values.items():
            change.packet.set_scalar(f"crs:{name}", value)
        output.append((change.frame.output_sidecar, change.packet))
    return output
