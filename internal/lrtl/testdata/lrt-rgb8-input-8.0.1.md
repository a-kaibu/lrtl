# RGB8 native working-plane conversion

The native image-input pipeline does not first expand RGB8 samples to RGB16.
Its RGB24/RGBA8 conversion rounds a limited-range matrix at 14-bit precision,
then doubles that intermediate to 15 bits before expanding to full-range YUV10.
The RGB48 path has a different initial quantization boundary. Expanding RGB8
channels by 257 before using RGB48 conversion therefore changes output codes.

The pure Go renderer now selects the RGB8 quantizer for opaque RGB8 PNG/TIFF
images in the even-sized native planar output path (`--output-color lrt-7.5.5`).
Native temporal processing receives its working YUV samples directly;
native spatial/plain output also stays planar through encoding. The selection
uses image representation and format, not whether RGB16 values happen to be
multiples of 257. RGB16 conversion remains unchanged. Standard output and the
existing RGB overlay paths retain their prior behavior.

## Evidence

The input conversion was isolated with the installed LRT 7.5.5 FFmpeg 8.0.1:

```text
raw RGB24 → scale=iw:ih:out_range=full:flags=lanczos → format=yuv444p10le
```

The integer pipeline was checked against that executable with normal CPU
selection and `-cpuflags 0`. The versioned FFmpeg swscale input/range/output
arithmetic was inspected locally to identify the intermediate precision; no
native application bytecode or licensing state was changed.

- All **16,777,216 RGB8 triplets**, represented once each in a 4096×4096 raw
  image, compare exactly against both FFmpeg CPU selections.
- Twelve externally generated SHA-256 references are stored in
  `lrt-rgb8-conversion-8.0.1.json`: channel/gray ramps, random and grid patterns,
  two CPU selections. These tests run without an installed FFmpeg.
- Seven frame shapes from 1×1 to 128×4 check raw RGB24 and opaque RGBA8.
- Validation tests cover incomplete pixels, invalid dimensions, source
  retention, precision selection and the existing non-opaque/JPEG fallback.
- Fuzz tests exercise dimensions and sample bounds without external encoders.

72 actual videos compare decoded samples and ffprobe dimensions, frame counts,
rates, pixel formats, primaries, transfer, matrix and range:

- 36: PNG/TIFF, plain output, sharpen/CAS, and strongest combined temporal/
  spatial processing, H.264/H.265/ProRes HQ, full/limited range;
- 18: slow blend, fast motion interpolation, and slow frame duplication,
  including subsequent native Motion Blur and spatial filters;
- 18: RGB8 PNG input through the three directly recorded native watermark/
  timestamp/fade caller graphs at 640×320, the same codecs and ranges.

The previously observed 4005/6144 differing working samples in a 64×32 RGB8
fixture, up to four 10-bit codes, came from RGB8-to-RGB16 expansion. The new
RGB8 conversion removes that source of error.

## Remaining scope

Native JPEG decoding is now covered separately in
[JPEG input measurements](lrt-jpeg-input-8.0.1.md); the RGB8 tests here remain
specific to the RGB input quantizer.

These measurements establish the unscaled, opaque truecolor RGB8 conversion
and the listed processing combinations. Separate measurements now cover
[JPEG input](lrt-jpeg-input-8.0.1.md),
[fixed-point scaling](lrt-native-scale-8.0.1.md), and
[Gray8/Gray16 PNG/TIFF](lrt-gray-input-8.0.1.md). Palette/alpha equivalence,
odd-output encoding, Camera Raw development and complete ICC conversion
remain unestablished. Standard Go overlays retain their existing RGB
processing; use native overlay styles for the measured planar path.

```powershell
$env:LRTL_COLOR_FFMPEG='C:\Program Files\LRTimelapse 7\ffmpeg.exe'
go test ./internal/lrtl -run '^TestNativeRGB8' -count=1
go test ./internal/lrtl -run '^$' -fuzz '^FuzzNativeRGB8Planes$' -fuzztime 10s
```
