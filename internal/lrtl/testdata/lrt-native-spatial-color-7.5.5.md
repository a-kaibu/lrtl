# Native SDR color through Go spatial filters

The native SDR command first creates full-range `yuv444p10le`, then applies
unsharp and CAS, followed by output color/range conversion. Converting a
filtered result back to RGB before encoding causes additional quantization
and clipping, particularly for saturated colors. The native spatial path
keeps these working planes through final encoding.

`nativeSDRPlanes` independently implements the observed numerical conversion:
BT.601 limited-range coefficients at 15-bit scale, intermediate 16-to-15-bit
truncation, fixed-point limited-to-full range expansion, then half-up 10-bit
rounding and clipping. This is the initial swscale default matrix of the
observed command; assigning BT.709 metadata afterwards does not change it.
The equations and rounding stages were inspected against the official
[FFmpeg n8.0.1 input](https://github.com/FFmpeg/FFmpeg/blob/n8.0.1/libswscale/input.c),
[output](https://github.com/FFmpeg/FFmpeg/blob/n8.0.1/libswscale/output.c),
[range conversion](https://github.com/FFmpeg/FFmpeg/blob/n8.0.1/libswscale/swscale.c)
and [coefficient setup](https://github.com/FFmpeg/FFmpeg/blob/n8.0.1/libswscale/utils.c).
No native binary, source code or image assets are redistributed.

The pure Go converter is checked against all 24 existing full-range 444
observations in `lrt-sdr-output-color-8.0.1.json`, without requiring FFmpeg.
With the installed bundled FFmpeg specified, an additional ten comparisons
cover every 16-bit gray, red, green and blue input plus 65,536 deterministic
xorshift RGB triplets, with both native CPU and `-cpuflags 0` paths. Each
comparison checks all three full output planes (1,966,080 component samples
across the ten cases).

`TestNativeSpatialActualImages` adds 84 full video comparisons: PNG/TIFF,
H.264/H.265 (420 8-bit, 422/444 10-bit), ProRes HQ (422 10-bit), full/limited,
and unsharp/CAS/both. Go performs the
matrix conversion and spatial filters; a separate reference encode reads the
original image files and performs the observed native FFmpeg filter graph.
Every decoded YUV byte and the ffprobe color/dimension/rate/frame metadata
must match. The reference never invokes the Go converter. The shared test
also retains the previous 12 unfiltered video cases.

Routing tests cover default, speed, Motion Blur, noise removal, airplane
removal, output deflicker and interpolation, plus existing RGB fallback for
overlays/fade/odd sizes/standard color. Source files must remain unchanged.
Temporal combinations additionally compare every output byte with the same
RGB temporal pipeline followed by exactly one native conversion and spatial
pass. This establishes ordering, not native whole-pipeline pixel parity.
Invalid layouts fail before launching an
encoder. Fuzzing covers small frame conversion, unsharp and CAS together.

```powershell
$env:LRTL_COLOR_FFMPEG='C:\Program Files\LRTimelapse 7\ffmpeg.exe'
go test ./internal/lrtl -run '^TestNative(SDR|Spatial)' -count=1 -v
go test ./internal/lrtl -run '^$' -fuzz '^FuzzNativeSDRConversion$' -fuzztime 10s
```

This path is enabled by `--output-color lrt-7.5.5` (also the saved preset
default), with sharpen and/or CAS, even output dimensions and no timestamp,
watermark or fade. Existing RGB conversion is retained for the other
combinations. Supported time filters now have a separate
[native temporal YUV path](lrt-native-temporal-color-7.5.5.md); other temporal
combinations retain the previous BT.709 approximation. Go resize/RAW/XMP
processing is not a reproduction of Camera Raw or
native swscale geometry. Matching the measured spatial path does not imply
complete LRT application, HDR, ICC or all filter-combination parity.
