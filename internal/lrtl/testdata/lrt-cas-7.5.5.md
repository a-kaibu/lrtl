# CAS output sharpening observations

The installed LRTimelapse 7.5.5 build 1059 render filter builder calls its CAS
helper with strength 0.9 when the saved `cas` flag is enabled. It appends this
after temporal blending and the ordinary unsharp filter, before output range
conversion, overlays, trimming and fade. This order and constant were inspected
in the render builder. The application's feature gates were not changed.

`lrt-cas-filter-7.5.5.json` records direct calls to the static filter-string
helper with five synthetic strengths: zero yields an empty string; 0.25, 0.5,
0.9 and 1 yield a CAS filter with two decimal places. The helper was invoked
independently of the gated complete rendering path.

`lrt-cas-ffmpeg-8.0.1.json` records synthetic full-range 10-bit planes processed
by the locally installed bundled FFmpeg 8.0.1 (gyan essentials). Eight 32x16
patterns (black, white, constant, center impulse, corner impulse, edge, ramp,
deterministic noise) at strengths 0.25/0.9/1 produce 24 observations and 12,288
sample values. The same input was supplied to each YUV444 plane; all three
outputs were confirmed equal. Only synthetic inputs and measured numbers are
stored, not application code, binaries or user settings.

The independent Go scalar kernel uses cross/square neighborhood extrema,
an adaptive negative cross-neighbor weight, replicated edges, float32
arithmetic and truncation to a 10-bit sample. Regression tests check every
recorded value without requiring FFmpeg or LRT. An optional installed-FFmpeg
test repeats every observation on all three planes. Thin and single-pixel
planes, clipping, source retention and RGB16 detail are checked separately.

The Go renderer computes approximate full-range BT.709 Y/Cb/Cr from its RGB16
input, filters all three planes, then adds the inverse-transformed deltas to
the original RGB16 samples. Keeping deltas preserves sub-10-bit detail when
no filter change occurs. The observed native path operates directly on its
10-bit YUV input. Equality of the sampled scalar plane operation therefore
does **not** establish complete native color-managed pixel equivalence.
HDR, wide-gamut processing and native scaling by CAS are not implemented.

Tests also check saved preset import, explicit false overrides, batch renders,
speed/temporal frame schedules, sharpener/overlay/fade ordering, JPEG AVI
passthrough, invalid-option output retention and still-export isolation.

An optional end-to-end test creates three-frame H.264 MP4 and H.265 MOV files
in full-range 10-bit 4:4:4, checks metadata and decodes 6,144 luma samples per
codec. Its reference is the Go-filtered RGB16 input projected to full-range
BT.709 luma, not a complete native render. With local FFmpeg 8.0.1, the maximum
and mean luma errors were 3 / 0.586 for H.264 and 17 / 2.121 for H.265.
H.265 CRF 0 still quantizes; the test bounds maximum/mean error separately
(32/4 codes, versus 4/3 for H.264). The stored scalar plane tests require exact
sample equality. These are different verification scopes.

References: [FFmpeg CAS options](https://ffmpeg.org/ffmpeg-filters.html#cas),
[FFmpeg 8.0.1 filter implementation](https://github.com/FFmpeg/FFmpeg/blob/n8.0.1/libavfilter/vf_cas.c),
[AMD CAS overview](https://gpuopen.com/fidelityfx-cas/).
