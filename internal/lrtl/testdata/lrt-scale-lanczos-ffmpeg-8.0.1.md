# Lanczos scale component observations

Recorded on2026-10-20, Windows, installed LRTimelapse7.5.5 build1059/Java25.
`lrt-scale-filter-7.5.5.json` records six ordinary native graph builder calls
with synthetic BT709 labels, one placeholder frame, default RenderPreset and
sQa(width,height,requestedHeight,0f). They all select full-range `scale` with
`flags=lanczos`. No native encoder/GUI or gate modifications were involved.
The native dimension selections are retained even when they differ from Go.

`lrt-scale-lanczos-ffmpeg-8.0.1.json` is a separate component observation using
the installed `C:\Program Files\LRTimelapse 7\ffmpeg.exe` (8.0.1 essentials
gyan build). It receives32x24 gray16le and outputs one gray16le frame through
`scale=W:H:flags=lanczos`. All samples are16-bit little-endian. Each of six
patterns is resized to16x12,19x17,48x36 and20x24, giving24 cases and16,338
stored actual output samples. Input patterns use integer arithmetic:

- constant:12345;
- ramp:1600*x+200*y;
- impulse:10000, except50000 at(16,12);
- edge:10000 for x<16, otherwise50000;
- checker:50000 when(x+y)%2==0, otherwise10000;
- noise:1000+((13*x+23*y+7*x*y)%101)*600.

The expected arrays come from FFmpeg, not the Go kernel. Go computes
sinc(pi*d)*sinc(pi*d/3), support3, widened for shrinking, with half-pixel
alignment and replicated edges. Unlike the previous Catmull-Rom filter it
matches the native kernel family. Floating-point RGB arithmetic differs from
FFmpeg's fixed-point/conversion implementation. Pooled measured RMSE is44.413
versus980.479 before the change. Largest absolute residual is1093/65535 on
synthetic noise upscaling. This is not a full native video or photographic
quality test; dimension selection, color management and RAW are out of scope.

The regular pure-Go test compares every stored sample with pattern-specific
bounds, and requires aggregate RMSE below10% of the former algorithm's RMSE.
An optional FFmpeg test replays the stored observations, allowing two16-bit
codes for platform rounding differences. Select the installed bundle with:

```powershell
$env:LRTL_SCALE_FFMPEG='C:\Program Files\LRTimelapse 7\ffmpeg.exe'
go test ./internal/lrtl -run '^TestResizeLanczos' -count=1 -v
```

No FFmpeg/Java dependency is added to runtime AVI or image exports. The
output filter is independent Go code, and ordinary observation tests still
run without those external applications.
