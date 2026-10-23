> Later extension: TIFF-family RAW dimension extraction now covers priority IFD selection, large integer dimensions and the CR2 override. See [RAW dimensions](lrt-raw-dimensions-7.5.5.md) for 80 input/ExifTool/native-save comparisons and remaining container limits.

# Camera metadata cache and save prefix, LRTimelapse 7.5.5

## Observations

`lrt-frame-metadata-save-7.5.5.json` contains 75 synthetic cases observed with
the installed Windows 7.5.5 JAR. The original
`scripts/probes/FrameMetadataSaveOracle.java` calls the genuine CSV metadata
reader on a controller shell without running its GUI constructor, then calls
the genuine frame save method. No main object, license state or proprietary
method is replaced. Three invalid dimension strings fail during metadata parsing;
the remaining 72 cases reach the known null `LRTimelapseMain.m()` history stop.

HasSettings is explicitly False to isolate camera-cache effects from the
clock-dependent capture-date/MetadataDate save branch. The probe removes the
nondeterministic history namespace using the same method as the registered-save
observer. This measures completed camera metadata effects before the history
stop, not a completed application workflow. Capture-date updates are not covered
by these frozen comparisons.

The synthetic CSV always has 16 columns, with a NEF filename, missing dates and
one varied field. The varied positions are dimensions (1), aperture (3), shutter
(4), ISO (5), shooting mode (7), orientation (10) and merged HDR (13).

## Implemented cache semantics

- Width/height are signed Java int32 values; only positive dimensions are saved
  as `lrt:Width/Height`. Exactly two nonempty lowercase-x-separated components are parsed; empty tokens are discarded (for example, `2xx3x` becomes 2 by 3).
  Unrecognized shape text leaves dimensions unset; invalid components or overflow
  abort parsing. Leading whitespace is not accepted by the native integer parser.
- Aperture must pass the Java floating-point lexical check, but its original
  spelling is saved, including whitespace, suffixes and hex notation. Missing,
  empty or invalid aperture becomes `n/a` in the synthetic fresh cache. Zero,
  negative values, NaN and Infinity are accepted by that cache reader.
- Shutter and ISO are strings, not validated physical camera numbers at this
  stage. Missing/empty shutter becomes `n/a`; missing ISO becomes `n/a` but an
  explicit empty ISO remains empty and is not written. HG later applies its own
  numeric validation.
- Shooting mode retains its spelling, with an empty string becoming `n/a`.
- Orientation defaults to 1 when parsing fails, accepts BMP Unicode digits and
  is not clamped to the valid display-orientation range by this cache helper.
- HDR trims whitespace, compares `true` without case sensitivity and writes
  canonical `true`/`false` as `lrt:IsMergedHDR`.

## CLI integration and image-reader limits

Default `init --frame-state lrt-7.5.5` now refreshes available camera metadata
before the existing registered-column save preparation. Preview lists the final
LRT dimensions and camera labels. `--frame-state preserve` skips this refresh.
Images are not modified; dry-run, sidecar backup and sequence preflight behavior
are retained.

The image adapter is independently implemented in Go. Supported raster headers
supply raw, unrotated dimensions without decoding pixels. Existing EXIF metadata
and the already measured HG label formatter supply aperture, shutter and ISO;
manual exposure is recognized. Existing LRT camera labels supply missing image
tags, including hand-entered lens aperture and cached mode/HDR values. Absent
orientation becomes 1. PNG eXIf orientation is read with bounded chunk traversal,
a 1 MiB payload limit and checksum validation, including either TIFF byte order.
A later malformed PNG metadata chunk rejects the initialization plan before
earlier sidecars are written.

Unknown/unreadable image metadata retains existing camera state; it does not
prevent the previously supported metadata-only initialization path. RAW images
can supply EXIF camera values where the existing reader supports them, but RAW
dimensions without a supported image configuration are not refreshed. In
particular, imagemeta's uint16 dimension fields are not used, to avoid silently
truncating large dimensions. Camera-specific auto/program mode labels, image
merged-HDR detection, every RAW container and full metadata cache precedence
remain incomplete. These are image-adapter limits; the 75 cases compare the
camera-cache parser/save semantics independently of image extraction.

Capture dates, wall-clock MetadataDate updates, full application cache loading,
`.lrt` cache persistence and history completion remain outside this change.
Other editing commands and `copy-settings` do not refresh image metadata here.

## Tests and reproduction

```powershell
go test ./internal/lrtl -run 'TestFrameMetadata|FuzzFrameMetadata' -count=1
go test ./internal/lrtl -run '^$' -fuzz '^FuzzFrameMetadataCameraFields$' -fuzztime 45s

$env:LRTL_NATIVE_JAR='C:/Program Files/LRTimelapse 7/LRTimelapse.jar'
go test ./internal/lrtl -run 'TestFrameMetadataInstalledCameraCache|TestRegisteredSaveInstalledPrefix' -count=1
```

The tests include 75 frozen cases and installed replay, 94 synthetic EXIF JPEGs
with independently expected exposure labels, both PNG TIFF byte orders, retained
manual camera labels, default/preserve CLI profiles, exact image/backup retention,
dry run, repeat without writes and rejection before writes for corrupt metadata.
The focused installed-component suite also reran the previous 864 registered-save
observations after extracting shared probe setup; it passed in 13.594 seconds.

On 2026-10-23, focused race tests passed (13.262 seconds), Go 1.23 tests passed,
Go 1.26 vet and a CGO-disabled executable build passed. The camera-field fuzz
target completed 176,450 generated inputs in 45 seconds, checking XML readback,
stable repeated writes and no mutation on errors. This is not an exhaustive native
comparison or a fuzz test of every third-party image decoder.

The built executable initialized three synthetic 32x24 PNGs and generated pure-Go
MJPEG AVI before and after. Dry-run, image retention, dimension preview/save and
repeat without writes passed. FFmpeg-decoded RGB bytes were identical across all
three frames (SHA-256
`74c50eb63f12082644b12318541aad234e89743965e7658e262b664bc5b8018e`).

The full pure-Go suite with the 75 frozen observations passed in 124.160 seconds. Focused checks passed again after the final strict-float lexical guard (2.698 seconds). No production dependencies were added.
