# Native cached-image Deflicker measurements

`lrt-native-deflicker-image-7.5.5.json` records six synthetic JPEG cases measured
with installed LRTimelapse 7.5.5 build 1059. The probe constructs a frame with a
normalized reference area, puts the JPEG bytes in the frame's image cache, sets
the sequence's black/white exclusion options, and calls LRT's cached-frame
measurement path. It also records the direct histogram and preview measurement
results. The static edge-weight cache (`tQa.Da`) is cleared before each case.

The fixtures include full-frame and partial regions, exclusion disabled and
enabled, different black/white cut percentages, and hard-cut mode. Each source
image is generated as a deterministic RGB pattern, JPEG-encoded, then decoded
by LRT and saved as a PNG fixture. The input and decoded images are synthetic;
no user photographs or vendor executable code are included.

The Go test checks that LRT's cached-frame, static-histogram, and preview
measurement paths agree; the Go histogram calculation on LRT's decoded raster
matches the recorded value within `1e-10 EV`. The metering path now upsamples
subsampled JPEG chroma around centered sample sites before converting to RGB.
All six source JPEGs use 4:2:0 chroma subsampling. Additional 4:2:2 and 4:4:4
measurements are recorded in [the subsampling oracle](lrt-native-jpeg-subsampling-7.5.5.md).
Across these six cases, decoded RGB differs from LRT's AWT raster by at most 3
levels per channel (mean absolute difference below 0.25), and Preview EV differs
by less than `0.0024 EV`. Both `analyzePreviewEVSequence` and native Deflicker
measurement are exercised through their production entry points. These are
synthetic fixtures; other JPEG encoders, the less common 4:4:0/4:1:1/4:1:0
sampling ratios, color profiles, and LRT versions still need direct comparison.

The Java probe can be rerun on a machine with LRTimelapse 7.5.5 and JDK 17 or
newer:

```powershell
$classes = Join-Path $env:TEMP 'lrtl-native-oracle-classes'
New-Item -ItemType Directory -Force -Path $classes | Out-Null
javac -cp 'C:\Program Files\LRTimelapse 7\LRTimelapse.jar' -d $classes scripts/probes/NativeDeflickerImageOracle.java
java -cp ('C:\Program Files\LRTimelapse 7\LRTimelapse.jar;' + $classes) lrtlprobe.NativeDeflickerImageOracle internal/lrtl/testdata internal/lrtl/testdata/lrt-native-deflicker-image-7.5.5.json
```

Run the Go regression test with:

```powershell
go test ./internal/lrtl -run '^TestNativeDeflickerCachedImageMeasurementsMatchLRT$' -count=1
```
