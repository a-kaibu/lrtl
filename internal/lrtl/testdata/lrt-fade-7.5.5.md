# LRT 7.5.5 fade-filter timing observations

Observed on installed LRTimelapse 7.5.5 build 1059, Java 25, Windows,
2026-10-19. An isolated rendering helper with a null UI reference invokes its
public filter-string builder. Original synthetic inputs are 3/30/90/3000 file
placeholders, frame-rate labels 24/29.97 and fade times 1/3 seconds. FadeOut is
enabled; Motion Blur, speed changes, overlays and optional processing are off.
The audio-input argument is enabled. A 64x32 geometry component and BT709 label
are supplied. No media files are opened and no native encoder or GUI is run.

The JSON records the 16 emitted video fade and audio afade start/duration
pairs. In every case their timing is identical. The builder calculates total
duration using single-precision frame count / displayed frame-rate number,
caps the requested fade at that duration, and formats the start with three
decimal places. Go compares every start exactly and every duration at float32
precision, matching the precision of the native serialized decimal results.

Go uses these timings for its own RGB fade and FFmpeg afade. Pixel positions
still use the actual rational output rate; audio trim and SRT timing retain
the rational-rate path. An explicit fraction is a lrtl input extension and is
converted to float32 for this fade calculation; the native builder accepts
decimal labels. Equivalent nominal NTSC decimal and fraction spellings may
therefore differ slightly in fade timing, as the native decimal division uses
the displayed label rather than its /1001 output ratio.

The Go image fade scales encoded RGB components after Motion Blur and
overlays. It is not linear-light exposure compensation. Source alpha and
16-bit precision survive the operation. This differs in processing space
from the native color-managed YUV filter chain; exact native pixel equivalence
is not established. Existing Go Motion Blur retains its frame-padding policy;
these native timing observations do not include native Motion Blur trimming.

Tests inspect RGB48 data with/without Motion Blur and watermark, actual AVI
JPEG pixels and untouched early-frame passthrough, native preset/CLI override,
invalid-input output preservation and batch forwarding. Optional FFmpeg tests
decode a real audio/video MP4, verify its frame fade and measure audio RMS in
early/late windows. The last sampled frame need not be black, because the
fade endpoint is the video duration, after that frame's presentation time.
