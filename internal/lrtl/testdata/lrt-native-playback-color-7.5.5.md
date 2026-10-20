# Native SDR YUV through playback and Motion Blur

The native SDR working path now retains full-range YUV444 10-bit through
Motion Blur and playback mode 1/2 at speeds >= 1, together with output
deflicker, all four denoise levels, all three airplane levels and unsharp/CAS.
Mode 0 frame duplication at speeds below 1 is also supported. Even output
dimensions and no timestamp, watermark or fade are required. Low-speed
blend/motion interpolation still uses the existing RGB path: native
minterpolate negotiates 8-bit planes, which needs a separate working-depth
implementation including downstream filters.

`weightedWorkingFrames` uses float32 unnormalized products and accumulation,
then one float32 reciprocal weight-sum multiplication, round-to-nearest-even
and 10-bit clipping. Uniform weights use the native integer average with
half-up rounding instead. Both differ from the legacy RGB16 float64 blend;
that existing behavior is retained for standard output color and fallbacks.
The independent implementation was inspected against the official FFmpeg
n8.0.1 [tmix kernel](https://github.com/FFmpeg/FFmpeg/blob/n8.0.1/libavfilter/vf_mix.c).

## Recorded output references

`lrt-native-playback-color-8.0.1.json` contains 22 byte counts and SHA-256 hashes
from the installed bundled FFmpeg 8.0.1 using `-cpuflags 0`. Each input has
41 opaque 64x32 RGB16 PNG frames; `nativePlaybackFixture` generates them with
the same published synthetic formula as the earlier temporal fixture.

Cases cover all seven Blur windows (2/3/4/5/10/15/20), modes 1 and 2 at
1.1/1.5/2/2.3/4/16 speed, two combinations with deflicker/denoise/airplane
removal/spatial filtering, and mode 0 at .5 speed with Blur. Reference graphs
use the separately measured installed playback helper JSON, independently
formatted three-decimal native blur weights and native trim bounds. The
initial RGB conversion and all filters are performed by FFmpeg in that
reference, without calling Go filtering functions. These are helper-graph
comparisons, not a complete licensed LRT application render.

The regular suite compares all 22 Go output buffers with the saved scalar
hashes without FFmpeg. The opt-in test compares every sample with live native
reference graphs. No arbitrary image tolerance is used. The earlier temporal
[SIMD discrepancy](lrt-native-temporal-color-7.5.5.md) remains outside this
scalar reference claim.

132 real videos cover all 22 cases × H.264 420 8-bit / H.265 444 10-bit /
ProRes HQ 422 10-bit × full/limited. Every decoded YUV byte and ffprobe
frame count, frame rate, dimensions, pixel format and color metadata must
match the independently encoded scalar reference. These use the same shared
video comparison helper as the 72 earlier temporal cases.

Additional tests cover saved combined presets and batch forwarding, original
source retention, invalid frame layouts/weights, uniform-vs-weighted rounding,
10-bit clipping, legacy fallback routing, corrupt dropped-tail detection and
preservation of existing output files. Fuzzing covers synthetic working-frame
windows up to 20 images. The Go converter and weighted kernel require no CGo.

```powershell
$env:LRTL_COLOR_FFMPEG='C:\Program Files\LRTimelapse 7\ffmpeg.exe'
go test ./internal/lrtl -run '^(TestNativePlayback|TestWeightedWorking)' -count=1 -v
go test ./internal/lrtl -run '^$' -fuzz '^FuzzWeightedWorkingFrames$' -fuzztime 10s
```

This does not establish full native resize, ICC, Camera Raw, HDR, low-speed
interpolation or overlay/fade parity. The native YUV path removes intermediate
RGB clipping and quantization for the listed combinations while keeping those
remaining gaps explicit.
