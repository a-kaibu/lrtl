# Native JPEG input path

Even-sized native output now uses a private pure Go JPEG decoder and preserves
decoded Y/Cb/Cr samples into the working-plane pipeline. The former route
decoded with standard Go, converted to RGB16, and converted back to YUV. Both
IDCT rounding and that color round trip could change native samples.

## Decoder and source provenance

The private `internal/nativejpeg` marker/Huffman/progressive reader is adapted
from Go 1.23 `image/jpeg`, retaining the Go Authors BSD notices and license.
Its independent fixed-point cosine-matrix IDCT uses the row/column precision,
signed storage and level-shift convention measured in the installed LRT
FFmpeg 8.0.1. The package does not globally register or replace standard Go's
JPEG decoder. Standard output and existing RGB fallback paths retain their
prior decoder selection. There is no CGo or added runtime dependency.

A restart interval in a non-interleaved progressive scan counts individual
blocks, even when the frame's luma sampling factors group four blocks per MCU.
The adapted reader fixes that distinction. The new block/row restart fixtures
would fail without the correction.

The allocation limit is 64 megapixels, matching the renderer. Source JPEG
images and reference hashes are synthetic and redistributable; native
application code, installer data and camera photographs are not included.
See [decoder notes](../../nativejpeg/README.md) and
[fixture provenance](../../nativejpeg/testdata/README.md).

## Working planes

YCbCr JPEG stays in YUV. Chroma uses separable Lanczos weights quantized at
14 bits horizontally and 12 bits vertically, with integer intermediate/output
rounding, near-zero tap reduction and border accumulation before quantization.
Only a bounded window of horizontal rows is retained, rather than a full
second image. The 4:4:4 unscaled copy convention differs: full-range luma uses
bit replication, chroma uses a shift. Subsampled paths retain their measured
scaler convention. Gray JPEG uses replicated luma and neutral chroma.

## Evidence

- 32 generated inputs: baseline/progressive 4:4:4, 4:2:2, 4:2:0 and grayscale,
  qualities 30/75/95, 64×32 and odd 17×13 dimensions, plus block/row restart
  intervals. Raw decoded planes and YUV10 working planes compare exactly with
  the bundled FFmpeg under scalar and normal CPU selection.
- Separate raw/working SHA-256 observations run without FFmpeg installed.
- Another 32 raw-plane shape/subsampling conditions from 1×1 to 66×34 compare
  chroma kernels under scalar and normal CPU selection, including 4:4:0.
- Three complete native overlay graphs were directly observed at 64×32,
  including plain/sharpen/Motion Blur with watermark, timestamp and fade.
- 180 actual videos compare decoded samples and ffprobe dimension/count/rate/
  pixel-format/color metadata: 144 cover eight JPEG families × three plain/
  spatial/strong-temporal paths × H.264/H.265/ProRes HQ × full/limited range;
  36 use the directly observed overlay graphs with baseline/progressive 4:2:0.
- Decode selection, malformed/truncated input and oversized SOF headers are
  checked; decoder and working-plane fuzz tests exercise separate layers.
  A failed future JPEG preserves an existing destination and removes staged
  subtitles/video. The fps/audio/fade reference now decodes actual JPEG input
  with FFmpeg instead of masking the earlier difference with Go RGB conversion.

## Scope still open

The exact render comparisons use identity geometry and prepared JPEG samples,
with native output color and native overlays. Arbitrary resize/crop/rotation,
Go XMP pixel adjustments, standard Go overlays, CMYK/RGB JPEG color handling,
12-bit/lossless JPEG and full ICC color management are not established by these
tests. Odd-dimension kernel tests do not establish the complete odd-output
video route. These limitations remain visible in README and are separate
work toward complete rendering fidelity.

```powershell
$env:LRTL_COLOR_FFMPEG='C:\Program Files\LRTimelapse 7\ffmpeg.exe'
go test ./internal/nativejpeg ./internal/lrtl -run 'Test.*JPEG' -count=1
go test ./internal/nativejpeg -run '^$' -fuzz '^FuzzDecode$' -fuzztime 10s
go test ./internal/lrtl -run '^$' -fuzz '^FuzzNativeJPEGWorkingFrame$' -fuzztime 10s
```
