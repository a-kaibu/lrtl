# LRT JPEG chroma subsampling decode measurements

`lrt-native-jpeg-subsampling-7.5.5.json` records full-frame cached-image
measurements from installed LRTimelapse 7.5.5 build 1059. It covers 4:2:2,
4:4:4, 4:4:0, 4:1:1, and 4:1:0 JPEGs. The 4:2:0 measurements and Deflicker
histogram settings are documented in [the cached-image oracle](lrt-native-deflicker-image-7.5.5.md).

The source is the synthetic `native-deflicker-measure-02.jpg` fixture. FFmpeg
8.0.1 creates the 4:2:2 and 4:4:4 variants at quality 2. ImageMagick
7.1.2-Q16-HDRI creates the other variants at quality 92 with explicit JPEG
sampling factors. LRT's `tQa.A(byte[])` decodes each output to the checked-in
PNG raster; the probe also measures the same JPEG through LRT's cached-frame
and static histogram paths with exclusion off. Those two LRT EV results must
agree within `1e-12`.

| Sampling | LRT Preview EV | Go RGB mean absolute delta | Max channel delta | Go Preview EV delta |
| --- | ---: | ---: | ---: | ---: |
| 4:2:2 | 0.49571749748815770 | 0.332 | 3 | < 0.0001 EV |
| 4:4:4 | 0.49321491040127800 | 0.206 | 2 | < 0.0001 EV |
| 4:4:0 | 0.49807190186550293 | 0.197 | 2 | 0.000684 EV |
| 4:1:1 | 0.49840236637889020 | 0.204 | 2 | 0.000314 EV |
| 4:1:0 | 0.49852666689564296 | 0.210 | 2 | 0.000373 EV |

The Go regression test verifies the JPEG component sampling ratio, decoded
RGB differences against the LRT raster, and Preview EV against LRT's direct
cached-frame value. It verifies centered interpolation for 4:2:0/4:2:2 and
nearest chroma selection for 4:4:0/4:1:1/4:1:0. Those three nearest-sample
cases use a 0.001 EV limit; their observed maximum delta is 0.000684 EV.
These are synthetic inputs re-encoded from an existing synthetic JPEG. Other
JPEG encoders and ICC profiles remain unverified.

Recreate the 4:2:2 and 4:4:4 inputs with FFmpeg, then produce the LRT decoded
rasters and measurement JSON with the included Java probe:

```powershell
ffmpeg -hide_banner -loglevel error -n -i internal/lrtl/testdata/native-deflicker-measure-02.jpg -frames:v 1 -q:v 2 -pix_fmt yuvj422p internal/lrtl/testdata/native-jpeg-subsampling-422.jpg
ffmpeg -hide_banner -loglevel error -n -i internal/lrtl/testdata/native-deflicker-measure-02.jpg -frames:v 1 -q:v 2 -pix_fmt yuvj444p internal/lrtl/testdata/native-jpeg-subsampling-444.jpg
magick internal/lrtl/testdata/native-deflicker-measure-02.jpg -sampling-factor 1x2,1x1,1x1 -quality 92 internal/lrtl/testdata/native-jpeg-subsampling-440.jpg
magick internal/lrtl/testdata/native-deflicker-measure-02.jpg -sampling-factor 4x1,1x1,1x1 -quality 92 internal/lrtl/testdata/native-jpeg-subsampling-411.jpg
magick internal/lrtl/testdata/native-deflicker-measure-02.jpg -sampling-factor 4x2,1x1,1x1 -quality 92 internal/lrtl/testdata/native-jpeg-subsampling-410.jpg
$classes = Join-Path $env:TEMP 'lrtl-native-oracle-classes'
New-Item -ItemType Directory -Force -Path $classes | Out-Null
javac -cp 'C:\Program Files\LRTimelapse 7\LRTimelapse.jar' -d $classes scripts/probes/LRTJPEGDecodeOracle.java
java -cp ('C:\Program Files\LRTimelapse 7\LRTimelapse.jar;' + $classes) lrtlprobe.LRTJPEGDecodeOracle internal/lrtl/testdata/lrt-native-jpeg-subsampling-7.5.5.json internal/lrtl/testdata/native-jpeg-subsampling-422.jpg internal/lrtl/testdata/native-jpeg-subsampling-444.jpg internal/lrtl/testdata/native-jpeg-subsampling-440.jpg internal/lrtl/testdata/native-jpeg-subsampling-411.jpg internal/lrtl/testdata/native-jpeg-subsampling-410.jpg
go test ./internal/lrtl -run '^TestLRTJPEGChromaSubsamplingMatchesNativeRaster$' -count=1
```
