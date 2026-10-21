# Native text watermark

`render --watermark TEXT --watermark-style lrt-7.5.5` reproduces the installed
7.5.5 watermark helper and uses the existing FFmpeg drawtext filter. The Go
renderer retains native working YUV until final output conversion, then applies:

```text
final color conversion → watermark → native timestamp → trim/reset → fade
```

Go's temporal pipeline has already trimmed output frames; the timestamp stage
temporarily offsets PTS to retain the native source-based subtitle clock.
Watermark and fade still occur in the measured order. No extra CGo dependency
or bundled proprietary font is added.

## Observations

The installed helper was invoked directly using a null UI owner, without
changing native bytecode or licensing state. Committed files contain only
synthetic inputs and observed return values:

- `lrt-watermark-filter-7.5.5.json`: 105 observations, 15 widths and seven text
  samples, covering every font-size boundary and punctuation/Unicode;
- `lrt-watermark-caller-7.5.5.json`: three complete graphs, with native timestamp,
  fade, and plain/sharpen/Motion Blur variants.

Native text replaces ASCII space, semicolon, colon, double quote, and apostrophe
with NBSP. Other characters retain native drawtext parsing/expansion behavior.
This style does not use the standard Go watermark's literal text layout.
Arial.ttf is used, with RGBA `0xf0f0f040`, shadow `0x00000020`, offset (-2,-2),
horizontal centering and vertical expression `h/4*2.5-max_glyph_h`.

| Output width | Font size |
|---|---:|
| ≤1280 | 60 |
| ≤1920 | 90 |
| ≤3072 | 150 |
| ≤4096 | 200 |
| ≤5120 | 250 |
| ≤7680 | 320 |
| >7680 | 400 |

The implementation resolves the installed Arial file to an absolute path, so
changing the encoder's working directory for native subtitles does not change
font lookup. The recorded Windows caller uses `/Windows/Fonts/arial.ttf`;
Windows tests compare that caller with the resolved font path.

## CLI and saved settings

```powershell
lrtl render C:\photos\developed --output C:\photos\watermarked.mp4 --output-color lrt-7.5.5 --watermark 'Kota 2026' --watermark-style lrt-7.5.5

# Both overlays use the native path and are faded together.
lrtl render C:\photos\developed --output C:\photos\captioned.mp4 --output-color lrt-7.5.5 --watermark 'Kota 2026' --watermark-style lrt-7.5.5 --timestamp-style lrt-7.5.5 --timestamp-every 3 --fade-out 1
```

Native styling requires even-sized MP4/MOV output and `--output-color
lrt-7.5.5`. Its position is fixed, so omit `--watermark-position`. Native and
standard overlays cannot be mixed in one render. Standard watermark behavior,
including five positions and the embedded Go font, is still the default.

When importing an LRT settings JSON, `renderCopyrightEnabled` and
`renderCopyrightText` are read alongside the selected preset, including named
presets. Enabled copyright selects native watermark styling. Explicit
`--watermark` and `--watermark-style` override the corresponding saved values;
`--watermark ''` disables saved text. Standalone preset JSON has no global
copyright setting. Invalid settings are rejected before video replacement.

## Verification and remaining differences

42 actual videos compare decoded samples and ffprobe color/dimension/count/rate
metadata against independent native observations:

- 18 run the three recorded 640×320 caller graphs through H.264/H.265/ProRes HQ,
  full and limited range, with native timestamp and fade;
- 18 cover strong temporal/spatial processing, slow blend, and fast motion
  interpolation, through the same codecs/ranges;
- six cover BT.2020 source to BT.709 SDR, BT.2020 SDR, and BT.2020 PQ, H.265,
  with watermark, native timestamp, sharpening and fade, both ranges.

The video reference helper was corrected at speed exactly 1.0: the native
caller bypasses the speed helper and emits frame-index PTS. Calling the speed
helper at 1.0 incorrectly inserts fps and extends tmedian EOF frames. The new
strong temporal case detects that reference error; the production frame planner
already matched the actual caller branch.

CLI/preset/batch tests cover style validation, explicit overrides, source and
existing-output preservation, failed-source temporary-file cleanup, and paths
containing spaces, commas, apostrophes and Unicode. The pure Go filter builder
is checked against all 105 observations and fuzzed.

The original 42 pixel comparisons use opaque prepared RGB16 PNG images.
The initial RGB8 conversion difference found during those tests has since been
fixed: [RGB8 input measurements](lrt-rgb8-input-8.0.1.md) cover all RGB8 triplets
and another 72 videos, including 18 RGB8 watermark/timestamp/fade caller videos.
An isolated 64×32 RGB24 input (`R=x*20 mod 256`, `G=y*30 mod 256`, `B=80`)
previously differed in 4005/6144 working samples, up to four codes, when expanded
to RGB48 first. [Native JPEG decoding](lrt-jpeg-input-8.0.1.md) now has separate
32-fixture and 180-video coverage for the identity-geometry path. Arbitrary
geometry, Camera Raw development and complete ICC color management also remain
separate fidelity work. Native drawtext expressions and platform fonts retain
their own rendering behavior; glyph availability is not guaranteed by a label.

```powershell
go test ./internal/lrtl -run '^TestNativeWatermark' -count=1
go test ./internal/lrtl -run '^$' -fuzz '^FuzzNativeWatermarkFilter$' -fuzztime 10s
```
