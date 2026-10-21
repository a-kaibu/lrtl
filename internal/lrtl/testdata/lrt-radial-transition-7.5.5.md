# Modern radial-mask transitions

The CLI now transforms complete modern radial masks in registered MaskGroup
positions 8 and 9, selector 1, before interpolating their shape. It uses image
dimensions to retain physical diagonal vectors and a continuous rotation,
then restores normalized rectangle coordinates. It preserves ordinary four-star
keyframe shape values and does not serialize its transient helper coordinates.

## Independent native observations

Recorded on Windows with installed LRTimelapse 7.5.5 build 1059 and Java 25,
2026-10-21. The original reflection probes operate on isolated synthetic
sequences and never save application preferences. They do not include
proprietary application implementations or require GUI interaction.

`lrt-radial-helper-7.5.5.json` contains **120 cases**:

- legacy CircularGradient positions 1/2 and modern MaskGroup positions 8/9;
- 6000×4000, 4000×6000, and 1000×1000 source dimensions;
- seven-frame sequences with three selected keyframes;
- ten shape/angle patterns, including quadrant boundaries, reversed edges,
  degenerate radius, negative/large rotations, the 80-degree direction rule,
  and the strict three-degree opening-angle threshold;
- raw geometry before/after preparation, transient coordinates on keyframes,
  and restoration after supplying prescribed helper coordinates.

The modern helper uses registered paths and produces its coordinates. The
legacy helper emits a trailing `:` instead of `/` after `CorrectionMasks[1]`;
its constructed angle lookup is unregistered and the observed stages leave
the input geometry unchanged. Legacy transient fields are also unregistered.
This is evidence about these helper invocations in this version, not a claim
that every legacy application workflow is inactive. The previously measured
legacy angle-normalization stage remains separate.

`lrt-radial-pipeline-7.5.5.json` contains **180 modern component pipelines**:
two slots × three source sizes × ten patterns × smoothness 0/50/100. The
probe invokes native radial preparation, the native equality/copy gate, native
Bezier point generation and numeric postprocessors, and native restoration.
The harness bins the native spline samples in sequence order. It records the
interpolated transient coordinates as well as restored shapes and cleanup.
It does not run the complete application Auto-Transition caller or Camera Raw.

Go applies these results through attribute, child-element and RDF Resource
forms: **540 saved-shape comparisons**, seven frames per case. Keyframe shapes,
unknown metadata and absence of leaked helper fields are checked.

`lrt-radial-mixed-7.5.5.json` adds **180 component pipelines** with ratings
`[4, 0, 5, 4, 2, 0, 4]`, again checked through all three RDF forms (**540 more
saved-shape comparisons**). The extra marked frames have different shapes.
Preparation includes all positively rated frames, while numeric spline anchors
remain the ordinary four-star frames. Native restoration skips all positively
rated frames: the two-/five-star frames retain ordinary scalar-interpolated
geometry. These observations cover the same slots, dimensions, patterns and
smoothness values as the ordinary pipelines.

## Numerical model

The source rectangle's center and signed physical diagonal determine an acute
opening angle. Openings less than three degrees from the group median share
that median. Each anchor chooses an equivalent quarter-turn representation
that minimizes rectangle error. Rotations are unwrapped modulo 180 degrees;
the measured direction rule adjusts large opposing changes. A common offset
centers the angles before the five helper columns pass through the existing
native-compatible numeric spline/copy model.

Restoration uses each frame's physical dimensions. Unlike the older angle-only
postprocessor, the active modern geometry route retains original keyframe
angles and restores non-keyframe angles to the measured quarter-turn range.
Restored geometry is saved at binary64 round-trip precision so repeated
interpolation does not create backups solely from 12-significant-digit loss.

A square-mask regression exposes the importance of last-bit arithmetic: a
different candidate can rotate the interpolation path by a quarter turn.
The arctangent uses the permissively licensed
[pure Go fdlibm adaptation](../../fdlibm/README.md), checked against 2,304 Java
bit observations. Anchor candidate sine/cosine uses a bounded 256-bit Taylor
sum over the reduced interval. This reproduces the measured tie case without
changing the project's general pixel-rendering math. It does not establish
that every Java trigonometric intrinsic on every CPU has identical rounding.

## Dimension policy and integration

`exif:PixelXDimension` / `PixelYDimension` take precedence, followed by
`tiff:ImageWidth` / `ImageLength`. CLI sequences can fill missing dimensions
from raster headers or supported image EXIF metadata without developing RAW.
The fallback uses unrotated primary-image dimensions. Explicit dimensions must
be positive 32-bit integers. Invalid consumed metadata fails before saving.
Unrelated dimension fields are not inspected for incomplete/unselected radial
selectors. A sequence needs at least two frames for the new route.

The complete shape and dimensions must currently be available for every
selected anchor and every output frame. Unknown dimensions, missing anchor
shape fields and unregistered slots retain the existing component transition.
Those cases are not established as exact native radial behavior. RAW header
coverage and orientation conventions need more camera-specific comparisons.
Incomplete masks on additional positively rated frames are also outside the
recorded mixed-mark cases.

CLI tests exercise header fallback, dry-run preservation, backup contents,
unchanged keyframes, repeat idempotence and invalid future dimension metadata
preventing all writes. A seven-frame pure Go AVI before/after interpolation
confirms changed intermediate pixels with identical keyframe JPEG payloads.
That checks the existing Go mask renderer's integration; it is not Camera Raw
pixel equivalence. Fuzzing checks finite results, centers, physical diagonal
lengths, angle range, source preservation and determinism.
The idempotence fixture uses ordinary four-star anchors; mixed-rating
preparation depends on the other marked frames' current shapes and is not
established as repeat-idempotent. Batch tests compare one and four workers
using cloned input trees and verify both geometry and byte-identical output.

## Verification record

On 2026-10-21, Go 1.26.9 passed the focused native-reference, dimension,
CLI/AVI, backup and batch tests with the race detector (118.206 seconds for
the lrtl package). The focused reference tests also passed on Go 1.23.0.
The final `go test -json ./... -count=1 -timeout 20m` passed with live FFmpeg,
application-JAR and ExifTool references enabled (667.251 seconds for the lrtl
package), including the latest mixed-mark, CLI/AVI and idempotence changes.
`go vet ./...` and a `CGO_ENABLED=0` CLI build passed. Replaying the mixed-mark
Java probe from its checked-in source reproduced the reference file's SHA-256.

Two 10-second fuzz runs passed: 11,996 radial geometry inputs and 994,814
arctangent inputs. Focused ordinary-test statement coverage was 98.5% for
radial preparation, 100% for restoration, 88.6% for the transition series and
98.6% for the fdlibm package. These measure exercised code, not proof of
universal native equivalence. Normal tests use the stored synthetic references
and do not require Java or the application installation.

## Reproduction

The source probes are original measurement harnesses in `scripts/probes/`.
Application binaries are not distributed or needed for normal Go tests.

```powershell
javac -d work/radial-classes scripts/probes/RadialPathOracle.java scripts/probes/RadialPipelineOracle.java scripts/probes/RadialMixedOracle.java
java -cp 'work/radial-classes;C:\Program Files\LRTimelapse 7\LRTimelapse.jar' lrtlprobe.RadialPathOracle work/radial-helper.json
java -cp 'work/radial-classes;C:\Program Files\LRTimelapse 7\LRTimelapse.jar' lrtlprobe.RadialPipelineOracle work/radial-pipeline.json
java -cp 'work/radial-classes;C:\Program Files\LRTimelapse 7\LRTimelapse.jar' lrtlprobe.RadialMixedOracle work/radial-mixed.json
go test ./internal/fdlibm ./internal/lrtl -run '^TestAtan2JavaBits$|^TestRadial' -count=1
go test ./internal/lrtl -run '^$' -fuzz '^FuzzRadialMaskTransform$' -fuzztime 10s -parallel 2
```
