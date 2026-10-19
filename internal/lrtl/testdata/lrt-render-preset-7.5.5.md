# LRT 7.5.5 render-preset observations

Observed on Windows with installed LRTimelapse 7.5.5 build 1059 and Java 25,
2026-10-19. The probe constructs six fresh RenderPreset objects, assigns only
synthetic names, outputQuality 0..5 and selected Motion Blur flags/counts, then
invokes the native `a():String` JSON serializer. Native generated IDs contain
no user information. These are original synthetic data and component results;
no application implementation or user settings file is included.

Each serialized object contains the constructor's h264 encoder, 1080 output
height, 29.97 frame-rate label, quality, 420 subsampling, speed 1 and disabled
optional controls. For qualities 1..5 the probe sets enabled Motion Blur counts
2/3/5/10/20; quality 0 retains its default off/zero. Counts 4 and 15 are supported
by the existing Go renderer, but are not new serializer observations here.

The probe also constructs the rendering helper with a null UI reference and
invokes its int-to-int quality helpers. `K(int)` returns H.264 CRF
22/18/15/10/8/0; `A(int)` returns H.265 CRF 22/20/16/10/8/0. The inspected command
builder uses those helpers for the corresponding encoder branches. Inspection
of the native encoder switches identifies encoder spellings ProRes, H265 and
DNxHR alongside default h264. Its ProRes profiles for qualities 0..4 are
0/1/2/3/4, with default profile 3; DNxHR profiles are LB/SQ/HQ/HQX/444 with HQ
as the default branch. These profile switches were inspected, not newly
observed through a complete native encoder launch.

Go compares the native JSON fields and measured CRFs, applies explicit CLI
overrides, chooses lastRenderSettings or an exact unique renderPresets name,
and reads the separate global renderAR only when renderConstrainCrop is true.
Unrecognized metadata is ignored, but enabled known unsupported processing
controls fail before any video publication. Source settings are read-only.
Relative audio paths are resolved against the JSON directory as a documented
lrtl convention; native relative-path behavior was not measured.

Tests compare raw RGB input and FFmpeg arguments with explicit CLI controls,
check batch forwarding, malformed/unsupported presets and existing-output
retention. Optional FFmpeg/ffprobe tests generate Proxy/LT/4444 MOVs and a real
H.264 MP4 with imported native 1080 height, 3:2 global crop and NTSC rate.
This does not prove native full-export/color-management or pixel equivalence.
