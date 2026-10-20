# Fractional speed and native duration observations

Recorded2026-10-20, Windows, LRTimelapse7.5.5 build1059, Java25. No native GUI,
full export or license/development gate changes were involved.

`lrt-speed-fractional-7.5.5.json` starts with36 direct mode-0 speed helper
calls: .06250001/.1/.2/.3/.7/.9/1.1/1.3/2.3/3.3/7.9/15.99 at24/29.97/59.94.
Each emitted filter is run by the installed native bundle's FFmpeg8.0.1 on
16x16 gray16le source-ID frames of lengths1/3/11/31/101/257/1001. Input rate
matches the native emitted fps value; fps_mode=passthrough prevents output
rescheduling. All166,242 output IDs in252 cases agree with the Go scheduler.

The fixture stores original filters plus cumulative source `ends`. ends[i]
is the number of observed outputs through source i. Actual monotonic output
IDs were counted and cumulatively summed; no production formula generated
them. Repeated ends encode dropped sources, gaps encode duplicated sources,
and the final end is the total count. This encoding preserves every observed
source ID without storing repeated integers. Pure-Go tests check every end;
optional FFmpeg replay checks all36 filters at source count11, every pixel.

`lrt-native-timing-7.5.5.json` is96 isolated ordinary graph builder calls:
rates24/29.97, windows0/2, speeds.1/.3/.7/1.1/1.3/2.3, source counts3/11/31/101.
FadeOut=true, FadeTime=1, audio input enabled, geometry64x32, synthetic BT709,
placeholder filenames. Original video/audio fade, atrim and temporal filter
fragments are retained, including invalid/empty native cases.

Native timing uses truncated float32 sourceCount/speed, subtracts warm-up
trim, divides by displayed FPS in float32, and formats atrim to three decimals.
This differs from actual integer PTS scheduling at some float boundaries.
All nonempty native timing observations match Go;24 fade plans differ from
the former actual-count plan. Empty outputs still fail before publication.

Imported presets enable this timing even when CLI overrides dimensions/rate/
speed/fade. Standalone CLI keeps actual-frame-count timing. Actual frame count
and subtitles remain based on the source map, independently of the native
duration estimate. TIFF/preview image export clears this video-only setting.

Separate Go H.264/AAC integration verifies11 sources at1.1x/24fps yield9 video
frames/0.375s and0.417s audio. Decoded RGB and PCM verify the native fade gain;
the audio tail is not cut at the last video frame. Preset audio therefore omits
video-frame/output-time/shortest caps and instead drains finite atrim plus raw
video EOF. This is our encoder test, not a native complete-export observation.
RAW/Camera Raw, color management and complete native encoder equivalence are
not established. No runtime Java dependency is introduced.

```powershell
$env:LRTL_SPEED_FFMPEG='C:\Program Files\LRTimelapse 7\ffmpeg.exe'
go test ./internal/lrtl -run 'TestFractionalSpeed|TestNativeTiming' -count=1 -v
```
