# Compatibility notes

These notes record observed interfaces and public workflow behavior used to design the independent `lrtl` implementation. They do not reproduce LRTimelapse code.

## Local installation observations

- The installed version is LRTimelapse 7.5.5 under `C:\Program Files\LRTimelapse 7`.
- `LRTimelapse.cmd` starts the bundled Java runtime with `-jar LRTimelapse.jar`; the JAR manifest names `lrtimelapse.LRTimelapseApp` as its entry point. The GUI EXE is a 32-bit PE launcher analyzed with Ghidra, rather than the main metadata-processing implementation.
- The installation includes separate `exiftool.exe`, `ffmpeg.exe`, and `dnglab.exe` programs. These indicate separate metadata, video, and RAW-processing components for future interoperability work.
- The installed locale resources expose actions for keyframe toggling, XMP metadata initialization/reload/save, and transitions. Their helper labels identify four stars as a default keyframe and five stars as a crop keyframe.
- A limited scan of app-class string constants found Adobe's Camera Raw XMP namespace URI and workflow names for keyframes, Auto-Transition, Holy Grail, and visual deflicker. No product code or class output is part of this repository.
- `javap` inspection of the installed 7.5.5 JAR's visual-deflicker call path showed per-sequence fields for pass count, smoothing factor, constant-target mode, accuracy, and workflow state. The worker receives the selected sequence, accuracy, pass count, workflow value, `smoothingFactor * 80`, and constant-target flag. Each pass visits ordered frame records, reads a per-frame numeric measurement, derives a smoothed target curve through `lrtimelapse.eA` and a regularized solver (`Pia`), then stores per-frame correction deltas. This establishes the control flow and parameter roles; the exact luminance statistic, ROI sampling, curve constraints, and pixel-development math are not fully identified from this bytecode trace.
- A follow-up scan of class-file string constants found local-correction names such as `crs:LocalExposure2012`, `crs:LocalContrast2012`, and `crs:LocalColorGradeShadowSat`, alongside mask fields such as `crs:CorrectionMasks` and `crs:CorrectionAmount`. This confirms the installed program recognizes a richer local-adjustment XMP vocabulary; constants alone do not establish its serialization or interpolation behavior. The OSS CLI therefore preserves these fields without editing them.
- The installed JAR contains three XMP templates (`empty.xmp`, `raw_m_1.xmp`, and `raw_m_2.xmp`). The empty template uses scalar child elements for Camera Raw version/settings and crop defaults (`CropTop/Left=0`, `CropBottom/Right=1`, `CropAngle=0`), plus a structured `crs:Look` with `Name` and `Amount` attributes. The RAW templates contain many more scalar slider fields, along with booleans, enum-like values, and structured properties. These templates establish packet shape and vocabulary, but not which fields Auto-Transition interpolates.
- Bytecode metadata names sequence-level fields `rating`, `workflowKeyframes`, and `workflowAutoTransition`, and a global `initializationCreatesKeyframes` setting. This distinguishes per-sequence workflow state from per-image XMP ratings. The observed names do not reveal the full runtime keyframe algorithm.

## Public workflow behavior

- The official workflow describes XMP files as the exchange layer between LRTimelapse and Lightroom. Users choose keyframes, edit those images, save metadata, then run Auto-Transition to calculate settings for intervening frames.
- The official Keyframes Wizard describes its suggestions as an automatic analysis of the sequence luminance progression. The installed JAR scan exposed the global `initializationCreatesKeyframes` setting, but the exact wizard selection algorithm was not identified. `lrtl keyframes` therefore uses its own explicit luminance-curve heuristic and should not be treated as a behavioral clone.
- Keyframes appear as four-star ratings in Lightroom. The current official guide also documents manually adding and removing keyframes with the `1` and `0` keys.
- LRTimelapse 7.4 release notes state that first/last images are not automatically keyframes, a single keyframe is used as a reference for other frames, and a sequence without keyframes uses the first image as its reference.
- LRTimelapse 7.4 also describes linear transitions at sequence ends. The implementation uses existing numeric settings on the first/last image as virtual edge anchors when present, while leaving those endpoint files unchanged. If an edge value is absent, it falls back to the nearest keyframe's value for that property.

## Implemented compatibility choices

- Four-star ratings (`xmp:Rating=4`) identify ordinary keyframes; five-star ratings (`xmp:Rating=5`) identify crop keyframes according to the installed labels.
- A marker made by `lrtl mark` records the prior Lightroom rating in its own namespace so `lrtl unmark` can restore it. It always keeps the Adobe rating marker that Lightroom filters can see.
- Transitions operate only on an explicit list of scalar numeric Camera Raw properties. It includes selected legacy controls listed with integer/real types in Adobe's Camera Raw namespace reference and common newer Develop controls such as `Exposure2012`, `Contrast2012`, and `Highlights2012`. Boolean flags, categorical values such as `CropUnits`, circular color-wheel hue values, profiles, masks, arrays, and tone curves are left untouched because a numeric encoding alone does not make linear interpolation meaningful. Slider-style legacy hue offsets such as `RedHue` remain ordinary scalar controls.
- Every write is atomic and makes a timestamped sidecar backup unless `--no-backup` is provided. Interpolation is preview-only unless `--write` is provided.
- This milestone reads external sidecars only. It does not inspect or update XMP embedded in JPEG, TIFF, or DNG files, nor compare embedded and sidecar timestamps.
- Local correction masks and their per-mask settings remain out of scope until representative XMP packets and transition behavior can be compared safely.

## References

- [Adobe Camera Raw namespace reference](https://developer.adobe.com/xmp/docs/xmp-namespaces/crs/)
- [Adobe XMP specification overview](https://developer.adobe.com/xmp/docs/xmp-specifications/)
- [Adobe Photoshop edit operations example showing Camera Raw XMP controls](https://developer.adobe.com/firefly-services/docs/photoshop/guides/photoshop-v2/v1-to-v2/edit-operations)
- [LRTimelapse visual workflow](https://lrtimelapse.com/workflow/visual-workflow/)
- [LRTimelapse basic tutorial](https://lrtimelapse.com/tutorial/basic/complete/)
- [LRTimelapse 7.2 release notes: Simple Deflicker](https://lrtimelapse.com/updates/lrtimelapse-7-2/)
- [LRTimelapse 7.4 release notes](https://lrtimelapse.com/updates/lrtimelapse-7-4/)
