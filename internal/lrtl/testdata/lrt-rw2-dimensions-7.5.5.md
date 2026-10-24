# Panasonic RAW / RW2 / RWL dimensions and preview video

## Native observations

`lrt-rw2-dimensions-7.5.5.json` contains 84 original synthetic input files,
ExifTool 13.59 ImageSize labels and the XMP saved by the genuine LRTimelapse
7.5.5 metadata-reader/save prefix. The inputs include both TIFF byte orders,
RW2's 24-byte identifying header, old Panasonic RAW headers and Leica RWL.
No camera photographs, proprietary binaries or decompiled code are included.

`scripts/probes/rw2_dimensions_fixture.py` uses the shared original TIFF builder
and a small original solid-color JPEG. Go tests read the frozen base64 data and
do not require Python. Some metadata-only cases alter JPEG dimension headers;
the video checks use the unmodified, valid 64x32 JPEG.

The existing original `RawDimensionsOracle.java` runs the installed ExifTool
ImageSize request, passes its label to the genuine 16-column CSV reader and
invokes the native frame store method. HasSettings=False isolates dimensions
from clock-dependent capture-date writes. Four signed-range cases fail during
CSV parsing; the other 80 reach the known null `LRTimelapseMain.m()` history
stop. Nondeterministic history is removed from the recorded synthetic result.
No application main, license state or proprietary method is replaced. This is
not a completed GUI save or a native video-rendering comparison.

## Implemented selection

The Go reader dispatches TIFF magic 85 to the Panasonic tag table. Ordinary TIFF
containers renamed .rw2 continue to use the ordinary TIFF dimension rules.

- SensorWidth/Height (tags 2/3) do not define the native output dimensions.
  ImageWidth is SensorRightBorder minus SensorLeftBorder (7 minus 5); ImageHeight
  is SensorBottomBorder minus SensorTopBorder (6 minus 4).
- Border tags come from the Panasonic main-directory/next-directory chain.
  Later main-chain values replace earlier values. The same small numeric tags
  inside ExifIFD or an unrecognized SubIFD do not supply these borders.
- A missing border pair allows that axis to fall back to ordinary dimensions
  from the standard ExifIFD, then to the embedded JPEG (tag 46). The two axes
  are independent: a case can produce 4000x32 from borders plus JPEG height.
- ExifIFD dimensions take precedence over embedded JPEG dimensions regardless
  of root entry order. Among valid embedded JPEG headers, the later one wins
  for this ImageSize fallback. The existing preview-image extractor separately
  chooses its largest decodable preview; these are different operations.
- Complete border pairs override fallback values even when the difference is
  zero or negative. Signed int32 values, including -2147483648, can enter the
  native frame cache; only positive values are written to lrt:Width/Height.
  A size label outside signed int32 is rejected before sidecar writes.
- Sensor dimensions, the separate Panasonic Crop tags and orientation do not
  replace this ImageSize calculation. A fully missing axis produces no size.

The TIFF graph retains the existing depth, directory count, scalar count/type
and byte-range bounds. JPEG data is read only when needed to fill a missing axis,
with a 32 MiB field limit; successful header reads do not decode JPEG pixels.
Unused JPEG pointers are not dereferenced when borders/ExifIFD already supply
both axes. Required out-of-file JPEG data rejects the plan. Cycles are skipped.
Malformed-field handling can be stricter than ExifTool warning/recovery behavior.

The path is shared by native-profile initialization and circular-mask dimension
lookup. `init --frame-state preserve` retains the earlier profile behavior.
The standard TIFF/CR2 reader was extracted into a reusable graph walker; its
previous 80 native observations and original generator byte strings are unchanged.

## Preview-based timelapse

The existing TIFF preview extractor already reads tag 46. `.raw` and `.rwl`
now join `.rw2` in its accepted extensions, enabling Panasonic TIFF-layout RAW
and Leica RWL previews in render/analysis/export workflows using `--raw-preview`.
This does not add readers for every format that uses the .raw suffix.

```powershell
lrtl init D:\Timelapse\RW2 --write
lrtl render D:\Timelapse\RW2 --raw-preview --apply-xmp-basic --output preview.avi
```

Output uses the embedded JPEG's resolution, which can differ from the sensor
dimensions written by init. It is not RAW demosaicing or Lightroom/Camera Raw
pixel equivalence. Full RAW development still uses the existing explicit backend.

## Tests and reproduction

```powershell
go test ./internal/lrtl -run 'TestRW2|TestRawDimensions|FuzzRW2|FuzzRawDimensions' -count=1
go test ./internal/lrtl -run '^$' -fuzz '^FuzzRW2Dimensions$' -fuzztime 45s

$env:LRTL_NATIVE_JAR='C:/Program Files/LRTimelapse 7/LRTimelapse.jar'
$env:LRTL_ICC_EXIFTOOL='C:/Program Files/LRTimelapse 7/exiftool.exe'
go test ./internal/lrtl -run 'TestRW2DimensionsInstalled|TestRawDimensionsInstalled' -count=1

# Optional regeneration into a NEW directory (Python standard library only).
python scripts/probes/rw2_dimensions_fixture.py work/rw2-inputs
```

Frozen tests compare all 84 labels and native XMP outputs, plus positive-size
radial lookup. Installed replay also reran the earlier 80 TIFF cases and camera
metadata probes (11.256 seconds). CLI regressions cover both byte orders, large
dimensions, JPEG/ExifIFD fallback, RAW/RWL variants, exact backups, source
retention, dry runs, repeated initialization, late overflow rejection, unused
versus required broken JPEG pointers and directory cycles.

Three-frame render tests for each of RW2/RWL/Panasonic RAW compare encoded AVI
frame data to an extracted-JPEG control. Built-executable checks separately
decoded those three 64x32-frame AVIs with FFmpeg; all RGB bytes matched the JPEG
control (SHA-256
`6bb0a959695b1b8d272f9fceaf3de8f0ea5a95e5b0b5eb632e61961df4c24138`).
Four additional built-executable metadata cases checked native dimension labels,
preview without writes, image retention and repeat without writes.

On 2026-10-23 the full pure-Go suite passed in 121.697 seconds, focused race tests
passed in 7.829 seconds, and Go 1.23 checks passed in 2.335 seconds. Go 1.26 vet
and CGO-disabled builds passed; no production dependencies were added.
The final fuzz run completed 18,156 generated inputs in 45 seconds, exercising
both signed-dimension cache round trips and preview extraction without changing
the source bytes. Each worker reuses a private temporary file to reduce directory
creation overhead. Generated input counts are not independent native observations.

## Remaining scope

These measurements cover synthetic headers and the observed cache/save prefix,
not all camera models or a completed application workflow. Proprietary maker
notes, very large/invalid embedded JPEG recovery, complete frame-cache precedence,
capture-date/history completion and native RAW development remain incomplete.
External sidecars supply RWL/Panasonic RAW edits; extending their embedded-XMP
container coverage remains separate work.
[RAF cropped-size composites](lrt-raf-dimensions-7.5.5.md) were added subsequently.
[CR3 dimensions](lrt-cr3-dimensions-7.5.5.md) were also added subsequently.
Phase One-specific size selection remains separate work.
