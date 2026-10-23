# Frame initialization and relative WB (LRTimelapse 7.5.5)

`init` now defaults to `--frame-state lrt-7.5.5`, adding quality classification,
WB defaults and the previously implemented WB/crop save preparation to mask
initialization. Use `--frame-state preserve` to initialize masks/headers while
retaining the former behavior for frame quality, WB and crop state.

```powershell
lrtl init SEQUENCE
lrtl init SEQUENCE --write
lrtl init SEQUENCE --frame-state preserve --write
lrtl set-sequence SEQUENCE IncrementalTemperature 25 --write
lrtl render SEQUENCE --output result.avi --apply-xmp-basic
```

## Quality and WB state

Quality reconciliation uses the **first 4-star frame, or the first frame if
there is no 4-star frame**. A 5-star crop keyframe does not take precedence.
The reference's classification is applied to the entire initialized sequence,
including mixed extensions, following the inspected native sequence rule.
Existing `lrt:Quality` values are replaced by this explicit initialization.

| Reference image | Initialized quality |
| --- | --- |
| JPEG (`jpg`/`jpeg`), PNG, TIFF (`tif`/`tiff`) | `FLAT` |
| DNG with a nonblank AsShotNeutral tag | `RAW` |
| DNG without that tag, or with a blank tag | `FLAT` |
| Other supported RAW extensions | `RAW` |

DNG classification inspects TIFF tag **50728 (AsShotNeutral)**, including
SubIFDs, with the bounded pure Go TIFF metadata reader. It does not infer the
result from PhotometricInterpretation or whether the image has already been
demosaiced. Read/bounds errors stop initialization before writing. The shared
reader continues to support TIFF XMP without changing tag-700 lookup order.

Exact `WhiteBalance=Custom` receives missing absolute defaults 5850/8 for RAW,
or incremental defaults 0/0 for FLAT. Existing cells are retained by that
helper, then the save preparation serializes the nine WB/crop numbers,
supplies missing `As Shot`, removes inactive WB numbers and recomputes HasCrop.
Other WB spellings retain their native case-sensitive behavior. Unknown
metadata, ratings, existing masks and source images are preserved. Root
initialization headers cannot be supplied accidentally by nested same-name
fields. Dry runs show quality and all five WB fields.

Tiny crop values can round to zero while initially enabling HasCrop. A later
fresh initialization can consequently change HasCrop once more, as in the
measured save-cache behavior. This is not hidden by an epsilon rule.

## Native evidence

`lrt-frame-state-7.5.5.json` records **426 conditions**:

- 196 completed CSV metadata-reader calls: 28 filenames × 7 neutral-column
  values, along with the native file-extension predicates.
- 224 completed WB-default calls: 8 cached quality states × 7 WB labels ×
  4 numeric states, including missing, fractional and invalid numeric text.
- 6 completed reference-selection calls covering 4-star precedence, repeated
  keyframes and the fallback when only 5-star or ordinary ratings exist.

The original [`FrameStateOracle.java`](../../../scripts/probes/FrameStateOracle.java)
invokes the installed `cOM7.A(String,tQa)` CSV reader, the three native flat-file
predicates, `tQa.h()` WB defaults and `Rsa.A()` reference selection. It creates
the controller shell with `Unsafe.allocateInstance` without its GUI
constructor; these measured methods do not use controller UI state. Unlike
the earlier edit-callback/save-prefix probes, these calls complete normally.
No main instance, licensing state or application bytecode is replaced.

The CSV phase is an intermediate result: for example, `.jpeg` initially
receives RAW, then the native reconciliation classifies it FLAT. The final
sequence reconciliation rule is derived from inspected `LRTimelapseMain`
control flow; the entire GUI initialization is not executed by this observer.
Its CSV neutral input is the `-AsShotNeutral` output column. The native DNG
predicate independently reads tag 50728 through its metadata extractor.

The optional test additionally passes **24 original synthetic DNG files**
through that installed DNG predicate. They exercise little/big endian classic
TIFF, root/SubIFD placement, absent values, rational/double/ASCII/zero values
and blank ASCII. They are minimal metadata-only TIFF fixtures named `.dng`,
not complete camera image/development fixtures. Every classification is
compared with the pure Go reader.
The normal suite retains the 426 observations without requiring Java/LRT.

## Relative WB pixels

The basic renderer now chooses incremental Temperature/Tint for an exact root
`lrt:Quality=FLAT`, ignoring the inactive absolute pair. Other/missing quality
labels keep the existing absolute-WB path. Invalid active incremental values
outside -100..100 or nonfinite values are rejected. Inactive absolute fields
cannot trigger WB validation errors for FLAT input.

This is an **independent sRGB approximation**, using the existing local-WB
convention: relative Temperature maps to `6500 + 40 × value` kelvin and tint
uses the existing green/magenta gain adjustment. This is not an observed
Adobe transfer curve. Zero relative WB preserves the neutral path, 16-bit
input differences and alpha. Tests compare relative output against explicit
absolute-WB metadata representing this mapping, including an AVI workflow
from initialization through relative edits. They do not prove Adobe pixel
equivalence.

## Tests and reproduction

```powershell
go test ./internal/lrtl -run '^(TestFrameState|TestInit|TestWBEdit|TestSaveState|FuzzFrameState|FuzzRelativeWBGains)' -count=1
go test -race ./internal/lrtl -run '^(TestFrameState|TestInit|TestWBEdit|TestSaveState|FuzzFrameState|FuzzRelativeWBGains)' -count=1
go test ./internal/lrtl -run '^$' -fuzz '^FuzzFrameStatePreparation$' -fuzztime 60s -parallel 2
go test ./internal/lrtl -run '^$' -fuzz '^FuzzFrameStateDNGMetadata$' -fuzztime 20s -parallel 2
go test ./internal/lrtl -run '^$' -fuzz '^FuzzRelativeWBGains$' -fuzztime 20s -parallel 2

$env:LRTL_NATIVE_JAR='C:/Program Files/LRTimelapse 7/LRTimelapse.jar'
go test ./internal/lrtl -run '^TestFrameStateInstalledComponents$' -count=1
```

Fuzzing exposed undeclared prefixed attributes that etree accepted but the
native SDK rejected. Binding that prefix during editing could retroactively
qualify an old attribute and create a duplicate. Semantic metadata operations
now reject undeclared prefixes before mutation. The original failing input is
retained as a regression seed; installed SDK rejection was checked separately.

## Validation recorded on 2026-10-23

- After the namespace fix, the full suite with installed LRTimelapse, FFmpeg
  and ExifTool comparisons enabled passed (775.273 s for `internal/lrtl`).
- The final full suite passed with CGO disabled, optional comparison variables
  unset and FFmpeg absent from PATH (106.233 s for `internal/lrtl`, including
  the added mixed-extension reference scenarios). Optional
  installed-component/codec tests skip in that configuration.
- Focused race tests including installed-component comparisons passed
  (28.930 s). That run covered 86.1% of statements in `frame_state.go`; this
  is focused file coverage, not whole-project coverage.
- Four additional mixed-extension CLI scenarios verify that the first 4-star
  reference determines the entire sequence's quality; their focused race
  run also passed.
- After fixing the undeclared-prefix defect, frame preparation completed
  910,704 fuzz executions in 60 seconds. Additional 20-second fuzz runs
  completed 4,847 DNG metadata, 216,235 relative-WB gain and 102,929 metadata
  paste executions without failure.
- Focused Go 1.23 tests, Go 1.26 `go vet ./...` and a `CGO_ENABLED=0`
  executable build passed.
- The built executable initialized three PNG sidecars, edited incremental WB
  and generated AVI output matching the explicit sRGB mapping control. Dry
  run, source-image preservation, repeated edits, reinitialization after
  editing and rejection of undeclared prefixes without writes passed.

## Remaining scope

This implements quality reconciliation, WB defaults and the measured WB/crop
save preparation within the existing mask initialization. Full original-WB
EXIF/CRD initialization, camera profiles, all other slider defaults and save
side effects, keyframe creation, history, sequence state/cache files and the
complete app initialization workflow remain incomplete. `copy-settings` and
numeric edits still use their existing cached/root quality rules; `init`
performs the new explicit image-based classification.
