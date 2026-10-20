# Native fps labels and shared subtitle presets (LRTimelapse 7.5.5)

The installed 7.5.5 build 1059 static fps translation helper and playback
generator were invoked for 156 labels. `lrt-rate-label-7.5.5.json` records the
input, translated rate, mode-1 half-speed graph or parse exception class.
No activation state was changed and no gated render feature was enabled.

Native translation parses the label as a Java double, rounds to the nearest
integer, and emits that integer when its absolute difference is strictly less
than .001. Otherwise it emits roundedInteger*1000/1001. This applies beyond
the usual NTSC presets: 24.5 becomes 25000/1001, and 23.90625 becomes
24000/1001. Ordinary binary double boundary behavior is preserved at .001.
Integer rounding is half upward for the valid positive-rate domain.

The dataset includes 14 integer bases with nine offsets around .001, .5 and
the next integer, standard NTSC choices, extra decimal labels, leading/trailing
ASCII whitespace, plus signs, decimal exponents, hexadecimal floats, Java f/D
suffixes, fractions, invalid text, zero, out-of-range values and non-finite
values. The Go native parser is checked against the translated rate whenever
it is a supported 1..240 rate, and rejects unsafe/invalid effective rates.
Labels are limited to 64 characters. Go-only underscores are rejected.

`lrt-rate-label-timing-8.0.1.json` contains 282 complete bundled-FFmpeg playback
observations: 141 valid numeric labels, lengths 3 and 8, native translated
input/output fps, the directly returned half-speed blend graph, synthetic
64x32 8-bit Y/U/V planes. Tests compare every output sample through SHA-256,
the output count and first-Y identifiers. The independent Go intermediate
clock uses the original normalized label for float32 intermediate-fps
calculation, while video timestamps use the translated rational rate.

`lrt-rate-label-audio-7.5.5.json` records five complete native graph-builder
observations for 15 frames, speed 1, fade 1 second and audio enabled. These
ordinary timing operations were obtained without modifying feature gates.
Audio/fade use the original label's float32 value, independently of translated
video fps. Labels 3.4 and 3.5 demonstrate audio ending before and after video;
24.5, 23.90625 and 24.0009 also exercise shortened fades. Tests compare exact
native trim/fade parameters, then run real H.264/AAC MP4s and check video rate,
frame count, video/audio durations, decoded video fade and audio RMS fade.

Stored preset rates activate this translation. Explicit CLI --fps overrides
keep the existing exact decimal/fraction semantics. Standalone --fps 24.5
therefore remains 49/2. Fractions in stored presets remain an lrtl extension:
the native translation helper passes them through, but the native playback
generator rejects them at Float.parseFloat. The Go extension uses the exact
rational as its numeric fallback. Full arbitrary Java numeric syntax outside
the bounded tested domain is not claimed.

The subtitles CLI now loads standalone presets, lastRenderSettings or named
renderPresets. It shares native fps, original-label interpolation, speed,
Motion Blur and light-trail scheduling with video. Explicit flags override the
stored values. Pixel filters and audio do not alter subtitle scheduling.
Tests cover direct/named settings, per-option overrides, batch video equality,
source/preset retention, invalid settings/names/duplicates, malformed fps and
existing-output/temporary-file retention. Fuzzing checks parser invariants.

This establishes rate translation and the stated synthetic timing/pixel
comparisons, not complete native color management, gated motion compensation
or every native frame-rate consumer (such as the separate UI duration helper).
Only synthetic observations and independent Go code are distributed.

To repeat the playback observations using the installed binary:

```powershell
$env:LRTL_RATE_FFMPEG='C:\Program Files\LRTimelapse 7\ffmpeg.exe'
go test ./internal/lrtl -run '^TestNativeFrameRateLiveTimelines$' -count=1
```
