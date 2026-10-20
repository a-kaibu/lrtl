# Native Motion Blur trim observations

Recorded 2026-10-20 on Windows, LRTimelapse 7.5.5 build1059, Java25.
An independently written reflection probe constructs mQa with a null UI,
sQa geometry64x32, and a constructor-default RenderPreset. It calls the
ordinary graph builder with synthetic BT709 source and placeholder file names.
No native export/GUI or license/development gate changes are involved.

The 48 cases cross window0/2/3/4/5/10/15/20, speed0.5/1/2, and source count6/31.
FPS is24; fadeOut=true, fadeTime=1. The original `trim` and video `fade`
fragments are retained. `temporal` combines the native setpts/fps fragment,
native tmix with its original three-decimal weight string, and native
trim/setpts reset. Whitespace from `, fps=24` is intentionally preserved.
Color/range transforms, overlays and fade are omitted from temporal replay.

Each `temporal` chain was run with the installed native bundle's FFmpeg8.0.1
essentials gyan build. Input is32x16 yuv444p10le at24fps, with source frame i
having Y=100+20*i and U=V=512 at every pixel. Raw input/output use16-bit
little-endian containers. `-fps_mode passthrough` prevents output rescheduling.
`luma` contains the first Y sample from every emitted frame. These are actual
filter outputs, independent of the Go implementation. An empty array records
a real empty result, rather than a substituted successful frame.

The result confirms native warm-up removal of N resampled output frames.
Go rejects empty timelines, preserving prior files. It retains16-bit RGB
blending; the captured Go sample is compared to native Y10 within0.51 codes,
while output counts and retained source timeline match exactly. This does not
test the complete LRT encoder, native color pipeline, overlays or RAW input.
The tests replay every output Y/U/V pixel when FFmpeg is available. To replay
against the installed bundle explicitly:

```powershell
$env:LRTL_MOTION_FFMPEG='C:\Program Files\LRTimelapse 7\ffmpeg.exe'
go test ./internal/lrtl -run '^TestMotionBlurActualFFmpegTemporalReference$' -count=1 -v
```

All stored observations and Go timeline comparisons remain available without
Java or FFmpeg. Actual MP4/audio duration/fade testing is optional when FFmpeg
and ffprobe are absent; pure-Go AVI, subtitle and retention tests still run.
