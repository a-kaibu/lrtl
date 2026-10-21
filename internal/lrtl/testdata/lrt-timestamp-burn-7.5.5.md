# Native SDR timestamp burn-in

`render --timestamp-style lrt-7.5.5` selects the measured native subtitle
style. It enables timestamps, requires `--output-color lrt-7.5.5` (or the
native color default of a saved render preset), even dimensions, MP4/MOV and
no fixed-text watermark. `--timestamp-every N` selects the native source-frame
interval; -1 uses the native timestamp fps. Existing Go font rendering remains
the default `standard` timestamp style. Other profile restrictions are errors
instead of silently changing the selected style.

## Direct observations

`lrt-timestamp-style-7.5.5.json` records three full command graphs returned by
the installed 7.5.5 build 1059 ordinary render command builder, with synthetic
paths and 17 64x32 frames: plain, sharpen, and Motion Blur plus sharpen. The
probe did not modify native code or licensing state. Only the command output
data is committed; local probes and inspected code remain excluded.

These command graphs specify Arial.ttf, font size 16 and primary color
`&He0e0e0`, through the FFmpeg subtitles/libass filter. They also prove that
final zscale range/color conversion occurs before subtitles, that subtitles
precede trim, and that fade occurs after trim. An earlier fade implementation
and its constructed reference graphs had placed fade before final color
conversion. This observation corrected both the implementation and those
pipeline references. Isolated fade kernel measurements were not evidence for
the complete ordering; they remain separate numerical reference tests.

The Go path performs source decoding, metadata handling, native SRT generation,
supported temporal/interpolation/spatial processing and buffering as before.
The existing external FFmpeg encoder then performs final color/subsampling,
libass subtitles and fade. No CGo library or additional native runtime is
added. The selected FFmpeg build must provide subtitles/libass and the fonts.

Go has already trimmed the input frames by the time they reach the encoder.
The encoder temporarily advances their PTS by the head trim count for SRT
evaluation, then restores zero-based PTS before fade. This preserves the
native subtitle clock before trim without adding discarded image frames.
Both 10-bit working paths and 8-bit low-speed interpolation paths are covered.

## Verification

66 actual video cases compare every decoded sample and the ffprobe color,
size, frame-rate and frame-count metadata:

- 18 cases run the three directly recorded caller graphs, substituting only
  the synthetic subtitle basename and the requested output range;
- 48 cases cover plain/sharpen/CAS, strong combined deflicker/denoise/airplane
  processing, mode 1/2 at .5 and 1.5 speed with Motion Blur, and 320x180 text;
- each path uses H.264, H.265 and ProRes HQ, both full and limited ranges.

The previous 30 native fade-only videos now use the corrected post-color
order. Raw-plane capture tests verify that Go does not fade or burn text before
encoding. The five recorded fractional/native-fps audio/video cases retain
exact decoded-video comparisons and their audio timing/RMS checks.

Additional tests cover presets, batch output, relative audio and video paths,
spaces, apostrophes, commas, square brackets and Japanese characters in output
directories. The encoder's working directory is the temporary SRT directory;
the filter references only a generated ASCII basename. Input/output/audio
paths are resolved before changing that child process directory. Go's process
working directory is unchanged. Temporary SRTs persist until the encoder has
closed or aborted, then are removed. Invalid configuration, missing metadata
and corrupt final input frames must retain the existing destination.

The SRT clock retains the 1,344 independently measured native timecodes.
Metadata precedence, all-frames-required behavior, timezone handling and Go
date layouts still have the scope described in `lrt-timestamp-clock-7.5.5.md`.
Font selection and glyph rasterization depend on the selected FFmpeg build
and installed fonts. These are helper-graph and decoded-video comparisons on
the measured Windows host, not full licensed-application renders on every OS.
Fixed-text native watermark, ICC/HDR and geometry fidelity remain separate work.

```powershell
lrtl render C:\photos\sequence --output C:\photos\dated.mp4 --output-color lrt-7.5.5 --timestamp-style lrt-7.5.5 --timestamp-every -1
go test ./internal/lrtl -run '^TestNativeTimestampBurn' -count=1 -v
go test ./internal/lrtl -run '^$' -fuzz '^FuzzNativeTimestampBurnFilter$' -fuzztime 10s
```
