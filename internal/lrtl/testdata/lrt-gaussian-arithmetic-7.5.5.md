# Native Gaussian arithmetic and CPU dispatch

The native SDR working-color path now selects fused Gaussian feedback
arithmetic when the host supports AVX2 and FMA. CPU detection uses the existing
pure Go `golang.org/x/sys/cpu` dependency. Go evaluates each feedback product
and sum with `math.FMA` on the float32 operands and rounds the result to
float32 once. Boundary scaling and postscale retain their measured separate
float32 operations. Other CPU configurations keep the scalar convention.
The standard/RGB fallback path retains its previous scalar arithmetic.

The float64-to-float32 step includes midpoint correction. A float64 FMA can
land exactly on a float32 midpoint after losing a smaller residual; a plain
cast would then double-round. The float32 product is exact in float64, so a
TwoSum residual is recovered for midpoint candidates and selects the correct
neighbor. Regression vectors include positive/negative midpoint residuals,
subnormal ties and overflow. The ordinary Gaussian path still uses the fast
conversion when no midpoint correction is needed.

This applies to sigma .5 in level 4 denoise's 8-bit preclean branch and sigma
15 in level 2/3 airplane removal. It changes neither frame scheduling nor
the filters' thresholds. On the measured 11th Gen Intel Core i7-11700 Windows
host, the previously documented one-component/two-code difference in the
strongest 64x32 combined temporal fixture is eliminated. The Go output now
matches the installed bundled FFmpeg's normal CPU path exactly. The old
scalar value is retained as a separate reference, not replaced with the Go
result or accepted via a tolerance.

## Evidence

The feedback arithmetic and native dispatch were inspected in the official
FFmpeg n8.0.1 [Gaussian implementation](https://github.com/FFmpeg/FFmpeg/blob/n8.0.1/libavfilter/vf_gblur.c),
[x86 kernels](https://github.com/FFmpeg/FFmpeg/blob/n8.0.1/libavfilter/x86/vf_gblur.asm)
and [CPU setup](https://github.com/FFmpeg/FFmpeg/blob/n8.0.1/libavfilter/x86/vf_gblur_init.c).

`lrt-gaussian-arithmetic-8.0.1.json` contains 192 byte counts and SHA-256 hashes
from the installed bundled FFmpeg 8.0.1: eight dimensions from 2x2 to 127x65,
8/10-bit planes, sigma .5/15, noise/ramp/impulse inputs, and scalar/native CPU
profiles. Gaussian kernel measurements use one filter thread to isolate
arithmetic from the small-frame native dispatch issue below. The regular
suite calculates both arithmetic variants and checks all saved hashes on any
host, without requiring an external binary. Live tests repeat the independent
FFmpeg measurements when `LRTL_COLOR_FFMPEG` is supplied.

The temporal, playback and low-speed working-color suites select the expected
CPU profile from the detected capabilities. Playback and slow-color datasets
now contain both their original 22 scalar rows and 22 independently measured
normal-CPU rows. The 24-row temporal dataset already contained both profiles.
All 12 temporal, 22 playback and 22 slow paths require exact selected-profile
matches. Real video comparisons also use that selected FFmpeg CPU profile,
including decoded samples and color/rate/frame-count metadata.

## Small-frame threading scope

Default-CPU small-frame experiments showed a separate issue, not an FMA
rounding difference. The native postscale dispatcher divides height among
up to `min(width*height, thread_count)` jobs; some jobs can have zero height.
The selected SIMD postscale kernel still executes an initial vector block
when its length is zero. Such jobs can overlap another postscale job and
apply scaling again. On this 16-logical-processor host, an 8x8 sigma-15
example differed in 16 components; one filter thread removed the issue.

Go implements the stable Gaussian operation and does not emulate that
overlapping native work. The opt-in diagnostic checks the single-thread
oracle and logs the default-thread discrepancy without treating a particular
overlap result as a deterministic reference. This does not certify every CPU,
native threading configuration or tiny-frame application render. Existing
scene-change SIMD, geometry, ICC/HDR and overlay/fade gaps also remain.

```powershell
$env:LRTL_COLOR_FFMPEG='C:\Program Files\LRTimelapse 7\ffmpeg.exe'
go test ./internal/lrtl -run '^(TestNativeGaussian|TestNativeTemporal)' -count=1 -v
go test ./internal/lrtl -run '^$' -fuzz '^FuzzNativeGaussianArithmetic$' -fuzztime 10s
```

The fallback is separately checked by disabling AVX2/FMA for a Go test process
with `GODEBUG=cpu.avx2=off,cpu.fma=off`; the original scalar pipeline hashes
must still match. No native library, CGo or proprietary source is required.
