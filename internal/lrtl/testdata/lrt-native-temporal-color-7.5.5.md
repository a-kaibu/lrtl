# Native SDR temporal YUV path

The native SDR path now converts RGB16 to full-range YUV444 10-bit before
output deflicker, denoise and airplane/light-trail removal. Filtered components
are passed directly to subsequent filters and the final native spatial/output
stage. Interleaved working samples retain the six-byte pixel layout for
bounded source queues and cropping; only the encoder input is planar.

This path applies to `--output-color lrt-7.5.5` (the saved preset default),
with one of these temporal filters, even output dimensions and no timestamp,
watermark or fade. The original measurements below disabled interpolation
and Motion Blur. The subsequent [playback extension](lrt-native-playback-color-7.5.5.md)
adds Motion Blur and modes 1/2 at speeds >= 1. The later
[slow interpolation path](lrt-native-slow-color-7.5.5.md) adds the native
8-bit transition for lower speeds. Mode 0 frame duplication retains the
existing timeline and native working color.

Level 4 denoise negotiates an 8-bit removegrain/Gaussian branch. With full-range
metadata, luma depth reduction subtracts the replicated high bits before
the measured dither, and expansion replicates high bits into the low bits.
Chroma keeps the shift-only convention. The prior raw-plane references had
unspecified range, so did not expose this distinction. The legacy RGB path
retains that previous untagged convention. See the official FFmpeg n8.0.1
[unscaled depth conversion](https://github.com/FFmpeg/FFmpeg/blob/n8.0.1/libswscale/swscale_unscaled.c).

## Measurements

`lrt-native-temporal-color-8.0.1.json` records full-output byte counts and
SHA-256 values for 12 deterministic RGB16 PNG sequences, with native CPU and
`-cpuflags 0` (24 observations). Every sequence has 17 opaque 64x32 frames;
the formula is in `nativeTemporalFixture`. Cases include denoise levels 1–4,
airplane levels 1–3, luma/chroma/both deflicker, and two combinations also
using unsharp and CAS. The reference reads the original PNG files with the
installed bundled FFmpeg 8.0.1 and runs the separately recorded installed
LRT 7.5.5 helper graphs. No license state was modified; these are helper-graph
comparisons, not a complete licensed application render.

The regular suite compares all 12 Go outputs with the saved scalar hashes,
without requiring FFmpeg. The opt-in live test compares every sample against
both CPU variants. All scalar cases match exactly. Native CPU has exactly
one differing component (delta 2) in the strongest combined fixture due to
the known Gaussian SIMD rounding boundary. The live test pins the native
reference hash and asserts that precise exception; it does not treat the
case as bit-identical. Other CPU/build combinations are not certified.

A staged comparison isolates this fixture difference: after deflicker and
level 4 denoise the CPU outputs are equal; after airplane removal there is
one component differing by one code; after unsharp/CAS it differs by two.
The live test asserts each stage, rather than suppressing a general tolerance.

72 real video cases compare the Go renderer against the independently
constructed scalar reference graph: 12 cases × H.264 420 8-bit / H.265 444
10-bit / ProRes HQ 422 10-bit × full/limited. All decoded YUV bytes and ffprobe
dimensions, fps, frame count, pixel format and color metadata must match.
The scalar reference choice is explicit because of the SIMD difference above.

Three additional full-plane comparisons cover left/center/right square
cropping after deflicker measurement, using saved native presets. Preset,
batch, explicit disabling, unchanged sources, bounded caches and failure
retention continue to have regression tests. Combined spatial routing checks
compare every output byte with the same temporal output followed by one
spatial pass. Fuzzing covers full-range conversion, both deflicker components,
all four denoise levels, all three airplane levels and spatial filtering.

```powershell
$env:LRTL_COLOR_FFMPEG='C:\Program Files\LRTimelapse 7\ffmpeg.exe'
go test ./internal/lrtl -run '^(TestNativeTemporal|TestOutputFrameColor)' -count=1 -v
go test ./internal/lrtl -run '^$' -fuzz '^FuzzNativeTemporalColor$' -fuzztime 10s
```

This does not reproduce native Lanczos resize, Camera Raw/ICC, all temporal
combinations, HDR/wide gamut, overlays or fade. It improves the actual working
color domain for the listed filters while leaving those broader gaps visible.
