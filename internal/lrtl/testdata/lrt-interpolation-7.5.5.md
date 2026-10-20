# Playback interpolation: LRTimelapse 7.5.5

The installed 7.5.5 build 1059 static playback helper was invoked for three
fps labels (24, 29.97, 23.976), twelve float speeds and modes 0/1/2. All 108
returned filter strings are recorded in `lrt-interpolation-filter-7.5.5.json`.
Three additional returned graphs in `lrt-interpolation-rounding-7.5.5.json`
check Java's half-up decimal formatting (47.8125 becomes 47.813, not 47.812).
Those unusual labels also expose native NTSC label mapping. The broader
[rate-label comparison](lrt-rate-label-7.5.5.md) now records 156 labels and
282 complete playback graphs. Standalone CLI decimals retain their literal
semantics; the native mapping is applied when loading a stored preset fps.
The caller uses this helper when speed differs from float32 1, and normally
resets timestamps without interpolation at speed 1. The complete caller gates
nonzero modes; no activation state or license checks were modified. Direct
helper observations do not establish a gated full application render.

Below speed 1, mode 1 generates `minterpolate=mi_mode=blend`, using a target fps
computed by float32 division of the displayed label by speed and formatted to
three decimals. Mode 2 instead specifies bidirectional adaptive overlapped
motion compensation with variable-size blocks. That slow mode remains
unimplemented. At speeds above 1, both modes use a uniform tmix window equal
to Java round(speed), when that integer exceeds 1. Thus speeds in (1,1.5)
only resample timestamps, and mode 2 at speeds >=1 is supported. Uniform tmix
replicates the first image and rounds integer averages half upward, separately
from Motion Blur's weighted history after resampling.

The bundled FFmpeg 8.0.1 only accepts 8-bit minterpolate input. The Go slow
path quantizes YUV10 using the measured 2x2 dither, blends with a 0..1024
integer coefficient and half-up rounding, then restores 10-bit codes by *4.
Scene detection compares luma mean absolute frame differences and their change
against a threshold of 10. A detected scene change chooses the right image
only above coefficient 512; exact endpoints copy that endpoint.

Only intervals through source N-2 can be generated; source N-1 supplies the
lookahead (indices start at zero). The fps stage holds the final generated
image until the rescaled original EOF. The intermediate fps rational, nearest
rescaling of EOF, setpts truncation and fps nearest rounding all matter. For
example N=3, 24 fps, speed=float32(.7) outputs three frames, versus four without
interpolation. The Go clock preserves rational rescaling and float64 reciprocal
setpts arithmetic. With light-trail removal it uses actual median frames for
interpolation and the original sequence's EOF. Native preset audio/fade still
uses int(float32(N)/speed), independently of the actual frame count.

Two reproducible synthetic output-hash datasets are distributed:

| Dataset | Cases | Plane samples | Scope |
| --- | ---: | ---: | --- |
| `lrt-interpolation-8.0.1.json` | 1,728 | 72,072,516 | complete mode-1 helper graphs, 8-bit in/out |
| `lrt-interpolation-10bit-8.0.1.json` | 576 | 24,450,744 | complete mode-1 graphs and implicit bit-depth conversion, native outer swscale flags |

Both cover 64x32 native CPU dispatch and 17x13 with cpuflags=0, all three
planes, ramps, a flash/cut, constant images and a moving point. The 8-bit
dataset uses lengths 1/2/3/4/8/17; 10-bit uses lengths 3/8 with varying low
bits. Speeds are .0625/.3/.5/.7/.9/1/1.1/1.5/2/2.3/4/16, at the three fps
labels above. Every output sample is compared through SHA-256, plus output
counts; 8-bit references also store first-Y values. Input formulas are in
`interpolationFixture`/`interpolationNativeInput` in the test source. No
proprietary frame data is included.

Additional probes at native widths 16 and 32 found SIMD scene decisions
different from cpuflags=0 for the same scalar luma differences; at 64x32 the
recorded native results match the scalar model. Those small-image CPU
artifacts are not imitated in Go. Other CPU dispatch/dimensions and arbitrary
motion scenes are not proved by this dataset.

Streaming tests verify bounded caches, single input reads, returned buffer
ownership and errors. CLI tests cover presets/explicit overrides/batch,
SRT count and source association, denoise/light-trail/speed/Motion Blur order,
JPEG AVI, still-export isolation, failed future or dropped-tail decoding and
existing-output retention. The combined temporal-buffer estimate is capped
at 768 MiB before creating output. A real-video matrix uses three fps labels,
speeds .5/.7/2.3 and H.264 MP4/H.265 MOV (18 cases). Each decoded Y/U/V sample
must equal a separately encoded offline RGB reference, while metadata checks
count, duration, pixel format and fps. Approximate mathematical BT.709 luma
differences are logged diagnostically; CRF 0 is not assumed lossless.

RGB16/YUV10 conversion, RAW development and color management are independent
Go approximations; transformed deltas retain the source RGB residual. These
tests do not claim full application video pixel parity, HDR or slow motion
compensation. Sources were checked against official FFmpeg
[minterpolate](https://github.com/FFmpeg/FFmpeg/blob/n8.0.1/libavfilter/vf_minterpolate.c),
[fps](https://github.com/FFmpeg/FFmpeg/blob/n8.0.1/libavfilter/vf_fps.c) and
[tmix](https://github.com/FFmpeg/FFmpeg/blob/n8.0.1/libavfilter/vf_mix.c) implementations.

Re-run the stored helper graphs using the locally installed binary:

```powershell
$env:LRTL_INTERPOLATION_FFMPEG='C:\Program Files\LRTimelapse 7\ffmpeg.exe'
go test ./internal/lrtl -run '^TestInterpolationLiveReferences$' -count=1
```
