# Compatibility notes

These notes record observed interfaces and public workflow behavior used to design the independent `lrtl` implementation. They do not reproduce LRTimelapse code.

## Regional native resampling

The native geometry path now calculates only its final crop region on the
virtual scaled canvas. Contribution ranges use the complete canvas coordinate
system, preserving phase/support/edge folding and the order of floating-point
operations for each retained pixel. Intermediate horizontal rows are only
region-wide. Ordinary full resize uses the same implementation over the whole
region; identity retains the original image/color model, including YCbCr low
bits. The existing odd-input crop/copy behavior is unchanged.

Tests compare whole-resize-plus-copy with regional results across10 source
variants,8 sizes and5 regions, including8/16-bit RGBA/NRGBA, Gray/Gray16,
palette, generic Image, JPEG/YCbCr, nonzero bounds, alpha and identity. Four
new direct native scale/crop observations cover virtual canvases over64MP,
including4320x69120. Small windows use fewer than20,000 source samples and
do not construct those canvases. The final output, not virtual scale, is
limited to32768 per side and64MP. Motion Blur's separate budget still applies.

The previously rejected native6144x12288->6144x384 plan now runs through the
CLI and real H.264 encoding; decoded pixels/count/size and source preservation
are tested. Optional FFmpeg replay of its native scale/crop component checks
the constant16-bit plane. A one-iteration Windows/Go1.26.9 resampling benchmark
allocates21,709,096 bytes for that region, versus603,979,776 bytes for the
old full RGBA64 canvas alone. This is not total renderer/process memory.
The64x32 window benchmark allocates46,320 bytes on this machine. Neither
timings nor these machine-specific allocation totals are portable test gates.
Native color-management/complete encoder equivalence remains unproven.

## Fractional scheduling and native preset timing

36 further native mode-0 filters across12 fractional speeds and24/29.97/59.94
were executed by bundled FFmpeg8.0.1 for lengths1/3/11/31/101/257/1001. The252
maps cover166,242 output frames; Go agrees with every source interval and
count. `lrt-speed-fractional-7.5.5.json` losslessly encodes each monotonic source
map as cumulative ends, derived from actual frame IDs, not the Go equation.
Optional replay covers all36 filters at length11, checking every output pixel.
CLI raw frames and SRT labels are compared with selected stored mappings.

`lrt-native-timing-7.5.5.json` records96 separate direct native graph calls,
including video/audio fade and three-decimal atrim.24 nonempty cases have
different fade timing from an actual-frame-count plan. Preset import now sets
NativeTiming independently of NativeGeometry and explicit CLI overrides.
Duration uses int(Float(sourceCount)/Float(speed)) minus the trim window,
then Float division by displayed FPS. Video counts and SRT remain actual.
Standalone CLI retains the prior actual-count timing; image exports clear
NativeTiming. Rational FPS labels retain the existing lrtl fallback extension.

Native audio is allowed to drain to its filter end, without video-frame,
output-time or shortest-stream caps. Raw-video EOF still supplies the exact
scheduled frame count; atrim makes padded audio finite. Actual H.264/AAC tests
confirm11 sources at1.1x/24fps produce9 frames/0.375s video and0.417s audio,
with native fade gain on decoded video and PCM, including the short audio tail.
Source/preset retention, preset/manual dimension overrides, batch forwarding,
and actual RGB48 fade wiring are tested. These are component observations and
our own encoder integration, not a complete native LRT export comparison.

## Crop placement

`testdata/lrt-crop-position-7.5.5.json` stores288 direct ordinary renderer
builder calls: six dimensions, resolutions0/32, aspects.5/1/1.7777778, and
positions0/1/25/33/50/67/99/100. Native coordinates use truncation of
float32(excess)*float32(position/100). All observed crop rectangles match.
The new `render --crop-position` option and batch forwarding apply this to
native post-scale geometry and to standalone pre-scale aspect cropping.
Omitted Go API pointer and omitted CLI flag preserve center50. The literal
CLI center retains exact integer division even for excess sizes above2^24.

No saved crop-position field was identified in the inspected settings model;
the observed native batch setup passes literal50. No invented JSON import is
added. Positions are explicitly supplied by CLI/Go API. Render-only CLI
exposure is intentional; export/preview command flags remain unchanged.
Go callers using shared RenderOptions can pass the pointer to image exports.

Tests cover every native rectangle, precision/alpha, nonzero source bounds,
endpoints/default/no-op, both geometry paths, Motion Blur order, exact AVI
JPEG preparation, batch/default forwarding, invalid-value failure retention
and source/preset retention. Actual MP4 decoded RGB tests distinguish left
and right crops. These comparisons do not establish full native color-managed
rendering or saved-UI-state equivalence; XMP crop animation is independent.

## Native preset geometry

`testdata/lrt-geometry-7.5.5.json` records480 direct ordinary renderer-builder
calls:12 source dimensions, resolutions0/16/32/720/1080, and8 aspect choices.
The independent planner matches every recorded scale size and crop rectangle,
including native degenerate rectangles; those are rejected before publishing.
NativeGeometry is enabled by preset import unless output-height/long-edge is
explicit. Explicit aspect alone participates in the native geometry planner.
Standalone CLI geometry retains its established literal-height/long-edge rules.

The native path uses float32 aspect arithmetic, snaps near standard ratios,
compares hundredths, restores dimensions within four pixels of the canvas,
then aligns width to4 and height to2. Native requested resolution acts as a
short-edge target for portrait aspect. Original0 uses the source canvas. The
normal graph first removes odd raw dimensions, scales, then center-crops.
Go expresses the initial crop in oriented coordinates, and tests all eight
EXIF orientations against cropping the raw source before orientation.

Captured RGB48 integration tests use native recorded rectangles and cover
landscape/portrait/odd/Original/aspect changes, Motion Blur, preset and batch
forwarding, explicit dimension overrides, source retention and failure output
retention. Actual MP4 tests verify32x24->20x16, portrait32x64->32x64, and odd
33x25->32x24. Final output dimensions are capped at32768 per side and64MP;
virtual scale dimensions are not capped by that allocation limit now that
only the output region is computed. This is a Go resource limit, not a native
observation. Non-center placement
is now covered separately above. Saved UI state and full Camera Raw/color
pipeline parity remain unverified. XMP rendering is still an independent path.

## Lanczos output resampling

Six native graph-builder calls recorded in `testdata/lrt-scale-filter-7.5.5.json`
confirm `scale=...:out_range=full:flags=lanczos`. FFmpeg documents default
Lanczos width3 (https://www.ffmpeg.org/ffmpeg-scaler.html). The former Go
Catmull-Rom output resize is replaced with an independent separable Lanczos3
filter: half-pixel centers, expanded support on downsampling, normalized
weights and out-of-image weights folded onto the nearest edge sample.
Premultiplied RGB/alpha is combined in float64 without clipping between axes;
the result is unassociated/clipped only at the final pixel. A sliding row
cache avoids a full-image four-channel float64 intermediate and reuses rows.

24 actual bundled FFmpeg 8.0.1 gray16le scale observations contain 16,338 output
samples (six patterns/four resizes). Their pooled RMSE drops from980.479 for
the former Catmull-Rom filter to44.413 for the new implementation. Stored
vectors, not the production equation, supply reference values. Optional
external replay checks the complete stored planes. Tests also cover16-bit
constants, identity, nonzero bounds, alpha/transparent color, extreme shrink
ratios, edge folding, normalization and source retention. This is a grayscale
component comparison; native color-managed RGB/YUV/video parity is not proven.
Residuals are largest on high-frequency upscaling. See fixture provenance.

The six native scale observations are now also matched by the saved-preset
geometry planner above. Literal CLI height retains different semantics by
design, with explicit override of presets. GUI/XMP geometry resamplers are
unchanged. The overall compatibility goal remains incomplete.

## Motion Blur warm-up trimming

`testdata/lrt-motion-trim-7.5.5.json` records 48 isolated ordinary native graph
builder calls: window 0/2/3/4/5/10/15/20, speed 0.5/1/2, source count 6/31,
24fps, fade enabled for one second. Native tmix is followed by
`trim=start_frame=N:end_frame=int(float(sourceCount)/float(speed))` and
`setpts=PTS-STARTPTS`. It drops N resampled frames, rather than N-1 input
frames. Trim bounds and the native video fade fragment are stored verbatim.
The bundled FFmpeg 8.0.1 executes the extracted native speed/fps/tmix/trim
chain on synthetic 10-bit constant-luma frame IDs; every resulting frame's
luma sample is stored separately. Empty native outputs are recorded too.

Pure Go now warms the mix queue, removes that prefix, and uses the remaining
count in AVI headers, FFmpeg video frames and the CLI summary. Standalone
audio/fade uses that count; preset duration estimates are described above.
The native float32 end bound also applies when it is shorter than
the existing fps schedule. Empty timelines fail before creating/replacing an
output. All sources are still prepared/validated, including discarded ones.
Subtitles accept `--motion-blur N`, use the same count/source mapping and
restart cue numbering/timestamps at zero. Image exports intentionally retain
all source correspondences and therefore do not use the video trim.

Stored count and fade observations match; optional FFmpeg replay verifies all
48 temporal chains. Captured Go RGB16 results agree with the synthetic native
Y10 samples within half a Y10 code, reflecting their different rounding
precision. This is not a whole-frame native color-management equivalence test.
Actual MP4 testing confirms 20 source frames at 2x, window2, 4fps produce eight
frames and two-second audio/video, with fade matching the shortened duration.
AVI source alignment, SRT labels/times, intermediate-image preservation and
failure retention are covered separately. This changes the prior untrimmed
video behavior for Motion Blur; no opt-in compatibility mode is needed.

## Output sharpening

Four isolated native filter-builder calls establish the on/off mapping to
`unsharp=luma_msize_x=7:luma_msize_y=7:luma_amount=0.4`, for both saved video
ranges. LRT's normal graph places it after speed/tmix and before overlays/fade;
its input is already yuv444p10le. Six synthetic 32x16 10-bit luma planes were
processed by the installed bundled FFmpeg 8.0.1; all 3072 samples match the
independent Go plane kernel. See the adjacent fixture provenance notes.

The measured high-depth arithmetic has a much weaker gain than the 8-bit
path: one rounded 12-bit binomial convolution followed by a signed 24-bit
fixed-point gain shift. Go intentionally matches these measurements rather
than applying an ordinary 0.4 gain directly to RGB16. Output rendering derives
full-range BT.709 luma independently and adds the resulting difference to
RGB48. This conversion, channel clipping, and its position relative to final
YUV/range conversion are approximations; native whole-video parity is not
established. Very small plane widths and other FFmpeg versions are not covered
by the stored native plane comparison.

`--sharpen` and saved `sharpen` work with explicit false override and batch
forwarding. The Go effect runs after resizing/speed/temporal blending, before
overlays and fade. TIFF/preview image exports clear this video-only setting.
Tests cover measured planes, 16-bit precision/chroma/clipping, alpha composition,
buffer ownership during duplicate frames, ordering, AVI JPEG bypass/encoding,
preset overrides and retention of source/XMP/preset/existing output files.

## Contrast adaptive sharpening

The native render builder appends `cas=strength=0.90` after unsharp and before
range conversion and overlays. Five direct string-helper observations are in
`testdata/lrt-cas-filter-7.5.5.json`. The gated full-render feature was not
enabled or modified to obtain these observations.

The independent pure Go 10-bit scalar CAS kernel matches 24 synthetic planes
processed by the installed bundled FFmpeg 8.0.1 (12,288 samples). Stored
observations cover eight patterns at three strengths; optional live tests
repeat the comparison on every YUV444 plane. Production uses strength 0.9.
Single-pixel/thin frames, float32 behavior, clipping and ownership are tested.
See `testdata/lrt-cas-7.5.5.md` for provenance and references.

`render --cas` and saved `cas` apply to AVI and external-encoder video,
including batch renders and explicit false overrides. Speed, Motion Blur,
ordinary sharpening, CAS, overlays and fade are checked for order and frame
duplication. Invalid flags preserve existing output; TIFF/preview exports clear
this video-only control. The RGB48 path derives approximate full-range BT.709
Y/Cb/Cr, filters all planes and adds inverse-transformed deltas to preserve
unaffected RGB16 detail. Native full-video/color-management parity and HDR are
not established by the scalar-plane comparisons.
Optional actual H.264 MP4/H.265 MOV tests also check format, frame count and
6,144 decoded luma samples per codec against the Go-filtered RGB input, with
bounded encode/decode error documented in the fixture notes.

## Output denoising

Four direct native graph-helper observations establish the saved denoise levels:
adaptive temporal averaging with windows 5/5/7/13, followed by CDF 9/7 wavelet
soft/garrote thresholding. The strongest level adds a twice-median/Gaussian
lighten preclean branch, which negotiates an 8-bit intermediate. Go implements
these scalar operations independently and connects them before speed/tmix.
The application feature gates were not modified to obtain the graphs.

`testdata/lrt-denoise-planes-8.0.1.json` stores 354 observations and 644,058
output values. Each record distinguishes default CPU dispatch from scalar
`-cpuflags 0` execution. All recorded values match. Three additional complete
output hashes cover 6/6/10 wavelet steps and 4,281,091 samples. The fixture notes
describe small-frame SIMD differences (strong preclean at 17x13 and complete
strong graph at 16x8) that Go does not reproduce. Default 32x16 graph samples
match, but these tests do not establish all-dimension/native-render parity.

The bounded streaming cache validates future frames, reads each source once
and follows the measured EOF padding behavior. Denoising adds no trim. Tests
check source ownership, speed/Motion Blur/sharpen/CAS/overlay/fade order,
preset defaults/overrides, batch forwarding, AVI encoding/JPEG bypass,
still-export isolation, invalid settings and source/output preservation.
Eight optional actual H.264 MP4/H.265 MOV cases decode 16,384 luma samples each
against the Go-filtered RGB reference with bounded encoding error.

Like CAS, this path uses independent approximate full-range BT.709 RGB48/YUV10
conversion and adds filter deltas to original RGB16 samples. Complete native
color management, Camera Raw and HDR equivalence remain unverified. See
`testdata/lrt-denoise-7.5.5.md` for exact settings, provenance and reproduction.

## Transient light-trail removal

The installed static helper supplies three graphs: 3/3/5-frame temporal medians,
with sigma-15 luma detail and highlight LUTs for medium/strong. Their default
LUT chroma expressions clip YUV10 to 64..960. The local full builder did not
activate this gated feature; no gate was changed. Caller head/tail/duration rules
were inspected separately. These scopes are distinguished in the fixture notes.

`testdata/lrt-airplane-planes-8.0.1.json` stores 102 observations, 323,253 output
samples and PTS traces. Default CPU 32x16 and scalar 17x13 plane outputs match
Go exactly. tmedian emits N-2r frames, timestamps each from the window's first
source and does not pad endpoints. At speed 1, native head trimming normally
leaves N-3 light/medium frames or N-5 strong frames, without Motion Blur.

At other speeds, setpts/fps propagates the original EOF time and holds the final
median image through that end. The Go source schedule models this hold before
speed/tmix/sharpen/CAS/overlays/fade. The caller trim end still uses Java float
division of the original count; head trim is max(MotionBlurWindow,1), tail bound
is 1. A hundred helper/caller-composition graphs executed by the native bundle
yield 62 nonempty cases (496 frames) and 38 empty/rejected cases. All Go frame
counts match, and direct median-ID tests cover the held tail. Native-style
output `-r 24` retained those counts.

Presets retain original-count estimates for audio/fade, independently of actual
median output. Tests check SRT anchors/counts, bounded cache ownership, future
errors, denoise/median/time/overlay order, preset defaults/overrides, batch,
JPEG AVI, still exports and source/output retention. Eighteen actual H.264/H.265
videos verify counts/durations/decoded luma; an audio test verifies the longer
native estimated tail and fade. RGB48/YUV10 conversion remains independent;
full native application/color-management equivalence is unproven. See
`testdata/lrt-airplane-7.5.5.md` for parameters, provenance and limits.

## Explicit video range

Three native color-filter-builder observations in
`testdata/lrt-video-range-7.5.5.json` establish full output for videoRange==1 and
limited output otherwise, with full input range. Preset imports now apply that
choice; `--video-range auto|limited|full` provides explicit override. Auto keeps
the previous encoder path. Explicit ranges use RGB48->GBR16->zscale YUV709,
output range signaling and colr matrix metadata. They require libzimg/zscale.
Image exports clear the video-only option; AVI accepts auto/full only.

Optional actual-video tests extract decoded luma from black/white/midpoint
16-bit inputs and inspect metadata for H.264/H.265/ProRes/DNxHR. Limited output
has standard 8/10-bit endpoints; H.264/H.265/DNxHR full range has full endpoints
and pc signaling. The measured ProRes encoder clamps full 10-bit endpoints to
4/1019, and its range is not reported by ffprobe; colr ensures the matrix is
still available. The fixture notes record those limits and distinguish native
component observations from our own FFmpeg pipeline. Full native color-managed
pixel equivalence and transfer/ICC/HDR handling remain unverified.

## Playback speed scheduling

`--speed` supports 1/16 through 16 in render, batch forwarding and subtitles;
saved native speed is imported with explicit CLI override. The normal mode-0
native helper emitted 20 PTS/fps filters; executing them with FFmpeg on short
frame-ID sequences produced 80 mapping cases. Go matches every count and
source index. See `testdata/lrt-speed-7.5.5.json` and its adjacent notes for
native versus FFmpeg observation boundaries and the precise rounding rules.

Scheduling occurs after source preparation and before temporal blending. Every
source is still validated even if it will be dropped; repeated outputs reuse
prepared RGB data. Captured overlay dates and SRT labels use the source index,
while encoder frame count uses the actual output count. Standalone fade/audio
use actual count; preset timing now follows the separate native estimate above.
Full combined color-managed pipeline parity remains unverified. Tests cover RGB48 identity and XMP,
blend/fade order, AVI pixel/count output, metadata/subtitles, preset/batch
forwarding, invalid/empty schedules, corrupt dropped inputs without publication,
and optional actual NTSC H.264 metadata. Experimental interpolation stays
unsupported, and non-unit speed AVI may recompress JPEG data.

## Fade out during rendering

Sixteen direct filter-builder calls are recorded in
`testdata/lrt-fade-7.5.5.json`. They confirm single-precision duration from the
displayed rate, clamping a fade longer than the video, three-decimal start
formatting and identical video/audio filter timing. The adjacent notes state
the inputs and limits; no native encoder or GUI was launched. The Go planner
matches all observed starts and float32 durations.

`--fade-out SECONDS` now applies to pure-Go AVI video and FFmpeg MP4/MOV video
and audio. Native fadeOut/fadeTime imports are connected; explicit CLI values
override them, including zero to disable. The Go video operation scales RGB
after Motion Blur and overlays, preserving 16-bit data; the audio path appends
afade after its existing padding/trim filter. TIFF/preview image exports clear
the video-only setting. Frame-count/index preservation, source retention,
early JPEG passthrough, layering, invalid values, preset/batch forwarding and
real decoded audio/video are regression tested. Native color-managed YUV pixel
equivalence remains unverified; Motion Blur trimming is covered separately above.

## Saved native render presets

`render --lrt-render-preset FILE` imports a native RenderPreset JSON object or
the lastRenderSettings member of a native settings JSON. `--lrt-preset-name`
selects one exact unique name from its renderPresets array. Native settings
declare those members separately from renderAR/renderConstrainCrop; the loader
also applies that global aspect constraint when enabled. It neither discovers
nor writes the native global settings. Explicit CLI values win for supported
render controls, and audio paths may be overridden or disabled explicitly.

Six native serialized objects and measured H.264/H.265 CRFs are in
`testdata/lrt-render-preset-7.5.5.json`; the adjacent notes distinguish direct
observations from inspected encoder profile switches. Go maps fps, height,
quality/codec profiles, Motion Blur and audio to its existing renderer. Known
unsupported enabled filters, interpolation and color/HDR modes fail
instead of being silently discarded. H.264/H.265 imports support 420, 422 and
444 sampling as documented below.
New ProRes Proxy/LT/4444 backends cover the native profile choices alongside
the existing 422/HQ paths. FFmpeg receives 10-bit YUV; its 4444 decoder reports
12-bit output, which is not evidence of full 12-bit input precision or alpha.

Regression tests compare explicit render controls with imported settings at
the RGB/input-argument boundary, check global/named presets, overrides, source
and output retention, batch forwarding, invalid JSON/options and optional real
ProRes and 1080p NTSC H.264 output through ffprobe. Complete native rendering
and color-management parity remain outside this measurement.

## H.264/H.265 chroma sampling and bit depth

The installed 7.5.5 encoder helpers were invoked with synthetic SDR presets:
two codecs, three sampling modes, three quality values and two video ranges.
All 36 observations are retained in `testdata/lrt-h26x-format-7.5.5.json`,
with provenance in the adjacent Markdown file. Both codecs select 8-bit
`yuv420p` for 420 and 10-bit `yuv422p10le` / `yuv444p10le` for 422 / 444.
The native extension helper selects MP4 for H.264 and MOV for H.265.

`--subsampling` and saved preset imports now follow those formats. H.265 MOV
joins the existing H.265 MP4 path. Before opening output, encoder help must
confirm support for the exact pixel format, preventing an unnoticed FFmpeg
format substitution. Explicit SDR range conversion uses the selected depth.
ProRes and DNxHR retain their codec-specific formats; still exports clear this
video-only option. HDR and complete native color-management parity remain
unsupported.

Tests cover all 36 observations, CLI/preset/batch forwarding, invalid inputs,
capability failures and source/output retention. Optional actual-encoder tests
cover 18 container/sampling/range combinations, checking ffprobe metadata and
decoded luma from a 16-bit gradient. The measured 10-bit outputs retain over
700 distinct luma levels per row; these tests do not compare full native renders.
Two further H.265 MOV/MP4 encodes verify two-frame PTS/DTS values. Outputs
shorter than three frames disable B frames to avoid the uninitialized delay
timestamp in older x265; the fixture notes link the upstream implementation.

## Fractional output frame rates

Nine direct observations of the installed `mQa` label parser confirm /1001
NTSC meanings for 23.976, 29.97, 47.952 and 59.94, alongside integer controls.
The synthetic fixture and measurement limits are recorded in
`testdata/lrt-frame-rate-7.5.5.json` and its adjacent notes. lrtl now accepts
decimal/fractional rates in render and subtitle commands, passes reduced
fractions to FFmpeg, and keeps audio trimming and SRT timing on the same rate.
Explicit fractions are literal; decimal NTSC labels are aliases. SRT rounding
uses the absolute frame index with integer arithmetic, avoiding accumulated
floating-point or per-frame rounding drift.

The pinned pure-Go MJPEG writer exposes integer FPS only. lrtl finalizes its
temporary AVI, verifies that writer's known fixed header layout, and updates
the main interval and video stream scale/rate before publication. It does not
parse or modify arbitrary input AVIs. Tests normalize only the generated strn creation timestamp, then compare all remaining bytes with
an integer-rate control, including JPEG payloads and idx1 entries. Optional
FFmpeg/ffprobe integration verifies actual stream rate/count/duration for AVI,
MP4 and MOV, plus AAC padding from short audio to rational video duration.

Investigation of aspect-aware radial geometry also located the `hSa` helper.
The inspected automatic worker reaches its conversion/restoration branch only
behind `LRTimelapseMain.I()`, which checks an internal development setting and
additional state. The ordinary alternative calls the measured angle helper.
No setting or gate was modified. Aspect geometry remains unimplemented, and
it should not be described as a proven omission from the normal runtime path.

## Measured mask-angle postprocessing

`testdata/lrt-mask-angle-7.5.5.json` records 24 direct invocations of the installed
void `K(Rsa)` helper with sequence mask versions 1 and 2. Its 144 observed cells
confirm exact +/-45 boundaries, single-step +/-90 adjustments, missing-value
retention, Circular 1/2 versus MaskGroup 8/9 dispatch, and unaffected modern
slots 1/10. Go compares 72 results for the corresponding pure layout across
three RDF representations, including serialization and reparsing (216 checks).
CLI tests additionally cover dry runs, original backups, image preservation and
stable repeats adjacent to the boundaries. Details and limits are recorded in
the adjacent fixture notes. The native helper selects one layout per sequence.
lrtl now applies that dispatch to selected values, missing columns, reference
copies and spline samples, including mixed-layout XMP. The same native fixture
is compared across all six columns and three RDF forms (432 mixed-layout
checks). Aspect-aware radial geometry remains separate and unresolved.

Native `Rsa.A(File)` checks the first image's effective XMP for MaskGroup first,
then Gradient. Six direct observations in `testdata/lrt-mask-format-7.5.5.json`
confirm that empty arrays count, MaskGroup wins when both exist, and Circular
or Paint alone do not identify a legacy sequence. Missing detection falls back
to the application preference; the probe explicitly sets that preference to 2.
lrtl uses modern as its fallback and offers `interpolate`/`batch-interpolate
--mask-format auto|legacy|modern` to specify the intended preference explicitly.
It does not read or change LRT's global settings. Auto detection examines only
the first frame in lrtl's existing sequence order. That ordering and its
sidecar/embedded-XMP discovery remain independent of the native folder loader.
The native detector probe reads synthetic opaque NEF placeholders plus valid
sidecars and invokes no GUI, image decoder or complete folder-loading workflow.

## CLI frame selection and sequence copies

`mark-sequence` and `unmark-sequence` share the selected-packet preflight used
by `set-sequence`. Marking uses the existing 4/5-star mapping and saves the
original rating once, including an absent rating. Changing kinds retains that
original. Bulk removal skips unowned markers unless `--force-rating` requests
clearing external 4/5-star ratings; HG ratings and internal HG marks survive.
An owned marker subsequently changed to HG is rejected before any writes.
These commands extend CLI workflow coverage; they are not a new claim of native
writer equivalence. Tests check restoration, selection inversion, idempotence,
backups, shared outputs, late preflight failures and embedded image retention.

`set-sequence` now applies the existing scalar/PreviewEV/local-mask editing rules
to selected frames. It shares root numeric parsing with single-image `set`,
prepares all candidates before writing, deduplicates shared-sidecar outputs and
rejects an edit that would affect an unselected image through a shared XMP.
Both candidate naming variants are checked: updating the common stem could
otherwise overtake an unselected image's currently active private sidecar.
Dry runs are the default; writes use original-content backups unless explicitly
disabled. Image bytes, embedded metadata and target-owned mask IDs survive.
Unchanged candidates produce no write or extra backup. Tests include per-image
XMP controls, selection/inversion, negative values/ratings, mask/internal-role
validation, late parsing failures, embedded packets and AVI output controls.

An observed sidecar-precedence regression was also fixed in the common writer:
when both naming variants have future modification times, writing the chosen
file with the current clock time could make the stale companion newest. Existing
future-dated outputs now advance by two seconds before publication, retaining
the effective sidecar and providing a changed timestamp on coarse filesystems.
The regression test checks both fresh discovery and the saved effective value.

The existing feature inventory includes selection, inversion and copying photos.
`select` now resolves case-insensitive filename, XMP rating and existing manifest
filters as an intersection, optionally inverted against the complete sequence.
It stores resolved membership in the existing version-2 selection format with
image size and modification time. Empty manifests can be inverted by these new
commands; the existing render/export all-excluded guard is preserved.

`copy-sequence` plans selected image files and their active sidecars using the
existing discovery/sidecar precedence, preserves names and modification times,
and copies shared stem sidecars once. It requires a new output outside the
source directory and publishes that folder after successful copying and source
identity/size/mtime checks. It does not recreate native workflow settings or
copy preview caches. Image development and XMP rewriting are unnecessary.

Tests cover filter intersections/inversion, missing/rejected/embedded ratings,
external-sidecar priority, stale manifests, shared sidecars with case-folded
paths, read-only files, symlink boundaries, real copy failures, source changes
and destination collisions. Copied selected bytes and MJPEG AVI frames/index
match the original selection; the same manifests also drive TIFF and JPEG
exports. This implements the documented workflow independently, without a
native copy UI/file-attribute parity claim. Move/remove/delete are still absent.

## Missing correction resources and RDF validity

Root range materialization is now compared with 144 installed-runtime cases in
`internal/lrtl/testdata/lrt-missing-range-7.5.5.json`: four correction families,
positions 1/2, five numeric range columns plus LumRange text, and absent/empty
range structs or absent parents. Registered root range paths can create both
their correction array and range struct without another parent scalar column.
Own PointModels survive, while source sample payloads are not copied. Malformed
text/array/attribute range representations fail before writes. The optional
native parser harness now accepts 190 generated packets. The probe boundary
and metadata-stamping tail exception are described alongside the fixture.

Follow-up selector observations are recorded in
`internal/lrtl/testdata/lrt-missing-selector-7.5.5.json`: forty cases cover
Gradient/Circular/MaskGroup first selectors, slots 1/2, absent/empty arrays and
items, and creation after a separately filled parent exposure column. Existing
parent IDs and exposure survive; source selector IDs and What are not copied.
New nested arrays use Bag and existing Seq arrays retain their form. Root
numeric/boolean availability is prepared before selector columns so one pass
produces the final proposed structure. The new path is restricted to registered
linear/radial coordinates and Flipped, excluding brush, selector-level ranges, SyncID fallback
and later selector components. Tests also exercise repeated CLI runs, original
backups, zero/boolean-only parents, and later structural failures without any
earlier file writes. The same native-writer metadata-tail limitation applies.

The installed missing-column helper and frame-to-XMP writer were exercised on
synthetic columns whose middle frame had no correction array. Sixteen
observations cover four correction collections, slots 1/2, and separate numeric
LocalExposure2012 and boolean CorrectionActive columns. Both values materialize
in an RDF Bag without cloning source IDs or unrelated trees. Details and the
probe's metadata-stamping tail failure are documented in
`internal/lrtl/testdata/lrt-missing-correction-7.5.5.md`; these component
observations do not establish a complete native Auto-Transition/save workflow.

lrtl now fills missing selected root corrections as well as fields on existing
resources. Filled numbers become anchors before interpolation. Missing selected
first selectors now receive registered linear/radial fields and Flipped when
their parent exists or can be prepared; missing selector-level range resources remain excluded; registered root ranges now materialize from scalar columns.
Tests compare all sixteen observed property
values and array sizes, check a CLI sequence against an independently prefilled
keyframe, and verify dry runs, original backups, own IDs, unrelated metadata,
and no-op repeats.

During the probe, Adobe's XMP parser rejected property attributes combined with
`rdf:parseType="Resource"`. lrtl's new-field writer now emits property children
on such elements. Normal tests check generated RDF shape; the optional
`TestGeneratedMaskResourcesAdobeParser` uses Java/javac and `LRTL_NATIVE_JAR`
to parse 190 generated packets in the installed runtime: modern/legacy
initialization, four missing-correction collections, forty first-selector cases, and 144 root-range cases. The runtime probe and
parser are development checks only, with no Java dependency in the Go CLI.

## Saved Deflicker controls

`--lrt-deflicker-settings` imports the modern numerical controls from each
sequence's `.lrt/lrtsequence.json`: `deflickerSmoothingFactor`,
`deflickerAccuracy`, `deflickerConstant`, `deflickerMultipass`, and
`deflickerPasses`, plus `filenameSort`. It selects the observed 7.5.5 correction
profile. Multipass disabled means one pass; enabled counts must be in the CLI's
supported 1–8 range. Complete input and matching positive `imageCount` are
checked. Sequence fields override global defaults; missing smoothing, accuracy,
constant, multipass, and enabled pass count come from the explicitly selected
`--lrt-preferences FILE`. Missing `filenameSort` uses the observed
`SequenceProperties` constructor default `false`, which sorts by `xmp:CreateDate`.
If a needed workflow default is absent from both files, processing fails before
writing. Old legacy smoothing/constant fields are ignored.

Six synthetic runtime serialization observations are in
`internal/lrtl/testdata/lrt-deflicker-settings-7.5.5.json`. Tests compare imported
controls against explicit CLI values for final XMP, including heterogeneous
parallel batches, saved date ordering, and optional reference boxes. Malformed
or incomplete settings and conflicting explicit controls fail before any XMP
write. Settings and preview caches remain unchanged.

`deflickerIgnoreBlacksWhites` chooses global preference (0), enabled (1), or
disabled (2); percentages and hard-cut mode are global preferences outside this
sequence file. Inspection of the installed 7.5.5 `SequenceProperties`
constructor showed that absent `filenameSort` initializes to `false`, absent
histogram mode to `0`, and the five numerical/workflow controls initialize from
`LRTSettings.defaultDeflicker*`. Inspection of the installed user's JSON
`LRTimelapse.config` confirmed those five defaults and the four global histogram
field names and types; the committed preference fixture uses synthetic values
and contains no user config. `--lrt-preferences FILE` reads all nine supported
fields from the explicitly selected file.
Mode 0 uses those defaults, mode 1
forces exclusion on and uses the saved thresholds, and mode 2 disables exclusion
even when the global preference enables it. Explicit histogram flags override
the corresponding global fields; explicit flags conflict with mode 2. Without a
preferences file, mode 1 still requires explicit thresholds. The input file is
read-only. Tests cover sequence-key omissions, preference fallback, constructor-
default date ordering, final XMP equality against explicit saved controls,
preference schema and bounds, override precedence, and parallel batch output.
This is control-file interoperability; it does not establish native Camera Raw
or multipass pixel equivalence.

## Local installation observations

- The installed version is LRTimelapse 7.5.5 under `C:\Program Files\LRTimelapse 7`.
- `LRTimelapse.cmd` starts the bundled Java runtime with `-jar LRTimelapse.jar`; the JAR manifest names `lrtimelapse.LRTimelapseApp` as its entry point. The GUI EXE is a 32-bit PE launcher analyzed with Ghidra, rather than the main metadata-processing implementation.
- The installation includes separate `exiftool.exe`, `ffmpeg.exe`, and `dnglab.exe` programs. The installed DNGLab reports version 0.7.2; `dnglab analyze --srgb <RAW>` writes a 16-bit sRGB TIFF to stdout. `lrtl render --raw-develop --dnglab PATH` can call a user-selected DNGLab executable through this public CLI and consume that TIFF. It does not depend on or redistribute the copy bundled with LRTimelapse. The upstream DNGLab project identifies itself as alpha software and says its Windows binary is untested, so camera support and output stability remain external limitations.
- The installed DNGLab 0.7.2 also exposes `dnglab convert <INPUT> <OUTPUT>`, with lossless or uncompressed compression, optional preview/thumbnail and embedded-RAW controls, and a crop choice. The optional `import --convert-dng --dnglab PATH` path invokes one DNGLab conversion process per supported RAW image, requests lossless or uncompressed output, keeps the source modification time when available, and copies any adjacent XMP sidecar alongside the converted DNG. It does not invoke or redistribute the executable from the LRTimelapse installation; users supply their own DNGLab. DNGLab's alpha status and untested Windows build apply to this conversion path as well.
- A current upstream review found that DNGLab is a Rust RAW-to-DNG/development CLI and uses the reusable `rawler` crate, whose API DNGLab describes as unstable. `rawler` is LGPL-2.1; the separate `zenraw` project also offers Rust RAW development backends but is dual-licensed AGPL-3.0/commercial. Since this repository is MIT-licensed, adding an in-process RAW backend needs a deliberate dependency and distribution-license review. The current optional DNGLab executable boundary avoids bundling either implementation while leaving RAW development available. Sources: [DNGLab README](https://github.com/dnglab/dnglab/blob/main/README.md), [zenraw README](https://github.com/imazen/zenraw).
- The installed locale resources expose actions for keyframe toggling, XMP metadata initialization/reload/save, and transitions. Their helper labels identify four stars as a default keyframe and five stars as a crop keyframe.
- On a generated JPEG sequence without camera EXIF, the installed Metadata menu's manual-lens Lens Tagger offered 40 aperture stops from f/1 through f/90. Selecting f/8 changed the displayed aperture to 8.0; saving produced `lrt:Aperture="8.0"` in the `http://lrtimelapse.com/` XMP namespace. The synthetic input also contained an unrelated `exif:ApertureValue`, which remained distinct, so the observed tag is not evidence that LRT updates standard EXIF aperture fields. LRT saved embedded XMP into those generated JPEGs; `lrtl lens-tag` writes the same LRT field to external XMP sidecars and uses it as Holy Grail's fallback when EXIF FNumber is missing. This implements the metadata value, not LRT's embedded-image rewrite behavior.
- The LRT-generated JPEGs demonstrated that app data can arrive as embedded XMP without an adjacent `.xmp`. Following the storage model in [Adobe XMP Specification Part 3](https://github.com/adobe/XMP-Toolkit-SDK/blob/main/docs/XMPSpecificationPart3.pdf), `lrtl` reads the standard JPEG XMP APP1 segment and its linked extended XMP APP1 chunks; TIFF-family files read the XML packet in TIFF tag 700. These embedded packets are used only when no external sidecar is present; edits serialize the complete packet to a sidecar and leave the image unchanged. Embedded packets are capped at 32 MiB. The tag-700 reader accepts classic TIFF, Panasonic RW2 magic 85, Kodak DCR, and Nikon NEF/NRW, Sony ARW/SR2, Pentax PEF, and Epson ERF files with classic TIFF headers. Olympus ORF uses the same reader after recognizing its custom IIRO, IIRS, or MMOR magic. Kodak DCR's TIFF structure is described in [libopenraw's DCR notes](https://libopenraw.freedesktop.org/formats/dcr/), and its byte-order/magic signature is cataloged by [PRONOM](https://www.nationalarchives.gov.uk/PRONOM/Format/proFormatSearch.aspx?id=917&status=detailReport&strPageToDisplay=signatures); NEF/NRW's TIFF structure and ORF's custom magic were cross-checked against [NEF/NRW notes](https://libopenraw.freedesktop.org/formats/nef/) and [ORF notes](https://libopenraw.freedesktop.org/formats/orf/); [libopenraw's TIFF-based format classifier](https://libopenraw.freedesktop.org/api/libopenraw/stable/rawfile_8cpp_source.html) also identifies ARW, SR2, PEF, and ERF through TIFF headers and vendor metadata. CR3 scans ISO Base Media UUID boxes for Adobe's XMP UUID, validates box bounds, and parses the XML payload. The CR2/RW2 headers were cross-checked against [CR2 notes](https://libopenraw.freedesktop.org/formats/cr2/) and [RW2 notes](https://libopenraw.freedesktop.org/formats/rw2/); the UUID and 16-byte user-type offset follow [ExifTool's QuickTime XMP definition](https://github.com/exiftool/exiftool/blob/master/lib/Image/ExifTool/QuickTime.pm#L3071). Other vendor-specific RAW XMP containers remain unsupported, except Fujifilm RAF's embedded JPEG path described below.
- Fujifilm RAF stores an embedded JPEG preview at offsets declared in the RAF header. `lrtl` uses that bounded JPEG section as an input stream to the same APP1 standard/extended-XMP parser. Public format notes place XMP in the embedded JPEG; this covers only that JPEG's XMP packet, not metadata in RAF-specific directory records. Source: [libopenraw's Fujifilm RAF format notes](https://libopenraw.freedesktop.org/formats/raf/).
- A limited scan of app-class string constants found Adobe's Camera Raw XMP namespace URI and workflow names for keyframes, Auto-Transition, Holy Grail, and visual deflicker. No product code or class output is part of this repository.
- `javap` inspection of the installed 7.5.5 JAR's visual-deflicker call path showed per-sequence fields for pass count, smoothing factor, constant-target mode, accuracy, and workflow state. The worker receives the selected sequence, accuracy, pass count, workflow value, `smoothingFactor * 80`, and constant-target flag. Each pass visits ordered frame records, reads a per-frame numeric measurement, derives a smoothed target curve through `lrtimelapse.eA` and a regularized solver (`Pia`), then stores per-frame correction deltas. This establishes the control flow and parameter roles; the exact luminance statistic, ROI sampling, curve constraints, and pixel-development math are not fully identified from this bytecode trace.
- A narrower trace of the `eA.A(length, indices, values, smoothing)` helper found that it constructs `Pia` with a bandwidth proportional to `smoothing * length / sampleCount`. The `Pia` path computes a tricube-weighted local linear regression over a moving nearest-sample window; the traced caller sets the robust-iteration count to zero, so this call makes a single smoothing pass. `lrtl deflicker --smoother lowess` now offers the same general regression family, with its own radius-to-window conversion. This does not identify LRT's input luminance statistic, sample selection semantics, correction response, or final developed pixels, and does not claim numerical parity.
- `javap -p` on the installed JAR's `lrtimelapse.settings.SequenceProperties` exposes sequence settings named `holyGrailReduceDeflicker`, `holyGrailRotate`, `holyGrailStretch`, and `workflowHolyGrailWizard`. This confirms separate sequence state and curve adjustment controls. The calculation uses an internal per-frame value loaded from XMP, not an EXIF calculation performed in this controller; how LRT creates that input value is still unresolved. lrtl retains its EXIF exposure-equivalent mode and now also has an opt-in `--lrt-input` approximation that consumes the precomputed XMP value.
- Further `javap` inspection mapped the Holy Grail UI controls into the sequence fields: the Rotate control ranges from -4.00 to +4.00, the Stretch control from -1.50 to +1.50, and a third value is stored as `holyGrailReduceDeflicker`. Its slider stores hundredths over 0..1. The installed sequence model persists these values outside per-image XMP. Upstream curve fitting and the precise mapping of internal correction values to correction layers remain incomplete.
- `SequenceProperties` also exposes `filenameSort` beside the three Holy Grail controls. `lrtl --lrt-holy-grail-settings` reads these sequence-level values from `.lrt/lrtsequence.json`, applies the stored LRT name/date ordering and the saved curve controls, and enables the existing `--lrt-input` calculation path. Tests compare saved XMP output against individually generated plans, cover per-sequence batch settings and reject incomplete/out-of-range settings before writes. This imports persisted controls; it does not recreate LRT's GUI or input-image measurement path.
- A follow-up bytecode trace mapped the Rotate slider to hundredths in `[-400,400]` and Stretch to hundredths in `[-150,150]`. The shape helper uses original Preview EV inputs, carries corrections across adjacent 2*/3* pairs, then normalizes using frames 1 through the end and adds a float32 centered trend. With no adjacent 2*/3* pair it returns zero. The UI passes a zero offset. See the 112 directly measured shape cases below; the previous generic normalization summary did not account for these branches.
- The `lrtimelapse.prN` Holy Grail optimizer performs a two-stage grid search by rerunning the controller calculation. Stretch evaluates 150 values from -1.48 through +1.50 in 0.02 increments and minimizes `sum(abs(curve[i] - smoothCurve[i]))`; Rotate evaluates 200 values from -3.96 through +4.00 in 0.04 increments and minimizes `sum(abs(smoothCurve[i]))`. The worker reads the raw and smoothed arrays through `tQa.A()` and `tQa.m()` after `cOM7.a(...)` stores them, then reruns the best pair and updates the Rotate/Stretch controls. This identifies the L1 objectives, although the upstream curve input and exact regularized smoother are still not fully mapped. The `holyGrailReduceDeflicker` slider value is passed into the curve interpolation/regularization helper.
- A separate trace of `lrtimelapse.cOM7.a(Rsa,float,float,float,float,boolean)`, the method that sets `SequenceProperties.workflowHolyGrailWizard`, exposed the main per-frame calculation path. It starts from the negated nullable `tQa.G()` value. For each adjacent 2*/3*/4* pair, it sets a carried correction offset from the difference to the preceding corrected value and a recency-weighted mean of up to ten preceding frame deltas; that offset continues across following non-pair frames. The corrected full-length curve passes through `eA.A` using `holyGrailReduceDeflicker`. A range helper recenters and optionally clips that curve to ±10 before the controller doubles it. The method then accumulates changes returned by an adjacent-frame helper when both frames are 2*/3*/4* eligible, recenters/clips again, and adds a separate shape curve based on original Preview EV inputs after adjacent 2*/3* corrections, scaled by Stretch and a float32 centered linear trend scaled by Rotate. Its normalization range excludes frame zero (see the measured shape-helper section). That curve passes through the same helper at a fixed 0.5 factor; a final recenter/clip constrains the results to ±1 before storing them on each in-memory `tQa`. lrtl's `--lrt-input` now follows the observed scaling, ten-delta weighted queue, carried offset, adjacent-frame correction, recenter/clip stages, and shape controls. The exact helper fallback and regression results remain approximate; lrtl now implements the recovered histogram on its selected raster, while LRT's source-specific preview creation and color management remain unverified; the default full-frame ROI and resize behavior are described below.
- The adjacent-frame helper `cOM7.A(tQa,tQa)` parses shutter, integer ISO and aperture metadata. Direct numerical measurements now confirm `(log2(shutterBefore / shutterAfter) - log2(ISOAfter / ISOBefore) - log2(FBefore^2 / FAfter^2)) / 4`, omitting ISO/aperture terms when either member is nonpositive. The earlier reading of `com3.A(2, ratio)` as `log2(2 / ratio)` was incorrect: bytecode computes `log(ratio) / log(2)`. Unchanged settings therefore contribute zero. See the 27 measured camera-pair cases below.
- Runtime lookup through the installed XMP registry maps property ID 4 to `lrt:ExternalExposureDefault` and ID 5 to `crs:Exposure2012`. A Holy Grail refresh path reads `ExternalExposureDefault`, divides it by four, and stores the result in the per-frame model value returned by `tQa.G()`. If `Exposure2012 / 4` equals that internal value, it clears the `Exposure2012` registry slot to zero. lrtl's `--lrt-input` reads the scalar field and applies the observed divide-by-four scale. This confirms the internal input source and scale at refresh time, but not the native preview generation or the effects of camera settings and Lightroom adjustments on the stored measurement. A headless call to the installed sequence table model reports column 2 as “Preview EV”; the column is editable and its getter returns the same per-frame value loaded as `ExternalExposureDefault / 4`. lrtl therefore exposes that user-facing EV value as `PreviewEV`, multiplying by four when writing the XMP field. The image measurement path is traced in the Preview EV measurement section below.
- The save route is now partly mapped. `lrtimelapse.LRTimelapseMain.A(Rsa,int,ArrayList<tQa>)` asks the frame's `tQa.A(Rsa,ArrayList,boolean)` method to update its in-memory XMP packet, then passes that packet to `nNa.A(file, Wd)` for writing. The frame writer formats three separate numeric model fields and writes them into existing Lightroom local-correction records. In MaskGroup mode (`Rsa.K()==2`), it scans `MaskGroupBasedCorrections`, finds array positions marked with `crs:CorrectionName` values prefixed `#LRT internal use`, and updates the corresponding records. The installed `raw_m_2.xmp` template labels these slots `(HG)`, `(Deflicker)`, and `(Global)`, in that order. In the other path it updates existing `GradientBasedCorrections[5]`, `[6]`, and `[7]` values at `crs:LocalExposure2012`; the observed path checks that each indexed item exists and does not create it there. The installed `raw_m_1.xmp` template identifies its legacy slots through one `CorrectionMasks` gradient per position, each with scalar property `crs:MaskName` set to the text `LRT: don't alter!`; positions 5–7 use this marker. The roles are identified, but their numeric units and calculation formulas are not fully mapped. lrtl's `holy-grail` writes its approximate exposure compensation to a record whose `crs:CorrectionName` is exactly `#LRT internal use (HG)`, or to legacy `GradientBasedCorrections[5]` when positions 5–7 all carry the template's internal mask marker. If neither signature is present, it falls back to global `crs:Exposure2012`. It records the target in `ol:HolyGrailXMPTarget` so `--reset` restores the same field. This matches the observed storage shapes, not LRT's calculation.
- A follow-up scan of class-file string constants found local-correction names such as `crs:LocalExposure2012`, `crs:LocalContrast2012`, and `crs:LocalColorGradeShadowSat`, alongside mask fields such as `crs:CorrectionMasks` and `crs:CorrectionAmount`. The save route above confirms that LRT writes some frame-state values through local correction records; this does not establish numerical parity or the full initialization and interpolation rules.
- Decompiling the installed XMP initialization path in `tQa` shows that it prepares `MaskGroupBasedCorrections` positions 1–9 and `CorrectionMasks[1]`. New correction entries receive `What=Correction`, `CorrectionAmount=1`, `CorrectionActive=true`, and `LocalExposure2012=0`; slots 1–4 and 8–9 also receive neutral local-adjustment defaults. The initializer creates a gradient selector for positions 1–7 and circular-gradient selectors for positions 8–9. This initialization range is distinct from the Auto-Transition registry: registry slots 1–4 and 8–11 are transitioned, while 5–7 are not. lrtl currently materializes only a missing registered position when it has transition data, using a nearby populated XMP item as its template; the CLI `init` now independently constructs the observed nine-slot layout when missing, as detailed below, while preserving existing user data. It does not reproduce the full metadata-reset workflow.
- The installed JAR contains three XMP templates (`empty.xmp`, `raw_m_1.xmp`, and `raw_m_2.xmp`). The empty template uses scalar child elements for Camera Raw version/settings and crop defaults (`CropTop/Left=0`, `CropBottom/Right=1`, `CropAngle=0`), plus a structured `crs:Look` with `Name` and `Amount` attributes. The RAW templates contain many more scalar slider fields, along with booleans, enum-like values, and structured properties. `raw_m_1.xmp` stores the legacy master `ToneCurve` as an `rdf:Seq` of `rdf:li` coordinate pairs (`0, 0`, `255, 255`); `raw_m_2.xmp` stores `ToneCurvePV2012` in the same shape and includes internal local-correction slots named HG, Deflicker, and Global. These templates establish packet shape and vocabulary, but not which fields Auto-Transition interpolates.
- Bytecode metadata names sequence-level fields `rating`, `workflowKeyframes`, and `workflowAutoTransition`, and a global `initializationCreatesKeyframes` setting. This distinguishes per-sequence workflow state from per-image XMP ratings. The observed names do not reveal the full runtime keyframe algorithm.

## Direct numerical smoothing comparison (2026-10-16)

The independently authored `scripts/probes/SmoothingOracle.java` calls the installed 7.5.5 `eA.A(length, indices, values, smoothing)` entry point on a synthetic sequence: `values[i] = (i * 37 % 23) - 11 + (i % 7) * 0.125`, with every frame sampled. The JAR SHA-256 was `e588b59a460ab45f41fe4614f72e9528938db77e0e21e37facb620226cdaa28c`. The JSON fixture under `internal/lrtl/testdata` contains only numeric observations, not application code or templates. Normal Go tests do not require LRT, Java, or a license.

The 53 cases cover lengths 1, 2, 3, 5, 10, 31 and 2001, smoothing 0, .001, .05, .2, .5, 1, 1.5 and 2 where the entry point completes without initializing its application logger. Up to 32 evenly distributed output positions per case include both endpoints. A few short-sequence settings that trigger a failed retry and application logging are excluded from this direct probe; they are not evidence of parity. All recorded outputs match within absolute 2e-8. This establishes agreement for this sampled component, not end-to-end Holy Grail, Visual Deflicker, sparse sampling or developed-image parity.

The comparison caught a real discrepancy: the previous approximation always used `n-1` neighbors for bandwidths above one. LRT instead retries after dividing the rejected bandwidth by that value plus a float32 .001 promoted to double. At n=2001, smoothing 1.5 and 2 select 1999 neighbors rather than 2000. The updated Go code follows that calculation. The local regression uses centered coordinates for numerical stability, so bitwise floating-point equality is not claimed.

To regenerate the observations, compile the probe into a private work directory with the locally installed JAR on the classpath, then run `lrtlprobe.SmoothingOracle` with the JAR first in the classpath and redirect stdout to a JSON file. Keep the generated classes and application binaries outside the repository. The skipped short-sequence failures require the full application's logging initialization and are deliberately not worked around by copying product classes.

## Deflicker correction storage

### Native single-pass numerical comparison (2026-10-17)

The batch-mode `cTa(null,true)` worker completed 108 original synthetic dense
additive passes without a UI. Below eight samples it uses the straight line
between endpoint measurements; Constant uses the arithmetic mean; longer
sequences use the existing `eA` LOWESS component. The residual is measured
minus target. The transfer uses a shifted residual and a bounded log-odds
function. Accuracy selects a float32 threshold; high accuracy additionally
uses a pass-dependent dampening factor with float32 0.6 promoted to double.
Updates accumulate into existing native corrections and clamp to [-1,1].
When every residual is exactly zero, the worker clears all corrections.

`native_deflicker.go` independently models this pass and compares targets and
corrections with the fixture under `testdata`. `--calculation lrt-7.5.5` now
connects it to single/batch Deflicker planning. Histogram measurements use
the existing raster/RAW/cache input pipeline and optional saved reference boxes.
The shared XMP planner tracks reversible deltas while storing native absolute
values in recognized internal slots. Global fallback retains user exposure as
a separate baseline. Existing tracking is migrated when necessary. Subsequent
passes remeasure candidate Go-rendered pixels, applying only the additional
tracked exposure since the starting display image when XMP is not reapplied.
The tests compare native fixture outputs through modern/legacy/global storage,
individual/batch final XMP, date-order ROI, reset, backups and error preflight.
This does not establish native Camera Raw or complete multipass pixel parity.

The earlier observed writer maps the internal Deflicker value to a correction named exactly `#LRT internal use (Deflicker)`, or the sixth legacy gradient after validating all three internal signatures at positions 5–7. lrtl now uses that existing storage slot as well. It does not create or copy proprietary mask templates. Packets without a recognized slot retain the global Exposure2012 fallback. `ol:DeflickerXMPTarget` records the destination separately from `ol:DeflickerCorrection2012`, preserving compatibility with earlier global-only lrtl corrections.

Tests cover modern and legacy packets, incomplete legacy signatures, independent HG/Deflicker slots, repeated application, reset after later user edits, migration from global-only tracking, malformed/orphaned tracking, and CLI backups. Rendering includes the actual managed slot's existing exposure offset and active/amount controls exactly once. With XMP exposure/basic enabled during measurement, a new residual is added to the existing managed delta; measurements without XMP replace the previous delta. New deltas account for the target CorrectionAmount; an inactive/zero-amount target fails if correction is needed. The luminance statistic and 70% correction response remain lrtl approximations, and do not claim LRT's complete Visual Deflicker behavior.

### Pixel feedback passes (2026-10-16)

Both `deflicker --passes N` and `render --deflicker --passes N` now remeasure corrected pixels on every pass. XMP planning clones packets in memory and runs the supported local/crop/global pipeline with the candidate corrections. Only final absolute writes are returned to the CLI, so intermediate passes create neither sidecars nor backups. Without XMP application, subsequent measurements apply the newly computed managed delta to source pixels, retaining the default replacement behavior. Planning stops early when no further XMP changes are required; CLI output reports the measured pass count.

Render-only feedback applies the cumulative output gain after supported XMP adjustments, matching its final rendering order. It combines exposure-only XMP and feedback into one gain operation to avoid intermediate clipping. XMP-based feedback changes exposure before the relevant tone operations. These are distinct pipelines; they do not promise identical results. Photometry uses the bounded median sample grid in linear sRGB with full 16-bit channel precision. Resizing, overlays and motion blur remain outside the measurement stage.

RAW preview/development results are cached opportunistically in private temporary files, capped at 512 MiB per operation. Uncached frames are decoded again. Cache I/O failures fall back to source decoding, and recorded cache files are removed on success or error. Source image data is never replaced by this cache.

Regression tests compare actual nonlinear XMP feedback against the earlier scalar simulation, check generated MJPEG AVI pixels, final CLI packets/backups, failed planning without writes, noncontiguous frame indices, adjacent 16-bit luminance levels, clipped feedback, target amount/active controls, and cache reuse/bounds/failure/cleanup. In the synthetic contrast fixture, four real passes leave about 0.00014 EV residual versus 0.20 EV from four simulated passes. This is an internal regression, not an LRT oracle or a general convergence guarantee.

A local three-frame Nikon NEF smoke run completed Adobe-backed XMP-basic analysis with three feedback passes, then rendered with output-gain Deflicker and a 640 px long-edge limit to MJPEG AVI. The command used preview-only XMP planning; source sidecars were not written. The camera sample and generated video remain local, outside the repository's tracked files.

## CLI composition editing (2026-10-16)

`crop IMAGE_OR_XMP|DIRECTORY` provides an explicit CLI path for the existing composition workflow. `--rect L,T,R,B` writes the four normalized crop fields, and `--angle` writes CropAngle; both enable HasCrop. Newly supplied angles are constrained to the installed numeric registry's -45..45 range. This observes XMP vocabulary and bounds, not the product's composition interaction or exact rendering. `--enable` changes the active flag alone. `--clear` restores the default bounds/angle and disables crop while preserving ratings and keyframe metadata. `--mark` is limited to one image/packet and delegates to the existing 5-star crop-keyframe operation, including HG keyframe protection.

The command preflights all selected packets and resulting metadata in memory, then writes changed sidecars using the existing atomic writer and backups. Identical edits do not write again. It supports embedded-XMP fallback and conventional sidecar names; neither image decoding nor RAW development is needed to edit metadata. Read-only text/JSON inspection is available without edit flags. Root HasCrop now participates in the scalar dispatch described below. The documented animation workflow enables the sequence explicitly before editing endpoint crop keyframes, including sequences with only 5-star crop marks.

Tests exercise complete/partial edits, zero angles, -45/+45 boundaries, malformed/NaN/infinite/inverted bounds, unchanged originals and original-content backups, no-op writes, dry runs, no-backup, HG protection, preflight failures on later packets, clear/reset, legacy flag inference, JSON, embedded vendor data, conventional/standalone sidecars, and a three-frame CLI crop/interpolation/render workflow whose actual decoded AVI pixels show the expected pan. Rendering still uses lrtl's approximate crop transform; automatic fitting and Camera Raw pixel parity are not implemented.

A local three-frame Nikon NEF sequence completed `crop --enable`, two endpoint rectangle edits with `--mark`, numeric interpolation, Adobe-backed RAW development and crop rendering to 640x426, 24 fps MJPEG AVI. ffprobe reports three frames. The first source RAW's SHA-256 still matches the original fixture. Sample images and the generated video are local ignored files.

## CLI point-curve editing (2026-10-16)

`curve IMAGE_OR_XMP|DIRECTORY` exposes the supported point-curve workflow without the GUI. It reads global master/RGB curves with the existing modern/legacy fallback mapping and writes point lists or an identity curve. `--mask COLLECTION[N]` selects an existing non-internal local correction and maps the channel to MainCurve/RedCurve/GreenCurve/BlueCurve. `--mark` can save a 4-star reference together with a single-image curve edit. Editing uses the existing 0..255 integer point vocabulary; this CLI uses the existing GUI's 2..64-point and endpoint constraints, without claiming that those are LRT's complete editor limits.

New properties are independently authored RDF Seq values. Existing attributes, plain text and supported RDF sequences retain their storage form. Shared writing now resolves the RDF URI before creating list items, fixing a hard-coded rdf prefix that could bind to an unrelated namespace. Unsupported structured representations and ambiguous duplicate properties fail before publication; sequence/container metadata is retained. The common channel/input-path helpers were moved out of GUI/crop-specific code so both command paths use the same metadata rules.

Read-only text/JSON output reports whether a physical point curve exists, can be edited, and belongs to the existing interpolation equalization set. An absent curve displays an identity default; it does not resolve a named preset into points. Global legacy RGB fallback names not in that set and non-MaskGroup local curves can still be edited directly across a directory. Curves are equalized from the chosen reference where supported; numeric point animation is not implemented. Local rendering still requires a supported mask, and HDR/extended curves and Adobe pixel parity remain unsupported.

Eight regression tests exercise all four channels with attribute/text/sequence/legacy representations, namespace collisions and unknown metadata, embedded-image preservation, no-op/dry-run/backup behavior, batch preflight failure, duplicate curves, HG/internal protection, point limits, directory editing/JSON, and global/local edit→reference-equalization→actual MJPEG pixels. Those end-to-end fixtures map a uniform 128 input at a declared curve point to 64 in all output frames, checking that frame zero also receives a reference curve from the middle 4-star frame. These are lrtl workflow tests, not native LRT or Camera Raw pixel comparisons.

A local three-frame NEF smoke run completed init, a middle-frame global master curve edit with a 4-star mark, a local MaskGroup[1] red curve edit, reference equalization and Adobe-backed RAW rendering. ffprobe confirms 640x426 MJPEG AVI at 24 fps with three frames. The first copied NEF's SHA-256 still matches the source fixture. Images, sidecars and video remain local ignored files.

## High precision frame transport (2026-10-16)

The renderer now sends little-endian 16-bit-per-channel RGB (`rgb48le`) to FFmpeg for MP4/MOV, rather than quantizing decoded TIFF/DNG/PNG to RGB24. The codec's final pixel format remains explicit: H.264/H.265 use 8-bit yuv420p by default or 10-bit yuv422p10le/yuv444p10le with explicit or imported sampling; ProRes uses yuv422p10le; DNxHR HQX/444 use their 10-bit formats. This is SDR sRGB input transport, not HDR, ICC profile interpretation or verified LRT color management.

Geometry allocates 16-bit image buffers for 16-bit input, including EXIF orientation, aspect crop, XMP crop/rotation and Catmull-Rom resizing. Exposure-only edits use a 65536-entry linear-light sRGB LUT and preserve alpha. Text overlays draw into the same precision. Motion blur now blends 16-bit RGB samples using the observed LRT weights and a larger memory estimate. LRT's YUV filter path still differs. The supported basic/local color, tone-curve and spatial controls now also use 16-bit NRGBA buffers, a 65536-entry decode table, float32 detail layers and float64 tone/color calculations. Tone curves retain PCHIP coefficients per original XMP segment and evaluate 16-bit positions directly, avoiding both an 8-bit LUT and a 128 KiB table per curve per frame. This improves internal numerical precision while retaining the existing independent SDR control approximations; it does not establish Adobe or LRT pixel parity.

Independent tests capture the external encoder's exact stdin using a child test process and verify adjacent 16-bit TIFF/PNG values, byte order, bounds, alpha on black, exposure, geometry and a combined orientation/crop/resize/basic-exposure/motion-blur/overlay render. A separate opt-in `LRTL_TEST_REAL_FFMPEG=1` test encodes a 1024-pixel synthetic gray ramp with actual ProRes HQ and decodes to gray10le: the installed FFmpeg retained 877 distinct values along the sampled row. With XMP contrast, saturation, identity curve, sharpening and luminance/color denoise active, the same actual ProRes test retains 693 distinct levels. Global precision tests cover 22 control combinations and local tests cover 12; identity curves are checked over all 65536 inputs, original knots and monotonicity are checked separately, and the parser/PCHIP path is fuzzed. Normal tests require no FFmpeg installation. RAW development, a two-frame Nikon NEF ProRes render and a NEF with exposure/contrast/saturation/nonlinear curve applied (640x426 yuv422p10le) are separately checked locally.

## Direct Auto-Transition component comparison (2026-10-16)

The independent `scripts/probes/TransitionOracle.java` invokes the installed 7.5.5 spline (`de`, with `OC` points) and numeric rounding helper (`lrtimelapse.tools.com3`) through reflection. The numeric JSON fixture contains 27 cases: lengths 5/17/101, three or four anchors including both endpoints, and smoothness 0/50/100. The fixture bins the measured spline points with the observed first-sample-per-frame rule while preserving marked anchors. It contains measurements, not product code. Normal tests need neither LRT nor Java.

The comparison found an arithmetic-order discrepancy in the Bezier basis. LRT computes the squared parameter/complement first and evaluates its basis products in a particular float32 order. The revised Go evaluation matches the recorded binned non-keyframe values within 1e-6. The fixture also records 11 integer rounding examples and 13 four-decimal examples. Integer rounding comes from LRT's custom half-away-from-zero helper, not Java Math.round (which behaves differently for negative ties). Four-decimal rounding uses BigDecimal constructed from the exact binary double with HALF_EVEN. Go reproduces that comparison with exact rational arithmetic instead of rounding `value * 10000`, which can erase tie information.

The probe does not execute the entire Auto-Transition worker, final XMP serialization, radial aspect correction or mode-specific keyframe handling. Direct invocation of the spline with only two anchors raises an array-bounds exception in the installed component; lrtl retains its existing two-anchor fallback. This does not establish what the complete LRT application does for that input.

A separate read of the installed numeric worker's bytecode and its two postprocessing helpers confirms: after spline sampling and type/bound adjustment, anchor gaps shorter than `max(2, sequenceLength * 4 / 100)` become linear. The comparison is strict; a gap equal to the threshold remains a spline. Intervals whose endpoint values compare equal are made constant; Double values are compared after the exact four-decimal rounding above. lrtl applies these rules to global and registered local/mask values, preserves marked keyframes and continues enforcing its registered bounds for invalid/out-of-range source values. Synthetic full-plan tests cover short gaps, negative integer ties, equal-value overshoot removal, precise doubles, local values and the strict threshold. These postprocessing tests are analytic regressions based on that trace, not whole-application oracle outputs.

The expanded tests also exposed that the interpolation planner initialized sidecar-free images as empty packets instead of calling the common embedded-XMP reader. It now uses the same sidecar-first / embedded-fallback reader as other commands. A CLI test starts from JPEG APP1 XMP with embedded keyframes and vendor metadata, interpolates to new sidecars and verifies that original JPEG bytes and unrelated packet data survive. `--smoothness VALUE` now participates in positional-argument reordering, including `--smoothness 50 --write`. Non-finite or float32-overflowing anchors are rejected before writing. The test fixture filename helper was also fixed for 100+ frames; its previous two-character encoding produced a colon at 100 on Windows.

## Rating-specific numeric anchors and sparse transitions (2026-10-16)

The outer automatic worker inspects a numeric column's registered name: Crop-prefixed columns use rating 5 when at least one 5-star frame exists, otherwise rating 4; ordinary develop and local-correction columns use rating 4. The installed predicate `tQa.a(int)` tests rating 4 and rating 5 separately. HG 2/3 ratings do not become either kind of edit anchor. lrtl now builds separate normal/crop index and preservation sets. A crop mark no longer protects an unrelated exposure or local slider from transition, and a 4-star develop mark no longer protects Crop fields when 5-star crop marks are present. With no selected normal marks, frame zero supplies the existing lrtl reference fallback. The CLI reports both groups explicitly.

The installed `bqa`/`Ura` sparse component interpolates in doubles using `first + (last-first) * (i * (1/(count-1)))`, with the existing custom integer rounding. The outer worker selects this route for two endpoint anchors. The earlier two-point Bezier/frame-bin fallback was not equivalent even for smoothness zero. The independent `SparseTransitionOracle.java` now invokes this component successfully for 20 synthetic cases (lengths 3/5/9/17/101; doubles including values beyond float32 precision; increasing/decreasing negative integers). Go tests compare interior values within 2e-15 and check all three smoothness settings. This is a sparse component comparison, not the complete application worker or its final XMP serialization. Registered lrtl range clamps remain in force. Two interior selected anchors create extra sequence-end anchors and continue through the existing spline path; they are not treated as the two-endpoint linear case. A single selected source now also copies global and local double values without the previous intermediate float32 rounding; an integration regression checks a 12-decimal exposure value at all five frames.

The installed post-transition structural equalizer clones the packet returned by the sequence's reference getter. That getter chooses the first rating-4 frame, then frame zero. lrtl now uses that reference for its existing root structures and registered MaskGroup curve copying, includes frame zero among possible targets, and preserves the chosen source. The historical internal variable names still refer to first-frame equalization; the actual source is the selected reference. The existing uncertainty over the complete PointColors/Preset equalization domain remains.

Regression tests cover mixed 2/3/4/5 ratings, a stale lrtl marker with a non-edit rating, five-star-only and unmarked sequences, crop fallback to four stars, a single crop mark, independent local/global/crop output, sparse precision/rounding, and reference curves with unknown vendor data. A nine-frame CLI integration runs interpolation and MJPEG AVI rendering, checking actual pixels against independently calculated sRGB exposure gains while preserving source images. The crop-anchor isolation test compares a separate all-four-star control with the same interior Crop anchors; it is a metamorphic test of selection, not a dense-spline oracle. Missing local numeric anchor values, full application mode/initialization behavior, Camera Raw crop rendering parity and the remaining dense-path differences are not resolved by this change.

## Local numeric missing-value preflight (2026-10-16)

`NumericMissingOracle.java` now accepts an optional registered property path. A second normally completed nine-case probe uses `GradientBasedCorrections[1]/crs:LocalExposure2012`; its input/output measurements are stored in `lrt-7.5.5-local-numeric-missing.json`. This establishes the same forward-fill/leading-backfill behavior for a local numeric column, including zero and precise doubles. It is a preflight component test, not a mask-rendering or XMP-writer oracle. Values outside the normal slider range in this synthetic fixture test the raw copy helper only.

lrtl prepares separate copies of indexed numeric maps before local interpolation. Registered root correction numbers can fill absent resources (including selected keyframes); the first selector can also materialize when its parent exists or can be prepared from root scalar columns. Numeric fields can be supplied by non-keyframes; filled values then participate in the normal 4-star anchor selection, and missing scalar fields on existing selected keyframe resources are proposed for writing. The original index and RDF trees are not mutated during preparation. Known original values are compared against proposed values, preserving the packet's own IDs, active flags and unrelated data. LocalTemperature/LocalTint are ordinary local numeric columns; the global WB preflight exclusions do not apply to them.

Missing root correction resources and empty root RDF items now materialize registered scalar values; missing selector-level ranges, unsupported selector fields, ID-fallback records, unregistered positions and selector components after index 1 retain their exclusions. Non-keyframe structure cloning remains a separate existing policy. Registered root RangeMask fields now materialize missing structs; selector-level ranges still require their existing resource. Malformed/non-finite/structured registered numbers fail instead of being silently treated as missing and overwritten by a neighbor. This validation is lrtl behavior; malformed product-input parity is not established. Filled radial angles receive the observed post-normalization once before serialization; explicit activity flags remain independent.

Eight regression tests include nine measured local-column cases, old/modern positional layouts, precise selected-keyframe writes, selector coordinates, own IDs and unrelated metadata, minimal selected root-resource creation without template cloning, ID/reserved/second-selector exclusions, malformed input without writes/backups, normalized radial angles with repeat idempotence, and absent RangeMask resources. A five-frame CLI integration previews, writes, verifies original-content backups/no-op repeats, and renders actual MJPEG AVI pixels against an independent sRGB exposure formula while preserving source images. It uses a uniform source to avoid confusing JPEG chroma artifacts with the exposure calculation. Complete local structural parity, unsupported mask types and Adobe pixel equivalence remain outside this measurement.

## Numeric missing-value preflight (2026-10-16)

The installed metadata preflight (`LRTimelapseMain.A(Rsa,boolean)`) invokes its generic missing-value helper for numeric columns before transition. Reflection through the independent `NumericMissingOracle.java` records nine successful numeric cases: leading/interior/trailing gaps, no gaps, all-null, zero and a 12-decimal input. The resulting JSON stores only synthetic measurements. The same probe resolves the eight excluded property names from their runtime indices: WhiteBalance, IncrementalTemperature, IncrementalTint, Temperature, Tint, xmp:CreateDate, exif:PixelXDimension and exif:PixelYDimension. Only the four WB numeric fields intersect lrtl's registered global numeric transition set.

lrtl now prepares registered global numeric inputs by forward fill then leading backfill, before choosing the already separate develop/crop anchors. Originally present values remain unchanged by preflight, zero is present, all-missing columns remain missing, and unregistered editor-only controls are not adopted into interpolation. Missing fields on selected keyframes are proposed for writing too, retaining the precision available in the numeric source. Non-keyframes can supply the carried input that reaches a missing keyframe; they still do not become interpolation anchors. WB numeric columns bypass this preflight. The existing As Shot cleanup occurs earlier and removed fields cannot be resurrected through preflight.

Invalid/empty/non-finite scalar numbers and structured values in registered numeric fields are rejected before sidecar writes instead of silently treating them as absent. That validation is lrtl's explicit preflight behavior; the fixture does not establish how LRT handles malformed XMP. The removed numeric input reader used to discard such errors and skip missing anchor fields. Registered range clamps, integer rounding and 12-significant-digit serialization continue to apply at their existing stages; the numeric fixture is not an XML serialization oracle.

Tests compare every filled value and missing-field proposal against the nine measurements, check WB/legacy-property exclusions, compare a missing middle keyframe against a separately authored fully populated control, handle both endpoints inheriting a precise interior value, and check mixed-rating crop inputs. A five-frame CLI integration previews, writes, checks original-content backups and no-op repeats, renders MJPEG AVI, verifies actual sRGB exposure pixels against an independent formula and preserves source images. Malformed later numeric fields fail without earlier writes or backups. The shared property-count API now includes scalar changes and removals too. Missing numeric values inside local corrections and unsupported/missing mask structures are not resolved by this global-input change.

## Root scalar transition dispatch (2026-10-16)

The independent `scripts/probes/ScalarTransitionOracle.java` calls the installed 7.5.5 missing-value helper, the single-column string worker, and its case-insensitive equality gate via reflection. Twelve synthetic cases are recorded in `testdata/lrt-7.5.5-scalar-transition.json`. The fixture covers forward fill followed by leading backfill, absent versus empty strings, whitespace, case-only differences and dotted/dotless I. Boolean fixture cases invoke the missing-value helper only. The probe completes normally and stores only input/output measurements; normal Go tests require neither Java nor LRT. These are component measurements, not a complete Auto-Transition/XMP oracle.

Tracing the outer automatic worker revealed that invoking the string helper unconditionally would be incorrect. The worker's reference is the first 4-star frame, falling back to frame zero. An absent reference column removes that column elsewhere. With at most one 4-star frame, or equal values among selected 4-star anchors plus sequence endpoints, it copies the reference value to the entire sequence. Otherwise its sparse path (up to two anchors including endpoints) does not equalize scalar strings; its dense path invokes the single-column worker. The dense Boolean path only fills missing values. The sparse wrapper runs missing-value preflight when a gap follows a present value. A 5-star crop marker does not become an anchor for these root scalar columns; numeric Crop fields use a separate rating choice.

WhiteBalance has an earlier special handler: a 4-star frame with the exact `Custom` value makes the sequence Custom. Otherwise, if the reference says `As Shot`, it clears Temperature, Tint, IncrementalTemperature and IncrementalTint. lrtl removes these from candidate numeric inputs before spline planning so they cannot reappear through interpolation. Camera profile and lens profile names are propagated as metadata; that does not implement their rendering engines.

The Go implementation applies those observed branches to 22 Camera Raw root string fields and four root Boolean fields, including keyframe packets. Explicit values can therefore be overwritten by the reference-copy branch; preservation of every existing Boolean is not the LRT rule. The CLI preview lists scalar changes and `(remove)` entries separately from numeric counts. Writes use existing sidecar backup/atomic machinery. Local CorrectionActive/Flipped, crd defaults, all metadata initialization behavior and whole-application serialization parity remain unimplemented by this scalar change.

A separate local experiment invoked the outer worker on six synthetic arrays. It produced the expected scalar snapshots for sparse/dense WB, a single WB keyframe, missing WB with Custom, mixed crop flags and 5-star-only crop flags, but then failed while notifying the absent headless UI. Those snapshots informed branch tests and are not treated as successful end-to-end oracle runs. Branch tests are identified as analytical regressions based on bytecode, distinct from the twelve normally completed component measurements. Tests also cover CLI dry runs, original-content backups, no-op repeats, WB slider removal without reintroduction, invalid later flags without writes, embedded XMP, conflicting namespace prefixes, unknown nested RDF and original image preservation.

## Independent correction initialization (2026-10-16)

`init DIRECTORY [--mask-format auto|modern|legacy] [--write]` constructs the observed correction layout with etree and ordinary Go values. It does not distribute or copy XML product templates. In the modern layout, positions 1–4 are neutral editable linear masks, 5–7 are the named HG/Deflicker/Global internal layers, and 8–9 are editable circular masks. Legacy storage places the same seven linear records under GradientBasedCorrections and the two circular records under CircularGradientBasedCorrections. Template inspection confirmed the internal gradient spans above the image (-.41 to -.4, -.31 to -.3, -.21 to -.2), shared X=.5, which cover the full canvas. Those coordinates and the property vocabulary are interoperability observations; the builder, identifiers and editable names are independently authored.

Fresh editable controls are neutral, correction amount is one, and selectors are active. Missing Camera Raw header fields receive the observed Version=14, ProcessVersion=11, HasSettings=True; existing fields are preserved. CorrectionSyncID and MaskSyncID are distinct generated identifiers shared across new frames; existing identifiers at matching positions are adopted where available. Existing editable masks are untouched. Recognized internal records retain existing values/geometry and receive missing neutral fields only. Conflicts at reserved positions, moved internal roles and opposite-format internal layers are rejected before any sidecar is written. Auto mode preserves each packet's existing format and uses the first known sequence format for empty packets, defaulting to modern.

Tests cover both layouts, per-slot supported selector shapes, IDs, repeat idempotence, retaining user edits and unknown metadata, adoption of existing IDs for new frames, conflict preflight across the sequence, embedded-JPEG metadata, preview-only operation and backups. An integration test runs init, keyframes, set, interpolate, deflicker and MJPEG AVI rendering while preserving original images; Deflicker remains separate from Exposure2012. Internal full-frame gradient coverage is checked at opposite image corners. A local real-NEF run completed init, keyframes, Holy Grail, interpolate and Adobe-backed RAW rendering to a two-frame 640x426 yuv422p10le ProRes MOV. The unchanged camera settings needed no HG compensation in that probe.

This initializes missing mask structures and a few format headers. It does not clear/reset all metadata, remove existing edits, create workflow-state files, select keyframes or validate a complete LRT/Camera Raw read/write round trip. Generated modern/legacy packets are now also checked with the installed Adobe XMP parser (see the RDF resource tests). Mixed old/new packet formats are preserved rather than converted, and cross-format mask animation is not established. It is a documented portion of metadata initialization, not a claim of full product initialization parity.

## CLI local correction editing (2026-10-16)

The existing scalar `set` command now accepts strict RDF-array paths such as `MaskGroupBasedCorrections[1].LocalExposure2012` and `GradientBasedCorrections[1].CorrectionMasks[1].FullY`. The new `masks IMAGE [--json]` command lists correction/component paths, selected scalar values, editing eligibility and numeric-transition eligibility. It does not dump binary selector payloads or claim that every listed mask can be rendered. Arrays in all four supported transition collections are addressed by one-based positions; existing wrapped rdf:Description entries and alternate namespace prefixes are preserved.

Setter validation uses the observed 7.5.5 numeric registry bounds and integer types. Exposure values are stored XMP EV, while normalized local contrast/saturation/etc. remain normalized rather than receiving a UI-slider conversion. Boolean Active/Inverted/Flipped fields are per-frame edits, not interpolated fields. Geometry editing is limited to corresponding gradient/radial fields; target elements must already exist. Existing product-internal names, legacy internal mask signatures and lrtl correction tracking targets are excluded from manual editing. Generated init masks can therefore be edited and animated without entering the internal HG/Deflicker storage path.

Tests cover path grammar and index bounds, numeric/boolean validation without mutation on error, internal slot protection, dry runs and original-content backups, both modern/legacy exposure/geometry animation through decoded AVI pixels, wrapped RDF, conflicting namespace prefixes, the second selector component (which is not transitioned), tracked custom slots, radial controls and embedded-image source preservation. The path parser has a bounded fuzz target; a local five-second run completed 27,082 executions without failure. A local three-frame NEF sequence was initialized, edited through these paths, interpolated and developed with Adobe DNG Converter into a 640x426 yuv422p10le ProRes MOV. The editing operations remain independent XMP manipulation; they are not evidence of Camera Raw rendering parity, arbitrary aggregate traversal, brush/AI rendering or full mask-creation UI equivalence.

## Public workflow behavior

### Adobe DNG Converter backend

- The installed Adobe 18.7 also developed the dependency fixture `imagemeta v0.3.1/testImages/Hero8.GPR` into a 4000×3000 RGB TIFF and a successful GUI image preview. This real GPR check supplements the NEF/CR2 cases below; normal automated tests do not download or redistribute these real camera samples.

- `--raw-develop` defaults to an installed Adobe DNG Converter; explicit `--dnglab PATH` retains the previous backend. `--dng-converter PATH` overrides automatic lookup in PATH, standard Windows installation paths and the macOS application. The command uses `-u -l -p0 -dng1.4 -d <private-temp> -o linear.dng <absolute-source>`. Adobe performs proprietary RAW decoding and demosaicing; Go reads the actual uncompressed LinearRaw plane (PhotometricInterpretation 34892). No embedded JPEG is used in this path. See [Adobe's CLI specification](https://helpx.adobe.com/content/dam/help/en/camera-raw/digital-negative/jcr_content/root/content/flex/items/position/position-par/download_section/download-1/dng_converter_commandline.pdf).
- The independent Go reader supports classic TIFF byte orders, SubIFDs, 8/16-bit chunky integer samples, one or three channels, strips/tiles, black/white levels and black deltas, linearization tables, ActiveArea/default crop and DefaultScale. It uses AsShotNeutral or AsShotWhiteXY, AnalogBalance, CameraCalibration, ForwardMatrix or ColorMatrix, Bradford adaptation to D50 and sRGB encoding. Dual-illuminant matrix selection uses an iterative McCamy CCT estimate and reciprocal-temperature interpolation. BaselineExposure is applied before encoding. Camera profiles' Hue/Sat/Look tables, profile tone curves, Opcode corrections, floating-point planes and full Camera Raw rendering are not implemented; fractional default crop coordinates round to pixel boundaries. This is independent approximate development after Adobe demosaicing. The [DNG specification](https://helpx.adobe.com/content/dam/help/en/camera-raw/digital-negative/jcr_content/root/content/flex/items/position/position-par/download_section_733958301/download-1/DNG_Spec_1_7_1_0.pdf) establishes the tag meanings.
- Conversion output lives in a private temporary directory and is cleaned on success and failure; the source RAW and sidecars are not edited. Each converter invocation has a five-minute timeout. Both the converted file and decoded raster are bounded to 1 GiB. `import --convert-dng` uses Adobe by default with lossless or uncompressed output and copies source sidecars; DNGLab remains explicitly selectable. These are external programs, not included in the OSS distribution.
- Synthetic tests check known black/white/gamma results, byte orders, tiles, crop, color transforms, malformed inputs, external-process failure and cleanup, all RAW analysis commands, DNG import and GUI preview/analysis/render APIs. Real Adobe 18.7 runs on [rawpy's test samples](https://github.com/letmaik/rawpy/tree/main/test) produced 4256×2832 sRGB from Nikon D3S NEF and 1944×1296 from Canon 40D sRAW CR2. A two-frame NEF sequence rendered to MJPEG AVI at 640×426; Canon sRAW rendered to H.264 MP4 and also followed CR2→imported DNG→AVI. Real samples and Adobe outputs stay in ignored local scratch storage. The opt-in tests use `LRTL_RAW_SAMPLE` and `LRTL_LINEAR_DNG_SAMPLE`; normal CI uses synthetic fixtures and a child test executable standing in for the converter.

### Preview EV measurement

- The installed 7.5.5 frame method `tQa.A(Rsa,double,double,double,double,BufferedImage,boolean)` measures the supplied or cached display preview. It has separate `ORa` and JAI histogram branches. A follow-up check on 2026-10-17 confirmed that the default `_dev_filter` setting does not select the `LRTimelapseMain.I()` alternate-branch gate. The normal JAI branch is now the target of lrtl's Preview EV calculation. The previous implementation followed `ORa`'s uniform smoothing and pooled counts, which produced different values. The separate long-term metric mode retains its own implementation.
- A reflection probe directly invoked static `tQa.A(Rqa,BufferedImage,double,double,double,double,boolean)` with false on 59 original synthetic RGB images and normalized areas. The normal histogram has 256 bins on [0,255), excluding byte value 255. Weighted radius-two smoothing uses an exclusive upper sample bound, rounds counts, restores each band's original population and rounds again. The bin width is 255/256. It computes each band mean separately before averaging RGB, then divides by 256 for Preview EV. An empty band yields the native 255/256 fallback. `testdata/lrt-preview-ev-roi-7.5.5.json` records the native values and verifies the corrected Go path with tolerance 1e-14. For a 20x20 RGB(128,128,128) image the observed result is 0.4990196228027344.
- A second probe passed true to select native black/white exclusion, independently setting the temporary process's black/white percentages and hard-cut flag. Soft mode combines band populations, applies normalized linear edge ramps and measures raw bin indices. Hard mode rounds percentage cut positions, measures each retained band with the 255/256 bin width and averages the band means; empty bands contribute zero. Both use the same preceding smoothed histogram. `testdata/lrt-preview-cuts-7.5.5.json` contains 96 native observations including zero settings, overlapping cuts and saturated images. The CLI exposes these as explicit `preview-ev --ignore-blacks N --ignore-whites N [--hard-cut]` modes. Its existing Deflicker estimator still uses linear-sRGB median metering.
- `tQa.G()` exposes the model value loaded from `lrt:ExternalExposureDefault / 4`, and the update path writes `PreviewEV * 4` to that XMP property. `lrtl preview-ev DIRECTORY` applies the recovered histogram calculation to the selected decoded raster, reports the PreviewEV and XMP value, and `--write` saves it to sidecars with backups by default. It supports decoded JPEG/PNG/TIFF, explicitly selected RAW previews/development, and existing native camera/visual JPEG caches. `--lrt-reference` loads saved normalized reference boxes, interpolates them using the observed native float32 natural cubic and maps date-sorted boxes to the original frames before measurement. A bytecode trace of the `zPa` UI callsite confirms that the default histogram call passes normalized `(0,0,1,1)` coordinates, measuring the full raster; `--lrt-reference` supplies an explicit area. Tests cover the full-frame default against a varied native 7.5.5 oracle image, XMP/backup association and failures before writing. Native preview creation and decoder/color-management equality remain unverified.
- The installed frame loader uses the configured `previewWidthCamera` only as a width threshold: when the decoded image is wider, it resizes to `width × int(width/1.5)` with one `Graphics2D` bilinear draw. `LRTSettings` initializes the width to 1024, and the user's `LRTimelapse.config` also contains `previewWidthCamera: 1024`; this produces a 1024×682 preview. `lrtl preview-ev` follows that rule before histogram measurement and after optional XMP adjustments; `--preview-width-camera N` lets users set a non-default threshold and output width explicitly without reading LRT's config file. Three native 7.5.5 measurements spanning landscape and portrait source dimensions verify the resized EV within 1e-5. A reflection oracle checks 82 RGB pixels across six up/downscale cases, with all 246 opaque color components matching exactly. The Go path reproduces Java2D's 32.32 source stepping and 8-bit fractional bilinear weights. Source-specific decode and color management remain unverified.

- The official workflow describes XMP files as the exchange layer between LRTimelapse and Lightroom. Users choose keyframes, edit those images, save metadata, then run Auto-Transition to calculate settings for intervening frames.
- The official Long-Term / Construction workflow describes filtering sequences by Luminance, Contrast, and Hue, then applying Deflicker and Motion Blur to the more consistent subset. The workflow page describes paired low/high filter limits but does not publish the underlying metric equations.
- The installed 7.5.5 release notes retain the LRT 4.8 Range Mask compatibility note: selector locations were animated for four default linear gradients, two circular gradients, and the first two brushes. This documents an earlier selector-animation compatibility domain; the installed 7.5.5 transition registry has no `PointModels` path, so its current sample update rule is not established by this note.
- LRTimelapse 7 release notes list “Select by Filename” for separating images such as left/right camera captures. `render --filename-contains TEXT` supplies a case-insensitive CLI subset filter; it does not implement the table-selection UI or the other sequence management actions.
- The official Visual Workflow enables the Holy Grail wizard for manual-mode sequences where exposure time and/or ISO changed during capture, and describes its orange curve as compensation for those camera adjustments. LRTimelapse offers rotate/stretch controls for shaping that curve.
- The current official Visual Workflow also gates the wizard on a manual-mode sequence. lrtl only creates automatic 2*/3* markers when EXIF explicitly reports Manual on every frame; `--mark-jumps` is an override for sequences whose camera omitted mode metadata.
- LRTimelapse's developer describes 2-star/3-star ratings as internal Holy Grail keyframes that mark camera-setting jumps and must not be overwritten by ordinary 4-star keyframes. This supports the separate marker model; it does not establish the exact correction values stored by the wizard.
- The official Keyframes Wizard describes its suggestions as an automatic analysis of the sequence luminance progression. A CFR decompile of the installed 7.5.5 action dispatcher shows its `AUTOKF` branch only records a “User action: Keyframe Wizard” event; the adjacent `EVENKF` action selects a helper panel. The `CREATE.AUTO.KEYFRAMES` helper quantity is calculated in `cOM7.A(int,int)`: take the range of adjusted luminance values, divide by 0.1, then multiply by `float32(1 + 1000 / frameCount / 10)`. The result is raised to at least `(frameCount-1)/250`; if `frameSpan/(count+1) < 50`, it becomes `frameSpan/50`. The helper defaults to one and raises the result to two if any 2*/3* Holy Grail marker exists. For each consecutive 2*/3* pair, it accumulates the pair's luminance difference as an offset before measuring the range. Its slider is limited to 1–20. `lrtl keyframes` uses this formula by default, subtracting existing 4*/5* marks because its CLI preserves those marks instead of clearing them. The helper reads each frame model value from lrt:ExternalExposureDefault / 4. lrtl uses that stored value for the default count when every frame has a finite scalar; if any value is missing or invalid, it falls back to measured image luminance. Explicit image-analysis options also select the image path. The formula follows LRT's count calculation over its stored inputs when available. `lrtl preview-ev` also applies the recovered RGB histogram to the selected decoded raster; LRT's native Preview EV raster generation is not reproduced; its default meter ROI and width-based resize are covered above.
- The Wizard's location selection is visible in the call from `WNa.K()` to `cOM7.A(count, rating, false, ...)`, then `cOM7.A(Rsa,count,indices,rating,false)`. The panel defaults to four stars and offers a five-star radio option. With no row selection, `Rsa.A()` supplies every frame index. For quantity one, it marks the first frame. For larger quantities it uses integer `step=frameCount/(count-1)`, marks indices at multiples of `step` while `index < frameCount - int(step*0.8)`, and always marks the last frame. The integer rounding can produce more selected positions than the slider number on short sequences. The helper first clears existing ratings of the selected type; ratings 2–5 on interior target rows can shift a target to a nearby preceding unmarked row, searching up to nine frames. `lrtl keyframes` follows the spacing/endpoints rule and exposes the 4/5-star choice with `--rating`. By default it preserves old markers and omits candidates that collide. `--replace-existing` clears the chosen rating, frees those rows, then applies the preceding-row collision shift (up to nine frames) where possible. The CLI does not reproduce selected-row mode or the LRT confirmation UI. Its automatic quantity subtracts existing markers matching the requested rating unless replacement mode is enabled.
- The installed Japanese and English resources expose a separate “create keyframes (evenly spaced)” action. `lrtl keyframes --evenly-spaced` skips image decoding and requests five keyframes by default. It applies the same recovered integer placement rule as the Wizard; integer rounding can produce more than the requested number on short sequences. Explicit `--count` accepts 1–20.
- Keyframes appear as four-star ratings in Lightroom. The current official guide also documents manually adding and removing keyframes with the `1` and `0` keys.
- LRTimelapse 7.4 release notes state that first/last images are not automatically treated as keyframes for interpolation, a single keyframe is used as a reference for other frames, and a sequence without keyframes uses the first image as its reference. This is distinct from the Keyframes Wizard helper traced above, which explicitly selects the first and last rows when its quantity is greater than one.
- LRTimelapse 7.4 also describes linear transitions at sequence ends. lrtl currently adds endpoint anchors using the nearest keyframe's value for each property, then samples the same spline across the full sequence. Existing numeric values on unmarked endpoint images are overwritten; marked keyframes retain their values. The earlier note claiming that unmarked endpoint values were preserved was stale. Exact LRT endpoint parity remains unverified.

## Implemented compatibility choices

- Four-star ratings (`xmp:Rating=4`) identify ordinary keyframes; five-star ratings (`xmp:Rating=5`) identify crop keyframes according to the installed labels.
- A marker made by `lrtl mark` records the prior Lightroom rating in its own namespace so `lrtl unmark` can restore it. It always keeps the Adobe rating marker that Lightroom filters can see.
- The set editor allows 131 scalar numeric Camera Raw properties; 127 are also present in the installed 7.5.5 Auto-Transition runtime registry. The list combines legacy controls in [Adobe's Camera Raw namespace reference](https://github.com/adobe/xmp-docs/blob/master/XMPNamespaces/crs.md), global scalar controls present in the installed LRTimelapse 7.5.5 `raw_m_1.xmp` and `raw_m_2.xmp` templates, and continuous scalar fields in its runtime property registry. It includes legacy Clarity, Fill Light, Highlight Recovery, incremental white-balance values, HDR maximum value, color-wheel angles, and Defringe hue bounds absent from the templates. HSL `HueAdjustment*`, color-grade/split-toning angle fields, and Defringe hue bounds are numeric XMP values; lrtl uses the LRT numeric spline without applying circular shortest-arc wrapping. For structured point-curve fields, lrtl copies the selected reference frame's global `ToneCurve`, `ToneCurveName`, `ToneCurvePV2012`, and RGB channel variants to other frames instead of animating them; `ToneCurveName2012` uses the root scalar dispatch above. LRT 7.1 notes explicitly say point curves cannot be animated and are populated from the first image. Adobe's public namespace describes `ToneCurve` as an ordered array of points. LRT also documents equalizing Look, Preset, Tone Curve, and PointColors. lrtl copies matching top-level `Look`, `Preset`, and `PointColors` values from the selected reference. The installed structural equalizer's reference getter selects the first rating-4 frame or frame zero, as detailed above; the complete supported structures/equalization domain remains unverified. For `GradientBasedCorrections`, `CircularGradientBasedCorrections`, `MaskGroupBasedCorrections`, and `PaintBasedCorrections`, lrtl matches LRT’s fixed RDF array positions: Linear 1–4, Circular 1–2, Brush 1–2, MaskGroup 1–4 and 8–11, and `CorrectionMasks[1]`. It interpolates registered numeric `CorrectionAmount`, `Local*`, and basic mask shape fields by position; Sync IDs are not required in these arrays. If an XMP collection is not stored as an RDF array, lrtl falls back to matching `CorrectionSyncID` / `MaskSyncID`. The installed 7.5.5 numeric worker constructs `de` Bezier-spline points from frame index and numeric property values, uses zero endpoint tangents, and solves interior tangent vectors through the observed tridiagonal recurrence. It samples each segment at `2 * sequenceLength` uniformly spaced parameters, casts each sample X coordinate to an integer frame index, and keeps the first value assigned to each non-keyframe index. It clamps registered property bounds and leaves keyframe values in place. The Auto-Transition UI slider ranges from 0 to 100 and defaults to 50; the worker sets its spline factor to `(100 - slider) / 500` unless its mode argument requests zero. `lrtl interpolate --smoothness` follows the UI factor and discrete sample/bin rule with default 50. Runtime registry inspection found 127 of the 131 editor-allowlisted global fields; lrtl now clamps sampled values to each property's registered base bounds and applies the installed custom integer-rounding behavior (half away from zero) to Integer fields. ChromaticAberrationB, ChromaticAberrationR, Defringe, and Exposure remain available for direct editing but are absent from the 7.5.5 transition registry, so lrtl skips them. The Exposure2012 bound is -5 to 5 normally and -10 to 10 when the first frame's crs:IsMergedHDR value is exactly true; lrtl applies this observed dynamic rule. The dense string-column worker copies the first nonempty sequence value; its reference-copy and sparse alternatives are documented in the scalar dispatch section; the runtime registry records `crs:CorrectionRangeMask/crs:LumRange` as `String` and `ColorAmount`, `Type`, `LumMin`, `LumMax`, and `LumFeather` as numeric properties. The registry contains no `PointModels` property path, and the installed transition worker has no identified PointModels handling, so lrtl leaves selector samples untouched. This registry-based mapping is supported by the decompiled code, but exact numeric parity has not been compared against an LRT-generated XMP sequence. Depth-based corrections, brush `Dabs`, numeric animation of local tone-curve control points, unregistered positional slots, and full Camera Raw mask rendering remain unsupported.
- Decompiling and invoking the installed string decoders for `LRTimelapseMain.K(Rsa)` reveals two mask-version-specific path pairs: `MaskGroupBasedCorrections[8 or 9]/crs:CorrectionMasks[1]/crs:Angle` and `CircularGradientBasedCorrections[1 or 2]/crs:CorrectionMasks[1]/crs:Angle`. The method walks the sequence and, per value, adds 90 when below -45, then subtracts 90 when above 45; these are two sequential single-step checks rather than a general modulo operation. lrtl applies the same rule to the registered positional Angle transitions and existing keyframe values: Circular 1–2 and MaskGroup 8–9. The numeric postprocessor and pure-layout RDF array indexing are now compared against the 24-case runtime fixture described above; the native helper itself was not invoked through a complete XMP save. The separate aspect-ratio-aware radial shape correction remains unsupported.
- `holy-grail` reads standard exposure time, ISO, and (unless `--ignore-aperture` is set) FNumber tags from supported JPEG/TIFF/CR2/CR3/DNG/RW2 files. TIFF-signature RAWs are detected by content rather than extension; the `imagemeta` v0.3.1 NEF and ARW sample files both supplied valid shutter, aperture, ISO, and exposure-mode tags through this path. It calculates a per-frame exposure-equivalent value `log2(time × ISO / FNumber²)` and adds the difference from the first frame to the selected XMP target. When an LRT HG internal local-correction record is identified, it writes `LocalExposure2012`; otherwise it updates global `crs:Exposure2012`. The applied correction and target are recorded under the project's `ol` namespace to make reruns replace the previous result and to support `--reset`; these are lrtl-specific bookkeeping fields, not LRTimelapse fields. LRT-compatible output is a sidecar XMP. The implementation does not decode embedded EXIF from every RAW container or read developed-image luminance. `holy-grail --lrt-preview camera|visual` can instead derive the Preview EV input from a selected existing LRT JPEG cache, optionally using saved reference boxes, without first writing lrt:ExternalExposureDefault to XMP. This reuses the measured Preview EV estimator; it does not recreate LRT cache generation.
- Some RAW files contain an empty but well-formed `rdf:RDF` XMP container with no `rdf:Description` element. The parser adds an empty description in memory so metadata operations can continue and serialize edits to a sidecar; it leaves the RAW file unchanged.
- `deflicker --write` measures linear-light sRGB median luminance in a selected image rectangle, smooths log2 luminance with the local Gaussian smoother (or targets its sequence median), and adds the resulting per-frame stop deltas to `crs:Exposure2012`. It tracks only its own additive delta in `ol:DeflickerCorrection2012` so recalculation replaces that contribution and `--reset` preserves Holy Grail and user exposure values. With `--raw-preview` or `--raw-develop`, it can also analyze the selected RAW preview/developed pixels. This is an independent approximation, not the installed program's Visual Deflicker worker described above.
- `deflicker --smoother lowess` uses the local linear tricube regression in `lowess.go`, selecting up to `2*radius+1` neighboring frames per estimate. It is an optional smoothing choice informed by the installed worker's observed kernel, while Gaussian remains the default and the surrounding luminance measurement and multi-pass correction remain independent approximations.
- `preview` writes a static SVG plot of per-frame log2 linear-sRGB median luminance relative to the first image, sidecar `crs:Exposure2012` relative to the first value, and 2*/3*/4*/5* keyframe markers. It can use selected embedded RAW previews or DNGLab-developed TIFFs as image inputs. This is a numeric sequence chart, not the installed app's rendered visual preview.
- The installed 7.5.5 long-term worker crops to the selected normalized composition, resizes to 64×64 with bilinear interpolation, applies a separable Gaussian kernel configured with scale 4 (radius 4, kernel sigma scale/3), then measures grayscale mean/standard deviation and arithmetic mean HSB hue. The optimized grayscale is integer `(2R+3G+B)/6`; hue comes from `Color.RGBtoHSB`. `lrtl --metric-profile lrt-7.5.5` reproduces that preprocessing on the full frame (or the XMP-cropped frame) and those three metric definitions. Its resampling/kernel arithmetic is independent Go code, and it does not reproduce LRT interactive composition selection or temporal filter curves. The default `lrtl` profile preserves the original median linear-sRGB luminance, log-luminance percentile spread, and saturation-weighted circular mean hue. Both profiles write a versioned JSON manifest; `render --selection` validates its source directory, exact frame set, file sizes, and modification times.
- The installed LRTimelapse 7.5.5 release notes list DNxHR and HDR output as Pro features. lrtl maps FFmpeg `dnxhd` encoding to DNxHR LB/SQ/HQ (`yuv422p`), HQX (`yuv422p10le`), and 444 (`yuv444p10le`) profiles in MOV. The bundled FFmpeg encoder help confirms these profiles and pixel formats. The current RGB48 transport can preserve high precision sources into HQX/444, subject to the editing limits documented above. HDR transfer functions remain unsupported. See the official FFmpeg [DNxHR profile list](https://ffmpeg.org/doxygen/trunk/profiles_8c.html) and [encoder implementation](https://ffmpeg.org/doxygen/8.1/dnxhdenc_8c_source.html).
- `import` copies supported still images from a folder, optionally recurses into subfolders, copies matching XMP sidecars, skips JPEGs, sorts by XMP/EXIF capture time, or renames by capture time. `--convert-dng` invokes a user-supplied DNGLab for supported RAW files while copying other images and matching XMP. It supports the DNGLab lossless/uncompressed choices but not lossy compression or resizing. `import-video` invokes user-supplied FFmpeg to extract all decoded video frames, or samples at a requested fps, to an ordered JPG/PNG sequence. Both import paths check output collisions; DNG conversion stages each output before publishing it. LRT sequence-state transfer, video preview/splitting, and DNG resizing remain unimplemented.
- `timeshift` shifts scalar XMP `exif:DateTimeOriginal`, `exif:DateTimeDigitized`, and `xmp:CreateDate` values by a fixed duration. If no capture date is present in the sidecar, it reads the image's EXIF capture time and writes the shifted `exif:DateTimeOriginal` to a sidecar. It leaves `xmp:ModifyDate` and embedded image metadata unchanged. The operation previews by default and writes with sidecar backups only after `--write`; it is an XMP-sidecar operation, not a claim of exact parity with LRT's metadata UI.
- The installed `RenderPreset` stores Motion Blur as an enable flag plus an integer level. Bytecode inspection of the render dialog shows its seven choices store 2, 3, 4, 5, 10, 15, or 20, and its getter returns those same values. Static inspection of `mQa`'s render command builder and `A(int,float)` shows LRT emits `tmix=frames=N:weights='...'`; it generates each weight as `(i/N)^1.5` for `i=1..N`, formats it to three decimal places, and joins values with spaces. Weights run from the oldest frame to the current frame, so the current frame receives weight 1. FFmpeg's `tmix` fills its first window by cloning the first frame and normalizes weights by their sum when no explicit scale is set. `render --motion-blur N` follows that frame schedule and weight curve in Pure Go, then trims the first N resampled video frames as described above. Pixel values remain an approximation because lrtl blends 16-bit RGB while the inspected LRT render filter runs on a 10-bit YUV444 path; no rendered pixel-by-pixel comparison has been made. See the [FFmpeg filter documentation](https://ffmpeg.org/ffmpeg-filters.html#tmix) and [`vf_mix.c`](https://www.ffmpeg.org/doxygen/8.1/vf__mix_8c_source.html) for `tmix` behavior.
- The installed `RenderPreset` has an audio enable flag and audio-file path. Static inspection of the render setup shows LRT includes an audio input only when the flag is enabled and the configured file exists. `render --audio FILE` adds the first audio stream from one file to MP4/MOV via FFmpeg, encodes AAC at 192 kbit/s, resets audio timestamps, pads short audio with silence, and trims long audio to the output frame duration. This is a CLI workflow, not LRT's audio-track UI; MJPEG AVI audio and multi-track selection are not implemented.
- `--raw-preview` walks classic TIFF IFD, ExifIFD, and SubIFD links, tests JPEG/PNG/TIFF interchange/strip segments, and selects the largest decodable embedded image for CR2/DNG/NEF/NRW/ARW/PEF/SRW/SR2/SRF/DCR/DCS/KDC/3FR/FFF extensions. Olympus ORF uses TIFF-layout directories with its recognized custom magic; previews in MakerNotes-only locations are not extracted. Panasonic RW2 uses the TIFF directory layout with magic 85 and its IFD0 tag 0x002e embedded JPEG. Fujifilm RAF uses the big-endian JPEG offset and length fields from its header. Sony SR2/SRF, Kodak DCR/DCS/KDC, and Hasselblad 3FR/FFF are passed through the classic TIFF IFD preview reader; only decodable embedded image segments are returned. For CR3, the parser looks for the Canon UUID preview record, bounds-checks its declared JPEG length, and validates the embedded JPEG. `render`, `keyframes`, `preview`, and `deflicker` can use those images when explicitly requested. This is the camera's embedded preview, not demosaicing, white-balance handling, or Camera Raw development; CR3 files without that UUID preview are not handled by this path. The CR3 layout was cross-checked against [ExifTool's public QuickTime parser notes](https://github.com/exiftool/exiftool/blob/master/lib/Image/ExifTool/QuickTime.pm#L761), RW2 against [libopenraw's RW2 notes](https://libopenraw.freedesktop.org/formats/rw2/) and [ExifTool's Panasonic tag definitions](https://github.com/exiftool/exiftool/blob/master/lib/Image/ExifTool/PanasonicRaw.pm#L193), and ORF/RAF against their [libopenraw format notes](https://libopenraw.freedesktop.org/formats/orf/) and [RAF header notes](https://libopenraw.freedesktop.org/formats/raf/); lrtl has independent Go parsing code.
- `--raw-develop --dnglab PATH` invokes an independently installed DNGLab with `analyze --loglevel error --srgb <RAW>`, reads its 16-bit sRGB TIFF from stdout, and supplies that image to `render`, luminance-based `keyframes`, `preview`, and `deflicker`. Analysis commands can apply the supported global XMP basic adjustments or exposure-only `crs:Exposure2012`, plus rotated crop geometry, before measuring luminance; `render` has equivalent options. `--dnglab` selects the executable; otherwise `dnglab` must be available on `PATH`. The accepted sequence extensions follow the public DNGLab supported-format table. This integrates an external CLI rather than copying its code or bundling the LRTimelapse installation. It does not read Camera Raw XMP into DNGLab, and DNGLab's demosaic, white balance, tone, and color output are not expected to match Adobe Camera Raw. The upstream project describes DNGLab as alpha software and its Windows binary as untested. RAW developed output is capped at 1 GiB per frame in memory; video rendering preserves it through the supported tone/color/spatial operations and RGB48 transport until the output codec quantizes it.
- Optional `--stretch` and `--rotate` controls reproduce the observed curve-helper geometry over the selected EXIF or LRT-input curve: min/max normalization to `[-0.5, +0.5]`, multiplication by Stretch, and addition of a centered linear trend scaled by Rotate. `--lrt-reduce-deflicker` exposes the LRT input curve's 0..1 smoothing setting. `--auto-shape` uses LRT's observed two-stage search ranges and L1 objectives with a tricube-weighted local linear LOWESS approximation. Direct calls into the installed `eA` helper and `Pia` smoother showed that the fixed 0.5 factor uses an effective half-length window and the optimizer's 1.5 factor falls back to `n-1`; lrtl applies those window counts, while Pia's error-message fallback and edge handling remain approximate. LRT's native generation of the preview raster/ROI and precise regularized fitting remain unimplemented; `lrtl preview-ev` uses the recovered histogram calculation on its decoded input; lrtl's renderer applies its recorded HG correction globally when the XMP target is an LRT internal local-correction layer.
- By default, `holy-grail` marks isolated EXIF setting changes as adjacent `xmp:Rating=2` / `xmp:Rating=3` keyframe pairs, with lrtl metadata to preserve and restore any prior rating. `--lrt-input` instead preserves existing 2*/3*/4* ratings and uses them to enable LRT's adjacent-frame correction logic; it does not treat them as interpolation anchors. When an identified LRT HG internal layer is available, lrtl writes `LocalExposure2012` there and records the managed correction; otherwise it updates `crs:Exposure2012`. lrtl's renderer applies the managed HG amount globally and avoids also processing the hidden local mask. This keeps user Exposure values intact in the internal-layer path; both calculation routes remain approximations.
- Every write is atomic and makes a timestamped sidecar backup unless `--no-backup` is provided. Interpolation is preview-only unless `--write` is provided.
- The initial milestone read external sidecars only. Current packet loading also reads embedded JPEG, TIFF/DNG, CR2, RW2, and CR3 XMP as described above. Sidecar precedence is explicit; embedded and sidecar timestamps are not compared.
- `render --apply-xmp-crop` reads `crs:HasCrop`, normalized crop bounds, and `crs:CropAngle` from sidecars. For nonzero angles, it follows Lightroom's published coordinate convention: positive angles rotate the crop clockwise, the diagonal crop corners are rotated back to derive the level crop bounds, and an inverse affine map samples the source into the fixed output frame. Catmull-Rom resampling is used for both angled and axis-aligned crops. The bundled XMP template uses 0-to-1 crop bounds; when `HasCrop` is absent, non-default bounds or angle are treated as an active crop. The geometry implementation follows the public [Camera Raw XMP namespace](https://github.com/adobe/xmp-docs/blob/master/XMPNamespaces/crs.md) and Adobe Community's [crop-corner coordinate walkthrough](https://community.adobe.com/questions-675/sdk-computing-the-corners-of-a-crop-rectangle-962108); numerical parity has not yet been checked against a Camera Raw render.
- `render --apply-xmp-basic` reads the scalar Exposure/Contrast/Highlights/Shadows/Whites/Blacks/Texture/Clarity2012/Dehaze/Sharpness/SharpenRadius/SharpenDetail/SharpenEdgeMasking/LuminanceSmoothing/LuminanceNoiseReductionDetail/LuminanceNoiseReductionContrast/ColorNoiseReduction/ColorNoiseReductionDetail/ColorNoiseReductionSmoothness/PostCropVignetteAmount/PostCropVignetteMidpoint/PostCropVignetteRoundness/PostCropVignetteFeather/PostCropVignetteHighlightContrast/Vibrance/Saturation and Temperature/Tint controls, four parametric tone-region sliders and three split controls, HSL Hue/Saturation/Luminance adjustments for Red/Orange/Yellow/Green/Aqua/Blue/Purple/Magenta, and explicit master/RGB point curves. The HSL property names match [Adobe's Camera Raw XMP sample](https://github.com/AdobeDocs/cis-photoshop-api-docs/blob/main/sample-code/lr-sample-app/crs.xml); current hue and saturation fields take -100 to 100 values, as do the legacy `RedHue`/`RedSaturation` family in [Adobe's namespace reference](https://github.com/adobe/xmp-docs/blob/master/XMPNamespaces/crs.md). lrtl interpolates adjustments between eight fixed hue centers (0, 30, 60, 120, 180, 240, 270, and 315 degrees), maps one hue slider unit to 0.3 degrees, scales saturation by 1 plus the slider/100, and offsets HSL lightness by at most 0.25. These are sRGB-domain approximations, not Camera Raw's RAW color pipeline; numerical parity has not been verified. Temperature/Tint apply estimated linear-sRGB gains from a Planckian-locus chromaticity relative to 6500 K, with an independent green-magenta gain. The temperature curve follows the piecewise polynomial approach described for 1667–25000 K by [Kang et al.](https://www.kci.go.kr/kciportal/ci/sereArticleSearch/ciSereArtiView.kci?sereArticleSearchBean.artiId=ART000987865); the implementation extends the formula above 25000 K without claiming equal accuracy. This is an approximation for rendered sRGB, not Camera Raw white balance: it has no sensor data or camera profile. Values outside Temperature 2000–50000 or Tint -150–150 are rejected. Curves are accepted as comma/semicolon-separated scalar pairs or ordered RDF arrays; the renderer builds PCHIP segment coefficients and directly evaluates 16-bit sample positions between points. Adobe documents Camera Raw `ToneCurve` as an ordered array of integer points, but the sRGB-domain interpolation, white-balance transform, and tone-control and parametric-curve equations here are independent approximations, not Adobe's RAW/render pipeline. Parametric sliders use smoothstep region weights around the split positions and add at most 0.15 linear luminance; split positions are kept ordered with at least 0.05 spacing. Global Sharpness applies a separable box-blur high-pass to rendered linear-sRGB luminance; radius scales from the 1080-pixel short edge, Detail scales its gain, and EdgeMasking thresholds high-pass magnitude. LocalSharpness uses the same masked high-pass with a fixed radius. These slider mappings are independent approximations, not Camera Raw sharpening. Global noise reduction reads the LuminanceSmoothing and ColorNoiseReduction control families, using a fixed 3×3, edge-weighted sRGB neighborhood average for luminance and chroma. Detail controls edge affinity, LuminanceNoiseReductionContrast retains part of local contrast, and ColorNoiseReductionSmoothness controls chroma affinity. The equations are lrtl-specific and have not been compared against Camera Raw. PostCropVignette uses a crop-relative radial mask: midpoint sets the inner radius, feather sets the smooth transition width, roundness adjusts aspect scaling, and amount applies up to 1.5 exposure stops. HighlightContrast preserves bright pixels when darkening. This does not reproduce Adobe adaptive highlight recovery. Curve-name presets without explicit point arrays, local corrections not listed below, and Camera Raw profile behavior remain unsupported.
- `preview`, `keyframes`, `deflicker`, and `long-term` accept `--apply-xmp-basic` to apply the same supported global sRGB tone, HSL color-band, Temperature/Tint, and explicit point-curve approximation used by render before measuring luminance or color. It cannot be combined with `--apply-xmp-exposure`, since the basic adjustment already includes `Exposure2012`. A supported single local gradient mask is evaluated before crop; global tone processing follows. The default LRT keyframe count uses complete lrt:ExternalExposureDefault / 4 XMP inputs when available, with measured sRGB luminance as a fallback. `keyframes --lrt-preview camera|visual` explicitly measures the selected LRT JPEG cache instead, even when those XMP inputs are present; this lets cache-only RAW sequences use their existing rendered pixels without decoding RAW originals. `preview --lrt-preview camera|visual` can plot brightness from that cache, and `long-term --lrt-preview camera|visual` can measure it under either metric profile. Missing or corrupt selected caches are errors; these commands do not silently fall back to RAW decoding or another cache. The optional `--luminance-deviation` placement and long-term metric formulas are lrtl-specific.
- `render --auto-orient` reads the EXIF orientation from the encoded raster, selected embedded preview, or DNGLab-developed TIFF using the existing Go metadata dependency. It applies the EXIF rotate/mirror transform before crop, tone adjustment, deflicker analysis, resize, and encoding; frames must agree in post-orientation dimensions. This is an independent pixel transform and does not update source metadata.
- A current `Mask/RangeMask` component can contain `CorrectionRangeMask` with `Type=2` and four ordered `LumRange` values. A public Lightroom XMP example uses values such as `0.603921 0.843137 1.000000 1.000000` and combines the component with a gradient using `MaskBlendMode=1`; `MaskValue=0` plus inversion flags suggests that the range selector is not a spatial base mask by itself. lrtl evaluates a smoothstep ramp through those four luminance breakpoints over linear-sRGB luminance, honors the component and range inversion fields, and combines it with supported gradients. Color `Type=1`, depth `Type=3`, legacy correction-level range masks for pixel rendering, and image-dependent sample rebuilding remain unsupported. This is an independent approximation; Lightroom renders have not been used for numerical comparison. See the public [Lightroom XMP example](https://forum.lrtimelapse.com/Thread-issue-with-luminance-range-mask-intersected-with-linear-gradient-mask?pid=57463) and Adobe masking guide (https://helpx.adobe.com/lightroom-classic/desktop/process-and-develop-photos/masking.html).
- The installed `raw_m_1.xmp` template serializes local corrections under `crs:GradientBasedCorrections` and `crs:CircularGradientBasedCorrections`; `raw_m_2.xmp` uses `crs:MaskGroupBasedCorrections` for the same correction/mask records. Their `crs:CorrectionMasks` contain `Mask/Gradient` (`ZeroX/ZeroY`, `FullX/FullY`) or `Mask/CircularGradient` (`Top/Left/Bottom/Right`, `Angle`, `Feather`) entries. With `--apply-xmp-basic`, lrtl renders LocalExposure2012, LocalContrast2012, LocalHighlights2012, LocalShadows2012, LocalWhites2012, LocalBlacks2012, LocalSaturation, LocalTexture, LocalClarity2012, LocalDehaze, LocalSharpness, LocalTemperature, LocalTint, LocalToningHue, and LocalToningSaturation when a correction has one or more active supported `Mask/Gradient`, `Mask/CircularGradient`, or luminance `Mask/RangeMask` components from any of those three collections. The local scalar tone, saturation, temperature, tint, and LocalSharpness controls are serialized as normalized fractional XMP values in Lightroom examples; lrtl multiplies those fields by 100 to recover the -100 to +100 slider scale, while LocalExposure2012 remains in EV. LocalToningSaturation is treated as a normalized 0-to-1 value. These unit conversions are based on observed XMP examples, not an Adobe schema declaration. The renderer reuses the global sRGB tone equations scaled by mask weight, and local temperature/tint interpolate estimated RGB gains from neutral. LocalTexture and LocalClarity2012 use masked high-pass luminance from separate box-blur radii as a detail-preserving approximation. Global/LocalDehaze use a wider-radius high-pass contrast lift for positive values and a neutral luminance veil for negative values, with a small saturation adjustment; this avoids per-frame atmospheric-light estimation but is not the Camera Raw Dehaze model. `LocalToningHue`/`LocalToningSaturation` approximate the local Color swatch by blending an HSL tint at the source pixel lightness, scaled by saturation and mask weight; see Adobe local-adjustment documentation at https://helpx.adobe.com/lightroom-classic/desktop/process-and-develop-photos/apply-local-adjustments.html. Linear masks use a clamped linear ramp; radial masks decode the signed bounds and bounded `Angle` into an oriented oval. Feather remains a smooth approximation. Multiple components follow MaskBlendMode: 0 adds using alpha-coverage union, and 1 intersects by multiplying coverage. The Add equation is an independent blend approximation and has not been checked against Camera Raw; Mask/Aggregate containers are traversed, but all active leaf components must be supported gradients or current luminance range selectors. Gradient components apply `MaskValue`; range selectors use full component coverage because observed XMP stores `MaskValue=0` for them. Modern `MaskInverted` and legacy `Flipped` are component inversion toggles (both active cancel each other); range selectors also honor `CorrectionRangeMask Invert`. Adobe documents component inversion in its [Lightroom Classic masking guide](https://helpx.adobe.com/lightroom-classic/desktop/process-and-develop-photos/masking.html). This mapping is an independent approximation: midpoint/roundness and exact Adobe feather semantics are not reproduced; compositions involving unsupported mask types, brush/AI/color/depth range masks, legacy range selectors, extended local curves and other local sliders remain unsupported. XMP transitions apply the registry-based numeric spline to matching IDs, copy the first nonempty `LumRange` string across matching records, and leave selector sample data untouched; exact parity has not been compared against an LRT-generated transition.

## References

- [Adobe Camera Raw namespace reference](https://developer.adobe.com/xmp/docs/xmp-namespaces/crs/)
- [Adobe Lightroom Classic local-adjustment controls](https://helpx.adobe.com/lightroom-classic/desktop/process-and-develop-photos/apply-local-adjustments.html)
- [Adobe Community XMP sample showing fractional local correction values](https://community.adobe.com/questions-675/syncing-range-masks-in-lr-classic-7-3-933460)
- [Adobe Lightroom Classic masking guide](https://helpx.adobe.com/lightroom-classic/desktop/process-and-develop-photos/masking.html)
- [Adobe XMP specification overview](https://developer.adobe.com/xmp/docs/xmp-specifications/)
- [DNGLab CLI, RAW format support, and project status](https://github.com/dnglab/dnglab)
- [Adobe Photoshop edit operations example showing Camera Raw XMP controls](https://developer.adobe.com/firefly-services/docs/photoshop/guides/photoshop-v2/v1-to-v2/edit-operations)
- [LRTimelapse 7 official feature matrix](https://lrtimelapse.com/features/)
- [LRTimelapse internal workflow](https://lrtimelapse.com/workflow/internal-workflow/)
- [LRTimelapse visual workflow](https://lrtimelapse.com/workflow/visual-workflow/)
- [LRTimelapse Long Term / Construction workflow](https://lrtimelapse.com/workflow/long-term-construction/)
- [LRT developer discussion of 2*/3* Holy Grail markers](https://forum.lrtimelapse.com/Thread-handling-different-exposure-times-with-keyframes)
- [EXIF and exposure metadata reader used by lrtl](https://github.com/evanoberholster/imagemeta)
- [LRTimelapse basic tutorial](https://lrtimelapse.com/tutorial/basic/complete/)
- [LRTimelapse 7.2 release notes: Simple Deflicker](https://lrtimelapse.com/updates/lrtimelapse-7-2/)
- [LRTimelapse 7.1 release notes: XMP equalization on Auto-Transition](https://lrtimelapse.com/news/lrtimelapse-7-1-released/)
- [LRT developer guidance on animating initialized masks between keyframes](https://forum.lrtimelapse.com/Thread-masks-on-keyframes-tip-on-adjusting-exposure-of-the-moon)
- [LRTimelapse discussion with an XMP example of intersected masks](https://forum.lrtimelapse.com/Thread-issue-with-luminance-range-mask-intersected-with-linear-gradient-mask?pid=57463)
- [LRTimelapse 7.4 release notes](https://lrtimelapse.com/updates/lrtimelapse-7-4/)
- [LRTimelapse 7 release notes: Select by Filename](https://lrtimelapse.com/news/lrtimelapse-7/)
# WB numeric null-reference branch

The installed 7.5.5 automatic worker `A(Rsa,int,boolean)`, bytecode offsets
216–268, tests the reference cell before selecting numeric interpolation. A
null reference clears that column on every frame when values differ. The
reference is the first rating-4 frame, falling back to frame zero. The four WB
numeric fields bypass metadata missing-value preflight, so this branch can
remove values that exist on later keyframes. `WhiteBalance=Custom` does not
create a numeric reference. A numeric zero remains present.

lrtl now applies that branch separately to Temperature, Tint,
IncrementalTemperature and IncrementalTint after numeric validation and before
anchor extraction. Planning records removals; disk writes retain the existing
backup and atomic-sidecar behavior. All-absent columns produce no edits and a
single-frame sequence is not cleared. Analytical regression tests cover
interior references, mixed ratings, zero, independent columns, CLI preview,
backup contents, repeated no-op planning and malformed-input preflight. These
are bytecode-derived branch tests, not successful whole-application oracle
runs or Camera Raw pixel comparisons.

## WB numeric column missing-value stage

`A(Rsa,int,int,boolean)` calls the common missing-value helper at bytecode
offsets 9–15 before reading the column's runtime type and collecting numeric
anchors. The sparse wrapper calls the same helper when a missing value follows
an existing value. When the first reference is present and multiple rating-4
anchors select the numeric worker, lrtl now fills these four WB columns at this
stage. It records filled keyframe values as writes, including donors that are
not keyframes. The reference-null removal still runs first. Single-reference
copying does not use donor filling.

`scripts/probes/WBNumericColumnOracle.java` independently invokes the installed
column component and produces `lrt-7.5.5-wb-numeric-column.json`: 16 successful
cases covering all four WB columns, missing selected anchors, leading/trailing
gaps and constant data. It obtains Integer/Double from the installed registry
instead of inferring the runtime type from artificial inputs. All four WB
columns register Integer. Mode zero uses rating-2 anchors and fixes tension to
zero without reading the GUI slider. Tests map those anchor positions to
ordinary rating-4 keys and Go smoothness 100; this is a component comparison,
not the outer automatic application route. Additional regression tests compare
missing versus manually prefilled keyframes at smoothness 0/50/100. A CLI test
checks preview, backup contents, no-op repeat and a five-frame MJPEG AVI against
a control with the component's complete metadata output. The AVI comparison
tests lrtl's own WB rendering, not Camera Raw pixel equivalence.

## Numeric reference-copy dispatch

The installed automatic worker calls `Rsa.A(column,rating)` before selecting
the sparse or dense numeric worker. A selected-rating count of zero or one
returns true; otherwise it compares selected frames and both endpoints through
their numeric string values. On true, the caller obtains the first frame of
the selected rating (`Rsa.h`), falling back to the ordinary reference, and
copies that cell with `Rsa.A(column,frame)`. That component does not clamp,
round or convert the value to float32. Consequently an ordinary non-HDR
Exposure2012 of 99 can be copied unchanged in this branch.

`scripts/probes/NumericCopyOracle.java` successfully invokes the installed
gate and copy helpers for 14 synthetic cases. The JSON fixture covers ordinary
and crop ratings, no marks, interior references, equal anchors with differing
non-keyframes, unequal endpoints, out-of-range integer/Double values,
float32-overflowing but finite Double values, high precision, tiny numbers and
signed zero. The signed-zero case confirms that Java's Double string equality
does not equate -0.0 with +0.0. The probe obtains overloaded methods by return
type as well as parameters. It does not invoke the outer workflow or establish
native spline output for cases whose copy gate is false.

lrtl evaluates this gate after ordinary numeric preflight and WB reference
removal, but before WB column-worker filling. Copy writes use round-trippable
double text and exact numeric comparison, preserving tiny values and signed
zero instead of the interpolation formatter's 12-significant-digit/near-zero
normalization. Regression tests compare all successful copy results at
smoothness 0/50/100. The previous single-anchor-clamp test was corrected using
this evidence; overflow rejection now tests differing anchors entering the
numeric worker. Local numeric dispatch remains separate. A CLI integration test
copies an interior +6 EV reference, checks backup and no-op repeat, and decodes
the resulting three-frame AVI against an independent linear-sRGB exposure
formula. This validates lrtl rendering and metadata, not Camera Raw equivalence.

## Local numeric reference copying

The same installed automatic dispatch visits registered local numeric columns.
`NumericCopyOracle.java` now accepts an optional registered path. Two successful
runs for `GradientBasedCorrections[1]/crs:LocalExposure2012` and
`GradientBasedCorrections[1]/crs:CorrectionMasks[1]/crs:ZeroX` provide 28 gate/copy
cases in `lrt-7.5.5-local-numeric-copy.json`. These invoke the actual sequence
gate and copy helpers; they do not establish complete application XMP writes or
native interpolation output when the gate is false.

lrtl applies reference copying after local metadata missing-value preparation
and before sparse/dense numeric interpolation. Only registered positional
corrections and first selectors participate. Copied values retain double
precision and bypass numeric-worker clamping and float32 checks. Radial Angle
post-processing still runs once. The parser validates finite numbers first;
float32 overflow validation now occurs after dispatch, and still rejects
columns entering a numeric worker or the SyncID fallback. The previous local
overflow test now uses differing selected values to exercise that worker.

Existing structure policies remain: missing non-keyframe positional resources
can be cloned; missing selected root corrections receive registered scalars without cloning source IDs/unsupported trees, and IDs/other
fields on existing targets survive numeric writes. Tests compare the component
fixtures at smoothness 0/50/100, exercise missing non-keyframe resources across
all four collections, and exclude unregistered positions/SyncID/second
selectors. A CLI test checks a +6 EV local reference through preview, backup,
preserved per-frame correction IDs, no-op repeat, and a three-frame AVI checked
against an independent linear-sRGB formula. This pixel check is for lrtl's own
rendering and does not claim Camera Raw equivalence.

During validation, the WB control-video test exposed a clock-dependent failure:
icza/mjpeg writes `time.Now()` into the AVI header comment. Two renders across
a clock second differed in the comment while frame/index payloads were equal.
The test now validates AVI signatures, compares payloads from `movi` through
the index, and retains decoded-frame count/pixel checks. The revised test
passed 200 consecutive runs. It still compares the actual encoded frame data.

## Local boolean transitions

Installed registry lookup confirms 26 local Boolean columns: CorrectionActive
at the 16 supported correction positions, plus Flipped at Circular selectors
1–2 and MaskGroup selectors 1–4/8–11 (CorrectionMasks[1] only). Linear Flipped
and MaskInverted are not registered there. `LocalBooleanOracle.java` queries
the installed registry and successfully invokes missing-value preflight,
selected-rating equality and reference-copy components for 24 synthetic cases
across correction and selector paths. All-absent columns skip the gate, as in
the caller's missing-column handling. Boolean columns with differing values
retain the filled sequence; they are not numerically interpolated. These are
component comparisons, not complete outer-application/save oracle results.

lrtl now plans those booleans from existing resource attributes or scalar
children, applying ordinary metadata preflight before the root-equivalent
reference/copy dispatch. Comparison uses boolean meaning, so unchanged lexical
forms such as tRuE are preserved; newly assigned values use True/False. Invalid
or structured registered booleans fail preflight. Missing selected resources,
SyncID fallback, unregistered slots, second selectors, MaskInverted and linear
Flipped retain the existing scope policy. Existing target IDs are preserved.

The previous angle test expected False/True/False activity to survive equal
False endpoint keys. The installed gate/copy fixture disproved that expectation;
the test now expects False across the sequence while continuing to check
one-time radial-angle normalization and no-op repeat. Additional tests cover
registry parity, all 24 component results, invalid-data/no-write behavior and
preservation of unregistered flags and lexical forms. A CLI integration test
uses an interior active/unflipped MaskGroup key against inactive/flipped
non-keys, checks backups and per-frame IDs, and verifies all three rendered AVI
frames against an independent +1 EV sRGB calculation. That pixel test validates
lrtl rendering, not Camera Raw equivalence.

## Capture-gap import splitting

The official [LRTImport workflow](https://lrtimelapse.com/workflow/lrtimport/)
describes importing multiple time-lapse sequences and links its split tutorial.
The official [LRT 4 announcement](https://lrtimelapse.com/news/lrtimelapse-4-this-is-new/)
explains that displaying capture intervals assists splitting multiple sequences.
Those sources support the workflow requirement, not any specific automatic
scene detector or native gap threshold. lrtl's `import --split-gap DURATION` is
an independent CLI implementation with an explicit user-supplied threshold.

The import plan requires XMP/EXIF timestamps, sorts by capture instant, and
starts a numbered/date-stamped sequence folder only when the adjacent interval
strictly exceeds the threshold. Equal timestamps retain natural source-path
ordering; timezone offsets and subsecond capture differences are respected.
Split imports can use date renaming, recursive source paths, JPEG exclusion
and existing DNG conversion planning. Every target and timestamp is checked
before publication. Generated destinations are checked against the resolved
output root, existing files are preserved, and failed copies remove only
created files and empty created sequence directories.

Tests cover exact gap boundaries, ties, reversed filenames, offsets,
millisecond thresholds, date-name collisions, dry-run/no-write, existing target
and invalid-parent preflight, missing capture metadata, recursive DNG plans,
disabled-option compatibility, and rollback after a source disappears. A CLI
test compares all copied image/XMP bytes to their sources and renders both
resulting sequences to real AVI files with their expected frame counts. This
does not compare against native automatic split decisions; DNG split tests
check the plan, with converter execution covered separately by existing tests.

## Manual geometry pixel application

The installed numeric registry already supplies PerspectiveRotate bounds
(-10..10) and PerspectiveScale bounds (50..150). Adobe's
[Geometry documentation](https://helpx.adobe.com/lightroom/desktop/edit-photos/crop-rotate-geometry.html)
describes rotation and uniform scaling, alongside other manual and Upright
controls. lrtl now consumes those two XMP values under apply-xmp-basic using an
independent centered affine transform. Scale defaults to 100 percent; positive
rotation is clockwise in screen coordinates. This is not a measured Camera Raw
transform/resampler oracle.

The fixed oriented canvas is retained. Source pixels are transformed after
local corrections and before the existing crop/global-tone stages. Areas
outside the source are transparent, composited on black by video output. The
shared spatial stage is used by analysis, rendered-pixel feedback, ordinary
rendering and the Motion Blur preparation paths. High-precision source types
retain 16-bit destinations and alpha. Default geometry returns the original
image without resampling. Horizontal, vertical, aspect, Upright, automatic
fitting and lens distortion remain unimplemented pixel controls.

Tests compare 16-bit linear ramps against an independent inverse affine map
at pixel centers, including nonzero image bounds and both rotation signs.
They check more than 900 distinct output levels, alpha, transparent borders,
default no-op, parser bounds and geometry-before-crop analysis ordering. A CLI
test marks and edits scale endpoints, interpolates 100/125/150, and decodes
the actual AVI frames against calculated sample positions, with/without XMP
application and with two-frame Motion Blur. Rendered-pixel checks preserve
source images and XMP, and invalid geometry on a later frame must not publish
an output video. The comparisons validate lrtl's implementation, not native
Camera Raw pixel equivalence.

## Measured Holy Grail shape and camera-pair helpers

Two independent callers now invoke installed numerical helpers without opening
the application UI. They allocate isolated controller objects; the shape caller
supplies a table containing only its synthetic sequence. They neither edit
image/XMP inputs nor alter the installed JAR. The probes require the user's
installed Java application; normal Go tests consume only synthetic JSON.

`HolyGrailShapeOracle.java` completed 112 cases spanning 7 input/rating patterns
and 16 Stretch/Rotate combinations. The helper uses original Preview EV,
accumulates offsets only at adjacent 2*/3* pairs, and returns zero if there is
no such pair. Its normalization min/max scan starts at frame one, so frame zero
can normalize outside [-.5,+.5]. The centered position uses float32 division
and subtraction before promotion to double. Go matches recorded results within
1e-12. lrtl retains a finite zero-shape fallback for degenerate zero-span data;
those native non-finite cases are not part of the successful fixture.

`HolyGrailPairOracle.java` completed 27 shutter/ISO/aperture combinations through
the installed private numerical helper. This exposed an earlier interpretation
error: `lrtimelapse.tools.com3.A(2,ratio)` computes log(ratio)/log(2), not
log2(2/ratio). The corrected shutter term is log2(before/after), with unchanged
ISO/aperture guards and division by four. Identical camera settings now yield
zero pair correction. Go agrees within 1e-14. These comparisons exclude the
upstream shutter/aperture string parsers and complete HG controller/save paths.

The controller's fixed .5 LOWESS applies to the separate shape array before
addition, not to the combined base curve. lrtl now respects that order. A
31-frame analytical regression preserves a fluctuating base when the shape is
zero. Other tests cover the marker gate in the combined calculation and finite
degenerate behavior. A CLI integration fixture generates its own JPEG/EXIF
(1 second, f/8, ISO 100), applies stored LRT inputs, checks analytically derived
compensation, preview/no-write, backups, repeat no-op, source ratings, reset and
five actual AVI frames against an independent sRGB exposure formula. It also
checks positional parsing of lrt-reduce-deflicker. Complete Holy Grail output,
Auto Shape optimum, clip edge cases and Camera Raw pixel parity remain unproven.

## Holy Grail numerical-controller comparison and ISO preprocessing

`HolyGrailControllerOracle.java` completes 66 calls to the installed
`cOM7.a(Rsa,float,float,float,float,boolean)` numerical controller, with
synthetic Preview EV, rating and camera metadata. The callback/UI branch and
clipping are disabled by its final flag. Inputs are small, ISO changes are
bounded, and the caller asserts finite final values with absolute magnitude
below one, so the disabled clipping does not distinguish these cases from the
manual numerical branch. No image/XMP or application settings are modified.
The cases cover 5/17/20/31 frames, adjacent 2/3 and 4 marks, 100/125 ISO,
three stretch values, two rotations and smoothing 0/.35/.5 where the installed
entry point finishes. One-neighbor smoothing cases are omitted. Both stored
raw and 1.5-smoothed curves agree with Go to absolute 2e-8 at every frame.

The initial comparison failed because the frame-pair wrapper parses ISO through
`lrtimelapse.tools.c.a(String)`, its shared exposure-number parser, before Java
int conversion. Canonical label 100 becomes 101.59366732596479 then 101, and
125 becomes 128. This differs from feeding raw EXIF ISO directly to the private
pair arithmetic helper. `HolyGrailISOOracle.java` records 140 inputs including
all integers 1..99, common larger labels, fractional values and int saturation.
lrtl applies the measured truncated-label mapping in its LRT-input frame-pair
wrapper, retaining the separately measured raw numerical pair helper. Unknown
labels use numeric truncation with Java int bounds. This does not prove the
full native formatter/parser behavior for every possible metadata string.

The installed controller receives Stretch/Rotate/Reduce Deflicker as float,
then promotes them for calculation. lrtl now does the same. In particular,
.35f times 20 lies below seven and selects six LOWESS neighbors. Decimal CLI
controls are tested against the measured 20-frame controller results, not just
against inputs already rounded to float32.

An integration test generates JPEG/EXIF with the fixture's changing ISO and
stored Preview EV, writes compensation through the CLI, checks XMP against the
actual controller outputs, retains original ratings/profile data and backup
bytes, then decodes all AVI frames against a separately calculated sRGB gain
using native measured EV. Source JPEGs are unchanged. The comparison does not
cover native XMP serialization, clipping-sensitive inputs, arbitrary shutter/
aperture formatting, Auto Shape optimal parameters or Camera Raw pixel output.

## Holy Grail clipping and Auto Shape grid

`HolyGrailRangeOracle.java` successfully invokes the installed range helper for
84 inputs/bounds/clip combinations. Go matches every double bit, including
signed zero and values near 1e300. The helper seeds its maximum with lower-1
and minimum with upper+1, then calculates the shift in a particular subtraction
order. Replacing that with the observed min/max midpoint was incorrect for
entirely out-of-range columns and for some floating-point cancellation cases.
lrtl now retains the sentinels and arithmetic order, with clipping optional.

Inspection of `prN` confirms that both candidate loops invoke the numerical
controller with its clipping/UI flag true. Candidate generation uses Java float
division followed by float subtraction: step/50f-1.5f and step/25f-4f. The old
Go search scored clipped manual-mode results and rounded double-generated
candidates afterward. lrtl now scores unclipped curves and generates candidates
in float32. Normal compensation publication still uses clipping.

`HolyGrailAutoOracle.java` independently drives the installed controller over
the same two grids and reads its stored raw/1.5-smoothed arrays. Six successful
cases cover 17/31/61 frames and reduce 0/.35. Go selects exactly the same
float32 Stretch/Rotate values; scores and selected curves agree within 2e-8.
The fixtures also record a wide rotation candidate exceeding magnitude one,
and Go matches that unclipped curve while the manual branch stays within its
bounds. Maximum magnitudes observed during the complete grids exceed two, so
these cases exercise the need for unclipped scoring. This invokes the actual
numerical controller but not the UI/progress worker itself, and does not prove
all possible Auto Shape outcomes or its metadata parsing paths.

The Auto Shape CLI integration saves the measured chosen result and checks a
17-frame AVI against native measured EV and an independent sRGB formula.
A separate analytical composition of the directly measured range helper tests
constant Preview EV=40: successive manual stages give -10, -4.5, then -1.
That fixture also runs CLI save/backup/render/source-preservation checks, but
is not represented as a successful full manual-mode native-controller oracle.

## Cached Holy Grail camera metadata

HolyGrailMetadataOracle.java measures the installed numeric string and aperture
parsers and decodes the camera field names used by the frame loader. They are
ShutterSpeed, Iso and Aperture in http://lrtimelapse.com/. Capitalization matters.
The fixture contains 137 numeric/shutter strings and 45 aperture strings. Go
matches the numeric double bits and 43 valid aperture results. For the two
malformed aperture labels F8 and f/8, lrtl deliberately rejects the input instead
of accepting the native parser's zero fallback.

HolyGrailMetadataPairOracle.java supplies parsed synthetic camera strings to the
actual frame-pair wrapper in 36 cases. Go agrees within 1e-14, including decimal
ISO spellings and aperture labels with trailing zeros. The lexical distinctions
100 versus 100.0 and 5.6 versus 5.60 are preserved; cached ISO values must not
pass through the canonical integer-label map a second time. Tables contain only
measured numerical facts, not native code. Probes are development tools; Go
runtime and tests do not require Java or an installed LRTimelapse.

The --lrt-input path now prioritizes these cached fields and reads EXIF only
when a required field is absent. A PNG fixture without EXIF exercises CLI save,
backup, retained camera metadata and AVI pixels against measured controller EV
and an independent sRGB gain formula. Partial-cache EXIF fallback and nine
malformed-late-frame cases check validation before XMP or backup writes.
The default physical EXIF mode is unchanged. Native formatting of arbitrary raw
EXIF into camera labels (extended below), all string forms, full save/display workflows and
Camera Raw pixel equivalence remain outside this comparison.

## EXIF display labels before Holy Grail calculation

The native importer requests the displayed ShutterSpeed, Aperture and ISO from
its ExifTool process, then stores those strings in the frame model. Directly
feeding raw ExposureTime and FNumber into HG omitted both display rounding and
the native nominal-label conversions. HolyGrailExifOracle.java creates temporary
synthetic metadata-only JPEGs and measures installed ExifTool 13.59 followed by
the native number/aperture parsers. Its 94 cases cover common shutter times,
the quarter-second formatting threshold, decimal rounding boundaries, common
apertures and apertures below one. The committed fixture records source
rationals, displayed labels and native parsed values; tests require exact double
bits. The native import controller itself is inspected, not invoked in this probe.

The compatibility path now formats missing cached fields from EXIF before
parsing them. A precision pass uses imagemeta's custom tag parser to retain
ExposureTime/FNumber rationals in double precision: its public metadata fields
otherwise round to float32 before formatting. In particular, raw 2.35 rounded
down through float32 would produce a different display label. JPEG and both TIFF
byte orders exercise the original rational values. The default physical EXIF
mode retains its existing behavior; cached strings retain their original spelling.
Additional measured aperture labels with .0 suffixes are registered separately.

HolyGrailControllerOracle.java camera mode invokes the installed numerical
controller for 12 cases, with 17 frames, changing shutter/aperture/ISO, mixed
2/3/4-star markers and different Stretch/Rotate/Reduce settings. Inputs avoid
clipping so the UI-disabled native invocation has the same numerical bounds.
Each case runs through generated JPEG/EXIF, CLI XMP save/backup and AVI decoding;
saved EV agrees within 2e-8 and decoded gray pixels within two 8-bit levels of
an independently computed sRGB gain using the native measured EV. A cached-only
PNG case uses the same changing camera labels. Source preservation and retained
metadata are checked. APEX precedence is extended below; Bulb and manufacturer-specific camera tags,
arbitrary formatting, native save/display behavior and Camera Raw pixels are
still not covered. Runtime and Go tests require neither ExifTool nor Java.

## APEX camera exposure fallback

HolyGrailApexOracle.java measures installed ExifTool composite ShutterSpeed and
Aperture labels followed by the installed HG parsers for 112 synthetic EXIF
cases. These include shutter APEX values from -5 to 13 in thirds, aperture APEX
values from -4 to 12 in thirds, signed/unsigned shutter declarations, the native
absolute-value cutoff at 100, ordinary-tag precedence and reversed tag order.
The fixture records the original tag IDs/types/rationals, labels and parsed
numbers. Go requires exact parsed double bits for valid inputs. Missing aperture
or nonpositive shutter results are deliberately rejected before saving.

The precise metadata pass now keeps primary tags and APEX alternatives separately
and resolves them after scanning. ExposureTime is preferred when present, even
if zero; FNumber falls back to ApertureValue when zero or absent. ShutterSpeedValue
is interpreted as signed even when declared unsigned, matching the installed
ExifTool's camera correction. The composite Aperture logic treats a raw APEX
zero as absent, despite its mathematical conversion yielding f/1. The 112 cases
also exposed the native 1.1 aperture-label conversion, now recorded in the table.
The default physical EXIF mode remains unchanged.

The controller probe's apex mode uses independently measured APEX-derived labels
in 12 native numerical-controller calls. Each resulting fixture records source
APEX rationals as well as the native labels and EV arrays. Go tests generate
actual JPEG EXIF containing only ShutterSpeedValue/ApertureValue and ISO, then
run CLI save, backup and 17-frame AVI pixel checks against native measured EV.
No Java or ExifTool is needed by these Go tests. Additional preflight tests
reject a late invalid shutter or missing APEX aperture without creating backups
or changing any image/sidecar; --ignore-aperture accepts the latter input.
These comparisons do not cover native import/save/display workers, Bulb or
manufacturer-specific precedence, arbitrary EXIF forms, or Camera Raw pixels.

## Interval-based sequence grouping (LRT 7.5.5)

The installed video-import path invokes FFmpeg to decode every frame to JPEG, with no scene detector or scene-splitting filter in that call. The separate “Split by Intervals” action opens the photo-sequence interval model. This corrects the earlier README implication that scene splitting was a demonstrated missing counterpart of video import.

Inspection of the photo interval calculation found outgoing millisecond intervals (next capture time minus current capture time) and a final sentinel of -1. The grouping model compares each interval with the previous interval, with a default tolerance of 1000 ms. A changed interval closes a run of at least two images after adding the current image; a shorter run is closed before the current image. Initial nonpositive intervals are collected separately. Runs shorter than the minimum length (default five images) are accumulated into one final group; no frames are discarded.

A local reflection harness populated synthetic frame records and executed the installed grouping model and its minimum-length filter. `internal/lrtl/testdata/lrt-7.5.5-interval-split.json` records 240 cases: lengths 1–24, tolerance 0/1/1000/1001/5000 ms, minimum lengths 1/2/3/5/10, random interval changes, constant runs, ramps, duplicate times, isolated gaps and the final -1 sentinel. The Go test compares exact ordered group membership and separately checks that every frame occurs once. Only numerical observations are included; the probe, decompilation output and product code are not distributed.

lrtl applies the grouping to capture-time-sorted import copies. Its `sequence_NNN_timestamp` and `single_images` folders preserve all images and sidecars. Native folder renaming, moving files out of the currently loaded sequence and GUI behavior are outside this comparison. Timestamp decoding is lrtl's existing XMP/EXIF reader; this does not establish equality with the product's timestamp parser for every metadata representation.

## Render Luma/Chroma Deflicker (7.5.5)

The installed static filter helper was run for all four boolean combinations.
Enabled Y/U/V planes independently use the ten-frame power mean; the caller
places this before output crop, denoise and light-trail removal. The CLI now
supports `--output-deflicker-luma` and `--output-deflicker-chroma`, preset fields,
explicit false overrides and batch forwarding. Native preset rendering measures
the entire scaled canvas before crop. Standalone CLI filters its prepared canvas.

288 bundled-FFmpeg observations cover 686,556 exact 10-bit plane samples at two
dimensions and eight sequence lengths, including short clips, zero planes and
EOF queue shrinkage. A bounded Go stream is compared to an offline reference;
CLI tests include crop position, stage order, JPEG AVI, still exports, future
input failure retention and 18 real H.264/H.265 outputs. Direct helper graphs
and the inspected caller order do not establish gated full-application output
or complete color-management equivalence. See
[`lrt-output-deflicker-7.5.5.md`](../internal/lrtl/testdata/lrt-output-deflicker-7.5.5.md)
for scope and reproduction.

## Playback blend interpolation (LRT 7.5.5)

The installed helper produced 108 graphs across three fps labels, twelve speeds
and three interpolation modes. Mode 1 now has an independent pure Go blend,
scene-change detection, rational intermediate clock, EOF hold and uniform
pre-resampling fast-speed average. The mode-2 fast branch is the same observed
filter and is supported; the slow bidirectional branch is now implemented and
compared in the motion section below.
Presets, explicit disabling, batch, subtitles and combined temporal filters use
the same scheduler. Still exports do not interpolate.

2,304 bundled-FFmpeg references hash 96,523,260 8/10-bit plane samples and verify
output counts. Input formulas are synthetic and reproducible. Widths 16/32 had
native SIMD scene decisions differing from scalar; the native 64x32 and scalar
17x13 comparison domains are recorded separately. Eighteen real H.264/H.265 cases
compare every decoded sample against a separate offline-reference encode, plus
metadata. Tests also cover JPEG AVI, ownership, input errors, dropped source
tails, memory guards and retention. Full native color conversion and gated
application output remain unverified. See
[`lrt-interpolation-7.5.5.md`](../internal/lrtl/testdata/lrt-interpolation-7.5.5.md).

## Stored fps label conversion and subtitle presets (LRT 7.5.5)

The static rate helper and half-speed generator were run for 156 labels. Native
labels within .001 of their nearest integer become integer fps; others become
roundedInteger*1000/1001. Stored preset fps now uses this mapping, including the
observed Java exponent/hex/suffix forms. Explicit CLI fps remains literal.
282 bundled-FFmpeg graphs verify full output hashes, counts and source IDs for
the translated rate and original-label interpolation clock. Five full native
graph-builder observations and real H.264/AAC outputs verify independent raw
label audio/fade durations, including audio shorter and longer than video.

Subtitles can load the same direct/named render preset and retain explicit flag
priority. Tests cover shared timing, batch video equality, settings/source
retention and failure retention. The comparison excludes the separate native
UI fps helper and full color management. See
[`lrt-rate-label-7.5.5.md`](../internal/lrtl/testdata/lrt-rate-label-7.5.5.md).

## Bidirectional motion interpolation (LRT 7.5.5)

The mode-2 helper chooses bidirectional estimation. That branch uses fixed
16x16 blocks and 32x32 cost/overlap support, with predicted zonal search and
prior vector history; its aobmc/vsbmc settings do not activate the separate
bilateral adaptive/variable-size implementation. Pure Go now supports this
actual branch. It computes integer weighted sums directly instead of storing
32 vectors at every pixel, while preserving the native reference cap/order.

720 complete 8/10-bit observations cover 96,085,440 samples across static,
panning, accelerating, moving-object, noise and cut sequences. All hashes and
frame counts match; 468 cases differ from simple blend. CLI/preset/batch/SRT,
combined temporal filters, full output filter order, independent overlap
reference, fuzzing, JPEG AVI and 18 actual H.264/H.265 cases are tested. Small
outputs and future-input errors preserve existing destinations. Full native
color management and gated whole-application render parity remain unverified.
See [`lrt-motion-interpolation-7.5.5.md`](../internal/lrtl/testdata/lrt-motion-interpolation-7.5.5.md).
