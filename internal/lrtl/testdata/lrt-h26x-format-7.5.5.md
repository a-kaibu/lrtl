# Native H.264/H.265 output formats

Observed with locally installed LRTimelapse 7.5.5 build 1059. The JSON records
only outputs of helpers invoked with synthetic inputs; no proprietary code or
user settings are included.

The probe initialized the native application settings class, constructed a
render command helper without a UI, and supplied a synthetic 64x32 SDR preset.
It invoked the codec, pixel-format and extension helpers directly. Inputs:
H.264/H.265, sampling 420/422/444, quality 0/3/5 and video range 0/1 (36 cases).
The pixel helper received the preset's native integer getter values in the
same order as its caller. No renderer, activation or license controls were
modified.

| Sampling | H.264 and H.265 pixel format |
| --- | --- |
| 420 | yuv420p (8-bit) |
| 422 | yuv422p10le (10-bit) |
| 444 | yuv444p10le (10-bit) |

Quality and video range did not change the selected format in these cases.
The extension helper returned `.mp4` for H.264 and `.mov` for H.265. The codec
helper returned libx264/libx265. This does not establish complete native
rendering or color-conversion equivalence, nor HDR behavior.

`TestSubsamplingNative36Formats` consumes these observations. Additional tests
exercise CLI overrides, batch forwarding, unsupported encoders, source/output
preservation and 18 real FFmpeg container/sampling/range combinations. The
actual-encoder test uses a synthetic 16-bit gradient, checks metadata and
decoded luma levels, and skips unavailable encoder formats explicitly.

FFmpeg can replace an unsupported requested pixel format; lrtl therefore
checks `-h encoder=...` before creating output. See the official
[pixel format option documentation](https://ffmpeg.org/ffmpeg.html#Advanced-Video-options).

Repeated real encoding exposed intermittent short-input mux failures in the
installed FFmpeg 8.0.1 / x265 4.1 build 211. The upstream 4.1
[encoder implementation](https://github.com/Multicorewareinc/x265/blob/4.1/source/encoder/encoder.cpp)
assigns `m_bframeDelayTime` only when the input POC reaches `m_bframeDelay`
(normally 2 with B-pyramid), then uses it when draining shorter sequences.
Its constructor does not initialize that member. lrtl disables B frames for
H.265 outputs shorter than three frames. Tests check the one/two/three-frame
boundary and leave longer sequences and H.264 unchanged. Real one-frame
gradation tests also exercise this guard. This safeguard is an independent
compatibility fix, not an observed native render option.
