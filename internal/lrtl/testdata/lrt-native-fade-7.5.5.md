# Native SDR working-plane fade

The installed 7.5.5 render command places fade after temporal processing,
sharpen/CAS, overlays and trim. For even SDR frames without overlays, Go now
keeps the working YUV planes through fade and encoding. The standard profile,
odd dimensions and timestamp/watermark combinations retain the RGB path.

The fade is evaluated in pure Go after spatial filtering. Start and duration
are truncated to microseconds, then rounded to the output frame clock. The remaining
gain uses a 65535 numerator and integer division. An unchanged frame bypasses
sample arithmetic. The 8/10-bit planar formats fade Y toward 16/64 and U/V
toward 128/512, even with full-range metadata. This is the observed native
format behavior, including raised black at the end of a full-range fade.
The 8-bit chroma bias is 8421367, nine below the nominal half-up value;
tests retain that difference at rounding ties. A positive microsecond duration
which rounds to zero frames becomes black on the next frame.

The timing and sample arithmetic were checked against the official FFmpeg
n8.0.1 [fade implementation](https://github.com/FFmpeg/FFmpeg/blob/n8.0.1/libavfilter/vf_fade.c).
The 42 saved hashes in `lrt-native-fade-8.0.1.json` were measured with the
installed bundled FFmpeg 8.0.1, not generated from Go output. They cover
8/10-bit planes, 24, 30000/1001 and 2 fps, seven start/duration pairs, 41 frames
per sequence, including sub-frame duration, microsecond truncation at half-frame boundaries and below-black/full-range codes. A positive duration truncated to zero microseconds uses the native default 25-frame ramp; an explicit CLI duration of zero still disables fade.
The ordinary suite checks these references without an external executable.

Live pipeline comparisons cover plain, sharpen/CAS and strong combined
deflicker/denoise/airplane processing, plus both slow interpolation modes
followed by 8-bit sharpen/CAS and fade. The strong case uses the measured
12-frame output after denoise latency and trim. Actual video comparisons
cover all five paths with H.264, H.265 and ProRes HQ, both full and
limited output ranges: 30 decoded videos and matching metadata.

These tests also exposed a previous high-depth unsharp clamp difference:
native 10-bit unsharp can retain a sample of 1024 in 16-bit storage before
CAS/fade. The native working path now preserves that overshoot. The original
RGB approximation retains its existing 10-bit clamp. An independent impulse
test compares the storage value directly against bundled unsharp. The storage
behavior is consistent with the official
[unsharp implementation](https://github.com/FFmpeg/FFmpeg/blob/n8.0.1/libavfilter/vf_unsharp.c).

This does not establish full licensed-application rendering parity. Overlay
fonts/layout, ICC/HDR, resizing and native threading differences remain
separate work. Existing native preset timing still uses the native estimated
duration; the new fade does not change scheduling or audio timing.

```powershell
$env:LRTL_COLOR_FFMPEG='C:\Program Files\LRTimelapse 7\ffmpeg.exe'
go test ./internal/lrtl -run '^(TestNativeFade|TestNativeSharpenStorage)' -count=1 -v
go test ./internal/lrtl -run '^$' -fuzz '^FuzzNativeFadePlanes$' -fuzztime 10s
```
