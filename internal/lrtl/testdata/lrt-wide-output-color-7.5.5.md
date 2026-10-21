# Prepared BT.2020 and PQ video output

The native output profile now accepts prepared BT.2020 RGB images, preserves
their coded sample values through the supported Go working-plane pipeline,
and performs the measured final conversion/encoding with the existing FFmpeg.
No CGo library or additional dependency is introduced.

Input gamut can now be detected from the first selected image's ICC description.
See [ICC input recognition](lrt-icc-input-7.5.5.md) for native label rules,
explicit override behavior, parser tests, and the distinction from ICC color
conversion. The explicit input declarations below remain supported.

```powershell
# Prepared BT.2020 SDR intermediate images to BT.2020 SDR video
lrtl render C:\photos\prepared --output C:\photos\wide.mp4 --output-color lrt-7.5.5 --input-color bt2020 --output-gamut bt2020 --dynamic-range sdr --codec h265

# Prepared PQ-encoded BT.2020 intermediate images to PQ video
lrtl render C:\photos\prepared-pq --output C:\photos\hdr.mp4 --output-color lrt-7.5.5 --input-color bt2020 --output-gamut bt2020 --dynamic-range pq --codec h265
```

`--input-color` is an explicit interpretation of already prepared RGB samples.
It does not inspect ICC profiles or convert arbitrary sRGB/AdobeRGB/RAW data
to BT.2020. As in the measured native helper, BT.2020 SDR output interprets
the input transfer as BT.2020-10; PQ output interprets the input as PQ.
BT.2020-to-BT.709 SDR uses the native BT.2020-10 input interpretation. This
is not a general PQ-to-SDR tone mapper. Reexport suitable intermediary images
for the requested output. Go RAW development and sRGB pixel adjustments are
rejected for explicitly declared BT.2020 input rather than relabeling them.

## Native observations

- `lrt-color-plan-7.5.5.json`: all 162 existing source/gamut/dynamic-range/range
  helper observations now match the Go planner, including native errors and
  input metadata. Previously only the three ordinary BT.709 rows were used.
- `lrt-wide-pixel-format-7.5.5.json`: 48 new direct observations of the native
  software H.264/H.265 pixel-format selector. Wide gamut or HDR selects
  10-bit 4:2:0; 4:2:2/4:4:4 remain 10-bit. Helper-level combinations include
  values which the public Go render options deliberately reject as inconsistent.
- `lrt-wide-caller-7.5.5.json`: three full ordinary render command graphs for
  BT.2020-to-BT.709, BT.2020 SDR and BT.2020 PQ, with sharpening, native
  timestamps and fade. The output range/color conversion precedes the overlay.

The new observations came from reflection calls on the installed 7.5.5 build
1059 helpers, with null UI owners and synthetic paths. No native bytecode or
licensing state was changed. Only input/output data is committed.

Encoder metadata options were inspected separately in the installed command
caller. H.264 uses x264 color/range/transfer parameters, H.265 uses x265
parameters, and ProRes uses the prores_metadata bitstream filter. This also
corrects the previously omitted H.264/ProRes BT.709 transfer metadata in the
ordinary native SDR path. Independent video references now include these
caller options instead of relying only on the container color flags.

PQ output omits the SDR MOV gamma/write_gama/prefer_icc settings. H.265 PQ
uses the native fixed mastering-display and MaxCLL/MaxFALL parameters; tests
confirm that mastering-display and content-light side data are present in the
encoded first frame. Other codecs receive the native requested container
metadata, without claiming equivalent HEVC HDR side-data carriage.

Some native metadata conventions are inconsistent. H.264's native x264
transfer parameter remains BT.2020-10 even with PQ container flags. H.265
BT.2020-to-BT.709 SDR uses the native source-dependent BT.2020nc matrix tag.
The compatibility path preserves these observed caller parameters; they are
not assertions that every native codec combination is correctly calibrated
or universally suitable for HDR playback. H.265 is the path with explicit
mastering/content-light verification here.

## Verification

94 actual videos compare decoded samples and ffprobe dimensions, frame counts,
rates, pixel formats, primaries, transfer, matrix and range metadata:

- 84 cases: three output plans, plain and strong combined native temporal/
  spatial processing, H.264/H.265 with all three chroma formats and ProRes HQ,
  each at full and limited range;
- six cases execute the directly recorded caller graphs with native timestamp
  and fade, using H.265 at full/limited range;
- four 256x128 cases cover DNxHR HQX, SDR/PQ and full/limited range.

The ordinary native SDR video references were updated for the missing x264
and ProRes metadata. Preset/batch tests cover saved gamut/dynamic-range fields,
explicit CLI overrides, mandatory 10-bit capability checks, source retention
and invalid configurations preserving existing files. The pure Go planner is
fuzzed separately from the external encoder comparisons.

Saved `gamut`/`dynamicRange` values 0/1 map to the new options. PQ requires a
BT.2020 target; wide targets require BT.2020 source, detected from ICC or
declared explicitly, as the native conversion helper does. Standard output
remains SDR. Codec-specific
depth still applies to ProRes/DNxHR profiles; the 10-bit automatic switch is
for the measured H.264/H.265 selector.

ICC color conversion, Camera Raw HDR development, scene-linear HDR
adjustments, arbitrary input-transfer selection, general tone mapping and
complete licensed-application rendering remain unimplemented or unverified.
The pixel comparisons use opaque synthetic RGB16 PNGs and the selected
FFmpeg/CPU path on the measured Windows host, not arbitrary camera footage.

```powershell
go test ./internal/lrtl -run '^TestNativeWideColor' -count=1 -v
go test ./internal/lrtl -run '^$' -fuzz '^FuzzNativeWideColorPlan$' -fuzztime 10s
```
