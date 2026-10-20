# LRT 7.5.5 normal-speed filter and frame-mapping observations

On 2026-10-19, the installed 7.5.5 build 1059 `mqa.A(float,String,int)` helper
was invoked with interpolation mode 0, ten speeds and two displayed rates.
Speeds are .0625/.125/.25/one-third/.5/1.5/2/3/4/16; rates are 24 and 29.97.
The one-third argument is the native float value serialized as 0.33333334.
The probe uses isolated component calls; it changes no license/development
setting and launches no native renderer or GUI. The 20 returned filter strings
are original component observations, not application implementation code.

Each filter was then executed with installed FFmpeg 8.0.1-full_build-www.gyan.dev
on 1/2/7/10 synthetic 16x16 gray frames. Every pixel in input frame i is i.
Input framerate equals the returned filter's fps rate (24 or 30000/1001).
Output is raw gray; the fixture stores each output's source index. This yields
80 cases. Thus filters are native observations, while frame mappings are
observations of those filters in this FFmpeg version, not a full LRT export.

The mode-0 filter scales PTS by 1/float(speed), then applies fps. Integer PTS
truncation produces each source frame's output interval
[floor(i/speed), floor((i+1)/speed)). Fast intervals can be empty; equal PTS
choose the later source. EOF uses the truncated scaled source-end timestamp.
For one-third the reciprocal is slightly below 3, so a single frame becomes
two frames, not three. Go compares counts and every source index in all 80
cases; zero-output cases are rejected by the public renderer before writing.

Go applies scheduling after source/XMP preparation and before Motion Blur.
Each source is decoded once, including dropped frames; duplicate outputs reuse
the prepared RGB data. Overlay capture times retain source correspondence.
Audio retains normal speed and uses actual output duration for padding/trim.
SRT uses the same schedule. TIFF/preview exports clear the video-only speed.

These original80 cases do not cover Motion Blur trimming or experimental blend/MCI
interpolation. Motion Blur trimming and native duration estimates are now covered
by separate fixtures. The native estimate int(float(frameCount)/speed) can
differ from actual filtered count at float boundaries. Preset imports now use
that estimate for fade/audio; standalone CLI keeps actual-count timing. See
lrt-native-timing-7.5.5.md for the further252 maps and96 timing calls. Non-unit
speed AVI goes through the RGB/JPEG processing path, so it does not preserve
original compressed JPEG bytes. Output/input pixel precision, processing order,
timestamps/subtitles, preset/CLI overrides, batch forwarding, corrupt dropped
inputs and actual AVI/MP4 output have separate regression coverage.
