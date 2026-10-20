# Transient light-trail / airplane removal

The installed LRTimelapse 7.5.5 build 1059 stores `removeAirplanes` and
`removeAirplanesLevel` (default 1). Direct calls to its static helper with
LIGHT/MEDIUM/STRONG enum values produced `lrt-airplane-filter-7.5.5.json`.
The caller maps saved levels 1/2/3 to those choices. The local complete render
builder's feature gate did not enable this option during the probe. Its gates
and activation state were not changed. Consequently, caller trim/duration
rules below are from bytecode inspection, not a complete native application
render with the feature enabled.

| Level | Temporal median | Luma detail | Highlight knee/gain/cap | Chroma |
| --- | --- | --- | --- | --- |
| 1 LIGHT | 3 frames, percentile .5 | none | none | median only |
| 2 MEDIUM | 3 frames, percentile .5 | sigma 15 Gaussian, amount .20 | 918 / 2 / 1020 | clip 64..960 |
| 3 STRONG | 5 frames, percentile .5 | sigma 15 Gaussian, amount .35 | 867 / 3 / 1020 | clip 64..960 |

Medium/strong blend `A + (A-B)*amount` on luma, clamp to 0..1023, truncate to an
integer, then apply the highlight LUT. The LUT overrides only Y; its default
U/V `clipval` expressions clamp 10-bit YUV chroma to 64..960 independently of
the frame's range metadata. Go reproduces this behavior. Temporal medians use
all three planes independently, which can affect other transient objects too.

## Plane and timestamp observations

`lrt-airplane-planes-8.0.1.json` stores 102 synthetic observations from the
installed bundled FFmpeg 8.0.1, including 323,253 output samples and output PTS.
Fields specify component, CPU flags, dimensions, input frames, output frames,
filter string, source/expected values and integer PTS. Values are ordered by
frame, plane, row and column. All stored output values match Go exactly.

| Component | Default CPU 32x16 records/samples | Scalar 17x13 records/samples |
| --- | --- | --- |
| Full helper graph | 24 / 129,024 | 24 / 55,692 |
| Temporal median | 15 / 78,336 | 15 / 33,813 |
| Spatial/luma/LUT part | 12 / 18,432 | 12 / 7,956 |

Inputs include monotonic ramps, a moving bright point, two consecutive flashes,
black, white, constant, center impulse and deterministic noise. The scalar
records use `-cpuflags 0`; default CPU records preserve local dispatch.

`tmedian` consumes complete windows without endpoint padding. Radius r yields
N-2r output images. The PTS of output i belongs to source i (the window's first
frame), while its pixels are medians over sources i through i+2r. Go preserves
that timestamp anchor, including the RGB16 residual of the first source.

## Speed, trim and EOF behavior

The native caller adds a head trim `max(MotionBlurWindow,1)` and tail bound 1,
independently of median radius. Its trim end is
`int(Float(originalCount)/Float(speed)) - 1`. This is an end bound on the
resampled filtered stream; it need not remove its actual final image.

At speed 1 the native graph bypasses fps: median output remains N-2r frames,
then head trimming normally removes one. With no Motion Blur, ordinary speed
therefore yields N-3 frames for light/medium and N-5 for strong.

At other speeds, native setpts/fps receives the original EOF timestamp even
though tmedian emitted fewer images. It repeats the last median image through
the original sequence end. Go models that hold before Motion Blur and trimming;
it does not rerun median/spatial filters on duplicated output. For eight source
frames and no Motion Blur:

| Level | Speed 1 | Speed .5 | Speed 2 |
| --- | --- | --- | --- |
| 1/2 | 5 | 14 | 2 |
| 3 | 3 | 14 | 2 |

`lrt-airplane-timing-8.0.1.json` records 100 constructed graphs: the native
helper strings plus the inspected caller trim formula. The bundled FFmpeg
executed counts 3/5/8/11/15, speeds .5/1/1.1/2/4, Motion Blur 0/2 and levels
1/3 at 24 fps on 32x16 synthetic gray-ID sequences. These are helper/caller
composition observations, not direct gated application renders. There are
62 nonempty cases (496 output frames) and 38 empty/rejected cases. Stored
first-pixel IDs reveal the held tail. A repeat with native-style output `-r 24`
preserved every output count; live regression also checks all stored IDs.
Inspection of the complete command builder confirmed input/output `-r` and
global `-sws_flags print_info+accurate_rnd+bitexact+full_chroma_int`. Initial
source decoding/color conversion under those flags is outside the plane-only
comparisons here. Diagnostic addresses in rejected graphs were normalized.

Subtitles use the same PTS/source schedule and trims. Still exports clear this
video-only option. Empty movies/subtitles are rejected before replacing output.
Every original source is read once by the streaming stage, including neighbors
that do not directly correspond to output timestamps.

## Preset timing and integration

Native preset audio/fade timing still estimates frames from original N/speed,
then subtracts explicit head/tail trims. It does not subtract tmedian's own
loss. For 12 images, strong level, speed 1, 2 fps: actual video has 7 frames /
3.5 seconds, while native estimated audio lasts 5 seconds. A one-second fade
starts at 4 seconds, after video ends. A real MP4 test checks video/audio
durations, unchanged video pixels and the decoded audio tail/fade. Standalone
CLI timing continues to use the actual output duration.

Tests also check 18 real H.264 MP4/H.265 MOV cases (three levels, three speeds,
two codecs), metadata, frame counts, durations and decoded luma against the
Go-filtered RGB input. Encode/decode maximum/mean tolerances are 4/3 codes for
H.264 and 32/4 for H.265; these differ from exact plane comparisons. Other tests
cover cache ownership, future-input errors, denoise/median/speed/Motion Blur/
sharpen/CAS/overlay/fade order, preset defaults/overrides, batch, SRT, JPEG AVI,
still-export isolation and source/preset/existing-output preservation.

The Go stage uses independent approximate full-range BT.709 RGB48/YUV10
conversion, adds transformed deltas to original RGB16, and implements scalar
recursive Gaussian filtering. Complete native color management, other CPU
dispatch/dimensions, surrounding native command behavior and full application
pixel equivalence are not established by these measurements. A combined
temporal-buffer estimate above 768 MiB is refused before output creation.

To repeat the recorded planes and timing graphs:

```powershell
$env:LRTL_AIRPLANE_FFMPEG='C:\Program Files\LRTimelapse 7\ffmpeg.exe'
go test ./internal/lrtl -run '^TestAirplaneRemovalLive' -count=1
```

Only synthetic observations and independent code are distributed. References:
official FFmpeg [tmedian](https://ffmpeg.org/ffmpeg-filters.html#tmedian),
[gblur](https://ffmpeg.org/ffmpeg-filters.html#gblur),
[blend](https://ffmpeg.org/ffmpeg-filters.html#blend), and
[lut/lutyuv](https://ffmpeg.org/ffmpeg-filters.html#lut_002c-lutrgb_002c-lutyuv).
