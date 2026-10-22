# Long-term selection metrics, LRTimelapse 7.5.5

## Observations and corrected behavior

Recorded on Windows on 2026-10-21 with installed LRTimelapse 7.5.5 build 1059
and Java 25. The original `scripts/probes/LongTermMetricOracle.java` invokes
the installed frame-analysis helper on synthetic images. No application
implementation, image, preference file or binary is distributed.

The previous Go profile used an antialiased bilinear shrink, measured
luminance from the blurred grayscale image, and returned the unscaled
standard deviation. The native frame-analysis helper and caller use:

- **Luminance:** RGB histograms from the original image, independently
  smoothed with a radius-two box. Each bin divides the sum of available
  neighboring counts by the number of available bins using integer division.
  The mean across the three resulting histograms is divided by **256**.
  Zero surviving histogram mass falls back to 255 before that division.
- **Contrast:** a single-pass bilinear 64×64 sample, Gaussian blur with scale
  four, then **twice** the population standard deviation of grayscale values.
  Grayscale uses integer `(2R + 3G + B) / 6`, divided by 255.
- **Hue:** arithmetic mean of Java-style single-precision HSB hues of the
  same blurred sample. The CLI expresses this as degrees. Achromatic pixels
  contribute zero, rather than being omitted or using a circular mean.

The caller's luminance division and contrast multiplier were checked in its
bytecode; the oracle applies those scale factors to native helper results.
It does not run the entire GUI workflow, preview creation or temporal filters.

The histogram integer division is observable even for tiny inputs. A single
black pixel has no mass after smoothing and receives luminance 255/256. A
larger solid-black image has a small positive luminance because the smoothed
endpoint histogram includes bins one and two. Solid white is slightly below
255/256 for the same reason. The Go profile retains these measured behaviors.

## Pixel stages and alpha

The native shrink samples at pixel centers with clamped edges; it does not
widen its support during reduction. The Go implementation uses integer
bilinear weights, rounded premultiplied 8-bit channels, and the measured
24-bit reciprocal for unpremultiplication. The arithmetic is independently
implemented; the Java2D alpha-table arithmetic can also be inspected in
[OpenJDK AlphaMath.c](https://github.com/openjdk/jdk/blob/jdk-25%2B36/src/java.desktop/share/native/libawt/java2d/loops/AlphaMath.c).
No OpenJDK source is copied into the implementation.

Gaussian coefficients include the intermediate float32 normalization before
the final kernel normalization. Each separable pass rounds channel values
after promotion to binary64. The blur forces opaque alpha. Those details
remove last-component differences in the transparent checkerboard case.

## Reference coverage

`lrt-longterm-metrics-7.5.5.json` contains **112 cases**:

- seven source sizes: 1×1, 2×3, 17×11, 64×64, 65×63, 127×91, 257×193;
- white, red, grayscale ramp, checkerboard, deterministic RGB pattern,
  quadrant edges, one white impulse, and black;
- opaque RGB and varying-alpha ARGB inputs for each combination;
- native luminance, contrast and hue, plus complete 64×64 sampled and blurred
  RGBA stages, stored as zlib-compressed base64 data.

The tests compare **3,670,016 RGBA components** exactly across both stages
and compare each metric within 1e-12. CLI tests exercise all three filters,
the selection-manifest consumer, an actual filtered AVI, original-file
preservation and failure on a corrupt later image with an existing output.
XMP exposure before measurement, legacy version-one/two manifest decisions,
nonzero image bounds and deterministic, finite results are also checked.
`java-hue-rgb-25.json` adds 256 SHA-256 blocks covering **all 16,777,216 RGB8
colors**. Each block hashes Java `Color.RGBtoHSB` hue binary32 bits for one red
value, with green/blue in ascending order and little-endian byte encoding.
The Go test reproduces every block exactly. The independent public-API probe
is `scripts/probes/LongTermHueOracle.java`.

This does not establish native image decoding, ICC/16-bit color conversion,
RAW/Camera Raw preview equivalence, selection-region UI, or the long-term
temporal filter curves. The reference inputs are already normalized 8-bit
RGB/ARGB images. The ordinary `lrtl` metric profile remains a separate model.

## Manifest compatibility

New native-profile manifests include
`"metric_revision": "lrt-7.5.5-histogram-v1"`. Existing version-one/two
selection manifests remain readable, and their saved inclusion flags are
not recalculated when rendered. The historical JSON field
`contrast_stddev_0_to_1` remains for compatibility; this revision stores
twice the standard deviation there. Re-run analysis to obtain corrected
native metrics and decisions from an older native-profile manifest.

## Reproduction

```powershell
javac -d work/longterm-classes scripts/probes/LongTermMetricOracle.java
java -cp 'work/longterm-classes;C:\Program Files\LRTimelapse 7\LRTimelapse.jar' lrtlprobe.LongTermMetricOracle work/longterm-metrics.json
javac -d work/longterm-classes scripts/probes/LongTermHueOracle.java
java -cp work/longterm-classes lrtlprobe.LongTermHueOracle work/java-hue-rgb-25.json
go test ./internal/lrtl -run '^TestLongTermNative' -count=1
go test -race ./internal/lrtl -run '^TestLongTermNative' -count=1
go test ./internal/lrtl -run '^$' -fuzz '^FuzzLongTermNativeMetrics$' -fuzztime 10s -parallel 2
```

Normal Go tests read the synthetic references and need no Java or installed
application. The additional code adds no module dependency or CGo use.

Go 1.26.9 focused tests passed with the race detector and on Go 1.23.0.
A 10-second fuzz run passed 5,235 inputs. The focused ordinary tests exercised
100% of statements in the measurement, sampling, grayscale, HSB and blur
functions (93.8% in the kernel builder). This coverage describes executed
code, not proof of native equivalence beyond the observations above.

On 2026-10-22 the final `go test -json ./... -count=1 -timeout 20m`
passed with live installed FFmpeg, application-JAR and ExifTool references
enabled (661.034 seconds for the lrtl package). The latest focused tests,
including XMP exposure and legacy manifest loading, passed with the race
detector (5.965 seconds) and on Go 1.23.0 (0.641 seconds). `go vet ./...`,
the Windows pure-Go CLI build, and Linux amd64/arm64 cross-builds passed.
Both probes reproduced the stored reference files' SHA-256 hashes.

An earlier full run stalled in an existing JPEG/H.265 video comparison and
was interrupted by a failed debugger attachment. It is not counted as a pass.
The complete rerun above passed that same condition in 0.33 seconds; the
cause of the original external-encoder wait is not established or fixed by
this measurement change.
