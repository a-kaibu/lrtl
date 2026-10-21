# ICC labels for native intermediary images

The installed 7.5.5 headless renderer queries the first sorted intermediary
image with its bundled ExifTool:

```text
-b -ICC_Profile:ProfileDescription <first-image> -m
```

The returned description, after CRLF replacement and Java-style trimming,
enters the native color planner. A case-sensitive `2020` substring selects
BT.2020; other descriptions, including absent profiles, select BT.709. A
case-sensitive `PQ` suffix sets the preset dynamic range to PQ. Inspection of
the preset setter confirms that it changes dynamic range only, not gamut.
The existing 162 measured color-plan observations cover the subsequent native
filter selection. These are label-based rules, not matrix/TRC/LUT evaluation.

## Implemented behavior

`render --output-color lrt-7.5.5` defaults to `--input-color auto`. It reads
only the first **selected** source, before output creation. Pure Go readers
support multipart JPEG APP2, PNG iCCP/zlib, and classic/BigTIFF ICC tag 34675,
including linked directories and SubIFDs. ICC `desc`, `text`, and `mluc`
description encodings are supported. As with the bundled ExifTool's base
ProfileDescription query, an mluc record in another language does not replace
the en-US base record. Missing profiles default to BT.709.

The CLI also detects PQ unless `--dynamic-range` was explicitly supplied.
An explicit `--input-color bt709|bt2020` bypasses ICC inspection and inference.
`--output-gamut` is never inferred: PQ still requires a BT.2020 output target.
Preset color settings are applied before inference; explicit CLI dynamic range
overrides are retained. Direct Go callers can enable PQ inference with
`RenderOptions.DetectDynamicRange`; zero-value options infer only input gamut.

```powershell
# An ICC description containing 2020 and ending in PQ selects the PQ path.
lrtl render C:\photos\prepared --output C:\photos\hdr.mp4 --output-color lrt-7.5.5 --output-gamut bt2020 --codec h265
```

Standard output, intermediate-image export, RAW development, embedded RAW
previews, and LRT preview-cache rendering retain their existing sRGB paths.
Malformed ICC metadata fails before replacing an existing video; callers can
explicitly select input color to bypass metadata interpretation. This strict
error behavior intentionally differs from ExifTool's `-m` tolerance.

## Tests and limits

- Synthetic profiles exercise 30 encoding/container combinations, including
  out-of-order multipart JPEG and both TIFF byte orders/offset widths.
- Optional bundled ExifTool comparison checks 20 synthetic descriptions,
  including Unicode, whitespace, absent en-US records and language-code case.
- Three locally installed profiles are compared with ExifTool when available;
  those binary profiles are not copied into the repository.
- CLI tests exercise automatic PQ, preset/batch rendering, explicit SDR/input
  overrides, first-selected-image filtering, source preservation, and existing
  output preservation on corrupt ICC metadata.
- Truncated metadata, invalid lengths/offsets/chunks/CRC, a compressed profile
  expanding beyond 32 MiB, directory cycles/depth limits, and fuzz inputs
  exercise parser bounds.

Input pixels must already use the intended gamut/transfer. This does not add
ICC color conversion, arbitrary transfer selection, camera RAW HDR development,
or general PQ-to-SDR tone mapping. Recognition of an ICC label does not prove
pixel equivalence for arbitrary profiled camera images.

```powershell
$env:LRTL_ICC_EXIFTOOL='C:\Program Files\LRTimelapse 7\exiftool.exe'
go test ./internal/lrtl -run '^TestICC' -count=1
go test ./internal/lrtl -run '^$' -fuzz '^FuzzICCDescription$' -fuzztime 10s
go test ./internal/lrtl -run '^$' -fuzz '^FuzzICCContainers$' -fuzztime 10s
```
