# Bidirectional motion interpolation (LRTimelapse 7.5.5)

The installed static playback helper's mode-2 slow graph is recorded in the
existing 108-entry `lrt-interpolation-filter-7.5.5.json`. It specifies
`mi_mode=mci:mc_mode=aobmc:me_mode=bidir:vsbmc=1`, with float32/three-decimal
intermediate fps, setpts reciprocal speed and fps conversion. No activation
state or license checks were changed; this is a direct helper observation,
not a gated full application render.

The bundled FFmpeg bidirectional branch actually uses fixed 16x16 blocks and
32x32 overlap support. The adaptive and variable-size code belongs to its
separate bilateral branch. Source inspection and three native option-alias
comparisons (pan-x, five frames, speed .5, across all three dimensions) confirm
that changing aobmc/vsbmc does not change this selected bidirectional output.

The Go estimator independently implements the numerical rules:

- two vector directions per block, with current left/top/top-right median,
  collocated previous vectors, acceleration from two earlier fields and the
  previous field's four spatial neighbors;
- strict lower-cost selection and repeated one-pixel diamond refinement,
  within 32-pixel search bounds and the complete-block grid;
- 32x32 luma difference cost with clamped overlap windows and a 64-code
  Manhattan penalty relative to the predicted vector;
- both backward and forward block fields shifted toward the intermediate
  time, with integer truncation, image-edge clamping and 0..1024 phase;
- a quantized separable tent generating the 32x32 overlap weights, rather
  than embedding a foreign lookup table;
- at most 32 pixel references in deterministic block/direction order,
  half-up normalized integer sums, and blend fallback on uncovered edges;
- original endpoint copies and the existing scalar scene-cut decision.

Candidate cost memoization and noncompetitive row early exits preserve tie
order. Pixel contributions accumulate directly into integer plane sums; the
implementation does not allocate the native per-pixel list of 32 vectors.
Only three RGB/plane inputs and two completed vector fields are needed at a
time. Source reads are sequential, including otherwise unused future/tail
images. Invalid dimensions and combined temporal-buffer estimates above
768 MiB are rejected before creating an output.

| Native dataset | Cases | Output plane samples |
| --- | ---: | ---: |
| `lrt-motion-interpolation-8.0.1.json` | 432 | 57,117,456 |
| `lrt-motion-interpolation-10bit-8.0.1.json` | 288 | 38,967,984 |

All 96,085,440 samples are checked through complete-output SHA-256 and frame
count. Input formulas are in `motionInterpolationFixture` and
`motionInterpolationInput` in the tests. The observations use 64x64 and
80x48 with native CPU dispatch, plus 65x49 with cpuflags=0; fps 24/29.97;
speeds .3/.5/.7; lengths 3/5/8 for 8-bit and 3/8 for 10-bit. Patterns are
static texture, horizontal/vertical/diagonal pan, acceleration, a moving
rectangle, changing noise and a cut. The 10-bit matrix includes varying low
bits, the implicit 10->8->10 conversion and native outer swscale flags.
468 cases differ from plain blend, including moving-texture cases.

Tests also compare a separate per-pixel gather implementation with the Go
block-scatter overlap across five phases, including the 32-reference cap and
uncovered last rows/columns. Fuzzing checks dimensions, search bounds and
input ownership. Streaming tests compare an offline full-sequence reference,
single source reads and bounded caches. CLI tests cover presets, explicit
disabling, batch, SRT, denoise/light-trail/blur order, the full output-filter
chain, JPEG AVI payloads, small-dimension failures and future-input retention.
Eighteen real H.264 MP4/H.265 MOV cases at three fps labels and three speeds
compare decoded Y/U/V against a separately encoded offline RGB reference,
plus rate, count and duration. Still exports do not interpolate.

Minimum output dimensions are 32x32 and at least three frames must remain
after median light-trail filtering. Small-image SIMD scene decisions can
differ from the scalar model as documented in the blend observations. Other
CPU dispatch/dimensions, all motion patterns, HDR and complete native color
management are not proved by these comparisons. The RGB16/YUV conversion
remains an independent approximation, so these exact plane observations do
not claim full application video pixel identity.

The algorithm and branch selection were cross-checked against official
[minterpolate](https://github.com/FFmpeg/FFmpeg/blob/n8.0.1/libavfilter/vf_minterpolate.c)
and [motion estimation](https://github.com/FFmpeg/FFmpeg/blob/n8.0.1/libavfilter/motion_estimation.c)
sources. Only synthetic numerical observations and independent Go code are
distributed; installed classes, decompilation and probes remain untracked.

Re-run the stored native graphs and option-alias checks:

```powershell
$env:LRTL_MOTION_FFMPEG='C:\Program Files\LRTimelapse 7\ffmpeg.exe'
go test ./internal/lrtl -run '^TestMotionInterpolationLiveReferences$' -count=1
```
