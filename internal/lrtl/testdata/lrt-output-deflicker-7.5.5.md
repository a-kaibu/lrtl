# Render Luma/Chroma Deflicker: LRTimelapse 7.5.5

The installed 7.5.5 build 1059 JAR's static filter generator was invoked for
all four luma/chroma boolean combinations. The returned strings are recorded
in `lrt-output-deflicker-filter-7.5.5.json`. They extract Y/U/V, independently
apply `deflicker=mode=pm:size=10` to enabled planes, copy disabled planes and
merge into yuv444p10le. No activation state was changed. The complete graph
builder checks a feature gate; these are direct helper observations, not a
claim that a gated full application render was executed.

Caller inspection places this stage after scaling and before orientation,
output crop, noise reduction, light-trail removal, speed conversion, Motion
Blur, output sharpening and CAS. Go computes the scalar plane means in
oriented coordinates, which preserves their sums. With a native preset it
filters the complete scaled canvas and then crops. Standalone CLI retains its
existing crop/resize preparation and filters that resulting canvas. XMP/RAW
development and all RGB/YUV conversion remain the independent Go pipeline.

`lrt-output-deflicker-planes-8.0.1.json` contains 288 synthetic input/output
observations from the installed bundled FFmpeg 8.0.1 gyan essentials:

- luma, chroma, both;
- dimensions 16x8 and 7x5;
- sequence lengths 1, 2, 5, 9, 10, 11, 17, 23;
- zero, constant, temporal/spatial ramp, flash, zero-to-positive step, noise;
- all three 10-bit planes, 686,556 output samples, exact comparison;
- output frame count equal to input count, including short clips and EOF.

The independently implemented math uses float32 spatial means, float32
10th-power terms accumulated in slot order, division by ten, a float32 0.1
root exponent, and division by the first mean. Each pixel is multiplied in
float32, truncated and clipped to 0..1023. Zero-mean planes produce zero.
Go math.Pow rounded to float32 matches every sample in this dataset; this
does not establish universal powf rounding equality on every input/platform.

Short clips initially leave unused mean slots zero. At EOF the frame queue
shrinks, all nine later mean slots still shift left, and the last source mean
is inserted at the shrinking queue's last index. Replacing this with ten
copies of the final frame would not match the observations. A ten-frame RGB16
cache loads each input once; returned RGB belongs to the caller. All selected
YUV deltas are applied together to the original RGB16 to avoid intermediate
channel clipping and retain its rounding residual.

Tests compare this stateful implementation with an offline full-sequence
reference. CLI tests cover native crop positions, preset and batch equality,
explicit false overrides, noise/light-trail/speed/blur/sharpen/CAS/overlay/fade
order, JPEG AVI payloads, still-export isolation, source/preset retention,
future decode failures and a combined 768 MiB memory guard. Real H.264 MP4
and H.265 MOV tests cover three enabled-plane combinations and speeds
1/0.5/2 (18 outputs): decoded luma, count, duration, pixel format and fps.
Codec round-trip tolerances are max/mean 4/3 for H.264 and 32/4 for H.265;
they are separate from exact scalar plane comparisons. Full native color
management and full application pixel equivalence remain unverified.

To repeat the recorded native helper graphs against the installed binary:

```powershell
$env:LRTL_OUTPUT_DEFLICKER_FFMPEG='C:\Program Files\LRTimelapse 7\ffmpeg.exe'
go test ./internal/lrtl -run '^TestOutputDeflickerLiveReferences$' -count=1
```

Only synthetic observations and independent code are distributed. The probe,
installed classes and decompilation stay outside version control. Algorithm
and queue behavior were cross-checked against the official FFmpeg
[deflicker source](https://github.com/FFmpeg/FFmpeg/blob/n8.0.1/libavfilter/vf_deflicker.c).
