# Native fixed-point scaling and orientation

The even-sized `lrt-7.5.5` working-plane path now combines color conversion
with separable integer Lanczos scaling. Native preset geometry with no Go
XMP pixel adjustments also follows the observed order: discard the odd
source edge, scale, convert to YUV444P10, transpose, then select the final
crop. Standard output, AVI/image export and Go XMP geometry retain their
existing image-space behavior.

## Implementation

- RGB16 first becomes limited-range 16-bit matrix samples; RGB8 retains its
  measured 14-bit matrix precision. Horizontal coefficients use 14 bits,
  horizontal intermediates retain 15 bits, range expansion follows that pass,
  and 12-bit vertical coefficients produce the final 10-bit samples.
- At half-width or smaller, RGB chroma uses the native paired-input path.
  RGB16 rounds the channel average before the matrix. RGB8 sums the channel
  pair and retains an extra matrix bit. Averaging already converted chroma
  or resizing RGB first gives different results.
- JPEG Y/Cb/Cr stays planar at the decoded sampling resolutions. Gray JPEG
  uses the same byte-plane scaler with neutral chroma. Identity geometry
  retains the previously measured unscaled copy conventions.
- Kernels reproduce fixed-point centers/distances, Lanczos radius 3,
  near-zero reduction, border accumulation, and error-carry coefficient
  normalization. Horizontal/vertical alignment is 4/2 on SSE2 and 1/1 on the
  scalar path. The chroma-only upsampler shares the kernel generator.
- Output crops map back through the transpose to scale coordinates before
  calculating pixels. This preserves full-canvas phase. Horizontal rows are
  retained only while the vertical support needs them; no complete scaled
  image is required for a small region. Deflicker still measures the full
  canvas before the final crop.

The implementation is pure Go. Algorithm references include the official
[FFmpeg 8.0.1 scaler](https://github.com/FFmpeg/FFmpeg/blob/n8.0.1/libswscale/utils.c),
[RGB input conversion](https://github.com/FFmpeg/FFmpeg/blob/n8.0.1/libswscale/input.c),
and [output rounding](https://github.com/FFmpeg/FFmpeg/blob/n8.0.1/libswscale/output.c).
No proprietary implementation, executable or installer is distributed.

## Independent observations and regression tests

`lrt-scale-caller-7.5.5.json` contains 48 complete filter strings returned by
the installed LRT 7.5.5 build 1059 render helper through Java reflection:

- 129×97 and 128×96 synthetic source dimensions;
- EXIF orientations 1 through 8;
- downscale, upscale plus sharpening, and square crop plus Motion Blur 3
  and sharpening;
- 17 frames at 24 fps, native BT.709 limited output.

The raw geometry test runs these prefixes on synthetic opaque RGB8 PNG,
RGB16 PNG and JPEG: **144 conditions**, including odd-edge removal. Fixture
orientation metadata is independently parsed, and FFmpeg automatic rotation
is disabled because the observed graph supplies the transpose explicitly.
`lrt-scale-working-8.0.1.json` stores planar byte counts and SHA-256 results
from the bundled FFmpeg 8.0.1 under normal CPU and `-cpuflags 0`. These
observations also support tests without FFmpeg installed. They are generated
from FFmpeg results, rather than from the implementation under test.

Separate raw tests cover seven target shapes for RGB8, RGB16, JPEG 4:4:4,
4:2:2, progressive 4:2:0 and gray: **84 comparisons** across normal CPU and
scalar selection. The targets exercise one-axis scaling and strong reduction.

Actual video comparisons cover:

- **216 videos**: three formats, four orientations (1/2/6/7), three recorded
  modes, H.264 4:2:0 / H.265 4:4:4 / ProRes HQ 4:2:2, full/limited range;
- **108 videos**: the same formats, orientations 1/6, crop positions 0/50/100,
  output luma/chroma deflicker, noise reduction 4, airplane removal 3,
  sharpening and CAS. These references combine separately recorded native
  temporal helper filters with the recorded scaling prefix; they are not
  observations of the complete licensed application pipeline.

Decoded planes and ffprobe size/count/rate/color fields are compared. Input
files are checked for preservation. The reference encoder clears input
display rotation so JPEG EXIF metadata does not rotate the already transformed
reference video again. With the bundled x265 build, some 48-pixel-wide
portrait tests exited with Windows heap-corruption status `0xc0000374` in
both the Go and reference encoders. The portrait downscale video fixtures
therefore use width 64; 48-pixel geometry remains covered by raw-plane tests.
This is a measured external-encoder limitation, not an established match for
those failed video cases.

Additional checks cover invalid dimensions/orientation, oversized output,
bounded-region behavior and source preservation. Fuzzing compares direct
region computation with a full scaled frame followed by crop under all eight
orientations. Race and Go 1.23 compatibility checks include offline references.

On 2026-10-21, the full suite with installed native tools passed in 550.710s.
The subsequently added temporal crop and image-dispatch cases also passed
separately. Targeted race checks, Go 1.23 tests, `go vet ./...`, and a
`CGO_ENABLED=0` CLI build passed. Two 10-second region fuzz runs completed
51,347 and 59,403 executions; the second also exercised RGB16 and gray inputs.

## Scope

Additional Gray8/Gray16 PNG/TIFF input measurements are documented in
[grayscale input](lrt-gray-input-8.0.1.md).

These measurements establish the listed synthetic conditions. They do not
establish all resolutions, TIFF variants, alpha/palette PNG conversion,
CMYK/RGB/12-bit JPEG, full ICC conversion, RAW/HDR development or odd-output
encoding. Native preset rotation with Go XMP spatial/exposure/deflicker
adjustments falls back to the existing image-space order. Literal CLI geometry
without native preset semantics also keeps its existing orientation/crop order.
Interpolation, overlays and HDR have separate measurements; their entire
cross-product with arbitrary new geometry is not covered here. CPU-specific
alignment beyond SSE2/scalar needs additional native observations.

```powershell
$env:LRTL_COLOR_FFMPEG='C:\Program Files\LRTimelapse 7\ffmpeg.exe'
go test ./internal/lrtl -run '^TestNativeScale' -count=1 -timeout 15m
go test ./internal/lrtl -run '^$' -fuzz '^FuzzNativeScaleRegion$' -fuzztime 10s -parallel 2
```

To regenerate the saved working hashes from FFmpeg, set `LRTL_RECORD_SCALE`
to an output JSON path and run `TestNativeScaleRecordedGeometryPlanes` with
the bundled executable. The recording writes only after every comparison
passes. Native caller graph acquisition additionally requires the installed
JAR and Java; those binaries are not needed by the application or offline tests.
