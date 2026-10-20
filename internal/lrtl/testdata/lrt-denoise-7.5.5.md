# Native output denoise observations

LRTimelapse 7.5.5 build 1059 stores an enable flag `denoise` and integer
`denoiseLevel` (constructor default 1). The render builder selects four graphs,
after scaling/cropping and before airplane removal, speed/tmix, output sharpening,
CAS, range conversion, overlays and fade. Direct calls to the static graph
helpers produced `lrt-denoise-filter-7.5.5.json`. No native feature gate, license
or activation state was changed; complete gated native rendering was not run.

| Level | Temporal window | Y A/B | Cb A/B | Cr A/B | Wavelet threshold/method/steps |
| --- | --- | --- | --- | --- | --- |
| 1 | 5 | .03/.04 | .10/.10 | .02/.04 (defaults) | 2 / soft / 2 |
| 2 | 5 | .05/.15 | .20/.40 | .20/.40 | 4 / garrote / 6 |
| 3 | 7 | .08/.17 | .20/.60 | .20/.60 | 4 / garrote / 6 |
| 4 | 13 | .10/.20 | .30/.80 | .30/.80 | 6 / garrote / 10 |

Every wavelet graph uses percent 80 and the default universal threshold type.
Steps are limited by frame dimensions. Level 4 first splits the input: one
branch passes through two removegrain mode-4 operations and Gaussian sigma .5;
the branches combine with lighten. The removegrain branch negotiates 8-bit
YUV, then converts back to 10-bit before blending with the original 10-bit
branch. A direct conversion probe measured phases `{1,2;3,0}` for 10-to-8-bit
rounding and a factor of four on conversion back. Go includes this quantization.

## Numerical comparisons

`lrt-denoise-planes-8.0.1.json` contains 354 synthetic observations from the
installed bundled FFmpeg 8.0.1 (gyan essentials), totalling 644,058 output
samples. Each record includes filter text, CPU flags, dimensions, source frame
count, input values and expected values, in frame/plane/row/column order.

| Component | Default CPU records/samples | `-cpuflags 0` records/samples |
| --- | --- | --- |
| Temporal | 80 / 43,008 | 80 / 43,008 |
| Wavelet | 48 / 52,776 | 48 / 52,776 |
| Strong preclean | 6 / 9,216 | 12 / 13,194 |
| Complete denoise graph | 40 / 344,064 | 40 / 86,016 |

Temporal comparisons cover all four preset windows and thresholds, lengths
1/2/3/7/15, ramps, near-threshold differences, scene jumps and alternating
values. The native parallel algorithm stops the joint past/future walk as soon
as either side fails; an accepted past value remains included if the following
future value fails. It rounds the accepted integer average to nearest.

Initial temporal padding repeats the first frame. The native EOF drain repeats
zero-based frame `max(0, count-radius)`, which can differ from the last frame.
Go follows this measured behavior and preserves the number of source frames.

The wavelet implementation independently evaluates CDF 9/7 analysis/synthesis
with symmetric extension, float32 arithmetic, soft/garrote thresholding and
rounding to 10-bit. Small plane cases include 32x16 and 17x13. The complete
graphs use 32x16 with default CPU dispatch and 16x8 with CPU dispatch disabled.
All stored observations match exactly.

`lrt-denoise-deep-8.0.1.json` additionally records SHA-256 hashes of complete
gray10le outputs at 129x129, 257x257 and 2049x2049. A deterministic noise input
exercises 6/6/10 transform steps. Tests reproduce all 4,281,091 samples and
compare complete output hashes, avoiding multi-million-value JSON arrays.

### SIMD scope

Exploratory default-CPU strong-preclean runs differed from the scalar algorithm
at 17x13, and the complete strongest graph differed at 16x8. Disabling CPU
dispatch removed these differences. Default 32x16 records matched. Go implements
the scalar mathematical operation; it does not reproduce these small-frame
SIMD artifacts. The scope of each stored record is explicit, and native
whole-video or all-dimension SIMD equivalence is not claimed.

## Renderer integration

The streaming cache reads each source once, includes future neighbors, and
holds at most the temporal window. Denoising precedes speed and Motion Blur.
It adds no trim and preserves timestamps/source correspondence. A combined
denoise/Motion Blur buffer estimate above 768 MiB is rejected before creating
output. Decoding and other image operations have additional memory costs.

RGB48 transport uses independent full-range BT.709 Y/Cb/Cr approximations at
10-bit. Filter deltas are transformed back and added to original RGB16 samples,
preserving unaffected fine detail. These conversions and the surrounding RAW,
ICC and HDR pipelines are not established as equivalent to native rendering.
This video option is separate from the approximate XMP noise-reduction sliders.

Tests cover cache ownership/bounds, read failures, preset defaults/overrides,
batch forwarding, CLI validation, AVI JPEG bypass/re-encoding, still-export
isolation, speed/tmix/sharpen/CAS/overlay/fade order and source/output retention.
Optional actual encodes cover all four levels with H.264 MP4 and H.265 MOV:
8-frame full-range 10-bit 4:4:4 outputs are probed and 16,384 decoded luma
samples per case are compared to the Go-filtered RGB reference. Encode/decode
maximum/mean tolerances are 4/3 codes for H.264 and 32/4 for H.265 (CRF 0 in
x265 still quantizes). These are distinct from the exact filter-plane checks.

Normal tests require neither LRT nor FFmpeg for the stored-reference checks.
To repeat every stored plane observation against the installed bundle:

```powershell
$env:LRTL_DENOISE_FFMPEG='C:\Program Files\LRTimelapse 7\ffmpeg.exe'
go test ./internal/lrtl -run '^TestOutputDenoiseLivePlaneReferences$' -count=1
```

Only independent implementation and synthetic observations are distributed.
References: official FFmpeg filter documentation for
[atadenoise](https://ffmpeg.org/ffmpeg-filters.html#atadenoise),
[vaguedenoiser](https://ffmpeg.org/ffmpeg-filters.html#vaguedenoiser),
[removegrain](https://ffmpeg.org/ffmpeg-filters.html#removegrain), and
[gblur](https://ffmpeg.org/ffmpeg-filters.html#gblur).
