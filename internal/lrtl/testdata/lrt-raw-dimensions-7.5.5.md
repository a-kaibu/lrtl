> Later extension: Panasonic RAW/RW2/RWL border and JPEG/ExifIFD dimension selection now has 84 additional native observations. See [RW2 dimensions and preview video](lrt-rw2-dimensions-7.5.5.md). The earlier 80 TIFF observations below remain unchanged.

# TIFF/RAW ImageSize and save prefix, LRTimelapse 7.5.5

## Native observations

`lrt-raw-dimensions-7.5.5.json` records 80 original metadata-only synthetic files,
their base64 bytes, the installed ExifTool 13.59 `ImageSize`, and the XMP result
from the genuine LRTimelapse 7.5.5 metadata reader/save prefix. The dataset contains
no camera photographs, proprietary binaries or decompiled implementation code.

`scripts/probes/raw_dimensions_fixture.py` creates the inputs using Python's
standard library. Go tests read the frozen bytes and do not need Python.
`scripts/probes/RawDimensionsOracle.java` requests ExifTool's `-ImageSize` in a
single batch, passes the resulting label to the genuine 16-column CSV metadata
reader, and calls the actual registered-column save method on synthetic frames.
HasSettings=False isolates dimension effects from clock-dependent date writes.
The save stops at the previously documented null `LRTimelapseMain.m()` history
stage; history metadata is removed from the recorded synthetic result. No main
object, license state or proprietary method is replaced. This is not a completed
application save or a RAW development comparison.

Two files exceed Java's positive int32 dimension range and fail in the metadata
parser; the other 78 reach the save prefix. Cases cover both TIFF byte orders,
32-bit dimensions above 65535, the int32 boundary, missing/zero dimensions,
ordinary and priority SubIFDs, multiple/nested/next IFDs, old SubfileType,
directory cycles, unsorted tags, CR2 EXIF overrides, BigTIFF and RAW extensions.

## Implemented rules

`init --frame-state lrt-7.5.5` now uses a bounded TIFF directory reader before
falling back to the image configuration. It does not decode pixel strips or
select the largest preview. It retains full integer precision for SHORT/LONG
and BigTIFF LONG8 fields instead of using imagemeta's uint16 dimension fields.

- ImageWidth and ImageHeight are selected independently. Without a priority
  directory, the first value wins. Thus a synthetic file can yield dimensions
  from different IFDs, as the native composite does.
- The first directory explicitly marked NewSubfileType=0 or OldSubfileType=1
  becomes the priority directory. Its dimension tags take precedence. The
  reader follows pointers depth first, in file entry order; a late SubfileType
  tag does not retroactively change already read dimensions. Unsorted-tag
  observations cover this difference.
- A CR2 header uses nonzero ExifImageWidth/ExifImageHeight together when both
  ordinary ImageWidth/Height are present, following the native ImageSize
  composite's Canon exception. Renaming a generic TIFF is not the CR2 test.
- Both dimensions must be available for an ImageSize label. Zero dimensions
  remain valid cache inputs, while only positive dimensions are written into
  lrt:Width/Height. Values above 2147483647 reject the initialization plan before
  any sidecars are saved.
- BigTIFF .tif/.tiff headers are supported. The installed ExifTool's DNG path
  does not expose ImageSize for the synthetic BigTIFF DNG files; the Go reader
  likewise does not invent a size for those RAW-extension inputs.
- Cycles and repeated offsets are skipped. Depth, directory count, entry count,
  pointer arrays, byte ranges, scalar type and int32 range are bounded. Malformed
  offsets, overlarge directories and unsupported scalar fields return errors.

`--frame-state preserve` retains the earlier initialization behavior.

## Circular-mask interpolation

The circular-mask dimension lookup now accepts root lrt:Width/Height after its
existing root EXIF/TIFF dimension fields. If those caches are incomplete, it uses
the same native TIFF ImageSize selection before its raster/general metadata
fallback. This fixes the TIFF-family path that could otherwise use a small
IFD0 preview or truncated width. Nested properties named like global dimensions
no longer shadow the root cache. The existing EXIF/TIFF cache precedence is
retained; complete application cache-loading precedence is not established here.

## Verification

- All 80 ImageSize and native XMP results match; installed replay reproduced the
  dataset. The focused installed suite also reran earlier metadata/save probes
  and initialization tests (19.378 seconds).
- Positive measured dimensions also pass through circular-mask frame-size
  lookup; cached dimensions, nested shadowing and overflow rejection have
  regressions. Existing radial helper/pipeline reference comparisons still pass.
- CLI tests cover large dimensions, preserve mode, preview without writes,
  exact backups, original image retention, repeated initialization and rejection
  of a later overflowing frame before earlier sidecars are written.
- The full pure-Go suite passed in 112.200 seconds. Focused race tests including
  radial pipelines passed in 114.341 seconds. Go 1.23 checks, Go 1.26 vet and
  CGO-disabled builds passed. The raw dimension fuzz target completed 18,461
  generated file inputs in 45 seconds; these are not native-reference cases.
- The built executable initialized six metadata-only files, including large
  NEF, priority SubIFD, CR2 and BigTIFF cases. Saved dimensions and previews match
  the native labels; images, exact backups and repeated results are retained.
  Separately, three actual PNG frames generated pure-Go MJPEG AVI before/after
  initialization. FFmpeg-decoded RGB bytes were identical (SHA-256
  `7b6d83b0c59a838ed7cf35143d16ec12ecb5c37da61d4dca99f7bafa0f7cc178`).

## Reproduction

```powershell
go test ./internal/lrtl -run 'TestRawDimensions|TestRadialMaskDimension|FuzzRawDimensions' -count=1
go test ./internal/lrtl -run '^$' -fuzz '^FuzzRawDimensions$' -fuzztime 45s

# Optional installed-component replay; inputs are written to a temporary folder.
$env:LRTL_NATIVE_JAR='C:/Program Files/LRTimelapse 7/LRTimelapse.jar'
$env:LRTL_ICC_EXIFTOOL='C:/Program Files/LRTimelapse 7/exiftool.exe'
go test ./internal/lrtl -run '^TestRawDimensionsInstalledImageSizeAndSave$' -count=1

# Regenerate original synthetic inputs in a NEW directory.
python scripts/probes/raw_dimensions_fixture.py work/raw-size-inputs
```

## Remaining scope

Subsequent work adds [RW2 dimensions](lrt-rw2-dimensions-7.5.5.md) and
[RAF cropped-size selection](lrt-raf-dimensions-7.5.5.md), with separate native
observations and limitations.

This implements the observed TIFF/CR2 dimension path. RW2's separate tag table,
RAF cropped-size composites, CR3/other containers, Phase One-specific size
selection, proprietary maker-note overrides and every malformed TIFF variant
are not covered by these comparisons. Non-TIFF circular-mask metadata fallback
still has the earlier general-reader limitations. RAW demosaicing/rendering is
not added by this metadata change. Capture-date updates, complete frame-cache
loading and history completion remain separate work.
