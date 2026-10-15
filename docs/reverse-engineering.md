# Compatibility notes

These notes record observed interfaces and public workflow behavior used to design the independent `lrtl` implementation. They do not reproduce LRTimelapse code.

## Local installation observations

- The installed version is LRTimelapse 7.5.5 under `C:\Program Files\LRTimelapse 7`.
- `LRTimelapse.cmd` starts the bundled Java runtime with `-jar LRTimelapse.jar`; the JAR manifest names `lrtimelapse.LRTimelapseApp` as its entry point. The GUI EXE is a 32-bit PE launcher analyzed with Ghidra, rather than the main metadata-processing implementation.
- The installation includes separate `exiftool.exe`, `ffmpeg.exe`, and `dnglab.exe` programs. These indicate separate metadata, video, and RAW-processing components for future interoperability work.
- The installed locale resources expose actions for keyframe toggling, XMP metadata initialization/reload/save, and transitions. Their helper labels identify four stars as a default keyframe and five stars as a crop keyframe.
- A limited scan of app-class string constants found Adobe's Camera Raw XMP namespace URI and workflow names for keyframes, Auto-Transition, Holy Grail, and visual deflicker. No product code or class output is part of this repository.

## Public workflow behavior

- The official workflow describes XMP files as the exchange layer between LRTimelapse and Lightroom. Users choose keyframes, edit those images, save metadata, then run Auto-Transition to calculate settings for intervening frames.
- Keyframes appear as four-star ratings in Lightroom. The current official guide also documents manually adding and removing keyframes with the `1` and `0` keys.
- LRTimelapse 7.4 release notes state that first/last images are not automatically keyframes, a single keyframe is used as a reference for other frames, and a sequence without keyframes uses the first image as its reference.
- LRTimelapse 7.4 also describes linear transitions at sequence ends. The implementation uses existing numeric settings on the first/last image as virtual edge anchors when present, while leaving those endpoint files unchanged. If an edge value is absent, it falls back to the nearest keyframe's value for that property.

## Implemented compatibility choices

- Four-star ratings (`xmp:Rating=4`) identify ordinary keyframes; five-star ratings (`xmp:Rating=5`) identify crop keyframes according to the installed labels.
- A marker made by `lrtl mark` records the prior Lightroom rating in its own namespace so `lrtl unmark` can restore it. It always keeps the Adobe rating marker that Lightroom filters can see.
- Transitions operate only on an explicit list of numeric Camera Raw properties. Categorical, structured, mask, profile, and tone-curve properties are left untouched.
- Every write is atomic and makes a timestamped sidecar backup unless `--no-backup` is provided. Interpolation is preview-only unless `--write` is provided.
- This milestone reads external sidecars only. It does not inspect or update XMP embedded in JPEG, TIFF, or DNG files, nor compare embedded and sidecar timestamps.

## References

- [LRTimelapse visual workflow](https://lrtimelapse.com/workflow/visual-workflow/)
- [LRTimelapse basic tutorial](https://lrtimelapse.com/tutorial/basic/complete/)
- [LRTimelapse 7.4 release notes](https://lrtimelapse.com/updates/lrtimelapse-7-4/)
