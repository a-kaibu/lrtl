# Native SDR slow interpolation working depth

The installed native minterpolate path accepts 8-bit YUV formats. Ordinary
SDR starts as full-range YUV444 10-bit, applies output deflicker, denoise and
airplane removal, then negotiates 8-bit before low-speed blend/MCI. Motion
Blur, unsharp and CAS remain 8-bit until final output color/range conversion.

The Go native path now follows that working-depth transition, retaining YUV
samples without an RGB round trip. Full-range luma depth reduction removes
replicated high bits before the measured 2x2 dither; chroma keeps the plain
shift convention. A low-speed interpolated frame stays at codes 0..255.
Unsharp uses the native 8-bit shift (16 rather than 24), CAS uses a 511
contrast upper bound and clips at 255, Blur clips at 255, and FFmpeg receives
8-bit planar `yuv444p`. Encoder output may still be 8-bit or 10-bit,
depending on its selected format. Earlier RGB/10-bit approximations remain
available via `--output-color standard` and unsupported native combinations.

The numerical behavior was checked against official FFmpeg n8.0.1
[format negotiation](https://github.com/FFmpeg/FFmpeg/blob/n8.0.1/libavfilter/vf_minterpolate.c),
[unsharp](https://github.com/FFmpeg/FFmpeg/blob/n8.0.1/libavfilter/vf_unsharp.c),
[CAS](https://github.com/FFmpeg/FFmpeg/blob/n8.0.1/libavfilter/vf_cas.c) and
[depth conversion](https://github.com/FFmpeg/FFmpeg/blob/n8.0.1/libswscale/swscale_unscaled.c).
The implementations are pure Go and require no CGo.

## Output comparisons

`lrt-native-slow-color-8.0.1.json` contains the original 22 scalar full-output
byte counts/hashes plus 22 normal-CPU observations from bundled FFmpeg 8.0.1.
Inputs have 11 opaque RGB16 PNG frames: a 64x32 changing gradient/light point
from `nativePlaybackFixture`, or a 64x64 textured horizontal pan from
`motionImageFixture`. No native image assets or binary code are included.

Both blend and MCI are tested at .0625/.3/.5/.7/.9 speed, with separate
unsharp/CAS/both cases, moving-image cases with Blur, and two combinations
including deflicker/denoise/airplane removal/Blur/spatial filtering. Reference
graphs are independently assembled from the recorded installed LRT 7.5.5
helper graphs and original PNG inputs; they do not call Go filters.

The regular suite checks all 22 Go outputs against saved selected-CPU hashes,
without requiring FFmpeg. The live test compares every output byte and
asserts 8-bit encoder input. It also requires MCI and blend to produce
different results on the moving image sequence. 132 real video cases cover
all 22 graphs × H.264 420 8-bit / H.265 444 10-bit / ProRes HQ 422 10-bit ×
full/limited. Every decoded YUV byte and ffprobe frame count, rate, dimensions,
format and color metadata must match the independently encoded reference.

Additional tests cover native presets/batch, explicit fallback combinations,
unchanged sources, corrupt future images preserving existing video, 8-bit
packing/clipping, depth validation and the different unsharp depth behavior.
Fuzzing covers both interpolation modes, full-range conversion, bounded
frame/vector caches, one read per source, native 8-bit Blur and spatial output.

```powershell
$env:LRTL_COLOR_FFMPEG='C:\Program Files\LRTimelapse 7\ffmpeg.exe'
go test ./internal/lrtl -run '^(TestNativeSlow|TestNativeWorkingEightBit)' -count=1 -v
go test ./internal/lrtl -run '^$' -fuzz '^FuzzNativeSlowWorkingColor$' -fuzztime 10s
```

These are selected-CPU helper-graph comparisons, not complete licensed
application renders. The [Gaussian arithmetic update](lrt-gaussian-arithmetic-7.5.5.md)
selects fused feedback on AVX2/FMA hosts and scalar feedback otherwise.
CPU-dependent scene and small-frame threading differences remain.
Native geometry resampling, ICC, Camera Raw, HDR/wide gamut,
overlay/fade parity and complete LRT application output remain outside scope
of these measurements. Even output dimensions and no timestamp, watermark or
fade are required for this native working path.
