# Grayscale PNG/TIFF native input

The even-sized native working-plane path now keeps true grayscale PNG/TIFF
samples in luma instead of converting through RGB. The old round trip could
change luma and produced chroma 511 where native grayscale input gives 512.
The implementation remains pure Go and adds no dependencies.

## Conversion and geometry

- Gray8 identity conversion uses bit replication from 8 to 10 bits, with
  neutral Cb/Cr 512. It shares the existing gray JPEG conversion.
- Gray16 identity conversion uses the measured full-range reduction and an
  ordered 8×8 dither. Its pattern can be expressed as a permutation/XOR of
  the six coordinate bits. Full-range normalization happens before the
  six-bit reduction. Simple truncation or half-up rounding does not match.
- Resized Gray8 uses the existing byte-plane scaler. Gray16 uses 14-bit
  horizontal coefficients, 15-bit intermediates and 12-bit vertical
  coefficients, followed by 10-bit rounding/clipping. Neither route inserts
  an RGB matrix or an unnecessary limited-range conversion.
- Identity and resized paths intentionally have different native rounding
  conventions. Identity crops retain the full-frame dither phase.
- Cropping the odd source edge preserves Gray/Gray16 storage, stride and bit
  depth. Native preset geometry scales before transposing the YUV444 working
  samples. Only the final requested region is calculated for resized crops;
  a bounded row window supports the vertical filter.

The public algorithm reference is FFmpeg 8.0.1's
[unscaled planar conversion](https://github.com/FFmpeg/FFmpeg/blob/n8.0.1/libswscale/swscale_unscaled.c).
The native geometry order and scale quantization have separate
[measurements](lrt-native-scale-8.0.1.md). No application binaries, proprietary
code, camera images or installed settings are included.

## Evidence

`lrt-gray-input-8.0.1.json` contains 72 planar byte counts and SHA-256
observations from the installed FFmpeg 8.0.1, built from synthetic inputs:

- 8 identity conversions: PNG/TIFF × Gray8/Gray16 × normal/scalar CPU.
  Every one of the 256 or 65,536 source values appears at every position
  of the 8×8 dither period. Gray16 therefore covers 4,194,304 source pixels
  per input image, rather than one dither position per value.
- 64 conversions: the same format/depth/CPU combinations, eight target
  shapes from a 64×48 patterned source. These include identity, horizontal
  or vertical changes, enlargement and strong reduction.

Go decodes each encoded fixture before conversion; the test checks that the
decoder retained its Gray/Gray16 sample type. The saved FFmpeg hashes also
run without the external executable. On non-SSE2 systems, the normal-profile
offline test selects the corresponding scalar observation. Live FFmpeg
comparisons additionally compare every sample, not only hashes.

**192 actual videos** compare decoded planes and ffprobe size/count/rate/color
fields. Four PNG/TIFF/depth combinations and orientations 1/6 cover identity
geometry, then resized square crops at positions 0/50/100 with luma/chroma
deflicker, noise reduction 4, airplane removal 3, sharpening and CAS. Each
condition runs H.264 4:2:0, H.265 4:4:4 and ProRes HQ 4:2:2 at full/limited
range. EXIF metadata is embedded in the synthetic sources, parsed separately,
and source files are checked for preservation. References use the separately
observed native temporal helpers and recorded geometry order; these are not
claims of complete Camera Raw or licensed application pipeline equivalence.

Further tests cover a measured gray16=12345 constant, neutral chroma,
nonzero image bounds, invalid/oversized images and invalid regions. The
large-canvas regression now expects the measured grayscale chroma 512.
The RGB48 standard-output precision assertion remains separately covered.
Corrupt future Gray16 PNG/TIFF frames retain valid headers so the renderer
reaches pixel decoding after preparation. Failure preserves an existing
destination, cleans staged video/subtitles and leaves all source bytes intact.
Region fuzzing checks Gray8/Gray16, all eight orientations, source preservation,
10-bit sample limits and equivalence with full-scale-then-crop. A 10-second
run completed 46,761 executions. Targeted race and Go 1.23 checks passed.

On 2026-10-21 the full suite with installed native tools passed in 663.027s.
After adding the extra source-size guard and future-frame failure regressions,
focused tests and race/Go 1.23 checks passed again. The three grayscale
conversion/scaling functions have 100% statement coverage in that focused
run; this is not an overall project coverage percentage. `go vet ./...`,
the native `CGO_ENABLED=0` CLI build and a Linux ARM64 cross-build also passed.

## Scope

The new PNG/TIFF measurements use synthetic opaque grayscale images encoded
by Go. Low-bit-depth, alpha-gray, grayscale palettes, WhiteIsZero TIFF,
floating-point TIFF, every TIFF compression/photometric variant, full ICC
conversion and RAW grayscale development are not established by these tests.
Go XMP pixel adjustments or literal CLI orientation can still convert samples
to an RGB image before the native stage; native preset rotation without those
adjustments preserves the measured grayscale path. Standard/AVI/image output
retains its existing processing. Cross-products with every overlay, codec,
HDR mode, resolution and interpolation setting remain unverified.

```powershell
# Independent saved observations, without FFmpeg
go test ./internal/lrtl -run '^TestNativeGray|^FuzzNativeGrayRegion$' -count=1

# Live reference comparisons, including 192 videos
$env:LRTL_COLOR_FFMPEG='C:\Program Files\LRTimelapse 7\ffmpeg.exe'
go test ./internal/lrtl -run '^TestNativeGray' -count=1 -timeout 10m
go test ./internal/lrtl -run '^$' -fuzz '^FuzzNativeGrayRegion$' -fuzztime 10s -parallel 2
```

To regenerate observations, set `LRTL_RECORD_GRAY` to an output JSON path
and run `TestNativeGrayInputPlanes` with the bundled executable. The test
records FFmpeg results only after sample comparisons pass.
